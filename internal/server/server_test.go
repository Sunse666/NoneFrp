package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Sunse666/NoneFrp/internal/config"
	"github.com/Sunse666/NoneFrp/internal/protocol"
	"github.com/Sunse666/NoneFrp/internal/room"
)

const testToken = "NFRP-TESTTEST"

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	cfg := config.DefaultServer()
	cfg.DomainSuffix = "mc.example.com"
	cfg.GracePeriod = config.Duration(30 * time.Second)
	cfg.Tokens = map[string]config.Token{testToken: {Note: "test host"}}
	srv := New(cfg, slog.New(slog.DiscardHandler))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

func dial(t *testing.T, ts *httptest.Server, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/agent"
	header := http.Header{}
	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}
	return websocket.DefaultDialer.Dial(url, header)
}

func readMessage(t *testing.T, conn *websocket.Conn) protocol.Envelope {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	env, err := protocol.Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return env
}

func writeMessage(t *testing.T, conn *websocket.Conn, t2 protocol.Type, payload any) {
	t.Helper()
	raw, err := protocol.Encode(t2, payload)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestRegisterAssignsRoom(t *testing.T) {
	_, ts := newTestServer(t)

	conn, _, err := dial(t, ts, testToken)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	writeMessage(t, conn, protocol.TypeRegister, protocol.Register{DeviceID: "dev-1", LocalPort: 25565})

	env := readMessage(t, conn)
	if env.Type != protocol.TypeRegistered {
		t.Fatalf("type = %q, want registered", env.Type)
	}
	got, err := protocol.As[protocol.Registered](env)
	if err != nil {
		t.Fatalf("as: %v", err)
	}
	if got.RoomCode == "" {
		t.Fatal("expected a room code")
	}
	if want := got.RoomCode + ".mc.example.com"; got.PublicHost != want {
		t.Fatalf("public host = %q, want %q", got.PublicHost, want)
	}
	if got.PublicPort != 25565 {
		t.Fatalf("public port = %d", got.PublicPort)
	}
}

func TestRejectsMissingToken(t *testing.T) {
	_, ts := newTestServer(t)

	conn, resp, err := dial(t, ts, "")
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected the dial to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v, want 401", resp)
	}
}

func TestRejectsUnknownToken(t *testing.T) {
	_, ts := newTestServer(t)

	conn, resp, err := dial(t, ts, "NFRP-WRONGWRO")
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected the dial to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v, want 401", resp)
	}
}

func TestUnregisterReleasesRoom(t *testing.T) {
	srv, ts := newTestServer(t)

	conn, _, err := dial(t, ts, testToken)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	writeMessage(t, conn, protocol.TypeRegister, protocol.Register{DeviceID: "dev-1", LocalPort: 25565})
	readMessage(t, conn)

	writeMessage(t, conn, protocol.TypeUnregister, protocol.Unregister{Reason: "test done"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.rooms.Len() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("room was not released, rooms = %d", srv.rooms.Len())
}

func TestDisconnectStartsGracePeriod(t *testing.T) {
	srv, ts := newTestServer(t)

	conn, _, err := dial(t, ts, testToken)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	writeMessage(t, conn, protocol.TypeRegister, protocol.Register{DeviceID: "dev-1", LocalPort: 25565})
	env := readMessage(t, conn)
	registered, err := protocol.As[protocol.Registered](env)
	if err != nil {
		t.Fatalf("as: %v", err)
	}

	_ = conn.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if found, ok := srv.rooms.Get(registered.RoomCode); ok && found.State == room.StateGrace {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("room never entered the grace state")
}

func TestIgnoresUnknownMessageTypes(t *testing.T) {
	srv, ts := newTestServer(t)

	conn, _, err := dial(t, ts, testToken)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	writeMessage(t, conn, protocol.TypeRegister, protocol.Register{DeviceID: "dev-1", LocalPort: 25565})
	readMessage(t, conn)

	// A newer agent may send message types this relay does not know about.
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"v":1,"type":"future"}`)); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if srv.rooms.Len() == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("room disappeared, rooms = %d", srv.rooms.Len())
}

func TestRegisterRejectsConflictingRoomCode(t *testing.T) {
	_, ts := newTestServer(t)

	first, _, err := dial(t, ts, testToken)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = first.Close() }()
	writeMessage(t, first, protocol.TypeRegister, protocol.Register{DeviceID: "dev-1", RoomCode: "aa23cd", LocalPort: 25565})
	readMessage(t, first)

	second, _, err := dial(t, ts, testToken)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = second.Close() }()
	writeMessage(t, second, protocol.TypeRegister, protocol.Register{DeviceID: "dev-2", RoomCode: "aa23cd", LocalPort: 25565})

	env := readMessage(t, second)
	if env.Type != protocol.TypeError {
		t.Fatalf("type = %q, want error", env.Type)
	}
	body, err := protocol.As[protocol.ErrorBody](env)
	if err != nil {
		t.Fatalf("as: %v", err)
	}
	if body.Code != protocol.ErrRoomConflict {
		t.Fatalf("code = %q", body.Code)
	}
}
