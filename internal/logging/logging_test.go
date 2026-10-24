package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingFileWritesAndRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.log")

	file, err := NewRotatingFile(path, 100, 2)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = file.Close() }()

	line := strings.Repeat("x", 60) + "\n"
	for range 5 {
		if _, err := file.Write([]byte(line)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected rotation, found %d files", len(entries))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() > 100 {
		t.Fatalf("current file is %d bytes, want at most 100", info.Size())
	}
}

func TestNewWritesToStderrAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "client.log")

	logger, closeLog := New(true, path)
	defer closeLog()

	logger.Info("hello from the test", "room", "a2b3c4")

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(raw), "hello from the test") {
		t.Fatalf("log does not contain the message: %q", string(raw))
	}
}

func TestNewSurvivesAnUnusablePath(t *testing.T) {
	// A path whose parent is a file can never be created.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	logger, closeLog := New(false, filepath.Join(blocker, "client.log"))
	defer closeLog()
	logger.Info("still works")
}
