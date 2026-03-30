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
		concurrency   int
		iterations    int
		configPath    string
		timeout       string
		workloadsPath string
	)

	cmd := &cobra.Command{
		Use:   "benchmark",
		Short: "Run CI benchmark workloads on ZFS clones",
		Long: `Runs real CI jobs (git clone, npm install, lint, build, test) against
open-source Next.js projects on ZFS clones. Generates CIEvent telemetry
with real timing, cgroup resource stats, and ZFS written bytes.

Workloads are loaded from a TOML file (--workloads flag). Edit the file
and send SIGHUP to hot-reload the workload mix and concurrency without
restarting:

  kill -HUP $(pidof bmci)

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

			// Load workloads: TOML file -> built-in defaults.
			workloads := benchmark.DefaultWorkloads()
			if workloadsPath != "" {
				wc, err := benchmark.LoadWorkloads(workloadsPath)
				if err != nil {
					// File specified but unreadable — fall back to defaults only
					// if using the implicit default path.
					if cmd.Flags().Changed("workloads") {
						return fmt.Errorf("load workloads: %w", err)
					}
					logger.Info("workloads file not found, using built-in defaults", "path", workloadsPath)
				} else {
					workloads = wc.Workloads
					if wc.Concurrency > 0 && !cmd.Flags().Changed("concurrency") {
						concurrency = wc.Concurrency
					}
					if wc.JobTimeout > 0 && !cmd.Flags().Changed("timeout") {
						jobTimeout = wc.JobTimeout
					}
					logger.Info("loaded workloads", "path", workloadsPath, "count", len(workloads))
				}
			}

			benchCfg := benchmark.Config{
				Workloads:   workloads,
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

			// SIGHUP reloads workloads file and reconfigures the runner.
			sighup := make(chan os.Signal, 1)
			signal.Notify(sighup, syscall.SIGHUP)
			go func() {
				for range sighup {
					logger.Info("SIGHUP received, reloading workloads", "path", workloadsPath)
					if workloadsPath == "" {
						logger.Warn("no workloads file configured, nothing to reload")
						continue
					}
					wc, err := benchmark.LoadWorkloads(workloadsPath)
					if err != nil {
						logger.Error("reload failed", "err", err)
						continue
					}
					newCfg := benchmark.Config{
						Workloads:   wc.Workloads,
						Concurrency: concurrency,
						Iterations:  0, // don't reset iteration target on reload
						JobTimeout:  jobTimeout,
						NodeID:      cfg.Latitude.Region + "-" + cfg.Latitude.Plan,
						Region:      cfg.Latitude.Region,
						Plan:        cfg.Latitude.Plan,
					}
					// File-level settings override CLI defaults on reload.
					if wc.Concurrency > 0 {
						newCfg.Concurrency = wc.Concurrency
					}
					if wc.JobTimeout > 0 {
						newCfg.JobTimeout = wc.JobTimeout
					}
					runner.Reconfigure(newCfg)
				}
			}()

			return runner.Run(ctx)
		},
	}

	cmd.Flags().IntVarP(&concurrency, "concurrency", "c", 4, "Max parallel jobs")
	cmd.Flags().IntVarP(&iterations, "iterations", "n", 0, "Total jobs to run (0 = run until stopped)")
	cmd.Flags().StringVar(&configPath, "config", "", "Config file override")
	cmd.Flags().StringVar(&timeout, "timeout", "10m", "Default per-job timeout")
	cmd.Flags().StringVarP(&workloadsPath, "workloads", "w", "config/workloads.toml",
		"Workload catalog TOML file (reload via SIGHUP)")

	return cmd
}
