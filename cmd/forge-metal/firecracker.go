package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/forge-metal/forge-metal/internal/firecracker"
)

func firecrackerTestCmd() *cobra.Command {
	var (
		repo            string
		commitSHA       string
		pool            string
		goldenZvol      string
		kernelPath      string
		fcBin           string
		jailerBin       string
		vcpus           int
		memoryMiB       int
		timeout         string
		hostInterface   string
		guestPoolCIDR   string
		networkLeaseDir string
	)

	cmd := &cobra.Command{
		Use:   "firecracker-test -- <command> [args...]",
		Short: "Run a command in a Firecracker VM and print runtime metrics",
		Long: `Standalone Firecracker runtime test.

Clones a golden zvol, boots a Firecracker VM, runs the command inside it,
captures output and metrics, and prints a wide event summary.

The command and its arguments are passed as positional args after --.

Examples:
  forge-metal firecracker-test -- node -e 'console.log(42)'
  forge-metal firecracker-test -- bash -c 'echo hello && sleep 1'
  forge-metal firecracker-test --vcpus 4 --memory 1024 -- npm test`,
		Args:                  cobra.MinimumNArgs(1),
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}))

			dur, err := time.ParseDuration(timeout)
			if err != nil {
				return fmt.Errorf("parse timeout: %w", err)
			}

			cfg := firecracker.DefaultConfig()
			if pool != "" {
				cfg.Pool = pool
			}
			if goldenZvol != "" {
				cfg.GoldenZvol = goldenZvol
			}
			if kernelPath != "" {
				cfg.KernelPath = kernelPath
			}
			if fcBin != "" {
				cfg.FirecrackerBin = fcBin
			}
			if jailerBin != "" {
				cfg.JailerBin = jailerBin
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

			orch := firecracker.New(cfg, logger)

			jobID := uuid.New().String()

			job := firecracker.JobConfig{
				JobID:      jobID,
				RunCommand: args, // positional args after --
				RunWorkDir: "/workspace",
				Env: map[string]string{
					"CI":   "true",
					"REPO": repo,
				},
			}
			if commitSHA != "" {
				job.Env["COMMIT_SHA"] = commitSHA
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

			logger.Info("starting firecracker test",
				"job_id", jobID,
				"command", args,
			)

			result, err := orch.Run(ctx, job)
			if err != nil {
				return fmt.Errorf("firecracker run: %w", err)
			}

			// Print results.
			fmt.Println()
			fmt.Println("=== Firecracker Results ===")
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
				fmt.Println("=== Serial Console Output ===")
				fmt.Println(result.Logs)
			}

			jobJSON, _ := json.Marshal(job)
			fmt.Println()
			fmt.Println("=== Wide Event (ClickHouse) ===")
			fmt.Printf("job_id:            %s\n", jobID)
			fmt.Printf("vm_boot_time_us:   %d\n", result.Metrics.BootTimeUs)
			fmt.Printf("vm_exit_code:      %d\n", result.ExitCode)
			fmt.Printf("zfs_written_bytes: %d\n", result.ZFSWritten)
			fmt.Printf("job_config_json:   %s\n", string(jobJSON))

			if result.ExitCode != 0 {
				os.Exit(result.ExitCode)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&repo, "repo", "", "Repository URL (metadata)")
	cmd.Flags().StringVar(&commitSHA, "commit", "", "Commit SHA (metadata)")
	cmd.Flags().StringVar(&pool, "pool", "", "ZFS pool name (default: benchpool)")
	cmd.Flags().StringVar(&goldenZvol, "golden-zvol", "", "Golden zvol name (default: golden-zvol)")
	cmd.Flags().StringVar(&kernelPath, "kernel", "", "Path to vmlinux (default: /var/lib/ci/vmlinux)")
	cmd.Flags().StringVar(&fcBin, "firecracker-bin", "", "Path to firecracker binary")
	cmd.Flags().StringVar(&jailerBin, "jailer-bin", "", "Path to jailer binary")
	cmd.Flags().IntVar(&vcpus, "vcpus", 0, "vCPU count (default: 2)")
	cmd.Flags().IntVar(&memoryMiB, "memory", 0, "Memory in MiB (default: 2048)")
	cmd.Flags().StringVar(&timeout, "timeout", "2m", "Job timeout")
	cmd.Flags().StringVar(&hostInterface, "host-interface", "", "Host uplink interface for guest egress (auto-detected)")
	cmd.Flags().StringVar(&guestPoolCIDR, "guest-pool-cidr", "", "IPv4 pool reserved for Firecracker guests")
	cmd.Flags().StringVar(&networkLeaseDir, "network-lease-dir", "", "Directory for persistent guest network leases")

	return cmd
}
