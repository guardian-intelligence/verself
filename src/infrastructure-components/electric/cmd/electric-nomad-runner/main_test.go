package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteEnvFileEscapesDatabasePassword(t *testing.T) {
	dir := t.TempDir()
	pgPasswordPath := filepath.Join(dir, "pg-password")
	secretPath := filepath.Join(dir, "electric-secret")
	envPath := filepath.Join(dir, "secrets", "electric.env")
	if err := os.WriteFile(pgPasswordPath, []byte("p@:s/word#1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretPath, []byte("electric-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := writeEnvFile(config{
		envFile:            envPath,
		pgUser:             "electric",
		pgPasswordFile:     pgPasswordPath,
		pgDatabase:         "sandbox_rental",
		electricSecretFile: secretPath,
	})
	if err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	if !strings.Contains(got, "DATABASE_URL=postgresql://electric:p%40%3As%2Fword%231@127.0.0.1:5432/sandbox_rental\n") {
		t.Fatalf("DATABASE_URL was not escaped correctly:\n%s", got)
	}
	if !strings.Contains(got, "ELECTRIC_SECRET=electric-secret\n") {
		t.Fatalf("ELECTRIC_SECRET missing:\n%s", got)
	}
}

func TestParseConfigAbsolutizesTaskLocalRuntimePaths(t *testing.T) {
	dir := t.TempDir()
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDir); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	t.Setenv("NOMAD_ALLOC_ID", "alloc-1")

	cfg, err := parseConfig([]string{
		"--ctr", "local/bin/ctr",
		"--containerd-address", "/run/containerd/containerd.sock",
		"--image", "registry.internal/electric:latest",
		"--env-file", "secrets/electric.env",
		"--pg-user", "electric",
		"--pg-password-file", "/etc/credstore/electric/pg-password",
		"--pg-database", "sandbox_rental",
		"--electric-secret-file", "/etc/credstore/electric/electric-secret",
		"--storage-dir", "/var/lib/electric",
		"--instance-id", "electric-sandbox",
		"--replication-stream-id", "stream-1",
		"--db-pool-size", "20",
		"--runc-binary", "local/bin/runc",
	})
	if err != nil {
		t.Fatal(err)
	}

	wantCtr := filepath.Join(dir, "local/bin/ctr")
	if cfg.ctrBin != wantCtr {
		t.Fatalf("ctr path = %q, want %q", cfg.ctrBin, wantCtr)
	}
	wantRunc := filepath.Join(dir, "local/bin/runc")
	if cfg.runcBinary != wantRunc {
		t.Fatalf("runc path = %q, want %q", cfg.runcBinary, wantRunc)
	}
}
