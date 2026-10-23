package channel

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/Sunse666/NoneFrp/internal/protocol"
)

// prefaceTimeout bounds how long the agent waits for a stream preface and the
// relay waits for the agent to accept a stream.
const prefaceTimeout = 10 * time.Second

// YamuxConfig returns the multiplexer settings shared by both ends.
func YamuxConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	// Without keepalive a NAT table entry can expire and leave a session that
	// looks open but never delivers a byte.
	cfg.EnableKeepAlive = true
	cfg.KeepAliveInterval = 15 * time.Second
	cfg.ConnectionWriteTimeout = 15 * time.Second
	// The default 256 KiB window is easily saturated by chunk bursts.
	cfg.MaxStreamWindowSize = 2 << 20
	cfg.AcceptBacklog = 256
	cfg.LogOutput = io.Discard
	return cfg
}

// YamuxDialer opens streams towards the agent over a single multiplexed
// connection. It is the relay side of a data session.
type YamuxDialer struct {
	session *yamux.Session
	ready   chan struct{}
}

// NewYamuxDialer wraps conn as the initiating side of a yamux session.
func NewYamuxDialer(conn net.Conn, cfg *yamux.Config) (*YamuxDialer, error) {
	session, err := yamux.Client(conn, cfg)
	if err != nil {
		return nil, fmt.Errorf("start yamux client: %w", err)
	}
	ready := make(chan struct{})
	close(ready)
	return &YamuxDialer{session: session, ready: ready}, nil
}

// Open opens a stream and introduces it with a preface.
func (d *YamuxDialer) Open(ctx context.Context, meta protocol.StreamMeta) (net.Conn, error) {
	type opened struct {
		stream net.Conn
		err    error
	}
	result := make(chan opened, 1)

	go func() {
		stream, err := d.session.OpenStream()
		result <- opened{stream: stream, err: err}
	}()

	var stream net.Conn
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case got := <-result:
		if got.err != nil {
			return nil, fmt.Errorf("open stream: %w", got.err)
		}
		stream = got.stream
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = stream.SetWriteDeadline(deadline)
	} else {
		_ = stream.SetWriteDeadline(time.Now().Add(prefaceTimeout))
	}
	if err := protocol.WritePreface(stream, meta); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("write preface: %w", err)
	}
	_ = stream.SetWriteDeadline(time.Time{})
	return stream, nil
}

// Ready reports that the session can carry streams.
func (d *YamuxDialer) Ready() <-chan struct{} { return d.ready }

// Close shuts the session down, which also closes every open stream.
func (d *YamuxDialer) Close() error { return d.session.Close() }

// YamuxAcceptor accepts streams from the relay. It is the agent side of a data
// session.
type YamuxAcceptor struct {
	session *yamux.Session
	ready   chan struct{}
}

// NewYamuxAcceptor wraps conn as the accepting side of a yamux session.
func NewYamuxAcceptor(conn net.Conn, cfg *yamux.Config) (*YamuxAcceptor, error) {
	session, err := yamux.Server(conn, cfg)
	if err != nil {
		return nil, fmt.Errorf("start yamux server: %w", err)
	}
	ready := make(chan struct{})
	close(ready)
	return &YamuxAcceptor{session: session, ready: ready}, nil
}

// Accept returns the next stream together with its preface.
func (a *YamuxAcceptor) Accept() (net.Conn, protocol.StreamMeta, error) {
	stream, err := a.session.AcceptStream()
	if err != nil {
		return nil, protocol.StreamMeta{}, fmt.Errorf("accept stream: %w", err)
	}

	_ = stream.SetReadDeadline(time.Now().Add(prefaceTimeout))
	meta, err := protocol.ReadPreface(stream)
	if err != nil {
		_ = stream.Close()
		return nil, protocol.StreamMeta{}, fmt.Errorf("read preface: %w", err)
	}
	_ = stream.SetReadDeadline(time.Time{})
	return stream, meta, nil
}

// Ready reports that the session can carry streams.
func (a *YamuxAcceptor) Ready() <-chan struct{} { return a.ready }

// Close shuts the session down, which also closes every open stream.
func (a *YamuxAcceptor) Close() error { return a.session.Close() }
