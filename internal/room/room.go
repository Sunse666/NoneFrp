// Package room tracks the lifecycle of relay rooms.
//
// A room lives in three states: it is active while its host agent is connected,
// it enters a grace period when the agent drops, and it is closed once that
// period elapses without a reconnect.
package room

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Sunse666/NoneFrp/internal/auth"
)

// State is the lifecycle state of a room.
type State string

// Room states.
const (
	StateActive State = "active"
	StateGrace  State = "grace"
	StateClosed State = "closed"
)

// ErrCodeTaken is returned when a requested room code belongs to another device.
var ErrCodeTaken = errors.New("room code already in use")

// Room is a single relay session.
type Room struct {
	Code       string
	DeviceID   string
	PublicHost string
	PublicPort int
	State      State
	CreatedAt  time.Time
	UpdatedAt  time.Time
	// ExpiresAt is when a room in the grace state is finally closed.
	ExpiresAt time.Time
	// Endpoints are the host's own addresses, usable by a player who can reach
	// them without the relay.
	Endpoints []string
}

// Registry is an in-memory room table. It is safe for concurrent use.
type Registry struct {
	mu      sync.Mutex
	rooms   map[string]*Room
	grace   time.Duration
	now     func() time.Time
	newCode func() (string, error)

	// lookupCode and rememberCode make an auto-assigned code stick to the
	// device that received it, so a reconnecting host keeps its address.
	lookupCode   func(deviceID string) (string, bool)
	rememberCode func(deviceID, code string)
}

// NewRegistry returns a registry that keeps dropped rooms alive for grace.
func NewRegistry(grace time.Duration) *Registry {
	return &Registry{
		rooms:   make(map[string]*Room),
		grace:   grace,
		now:     time.Now,
		newCode: auth.GenerateRoomCode,
	}
}

// Create claims a room for deviceID. An empty or "auto" requested code asks the
// relay to assign one. Recreating a room with the same device and code is
// idempotent so that reconnecting agents keep their public address.
func (r *Registry) Create(deviceID, requestedCode, publicHost string, publicPort int) (Room, error) {
	if deviceID == "" {
		return Room{}, fmt.Errorf("create room: device id must not be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	code := requestedCode
	if code == "" || code == "auto" {
		// Reuse the code this device had before. Without this every reconnect,
		// and every relay restart, would hand out a new address.
		code = r.rememberedCodeLocked(deviceID)
		if code == "" {
			assigned, err := r.assignCodeLocked()
			if err != nil {
				return Room{}, err
			}
			code = assigned
			if r.rememberCode != nil {
				r.rememberCode(deviceID, code)
			}
		}
	} else {
		normalized, err := auth.NormalizeRoomCode(code)
		if err != nil {
			return Room{}, err
		}
		code = normalized
		if existing, ok := r.rooms[code]; ok && existing.DeviceID != deviceID {
			return Room{}, fmt.Errorf("%w: %s", ErrCodeTaken, code)
		}
	}

	now := r.now()
	if existing, ok := r.rooms[code]; ok {
		existing.DeviceID = deviceID
		existing.PublicHost = publicHost
		existing.PublicPort = publicPort
		existing.State = StateActive
		existing.UpdatedAt = now
		existing.ExpiresAt = time.Time{}
		return *existing, nil
	}

	created := &Room{
		Code:       code,
		DeviceID:   deviceID,
		PublicHost: publicHost,
		PublicPort: publicPort,
		State:      StateActive,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	r.rooms[code] = created
	return *created, nil
}

// SetCodeAssigner supplies the storage that keeps auto-assigned codes stable.
// Passing nil callbacks falls back to a fresh code per room.
func (r *Registry) SetCodeAssigner(lookup func(deviceID string) (string, bool), remember func(deviceID, code string)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lookupCode = lookup
	r.rememberCode = remember
}

// rememberedCodeLocked returns the code previously given to deviceID, if it is
// still usable.
func (r *Registry) rememberedCodeLocked(deviceID string) string {
	if r.lookupCode == nil {
		return ""
	}
	known, ok := r.lookupCode(deviceID)
	if !ok || known == "" {
		return ""
	}
	if existing, taken := r.rooms[known]; taken && existing.DeviceID != deviceID {
		return ""
	}
	return known
}

func (r *Registry) assignCodeLocked() (string, error) {
	for range 16 {
		code, err := r.newCode()
		if err != nil {
			return "", err
		}
		if _, taken := r.rooms[code]; !taken {
			return code, nil
		}
	}
	return "", fmt.Errorf("assign room code: no free code found")
}

// Get returns a copy of the room with the given code.
func (r *Registry) Get(code string) (Room, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	found, ok := r.rooms[code]
	if !ok {
		return Room{}, false
	}
	return *found, true
}

// Touch refreshes an active room, e.g. on every heartbeat.
func (r *Registry) Touch(code string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	found, ok := r.rooms[code]
	if !ok {
		return false
	}
	found.UpdatedAt = r.now()
	if found.State == StateGrace {
		found.State = StateActive
		found.ExpiresAt = time.Time{}
	}
	return true
}

// SetPublicHost records the address players should connect to.
func (r *Registry) SetPublicHost(code, host string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if found, ok := r.rooms[code]; ok {
		found.PublicHost = host
	}
}

// MarkGrace starts the grace period for a room whose agent disconnected.
// SetEndpoints records the host's direct addresses, the ones a player may
// reach without the relay.
func (r *Registry) SetEndpoints(code string, endpoints []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	tracked, ok := r.rooms[code]
	if !ok {
		return
	}
	tracked.Endpoints = append([]string(nil), endpoints...)
	tracked.UpdatedAt = r.now()
}

func (r *Registry) MarkGrace(code string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	found, ok := r.rooms[code]
	if !ok {
		return
	}
	now := r.now()
	found.State = StateGrace
	found.UpdatedAt = now
	found.ExpiresAt = now.Add(r.grace)
}

// Remove deletes a room immediately.
func (r *Registry) Remove(code string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rooms, code)
}

// Reap closes rooms whose grace period has elapsed and returns how many were removed.
func (r *Registry) Reap() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	removed := 0
	for code, found := range r.rooms {
		if found.State != StateGrace {
			continue
		}
		if !found.ExpiresAt.IsZero() && found.ExpiresAt.After(now) {
			continue
		}
		delete(r.rooms, code)
		removed++
	}
	return removed
}

// Len reports how many rooms are tracked.
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.rooms)
}

// Snapshot returns a copy of every tracked room.
func (r *Registry) Snapshot() []Room {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Room, 0, len(r.rooms))
	for _, found := range r.rooms {
		out = append(out, *found)
	}
	return out
}
