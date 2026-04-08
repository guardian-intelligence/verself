package ci

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	vmorchestrator "github.com/forge-metal/vm-orchestrator"
)

const defaultGuestArtifactManifestDir = "/var/lib/ci"

// GuestArtifactManifest captures the built guest artifact footprint that is
// shared by Firecracker jobs on a worker.
type GuestArtifactManifest struct {
	SchemaVersion           int    `json:"schema_version"`
	BuiltAtUTC              string `json:"built_at_utc,omitempty"`
	AlpineVersion           string `json:"alpine_version,omitempty"`
	FirecrackerVersion      string `json:"firecracker_version,omitempty"`
	GuestKernelVersion      string `json:"guest_kernel_version,omitempty"`
	RootfsSHA256            string `json:"rootfs_sha256,omitempty"`
	RootfsTreeBytes         uint64 `json:"rootfs_tree_bytes,omitempty"`
	RootfsApparentBytes     uint64 `json:"rootfs_apparent_bytes,omitempty"`
	RootfsAllocatedBytes    uint64 `json:"rootfs_allocated_bytes,omitempty"`
	RootfsFilesystemBytes   uint64 `json:"rootfs_filesystem_bytes,omitempty"`
	RootfsUsedBytes         uint64 `json:"rootfs_used_bytes,omitempty"`
	RootfsFreeBytes         uint64 `json:"rootfs_free_bytes,omitempty"`
	KernelSHA256            string `json:"kernel_sha256,omitempty"`
	KernelBytes             uint64 `json:"kernel_bytes,omitempty"`
	SBOMSHA256              string `json:"sbom_sha256,omitempty"`
	SBOMBytes               uint64 `json:"sbom_bytes,omitempty"`
	PackageCount            uint32 `json:"package_count,omitempty"`
	InitSHA256              string `json:"init_sha256,omitempty"`
	InitBytes               uint64 `json:"init_bytes,omitempty"`
	VMGuestTelemetryPresent bool   `json:"vm_guest_telemetry_present,omitempty"`
	VMGuestTelemetrySHA256  string `json:"vm_guest_telemetry_sha256,omitempty"`
	VMGuestTelemetryBytes   uint64 `json:"vm_guest_telemetry_bytes,omitempty"`
}

func guestArtifactManifestPath(cfg vmorchestrator.Config) string {
	dir := filepath.Dir(cfg.KernelPath)
	if dir == "" || dir == "." {
		dir = defaultGuestArtifactManifestDir
	}
	return filepath.Join(dir, "guest-artifacts.json")
}

func loadGuestArtifactManifest(path string) (*GuestArtifactManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read guest artifact manifest %s: %w", path, err)
	}
	var manifest GuestArtifactManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode guest artifact manifest %s: %w", path, err)
	}
	return &manifest, nil
}
