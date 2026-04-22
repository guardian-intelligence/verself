package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	fmotel "github.com/forge-metal/otel"

	"github.com/forge-metal/temporal-platform/internal/temporalweb"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, logger, err := fmotel.Init(ctx, fmotel.Config{
		ServiceName:    "temporal-web",
		ServiceVersion: version,
	})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() {
		_ = otelShutdown(context.Background())
	}()
	slog.SetDefault(logger)

	cfg := temporalweb.Config{
		ConfigDir:        envOr("FM_TEMPORAL_WEB_CONFIG_DIR", "/etc/temporal-web"),
		Environment:      envOr("FM_TEMPORAL_WEB_CONFIG_ENV", "production"),
		FrontendAddress:  envOr("FM_TEMPORAL_FRONTEND_ADDRESS", ""),
		SPIFFESocketAddr: envOr("SPIFFE_ENDPOINT_SOCKET", ""),
	}
	if cfg.FrontendAddress == "" {
		return errors.New("FM_TEMPORAL_FRONTEND_ADDRESS is required")
	}
	if cfg.SPIFFESocketAddr == "" {
		return errors.New("SPIFFE_ENDPOINT_SOCKET is required")
	}

	slog.InfoContext(
		ctx,
		"starting temporal web",
		"config_dir", cfg.ConfigDir,
		"config_env", cfg.Environment,
		"frontend_address", cfg.FrontendAddress,
	)

	return temporalweb.Run(ctx, cfg)
}

func envOr(name, fallback string) string {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	return raw
}
