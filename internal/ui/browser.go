package ui

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
)

// OpenBrowser asks the desktop to open url in the default browser.
func OpenBrowser(raw string) error {
	if _, err := url.Parse(raw); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", raw)
	case "darwin":
		cmd = exec.Command("open", raw)
	default:
		cmd = exec.Command("xdg-open", raw)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	// The launcher exits on its own; do not hold a handle to it.
	go func() { _ = cmd.Wait() }()
	return nil
}
