package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/worker"

	workloadauth "github.com/verself/auth-middleware/workload"
	"github.com/verself/envconfig"
	verselfotel "github.com/verself/otel"
	"github.com/verself/sandbox-rental-service/internal/recurring"
	"github.com/verself/sandbox-rental-service/internal/sourceworkflow"
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
		ServiceName:    "sandbox-rental-recurring-worker",
		ServiceVersion: version,
	})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() {
		_ = otelShutdown(context.Background())
	}()
	slog.SetDefault(logger)

	cfg := envconfig.New()
	pgDSN := cfg.RequireString("SANDBOX_PG_DSN")
	temporalFrontendAddress := cfg.String("SANDBOX_TEMPORAL_FRONTEND_ADDRESS", sdkclient.DefaultFrontendAddress)
	temporalNamespace := cfg.String("SANDBOX_TEMPORAL_NAMESPACE", recurring.DefaultNamespace)
	temporalRecurringTaskQueue := cfg.String("SANDBOX_TEMPORAL_TASK_QUEUE_RECURRING", recurring.DefaultTaskQueue)
	sourceInternalURL := cfg.URL("SANDBOX_SOURCE_INTERNAL_URL", "https://127.0.0.1:4262")
	pgMaxConns := cfg.Int("SANDBOX_PG_MAX_CONNS", 4)
	pgMinConns := cfg.Int("SANDBOX_PG_MIN_CONNS", 1)
	pgConnMaxLifetime := cfg.Int("SANDBOX_PG_CONN_MAX_LIFETIME_SECONDS", 1800)
	pgConnMaxIdle := cfg.Int("SANDBOX_PG_CONN_MAX_IDLE_SECONDS", 300)
	spiffeEndpoint := cfg.String(workloadauth.EndpointSocketEnv, "")
	if err := cfg.Err(); err != nil {
		return err
	}

	spiffeSource, err := sdkclient.NewSource(ctx, spiffeEndpoint)
	if err != nil {
		return fmt.Errorf("sandbox-rental recurring spiffe source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "sandbox-rental recurring spiffe source close", "error", err)
		}
	}()

	pgxConfig, err := pgxpool.ParseConfig(pgDSN)
	if err != nil {
		return fmt.Errorf("parse postgres dsn: %w", err)
	}
	pgxConfig.MaxConns = int32(pgMaxConns)
	pgxConfig.MinConns = int32(pgMinConns)
	pgxConfig.MaxConnLifetime = time.Duration(pgConnMaxLifetime) * time.Second
	pgxConfig.MaxConnIdleTime = time.Duration(pgConnMaxIdle) * time.Second
	pgxPool, err := pgxpool.NewWithConfig(ctx, pgxConfig)
	if err != nil {
		return fmt.Errorf("open postgres pool: %w", err)
	}
	defer pgxPool.Close()
	pgxPingCtx, cancelPGXPing := context.WithTimeout(ctx, 5*time.Second)
	defer cancelPGXPing()
	if err := pgxPool.Ping(pgxPingCtx); err != nil {
		return fmt.Errorf("ping postgres pool: %w", err)
	}

	sourceHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceSourceCodeHosting, nil)
	if err != nil {
		return fmt.Errorf("sandbox-rental recurring source-code-hosting mtls: %w", err)
	}
	sourceDispatcher, err := sourceworkflow.NewDispatcher(sourceInternalURL, sourceHTTPClient)
	if err != nil {
		return fmt.Errorf("create source workflow dispatcher: %w", err)
	}

	temporalClient, err := sdkclient.NewWorkflowClient(sdkclient.Config{
		HostPort: temporalFrontendAddress,
	}, temporalNamespace, spiffeSource, "sandbox-rental-recurring-worker-sdk")
	if err != nil {
		return fmt.Errorf("sandbox-rental recurring temporal client: %w", err)
	}
	defer temporalClient.Close()

	recurringService, err := recurring.NewService(recurring.Config{
		PGX:            pgxPool,
		TemporalClient: temporalClient,
		Namespace:      temporalNamespace,
		TaskQueue:      temporalRecurringTaskQueue,
		Logger:         logger,
		Dispatcher:     sourceDispatcher,
	})
	if err != nil {
		return fmt.Errorf("create recurring service: %w", err)
	}

	workerInstance := worker.New(temporalClient, temporalRecurringTaskQueue, worker.Options{})
	recurringService.RegisterWorker(workerInstance)
	if err := workerInstance.Start(); err != nil {
		return fmt.Errorf("start recurring worker: %w", err)
	}
	defer workerInstance.Stop()

	logger.Info("sandbox-rental recurring worker started", "namespace", temporalNamespace, "task_queue", temporalRecurringTaskQueue)
	<-ctx.Done()
	return nil
}
