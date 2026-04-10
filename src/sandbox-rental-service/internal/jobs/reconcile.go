package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	billingclient "github.com/forge-metal/billing-service/client"
	vmorchestrator "github.com/forge-metal/vm-orchestrator"
	"github.com/google/uuid"
)

const reconcileStaleAfter = 10 * time.Second

type reconcilerJobReader interface {
	GetJobStatus(ctx context.Context, jobID string, includeOutput bool) (vmorchestrator.JobStatus, error)
}

type reconcileCandidate struct {
	ExecutionID       uuid.UUID
	AttemptID         uuid.UUID
	State             string
	WindowState       string
	FailureReason     string
	ExitCode          int
	DurationMs        int64
	StartedAt         sql.NullTime
	CompletedAt       sql.NullTime
	GoldenSnapshot    string
	Reservation       billingclient.Reservation
	WindowSeq         int
	WindowSeconds     int
	OrchestratorJobID string
}

func (s *Service) Reconcile(ctx context.Context) error {
	if err := s.reconcileReservedAttempts(ctx); err != nil {
		return err
	}
	if err := s.reconcileFinalizingAttempts(ctx); err != nil {
		return err
	}
	if err := s.reconcileActiveAttempts(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Service) reconcileReservedAttempts(ctx context.Context) error {
	rows, err := s.PG.QueryContext(ctx, `
		SELECT
			e.execution_id,
			a.attempt_id,
			w.window_seq,
			w.window_seconds,
			w.reservation
		FROM executions e
		JOIN execution_attempts a ON a.execution_id = e.execution_id
		JOIN execution_billing_windows w ON w.attempt_id = a.attempt_id AND w.window_seq = 0
		WHERE a.state = 'reserved'
		  AND w.state = 'reserved'
		  AND a.orchestrator_job_id = ''
		  AND a.updated_at < (now() - ($1 * interval '1 second'))
	`, int(reconcileStaleAfter.Seconds()))
	if err != nil {
		return fmt.Errorf("query reserved reconciliation candidates: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			candidate       reconcileCandidate
			reservationJSON []byte
		)
		if err := rows.Scan(
			&candidate.ExecutionID,
			&candidate.AttemptID,
			&candidate.WindowSeq,
			&candidate.WindowSeconds,
			&reservationJSON,
		); err != nil {
			return fmt.Errorf("scan reserved reconciliation candidate: %w", err)
		}
		if err := json.Unmarshal(reservationJSON, &candidate.Reservation); err != nil {
			return fmt.Errorf("decode reserved reconciliation reservation: %w", err)
		}
		if err := s.Billing.Void(ctx, candidate.Reservation); err != nil {
			return fmt.Errorf("void stale reservation attempt %s: %w", candidate.AttemptID, err)
		}
		completedAt := time.Now().UTC()
		s.writeSystemLog(ctx, candidate.ExecutionID, candidate.AttemptID, "reconciler voided stale reserved window_seq=%d", candidate.WindowSeq)
		if err := s.markWindowVoided(ctx, candidate.AttemptID, candidate.WindowSeq, completedAt); err != nil {
			return fmt.Errorf("mark stale window voided %s: %w", candidate.AttemptID, err)
		}
		if err := s.markTerminal(ctx, candidate.ExecutionID, candidate.AttemptID, executionOutcome{
			State:         StateFailed,
			FailureReason: "reconciled_reserved_timeout",
			StartedAt:     completedAt,
			CompletedAt:   completedAt,
		}); err != nil {
			return fmt.Errorf("mark stale reserved attempt terminal %s: %w", candidate.AttemptID, err)
		}
	}
	return rows.Err()
}

func (s *Service) reconcileFinalizingAttempts(ctx context.Context) error {
	rows, err := s.PG.QueryContext(ctx, `
		SELECT
			e.execution_id,
			a.attempt_id,
			a.failure_reason,
			COALESCE(a.exit_code, 0),
			COALESCE(a.duration_ms, 0),
			a.started_at,
			a.completed_at,
			a.golden_snapshot,
			w.window_seq,
			w.window_seconds,
			w.state,
			w.reservation
		FROM executions e
		JOIN execution_attempts a ON a.execution_id = e.execution_id
		JOIN LATERAL (
			SELECT window_seq, window_seconds, state, reservation
			FROM execution_billing_windows
			WHERE attempt_id = a.attempt_id
			ORDER BY window_seq DESC
			LIMIT 1
		) w ON true
		WHERE a.state = 'finalizing'
		  AND w.state IN ('reserved', 'settled', 'voided')
	`)
	if err != nil {
		return fmt.Errorf("query finalizing reconciliation candidates: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			candidate       reconcileCandidate
			reservationJSON []byte
		)
		if err := rows.Scan(
			&candidate.ExecutionID,
			&candidate.AttemptID,
			&candidate.FailureReason,
			&candidate.ExitCode,
			&candidate.DurationMs,
			&candidate.StartedAt,
			&candidate.CompletedAt,
			&candidate.GoldenSnapshot,
			&candidate.WindowSeq,
			&candidate.WindowSeconds,
			&candidate.WindowState,
			&reservationJSON,
		); err != nil {
			return fmt.Errorf("scan finalizing reconciliation candidate: %w", err)
		}
		if err := json.Unmarshal(reservationJSON, &candidate.Reservation); err != nil {
			return fmt.Errorf("decode finalizing reconciliation reservation: %w", err)
		}
		if err := s.reconcileFinalizingCandidate(ctx, candidate); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Service) reconcileActiveAttempts(ctx context.Context) error {
	reader, ok := s.Orchestrator.(reconcilerJobReader)
	if !ok {
		return nil
	}

	rows, err := s.PG.QueryContext(ctx, `
		SELECT
			e.execution_id,
			a.attempt_id,
			a.state,
			a.orchestrator_job_id
		FROM executions e
		JOIN execution_attempts a ON a.execution_id = e.execution_id
		WHERE a.state IN ('launching', 'running')
		  AND a.orchestrator_job_id <> ''
		  AND a.updated_at < (now() - ($1 * interval '1 second'))
	`, int(reconcileStaleAfter.Seconds()))
	if err != nil {
		return fmt.Errorf("query active reconciliation candidates: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			executionID       uuid.UUID
			attemptID         uuid.UUID
			state             string
			orchestratorJobID string
		)
		if err := rows.Scan(&executionID, &attemptID, &state, &orchestratorJobID); err != nil {
			return fmt.Errorf("scan active reconciliation candidate: %w", err)
		}
		status, err := reader.GetJobStatus(ctx, orchestratorJobID, true)
		if err != nil {
			if err := s.markLost(ctx, executionID, attemptID, "reconcile_status_lookup_failed"); err != nil {
				return err
			}
			continue
		}
		if !status.Terminal {
			continue
		}
		candidate, err := s.loadFinalizingCandidate(ctx, executionID, attemptID)
		if err != nil {
			return err
		}
		candidate.State = state
		candidate.OrchestratorJobID = orchestratorJobID
		candidate = applyJobStatusToCandidate(candidate, status)
		if err := s.markFinalizing(ctx, executionID, attemptID, outcomeFromCandidate(candidate)); err != nil {
			return fmt.Errorf("mark reconciled active attempt finalizing %s: %w", attemptID, err)
		}
		if err := s.settleReconciledAttempt(ctx, candidate); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Service) loadFinalizingCandidate(ctx context.Context, executionID, attemptID uuid.UUID) (reconcileCandidate, error) {
	var (
		candidate       reconcileCandidate
		reservationJSON []byte
	)
	row := s.PG.QueryRowContext(ctx, `
		SELECT
			e.execution_id,
			a.attempt_id,
			a.failure_reason,
			COALESCE(a.exit_code, 0),
			COALESCE(a.duration_ms, 0),
			a.started_at,
			a.completed_at,
			a.golden_snapshot,
			w.window_seq,
			w.window_seconds,
			w.state,
			w.reservation
		FROM executions e
		JOIN execution_attempts a ON a.execution_id = e.execution_id
		JOIN LATERAL (
			SELECT window_seq, window_seconds, state, reservation
			FROM execution_billing_windows
			WHERE attempt_id = a.attempt_id
			ORDER BY window_seq DESC
			LIMIT 1
		) w ON true
		WHERE e.execution_id = $1 AND a.attempt_id = $2
	`, executionID, attemptID)
	if err := row.Scan(
		&candidate.ExecutionID,
		&candidate.AttemptID,
		&candidate.FailureReason,
		&candidate.ExitCode,
		&candidate.DurationMs,
		&candidate.StartedAt,
		&candidate.CompletedAt,
		&candidate.GoldenSnapshot,
		&candidate.WindowSeq,
		&candidate.WindowSeconds,
		&candidate.WindowState,
		&reservationJSON,
	); err != nil {
		return reconcileCandidate{}, fmt.Errorf("load finalizing candidate %s: %w", attemptID, err)
	}
	if err := json.Unmarshal(reservationJSON, &candidate.Reservation); err != nil {
		return reconcileCandidate{}, fmt.Errorf("decode finalizing reservation %s: %w", attemptID, err)
	}
	return candidate, nil
}

func (s *Service) reconcileFinalizingCandidate(ctx context.Context, candidate reconcileCandidate) error {
	switch candidate.WindowState {
	case "reserved":
		// If a previous billing operation failed (settle or void), the
		// reconciler should void rather than retry the original operation.
		reason := strings.TrimSpace(candidate.FailureReason)
		if reason == "billing_settle_failed" || reason == "billing_void_failed" {
			return s.voidReconciledAttempt(ctx, candidate)
		}
		return s.settleReconciledAttempt(ctx, candidate)
	case "settled", "voided":
		if err := s.markTerminal(ctx, candidate.ExecutionID, candidate.AttemptID, outcomeFromCandidate(candidate)); err != nil {
			return fmt.Errorf("mark reconciled attempt terminal %s: %w", candidate.AttemptID, err)
		}
		s.writeSystemLog(ctx, candidate.ExecutionID, candidate.AttemptID, "reconciler finalized attempt from window_state=%s", candidate.WindowState)
		return nil
	default:
		return fmt.Errorf("unsupported finalizing reconciliation window state %q for attempt %s", candidate.WindowState, candidate.AttemptID)
	}
}

func (s *Service) settleReconciledAttempt(ctx context.Context, candidate reconcileCandidate) error {
	actualSeconds := actualSecondsForReservation(candidate.Reservation, candidate.CompletedAt.Time)
	if err := s.Billing.Settle(ctx, candidate.Reservation, uint32(actualSeconds)); err != nil {
		s.writeSystemLog(ctx, candidate.ExecutionID, candidate.AttemptID, "reconciler settle failed window_seq=%d actual_seconds=%d error=%v", candidate.WindowSeq, actualSeconds, err)
		candidate.FailureReason = "billing_settle_failed"
		if updateErr := s.markFinalizing(ctx, candidate.ExecutionID, candidate.AttemptID, outcomeFromCandidate(candidate)); updateErr != nil {
			return fmt.Errorf("persist billing failure on reconciled attempt %s: %w", candidate.AttemptID, updateErr)
		}
		if voidErr := s.voidReconciledAttempt(ctx, candidate); voidErr != nil {
			return fmt.Errorf("settle reconciled attempt %s: %w (void fallback: %v)", candidate.AttemptID, err, voidErr)
		}
		return nil
	}
	settledAt := time.Now().UTC()
	s.writeSystemLog(ctx, candidate.ExecutionID, candidate.AttemptID, "reconciler settled reserved window_seq=%d actual_seconds=%d", candidate.WindowSeq, actualSeconds)
	if err := s.markWindowSettled(ctx, candidate.AttemptID, candidate.WindowSeq, actualSeconds, candidate.Reservation.PricingPhase, settledAt); err != nil {
		return fmt.Errorf("mark reconciled window settled %s: %w", candidate.AttemptID, err)
	}
	if err := s.markTerminal(ctx, candidate.ExecutionID, candidate.AttemptID, outcomeFromCandidate(candidate)); err != nil {
		return fmt.Errorf("mark reconciled attempt terminal %s: %w", candidate.AttemptID, err)
	}
	return nil
}

func (s *Service) voidReconciledAttempt(ctx context.Context, candidate reconcileCandidate) error {
	if err := s.Billing.Void(ctx, candidate.Reservation); err != nil {
		return fmt.Errorf("void reconciled attempt %s: %w", candidate.AttemptID, err)
	}
	voidedAt := time.Now().UTC()
	s.writeSystemLog(ctx, candidate.ExecutionID, candidate.AttemptID, "reconciler voided billing window_seq=%d after settle failure", candidate.WindowSeq)
	if err := s.markWindowVoided(ctx, candidate.AttemptID, candidate.WindowSeq, voidedAt); err != nil {
		return fmt.Errorf("mark reconciled window voided %s: %w", candidate.AttemptID, err)
	}
	if err := s.markTerminal(ctx, candidate.ExecutionID, candidate.AttemptID, outcomeFromCandidate(candidate)); err != nil {
		return fmt.Errorf("mark reconciled voided attempt terminal %s: %w", candidate.AttemptID, err)
	}
	return nil
}

func (s *Service) markLost(ctx context.Context, executionID, attemptID uuid.UUID, reason string) error {
	now := time.Now().UTC()
	s.writeSystemLog(ctx, executionID, attemptID, "reconciler marked attempt lost reason=%s", strings.TrimSpace(reason))

	// Void any reserved billing windows before marking lost.
	// Without this, reserved credits leak permanently in TigerBeetle.
	if err := s.voidReservedWindows(ctx, attemptID, now); err != nil {
		return fmt.Errorf("void reserved windows for lost attempt %s: %w", attemptID, err)
	}

	tx, err := s.PG.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE execution_attempts
		SET state = $2, failure_reason = $3, updated_at = $4
		WHERE attempt_id = $1
	`, attemptID, StateLost, strings.TrimSpace(reason), now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE executions
		SET status = $2, updated_at = $3
		WHERE execution_id = $1
	`, executionID, StateLost, now); err != nil {
		return err
	}
	return tx.Commit()
}

// voidReservedWindows loads and voids all billing windows still in 'reserved' state
// for the given attempt. This ensures credits are returned to the org on lost attempts.
func (s *Service) voidReservedWindows(ctx context.Context, attemptID uuid.UUID, now time.Time) error {
	rows, err := s.PG.QueryContext(ctx, `
		SELECT window_seq, reservation
		FROM execution_billing_windows
		WHERE attempt_id = $1 AND state = 'reserved'
	`, attemptID)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			windowSeq       int
			reservationJSON []byte
		)
		if err := rows.Scan(&windowSeq, &reservationJSON); err != nil {
			return err
		}
		var reservation billingclient.Reservation
		if err := json.Unmarshal(reservationJSON, &reservation); err != nil {
			return fmt.Errorf("decode reservation window_seq=%d: %w", windowSeq, err)
		}
		if err := s.Billing.Void(ctx, reservation); err != nil {
			return fmt.Errorf("void window_seq=%d: %w", windowSeq, err)
		}
		if err := s.markWindowVoided(ctx, attemptID, windowSeq, now); err != nil {
			return fmt.Errorf("mark window_seq=%d voided: %w", windowSeq, err)
		}
	}
	return rows.Err()
}

func applyJobStatusToCandidate(candidate reconcileCandidate, status vmorchestrator.JobStatus) reconcileCandidate {
	candidate.CompletedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
	if !candidate.StartedAt.Valid {
		candidate.StartedAt = sql.NullTime{Time: candidate.CompletedAt.Time, Valid: true}
	}
	if status.Result != nil {
		candidate.ExitCode = status.Result.ExitCode
		candidate.DurationMs = status.Result.Duration.Milliseconds()
		if status.Result.Duration <= 0 {
			candidate.DurationMs = candidate.CompletedAt.Time.Sub(candidate.StartedAt.Time).Milliseconds()
		}
		// GoldenSnapshot is already populated from the DB row; no override needed.
	}
	if status.ErrorMessage != "" {
		candidate.FailureReason = status.ErrorMessage
	}
	return candidate
}

func outcomeFromCandidate(candidate reconcileCandidate) executionOutcome {
	startedAt := time.Now().UTC()
	if candidate.StartedAt.Valid {
		startedAt = candidate.StartedAt.Time
	}
	completedAt := time.Now().UTC()
	if candidate.CompletedAt.Valid {
		completedAt = candidate.CompletedAt.Time
	}
	outcome := executionOutcome{
		FailureReason:  strings.TrimSpace(candidate.FailureReason),
		ExitCode:       candidate.ExitCode,
		DurationMs:     candidate.DurationMs,
		GoldenSnapshot: candidate.GoldenSnapshot,
		StartedAt:      startedAt,
		CompletedAt:    completedAt,
	}
	switch {
	case outcome.FailureReason == "attempt_canceled":
		outcome.State = StateCanceled
	case outcome.ExitCode == 0 && outcome.FailureReason == "":
		outcome.State = StateSucceeded
	default:
		outcome.State = StateFailed
	}
	if outcome.DurationMs == 0 {
		outcome.DurationMs = outcome.CompletedAt.Sub(outcome.StartedAt).Milliseconds()
	}
	return outcome
}
