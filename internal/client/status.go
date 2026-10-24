package client

import (
	"sync"
	"time"
)

// Status is a snapshot of the agent state, used by the web interface.
type Status struct {
	DeviceID      string `json:"device_id"`
	RelayURL      string `json:"relay_url"`
	RoomCode      string `json:"room_code"`
	PublicHost    string `json:"public_host"`
	PublicPort    int    `json:"public_port"`
	LocalPort     int    `json:"local_port"`
	ProxyProtocol bool   `json:"proxy_protocol"`
	Connected     bool   `json:"connected"`
	Players       int    `json:"players"`
	// LocalOK reports whether the local Minecraft server answered a TCP probe.
	LocalOK     bool   `json:"local_ok"`
	BytesUp     int64  `json:"bytes_up"`
	BytesDown   int64  `json:"bytes_down"`
	LastError   string `json:"last_error,omitempty"`
	ConnectedAt string `json:"connected_at,omitempty"`
}

type statusStore struct {
	mu     sync.Mutex
	status Status
	now    func() time.Time
}

func newStatusStore() *statusStore {
	return &statusStore{now: time.Now}
}

// snapshot returns a copy of the current status.
func (s *statusStore) snapshot() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// with runs fn while holding the lock.
func (s *statusStore) with(fn func(*Status)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.status)
}

func (s *statusStore) markConnected(relayURL string, roomCode, publicHost string, publicPort int) {
	s.with(func(st *Status) {
		st.RelayURL = relayURL
		st.RoomCode = roomCode
		st.PublicHost = publicHost
		st.PublicPort = publicPort
		st.Connected = true
		st.Players = 0
		st.LastError = ""
		st.ConnectedAt = s.now().UTC().Format(time.RFC3339)
	})
}

func (s *statusStore) markDisconnected(err error) {
	s.with(func(st *Status) {
		st.Connected = false
		st.Players = 0
		if err != nil {
			st.LastError = err.Error()
		}
	})
}

// noteProblem records something the host needs to see but that is not a
// connection failure, such as a local server refusing every player.
func (s *statusStore) noteProblem(message string) {
	s.with(func(st *Status) { st.LastError = message })
}

func (s *statusStore) setLocal(ok bool) {
	s.with(func(st *Status) { st.LocalOK = ok })
}

func (s *statusStore) streamStarted() {
	s.with(func(st *Status) { st.Players++ })
}

func (s *statusStore) streamFinished(player bool, up, down int64) {
	s.with(func(st *Status) {
		if player && st.Players > 0 {
			st.Players--
		}
		st.BytesUp += up
		st.BytesDown += down
	})
}
