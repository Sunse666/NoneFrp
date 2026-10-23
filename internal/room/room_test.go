package room

import (
	"errors"
	"testing"
	"time"
)

func newTestRegistry(t *testing.T) (*Registry, *time.Time) {
	t.Helper()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	reg := NewRegistry(60 * time.Second)
	reg.now = func() time.Time { return now }
	seq := 0
	reg.newCode = func() (string, error) {
		seq++
		return []string{"aa0001", "aa0002", "aa0003"}[seq%3], nil
	}
	return reg, &now
}

func TestCreateAssignsCode(t *testing.T) {
	reg, _ := newTestRegistry(t)

	got, err := reg.Create("dev-1", "", "mc.example.com", 25565)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got.Code == "" {
		t.Fatal("expected an assigned room code")
	}
	if got.State != StateActive {
		t.Fatalf("state = %q", got.State)
	}
	if got.PublicHost != "mc.example.com" || got.PublicPort != 25565 {
		t.Fatalf("public endpoint = %s:%d", got.PublicHost, got.PublicPort)
	}
	if reg.Len() != 1 {
		t.Fatalf("len = %d", reg.Len())
	}
}

func TestCreateRequestedCode(t *testing.T) {
	reg, _ := newTestRegistry(t)

	got, err := reg.Create("dev-1", "AB23CD", "mc.example.com", 25565)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got.Code != "ab23cd" {
		t.Fatalf("code = %q", got.Code)
	}
}

func TestCreateIsIdempotentForSameDevice(t *testing.T) {
	reg, _ := newTestRegistry(t)

	first, err := reg.Create("dev-1", "ab23cd", "mc.example.com", 25565)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	second, err := reg.Create("dev-1", "ab23cd", "mc.example.com", 25565)
	if err != nil {
		t.Fatalf("recreate: %v", err)
	}
	if first.Code != second.Code {
		t.Fatalf("code changed from %q to %q", first.Code, second.Code)
	}
	if reg.Len() != 1 {
		t.Fatalf("len = %d", reg.Len())
	}
}

func TestCreateRejectsCodeOfAnotherDevice(t *testing.T) {
	reg, _ := newTestRegistry(t)

	if _, err := reg.Create("dev-1", "ab23cd", "mc.example.com", 25565); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err := reg.Create("dev-2", "ab23cd", "mc.example.com", 25565)
	if !errors.Is(err, ErrCodeTaken) {
		t.Fatalf("err = %v, want ErrCodeTaken", err)
	}
}

func TestMarkGraceThenReap(t *testing.T) {
	reg, now := newTestRegistry(t)

	if _, err := reg.Create("dev-1", "ab23cd", "mc.example.com", 25565); err != nil {
		t.Fatalf("create: %v", err)
	}
	reg.MarkGrace("ab23cd")

	got, ok := reg.Get("ab23cd")
	if !ok || got.State != StateGrace {
		t.Fatalf("room = %+v, ok = %v", got, ok)
	}

	if removed := reg.Reap(); removed != 0 {
		t.Fatalf("reaped %d rooms before the grace period elapsed", removed)
	}

	*now = now.Add(61 * time.Second)
	if removed := reg.Reap(); removed != 1 {
		t.Fatalf("reaped %d rooms, want 1", removed)
	}
	if _, ok := reg.Get("ab23cd"); ok {
		t.Fatal("room should be gone after the grace period")
	}
}

func TestReconnectWithinGraceKeepsCode(t *testing.T) {
	reg, _ := newTestRegistry(t)

	if _, err := reg.Create("dev-1", "ab23cd", "mc.example.com", 25565); err != nil {
		t.Fatalf("create: %v", err)
	}
	reg.MarkGrace("ab23cd")

	back, err := reg.Create("dev-1", "ab23cd", "mc.example.com", 25565)
	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if back.State != StateActive {
		t.Fatalf("state = %q", back.State)
	}
	if !back.ExpiresAt.IsZero() {
		t.Fatalf("expires at = %v, want zero", back.ExpiresAt)
	}
}

func TestTouchRevivesGraceRoom(t *testing.T) {
	reg, _ := newTestRegistry(t)

	if _, err := reg.Create("dev-1", "ab23cd", "mc.example.com", 25565); err != nil {
		t.Fatalf("create: %v", err)
	}
	reg.MarkGrace("ab23cd")
	if !reg.Touch("ab23cd") {
		t.Fatal("touch reported a missing room")
	}
	got, _ := reg.Get("ab23cd")
	if got.State != StateActive {
		t.Fatalf("state = %q", got.State)
	}
}

func TestRemove(t *testing.T) {
	reg, _ := newTestRegistry(t)

	if _, err := reg.Create("dev-1", "ab23cd", "mc.example.com", 25565); err != nil {
		t.Fatalf("create: %v", err)
	}
	reg.Remove("ab23cd")
	if reg.Len() != 0 {
		t.Fatalf("len = %d", reg.Len())
	}
}

func TestCreateRejectsEmptyDevice(t *testing.T) {
	reg, _ := newTestRegistry(t)
	if _, err := reg.Create("", "ab23cd", "mc.example.com", 25565); err == nil {
		t.Fatal("expected an error for an empty device id")
	}
}
