package mcproto

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Packet ids used while answering a server list ping. The numeric values repeat
// across protocol states, so they are named per state.
const (
	statusRequestID   = 0x00
	statusResponseID  = 0x00
	pingID            = 0x01
	pongID            = 0x01
	loginDisconnectID = 0x00
)

// maxStatusPacket bounds a packet read in the status or login state.
const maxStatusPacket = 32767

// StatusReply is the server list entry shown to a player who cannot be routed.
type StatusReply struct {
	// ProtocolVersion must echo the value from the player handshake, otherwise
	// the client shows a version mismatch instead of the description.
	ProtocolVersion int
	VersionName     string
	Description     string
	Online          int
	Max             int
}

// ServeStatusReply answers a server list ping with a canned entry. It consumes
// the status request and the following ping, and replies to both.
func ServeStatusReply(rw io.ReadWriter, reply StatusReply) error {
	packetID, _, err := ReadPacket(rw, maxStatusPacket)
	if err != nil {
		return fmt.Errorf("read status request: %w", err)
	}
	if packetID != statusRequestID {
		return fmt.Errorf("%w: expected a status request, got packet %d", ErrMalformed, packetID)
	}

	payload, err := reply.marshal()
	if err != nil {
		return err
	}
	if err := WritePacket(rw, statusResponseID, payload); err != nil {
		return fmt.Errorf("write status response: %w", err)
	}

	packetID, pingPayload, err := ReadPacket(rw, maxStatusPacket)
	if err != nil {
		return fmt.Errorf("read ping: %w", err)
	}
	if packetID != pingID {
		return fmt.Errorf("%w: expected a ping, got packet %d", ErrMalformed, packetID)
	}
	// The pong must echo the ping payload byte for byte.
	if err := WritePacket(rw, pongID, pingPayload); err != nil {
		return fmt.Errorf("write pong: %w", err)
	}
	return nil
}

func (r StatusReply) marshal() ([]byte, error) {
	doc := map[string]any{
		"version": map[string]any{
			"name":     r.VersionName,
			"protocol": r.ProtocolVersion,
		},
		"players": map[string]any{
			"online": r.Online,
			"max":    r.Max,
		},
		"description": map[string]string{"text": r.Description},
	}
	payload, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode status response: %w", err)
	}
	return payload, nil
}

// WriteLoginDisconnect sends a disconnect message in the login state, which the
// client renders as the normal red connection screen.
func WriteLoginDisconnect(w io.Writer, message string) error {
	payload, err := json.Marshal(map[string]string{"text": message})
	if err != nil {
		return fmt.Errorf("encode disconnect message: %w", err)
	}
	if err := WritePacket(w, loginDisconnectID, payload); err != nil {
		return fmt.Errorf("write disconnect: %w", err)
	}
	return nil
}

// ReadPacket reads one length prefixed packet and returns its id and payload.
func ReadPacket(r io.Reader, limit int) (int, []byte, error) {
	var raw []byte
	length, err := readVarIntBytes(r, &raw)
	if err != nil {
		return 0, nil, err
	}
	if length < 1 || length > limit {
		return 0, nil, fmt.Errorf("%w: packet length %d", ErrMalformed, length)
	}

	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, nil, fmt.Errorf("%w: short packet: %v", ErrMalformed, err)
	}

	reader := bytes.NewReader(body)
	packetID, err := readVarInt(reader)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: packet id: %v", ErrMalformed, err)
	}
	payload, err := io.ReadAll(reader)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: payload: %v", ErrMalformed, err)
	}
	return packetID, payload, nil
}

// WritePacket writes one length prefixed packet.
func WritePacket(w io.Writer, packetID int, payload []byte) error {
	body := appendVarInt(nil, packetID)
	body = append(body, payload...)

	framed := appendVarInt(nil, len(body))
	framed = append(framed, body...)
	if _, err := w.Write(framed); err != nil {
		return fmt.Errorf("write packet %d: %w", packetID, err)
	}
	return nil
}

func appendVarInt(dst []byte, value int) []byte {
	u := uint32(value)
	for {
		b := byte(u & 0x7f)
		u >>= 7
		if u != 0 {
			b |= 0x80
		}
		dst = append(dst, b)
		if u == 0 {
			return dst
		}
	}
}
