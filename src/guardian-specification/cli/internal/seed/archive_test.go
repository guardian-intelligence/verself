package seed

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteTarZstdDeterministic(t *testing.T) {
	dir := t.TempDir()
	files := writeSeedFiles(t, dir, 3, 1024*1024, compressiblePattern)

	var first bytes.Buffer
	firstManifest, err := WriteTarZstd(&first, files)
	if err != nil {
		t.Fatalf("first WriteTarZstd: %v", err)
	}
	var second bytes.Buffer
	secondManifest, err := WriteTarZstd(&second, files)
	if err != nil {
		t.Fatalf("second WriteTarZstd: %v", err)
	}
	if firstManifest.ArchiveSHA256 != secondManifest.ArchiveSHA256 {
		t.Fatalf("archive digest changed: %s != %s", firstManifest.ArchiveSHA256, secondManifest.ArchiveSHA256)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatalf("archive bytes changed between identical inputs")
	}
	if firstManifest.UncompressedBytes != int64(3*1024*1024) {
		t.Fatalf("UncompressedBytes = %d, want %d", firstManifest.UncompressedBytes, 3*1024*1024)
	}
}

func TestWriteTarZstdRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.WriteFile(source, []byte("x"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	_, err := WriteTarZstd(io.Discard, []File{{
		Source: source,
		Target: "../escape",
		Mode:   "0644",
	}})
	if err == nil {
		t.Fatalf("WriteTarZstd accepted target traversal")
	}
}

type seedFilePattern int

const (
	compressiblePattern seedFilePattern = iota
	pseudoRandomPattern
)

func writeSeedFiles(tb testing.TB, dir string, count int, size int, pattern seedFilePattern) []File {
	tb.Helper()
	files := make([]File, 0, count)
	block := make([]byte, 128*1024)
	for i := 0; i < count; i++ {
		fillBlock(block, i, pattern)
		source := filepath.Join(dir, "seed-"+string(rune('a'+i)))
		file, err := os.OpenFile(source, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			tb.Fatalf("create benchmark file: %v", err)
		}
		remaining := size
		chunkIndex := 0
		for remaining > 0 {
			if pattern == pseudoRandomPattern {
				fillBlock(block, i*1024+chunkIndex, pattern)
			}
			chunk := min(remaining, len(block))
			if _, err := file.Write(block[:chunk]); err != nil {
				_ = file.Close()
				tb.Fatalf("write benchmark file: %v", err)
			}
			remaining -= chunk
			chunkIndex++
		}
		if err := file.Close(); err != nil {
			tb.Fatalf("close benchmark file: %v", err)
		}
		files = append(files, File{
			Source: source,
			Target: "files/" + filepath.Base(source),
			Mode:   "0644",
		})
	}
	return files
}

func fillBlock(block []byte, fileIndex int, pattern seedFilePattern) {
	switch pattern {
	case compressiblePattern:
		for i := range block {
			block[i] = byte((i * 31) % 251)
		}
	case pseudoRandomPattern:
		state := uint64(0x9e3779b97f4a7c15) + uint64(fileIndex)
		for i := range block {
			state ^= state << 13
			state ^= state >> 7
			state ^= state << 17
			block[i] = byte(state)
		}
	}
}
