package client

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Sunse666/NoneFrp/internal/channel"
	"github.com/Sunse666/NoneFrp/internal/config"
	"github.com/Sunse666/NoneFrp/internal/mcproto"
	"github.com/Sunse666/NoneFrp/internal/protocol"
	"github.com/Sunse666/NoneFrp/internal/proxy"
)

const (
	// localDialTimeout bounds how long the agent waits for the local server.
	localDialTimeout = 5 * time.Second
)

// runDataSession keeps a data session open for as long as the room is
// registered, reconnecting with backoff.
func (c *Client) runDataSession(ctx context.Context, relay config.Relay, registered protocol.Registered) {
	backoff := minBackoff
	for {
		if ctx.Err() != nil {
			return
		}
		err := c.serveData(ctx, relay, registered)
		if ctx.Err() != nil {
			return
		}
		c.log.Warn("data session ended", "relay", relay.URL, "error", err)

		select {
		case <-ctx.Done():
			return
		case <-time.After(jitter(backoff)):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// serveData runs one data session until it fails.
func (c *Client) serveData(ctx context.Context, relay config.Relay, registered protocol.Registered) error {
	query := url.Values{}
	query.Set("room", registered.RoomCode)
	query.Set("device", c.deviceID)
	endpoint := strings.TrimSuffix(relay.URL, "/") + "/v1/data?" + query.Encode()

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
		return fmt.Errorf("dial %s: %w (http %d)", endpoint, err, status)
	}

	acceptor, err := channel.NewYamuxAcceptor(channel.NewWSConn(conn), channel.YamuxConfig())
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("start yamux server: %w", err)
	}
	defer func() { _ = acceptor.Close() }()

	c.log.Info("data session established", "room", registered.RoomCode)

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = acceptor.Close()
		case <-done:
		}
	}()

	for {
		stream, meta, err := acceptor.Accept()
		if err != nil {
			return fmt.Errorf("accept stream: %w", err)
		}
		go c.serveStream(stream, meta)
	}
}

// serveStream forwards one player stream to the local Minecraft server.
func (c *Client) serveStream(stream net.Conn, meta protocol.StreamMeta) {
	defer func() { _ = stream.Close() }()

	target := net.JoinHostPort("127.0.0.1", strconv.Itoa(c.cfg.Local.Port))
	local, err := net.DialTimeout("tcp", target, localDialTimeout)
	if err != nil {
		c.log.Warn("local server unreachable",
			"target", target,
			"trace", meta.TraceID,
			"error", err,
		)
		return
	}
	defer func() { _ = local.Close() }()

	if c.cfg.Local.ProxyProtocol && meta.ClientIP != "" {
		header, err := mcproto.ProxyHeader(meta.ClientIP, meta.ClientPort, "127.0.0.1", c.cfg.Local.Port)
		if err != nil {
			c.log.Warn("skipping the proxy protocol header", "trace", meta.TraceID, "error", err)
		} else if _, err := local.Write(header); err != nil {
			c.log.Warn("writing the proxy protocol header failed", "trace", meta.TraceID, "error", err)
			return
		}
	}

	// Only real logins count as players; a server list ping is a stream too.
	isPlayer := meta.NextState == mcproto.NextStateLogin
	if isPlayer {
		c.status.streamStarted()
	}

	result := proxy.SpliceWith(stream, local, proxy.Options{
		Idle:               proxy.DefaultIdleTimeout,
		CloseOnUpstreamEOF: true,
	})
	c.status.streamFinished(isPlayer, result.Up, result.Down)
	c.log.Debug("stream finished",
		"trace", meta.TraceID,
		"up", result.Up,
		"down", result.Down,
		"error", result.Err,
	)
}
