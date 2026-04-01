package ci

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/forge-metal/forge-metal/internal/firecracker"
)

func TestBuildExecJobConfigJSONIncludesGuestArtifactMetrics(t *testing.T) {
	input := emitExecTelemetryInput{
		Request: ExecRequest{
			Repo: "forge-admin-usj5/next-bun-monorepo",
			Ref:  "refs/pull/82/head",
		},
		RunID: "fixtures-e2e-20260401-063752",
		Manifest: &Manifest{
			Version:  1,
			WorkDir:  ".",
			Services: []string{"postgres"},
			Env:      []string{"DATABASE_URL"},
			Profile:  RuntimeProfileNode,
		},
		Toolchain: &Toolchain{
			PackageManager:        PackageManagerBun,
			PackageManagerVersion: "1.2.20",
			NodeVersion:           "22.14.0",
		},
		InstallNeeded: true,
		Job: firecracker.JobConfig{
			PrepareCommand: []string{"bun", "install", "--frozen-lockfile"},
			PrepareWorkDir: "/workspace",
			RunCommand:     []string{"bun", "run", "ci"},
			RunWorkDir:     "/workspace/apps/web",
			Services:       []string{"postgres"},
			Env:            map[string]string{"DATABASE_URL": "postgres://fixture", "CI": "true"},
		},
		JobResult: firecracker.JobResult{
			BootToReadyDuration:  5 * time.Millisecond,
			ServiceStartDuration: 147 * time.Millisecond,
			PrepareDuration:      2 * time.Second,
			RunDuration:          11 * time.Second,
			StdoutBytes:          876,
			StderrBytes:          13,
		},
		CommitSHA: "367befa8562f50dfac64b5589e842a215598b90a",
		PRNumber:  82,
	}
	artifacts := &GuestArtifactManifest{
		SchemaVersion:         1,
		AlpineVersion:         "3.21.6",
		FirecrackerVersion:    "1.15.0",
		GuestKernelVersion:    "6.1.155",
		RootfsSHA256:          "rootfs-sha",
		RootfsTreeBytes:       734003200,
		RootfsApparentBytes:   4294967296,
		RootfsAllocatedBytes:  905969664,
		RootfsFilesystemBytes: 4294967296,
		RootfsUsedBytes:       801112064,
		RootfsFreeBytes:       3493855232,
		KernelSHA256:          "kernel-sha",
		KernelBytes:           35651584,
		SBOMSHA256:            "sbom-sha",
		SBOMBytes:             4096,
		PackageCount:          137,
		InitBytes:             5341184,
	}

	data, err := buildExecJobConfigJSON(input, "/var/lib/ci/guest-artifacts.json", artifacts, nil)
	if err != nil {
		t.Fatalf("buildExecJobConfigJSON: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if payload["runtime_protocol"] != "vsock-v1" {
		t.Fatalf("runtime_protocol: got %v", payload["runtime_protocol"])
	}
	if payload["guest_artifact_manifest_present"] != true {
		t.Fatalf("guest_artifact_manifest_present: got %v", payload["guest_artifact_manifest_present"])
	}
	if payload["guest_rootfs_used_bytes"] != float64(801112064) {
		t.Fatalf("guest_rootfs_used_bytes: got %v", payload["guest_rootfs_used_bytes"])
	}
	if payload["guest_kernel_bytes"] != float64(35651584) {
		t.Fatalf("guest_kernel_bytes: got %v", payload["guest_kernel_bytes"])
	}
	if payload["guest_package_count"] != float64(137) {
		t.Fatalf("guest_package_count: got %v", payload["guest_package_count"])
	}
}
