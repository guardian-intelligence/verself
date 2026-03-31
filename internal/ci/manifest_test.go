package ci

import (
	"path/filepath"
	"testing"
)

func TestLoadManifest_FixturePNPM(t *testing.T) {
	root := filepath.Join("..", "..", "test", "fixtures", "next-pnpm-postgres")
	manifest, err := LoadManifest(root)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if manifest.RepoName != "next-pnpm-postgres" {
		t.Fatalf("repo_name: got %q", manifest.RepoName)
	}
	if manifest.RepoWorkDir() != "/workspace" {
		t.Fatalf("workdir: got %q", manifest.RepoWorkDir())
	}
	if len(manifest.Services) != 1 || manifest.Services[0] != "postgres" {
		t.Fatalf("services: got %+v", manifest.Services)
	}
}

func TestLoadManifest_FixtureBun(t *testing.T) {
	root := filepath.Join("..", "..", "test", "fixtures", "next-bun-monorepo")
	manifest, err := LoadManifest(root)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if manifest.RepoWorkDir() != "/workspace/apps/web" {
		t.Fatalf("workdir: got %q", manifest.RepoWorkDir())
	}
	if len(manifest.Services) != 0 {
		t.Fatalf("services: got %+v", manifest.Services)
	}
}
