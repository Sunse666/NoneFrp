package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// usageState is what lands in state.json: which devices already used which
// credential. It lives next to the configuration rather than inside it, because
// server.yaml is edited by hand and rewritten by the CLI.
type usageState struct {
	Version int                 `json:"version"`
	Used    map[string][]string `json:"used"`
}

// usageStore remembers the devices that registered with each credential.
type usageStore struct {
	mu    sync.Mutex
	path  string
	state usageState
}

func newUsageStore(path string) *usageStore {
	store := &usageStore{path: path, state: usageState{Version: 1, Used: map[string][]string{}}}
	if path != "" {
		store.load()
	}
	return store
}

func (u *usageStore) load() {
	raw, err := os.ReadFile(u.path)
	if err != nil {
		if !os.IsNotExist(err) {
			// A damaged state file must not stop the relay.
			fmt.Fprintf(os.Stderr, "warning: cannot read %s: %v\n", u.path, err)
		}
		return
	}

	var loaded usageState
	if err := json.Unmarshal(raw, &loaded); err != nil {
		fmt.Fprintf(os.Stderr, "warning: ignoring damaged %s: %v\n", u.path, err)
		return
	}
	if loaded.Used != nil {
		u.state.Used = loaded.Used
	}
}

// devices returns the devices that registered with code.
func (u *usageStore) devices(code string) []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.state.Used[code]...)
}

// remember records a device for code and persists the change. Recording the
// same device twice is a no-op.
func (u *usageStore) remember(code, deviceID string) error {
	if deviceID == "" {
		return nil
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	for _, known := range u.state.Used[code] {
		if known == deviceID {
			return nil
		}
	}
	u.state.Used[code] = append(u.state.Used[code], deviceID)
	return u.persistLocked()
}

// count reports how many devices used code.
func (u *usageStore) count(code string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.state.Used[code])
}

func (u *usageStore) persistLocked() error {
	if u.path == "" {
		return nil
	}

	encoded, err := json.MarshalIndent(u.state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode usage state: %w", err)
	}
	encoded = append(encoded, '\n')

	temp := u.path + ".tmp"
	if err := os.WriteFile(temp, encoded, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", temp, err)
	}
	if err := os.Rename(temp, u.path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("replace %s: %w", u.path, err)
	}
	return nil
}

// LoadUsage reports how many devices used each credential. A missing or damaged
// file yields an empty map: a listing is not worth failing over.
func LoadUsage(path string) map[string]int {
	counts := map[string]int{}

	raw, err := os.ReadFile(path)
	if err != nil {
		return counts
	}
	var state usageState
	if err := json.Unmarshal(raw, &state); err != nil {
		return counts
	}
	for code, devices := range state.Used {
		counts[code] = len(devices)
	}
	return counts
}

// stateFilePath returns the state file next to a configuration file.
func stateFilePath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "state.json")
}
