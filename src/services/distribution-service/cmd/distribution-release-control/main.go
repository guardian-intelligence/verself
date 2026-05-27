package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/verself/distribution-service/internal/releaseworkflow"
	verselfotel "github.com/verself/observability/otel"
	workloadauth "github.com/verself/service-runtime/workload"
	"github.com/verself/temporal-platform/sdkclient"
)

const (
	defaultSPIFFESocket = "unix:///run/spire-agent/sockets/agent.sock"
	temporalCatalogName = "temporal-frontend-grpc"
)

var version = "dev"

type mkskConfig struct {
	channel         string
	releaseVersion  string
	sourceRef       string
	sourceCommit    string
	temporalAddress string
	namespace       string
	taskQueue       string
	spiffeSocket    string
	format          string
}

type dispatchRecord struct {
	Package      string `json:"package"`
	Channel      string `json:"channel"`
	Version      string `json:"version,omitempty"`
	SourceRef    string `json:"source_ref"`
	SourceCommit string `json:"source_commit"`
	Namespace    string `json:"namespace"`
	TaskQueue    string `json:"task_queue"`
	WorkflowID   string `json:"workflow_id"`
	RunID        string `json:"run_id"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "distribution-release-control: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	shutdown, logger, err := verselfotel.Init(ctx, verselfotel.Config{
		ServiceName:    "distribution-release-control",
		ServiceVersion: version,
	})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(context.Background(), "distribution-release-control otel shutdown", "error", err)
		}
	}()
	slog.SetDefault(logger)

	if len(args) == 0 {
		return fmt.Errorf("missing subcommand: mksk")
	}
	switch args[0] {
	case "mksk":
		return runMksk(ctx, args[1:])
	case "-h", "--help", "help":
		printUsage(os.Stdout)
		return nil
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func printUsage(w *os.File) {
	_, _ = fmt.Fprint(w, `distribution-release-control <subcommand> [flags]

Subcommands:
  mksk  Dispatch a make-skill release workflow.
`)
}

func runMksk(ctx context.Context, args []string) error {
	cfg := mkskConfig{
		namespace:    releaseworkflow.DefaultNamespace,
		taskQueue:    releaseworkflow.DefaultTaskQueue,
		spiffeSocket: defaultSPIFFESocket,
		format:       "text",
	}
	fs := flag.NewFlagSet("distribution-release-control mksk", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&cfg.channel, "channel", "", "Release channel: nightly, rc, or stable.")
	fs.StringVar(&cfg.releaseVersion, "version", "", "Release version. Required for rc and stable.")
	fs.StringVar(&cfg.sourceRef, "source-ref", "", "Git ref recorded as release source.")
	fs.StringVar(&cfg.sourceCommit, "source-commit", "", "Resolved 40-character git source commit.")
	fs.StringVar(&cfg.temporalAddress, "temporal-address", "", "Temporal frontend host:port. Defaults to Nomad service discovery.")
	fs.StringVar(&cfg.namespace, "temporal-namespace", releaseworkflow.DefaultNamespace, "Temporal namespace.")
	fs.StringVar(&cfg.taskQueue, "temporal-task-queue", releaseworkflow.DefaultTaskQueue, "Temporal task queue.")
	fs.StringVar(&cfg.spiffeSocket, "spiffe-socket", defaultSPIFFESocket, "SPIFFE Workload API socket.")
	fs.StringVar(&cfg.format, "format", "text", "Output format: text or json.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional args: %s", strings.Join(fs.Args(), " "))
	}
	format, err := normalizeOutputFormat(cfg.format)
	if err != nil {
		return err
	}
	cfg.format = format
	record, err := dispatchMksk(ctx, cfg)
	if err != nil {
		return err
	}
	return printDispatchRecord(os.Stdout, cfg.format, record)
}

func dispatchMksk(ctx context.Context, cfg mkskConfig) (dispatchRecord, error) {
	source := releaseworkflow.PinnedSource{
		Ref:    strings.TrimSpace(cfg.sourceRef),
		Commit: strings.TrimSpace(cfg.sourceCommit),
	}
	channel := strings.TrimSpace(cfg.channel)
	releaseVersion := strings.TrimSpace(cfg.releaseVersion)
	var startWorkflow func(context.Context, *releaseworkflow.Client) (releaseworkflow.ReleaseRun, error)
	switch channel {
	case releaseworkflow.ChannelNightly:
		startWorkflow = func(ctx context.Context, client *releaseworkflow.Client) (releaseworkflow.ReleaseRun, error) {
			return client.StartNightly(ctx, releaseworkflow.NightlyReleaseInput{
				Package: releaseworkflow.PackageMksk,
				Source:  source,
			})
		}
	case releaseworkflow.ChannelRC:
		startWorkflow = func(ctx context.Context, client *releaseworkflow.Client) (releaseworkflow.ReleaseRun, error) {
			return client.StartReleaseCandidate(ctx, releaseworkflow.ReleaseCandidateInput{
				Package: releaseworkflow.PackageMksk,
				Version: releaseVersion,
				Source:  source,
			})
		}
	case releaseworkflow.ChannelStable:
		startWorkflow = func(ctx context.Context, client *releaseworkflow.Client) (releaseworkflow.ReleaseRun, error) {
			return client.StartStable(ctx, releaseworkflow.StableReleaseInput{
				Package: releaseworkflow.PackageMksk,
				Version: releaseVersion,
				Source:  source,
			})
		}
	default:
		return dispatchRecord{}, fmt.Errorf("%w: --channel must be nightly, rc, or stable", releaseworkflow.ErrInvalidInput)
	}
	address := strings.TrimSpace(cfg.temporalAddress)
	if address == "" {
		resolved, err := resolveTemporalAddress(ctx)
		if err != nil {
			return dispatchRecord{}, err
		}
		address = resolved
	}
	spiffeSource, err := sdkclient.NewSource(ctx, strings.TrimSpace(cfg.spiffeSocket))
	if err != nil {
		return dispatchRecord{}, fmt.Errorf("distribution release control spiffe source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			slog.ErrorContext(context.Background(), "distribution release control spiffe source close", "error", err)
		}
	}()
	if _, err := workloadauth.CurrentIDForService(spiffeSource, workloadauth.ServiceDistribution); err != nil {
		return dispatchRecord{}, err
	}
	client, err := releaseworkflow.Dial(ctx, releaseworkflow.Config{
		HostPort:  address,
		Namespace: strings.TrimSpace(cfg.namespace),
		TaskQueue: strings.TrimSpace(cfg.taskQueue),
		Source:    spiffeSource,
	})
	if err != nil {
		return dispatchRecord{}, err
	}
	defer client.Close()

	run, err := startWorkflow(ctx, client)
	if err != nil {
		return dispatchRecord{}, err
	}
	slog.InfoContext(ctx, "distribution release workflow dispatched",
		"package", releaseworkflow.PackageMksk,
		"channel", channel,
		"version", releaseVersion,
		"source_ref", source.Ref,
		"source_commit", source.Commit,
		"workflow_id", run.WorkflowID,
		"run_id", run.RunID,
	)
	return dispatchRecord{
		Package:      releaseworkflow.PackageMksk,
		Channel:      channel,
		Version:      releaseVersion,
		SourceRef:    source.Ref,
		SourceCommit: source.Commit,
		Namespace:    defaultString(cfg.namespace, releaseworkflow.DefaultNamespace),
		TaskQueue:    defaultString(cfg.taskQueue, releaseworkflow.DefaultTaskQueue),
		WorkflowID:   run.WorkflowID,
		RunID:        run.RunID,
	}, nil
}

func resolveTemporalAddress(ctx context.Context) (string, error) {
	resolver, err := workloadauth.NewResolverFromEnv()
	if err != nil {
		return "", err
	}
	endpoints, err := resolver.Resolve(ctx, temporalCatalogName)
	if err != nil {
		return "", err
	}
	endpoint, err := workloadauth.Pick(endpoints)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(endpoint.Address, strconv.Itoa(endpoint.Port)), nil
}

func printDispatchRecord(w *os.File, format string, record dispatchRecord) error {
	normalized, err := normalizeOutputFormat(format)
	if err != nil {
		return err
	}
	switch normalized {
	case "text":
		_, err := fmt.Fprintf(w, "release workflow: %s\nrun id: %s\nnamespace: %s\ntask queue: %s\nsource: %s@%s\n",
			record.WorkflowID,
			record.RunID,
			record.Namespace,
			record.TaskQueue,
			record.SourceRef,
			record.SourceCommit,
		)
		return err
	case "json":
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(record)
	}
	return nil
}

func normalizeOutputFormat(format string) (string, error) {
	switch normalized := strings.ToLower(strings.TrimSpace(format)); normalized {
	case "", "text":
		return "text", nil
	case "json":
		return "json", nil
	default:
		return "", fmt.Errorf("unsupported --format %q", format)
	}
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
