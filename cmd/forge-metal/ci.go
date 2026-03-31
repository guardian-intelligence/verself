package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	ci "github.com/forge-metal/forge-metal/internal/ci"
	"github.com/forge-metal/forge-metal/internal/firecracker"
)

func ciCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ci",
		Short: "Warm and execute repo-specific CI goldens",
	}
	cmd.AddCommand(ciWarmCmd())
	cmd.AddCommand(ciExecCmd())
	return cmd
}

func ciWarmCmd() *cobra.Command {
	var (
		repo            string
		forgejoURL      string
		defaultBranch   string
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
		Use:   "warm",
		Short: "Build or refresh a repo golden from its default branch",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := ciFirecrackerConfig(pool, goldenZvol, kernelPath, fcBin, jailerBin, vcpus, memoryMiB, hostInterface, guestPoolCIDR, networkLeaseDir)
			if err != nil {
				return err
			}
			dur, err := time.ParseDuration(timeout)
			if err != nil {
				return err
			}

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
			manager := ci.NewManager(cfg, logger)

			ctx, cancel := context.WithTimeout(context.Background(), dur)
			defer cancel()
			handleSignals(cancel, logger)

			repoURL := forgejoRepoURL(forgejoURL, repo)
			return manager.Warm(ctx, ci.WarmRequest{
				Repo:          repo,
				RepoURL:       repoURL,
				DefaultBranch: defaultBranch,
			})
		},
	}

	cmd.Flags().StringVar(&repo, "repo", "", "Repository in owner/name form")
	cmd.Flags().StringVar(&forgejoURL, "forgejo-url", "http://127.0.0.1:3000", "Forgejo base URL")
	cmd.Flags().StringVar(&defaultBranch, "default-branch", "main", "Default branch to warm")
	addFirecrackerFlags(cmd, &pool, &goldenZvol, &kernelPath, &fcBin, &jailerBin, &vcpus, &memoryMiB, &timeout, &hostInterface, &guestPoolCIDR, &networkLeaseDir)
	_ = cmd.MarkFlagRequired("repo")
	return cmd
}

func ciExecCmd() *cobra.Command {
	var (
		repo            string
		ref             string
		runID           string
		forgejoURL      string
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
		Use:   "exec",
		Short: "Execute a repo ref from its warmed golden image",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := ciFirecrackerConfig(pool, goldenZvol, kernelPath, fcBin, jailerBin, vcpus, memoryMiB, hostInterface, guestPoolCIDR, networkLeaseDir)
			if err != nil {
				return err
			}
			dur, err := time.ParseDuration(timeout)
			if err != nil {
				return err
			}

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
			manager := ci.NewManager(cfg, logger)

			ctx, cancel := context.WithTimeout(context.Background(), dur)
			defer cancel()
			handleSignals(cancel, logger)

			result, err := manager.Exec(ctx, ci.ExecRequest{
				Repo:    repo,
				RepoURL: forgejoRepoURL(forgejoURL, repo),
				Ref:     ref,
				RunID:   runID,
			})
			if err != nil {
				return err
			}
			if result.Logs != "" {
				fmt.Println("=== Serial Console Output ===")
				fmt.Println(result.Logs)
			}
			if result.ExitCode != 0 {
				os.Exit(result.ExitCode)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&repo, "repo", "", "Repository in owner/name form")
	cmd.Flags().StringVar(&ref, "ref", "", "Commit SHA or ref to execute")
	cmd.Flags().StringVar(&runID, "run-id", "", "Logical run ID for telemetry grouping")
	cmd.Flags().StringVar(&forgejoURL, "forgejo-url", "http://127.0.0.1:3000", "Forgejo base URL")
	addFirecrackerFlags(cmd, &pool, &goldenZvol, &kernelPath, &fcBin, &jailerBin, &vcpus, &memoryMiB, &timeout, &hostInterface, &guestPoolCIDR, &networkLeaseDir)
	_ = cmd.MarkFlagRequired("repo")
	_ = cmd.MarkFlagRequired("ref")
	return cmd
}

func ciFirecrackerConfig(pool, goldenZvol, kernelPath, fcBin, jailerBin string, vcpus, memoryMiB int, hostInterface, guestPoolCIDR, networkLeaseDir string) (firecracker.Config, error) {
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
	return cfg, nil
}

func addFirecrackerFlags(cmd *cobra.Command, pool, goldenZvol, kernelPath, fcBin, jailerBin *string, vcpus, memoryMiB *int, timeout, hostInterface, guestPoolCIDR, networkLeaseDir *string) {
	cmd.Flags().StringVar(pool, "pool", "", "ZFS pool name (default: benchpool)")
	cmd.Flags().StringVar(goldenZvol, "golden-zvol", "", "Base guest golden zvol name (default: golden-zvol)")
	cmd.Flags().StringVar(kernelPath, "kernel", "", "Path to vmlinux (default: /var/lib/ci/vmlinux)")
	cmd.Flags().StringVar(fcBin, "firecracker-bin", "", "Path to firecracker binary")
	cmd.Flags().StringVar(jailerBin, "jailer-bin", "", "Path to jailer binary")
	cmd.Flags().IntVar(vcpus, "vcpus", 0, "vCPU count")
	cmd.Flags().IntVar(memoryMiB, "memory", 0, "Memory in MiB")
	cmd.Flags().StringVar(timeout, "timeout", "20m", "Command timeout")
	cmd.Flags().StringVar(hostInterface, "host-interface", "", "Host uplink interface for guest egress")
	cmd.Flags().StringVar(guestPoolCIDR, "guest-pool-cidr", "", "IPv4 pool reserved for Firecracker guests")
	cmd.Flags().StringVar(networkLeaseDir, "network-lease-dir", "", "Directory for persistent guest network leases")
}

func handleSignals(cancel context.CancelFunc, logger *slog.Logger) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("signal received, shutting down")
		cancel()
	}()
}

func forgejoRepoURL(baseURL, repo string) string {
	return fmt.Sprintf("%s/%s.git", trimTrailingSlash(baseURL), repo)
}

func trimTrailingSlash(value string) string {
	for len(value) > 0 && value[len(value)-1] == '/' {
		value = value[:len(value)-1]
	}
	return value
}
