// Command verself-deploy is the typed orchestrator for verself
// deploys. It owns Bazel-driven artifact discovery (via BEP),
// SSH-tunneled Garage publish, and Nomad submit/monitor.
//
// Subcommand surface mirrors `aspect <group> <action>`:
//
//	verself-deploy nomad submit     --spec=<path> --nomad-addr=<url>
//	verself-deploy nomad deploy-all --site=<site> [--repo-root=<path>]
//
// Each subcommand initialises OpenTelemetry once via the shared
// verselfotel package, projects the deploy identity onto outgoing
// W3C baggage so every span carries `verself.deploy_run_key` etc.,
// and extracts a parent trace from the TRACEPARENT env (set by the
// AXL deploy task) so this binary's spans are children of the
// deploy's root span rather than orphans.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	verselfotel "github.com/verself/otel"

	"github.com/verself/deployment-tooling/internal/identity"
)

const (
	serviceName    = "verself-deploy"
	serviceVersion = "0.2.0"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "nomad":
		os.Exit(runNomad(os.Args[2:]))
	case "-h", "--help", "help":
		usage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "verself-deploy: unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `verself-deploy — typed orchestrator for verself deploys

usage:
  verself-deploy nomad submit     --spec=<path> --nomad-addr=<url> [--timeout=5m]
  verself-deploy nomad deploy-all --site=<site> [--repo-root=<path>]

`)
}

func runNomad(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "verself-deploy nomad: missing subcommand (try `submit` or `deploy-all`)")
		return 2
	}
	switch args[0] {
	case "submit":
		return runNomadSubmit(args[1:])
	case "deploy-all":
		return runNomadDeployAll(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "verself-deploy nomad: unknown subcommand: %s\n", args[0])
		return 2
	}
}

// initContext sets up the per-process OTel + identity scaffolding.
// All subcommands route through here so the contract — service name,
// identity baggage, parent trace extraction — is in one place.
func initContext() (context.Context, func(), context.CancelFunc, error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	shutdown, _, err := verselfotel.Init(ctx, verselfotel.Config{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
	})
	if err != nil {
		stop()
		return nil, nil, nil, fmt.Errorf("otel init: %w", err)
	}
	flushOnExit := func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = shutdown(flushCtx)
	}
	ctx = identity.Inject(ctx)
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{
		"traceparent": os.Getenv("TRACEPARENT"),
		"tracestate":  os.Getenv("TRACESTATE"),
	})
	return ctx, flushOnExit, stop, nil
}
