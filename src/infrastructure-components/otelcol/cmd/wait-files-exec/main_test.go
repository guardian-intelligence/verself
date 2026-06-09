package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	cfg, err := parse([]string{"--timeout=2s", "/tmp/a", "/tmp/b", "--", "otelcol", "--config", "config.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Timeout != 2*time.Second {
		t.Fatalf("timeout = %s", cfg.Timeout)
	}
	if len(cfg.Paths) != 2 || cfg.Paths[1] != "/tmp/b" {
		t.Fatalf("paths = %#v", cfg.Paths)
	}
	if len(cfg.Command) != 3 || cfg.Command[0] != "otelcol" {
		t.Fatalf("command = %#v", cfg.Command)
	}
}

func TestMissingFiles(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	ready := filepath.Join(dir, "ready")
	missing := filepath.Join(dir, "missing")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ready, []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := missingFiles([]string{empty, ready, missing})
	if len(got) != 2 || got[0] != empty || got[1] != missing {
		t.Fatalf("missing = %#v", got)
	}
}
