package channel

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// echoServer upgrades and echoes every message back until the peer closes.
func echoServer() *httptest.Server {
	upgrader := websocket.Upgrader{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		for {
			typ, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(typ, msg); err != nil {
				return
			}
		}
	}))
}

func dialWS(t *testing.T, ts *httptest.Server) *WSConn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(ts.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return NewWSConn(conn)
}

func TestWSConnRoundTrip(t *testing.T) {
	ts := echoServer()
	defer ts.Close()
	conn := dialWS(t, ts)

	payload := []byte("handshake bytes from a minecraft client")
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("got %q, want %q", got, payload)
	}
}

func TestWSConnReadReturnsWholeMessage(t *testing.T) {
	ts := echoServer()
	defer ts.Close()
	conn := dialWS(t, ts)

	first := []byte("first")
	second := []byte("second")
	if _, err := conn.Write(first); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if _, err := conn.Write(second); err != nil {
		t.Fatalf("write second: %v", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}

	// Reads must not merge two websocket messages into one call sequence that
	// loses boundaries beyond what net.Conn allows, but they must keep order.
	buf := make([]byte, 64)
	total := 0
	for total < len(first)+len(second) {
		n, err := conn.Read(buf[total:])
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		total += n
	}
	if got := string(buf[:total]); got != "firstsecond" {
		t.Fatalf("got %q", got)
	}
}

func TestWSConnCloseIsIdempotent(t *testing.T) {
	ts := echoServer()
	defer ts.Close()
	conn := dialWS(t, ts)

	if err := conn.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := conn.Write([]byte("x")); err == nil {
		t.Fatal("expected a write error after close")
	}
}

var _ net.Conn = (*WSConn)(nil)
