//go:build integration

package firecracker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type smelterSnapshot struct {
	SchemaVersion    int         `json:"schema_version"`
	JailerRoot       string      `json:"jailer_root"`
	GuestPort        uint32      `json:"guest_port"`
	SamplePeriodMS   uint32      `json:"sample_period_ms"`
	ObservedAtUnixMS int64       `json:"observed_at_unix_ms"`
	VMs              []smelterVM `json:"vms"`
}

type smelterVM struct {
	JobID            string         `json:"job_id"`
	UDSPath          string         `json:"uds_path"`
	Present          bool           `json:"present"`
	WorkerActive     bool           `json:"worker_active"`
	Connected        bool           `json:"connected"`
	LastUpdateUnixMS int64          `json:"last_update_unix_ms"`
	LastError        *string        `json:"last_error"`
	Hello            *smelterHello  `json:"hello"`
	Sample           *smelterSample `json:"sample"`
}

type smelterHello struct {
	Seq            uint32 `json:"seq"`
	Flags          uint32 `json:"flags"`
	MonoNS         uint64 `json:"mono_ns"`
	WallNS         uint64 `json:"wall_ns"`
	SamplePeriodMS uint32 `json:"sample_period_ms"`
	GuestPort      uint32 `json:"guest_port"`
	BootID         string `json:"boot_id"`
	NetIface       string `json:"net_iface"`
	BlockDev       string `json:"block_dev"`
}

type smelterSample struct {
	Seq            uint32 `json:"seq"`
	Flags          uint32 `json:"flags"`
	MonoNS         uint64 `json:"mono_ns"`
	WallNS         uint64 `json:"wall_ns"`
	CPUUserTicks   uint64 `json:"cpu_user_ticks"`
	CPUSystemTicks uint64 `json:"cpu_system_ticks"`
	CPUIdleTicks   uint64 `json:"cpu_idle_ticks"`
	Load1Centis    uint32 `json:"load1_centis"`
	Load5Centis    uint32 `json:"load5_centis"`
	Load15Centis   uint32 `json:"load15_centis"`
	ProcsRunning   uint16 `json:"procs_running"`
	ProcsBlocked   uint16 `json:"procs_blocked"`
	MemTotalKB     uint64 `json:"mem_total_kb"`
	MemAvailableKB uint64 `json:"mem_available_kb"`
	IOReadBytes    uint64 `json:"io_read_bytes"`
	IOWriteBytes   uint64 `json:"io_write_bytes"`
	NetRXBytes     uint64 `json:"net_rx_bytes"`
	NetTXBytes     uint64 `json:"net_tx_bytes"`
	PSICPUPct100   uint16 `json:"psi_cpu_pct100"`
	PSIMemPct100   uint16 `json:"psi_mem_pct100"`
	PSIIOPct100    uint16 `json:"psi_io_pct100"`
}

func TestHomesteadSmelterReportsRunningVMs(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Fatal("integration test requires root; run via sudo")
	}
	if _, err := exec.LookPath("zig"); err != nil {
		t.Skip("zig not available in PATH")
	}

	cfg := DefaultConfig()
	cfg.VCPUs = 1
	cfg.MemoryMiB = 512

	requireFirecrackerIntegrationPrereqs(t, cfg)

	smelterBin := buildHomesteadSmelterHostBinary(t)
	controlSock := filepath.Join(t.TempDir(), "homestead-smelter.sock")
	jailerScanRoot := filepath.Join(cfg.JailerRoot, "firecracker")

	var hostLogs bytes.Buffer
	smelterCmd := exec.Command(smelterBin,
		"serve",
		"--listen-uds", controlSock,
		"--jailer-root", jailerScanRoot,
		"--port", "10790",
	)
	smelterCmd.Stdout = &hostLogs
	smelterCmd.Stderr = &hostLogs
	if err := smelterCmd.Start(); err != nil {
		t.Fatalf("start homestead-smelter host: %v", err)
	}
	defer stopProcess(t, smelterCmd)

	waitForSmelterPing(t, controlSock, 10*time.Second, &hostLogs)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	orch := New(cfg, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	jobIDs := []string{uuid.NewString(), uuid.NewString()}
	results := make(chan runOutcome, len(jobIDs))

	for _, jobID := range jobIDs {
		job := JobConfig{
			JobID:      jobID,
			RunCommand: []string{"sh", "-lc", "echo smelter-start && sleep 6 && echo smelter-done"},
			RunWorkDir: "/workspace",
			Env: map[string]string{
				"CI": "true",
			},
		}

		go func(job JobConfig) {
			result, err := orch.Run(ctx, job)
			results <- runOutcome{jobID: job.JobID, result: result, err: err}
		}(job)
	}

	if _, err := waitForRunningJobLeases(ctx, cfg.NetworkLeaseDir, jobIDs, len(jobIDs)); err != nil {
		cancel()
		t.Fatalf("wait for running VMs: %v", err)
	}

	snapshot := waitForSmelterJobs(t, ctx, controlSock, jobIDs, &hostLogs)
	if snapshot.GuestPort != 10790 {
		t.Fatalf("unexpected guest port in snapshot: %+v", snapshot)
	}
	if snapshot.SamplePeriodMS != 500 {
		t.Fatalf("unexpected sample period in snapshot: %+v", snapshot)
	}
	if snapshot.JailerRoot != jailerScanRoot {
		t.Fatalf("unexpected jailer root in snapshot: %+v", snapshot)
	}

	for _, jobID := range jobIDs {
		vm, ok := smelterVMByID(snapshot, jobID)
		if !ok {
			t.Fatalf("snapshot missing job %s: %+v", jobID, snapshot)
		}
		if !vm.Present || !vm.WorkerActive || !vm.Connected {
			t.Fatalf("snapshot reported inactive VM for %s: %+v", jobID, vm)
		}
		if vm.LastError != nil {
			t.Fatalf("snapshot reported error for %s: %+v", jobID, vm)
		}
		if vm.Hello == nil || vm.Sample == nil {
			t.Fatalf("snapshot missing telemetry for %s: %+v", jobID, vm)
		}
		if vm.Hello.GuestPort != 10790 || vm.Hello.SamplePeriodMS != 500 {
			t.Fatalf("snapshot reported unexpected hello metadata for %s: %+v", jobID, vm.Hello)
		}
		if vm.Hello.BootID == "" || vm.Hello.NetIface != "eth0" || vm.Hello.BlockDev != "vda" {
			t.Fatalf("snapshot reported unexpected hello identity for %s: %+v", jobID, vm.Hello)
		}
		if vm.Sample.Seq <= vm.Hello.Seq {
			t.Fatalf("snapshot sample did not advance past hello for %s: %+v / %+v", jobID, vm.Hello, vm.Sample)
		}
		if vm.Sample.MemTotalKB == 0 || vm.Sample.MemAvailableKB == 0 {
			t.Fatalf("snapshot reported invalid memory sample for %s: %+v", jobID, vm.Sample)
		}
		if vm.Sample.WallNS == 0 || vm.Sample.MonoNS == 0 {
			t.Fatalf("snapshot reported invalid timestamps for %s: %+v", jobID, vm.Sample)
		}
	}

	for range jobIDs {
		outcome := <-results
		if outcome.err != nil {
			t.Fatalf("job %s failed: %v", outcome.jobID, outcome.err)
		}
		if outcome.result.ExitCode != 0 {
			t.Fatalf("job %s exited with %d\nlogs:\n%s\nhost logs:\n%s", outcome.jobID, outcome.result.ExitCode, outcome.result.Logs, hostLogs.String())
		}
	}
}

func buildHomesteadSmelterHostBinary(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	smelterDir := filepath.Clean(filepath.Join(wd, "..", "..", "homestead-smelter"))

	cmd := exec.Command("zig", "build", "-Doptimize=ReleaseSafe", "-Dtarget=x86_64-linux-musl")
	cmd.Dir = smelterDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build homestead-smelter host: %v\n%s", err, string(output))
	}

	return filepath.Join(smelterDir, "zig-out", "bin", "homestead-smelter-host")
}

func stopProcess(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
}

func waitForSmelterPing(t *testing.T, controlSock string, timeout time.Duration, hostLogs *bytes.Buffer) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", controlSock, 250*time.Millisecond)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if _, err := io.WriteString(conn, "PING\n"); err != nil {
			conn.Close()
			time.Sleep(100 * time.Millisecond)
			continue
		}

		line, err := bufio.NewReader(conn).ReadString('\n')
		conn.Close()
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if strings.TrimSpace(line) == "PONG homestead-smelter-host" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("homestead-smelter host did not answer ping\nhost logs:\n%s", hostLogs.String())
}

func waitForSmelterJobs(t *testing.T, ctx context.Context, controlSock string, jobIDs []string, hostLogs *bytes.Buffer) smelterSnapshot {
	t.Helper()

	for {
		snapshot, err := smelterSnapshotRequest(controlSock)
		if err == nil && snapshotContainsJobs(snapshot, jobIDs) {
			return snapshot
		}

		select {
		case <-ctx.Done():
			if err != nil {
				t.Fatalf("wait for smelter snapshot: %v\nhost logs:\n%s", err, hostLogs.String())
			}
			t.Fatalf("wait for smelter snapshot timed out: %+v\nhost logs:\n%s", snapshot, hostLogs.String())
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func smelterSnapshotRequest(controlSock string) (smelterSnapshot, error) {
	conn, err := net.DialTimeout("unix", controlSock, 500*time.Millisecond)
	if err != nil {
		return smelterSnapshot{}, err
	}
	defer conn.Close()

	if _, err := io.WriteString(conn, "SNAPSHOT\n"); err != nil {
		return smelterSnapshot{}, err
	}

	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return smelterSnapshot{}, err
	}

	var snapshot smelterSnapshot
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &snapshot); err != nil {
		return smelterSnapshot{}, fmt.Errorf("decode snapshot: %w", err)
	}
	return snapshot, nil
}

func snapshotContainsJobs(snapshot smelterSnapshot, jobIDs []string) bool {
	for _, jobID := range jobIDs {
		vm, ok := smelterVMByID(snapshot, jobID)
		if !ok || !vm.Present || !vm.WorkerActive || !vm.Connected || vm.Hello == nil || vm.Sample == nil {
			return false
		}
	}
	return true
}

func smelterVMByID(snapshot smelterSnapshot, jobID string) (smelterVM, bool) {
	for _, vm := range snapshot.VMs {
		if vm.JobID == jobID {
			return vm, true
		}
	}
	return smelterVM{}, false
}
