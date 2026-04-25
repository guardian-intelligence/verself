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
	"time"

	workloadauth "github.com/verself/auth-middleware/workload"
	verselfotel "github.com/verself/otel"
	"github.com/verself/temporal-platform/internal/namespaceadmin"
	"github.com/verself/temporal-platform/sdkclient"
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
		ServiceName:    "temporal-bootstrap",
		ServiceVersion: version,
	})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() {
		_ = otelShutdown(context.Background())
	}()
	slog.SetDefault(logger)

	cfg, err := sdkclient.LoadConfigFromEnv()
	if err != nil {
		return err
	}
	specs, err := loadSpecsFromEnv()
	if err != nil {
		return err
	}
	source, err := sdkclient.NewSource(ctx, strings.TrimSpace(os.Getenv(workloadauth.EndpointSocketEnv)))
	if err != nil {
		return fmt.Errorf("open temporal bootstrap spiffe source: %w", err)
	}
	defer func() {
		if err := source.Close(); err != nil {
			logger.ErrorContext(context.Background(), "temporal bootstrap spiffe source close", "error", err)
		}
	}()

	namespaceClient, err := sdkclient.NewNamespaceClient(cfg, source, "temporal-bootstrap-sdk")
	if err != nil {
		return fmt.Errorf("dial temporal namespace client: %w", err)
	}
	defer namespaceClient.Close()

	return namespaceadmin.Ensure(ctx, namespaceClient, specs)
}

func loadSpecsFromEnv() ([]namespaceadmin.Spec, error) {
	raw := strings.TrimSpace(os.Getenv("VERSELF_TEMPORAL_BOOTSTRAP_NAMESPACES"))
	if raw == "" {
		return nil, errors.New("VERSELF_TEMPORAL_BOOTSTRAP_NAMESPACES is required")
	}
	retention := loadRetention()
	seen := map[string]struct{}{}
	specs := make([]namespaceadmin.Spec, 0, 8)
	for _, item := range strings.Split(raw, ",") {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		specs = append(specs, namespaceadmin.Spec{
			Name:      name,
			Retention: retention,
		})
	}
	if len(specs) == 0 {
		return nil, errors.New("VERSELF_TEMPORAL_BOOTSTRAP_NAMESPACES must contain at least one namespace")
	}
	return specs, nil
}

func loadRetention() time.Duration {
	raw := strings.TrimSpace(os.Getenv("VERSELF_TEMPORAL_BOOTSTRAP_NAMESPACE_RETENTION"))
	if raw == "" {
		return 24 * time.Hour
	}
	retention, err := time.ParseDuration(raw)
	if err != nil {
		return 24 * time.Hour
	}
	return retention
}
