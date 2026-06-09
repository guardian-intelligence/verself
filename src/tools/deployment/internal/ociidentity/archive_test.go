package ociidentity

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadArchiveDigests(t *testing.T) {
	t.Parallel()
	archivePath, rootDigest, manifestDigest, archiveDigest := writeTestOCIArchive(t)

	digests, err := ReadArchiveDigests(archivePath)
	if err != nil {
		t.Fatalf("ReadArchiveDigests() error = %v", err)
	}
	if digests.ArchiveSHA256 != archiveDigest {
		t.Fatalf("ArchiveSHA256 = %q, want %q", digests.ArchiveSHA256, archiveDigest)
	}
	if got, want := digests.RootDigests, []string{rootDigest}; !sameStrings(got, want) {
		t.Fatalf("RootDigests = %#v, want %#v", got, want)
	}
	if got, want := digests.ManifestDigests, []string{manifestDigest}; !sameStrings(got, want) {
		t.Fatalf("ManifestDigests = %#v, want %#v", got, want)
	}
}

func TestReadArchiveDigestsRejectsMissingIndex(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "empty.oci.tar")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := tar.NewWriter(file).Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadArchiveDigests(path); err == nil {
		t.Fatal("ReadArchiveDigests() error = nil, want missing index error")
	}
}

func writeTestOCIArchive(t *testing.T) (string, string, string, string) {
	t.Helper()
	manifestJSON := mustJSON(t, map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{
			"mediaType": "application/vnd.oci.image.config.v1+json",
			"digest":    testDigest("config"),
			"size":      2,
		},
		"layers": []any{},
	})
	manifestDigest := digestOf(manifestJSON)
	indexJSON := mustJSON(t, map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.index.v1+json",
		"manifests": []any{
			map[string]any{
				"mediaType": "application/vnd.oci.image.manifest.v1+json",
				"digest":    manifestDigest,
				"size":      len(manifestJSON),
			},
		},
	})
	rootDigest := digestOf(indexJSON)
	rootIndexJSON := mustJSON(t, map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.index.v1+json",
		"manifests": []any{
			map[string]any{
				"mediaType": "application/vnd.oci.image.index.v1+json",
				"digest":    rootDigest,
				"size":      len(indexJSON),
			},
		},
	})

	archivePath := filepath.Join(t.TempDir(), "image.oci.tar")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(file)
	writeTarFile(t, writer, "index.json", rootIndexJSON)
	writeBlob(t, writer, rootDigest, indexJSON)
	writeBlob(t, writer, manifestDigest, manifestJSON)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	return archivePath, rootDigest, manifestDigest, "sha256:" + hex.EncodeToString(sum[:])
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func digestOf(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeBlob(t *testing.T, writer *tar.Writer, digest string, body []byte) {
	t.Helper()
	writeTarFile(t, writer, "blobs/sha256/"+digest[len("sha256:"):], body)
}

func writeTarFile(t *testing.T, writer *tar.Writer, name string, body []byte) {
	t.Helper()
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(body); err != nil {
		t.Fatal(err)
	}
}

func testDigest(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
