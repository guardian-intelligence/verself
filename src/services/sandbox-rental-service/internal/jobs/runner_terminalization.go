package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	billingclient "github.com/verself/billing-service/client"
	"github.com/verself/sandbox-rental-service/internal/store"
	"go.opentelemetry.io/otel/trace"
)

type runnerAllocationRef struct {
	Provider     string
	AllocationID uuid.UUID
	ExecutionID  uuid.UUID
	AttemptID    uuid.UUID
	RunnerName   string
}

type runnerAllocationFailure struct {
	Problems       runnerProblemSet
	EnqueueCleanup bool
	FailAttempt    bool
	Cause          error
}

func (s *Service) terminalizeRunnerAllocation(ctx context.Context, ref runnerAllocationRef, failure runnerAllocationFailure) error {
	if s == nil || s.PGX == nil {
		return ErrRunnerUnavailable
	}
	if ref.AllocationID == uuid.Nil {
		return fmt.Errorf("%w: allocation_id is required", ErrRunnerUnavailable)
	}
	if failure.Problems.empty() {
		failure.Problems.add(runnerAllocationProblem("runner_allocation", "runner_allocation.failed", "Runner allocation failed", "", false))
	}

	now := time.Now().UTC()
	tx, err := s.PGX.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	qtx := store.New(tx)
	rows, err := qtx.FailRunnerAllocation(ctx, store.FailRunnerAllocationParams{
		UpdatedAt:    pgTime(now),
		Provider:     ref.Provider,
		AllocationID: ref.AllocationID,
	})
	if err != nil {
		return err
	}
	allocationFailed := rows > 0
	if !allocationFailed && !failure.FailAttempt {
		return nil
	}
	if allocationFailed {
		if err := appendRunnerAllocationProblems(ctx, qtx, ref.AllocationID, failure.Problems); err != nil {
			return err
		}
	}
	attemptFailed := false
	if failure.FailAttempt && ref.ExecutionID != uuid.Nil && ref.AttemptID != uuid.Nil {
		attempt, err := qtx.GetExecutionWorkItem(ctx, store.GetExecutionWorkItemParams{
			ExecutionID: ref.ExecutionID,
			AttemptID:   ref.AttemptID,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil && !runnerAttemptTerminal(attempt.AttemptState) {
			traceID := trace.SpanContextFromContext(ctx).TraceID().String()
			if err := qtx.SetExecutionState(ctx, store.SetExecutionStateParams{
				State:       StateFailed,
				UpdatedAt:   pgTime(now),
				ExecutionID: ref.ExecutionID,
			}); err != nil {
				return err
			}
			if err := qtx.MarkAttemptFailed(ctx, store.MarkAttemptFailedParams{
				State:         StateFailed,
				FailureReason: failure.Problems.reason(),
				TraceID:       traceID,
				CompletedAt:   pgTime(now),
				AttemptID:     ref.AttemptID,
			}); err != nil {
				return err
			}
			if err := qtx.InsertExecutionEvent(ctx, store.InsertExecutionEventParams{
				ExecutionID: ref.ExecutionID,
				AttemptID:   ref.AttemptID,
				FromState:   attempt.AttemptState,
				ToState:     StateFailed,
				Reason:      failure.Problems.reason(),
				CreatedAt:   pgTime(now),
			}); err != nil {
				return err
			}
			attemptFailed = true
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	if attemptFailed {
		if item, err := s.loadWorkItem(ctx, ref.ExecutionID, ref.AttemptID); err == nil {
			s.cleanupTerminalizedRunnerAttempt(detachedContext(ctx), item, failure.Problems.reason())
			s.failOpenDurableOperationsForAttempt(detachedContext(ctx), item, failure.Problems.reason(), failure.Cause)
			s.MarkRunnerExecutionExited(detachedContext(ctx), item.ExecutionID)
		} else if s.Logger != nil {
			s.Logger.WarnContext(ctx, "load terminalized runner attempt for cleanup failed",
				"execution_id", ref.ExecutionID.String(),
				"attempt_id", ref.AttemptID.String(),
				"error", err)
		}
	}
	if failure.EnqueueCleanup && allocationFailed && s.Scheduler != nil {
		if _, err := s.Scheduler.EnqueueRunnerCleanup(ctx, schedulerCleanupRequest(ctx, ref.AllocationID)); err != nil && s.Logger != nil {
			s.Logger.WarnContext(ctx, "enqueue runner cleanup after allocation terminalization",
				"allocation_id", ref.AllocationID.String(), "error", err)
		}
	}
	if s.Logger != nil {
		s.Logger.WarnContext(ctx, "runner allocation terminalized",
			"allocation_id", ref.AllocationID.String(),
			"provider", ref.Provider,
			"runner_name", ref.RunnerName,
			"problem", failure.Problems.reason(),
		)
	}
	return nil
}

func (s *Service) cleanupTerminalizedRunnerAttempt(ctx context.Context, item executionWorkItem, reason string) {
	if item.LeaseID != "" && s.Orchestrator != nil {
		started := time.Now().UTC()
		cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		releaseErr := s.Orchestrator.ReleaseLease(cleanupCtx, item.LeaseID, item.AttemptID.String()+":runner-terminalized-release")
		cancel()
		completed := time.Now().UTC()
		result, detail := sandboxPhaseResult(releaseErr)
		s.recordExecutionPhase(ctx, item, "sandbox-rental", "sandbox.runner.terminalize.release_lease", result, detail, started, completed, sandboxPhaseAttrs{
			"reason": reason,
		})
		if releaseErr != nil && s.Logger != nil {
			s.Logger.WarnContext(ctx, "release terminalized runner lease failed",
				"execution_id", item.ExecutionID.String(),
				"attempt_id", item.AttemptID.String(),
				"lease_id", item.LeaseID,
				"error", releaseErr)
		}
	}
	if s.Billing == nil {
		return
	}
	windows, err := s.listBillingWindows(ctx, item.AttemptID)
	if err != nil {
		if s.Logger != nil {
			s.Logger.WarnContext(ctx, "load terminalized runner billing window failed",
				"execution_id", item.ExecutionID.String(),
				"attempt_id", item.AttemptID.String(),
				"error", err)
		}
		return
	}
	if len(windows) == 0 {
		return
	}
	window := windows[len(windows)-1]
	switch window.State {
	case "voided", "settled":
		return
	}
	reservation := billingReservationFromWindow(item, window)
	started := time.Now().UTC()
	cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = s.voidBillingWindow(cleanupCtx, reservation)
	cancel()
	if err == nil {
		err = s.markBillingWindow(ctx, item.AttemptID, reservation.WindowID, "voided", 0, billingclient.BillingSettleResult{})
	}
	completed := time.Now().UTC()
	result, detail := sandboxPhaseResult(err)
	s.recordExecutionPhase(ctx, item, "sandbox-rental", "sandbox.runner.terminalize.void_billing", result, detail, started, completed, sandboxPhaseAttrs{
		"billing_window_id": reservation.WindowID,
		"reason":            reason,
	})
	if err != nil && s.Logger != nil {
		s.Logger.WarnContext(ctx, "void terminalized runner billing window failed",
			"execution_id", item.ExecutionID.String(),
			"attempt_id", item.AttemptID.String(),
			"billing_window_id", reservation.WindowID,
			"error", err)
	}
}

func runnerAttemptTerminal(state string) bool {
	switch state {
	case StateSucceeded, StateFailed, StateCanceled, StateLost:
		return true
	default:
		return false
	}
}
