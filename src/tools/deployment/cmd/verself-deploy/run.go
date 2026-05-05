package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/deploydb"
	"github.com/verself/deployment-tools/internal/identity"
	"github.com/verself/deployment-tools/internal/runtime"
)

const deployScope = "nomad"

type runOptions struct {
	Site     string
	SHA      string
	RepoRoot string
}

func runRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	site := fs.String("site", "prod", "deployment site")
	sha := fs.String("sha", "", "git SHA being deployed")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rr := *repoRoot
	if rr == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy run: cwd: %v\n", err)
			return 1
		}
		rr = cwd
	}
	if err := run(context.Background(), runOptions{Site: *site, SHA: *sha, RepoRoot: rr}); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy run: %v\n", err)
		return 1
	}
	return 0
}

func run(ctx context.Context, opts runOptions) error {
	if opts.Site == "" {
		return fmt.Errorf("site is required")
	}
	if opts.RepoRoot == "" {
		return fmt.Errorf("repo root is required")
	}
	snap, err := identity.Generate(identity.GenerateOptions{
		Site:  opts.Site,
		Sha:   opts.SHA,
		Scope: deployScope,
		Kind:  "nomad-deploy",
	})
	if err != nil {
		return fmt.Errorf("derive identity: %w", err)
	}
	snap.ApplyEnv()
	resolvedSHA := snap.Get("VERSELF_DEPLOY_SHA")
	if resolvedSHA == "" {
		resolvedSHA = snap.Get("VERSELF_COMMIT_SHA")
	}

	parentCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	rt, err := runtime.Init(parentCtx, runtime.Options{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Site:           opts.Site,
		RepoRoot:       opts.RepoRoot,
	})
	if err != nil {
		return fmt.Errorf("runtime init: %w", err)
	}
	defer rt.Close()

	runCtx, span := rt.Tracer.Start(rt.Ctx, "verself_deploy.run",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("verself.site", opts.Site),
			attribute.String("verself.deploy_scope", deployScope),
		),
	)
	defer span.End()

	startedAt := time.Now()
	if err := rt.DeployDB.RecordDeployEvent(runCtx, deploydb.DeployEvent{
		EventAt: startedAt,
		RunKey:  snap.RunKey(),
		Site:    opts.Site,
		Sha:     resolvedSHA,
		Actor:   snap.Get("VERSELF_AUTHOR"),
		Scope:   deployScope,
		Kind:    deploydb.EventStarted,
	}); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("record deploy start: %w", err)
	}

	plan, err := buildDeployPlan(runCtx, rt, opts.RepoRoot, opts.Site, resolvedSHA, snap)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		_ = recordDeployFailed(runCtx, rt.DeployDB, &deployPlan{
			Identity: snap,
			SHA:      resolvedSHA,
			Site:     opts.Site,
		}, startedAt, err)
		return err
	}
	span.SetAttributes(
		attribute.Int("verself.artifact_count", len(plan.Artifacts)),
		attribute.Int("verself.nomad_job_count", len(plan.Jobs)),
	)

	if err := publishPlanArtifacts(runCtx, rt, plan); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		_ = recordDeployFailed(runCtx, rt.DeployDB, plan, startedAt, err)
		return err
	}

	results, err := applyNomadPlan(runCtx, rt, plan)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		_ = recordDeployFailed(runCtx, rt.DeployDB, plan, startedAt, err)
		return err
	}

	if err := recordDeploySucceeded(runCtx, rt.DeployDB, plan, results, startedAt); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}
