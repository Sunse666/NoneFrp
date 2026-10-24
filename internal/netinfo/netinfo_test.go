package netinfo

import (
	"net"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		ip   string
		kind Kind
		ok   bool
	}{
		{"192.168.1.5", KindLAN, true},
		{"10.1.2.3", KindLAN, true},
		{"172.16.0.9", KindLAN, true},
		{"8.8.8.8", KindPublic, true},
		{"100.101.42.7", KindTailnet, true},
		{"2409:8938:c72:7b00::1", KindPublic, true},
		{"fdfd::1", "", false},
		{"fe80::1", "", false},
		{"2001:0:14c9::1", "", false},
		{"127.0.0.1", "", false},
		{"::1", "", false},
		{"169.254.1.1", "", false},
		{"0.0.0.0", "", false},
	}

	for _, tc := range cases {
		ip := net.ParseIP(tc.ip)
		if ip == nil {
			t.Fatalf("bad test address %q", tc.ip)
		}
		got, ok := classify(ip)
		if ok != tc.ok {
			t.Fatalf("classify(%s) ok = %v, want %v", tc.ip, ok, tc.ok)
		}
		if ok && got.Kind != tc.kind {
			t.Fatalf("classify(%s) kind = %q, want %q", tc.ip, got.Kind, tc.kind)
		}
	}
}

func TestEndpoint(t *testing.T) {
	if got := Endpoint("192.168.1.5", 25565); got != "192.168.1.5:25565" {
		t.Fatalf("endpoint = %q", got)
	}
	if got := Endpoint("2409:8938:c72:7b00::1", 25565); got != "[2409:8938:c72:7b00::1]:25565" {
		t.Fatalf("endpoint = %q", got)
	}
}

func TestDescribe(t *testing.T) {
	if got := Describe(Candidate{IP: "192.168.1.5", Kind: KindLAN}, 25565); got != "192.168.1.5:25565（局域网）" {
		t.Fatalf("describe = %q", got)
	}
	if got := Describe(Candidate{IP: "100.101.42.7", Kind: KindTailnet}, 25565); got != "100.101.42.7:25565（社团网络，需装客户端）" {
		t.Fatalf("describe = %q", got)
	}
	if got := Describe(Candidate{IP: "2409::1", Kind: KindPublic}, 25565); got != "[2409::1]:25565（公网 IPv6）" {
		t.Fatalf("describe = %q", got)
	}
}

func TestIsVirtualInterface(t *testing.T) {
	cases := map[string]bool{
		"Radmin VPN": true,
		// A mesh adapter is no longer hidden: its 100.64/10 address is
		// classified as a tailnet address instead.
		"Tailscale":                     false,
		"VMware Network Adapter VMnet8": true,
		"vEthernet (WSL)":               true,
		"以太网":                           false,
		"WLAN":                          false,
	}
	for name, want := range cases {
		if got := isVirtualInterface(name); got != want {
			t.Fatalf("isVirtualInterface(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestCandidatesAreLoopbackFree(t *testing.T) {
	for _, candidate := range Candidates() {
		ip := net.ParseIP(candidate.IP)
		if ip == nil {
			t.Fatalf("unparsable candidate %q", candidate.IP)
		}
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			t.Fatalf("candidate %q should have been filtered", candidate.IP)
		}
	}
}
