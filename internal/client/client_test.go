package client

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Sunse666/NoneFrp/internal/config"
	"github.com/Sunse666/NoneFrp/internal/protocol"
	"github.com/Sunse666/NoneFrp/internal/server"
)

const testToken = "NFRP-TESTTEST"

func startRelay(t *testing.T) (*httptest.Server, config.Server) {
	t.Helper()
	cfg := config.DefaultServer()
	cfg.DomainSuffix = "mc.example.com"
	cfg.GracePeriod = config.Duration(30 * time.Second)
	cfg.Tokens = map[string]string{testToken: "test host"}
	relay := server.New(cfg, slog.New(slog.DiscardHandler))
	ts := httptest.NewServer(relay.Handler())
	t.Cleanup(ts.Close)
	return ts, cfg
}

func newAgent(t *testing.T, relayURL string) *Client {
	t.Helper()
	cfg := config.DefaultClient()
	cfg.Token = testToken
	cfg.Relay = []config.Relay{{Name: "test", URL: "ws" + strings.TrimPrefix(relayURL, "http")}}

	agent, err := New(cfg, t.TempDir(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return agent
}

func TestConnectRegistersRoom(t *testing.T) {
	ts, serverCfg := startRelay(t)
	agent := newAgent(t, ts.URL)

	got := make(chan protocol.Registered, 1)
	agent.OnRegistered = func(r protocol.Registered) { got <- r }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx) }()

	select {
	case registered := <-got:
		if registered.RoomCode == "" {
			t.Fatal("empty room code")
		}
		want := registered.RoomCode + "." + serverCfg.DomainSuffix
		if registered.PublicHost != want {
			t.Fatalf("public host = %q, want %q", registered.PublicHost, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("registration timed out")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("agent did not stop")
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	ts, _ := startRelay(t)
	agent := newAgent(t, ts.URL)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("agent did not stop after cancel")
	}
}

func TestBadTokenIsReported(t *testing.T) {
	ts, _ := startRelay(t)
	cfg := config.DefaultClient()
	cfg.Token = "NFRP-WRONGWRO"
	cfg.Relay = []config.Relay{{Name: "test", URL: "ws" + strings.TrimPrefix(ts.URL, "http")}}

	agent, err := New(cfg, t.TempDir(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = agent.connect(context.Background(), cfg.Relay[0])
	if err == nil {
		t.Fatal("expected an error for an invalid token")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %v, want it to mention 401", err)
	}
}

func TestDeviceIDIsStable(t *testing.T) {
	dir := t.TempDir()

	first, err := loadOrCreateDeviceID(dir)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := loadOrCreateDeviceID(dir)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first != second {
		t.Fatalf("device id changed: %q then %q", first, second)
	}
	if len(first) != 32 {
		t.Fatalf("device id length = %d", len(first))
	}

	info, err := os.Stat(filepath.Join(dir, "device_id"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// Windows does not honour Unix permission bits, so only check them where
	// they are meaningful.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}

func TestJitterStaysClose(t *testing.T) {
	for range 100 {
		got := jitter(time.Second)
		if got < 800*time.Millisecond || got > 1200*time.Millisecond {
			t.Fatalf("jitter(%v) = %v", time.Second, got)
		}
	}
}
