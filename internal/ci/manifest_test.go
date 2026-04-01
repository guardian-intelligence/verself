package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadManifest_FixturePNPM(t *testing.T) {
	root := filepath.Join("..", "..", "test", "fixtures", "next-pnpm-postgres")
	manifest, err := LoadManifest(root)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if manifest.RepoWorkDir() != "/workspace" {
		t.Fatalf("workdir: got %q", manifest.RepoWorkDir())
	}
	if len(manifest.Services) != 1 || manifest.Services[0] != "postgres" {
		t.Fatalf("services: got %+v", manifest.Services)
	}
	if got := strings.Join(manifest.Run, " "); got != "pnpm run ci" {
		t.Fatalf("run: got %q", got)
	}
	if got := strings.Join(manifest.ResolvedPrepare(), " "); got != "pnpm run ci" {
		t.Fatalf("prepare: got %q", got)
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
	if manifest.Profile != RuntimeProfileAuto {
		t.Fatalf("profile: got %q", manifest.Profile)
	}
}

func TestLoadManifest_DefaultsPrepareWorkdirEnvAndProfile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".forge-metal"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data := "version = 1\nrun = [\"npm\", \"test\"]\n"
	if err := os.WriteFile(filepath.Join(root, ".forge-metal", "ci.toml"), []byte(data), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	manifest, err := LoadManifest(root)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if manifest.WorkDir != "." {
		t.Fatalf("workdir: got %q", manifest.WorkDir)
	}
	if manifest.Profile != RuntimeProfileAuto {
		t.Fatalf("profile: got %q", manifest.Profile)
	}
	if len(manifest.Env) != 0 {
		t.Fatalf("env: got %+v", manifest.Env)
	}
	if got := strings.Join(manifest.ResolvedPrepare(), " "); got != "npm test" {
		t.Fatalf("prepare: got %q", got)
	}
}

func TestLoadManifest_RejectsUnsupportedService(t *testing.T) {
	root := t.TempDir()
	writeManifestForTest(t, root, "version = 1\nrun = [\"npm\", \"test\"]\nservices = [\"redis\"]\n")

	_, err := LoadManifest(root)
	if err == nil || !strings.Contains(err.Error(), `unsupported service "redis"`) {
		t.Fatalf("LoadManifest error: %v", err)
	}
}

func TestLoadManifest_RejectsUnsupportedProfile(t *testing.T) {
	root := t.TempDir()
	writeManifestForTest(t, root, "version = 1\nrun = [\"npm\", \"test\"]\nprofile = \"python\"\n")

	_, err := LoadManifest(root)
	if err == nil || !strings.Contains(err.Error(), `unsupported manifest profile "python"`) {
		t.Fatalf("LoadManifest error: %v", err)
	}
}

func TestLoadManifest_RejectsInvalidEnvName(t *testing.T) {
	root := t.TempDir()
	writeManifestForTest(t, root, "version = 1\nrun = [\"npm\", \"test\"]\nenv = [\"BAD-NAME\"]\n")

	_, err := LoadManifest(root)
	if err == nil || !strings.Contains(err.Error(), `invalid env name "BAD-NAME"`) {
		t.Fatalf("LoadManifest error: %v", err)
	}
}

func writeManifestForTest(t *testing.T, root, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".forge-metal"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".forge-metal", "ci.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}
