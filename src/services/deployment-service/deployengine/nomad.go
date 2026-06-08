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
	Jobs          []NomadRegisterResult
	SubmittedJobs uint32
}

func registerNomadJobs(ctx context.Context, exec execution, inputs *deployInputs) (nomadApplyResult, error) {
	client, err := nomadclient.New(inputs.NomadAddr)
	if err != nil {
		return nomadApplyResult{}, err
	}
	jobs, err := prepareNomadJobsForSite(ctx, client, exec.RepoRoot, inputs.SiteModel, inputs.JobPaths, exec.TaskUserResolver)
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
