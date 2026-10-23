package mcproto

import (
	"fmt"
	"net"
)

// ProxyHeader builds a PROXY protocol v1 header that tells the local server
// which address the player really came from.
//
// The header must be written before any Minecraft bytes. It is only understood
// by servers configured to expect it (Paper/Spigot: proxy-protocol: true).
func ProxyHeader(clientIP string, clientPort int, localIP string, localPort int) ([]byte, error) {
	src := net.ParseIP(clientIP)
	if src == nil {
		return nil, fmt.Errorf("invalid client address %q", clientIP)
	}

	family := "TCP4"
	dst := net.IPv4(127, 0, 0, 1)
	if src.To4() == nil {
		family = "TCP6"
		dst = net.IPv6loopback
	}
	// A destination of the other family would make the header self
	// inconsistent, so the advertised local address follows the client.
	if parsed := net.ParseIP(localIP); parsed != nil {
		if (family == "TCP4") == (parsed.To4() != nil) {
			dst = parsed
		}
	}

	line := fmt.Sprintf("PROXY %s %s %s %d %d\r\n", family, src.String(), dst.String(), clientPort, localPort)
	return []byte(line), nil
}
