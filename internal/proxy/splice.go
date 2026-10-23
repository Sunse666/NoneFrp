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

// Result reports how many bytes travelled in each direction.
type Result struct {
	// Up is the number of bytes copied from the first connection to the second.
	Up int64
	// Down is the number of bytes copied from the second connection to the first.
	Down int64
	// Err is the first non-EOF error seen in either direction.
	Err error
}

// Timeout reports whether the result was caused by an idle timeout.
func (r Result) Timeout() bool {
	var netErr net.Error
	return errors.As(r.Err, &netErr) && netErr.Timeout()
}

// Splice copies bytes both ways between a and b until both directions finish,
// then closes both. An idle of zero means no idle timeout.
func Splice(a, b net.Conn, idle time.Duration) Result {
	type outcome struct {
		up  bool
		n   int64
		err error
	}
	results := make(chan outcome, 2)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		n, err := copyDirection(b, a, idle)
		results <- outcome{up: true, n: n, err: err}
	}()
	go func() {
		defer wg.Done()
		n, err := copyDirection(a, b, idle)
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
		if got.err != nil && res.Err == nil {
			res.Err = got.err
		}
	}

	_ = a.Close()
	_ = b.Close()
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

// halfClose forwards a FIN where the transport supports it. Streams without
// half-close support rely on the peer being closed once both directions finish.
func halfClose(conn net.Conn) {
	if cw, ok := conn.(closeWriter); ok {
		_ = cw.CloseWrite()
	}
}
