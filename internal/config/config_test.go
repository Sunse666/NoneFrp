package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestLoadServerAppliesDefaults(t *testing.T) {
	path := writeTemp(t, "server.yaml", `domain_suffix: mc.example.com
`)

	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Listen != "127.0.0.1:8080" {
		t.Fatalf("listen = %q", cfg.Listen)
	}
	if cfg.PublicPort != 25565 {
		t.Fatalf("public_port = %d", cfg.PublicPort)
	}
	if cfg.GracePeriod.Std() != 60*time.Second {
		t.Fatalf("grace_period = %v", cfg.GracePeriod.Std())
	}
	if cfg.Limits.MaxPlayersPerRoom != 10 {
		t.Fatalf("max_players_per_room = %d", cfg.Limits.MaxPlayersPerRoom)
	}
}

func TestLoadServerParsesDurationsAndTokens(t *testing.T) {
	path := writeTemp(t, "server.yaml", `domain_suffix: mc.example.com
grace_period: 90s
tokens:
  NFRP-AAAA: "first host"
`)

	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.GracePeriod.Std() != 90*time.Second {
		t.Fatalf("grace_period = %v", cfg.GracePeriod.Std())
	}
	if cfg.Tokens["NFRP-AAAA"] != "first host" {
		t.Fatalf("tokens = %v", cfg.Tokens)
	}
}

func TestLoadServerRejectsBadDuration(t *testing.T) {
	path := writeTemp(t, "server.yaml", `domain_suffix: mc.example.com
grace_period: soon
`)
	if _, err := LoadServer(path); err == nil {
		t.Fatal("expected an error for an invalid duration")
	}
}

func TestLoadServerRejectsMissingDomainSuffix(t *testing.T) {
	path := writeTemp(t, "server.yaml", `listen: 127.0.0.1:8080
`)
	if _, err := LoadServer(path); err == nil {
		t.Fatal("expected a validation error")
	}
}

func TestLoadClientAppliesDefaultsAndEnvOverrides(t *testing.T) {
	path := writeTemp(t, "client.yaml", `relay:
  - name: file
    url: wss://from-file.example.com
token: from-file
`)
	t.Setenv("NONERP_TOKEN", "from-env")
	t.Setenv("NONERP_LOCAL_PORT", "25570")

	cfg, err := LoadClient(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Token != "from-env" {
		t.Fatalf("token = %q", cfg.Token)
	}
	if cfg.Local.Port != 25570 {
		t.Fatalf("local port = %d", cfg.Local.Port)
	}
	if cfg.Room.Code != "auto" {
		t.Fatalf("room code = %q", cfg.Room.Code)
	}
	if len(cfg.Relay) != 1 || cfg.Relay[0].URL != "wss://from-file.example.com" {
		t.Fatalf("relay = %+v", cfg.Relay)
	}
}

func TestLoadClientRelayEnvOverride(t *testing.T) {
	path := writeTemp(t, "client.yaml", `relay:
  - name: file
    url: wss://from-file.example.com
token: t
`)
	t.Setenv("NONERP_RELAY", "wss://a.example.com, wss://b.example.com")

	cfg, err := LoadClient(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Relay) != 2 || cfg.Relay[1].URL != "wss://b.example.com" {
		t.Fatalf("relay = %+v", cfg.Relay)
	}
}

func TestLoadClientRejectsBadScheme(t *testing.T) {
	path := writeTemp(t, "client.yaml", `relay:
  - url: https://relay.example.com
token: t
`)
	if _, err := LoadClient(path); err == nil {
		t.Fatal("expected an error for a non-websocket relay url")
	}
}

func TestRoomHost(t *testing.T) {
	cfg := DefaultServer()
	cfg.DomainSuffix = "mc.example.com"
	if got := cfg.RoomHost("a1b2"); got != "a1b2.mc.example.com" {
		t.Fatalf("room host = %q", got)
	}
}
