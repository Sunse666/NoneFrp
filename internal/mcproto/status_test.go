package mcproto

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestServeStatusReply(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = client.Close() }()

	reply := StatusReply{
		ProtocolVersion: 767,
		VersionName:     "NoneFrp",
		Description:     "room not found",
		Online:          0,
		Max:             0,
	}

	done := make(chan error, 1)
	go func() {
		defer func() { _ = server.Close() }()
		done <- ServeStatusReply(server, reply)
	}()

	_ = client.SetDeadline(time.Now().Add(3 * time.Second))

	// 1. status request
	if err := WritePacket(client, statusRequestID, nil); err != nil {
		t.Fatalf("write status request: %v", err)
	}

	// 2. status response
	packetID, payload, err := ReadPacket(client, maxStatusPacket)
	if err != nil {
		t.Fatalf("read status response: %v", err)
	}
	if packetID != statusResponseID {
		t.Fatalf("packet id = %d", packetID)
	}

	var doc struct {
		Version struct {
			Name     string `json:"name"`
			Protocol int    `json:"protocol"`
		} `json:"version"`
		Players struct {
			Online int `json:"online"`
			Max    int `json:"max"`
		} `json:"players"`
		Description struct {
			Text string `json:"text"`
		} `json:"description"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil {
		t.Fatalf("status json: %v", err)
	}
	if doc.Version.Protocol != 767 {
		t.Fatalf("protocol = %d, want the value echoed back", doc.Version.Protocol)
	}
	if doc.Version.Name != "NoneFrp" {
		t.Fatalf("name = %q", doc.Version.Name)
	}
	if doc.Description.Text != "room not found" {
		t.Fatalf("description = %q", doc.Description.Text)
	}

	// 3. ping, 4. pong echo
	pingPayload := make([]byte, 8)
	binary.BigEndian.PutUint64(pingPayload, 1234567890)
	if err := WritePacket(client, pingID, pingPayload); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	packetID, pongPayload, err := ReadPacket(client, maxStatusPacket)
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if packetID != pongID {
		t.Fatalf("packet id = %d, want pong", packetID)
	}
	if binary.BigEndian.Uint64(pongPayload) != 1234567890 {
		t.Fatalf("pong payload = %x", pongPayload)
	}

	if err := <-done; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

func TestServeStatusReplyRejectsWrongPacket(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = client.Close() }()

	done := make(chan error, 1)
	go func() { done <- ServeStatusReply(server, StatusReply{}) }()

	if err := WritePacket(client, pingID, make([]byte, 8)); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case err := <-done:
		if !errors.Is(err, ErrMalformed) {
			t.Fatalf("err = %v, want ErrMalformed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not return")
	}
}

func TestWriteLoginDisconnect(t *testing.T) {
	var buf bytesBuffer
	if err := WriteLoginDisconnect(&buf, "room is full"); err != nil {
		t.Fatalf("write: %v", err)
	}

	packetID, payload, err := ReadPacket(&buf, maxStatusPacket)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if packetID != loginDisconnectID {
		t.Fatalf("packet id = %d", packetID)
	}

	var doc struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil {
		t.Fatalf("disconnect json: %v", err)
	}
	if doc.Text != "room is full" {
		t.Fatalf("text = %q", doc.Text)
	}
}

func TestReadPacketRejectsOversized(t *testing.T) {
	var buf bytesBuffer
	buf.Write(appendVarInt(nil, maxStatusPacket+1))
	if _, _, err := ReadPacket(&buf, maxStatusPacket); !errors.Is(err, ErrMalformed) {
		t.Fatalf("err = %v, want ErrMalformed", err)
	}
}

func TestReadPacketRejectsTruncated(t *testing.T) {
	var buf bytesBuffer
	buf.Write(appendVarInt(nil, 10))
	buf.Write([]byte{0x00, 0x01})
	_, _, err := ReadPacket(&buf, maxStatusPacket)
	if err == nil {
		t.Fatal("expected an error for a truncated packet")
	}
}

// bytesBuffer is a minimal io.ReadWriter used to keep the tests dependency free.
type bytesBuffer struct {
	data []byte
	off  int
}

func (b *bytesBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *bytesBuffer) Read(p []byte) (int, error) {
	if b.off >= len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.off:])
	b.off += n
	return n, nil
}
