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

// A handshake whose length prefix starts with 0xFE must still parse: 0xFE is
// an ordinary continuation byte for body lengths of 126 and above.
func TestReadHandshakeAcceptsLongAddress(t *testing.T) {
	// Find the address length whose body encodes to a length prefix starting
	// with 0xFE - the byte that used to be mistaken for a legacy ping.
	const suffix = ".mc.example.com"
	var (
		address string
		packet  []byte
	)
	for n := 200; n <= MaxServerAddress; n++ {
		address = strings.Repeat("a", n-len(suffix)) + suffix
		packet = buildHandshake(767, address, 25565, NextStateStatus)
		if packet[0] == 0xfe {
			break
		}
	}
	if packet[0] != 0xfe {
		t.Skip("no address length produced a 0xFE length prefix")
	}

	if len(packet) < 127 {
		t.Fatalf("test setup: packet is only %d bytes, length prefix does not start with 0xFE", len(packet))
	}
	if packet[0] != 0xfe {
		t.Fatalf("test setup: first byte = %#x, want 0xfe", packet[0])
	}

	got, err := ReadHandshake(bytes.NewReader(packet))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.ServerAddress != address {
		t.Fatalf("address = %q", got.ServerAddress)
	}
}

func TestReadHandshakeEmptyStream(t *testing.T) {
	if _, err := ReadHandshake(bytes.NewReader(nil)); err == nil {
		t.Fatal("expected an error for an empty stream")
	}
}
