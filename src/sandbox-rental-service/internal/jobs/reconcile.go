package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	billingclient "github.com/forge-metal/billing-service/client"
	vmorchestrator "github.com/forge-metal/vm-orchestrator"
	"github.com/google/uuid"
)

const reconcileStaleAfter = 10 * time.Second

type reconcilerRunReader interface {
	GetRun(ctx context.Context, runID string, includeOutput bool) (vmorchestrator.HostRunSnapshot, error)
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
	WindowSeq         int
	BillingWindowID   string
	ReservationShape  string
	ReservedQuantity  int
	PricingPhase      string
	WindowStart       time.Time
	ActivatedAt       sql.NullTime
	Reservation       billingclient.Reservation
	OrchestratorRunID string
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
			w.billing_window_id,
			w.reservation_shape,
			w.reserved_quantity,
			w.pricing_phase,
				w.window_start,
				w.activated_at
		FROM executions e
		JOIN execution_attempts a ON a.execution_id = e.execution_id
		JOIN execution_billing_windows w ON w.attempt_id = a.attempt_id AND w.window_seq = 0
		WHERE a.state = 'reserved'
		  AND w.state = 'reserved'
		  AND a.orchestrator_run_id = ''
		  AND a.updated_at < (now() - ($1 * interval '1 second'))
	`, int(reconcileStaleAfter.Seconds()))
	if err != nil {
		return fmt.Errorf("query reserved reconciliation candidates: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var candidate reconcileCandidate
		if err := rows.Scan(
			&candidate.ExecutionID,
			&candidate.AttemptID,
			&candidate.WindowSeq,
			&candidate.BillingWindowID,
			&candidate.ReservationShape,
			&candidate.ReservedQuantity,
			&candidate.PricingPhase,
			&candidate.WindowStart,
			&candidate.ActivatedAt,
		); err != nil {
			return fmt.Errorf("scan reserved reconciliation candidate: %w", err)
		}
		candidate.Reservation = candidate.reservation()
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
			w.window_seq,
			w.billing_window_id,
			w.reservation_shape,
			w.reserved_quantity,
			w.pricing_phase,
			w.state,
				w.window_start,
				w.activated_at
		FROM executions e
		JOIN execution_attempts a ON a.execution_id = e.execution_id
		JOIN LATERAL (
			SELECT window_seq, billing_window_id, reservation_shape, reserved_quantity, pricing_phase, state, window_start, activated_at
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
		var candidate reconcileCandidate
		if err := rows.Scan(
			&candidate.ExecutionID,
			&candidate.AttemptID,
			&candidate.FailureReason,
			&candidate.ExitCode,
			&candidate.DurationMs,
			&candidate.StartedAt,
			&candidate.CompletedAt,
			&candidate.WindowSeq,
			&candidate.BillingWindowID,
			&candidate.ReservationShape,
			&candidate.ReservedQuantity,
			&candidate.PricingPhase,
			&candidate.WindowState,
			&candidate.WindowStart,
			&candidate.ActivatedAt,
		); err != nil {
			return fmt.Errorf("scan finalizing reconciliation candidate: %w", err)
		}
		candidate.Reservation = candidate.reservation()
		if err := s.reconcileFinalizingCandidate(ctx, candidate); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Service) reconcileActiveAttempts(ctx context.Context) error {
	reader, ok := s.Orchestrator.(reconcilerRunReader)
	if !ok {
		return nil
	}

	rows, err := s.PG.QueryContext(ctx, `
		SELECT
			e.execution_id,
			a.attempt_id,
			a.state,
			a.orchestrator_run_id
		FROM executions e
		JOIN execution_attempts a ON a.execution_id = e.execution_id
		WHERE a.state IN ('launching', 'running')
		  AND a.orchestrator_run_id <> ''
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
			orchestratorRunID string
		)
		if err := rows.Scan(&executionID, &attemptID, &state, &orchestratorRunID); err != nil {
			return fmt.Errorf("scan active reconciliation candidate: %w", err)
		}
		snapshot, err := reader.GetRun(ctx, orchestratorRunID, true)
		if err != nil {
			if err := s.markLost(ctx, executionID, attemptID, "reconcile_status_lookup_failed"); err != nil {
				return err
			}
			continue
		}
		if !snapshot.Terminal {
			continue
		}
		candidate, err := s.loadFinalizingCandidate(ctx, executionID, attemptID)
		if err != nil {
			return err
		}
		candidate.State = state
		candidate.OrchestratorRunID = orchestratorRunID
		candidate = applyRunSnapshotToCandidate(candidate, snapshot)
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
	var candidate reconcileCandidate
	row := s.PG.QueryRowContext(ctx, `
		SELECT
			e.execution_id,
			a.attempt_id,
			a.failure_reason,
			COALESCE(a.exit_code, 0),
			COALESCE(a.duration_ms, 0),
			a.started_at,
			a.completed_at,
			w.window_seq,
			w.billing_window_id,
			w.reservation_shape,
			w.reserved_quantity,
			w.pricing_phase,
			w.state,
			w.window_start,
			w.activated_at
		FROM executions e
		JOIN execution_attempts a ON a.execution_id = e.execution_id
		JOIN LATERAL (
			SELECT window_seq, billing_window_id, reservation_shape, reserved_quantity, pricing_phase, state, window_start, activated_at
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
		&candidate.WindowSeq,
		&candidate.BillingWindowID,
		&candidate.ReservationShape,
		&candidate.ReservedQuantity,
		&candidate.PricingPhase,
		&candidate.WindowState,
		&candidate.WindowStart,
		&candidate.ActivatedAt,
	); err != nil {
		return reconcileCandidate{}, fmt.Errorf("load finalizing candidate %s: %w", attemptID, err)
	}
	candidate.Reservation = candidate.reservation()
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
	if candidate.Reservation.ActivatedAt == nil {
		if !candidate.StartedAt.Valid {
			return s.voidReconciledAttempt(ctx, candidate)
		}
		activated, err := s.Billing.Activate(ctx, candidate.Reservation, candidate.StartedAt.Time.UTC())
		if err != nil {
			return fmt.Errorf("activate reconciled attempt %s: %w", candidate.AttemptID, err)
		}
		candidate.Reservation = activated
		candidate.WindowStart = activated.WindowStart
		candidate.ActivatedAt = sql.NullTime{Time: activated.WindowStart, Valid: true}
		if err := s.markWindowActivated(ctx, candidate.AttemptID, candidate.WindowSeq, activated.WindowStart); err != nil {
			return fmt.Errorf("mark reconciled window activated %s: %w", candidate.AttemptID, err)
		}
	}
	actualSeconds := actualSecondsForReservation(candidate.Reservation, candidate.CompletedAt.Time)
	if err := s.Billing.Settle(ctx, candidate.Reservation, uint32(actualSeconds), nil); err != nil {
		s.writeSystemLog(ctx, candidate.ExecutionID, candidate.AttemptID, "reconciler settle failed window_seq=%d actual_quantity=%d error=%v", candidate.WindowSeq, actualSeconds, err)
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
	s.writeSystemLog(ctx, candidate.ExecutionID, candidate.AttemptID, "reconciler settled reserved window_seq=%d actual_quantity=%d", candidate.WindowSeq, actualSeconds)
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

	fromState, err := s.lockAttemptState(ctx, tx, attemptID)
	if err != nil {
		return err
	}
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
	if err := s.appendExecutionEvent(ctx, tx, executionID, attemptID, fromState, StateLost, strings.TrimSpace(reason), now); err != nil {
		return err
	}
	return tx.Commit()
}

// voidReservedWindows loads and voids all billing windows still in 'reserved' state
// for the given attempt. This ensures credits are returned to the org on lost attempts.
func (s *Service) voidReservedWindows(ctx context.Context, attemptID uuid.UUID, now time.Time) error {
	rows, err := s.PG.QueryContext(ctx, `
		SELECT window_seq, billing_window_id, reservation_shape, reserved_quantity, pricing_phase, window_start, activated_at
		FROM execution_billing_windows
		WHERE attempt_id = $1 AND state = 'reserved'
	`, attemptID)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var candidate reconcileCandidate
		if err := rows.Scan(
			&candidate.WindowSeq,
			&candidate.BillingWindowID,
			&candidate.ReservationShape,
			&candidate.ReservedQuantity,
			&candidate.PricingPhase,
			&candidate.WindowStart,
			&candidate.ActivatedAt,
		); err != nil {
			return err
		}
		reservation := candidate.reservation()
		if err := s.Billing.Void(ctx, reservation); err != nil {
			return fmt.Errorf("void window_seq=%d: %w", candidate.WindowSeq, err)
		}
		if err := s.markWindowVoided(ctx, attemptID, candidate.WindowSeq, now); err != nil {
			return fmt.Errorf("mark window_seq=%d voided: %w", candidate.WindowSeq, err)
		}
	}
	return rows.Err()
}

func (candidate reconcileCandidate) reservation() billingclient.Reservation {
	var activatedAt *time.Time
	if candidate.ActivatedAt.Valid {
		value := candidate.ActivatedAt.Time.UTC()
		activatedAt = &value
	}
	return billingclient.Reservation{
		WindowId:         candidate.BillingWindowID,
		SourceType:       executionSourceType,
		SourceRef:        candidate.AttemptID.String(),
		WindowSeq:        int32(candidate.WindowSeq),
		ReservationShape: candidate.ReservationShape,
		WindowSecs:       int32(candidate.ReservedQuantity),
		PricingPhase:     candidate.PricingPhase,
		WindowStart:      candidate.WindowStart.UTC(),
		ActivatedAt:      activatedAt,
	}
}

func applyRunSnapshotToCandidate(candidate reconcileCandidate, snapshot vmorchestrator.HostRunSnapshot) reconcileCandidate {
	candidate.CompletedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
	if snapshot.Result != nil {
		candidate.ExitCode = snapshot.Result.ExitCode
		candidate.DurationMs = snapshot.Result.Duration.Milliseconds()
		if snapshot.Result.Duration <= 0 && candidate.StartedAt.Valid {
			candidate.DurationMs = candidate.CompletedAt.Time.Sub(candidate.StartedAt.Time).Milliseconds()
		}
	}
	if snapshot.TerminalReason != "" {
		candidate.FailureReason = snapshot.TerminalReason
	}
	return candidate
}

func outcomeFromCandidate(candidate reconcileCandidate) executionOutcome {
	var startedAt time.Time
	if candidate.StartedAt.Valid {
		startedAt = candidate.StartedAt.Time
	}
	completedAt := time.Now().UTC()
	if candidate.CompletedAt.Valid {
		completedAt = candidate.CompletedAt.Time
	}
	outcome := executionOutcome{
		FailureReason: strings.TrimSpace(candidate.FailureReason),
		ExitCode:      candidate.ExitCode,
		DurationMs:    candidate.DurationMs,
		StartedAt:     startedAt,
		CompletedAt:   completedAt,
	}
	switch {
	case outcome.FailureReason == "attempt_canceled":
		outcome.State = StateCanceled
	case outcome.ExitCode == 0 && outcome.FailureReason == "":
		outcome.State = StateSucceeded
	default:
		outcome.State = StateFailed
	}
	if outcome.DurationMs == 0 && !outcome.StartedAt.IsZero() {
		outcome.DurationMs = outcome.CompletedAt.Sub(outcome.StartedAt).Milliseconds()
	}
	return outcome
}
