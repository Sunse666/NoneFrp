package mcproto

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func encodeVarInt(v int) []byte {
	var out []byte
	u := uint32(v)
	for {
		b := byte(u & 0x7f)
		u >>= 7
		if u != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if u == 0 {
			return out
		}
	}
}

func encodeString(s string) []byte {
	return append(encodeVarInt(len(s)), []byte(s)...)
}

func buildHandshake(version int, address string, port uint16, nextState int) []byte {
	var body []byte
	body = append(body, encodeVarInt(handshakePacketID)...)
	body = append(body, encodeVarInt(version)...)
	body = append(body, encodeString(address)...)
	body = append(body, byte(port>>8), byte(port))
	body = append(body, encodeVarInt(nextState)...)

	out := encodeVarInt(len(body))
	return append(out, body...)
}

func TestReadHandshakeStatus(t *testing.T) {
	packet := buildHandshake(767, "a2b3c4.mc.example.com", 25565, NextStateStatus)

	got, err := ReadHandshake(bytes.NewReader(packet))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.ProtocolVersion != 767 {
		t.Fatalf("version = %d", got.ProtocolVersion)
	}
	if got.ServerAddress != "a2b3c4.mc.example.com" {
		t.Fatalf("address = %q", got.ServerAddress)
	}
	if got.ServerPort != 25565 {
		t.Fatalf("port = %d", got.ServerPort)
	}
	if got.NextState != NextStateStatus {
		t.Fatalf("next state = %d", got.NextState)
	}
	if !bytes.Equal(got.Raw, packet) {
		t.Fatalf("raw bytes = %x, want %x", got.Raw, packet)
	}
}

func TestReadHandshakeLogin(t *testing.T) {
	packet := buildHandshake(767, "mc.example.com", 25565, NextStateLogin)
	got, err := ReadHandshake(bytes.NewReader(packet))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.NextState != NextStateLogin {
		t.Fatalf("next state = %d", got.NextState)
	}
}

func TestReadHandshakeLeavesRestOfStream(t *testing.T) {
	packet := buildHandshake(767, "mc.example.com", 25565, NextStateStatus)
	tail := []byte{0x01, 0x00, 0x02, 0x03}
	stream := append(append([]byte{}, packet...), tail...)

	reader := bytes.NewReader(stream)
	got, err := ReadHandshake(reader)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	rest := make([]byte, len(tail))
	if _, err := reader.Read(rest); err != nil {
		t.Fatalf("read rest: %v", err)
	}
	if !bytes.Equal(rest, tail) {
		t.Fatalf("rest = %x, want %x", rest, tail)
	}
	if !bytes.Contains(got.Raw, []byte("mc.example.com")) {
		t.Fatal("raw bytes do not contain the address")
	}
}

func TestReadHandshakeRejectsWrongPacketID(t *testing.T) {
	body := append(encodeVarInt(0x01), encodeVarInt(767)...)
	packet := append(encodeVarInt(len(body)), body...)

	if _, err := ReadHandshake(bytes.NewReader(packet)); !errors.Is(err, ErrMalformed) {
		t.Fatalf("err = %v, want ErrMalformed", err)
	}
}

func TestReadHandshakeRejectsOversizedPacket(t *testing.T) {
	packet := encodeVarInt(MaxHandshakeSize + 1)
	if _, err := ReadHandshake(bytes.NewReader(packet)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestReadHandshakeRejectsLongAddress(t *testing.T) {
	packet := buildHandshake(767, strings.Repeat("a", MaxServerAddress+1), 25565, NextStateStatus)
	if _, err := ReadHandshake(bytes.NewReader(packet)); !errors.Is(err, ErrMalformed) {
		t.Fatalf("err = %v, want ErrMalformed", err)
	}
}

func TestReadHandshakeRejectsTruncatedPacket(t *testing.T) {
	packet := buildHandshake(767, "mc.example.com", 25565, NextStateStatus)
	if _, err := ReadHandshake(bytes.NewReader(packet[:len(packet)-3])); err == nil {
		t.Fatal("expected an error for a truncated packet")
	}
}

func TestReadHandshakeRejectsBadNextState(t *testing.T) {
	packet := buildHandshake(767, "mc.example.com", 25565, 3)
	if _, err := ReadHandshake(bytes.NewReader(packet)); !errors.Is(err, ErrMalformed) {
		t.Fatalf("err = %v, want ErrMalformed", err)
	}
}

func TestReadHandshakeDetectsLegacyPing(t *testing.T) {
	if _, err := ReadHandshake(bytes.NewReader([]byte{legacyPingByte, 0x01})); !errors.Is(err, ErrLegacyPing) {
		t.Fatalf("err = %v, want ErrLegacyPing", err)
	}
}

func TestReadHandshakeEmptyStream(t *testing.T) {
	if _, err := ReadHandshake(bytes.NewReader(nil)); err == nil {
		t.Fatal("expected an error for an empty stream")
	}
}
