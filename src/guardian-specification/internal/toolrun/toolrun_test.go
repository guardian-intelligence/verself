package toolrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/verself/guardian-specification/internal/toolcatalog"
)

func TestEnsureExecutableFetchesAndVerifiesFileMirror(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source-tool")
	body := []byte("#!/bin/sh\nexit 0\n")
	if err := os.WriteFile(source, body, 0o755); err != nil {
		t.Fatalf("write source: %v", err)
	}
	sum := sha256.Sum256(body)
	tool := toolcatalog.ResolvedTool{
		Name:       "fake",
		Platform:   "linux/amd64",
		Ref:        "oci.verself.sh/tools/fake@sha256:" + hex.EncodeToString(sum[:]),
		Digest:     "sha256:" + hex.EncodeToString(sum[:]),
		Executable: "fake",
		Admission:  "admitted",
		Mirrors:    []string{"file://" + source},
	}
	path, err := EnsureExecutable(context.Background(), tool, Options{CacheRoot: filepath.Join(dir, "cache")})
	if err != nil {
		t.Fatalf("EnsureExecutable: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cached executable missing: %v", err)
	}
}

func TestEnsureExecutableRejectsUnadmittedTool(t *testing.T) {
	_, err := EnsureExecutable(context.Background(), toolcatalog.ResolvedTool{
		Name:       "fake",
		Digest:     "sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Executable: "fake",
		Admission:  "pending",
	}, Options{CacheRoot: t.TempDir()})
	if err == nil {
		t.Fatal("EnsureExecutable succeeded, want admission failure")
	}
}
