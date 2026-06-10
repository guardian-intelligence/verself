package deployengine

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/nomad/api"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-service/internal/nomadclient"
)

type NomadRegisterResult struct {
	JobID          string
	EvalID         string
	JobModifyIndex uint64
	// SkippedUnchanged reports that the running job already carries the
	// desired spec_sha256, so no register was submitted for this unit.
	SkippedUnchanged bool
}

type nomadApplyResult struct {
	Jobs          []NomadRegisterResult
	SubmittedJobs uint32
	SkippedJobs   uint32
}

func registerNomadJobs(ctx context.Context, exec execution, inputs *deployInputs) (nomadApplyResult, error) {
	client, err := nomadclient.New(inputs.SiteCfg.NomadAddr)
	if err != nil {
		return nomadApplyResult{}, err
	}
	if err := publishArtifacts(ctx, exec, inputs); err != nil {
		return nomadApplyResult{}, err
	}
	if err := publishOCIImages(ctx, exec, inputs); err != nil {
		return nomadApplyResult{}, err
	}
	if err := admitOCIImages(ctx, exec, inputs); err != nil {
		return nomadApplyResult{}, err
	}
	jobs, err := prepareNomadJobsForSite(ctx, client, exec.RepoRoot, inputs.SiteModel, inputs.Bindings, inputs.OCIImages, inputs.Components, exec.TaskUserResolver, deployJobMetadata{
		DeployRunKey: inputs.DeployRunKey,
		SHA:          inputs.SHA,
	})
	if err != nil {
		return nomadApplyResult{}, err
	}
	plan, err := planRuntimeSecretDeployment(exec.RepoRoot, jobs)
	if err != nil {
		return nomadApplyResult{}, err
	}
	result := nomadApplyResult{Jobs: make([]NomadRegisterResult, 0, len(plan.jobs))}
	for _, job := range plan.jobs {
		submitted, err := registerNomadJob(ctx, exec, client, job)
		if err != nil {
			return result, err
		}
		result.add(submitted)
		if reasons := plan.readinessReasons[job.JobID]; len(reasons) > 0 {
			if _, err := fmt.Fprintf(exec.stdout(), "deployment-service: %s waiting for readiness dependencies=%d\n", job.JobID, len(reasons)); err != nil {
				return result, fmt.Errorf("%s: write readiness wait status: %w", job.JobID, err)
			}
			if err := waitForRuntimeSecretProducer(ctx, exec, client, job); err != nil {
				return result, err
			}
			if _, err := fmt.Fprintf(exec.stdout(), "deployment-service: %s readiness gate ready\n", job.JobID); err != nil {
				return result, fmt.Errorf("%s: write readiness ready status: %w", job.JobID, err)
			}
		}
	}
	return result, nil
}

func (r *nomadApplyResult) add(job NomadRegisterResult) {
	r.Jobs = append(r.Jobs, job)
	if job.SkippedUnchanged {
		r.SkippedJobs++
		return
	}
	r.SubmittedJobs++
}

// nomadUnitDecision compares the desired unit fingerprint against the
// registered job. Per-run identifiers (deploy_run_key, deploy_sha) are stamped
// into Meta after spec_sha256 is computed, so the submitted payload always
// differs across runs and Nomad's own version diff can never no-op — this
// comparison is the only skip gate. A skipped unit keeps the prior run's
// deploy metadata; runtime evidence joins on spec_sha256, not run key.
func nomadUnitDecision(running *api.Job, found bool, desired string) (decision string, previous string) {
	if !found || running == nil {
		return "create", ""
	}
	if running.Meta != nil {
		previous = running.Meta["spec_sha256"]
	}
	if previous != desired {
		return "roll", previous
	}
	if running.Stop != nil && *running.Stop {
		return "roll", previous
	}
	if running.Status == nil || *running.Status != "running" {
		return "roll", previous
	}
	return "skip", previous
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
	desired := ""
	if job.Job != nil && job.Job.Meta != nil {
		desired = job.Job.Meta["spec_sha256"]
	}
	if desired == "" {
		err := fmt.Errorf("%s: rendered job is missing spec_sha256 metadata", job.JobID)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return NomadRegisterResult{}, err
	}
	running, found, err := client.RegisteredJob(ctx, job.JobID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return NomadRegisterResult{}, fmt.Errorf("%s: %w", job.JobID, err)
	}
	decision, previous := nomadUnitDecision(running, found, desired)
	span.SetAttributes(
		attribute.String("verself.deploy_spec_sha256", desired),
		attribute.String("verself.deploy_previous_spec_sha256", previous),
		attribute.String("verself.deploy_unit_decision", decision),
	)
	if decision == "skip" {
		modifyIndex := uint64(0)
		if running.JobModifyIndex != nil {
			modifyIndex = *running.JobModifyIndex
		}
		if _, err := fmt.Fprintf(exec.stdout(), "deployment-service: %s unchanged spec_sha256=%s job_modify_index=%d register skipped\n", job.JobID, desired, modifyIndex); err != nil {
			return NomadRegisterResult{}, fmt.Errorf("%s: write skipped job status: %w", job.JobID, err)
		}
		span.SetAttributes(attribute.String("nomad.job_modify_index", strconv.FormatUint(modifyIndex, 10)))
		span.SetStatus(codes.Ok, "")
		return NomadRegisterResult{
			JobID:            job.JobID,
			JobModifyIndex:   modifyIndex,
			SkippedUnchanged: true,
		}, nil
	}
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
