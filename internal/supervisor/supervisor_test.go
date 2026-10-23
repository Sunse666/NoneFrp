package supervisor

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sunse666/NoneFrp/internal/config"
	"github.com/Sunse666/NoneFrp/internal/server"
)

const testToken = "NFRP-TESTTEST"

func startRelay(t *testing.T) *httptest.Server {
	t.Helper()
	cfg := config.DefaultServer()
	cfg.DomainSuffix = "mc.example.com"
	cfg.GracePeriod = config.Duration(30 * time.Second)
	cfg.Tokens = map[string]string{testToken: "test host"}

	relay := server.New(cfg, slog.New(slog.DiscardHandler))
	ts := httptest.NewServer(relay.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func newRunner(t *testing.T, relayURL, path string) *Runner {
	t.Helper()
	cfg := config.DefaultClient()
	cfg.Token = testToken
	cfg.Relay = []config.Relay{{Name: "test", URL: relayURL}}
	cfg.UI.Enabled = false
	return New(path, t.TempDir(), cfg, slog.New(slog.DiscardHandler))
}

func waitConnected(t *testing.T, runner *Runner, want bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if runner.Status().Connected == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("connected = %v after %v", runner.Status().Connected, timeout)
}

func TestRunnerStartsTheAgent(t *testing.T) {
	ts := startRelay(t)
	path := filepath.Join(t.TempDir(), "client.yaml")
	runner := newRunner(t, "ws"+strings.TrimPrefix(ts.URL, "http"), path)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)
	defer runner.Stop()

	waitConnected(t, runner, true, 5*time.Second)
	if got := runner.Status().RoomCode; got == "" {
		t.Fatal("room code is empty")
	}
}

func TestApplyRestartsWithNewSettings(t *testing.T) {
	ts := startRelay(t)
	path := filepath.Join(t.TempDir(), "client.yaml")
	runner := newRunner(t, "ws"+strings.TrimPrefix(ts.URL, "http"), path)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)
	defer runner.Stop()
	waitConnected(t, runner, true, 5*time.Second)

	updated := runner.Config()
	updated.Local.Port = 25599
	updated.Local.ProxyProtocol = true

	if err := runner.Apply(updated); err != nil {
		t.Fatalf("apply: %v", err)
	}
	waitConnected(t, runner, true, 5*time.Second)

	status := runner.Status()
	if status.LocalPort != 25599 || !status.ProxyProtocol {
		t.Fatalf("status = %+v", status)
	}

	// The settings must be on disk for the next start.
	saved, err := config.LoadClient(path)
	if err != nil {
		t.Fatalf("load saved config: %v", err)
	}
	if saved.Local.Port != 25599 {
		t.Fatalf("saved port = %d", saved.Local.Port)
	}
}

func TestApplyRejectsInvalidSettingsAndKeepsRunning(t *testing.T) {
	ts := startRelay(t)
	path := filepath.Join(t.TempDir(), "client.yaml")
	runner := newRunner(t, "ws"+strings.TrimPrefix(ts.URL, "http"), path)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)
	defer runner.Stop()
	waitConnected(t, runner, true, 5*time.Second)

	broken := runner.Config()
	broken.Relay = []config.Relay{{Name: "bad", URL: "https://relay.example.com"}}

	if err := runner.Apply(broken); err == nil {
		t.Fatal("expected the invalid settings to be rejected")
	}
	if !runner.Status().Connected {
		t.Fatal("the running agent was torn down by an invalid change")
	}
	if got := runner.Config().Relay[0].URL; got == "https://relay.example.com" {
		t.Fatal("the rejected settings were kept in memory")
	}
}

func TestStopDisconnects(t *testing.T) {
	ts := startRelay(t)
	path := filepath.Join(t.TempDir(), "client.yaml")
	runner := newRunner(t, "ws"+strings.TrimPrefix(ts.URL, "http"), path)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)
	waitConnected(t, runner, true, 5*time.Second)

	runner.Stop()
	if runner.Status().Connected {
		t.Fatal("still connected after stop")
	}
}
