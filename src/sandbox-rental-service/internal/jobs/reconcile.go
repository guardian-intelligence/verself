package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const reconcileStaleAfter = 10 * time.Second

// Reconcile repairs durable execution state after a worker or caller context dies.
// vm-orchestrator enforces lease deadlines locally, so this reconciler focuses on
// control-plane rows and billing holds that can be stranded by sandbox crashes.
func (s *Service) Reconcile(ctx context.Context) error {
	if err := s.reconcileReservedAttempts(ctx); err != nil {
		return err
	}
	if err := s.reconcileLaunchingAttempts(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Service) reconcileReservedAttempts(ctx context.Context) error {
	rows, err := s.PGX.Query(ctx, `
		SELECT e.execution_id, a.attempt_id, COALESCE(w.billing_window_id, '')
		FROM executions e
		JOIN execution_attempts a ON a.execution_id = e.execution_id
		LEFT JOIN execution_billing_windows w ON w.attempt_id = a.attempt_id
		WHERE a.state = $1
		  AND COALESCE(w.state, 'reserved') = 'reserved'
		  AND COALESCE(a.lease_id, '') = ''
		  AND a.updated_at < (now() - ($2 * interval '1 second'))
	`, StateReserved, int(reconcileStaleAfter.Seconds()))
	if err != nil {
		return fmt.Errorf("query stale reserved attempts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		item, err := s.scanReconcileWorkItem(ctx, rows)
		if err != nil {
			return err
		}
		if item.windowID != "" {
			_ = s.markBillingWindow(ctx, item.AttemptID, item.windowID, "voided", 0)
		}
		if err := s.failAttempt(ctx, item.executionWorkItem, "reconciled_reserved_timeout", nil); err != nil {
			return fmt.Errorf("fail stale reserved attempt %s: %w", item.AttemptID, err)
		}
	}
	return rows.Err()
}

func (s *Service) reconcileLaunchingAttempts(ctx context.Context) error {
	rows, err := s.PGX.Query(ctx, `
		SELECT e.execution_id, a.attempt_id, COALESCE(w.billing_window_id, '')
		FROM executions e
		JOIN execution_attempts a ON a.execution_id = e.execution_id
		LEFT JOIN LATERAL (
			SELECT billing_window_id
			FROM execution_billing_windows
			WHERE attempt_id = a.attempt_id
			ORDER BY window_seq DESC
			LIMIT 1
		) w ON true
		WHERE a.state = $1
		  AND COALESCE(a.exec_id, '') = ''
		  AND a.updated_at < (now() - ($2 * interval '1 second'))
	`, StateLaunching, int((5 * time.Minute).Seconds()))
	if err != nil {
		return fmt.Errorf("query stale launching attempts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		item, err := s.scanReconcileWorkItem(ctx, rows)
		if err != nil {
			return err
		}
		if item.LeaseID != "" && s.Orchestrator != nil {
			_ = s.Orchestrator.ReleaseLease(detachedContext(ctx), item.LeaseID, item.AttemptID.String()+":reconcile-release")
		}
		if item.windowID != "" {
			_ = s.markBillingWindow(ctx, item.AttemptID, item.windowID, "voided", 0)
		}
		if err := s.failAttempt(ctx, item.executionWorkItem, "reconciled_launch_timeout", nil); err != nil {
			return fmt.Errorf("fail stale launching attempt %s: %w", item.AttemptID, err)
		}
	}
	return rows.Err()
}

type reconcileWorkItem struct {
	executionWorkItem
	windowID string
}

func (s *Service) scanReconcileWorkItem(ctx context.Context, scanner interface {
	Scan(dest ...any) error
}) (reconcileWorkItem, error) {
	var (
		executionID uuid.UUID
		attemptID   uuid.UUID
		windowID    string
	)
	if err := scanner.Scan(&executionID, &attemptID, &windowID); err != nil {
		return reconcileWorkItem{}, fmt.Errorf("scan reconcile candidate: %w", err)
	}
	item, err := s.loadWorkItem(ctx, executionID, attemptID)
	if err != nil {
		return reconcileWorkItem{}, err
	}
	return reconcileWorkItem{executionWorkItem: item, windowID: windowID}, nil
}
