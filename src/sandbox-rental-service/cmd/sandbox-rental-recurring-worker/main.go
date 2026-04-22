package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"go.temporal.io/sdk/worker"

	workloadauth "github.com/forge-metal/auth-middleware/workload"
	fmotel "github.com/forge-metal/otel"
	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
	"github.com/forge-metal/sandbox-rental-service/internal/recurring"
	"github.com/forge-metal/sandbox-rental-service/internal/scheduler"
	"github.com/forge-metal/temporal-platform/sdkclient"
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

	pgDSN := requireEnv("SANDBOX_PG_DSN")
	temporalServerSPIFFEID, err := workloadauth.ParseID(requireEnv("SANDBOX_TEMPORAL_SERVER_SPIFFE_ID"))
	if err != nil {
		return err
	}
	temporalFrontendAddress := envOr("SANDBOX_TEMPORAL_FRONTEND_ADDRESS", sdkclient.DefaultFrontendAddress)
	temporalNamespace := envOr("SANDBOX_TEMPORAL_NAMESPACE", recurring.DefaultNamespace)
	temporalRecurringTaskQueue := envOr("SANDBOX_TEMPORAL_TASK_QUEUE_RECURRING", recurring.DefaultTaskQueue)

	spiffeSource, err := sdkclient.NewSource(ctx, strings.TrimSpace(os.Getenv(workloadauth.EndpointSocketEnv)))
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
		return fmt.Errorf("parse scheduler postgres dsn: %w", err)
	}
	pgxConfig.MaxConns = int32(envInt("SANDBOX_RIVER_PG_MAX_CONNS", 8))
	pgxConfig.MinConns = int32(envInt("SANDBOX_RIVER_PG_MIN_CONNS", 1))
	pgxConfig.MaxConnLifetime = time.Duration(envInt("SANDBOX_RIVER_PG_CONN_MAX_LIFETIME_SECONDS", 1800)) * time.Second
	pgxConfig.MaxConnIdleTime = time.Duration(envInt("SANDBOX_RIVER_PG_CONN_MAX_IDLE_SECONDS", 300)) * time.Second
	pgxPool, err := pgxpool.NewWithConfig(ctx, pgxConfig)
	if err != nil {
		return fmt.Errorf("open scheduler postgres pool: %w", err)
	}
	defer pgxPool.Close()
	pgxPingCtx, cancelPGXPing := context.WithTimeout(ctx, 5*time.Second)
	defer cancelPGXPing()
	if err := pgxPool.Ping(pgxPingCtx); err != nil {
		return fmt.Errorf("ping scheduler postgres pool: %w", err)
	}

	jobService := &jobs.Service{
		PGX:    pgxPool,
		Logger: logger,
	}

	schedulerRuntime, err := scheduler.NewRuntime(pgxPool, scheduler.Config{
		Logger: logger,
		// River validates inserted job kinds against the local worker bundle even
		// when this process only enqueues and never starts a River worker loop.
		RegisterWorkers: func(workers *river.Workers) error {
			return jobs.RegisterSchedulerWorkers(workers, jobService)
		},
	})
	if err != nil {
		return fmt.Errorf("create scheduler runtime: %w", err)
	}
	jobService.Scheduler = schedulerRuntime

	temporalClient, err := sdkclient.NewWorkflowClient(sdkclient.Config{
		HostPort: temporalFrontendAddress,
		ServerID: temporalServerSPIFFEID,
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
		Submitter:      jobService,
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

func requireEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		fmt.Fprintf(os.Stderr, "required env %s is empty\n", key)
		os.Exit(1)
	}
	return value
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		fmt.Fprintf(os.Stderr, "env %s must be a positive integer\n", key)
		os.Exit(1)
	}
	return value
}
