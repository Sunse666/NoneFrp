// Package link builds and parses the nonefrp://join URL that carries everything
// a host needs to set up an agent: the relay address and an access token.
package link

import (
	"fmt"
	"net/url"
	"strings"
)

// Scheme and host of a join link.
const (
	Scheme = "nonefrp"
	Host   = "join"
)

// JoinLink builds a link such as
//
//	nonefrp://join?relay=wss%3A%2F%2Frelay.example.com&room=auto&token=NFRP-XXXX
func JoinLink(relayURL, token, room string) (string, error) {
	if err := validateRelay(relayURL); err != nil {
		return "", err
	}
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("join link: token must not be empty")
	}

	query := url.Values{}
	query.Set("relay", relayURL)
	query.Set("token", token)
	if room != "" {
		query.Set("room", room)
	}

	built := url.URL{
		Scheme:   Scheme,
		Host:     Host,
		RawQuery: query.Encode(),
	}
	return built.String(), nil
}

// JoinRequest is the decoded content of a join link.
type JoinRequest struct {
	RelayURL string
	Token    string
	Room     string
}

// ParseJoin decodes a nonefrp://join link.
func ParseJoin(raw string) (JoinRequest, error) {
	var out JoinRequest

	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return out, fmt.Errorf("join link: %w", err)
	}
	if parsed.Scheme != Scheme || parsed.Host != Host {
		return out, fmt.Errorf("join link: unsupported link %q", raw)
	}

	query := parsed.Query()
	out.RelayURL = query.Get("relay")
	out.Token = query.Get("token")
	out.Room = query.Get("room")

	if err := validateRelay(out.RelayURL); err != nil {
		return out, err
	}
	if out.Token == "" {
		return out, fmt.Errorf("join link: token must not be empty")
	}
	return out, nil
}

func validateRelay(relayURL string) error {
	if relayURL == "" {
		return fmt.Errorf("join link: relay must not be empty")
	}
	if !strings.HasPrefix(relayURL, "ws://") && !strings.HasPrefix(relayURL, "wss://") {
		return fmt.Errorf("join link: relay %q must start with ws:// or wss://", relayURL)
	}
	return nil
}
