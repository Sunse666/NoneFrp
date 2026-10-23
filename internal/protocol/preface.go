package protocol

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Preface framing constants.
const (
	// PrefaceMagic identifies the metadata block at the head of every data stream.
	PrefaceMagic = "NFRP"
	// PrefaceVersion is the current preface format version.
	PrefaceVersion = 1
	// MaxPrefaceSize caps the JSON body a peer may send.
	MaxPrefaceSize = 4096
	// prefaceHeaderSize is magic + version + uint16 length.
	prefaceHeaderSize = len(PrefaceMagic) + 1 + 2
)

// ErrBadPreface is returned when a stream does not start with a valid preface.
var ErrBadPreface = errors.New("bad stream preface")

// StreamMeta describes a single player connection to the agent.
type StreamMeta struct {
	ClientIP   string `json:"client_ip"`
	ClientPort int    `json:"client_port"`
	Host       string `json:"requested_host"`
	NextState  int    `json:"next_state"`
	RoomCode   string `json:"room_code"`
	TraceID    string `json:"trace_id"`
}

// WritePreface writes the metadata block that introduces a data stream.
func WritePreface(w io.Writer, meta StreamMeta) error {
	body, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("encode preface: %w", err)
	}
	if len(body) > MaxPrefaceSize {
		return fmt.Errorf("%w: preface is %d bytes", ErrBadPreface, len(body))
	}

	header := make([]byte, 0, prefaceHeaderSize)
	header = append(header, PrefaceMagic...)
	header = append(header, PrefaceVersion)
	header = binary.BigEndian.AppendUint16(header, uint16(len(body)))

	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("write preface header: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("write preface body: %w", err)
	}
	return nil
}

// ReadPreface reads the metadata block that introduces a data stream.
func ReadPreface(r io.Reader) (StreamMeta, error) {
	var meta StreamMeta

	header := make([]byte, prefaceHeaderSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return meta, fmt.Errorf("%w: short header: %v", ErrBadPreface, err)
	}
	if string(header[:len(PrefaceMagic)]) != PrefaceMagic {
		return meta, fmt.Errorf("%w: magic %q", ErrBadPreface, header[:len(PrefaceMagic)])
	}
	version := header[len(PrefaceMagic)]
	if version != PrefaceVersion {
		// An unknown version is fatal: the caller must not guess the layout.
		return meta, fmt.Errorf("%w: version %d", ErrBadPreface, version)
	}
	length := binary.BigEndian.Uint16(header[len(PrefaceMagic)+1:])
	if length > MaxPrefaceSize {
		return meta, fmt.Errorf("%w: body length %d", ErrBadPreface, length)
	}

	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return meta, fmt.Errorf("%w: short body: %v", ErrBadPreface, err)
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		// Unknown fields are tolerated; malformed JSON is not.
		if !bytes.Contains(body, []byte("{")) {
			return meta, fmt.Errorf("%w: body: %v", ErrBadPreface, err)
		}
		return meta, fmt.Errorf("%w: body: %v", ErrBadPreface, err)
	}
	return meta, nil
}
