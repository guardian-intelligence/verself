// vm-run is a thin CLI client for the vm-orchestrator gRPC daemon.
// It runs an arbitrary command inside a Firecracker VM and prints
// timing metrics and guest output.
//
// Usage:
//
//	vm-run [flags] -- <command> [args...]
//	vm-run --golden-zvol vm-guest-telemetry-dev-zvol --timeout 60s -- sleep 30
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	vmorchestrator "github.com/forge-metal/vm-orchestrator"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var (
		apiSocket       string
		repo            string
		commitSHA       string
		goldenZvol      string
		vcpus           int
		memoryMiB       int
		timeout         string
		hostInterface   string
		guestPoolCIDR   string
		networkLeaseDir string
	)

	flag.StringVar(&apiSocket, "api-socket", vmorchestrator.DefaultSocketPath, "Unix socket path for vm-orchestrator")
	flag.StringVar(&repo, "repo", "", "Repository URL (metadata)")
	flag.StringVar(&commitSHA, "commit", "", "Commit SHA (metadata)")
	flag.StringVar(&goldenZvol, "golden-zvol", "", "Golden zvol name")
	flag.IntVar(&vcpus, "vcpus", 0, "vCPU count override")
	flag.IntVar(&memoryMiB, "memory", 0, "Memory in MiB override")
	flag.StringVar(&timeout, "timeout", "2m", "Job timeout")
	flag.StringVar(&hostInterface, "host-interface", "", "Host uplink interface for guest egress")
	flag.StringVar(&guestPoolCIDR, "guest-pool-cidr", "", "IPv4 pool reserved for Firecracker guests")
	flag.StringVar(&networkLeaseDir, "network-lease-dir", "", "Directory for persistent guest network leases")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		return fmt.Errorf("usage: vm-run [flags] -- <command> [args...]")
	}

	dur, err := time.ParseDuration(timeout)
	if err != nil {
		return fmt.Errorf("parse timeout: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg := vmorchestrator.Config{}
	if goldenZvol != "" {
		cfg.GoldenZvol = goldenZvol
	}
	if vcpus > 0 {
		cfg.VCPUs = vcpus
	}
	if memoryMiB > 0 {
		cfg.MemoryMiB = memoryMiB
	}
	cfg.HostInterface = hostInterface
	if guestPoolCIDR != "" {
		cfg.GuestPoolCIDR = guestPoolCIDR
	}
	if networkLeaseDir != "" {
		cfg.NetworkLeaseDir = networkLeaseDir
	}

	ctx, cancel := context.WithTimeout(context.Background(), dur)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("signal received, shutting down")
		cancel()
	}()

	client, err := vmorchestrator.NewClient(ctx, apiSocket)
	if err != nil {
		return err
	}
	defer client.Close()

	jobID := uuid.New().String()
	job := vmorchestrator.JobConfig{
		JobID:      jobID,
		RunCommand: args,
		RunWorkDir: "/workspace",
		Env: map[string]string{
			"CI":   "true",
			"REPO": repo,
		},
	}
	if commitSHA != "" {
		job.Env["COMMIT_SHA"] = commitSHA
	}

	logger.Info("starting VM job", "job_id", jobID, "command", args)

	result, err := client.RunWithConfig(ctx, cfg, job)
	if err != nil {
		return fmt.Errorf("vm run: %w", err)
	}

	fmt.Println()
	fmt.Println("=== VM Results ===")
	fmt.Printf("Job ID:         %s\n", jobID)
	fmt.Printf("Exit Code:      %d\n", result.ExitCode)
	fmt.Printf("Total Duration: %s\n", result.Duration.Round(time.Millisecond))
	fmt.Printf("  Clone:        %s\n", result.CloneTime.Round(time.Millisecond))
	fmt.Printf("  Jail Setup:   %s\n", result.JailSetupTime.Round(time.Millisecond))
	fmt.Printf("  VM Boot:      %s\n", result.VMBootTime.Round(time.Millisecond))
	fmt.Printf("  Cleanup:      %s\n", result.CleanupTime.Round(time.Millisecond))
	fmt.Printf("ZFS Written:    %d bytes\n", result.ZFSWritten)

	if result.Metrics != nil {
		fmt.Printf("VM Boot (FC):   %d us\n", result.Metrics.BootTimeUs)
		fmt.Printf("Block R/W:      %d / %d bytes\n",
			result.Metrics.BlockReadBytes, result.Metrics.BlockWriteBytes)
		fmt.Printf("Net RX/TX:      %d / %d bytes\n",
			result.Metrics.NetRxBytes, result.Metrics.NetTxBytes)
		fmt.Printf("vCPU Exits:     %d\n", result.Metrics.VCPUExitCount)
	}

	if result.Logs != "" {
		fmt.Println()
		fmt.Println("=== Guest Output ===")
		fmt.Println(result.Logs)
	}
	if result.SerialLogs != "" {
		fmt.Println()
		fmt.Println("=== Serial Diagnostics ===")
		fmt.Println(result.SerialLogs)
	}

	jobJSON, _ := json.Marshal(job)
	fmt.Println()
	fmt.Println("=== Wide Event (ClickHouse) ===")
	fmt.Printf("job_id:            %s\n", jobID)
	if result.Metrics != nil {
		fmt.Printf("vm_boot_time_us:   %d\n", result.Metrics.BootTimeUs)
	}
	fmt.Printf("vm_exit_code:      %d\n", result.ExitCode)
	fmt.Printf("zfs_written_bytes: %d\n", result.ZFSWritten)
	fmt.Printf("job_config_json:   %s\n", string(jobJSON))

	if result.ExitCode != 0 {
		os.Exit(result.ExitCode)
	}
	return nil
}
