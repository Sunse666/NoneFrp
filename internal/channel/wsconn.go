package channel

import (
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// wsReadLimit is the largest single websocket message accepted from a peer.
const wsReadLimit = 4 << 20

// WSConn adapts a websocket connection to net.Conn so that the multiplexer can
// run over it. Each net.Conn Read/Write maps onto one websocket message.
type WSConn struct {
	conn *websocket.Conn

	readMu sync.Mutex
	reader io.Reader

	writeMu sync.Mutex
	closed  atomic.Bool
	once    sync.Once
}

// NewWSConn wraps an upgraded websocket connection.
func NewWSConn(conn *websocket.Conn) *WSConn {
	conn.SetReadLimit(wsReadLimit)
	return &WSConn{conn: conn}
}

// Read implements net.Conn.
func (c *WSConn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	for {
		if c.reader == nil {
			typ, next, err := c.conn.NextReader()
			if err != nil {
				return 0, err
			}
			if typ != websocket.BinaryMessage && typ != websocket.TextMessage {
				continue
			}
			c.reader = next
		}
		n, err := c.reader.Read(p)
		if errors.Is(err, io.EOF) {
			c.reader = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

// Write implements net.Conn.
func (c *WSConn) Write(p []byte) (int, error) {
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close implements net.Conn.
func (c *WSConn) Close() error {
	c.once.Do(func() {
		c.closed.Store(true)
		_ = c.conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second),
		)
		_ = c.conn.Close()
	})
	return nil
}

// LocalAddr implements net.Conn.
func (c *WSConn) LocalAddr() net.Addr { return c.conn.LocalAddr() }

// RemoteAddr implements net.Conn.
func (c *WSConn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }

// SetDeadline implements net.Conn.
func (c *WSConn) SetDeadline(t time.Time) error {
	if err := c.conn.SetReadDeadline(t); err != nil {
		return err
	}
	return c.conn.SetWriteDeadline(t)
}

// SetReadDeadline implements net.Conn.
func (c *WSConn) SetReadDeadline(t time.Time) error { return c.conn.SetReadDeadline(t) }

// SetWriteDeadline implements net.Conn.
func (c *WSConn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }
