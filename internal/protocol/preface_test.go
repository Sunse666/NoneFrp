package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

func TestPrefaceRoundTrip(t *testing.T) {
	want := StreamMeta{
		ClientIP:   "203.0.113.7",
		ClientPort: 54321,
		Host:       "a2b3c4.mc.example.com",
		NextState:  1,
		RoomCode:   "a2b3c4",
		TraceID:    "0123456789abcdef",
	}

	var buf bytes.Buffer
	if err := WritePreface(&buf, want); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := ReadPreface(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != want {
		t.Fatalf("meta = %+v, want %+v", got, want)
	}
}

func TestPrefaceLeavesPayloadUntouched(t *testing.T) {
	payload := []byte{0x05, 0x00, 0xff, 0x10}

	var buf bytes.Buffer
	if err := WritePreface(&buf, StreamMeta{RoomCode: "a2b3c4"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf.Write(payload)

	if _, err := ReadPreface(&buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), payload) {
		t.Fatalf("remaining = %x, want %x", buf.Bytes(), payload)
	}
}

func TestReadPrefaceRejectsBadMagic(t *testing.T) {
	raw := []byte{0x58, 0x58, 0x58, 0x58, 1, 0, 2, 0x7b, 0x7d}
	if _, err := ReadPreface(bytes.NewReader(raw)); !errors.Is(err, ErrBadPreface) {
		t.Fatalf("err = %v, want ErrBadPreface", err)
	}
}

func TestReadPrefaceRejectsUnknownVersion(t *testing.T) {
	raw := []byte(PrefaceMagic)
	raw = append(raw, 99, 0, 2)
	raw = append(raw, 0x7b, 0x7d)
	if _, err := ReadPreface(bytes.NewReader(raw)); !errors.Is(err, ErrBadPreface) {
		t.Fatalf("err = %v, want ErrBadPreface", err)
	}
}

func TestReadPrefaceRejectsOversizedBody(t *testing.T) {
	raw := []byte(PrefaceMagic)
	raw = append(raw, PrefaceVersion)
	raw = binary.BigEndian.AppendUint16(raw, MaxPrefaceSize+1)
	if _, err := ReadPreface(bytes.NewReader(raw)); !errors.Is(err, ErrBadPreface) {
		t.Fatalf("err = %v, want ErrBadPreface", err)
	}
}

func TestReadPrefaceRejectsTruncatedBody(t *testing.T) {
	var buf bytes.Buffer
	if err := WritePreface(&buf, StreamMeta{Host: strings.Repeat("x", 200)}); err != nil {
		t.Fatalf("write: %v", err)
	}
	full := buf.Bytes()
	if _, err := ReadPreface(bytes.NewReader(full[:len(full)-10])); !errors.Is(err, ErrBadPreface) {
		t.Fatalf("err = %v, want ErrBadPreface", err)
	}
}

func TestReadPrefaceToleratesUnknownFields(t *testing.T) {
	body := []byte(`{"room_code":"a2b3c4","future_field":true}`)
	raw := []byte(PrefaceMagic)
	raw = append(raw, PrefaceVersion)
	raw = binary.BigEndian.AppendUint16(raw, uint16(len(body)))
	raw = append(raw, body...)

	got, err := ReadPreface(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.RoomCode != "a2b3c4" {
		t.Fatalf("room = %q", got.RoomCode)
	}
}
