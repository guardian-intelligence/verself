package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	billingclient "github.com/verself/billing-service/client"
	"github.com/verself/sandbox-rental-service/internal/store"
	vmorchestrator "github.com/verself/vm-orchestrator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	if err := s.reconcileLeasedAttempts(ctx); err != nil {
		return err
	}
	if err := s.reconcileTerminalDurableOperations(ctx); err != nil {
		return err
	}
	if err := s.observeDurableStorage(ctx); err != nil {
		return err
	}
	if err := s.pruneGoldenVMSnapshots(ctx); err != nil {
		return err
	}
	if err := s.pruneDurableGenerations(ctx); err != nil {
		return err
	}
	if err := s.reconcileTerminalRunnerAllocations(ctx); err != nil {
		return err
	}
	if err := s.reconcileCleanedRunnerAttempts(ctx); err != nil {
		return err
	}
	if err := s.reconcileExpiredRunnerAllocations(ctx); err != nil {
		return err
	}
	if err := s.reconcileQueuedRunnerJobs(ctx); err != nil {
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
			_ = s.markBillingWindow(ctx, item.AttemptID, item.windowID, "voided", 0, billingclient.BillingSettleResult{})
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
		StaleSeconds: int32((leaseReadyTimeout + 10*time.Second).Seconds()),
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
			_ = s.markBillingWindow(ctx, item.AttemptID, item.windowID, "voided", 0, billingclient.BillingSettleResult{})
		}
		if err := s.failAttempt(ctx, item.executionWorkItem, "reconciled_launch_timeout", nil); err != nil {
			return fmt.Errorf("fail stale launching attempt %s: %w", item.AttemptID, err)
		}
	}
	return nil
}

func (s *Service) reconcileLeasedAttempts(ctx context.Context) error {
	if s.Orchestrator == nil {
		return nil
	}
	rows, err := s.storeQueries().ListLeasedAttemptsForReconcile(ctx, store.ListLeasedAttemptsForReconcileParams{
		StaleSeconds: int32((30 * time.Second).Seconds()),
	})
	if err != nil {
		return fmt.Errorf("query leased attempts for reconcile: %w", err)
	}
	for _, row := range rows {
		item, err := s.loadReconcileWorkItem(ctx, row.ExecutionID, row.AttemptID, row.BillingWindowID)
		if err != nil {
			return err
		}
		lease, err := s.Orchestrator.GetLease(ctx, item.LeaseID)
		if err != nil {
			if !orchestratorLeaseMissing(err) {
				return fmt.Errorf("get lease %s for attempt %s: %w", item.LeaseID, item.AttemptID, err)
			}
			if item.windowID != "" {
				_ = s.markBillingWindow(ctx, item.AttemptID, item.windowID, "voided", 0, billingclient.BillingSettleResult{})
			}
			if err := s.failAttempt(ctx, item.executionWorkItem, "reconciled_missing_lease", err); err != nil {
				return fmt.Errorf("fail missing-lease attempt %s: %w", item.AttemptID, err)
			}
			continue
		}
		if !lease.State.Terminal() {
			continue
		}
		if item.windowID != "" {
			_ = s.markBillingWindow(ctx, item.AttemptID, item.windowID, "voided", 0, billingclient.BillingSettleResult{})
		}
		if err := s.failAttempt(ctx, item.executionWorkItem, "reconciled_terminal_lease_"+leaseStateName(lease.State), nil); err != nil {
			return fmt.Errorf("fail terminal-lease attempt %s: %w", item.AttemptID, err)
		}
	}
	return nil
}

func (s *Service) reconcileTerminalDurableOperations(ctx context.Context) error {
	rows, err := s.storeQueries().ListTerminalAttemptsWithOpenDurableOperations(ctx)
	if err != nil {
		return fmt.Errorf("query terminal attempts with open durable operations: %w", err)
	}
	for _, row := range rows {
		item, err := s.loadWorkItem(ctx, row.ExecutionID, row.AttemptID)
		if err != nil {
			return err
		}
		reason := "reconciled_terminal_attempt_without_durable_result:" + row.AttemptState
		if row.AttemptFailureReason != "" {
			reason += ":" + row.AttemptFailureReason
		}
		s.failOpenDurableOperationsForAttempt(ctx, item, reason, nil)
	}
	return nil
}

func (s *Service) reconcileTerminalRunnerAllocations(ctx context.Context) error {
	rows, err := s.storeQueries().ListTerminalRunnerExecutionsWithLiveAllocations(ctx)
	if err != nil {
		return fmt.Errorf("query terminal runner executions with live allocations: %w", err)
	}
	for _, executionID := range rows {
		s.MarkRunnerExecutionExited(ctx, executionID)
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
			_ = s.markBillingWindow(ctx, item.AttemptID, item.windowID, "voided", 0, billingclient.BillingSettleResult{})
		}
		if err := s.failAttempt(ctx, item.executionWorkItem, "reconciled_cleaned_runner", nil); err != nil {
			return fmt.Errorf("fail cleaned runner attempt %s: %w", item.AttemptID, err)
		}
	}
	return nil
}

// reconcileExpiredRunnerAllocations fails allocations whose current-state
// deadline has elapsed. The deadline columns are populated when the allocation
// is created (github_runner.go:660-666); a row stuck past its deadline means
// the worker that should have driven the next transition died, the catalog
// resolver couldn't reach the upstream, or the guest VM never registered.
// Failing the allocation also enqueues runner cleanup so the GitHub-side JIT
// runner registration is removed and the lease is released.
func (s *Service) reconcileExpiredRunnerAllocations(ctx context.Context) error {
	rows, err := s.storeQueries().ListExpiredRunnerAllocations(ctx)
	if err != nil {
		return fmt.Errorf("query expired runner allocations: %w", err)
	}
	for _, row := range rows {
		reason := failureReasonForExpiredAllocation(row.State)
		if err := s.storeQueries().SetRunnerAllocationState(ctx, store.SetRunnerAllocationStateParams{
			State:         "failed",
			FailureReason: reason,
			UpdatedAt:     pgTime(time.Now().UTC()),
			Provider:      row.Provider,
			AllocationID:  row.AllocationID,
		}); err != nil {
			return fmt.Errorf("fail expired runner allocation %s: %w", row.AllocationID, err)
		}
		if s.Scheduler != nil {
			if _, err := s.Scheduler.EnqueueRunnerCleanup(ctx, schedulerCleanupRequest(ctx, row.AllocationID)); err != nil {
				s.Logger.WarnContext(ctx, "enqueue runner cleanup after deadline expiry",
					"allocation_id", row.AllocationID.String(), "error", err)
			}
		}
		s.Logger.WarnContext(ctx, "runner allocation deadline expired",
			"allocation_id", row.AllocationID.String(),
			"provider", row.Provider,
			"prior_state", row.State,
			"failure_reason", reason,
		)
	}
	return nil
}

func failureReasonForExpiredAllocation(state string) string {
	switch state {
	case "pending":
		return "allocate_deadline_exceeded"
	case "jit_creating", "bootstrap_creating":
		return "jit_creation_deadline_exceeded"
	case "jit_created", "bootstrap_created":
		return "vm_submission_deadline_exceeded"
	case "vm_submitted":
		return "runner_listening_deadline_exceeded"
	case "runner_config_fetched":
		return "assignment_deadline_exceeded"
	case "assigned":
		return "vm_exit_deadline_exceeded"
	case "vm_exited":
		return "cleanup_deadline_exceeded"
	default:
		return "deadline_exceeded"
	}
}

func (s *Service) reconcileQueuedRunnerJobs(ctx context.Context) error {
	rows, err := s.storeQueries().ListQueuedRunnerJobsWithoutActiveAllocation(ctx)
	if err != nil {
		return fmt.Errorf("query queued runner jobs without active allocation: %w", err)
	}
	for _, row := range rows {
		if err := s.ReconcileRunnerCapacity(ctx, row.Provider, row.ProviderJobID); err != nil {
			return fmt.Errorf("reconcile queued runner job %s/%d: %w", row.Provider, row.ProviderJobID, err)
		}
		if s.Logger != nil {
			s.Logger.InfoContext(ctx, "reconciled queued runner job without active allocation",
				"provider", row.Provider,
				"provider_job_id", row.ProviderJobID,
			)
		}
	}
	return nil
}

func orchestratorLeaseMissing(err error) bool {
	if err == nil {
		return false
	}
	return status.Code(err) == codes.NotFound
}

func leaseStateName(state vmorchestrator.LeaseState) string {
	switch state {
	case vmorchestrator.LeaseStateAcquiring:
		return "acquiring"
	case vmorchestrator.LeaseStateReady:
		return "ready"
	case vmorchestrator.LeaseStateDraining:
		return "draining"
	case vmorchestrator.LeaseStateReleased:
		return "released"
	case vmorchestrator.LeaseStateExpired:
		return "expired"
	case vmorchestrator.LeaseStateCrashed:
		return "crashed"
	default:
		return "unspecified"
	}
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
