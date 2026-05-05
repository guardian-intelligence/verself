package main

import (
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/deploymodel"
	"github.com/verself/deployment-tools/internal/nomadclient"
)

func recordDeployStarted(span trace.Span, runKey, site, sha, actor string, startedAt time.Time) {
	span.SetAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("verself.deploy_sha", sha),
		attribute.String("verself.actor", actor),
	)
	span.AddEvent("verself.deploy.started", trace.WithTimestamp(startedAt), trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("verself.deploy_sha", sha),
		attribute.String("verself.actor", actor),
	))
}

func recordDeploySucceeded(span trace.Span, plan *deployPlan, results []jobApplyResult, startedAt time.Time) {
	changed := changedJobIDs(results)
	span.SetAttributes(
		attribute.StringSlice("verself.changed_jobs", changed),
		attribute.Int("verself.changed_job_count", len(changed)),
		attribute.Int64("verself.deploy.duration_ms", time.Since(startedAt).Milliseconds()),
	)
	span.AddEvent("verself.deploy.succeeded", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", plan.Identity.RunKey()),
		attribute.StringSlice("verself.changed_jobs", changed),
		attribute.Int("verself.changed_job_count", len(changed)),
		attribute.Int64("verself.deploy.duration_ms", time.Since(startedAt).Milliseconds()),
	))
}

func recordDeployFailed(span trace.Span, plan *deployPlan, runKey, site, sha string, startedAt time.Time, err error) {
	affected := []string{}
	if plan != nil {
		affected = jobIDs(plan.Jobs)
	}
	span.SetAttributes(
		attribute.StringSlice("verself.affected_jobs", affected),
		attribute.Int("verself.affected_job_count", len(affected)),
		attribute.Int64("verself.deploy.duration_ms", time.Since(startedAt).Milliseconds()),
	)
	span.AddEvent("verself.deploy.failed", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("verself.deploy_sha", sha),
		attribute.StringSlice("verself.affected_jobs", affected),
		attribute.Int64("verself.deploy.duration_ms", time.Since(startedAt).Milliseconds()),
		attribute.String("error.message", truncateError(err)),
	))
}

func recordNomadDecision(span trace.Span, runKey, site string, job deploymodel.NomadJob, decision nomadclient.Decision, duration time.Duration) {
	span.AddEvent("verself.nomad.decision", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("verself.spec_sha256", job.SpecSHA256),
		attribute.String("verself.artifact_sha256", job.ArtifactSHA256),
		attribute.String("nomad.prior_spec_sha256", decision.PriorSpecDigest),
		attribute.Int64("nomad.prior_job_modify_index", int64FromUint64(decision.PriorJobModifyIndex, "prior job modify index")),
		attribute.Int64("nomad.prior_version", int64FromUint64(decision.PriorVersion, "prior version")),
		attribute.Bool("nomad.prior_stopped", decision.PriorStopped),
		attribute.Bool("nomad.decision.noop", decision.NoOp),
		attribute.StringSlice("verself.dependency_units", job.DependsOn),
		attribute.Int64("verself.duration_ms", duration.Milliseconds()),
	))
}

func recordNomadSkipped(span trace.Span, runKey, site string, job deploymodel.NomadJob, decision nomadclient.Decision) {
	span.AddEvent("verself.nomad.skipped", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("verself.spec_sha256", job.SpecSHA256),
		attribute.String("nomad.prior_spec_sha256", decision.PriorSpecDigest),
		attribute.Bool("nomad.decision.noop", true),
	))
}

func recordNomadSubmitted(span trace.Span, runKey, site string, job deploymodel.NomadJob, decision nomadclient.Decision, submitted *nomadclient.SubmitResult, duration time.Duration) {
	span.AddEvent("verself.nomad.submitted", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("nomad.eval_id", submitted.EvalID),
		attribute.String("nomad.deployment_id", submitted.DeploymentID),
		attribute.Int64("nomad.job_modify_index", int64FromUint64(submitted.JobModifyIndex, "job modify index")),
		attribute.Int64("nomad.prior_job_modify_index", int64FromUint64(decision.PriorJobModifyIndex, "prior job modify index")),
		attribute.Int64("verself.duration_ms", duration.Milliseconds()),
	))
}

func recordNomadSubmitFailed(span trace.Span, runKey, site string, job deploymodel.NomadJob, decision nomadclient.Decision, duration time.Duration, err error) {
	span.AddEvent("verself.nomad.submit_failed", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("verself.spec_sha256", job.SpecSHA256),
		attribute.String("nomad.prior_spec_sha256", decision.PriorSpecDigest),
		attribute.Int64("verself.duration_ms", duration.Milliseconds()),
		attribute.String("error.message", truncateError(err)),
	))
}

func recordNomadDeploymentSucceeded(span trace.Span, runKey, site string, job deploymodel.NomadJob, submitted *nomadclient.SubmitResult, monitor nomadclient.MonitorResult, duration time.Duration) {
	span.AddEvent("verself.nomad.deployment_succeeded", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("nomad.eval_id", submitted.EvalID),
		attribute.String("nomad.deployment_id", monitor.DeploymentID),
		attribute.String("nomad.terminal_status", monitor.TerminalStatus),
		attribute.Int("nomad.tg.desired_total", monitor.DesiredTotal),
		attribute.Int("nomad.tg.healthy", monitor.HealthyTotal),
		attribute.Int("nomad.tg.unhealthy", monitor.UnhealthyTotal),
		attribute.Int("nomad.tg.placed", monitor.PlacedTotal),
		attribute.Int64("verself.duration_ms", duration.Milliseconds()),
	))
}

func recordNomadDeploymentFailed(span trace.Span, runKey, site string, job deploymodel.NomadJob, submitted *nomadclient.SubmitResult, monitor nomadclient.MonitorResult, duration time.Duration, err error) {
	span.AddEvent("verself.nomad.deployment_failed", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("nomad.eval_id", submitted.EvalID),
		attribute.String("nomad.deployment_id", monitor.DeploymentID),
		attribute.String("nomad.terminal_status", monitor.TerminalStatus),
		attribute.String("nomad.status_description", monitor.StatusDescription),
		attribute.Int("nomad.tg.desired_total", monitor.DesiredTotal),
		attribute.Int("nomad.tg.healthy", monitor.HealthyTotal),
		attribute.Int("nomad.tg.unhealthy", monitor.UnhealthyTotal),
		attribute.Int("nomad.tg.placed", monitor.PlacedTotal),
		attribute.Int64("verself.duration_ms", duration.Milliseconds()),
		attribute.String("error.message", truncateError(err)),
	))
}

func changedJobIDs(results []jobApplyResult) []string {
	out := []string{}
	for _, result := range results {
		if result.Changed {
			out = append(out, result.JobID)
		}
	}
	return out
}

func jobIDs(jobs []deploymodel.NomadJob) []string {
	out := make([]string, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, job.JobID)
	}
	return out
}
