// Command verself-deploy is the typed orchestrator for verself
// deploys. It owns Nomad submit/monitor today; subsequent phases pull
// in BEP-driven artifact resolution, the SSH tunnel multiplexer, the
// streaming Ansible parser, and the deploy ledger ClickHouse writer.
//
// Subcommand surface mirrors `aspect <group> <action> --flag=value`:
//
//	verself-deploy nomad submit --spec=<path> --nomad-addr=<url>
//
// Each subcommand initialises OpenTelemetry once via the shared
// verselfotel package, projects the deploy identity onto outgoing
// baggage so every span carries `verself.deploy_run_key` etc., and
// extracts a parent trace from the TRACEPARENT env (set by the AXL
// deploy task) so this binary's spans are children of the deploy's
// root span rather than orphans.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	verselfotel "github.com/verself/otel"

	"github.com/verself/deployment-tooling/internal/identity"
	"github.com/verself/deployment-tooling/internal/nomadclient"
)

const (
	serviceName    = "verself-deploy"
	serviceVersion = "0.1.0"
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
  verself-deploy nomad submit --spec=<path> --nomad-addr=<url> [--timeout=5m]

`)
}

func runNomad(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "verself-deploy nomad: missing subcommand (try `submit`)")
		return 2
	}
	switch args[0] {
	case "submit":
		return runNomadSubmit(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "verself-deploy nomad: unknown subcommand: %s\n", args[0])
		return 2
	}
}

func runNomadSubmit(args []string) int {
	fs := flag.NewFlagSet("nomad submit", flag.ContinueOnError)
	specPath := fs.String("spec", "", "path to a rendered Nomad job spec (.nomad.json)")
	nomadAddr := fs.String("nomad-addr", "", "Nomad agent HTTP address (e.g. http://127.0.0.1:4646)")
	deployTimeout := fs.Duration("timeout", 5*time.Minute, "deployment-monitor wall-clock timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *specPath == "" || *nomadAddr == "" {
		fmt.Fprintln(os.Stderr, "verself-deploy nomad submit: --spec and --nomad-addr are required")
		fs.Usage()
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdown, err := initOTel(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy: otel init: %v\n", err)
		return 1
	}
	defer shutdown()

	ctx = identity.Inject(ctx)
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{
		"traceparent": os.Getenv("TRACEPARENT"),
		"tracestate":  os.Getenv("TRACESTATE"),
	})

	tracer := otel.Tracer(serviceName)
	ctx, span := tracer.Start(ctx, "verself_deploy.nomad.submit",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("nomad.addr", *nomadAddr),
			attribute.String("verself.spec_path", *specPath),
		),
	)
	defer span.End()

	monitorCtx, cancelMonitor := context.WithTimeout(ctx, *deployTimeout)
	defer cancelMonitor()

	if err := submitOnce(monitorCtx, span, *nomadAddr, *specPath); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		fmt.Fprintf(os.Stderr, "verself-deploy nomad submit: %v\n", err)
		return exitCodeFor(err)
	}
	span.SetStatus(codes.Ok, "")
	return 0
}

func submitOnce(ctx context.Context, parent trace.Span, addr, specPath string) error {
	spec, err := nomadclient.LoadSpec(specPath)
	if err != nil {
		return err
	}
	parent.SetAttributes(attribute.String("nomad.job_id", spec.JobID()))

	client, err := nomadclient.New(addr)
	if err != nil {
		return err
	}

	decision, err := client.Decide(ctx, spec)
	if err != nil {
		return err
	}
	parent.SetAttributes(attribute.Bool("verself.noop", decision.NoOp))
	if decision.NoOp {
		fmt.Fprintf(os.Stdout, "verself-deploy: %s already at desired digests; no submit\n", spec.JobID())
		return nil
	}

	submitted, err := client.Submit(ctx, spec, decision.PriorJobModifyIndex)
	if err != nil {
		return err
	}
	parent.SetAttributes(
		attribute.String("nomad.eval_id", submitted.EvalID),
		attribute.Int64("nomad.job_modify_index", int64(submitted.JobModifyIndex)),
		attribute.String("nomad.deployment_id", submitted.DeploymentID),
	)
	fmt.Fprintf(os.Stdout, "verself-deploy: %s submitted job_modify_index=%d eval_id=%s deployment_id=%s\n",
		submitted.JobID, submitted.JobModifyIndex, submitted.EvalID, submitted.DeploymentID)

	if err := client.Monitor(ctx, submitted); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "verself-deploy: %s healthy\n", submitted.JobID)
	return nil
}

// exitCodeFor distinguishes "deployment ended badly" (exit 1, the
// canonical failure) from other errors. The orchestrator (nomad-deploy-all.sh
// today, the Phase 2 Go fan-out tomorrow) treats both as fatal but
// future tooling may want to branch.
func exitCodeFor(err error) int {
	var terminal *nomadclient.TerminalError
	if errors.As(err, &terminal) {
		return 1
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return 1
	}
	return 1
}

// initOTel wires the shared verselfotel package and returns a shutdown
// closure that flushes pending spans within a bounded budget. The
// budget matches with-otel-agent.sh's grace window so a fast-exiting
// child doesn't drop spans.
func initOTel(ctx context.Context) (func(), error) {
	shutdown, _, err := verselfotel.Init(ctx, verselfotel.Config{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
	})
	if err != nil {
		return nil, err
	}
	return func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = shutdown(flushCtx)
	}, nil
}
