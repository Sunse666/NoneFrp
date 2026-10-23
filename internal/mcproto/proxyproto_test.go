package mcproto

import (
	"strings"
	"testing"
)

func TestProxyHeaderIPv4(t *testing.T) {
	got, err := ProxyHeader("198.51.100.9", 54321, "127.0.0.1", 25565)
	if err != nil {
		t.Fatalf("header: %v", err)
	}
	want := "PROXY TCP4 198.51.100.9 127.0.0.1 54321 25565\r\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if !strings.HasSuffix(string(got), "\r\n") {
		t.Fatal("header must end with CRLF")
	}
}

func TestProxyHeaderIPv6(t *testing.T) {
	got, err := ProxyHeader("2001:db8::1", 54321, "127.0.0.1", 25565)
	if err != nil {
		t.Fatalf("header: %v", err)
	}
	// The destination must follow the client family even though the local
	// listener is on IPv4 loopback.
	want := "PROXY TCP6 2001:db8::1 ::1 54321 25565\r\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestProxyHeaderRejectsBadAddress(t *testing.T) {
	if _, err := ProxyHeader("not-an-ip", 1, "127.0.0.1", 25565); err == nil {
		t.Fatal("expected an error for an invalid address")
	}
}
