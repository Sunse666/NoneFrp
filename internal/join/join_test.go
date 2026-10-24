package join

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// echoServer answers every connection by echoing what it receives.
func echoServer(t *testing.T) (string, *int) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	accepted := 0
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			accepted++
			go func() {
				defer func() { _ = conn.Close() }()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()

	return listener.Addr().String(), &accepted
}

// lookupServer stands in for the relay's /v1/room/{code} endpoint.
func lookupServer(t *testing.T, target Target) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/room/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(target)
	}))
	t.Cleanup(server.Close)
	return server
}

func startJoin(t *testing.T, relay string, room string) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() {
		_ = ServeListener(ctx, listener, Options{Relay: relay, Room: room}, slog.New(slog.DiscardHandler))
	}()

	return listener.Addr().String()
}

func speak(t *testing.T, address, message string) string {
	t.Helper()

	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.Write([]byte(message)); err != nil {
		t.Fatalf("write: %v", err)
	}

	buffer := make([]byte, len(message))
	if _, err := io.ReadFull(conn, buffer); err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(buffer)
}

// The whole point: a direct address that works is used before the relay.
func TestJoinPrefersTheDirectAddress(t *testing.T) {
	upstream, accepted := echoServer(t)
	relay := lookupServer(t, Target{
		Code:         "a2b3c4",
		Online:       true,
		Endpoints:    []string{upstream},
		RelayAddress: "127.0.0.1:1", // deliberately unreachable
	})

	local := startJoin(t, relay.URL, "a2b3c4")

	if got := speak(t, local, "hello-direct"); got != "hello-direct" {
		t.Fatalf("echo = %q", got)
	}
	if *accepted != 1 {
		t.Fatalf("upstream saw %d connections", *accepted)
	}
}

// A stale endpoint must not stop the player: the relay is still there.
func TestJoinFallsBackToTheRelay(t *testing.T) {
	upstream, accepted := echoServer(t)
	relay := lookupServer(t, Target{
		Code:         "a2b3c4",
		Online:       true,
		Endpoints:    []string{"127.0.0.1:1"}, // nothing listens there
		RelayAddress: upstream,
	})

	local := startJoin(t, relay.URL, "a2b3c4")

	if got := speak(t, local, "hello-relay"); got != "hello-relay" {
		t.Fatalf("echo = %q", got)
	}
	if *accepted != 1 {
		t.Fatalf("upstream saw %d connections", *accepted)
	}
}

func TestLookupReportsAMissingRoom(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer server.Close()

	if _, err := Lookup(context.Background(), server.URL, "nope"); err != ErrRoomNotFound {
		t.Fatalf("err = %v", err)
	}
}
