package config

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

// Credential policy errors, reported by Check and mapped to protocol codes by
// the relay.
var (
	// ErrInviteExpired means the credential is past its expiry.
	ErrInviteExpired = errors.New("invite expired")
	// ErrInviteExhausted means every device slot has been taken.
	ErrInviteExhausted = errors.New("invite exhausted")
)

// Token is a relay credential. In YAML it may be a plain string, which means
// permanent and unlimited, or a mapping carrying a policy:
//
//	tokens:
//	  NFRP-AAAA2222: "a host"                     # permanent
//	  JOIN-3X8P2R:
//	    note: "a friend"
//	    max_devices: 1
//	    expires: 2026-11-01T00:00:00Z
type Token struct {
	Note string
	// MaxDevices caps how many distinct devices may register with this
	// credential. Zero means unlimited.
	MaxDevices int
	// Expires is when the credential stops working. Zero means never.
	Expires time.Time
}

// UnmarshalYAML accepts both the string and the mapping form.
func (t *Token) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		var note string
		if err := value.Decode(&note); err != nil {
			return fmt.Errorf("decode token: %w", err)
		}
		*t = Token{Note: note}
		return nil
	}

	var raw struct {
		Note       string    `yaml:"note"`
		MaxDevices int       `yaml:"max_devices"`
		Expires    time.Time `yaml:"expires"`
	}
	if err := value.Decode(&raw); err != nil {
		return fmt.Errorf("decode token: %w", err)
	}
	*t = Token{Note: raw.Note, MaxDevices: raw.MaxDevices, Expires: raw.Expires}
	return nil
}

// MarshalYAML keeps the compact form for credentials without a policy, so a
// rewritten configuration stays readable.
func (t Token) MarshalYAML() (any, error) {
	if t.MaxDevices == 0 && t.Expires.IsZero() {
		return t.Note, nil
	}
	out := map[string]any{"note": t.Note}
	if t.MaxDevices > 0 {
		out["max_devices"] = t.MaxDevices
	}
	if !t.Expires.IsZero() {
		out["expires"] = t.Expires.Format(time.RFC3339)
	}
	return out, nil
}

// Permanent reports whether the credential never expires and has no device cap.
func (t Token) Permanent() bool {
	return t.MaxDevices == 0 && t.Expires.IsZero()
}

// Check reports whether deviceID may register with this credential. used holds
// the devices that already registered; it is ignored for unlimited
// credentials.
func (t Token) Check(deviceID string, used []string, now time.Time) error {
	if !t.Expires.IsZero() && now.After(t.Expires) {
		return ErrInviteExpired
	}
	if t.MaxDevices <= 0 {
		return nil
	}
	for _, known := range used {
		if known == deviceID {
			return nil
		}
	}
	if len(used) >= t.MaxDevices {
		return ErrInviteExhausted
	}
	return nil
}

// Usage renders the policy for a listing, e.g. "1/1 devices, expires 2026-11-01".
func (t Token) Usage(used int) string {
	devices := "unlimited devices"
	if t.MaxDevices > 0 {
		devices = fmt.Sprintf("%d/%d devices", used, t.MaxDevices)
	}
	expiry := "never expires"
	if !t.Expires.IsZero() {
		expiry = "expires " + t.Expires.Format("2006-01-02")
	}
	return devices + ", " + expiry
}

// SortedCodes returns the credential codes in a stable order.
func (s Server) SortedCodes() []string {
	codes := make([]string, 0, len(s.Tokens))
	for code := range s.Tokens {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}
