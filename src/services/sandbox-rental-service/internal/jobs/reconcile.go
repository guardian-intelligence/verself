package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/verself/domain-transfer-objects"
	"github.com/verself/sandbox-rental-service/internal/store"
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
	if err := s.reconcileCleanedRunnerAttempts(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Service) reconcileReservedAttempts(ctx context.Context) error {
	rows, err := s.storeQueries().ListStaleReservedAttempts(ctx, store.ListStaleReservedAttemptsParams{
		State:        StateReserved,
		StaleSeconds: int32(reconcileStaleAfter.Seconds()),
	})
	if err != nil {
		return fmt.Errorf("query stale reserved attempts: %w", err)
	}

	for _, row := range rows {
		item, err := s.loadReconcileWorkItem(ctx, row.ExecutionID, row.AttemptID, row.BillingWindowID)
		if err != nil {
			return err
		}
		if item.windowID != "" {
			_ = s.markBillingWindow(ctx, item.AttemptID, item.windowID, "voided", 0, dto.BillingSettleResult{})
		}
		if err := s.failAttempt(ctx, item.executionWorkItem, "reconciled_reserved_timeout", nil); err != nil {
			return fmt.Errorf("fail stale reserved attempt %s: %w", item.AttemptID, err)
		}
	}
	return nil
}

func (s *Service) reconcileLaunchingAttempts(ctx context.Context) error {
	rows, err := s.storeQueries().ListStaleLaunchingAttempts(ctx, store.ListStaleLaunchingAttemptsParams{
		State:        StateLaunching,
		StaleSeconds: int32((5 * time.Minute).Seconds()),
	})
	if err != nil {
		return fmt.Errorf("query stale launching attempts: %w", err)
	}

	for _, row := range rows {
		item, err := s.loadReconcileWorkItem(ctx, row.ExecutionID, row.AttemptID, row.BillingWindowID)
		if err != nil {
			return err
		}
		if item.LeaseID != "" && s.Orchestrator != nil {
			_ = s.Orchestrator.ReleaseLease(detachedContext(ctx), item.LeaseID, item.AttemptID.String()+":reconcile-release")
		}
		if item.windowID != "" {
			_ = s.markBillingWindow(ctx, item.AttemptID, item.windowID, "voided", 0, dto.BillingSettleResult{})
		}
		if err := s.failAttempt(ctx, item.executionWorkItem, "reconciled_launch_timeout", nil); err != nil {
			return fmt.Errorf("fail stale launching attempt %s: %w", item.AttemptID, err)
		}
	}
	return nil
}

func (s *Service) reconcileCleanedRunnerAttempts(ctx context.Context) error {
	rows, err := s.storeQueries().ListCleanedRunnerAttempts(ctx, store.ListCleanedRunnerAttemptsParams{
		WorkloadKind: WorkloadKindRunner,
		State:        StateRunning,
		StaleSeconds: int32((2 * time.Minute).Seconds()),
	})
	if err != nil {
		return fmt.Errorf("query cleaned runner attempts: %w", err)
	}

	for _, row := range rows {
		item, err := s.loadReconcileWorkItem(ctx, row.ExecutionID, row.AttemptID, row.BillingWindowID)
		if err != nil {
			return err
		}
		if item.LeaseID != "" && s.Orchestrator != nil {
			_ = s.Orchestrator.ReleaseLease(detachedContext(ctx), item.LeaseID, item.AttemptID.String()+":reconcile-cleaned-release")
		}
		if item.windowID != "" {
			_ = s.markBillingWindow(ctx, item.AttemptID, item.windowID, "voided", 0, dto.BillingSettleResult{})
		}
		if err := s.failAttempt(ctx, item.executionWorkItem, "reconciled_cleaned_runner", nil); err != nil {
			return fmt.Errorf("fail cleaned runner attempt %s: %w", item.AttemptID, err)
		}
	}
	return nil
}

type reconcileWorkItem struct {
	executionWorkItem
	windowID string
}

func (s *Service) loadReconcileWorkItem(ctx context.Context, executionID, attemptID uuid.UUID, windowID string) (reconcileWorkItem, error) {
	item, err := s.loadWorkItem(ctx, executionID, attemptID)
	if err != nil {
		return reconcileWorkItem{}, err
	}
	return reconcileWorkItem{executionWorkItem: item, windowID: windowID}, nil
}
