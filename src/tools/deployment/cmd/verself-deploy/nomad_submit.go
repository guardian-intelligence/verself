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

	"github.com/verself/deployment-tools/internal/deploydb"
	"github.com/verself/deployment-tools/internal/nomadclient"
	"github.com/verself/deployment-tools/internal/runtime"
)

func runNomadSubmit(args []string) int {
	fs := flag.NewFlagSet("nomad submit", flag.ContinueOnError)
	specPath := fs.String("spec", "", "path to a resolved Nomad job JSON spec")
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
		SkipClickHouse: true,
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

	if err := submitOnce(monitorCtx, span, client, *specPath, nomadJobEvidenceWriter{
		db:     rt.DeployDB,
		runKey: rt.Identity.RunKey(),
		site:   rt.Site,
	}); err != nil {
		recordFailure(span, err)
		fmt.Fprintf(os.Stderr, "verself-deploy nomad submit: %v\n", err)
		return 1
	}
	span.SetStatus(codes.Ok, "")
	return 0
}

type nomadJobEvidenceWriter struct {
	db               *deploydb.Client
	runKey           string
	site             string
	unitID           string
	unitDependencies []string
	unitPayloadKind  string
}

func (w nomadJobEvidenceWriter) record(ctx context.Context, ev deploydb.NomadJobEvent) error {
	if w.db == nil {
		return nil
	}
	ev.RunKey = w.runKey
	ev.Site = w.site
	return w.db.RecordNomadJobEvent(ctx, ev)
}

func (w nomadJobEvidenceWriter) recordUnit(ctx context.Context, ev deploydb.DeployUnitEvent) error {
	if w.db == nil || w.unitID == "" {
		return nil
	}
	ev.RunKey = w.runKey
	ev.Site = w.site
	ev.Executor = deploydb.DeployExecutorNomad
	ev.UnitID = w.unitID
	if ev.PayloadKind == "" {
		ev.PayloadKind = w.unitPayloadKind
	}
	ev.DependencyUnits = append([]string(nil), w.unitDependencies...)
	return w.db.RecordDeployUnitEvent(ctx, ev)
}

func submitOnce(ctx context.Context, parent trace.Span, client *nomadclient.Client, specPath string, evidence nomadJobEvidenceWriter) error {
	spec, err := nomadclient.LoadSpec(specPath)
	if err != nil {
		return err
	}
	return submitSpec(ctx, parent, client, spec, evidence)
}

func submitSpec(ctx context.Context, parent trace.Span, client *nomadclient.Client, spec *nomadclient.Spec, evidence nomadJobEvidenceWriter) error {
	parent.SetAttributes(attribute.String("nomad.job_id", spec.JobID()))

	stageStartedAt := time.Now()
	decision, err := client.Decide(ctx, spec)
	if err != nil {
		if recordErr := evidence.record(ctx, deploydb.NomadJobEvent{
			JobID:          spec.JobID(),
			Kind:           deploydb.NomadJobEventSubmitFailed,
			SpecSHA256:     spec.SpecDigest,
			ArtifactSHA256: spec.ArtifactDigest,
			DurationMs:     durationMillis(time.Since(stageStartedAt), "nomad decision duration"),
			ErrorMessage:   err.Error(),
		}); recordErr != nil {
			return recordErr
		}
		return err
	}
	parent.SetAttributes(attribute.Bool("verself.noop", decision.NoOp))
	if err := evidence.recordUnit(ctx, deploydb.DeployUnitEvent{
		Kind:           deploydb.DeployUnitEventDecided,
		DesiredDigest:  spec.SpecDigest,
		ObservedDigest: decision.PriorSpecDigest,
		NoOp:           decision.NoOp,
		PayloadKind:    "nomad_job",
		DurationMs:     durationMillis(time.Since(stageStartedAt), "nomad decision duration"),
	}); err != nil {
		return err
	}
	if err := evidence.record(ctx, deploydb.NomadJobEvent{
		JobID:               spec.JobID(),
		Kind:                deploydb.NomadJobEventDecided,
		SpecSHA256:          spec.SpecDigest,
		ArtifactSHA256:      spec.ArtifactDigest,
		PriorJobModifyIndex: decision.PriorJobModifyIndex,
		PriorVersion:        decision.PriorVersion,
		PriorStopped:        decision.PriorStopped,
		NoOp:                decision.NoOp,
		DurationMs:          durationMillis(time.Since(stageStartedAt), "nomad decision duration"),
	}); err != nil {
		return err
	}
	if decision.NoOp {
		if err := evidence.recordUnit(ctx, deploydb.DeployUnitEvent{
			Kind:           deploydb.DeployUnitEventSkipped,
			DesiredDigest:  spec.SpecDigest,
			ObservedDigest: decision.PriorSpecDigest,
			NoOp:           true,
			PayloadKind:    "nomad_job",
		}); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(os.Stdout, "verself-deploy: %s already at desired digests; no submit\n", spec.JobID())
		return nil
	}

	stageStartedAt = time.Now()
	submitted, err := client.Submit(ctx, spec, decision.PriorJobModifyIndex)
	if err != nil {
		unitErr := evidence.recordUnit(ctx, deploydb.DeployUnitEvent{
			Kind:           deploydb.DeployUnitEventFailed,
			DesiredDigest:  spec.SpecDigest,
			ObservedDigest: decision.PriorSpecDigest,
			PayloadKind:    "nomad_job",
			DurationMs:     durationMillis(time.Since(stageStartedAt), "nomad submit duration"),
			ErrorMessage:   err.Error(),
		})
		if recordErr := evidence.record(ctx, deploydb.NomadJobEvent{
			JobID:               spec.JobID(),
			Kind:                deploydb.NomadJobEventSubmitFailed,
			SpecSHA256:          spec.SpecDigest,
			ArtifactSHA256:      spec.ArtifactDigest,
			PriorJobModifyIndex: decision.PriorJobModifyIndex,
			PriorVersion:        decision.PriorVersion,
			PriorStopped:        decision.PriorStopped,
			DurationMs:          durationMillis(time.Since(stageStartedAt), "nomad submit duration"),
			ErrorMessage:        err.Error(),
		}); recordErr != nil || unitErr != nil {
			return errors.Join(recordErr, unitErr)
		}
		return err
	}
	parent.SetAttributes(
		attribute.String("nomad.eval_id", submitted.EvalID),
		attribute.Int64("nomad.job_modify_index", int64FromUint64(submitted.JobModifyIndex, "job modify index")),
		attribute.String("nomad.deployment_id", submitted.DeploymentID),
	)
	_, _ = fmt.Fprintf(os.Stdout, "verself-deploy: %s submitted job_modify_index=%d eval_id=%s deployment_id=%s\n",
		submitted.JobID, submitted.JobModifyIndex, submitted.EvalID, submitted.DeploymentID)
	if err := evidence.record(ctx, deploydb.NomadJobEvent{
		JobID:               spec.JobID(),
		Kind:                deploydb.NomadJobEventSubmitted,
		SpecSHA256:          spec.SpecDigest,
		ArtifactSHA256:      spec.ArtifactDigest,
		PriorJobModifyIndex: decision.PriorJobModifyIndex,
		PriorVersion:        decision.PriorVersion,
		PriorStopped:        decision.PriorStopped,
		EvalID:              submitted.EvalID,
		DeploymentID:        submitted.DeploymentID,
		JobModifyIndex:      submitted.JobModifyIndex,
		DurationMs:          durationMillis(time.Since(stageStartedAt), "nomad submit duration"),
	}); err != nil {
		return err
	}

	stageStartedAt = time.Now()
	monitorResult, err := client.Monitor(ctx, submitted)
	eventKind := deploydb.NomadJobEventDeploymentSucceeded
	unitKind := deploydb.DeployUnitEventSucceeded
	errorMessage := ""
	if err != nil {
		eventKind = deploydb.NomadJobEventDeploymentFailed
		unitKind = deploydb.DeployUnitEventFailed
		errorMessage = err.Error()
	}
	unitRecordErr := evidence.recordUnit(ctx, deploydb.DeployUnitEvent{
		Kind:           unitKind,
		DesiredDigest:  spec.SpecDigest,
		ObservedDigest: spec.SpecDigest,
		PayloadKind:    "nomad_job",
		DurationMs:     durationMillis(time.Since(stageStartedAt), "nomad monitor duration"),
		ErrorMessage:   errorMessage,
	})
	if recordErr := evidence.record(ctx, deploydb.NomadJobEvent{
		JobID:               spec.JobID(),
		Kind:                eventKind,
		SpecSHA256:          spec.SpecDigest,
		ArtifactSHA256:      spec.ArtifactDigest,
		PriorJobModifyIndex: decision.PriorJobModifyIndex,
		PriorVersion:        decision.PriorVersion,
		PriorStopped:        decision.PriorStopped,
		EvalID:              submitted.EvalID,
		DeploymentID:        monitorResult.DeploymentID,
		JobModifyIndex:      submitted.JobModifyIndex,
		DesiredTotal:        uint16FromInt(monitorResult.DesiredTotal, "nomad desired total"),
		HealthyTotal:        uint16FromInt(monitorResult.HealthyTotal, "nomad healthy total"),
		UnhealthyTotal:      uint16FromInt(monitorResult.UnhealthyTotal, "nomad unhealthy total"),
		PlacedTotal:         uint16FromInt(monitorResult.PlacedTotal, "nomad placed total"),
		TerminalStatus:      monitorResult.TerminalStatus,
		DurationMs:          durationMillis(time.Since(stageStartedAt), "nomad monitor duration"),
		ErrorMessage:        errorMessage,
	}); recordErr != nil || unitRecordErr != nil {
		return errors.Join(recordErr, unitRecordErr)
	}
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "verself-deploy: %s healthy\n", submitted.JobID)
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
