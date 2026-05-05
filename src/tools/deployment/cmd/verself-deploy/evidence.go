package main

import (
	"context"
	"errors"
	"time"

	"github.com/verself/deployment-tools/internal/deploydb"
	"github.com/verself/deployment-tools/internal/deploymodel"
	"github.com/verself/deployment-tools/internal/nomadclient"
)

func recordDeploySucceeded(ctx context.Context, db *deploydb.Client, plan *deployPlan, results []jobApplyResult, startedAt time.Time) error {
	return db.RecordDeployEvent(ctx, deploydb.DeployEvent{
		RunKey:             plan.Identity.RunKey(),
		Site:               plan.Site,
		Sha:                plan.SHA,
		Actor:              plan.Identity.Get("VERSELF_AUTHOR"),
		Scope:              deployScope,
		AffectedComponents: changedJobIDs(results),
		Kind:               deploydb.EventSucceeded,
		DurationMs:         durationMillis(time.Since(startedAt)),
	})
}

func recordDeployFailed(ctx context.Context, db *deploydb.Client, plan *deployPlan, startedAt time.Time, err error) error {
	if plan == nil {
		return errors.New("deploy plan is nil")
	}
	return db.RecordDeployEvent(ctx, deploydb.DeployEvent{
		RunKey:             plan.Identity.RunKey(),
		Site:               plan.Site,
		Sha:                plan.SHA,
		Actor:              plan.Identity.Get("VERSELF_AUTHOR"),
		Scope:              deployScope,
		AffectedComponents: jobIDs(plan.Jobs),
		Kind:               deploydb.EventFailed,
		DurationMs:         durationMillis(time.Since(startedAt)),
		ErrorMessage:       truncateError(err),
	})
}

func recordNomadDecision(ctx context.Context, db *deploydb.Client, runKey, site string, job deploymodel.NomadJob, decision nomadclient.Decision, duration time.Duration) error {
	if err := db.RecordDeployUnitEvent(ctx, deploydb.DeployUnitEvent{
		RunKey:          runKey,
		Site:            site,
		Executor:        deploydb.DeployExecutorNomad,
		UnitID:          job.JobID,
		Kind:            deploydb.DeployUnitEventDecided,
		DesiredDigest:   job.SpecSHA256,
		ObservedDigest:  decision.PriorSpecDigest,
		NoOp:            decision.NoOp,
		DependencyUnits: job.DependsOn,
		PayloadKind:     "nomad_job",
		DurationMs:      durationMillis(duration),
	}); err != nil {
		return err
	}
	return db.RecordNomadJobEvent(ctx, deploydb.NomadJobEvent{
		RunKey:              runKey,
		Site:                site,
		JobID:               job.JobID,
		Kind:                deploydb.NomadJobEventDecided,
		SpecSHA256:          job.SpecSHA256,
		ArtifactSHA256:      job.ArtifactSHA256,
		PriorJobModifyIndex: decision.PriorJobModifyIndex,
		PriorVersion:        decision.PriorVersion,
		PriorStopped:        decision.PriorStopped,
		NoOp:                decision.NoOp,
		DurationMs:          durationMillis(duration),
	})
}

func recordNomadSkipped(ctx context.Context, db *deploydb.Client, runKey, site string, job deploymodel.NomadJob, decision nomadclient.Decision) error {
	return db.RecordDeployUnitEvent(ctx, deploydb.DeployUnitEvent{
		RunKey:          runKey,
		Site:            site,
		Executor:        deploydb.DeployExecutorNomad,
		UnitID:          job.JobID,
		Kind:            deploydb.DeployUnitEventSkipped,
		DesiredDigest:   job.SpecSHA256,
		ObservedDigest:  decision.PriorSpecDigest,
		NoOp:            true,
		DependencyUnits: job.DependsOn,
		PayloadKind:     "nomad_job",
	})
}

func recordNomadSubmitted(ctx context.Context, db *deploydb.Client, runKey, site string, job deploymodel.NomadJob, decision nomadclient.Decision, submitted *nomadclient.SubmitResult, duration time.Duration) error {
	return db.RecordNomadJobEvent(ctx, deploydb.NomadJobEvent{
		RunKey:              runKey,
		Site:                site,
		JobID:               job.JobID,
		Kind:                deploydb.NomadJobEventSubmitted,
		SpecSHA256:          job.SpecSHA256,
		ArtifactSHA256:      job.ArtifactSHA256,
		PriorJobModifyIndex: decision.PriorJobModifyIndex,
		PriorVersion:        decision.PriorVersion,
		PriorStopped:        decision.PriorStopped,
		EvalID:              submitted.EvalID,
		DeploymentID:        submitted.DeploymentID,
		JobModifyIndex:      submitted.JobModifyIndex,
		DurationMs:          durationMillis(duration),
	})
}

func recordNomadSubmitFailed(ctx context.Context, db *deploydb.Client, runKey, site string, job deploymodel.NomadJob, decision nomadclient.Decision, duration time.Duration, err error) error {
	unitErr := db.RecordDeployUnitEvent(ctx, deploydb.DeployUnitEvent{
		RunKey:          runKey,
		Site:            site,
		Executor:        deploydb.DeployExecutorNomad,
		UnitID:          job.JobID,
		Kind:            deploydb.DeployUnitEventFailed,
		DesiredDigest:   job.SpecSHA256,
		ObservedDigest:  decision.PriorSpecDigest,
		NoOp:            false,
		DependencyUnits: job.DependsOn,
		PayloadKind:     "nomad_job",
		DurationMs:      durationMillis(duration),
		ErrorMessage:    truncateError(err),
	})
	jobErr := db.RecordNomadJobEvent(ctx, deploydb.NomadJobEvent{
		RunKey:              runKey,
		Site:                site,
		JobID:               job.JobID,
		Kind:                deploydb.NomadJobEventSubmitFailed,
		SpecSHA256:          job.SpecSHA256,
		ArtifactSHA256:      job.ArtifactSHA256,
		PriorJobModifyIndex: decision.PriorJobModifyIndex,
		PriorVersion:        decision.PriorVersion,
		PriorStopped:        decision.PriorStopped,
		DurationMs:          durationMillis(duration),
		ErrorMessage:        truncateError(err),
	})
	return errors.Join(unitErr, jobErr)
}

func recordNomadDeploymentSucceeded(ctx context.Context, db *deploydb.Client, runKey, site string, job deploymodel.NomadJob, decision nomadclient.Decision, submitted *nomadclient.SubmitResult, monitor nomadclient.MonitorResult, duration time.Duration) error {
	if err := recordNomadDeploymentUnit(ctx, db, runKey, site, job, deploydb.DeployUnitEventSucceeded, "", duration); err != nil {
		return err
	}
	return db.RecordNomadJobEvent(ctx, deploydb.NomadJobEvent{
		RunKey:              runKey,
		Site:                site,
		JobID:               job.JobID,
		Kind:                deploydb.NomadJobEventDeploymentSucceeded,
		SpecSHA256:          job.SpecSHA256,
		ArtifactSHA256:      job.ArtifactSHA256,
		PriorJobModifyIndex: decision.PriorJobModifyIndex,
		PriorVersion:        decision.PriorVersion,
		PriorStopped:        decision.PriorStopped,
		EvalID:              submitted.EvalID,
		DeploymentID:        monitor.DeploymentID,
		JobModifyIndex:      submitted.JobModifyIndex,
		DesiredTotal:        uint16FromInt(monitor.DesiredTotal),
		HealthyTotal:        uint16FromInt(monitor.HealthyTotal),
		UnhealthyTotal:      uint16FromInt(monitor.UnhealthyTotal),
		PlacedTotal:         uint16FromInt(monitor.PlacedTotal),
		TerminalStatus:      monitor.TerminalStatus,
		DurationMs:          durationMillis(duration),
	})
}

func recordNomadDeploymentFailed(ctx context.Context, db *deploydb.Client, runKey, site string, job deploymodel.NomadJob, decision nomadclient.Decision, submitted *nomadclient.SubmitResult, monitor nomadclient.MonitorResult, duration time.Duration, err error) error {
	unitErr := recordNomadDeploymentUnit(ctx, db, runKey, site, job, deploydb.DeployUnitEventFailed, err.Error(), duration)
	jobErr := db.RecordNomadJobEvent(ctx, deploydb.NomadJobEvent{
		RunKey:              runKey,
		Site:                site,
		JobID:               job.JobID,
		Kind:                deploydb.NomadJobEventDeploymentFailed,
		SpecSHA256:          job.SpecSHA256,
		ArtifactSHA256:      job.ArtifactSHA256,
		PriorJobModifyIndex: decision.PriorJobModifyIndex,
		PriorVersion:        decision.PriorVersion,
		PriorStopped:        decision.PriorStopped,
		EvalID:              submitted.EvalID,
		DeploymentID:        monitor.DeploymentID,
		JobModifyIndex:      submitted.JobModifyIndex,
		DesiredTotal:        uint16FromInt(monitor.DesiredTotal),
		HealthyTotal:        uint16FromInt(monitor.HealthyTotal),
		UnhealthyTotal:      uint16FromInt(monitor.UnhealthyTotal),
		PlacedTotal:         uint16FromInt(monitor.PlacedTotal),
		TerminalStatus:      monitor.TerminalStatus,
		DurationMs:          durationMillis(duration),
		ErrorMessage:        truncateError(err),
	})
	return errors.Join(unitErr, jobErr)
}

func recordNomadDeploymentUnit(ctx context.Context, db *deploydb.Client, runKey, site string, job deploymodel.NomadJob, kind, message string, duration time.Duration) error {
	return db.RecordDeployUnitEvent(ctx, deploydb.DeployUnitEvent{
		RunKey:          runKey,
		Site:            site,
		Executor:        deploydb.DeployExecutorNomad,
		UnitID:          job.JobID,
		Kind:            kind,
		DesiredDigest:   job.SpecSHA256,
		ObservedDigest:  job.SpecSHA256,
		NoOp:            false,
		DependencyUnits: job.DependsOn,
		PayloadKind:     "nomad_job",
		DurationMs:      durationMillis(duration),
		ErrorMessage:    truncateErrorString(message),
	})
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
