package client

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Sunse666/NoneFrp/internal/config"
	"github.com/Sunse666/NoneFrp/internal/mcproto"
	"github.com/Sunse666/NoneFrp/internal/protocol"
	"github.com/Sunse666/NoneFrp/internal/server"
)

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

func buildHandshake(address string, nextState int) []byte {
	var body []byte
	body = append(body, encodeVarInt(0x00)...)
	body = append(body, encodeVarInt(767)...)
	body = append(body, encodeString(address)...)
	body = append(body, 0x63, 0xdd)
	body = append(body, encodeVarInt(nextState)...)
	return append(encodeVarInt(len(body)), body...)
}

// fakeMinecraft listens on a loopback port, consumes a single byte and replies
// with a canned payload, standing in for a real server.
func fakeMinecraft(t *testing.T, reply []byte) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
				var first [1]byte
				if _, err := io.ReadFull(c, first[:]); err != nil {
					return
				}
				_, _ = c.Write(reply)
			}(conn)
		}
	}()

	return ln.Addr().(*net.TCPAddr).Port
}

func TestServeStreamForwardsToLocalServer(t *testing.T) {
	reply := []byte("pong!")
	port := fakeMinecraft(t, reply)

	cfg := config.DefaultClient()
	cfg.Local.Port = port
	cfg.Token = "NFRP-TESTTEST"
	cfg.Relay = []config.Relay{{Name: "test", URL: "ws://127.0.0.1:1"}}

	agent, err := New(cfg, t.TempDir(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	agentSide, playerSide := net.Pipe()
	defer func() { _ = playerSide.Close() }()
	go agent.serveStream(agentSide, protocol.StreamMeta{TraceID: "trace"})

	_ = playerSide.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := playerSide.Write([]byte("h")); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := make([]byte, len(reply))
	if _, err := io.ReadFull(playerSide, got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, reply) {
		t.Fatalf("got %q, want %q", got, reply)
	}
}

func TestServeStreamSendsProxyProtocolHeader(t *testing.T) {
	lines := make(chan string, 1)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		reader := bufio.NewReader(conn)
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		lines <- line
		_, _ = conn.Write([]byte("ok"))
	}()

	cfg := config.DefaultClient()
	cfg.Local.Port = ln.Addr().(*net.TCPAddr).Port
	cfg.Local.ProxyProtocol = true
	cfg.Token = "NFRP-TESTTEST"
	cfg.Relay = []config.Relay{{Name: "test", URL: "ws://127.0.0.1:1"}}

	agent, err := New(cfg, t.TempDir(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	agentSide, playerSide := net.Pipe()
	defer func() { _ = playerSide.Close() }()
	go agent.serveStream(agentSide, protocol.StreamMeta{ClientIP: "198.51.100.9", ClientPort: 54321})

	_ = playerSide.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := playerSide.Write([]byte("h")); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case line := <-lines:
		want := "PROXY TCP4 198.51.100.9 127.0.0.1 54321 " + strconv.Itoa(cfg.Local.Port) + "\r\n"
		if line != want {
			t.Fatalf("header = %q, want %q", line, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no proxy protocol header was written")
	}
}

func TestServeStreamUpdatesStatus(t *testing.T) {
	reply := []byte("pong!")
	port := fakeMinecraft(t, reply)

	cfg := config.DefaultClient()
	cfg.Local.Port = port
	cfg.Token = "NFRP-TESTTEST"
	cfg.Relay = []config.Relay{{Name: "test", URL: "ws://127.0.0.1:1"}}

	agent, err := New(cfg, t.TempDir(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if got := agent.Status().Players; got != 0 {
		t.Fatalf("players = %d before any connection", got)
	}

	agentSide, playerSide := net.Pipe()
	defer func() { _ = playerSide.Close() }()
	go agent.serveStream(agentSide, protocol.StreamMeta{TraceID: "trace"})

	_ = playerSide.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := playerSide.Write([]byte("h")); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := make([]byte, len(reply))
	if _, err := io.ReadFull(playerSide, got); err != nil {
		t.Fatalf("read: %v", err)
	}
	_ = playerSide.Close()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status := agent.Status()
		if status.Players == 0 && status.BytesUp == 1 && status.BytesDown == int64(len(reply)) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("status = %+v", agent.Status())
}

func TestServeStreamReportsUnreachableLocalServer(t *testing.T) {
	// Bind and close a listener to obtain a port that nothing listens on.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	cfg := config.DefaultClient()
	cfg.Local.Port = port
	cfg.Token = "NFRP-TESTTEST"
	cfg.Relay = []config.Relay{{Name: "test", URL: "ws://127.0.0.1:1"}}

	agent, err := New(cfg, t.TempDir(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	agentSide, playerSide := net.Pipe()
	defer func() { _ = playerSide.Close() }()
	done := make(chan struct{})
	go func() {
		agent.serveStream(agentSide, protocol.StreamMeta{})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("serveStream did not give up on an unreachable local server")
	}
}

func TestPlayerReachesLocalServerEndToEnd(t *testing.T) {
	reply := []byte("SERVER-REPLY")
	mcPort := fakeMinecraft(t, reply)

	relayCfg := config.DefaultServer()
	relayCfg.DomainSuffix = "mc.example.com"
	relayCfg.PublicPort = 0
	relayCfg.Tokens = map[string]string{testToken: "test host"}
	relay := server.New(relayCfg, slog.New(slog.DiscardHandler))

	relayCtx, stopRelay := context.WithCancel(context.Background())
	defer stopRelay()
	go func() { _ = relay.ServePublic(relayCtx) }()

	deadline := time.Now().Add(3 * time.Second)
	for relay.PlayerAddr() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if relay.PlayerAddr() == nil {
		t.Fatal("relay never opened its public port")
	}

	ts := httptest.NewServer(relay.Handler())
	defer ts.Close()

	cfg := config.DefaultClient()
	cfg.Local.Port = mcPort
	cfg.Token = testToken
	cfg.Relay = []config.Relay{{Name: "test", URL: "ws" + ts.URL[len("http"):]}}

	agent, err := New(cfg, t.TempDir(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	registered := make(chan protocol.Registered, 1)
	agent.OnRegistered = func(r protocol.Registered) { registered <- r }

	agentCtx, stopAgent := context.WithCancel(context.Background())
	defer stopAgent()
	go func() { _ = agent.Run(agentCtx) }()

	var room protocol.Registered
	select {
	case room = <-registered:
	case <-time.After(5 * time.Second):
		t.Fatal("agent never registered")
	}

	handshake := buildHandshake(room.RoomCode+".mc.example.com", mcproto.NextStateStatus)

	player, err := net.Dial("tcp", relay.PlayerAddr().String())
	if err != nil {
		t.Fatalf("dial player port: %v", err)
	}
	defer func() { _ = player.Close() }()

	if _, err := player.Write(handshake); err != nil {
		t.Fatalf("write handshake: %v", err)
	}

	_ = player.SetReadDeadline(time.Now().Add(10 * time.Second))
	got := make([]byte, len(reply))
	if _, err := io.ReadFull(player, got); err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if !bytes.Equal(got, reply) {
		t.Fatalf("player got %q, want %q", got, reply)
	}
}
