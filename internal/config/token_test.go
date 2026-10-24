package config

import (
	"errors"
	"testing"
	"time"
)

func TestTokenParsesBothForms(t *testing.T) {
	path := writeTemp(t, "server.yaml", `domain_suffix: mc.example.com
tokens:
  NFRP-AAAA2222: "老张"
  JOIN-3X8P2R:
    note: "小明的服"
    max_devices: 1
    expires: 2026-11-01T00:00:00Z
`)

	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	legacy := cfg.Tokens["NFRP-AAAA2222"]
	if legacy.Note != "老张" {
		t.Fatalf("legacy note = %q", legacy.Note)
	}
	if !legacy.Permanent() {
		t.Fatal("a plain string token must be permanent")
	}

	invite := cfg.Tokens["JOIN-3X8P2R"]
	if invite.Note != "小明的服" {
		t.Fatalf("invite note = %q", invite.Note)
	}
	if invite.MaxDevices != 1 {
		t.Fatalf("max devices = %d", invite.MaxDevices)
	}
	if invite.Expires.IsZero() {
		t.Fatal("expiry was not parsed")
	}
}

func TestTokenCheck(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

	invite := Token{MaxDevices: 1}
	if err := invite.Check("aaa", nil, now); err != nil {
		t.Fatalf("first device rejected: %v", err)
	}
	if err := invite.Check("aaa", []string{"aaa"}, now); err != nil {
		t.Fatalf("the same device must be allowed again: %v", err)
	}
	if err := invite.Check("bbb", []string{"aaa"}, now); !errors.Is(err, ErrInviteExhausted) {
		t.Fatalf("err = %v, want ErrInviteExhausted", err)
	}

	expired := Token{Expires: now.Add(-time.Minute)}
	if err := expired.Check("aaa", nil, now); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("err = %v, want ErrInviteExpired", err)
	}

	permanent := Token{}
	for range 100 {
		if err := permanent.Check("aaa", []string{"x", "y"}, now); err != nil {
			t.Fatalf("a permanent token must never be limited: %v", err)
		}
	}
}

func TestTokenUsageText(t *testing.T) {
	invite := Token{MaxDevices: 2, Expires: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
	if got := invite.Usage(1); got != "1/2 devices, expires 2026-11-01" {
		t.Fatalf("usage = %q", got)
	}
	if got := (Token{}).Usage(0); got != "unlimited devices, never expires" {
		t.Fatalf("usage = %q", got)
	}
}

func TestTokenRoundTripsThroughSave(t *testing.T) {
	path := writeTemp(t, "server.yaml", `domain_suffix: mc.example.com
grace_period: 60s
tokens:
  NFRP-AAAA2222: "老张"
`)

	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cfg.Tokens["JOIN-3X8P2R"] = Token{Note: "小明", MaxDevices: 1}

	if err := SaveServer(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	reloaded, err := LoadServer(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Tokens["NFRP-AAAA2222"].Note != "老张" {
		t.Fatalf("legacy token lost: %+v", reloaded.Tokens)
	}
	if reloaded.Tokens["JOIN-3X8P2R"].MaxDevices != 1 {
		t.Fatalf("invite lost: %+v", reloaded.Tokens)
	}
}
