// Package render defines the Renderer interface every artefact implements.
// One package per artefact lives under render/<name>/; each registers a
// Renderer that the CLI iterates over for `generate`.
//
// The interface accepts a writable filesystem rather than returning
// bytes-and-an-OutputPath: a single artefact can produce multiple files
// (think `etc/nftables.d/<component>.nft` per component), and routing
// renderers through WritableFS lets the CLI swap in an in-memory FS for
// tests, an os-rooted FS for `generate`, or a Bazel-output FS for future
// `cue_render` rules — without renderers caring which.
package render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/verself/cue-renderer/internal/load"
)

// Renderer turns a Loaded topology snapshot into one or more files
// underneath out. Implementations should be deterministic — the `check`
// command diffs every generated path against the on-disk file.
type Renderer interface {
	// Name is the short identifier used by `cue-renderer render <name>`
	// and as the value of the `topology.artifact` span attribute.
	Name() string

	// Render writes the artefact's files to out. Paths inside out are
	// cache-relative — a per-component nftables renderer writes to
	// "share/rendered/etc/nftables.d/<name>.nft" for each component.
	// The CLI's `--output-dir` flag anchors these under the cache root
	// (e.g. `.cache/render/prod/`).
	Render(ctx context.Context, loaded load.Loaded, out WritableFS) error
}

// WritableFS is the small interface every renderer writes through. We
// don't use io/fs because the standard library only models read access;
// we need one method (WriteFile) and want to keep mocking trivial.
type WritableFS interface {
	// WriteFile writes data to the given repo-relative path, creating
	// any parent directories. Repeated writes overwrite.
	WriteFile(path string, data []byte) error
}

// OSFS writes into a real directory tree rooted at Root. Used by
// `cue-renderer generate` to write into the working repo.
type OSFS struct {
	Root string
}

// WriteFile creates parent dirs and writes path under fs.Root. Generated
// files are always mode 0644 — they're checked-in artefacts, not secrets.
func (o OSFS) WriteFile(path string, data []byte) error {
	full := filepath.Join(o.Root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, data, 0o644)
}

// MemFS captures writes in-memory. Used by `check` (to diff renders
// against on-disk content without touching the disk) and by tests.
type MemFS struct {
	mu    sync.Mutex
	files map[string][]byte
}

// NewMemFS returns an empty in-memory writable filesystem.
func NewMemFS() *MemFS {
	return &MemFS{files: map[string][]byte{}}
}

// WriteFile records the path -> data mapping. Last writer wins.
func (m *MemFS) WriteFile(path string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	m.files[path] = cp
	return nil
}

// Get returns the bytes recorded for path and whether it was written.
func (m *MemFS) Get(path string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[path]
	if !ok {
		return nil, false
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp, true
}

// Paths returns every path written, sorted lexically.
func (m *MemFS) Paths() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.files))
	for p := range m.files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// SHA256 of a path written into m, hex-encoded. Empty for missing paths.
// Spans use this as the topology.generated_sha256 attribute.
func (m *MemFS) SHA256(path string) string {
	b, ok := m.Get(path)
	if !ok {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
