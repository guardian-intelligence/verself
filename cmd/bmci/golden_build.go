package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/forge-metal/forge-metal/internal/benchmark"
	"github.com/forge-metal/forge-metal/internal/config"
	"github.com/forge-metal/forge-metal/internal/zfsharness"
)

func goldenBuildCmd() *cobra.Command {
	var (
		configPath string
		force      bool
	)

	cmd := &cobra.Command{
		Use:   "golden-build",
		Short: "Build and snapshot the benchmark golden image",
		Long: `Creates the golden ZFS dataset, clones benchmark projects into it,
runs npm ci and warm builds, then snapshots as @ready.

Idempotent: skips build if @ready exists. Use --force to rebuild.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}))

			harness := zfsharness.New(zfsharness.Config{
				Pool:          cfg.ZFS.Pool,
				GoldenDataset: cfg.ZFS.GoldenDataset,
				CIDataset:     cfg.ZFS.CIDataset,
			})

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				logger.Info("interrupted, cancelling golden build")
				cancel()
			}()

			return benchmark.BuildGoldenImage(ctx, harness, benchmark.DefaultGoldenProjects(), force, logger)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "Config file override")
	cmd.Flags().BoolVar(&force, "force", false, "Rebuild even if @ready exists")

	return cmd
}
