package nomadclient

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/nomad/api"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/verself/deployment-tooling/internal/nomadclient"

// Client is a thin span-emitting wrapper around *api.Client. It carries
// its own tracer rather than reaching for the global one so tests can
// substitute a NoopTracerProvider via the otel global.
type Client struct {
	api    *api.Client
	tracer trace.Tracer
}

// New constructs a Client pointed at the given Nomad HTTP address.
// The address is the only configurable knob; namespace, region, and
// auth come from the standard NOMAD_* env vars that api.DefaultConfig
// already reads.
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

// Decision summarises whether a submit is needed and, if so, what
// JobModifyIndex the EnforceRegister call must CAS against. NoOp=true
// means the currently-registered job is already at the target digests
// and is not stopped — we exit successfully without burning an eval.
type Decision struct {
	NoOp                bool
	PriorJobModifyIndex uint64
	PriorVersion        uint64
	PriorStopped        bool
}

// Decide reads the currently-registered job (if any) and compares its
// renderer-stamped digests against the spec's. The CAS fence is the
// PriorJobModifyIndex; passing 0 to EnforceRegister tells Nomad "this
// job is new" which is what we want for the never-registered case.
func (c *Client) Decide(ctx context.Context, spec *Spec) (Decision, error) {
	ctx, span := c.tracer.Start(ctx, "verself_deploy.nomad.decide",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("nomad.job_id", spec.JobID()),
			attribute.String("verself.artifact_sha256", shortDigest(spec.ArtifactDigest)),
			attribute.String("verself.spec_sha256", shortDigest(spec.SpecDigest)),
		),
	)
	defer span.End()

	cur, _, err := c.api.Jobs().Info(spec.JobID(), (&api.QueryOptions{}).WithContext(ctx))
	if err != nil {
		// The Go client surfaces 404s as errors; treat "job not found"
		// as a clean first-submit signal rather than a failure.
		if isNotFound(err) {
			span.SetAttributes(attribute.Bool("nomad.job.exists", false))
			span.SetStatus(codes.Ok, "")
			return Decision{}, nil
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Decision{}, fmt.Errorf("inspect %s: %w", spec.JobID(), err)
	}

	prevModify := derefUint64(cur.JobModifyIndex)
	prevVersion := derefUint64(cur.Version)
	stopped := derefBool(cur.Stop)
	curArtifact := cur.Meta["artifact_sha256"]
	curSpec := cur.Meta["spec_sha256"]
	noop := !stopped && curArtifact == spec.ArtifactDigest && curSpec == spec.SpecDigest

	span.SetAttributes(
		attribute.Bool("nomad.job.exists", true),
		attribute.Int64("nomad.prior_job_modify_index", int64(prevModify)),
		attribute.Int64("nomad.prior_version", int64(prevVersion)),
		attribute.Bool("nomad.prior_stopped", stopped),
		attribute.Bool("nomad.decision.noop", noop),
	)
	span.SetStatus(codes.Ok, "")
	return Decision{
		NoOp:                noop,
		PriorJobModifyIndex: prevModify,
		PriorVersion:        prevVersion,
		PriorStopped:        stopped,
	}, nil
}

// SubmitResult is the handle returned from a successful Register call.
// DeploymentID may be empty if the job has no update stanza (system
// jobs, batch jobs); Monitor is a no-op in that case.
type SubmitResult struct {
	JobID          string
	EvalID         string
	JobModifyIndex uint64
	DeploymentID   string
}

// Submit issues Plan (for diff visibility) and EnforceRegister (for
// CAS-safe submit). The diff is a span attribute; the CAS fence is the
// caller-supplied priorModifyIndex (zero for first submits).
func (c *Client) Submit(ctx context.Context, spec *Spec, priorModifyIndex uint64) (*SubmitResult, error) {
	planResp, err := c.plan(ctx, spec)
	if err != nil {
		return nil, err
	}
	regResp, err := c.register(ctx, spec, priorModifyIndex, planResp.JobModifyIndex)
	if err != nil {
		return nil, err
	}
	return &SubmitResult{
		JobID:          spec.JobID(),
		EvalID:         regResp.EvalID,
		JobModifyIndex: regResp.JobModifyIndex,
		DeploymentID:   c.findDeploymentID(ctx, spec.JobID(), regResp.JobModifyIndex),
	}, nil
}

func (c *Client) plan(ctx context.Context, spec *Spec) (*api.JobPlanResponse, error) {
	ctx, span := c.tracer.Start(ctx, "verself_deploy.nomad.plan",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("nomad.job_id", spec.JobID())),
	)
	defer span.End()

	resp, _, err := c.api.Jobs().Plan(spec.Job, true, (&api.WriteOptions{}).WithContext(ctx))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("plan %s: %w", spec.JobID(), err)
	}
	span.SetAttributes(
		attribute.Int64("nomad.job_modify_index", int64(resp.JobModifyIndex)),
		attribute.Int("nomad.failed_tg_allocs", len(resp.FailedTGAllocs)),
	)
	if resp.Diff != nil {
		span.SetAttributes(attribute.String("nomad.diff_type", resp.Diff.Type))
	}
	span.SetStatus(codes.Ok, "")
	return resp, nil
}

func (c *Client) register(ctx context.Context, spec *Spec, priorModifyIndex, planModifyIndex uint64) (*api.JobRegisterResponse, error) {
	ctx, span := c.tracer.Start(ctx, "verself_deploy.nomad.register",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("nomad.job_id", spec.JobID()),
			attribute.Int64("nomad.enforce_index", int64(priorModifyIndex)),
			attribute.Int64("nomad.plan_modify_index", int64(planModifyIndex)),
		),
	)
	defer span.End()

	resp, _, err := c.api.Jobs().EnforceRegister(spec.Job, priorModifyIndex, (&api.WriteOptions{}).WithContext(ctx))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("register %s: %w", spec.JobID(), err)
	}
	span.SetAttributes(
		attribute.String("nomad.eval_id", resp.EvalID),
		attribute.Int64("nomad.eval_create_index", int64(resp.EvalCreateIndex)),
		attribute.Int64("nomad.job_modify_index", int64(resp.JobModifyIndex)),
	)
	span.SetStatus(codes.Ok, "")
	return resp, nil
}

// findDeploymentID returns the deployment id Nomad created for the
// just-submitted job, or empty string if the job has no update
// strategy. It tolerates the brief window after Register where the
// deployment object hasn't been written yet.
func (c *Client) findDeploymentID(ctx context.Context, jobID string, jobModifyIndex uint64) string {
	dep, _, err := c.api.Jobs().LatestDeployment(jobID, (&api.QueryOptions{}).WithContext(ctx))
	if err != nil || dep == nil {
		return ""
	}
	if dep.JobModifyIndex < jobModifyIndex {
		// LatestDeployment can briefly return the prior submission's
		// deployment after Register; the monitor loop reads through that.
		return ""
	}
	return dep.ID
}

// isNotFound matches Nomad's "Unexpected response code: 404" wrapping.
// The Go client surfaces 404s as a plain error rather than a typed
// sentinel, so we look at the substring; the upstream cli does the
// same. (See nomad/api/api.go around requireOK.)
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "Unexpected response code: 404")
}

func derefUint64(p *uint64) uint64 {
	if p == nil {
		return 0
	}
	return *p
}

func derefBool(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}
