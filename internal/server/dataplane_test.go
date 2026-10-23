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
	return newDataTestServerWith(t, nil)
}

func newDataTestServerWith(t *testing.T, mutate func(*config.Server)) (*Server, *httptest.Server) {
	t.Helper()
	cfg := config.DefaultServer()
	cfg.DomainSuffix = "mc.example.com"
	cfg.PublicPort = 0
	cfg.GracePeriod = config.Duration(30 * time.Second)
	cfg.Tokens = map[string]string{testToken: "test host"}
	if mutate != nil {
		mutate(&cfg)
	}

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
	_, code := registerRoomConn(t, ts, deviceID)
	return code
}

// registerRoomConn keeps the control connection open so tests can read the
// advisory messages pushed to the agent.
func registerRoomConn(t *testing.T, ts *httptest.Server, deviceID string) (*websocket.Conn, string) {
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
	return conn, registered.RoomCode
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

func TestDataplanePlayerLimit(t *testing.T) {
	plane := newDataplane()
	limits := config.Limits{MaxPlayersPerRoom: 2, MaxStreamsPerRoom: 10}

	if !plane.acquire("a2b3c4", limits, true) {
		t.Fatal("first player rejected")
	}
	if !plane.acquire("a2b3c4", limits, true) {
		t.Fatal("second player rejected")
	}
	if plane.acquire("a2b3c4", limits, true) {
		t.Fatal("third player should have been rejected")
	}

	// Server list pings do not take a player slot.
	if !plane.acquire("a2b3c4", limits, false) {
		t.Fatal("status ping rejected while the room was full")
	}

	plane.release("a2b3c4", true)
	if !plane.acquire("a2b3c4", limits, true) {
		t.Fatal("a freed player slot was not reusable")
	}

	// Other rooms are unaffected.
	if !plane.acquire("b7k2m9", limits, true) {
		t.Fatal("a different room was rejected")
	}
}

func TestDataplaneStreamLimit(t *testing.T) {
	plane := newDataplane()
	limits := config.Limits{MaxPlayersPerRoom: 100, MaxStreamsPerRoom: 2}

	if !plane.acquire("a2b3c4", limits, false) {
		t.Fatal("first stream rejected")
	}
	if !plane.acquire("a2b3c4", limits, true) {
		t.Fatal("second stream rejected")
	}
	if plane.acquire("a2b3c4", limits, false) {
		t.Fatal("third stream should have been rejected")
	}

	plane.release("a2b3c4", true)
	if !plane.acquire("a2b3c4", limits, false) {
		t.Fatal("a freed stream slot was not reusable")
	}
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

func TestAgentIsNotifiedAboutPlayers(t *testing.T) {
	srv, ts := newDataTestServer(t)

	control, roomCode := registerRoomConn(t, ts, "dev-1")
	acceptor := openDataSession(t, ts, roomCode, "dev-1")
	waitForDialer(t, srv, roomCode)

	// Drain the yamux stream so the player connection stays open.
	go func() {
		stream, _, err := acceptor.Accept()
		if err != nil {
			return
		}
		buf := make([]byte, 256)
		_, _ = stream.Read(buf)
		time.Sleep(200 * time.Millisecond)
		_ = stream.Close()
	}()

	player, err := net.Dial("tcp", srv.PlayerAddr().String())
	if err != nil {
		t.Fatalf("dial player: %v", err)
	}

	handshake := buildHandshake(767, roomCode+".mc.example.com", 25565, mcproto.NextStateStatus)
	if _, err := player.Write(handshake); err != nil {
		t.Fatalf("write handshake: %v", err)
	}

	_ = control.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := control.ReadMessage()
	if err != nil {
		t.Fatalf("read control message: %v", err)
	}
	env, err := protocol.Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Type != protocol.TypePeerJoined {
		t.Fatalf("type = %q, want peer_joined", env.Type)
	}
	joined, err := protocol.As[protocol.PeerJoined](env)
	if err != nil {
		t.Fatalf("as: %v", err)
	}
	if joined.PlayerIP == "" {
		t.Fatal("player address missing")
	}

	_ = player.Close()
	_ = control.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err = control.ReadMessage()
	if err != nil {
		t.Fatalf("read peer_left: %v", err)
	}
	env, err = protocol.Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Type != protocol.TypePeerLeft {
		t.Fatalf("type = %q, want peer_left", env.Type)
	}
}

// A full room must turn players away before a stream is even opened, with a
// message they can read instead of a silent drop.
func TestFullRoomIsRejected(t *testing.T) {
	srv, ts := newDataTestServerWith(t, func(cfg *config.Server) {
		cfg.Limits.MaxPlayersPerRoom = 1
	})

	_, roomCode := registerRoomConn(t, ts, "dev-1")
	acceptor := openDataSession(t, ts, roomCode, "dev-1")
	waitForDialer(t, srv, roomCode)

	// Stand in for the local server: hold every stream open until the test ends.
	accepted := make(chan struct{}, 4)
	release := make(chan struct{})
	defer close(release)
	go func() {
		for {
			stream, _, err := acceptor.Accept()
			if err != nil {
				return
			}
			accepted <- struct{}{}
			go func(conn net.Conn) {
				<-release
				_ = conn.Close()
			}(stream)
		}
	}()

	first, err := net.Dial("tcp", srv.PlayerAddr().String())
	if err != nil {
		t.Fatalf("dial first player: %v", err)
	}
	defer func() { _ = first.Close() }()
	if _, err := first.Write(buildHandshake(767, roomCode+".mc.example.com", 25565, mcproto.NextStateLogin)); err != nil {
		t.Fatalf("write first handshake: %v", err)
	}

	select {
	case <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("the first player was never forwarded")
	}

	second, err := net.Dial("tcp", srv.PlayerAddr().String())
	if err != nil {
		t.Fatalf("dial second player: %v", err)
	}
	defer func() { _ = second.Close() }()
	_ = second.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := second.Write(buildHandshake(767, roomCode+".mc.example.com", 25565, mcproto.NextStateLogin)); err != nil {
		t.Fatalf("write second handshake: %v", err)
	}

	packetID, payload, err := mcproto.ReadPacket(second, 32767)
	if err != nil {
		t.Fatalf("read rejection: %v", err)
	}
	if packetID != 0x00 {
		t.Fatalf("packet id = %d, want a disconnect", packetID)
	}

	var doc struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil {
		t.Fatalf("disconnect json: %v", err)
	}
	if doc.Text != messageRoomFull {
		t.Fatalf("message = %q, want %q", doc.Text, messageRoomFull)
	}

	// A freed slot must let the next player through. The relay needs a moment to
	// notice the departure, so retry like a real client would.
	_ = first.Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err := net.Dial("tcp", srv.PlayerAddr().String())
		if err != nil {
			t.Fatalf("dial after the slot was freed: %v", err)
		}
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := conn.Write(buildHandshake(767, roomCode+".mc.example.com", 25565, mcproto.NextStateLogin)); err != nil {
			t.Fatalf("write handshake: %v", err)
		}

		select {
		case <-accepted:
			_ = conn.Close()
			return
		case <-time.After(200 * time.Millisecond):
			_ = conn.Close()
		}
		if time.Now().After(deadline) {
			t.Fatal("the freed slot was never reused")
		}
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
