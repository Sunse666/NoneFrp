package server

import (
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/Sunse666/NoneFrp/internal/room"
)

// stats holds the counters exposed on /metrics. Everything is cheap to update
// from the hot path.
type stats struct {
	players       atomic.Int64
	bytesUp       atomic.Int64
	bytesDown     atomic.Int64
	registrations atomic.Int64
	sessions      atomic.Int64

	mu         sync.Mutex
	rejections map[string]int64
}

func newStats() *stats {
	return &stats{rejections: map[string]int64{}}
}

func (s *stats) playerJoined()  { s.players.Add(1) }
func (s *stats) playerLeft()    { s.players.Add(-1) }
func (s *stats) sessionOpen()   { s.sessions.Add(1) }
func (s *stats) sessionClosed() { s.sessions.Add(-1) }
func (s *stats) registered()    { s.registrations.Add(1) }

func (s *stats) transferred(up, down int64) {
	s.bytesUp.Add(up)
	s.bytesDown.Add(down)
}

func (s *stats) rejected(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rejections[reason]++
}

// write renders the Prometheus text format. Rooms come from the registry so the
// numbers cannot drift from what the relay actually routes.
func (s *stats) write(w io.Writer, rooms []room.Room) {
	active, grace := 0, 0
	for _, tracked := range rooms {
		switch tracked.State {
		case room.StateActive:
			active++
		case room.StateGrace:
			grace++
		}
	}

	fmt.Fprintf(w, "# HELP nonefrp_rooms Rooms by state.\n# TYPE nonefrp_rooms gauge\n")
	fmt.Fprintf(w, "nonefrp_rooms{state=\"active\"} %d\n", active)
	fmt.Fprintf(w, "nonefrp_rooms{state=\"grace\"} %d\n", grace)

	fmt.Fprintf(w, "# HELP nonefrp_players Player streams open right now.\n# TYPE nonefrp_players gauge\n")
	fmt.Fprintf(w, "nonefrp_players %d\n", s.players.Load())

	fmt.Fprintf(w, "# HELP nonefrp_agent_sessions Data sessions open right now.\n# TYPE nonefrp_agent_sessions gauge\n")
	fmt.Fprintf(w, "nonefrp_agent_sessions %d\n", s.sessions.Load())

	fmt.Fprintf(w, "# HELP nonefrp_registrations_total Registrations accepted since start.\n# TYPE nonefrp_registrations_total counter\n")
	fmt.Fprintf(w, "nonefrp_registrations_total %d\n", s.registrations.Load())

	fmt.Fprintf(w, "# HELP nonefrp_bytes_total Bytes forwarded, by direction.\n# TYPE nonefrp_bytes_total counter\n")
	fmt.Fprintf(w, "nonefrp_bytes_total{direction=\"up\"} %d\n", s.bytesUp.Load())
	fmt.Fprintf(w, "nonefrp_bytes_total{direction=\"down\"} %d\n", s.bytesDown.Load())

	fmt.Fprintf(w, "# HELP nonefrp_rejections_total Players refused, by reason.\n# TYPE nonefrp_rejections_total counter\n")
	s.mu.Lock()
	reasons := make([]string, 0, len(s.rejections))
	for reason := range s.rejections {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	for _, reason := range reasons {
		fmt.Fprintf(w, "nonefrp_rejections_total{reason=\"%s\"} %d\n", reason, s.rejections[reason])
	}
	s.mu.Unlock()
}
