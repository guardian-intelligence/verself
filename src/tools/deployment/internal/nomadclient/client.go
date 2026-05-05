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

const tracerName = "github.com/verself/deployment-tools/internal/nomadclient"

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
	PriorArtifactDigest string
	PriorSpecDigest     string
}

// Decide reads the currently-registered job (if any) and compares its
// resolver-stamped digests against the spec's. The CAS fence is the
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
		attribute.Int64("nomad.prior_job_modify_index", int64FromUint64(prevModify, "prior job modify index")),
		attribute.Int64("nomad.prior_version", int64FromUint64(prevVersion, "prior version")),
		attribute.Bool("nomad.prior_stopped", stopped),
		attribute.Bool("nomad.decision.noop", noop),
	)
	span.SetStatus(codes.Ok, "")
	return Decision{
		NoOp:                noop,
		PriorJobModifyIndex: prevModify,
		PriorVersion:        prevVersion,
		PriorStopped:        stopped,
		PriorArtifactDigest: curArtifact,
		PriorSpecDigest:     curSpec,
	}, nil
}

// ParseJobHCL asks the target Nomad agent to parse an authored HCL2 jobspec.
// This keeps deploy behavior aligned with the server version that will run the job.
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
		attribute.Int64("nomad.job_modify_index", int64FromUint64(resp.JobModifyIndex, "job modify index")),
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
			attribute.Int64("nomad.enforce_index", int64FromUint64(priorModifyIndex, "enforce index")),
			attribute.Int64("nomad.plan_modify_index", int64FromUint64(planModifyIndex, "plan modify index")),
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
		attribute.Int64("nomad.eval_create_index", int64FromUint64(resp.EvalCreateIndex, "eval create index")),
		attribute.Int64("nomad.job_modify_index", int64FromUint64(resp.JobModifyIndex, "job modify index")),
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

// ServiceAddress is the resolved network endpoint for one entry in
// Nomad's service catalog.
type ServiceAddress struct {
	Name      string
	ServiceID string
	AllocID   string
	JobID     string
	Address   string
	Port      int
}

// ListServiceAddresses returns the routable addresses of every
// Nomad-native service registration. Registrations are filtered through
// allocation deployment health so HAProxy does not route canaries before
// Nomad's own checks have passed.
//
// Caller-side filtering is expected: registrations come from any
// component that opted into Nomad service registration, including
// services the caller doesn't care about.
func (c *Client) ListServiceAddresses(ctx context.Context) ([]ServiceAddress, error) {
	ctx, span := c.tracer.Start(ctx, "verself_deploy.nomad.list_services",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	stubs, _, err := c.api.Services().List((&api.QueryOptions{}).WithContext(ctx))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("nomad services list: %w", err)
	}
	var out []ServiceAddress
	allocRoutable := map[string]bool{}
	for _, ns := range stubs {
		for _, svc := range ns.Services {
			regs, _, err := c.api.Services().Get(svc.ServiceName, (&api.QueryOptions{}).WithContext(ctx))
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return nil, fmt.Errorf("nomad services get %s: %w", svc.ServiceName, err)
			}
			for _, reg := range regs {
				if reg == nil {
					continue
				}
				routable, ok := allocRoutable[reg.AllocID]
				if !ok {
					var err error
					routable, err = c.allocationRoutable(ctx, reg.AllocID)
					if err != nil {
						span.RecordError(err)
						span.SetStatus(codes.Error, err.Error())
						return nil, err
					}
					allocRoutable[reg.AllocID] = routable
				}
				if !routable {
					continue
				}
				out = append(out, ServiceAddress{
					Name:      reg.ServiceName,
					ServiceID: reg.ID,
					AllocID:   reg.AllocID,
					JobID:     reg.JobID,
					Address:   reg.Address,
					Port:      reg.Port,
				})
			}
		}
	}
	span.SetAttributes(
		attribute.Int("nomad.services.count", len(out)),
		attribute.Int("nomad.allocations.inspected", len(allocRoutable)),
	)
	span.SetStatus(codes.Ok, "")
	return out, nil
}

func (c *Client) allocationRoutable(ctx context.Context, allocID string) (bool, error) {
	if allocID == "" {
		return false, nil
	}
	alloc, _, err := c.api.Allocations().Info(allocID, (&api.QueryOptions{}).WithContext(ctx))
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("nomad allocation info %s: %w", allocID, err)
	}
	if alloc == nil {
		return false, nil
	}
	if alloc.ClientStatus != api.AllocClientStatusRunning {
		return false, nil
	}
	if alloc.DeploymentStatus == nil {
		return true, nil
	}
	if alloc.DeploymentStatus.Healthy == nil {
		return false, nil
	}
	return *alloc.DeploymentStatus.Healthy, nil
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
