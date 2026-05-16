package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSeedCatalogValidatesAndPreservesOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	body := []byte(`{
  "images": [
    {
      "ref": "substrate",
      "strategy": "dd_from_file",
      "source_path": "/var/lib/verself/guest-images/substrate.ext4",
      "size_bytes": 2147483648,
      "volblocksize": "16K"
    },
    {
      "ref": "gh-actions-runner",
      "strategy": "dd_from_file",
      "source_path": "/var/lib/verself/guest-images/toolchains/gh-actions-runner.ext4",
      "size_bytes": 1073741824,
      "volblocksize": "16K"
    }
  ]
}`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	catalog, err := loadSeedCatalog(path)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	if got, want := len(catalog.Images), 2; got != want {
		t.Fatalf("image count = %d, want %d", got, want)
	}
	if got, want := catalog.Images[0].Ref, "substrate"; got != want {
		t.Fatalf("first ref = %q, want %q", got, want)
	}
	if got, want := catalog.Images[1].Ref, "gh-actions-runner"; got != want {
		t.Fatalf("second ref = %q, want %q", got, want)
	}
}

func TestLoadSeedCatalogRejectsInvalidImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	body := []byte(`{"images":[{"ref":"substrate","strategy":"dd_from_file","size_bytes":1}]}`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := loadSeedCatalog(path); err == nil {
		t.Fatal("expected validation error")
	}
}
