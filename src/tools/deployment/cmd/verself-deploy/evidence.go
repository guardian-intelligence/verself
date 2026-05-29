package main

import (
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/deploymodel"
	"github.com/verself/deployment-tools/internal/nomadclient"
	"github.com/verself/deployment-tools/internal/nomadlint"
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

func recordRehearsalStarted(span trace.Span, runKey, site, sha string, startedAt time.Time) {
	span.SetAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("verself.deploy_sha", sha),
	)
	span.AddEvent("verself.rehearsal.started", trace.WithTimestamp(startedAt), trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("verself.deploy_sha", sha),
	))
}

func recordRehearsalSucceeded(span trace.Span, runKey, site, sha string, rehearsal *nomadRehearsal, startedAt time.Time) {
	span.SetAttributes(
		attribute.Int("verself.nomad_job_count", rehearsalJobCount(rehearsal)),
		attribute.Int("verself.changed_job_count", rehearsalChangedJobCount(rehearsal)),
		attribute.Int64("verself.deploy.duration_ms", time.Since(startedAt).Milliseconds()),
	)
	span.AddEvent("verself.rehearsal.succeeded", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("verself.deploy_sha", sha),
		attribute.Int("verself.nomad_job_count", rehearsalJobCount(rehearsal)),
		attribute.Int("verself.changed_job_count", rehearsalChangedJobCount(rehearsal)),
		attribute.Int64("verself.deploy.duration_ms", time.Since(startedAt).Milliseconds()),
	))
}

func recordRehearsalFailed(span trace.Span, runKey, site, sha string, rehearsal *nomadRehearsal, startedAt time.Time, err error) {
	span.SetAttributes(
		attribute.Int("verself.nomad_job_count", rehearsalJobCount(rehearsal)),
		attribute.Int("verself.failed_job_count", rehearsalFailedJobCount(rehearsal)),
		attribute.Int64("verself.deploy.duration_ms", time.Since(startedAt).Milliseconds()),
	)
	span.AddEvent("verself.rehearsal.failed", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("verself.deploy_sha", sha),
		attribute.Int("verself.nomad_job_count", rehearsalJobCount(rehearsal)),
		attribute.Int("verself.failed_job_count", rehearsalFailedJobCount(rehearsal)),
		attribute.Int64("verself.deploy.duration_ms", time.Since(startedAt).Milliseconds()),
		attribute.String("error.message", truncateError(err)),
	))
}

func recordNomadDecision(span trace.Span, runKey, site string, job deploymodel.NomadJob, decision nomadclient.Decision, duration time.Duration) {
	span.AddEvent("verself.nomad.decision", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("verself.deploy_wave", job.DeployPhase),
		attribute.String("verself.spec_sha256", job.SpecSHA256),
		attribute.String("verself.artifact_sha256", job.ArtifactSHA256),
		attribute.String("verself.input_sha256", job.InputSHA256),
		attribute.String("nomad.prior_spec_sha256", decision.PriorSpecDigest),
		attribute.Bool("nomad.prior_exists", decision.PriorExists),
		attribute.Int64("nomad.prior_job_modify_index", int64FromUint64(decision.PriorJobModifyIndex, "prior job modify index")),
		attribute.Int64("nomad.prior_version", int64FromUint64(decision.PriorVersion, "prior version")),
		attribute.Bool("nomad.prior_stopped", decision.PriorStopped),
		attribute.Bool("nomad.decision.noop", decision.NoOp),
		attribute.StringSlice("verself.dependency_units", job.DependsOn),
		attribute.Int64("verself.duration_ms", duration.Milliseconds()),
	))
}

func recordNomadLint(span trace.Span, runKey, site string, job deploymodel.NomadJob, result nomadlint.Result) {
	span.AddEvent("verself.nomad.lint", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("verself.deploy_wave", job.DeployPhase),
		attribute.Int("verself.nomad_lint_errors", result.ErrorCount()),
		attribute.Int("verself.nomad_lint_warnings", result.WarningCount()),
		attribute.StringSlice("verself.nomad_lint_rules", lintRuleIDs(result.Findings)),
	))
}

func recordNomadValidate(span trace.Span, runKey, site string, job deploymodel.NomadJob, result nomadclient.ValidationResult) {
	span.AddEvent("verself.nomad.validate", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("verself.deploy_wave", job.DeployPhase),
		attribute.Bool("nomad.driver_config_validated", result.DriverConfigValidated),
		attribute.Int("nomad.validation_errors", len(result.Errors)),
		attribute.Bool("nomad.validation_warnings", result.Warnings != ""),
	))
}

func recordNomadPlan(span trace.Span, runKey, site string, job deploymodel.NomadJob, result nomadclient.PlanResult) {
	span.AddEvent("verself.nomad.plan", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("verself.deploy_wave", job.DeployPhase),
		attribute.String("nomad.diff_type", result.DiffType),
		attribute.Int64("nomad.job_modify_index", int64FromUint64(result.JobModifyIndex, "job modify index")),
		attribute.Int("nomad.failed_tg_allocs", len(result.Failures)),
		attribute.Int("nomad.planned_update_count", len(result.Updates)),
		attribute.Bool("nomad.plan_warnings", result.Warnings != ""),
	))
}

func recordNomadSkipped(span trace.Span, runKey, site string, job deploymodel.NomadJob, decision nomadclient.Decision) {
	span.AddEvent("verself.nomad.skipped", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("verself.deploy_wave", job.DeployPhase),
		attribute.String("verself.spec_sha256", job.SpecSHA256),
		attribute.String("verself.input_sha256", job.InputSHA256),
		attribute.String("nomad.prior_spec_sha256", decision.PriorSpecDigest),
		attribute.Bool("nomad.prior_exists", decision.PriorExists),
		attribute.Bool("nomad.decision.noop", true),
	))
}

func recordNomadSubmitted(span trace.Span, runKey, site string, job deploymodel.NomadJob, decision nomadclient.Decision, submitted *nomadclient.SubmitResult, duration time.Duration) {
	span.AddEvent("verself.nomad.submitted", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("verself.deploy_wave", job.DeployPhase),
		attribute.String("verself.input_sha256", job.InputSHA256),
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
		attribute.String("verself.deploy_wave", job.DeployPhase),
		attribute.String("verself.spec_sha256", job.SpecSHA256),
		attribute.String("verself.input_sha256", job.InputSHA256),
		attribute.String("nomad.prior_spec_sha256", decision.PriorSpecDigest),
		attribute.Bool("nomad.prior_exists", decision.PriorExists),
		attribute.Int64("verself.duration_ms", duration.Milliseconds()),
		attribute.String("error.message", truncateError(err)),
	))
}

func recordNomadDeploymentSucceeded(span trace.Span, runKey, site string, job deploymodel.NomadJob, submitted *nomadclient.SubmitResult, monitor nomadclient.MonitorResult, duration time.Duration) {
	span.AddEvent("verself.nomad.deployment_succeeded", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("verself.deploy_wave", job.DeployPhase),
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
		attribute.String("verself.deploy_wave", job.DeployPhase),
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

func recordNomadFailurePacket(span trace.Span, runKey, site string, job deploymodel.NomadJob, packet nomadclient.FailurePacket) {
	span.AddEvent("verself.nomad.failure_packet", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("nomad.eval_id", packet.EvalID),
		attribute.String("nomad.deployment_id", packet.DeploymentID),
		attribute.String("nomad.deployment.status", packet.DeploymentStatus),
		attribute.String("nomad.status_description", packet.StatusDescription),
		attribute.Int("nomad.evaluation_count", len(packet.Evaluations)),
		attribute.Int("nomad.allocation_count", len(packet.Allocations)),
		attribute.Int("nomad.inspect_error_count", len(packet.InspectErrors)),
	))
}

func recordNomadRehearsalStarted(span trace.Span, runKey, site string, jobCount int, startedAt time.Time) {
	span.AddEvent("verself.nomad.rehearsal_started", trace.WithTimestamp(startedAt), trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.Int("verself.nomad_job_count", jobCount),
	))
}

func recordNomadRehearsalSucceeded(span trace.Span, runKey, site string, rehearsal *nomadRehearsal, duration time.Duration) {
	span.AddEvent("verself.nomad.rehearsal_succeeded", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.Int("verself.nomad_job_count", rehearsalJobCount(rehearsal)),
		attribute.Int("verself.changed_job_count", rehearsalChangedJobCount(rehearsal)),
		attribute.Int64("verself.duration_ms", duration.Milliseconds()),
	))
}

func recordNomadRehearsalFailed(span trace.Span, runKey, site string, rehearsal *nomadRehearsal, duration time.Duration, err error) {
	span.AddEvent("verself.nomad.rehearsal_failed", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.Int("verself.nomad_job_count", rehearsalJobCount(rehearsal)),
		attribute.Int("verself.failed_job_count", rehearsalFailedJobCount(rehearsal)),
		attribute.Int64("verself.duration_ms", duration.Milliseconds()),
		attribute.String("error.message", truncateError(err)),
	))
}

func recordPostDeployCanariesSucceeded(span trace.Span, runKey, site string, job deploymodel.NomadJob, selection string, duration time.Duration) {
	count := len(selectPostDeployCanaries(job.PostDeployCanaries, selection))
	span.AddEvent("verself.canary.succeeded", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("verself.component", job.Component),
		attribute.String("verself.canary_selection", selection),
		attribute.Int("verself.canary_count", count),
		attribute.Int64("verself.duration_ms", duration.Milliseconds()),
	))
}

func recordPostDeployCanariesFailed(span trace.Span, runKey, site string, job deploymodel.NomadJob, selection string, duration time.Duration, err error) {
	count := len(selectPostDeployCanaries(job.PostDeployCanaries, selection))
	span.AddEvent("verself.canary.failed", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("verself.component", job.Component),
		attribute.String("verself.canary_selection", selection),
		attribute.Int("verself.canary_count", count),
		attribute.Int64("verself.duration_ms", duration.Milliseconds()),
		attribute.String("error.message", truncateError(err)),
	))
}

func recordNomadRollbackSucceeded(span trace.Span, runKey, site string, job deploymodel.NomadJob, submitted *nomadclient.SubmitResult, monitor nomadclient.MonitorResult, duration time.Duration) {
	span.AddEvent("verself.nomad.rollback_succeeded", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("verself.component", job.Component),
		attribute.String("nomad.eval_id", submitted.EvalID),
		attribute.String("nomad.deployment_id", monitor.DeploymentID),
		attribute.String("nomad.terminal_status", monitor.TerminalStatus),
		attribute.Int64("verself.duration_ms", duration.Milliseconds()),
	))
}

func recordNomadRollbackFailed(span trace.Span, runKey, site string, job deploymodel.NomadJob, duration time.Duration, err error) {
	span.AddEvent("verself.nomad.rollback_failed", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("verself.component", job.Component),
		attribute.Int64("verself.duration_ms", duration.Milliseconds()),
		attribute.String("error.message", truncateError(err)),
	))
}

func recordDeployWaveStarted(span trace.Span, runKey, site, wave string, intents []jobApplyIntent, artifacts []deploymodel.Artifact, startedAt time.Time) {
	span.AddEvent("verself.deploy.wave_started", trace.WithTimestamp(startedAt), trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("verself.deploy_wave", wave),
		attribute.Int("verself.nomad_job_count", len(intents)),
		attribute.Int("verself.changed_job_count", changedIntentCount(intents)),
		attribute.Int("verself.artifact_count", len(artifacts)),
	))
}

func recordDeployWaveSucceeded(span trace.Span, runKey, site, wave string, intents []jobApplyIntent, artifacts []deploymodel.Artifact, startedAt time.Time) {
	duration := time.Since(startedAt)
	span.AddEvent("verself.deploy.wave_succeeded", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("verself.deploy_wave", wave),
		attribute.Int("verself.nomad_job_count", len(intents)),
		attribute.Int("verself.changed_job_count", changedIntentCount(intents)),
		attribute.Int("verself.artifact_count", len(artifacts)),
		attribute.Int64("verself.duration_ms", duration.Milliseconds()),
	))
}

func recordDeployWaveFailed(span trace.Span, runKey, site, wave string, intents []jobApplyIntent, artifacts []deploymodel.Artifact, startedAt time.Time, err error) {
	duration := time.Since(startedAt)
	span.AddEvent("verself.deploy.wave_failed", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.String("verself.deploy_wave", wave),
		attribute.Int("verself.nomad_job_count", len(intents)),
		attribute.Int("verself.changed_job_count", changedIntentCount(intents)),
		attribute.Int("verself.artifact_count", len(artifacts)),
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

func rehearsalJobCount(rehearsal *nomadRehearsal) int {
	if rehearsal == nil {
		return 0
	}
	return len(rehearsal.Intents)
}

func rehearsalChangedJobCount(rehearsal *nomadRehearsal) int {
	if rehearsal == nil {
		return 0
	}
	count := 0
	for _, intent := range rehearsal.Intents {
		if intent.Changed {
			count++
		}
	}
	return count
}

func rehearsalFailedJobCount(rehearsal *nomadRehearsal) int {
	if rehearsal == nil {
		return 0
	}
	count := 0
	for _, intent := range rehearsal.Intents {
		if intent.Lint.Failed() || intent.Validation.Failed() || intent.Plan.Failed() || intent.RehearsalError != "" {
			count++
		}
	}
	return count
}

func lintRuleIDs(findings []nomadlint.Finding) []string {
	seen := map[string]bool{}
	for _, finding := range findings {
		seen[finding.RuleID] = true
	}
	out := make([]string, 0, len(seen))
	for ruleID := range seen {
		out = append(out, ruleID)
	}
	sortStrings(out)
	return out
}

func jobIDs(jobs []deploymodel.NomadJob) []string {
	out := make([]string, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, job.JobID)
	}
	return out
}
