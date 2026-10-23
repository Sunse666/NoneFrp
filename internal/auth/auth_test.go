package auth

import (
	"strings"
	"testing"
)

func TestGenerateTokenShape(t *testing.T) {
	token, err := GenerateToken()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.HasPrefix(token, TokenPrefix) {
		t.Fatalf("token %q is missing the prefix", token)
	}
	if len(token) != len(TokenPrefix)+TokenLength {
		t.Fatalf("token %q has length %d", token, len(token))
	}
	if !ValidToken(token) {
		t.Fatalf("token %q failed validation", token)
	}
}

func TestGenerateTokenIsRandom(t *testing.T) {
	seen := map[string]bool{}
	for range 64 {
		token, err := GenerateToken()
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if seen[token] {
			t.Fatalf("duplicate token %q", token)
		}
		seen[token] = true
	}
}

func TestValidTokenRejectsJunk(t *testing.T) {
	cases := []string{"", "NFRP-", "NFRP-short", "XXXX-7K2M9QXP", "NFRP-7K2M9QXO", "NFRP-7K2M9QX"}
	for _, c := range cases {
		if ValidToken(c) {
			t.Fatalf("%q should be rejected", c)
		}
	}
}

func TestGenerateRoomCodeShape(t *testing.T) {
	code, err := GenerateRoomCode()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(code) != RoomCodeLength {
		t.Fatalf("code %q has length %d", code, len(code))
	}
	normalized, err := NormalizeRoomCode(code)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if normalized != code {
		t.Fatalf("normalized %q != %q", normalized, code)
	}
}

func TestNormalizeRoomCode(t *testing.T) {
	got, err := NormalizeRoomCode("  A2B3C4 ")
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got != "a2b3c4" {
		t.Fatalf("normalized = %q", got)
	}

	for _, bad := range []string{"", "a1b0c3", "a2b3c", "room_one"} {
		if _, err := NormalizeRoomCode(bad); err == nil {
			t.Fatalf("%q should be rejected", bad)
		}
	}
}
