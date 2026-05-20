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

func TestDigestDirectoryIncludesPathsAndContents(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a", "one.txt"), "same")
	mustWriteFile(t, filepath.Join(root, "b", "two.txt"), "same")

	first, err := digestDirectory(root)
	if err != nil {
		t.Fatalf("digest directory: %v", err)
	}
	if err := os.Rename(filepath.Join(root, "b", "two.txt"), filepath.Join(root, "b", "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	second, err := digestDirectory(root)
	if err != nil {
		t.Fatalf("digest directory after rename: %v", err)
	}
	if first == second {
		t.Fatal("digest did not change after path rename")
	}
}

func TestStageGuestImagesSkipsCurrentArtifacts(t *testing.T) {
	root := t.TempDir()
	substrateInputs := filepath.Join(root, "substrate-inputs")
	toolchainInputs := filepath.Join(root, "toolchain-inputs")
	guestImages := filepath.Join(root, "guest-images")
	for _, rel := range substrateOutputFiles {
		mustWriteFile(t, filepath.Join(guestImages, rel), rel)
	}
	for _, rel := range toolchainOutputFiles {
		mustWriteFile(t, filepath.Join(guestImages, rel), rel)
		mustWriteFile(t, filepath.Join(toolchainInputs, rel), rel)
	}
	mustWriteFile(t, filepath.Join(substrateInputs, "build-substrate"), "unused")
	mustWriteFile(t, filepath.Join(substrateInputs, "vm-bridge"), "unused")

	substrateDigest, err := digestDirectory(substrateInputs)
	if err != nil {
		t.Fatal(err)
	}
	toolchainDigest, err := digestDirectory(toolchainInputs)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDigestFile(filepath.Join(guestImages, ".substrate-inputs.sha256"), substrateDigest); err != nil {
		t.Fatal(err)
	}
	if err := writeDigestFile(filepath.Join(guestImages, "toolchains", ".toolchain-images.sha256"), toolchainDigest); err != nil {
		t.Fatal(err)
	}

	result, err := stageGuestImages(t.Context(), stageGuestImagesFlags{
		substrateInputs: substrateInputs,
		toolchainImages: toolchainInputs,
		guestImagesDir:  guestImages,
		workDir:         filepath.Join(root, "work"),
	})
	if err != nil {
		t.Fatalf("stage guest images: %v", err)
	}
	if result.SubstrateRebuilt {
		t.Fatal("substrate rebuilt despite current digest")
	}
	if result.ToolchainStaged {
		t.Fatal("toolchain images staged despite current digest")
	}
}

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
