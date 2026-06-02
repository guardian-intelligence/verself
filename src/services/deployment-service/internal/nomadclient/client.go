// Package nomadclient is a typed wrapper around github.com/hashicorp/nomad/api.
// Authored source specs are owner-local HCL2 files parsed by the target Nomad
// server and then registered without deploy-runner mutation.
package nomadclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/nomad/api"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/verself/deployment-service/internal/nomadclient"

// Client is a thin span-emitting wrapper around *api.Client.
type Client struct {
	api    *api.Client
	tracer trace.Tracer
}

// New constructs a Client pointed at the given Nomad HTTP address.
// Namespace, region, and auth come from the standard NOMAD_* environment.
func New(addr string) (*Client, error) {
	if addr == "" {
		return nil, errors.New("nomad address is required")
	}
	cfg := api.DefaultConfig()
	cfg.Address = addr
	c, err := api.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("nomad client: %w", err)
	}
	return &Client{
		api:    c,
		tracer: otel.Tracer(tracerName),
	}, nil
}

// ParseJobHCL asks the target Nomad agent to parse an authored HCL2 jobspec.
// This keeps deploy behavior aligned with the server version that will run it.
func (c *Client) ParseJobHCL(ctx context.Context, body []byte, source string) (*api.Job, error) {
	_, span := c.tracer.Start(ctx, "verself_deploy.nomad.parse_hcl",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("nomad.jobspec_source", source)),
	)
	defer span.End()

	if len(body) == 0 {
		err := fmt.Errorf("%s: Nomad jobspec is empty", source)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	// nomad/api does not expose a context-aware ParseHCLOpts call in the pinned client.
	job, err := c.api.Jobs().ParseHCLOpts(&api.JobsParseRequest{
		JobHCL:       string(body),
		Canonicalize: false,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("parse %s: %w", source, err)
	}
	if job == nil || job.ID == nil || *job.ID == "" {
		err := fmt.Errorf("%s: parsed Nomad job is missing ID", source)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(attribute.String("nomad.job_id", *job.ID))
	span.SetStatus(codes.Ok, "")
	return job, nil
}

// SubmitResult is the handle returned from a successful Nomad Register call.
type SubmitResult struct {
	JobID          string
	JobType        string
	EvalID         string
	JobModifyIndex uint64
}

// Register submits a job exactly as parsed by the target Nomad API. Rollout
// health, promotion, and rollback policy stay in Nomad job configuration.
func (c *Client) Register(ctx context.Context, job *api.Job) (*SubmitResult, error) {
	jobID := ""
	if job != nil && job.ID != nil {
		jobID = *job.ID
	}
	jobType := "service"
	if job != nil && job.Type != nil && *job.Type != "" {
		jobType = *job.Type
	}
	ctx, span := c.tracer.Start(ctx, "verself_deploy.nomad.register",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("nomad.job_id", jobID)),
	)
	defer span.End()

	if job == nil || jobID == "" {
		err := errors.New("nomad job is required")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	resp, _, err := c.api.Jobs().Register(job, (&api.WriteOptions{}).WithContext(ctx))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("register %s: %w", jobID, err)
	}
	sub := &SubmitResult{
		JobID:          jobID,
		JobType:        jobType,
		EvalID:         resp.EvalID,
		JobModifyIndex: resp.JobModifyIndex,
	}
	span.SetAttributes(
		attribute.String("nomad.eval_id", sub.EvalID),
		attribute.Int64("nomad.eval_create_index", int64FromUint64(resp.EvalCreateIndex, "eval create index")),
		attribute.Int64("nomad.job_modify_index", int64FromUint64(sub.JobModifyIndex, "job modify index")),
	)
	span.SetStatus(codes.Ok, "")
	return sub, nil
}

type DispatchResult struct {
	ParentJobID     string
	DispatchedJobID string
	EvalID          string
	EvalCreateIndex uint64
}

func (c *Client) Dispatch(ctx context.Context, jobID string, meta map[string]string, payload []byte, idPrefixTemplate string) (*DispatchResult, error) {
	ctx, span := c.tracer.Start(ctx, "verself_deploy.nomad.dispatch",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("nomad.job_id", jobID)),
	)
	defer span.End()
	if jobID == "" {
		err := errors.New("nomad job id is required")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	resp, _, err := c.api.Jobs().DispatchOpts(&api.DispatchOptions{
		JobID:            jobID,
		Meta:             meta,
		Payload:          payload,
		IdPrefixTemplate: idPrefixTemplate,
	}, (&api.WriteOptions{}).WithContext(ctx))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("dispatch %s: %w", jobID, err)
	}
	result := &DispatchResult{
		ParentJobID:     jobID,
		DispatchedJobID: resp.DispatchedJobID,
		EvalID:          resp.EvalID,
		EvalCreateIndex: resp.EvalCreateIndex,
	}
	span.SetAttributes(
		attribute.String("nomad.dispatched_job_id", result.DispatchedJobID),
		attribute.String("nomad.eval_id", result.EvalID),
		attribute.Int64("nomad.eval_create_index", int64FromUint64(result.EvalCreateIndex, "eval create index")),
	)
	span.SetStatus(codes.Ok, "")
	return result, nil
}

type TaskExpectation struct {
	Name     string
	State    string
	ExitCode *int
}

func (c *Client) WaitForTasks(ctx context.Context, jobID string, expectations []TaskExpectation, timeout time.Duration) error {
	if jobID == "" {
		return errors.New("nomad job id is required")
	}
	if len(expectations) == 0 {
		return errors.New("at least one task expectation is required")
	}
	if timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	ctx, span := c.tracer.Start(ctx, "verself_deploy.nomad.wait_tasks",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("nomad.job_id", jobID)),
	)
	defer span.End()
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		ready, err := c.tasksReady(waitCtx, jobID, expectations)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		if ready {
			span.SetStatus(codes.Ok, "")
			return nil
		}
		select {
		case <-waitCtx.Done():
			err := fmt.Errorf("wait for %s task readiness: %w", jobID, waitCtx.Err())
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		case <-ticker.C:
		}
	}
}

func (c *Client) WaitForBatchComplete(ctx context.Context, jobID string, timeout time.Duration) error {
	if jobID == "" {
		return errors.New("nomad job id is required")
	}
	if timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	ctx, span := c.tracer.Start(ctx, "verself_deploy.nomad.wait_batch_complete",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("nomad.job_id", jobID)),
	)
	defer span.End()
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		status, err := c.batchStatus(waitCtx, jobID)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		if status.complete > 0 {
			span.SetAttributes(
				attribute.Int("nomad.allocations_complete", status.complete),
				attribute.Int("nomad.allocations_failed", status.failed),
			)
			span.SetStatus(codes.Ok, "")
			return nil
		}
		if status.dead && status.active == 0 {
			err := fmt.Errorf("%s finished without a successful allocation: failed=%d", jobID, status.failed)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		select {
		case <-waitCtx.Done():
			err := fmt.Errorf("wait for %s completion: %w", jobID, waitCtx.Err())
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		case <-ticker.C:
		}
	}
}

type batchStatus struct {
	dead     bool
	active   int
	complete int
	failed   int
}

func (c *Client) batchStatus(ctx context.Context, jobID string) (batchStatus, error) {
	job, _, err := c.api.Jobs().Info(jobID, (&api.QueryOptions{}).WithContext(ctx))
	if err != nil {
		return batchStatus{}, fmt.Errorf("read %s: %w", jobID, err)
	}
	status := batchStatus{}
	if job != nil && job.Status != nil && *job.Status == "dead" {
		status.dead = true
	}
	allocs, _, err := c.api.Jobs().Allocations(jobID, true, (&api.QueryOptions{}).WithContext(ctx))
	if err != nil {
		return batchStatus{}, fmt.Errorf("read %s allocations: %w", jobID, err)
	}
	for _, alloc := range allocs {
		switch alloc.ClientStatus {
		case "complete":
			status.complete++
		case "failed", "lost", "unknown":
			status.failed++
		case "pending", "running":
			status.active++
		default:
			status.active++
		}
	}
	return status, nil
}

func (c *Client) tasksReady(ctx context.Context, jobID string, expectations []TaskExpectation) (bool, error) {
	allocs, _, err := c.api.Jobs().Allocations(jobID, true, (&api.QueryOptions{}).WithContext(ctx))
	if err != nil {
		return false, fmt.Errorf("read %s allocations: %w", jobID, err)
	}
	for _, alloc := range allocs {
		if alloc.DesiredStatus != "run" {
			continue
		}
		if taskStatesMatch(alloc.TaskStates, expectations) {
			return true, nil
		}
	}
	return false, nil
}

func taskStatesMatch(states map[string]*api.TaskState, expectations []TaskExpectation) bool {
	for _, expectation := range expectations {
		state := states[expectation.Name]
		if state == nil {
			return false
		}
		if expectation.State != "" && state.State != expectation.State {
			return false
		}
		if expectation.ExitCode != nil {
			code, ok := latestExitCode(state)
			if !ok || code != *expectation.ExitCode {
				return false
			}
		}
	}
	return true
}

func latestExitCode(state *api.TaskState) (int, bool) {
	for i := len(state.Events) - 1; i >= 0; i-- {
		event := state.Events[i]
		if event.Type == api.TaskTerminated {
			return event.ExitCode, true
		}
	}
	return 0, false
}
