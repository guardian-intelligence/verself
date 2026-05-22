package main

import (
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/deploymodel"
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

func recordDeploySucceeded(span trace.Span, plan *deployPlan, results []jobRunResult, startedAt time.Time) {
	submitted := submittedJobIDs(results)
	span.SetAttributes(
		attribute.StringSlice("verself.submitted_jobs", submitted),
		attribute.Int("verself.submitted_job_count", len(submitted)),
		attribute.Int64("verself.deploy.duration_ms", time.Since(startedAt).Milliseconds()),
	)
	span.AddEvent("verself.deploy.succeeded", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", plan.Identity.RunKey()),
		attribute.StringSlice("verself.submitted_jobs", submitted),
		attribute.Int("verself.submitted_job_count", len(submitted)),
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

func recordNomadRunStarted(span trace.Span, runKey, site string, job deploymodel.NomadJob, startedAt time.Time) {
	span.AddEvent("verself.nomad.run_started", trace.WithTimestamp(startedAt), trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("verself.deploy_wave", job.DeployPhase),
		attribute.String("verself.spec_sha256", job.SpecSHA256),
		attribute.String("verself.artifact_sha256", job.ArtifactSHA256),
		attribute.String("verself.input_sha256", job.InputSHA256),
		attribute.StringSlice("verself.dependency_units", job.DependsOn),
	))
}

func recordNomadRunSucceeded(span trace.Span, runKey, site string, job deploymodel.NomadJob, duration time.Duration, stdout string) {
	span.AddEvent("verself.nomad.run_succeeded", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("verself.deploy_wave", job.DeployPhase),
		attribute.String("verself.spec_sha256", job.SpecSHA256),
		attribute.Int64("verself.duration_ms", duration.Milliseconds()),
		attribute.String("nomad.stdout", truncateErrorString(stdout)),
	))
}

func recordNomadRunFailed(span trace.Span, runKey, site string, job deploymodel.NomadJob, duration time.Duration, stderr string, err error) {
	span.AddEvent("verself.nomad.run_failed", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("verself.deploy_wave", job.DeployPhase),
		attribute.String("verself.spec_sha256", job.SpecSHA256),
		attribute.Int64("verself.duration_ms", duration.Milliseconds()),
		attribute.String("nomad.stderr", truncateErrorString(stderr)),
		attribute.String("error.message", truncateError(err)),
	))
}

func recordDeployWaveStarted(span trace.Span, runKey, site, wave string, jobs []deploymodel.NomadJob, artifacts []deploymodel.Artifact, startedAt time.Time) {
	span.AddEvent("verself.deploy.wave_started", trace.WithTimestamp(startedAt), trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("verself.deploy_wave", wave),
		attribute.Int("verself.nomad_job_count", len(jobs)),
		attribute.Int("verself.submitted_job_count", len(jobs)),
		attribute.Int("verself.artifact_count", len(artifacts)),
	))
}

func recordDeployWaveSucceeded(span trace.Span, runKey, site, wave string, jobs []deploymodel.NomadJob, artifacts []deploymodel.Artifact, startedAt time.Time) {
	duration := time.Since(startedAt)
	span.AddEvent("verself.deploy.wave_succeeded", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("verself.deploy_wave", wave),
		attribute.Int("verself.nomad_job_count", len(jobs)),
		attribute.Int("verself.submitted_job_count", len(jobs)),
		attribute.Int("verself.artifact_count", len(artifacts)),
		attribute.Int64("verself.duration_ms", duration.Milliseconds()),
	))
}

func recordDeployWaveFailed(span trace.Span, runKey, site, wave string, jobs []deploymodel.NomadJob, artifacts []deploymodel.Artifact, startedAt time.Time, err error) {
	duration := time.Since(startedAt)
	span.AddEvent("verself.deploy.wave_failed", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("verself.deploy_wave", wave),
		attribute.Int("verself.nomad_job_count", len(jobs)),
		attribute.Int("verself.submitted_job_count", len(jobs)),
		attribute.Int("verself.artifact_count", len(artifacts)),
		attribute.Int64("verself.duration_ms", duration.Milliseconds()),
		attribute.String("error.message", truncateError(err)),
	))
}

func submittedJobIDs(results []jobRunResult) []string {
	out := []string{}
	for _, result := range results {
		out = append(out, result.JobID)
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
