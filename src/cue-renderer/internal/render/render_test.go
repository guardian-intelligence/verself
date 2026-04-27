package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMemFS_RoundtripAndPathOrder(t *testing.T) {
	fs := NewMemFS()
	if err := fs.WriteFile("z/late.txt", []byte("z")); err != nil {
		t.Fatalf("WriteFile late: %v", err)
	}
	if err := fs.WriteFile("a/early.txt", []byte("a")); err != nil {
		t.Fatalf("WriteFile early: %v", err)
	}

	got, ok := fs.Get("a/early.txt")
	if !ok || string(got) != "a" {
		t.Fatalf("Get early: got=%q ok=%v", got, ok)
	}
	if _, ok := fs.Get("missing"); ok {
		t.Fatal("Get missing: expected absent")
	}

	paths := fs.Paths()
	want := []string{"a/early.txt", "z/late.txt"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("Paths: got %v want %v", paths, want)
	}
}

func TestMemFS_LastWriterWinsAndCopiesData(t *testing.T) {
	fs := NewMemFS()
	src := []byte("v1")
	_ = fs.WriteFile("k", src)
	src[0] = 'X' // mutate caller's buffer

	got, _ := fs.Get("k")
	if string(got) != "v1" {
		t.Fatalf("MemFS retained reference to caller buffer: %q", got)
	}

	got[0] = 'Y' // mutate Get's buffer
	got2, _ := fs.Get("k")
	if string(got2) != "v1" {
		t.Fatalf("MemFS Get returned shared reference: %q", got2)
	}

	_ = fs.WriteFile("k", []byte("v2"))
	got3, _ := fs.Get("k")
	if string(got3) != "v2" {
		t.Fatalf("MemFS overwrite: got %q", got3)
	}
}

func TestMemFS_SHA256(t *testing.T) {
	fs := NewMemFS()
	_ = fs.WriteFile("k", []byte("hello"))

	want := sha256.Sum256([]byte("hello"))
	if got := fs.SHA256("k"); got != hex.EncodeToString(want[:]) {
		t.Fatalf("SHA256: got %q want %q", got, hex.EncodeToString(want[:]))
	}
	if got := fs.SHA256("missing"); got != "" {
		t.Fatalf("SHA256 missing: want empty, got %q", got)
	}
}

func TestMemFS_DiffAgainstDisk(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "match.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "diverged.txt"), []byte("disk"), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := NewMemFS()
	_ = fs.WriteFile("match.txt", []byte("ok"))
	_ = fs.WriteFile("sub/diverged.txt", []byte("memory"))
	_ = fs.WriteFile("sub/missing.txt", []byte("only-in-memory"))

	stale, err := fs.DiffAgainstDisk(root)
	if err != nil {
		t.Fatalf("DiffAgainstDisk: %v", err)
	}
	want := []string{"sub/diverged.txt", "sub/missing.txt"}
	if !reflect.DeepEqual(stale, want) {
		t.Fatalf("stale: got %v want %v", stale, want)
	}
}

func TestOSFS_CreatesParentDirs(t *testing.T) {
	root := t.TempDir()
	fs := OSFS{Root: root}
	want := []byte("contents")
	if err := fs.WriteFile("nested/dir/file.txt", want); err != nil {
		t.Fatalf("OSFS WriteFile: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "nested/dir/file.txt"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("contents: got %q want %q", got, want)
	}
}
