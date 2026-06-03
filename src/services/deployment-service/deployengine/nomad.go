package deployengine

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-service/internal/nomadclient"
)

const (
	controlPlaneReadyTimeout = 3 * time.Minute
	controlPlaneApplyTimeout = 5 * time.Minute
)

type NomadRegisterResult struct {
	JobID          string
	EvalID         string
	JobModifyIndex uint64
}

type nomadApplyResult struct {
	Jobs           []NomadRegisterResult
	SubmittedJobs  uint32
	DispatchedJobs uint32
}

func registerNomadJobs(ctx context.Context, exec execution, inputs *deployInputs) (nomadApplyResult, error) {
	client, err := nomadclient.New(inputs.SiteCfg.NomadAddr)
	if err != nil {
		return nomadApplyResult{}, err
	}
	if err := publishArtifacts(ctx, exec, inputs); err != nil {
		return nomadApplyResult{}, err
	}
	jobs, err := prepareNomadJobsForSite(ctx, client, exec.RepoRoot, inputs.SiteModel, inputs.Bindings, inputs.Components)
	if err != nil {
		return nomadApplyResult{}, err
	}
	result := nomadApplyResult{Jobs: make([]NomadRegisterResult, 0, len(jobs))}
	if !exec.bootstrapMode() {
		return registerNomadJobsNormal(ctx, exec, client, inputs, jobs, result)
	}
	return registerNomadJobsBootstrap(ctx, exec, client, inputs, jobs, result)
}

func registerNomadJobsNormal(ctx context.Context, exec execution, client *nomadclient.Client, inputs *deployInputs, jobs []nomadJob, result nomadApplyResult) (nomadApplyResult, error) {
	controlPlaneJobs, runtimeJobs, err := splitControlPlaneHandoff(jobs)
	if err != nil {
		return result, err
	}
	for _, job := range controlPlaneJobs {
		submitted, err := registerNomadJob(ctx, exec, client, job)
		if err != nil {
			return result, err
		}
		result.add(submitted)
	}
	dispatched, err := dispatchControlPlane(ctx, exec, client, inputs)
	if err != nil {
		return result, err
	}
	if dispatched != nil {
		result.DispatchedJobs++
	}
	for _, job := range runtimeJobs {
		submitted, err := registerNomadJob(ctx, exec, client, job)
		if err != nil {
			return result, err
		}
		result.add(submitted)
	}
	return result, nil
}

func registerNomadJobsBootstrap(ctx context.Context, exec execution, client *nomadclient.Client, inputs *deployInputs, jobs []nomadJob, result nomadApplyResult) (nomadApplyResult, error) {
	controlPlaneJobs, runtimeJobs, err := splitControlPlaneHandoff(jobs)
	if err != nil {
		return result, err
	}
	for _, job := range controlPlaneJobs {
		submitted, err := registerNomadJob(ctx, exec, client, job)
		if err != nil {
			return result, err
		}
		result.add(submitted)
	}
	if err := waitForControlPlanePrereqs(ctx, client); err != nil {
		return result, err
	}
	dispatched, err := dispatchControlPlane(ctx, exec, client, inputs)
	if err != nil {
		return result, err
	}
	if dispatched != nil {
		result.DispatchedJobs++
		if err := client.WaitForBatchComplete(ctx, dispatched.DispatchedJobID, controlPlaneApplyTimeout); err != nil {
			return result, err
		}
		if _, err := fmt.Fprintf(exec.stdout(), "deployment-service: %s completed\n", dispatched.DispatchedJobID); err != nil {
			return result, fmt.Errorf("write substrate control-plane completion: %w", err)
		}
	}
	blockingBatchJobs := blockingRuntimeBatchJobs(runtimeJobs)
	for _, job := range runtimeJobs {
		if blockingBatchJobs[job.JobID] {
			if err := purgeBlockingBatchJob(ctx, exec, client, job.JobID); err != nil {
				return result, err
			}
		}
		submitted, err := registerNomadJob(ctx, exec, client, job)
		if err != nil {
			return result, err
		}
		result.add(submitted)
		if !blockingBatchJobs[job.JobID] {
			continue
		}
		if err := client.WaitForBatchComplete(ctx, job.JobID, controlPlaneApplyTimeout); err != nil {
			return result, err
		}
		if _, err := fmt.Fprintf(exec.stdout(), "deployment-service: %s completed\n", job.JobID); err != nil {
			return result, fmt.Errorf("write batch completion: %w", err)
		}
	}
	return result, nil
}

func purgeBlockingBatchJob(ctx context.Context, exec execution, client *nomadclient.Client, jobID string) error {
	purged, err := client.PurgeJob(ctx, jobID)
	if err != nil {
		return err
	}
	if purged == nil {
		return nil
	}
	if _, err := fmt.Fprintf(exec.stdout(), "deployment-service: %s purged eval_id=%s\n", purged.JobID, purged.EvalID); err != nil {
		return fmt.Errorf("%s: write purged job status: %w", purged.JobID, err)
	}
	return nil
}

func (r *nomadApplyResult) add(job NomadRegisterResult) {
	r.Jobs = append(r.Jobs, job)
	r.SubmittedJobs++
}

func waitForControlPlanePrereqs(ctx context.Context, client *nomadclient.Client) error {
	if err := client.WaitForTasks(ctx, "openbao", []nomadclient.TaskExpectation{
		{Name: "bootstrap", State: "dead", ExitCode: ptr(0)},
		{Name: "server", State: "running"},
	}, controlPlaneReadyTimeout); err != nil {
		return err
	}
	if err := client.WaitForTasks(ctx, "postgresql", []nomadclient.TaskExpectation{
		{Name: "setup", State: "dead", ExitCode: ptr(0)},
		{Name: "server", State: "running"},
	}, controlPlaneReadyTimeout); err != nil {
		return err
	}
	return nil
}

func ptr(value int) *int {
	return &value
}

func splitControlPlaneHandoff(jobs []nomadJob) ([]nomadJob, []nomadJob, error) {
	requiredOrder := []string{"openbao", "postgresql", "substrate-control-plane"}
	byID := make(map[string]nomadJob, len(jobs))
	for _, job := range jobs {
		byID[job.JobID] = job
	}
	controlPlaneJobs := make([]nomadJob, 0, len(requiredOrder))
	required := make(map[string]bool, len(requiredOrder))
	for _, jobID := range requiredOrder {
		job, ok := byID[jobID]
		if !ok {
			return nil, nil, fmt.Errorf("required control-plane handoff job %q is missing", jobID)
		}
		controlPlaneJobs = append(controlPlaneJobs, job)
		required[jobID] = true
	}
	runtimeJobs := make([]nomadJob, 0, len(jobs)-len(controlPlaneJobs))
	for _, job := range jobs {
		if required[job.JobID] {
			continue
		}
		runtimeJobs = append(runtimeJobs, job)
	}
	return controlPlaneJobs, runtimeJobs, nil
}

func blockingRuntimeBatchJobs(jobs []nomadJob) map[string]bool {
	required := resourcesRequiredByNonBatchJobs(jobs)
	blocking := make(map[string]bool)
	for {
		changed := false
		for _, job := range jobs {
			if !isBatchJob(job) || blocking[job.JobID] || !providesRequiredResource(job, required) {
				continue
			}
			blocking[job.JobID] = true
			for _, resource := range job.Requires {
				if required[resource] {
					continue
				}
				required[resource] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return blocking
}

func resourcesRequiredByNonBatchJobs(jobs []nomadJob) map[string]bool {
	required := make(map[string]bool)
	for _, job := range jobs {
		if isBatchJob(job) {
			continue
		}
		for _, resource := range job.Requires {
			required[resource] = true
		}
	}
	return required
}

func isBatchJob(job nomadJob) bool {
	return job.Job != nil && job.Job.Type != nil && *job.Job.Type == "batch"
}

func providesRequiredResource(job nomadJob, required map[string]bool) bool {
	for _, resource := range job.Provides {
		if required[resource] {
			return true
		}
	}
	return false
}

func registerNomadJob(ctx context.Context, exec execution, client *nomadclient.Client, job nomadJob) (NomadRegisterResult, error) {
	ctx, span := exec.Tracer.Start(ctx, "verself_deploy.nomad.register",
		trace.WithAttributes(
			attribute.String("nomad.job_id", job.JobID),
			attribute.String("verself.component", job.Component),
			attribute.String("nomad.jobspec_source", job.Source),
		),
	)
	defer span.End()
	submitted, err := client.Register(ctx, job.Job)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return NomadRegisterResult{}, fmt.Errorf("%s: %w", job.JobID, err)
	}
	if _, err := fmt.Fprintf(exec.stdout(), "deployment-service: %s submitted job_modify_index=%d eval_id=%s\n", submitted.JobID, submitted.JobModifyIndex, submitted.EvalID); err != nil {
		return NomadRegisterResult{}, fmt.Errorf("%s: write submitted job status: %w", submitted.JobID, err)
	}
	span.SetAttributes(
		attribute.String("nomad.eval_id", submitted.EvalID),
		attribute.String("nomad.job_modify_index", strconv.FormatUint(submitted.JobModifyIndex, 10)),
	)
	span.SetStatus(codes.Ok, "")
	return NomadRegisterResult{
		JobID:          submitted.JobID,
		EvalID:         submitted.EvalID,
		JobModifyIndex: submitted.JobModifyIndex,
	}, nil
}

func dispatchControlPlane(ctx context.Context, exec execution, client *nomadclient.Client, inputs *deployInputs) (*nomadclient.DispatchResult, error) {
	if inputs.ControlPlaneObject.Artifact.GetterSource == "" || inputs.ControlPlaneObject.Artifact.SHA256 == "" {
		return nil, fmt.Errorf("substrate control-plane bundle artifact was not prepared")
	}
	if _, err := fmt.Fprintf(exec.stdout(), "deployment-service: substrate-control-plane bundle_sha256=%s artifact_sha256=%s artifact_bytes=%d\n", inputs.ControlPlaneSHA256, inputs.ControlPlaneObject.Artifact.SHA256, len(inputs.ControlPlaneObject.Body)); err != nil {
		return nil, fmt.Errorf("write substrate control-plane bundle status: %w", err)
	}
	ctx, span := exec.Tracer.Start(ctx, "verself_deploy.nomad.dispatch_control_plane",
		trace.WithAttributes(
			attribute.String("nomad.job_id", "substrate-control-plane"),
			attribute.String("verself.control_plane_bundle_sha256", inputs.ControlPlaneSHA256),
			attribute.String("verself.control_plane_bundle_artifact_sha256", inputs.ControlPlaneObject.Artifact.SHA256),
			attribute.String("verself.control_plane_bundle_artifact_source", inputs.ControlPlaneObject.Artifact.GetterSource),
			attribute.Int("verself.control_plane_bundle_artifact_bytes", len(inputs.ControlPlaneObject.Body)),
		),
	)
	defer span.End()
	result, err := client.Dispatch(ctx, "substrate-control-plane", map[string]string{
		"bundle_artifact_sha256":      inputs.ControlPlaneObject.Artifact.SHA256,
		"bundle_source":               inputs.ControlPlaneObject.Artifact.GetterSource,
		"control_plane_bundle_sha256": inputs.ControlPlaneSHA256,
		"deploy_run_key":              inputs.DeployRunKey,
		"sha":                         inputs.SHA,
		"site":                        exec.Site,
	}, nil, "")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	if _, err := fmt.Fprintf(exec.stdout(), "deployment-service: dispatched substrate-control-plane job=%s eval_id=%s\n", result.DispatchedJobID, result.EvalID); err != nil {
		return nil, fmt.Errorf("write substrate control-plane dispatch status: %w", err)
	}
	span.SetAttributes(
		attribute.String("nomad.dispatched_job_id", result.DispatchedJobID),
		attribute.String("nomad.eval_id", result.EvalID),
	)
	span.SetStatus(codes.Ok, "")
	return result, nil
}
