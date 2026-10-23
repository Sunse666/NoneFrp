package server

import (
	"sync"
	"time"
)

// ipLimiter caps how many player connections a single address may open per
// second. It uses fixed one second windows, which is coarse but enough to stop
// a scanner from burning the relay's bandwidth.
type ipLimiter struct {
	mu     sync.Mutex
	rate   int
	now    func() time.Time
	hits   map[string]*ipWindow
	window time.Duration
}

type ipWindow struct {
	start time.Time
	count int
}

func newIPLimiter(rate int) *ipLimiter {
	return &ipLimiter{
		rate:   rate,
		now:    time.Now,
		hits:   make(map[string]*ipWindow),
		window: time.Second,
	}
}

// allow reports whether the address may open one more connection now.
func (l *ipLimiter) allow(ip string) bool {
	if l.rate <= 0 {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	current, ok := l.hits[ip]
	if !ok || now.Sub(current.start) >= l.window {
		l.hits[ip] = &ipWindow{start: now, count: 1}
		return true
	}
	if current.count >= l.rate {
		return false
	}
	current.count++
	return true
}

// gc drops windows that are older than one interval.
func (l *ipLimiter) gc() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	for ip, current := range l.hits {
		if now.Sub(current.start) >= l.window {
			delete(l.hits, ip)
		}
	}
}
