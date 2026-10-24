// Package auth issues and normalises the credentials used by the relay:
// access tokens handed to hosts and the short room codes shown to players.
package auth

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// TokenPrefix marks every access token issued by the relay.
const TokenPrefix = "NFRP-"

// tokenAlphabet omits 0/1/I/L/O so tokens survive being read out loud.
const tokenAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// roomAlphabet is the lowercase form used for DNS labels.
const roomAlphabet = "23456789abcdefghjkmnpqrstuvwxyz"

// TokenLength is the number of random characters in an access token.
const TokenLength = 8

// RoomCodeLength is the number of characters of a room code.
const RoomCodeLength = 6

// ErrEmptyValue is returned when a generated value would be empty.
var ErrEmptyValue = errors.New("empty value")

// GenerateToken returns a new access token such as "NFRP-7K2M9QXP".
func GenerateToken() (string, error) {
	body, err := randomString(tokenAlphabet, TokenLength)
	if err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return TokenPrefix + body, nil
}

// InvitePrefix marks a credential handed out as an invitation.
const InvitePrefix = "JOIN-"

// GenerateInvite returns a new invite code such as "JOIN-3X8P2R9T".
func GenerateInvite() (string, error) {
	body, err := randomString(tokenAlphabet, TokenLength)
	if err != nil {
		return "", fmt.Errorf("generate invite: %w", err)
	}
	return InvitePrefix + body, nil
}

// GenerateRoomCode returns a new room code such as "a1b2c3".
func GenerateRoomCode() (string, error) {
	code, err := randomString(roomAlphabet, RoomCodeLength)
	if err != nil {
		return "", fmt.Errorf("generate room code: %w", err)
	}
	return code, nil
}

// NormalizeRoomCode canonicalises a user supplied room code into a DNS label.
func NormalizeRoomCode(raw string) (string, error) {
	code := strings.ToLower(strings.TrimSpace(raw))
	if code == "" {
		return "", ErrEmptyValue
	}
	if len(code) != RoomCodeLength {
		return "", fmt.Errorf("room code %q must be %d characters", raw, RoomCodeLength)
	}
	for _, r := range code {
		if !strings.ContainsRune(roomAlphabet, r) {
			return "", fmt.Errorf("room code %q contains invalid character %q", raw, r)
		}
	}
	return code, nil
}

// ValidToken reports whether raw looks like a token issued by GenerateToken.
func ValidToken(raw string) bool {
	body, ok := strings.CutPrefix(strings.TrimSpace(raw), TokenPrefix)
	if !ok || len(body) != TokenLength {
		return false
	}
	for _, r := range body {
		if !strings.ContainsRune(tokenAlphabet, r) {
			return false
		}
	}
	return true
}

func randomString(alphabet string, n int) (string, error) {
	if n <= 0 {
		return "", ErrEmptyValue
	}
	max := big.NewInt(int64(len(alphabet)))
	var sb strings.Builder
	sb.Grow(n)
	for range n {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		sb.WriteByte(alphabet[idx.Int64()])
	}
	return sb.String(), nil
}
