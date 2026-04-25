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
	"go.temporal.io/server/common/authorization"
	"go.temporal.io/server/common/config"
	"go.temporal.io/server/temporal"

	"github.com/verself/temporal-platform/internal/pgsocket"
	"github.com/verself/temporal-platform/internal/spiffeauth"
	"github.com/verself/temporal-platform/internal/tlsprovider"
)

var temporalServices = []string{
	"frontend",
	"internal-frontend",
	"history",
	"matching",
	"worker",
}

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
		ServiceName:    "temporal-server",
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
	cfgPath := l.RequireString("VERSELF_TEMPORAL_CONFIG_PATH")
	spiffeSocket := l.String("SPIFFE_ENDPOINT_SOCKET", "")
	if err := l.Err(); err != nil {
		return err
	}
	cfg, err := config.Load(config.WithConfigFile(cfgPath))
	if err != nil {
		return fmt.Errorf("load temporal config %s: %w", cfgPath, err)
	}
	if err := pgsocket.ConfigureTemporalDatastores(cfg); err != nil {
		return fmt.Errorf("configure temporal postgres sockets: %w", err)
	}

	authzCfg, err := spiffeauth.LoadFromEnv()
	if err != nil {
		return fmt.Errorf("load temporal spiffe authorization config: %w", err)
	}

	tlsConfigProvider, err := tlsprovider.New(ctx, spiffeSocket)
	if err != nil {
		return fmt.Errorf("build temporal tls provider: %w", err)
	}
	defer func() {
		if err := tlsConfigProvider.Close(); err != nil {
			logger.ErrorContext(context.Background(), "temporal tls provider close", "error", err)
		}
	}()
	tlsSnapshot := tlsConfigProvider.Snapshot()
	logger.InfoContext(
		ctx,
		"temporal tls config prepared",
		"internode_server_client_auth", tlsSnapshot.InternodeServerClientAuth.String(),
		"frontend_server_client_auth", tlsSnapshot.FrontendServerClientAuth.String(),
		"frontend_client_server_name", tlsSnapshot.FrontendClientServerName,
		"remote_cluster_client_configs", tlsSnapshot.RemoteClusterConfigs,
	)

	interruptCh := make(chan interface{}, 1)
	go func() {
		<-ctx.Done()
		interruptCh <- ctx.Err()
	}()

	server, err := temporal.NewServer(
		temporal.WithConfig(cfg),
		temporal.ForServices(temporalServices),
		temporal.InterruptOn(interruptCh),
		temporal.WithTLSConfigFactory(tlsConfigProvider),
		temporal.WithClaimMapper(func(*config.Config) authorization.ClaimMapper {
			return spiffeauth.NewClaimMapper(authzCfg)
		}),
		temporal.WithAuthorizer(spiffeauth.NewTracingAuthorizer(authorization.NewDefaultAuthorizer())),
		temporal.WithChainedFrontendGrpcInterceptors(
			spiffeauth.UnaryServerInterceptor(),
		),
	)
	if err != nil {
		return fmt.Errorf("construct temporal server: %w", err)
	}
	if err := server.Start(); err != nil {
		return fmt.Errorf("start temporal server: %w", err)
	}
	return nil
}
