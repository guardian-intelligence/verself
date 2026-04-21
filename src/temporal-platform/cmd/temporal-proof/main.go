package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	workloadauth "github.com/forge-metal/auth-middleware/workload"
	fmotel "github.com/forge-metal/otel"
	"github.com/forge-metal/temporal-platform/internal/proof"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: temporal-proof <bootstrap|denied|start|await|worker>")
	}

	command := strings.TrimSpace(args[0])
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serviceName := "temporal-proof"
	if command == "worker" {
		serviceName = "temporal-proof-worker"
	}
	otelShutdown, logger, err := fmotel.Init(ctx, fmotel.Config{
		ServiceName:    serviceName,
		ServiceVersion: version,
	})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() {
		_ = otelShutdown(context.Background())
	}()
	slog.SetDefault(logger)

	cfg, err := proof.LoadConfigFromEnv()
	if err != nil {
		return err
	}
	cfg.ServiceVersion = version

	source, err := proof.NewSource(ctx, strings.TrimSpace(os.Getenv(workloadauth.EndpointSocketEnv)))
	if err != nil {
		return fmt.Errorf("open proof spiffe source: %w", err)
	}
	defer func() {
		if err := source.Close(); err != nil {
			logger.ErrorContext(context.Background(), "temporal-proof spiffe source close", "error", err)
		}
	}()

	switch command {
	case "bootstrap":
		return proof.BootstrapNamespaces(ctx, cfg, source)
	case "denied":
		flags := flag.NewFlagSet("denied", flag.ContinueOnError)
		flags.SetOutput(os.Stderr)
		runID := flags.String("run-id", defaultRunID("denied"), "proof run id")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return proof.ExpectDeniedNamespaceStart(ctx, cfg, source, *runID)
	case "start":
		flags := flag.NewFlagSet("start", flag.ContinueOnError)
		flags.SetOutput(os.Stderr)
		runID := flags.String("run-id", defaultRunID("start"), "proof run id")
		sleepFor := flags.Duration("sleep", 10*time.Second, "durable sleep before the governance activity")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		result, err := proof.StartWorkflow(ctx, cfg, source, *runID, *sleepFor)
		if err != nil {
			return err
		}
		return printJSON(result)
	case "await":
		flags := flag.NewFlagSet("await", flag.ContinueOnError)
		flags.SetOutput(os.Stderr)
		workflowID := flags.String("workflow-id", "", "workflow id")
		runID := flags.String("run-id", "", "workflow run id")
		timeout := flags.Duration("timeout", 90*time.Second, "await timeout")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*workflowID) == "" || strings.TrimSpace(*runID) == "" {
			return errors.New("await requires --workflow-id and --run-id")
		}
		awaitCtx, cancel := context.WithTimeout(ctx, *timeout)
		defer cancel()
		result, err := proof.AwaitWorkflow(awaitCtx, cfg, source, *workflowID, *runID)
		if err != nil {
			return err
		}
		return printJSON(result)
	case "worker":
		return proof.RunWorker(ctx, cfg, source)
	default:
		return fmt.Errorf("unknown temporal-proof command %q", command)
	}
}

func defaultRunID(prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, time.Now().UTC().Format("20060102T150405Z"))
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
