package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUsageSurvivesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	store := newUsageStore(path)
	if err := store.remember("JOIN-AAAA2222", "device-a"); err != nil {
		t.Fatalf("remember: %v", err)
	}
	if got := store.devices("JOIN-AAAA2222"); len(got) != 1 || got[0] != "device-a" {
		t.Fatalf("devices = %v", got)
	}

	// A new store on the same file must see the record.
	reloaded := newUsageStore(path)
	if got := reloaded.count("JOIN-AAAA2222"); got != 1 {
		t.Fatalf("after reload: %d devices", got)
	}
}

func TestUsageRemembersADeviceOnce(t *testing.T) {
	store := newUsageStore("")

	for range 5 {
		if err := store.remember("JOIN-AAAA2222", "device-a"); err != nil {
			t.Fatalf("remember: %v", err)
		}
	}
	if got := store.count("JOIN-AAAA2222"); got != 1 {
		t.Fatalf("devices = %d, want 1", got)
	}
}

func TestUsageIgnoresADamagedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	store := newUsageStore(path)
	if got := store.count("JOIN-AAAA2222"); got != 0 {
		t.Fatalf("devices = %d", got)
	}
	if err := store.remember("JOIN-AAAA2222", "device-a"); err != nil {
		t.Fatalf("remember: %v", err)
	}
	if got := newUsageStore(path).count("JOIN-AAAA2222"); got != 1 {
		t.Fatalf("a damaged file was not rewritten: %d", got)
	}
}
