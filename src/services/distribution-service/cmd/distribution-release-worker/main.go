package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"go.temporal.io/sdk/worker"

	"github.com/verself/distribution-service/internal/releaseworkflow"
	verselfotel "github.com/verself/observability/otel"
	"github.com/verself/service-runtime/envconfig"
	workloadauth "github.com/verself/service-runtime/workload"
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
		ServiceName:    "distribution-release-worker",
		ServiceVersion: version,
	})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() {
		if err := otelShutdown(context.Background()); err != nil {
			logger.ErrorContext(context.Background(), "distribution-release-worker otel shutdown", "error", err)
		}
	}()
	slog.SetDefault(logger)

	cfg := envconfig.New()
	temporalFrontendAddress := cfg.RequireString("VERSELF_TEMPORAL_FRONTEND_ADDRESS")
	namespace := cfg.String("DISTRIBUTION_RELEASE_TEMPORAL_NAMESPACE", releaseworkflow.DefaultNamespace)
	taskQueue := cfg.String("DISTRIBUTION_RELEASE_TEMPORAL_TASK_QUEUE", releaseworkflow.DefaultTaskQueue)
	artifactRoot := cfg.String("DISTRIBUTION_RELEASE_ARTIFACT_ROOT", releaseworkflow.DefaultArtifactRoot)
	workRoot := cfg.String("DISTRIBUTION_RELEASE_WORK_ROOT", releaseworkflow.DefaultWorkRoot)
	releaseToolsTar := cfg.String("DISTRIBUTION_RELEASE_TOOLS_TAR", releaseworkflow.DefaultReleaseToolsTar)
	gitBinary := cfg.String("DISTRIBUTION_RELEASE_GIT_BINARY", releaseworkflow.DefaultGitBin)
	builderID := cfg.String("DISTRIBUTION_RELEASE_BUILDER_ID", releaseworkflow.DefaultBuilderID)
	spiffeEndpoint := cfg.String(workloadauth.EndpointSocketEnv, "")
	if err := cfg.Err(); err != nil {
		return err
	}

	spiffeSource, err := sdkclient.NewSource(ctx, spiffeEndpoint)
	if err != nil {
		return fmt.Errorf("distribution-release-worker spiffe source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "distribution-release-worker spiffe source close", "error", err)
		}
	}()
	if _, err := workloadauth.CurrentIDForService(spiffeSource, workloadauth.ServiceDistribution); err != nil {
		return err
	}

	temporalClient, err := sdkclient.NewWorkflowClient(sdkclient.Config{
		HostPort: temporalFrontendAddress,
	}, namespace, spiffeSource, "distribution-release-worker-sdk")
	if err != nil {
		return fmt.Errorf("distribution-release-worker temporal client: %w", err)
	}
	defer temporalClient.Close()

	runner, err := releaseworkflow.NewCommandRunner(releaseworkflow.CommandRunnerConfig{
		ArtifactRoot:    artifactRoot,
		WorkRoot:        workRoot,
		ReleaseToolsTar: releaseToolsTar,
		GitBinary:       gitBinary,
		BuilderID:       builderID,
	})
	if err != nil {
		return err
	}

	workerInstance := worker.New(temporalClient, taskQueue, worker.Options{})
	releaseworkflow.RegisterWorker(workerInstance, runner)
	if err := workerInstance.Start(); err != nil {
		return fmt.Errorf("start distribution release worker: %w", err)
	}
	defer workerInstance.Stop()

	logger.Info("distribution release worker started", "namespace", namespace, "task_queue", taskQueue)
	<-ctx.Done()
	return nil
}
