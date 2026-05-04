package nomadclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/nomad/api"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// MonitorWaitTime is the per-iteration blocking-query timeout. The
// upstream `nomad deployment status -monitor` command uses a similar
// value; keeping it bounded means the monitor span emits progress
// attributes regularly even on slow rollouts.
const MonitorWaitTime = 10 * time.Second

// FailFastDeadAllocs aborts the monitor once this many distinct
// allocations of the current deployment have entered a terminal failed
// state. The default rolling-restart contract (max_parallel=1,
// auto_revert=true) gives Nomad three chances; if all three die the
// same way no further waiting helps.
const FailFastDeadAllocs = 3

// Monitor mirrors the upstream `nomad deployment status -monitor`
// blocking-query loop on Deployments.Info. Returns nil when the
// deployment terminates successfully; returns a *TerminalError when
// the deployment ends in any non-successful state.
func (c *Client) Monitor(ctx context.Context, sub *SubmitResult) error {
	ctx, span := c.tracer.Start(ctx, "verself_deploy.nomad.deployment_monitor",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("nomad.job_id", sub.JobID),
			attribute.String("nomad.eval_id", sub.EvalID),
			attribute.Int64("nomad.job_modify_index", int64FromUint64(sub.JobModifyIndex, "job modify index")),
		),
	)
	defer span.End()

	if sub.DeploymentID == "" {
		// Register can return before the /v1/deployment row lands;
		// give Nomad one more chance to populate it.
		sub.DeploymentID = c.findDeploymentID(ctx, sub.JobID, sub.JobModifyIndex)
	}
	if sub.DeploymentID == "" {
		span.SetAttributes(attribute.Bool("nomad.deployment.exists", false))
		span.SetStatus(codes.Ok, "no deployment created (system/batch job)")
		return nil
	}
	span.SetAttributes(attribute.String("nomad.deployment_id", sub.DeploymentID))

	q := (&api.QueryOptions{
		AllowStale: true,
		WaitTime:   MonitorWaitTime,
	}).WithContext(ctx)

	for {
		dep, meta, err := c.api.Deployments().Info(sub.DeploymentID, q)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return fmt.Errorf("deployment info %s: %w", sub.DeploymentID, err)
		}
		if dep == nil {
			err := fmt.Errorf("deployment %s not found", sub.DeploymentID)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}

		switch dep.Status {
		case api.DeploymentStatusSuccessful:
			c.recordTerminalAttributes(span, dep, "successful")
			span.SetStatus(codes.Ok, "")
			return nil
		case api.DeploymentStatusFailed,
			api.DeploymentStatusCancelled,
			api.DeploymentStatusBlocked:
			c.recordTerminalAttributes(span, dep, dep.Status)
			reason := c.latestAllocFailure(ctx, sub.DeploymentID)
			err := &TerminalError{
				DeploymentID:      sub.DeploymentID,
				Status:            dep.Status,
				StatusDescription: dep.StatusDescription,
				Reason:            reason,
			}
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}

		// Still in flight. Surface progress on the span so a slow
		// rollout is visible to operators without enabling debug logs.
		desired, healthy, unhealthy, placed := tgTotals(dep)
		span.SetAttributes(
			attribute.Int("nomad.tg.desired_total", desired),
			attribute.Int("nomad.tg.healthy", healthy),
			attribute.Int("nomad.tg.unhealthy", unhealthy),
			attribute.Int("nomad.tg.placed", placed),
		)

		// Fail-fast on a wedged rollout. Deployments.Allocations
		// scopes the count to this deployment, so prior submissions
		// don't pollute it.
		if dead := c.countDeadDeploymentAllocs(ctx, sub.DeploymentID); dead >= FailFastDeadAllocs {
			reason := c.latestAllocFailure(ctx, sub.DeploymentID)
			err := &TerminalError{
				DeploymentID:      sub.DeploymentID,
				Status:            "fail_fast",
				StatusDescription: fmt.Sprintf("%d allocs already failed", dead),
				Reason:            reason,
			}
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}

		// LastIndex advances the blocking query for the next round.
		// Without it, Nomad returns immediately every iteration.
		q.WaitIndex = meta.LastIndex

		if err := ctx.Err(); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
}

func (c *Client) recordTerminalAttributes(span trace.Span, dep *api.Deployment, status string) {
	desired, healthy, unhealthy, placed := tgTotals(dep)
	span.SetAttributes(
		attribute.String("nomad.terminal_status", status),
		attribute.String("nomad.status_description", dep.StatusDescription),
		attribute.Int("nomad.tg.desired_total", desired),
		attribute.Int("nomad.tg.healthy", healthy),
		attribute.Int("nomad.tg.unhealthy", unhealthy),
		attribute.Int("nomad.tg.placed", placed),
	)
}

func tgTotals(dep *api.Deployment) (desired, healthy, unhealthy, placed int) {
	for _, tg := range dep.TaskGroups {
		desired += tg.DesiredTotal
		healthy += tg.HealthyAllocs
		unhealthy += tg.UnhealthyAllocs
		placed += tg.PlacedAllocs
	}
	return
}

// countDeadDeploymentAllocs counts terminal-failed allocs scoped to
// the deployment via /v1/deployment/<id>/allocations. The returned
// stubs are this deployment's submission only, so prior-failure noise
// from re-submits never trips the fail-fast gate.
func (c *Client) countDeadDeploymentAllocs(ctx context.Context, deploymentID string) int {
	allocs, _, err := c.api.Deployments().Allocations(deploymentID, (&api.QueryOptions{}).WithContext(ctx))
	if err != nil {
		return 0
	}
	dead := 0
	for _, a := range allocs {
		if a.ClientStatus == api.AllocClientStatusFailed || a.ClientStatus == api.AllocClientStatusLost {
			dead++
		}
	}
	return dead
}

// latestAllocFailure walks the deployment's most-recently-modified
// failed alloc and returns the most-informative event message —
// DriverError if present, DisplayMessage otherwise. Returns empty
// string when no relevant alloc failure can be attributed.
func (c *Client) latestAllocFailure(ctx context.Context, deploymentID string) string {
	allocs, _, err := c.api.Deployments().Allocations(deploymentID, (&api.QueryOptions{}).WithContext(ctx))
	if err != nil {
		return ""
	}
	var newest *api.AllocationListStub
	for _, a := range allocs {
		if a.ClientStatus != api.AllocClientStatusFailed && a.ClientStatus != api.AllocClientStatusLost {
			continue
		}
		if newest == nil || a.ModifyIndex > newest.ModifyIndex {
			newest = a
		}
	}
	if newest == nil {
		return ""
	}
	full, _, err := c.api.Allocations().Info(newest.ID, (&api.QueryOptions{}).WithContext(ctx))
	if err != nil || full == nil {
		return ""
	}
	var lastEvent string
	var lastTime int64
	for _, ts := range full.TaskStates {
		for _, ev := range ts.Events {
			if ev.Time < lastTime {
				continue
			}
			msg := ev.DriverError
			if msg == "" {
				msg = ev.DisplayMessage
			}
			if msg == "" {
				continue
			}
			lastTime = ev.Time
			lastEvent = ev.Type + ": " + msg
		}
	}
	return lastEvent
}

// TerminalError describes a deployment that ended in a non-successful
// state. Callers can errors.As() to branch on it.
type TerminalError struct {
	DeploymentID      string
	Status            string
	StatusDescription string
	Reason            string
}

func (e *TerminalError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("deployment %s ended with status=%s: %s; last alloc failure: %s",
			e.DeploymentID, e.Status, e.StatusDescription, e.Reason)
	}
	return fmt.Sprintf("deployment %s ended with status=%s: %s",
		e.DeploymentID, e.Status, e.StatusDescription)
}

// IsTerminal returns true when the error chain contains a *TerminalError.
func IsTerminal(err error) bool {
	var te *TerminalError
	return errors.As(err, &te)
}
