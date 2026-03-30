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

	"github.com/forge-metal/forge-metal/internal/benchmark"
	"github.com/forge-metal/forge-metal/internal/clickhouse"
	"github.com/forge-metal/forge-metal/internal/config"
	"github.com/forge-metal/forge-metal/internal/zfsharness"
)

func benchmarkCmd() *cobra.Command {
	var (
		concurrency int
		iterations  int
		configPath  string
		timeout     string
	)

	cmd := &cobra.Command{
		Use:   "benchmark",
		Short: "Run CI benchmark workloads on ZFS clones",
		Long: `Runs real CI jobs (git clone, npm install, lint, build, test) against
open-source Next.js projects on ZFS clones. Generates CIEvent telemetry
with real timing, cgroup resource stats, and ZFS written bytes.

Requires root (for ZFS clone operations) and a golden@ready snapshot.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			jobTimeout, err := time.ParseDuration(timeout)
			if err != nil {
				return fmt.Errorf("parse timeout %q: %w", timeout, err)
			}

			// Create ZFS harness.
			harness := zfsharness.New(zfsharness.Config{
				Pool:          cfg.ZFS.Pool,
				GoldenDataset: cfg.ZFS.GoldenDataset,
				CIDataset:     cfg.ZFS.CIDataset,
			})

			// Try to connect to ClickHouse (non-fatal if unavailable).
			var chClient *clickhouse.Client
			if cfg.ClickHouse.Addr != "" {
				ch, err := clickhouse.New(cfg.ClickHouse)
				if err != nil {
					slog.Warn("clickhouse unavailable, events will be logged only", "err", err)
				} else {
					chClient = ch
					defer ch.Close()
				}
			}

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}))

			benchCfg := benchmark.Config{
				Workloads:   benchmark.DefaultWorkloads(),
				Concurrency: concurrency,
				Iterations:  iterations,
				JobTimeout:  jobTimeout,
				NodeID:      cfg.Latitude.Region + "-" + cfg.Latitude.Plan,
				Region:      cfg.Latitude.Region,
				Plan:        cfg.Latitude.Plan,
			}

			runner := benchmark.New(harness, chClient, benchCfg, logger)

			// Graceful shutdown on SIGINT/SIGTERM.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				sig := <-sigCh
				logger.Info("received signal, shutting down gracefully", "signal", sig)
				cancel()
			}()

			return runner.Run(ctx)
		},
	}

	cmd.Flags().IntVarP(&concurrency, "concurrency", "c", 4, "Max parallel jobs")
	cmd.Flags().IntVarP(&iterations, "iterations", "n", 0, "Total jobs to run (0 = run until stopped)")
	cmd.Flags().StringVar(&configPath, "config", "", "Config file override")
	cmd.Flags().StringVar(&timeout, "timeout", "10m", "Default per-job timeout")

	return cmd
}
