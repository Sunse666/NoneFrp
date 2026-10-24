package server

import (
	"encoding/json"
	"io"
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

func newAuthTestServer(t *testing.T, tokens map[string]config.Token) (*Server, *httptest.Server) {
	t.Helper()
	cfg := config.DefaultServer()
	cfg.DomainSuffix = "mc.example.com"
	cfg.PublicPort = 0
	cfg.Tokens = tokens

	srv := New(cfg, slog.New(slog.DiscardHandler))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

func register(t *testing.T, conn *websocket.Conn, deviceID string) protocol.Envelope {
	t.Helper()
	writeMessage(t, conn, protocol.TypeRegister, protocol.Register{DeviceID: deviceID, LocalPort: 25565})
	return readMessage(t, conn)
}

// One invite, one device. The same device may reconnect as often as it likes,
// which is what "single use" has to mean for a host that drops offline.
func TestInviteIsBoundToOneDevice(t *testing.T) {
	_, ts := newAuthTestServer(t, map[string]config.Token{
		"JOIN-AAAA2222": {Note: "one device", MaxDevices: 1},
	})

	first, _, err := dial(t, ts, "JOIN-AAAA2222")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = first.Close() }()
	if env := register(t, first, "device-a"); env.Type != protocol.TypeRegistered {
		t.Fatalf("first registration: %s", env.Type)
	}

	again, _, err := dial(t, ts, "JOIN-AAAA2222")
	if err != nil {
		t.Fatalf("dial again: %v", err)
	}
	defer func() { _ = again.Close() }()
	if env := register(t, again, "device-a"); env.Type != protocol.TypeRegistered {
		t.Fatalf("the same device must be allowed back: %s", env.Type)
	}

	other, _, err := dial(t, ts, "JOIN-AAAA2222")
	if err != nil {
		t.Fatalf("dial other: %v", err)
	}
	defer func() { _ = other.Close() }()
	env := register(t, other, "device-b")
	if env.Type != protocol.TypeError {
		t.Fatalf("a second device must be rejected, got %s", env.Type)
	}
	body, err := protocol.As[protocol.ErrorBody](env)
	if err != nil {
		t.Fatalf("as: %v", err)
	}
	if body.Code != protocol.ErrInviteExhausted {
		t.Fatalf("code = %q", body.Code)
	}
}

// SIGHUP reloads the credential table: a newly issued code works and a revoked
// one stops working, without restarting anything.
func TestReloadTokens(t *testing.T) {
	srv, ts := newAuthTestServer(t, map[string]config.Token{
		"JOIN-OLD11111": {Note: "the old one"},
	})

	conn, _, err := dial(t, ts, "JOIN-OLD11111")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()

	srv.ReloadTokens(map[string]config.Token{
		"JOIN-NEW22222": {Note: "the new one"},
	})

	revoked, resp, err := dial(t, ts, "JOIN-OLD11111")
	if err == nil {
		_ = revoked.Close()
		t.Fatal("a revoked credential still connected")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v, want 401", resp)
	}

	fresh, _, err := dial(t, ts, "JOIN-NEW22222")
	if err != nil {
		t.Fatalf("dial with the new credential: %v", err)
	}
	defer func() { _ = fresh.Close() }()
	if env := register(t, fresh, "device-a"); env.Type != protocol.TypeRegistered {
		t.Fatalf("the new credential was rejected: %s", env.Type)
	}
}

func TestExpiredInviteIsRejected(t *testing.T) {
	_, ts := newAuthTestServer(t, map[string]config.Token{
		"JOIN-EEEE3333": {Expires: time.Now().Add(-time.Minute)},
	})

	conn, _, err := dial(t, ts, "JOIN-EEEE3333")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	env := register(t, conn, "device-a")
	if env.Type != protocol.TypeError {
		t.Fatalf("an expired invite must be rejected, got %s", env.Type)
	}
	body, err := protocol.As[protocol.ErrorBody](env)
	if err != nil {
		t.Fatalf("as: %v", err)
	}
	if body.Code != protocol.ErrInviteExpired {
		t.Fatalf("code = %q", body.Code)
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

func TestRoomLookupPublishesEndpoints(t *testing.T) {
	_, ts := newAuthTestServer(t, map[string]config.Token{testToken: {Note: "test host"}})

	conn, _, err := dial(t, ts, testToken)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	writeMessage(t, conn, protocol.TypeRegister, protocol.Register{
		DeviceID:  "dev-1",
		LocalPort: 25565,
		Endpoints: []string{"[2409::1]:25565", "192.168.1.9:25565"},
	})
	env := readMessage(t, conn)
	if env.Type != protocol.TypeRegistered {
		t.Fatalf("register: %s", env.Type)
	}
	registered, err := protocol.As[protocol.Registered](env)
	if err != nil {
		t.Fatalf("as: %v", err)
	}

	response, err := http.Get(ts.URL + "/v1/room/" + registered.RoomCode)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	var lookup RoomLookup
	if err := json.NewDecoder(response.Body).Decode(&lookup); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !lookup.Online {
		t.Fatal("the room should be reported online")
	}
	if len(lookup.Endpoints) != 2 || lookup.Endpoints[0] != "[2409::1]:25565" {
		t.Fatalf("endpoints = %v", lookup.Endpoints)
	}
	if !strings.Contains(lookup.RelayAddress, ":") {
		t.Fatalf("relay address = %q", lookup.RelayAddress)
	}

	missing, err := http.Get(ts.URL + "/v1/room/nosuch")
	if err != nil {
		t.Fatalf("get missing: %v", err)
	}
	defer func() { _ = missing.Body.Close() }()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", missing.StatusCode)
	}
}

func TestMetricsNeedACredential(t *testing.T) {
	_, ts := newAuthTestServer(t, map[string]config.Token{testToken: {Note: "test host"}})

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestMetricsReportState(t *testing.T) {
	srv, ts := newAuthTestServer(t, map[string]config.Token{testToken: {Note: "test host"}})

	conn, _, err := dial(t, ts, testToken)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if env := register(t, conn, "dev-1"); env.Type != protocol.TypeRegistered {
		t.Fatalf("register: %s", env.Type)
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/metrics", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(body)
	for _, want := range []string{
		"nonefrp_rooms{state=\"active\"} 1",
		"nonefrp_registrations_total 1",
		"nonefrp_players 0",
		"nonefrp_bytes_total{direction=\"up\"}",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("metrics missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("content type = %q", resp.Header.Get("Content-Type"))
	}
	_ = srv
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
