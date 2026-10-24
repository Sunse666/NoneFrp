// Package netinfo lists the addresses a player could use to reach the local
// server without the relay: a LAN address for the same network, or a global
// address when the host happens to be reachable from the internet.
package netinfo

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
)

// Kind describes who can reach an address.
type Kind string

// Address kinds.
const (
	// KindLAN is a private IPv4 address, reachable inside the local network.
	KindLAN Kind = "lan"
	// KindPublic is a globally routable address, reachable from the internet if
	// nothing (router firewall, ISP) blocks it.
	KindPublic Kind = "public"
	// KindTailnet is a 100.64.0.0/10 address handed out by a mesh network such
	// as Tailscale. It is only reachable by devices that joined that network.
	KindTailnet Kind = "tailnet"
)

// Candidate is one address a player might connect to.
type Candidate struct {
	IP   string `json:"ip"`
	Kind Kind   `json:"kind"`
}

// Endpoint renders ip and port the way a Minecraft address field expects it.
func Endpoint(ip string, port int) string {
	if strings.Contains(ip, ":") {
		return "[" + ip + "]:" + strconv.Itoa(port)
	}
	return net.JoinHostPort(ip, strconv.Itoa(port))
}

// Candidates lists the usable local addresses, LAN first.
func Candidates() []Candidate {
	var out []Candidate
	seen := make(map[string]bool)

	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isVirtualInterface(iface.Name) {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			var ip net.IP
			switch value := address.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			default:
				continue
			}
			candidate, ok := classify(ip)
			if !ok || seen[candidate.IP] {
				continue
			}
			seen[candidate.IP] = true
			out = append(out, candidate)
		}
	}

	out = dedupePrefixes(out)

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return rank(out[i].Kind) < rank(out[j].Kind)
		}
		return out[i].IP < out[j].IP
	})
	return out
}

// rank orders the kinds the way a host should read them: someone next to them
// first, then the club network, then the open internet.
func rank(kind Kind) int {
	switch kind {
	case KindLAN:
		return 0
	case KindTailnet:
		return 1
	default:
		return 2
	}
}

// dedupePrefixes keeps one IPv6 address per /64. Windows keeps a stable address
// and a rotating privacy address in the same prefix, and showing both only
// confuses the person choosing one to share.
func dedupePrefixes(in []Candidate) []Candidate {
	out := make([]Candidate, 0, len(in))
	seen := make(map[string]bool)

	for _, candidate := range in {
		ip := net.ParseIP(candidate.IP)
		key := candidate.IP
		if ip != nil && strings.Contains(candidate.IP, ":") {
			if network := (&net.IPNet{IP: ip.Mask(net.CIDRMask(64, 128)), Mask: net.CIDRMask(64, 128)}); network.IP != nil {
				key = network.String()
			}
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, candidate)
	}
	return out
}

// virtualInterfaceHints names adapters that carry traffic to something other
// than the physical network. Addresses on them look public but are not
// reachable by a player.
var virtualInterfaceHints = []string{
	"vpn", "tap", "tun", "virtual", "vmware", "hyper-v", "vethernet",
	"docker", "radmin", "hamachi", "wireguard", "clash",
}

func isVirtualInterface(name string) bool {
	lower := strings.ToLower(name)
	for _, hint := range virtualInterfaceHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

// classify decides whether an address is worth showing to the host.
func classify(ip net.IP) (Candidate, bool) {
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
		return Candidate{}, false
	}

	if v4 := ip.To4(); v4 != nil {
		if inTailnetRange(v4) {
			return Candidate{IP: v4.String(), Kind: KindTailnet}, true
		}
		if ip.IsPrivate() {
			return Candidate{IP: v4.String(), Kind: KindLAN}, true
		}
		return Candidate{IP: v4.String(), Kind: KindPublic}, true
	}

	// IPv6: unique local addresses are not reachable from the internet, and
	// Teredo tunnels are too unreliable to advertise.
	if ip.IsPrivate() {
		return Candidate{}, false
	}
	if isTeredo(ip) {
		return Candidate{}, false
	}
	return Candidate{IP: ip.String(), Kind: KindPublic}, true
}

// tailnetRange is 100.64.0.0/10, the carrier grade NAT space that mesh
// networks such as Tailscale allocate from.
var tailnetRange = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func inTailnetRange(ip net.IP) bool {
	return tailnetRange.Contains(ip)
}

// isTeredo reports whether ip is inside 2001:0::/32.
func isTeredo(ip net.IP) bool {
	return len(ip) == net.IPv6len && ip[0] == 0x20 && ip[1] == 0x01 && ip[2] == 0x00 && ip[3] == 0x00
}

// Describe renders a candidate for display, e.g. "[2409::1]:25565 (公网 IPv6)".
func Describe(candidate Candidate, port int) string {
	label := "局域网"
	switch candidate.Kind {
	case KindTailnet:
		label = "社团网络，需装客户端"
	case KindPublic:
		if strings.Contains(candidate.IP, ":") {
			label = "公网 IPv6"
		} else {
			label = "公网 IPv4"
		}
	}
	return fmt.Sprintf("%s（%s）", Endpoint(candidate.IP, port), label)
}
