package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

const tokensKey = "tokens:"

// ErrMissingConfig is returned when the server configuration does not exist yet.
var ErrMissingConfig = errors.New("server configuration not found")

// AppendTokenFile adds token to the token table of a server configuration file.
//
// The file is edited textually so that comments and formatting survive. An
// existing "tokens:" block is extended; otherwise a new block is appended.
func AppendTokenFile(path, token, note string) error {
	if !ValidToken(token) {
		return fmt.Errorf("invalid token %q", token)
	}
	quoted, err := json.Marshal(note)
	if err != nil {
		return fmt.Errorf("quote note: %w", err)
	}
	entry := "  " + token + ": " + string(quoted)

	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrMissingConfig, path)
		}
		return fmt.Errorf("read %s: %w", path, err)
	}

	var lines []string
	if len(strings.TrimSpace(string(raw))) > 0 {
		lines = strings.Split(strings.TrimRight(string(raw), "\r\n"), "\n")
	}

	out := make([]string, 0, len(lines)+2)
	inserted := false
	for _, line := range lines {
		out = append(out, line)
		if !inserted && strings.HasPrefix(line, tokensKey) {
			out = append(out, entry)
			inserted = true
		}
	}
	if !inserted {
		out = append(out, tokensKey, entry)
	}

	content := strings.Join(out, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
