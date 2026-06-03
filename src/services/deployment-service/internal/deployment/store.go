package deployment

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const pgUniqueViolation = "23505"
const pgCannotConnectNow = "57P03"

type Store struct {
	PG *pgxpool.Pool
}

func (s Store) Ready(ctx context.Context) error {
	if s.PG == nil {
		return errors.New("postgres pool is nil")
	}
	return s.PG.Ping(ctx)
}

func (s Store) InsertQueued(ctx context.Context, r Record) error {
	if s.PG == nil {
		return errors.New("postgres pool is nil")
	}
	attempt, err := int32FromUint32(r.ProviderRunAttempt, "provider_run_attempt")
	if err != nil {
		return err
	}
	tx, err := s.PG.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin queued deployment insert: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `
INSERT INTO deployment_requests (
  deployment_id, site, sha, deploy_run_key, source_kind, source_subject,
  repository, ref, workflow, job_workflow_ref, provider_run_id,
  provider_run_attempt, state
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (site, source_kind, repository, provider_run_id, provider_run_attempt)
WHERE source_kind = 'github_actions_oidc' AND provider_run_id <> ''
DO NOTHING
`, r.DeploymentID, r.Site, r.SHA, r.DeployRunKey, r.SourceKind, r.SourceSubject,
		r.Repository, r.Ref, r.Workflow, r.JobWorkflowRef, r.ProviderRunID,
		attempt, StateQueued)
	if err != nil {
		if activeDeploymentConflict(err) {
			return ErrBusy
		}
		return fmt.Errorf("insert deployment request: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrDuplicate
	}
	if err := appendEventTx(ctx, tx, r.DeploymentID, StateQueued, "deployment.queued", ""); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit queued deployment insert: %w", err)
	}
	return nil
}

func (s Store) MarkRunning(ctx context.Context, deploymentID string) error {
	return s.updateState(ctx, deploymentID, StateRunning, "deployment.running", "", DeploymentResult{}, true, false)
}

func (s Store) MarkSucceeded(ctx context.Context, deploymentID string, result DeploymentResult) error {
	return s.updateState(ctx, deploymentID, StateSucceeded, "deployment.succeeded", "", result, false, true)
}

func (s Store) MarkFailed(ctx context.Context, deploymentID string, err error) error {
	return s.updateState(ctx, deploymentID, StateFailed, ErrorCode(err), err.Error(), DeploymentResult{}, false, true)
}

func (s Store) MarkInterrupted(ctx context.Context, site string, detail string) (int64, error) {
	if s.PG == nil {
		return 0, errors.New("postgres pool is nil")
	}
	site = strings.TrimSpace(site)
	if site == "" {
		return 0, errors.New("site is required")
	}
	tx, err := s.PG.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin interrupted deployment recovery: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
SELECT deployment_id
FROM deployment_requests
WHERE site = $1
  AND state IN ($2, $3)
ORDER BY created_at
`, site, StateQueued, StateRunning)
	if err != nil {
		return 0, fmt.Errorf("query interrupted deployments: %w", err)
	}
	var deploymentIDs []string
	for rows.Next() {
		var deploymentID string
		if err := rows.Scan(&deploymentID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan interrupted deployment: %w", err)
		}
		deploymentIDs = append(deploymentIDs, deploymentID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("read interrupted deployments: %w", err)
	}
	rows.Close()
	if len(deploymentIDs) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return 0, fmt.Errorf("commit interrupted deployment recovery: %w", err)
		}
		return 0, nil
	}
	for _, deploymentID := range deploymentIDs {
		command, err := tx.Exec(ctx, `
UPDATE deployment_requests
SET state = $1,
    error_code = $2,
    error_detail = $3,
    finished_at = now(),
    updated_at = now()
WHERE deployment_id = $4
  AND site = $5
  AND state IN ($6, $7)
`, StateFailed, "deployment.interrupted", detail, deploymentID, site, StateQueued, StateRunning)
		if err != nil {
			return 0, fmt.Errorf("mark interrupted deployment failed: %w", err)
		}
		if command.RowsAffected() == 0 {
			return 0, fmt.Errorf("interrupted deployment %s was not updated", deploymentID)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO deployment_events (deployment_id, state, code, detail)
VALUES ($1, $2, $3, $4)
`, deploymentID, StateFailed, "deployment.interrupted", detail); err != nil {
			return 0, fmt.Errorf("insert interrupted deployment event: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit interrupted deployment recovery: %w", err)
	}
	return int64(len(deploymentIDs)), nil
}

func activeDeploymentConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == pgUniqueViolation &&
		pgErr.ConstraintName == "deployment_requests_one_active_per_site_idx"
}

type DeploymentResult struct {
	NomadSubmittedJobs uint32
}

func (s Store) updateState(ctx context.Context, deploymentID string, state string, code string, detail string, result DeploymentResult, started bool, finished bool) error {
	if s.PG == nil {
		return errors.New("postgres pool is nil")
	}
	submittedJobs, err := int32FromUint32(result.NomadSubmittedJobs, "nomad_submitted_jobs")
	if err != nil {
		return err
	}
	tx, err := s.PG.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin deployment state update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `
UPDATE deployment_requests
SET state = $2,
    error_code = $3,
    error_detail = $4,
    nomad_submitted_jobs = CASE WHEN $5::integer > 0 THEN $5 ELSE nomad_submitted_jobs END,
    started_at = CASE WHEN $6 THEN now() ELSE started_at END,
    finished_at = CASE WHEN $7 THEN now() ELSE finished_at END,
    updated_at = now()
WHERE deployment_id = $1
`, deploymentID, state, code, detail, submittedJobs, started, finished)
	if err != nil {
		return fmt.Errorf("update deployment request: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := appendEventTx(ctx, tx, deploymentID, state, code, detail); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit deployment state update: %w", err)
	}
	return nil
}

func (s Store) AppendEvent(ctx context.Context, deploymentID string, state string, code string, detail string) error {
	if s.PG == nil {
		return errors.New("postgres pool is nil")
	}
	_, err := s.PG.Exec(ctx, `
INSERT INTO deployment_events (deployment_id, state, code, detail)
VALUES ($1,$2,$3,$4)
`, deploymentID, state, code, detail)
	if err != nil {
		return fmt.Errorf("insert deployment event: %w", err)
	}
	return nil
}

func appendEventTx(ctx context.Context, tx pgx.Tx, deploymentID string, state string, code string, detail string) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO deployment_events (deployment_id, state, code, detail)
VALUES ($1,$2,$3,$4)
`, deploymentID, state, code, detail); err != nil {
		return fmt.Errorf("insert deployment event: %w", err)
	}
	return nil
}

func (s Store) Get(ctx context.Context, deploymentID string) (Record, error) {
	if s.PG == nil {
		return Record{}, errors.New("postgres pool is nil")
	}
	row := s.PG.QueryRow(ctx, `
SELECT deployment_id, site, sha, deploy_run_key, source_kind, source_subject,
       repository, ref, workflow, job_workflow_ref, provider_run_id,
       provider_run_attempt, state, error_code, error_detail,
       nomad_submitted_jobs,
       created_at, updated_at, started_at, finished_at
FROM deployment_requests
WHERE deployment_id = $1
`, deploymentID)
	return scanRecord(row)
}

func (s Store) FindBySourceAttempt(ctx context.Context, site string, source Source) (Record, bool, error) {
	if s.PG == nil {
		return Record{}, false, errors.New("postgres pool is nil")
	}
	attempt, err := int32FromUint32(source.RunAttempt, "provider_run_attempt")
	if err != nil {
		return Record{}, false, err
	}
	row := s.PG.QueryRow(ctx, `
SELECT deployment_id, site, sha, deploy_run_key, source_kind, source_subject,
       repository, ref, workflow, job_workflow_ref, provider_run_id,
       provider_run_attempt, state, error_code, error_detail,
       nomad_submitted_jobs,
       created_at, updated_at, started_at, finished_at
FROM deployment_requests
WHERE site = $1
  AND source_kind = $2
  AND repository = $3
  AND provider_run_id = $4
  AND provider_run_attempt = $5
ORDER BY created_at DESC
LIMIT 1
`, site, source.Kind, source.Repository, source.RunID, attempt)
	record, err := scanRecord(row)
	if errors.Is(err, ErrNotFound) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, err
	}
	return record, true, nil
}

func (s Store) ListEvents(ctx context.Context, deploymentID string) ([]Event, error) {
	if s.PG == nil {
		return nil, errors.New("postgres pool is nil")
	}
	rows, err := s.PG.Query(ctx, `
SELECT event_id, deployment_id, at, state, code, detail
FROM deployment_events
WHERE deployment_id = $1
ORDER BY at, event_id
`, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("query deployment events: %w", err)
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.EventID, &event.DeploymentID, &event.At, &event.State, &event.Code, &event.Detail); err != nil {
			return nil, fmt.Errorf("scan deployment event: %w", err)
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read deployment events: %w", err)
	}
	return out, nil
}

type recordRow interface {
	Scan(dest ...any) error
}

func scanRecord(row recordRow) (Record, error) {
	var record Record
	var attempt int32
	var submittedJobs int32
	if err := row.Scan(
		&record.DeploymentID,
		&record.Site,
		&record.SHA,
		&record.DeployRunKey,
		&record.SourceKind,
		&record.SourceSubject,
		&record.Repository,
		&record.Ref,
		&record.Workflow,
		&record.JobWorkflowRef,
		&record.ProviderRunID,
		&attempt,
		&record.State,
		&record.ErrorCode,
		&record.ErrorDetail,
		&submittedJobs,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.StartedAt,
		&record.FinishedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Record{}, ErrNotFound
		}
		return Record{}, datastoreError("scan deployment request", err)
	}
	convertedAttempt, err := uint32FromInt32(attempt, "provider_run_attempt")
	if err != nil {
		return Record{}, err
	}
	convertedSubmittedJobs, err := uint32FromInt32(submittedJobs, "nomad_submitted_jobs")
	if err != nil {
		return Record{}, err
	}
	record.ProviderRunAttempt = convertedAttempt
	record.NomadSubmittedJobs = convertedSubmittedJobs
	return record, nil
}

func datastoreError(operation string, err error) error {
	if postgresUnavailable(err) {
		return coded("deployment.datastore_unavailable", fmt.Errorf("%w: %s: %w", ErrUnavailable, operation, err))
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func postgresUnavailable(err error) bool {
	var connectErr *pgconn.ConnectError
	if errors.As(err, &connectErr) {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgCannotConnectNow
}

func int32FromUint32(value uint32, field string) (int32, error) {
	const maxInt32 = uint32(1<<31 - 1)
	if value > maxInt32 {
		return 0, fmt.Errorf("%s exceeds int32 range: %d", field, value)
	}
	return int32(value), nil // #nosec G115 -- value is checked against the int32 range above.
}

func uint32FromInt32(value int32, field string) (uint32, error) {
	if value < 0 {
		return 0, fmt.Errorf("%s is negative: %d", field, value)
	}
	return uint32(value), nil // #nosec G115 -- value is checked to be non-negative above.
}
