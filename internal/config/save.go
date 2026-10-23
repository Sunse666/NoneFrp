package config

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// SaveHeader is prepended to files written by the program.
const SaveHeader = "# Written by NoneFrp. Comments added by hand are not preserved.\n"

// MarshalDuration renders a Duration the way the loader expects it back.
func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}

// SaveClient writes the agent configuration to path atomically.
func SaveClient(path string, cfg Client) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	var buf bytes.Buffer
	buf.WriteString(SaveHeader)
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("encode client config: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("encode client config: %w", err)
	}

	return writeFileAtomic(path, buf.Bytes())
}

// SaveServer writes the relay configuration to path atomically.
func SaveServer(path string, cfg Server) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	var buf bytes.Buffer
	buf.WriteString(SaveHeader)
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("encode server config: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("encode server config: %w", err)
	}

	return writeFileAtomic(path, buf.Bytes())
}

// writeFileAtomic replaces path without leaving a truncated file behind.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
