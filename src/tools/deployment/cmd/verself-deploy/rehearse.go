package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/deploymodel"
	"github.com/verself/deployment-tools/internal/identity"
	"github.com/verself/deployment-tools/internal/nomadclient"
	"github.com/verself/deployment-tools/internal/nomadlint"
	"github.com/verself/deployment-tools/internal/runtime"
)

type rehearseOptions struct {
	Site     string
	SHA      string
	RepoRoot string
}

type nomadRehearsal struct {
	Intents []jobApplyIntent
}

func runRehearse(args []string) int {
	fs := flag.NewFlagSet("rehearse", flag.ContinueOnError)
	site := fs.String("site", "prod", "deployment site")
	sha := fs.String("sha", "", "git SHA being rehearsed")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rr := *repoRoot
	if rr == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy rehearse: cwd: %v\n", err)
			return 1
		}
		rr = cwd
	}
	if err := rehearse(context.Background(), rehearseOptions{Site: *site, SHA: *sha, RepoRoot: rr}); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy rehearse: %v\n", err)
		return 1
	}
	return 0
}

func rehearse(ctx context.Context, opts rehearseOptions) error {
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
		Kind:  "nomad-rehearsal",
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

	runCtx, span := rt.Tracer.Start(rt.Ctx, "verself_deploy.rehearse",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("verself.site", opts.Site),
			attribute.String("verself.deploy_scope", deployScope),
		),
	)
	defer span.End()

	startedAt := time.Now()
	recordRehearsalStarted(span, snap.RunKey(), opts.Site, resolvedSHA, startedAt)
	plan, err := buildDeployPlan(runCtx, rt, opts.RepoRoot, opts.Site, resolvedSHA, snap)
	if err != nil {
		recordRehearsalFailed(span, snap.RunKey(), opts.Site, resolvedSHA, nil, startedAt, err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(
		attribute.Int("verself.artifact_count", len(plan.Artifacts)),
		attribute.Int("verself.nomad_job_count", len(plan.Jobs)),
	)
	rehearsal, err := rehearseWithForward(runCtx, rt, plan)
	printRehearsalReport(rehearsal)
	if err != nil {
		recordRehearsalFailed(span, snap.RunKey(), opts.Site, resolvedSHA, rehearsal, startedAt, err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	recordRehearsalSucceeded(span, snap.RunKey(), opts.Site, resolvedSHA, rehearsal, startedAt)
	span.SetStatus(codes.Ok, "")
	return nil
}

func rehearseWithForward(ctx context.Context, rt *runtime.Runtime, plan *deployPlan) (*nomadRehearsal, error) {
	forward, err := openNomadForward(ctx, rt, plan.SiteCfg.NomadAddr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = forward.Close() }()
	client, err := nomadclient.New("http://" + forward.ListenAddr)
	if err != nil {
		return nil, err
	}
	return rehearseNomadPlan(ctx, rt, client, plan)
}

func rehearseNomadPlan(ctx context.Context, rt *runtime.Runtime, client *nomadclient.Client, plan *deployPlan) (*nomadRehearsal, error) {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.nomad.rehearsal",
		trace.WithAttributes(
			attribute.Int("verself.nomad_job_count", len(plan.Jobs)),
		),
	)
	defer span.End()
	started := time.Now()
	recordNomadRehearsalStarted(span, rt.Identity.RunKey(), rt.Site, len(plan.Jobs), started)
	nodeCapacities, err := client.NodeCapacities(ctx)
	if err != nil {
		recordNomadRehearsalFailed(span, rt.Identity.RunKey(), rt.Site, &nomadRehearsal{}, time.Since(started), err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(attribute.Int("nomad.node_capacity_count", len(nodeCapacities)))
	lintCapacities := lintNodeCapacities(nodeCapacities)

	rehearsal := &nomadRehearsal{Intents: make([]jobApplyIntent, 0, len(plan.Jobs))}
	var failures []string
	for _, job := range plan.Jobs {
		intent, err := rehearseNomadJob(ctx, rt, client, job, lintCapacities)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", job.JobID, err))
		}
		rehearsal.Intents = append(rehearsal.Intents, intent)
	}
	if len(failures) > 0 {
		err := fmt.Errorf("nomad rehearsal failed: %s", strings.Join(failures, "; "))
		recordNomadRehearsalFailed(span, rt.Identity.RunKey(), rt.Site, rehearsal, time.Since(started), err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return rehearsal, err
	}
	recordNomadRehearsalSucceeded(span, rt.Identity.RunKey(), rt.Site, rehearsal, time.Since(started))
	span.SetStatus(codes.Ok, "")
	return rehearsal, nil
}

func lintNodeCapacities(capacities []nomadclient.NodeCapacity) []nomadlint.NodeCapacity {
	out := make([]nomadlint.NodeCapacity, 0, len(capacities))
	for _, capacity := range capacities {
		out = append(out, nomadlint.NodeCapacity{
			NodeID:   capacity.NodeID,
			Name:     capacity.Name,
			Class:    capacity.Class,
			CPU:      capacity.CPU,
			MemoryMB: capacity.MemoryMB,
			DiskMB:   capacity.DiskMB,
		})
	}
	return out
}

func rehearseNomadJob(ctx context.Context, rt *runtime.Runtime, client *nomadclient.Client, job deploymodel.NomadJob, capacities []nomadlint.NodeCapacity) (jobApplyIntent, error) {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.nomad.rehearse_job",
		trace.WithAttributes(
			attribute.String("nomad.job_id", job.JobID),
			attribute.String("verself.deploy_wave", job.DeployPhase),
		),
	)
	defer span.End()
	intent := jobApplyIntent{Job: job}
	spec, err := nomadclient.ParseSpec(job.Spec, "nomad job "+job.JobID)
	if err != nil {
		intent.RehearsalError = err.Error()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return intent, err
	}
	intent.Spec = spec
	if spec.SpecDigest != job.SpecSHA256 {
		err := fmt.Errorf("job spec digest mismatch: descriptor=%s spec=%s", job.SpecSHA256, spec.SpecDigest)
		intent.RehearsalError = err.Error()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return intent, err
	}
	if spec.ArtifactDigest != job.ArtifactSHA256 {
		err := fmt.Errorf("job artifact digest mismatch: descriptor=%s spec=%s", job.ArtifactSHA256, spec.ArtifactDigest)
		intent.RehearsalError = err.Error()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return intent, err
	}
	intent.Lint = nomadlint.LintWithCapacity(spec.Job, capacities)
	recordNomadLint(span, rt.Identity.RunKey(), rt.Site, job, intent.Lint)
	validation, err := client.Validate(ctx, spec)
	if err != nil {
		intent.RehearsalError = err.Error()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return intent, err
	}
	intent.Validation = validation
	recordNomadValidate(span, rt.Identity.RunKey(), rt.Site, job, validation)
	if intent.Lint.Failed() || validation.Failed() {
		err := fmt.Errorf("lint_errors=%d validation_errors=%d", intent.Lint.ErrorCount(), len(validation.Errors))
		intent.RehearsalError = err.Error()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return intent, err
	}
	decisionStarted := time.Now()
	decision, err := client.Decide(ctx, spec)
	if err != nil {
		intent.RehearsalError = err.Error()
		recordNomadSubmitFailed(span, rt.Identity.RunKey(), rt.Site, job, decision, time.Since(decisionStarted), err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return intent, err
	}
	intent.Decision = decision
	intent.Changed = !decision.NoOp
	recordNomadDecision(span, rt.Identity.RunKey(), rt.Site, job, decision, time.Since(decisionStarted))
	plan, err := client.Plan(ctx, spec)
	if err != nil {
		intent.RehearsalError = err.Error()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return intent, err
	}
	intent.Plan = plan
	recordNomadPlan(span, rt.Identity.RunKey(), rt.Site, job, plan)
	if plan.Failed() {
		err := fmt.Errorf("scheduler placement failed")
		intent.RehearsalError = err.Error()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return intent, err
	}
	if plan.JobModifyIndex != decision.PriorJobModifyIndex {
		err := fmt.Errorf("planned job_modify_index=%d does not match inspected prior index=%d", plan.JobModifyIndex, decision.PriorJobModifyIndex)
		intent.RehearsalError = err.Error()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return intent, err
	}
	if decision.NoOp {
		recordNomadSkipped(span, rt.Identity.RunKey(), rt.Site, job, decision)
	}
	span.SetAttributes(attribute.Bool("nomad.decision.noop", decision.NoOp))
	span.SetStatus(codes.Ok, "")
	return intent, nil
}

func printRehearsalReport(rehearsal *nomadRehearsal) {
	if rehearsal == nil {
		return
	}
	fmt.Println("verself-deploy: nomad rehearsal")
	fmt.Println("phase\tjob\tresult\tplan")
	for _, intent := range rehearsal.Intents {
		var result string
		if intent.Lint.Failed() || intent.Validation.Failed() || intent.Plan.Failed() || intent.RehearsalError != "" {
			result = "fail"
		} else if intent.Changed {
			result = "change"
		} else {
			result = "noop"
		}
		fmt.Printf("%s\t%s\t%s\t%s\n", intent.Job.DeployPhase, intent.Job.JobID, result, planSummary(intent.Plan))
		for _, finding := range intent.Lint.Findings {
			fmt.Printf("  %s %s group=%s task=%s %s %s\n", finding.Severity, finding.RuleID, finding.TaskGroup, finding.Task, finding.Message, finding.Evidence)
		}
		for _, failure := range intent.Plan.Failures {
			fmt.Printf("  error nomad.plan.placement_failed %s\n", failure.Summary())
		}
		if intent.Validation.Warnings != "" {
			fmt.Printf("  warn nomad.validate.warning %s\n", intent.Validation.Warnings)
		}
		if intent.Plan.Warnings != "" {
			fmt.Printf("  warn nomad.plan.warning %s\n", intent.Plan.Warnings)
		}
		if intent.RehearsalError != "" {
			fmt.Printf("  error nomad.rehearsal %s\n", intent.RehearsalError)
		}
	}
}

func planSummary(plan nomadclient.PlanResult) string {
	if plan.Failed() {
		return "placement_failed"
	}
	if len(plan.Updates) == 0 {
		if plan.DiffType == "" {
			return "-"
		}
		return plan.DiffType
	}
	parts := make([]string, 0, len(plan.Updates))
	for _, update := range plan.Updates {
		parts = append(parts, fmt.Sprintf("%s:%s=%d", update.Group, update.Kind, update.Desired))
	}
	return strings.Join(parts, ",")
}
