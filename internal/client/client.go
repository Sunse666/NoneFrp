// Package client implements the host agent: it keeps a control connection to
// the relay, registers a room and reconnects with backoff when the link drops.
package client

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	mrand "math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Sunse666/NoneFrp/internal/config"
	"github.com/Sunse666/NoneFrp/internal/protocol"
)

const (
	writeWait       = 10 * time.Second
	pongWait        = 45 * time.Second
	pingPeriod      = 20 * time.Second
	registerTimeout = 15 * time.Second
	maxBackoff      = 30 * time.Second
	minBackoff      = time.Second
)

// Client is the host-side agent.
type Client struct {
	cfg      config.Client
	log      *slog.Logger
	deviceID string
	status   *statusStore

	// OnRegistered is called for every successful registration.
	OnRegistered func(protocol.Registered)
	// OnRelayGone is called when an established control connection drops.
	OnRelayGone func(relay string, err error)
}

// New returns an agent for cfg. dataDir stores the stable device identifier.
func New(cfg config.Client, dataDir string, logger *slog.Logger) (*Client, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if dataDir == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("resolve config directory: %w", err)
		}
		dataDir = filepath.Join(dir, "nonefrp")
	}
	deviceID, err := loadOrCreateDeviceID(dataDir)
	if err != nil {
		return nil, err
	}
	status := newStatusStore()
	status.with(func(st *Status) {
		st.DeviceID = deviceID
		st.LocalPort = cfg.Local.Port
		st.ProxyProtocol = cfg.Local.ProxyProtocol
	})

	return &Client{cfg: cfg, log: logger, deviceID: deviceID, status: status}, nil
}

// Status returns a snapshot of the agent state.
func (c *Client) Status() Status { return c.status.snapshot() }

// DeviceID loads or creates the stable identifier kept in dir. Probes use it
// without starting an agent.
func DeviceID(dir string) (string, error) {
	return loadOrCreateDeviceID(dir)
}

// DeviceID returns the stable identifier used to reclaim rooms after a restart.
func (c *Client) DeviceID() string { return c.deviceID }

// Run keeps the agent registered until ctx is cancelled.
func (c *Client) Run(ctx context.Context) error {
	go c.probeLocal(ctx)

	backoff := minBackoff
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		registered := false
		for _, relay := range c.cfg.Relay {
			ok, err := c.connect(ctx, relay)
			registered = registered || ok
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				c.log.Warn("relay attempt failed", "relay", relay.URL, "error", err)
			}
		}
		if registered {
			backoff = minBackoff
		}

		delay := jitter(backoff)
		c.log.Info("retrying relays", "delay", delay.String())
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// connect runs one control connection. It reports whether registration
// succeeded before the connection ended.
func (c *Client) connect(ctx context.Context, relay config.Relay) (bool, error) {
	endpoint := strings.TrimSuffix(relay.URL, "/") + "/v1/agent"
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.cfg.Token)
	header.Set("X-Device-ID", c.deviceID)
	header.Set("X-Protocol-Version", strconv.Itoa(protocol.Version))

	dialer := &websocket.Dialer{HandshakeTimeout: registerTimeout}
	conn, resp, err := dialer.DialContext(ctx, endpoint, header)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		if status == http.StatusUnauthorized {
			return false, fmt.Errorf("the relay does not accept this credential (401): it may have been revoked, or the relay's tokens changed — ask for a new invite: %w", err)
		}
		return false, fmt.Errorf("dial %s: %w (http %d)", endpoint, err, status)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetReadDeadline(time.Now().Add(registerTimeout)); err != nil {
		return false, err
	}
	conn.SetReadLimit(1 << 20)

	raw, err := protocol.Encode(protocol.TypeRegister, protocol.Register{
		DeviceID:  c.deviceID,
		RoomCode:  c.cfg.Room.Code,
		LocalPort: c.cfg.Local.Port,
		Endpoints: directEndpoints(c.cfg.Local.Port),
	})
	if err != nil {
		return false, err
	}
	if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
		return false, fmt.Errorf("send register: %w", err)
	}

	registered, err := c.awaitRegistered(conn)
	if err != nil {
		return false, err
	}
	c.log.Info("room registered",
		"relay", relay.URL,
		"room", registered.RoomCode,
		"public", registered.PublicHost,
		"port", registered.PublicPort,
	)
	c.status.markConnected(relay.URL, registered.RoomCode, registered.PublicHost, registered.PublicPort)

	if c.OnRegistered != nil {
		c.OnRegistered(registered)
	}

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stop:
		}
	}()
	go pingLoop(conn, stop, c.log)

	dataCtx, stopData := context.WithCancel(ctx)
	defer stopData()
	go c.runDataSession(dataCtx, relay, registered)

	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))

	err = c.readLoop(conn)
	c.status.markDisconnected(err)

	if c.OnRelayGone != nil {
		c.OnRelayGone(relay.URL, err)
	}
	return true, err
}

func (c *Client) awaitRegistered(conn *websocket.Conn) (protocol.Registered, error) {
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return protocol.Registered{}, fmt.Errorf("await register reply: %w", err)
		}
		env, err := protocol.Decode(raw)
		if err != nil {
			return protocol.Registered{}, err
		}
		switch env.Type {
		case protocol.TypeRegistered:
			return protocol.As[protocol.Registered](env)
		case protocol.TypeError:
			body, _ := protocol.As[protocol.ErrorBody](env)
			return protocol.Registered{}, fmt.Errorf("relay rejected registration: %s: %s", body.Code, body.Message)
		}
	}
}

func (c *Client) readLoop(conn *websocket.Conn) error {
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return err
			}
			return nil
		}
		if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
			return err
		}
		env, err := protocol.Decode(raw)
		if err != nil {
			c.log.Warn("dropping malformed control message", "error", err)
			continue
		}
		switch env.Type {
		case protocol.TypePeerJoined:
			joined, _ := protocol.As[protocol.PeerJoined](env)
			c.log.Info("player joined", "player", joined.PlayerIP)
		default:
			c.log.Debug("ignoring control message", "type", env.Type)
		}
	}
}

func pingLoop(conn *websocket.Conn, stop <-chan struct{}, log *slog.Logger) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
				log.Debug("ping failed", "error", err)
				return
			}
		}
	}
}

func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	delta := float64(d) * 0.2
	return time.Duration(float64(d) + (mrand.Float64()*2-1)*delta)
}

func loadOrCreateDeviceID(dir string) (string, error) {
	path := filepath.Join(dir, "device_id")
	raw, err := os.ReadFile(path)
	if err == nil {
		if id := strings.TrimSpace(string(raw)); id != "" {
			return id, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read device id: %w", err)
	}

	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate device id: %w", err)
	}
	id := hex.EncodeToString(buf)

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create data directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write device id: %w", err)
	}
	return id, nil
}
