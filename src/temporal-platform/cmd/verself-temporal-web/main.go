package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/verself/envconfig"
	verselfotel "github.com/verself/otel"

	"github.com/verself/temporal-platform/internal/temporalweb"
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

	otelShutdown, logger, err := verselfotel.Init(ctx, verselfotel.Config{
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

	l := envconfig.New()
	cfg := temporalweb.Config{
		ConfigDir:        l.String("VERSELF_TEMPORAL_WEB_CONFIG_DIR", "/etc/temporal-web"),
		Environment:      l.String("VERSELF_TEMPORAL_WEB_CONFIG_ENV", "production"),
		FrontendAddress:  l.RequireString("VERSELF_TEMPORAL_FRONTEND_ADDRESS"),
		SPIFFESocketAddr: l.RequireString("SPIFFE_ENDPOINT_SOCKET"),
	}
	if err := l.Err(); err != nil {
		return err
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
