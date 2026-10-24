// Package protocol defines the control-plane wire format shared by the relay
// server and the host agent.
//
// Every control message is a JSON envelope with a type tag. Unknown message
// types and unknown fields are tolerated so that either side can be upgraded
// independently.
package protocol

import (
	"encoding/json"
	"fmt"
)

// Version is the control protocol version carried in every envelope.
const Version = 1

// Type identifies the kind of a control message.
type Type string

// Control message types.
const (
	TypeRegister   Type = "register"
	TypeRegistered Type = "registered"
	TypePeerJoined Type = "peer_joined"
	TypePeerLeft   Type = "peer_left"
	TypeProbe      Type = "probe"
	TypeProbeOK    Type = "probe_ok"
	TypeUnregister Type = "unregister"
	TypeError      Type = "error"
)

// Envelope is the outer frame of every control message.
type Envelope struct {
	V    int             `json:"v"`
	Type Type            `json:"type"`
	Body json.RawMessage `json:"body,omitempty"`
}

// Encode wraps payload into an envelope of the given type.
func Encode(t Type, payload any) ([]byte, error) {
	env := Envelope{V: Version, Type: t}
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode %s body: %w", t, err)
		}
		env.Body = body
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("encode %s envelope: %w", t, err)
	}
	return raw, nil
}

// Decode parses an envelope without inspecting its payload.
func Decode(raw []byte) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return env, fmt.Errorf("decode envelope: %w", err)
	}
	if env.Type == "" {
		return env, fmt.Errorf("decode envelope: missing type")
	}
	return env, nil
}

// As decodes the payload of env into T. An empty payload yields the zero value.
func As[T any](env Envelope) (T, error) {
	var out T
	if len(env.Body) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(env.Body, &out); err != nil {
		return out, fmt.Errorf("decode %s body: %w", env.Type, err)
	}
	return out, nil
}

// ErrorCode is a machine readable failure reason.
type ErrorCode string

// Error codes returned by the relay.
const (
	ErrBadRequest      ErrorCode = "bad_request"
	ErrUnauthorized    ErrorCode = "unauthorized"
	ErrUpgradeRequired ErrorCode = "upgrade_required"
	ErrRoomConflict    ErrorCode = "room_conflict"
	ErrInviteExpired   ErrorCode = "invite_expired"
	ErrInviteExhausted ErrorCode = "invite_exhausted"
	ErrRoomNotFound    ErrorCode = "room_not_found"
	ErrRateLimited     ErrorCode = "rate_limited"
	ErrInternal        ErrorCode = "internal"
)

// Register is sent by an agent to claim a room.
type Register struct {
	DeviceID string `json:"device_id"`
	RoomCode string `json:"room_code,omitempty"`
	// Endpoints are addresses a player could dial to reach the local server
	// without the relay, e.g. "[2409::1]:25565".
	Endpoints []string `json:"endpoints,omitempty"`
	LocalPort int      `json:"local_port"`
}

// Registered confirms room creation and tells the agent what to show players.
type Registered struct {
	RoomCode   string `json:"room_code"`
	PublicHost string `json:"public_host"`
	PublicPort int    `json:"public_port"`
	ExpiresAt  string `json:"expires_at"`
}

// PeerJoined notifies the agent that a player is connecting. It is advisory and
// only used for display.
type PeerJoined struct {
	PlayerIP string `json:"player_ip"`
	At       string `json:"at"`
}

// PeerLeft notifies the agent that a player connection finished.
type PeerLeft struct {
	PlayerIP string `json:"player_ip"`
	At       string `json:"at"`
}

// Probe asks the relay whether a credential would be accepted. It must not
// consume an invite, so the interface can offer a "test connection" button.
type Probe struct {
	DeviceID string `json:"device_id"`
}

// ProbeOK answers a successful probe.
type ProbeOK struct {
	RoomDomain string `json:"room_domain"`
}

// Unregister asks the relay to release the room immediately.
type Unregister struct {
	Reason string `json:"reason,omitempty"`
}

// ErrorBody carries a failure back to the peer.
type ErrorBody struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}
