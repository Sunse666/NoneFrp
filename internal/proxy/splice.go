// Package proxy moves bytes between two connections until both directions are
// finished, preserving half-close semantics.
//
// Half-close matters for Minecraft: a server list ping sends its request, shuts
// down its write side and then waits for the response. Losing the FIN turns the
// multiplayer screen into a spinner that never resolves.
package proxy

import (
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DefaultIdleTimeout closes a connection that carried no bytes for this long.
	DefaultIdleTimeout = 10 * time.Minute
	// bufferSize is the per direction copy buffer.
	bufferSize = 32 * 1024
)

// closeWriter is implemented by connections that support half-close.
type closeWriter interface {
	CloseWrite() error
}

// LoginIdleTimeout is the idle limit for connections that carry a real game
// session. Minecraft exchanges keep-alive packets every fifteen seconds, so a
// minute of silence means the player is gone even if the socket still looks
// healthy.
const LoginIdleTimeout = 60 * time.Second

// Options tune how Splice behaves.
type Options struct {
	// Idle closes the pair after this long without traffic. Zero disables it.
	Idle time.Duration
	// CloseOnServerEOF ends both connections as soon as the second connection
	// stops sending. A Minecraft server closes the connection when the session
	// is over, so waiting for the client to close as well would leave zombie
	// streams behind every kick.
	CloseOnServerEOF bool
	// CloseOnClientEOF ends both connections as soon as the first connection
	// stops sending. It must stay off for status pings, where the client
	// legitimately shuts down its write side and then waits for the reply.
	CloseOnClientEOF bool
}

// Result reports how many bytes travelled in each direction.
type Result struct {
	// Up is the number of bytes copied from the first connection to the second.
	Up int64
	// Down is the number of bytes copied from the second connection to the first.
	Down int64
	// Err is the first unexpected error seen in either direction.
	Err error
}

// Timeout reports whether the result was caused by an idle timeout.
func (r Result) Timeout() bool {
	var netErr net.Error
	return errors.As(r.Err, &netErr) && netErr.Timeout()
}

// Splice copies bytes both ways and keeps half-close semantics.
func Splice(a, b net.Conn, idle time.Duration) Result {
	return SpliceWith(a, b, Options{Idle: idle})
}

// SpliceWith copies bytes both ways using the given options.
func SpliceWith(a, b net.Conn, opts Options) Result {
	type outcome struct {
		up  bool
		n   int64
		err error
	}
	results := make(chan outcome, 2)

	var upstreamClosed atomic.Bool
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		// A client half-close is normal during a status ping, so it only ends the
		// pair when the caller says this is a real game session.
		n, err := copyDirection(b, a, opts.Idle)
		if err != nil || opts.CloseOnClientEOF {
			// A broken connection or a vanished player: nothing useful can follow.
			upstreamClosed.Store(true)
			hardClose(a)
			hardClose(b)
		}
		results <- outcome{up: true, n: n, err: err}
	}()

	go func() {
		defer wg.Done()
		n, err := copyDirection(a, b, opts.Idle)
		if err != nil || opts.CloseOnServerEOF {
			// The server is done sending, which for Minecraft means the session
			// is over. Stop waiting for the client and tear the pair down.
			upstreamClosed.Store(true)
			hardClose(a)
			hardClose(b)
		}
		results <- outcome{up: false, n: n, err: err}
	}()

	wg.Wait()
	close(results)

	var res Result
	for got := range results {
		if got.up {
			res.Up = got.n
		} else {
			res.Down = got.n
		}
		if got.err != nil && res.Err == nil && !isClosedErr(got.err) {
			res.Err = got.err
		}
	}

	hardClose(a)
	hardClose(b)
	return res
}

// copyDirection copies src into dst until src reaches EOF or fails.
func copyDirection(dst, src net.Conn, idle time.Duration) (int64, error) {
	buf := make([]byte, bufferSize)
	var total int64

	for {
		if idle > 0 {
			_ = src.SetReadDeadline(time.Now().Add(idle))
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			if idle > 0 {
				_ = dst.SetWriteDeadline(time.Now().Add(idle))
			}
			if _, writeErr := dst.Write(buf[:n]); writeErr != nil {
				return total, writeErr
			}
			total += int64(n)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				halfClose(dst)
				return total, nil
			}
			return total, readErr
		}
	}
}

// hardClose tears a connection down and, crucially, wakes any goroutine that is
// blocked reading from it. A plain Close is not enough for multiplexed streams:
// a concurrent Read may keep waiting for data that will never arrive, which
// leaves the whole splice, and the player slot behind it, stuck.
func hardClose(conn net.Conn) {
	_ = conn.SetReadDeadline(time.Now())
	if resetter, ok := conn.(interface{ Reset() error }); ok {
		_ = resetter.Reset()
	}
	_ = conn.Close()
}

// halfClose forwards a FIN where the transport supports it. Streams without
// half-close support rely on the peer being closed once both directions finish.
func halfClose(conn net.Conn) {
	if cw, ok := conn.(closeWriter); ok {
		_ = cw.CloseWrite()
	}
}

// isClosedErr reports whether err is the noise produced by us closing a
// connection that the other direction is still using.
func isClosedErr(err error) bool {
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return false
	}
	// Windows reports "use of closed network connection" as a plain error.
	message := err.Error()
	return message == "use of closed network connection" ||
		message == "wsarecv: An existing connection was forcibly closed by the remote host." ||
		message == "wsasend: An existing connection was forcibly closed by the remote host."
}
