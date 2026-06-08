package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteInstanceEnvFileUsesEncodedCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NOMAD_SECRETS_DIR", dir)

	path, err := writeInstanceEnvFile(electricInstance{
		Name:         "electric",
		Database:     "sandbox_rental",
		DatabaseUser: "electric",
	}, runtimeSecrets{
		pgPassword: "pass word/with:symbols",
		apiSecret:  "api-secret",
	}, postgresConfig{
		Host: "127.0.0.1",
		Port: 5432,
	})
	if err != nil {
		t.Fatalf("writeInstanceEnvFile: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("env file dir = %q, want %q", filepath.Dir(path), dir)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "DATABASE_URL=postgresql://electric:pass+word%2Fwith%3Asymbols@127.0.0.1:5432/sandbox_rental?sslmode=disable") {
		t.Fatalf("env file did not contain encoded database URL:\n%s", text)
	}
	if !strings.Contains(text, "ELECTRIC_SECRET=api-secret\n") {
		t.Fatalf("env file missing API secret:\n%s", text)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat env file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("env file mode = %o, want 0600", got)
	}
}

func TestValidateInstanceRejectsBadIdentifiers(t *testing.T) {
	err := validateInstance(electricInstance{
		Name:                "electric",
		ServiceName:         "electric",
		StorageDir:          "/var/lib/electric",
		Database:            "bad-name",
		DatabaseUser:        "electric",
		ConnectionLimit:     25,
		ReplicationStreamID: "default",
		DBPoolSize:          15,
	})
	if err == nil {
		t.Fatalf("validateInstance accepted invalid database identifier")
	}
}

func TestIsElectricContainerdProcessMatchesOnlyConfiguredRuntime(t *testing.T) {
	if !isElectricContainerdProcess("/var/lib/electric/runtime/current/bin/containerd --address /run/electric-containerd/containerd.sock --root /var/lib/electric-containerd/root --state /run/electric-containerd/state", "/run/electric-containerd/containerd.sock", "/var/lib/electric-containerd/root") {
		t.Fatal("expected Electric containerd command to match")
	}
	if !isElectricContainerdProcess("/var/lib/electric/runtime/current/bin/containerd-shim-runc-v2 -namespace default -id electric-old -address /run/electric-containerd/containerd.sock", "/run/electric-containerd/containerd.sock", "/var/lib/electric-containerd/root") {
		t.Fatal("expected Electric containerd shim command to match")
	}
	if isElectricContainerdProcess("/usr/bin/containerd --address /run/containerd/containerd.sock --root /var/lib/containerd", "/run/electric-containerd/containerd.sock", "/var/lib/electric-containerd/root") {
		t.Fatal("did not expect unrelated containerd command to match")
	}
}

func TestProcessStateFromStatHandlesNamesWithSpaces(t *testing.T) {
	state, ok := processStateFromStat("1234 (containerd worker) Z 1 2 3 4")
	if !ok {
		t.Fatal("expected process state")
	}
	if state != "Z" {
		t.Fatalf("state = %q, want Z", state)
	}
}
