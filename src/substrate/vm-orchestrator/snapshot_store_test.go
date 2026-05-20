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
}
