// Package logging builds the logger used by the agent: stderr for the console
// that a double click opens, plus a rotating file so a report can be diagnosed
// after the window is gone.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// Defaults for the rotating file.
const (
	DefaultMaxBytes = 10 << 20 // 10 MiB
	DefaultKeep     = 3
)

// New returns a logger writing JSON-free text to stderr and, when filePath is
// usable, to a rotating file. The returned function closes the file.
func New(debug bool, filePath string) (*slog.Logger, func()) {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}

	writers := []io.Writer{os.Stderr}
	closeFile := func() {}

	if filePath != "" {
		file, err := NewRotatingFile(filePath, DefaultMaxBytes, DefaultKeep)
		if err != nil {
			// Losing the log file must not stop the agent.
			fmt.Fprintf(os.Stderr, "warning: cannot write %s: %v\n", filePath, err)
		} else {
			writers = append(writers, file)
			closeFile = func() { _ = file.Close() }
		}
	}

	handler := slog.NewTextHandler(io.MultiWriter(writers...), &slog.HandlerOptions{Level: level})
	return slog.New(handler), closeFile
}

// RotatingFile is an io.Writer that keeps at most keep old copies of a file and
// starts a new one once it grows past maxBytes.
type RotatingFile struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	keep     int

	file *os.File
	size int64
}

// NewRotatingFile opens path, creating its directory if needed.
func NewRotatingFile(path string, maxBytes int64, keep int) (*RotatingFile, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if keep < 1 {
		keep = DefaultKeep
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("stat log file: %w", err)
	}

	return &RotatingFile{path: path, maxBytes: maxBytes, keep: keep, file: file, size: info.Size()}, nil
}

// Write implements io.Writer.
func (f *RotatingFile) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.file == nil {
		return 0, os.ErrClosed
	}
	if f.size+int64(len(p)) > f.maxBytes {
		if err := f.rotateLocked(); err != nil {
			return 0, err
		}
	}

	n, err := f.file.Write(p)
	f.size += int64(n)
	return n, err
}

// Close implements io.Closer.
func (f *RotatingFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.file == nil {
		return nil
	}
	err := f.file.Close()
	f.file = nil
	return err
}

func (f *RotatingFile) rotateLocked() error {
	if err := f.file.Close(); err != nil {
		return err
	}

	// path.2 -> path.3, path.1 -> path.2, path -> path.1
	for index := f.keep - 1; index >= 1; index-- {
		older := fmt.Sprintf("%s.%d", f.path, index)
		newer := fmt.Sprintf("%s.%d", f.path, index+1)
		_ = os.Remove(newer)
		_ = os.Rename(older, newer)
	}
	_ = os.Remove(f.path + ".1")
	if err := os.Rename(f.path, f.path+".1"); err != nil && !os.IsNotExist(err) {
		return err
	}

	file, err := os.OpenFile(f.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	f.file = file
	f.size = 0
	return nil
}
