package server

import (
	"bytes"
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

	"github.com/gorilla/websocket"

	"github.com/Sunse666/NoneFrp/internal/channel"
	"github.com/Sunse666/NoneFrp/internal/config"
	"github.com/Sunse666/NoneFrp/internal/mcproto"
	"github.com/Sunse666/NoneFrp/internal/protocol"
)

func newDataTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	cfg := config.DefaultServer()
	cfg.DomainSuffix = "mc.example.com"
	cfg.PublicPort = 0
	cfg.GracePeriod = config.Duration(30 * time.Second)
	cfg.Tokens = map[string]string{testToken: "test host"}

	srv := New(cfg, slog.New(slog.DiscardHandler))
	if err := srv.listenPublic(); err != nil {
		t.Fatalf("listen public: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go srv.servePublic(ctx)
	t.Cleanup(func() {
		cancel()
		srv.plane.closeAll()
		_ = srv.publicListener.Close()
	})

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

func encodeVarInt(v int) []byte {
	var out []byte
	u := uint32(v)
	for {
		b := byte(u & 0x7f)
		u >>= 7
		if u != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if u == 0 {
			return out
		}
	}
}

func encodeString(s string) []byte {
	return append(encodeVarInt(len(s)), []byte(s)...)
}

func buildHandshake(version int, address string, port uint16, nextState int) []byte {
	var body []byte
	body = append(body, encodeVarInt(0x00)...)
	body = append(body, encodeVarInt(version)...)
	body = append(body, encodeString(address)...)
	body = append(body, byte(port>>8), byte(port))
	body = append(body, encodeVarInt(nextState)...)
	return append(encodeVarInt(len(body)), body...)
}

// registerRoom opens a control connection and returns the assigned room code.
func registerRoom(t *testing.T, ts *httptest.Server, deviceID string) string {
	t.Helper()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/agent"
	header := http.Header{}
	header.Set("Authorization", "Bearer "+testToken)
	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		t.Fatalf("dial control: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	raw, err := protocol.Encode(protocol.TypeRegister, protocol.Register{DeviceID: deviceID, LocalPort: 25565})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
		t.Fatalf("write register: %v", err)
	}
	_, reply, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read registered: %v", err)
	}
	env, err := protocol.Decode(reply)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	registered, err := protocol.As[protocol.Registered](env)
	if err != nil {
		t.Fatalf("as: %v", err)
	}
	return registered.RoomCode
}

func openDataSession(t *testing.T, ts *httptest.Server, roomCode, deviceID string) *channel.YamuxAcceptor {
	t.Helper()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/data?room=" + roomCode + "&device=" + deviceID
	header := http.Header{}
	header.Set("Authorization", "Bearer "+testToken)
	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		t.Fatalf("dial data: %v", err)
	}
	acceptor, err := channel.NewYamuxAcceptor(channel.NewWSConn(conn), channel.YamuxConfig())
	if err != nil {
		t.Fatalf("yamux acceptor: %v", err)
	}
	t.Cleanup(func() { _ = acceptor.Close() })
	return acceptor
}

func waitForDialer(t *testing.T, srv *Server, roomCode string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := srv.plane.dialer(roomCode); ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the relay never registered a data session")
}

func TestRoomCodeFromHost(t *testing.T) {
	cases := []struct {
		host string
		want string
		ok   bool
	}{
		{"a2b3c4.mc.example.com", "a2b3c4", true},
		{"A2B3C4.MC.EXAMPLE.COM.", "a2b3c4", true},
		{"a2b3c4.mc.example.com:25565", "a2b3c4", true},
		{"a2b3c4-map.mc.example.com", "a2b3c4", true},
		{"a2b3c4.other.com", "", false},
		{"mc.example.com", "", false},
		{"", "", false},
		{"a2b3c4.mc.example.com.evil.test", "", false},
	}
	for _, tc := range cases {
		got, ok := roomCodeFromHost(tc.host, "mc.example.com")
		if ok != tc.ok || got != tc.want {
			t.Fatalf("roomCodeFromHost(%q) = (%q, %v), want (%q, %v)", tc.host, got, ok, tc.want, tc.ok)
		}
	}
}

func TestPlayerIsRoutedToTheAgent(t *testing.T) {
	srv, ts := newDataTestServer(t)

	roomCode := registerRoom(t, ts, "dev-1")
	acceptor := openDataSession(t, ts, roomCode, "dev-1")
	waitForDialer(t, srv, roomCode)

	player, err := net.Dial("tcp", srv.PlayerAddr().String())
	if err != nil {
		t.Fatalf("dial player port: %v", err)
	}
	defer func() { _ = player.Close() }()

	host := roomCode + ".mc.example.com"
	handshake := buildHandshake(767, host, 25565, mcproto.NextStateStatus)
	if _, err := player.Write(handshake); err != nil {
		t.Fatalf("write handshake: %v", err)
	}

	stream, meta, err := acceptor.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	defer func() { _ = stream.Close() }()

	if meta.RoomCode != roomCode {
		t.Fatalf("room = %q, want %q", meta.RoomCode, roomCode)
	}
	if meta.Host != host {
		t.Fatalf("host = %q, want %q", meta.Host, host)
	}
	if meta.NextState != mcproto.NextStateStatus {
		t.Fatalf("next state = %d", meta.NextState)
	}
	if meta.ClientIP == "" || meta.ClientPort == 0 {
		t.Fatalf("client address missing: %+v", meta)
	}
	if meta.TraceID == "" {
		t.Fatal("trace id missing")
	}

	// The agent must receive the exact handshake bytes the relay consumed.
	_ = stream.SetReadDeadline(time.Now().Add(3 * time.Second))
	replayed := make([]byte, len(handshake))
	if _, err := io.ReadFull(stream, replayed); err != nil {
		t.Fatalf("read replayed handshake: %v", err)
	}
	if !bytes.Equal(replayed, handshake) {
		t.Fatalf("replayed = %x, want %x", replayed, handshake)
	}

	// And the return path must work.
	reply := []byte("status response")
	if _, err := stream.Write(reply); err != nil {
		t.Fatalf("write reply: %v", err)
	}

	_ = player.SetReadDeadline(time.Now().Add(3 * time.Second))
	got := make([]byte, len(reply))
	if _, err := io.ReadFull(player, got); err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if !bytes.Equal(got, reply) {
		t.Fatalf("player got %q", got)
	}
}

func TestUnknownRoomGetsStatusReply(t *testing.T) {
	srv, _ := newDataTestServer(t)

	player, err := net.Dial("tcp", srv.PlayerAddr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = player.Close() }()
	_ = player.SetDeadline(time.Now().Add(5 * time.Second))

	handshake := buildHandshake(767, "zzzzzz.mc.example.com", 25565, mcproto.NextStateStatus)
	if _, err := player.Write(handshake); err != nil {
		t.Fatalf("write handshake: %v", err)
	}
	if err := mcproto.WritePacket(player, 0x00, nil); err != nil {
		t.Fatalf("write status request: %v", err)
	}

	packetID, payload, err := mcproto.ReadPacket(player, 32767)
	if err != nil {
		t.Fatalf("read status response: %v", err)
	}
	if packetID != 0x00 {
		t.Fatalf("packet id = %d", packetID)
	}

	var doc struct {
		Version struct {
			Protocol int `json:"protocol"`
		} `json:"version"`
		Description struct {
			Text string `json:"text"`
		} `json:"description"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil {
		t.Fatalf("status json: %v", err)
	}
	if doc.Version.Protocol != 767 {
		t.Fatalf("protocol = %d, want the client version echoed back", doc.Version.Protocol)
	}
	if doc.Description.Text != messageRoomNotFound {
		t.Fatalf("description = %q", doc.Description.Text)
	}

	// The ping must still be answered so the entry renders instead of spinning.
	ping := []byte{0, 0, 0, 0, 0, 0, 0, 42}
	if err := mcproto.WritePacket(player, 0x01, ping); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	packetID, pong, err := mcproto.ReadPacket(player, 32767)
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if packetID != 0x01 || !bytes.Equal(pong, ping) {
		t.Fatalf("pong = id %d payload %x", packetID, pong)
	}
}

func TestUnknownRoomGetsLoginDisconnect(t *testing.T) {
	srv, _ := newDataTestServer(t)

	player, err := net.Dial("tcp", srv.PlayerAddr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = player.Close() }()
	_ = player.SetDeadline(time.Now().Add(5 * time.Second))

	handshake := buildHandshake(767, "zzzzzz.mc.example.com", 25565, mcproto.NextStateLogin)
	if _, err := player.Write(handshake); err != nil {
		t.Fatalf("write handshake: %v", err)
	}

	packetID, payload, err := mcproto.ReadPacket(player, 32767)
	if err != nil {
		t.Fatalf("read disconnect: %v", err)
	}
	if packetID != 0x00 {
		t.Fatalf("packet id = %d", packetID)
	}

	var doc struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil {
		t.Fatalf("disconnect json: %v", err)
	}
	if doc.Text != messageRoomNotFound {
		t.Fatalf("text = %q", doc.Text)
	}
}

func TestDataEndpointRejectsForeignRoom(t *testing.T) {
	_, ts := newDataTestServer(t)
	roomCode := registerRoom(t, ts, "dev-1")

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/data?room=" + roomCode + "&device=dev-2"
	header := http.Header{}
	header.Set("Authorization", "Bearer "+testToken)
	conn, resp, err := websocket.DefaultDialer.Dial(url, header)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected the dial to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %v, want 403", resp)
	}
}
