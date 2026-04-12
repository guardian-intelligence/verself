// vm-run is a thin CLI client for the vm-orchestrator gRPC daemon.
// It runs an arbitrary command inside a Firecracker VM and prints
// timing metrics and guest output.
//
// Usage:
//
//	vm-run [flags] -- <command> [args...]
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
	"github.com/forge-metal/vm-orchestrator/vmproto"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var (
		apiSocket        string
		repo             string
		commitSHA        string
		timeout          string
		traceGuestEvents bool
		checkpointRefs   repeatedFlag
	)

	flag.StringVar(&apiSocket, "api-socket", vmorchestrator.DefaultSocketPath, "Unix socket path for vm-orchestrator")
	flag.StringVar(&repo, "repo", "", "Repository URL (metadata)")
	flag.StringVar(&commitSHA, "commit", "", "Commit SHA (metadata)")
	flag.StringVar(&timeout, "timeout", "2m", "Run timeout")
	flag.BoolVar(&traceGuestEvents, "trace-guest-events", false, "Stream host-derived run phase events")
	flag.Var(&checkpointRefs, "checkpoint-save-ref", "Checkpoint ref the guest may save; repeatable")
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

	runID := uuid.New().String()
	spec := vmorchestrator.HostRunSpec{
		RunID:              runID,
		RunCommand:         args,
		RunWorkDir:         "/workspace",
		BillablePhases:     []string{"run"},
		CheckpointSaveRefs: []string(checkpointRefs),
		Env: map[string]string{
			"REPO":                   repo,
			"FORGE_METAL_ATTEMPT_ID": runID,
		},
	}
	if commitSHA != "" {
		spec.Env["COMMIT_SHA"] = commitSHA
	}

	logger.Info("starting VM run", "run_id", runID, "command", args)

	var result vmorchestrator.RunResult
	if traceGuestEvents {
		ensuredRunID, _, err := client.EnsureRun(ctx, spec)
		if err != nil {
			return fmt.Errorf("run ensure: %w", err)
		}
		if ensuredRunID != runID {
			return fmt.Errorf("ensured run id mismatch: got %s want %s", ensuredRunID, runID)
		}

		streamErrCh := make(chan error, 1)
		go func() {
			streamErrCh <- client.StreamRunEvents(ctx, runID, 0, true, func(event vmorchestrator.HostRunEvent) error {
				attrsJSON, err := json.Marshal(event.Attrs)
				if err != nil {
					return fmt.Errorf("marshal run event attrs: %w", err)
				}
				fmt.Printf("[run-event] seq=%d type=%s attrs=%s\n", event.Seq, event.EventType, string(attrsJSON))
				return nil
			})
		}()

		snapshot, err := client.WaitRun(ctx, runID, true)
		if err != nil {
			return fmt.Errorf("run wait: %w", err)
		}
		if snapshot.Result == nil {
			return fmt.Errorf("run %s completed without result", runID)
		}
		result = *snapshot.Result

		if err := <-streamErrCh; err != nil {
			return fmt.Errorf("run event stream: %w", err)
		}
	} else {
		runResult, err := client.Run(ctx, spec)
		if err != nil {
			return fmt.Errorf("run execute: %w", err)
		}
		result = runResult
	}

	fmt.Println()
	fmt.Println("=== VM Results ===")
	fmt.Printf("Run ID:         %s\n", runID)
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

	specJSON, _ := json.Marshal(spec)
	fmt.Println()
	fmt.Println("=== Wide Event (ClickHouse) ===")
	fmt.Printf("run_id:            %s\n", runID)
	if result.Metrics != nil {
		fmt.Printf("vm_boot_time_us:   %d\n", result.Metrics.BootTimeUs)
	}
	fmt.Printf("vm_exit_code:      %d\n", result.ExitCode)
	fmt.Printf("zfs_written_bytes: %d\n", result.ZFSWritten)
	fmt.Printf("run_spec_json:     %s\n", string(specJSON))

	if result.ExitCode != 0 {
		os.Exit(result.ExitCode)
	}
	return nil
}

type repeatedFlag []string

func (f *repeatedFlag) String() string {
	if f == nil {
		return ""
	}
	return fmt.Sprint([]string(*f))
}

func (f *repeatedFlag) Set(value string) error {
	if err := vmproto.ValidateCheckpointRef(value); err != nil {
		return err
	}
	*f = append(*f, value)
	return nil
}
