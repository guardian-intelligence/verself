package deployengine

import (
	"context"
	"fmt"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-service/internal/nomadclient"
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
	jobs, err := prepareNomadJobsForSite(ctx, client, exec.RepoRoot, inputs.SiteModel, inputs.Bindings, inputs.Components, exec.TaskUserResolver)
	if err != nil {
		return nomadApplyResult{}, err
	}
	result := nomadApplyResult{Jobs: make([]NomadRegisterResult, 0, len(jobs))}
	for _, job := range jobs {
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
	return result, nil
}

func (r *nomadApplyResult) add(job NomadRegisterResult) {
	r.Jobs = append(r.Jobs, job)
	r.SubmittedJobs++
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
