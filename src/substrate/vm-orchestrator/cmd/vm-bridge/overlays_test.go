package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyEtcOverlayRejectsGlobalIdentityFiles(t *testing.T) {
	for _, rel := range []string{"passwd", "group", "shadow", "gshadow"} {
		t.Run(rel, func(t *testing.T) {
			mountPath := t.TempDir()
			overlayPath := filepath.Join(mountPath, etcOverlayDir, rel)
			if err := os.MkdirAll(filepath.Dir(overlayPath), 0o755); err != nil {
				t.Fatalf("mkdir overlay: %v", err)
			}
			if err := os.WriteFile(overlayPath, []byte("runner:x:1000:1000::/home/runner:/bin/bash\n"), 0o644); err != nil {
				t.Fatalf("write overlay: %v", err)
			}

			err := (&agentSession{}).applyEtcOverlay("test-toolchain", mountPath)
			if err == nil {
				t.Fatal("applyEtcOverlay returned nil error")
			}
			if !strings.Contains(err.Error(), "global identity files are substrate-owned") {
				t.Fatalf("error = %q, want identity rejection", err.Error())
			}
		})
	}
}

func TestApplyEtcOverlayCopiesNonIdentityFile(t *testing.T) {
	mountPath := t.TempDir()
	etcRootPath := t.TempDir()
	oldEtcRoot := etcRoot
	etcRoot = etcRootPath
	defer func() { etcRoot = oldEtcRoot }()

	overlayPath := filepath.Join(mountPath, etcOverlayDir, "profile.d", "verself-actions-runner.sh")
	if err := os.MkdirAll(filepath.Dir(overlayPath), 0o755); err != nil {
		t.Fatalf("mkdir overlay: %v", err)
	}
	if err := os.WriteFile(overlayPath, []byte("export RUNNER_TOOL_CACHE=/tmp/verself-hostedtoolcache\n"), 0o644); err != nil {
		t.Fatalf("write overlay: %v", err)
	}

	if err := (&agentSession{}).applyEtcOverlay("test-toolchain", mountPath); err != nil {
		t.Fatalf("applyEtcOverlay: %v", err)
	}
	dest := filepath.Join(etcRootPath, "profile.d", "verself-actions-runner.sh")
	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read copied overlay: %v", err)
	}
	if got := string(raw); got != "export RUNNER_TOOL_CACHE=/tmp/verself-hostedtoolcache\n" {
		t.Fatalf("copied overlay = %q", got)
	}
}
