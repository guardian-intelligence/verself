package controllerstate

import (
	"path/filepath"
	"testing"
)

func TestConfigPathUsesControllerRootEnv(t *testing.T) {
	root := filepath.Join(t.TempDir(), "controller")
	t.Setenv(RootEnv, root)

	got, err := ConfigPath("/unused/repo", "object-storage/gamma.token")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "object-storage", "gamma.token")
	if got != want {
		t.Fatalf("ConfigPath() = %q, want %q", got, want)
	}
}

func TestConfigPathDefaultsToRepoControllerState(t *testing.T) {
	repo := t.TempDir()

	got, err := ConfigPath(repo, "object-storage/gamma.token")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(repo, ".verself", "controller", "object-storage", "gamma.token")
	if got != want {
		t.Fatalf("ConfigPath() = %q, want %q", got, want)
	}
}

func TestConfigPathRejectsCheckoutRelativeControllerPaths(t *testing.T) {
	if _, err := ConfigPath(t.TempDir(), ".verself/controller/object-storage/gamma.token"); err == nil {
		t.Fatal("expected checked-out .verself path to fail")
	}
}

func TestRootRejectsRelativeRootEnv(t *testing.T) {
	t.Setenv(RootEnv, "relative")

	if _, err := Root(t.TempDir()); err == nil {
		t.Fatal("expected relative controller root env to fail")
	}
}
