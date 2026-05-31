// Package nomadclient is a typed wrapper around github.com/hashicorp/nomad/api.
// Authored source specs are owner-local HCL2 files parsed by the target Nomad
// server and then registered without deploy-runner mutation.
package nomadclient

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/nomad/api"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/verself/deployment-tools/internal/nomadclient"

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
