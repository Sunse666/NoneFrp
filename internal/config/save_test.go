package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveClientRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.yaml")

	want := DefaultClient()
	want.Token = "NFRP-7K2M9QXP"
	want.Relay = []Relay{
		{Name: "official", URL: "wss://relay.example.com"},
		{Name: "self", URL: "wss://relay.mydomain.net"},
	}
	want.Local.Port = 25570
	want.Local.ProxyProtocol = true
	want.Room.Code = "b7k2m9"

	if err := SaveClient(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := LoadClient(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Token != want.Token {
		t.Fatalf("token = %q", got.Token)
	}
	if len(got.Relay) != 2 || got.Relay[1].URL != "wss://relay.mydomain.net" {
		t.Fatalf("relay = %+v", got.Relay)
	}
	if got.Local.Port != 25570 || !got.Local.ProxyProtocol {
		t.Fatalf("local = %+v", got.Local)
	}
	if got.Room.Code != "b7k2m9" {
		t.Fatalf("room = %+v", got.Room)
	}
}

func TestSaveClientWritesHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.yaml")

	cfg := DefaultClient()
	cfg.Token = "NFRP-7K2M9QXP"
	cfg.Relay = []Relay{{Name: "x", URL: "wss://relay.example.com"}}
	if err := SaveClient(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasPrefix(string(raw), SaveHeader) {
		t.Fatalf("file does not start with the header: %q", string(raw[:min(len(raw), 80)]))
	}
}

func TestSaveClientRejectsInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.yaml")

	cfg := DefaultClient()
	cfg.Relay = []Relay{{Name: "x", URL: "https://wrong.example.com"}}
	cfg.Token = "t"
	if err := SaveClient(path, cfg); err == nil {
		t.Fatal("expected a validation error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("an invalid configuration must not be written")
	}
}

func TestSaveServerDurationRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")

	cfg := DefaultServer()
	cfg.DomainSuffix = "mc.example.com"
	cfg.GracePeriod = Duration(90 * time.Second)
	cfg.Tokens = map[string]string{"NFRP-7K2M9QXP": "a host"}

	if err := SaveServer(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadServer(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.GracePeriod.Std() != 90*time.Second {
		t.Fatalf("grace = %v", got.GracePeriod.Std())
	}
	if got.Tokens["NFRP-7K2M9QXP"] != "a host" {
		t.Fatalf("tokens = %v", got.Tokens)
	}
}
