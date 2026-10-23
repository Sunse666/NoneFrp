// Package mcproto parses the small part of the Minecraft protocol the relay
// needs: the opening handshake packet, which carries the hostname a player
// typed into their client. Everything after that byte range is forwarded
// untouched.
package mcproto

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Protocol constants used while reading the handshake.
const (
	// MaxHandshakeSize caps the first packet so a hostile client cannot make
	// the relay buffer an arbitrary amount of data.
	MaxHandshakeSize = 1024
	// MaxServerAddress is the protocol limit of the server address field.
	MaxServerAddress = 255
	// maxVarIntBytes is the protocol limit for a VarInt.
	maxVarIntBytes = 5
	// legacyPingByte starts the pre-1.7 server list ping.
	legacyPingByte = 0xfe
	// handshakePacketID is the id of the opening packet.
	handshakePacketID = 0x00
)

// NextState values carried in the handshake.
const (
	// NextStateStatus marks a server list ping.
	NextStateStatus = 1
	// NextStateLogin marks a real login attempt.
	NextStateLogin = 2
)

// Errors returned while reading a handshake.
var (
	ErrMalformed  = errors.New("malformed handshake")
	ErrTooLarge   = errors.New("handshake too large")
	ErrLegacyPing = errors.New("legacy server list ping")
)

// Handshake is the decoded opening packet.
type Handshake struct {
	ProtocolVersion int
	ServerAddress   string
	ServerPort      uint16
	NextState       int
	// Raw holds every byte consumed from the stream, so callers can replay the
	// packet downstream before forwarding the rest of the connection.
	Raw []byte
}

// ReadHandshake reads and decodes the opening packet of a Minecraft connection.
func ReadHandshake(r io.Reader) (Handshake, error) {
	raw := make([]byte, 0, 64)

	length, err := readVarIntBytes(r, &raw)
	if err != nil {
		return Handshake{}, err
	}
	if length <= 0 {
		return Handshake{}, fmt.Errorf("%w: packet length %d", ErrMalformed, length)
	}
	if length > MaxHandshakeSize {
		return Handshake{}, fmt.Errorf("%w: packet length %d", ErrTooLarge, length)
	}

	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return Handshake{}, fmt.Errorf("%w: short packet: %v", ErrMalformed, err)
	}
	raw = append(raw, body...)

	decoded, err := parseHandshake(body)
	if err != nil {
		return Handshake{}, err
	}
	decoded.Raw = raw
	return decoded, nil
}

func parseHandshake(body []byte) (Handshake, error) {
	rd := bytes.NewReader(body)
	var out Handshake

	packetID, err := readVarInt(rd)
	if err != nil {
		return out, fmt.Errorf("%w: packet id: %v", ErrMalformed, err)
	}
	if packetID != handshakePacketID {
		return out, fmt.Errorf("%w: unexpected packet id %d", ErrMalformed, packetID)
	}

	version, err := readVarInt(rd)
	if err != nil {
		return out, fmt.Errorf("%w: protocol version: %v", ErrMalformed, err)
	}
	out.ProtocolVersion = version

	address, err := readString(rd, MaxServerAddress)
	if err != nil {
		return out, err
	}
	out.ServerAddress = address

	var port [2]byte
	if _, err := io.ReadFull(rd, port[:]); err != nil {
		return out, fmt.Errorf("%w: server port: %v", ErrMalformed, err)
	}
	out.ServerPort = binary.BigEndian.Uint16(port[:])

	nextState, err := readVarInt(rd)
	if err != nil {
		return out, fmt.Errorf("%w: next state: %v", ErrMalformed, err)
	}
	if nextState != NextStateStatus && nextState != NextStateLogin {
		return out, fmt.Errorf("%w: unsupported next state %d", ErrMalformed, nextState)
	}
	out.NextState = nextState

	return out, nil
}

func readVarIntBytes(r io.Reader, raw *[]byte) (int, error) {
	var value uint32
	var buf [1]byte
	for i := range maxVarIntBytes {
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return 0, fmt.Errorf("%w: %v", ErrMalformed, err)
		}
		b := buf[0]
		if i == 0 && b == legacyPingByte {
			return 0, ErrLegacyPing
		}
		*raw = append(*raw, b)
		value |= uint32(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return int(value), nil
		}
	}
	return 0, fmt.Errorf("%w: varint is too long", ErrMalformed)
}

func readVarInt(r io.ByteReader) (int, error) {
	var value uint32
	for i := range maxVarIntBytes {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		value |= uint32(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return int(value), nil
		}
	}
	return 0, errors.New("varint is too long")
}

func readString(r *bytes.Reader, limit int) (string, error) {
	length, err := readVarInt(r)
	if err != nil {
		return "", fmt.Errorf("%w: string length: %v", ErrMalformed, err)
	}
	if length < 0 || length > limit {
		return "", fmt.Errorf("%w: string length %d exceeds %d", ErrMalformed, length, limit)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", fmt.Errorf("%w: short string: %v", ErrMalformed, err)
	}
	return string(buf), nil
}
