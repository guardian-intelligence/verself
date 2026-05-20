package vmorchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	snapshotStateFilename    = "vmstate"
	snapshotMemoryFilename   = "memory"
	snapshotManifestFilename = "manifest.json"
)

type SnapshotArtifact struct {
	Key       string
	Dir       string
	StatePath string
	MemPath   string
}

type JailSnapshotPaths struct {
	StateHostPath string
	MemHostPath   string
	StateJailPath string
	MemJailPath   string
}

type snapshotManifest struct {
	Key            string    `json:"key"`
	Schema         string    `json:"schema"`
	CreatedAt      time.Time `json:"created_at"`
	StateBytes     int64     `json:"state_bytes"`
	MemoryBytes    int64     `json:"memory_bytes"`
	ActivationMode string    `json:"activation_mode"`
}

type SnapshotStore struct {
	root string
	uid  int
	gid  int
}

func NewSnapshotStore(root string, uid, gid int) *SnapshotStore {
	return &SnapshotStore{root: filepath.Clean(strings.TrimSpace(root)), uid: uid, gid: gid}
}

func (s *SnapshotStore) Enabled() bool {
	return s != nil && s.root != "" && s.root != "."
}

func (s *SnapshotStore) Lookup(ctx context.Context, key SnapshotKey) (SnapshotArtifact, bool, error) {
	if !s.Enabled() {
		return SnapshotArtifact{}, false, nil
	}
	if err := ctx.Err(); err != nil {
		return SnapshotArtifact{}, false, err
	}
	keyValue := key.String()
	if keyValue == "" {
		return SnapshotArtifact{}, false, fmt.Errorf("snapshot key is required")
	}
	dir := filepath.Join(s.root, keyValue)
	manifestPath := filepath.Join(dir, snapshotManifestFilename)
	statePath := filepath.Join(dir, snapshotStateFilename)
	memPath := filepath.Join(dir, snapshotMemoryFilename)
	if _, err := os.Stat(manifestPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SnapshotArtifact{}, false, nil
		}
		return SnapshotArtifact{}, false, fmt.Errorf("stat snapshot manifest %s: %w", manifestPath, err)
	}
	for _, path := range []string{statePath, memPath} {
		info, err := os.Stat(path)
		if err != nil {
			return SnapshotArtifact{}, false, fmt.Errorf("stat snapshot artifact %s: %w", path, err)
		}
		if info.Size() == 0 {
			return SnapshotArtifact{}, false, fmt.Errorf("snapshot artifact %s is empty", path)
		}
	}
	return SnapshotArtifact{Key: keyValue, Dir: dir, StatePath: statePath, MemPath: memPath}, true, nil
}

func (s *SnapshotStore) Delete(ctx context.Context, key SnapshotKey) error {
	if !s.Enabled() {
		return fmt.Errorf("snapshot store is disabled")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	keyValue := key.String()
	if keyValue == "" {
		return fmt.Errorf("snapshot key is required")
	}
	return os.RemoveAll(filepath.Join(s.root, keyValue))
}

func (s *SnapshotStore) StageForJail(ctx context.Context, artifact SnapshotArtifact, jailRoot string) (JailSnapshotPaths, func(), error) {
	if !s.Enabled() {
		return JailSnapshotPaths{}, nil, fmt.Errorf("snapshot store is disabled")
	}
	if err := ctx.Err(); err != nil {
		return JailSnapshotPaths{}, nil, err
	}
	dir, err := s.ensureJailSnapshotDir(jailRoot)
	if err != nil {
		return JailSnapshotPaths{}, nil, err
	}
	base := "restore-" + artifact.Key
	paths := JailSnapshotPaths{
		StateHostPath: filepath.Join(dir, base+".vmstate"),
		MemHostPath:   filepath.Join(dir, base+".mem"),
		StateJailPath: "/snapshots/" + base + ".vmstate",
		MemJailPath:   "/snapshots/" + base + ".mem",
	}
	if err := replaceLinkedOrCopiedFile(ctx, artifact.StatePath, paths.StateHostPath, s.uid, s.gid); err != nil {
		return JailSnapshotPaths{}, nil, fmt.Errorf("stage snapshot state: %w", err)
	}
	if err := replaceLinkedOrCopiedFile(ctx, artifact.MemPath, paths.MemHostPath, s.uid, s.gid); err != nil {
		_ = os.Remove(paths.StateHostPath)
		return JailSnapshotPaths{}, nil, fmt.Errorf("stage snapshot memory: %w", err)
	}
	cleanup := func() {
		_ = os.Remove(paths.StateHostPath)
		_ = os.Remove(paths.MemHostPath)
	}
	return paths, cleanup, nil
}

func (s *SnapshotStore) BuildPaths(ctx context.Context, key SnapshotKey, jailRoot string) (JailSnapshotPaths, error) {
	if !s.Enabled() {
		return JailSnapshotPaths{}, fmt.Errorf("snapshot store is disabled")
	}
	if err := ctx.Err(); err != nil {
		return JailSnapshotPaths{}, err
	}
	dir, err := s.ensureJailSnapshotDir(jailRoot)
	if err != nil {
		return JailSnapshotPaths{}, err
	}
	base := "build-" + key.String()
	paths := JailSnapshotPaths{
		StateHostPath: filepath.Join(dir, base+".vmstate"),
		MemHostPath:   filepath.Join(dir, base+".mem"),
		StateJailPath: "/snapshots/" + base + ".vmstate",
		MemJailPath:   "/snapshots/" + base + ".mem",
	}
	_ = os.Remove(paths.StateHostPath)
	_ = os.Remove(paths.MemHostPath)
	return paths, nil
}

func (s *SnapshotStore) Publish(ctx context.Context, key SnapshotKey, paths JailSnapshotPaths) (SnapshotArtifact, error) {
	if !s.Enabled() {
		return SnapshotArtifact{}, fmt.Errorf("snapshot store is disabled")
	}
	if err := ctx.Err(); err != nil {
		return SnapshotArtifact{}, err
	}
	keyValue := key.String()
	finalDir := filepath.Join(s.root, keyValue)
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return SnapshotArtifact{}, fmt.Errorf("mkdir snapshot cache root %s: %w", s.root, err)
	}
	if artifact, ok, err := s.Lookup(ctx, key); err != nil {
		return SnapshotArtifact{}, err
	} else if ok {
		return artifact, nil
	}
	tmpDir := filepath.Join(s.root, fmt.Sprintf(".%s.%d.tmp", keyValue, time.Now().UnixNano()))
	_ = os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return SnapshotArtifact{}, fmt.Errorf("mkdir snapshot cache temp %s: %w", tmpDir, err)
	}
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.RemoveAll(tmpDir)
		}
	}()
	stateTarget := filepath.Join(tmpDir, snapshotStateFilename)
	memTarget := filepath.Join(tmpDir, snapshotMemoryFilename)
	if err := replaceLinkedOrCopiedFile(ctx, paths.StateHostPath, stateTarget, s.uid, s.gid); err != nil {
		return SnapshotArtifact{}, fmt.Errorf("publish snapshot state: %w", err)
	}
	if err := replaceLinkedOrCopiedFile(ctx, paths.MemHostPath, memTarget, s.uid, s.gid); err != nil {
		return SnapshotArtifact{}, fmt.Errorf("publish snapshot memory: %w", err)
	}
	stateInfo, err := os.Stat(stateTarget)
	if err != nil {
		return SnapshotArtifact{}, fmt.Errorf("stat published snapshot state: %w", err)
	}
	memInfo, err := os.Stat(memTarget)
	if err != nil {
		return SnapshotArtifact{}, fmt.Errorf("stat published snapshot memory: %w", err)
	}
	manifest := snapshotManifest{
		Key:            keyValue,
		Schema:         snapshotKeySchema,
		CreatedAt:      time.Now().UTC(),
		StateBytes:     stateInfo.Size(),
		MemoryBytes:    memInfo.Size(),
		ActivationMode: string(ActivationModeSnapshotRestore),
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return SnapshotArtifact{}, fmt.Errorf("marshal snapshot manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, snapshotManifestFilename), append(data, '\n'), 0o644); err != nil {
		return SnapshotArtifact{}, fmt.Errorf("write snapshot manifest: %w", err)
	}
	if err := os.Rename(tmpDir, finalDir); err != nil {
		artifact, ok, lookupErr := s.Lookup(ctx, key)
		if lookupErr != nil {
			return SnapshotArtifact{}, lookupErr
		}
		if ok {
			return artifact, nil
		}
		return SnapshotArtifact{}, fmt.Errorf("publish snapshot artifact %s: %w", finalDir, err)
	}
	cleanupTmp = false
	return SnapshotArtifact{
		Key:       keyValue,
		Dir:       finalDir,
		StatePath: filepath.Join(finalDir, snapshotStateFilename),
		MemPath:   filepath.Join(finalDir, snapshotMemoryFilename),
	}, nil
}

func (s *SnapshotStore) ensureJailSnapshotDir(jailRoot string) (string, error) {
	jailRoot = filepath.Clean(strings.TrimSpace(jailRoot))
	if jailRoot == "" || jailRoot == "." {
		return "", fmt.Errorf("jail root is required")
	}
	dir := filepath.Join(jailRoot, "snapshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir jail snapshot dir %s: %w", dir, err)
	}
	if err := os.Chown(dir, s.uid, s.gid); err != nil {
		return "", fmt.Errorf("chown jail snapshot dir %s: %w", dir, err)
	}
	return dir, nil
}

func replaceLinkedOrCopiedFile(ctx context.Context, src, dst string, uid, gid int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(src) == "" || strings.TrimSpace(dst) == "" {
		return fmt.Errorf("source and destination paths are required")
	}
	_ = os.Remove(dst)
	if err := os.Link(src, dst); err != nil {
		if copyErr := copyFileContents(ctx, src, dst); copyErr != nil {
			return fmt.Errorf("link %s -> %s: %w; copy: %v", src, dst, err, copyErr)
		}
	}
	if err := os.Chmod(dst, 0o644); err != nil {
		return fmt.Errorf("chmod %s: %w", dst, err)
	}
	if err := os.Chown(dst, uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", dst, err)
	}
	return nil
}

func copyFileContents(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("copy %s -> %s: %w", src, dst, copyErr)
	}
	if syncErr != nil {
		return fmt.Errorf("fsync %s: %w", dst, syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", dst, closeErr)
	}
	return nil
}
