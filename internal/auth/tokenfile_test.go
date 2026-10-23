package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sunse666/NoneFrp/internal/config"
)

func TestAppendTokenFileExtendsExistingBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")
	original := `# relay configuration
listen: "127.0.0.1:8080"
domain_suffix: "mc.example.com"
tokens:
  NFRP-AAAA2222: "first host"
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := AppendTokenFile(path, "NFRP-BBBB3333", "second host"); err != nil {
		t.Fatalf("append: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(raw), "# relay configuration") {
		t.Fatal("the leading comment was lost")
	}

	cfg, err := config.LoadServer(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Tokens["NFRP-BBBB3333"] != "second host" {
		t.Fatalf("tokens = %v", cfg.Tokens)
	}
	if cfg.Tokens["NFRP-AAAA2222"] != "first host" {
		t.Fatalf("existing token was lost: %v", cfg.Tokens)
	}
}

func TestAppendTokenFileCreatesBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")
	if err := os.WriteFile(path, []byte("domain_suffix: mc.example.com\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := AppendTokenFile(path, "NFRP-CCCC4444", ""); err != nil {
		t.Fatalf("append: %v", err)
	}

	cfg, err := config.LoadServer(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := cfg.Tokens["NFRP-CCCC4444"]; !ok {
		t.Fatalf("tokens = %v", cfg.Tokens)
	}
}

func TestAppendTokenFileRejectsInvalidToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")
	if err := os.WriteFile(path, []byte("domain_suffix: mc.example.com\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := AppendTokenFile(path, "not-a-token", ""); err == nil {
		t.Fatal("expected an error for an invalid token")
	}
}

func TestAppendTokenFileMissingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.yaml")
	err := AppendTokenFile(path, "NFRP-DDDD5555", "")
	if !errors.Is(err, ErrMissingConfig) {
		t.Fatalf("err = %v, want ErrMissingConfig", err)
	}
}
