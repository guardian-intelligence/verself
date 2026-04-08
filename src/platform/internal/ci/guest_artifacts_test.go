package ci

import (
	"os"
	"path/filepath"
	"testing"

	vmorchestrator "github.com/forge-metal/vm-orchestrator"
)

func TestGuestArtifactManifestPathUsesKernelDirectory(t *testing.T) {
	cfg := vmorchestrator.Config{KernelPath: "/var/lib/ci/vmlinux"}
	got := guestArtifactManifestPath(cfg)
	want := "/var/lib/ci/guest-artifacts.json"
	if got != want {
		t.Fatalf("guestArtifactManifestPath: got %q want %q", got, want)
	}
}

func TestGuestArtifactManifestPathFallsBackToDefaultDir(t *testing.T) {
	got := guestArtifactManifestPath(vmorchestrator.Config{})
	want := "/var/lib/ci/guest-artifacts.json"
	if got != want {
		t.Fatalf("guestArtifactManifestPath fallback: got %q want %q", got, want)
	}
}

func TestLoadGuestArtifactManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "guest-artifacts.json")
	if err := os.WriteFile(path, []byte(`{
  "schema_version": 1,
  "rootfs_tree_bytes": 1234,
  "rootfs_used_bytes": 2345,
  "kernel_bytes": 3456,
  "package_count": 42,
  "init_sha256": "init-sha",
  "vm_guest_telemetry_present": true,
  "vm_guest_telemetry_sha256": "telemetry-sha",
  "vm_guest_telemetry_bytes": 7890
}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	manifest, err := loadGuestArtifactManifest(path)
	if err != nil {
		t.Fatalf("loadGuestArtifactManifest: %v", err)
	}
	if manifest == nil {
		t.Fatal("loadGuestArtifactManifest: got nil manifest")
	}
	if manifest.RootfsTreeBytes != 1234 || manifest.RootfsUsedBytes != 2345 || manifest.KernelBytes != 3456 || manifest.PackageCount != 42 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if manifest.InitSHA256 != "init-sha" || !manifest.VMGuestTelemetryPresent || manifest.VMGuestTelemetrySHA256 != "telemetry-sha" || manifest.VMGuestTelemetryBytes != 7890 {
		t.Fatalf("unexpected manifest metadata: %+v", manifest)
	}
}

func TestLoadGuestArtifactManifestMissingFileReturnsNil(t *testing.T) {
	manifest, err := loadGuestArtifactManifest(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("loadGuestArtifactManifest missing file: %v", err)
	}
	if manifest != nil {
		t.Fatalf("loadGuestArtifactManifest missing file: got %+v want nil", manifest)
	}
}
