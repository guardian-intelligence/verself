package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/verself/sandbox-rental-service/internal/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// flightActiveStatuses mirrors the Electric shape `where status IN (...)` in
// the web app's electric proxy. A GitHub job leaves the departures board the
// instant GitHub reports it completed; these two lists must stay in lockstep.
var flightActiveStatuses = map[string]struct{}{
	"queued":      {},
	"in_progress": {},
	"waiting":     {},
}

func isActiveFlightStatus(status string) bool {
	_, ok := flightActiveStatuses[status]
	return ok
}

// projectFlightJob maintains the console_flight_jobs read-model row for a
// GitHub workflow job, inside the webhook transaction so the projection is
// transactionally consistent with the runner_jobs/invocation upserts. It is a
// pure projection: the web app only ever reads this table over Electric.
//
// Returns the resolved Verself org id, or "" when the repository is not bound
// to an org (nothing to show: no row is written and no baseline is computed).
func projectFlightJob(
	ctx context.Context,
	q *store.Queries,
	event GitHubWorkflowJobWebhook,
	status string,
	now time.Time,
) (string, error) {
	ctx, span := tracer.Start(ctx, "flight.projection.upsert")
	defer span.End()

	orgID, err := q.GetGitHubRunnerRepositoryOrgID(ctx, store.GetGitHubRunnerRepositoryOrgIDParams{
		ProviderRepositoryID: event.Repository.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			span.SetAttributes(attribute.Bool("flight.repo_unbound", true))
			return "", nil
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}

	if err := q.UpsertConsoleFlightJob(ctx, store.UpsertConsoleFlightJobParams{
		ProviderJobID:          event.WorkflowJob.ID,
		OrgID:                  orgID,
		ProviderRunID:          event.WorkflowJob.RunID,
		ProviderRunAttempt:     event.WorkflowJob.RunAttempt,
		RepositoryFullName:     event.Repository.FullName,
		WorkflowName:           event.WorkflowJob.WorkflowName,
		JobName:                event.WorkflowJob.Name,
		HeadBranch:             event.WorkflowJob.HeadBranch,
		HeadSha:                event.WorkflowJob.HeadSHA,
		ActorLogin:             event.Sender.Login,
		Status:                 status,
		StartedAt:              pgOptionalTime(event.WorkflowJob.StartedAt),
		UpdatedAt:              pgTime(now),
		ProviderInstallationID: event.Installation.ID,
		ProviderRepositoryID:   event.Repository.ID,
	}); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}

	span.SetAttributes(
		attribute.String("verself.org_id", orgID),
		attribute.Int64("github.workflow_run.id", event.WorkflowJob.RunID),
		attribute.String("github.repository_full_name", event.Repository.FullName),
		attribute.String("flight.status", status),
	)
	return orgID, nil
}

// refreshFlightBaseline freezes the p50 of the same repo/workflow/job over the
// last 90d of succeeded runs onto the flight row. Best-effort and detached
// from the webhook path: ClickHouse must never gate webhook availability, and
// the SetConsoleFlightBaseline guard (predicted_baseline_ms IS NULL) keeps the
// badge threshold frozen at first sighting. NULL stays NULL on cold start,
// which the widget renders as a clock with the badge pinned to "On Time".
func (s *Service) refreshFlightBaseline(ctx context.Context, providerJobID int64, orgID, repo, workflow, job string) {
	if s.CH == nil {
		return
	}
	ctx, span := tracer.Start(ctx, "flight.baseline.query")
	span.SetAttributes(
		attribute.String("verself.org_id", orgID),
		attribute.String("github.repository_full_name", repo),
		attribute.String("github.workflow_name", workflow),
		attribute.String("github.job_name", job),
	)

	var samples uint64
	var p50 float64
	if err := s.CH.QueryRow(ctx, `
		SELECT count(), quantileTDigest(0.5)(duration_ms)
		FROM `+s.CHDatabase+`.job_events
		WHERE org_id = $1
		  AND repository_full_name = $2
		  AND workflow_name = $3
		  AND job_name = $4
		  AND status = 'succeeded'
		  AND created_at > now() - INTERVAL 90 DAY
	`, orgID, repo, workflow, job).Scan(&samples, &p50); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		return
	}
	span.SetAttributes(attribute.Int64("flight.baseline.samples", int64(samples)))
	if samples == 0 {
		span.SetAttributes(attribute.Bool("flight.baseline.cold_start", true))
		span.End()
		return
	}
	baselineMs := int64(p50 + 0.5)
	span.SetAttributes(attribute.Int64("flight.baseline.p50_ms", baselineMs))
	span.End()

	uctx, uspan := tracer.Start(ctx, "flight.baseline.update")
	defer uspan.End()
	if err := s.storeQueries().SetConsoleFlightBaseline(uctx, store.SetConsoleFlightBaselineParams{
		PredictedBaselineMs: baselineMs,
		UpdatedAt:           pgTime(time.Now().UTC()),
		ProviderJobID:       providerJobID,
	}); err != nil {
		uspan.RecordError(err)
		uspan.SetStatus(codes.Error, err.Error())
	}
}
