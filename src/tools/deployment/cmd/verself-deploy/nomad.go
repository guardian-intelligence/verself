package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/nomadclient"
	"github.com/verself/deployment-tools/internal/runtime"
	"github.com/verself/deployment-tools/internal/sshtun"
)

const (
	defaultNomadRemotePort    = 4646
	nomadDispatchPayloadLimit = 16 * 1024
	controlPlaneReadyTimeout  = 3 * time.Minute
	controlPlaneApplyTimeout  = 5 * time.Minute
)

func openNomadForward(ctx context.Context, rt *runtime.Runtime, addr string) (*sshtun.Forward, error) {
	port := defaultNomadRemotePort
	if addr != "" {
		parsed, err := url.Parse(addr)
		if err != nil {
			return nil, fmt.Errorf("parse nomad_addr %q: %w", addr, err)
		}
		if parsed.Port() != "" {
			p, err := strconv.Atoi(parsed.Port())
			if err != nil || p <= 0 || p > 65535 {
				return nil, fmt.Errorf("invalid nomad_addr port %q", parsed.Port())
			}
			port = p
		}
	}
	return rt.SSH.Forward(ctx, "nomad", port)
}

type nomadRegisterResult struct {
	JobID          string
	EvalID         string
	JobModifyIndex uint64
}

func registerNomadJobs(ctx context.Context, rt *runtime.Runtime, inputs *deployInputs) ([]nomadRegisterResult, error) {
	forward, err := openNomadForward(ctx, rt, inputs.SiteCfg.NomadAddr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = forward.Close() }()
	client, err := nomadclient.New("http://" + forward.ListenAddr)
	if err != nil {
		return nil, err
	}
	if err := publishArtifacts(ctx, rt, inputs); err != nil {
		return nil, err
	}
	jobs, err := prepareNomadJobsForSite(ctx, client, rt.RepoRoot, inputs.SiteModel, inputs.Bindings, inputs.Components)
	if err != nil {
		return nil, err
	}
	results := make([]nomadRegisterResult, 0, len(jobs))
	controlPlaneJobs, runtimeJobs, err := splitControlPlaneHandoff(jobs)
	if err != nil {
		return nil, err
	}
	for _, job := range controlPlaneJobs {
		submitted, err := registerNomadJob(ctx, rt, client, job)
		if err != nil {
			return results, err
		}
		results = append(results, submitted)
	}
	if err := waitForControlPlanePrereqs(ctx, client); err != nil {
		return results, err
	}
	dispatched, err := dispatchControlPlane(ctx, rt, client, inputs)
	if err != nil {
		return results, err
	}
	if err := client.WaitForBatchComplete(ctx, dispatched.DispatchedJobID, controlPlaneApplyTimeout); err != nil {
		return results, err
	}
	fmt.Printf("verself-deploy: %s completed\n", dispatched.DispatchedJobID)
	batchJobs, serviceJobs := splitBlockingRuntimeBatchJobs(runtimeJobs)
	for _, job := range batchJobs {
		submitted, err := registerNomadJob(ctx, rt, client, job)
		if err != nil {
			return results, err
		}
		results = append(results, submitted)
	}
	for _, job := range batchJobs {
		// Batch jobs are one-time substrate mutations; runtime jobs must not
		// race the state they create.
		if err := client.WaitForBatchComplete(ctx, job.JobID, controlPlaneApplyTimeout); err != nil {
			return results, err
		}
		fmt.Printf("verself-deploy: %s completed\n", job.JobID)
	}
	for _, job := range serviceJobs {
		submitted, err := registerNomadJob(ctx, rt, client, job)
		if err != nil {
			return results, err
		}
		results = append(results, submitted)
	}
	return results, nil
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

func splitBlockingRuntimeBatchJobs(jobs []nomadJob) ([]nomadJob, []nomadJob) {
	required := make(map[string]bool)
	for _, job := range jobs {
		for _, resource := range job.Requires {
			required[resource] = true
		}
	}
	batchJobs := make([]nomadJob, 0)
	serviceJobs := make([]nomadJob, 0, len(jobs))
	for _, job := range jobs {
		if job.Job != nil && job.Job.Type != nil && *job.Job.Type == "batch" && providesRequiredResource(job, required) {
			batchJobs = append(batchJobs, job)
			continue
		}
		serviceJobs = append(serviceJobs, job)
	}
	return batchJobs, serviceJobs
}

func providesRequiredResource(job nomadJob, required map[string]bool) bool {
	for _, resource := range job.Provides {
		if required[resource] {
			return true
		}
	}
	return false
}

func registerNomadJob(ctx context.Context, rt *runtime.Runtime, client *nomadclient.Client, job nomadJob) (nomadRegisterResult, error) {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.nomad.register",
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
		return nomadRegisterResult{}, fmt.Errorf("%s: %w", job.JobID, err)
	}
	fmt.Printf("verself-deploy: %s registered job_modify_index=%d eval_id=%s\n", submitted.JobID, submitted.JobModifyIndex, submitted.EvalID)
	span.SetAttributes(
		attribute.String("nomad.eval_id", submitted.EvalID),
		attribute.String("nomad.job_modify_index", strconv.FormatUint(submitted.JobModifyIndex, 10)),
	)
	span.SetStatus(codes.Ok, "")
	return nomadRegisterResult{
		JobID:          submitted.JobID,
		EvalID:         submitted.EvalID,
		JobModifyIndex: submitted.JobModifyIndex,
	}, nil
}

func dispatchControlPlane(ctx context.Context, rt *runtime.Runtime, client *nomadclient.Client, inputs *deployInputs) (*nomadclient.DispatchResult, error) {
	body, err := json.Marshal(inputs.ControlPlane)
	if err != nil {
		return nil, fmt.Errorf("encode substrate control-plane bundle: %w", err)
	}
	payload, err := gzipPayload(body)
	if err != nil {
		return nil, fmt.Errorf("compress substrate control-plane bundle: %w", err)
	}
	if len(payload) > nomadDispatchPayloadLimit {
		return nil, fmt.Errorf("substrate control-plane bundle is %d compressed bytes (%d raw); Nomad dispatch payload limit is %d bytes", len(payload), len(body), nomadDispatchPayloadLimit)
	}
	fmt.Printf("verself-deploy: substrate-control-plane bundle_bytes=%d compressed_bytes=%d\n", len(body), len(payload))
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.nomad.dispatch_control_plane",
		trace.WithAttributes(
			attribute.String("nomad.job_id", "substrate-control-plane"),
			attribute.Int("verself.control_plane_bundle_bytes", len(body)),
			attribute.Int("verself.control_plane_bundle_compressed_bytes", len(payload)),
		),
	)
	defer span.End()
	result, err := client.Dispatch(ctx, "substrate-control-plane", map[string]string{
		"deploy_run_key": inputs.DeployRunKey,
		"sha":            inputs.SHA,
		"site":           rt.Site,
	}, payload, "")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	fmt.Printf("verself-deploy: dispatched substrate-control-plane job=%s eval_id=%s\n", result.DispatchedJobID, result.EvalID)
	span.SetAttributes(
		attribute.String("nomad.dispatched_job_id", result.DispatchedJobID),
		attribute.String("nomad.eval_id", result.EvalID),
	)
	span.SetStatus(codes.Ok, "")
	return result, nil
}

func gzipPayload(body []byte) ([]byte, error) {
	var out bytes.Buffer
	zw := gzip.NewWriter(&out)
	if _, err := zw.Write(body); err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
