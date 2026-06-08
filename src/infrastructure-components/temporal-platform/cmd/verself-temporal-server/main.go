package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	verselfotel "github.com/verself/observability/otel"
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
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type options struct {
	configPath     string
	authConfigPath string
	schemaBinary   string
	spiffeSocket   string
}

func run(args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
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

	if err := migrateSchema(opts); err != nil {
		return err
	}

	cfg, err := config.Load(config.WithConfigFile(opts.configPath))
	if err != nil {
		return fmt.Errorf("load temporal config %s: %w", opts.configPath, err)
	}
	if err := pgsocket.ConfigureTemporalDatastores(cfg); err != nil {
		return fmt.Errorf("configure temporal postgres sockets: %w", err)
	}

	authzCfg, err := spiffeauth.LoadFromFile(opts.authConfigPath)
	if err != nil {
		return fmt.Errorf("load temporal spiffe authorization config: %w", err)
	}

	tlsConfigProvider, err := tlsprovider.New(ctx, opts.spiffeSocket)
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

func parseOptions(args []string) (options, error) {
	opts := options{}
	fs := flag.NewFlagSet("verself-temporal-server", flag.ContinueOnError)
	fs.StringVar(&opts.configPath, "config", "", "Temporal server config path.")
	fs.StringVar(&opts.authConfigPath, "auth-config", "", "Temporal SPIFFE authorization config path.")
	fs.StringVar(&opts.schemaBinary, "schema-binary", "", "temporal-schema binary path.")
	fs.StringVar(&opts.spiffeSocket, "spiffe-socket", "", "SPIFFE Workload API socket URL.")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	opts.configPath = strings.TrimSpace(opts.configPath)
	opts.authConfigPath = strings.TrimSpace(opts.authConfigPath)
	opts.schemaBinary = strings.TrimSpace(opts.schemaBinary)
	opts.spiffeSocket = strings.TrimSpace(opts.spiffeSocket)
	if opts.configPath == "" || opts.authConfigPath == "" || opts.schemaBinary == "" || opts.spiffeSocket == "" {
		return options{}, errors.New("--config, --auth-config, --schema-binary, and --spiffe-socket are required")
	}
	return opts, nil
}

func migrateSchema(opts options) error {
	cmd := exec.Command(opts.schemaBinary, "migrate", "--config", opts.configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("migrate temporal schema: %w", err)
	}
	return nil
}
