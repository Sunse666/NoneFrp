package server

import (
	"testing"
	"time"
)

func TestIPLimiterAllowsUpToRate(t *testing.T) {
	now := time.Unix(0, 0)
	limiter := newIPLimiter(3)
	limiter.now = func() time.Time { return now }

	for i := range 3 {
		if !limiter.allow("203.0.113.7") {
			t.Fatalf("connection %d was rejected", i+1)
		}
	}
	if limiter.allow("203.0.113.7") {
		t.Fatal("the fourth connection in the same second should be rejected")
	}
	if !limiter.allow("203.0.113.8") {
		t.Fatal("a different address must not be affected")
	}
}

func TestIPLimiterRefillsNextSecond(t *testing.T) {
	now := time.Unix(0, 0)
	limiter := newIPLimiter(1)
	limiter.now = func() time.Time { return now }

	if !limiter.allow("203.0.113.7") {
		t.Fatal("first connection rejected")
	}
	if limiter.allow("203.0.113.7") {
		t.Fatal("second connection in the same second should be rejected")
	}

	now = now.Add(time.Second)
	if !limiter.allow("203.0.113.7") {
		t.Fatal("the window should have refilled")
	}
}

func TestIPLimiterDisabled(t *testing.T) {
	limiter := newIPLimiter(0)
	for range 100 {
		if !limiter.allow("203.0.113.7") {
			t.Fatal("a zero rate must disable the limiter")
		}
	}
}

func TestIPLimiterGC(t *testing.T) {
	now := time.Unix(0, 0)
	limiter := newIPLimiter(5)
	limiter.now = func() time.Time { return now }

	limiter.allow("203.0.113.7")
	now = now.Add(2 * time.Second)
	limiter.gc()

	if len(limiter.hits) != 0 {
		t.Fatalf("windows = %d, want 0", len(limiter.hits))
	}
}
