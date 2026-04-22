package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	fmotel "github.com/forge-metal/otel"
	"github.com/spiffe/go-spiffe/v2/spiffeid"

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

	cfg.ExpectedClientID, err = parseSPIFFEIDEnv("FM_TEMPORAL_WEB_SPIFFE_ID")
	if err != nil {
		return err
	}
	cfg.ServerID, err = parseSPIFFEIDEnv("FM_TEMPORAL_SERVER_SPIFFE_ID")
	if err != nil {
		return err
	}

	slog.InfoContext(
		ctx,
		"starting temporal web",
		"config_dir", cfg.ConfigDir,
		"config_env", cfg.Environment,
		"frontend_address", cfg.FrontendAddress,
		"temporal_server_spiffe_id", cfg.ServerID.String(),
		"temporal_web_spiffe_id", cfg.ExpectedClientID.String(),
	)

	return temporalweb.Run(ctx, cfg)
}

func parseSPIFFEIDEnv(name string) (spiffeid.ID, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return spiffeid.ID{}, fmt.Errorf("%s is required", name)
	}
	id, err := spiffeid.FromString(raw)
	if err != nil {
		return spiffeid.ID{}, fmt.Errorf("parse %s: %w", name, err)
	}
	return id, nil
}

func envOr(name, fallback string) string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	return raw
}
