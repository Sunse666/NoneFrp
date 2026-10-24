// Package join connects a player to a room: it listens on a local address and
// forwards each connection to the host directly when that works, falling back
// to the relay when it does not.
//
// The point is that the player only ever types "localhost" into Minecraft. The
// host's address can change, the relay can be in another country, and none of
// that reaches the person playing.
package join

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Sunse666/NoneFrp/internal/proxy"
)

const (
	// lookupTTL is how long a resolved target is reused.
	lookupTTL = 20 * time.Second
	// dialTimeout bounds one upstream attempt.
	dialTimeout = 3 * time.Second
	// spliceIdle closes a connection nobody has used for a while.
	spliceIdle = 10 * time.Minute
)

// ErrRoomNotFound means the relay has no such room.
var ErrRoomNotFound = errors.New("room not found")

// Target is what the relay knows about a room.
type Target struct {
	Code         string   `json:"code"`
	Online       bool     `json:"online"`
	Endpoints    []string `json:"endpoints"`
	RelayAddress string   `json:"relay_address"`
}

// Options configures Serve.
type Options struct {
	// Relay is the relay hostname, with or without a scheme.
	Relay string
	// Room is the room code to follow.
	Room string
	// Listen is the local address, defaulting to 127.0.0.1:25565.
	Listen string
}

// baseURL turns what a person types into a URL.
func baseURL(relay string) string {
	trimmed := strings.TrimSpace(relay)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "://") {
		return strings.TrimSuffix(trimmed, "/")
	}
	return "https://" + strings.TrimSuffix(trimmed, "/")
}

// Lookup asks the relay where a room can be reached.
func Lookup(ctx context.Context, relay, code string) (Target, error) {
	var target Target

	url := fmt.Sprintf("%s/v1/room/%s", baseURL(relay), code)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return target, err
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return target, fmt.Errorf("lookup %s: %w", url, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusNotFound {
		return target, ErrRoomNotFound
	}
	if response.StatusCode != http.StatusOK {
		return target, fmt.Errorf("lookup %s: http %d", url, response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(&target); err != nil {
		return target, fmt.Errorf("decode lookup: %w", err)
	}
	return target, nil
}

// resolver caches the last target so a burst of connections does not hammer the
// relay.
type resolver struct {
	relay string
	room  string

	mu     sync.Mutex
	target Target
	at     time.Time
}

func (r *resolver) get(ctx context.Context) (Target, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.at.IsZero() && time.Since(r.at) < lookupTTL {
		return r.target, nil
	}

	target, err := Lookup(ctx, r.relay, r.room)
	if err != nil {
		// Keep serving the previous answer if the relay is unreachable.
		if !r.at.IsZero() {
			return r.target, nil
		}
		return target, err
	}
	r.target = target
	r.at = time.Now()
	return target, nil
}

// Serve listens on opts.Listen and forwards until ctx is cancelled.
func Serve(ctx context.Context, opts Options, logger *slog.Logger) error {
	address := opts.Listen
	if address == "" {
		address = "127.0.0.1:25565"
	}

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen %s: %w", address, err)
	}
	return ServeListener(ctx, listener, opts, logger)
}

// ServeListener is Serve with a listener the caller owns, which is what tests
// use to bind port zero.
func ServeListener(ctx context.Context, listener net.Listener, opts Options, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	resolver := &resolver{relay: opts.Relay, room: opts.Room}

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			return err
		}
		go forward(ctx, conn, resolver, logger)
	}
}

// forward connects one player to the best available upstream.
func forward(ctx context.Context, player net.Conn, resolver *resolver, logger *slog.Logger) {
	defer func() { _ = player.Close() }()

	target, err := resolver.get(ctx)
	if err != nil {
		logger.Warn("cannot reach the relay", "error", err)
		return
	}

	upstream, via, err := dialUpstream(ctx, target)
	if err != nil {
		logger.Warn("no route to the room", "room", target.Code, "error", err)
		return
	}
	defer func() { _ = upstream.Close() }()

	logger.Debug("player connected", "room", target.Code, "via", via)

	result := proxy.SpliceWith(player, upstream, proxy.Options{
		Idle:             spliceIdle,
		CloseOnServerEOF: true,
		// The client answers a status ping and closes: wait for the reply.
		CloseOnClientEOF: false,
	})
	logger.Debug("player finished", "via", via, "up", result.Up, "down", result.Down, "error", result.Err)
}

// dialUpstream tries the host's own addresses first and the relay last.
func dialUpstream(ctx context.Context, target Target) (net.Conn, string, error) {
	var lastErr error

	for _, endpoint := range target.Endpoints {
		conn, err := dial(ctx, endpoint)
		if err == nil {
			return conn, "direct " + endpoint, nil
		}
		lastErr = err
	}

	if target.RelayAddress == "" {
		if lastErr == nil {
			lastErr = errors.New("the room has no reachable address")
		}
		return nil, "", lastErr
	}

	conn, err := dial(ctx, target.RelayAddress)
	if err != nil {
		if lastErr != nil {
			return nil, "", fmt.Errorf("direct attempts failed (%v) and the relay failed (%w)", lastErr, err)
		}
		return nil, "", err
	}
	return conn, "relay " + target.RelayAddress, nil
}

func dial(ctx context.Context, endpoint string) (net.Conn, error) {
	dialer := net.Dialer{Timeout: dialTimeout}
	return dialer.DialContext(ctx, "tcp", endpoint)
}
