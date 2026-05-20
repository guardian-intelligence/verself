package vmorchestrator

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/verself/vm-orchestrator/zfs"
	"go.opentelemetry.io/otel/attribute"
)

const storageKeyBytes = 32

type StorageKeyMaterial struct {
	Key     []byte
	Version string
}

type StorageKeyProvider interface {
	GetOrCreateStorageKey(ctx context.Context, orgID string) (StorageKeyMaterial, error)
}

type FileStorageKeyProvider struct {
	dir string
}

func NewFileStorageKeyProvider(dir string) *FileStorageKeyProvider {
	return &FileStorageKeyProvider{dir: strings.TrimSpace(dir)}
}

func (p *FileStorageKeyProvider) GetOrCreateStorageKey(ctx context.Context, orgID string) (StorageKeyMaterial, error) {
	if err := ctx.Err(); err != nil {
		return StorageKeyMaterial{}, err
	}
	orgID = strings.TrimSpace(orgID)
	if !zfs.IsValidRef(orgID) {
		return StorageKeyMaterial{}, fmt.Errorf("storage key org id is invalid: %s", orgID)
	}
	if p == nil || p.dir == "" {
		return StorageKeyMaterial{}, fmt.Errorf("storage key directory is required")
	}
	if err := os.MkdirAll(p.dir, 0o700); err != nil {
		return StorageKeyMaterial{}, fmt.Errorf("create storage key directory %s: %w", p.dir, err)
	}
	if err := os.Chmod(p.dir, 0o700); err != nil {
		return StorageKeyMaterial{}, fmt.Errorf("chmod storage key directory %s: %w", p.dir, err)
	}
	path := filepath.Join(p.dir, orgID+".raw")
	key, err := readStorageKeyFile(path)
	if err == nil {
		return StorageKeyMaterial{Key: key, Version: storageKeyVersion(key)}, nil
	}
	if !os.IsNotExist(err) {
		return StorageKeyMaterial{}, err
	}
	key = make([]byte, storageKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return StorageKeyMaterial{}, fmt.Errorf("generate storage key: %w", err)
	}
	file, createErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if createErr != nil {
		if os.IsExist(createErr) {
			key, err = readStorageKeyFile(path)
			if err != nil {
				return StorageKeyMaterial{}, err
			}
			return StorageKeyMaterial{Key: key, Version: storageKeyVersion(key)}, nil
		}
		return StorageKeyMaterial{}, fmt.Errorf("create storage key file %s: %w", path, createErr)
	}
	writeErr := func() error {
		defer func() { _ = file.Close() }()
		if _, err := file.Write(key); err != nil {
			return fmt.Errorf("write storage key file %s: %w", path, err)
		}
		if err := file.Sync(); err != nil {
			return fmt.Errorf("fsync storage key file %s: %w", path, err)
		}
		return nil
	}()
	if writeErr != nil {
		_ = os.Remove(path)
		return StorageKeyMaterial{}, writeErr
	}
	return StorageKeyMaterial{Key: key, Version: storageKeyVersion(key)}, nil
}

func readStorageKeyFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}
		return nil, fmt.Errorf("stat storage key file %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("storage key file %s must not be a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("storage key file %s must be a regular file", path)
	}
	if info.Mode().Perm() != 0o600 {
		if err := os.Chmod(path, 0o600); err != nil {
			return nil, fmt.Errorf("chmod storage key file %s: %w", path, err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read storage key file %s: %w", path, err)
	}
	if len(data) != storageKeyBytes {
		return nil, fmt.Errorf("storage key file %s has %d bytes, want %d", path, len(data), storageKeyBytes)
	}
	return append([]byte(nil), data...), nil
}

func storageKeyVersion(key []byte) string {
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:])[:16]
}

type StorageKeyManager struct {
	provider StorageKeyProvider
	logger   *slog.Logger

	mu   sync.Mutex
	refs map[string]int
}

func NewStorageKeyManager(provider StorageKeyProvider, logger *slog.Logger) *StorageKeyManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &StorageKeyManager{
		provider: provider,
		logger:   logger,
		refs:     map[string]int{},
	}
}

type StorageKeyHold struct {
	manager *StorageKeyManager
	orgID   string
	leaseID string
	key     []byte
	version string

	mu       sync.Mutex
	released bool
}

func (m *StorageKeyManager) Acquire(ctx context.Context, orgID, leaseID string) (*StorageKeyHold, error) {
	ctx, end := startStepSpan(ctx, "vmorchestrator.storage_key.acquire",
		attribute.String("org.id", orgID),
		attribute.String("lease.id", leaseID),
	)
	var err error
	defer func() { end(err) }()
	if m == nil || m.provider == nil {
		err = fmt.Errorf("storage key provider is required")
		return nil, err
	}
	material, err := m.provider.GetOrCreateStorageKey(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if len(material.Key) != storageKeyBytes {
		err = fmt.Errorf("storage key for org %s has %d bytes, want %d", orgID, len(material.Key), storageKeyBytes)
		return nil, err
	}
	if strings.TrimSpace(material.Version) == "" {
		material.Version = storageKeyVersion(material.Key)
	}
	m.mu.Lock()
	m.refs[orgID]++
	refCount := m.refs[orgID]
	m.mu.Unlock()
	m.logger.InfoContext(ctx, "storage key acquired",
		"org_id", orgID,
		"lease_id", leaseID,
		"key_version", material.Version,
		"ref_count", refCount,
	)
	return &StorageKeyHold{
		manager: m,
		orgID:   orgID,
		leaseID: leaseID,
		key:     append([]byte(nil), material.Key...),
		version: material.Version,
	}, nil
}

func (h *StorageKeyHold) Key() []byte {
	if h == nil {
		return nil
	}
	return append([]byte(nil), h.key...)
}

func (h *StorageKeyHold) Version() string {
	if h == nil {
		return ""
	}
	return h.version
}

func (h *StorageKeyHold) Release(ctx context.Context) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	if h.released {
		h.mu.Unlock()
		return false
	}
	h.released = true
	h.mu.Unlock()
	lastRef := false
	if h.manager != nil {
		lastRef = h.manager.release(ctx, h)
	}
	for i := range h.key {
		h.key[i] = 0
	}
	return lastRef
}

func (m *StorageKeyManager) release(ctx context.Context, hold *StorageKeyHold) bool {
	ctx, end := startStepSpan(ctx, "vmorchestrator.storage_key.release",
		attribute.String("org.id", hold.orgID),
		attribute.String("lease.id", hold.leaseID),
		attribute.String("storage_key.version", hold.version),
	)
	defer func() { end(nil) }()
	m.mu.Lock()
	refCount := m.refs[hold.orgID]
	if refCount <= 1 {
		delete(m.refs, hold.orgID)
		refCount = 0
	} else {
		refCount--
		m.refs[hold.orgID] = refCount
	}
	lastRef := refCount == 0
	m.mu.Unlock()
	m.logger.InfoContext(ctx, "storage key released",
		"org_id", hold.orgID,
		"lease_id", hold.leaseID,
		"key_version", hold.version,
		"ref_count", refCount,
	)
	return lastRef
}
