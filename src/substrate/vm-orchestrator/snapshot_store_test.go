package vmorchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotStorePublishLookupStage(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	jailRoot := t.TempDir()
	store := NewSnapshotStore(filepath.Join(root, "cache"), os.Getuid(), os.Getgid())
	key := SnapshotKey{value: "abc123"}
	buildDir := filepath.Join(jailRoot, "snapshots")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatalf("mkdir build dir: %v", err)
	}
	paths := JailSnapshotPaths{
		StateHostPath: filepath.Join(buildDir, "build.vmstate"),
		MemHostPath:   filepath.Join(buildDir, "build.mem"),
	}
	if err := os.WriteFile(paths.StateHostPath, []byte("state"), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if err := os.WriteFile(paths.MemHostPath, []byte("memory"), 0o644); err != nil {
		t.Fatalf("write memory: %v", err)
	}

	artifact, err := store.Publish(ctx, key, paths)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if artifact.Key != key.String() {
		t.Fatalf("artifact key = %q, want %q", artifact.Key, key.String())
	}
	got, ok, err := store.Lookup(ctx, key)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if !ok {
		t.Fatal("lookup missed published artifact")
	}
	if got.StatePath != artifact.StatePath || got.MemPath != artifact.MemPath {
		t.Fatalf("lookup artifact = %#v, want %#v", got, artifact)
	}

	staged, cleanup, err := store.StageForJail(ctx, got, jailRoot)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	defer cleanup()
	if staged.StateJailPath == "" || staged.MemJailPath == "" {
		t.Fatalf("staged jail paths missing: %#v", staged)
	}
	stateData, err := os.ReadFile(staged.StateHostPath)
	if err != nil {
		t.Fatalf("read staged state: %v", err)
	}
	if string(stateData) != "state" {
		t.Fatalf("staged state = %q", stateData)
	}
	memData, err := os.ReadFile(staged.MemHostPath)
	if err != nil {
		t.Fatalf("read staged memory: %v", err)
	}
	if string(memData) != "memory" {
		t.Fatalf("staged memory = %q", memData)
	}
	assertSameFile(t, got.StatePath, staged.StateHostPath)
	assertSameFile(t, got.MemPath, staged.MemHostPath)
}

func assertSameFile(t *testing.T, wantPath, gotPath string) {
	t.Helper()
	want, err := os.Stat(wantPath)
	if err != nil {
		t.Fatalf("stat %s: %v", wantPath, err)
	}
	got, err := os.Stat(gotPath)
	if err != nil {
		t.Fatalf("stat %s: %v", gotPath, err)
	}
	if !os.SameFile(want, got) {
		t.Fatalf("%s and %s are not the same hardlinked file", wantPath, gotPath)
	}
}

func TestDefaultSnapshotCacheSharesFilesystemWithJailerRoot(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.JailerRoot != defaultJailerRoot {
		t.Fatalf("default jailer root = %q, want %q", cfg.JailerRoot, defaultJailerRoot)
	}
	wantCacheDir := filepath.Join(filepath.Dir(cfg.JailerRoot), "firecracker-snapshot-cache")
	if cfg.SnapshotCacheDir != wantCacheDir {
		t.Fatalf("default snapshot cache dir = %q, want %q", cfg.SnapshotCacheDir, wantCacheDir)
	}
	if filepath.Dir(cfg.SnapshotCacheDir) != filepath.Dir(cfg.JailerRoot) {
		t.Fatalf("snapshot cache and jailer root should share a parent: cache=%q jailer=%q", cfg.SnapshotCacheDir, cfg.JailerRoot)
	}

	orchestrator := New(Config{Pool: "vspool"}, nil)
	if orchestrator.cfg.JailerRoot != defaultJailerRoot {
		t.Fatalf("normalized jailer root = %q, want %q", orchestrator.cfg.JailerRoot, defaultJailerRoot)
	}
	if orchestrator.cfg.SnapshotCacheDir != wantCacheDir {
		t.Fatalf("normalized snapshot cache dir = %q, want %q", orchestrator.cfg.SnapshotCacheDir, wantCacheDir)
	}

	custom := New(Config{Pool: "vspool", JailerRoot: "/tmp/jailer"}, nil)
	customWantCacheDir := filepath.Join(filepath.Dir(custom.cfg.JailerRoot), "firecracker-snapshot-cache")
	if custom.cfg.SnapshotCacheDir != customWantCacheDir {
		t.Fatalf("custom snapshot cache dir = %q, want %q", custom.cfg.SnapshotCacheDir, customWantCacheDir)
	}
}
