package workload

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDetectToolchain_FixturePNPM(t *testing.T) {
	root := filepath.Join("..", "platform", "test", "fixtures", "next-pnpm-postgres")
	tc, err := DetectToolchain(root)
	if err != nil {
		t.Fatalf("DetectToolchain: %v", err)
	}
	if tc.PackageManager != PackageManagerPNPM {
		t.Fatalf("package manager: got %q, want %q", tc.PackageManager, PackageManagerPNPM)
	}
	if tc.PackageManagerVersion != "9.15.0" {
		t.Fatalf("package manager version: got %q", tc.PackageManagerVersion)
	}
	if tc.LockfileRelPath != "pnpm-lock.yaml" {
		t.Fatalf("lockfile: got %q", tc.LockfileRelPath)
	}
}

func TestDetectToolchain_FixtureBun(t *testing.T) {
	root := filepath.Join("..", "platform", "test", "fixtures", "next-bun-monorepo")
	tc, err := DetectToolchain(root)
	if err != nil {
		t.Fatalf("DetectToolchain: %v", err)
	}
	if tc.PackageManager != PackageManagerBun {
		t.Fatalf("package manager: got %q, want %q", tc.PackageManager, PackageManagerBun)
	}
	if tc.PackageManagerVersion != "1.3.6" {
		t.Fatalf("package manager version: got %q", tc.PackageManagerVersion)
	}
	if tc.LockfileRelPath != "bun.lock" {
		t.Fatalf("lockfile: got %q", tc.LockfileRelPath)
	}
}

func TestDetectToolchain_FixtureNPMWorkspaces(t *testing.T) {
	root := filepath.Join("..", "platform", "test", "fixtures", "next-npm-workspaces")
	tc, err := DetectToolchain(root)
	if err != nil {
		t.Fatalf("DetectToolchain: %v", err)
	}
	if tc.PackageManager != PackageManagerNPM {
		t.Fatalf("package manager: got %q, want %q", tc.PackageManager, PackageManagerNPM)
	}
	if tc.PackageManagerVersion != "10.9.0" {
		t.Fatalf("package manager version: got %q", tc.PackageManagerVersion)
	}
	if tc.LockfileRelPath != "package-lock.json" {
		t.Fatalf("lockfile: got %q", tc.LockfileRelPath)
	}
}

func TestDetectToolchain_FixtureNPMSingleApp(t *testing.T) {
	root := filepath.Join("..", "platform", "test", "fixtures", "next-npm-single-app")
	tc, err := DetectToolchain(root)
	if err != nil {
		t.Fatalf("DetectToolchain: %v", err)
	}
	if tc.PackageManager != PackageManagerNPM {
		t.Fatalf("package manager: got %q, want %q", tc.PackageManager, PackageManagerNPM)
	}
	if tc.PackageManagerVersion != "10.9.0" {
		t.Fatalf("package manager version: got %q", tc.PackageManagerVersion)
	}
	if tc.LockfileRelPath != "package-lock.json" {
		t.Fatalf("lockfile: got %q", tc.LockfileRelPath)
	}
}

func TestDetectToolchain_NPMFromPackageManagerField(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{
  "name": "npm-fixture",
  "packageManager": "npm@10.9.0",
  "engines": { "node": "22.x" }
}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}

	tc, err := DetectToolchain(root)
	if err != nil {
		t.Fatalf("DetectToolchain: %v", err)
	}
	if tc.PackageManager != PackageManagerNPM {
		t.Fatalf("package manager: got %q, want %q", tc.PackageManager, PackageManagerNPM)
	}
	if tc.PackageManagerVersion != "10.9.0" {
		t.Fatalf("package manager version: got %q", tc.PackageManagerVersion)
	}
	if tc.NodeVersion != "22.x" {
		t.Fatalf("node version: got %q", tc.NodeVersion)
	}
}

func TestInstallCommand_PNPMUsesDirectArgv(t *testing.T) {
	tc := &Toolchain{
		PackageManager:        PackageManagerPNPM,
		PackageManagerVersion: "9.15.0",
	}
	want := []string{"npx", "--yes", "pnpm@9.15.0", "install", "--frozen-lockfile"}
	if got := tc.InstallCommand(); !reflect.DeepEqual(got, want) {
		t.Fatalf("InstallCommand: got %+v want %+v", got, want)
	}
}

func TestInstallCommand_BunUsesDirectArgv(t *testing.T) {
	tc := &Toolchain{PackageManager: PackageManagerBun}
	want := []string{"bun", "install", "--frozen-lockfile"}
	if got := tc.InstallCommand(); !reflect.DeepEqual(got, want) {
		t.Fatalf("InstallCommand: got %+v want %+v", got, want)
	}
}

func TestResolveCommand_PNPMUsesVersionedNPXLauncher(t *testing.T) {
	tc := &Toolchain{
		PackageManager:        PackageManagerPNPM,
		PackageManagerVersion: "9.15.0",
	}
	want := []string{"npx", "--yes", "pnpm@9.15.0", "run", "ci"}
	if got := tc.ResolveCommand([]string{"pnpm", "run", "ci"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveCommand: got %+v want %+v", got, want)
	}
}

func TestResolveCommand_PreservesExplicitShellCommands(t *testing.T) {
	tc := &Toolchain{
		PackageManager:        PackageManagerPNPM,
		PackageManagerVersion: "9.15.0",
	}
	want := []string{"bash", "-lc", "pnpm run ci"}
	if got := tc.ResolveCommand(want); !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveCommand shell: got %+v want %+v", got, want)
	}
}
