package jobs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	billingclient "github.com/verself/billing-service/client"
	"github.com/verself/sandbox-rental-service/internal/scheduler"
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
	if err := s.reconcileGoldenVMCreateOperations(ctx); err != nil {
		return err
	}
	if err := s.observeDurableStorage(ctx); err != nil {
		return err
	}
	if err := s.reapGoldenVMSnapshots(ctx); err != nil {
		return err
	}
	if err := s.reapDurableGenerations(ctx); err != nil {
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
	return nil
}

func (s *Service) reconcileGoldenVMCreateOperations(ctx context.Context) error {
	if s.Scheduler == nil {
		return nil
	}
	rows, err := s.storeQueries().ListGoldenVMCreateOperationsForReconcile(ctx, store.ListGoldenVMCreateOperationsForReconcileParams{
		StaleSeconds: int32(reconcileStaleAfter.Seconds()),
		LimitCount:   50,
	})
	if err != nil {
		return fmt.Errorf("query stale golden VM create operations: %w", err)
	}
	for _, operationID := range rows {
		op, err := s.storeQueries().GetGoldenVMOperationForCreate(ctx, store.GetGoldenVMOperationForCreateParams{OperationID: operationID})
		if err != nil {
			return fmt.Errorf("load stale golden VM operation %s: %w", operationID, err)
		}
		if goldenVMOperationTerminal(op.State) {
			continue
		}
		if strings.TrimSpace(op.LeaseID) == "" {
			_ = s.storeQueries().MarkGoldenVMOperationFailed(ctx, store.MarkGoldenVMOperationFailedParams{
				RecordedAt:    pgTime(time.Now().UTC()),
				FailureReason: "reconciled_missing_lease",
				OperationID:   op.OperationID,
			})
			continue
		}
		if _, err := s.Scheduler.EnqueueGoldenVMCreate(ctx, scheduler.GoldenVMCreateRequest{
			OperationID:   op.OperationID.String(),
			ExecutionID:   op.ExecutionID.String(),
			AttemptID:     op.AttemptID.String(),
			LeaseID:       op.LeaseID,
			ExecID:        op.ExecID,
			CorrelationID: CorrelationIDFromContext(ctx),
			TraceParent:   traceParent(ctx),
		}); err != nil {
			return fmt.Errorf("enqueue stale golden VM create %s: %w", op.OperationID, err)
		}
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
			if (item.AttemptState == StateRunning || item.AttemptState == StateFinalizing) && s.Scheduler != nil {
				if err := s.enqueueExecutionMonitor(ctx, item.executionWorkItem, 0); err != nil {
					return fmt.Errorf("enqueue stale execution monitor %s: %w", item.AttemptID, err)
				}
			}
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
// deadline has elapsed. The deadline columns are populated when the provider
// integration submits the runner job; a row stuck past its deadline means the
// next transition did not arrive, the catalog resolver could not reach the
// upstream, or the guest VM never registered. Failing the allocation also
// enqueues runner cleanup so the lease is released.
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
