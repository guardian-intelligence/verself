package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	verselfotel "github.com/verself/observability/otel"
	"github.com/verself/temporal-platform/internal/namespaceadmin"
	"github.com/verself/temporal-platform/sdkclient"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type options struct {
	frontendAddress string
	resourceGraph   string
	resourceName    string
	spiffeSocket    string
	attempts        int
	retryDelay      time.Duration
}

type document struct {
	Entrypoint json.RawMessage `json:"entrypoint"`
	Resources  []resource      `json:"resources"`
}

type resource struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   metadata        `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
}

type metadata struct {
	Name string `json:"name"`
}

type temporalSpec struct {
	Bootstrap struct {
		Namespaces []namespaceSpec `json:"namespaces"`
	} `json:"bootstrap"`
}

type namespaceSpec struct {
	Name      string `json:"name"`
	Retention string `json:"retention"`
}

func run(args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
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

	specs, err := loadSpecs(opts)
	if err != nil {
		return err
	}
	source, err := sdkclient.NewSource(ctx, opts.spiffeSocket)
	if err != nil {
		return fmt.Errorf("open temporal bootstrap spiffe source: %w", err)
	}
	defer func() {
		if err := source.Close(); err != nil {
			logger.ErrorContext(context.Background(), "temporal bootstrap spiffe source close", "error", err)
		}
	}()

	cfg := sdkclient.Config{HostPort: opts.frontendAddress}
	var lastErr error
	for attempt := 1; attempt <= opts.attempts; attempt++ {
		namespaceClient, err := sdkclient.NewNamespaceClient(cfg, source, "temporal-bootstrap-sdk")
		if err == nil {
			err = namespaceadmin.Ensure(ctx, namespaceClient, specs)
			namespaceClient.Close()
		}
		if err == nil {
			return nil
		}
		lastErr = err
		logger.WarnContext(ctx, "temporal namespace bootstrap attempt failed", "attempt", attempt, "attempts", opts.attempts, "error", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(opts.retryDelay):
		}
	}
	return fmt.Errorf("temporal namespace bootstrap failed after %d attempts: %w", opts.attempts, lastErr)
}

func parseOptions(args []string) (options, error) {
	opts := options{resourceName: "temporal", attempts: 30, retryDelay: time.Second}
	fs := flag.NewFlagSet("temporal-bootstrap", flag.ContinueOnError)
	fs.StringVar(&opts.frontendAddress, "frontend-address", "", "Temporal frontend gRPC host:port.")
	fs.StringVar(&opts.resourceGraph, "resource-graph", "", "Guardian resource graph document path.")
	fs.StringVar(&opts.resourceName, "resource-name", opts.resourceName, "TemporalPlatform resource name.")
	fs.StringVar(&opts.spiffeSocket, "spiffe-socket", "", "SPIFFE Workload API socket URL.")
	fs.IntVar(&opts.attempts, "attempts", opts.attempts, "Temporal bootstrap attempts.")
	fs.DurationVar(&opts.retryDelay, "retry-delay", opts.retryDelay, "Delay between bootstrap attempts.")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	opts.frontendAddress = strings.TrimSpace(opts.frontendAddress)
	opts.resourceGraph = strings.TrimSpace(opts.resourceGraph)
	opts.resourceName = strings.TrimSpace(opts.resourceName)
	opts.spiffeSocket = strings.TrimSpace(opts.spiffeSocket)
	if opts.frontendAddress == "" || opts.resourceGraph == "" || opts.resourceName == "" || opts.spiffeSocket == "" {
		return options{}, errors.New("--frontend-address, --resource-graph, --resource-name, and --spiffe-socket are required")
	}
	if opts.attempts <= 0 || opts.retryDelay <= 0 {
		return options{}, errors.New("--attempts and --retry-delay must be positive")
	}
	if !filepath.IsAbs(opts.resourceGraph) {
		return options{}, errors.New("--resource-graph must be an absolute path")
	}
	return opts, nil
}

func loadSpecs(opts options) ([]namespaceadmin.Spec, error) {
	body, err := os.ReadFile(opts.resourceGraph)
	if err != nil {
		return nil, fmt.Errorf("read Guardian resource graph: %w", err)
	}
	var doc document
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode Guardian resource graph: %w", err)
	}
	for _, resource := range doc.Resources {
		if resource.APIVersion != "temporal.guardianintelligence.org/v1alpha1" || resource.Kind != "TemporalPlatform" || resource.Metadata.Name != opts.resourceName {
			continue
		}
		var spec temporalSpec
		specDecoder := json.NewDecoder(bytes.NewReader(resource.Spec))
		if err := specDecoder.Decode(&spec); err != nil {
			return nil, fmt.Errorf("decode TemporalPlatform spec: %w", err)
		}
		return namespaceSpecs(spec)
	}
	return nil, fmt.Errorf("guardian resource graph missing TemporalPlatform %q", opts.resourceName)
}

func namespaceSpecs(spec temporalSpec) ([]namespaceadmin.Spec, error) {
	if len(spec.Bootstrap.Namespaces) == 0 {
		return nil, errors.New("TemporalPlatform.spec.bootstrap.namespaces must contain at least one namespace")
	}
	seen := map[string]struct{}{}
	specs := make([]namespaceadmin.Spec, 0, len(spec.Bootstrap.Namespaces))
	for _, item := range spec.Bootstrap.Namespaces {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return nil, errors.New("TemporalPlatform.spec.bootstrap.namespaces entries require name")
		}
		if _, exists := seen[name]; exists {
			continue
		}
		retention, err := time.ParseDuration(strings.TrimSpace(item.Retention))
		if err != nil {
			return nil, fmt.Errorf("parse retention for namespace %s: %w", name, err)
		}
		seen[name] = struct{}{}
		specs = append(specs, namespaceadmin.Spec{
			Name:      name,
			Retention: retention,
		})
	}
	return specs, nil
}
