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

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tooling/internal/nomadclient"
	"github.com/verself/deployment-tooling/internal/runtime"
)

func runNomadSubmit(args []string) int {
	fs := flag.NewFlagSet("nomad submit", flag.ContinueOnError)
	specPath := fs.String("spec", "", "path to a rendered Nomad job spec (.nomad.json)")
	nomadAddr := fs.String("nomad-addr", "", "Nomad agent HTTP address; if empty, the binary opens an SSH-forwarded tunnel to the controller")
	site := fs.String("site", "prod", "site label (selects inventory and agent queue dir)")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	deployTimeout := fs.Duration("timeout", 5*time.Minute, "deployment-monitor wall-clock timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *specPath == "" {
		fmt.Fprintln(os.Stderr, "verself-deploy nomad submit: --spec is required")
		fs.Usage()
		return 2
	}

	parentCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rt, err := runtime.Init(parentCtx, runtime.Options{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Site:           *site,
		RepoRoot:       *repoRoot,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy nomad submit: %v\n", err)
		return 1
	}
	defer rt.Close()

	addr := *nomadAddr
	if addr == "" {
		fwd, err := rt.SSH.Forward(rt.Ctx, "nomad", defaultNomadRemotePort)
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy nomad submit: open nomad forward: %v\n", err)
			return 1
		}
		addr = "http://" + fwd.ListenAddr
	}

	ctx, span := rt.Tracer.Start(rt.Ctx, "verself_deploy.nomad.submit",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("nomad.addr", addr),
			attribute.String("verself.spec_path", *specPath),
		),
	)
	defer span.End()

	monitorCtx, cancelMonitor := context.WithTimeout(ctx, *deployTimeout)
	defer cancelMonitor()

	client, err := nomadclient.New(addr)
	if err != nil {
		recordFailure(span, err)
		fmt.Fprintf(os.Stderr, "verself-deploy nomad submit: %v\n", err)
		return 1
	}

	if err := submitOnce(monitorCtx, span, client, *specPath); err != nil {
		recordFailure(span, err)
		fmt.Fprintf(os.Stderr, "verself-deploy nomad submit: %v\n", err)
		return 1
	}
	span.SetStatus(codes.Ok, "")
	return 0
}

func submitOnce(ctx context.Context, parent trace.Span, client *nomadclient.Client, specPath string) error {
	spec, err := nomadclient.LoadSpec(specPath)
	if err != nil {
		return err
	}
	parent.SetAttributes(attribute.String("nomad.job_id", spec.JobID()))

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

func recordFailure(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	var terminal *nomadclient.TerminalError
	if errors.As(err, &terminal) {
		span.SetAttributes(
			attribute.String("nomad.terminal_status", terminal.Status),
			attribute.String("nomad.status_description", terminal.StatusDescription),
			attribute.String("nomad.alloc_failure_reason", terminal.Reason),
		)
	}
}
