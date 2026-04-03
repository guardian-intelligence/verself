package ci

import (
	"encoding/json"
	"strings"
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
		RunID: "fixtures-pass-20260401-063752",
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
			VMExitWaitDuration:   320 * time.Millisecond,
			Logs:                 "build ok\nError: FORGE_METAL_FIXTURE_EXPECTED_TEST_FAILURE\n",
			StdoutBytes:          876,
			StderrBytes:          13,
			PhaseResults: []firecracker.PhaseResult{
				{Name: "prepare", ExitCode: 0, DurationMS: 2000},
				{Name: "run", ExitCode: 1, DurationMS: 11000},
			},
			FailurePhase: "run",
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
	if payload["event_kind"] != "exec" {
		t.Fatalf("event_kind: got %v", payload["event_kind"])
	}
	if payload["shutdown_mode"] != "guest_reboot_k" {
		t.Fatalf("shutdown_mode: got %v", payload["shutdown_mode"])
	}
	if payload["vm_exit_wait_ns"] != float64((320 * time.Millisecond).Nanoseconds()) {
		t.Fatalf("vm_exit_wait_ns: got %v", payload["vm_exit_wait_ns"])
	}
	if payload["vm_exit_forced"] != false {
		t.Fatalf("vm_exit_forced: got %v", payload["vm_exit_forced"])
	}
	if payload["failure_phase"] != "run" {
		t.Fatalf("failure_phase: got %v", payload["failure_phase"])
	}
	if payload["failure_exit_code"] != float64(1) {
		t.Fatalf("failure_exit_code: got %v", payload["failure_exit_code"])
	}
	if !strings.Contains(payload["guest_log_tail"].(string), "FORGE_METAL_FIXTURE_EXPECTED_TEST_FAILURE") {
		t.Fatalf("guest_log_tail missing expected sentinel: %v", payload["guest_log_tail"])
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

func TestBuildWarmJobConfigJSONIncludesFilesystemGateTelemetry(t *testing.T) {
	input := emitWarmTelemetryInput{
		Request: WarmRequest{
			Repo:          "forge-admin-usj5/next-bun-monorepo",
			RepoURL:       "http://127.0.0.1:3000/forge-admin-usj5/next-bun-monorepo.git",
			DefaultBranch: "main",
			RunID:         "fixtures-pass-20260401-072318",
		},
		RunID:           "fixtures-pass-20260401-072318-warm",
		ParentRunID:     "fixtures-pass-20260401-072318",
		Manifest:        &Manifest{Version: 1, WorkDir: ".", Profile: RuntimeProfileNode},
		Toolchain:       &Toolchain{PackageManager: PackageManagerBun, PackageManagerVersion: "1.2.20", NodeVersion: "22.14.0"},
		TargetDataset:   "benchpool/repo-goldens/next-bun-monorepo-1",
		PreviousDataset: "benchpool/repo-goldens/next-bun-monorepo-0",
		Job: firecracker.JobConfig{
			JobID:          "5c0e6fd6-d718-4b52-abcd-1234567890ab",
			RunCommand:     []string{"bun", "run", "warm"},
			RunWorkDir:     "/workspace",
			PrepareCommand: []string{"bun", "install", "--frozen-lockfile"},
			PrepareWorkDir: "/workspace",
			Env:            map[string]string{"CI": "true"},
		},
		JobResult: firecracker.JobResult{
			BootToReadyDuration:  5 * time.Millisecond,
			PrepareDuration:      2 * time.Second,
			RunDuration:          3 * time.Second,
			ServiceStartDuration: 0,
			VMExitWaitDuration:   410 * time.Millisecond,
			StdoutBytes:          128,
			StderrBytes:          0,
		},
		FilesystemCheckDuration:   221 * time.Millisecond,
		SnapshotPromotionDuration: 9 * time.Millisecond,
		PreviousDestroyDuration:   4 * time.Millisecond,
		FilesystemCheckOK:         true,
		Promoted:                  true,
		CommitSHA:                 "367befa8562f50dfac64b5589e842a215598b90a",
	}
	artifacts := &GuestArtifactManifest{
		SchemaVersion:      1,
		RootfsSHA256:       "rootfs-sha",
		KernelSHA256:       "kernel-sha",
		PackageCount:       63,
		RootfsUsedBytes:    405045248,
		RootfsTreeBytes:    257605887,
		KernelBytes:        44279576,
		FirecrackerVersion: "1.15.0",
		GuestKernelVersion: "6.1.155",
	}

	data, err := buildWarmJobConfigJSON(input, "/var/lib/ci/guest-artifacts.json", artifacts, nil)
	if err != nil {
		t.Fatalf("buildWarmJobConfigJSON: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if payload["event_kind"] != "warm" {
		t.Fatalf("event_kind: got %v", payload["event_kind"])
	}
	if payload["parent_run_id"] != "fixtures-pass-20260401-072318" {
		t.Fatalf("parent_run_id: got %v", payload["parent_run_id"])
	}
	if payload["filesystem_check_ok"] != true {
		t.Fatalf("filesystem_check_ok: got %v", payload["filesystem_check_ok"])
	}
	if payload["filesystem_check_ns"] != float64((221 * time.Millisecond).Nanoseconds()) {
		t.Fatalf("filesystem_check_ns: got %v", payload["filesystem_check_ns"])
	}
	if payload["shutdown_mode"] != "guest_reboot_k" {
		t.Fatalf("shutdown_mode: got %v", payload["shutdown_mode"])
	}
	if payload["vm_exit_wait_ns"] != float64((410 * time.Millisecond).Nanoseconds()) {
		t.Fatalf("vm_exit_wait_ns: got %v", payload["vm_exit_wait_ns"])
	}
	if payload["vm_exit_forced"] != false {
		t.Fatalf("vm_exit_forced: got %v", payload["vm_exit_forced"])
	}
}

func TestWarmRunIDs(t *testing.T) {
	runID, parent := warmRunIDs("fixtures-pass-20260401-072318")
	if runID != "fixtures-pass-20260401-072318-warm" || parent != "fixtures-pass-20260401-072318" {
		t.Fatalf("warmRunIDs explicit: got run_id=%q parent=%q", runID, parent)
	}
	runID, parent = warmRunIDs("")
	if parent != "" || runID == "" || runID == "-warm" {
		t.Fatalf("warmRunIDs generated: got run_id=%q parent=%q", runID, parent)
	}
}
