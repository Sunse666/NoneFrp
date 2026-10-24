package link

import (
	"strings"
	"testing"
)

func TestJoinLinkRoundTrip(t *testing.T) {
	built, err := JoinLink("wss://relay.example.com", "NFRP-7K2M9QXP", "auto")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.HasPrefix(built, "nonefrp://join?") {
		t.Fatalf("link = %q", built)
	}

	got, err := ParseJoin(built)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.RelayURL != "wss://relay.example.com" {
		t.Fatalf("relay = %q", got.RelayURL)
	}
	if got.Token != "NFRP-7K2M9QXP" {
		t.Fatalf("token = %q", got.Token)
	}
	if got.Room != "auto" {
		t.Fatalf("room = %q", got.Room)
	}
}

func TestJoinLinkOmitsEmptyRoom(t *testing.T) {
	built, err := JoinLink("ws://127.0.0.1:8080", "NFRP-7K2M9QXP", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if strings.Contains(built, "room=") {
		t.Fatalf("link = %q, want no room parameter", built)
	}
}

func TestJoinLinkEscapesTheRelayURL(t *testing.T) {
	built, err := JoinLink("wss://relay.example.com/base", "NFRP-7K2M9QXP", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	got, err := ParseJoin(built)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.RelayURL != "wss://relay.example.com/base" {
		t.Fatalf("relay = %q", got.RelayURL)
	}
}

func TestJoinLinkRejectsBadInput(t *testing.T) {
	if _, err := JoinLink("https://relay.example.com", "NFRP-7K2M9QXP", ""); err == nil {
		t.Fatal("expected an error for a non-websocket relay")
	}
	if _, err := JoinLink("", "NFRP-7K2M9QXP", ""); err == nil {
		t.Fatal("expected an error for an empty relay")
	}
	if _, err := JoinLink("wss://relay.example.com", "", ""); err == nil {
		t.Fatal("expected an error for an empty token")
	}
}

func TestParseJoinRejectsForeignLinks(t *testing.T) {
	cases := []string{
		"",
		"https://example.com",
		"nonefrp://other?relay=wss%3A%2F%2Fr.example.com&token=t",
		"nonefrp://join?token=t",
		"nonefrp://join?relay=wss%3A%2F%2Fr.example.com",
		"nonefrp://join?relay=http%3A%2F%2Fr.example.com&token=t",
	}
	for _, raw := range cases {
		if _, err := ParseJoin(raw); err == nil {
			t.Fatalf("%q should be rejected", raw)
		}
	}
}
