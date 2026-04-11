// Package jobs implements the execution control plane for sandbox workloads:
// durable execution identity, attempt lifecycle, billing-window recording, and
// ClickHouse summary writes.
package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	billingclient "github.com/forge-metal/billing-service/client"
	vmorchestrator "github.com/forge-metal/vm-orchestrator"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("sandbox-rental-service")

const (
	KindDirect        = "direct"
	KindRepoExec      = "repo_exec"
	KindWarmGolden    = "warm_golden"
	KindForgejoRunner = "forgejo_runner"

	StateQueued     = "queued"
	StateReserved   = "reserved"
	StateLaunching  = "launching"
	StateRunning    = "running"
	StateFinalizing = "finalizing"
	StateSucceeded  = "succeeded"
	StateFailed     = "failed"
	StateCanceled   = "canceled"
	StateLost       = "lost"

	defaultProductID     = "sandbox"
	defaultBranchName    = "main"
	defaultRunCommand    = "echo hello"
	defaultLogStream     = "stdout"
	executionSourceType  = "execution_attempt"
	runnerJobTimeoutSecs = 7200
)

var (
	ErrQuotaExceeded      = errors.New("sandbox-rental: quota exceeded")
	ErrExecutionMissing   = errors.New("sandbox-rental: execution not found")
	ErrRepoNotReady       = errors.New("sandbox-rental: repo not ready")
	ErrRunnerUnavailable  = errors.New("sandbox-rental: runner unavailable")
	ErrBillingUnavailable = errors.New("sandbox-rental: billing unavailable")
)

// Runner abstracts VM execution for direct sandbox jobs and repo-bound
// workloads. Production uses *vmorchestrator.Client; tests use a fake.
type Runner interface {
	Run(ctx context.Context, job vmorchestrator.JobConfig) (vmorchestrator.JobResult, error)
	RunWithConfig(ctx context.Context, cfg vmorchestrator.Config, job vmorchestrator.JobConfig) (vmorchestrator.JobResult, error)
	ExecRepo(ctx context.Context, req vmorchestrator.RepoExecRequest) (vmorchestrator.JobStatus, error)
	WarmGolden(ctx context.Context, req vmorchestrator.WarmGoldenRequest) (vmorchestrator.WarmGoldenResult, error)
}

type BillingClient interface {
	Reserve(
		ctx context.Context,
		jobID int64,
		orgID uint64,
		productID string,
		actorID string,
		concurrentCount uint64,
		sourceType string,
		sourceRef string,
		allocation map[string]float64,
		reqEditors ...billingclient.RequestEditorFn,
	) (billingclient.Reservation, error)
	Settle(ctx context.Context, reservation billingclient.Reservation, actualSeconds uint32, reqEditors ...billingclient.RequestEditorFn) error
	Void(ctx context.Context, reservation billingclient.Reservation, reqEditors ...billingclient.RequestEditorFn) error
}

type SubmitRequest struct {
	Kind            string `json:"kind"`
	ProductID       string `json:"product_id,omitempty"`
	Provider        string `json:"provider,omitempty"`
	IdempotencyKey  string `json:"idempotency_key,omitempty"`
	RepoID          string `json:"repo_id,omitempty"`
	Repo            string `json:"repo,omitempty"`
	RepoURL         string `json:"repo_url,omitempty"`
	Ref             string `json:"ref,omitempty"`
	DefaultBranch   string `json:"default_branch,omitempty"`
	RunCommand      string `json:"run_command,omitempty"`
	WorkflowPath    string `json:"workflow_path,omitempty"`
	WorkflowJobName string `json:"workflow_job_name,omitempty"`
	ProviderRunID   string `json:"provider_run_id,omitempty"`
	ProviderJobID   string `json:"provider_job_id,omitempty"`

	GoldenGenerationID *uuid.UUID `json:"-"`
	GoldenSnapshotRef  string     `json:"-"`
}

type ExecutionRecord struct {
	ExecutionID        uuid.UUID       `json:"execution_id"`
	OrgID              uint64          `json:"org_id"`
	ActorID            string          `json:"actor_id"`
	Kind               string          `json:"kind"`
	Provider           string          `json:"provider,omitempty"`
	ProductID          string          `json:"product_id"`
	Status             string          `json:"status"`
	CorrelationID      string          `json:"correlation_id,omitempty"`
	IdempotencyKey     string          `json:"idempotency_key,omitempty"`
	RepoID             string          `json:"repo_id,omitempty"`
	GoldenGenerationID string          `json:"golden_generation_id,omitempty"`
	Repo               string          `json:"repo,omitempty"`
	RepoURL            string          `json:"repo_url,omitempty"`
	Ref                string          `json:"ref,omitempty"`
	DefaultBranch      string          `json:"default_branch,omitempty"`
	RunCommand         string          `json:"run_command,omitempty"`
	CommitSHA          string          `json:"commit_sha,omitempty"`
	WorkflowPath       string          `json:"workflow_path,omitempty"`
	WorkflowJobName    string          `json:"workflow_job_name,omitempty"`
	ProviderRunID      string          `json:"provider_run_id,omitempty"`
	ProviderJobID      string          `json:"provider_job_id,omitempty"`
	LatestAttempt      AttemptRecord   `json:"latest_attempt"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	BillingWindows     []BillingWindow `json:"billing_windows,omitempty"`
}

type AttemptRecord struct {
	AttemptID         uuid.UUID  `json:"attempt_id"`
	AttemptSeq        int        `json:"attempt_seq"`
	State             string     `json:"state"`
	OrchestratorJobID string     `json:"orchestrator_job_id,omitempty"`
	BillingJobID      int64      `json:"billing_job_id,omitempty"`
	RunnerName        string     `json:"runner_name,omitempty"`
	GoldenSnapshot    string     `json:"golden_snapshot,omitempty"`
	FailureReason     string     `json:"failure_reason,omitempty"`
	ExitCode          int        `json:"exit_code,omitempty"`
	DurationMs        int64      `json:"duration_ms,omitempty"`
	ZFSWritten        int64      `json:"zfs_written,omitempty"`
	StdoutBytes       int64      `json:"stdout_bytes,omitempty"`
	StderrBytes       int64      `json:"stderr_bytes,omitempty"`
	TraceID           string     `json:"trace_id,omitempty"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type BillingWindow struct {
	AttemptID        uuid.UUID  `json:"attempt_id"`
	BillingWindowID  string     `json:"billing_window_id"`
	WindowSeq        int        `json:"window_seq"`
	ReservationShape string     `json:"reservation_shape"`
	ReservedQuantity int        `json:"reserved_quantity"`
	ActualQuantity   int        `json:"actual_quantity,omitempty"`
	PricingPhase     string     `json:"pricing_phase,omitempty"`
	State            string     `json:"state"`
	WindowStart      time.Time  `json:"window_start"`
	CreatedAt        time.Time  `json:"created_at"`
	SettledAt        *time.Time `json:"settled_at,omitempty"`
}

type JobLogRow struct {
	ExecutionID string    `ch:"execution_id"`
	AttemptID   string    `ch:"attempt_id"`
	Seq         uint32    `ch:"seq"`
	Stream      string    `ch:"stream"`
	Chunk       string    `ch:"chunk"`
	CreatedAt   time.Time `ch:"created_at"`
}

type JobEventRow struct {
	ExecutionID        string    `ch:"execution_id"`
	AttemptID          string    `ch:"attempt_id"`
	OrgID              uint64    `ch:"org_id"`
	ActorID            string    `ch:"actor_id"`
	Kind               string    `ch:"kind"`
	Provider           string    `ch:"provider"`
	ProductID          string    `ch:"product_id"`
	RepoID             string    `ch:"repo_id"`
	GoldenGenerationID string    `ch:"golden_generation_id"`
	Repo               string    `ch:"repo"`
	RepoURL            string    `ch:"repo_url"`
	Ref                string    `ch:"ref"`
	DefaultBranch      string    `ch:"default_branch"`
	RunCommand         string    `ch:"run_command"`
	CommitSHA          string    `ch:"commit_sha"`
	WorkflowPath       string    `ch:"workflow_path"`
	WorkflowJobName    string    `ch:"workflow_job_name"`
	ProviderRunID      string    `ch:"provider_run_id"`
	ProviderJobID      string    `ch:"provider_job_id"`
	RunnerName         string    `ch:"runner_name"`
	Status             string    `ch:"status"`
	ExitCode           int32     `ch:"exit_code"`
	DurationMs         int64     `ch:"duration_ms"`
	ZFSWritten         uint64    `ch:"zfs_written"`
	StdoutBytes        uint64    `ch:"stdout_bytes"`
	StderrBytes        uint64    `ch:"stderr_bytes"`
	GoldenSnapshot     string    `ch:"golden_snapshot"`
	BillingJobID       int64     `ch:"billing_job_id"`
	ChargeUnits        uint64    `ch:"charge_units"`
	PricingPhase       string    `ch:"pricing_phase"`
	CorrelationID      string    `ch:"correlation_id"`
	VerificationRunID  string    `ch:"verification_run_id"`
	StartedAt          time.Time `ch:"started_at"`
	CompletedAt        time.Time `ch:"completed_at"`
	CreatedAt          time.Time `ch:"created_at"`
	TraceID            string    `ch:"trace_id"`
}

// Service manages execution submission, billing, and state transitions.
type Service struct {
	PG                        *sql.DB
	CH                        driver.Conn
	CHDatabase                string
	Orchestrator              Runner
	Billing                   BillingClient
	BillingVCPUs              int
	BillingMemMiB             int
	ForgejoURL                string
	ForgejoRunnerLabel        string
	ForgejoRunnerToken        string
	ForgejoRunnerBinaryURL    string
	ForgejoRunnerBinarySHA256 string
	WebhookSecretCodec        *SecretCodec
	Logger                    *slog.Logger
}

type executionSnapshot struct {
	ExecutionID     uuid.UUID
	LatestAttemptID uuid.UUID
	Status          string
}

type executionOutcome struct {
	State          string
	FailureReason  string
	ExitCode       int
	DurationMs     int64
	ZFSWritten     uint64
	StdoutBytes    uint64
	StderrBytes    uint64
	Logs           string
	CommitSHA      string
	RunnerName     string
	GoldenSnapshot string
	StartedAt      time.Time
	CompletedAt    time.Time
}

type workloadResult struct {
	err     error
	outcome executionOutcome
}

// Submit creates a durable execution and first attempt, reserves billing, and
// starts asynchronous execution. It returns the execution and attempt IDs
// immediately; callers poll for completion.
func (s *Service) Submit(ctx context.Context, orgID uint64, actorID string, req SubmitRequest) (uuid.UUID, uuid.UUID, error) {
	req, err := normalizeSubmitRequest(req)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if s.Orchestrator == nil {
		return uuid.Nil, uuid.Nil, ErrRunnerUnavailable
	}
	if s.Billing == nil {
		return uuid.Nil, uuid.Nil, ErrBillingUnavailable
	}
	req, err = s.hydrateImportedRepoRequest(ctx, orgID, req)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	if req.IdempotencyKey != "" {
		if snapshot, ok, err := s.findByIdempotencyKey(ctx, orgID, req.IdempotencyKey); err != nil {
			return uuid.Nil, uuid.Nil, err
		} else if ok {
			return snapshot.ExecutionID, snapshot.LatestAttemptID, nil
		}
	}

	executionID := uuid.New()
	attemptID := uuid.New()
	correlationID := strings.TrimSpace(CorrelationIDFromContext(ctx))
	traceID := traceIDFromContext(ctx)
	now := time.Now().UTC()

	ctx, span := tracer.Start(ctx, "execution.Submit",
		trace.WithAttributes(
			attribute.String("execution.id", executionID.String()),
			attribute.String("attempt.id", attemptID.String()),
			attribute.Int64("execution.org_id", int64(orgID)),
			attribute.String("execution.kind", req.Kind),
			attribute.String("execution.repo", req.Repo),
		))
	defer span.End()

	if err := s.insertQueuedExecution(ctx, executionID, attemptID, orgID, actorID, req, traceID, correlationID, now); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return uuid.Nil, uuid.Nil, fmt.Errorf("insert queued execution: %w", err)
	}

	billingJobID, err := s.nextBillingJobID(ctx)
	if err != nil {
		_ = s.failWithoutBilling(ctx, executionID, attemptID, "billing_job_id_unavailable", now)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return uuid.Nil, uuid.Nil, fmt.Errorf("allocate billing job id: %w", err)
	}

	currentConcurrent, err := s.countActiveAttempts(ctx, orgID)
	if err != nil {
		_ = s.failWithoutBilling(ctx, executionID, attemptID, "count_active_attempts_failed", now)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return uuid.Nil, uuid.Nil, fmt.Errorf("count active attempts: %w", err)
	}

	allocation := map[string]float64{
		"vcpu": float64(s.BillingVCPUs),
		"gib":  float64(s.BillingMemMiB) / 1024.0,
	}
	reservation, err := s.Billing.Reserve(
		ctx,
		billingJobID,
		orgID,
		req.ProductID,
		actorID,
		uint64(currentConcurrent+1),
		executionSourceType,
		attemptID.String(),
		allocation,
	)
	if err != nil {
		_ = s.failWithoutBilling(ctx, executionID, attemptID, reserveFailureReason(err), now)
		if errors.Is(err, billingclient.ErrForbidden) {
			return uuid.Nil, uuid.Nil, ErrQuotaExceeded
		}
		return uuid.Nil, uuid.Nil, fmt.Errorf("billing reserve: %w", err)
	}

	if err := s.markReserved(ctx, executionID, attemptID, billingJobID, reservation, traceID, now); err != nil {
		if voidErr := s.Billing.Void(ctx, reservation); voidErr != nil {
			s.Logger.ErrorContext(ctx, "billing void after reserve persistence failure", "attempt_id", attemptID, "error", voidErr)
		}
		_ = s.failWithoutBilling(ctx, executionID, attemptID, "reserve_persist_failed", now)
		return uuid.Nil, uuid.Nil, fmt.Errorf("persist reservation: %w", err)
	}

	s.Logger.InfoContext(ctx, "execution reserved", "execution_id", executionID, "attempt_id", attemptID, "billing_job_id", billingJobID, "org_id", orgID, "kind", req.Kind, "window_seq", reservation.WindowSeq, "fm_correlation_id", correlationID)
	s.writeSystemLog(ctx, executionID, attemptID,
		"reserved billing window seq=%d seconds=%d pricing_phase=%s kind=%s",
		reservation.WindowSeq,
		reservation.WindowSecs,
		reservation.PricingPhase,
		req.Kind,
	)
	if verificationRunID := VerificationRunIDFromContext(ctx); verificationRunID != "" {
		s.Logger.InfoContext(ctx, "execution verification correlation",
			"verification_run_id", verificationRunID,
			"execution_id", executionID,
			"attempt_id", attemptID,
			"org_id", orgID,
			"kind", req.Kind,
		)
	}

	execCtx := context.WithoutCancel(ctx)
	go s.execute(execCtx, executionID, attemptID, orgID, actorID, req, reservation)

	return executionID, attemptID, nil
}

func (s *Service) execute(ctx context.Context, executionID, attemptID uuid.UUID, orgID uint64, actorID string, req SubmitRequest, reservation billingclient.Reservation) {
	ctx, span := tracer.Start(ctx, "execution.Attempt",
		trace.WithAttributes(
			attribute.String("execution.id", executionID.String()),
			attribute.String("attempt.id", attemptID.String()),
			attribute.String("execution.kind", req.Kind),
		))
	defer span.End()

	traceID := traceIDFromContext(ctx)
	orchestratorJobID := attemptID.String()
	if err := s.markLaunching(ctx, executionID, attemptID, orchestratorJobID, traceID, time.Now().UTC()); err != nil {
		s.Logger.ErrorContext(ctx, "mark launching", "execution_id", executionID, "attempt_id", attemptID, "error", err)
		return
	}
	s.writeSystemLog(ctx, executionID, attemptID, "launching workload kind=%s orchestrator_job_id=%s", req.Kind, orchestratorJobID)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	resultCh := make(chan workloadResult, 1)
	go func() {
		outcome, err := s.runAttemptWorkload(runCtx, executionID, attemptID, req)
		resultCh <- workloadResult{outcome: outcome, err: err}
	}()

	currentReservation := reservation
	totalChargeUnits := uint64(0)
	forcedFailureReason := ""
	skipFinalBilling := false
	windowAdvanceUnresolved := false
	nextWindowReserveAt := reservation.RenewBy

	for {
		var (
			windowAdvanceTimer  *time.Timer
			windowAdvanceTimerC <-chan time.Time
		)
		if !skipFinalBilling && !windowAdvanceUnresolved && !nextWindowReserveAt.IsZero() {
			timerDelay := time.Until(nextWindowReserveAt)
			if timerDelay < 0 {
				timerDelay = 0
			}
			windowAdvanceTimer = time.NewTimer(timerDelay)
			windowAdvanceTimerC = windowAdvanceTimer.C
		}

		select {
		case result := <-resultCh:
			stopTimer(windowAdvanceTimer)
			outcome := result.outcome
			err := result.err
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				if outcome.StartedAt.IsZero() && !skipFinalBilling && !windowAdvanceUnresolved {
					if voidErr := s.Billing.Void(ctx, currentReservation); voidErr != nil {
						s.Logger.ErrorContext(ctx, "void launch failure reservation", "attempt_id", attemptID, "error", voidErr)
						s.writeSystemLog(ctx, executionID, attemptID, "billing void failed after launch failure: %v", voidErr)
						_ = s.markFinalizing(ctx, executionID, attemptID, executionOutcome{
							State:         StateFinalizing,
							FailureReason: "billing_void_failed",
							CompletedAt:   time.Now().UTC(),
						})
						return
					}
					s.writeSystemLog(ctx, executionID, attemptID, "billing voided after launch failure window_seq=%d", currentReservation.WindowSeq)
					if err := s.markWindowVoided(ctx, attemptID, int(currentReservation.WindowSeq), time.Now().UTC()); err != nil {
						s.Logger.ErrorContext(ctx, "mark window voided", "attempt_id", attemptID, "error", err)
					}
					terminalOutcome := outcome
					terminalOutcome.State = StateFailed
					if terminalOutcome.FailureReason == "" {
						terminalOutcome.FailureReason = failureReasonFromError(err)
					}
					terminalOutcome.CompletedAt = time.Now().UTC()
					if termErr := s.markTerminal(ctx, executionID, attemptID, terminalOutcome); termErr != nil {
						s.Logger.ErrorContext(ctx, "mark terminal launch failure", "execution_id", executionID, "attempt_id", attemptID, "error", termErr)
					}
					return
				}
			}

			if forcedFailureReason != "" {
				outcome.State = StateFailed
				outcome.FailureReason = forcedFailureReason
			}
			if outcome.State == "" {
				if outcome.ExitCode == 0 {
					outcome.State = StateSucceeded
				} else {
					outcome.State = StateFailed
				}
			}
			if outcome.CompletedAt.IsZero() {
				outcome.CompletedAt = time.Now().UTC()
			}
			if outcome.StartedAt.IsZero() {
				outcome.StartedAt = outcome.CompletedAt
			}
			if outcome.DurationMs == 0 {
				outcome.DurationMs = outcome.CompletedAt.Sub(outcome.StartedAt).Milliseconds()
			}

			if skipFinalBilling || windowAdvanceUnresolved {
				if windowAdvanceUnresolved {
					s.writeSystemLog(ctx, executionID, attemptID, "execution stopped with unresolved billing window advance window_seq=%d", currentReservation.WindowSeq)
				}
				if err := s.markTerminal(ctx, executionID, attemptID, outcome); err != nil {
					s.Logger.ErrorContext(ctx, "mark terminal without final billing", "execution_id", executionID, "attempt_id", attemptID, "error", err)
					return
				}
				if req.Kind == KindWarmGolden {
					if err := s.finalizeWarmGoldenGeneration(ctx, executionID, attemptID, req, outcome); err != nil {
						s.Logger.ErrorContext(ctx, "finalize warm golden generation", "execution_id", executionID, "attempt_id", attemptID, "repo_id", req.RepoID, "generation_id", req.GoldenGenerationID, "error", err)
					}
				}
				s.recordExecutionCompletion(ctx, executionID, attemptID, orgID, actorID, req, currentReservation, outcome, totalChargeUnits)
				return
			}

			if err := s.markFinalizing(ctx, executionID, attemptID, outcome); err != nil {
				s.Logger.ErrorContext(ctx, "mark finalizing", "execution_id", executionID, "attempt_id", attemptID, "error", err)
				return
			}
			s.writeSystemLog(ctx, executionID, attemptID, "execution completed state=%s exit_code=%d duration_ms=%d", outcome.State, outcome.ExitCode, outcome.DurationMs)

			actualSeconds := actualSecondsForReservation(currentReservation, outcome.CompletedAt)
			if settleErr := s.Billing.Settle(ctx, currentReservation, uint32(actualSeconds)); settleErr != nil {
				s.handleSettleFailure(ctx, executionID, attemptID, orgID, actorID, req, currentReservation, outcome, actualSeconds, settleErr, totalChargeUnits)
				return
			}
			windowChargeUnits := chargeUnits(currentReservation.CostPerSec, actualSeconds)
			totalChargeUnits += windowChargeUnits
			settledAt := time.Now().UTC()
			s.writeSystemLog(ctx, executionID, attemptID, "billing settled window_seq=%d actual_quantity=%d charge_units=%d", currentReservation.WindowSeq, actualSeconds, windowChargeUnits)
			if err := s.markWindowSettled(ctx, attemptID, int(currentReservation.WindowSeq), actualSeconds, currentReservation.PricingPhase, settledAt); err != nil {
				s.Logger.ErrorContext(ctx, "mark window settled", "attempt_id", attemptID, "error", err)
				return
			}

			if err := s.markTerminal(ctx, executionID, attemptID, outcome); err != nil {
				s.Logger.ErrorContext(ctx, "mark terminal", "execution_id", executionID, "attempt_id", attemptID, "error", err)
				s.writeSystemLog(ctx, executionID, attemptID, "terminal persistence deferred after billing settled: %v", err)
				return
			}
			if req.Kind == KindWarmGolden {
				if err := s.finalizeWarmGoldenGeneration(ctx, executionID, attemptID, req, outcome); err != nil {
					s.Logger.ErrorContext(ctx, "finalize warm golden generation", "execution_id", executionID, "attempt_id", attemptID, "repo_id", req.RepoID, "generation_id", req.GoldenGenerationID, "error", err)
				}
			}

			s.recordExecutionCompletion(ctx, executionID, attemptID, orgID, actorID, req, currentReservation, outcome, totalChargeUnits)
			return
		case <-windowAdvanceTimerC:
			if skipFinalBilling || windowAdvanceUnresolved {
				continue
			}
			windowSeconds := actualSecondsForReservation(currentReservation, time.Now().UTC())
			if settleErr := s.Billing.Settle(ctx, currentReservation, uint32(windowSeconds)); settleErr != nil {
				s.Logger.ErrorContext(ctx, "billing window advance settle", "attempt_id", attemptID, "window_seq", currentReservation.WindowSeq, "actual_quantity", windowSeconds, "error", settleErr)
				s.writeSystemLog(ctx, executionID, attemptID, "billing window advance settle failed window_seq=%d actual_quantity=%d error=%v", currentReservation.WindowSeq, windowSeconds, settleErr)
				forcedFailureReason = "billing_window_advance_failed"
				windowAdvanceUnresolved = true
				nextWindowReserveAt = time.Time{}
				cancel()
				continue
			}

			windowChargeUnits := chargeUnits(currentReservation.CostPerSec, windowSeconds)
			totalChargeUnits += windowChargeUnits
			settledAt := time.Now().UTC()
			if err := s.markWindowSettled(ctx, attemptID, int(currentReservation.WindowSeq), windowSeconds, currentReservation.PricingPhase, settledAt); err != nil {
				s.Logger.ErrorContext(ctx, "mark advanced window settled", "attempt_id", attemptID, "window_seq", currentReservation.WindowSeq, "error", err)
				forcedFailureReason = "billing_window_advance_persist_failed"
				skipFinalBilling = true
				nextWindowReserveAt = time.Time{}
				cancel()
				continue
			}

			nextReservation, reserveErr := s.Billing.Reserve(
				ctx,
				0,
				orgID,
				req.ProductID,
				actorID,
				0,
				currentReservation.SourceType,
				currentReservation.SourceRef,
				currentReservation.Allocation,
			)
			if reserveErr != nil {
				forcedFailureReason = windowAdvanceFailureReason(reserveErr)
				if !errors.Is(reserveErr, billingclient.ErrPaymentRequired) && !errors.Is(reserveErr, billingclient.ErrForbidden) {
					forcedFailureReason = "billing_window_advance_failed"
				}
				skipFinalBilling = true
				nextWindowReserveAt = time.Time{}
				s.writeSystemLog(ctx, executionID, attemptID, "billing reserve-next failed current_window_seq=%d actual_quantity=%d error=%v", currentReservation.WindowSeq, windowSeconds, reserveErr)
				cancel()
				continue
			}

			if err := s.markNextWindowReserved(ctx, attemptID, nextReservation, settledAt); err != nil {
				s.Logger.ErrorContext(ctx, "mark next billing window reserved", "attempt_id", attemptID, "window_seq", nextReservation.WindowSeq, "error", err)
				if voidErr := s.Billing.Void(ctx, nextReservation); voidErr != nil {
					s.Logger.ErrorContext(ctx, "void next billing window after persistence failure", "attempt_id", attemptID, "window_seq", nextReservation.WindowSeq, "error", voidErr)
				}
				forcedFailureReason = "billing_window_advance_persist_failed"
				skipFinalBilling = true
				nextWindowReserveAt = time.Time{}
				cancel()
				continue
			}

			s.writeSystemLog(ctx, executionID, attemptID, "billing advanced window_seq=%d next_window_seq=%d actual_quantity=%d charge_units=%d", currentReservation.WindowSeq, nextReservation.WindowSeq, windowSeconds, windowChargeUnits)
			currentReservation = nextReservation
			nextWindowReserveAt = nextReservation.RenewBy
		}
	}
}

func (s *Service) handleSettleFailure(
	ctx context.Context,
	executionID, attemptID uuid.UUID,
	orgID uint64,
	actorID string,
	req SubmitRequest,
	reservation billingclient.Reservation,
	outcome executionOutcome,
	actualSeconds int,
	settleErr error,
	totalChargeUnits uint64,
) {
	failureOutcome := outcome
	failureOutcome.State = StateFailed
	// Always set billing_settle_failed so the reconciler can dispatch to void
	// instead of retrying settle. The original failure reason is captured in
	// system logs and the execution outcome's exit_code.
	failureOutcome.FailureReason = "billing_settle_failed"
	if failureOutcome.CompletedAt.IsZero() {
		failureOutcome.CompletedAt = time.Now().UTC()
	}
	if failureOutcome.StartedAt.IsZero() {
		failureOutcome.StartedAt = failureOutcome.CompletedAt
	}
	if failureOutcome.DurationMs == 0 {
		failureOutcome.DurationMs = failureOutcome.CompletedAt.Sub(failureOutcome.StartedAt).Milliseconds()
	}

	s.Logger.ErrorContext(ctx, "billing settle", "execution_id", executionID, "attempt_id", attemptID, "actual_quantity", actualSeconds, "error", settleErr)
	s.writeSystemLog(ctx, executionID, attemptID, "billing settle failed window_seq=%d actual_quantity=%d error=%v", reservation.WindowSeq, actualSeconds, settleErr)
	if err := s.markFinalizing(ctx, executionID, attemptID, failureOutcome); err != nil {
		s.Logger.ErrorContext(ctx, "persist billing failure on finalizing attempt", "execution_id", executionID, "attempt_id", attemptID, "error", err)
	}

	if voidErr := s.Billing.Void(ctx, reservation); voidErr != nil {
		s.Logger.ErrorContext(ctx, "billing void after settle failure", "execution_id", executionID, "attempt_id", attemptID, "error", voidErr)
		s.writeSystemLog(ctx, executionID, attemptID, "billing void failed after settle failure window_seq=%d error=%v", reservation.WindowSeq, voidErr)
		return
	}

	voidedAt := time.Now().UTC()
	s.writeSystemLog(ctx, executionID, attemptID, "billing voided after settle failure window_seq=%d", reservation.WindowSeq)
	if err := s.markWindowVoided(ctx, attemptID, int(reservation.WindowSeq), voidedAt); err != nil {
		s.Logger.ErrorContext(ctx, "mark window voided after settle failure", "execution_id", executionID, "attempt_id", attemptID, "error", err)
		return
	}
	if err := s.markTerminal(ctx, executionID, attemptID, failureOutcome); err != nil {
		s.Logger.ErrorContext(ctx, "mark terminal after settle failure", "execution_id", executionID, "attempt_id", attemptID, "error", err)
		s.writeSystemLog(ctx, executionID, attemptID, "terminal persistence deferred after billing failure: %v", err)
		return
	}

	s.recordExecutionCompletion(ctx, executionID, attemptID, orgID, actorID, req, reservation, failureOutcome, totalChargeUnits)
}

func (s *Service) runAttemptWorkload(ctx context.Context, executionID, attemptID uuid.UUID, req SubmitRequest) (executionOutcome, error) {
	switch req.Kind {
	case KindDirect:
		return s.runDirect(ctx, executionID, attemptID, req)
	case KindRepoExec:
		return s.runRepoExec(ctx, executionID, attemptID, req)
	case KindWarmGolden:
		return s.runWarmGolden(ctx, executionID, attemptID, req)
	case KindForgejoRunner:
		return s.runForgejoRunner(ctx, executionID, attemptID, req)
	default:
		return executionOutcome{}, fmt.Errorf("unsupported execution kind %q", req.Kind)
	}
}

func stopTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func (s *Service) runDirect(ctx context.Context, executionID, attemptID uuid.UUID, req SubmitRequest) (executionOutcome, error) {
	startedAt := time.Now().UTC()
	if err := s.markRunning(ctx, executionID, attemptID, startedAt); err != nil {
		return executionOutcome{}, err
	}
	s.writeSystemLog(ctx, executionID, attemptID, "running direct execution")

	command := strings.TrimSpace(req.RunCommand)
	if command == "" {
		command = defaultRunCommand
	}
	if err := s.updateExecutionRunCommand(ctx, executionID, command); err != nil {
		return executionOutcome{}, err
	}

	jobCfg := vmorchestrator.JobConfig{
		JobID:      attemptID.String(),
		RunCommand: []string{"sh", "-c", command},
		Env: map[string]string{
			"REPO_URL": req.RepoURL,
		},
	}
	result, err := s.Orchestrator.Run(ctx, jobCfg)
	outcome := executionOutcome{
		StartedAt:   startedAt,
		CompletedAt: time.Now().UTC(),
		Logs:        result.Logs,
		ExitCode:    result.ExitCode,
		ZFSWritten:  result.ZFSWritten,
		StdoutBytes: result.StdoutBytes,
		StderrBytes: result.StderrBytes,
	}
	outcome.DurationMs = outcome.CompletedAt.Sub(startedAt).Milliseconds()
	if err != nil {
		outcome.State = StateFailed
		outcome.FailureReason = failureReasonFromError(err)
		return outcome, err
	}
	if result.ExitCode != 0 {
		outcome.State = StateFailed
	}
	return outcome, nil
}

func (s *Service) runRepoExec(ctx context.Context, executionID, attemptID uuid.UUID, req SubmitRequest) (executionOutcome, error) {
	prepared, err := PrepareRepoExec(RepoExecSpec{
		JobID: attemptID.String(),
		RepoTarget: RepoTarget{
			Repo:    req.Repo,
			RepoURL: req.RepoURL,
		},
		Ref: req.Ref,
	})
	if err != nil {
		return executionOutcome{FailureReason: "repo_prepare_failed"}, err
	}
	defer prepared.Cleanup()

	actualRunCommand := strings.Join(prepared.Request.JobTemplate.RunCommand, " ")
	if err := s.updateExecutionPreparedFields(ctx, executionID, prepared.Inspection.CommitSHA, actualRunCommand); err != nil {
		return executionOutcome{FailureReason: "persist_prepared_fields_failed"}, err
	}

	startedAt := time.Now().UTC()
	if err := s.markRunning(ctx, executionID, attemptID, startedAt); err != nil {
		return executionOutcome{FailureReason: "mark_running_failed"}, err
	}
	s.writeSystemLog(ctx, executionID, attemptID, "running repo execution ref=%s repo=%s", req.Ref, req.Repo)

	status, err := s.Orchestrator.ExecRepo(ctx, prepared.Request)
	if shouldWarmRepoGolden(err, status.ErrorMessage) {
		// Surgical note: the steady-state contract is still "push default branch to
		// warm the golden ahead of time". For first-contact website runs we accept a
		// colder path and warm on demand so the product does not fail immediately on
		// a brand-new repo.
		s.Logger.InfoContext(ctx, "repo execution warming missing golden",
			"execution_id", executionID,
			"attempt_id", attemptID,
			"repo", req.Repo,
			"default_branch", req.DefaultBranch,
		)
		s.writeSystemLog(ctx, executionID, attemptID, "active golden missing; warming repo golden on demand for repo=%s", req.Repo)
		if warmErr := s.warmRepoGoldenOnDemand(ctx, req); warmErr != nil {
			outcome := executionOutcome{
				StartedAt:     startedAt,
				CompletedAt:   time.Now().UTC(),
				CommitSHA:     prepared.Inspection.CommitSHA,
				FailureReason: failureReasonFromError(warmErr),
				State:         StateFailed,
			}
			outcome.DurationMs = outcome.CompletedAt.Sub(startedAt).Milliseconds()
			return outcome, warmErr
		}
		prepared.Request.JobTemplate.JobID = uuid.NewString()
		status, err = s.Orchestrator.ExecRepo(ctx, prepared.Request)
	}
	outcome := executionOutcome{
		StartedAt:   startedAt,
		CompletedAt: time.Now().UTC(),
		CommitSHA:   prepared.Inspection.CommitSHA,
	}
	outcome.DurationMs = outcome.CompletedAt.Sub(startedAt).Milliseconds()
	if status.RepoExec != nil {
		if status.RepoExec.CommitSHA != "" {
			outcome.CommitSHA = status.RepoExec.CommitSHA
		}
		outcome.GoldenSnapshot = status.RepoExec.GoldenSnapshot
	}
	if status.Result != nil {
		outcome.ExitCode = status.Result.ExitCode
		outcome.Logs = status.Result.Logs
		outcome.ZFSWritten = status.Result.ZFSWritten
		outcome.StdoutBytes = status.Result.StdoutBytes
		outcome.StderrBytes = status.Result.StderrBytes
	}
	if err != nil {
		outcome.State = StateFailed
		outcome.FailureReason = failureReasonFromError(err)
		return outcome, err
	}
	if status.ErrorMessage != "" {
		outcome.State = StateFailed
		outcome.FailureReason = status.ErrorMessage
		return outcome, errors.New(status.ErrorMessage)
	}
	if status.State == vmorchestrator.JobStateCanceled {
		outcome.State = StateCanceled
		return outcome, nil
	}
	if outcome.ExitCode != 0 || status.State == vmorchestrator.JobStateFailed {
		outcome.State = StateFailed
	}
	return outcome, nil
}

func (s *Service) warmRepoGoldenOnDemand(ctx context.Context, req SubmitRequest) error {
	prepared, err := PrepareWarmGolden(WarmGoldenSpec{
		JobID: uuid.NewString(),
		RepoTarget: RepoTarget{
			Repo:    req.Repo,
			RepoURL: req.RepoURL,
		},
		DefaultBranch: req.DefaultBranch,
	})
	if err != nil {
		return err
	}
	defer prepared.Cleanup()

	result, err := s.Orchestrator.WarmGolden(ctx, prepared.Request)
	if err != nil {
		return err
	}
	if result.ErrorMessage != "" {
		return errors.New(result.ErrorMessage)
	}
	if result.JobResult.ExitCode != 0 {
		return fmt.Errorf("warm golden failed with exit code %d", result.JobResult.ExitCode)
	}
	return nil
}

func (s *Service) runWarmGolden(ctx context.Context, executionID, attemptID uuid.UUID, req SubmitRequest) (executionOutcome, error) {
	prepared, err := PrepareWarmGolden(WarmGoldenSpec{
		JobID: attemptID.String(),
		RepoTarget: RepoTarget{
			Repo:    req.Repo,
			RepoURL: req.RepoURL,
		},
		DefaultBranch: req.DefaultBranch,
	})
	if err != nil {
		return executionOutcome{FailureReason: "warm_prepare_failed"}, err
	}
	defer prepared.Cleanup()

	actualRunCommand := strings.Join(prepared.Request.Job.RunCommand, " ")
	if err := s.updateExecutionPreparedFields(ctx, executionID, prepared.Inspection.CommitSHA, actualRunCommand); err != nil {
		return executionOutcome{FailureReason: "persist_prepared_fields_failed"}, err
	}

	startedAt := time.Now().UTC()
	if err := s.markRunning(ctx, executionID, attemptID, startedAt); err != nil {
		return executionOutcome{FailureReason: "mark_running_failed"}, err
	}
	s.writeSystemLog(ctx, executionID, attemptID, "warming golden for repo=%s default_branch=%s", req.Repo, req.DefaultBranch)
	if req.GoldenGenerationID != nil {
		if err := s.SetGoldenGenerationState(ctx, *req.GoldenGenerationID, GenerationStateBuilding, "", ""); err != nil {
			return executionOutcome{FailureReason: "mark_generation_building_failed"}, err
		}
	}

	result, err := s.Orchestrator.WarmGolden(ctx, prepared.Request)
	outcome := executionOutcome{
		StartedAt:      startedAt,
		CompletedAt:    time.Now().UTC(),
		CommitSHA:      firstNonEmpty(result.CommitSHA, prepared.Inspection.CommitSHA),
		GoldenSnapshot: result.TargetDataset,
		ExitCode:       result.JobResult.ExitCode,
		Logs:           result.JobResult.Logs,
		ZFSWritten:     result.JobResult.ZFSWritten,
		StdoutBytes:    result.JobResult.StdoutBytes,
		StderrBytes:    result.JobResult.StderrBytes,
	}
	outcome.DurationMs = outcome.CompletedAt.Sub(startedAt).Milliseconds()
	if err != nil {
		outcome.State = StateFailed
		outcome.FailureReason = failureReasonFromError(err)
		return outcome, err
	}
	if result.ErrorMessage != "" {
		outcome.State = StateFailed
		outcome.FailureReason = result.ErrorMessage
		return outcome, errors.New(result.ErrorMessage)
	}
	if outcome.ExitCode != 0 {
		outcome.State = StateFailed
	}
	return outcome, nil
}

func (s *Service) GetExecution(ctx context.Context, orgID uint64, executionID uuid.UUID) (*ExecutionRecord, error) {
	row := s.PG.QueryRowContext(ctx, `
		SELECT
			e.execution_id,
			e.org_id,
			e.actor_id,
			e.kind,
			e.provider,
			e.product_id,
			e.status,
			e.correlation_id,
			COALESCE(e.idempotency_key, ''),
			COALESCE(e.repo_id::text, ''),
			COALESCE(e.golden_generation_id::text, ''),
			e.repo,
			e.repo_url,
			e.ref,
			e.default_branch,
			e.run_command,
			e.commit_sha,
			e.workflow_path,
			e.workflow_job_name,
			e.provider_run_id,
			e.provider_job_id,
			e.created_at,
			e.updated_at,
			a.attempt_id,
			a.attempt_seq,
			a.state,
			a.orchestrator_job_id,
			COALESCE(a.billing_job_id, 0),
			a.runner_name,
			a.golden_snapshot,
			a.failure_reason,
			COALESCE(a.exit_code, 0),
			COALESCE(a.duration_ms, 0),
			COALESCE(a.zfs_written, 0),
			COALESCE(a.stdout_bytes, 0),
			COALESCE(a.stderr_bytes, 0),
			a.trace_id,
			a.started_at,
			a.completed_at,
			a.created_at,
			a.updated_at
		FROM executions e
		LEFT JOIN execution_attempts a ON a.attempt_id = e.latest_attempt_id
		WHERE e.execution_id = $1 AND e.org_id = $2
	`, executionID, int64(orgID))

	var (
		record             ExecutionRecord
		attempt            AttemptRecord
		repoID             string
		goldenGenerationID string
		startedAt          sql.NullTime
		completedAt        sql.NullTime
		attemptCreatedAt   sql.NullTime
		attemptUpdatedAt   sql.NullTime
	)
	if err := row.Scan(
		&record.ExecutionID,
		&record.OrgID,
		&record.ActorID,
		&record.Kind,
		&record.Provider,
		&record.ProductID,
		&record.Status,
		&record.CorrelationID,
		&record.IdempotencyKey,
		&repoID,
		&goldenGenerationID,
		&record.Repo,
		&record.RepoURL,
		&record.Ref,
		&record.DefaultBranch,
		&record.RunCommand,
		&record.CommitSHA,
		&record.WorkflowPath,
		&record.WorkflowJobName,
		&record.ProviderRunID,
		&record.ProviderJobID,
		&record.CreatedAt,
		&record.UpdatedAt,
		&attempt.AttemptID,
		&attempt.AttemptSeq,
		&attempt.State,
		&attempt.OrchestratorJobID,
		&attempt.BillingJobID,
		&attempt.RunnerName,
		&attempt.GoldenSnapshot,
		&attempt.FailureReason,
		&attempt.ExitCode,
		&attempt.DurationMs,
		&attempt.ZFSWritten,
		&attempt.StdoutBytes,
		&attempt.StderrBytes,
		&attempt.TraceID,
		&startedAt,
		&completedAt,
		&attemptCreatedAt,
		&attemptUpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrExecutionMissing
		}
		return nil, fmt.Errorf("scan execution: %w", err)
	}
	if startedAt.Valid {
		attempt.StartedAt = &startedAt.Time
	}
	record.RepoID = strings.TrimSpace(repoID)
	record.GoldenGenerationID = strings.TrimSpace(goldenGenerationID)
	if completedAt.Valid {
		attempt.CompletedAt = &completedAt.Time
	}
	if attemptCreatedAt.Valid {
		attempt.CreatedAt = attemptCreatedAt.Time
	}
	if attemptUpdatedAt.Valid {
		attempt.UpdatedAt = attemptUpdatedAt.Time
	}
	record.LatestAttempt = attempt

	windows, err := s.getBillingWindows(ctx, attempt.AttemptID)
	if err != nil {
		return nil, err
	}
	record.BillingWindows = windows
	return &record, nil
}

func (s *Service) GetExecutionLogs(ctx context.Context, orgID uint64, executionID uuid.UUID) (uuid.UUID, string, error) {
	var attemptID uuid.UUID
	if err := s.PG.QueryRowContext(ctx, `
		SELECT latest_attempt_id
		FROM executions
		WHERE execution_id = $1 AND org_id = $2
	`, executionID, int64(orgID)).Scan(&attemptID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, "", ErrExecutionMissing
		}
		return uuid.Nil, "", fmt.Errorf("query latest attempt: %w", err)
	}

	rows, err := s.PG.QueryContext(ctx,
		`SELECT chunk FROM execution_logs WHERE attempt_id = $1 ORDER BY seq`,
		attemptID,
	)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("query execution logs: %w", err)
	}
	defer rows.Close()

	var buf strings.Builder
	for rows.Next() {
		var chunk string
		if err := rows.Scan(&chunk); err != nil {
			return uuid.Nil, "", fmt.Errorf("scan execution log chunk: %w", err)
		}
		buf.WriteString(chunk)
	}
	return attemptID, buf.String(), rows.Err()
}

func (s *Service) getBillingWindows(ctx context.Context, attemptID uuid.UUID) ([]BillingWindow, error) {
	rows, err := s.PG.QueryContext(ctx, `
		SELECT
			attempt_id,
			billing_window_id,
			window_seq,
			reservation_shape,
			reserved_quantity,
			COALESCE(actual_quantity, 0),
			pricing_phase,
			state,
			window_start,
			created_at,
			settled_at
		FROM execution_billing_windows
		WHERE attempt_id = $1
		ORDER BY window_seq
	`, attemptID)
	if err != nil {
		return nil, fmt.Errorf("query billing windows: %w", err)
	}
	defer rows.Close()

	var out []BillingWindow
	for rows.Next() {
		var (
			window    BillingWindow
			settledAt sql.NullTime
		)
		if err := rows.Scan(
			&window.AttemptID,
			&window.BillingWindowID,
			&window.WindowSeq,
			&window.ReservationShape,
			&window.ReservedQuantity,
			&window.ActualQuantity,
			&window.PricingPhase,
			&window.State,
			&window.WindowStart,
			&window.CreatedAt,
			&settledAt,
		); err != nil {
			return nil, fmt.Errorf("scan billing window: %w", err)
		}
		if settledAt.Valid {
			window.SettledAt = &settledAt.Time
		}
		out = append(out, window)
	}
	return out, rows.Err()
}

func (s *Service) insertQueuedExecution(ctx context.Context, executionID, attemptID uuid.UUID, orgID uint64, actorID string, req SubmitRequest, traceID, correlationID string, now time.Time) error {
	tx, err := s.PG.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var repoID any
	if strings.TrimSpace(req.RepoID) != "" {
		parsedRepoID, err := uuid.Parse(strings.TrimSpace(req.RepoID))
		if err != nil {
			return fmt.Errorf("parse repo_id: %w", err)
		}
		repoID = parsedRepoID
	}
	var goldenGenerationID any
	if req.GoldenGenerationID != nil {
		goldenGenerationID = *req.GoldenGenerationID
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO executions (
			execution_id, org_id, actor_id, kind, provider, product_id, status, correlation_id,
			idempotency_key, repo_id, golden_generation_id, repo, repo_url, ref, default_branch, run_command,
			workflow_path, workflow_job_name, provider_run_id, provider_job_id,
			latest_attempt_id, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			NULLIF($9, ''), $10, $11, $12, $13, $14, $15, $16,
			$17, $18, $19, $20,
			$21, $22, $22
		)
	`, executionID, int64(orgID), actorID, req.Kind, req.Provider, req.ProductID, StateQueued, correlationID, req.IdempotencyKey, repoID, goldenGenerationID, req.Repo, req.RepoURL, req.Ref, req.DefaultBranch, req.RunCommand, req.WorkflowPath, req.WorkflowJobName, req.ProviderRunID, req.ProviderJobID, attemptID, now); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO execution_attempts (
			attempt_id, execution_id, attempt_seq, state, trace_id, created_at, updated_at
		) VALUES ($1, $2, 1, $3, $4, $5, $5)
	`, attemptID, executionID, StateQueued, traceID, now); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Service) markReserved(ctx context.Context, executionID, attemptID uuid.UUID, billingJobID int64, reservation billingclient.Reservation, traceID string, now time.Time) error {
	tx, err := s.PG.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE execution_attempts
		SET state = $2, billing_job_id = $3, trace_id = $4, updated_at = $5
		WHERE attempt_id = $1
	`, attemptID, StateReserved, billingJobID, traceID, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO execution_billing_windows (
			attempt_id, billing_window_id, window_seq, reservation_shape,
			reserved_quantity, pricing_phase, state, window_start, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, 'reserved', $7, $8)
	`, attemptID, reservation.WindowId, reservation.WindowSeq, reservation.ReservationShape, reservation.WindowSecs, reservation.PricingPhase, reservation.WindowStart, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE executions
		SET status = $2, updated_at = $3
		WHERE execution_id = $1
	`, executionID, StateReserved, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) markNextWindowReserved(ctx context.Context, attemptID uuid.UUID, next billingclient.Reservation, reservedAt time.Time) error {
	tx, err := s.PG.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO execution_billing_windows (
			attempt_id, billing_window_id, window_seq, reservation_shape,
			reserved_quantity, pricing_phase, state, window_start, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, 'reserved', $7, $8)
	`, attemptID, next.WindowId, next.WindowSeq, next.ReservationShape, next.WindowSecs, next.PricingPhase, next.WindowStart, reservedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE execution_attempts
		SET updated_at = $2
		WHERE attempt_id = $1
	`, attemptID, reservedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) markLaunching(ctx context.Context, executionID, attemptID uuid.UUID, orchestratorJobID, traceID string, now time.Time) error {
	tx, err := s.PG.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE execution_attempts
		SET state = $2, orchestrator_job_id = $3, trace_id = $4, updated_at = $5
		WHERE attempt_id = $1
	`, attemptID, StateLaunching, orchestratorJobID, traceID, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE executions
		SET status = $2, updated_at = $3
		WHERE execution_id = $1
	`, executionID, StateLaunching, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) markRunning(ctx context.Context, executionID, attemptID uuid.UUID, startedAt time.Time) error {
	tx, err := s.PG.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE execution_attempts
		SET state = $2, started_at = $3, updated_at = $3
		WHERE attempt_id = $1
	`, attemptID, StateRunning, startedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE executions
		SET status = $2, updated_at = $3
		WHERE execution_id = $1
	`, executionID, StateRunning, startedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) markFinalizing(ctx context.Context, executionID, attemptID uuid.UUID, outcome executionOutcome) error {
	tx, err := s.PG.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE execution_attempts
		SET state = $2,
		    failure_reason = $3,
		    exit_code = $4,
		    duration_ms = $5,
		    zfs_written = $6,
		    stdout_bytes = $7,
		    stderr_bytes = $8,
		    golden_snapshot = $9,
		    started_at = COALESCE(started_at, $10),
		    completed_at = $11,
		    updated_at = $11
		WHERE attempt_id = $1
	`, attemptID, StateFinalizing, outcome.FailureReason, outcome.ExitCode, outcome.DurationMs, int64(outcome.ZFSWritten), int64(outcome.StdoutBytes), int64(outcome.StderrBytes), outcome.GoldenSnapshot, outcome.StartedAt, outcome.CompletedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE executions
		SET status = $2,
		    commit_sha = CASE WHEN $3 <> '' THEN $3 ELSE commit_sha END,
		    updated_at = $4
		WHERE execution_id = $1
	`, executionID, StateFinalizing, outcome.CommitSHA, outcome.CompletedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) markTerminal(ctx context.Context, executionID, attemptID uuid.UUID, outcome executionOutcome) error {
	tx, err := s.PG.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE execution_attempts
		SET state = $2,
		    failure_reason = $3,
		    exit_code = $4,
		    duration_ms = $5,
		    zfs_written = $6,
		    stdout_bytes = $7,
		    stderr_bytes = $8,
		    golden_snapshot = $9,
		    started_at = COALESCE(started_at, $10),
		    completed_at = $11,
		    updated_at = $11
		WHERE attempt_id = $1
	`, attemptID, outcome.State, outcome.FailureReason, outcome.ExitCode, outcome.DurationMs, int64(outcome.ZFSWritten), int64(outcome.StdoutBytes), int64(outcome.StderrBytes), outcome.GoldenSnapshot, outcome.StartedAt, outcome.CompletedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE executions
		SET status = $2,
		    commit_sha = CASE WHEN $3 <> '' THEN $3 ELSE commit_sha END,
		    updated_at = $4
		WHERE execution_id = $1
	`, executionID, outcome.State, outcome.CommitSHA, outcome.CompletedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) markWindowSettled(ctx context.Context, attemptID uuid.UUID, windowSeq, actualSeconds int, pricingPhase string, settledAt time.Time) error {
	_, err := s.PG.ExecContext(ctx, `
		UPDATE execution_billing_windows
		SET actual_quantity = $3, pricing_phase = $4, state = 'settled', settled_at = $5
		WHERE attempt_id = $1 AND window_seq = $2
	`, attemptID, windowSeq, actualSeconds, pricingPhase, settledAt)
	return err
}

func (s *Service) markWindowVoided(ctx context.Context, attemptID uuid.UUID, windowSeq int, settledAt time.Time) error {
	_, err := s.PG.ExecContext(ctx, `
		UPDATE execution_billing_windows
		SET state = 'voided', settled_at = $3
		WHERE attempt_id = $1 AND window_seq = $2
	`, attemptID, windowSeq, settledAt)
	return err
}

func (s *Service) failWithoutBilling(ctx context.Context, executionID, attemptID uuid.UUID, reason string, completedAt time.Time) error {
	tx, err := s.PG.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE execution_attempts
		SET state = $2, failure_reason = $3, completed_at = $4, updated_at = $4
		WHERE attempt_id = $1
	`, attemptID, StateFailed, reason, completedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE executions
		SET status = $2, updated_at = $3
		WHERE execution_id = $1
	`, executionID, StateFailed, completedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) updateExecutionPreparedFields(ctx context.Context, executionID uuid.UUID, commitSHA, runCommand string) error {
	_, err := s.PG.ExecContext(ctx, `
		UPDATE executions
		SET commit_sha = CASE WHEN $2 <> '' THEN $2 ELSE commit_sha END,
		    run_command = CASE WHEN $3 <> '' THEN $3 ELSE run_command END,
		    updated_at = now()
		WHERE execution_id = $1
	`, executionID, commitSHA, runCommand)
	return err
}

func (s *Service) updateExecutionRunCommand(ctx context.Context, executionID uuid.UUID, runCommand string) error {
	_, err := s.PG.ExecContext(ctx, `
		UPDATE executions
		SET run_command = $2, updated_at = now()
		WHERE execution_id = $1
	`, executionID, runCommand)
	return err
}

func (s *Service) findByIdempotencyKey(ctx context.Context, orgID uint64, key string) (executionSnapshot, bool, error) {
	row := s.PG.QueryRowContext(ctx, `
		SELECT execution_id, latest_attempt_id, status
		FROM executions
		WHERE org_id = $1 AND idempotency_key = $2
	`, int64(orgID), key)
	var snapshot executionSnapshot
	if err := row.Scan(&snapshot.ExecutionID, &snapshot.LatestAttemptID, &snapshot.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return executionSnapshot{}, false, nil
		}
		return executionSnapshot{}, false, err
	}
	return snapshot, true, nil
}

func (s *Service) writeLogChunks(ctx context.Context, executionID, attemptID uuid.UUID, logs string, createdAt time.Time) {
	s.writeLogChunksWithStream(ctx, executionID, attemptID, defaultLogStream, logs, createdAt)
}

func (s *Service) writeSystemLog(ctx context.Context, executionID, attemptID uuid.UUID, format string, args ...any) {
	message := strings.TrimSpace(fmt.Sprintf(format, args...))
	if message == "" {
		return
	}
	s.writeLogChunksWithStream(ctx, executionID, attemptID, "system", message+"\n", time.Now().UTC())
}

func (s *Service) writeLogChunksWithStream(ctx context.Context, executionID, attemptID uuid.UUID, stream, logs string, createdAt time.Time) {
	const chunkSize = 8192
	seq := s.nextLogSeq(ctx, attemptID)

	for start := 0; start < len(logs); start += chunkSize {
		end := start + chunkSize
		if end > len(logs) {
			end = len(logs)
		}
		chunk := logs[start:end]

		if _, pgErr := s.PG.ExecContext(ctx,
			`INSERT INTO execution_logs (attempt_id, seq, stream, chunk, created_at) VALUES ($1, $2, $3, $4, $5)`,
			attemptID, seq, stream, chunk, createdAt,
		); pgErr != nil {
			s.Logger.ErrorContext(ctx, "write execution log chunk to PG", "attempt_id", attemptID, "seq", seq, "error", pgErr)
		}

		chErr := s.writeLogChunkCH(ctx, JobLogRow{
			ExecutionID: executionID.String(),
			AttemptID:   attemptID.String(),
			Seq:         uint32(seq),
			Stream:      stream,
			Chunk:       chunk,
			CreatedAt:   createdAt,
		})
		if chErr != nil {
			s.Logger.ErrorContext(ctx, "write execution log chunk to ClickHouse", "attempt_id", attemptID, "seq", seq, "error", chErr)
		}

		seq++
	}
}

func (s *Service) nextLogSeq(ctx context.Context, attemptID uuid.UUID) int {
	if s.PG == nil {
		return 0
	}
	var next int
	if err := s.PG.QueryRowContext(ctx, `
		SELECT COALESCE(max(seq) + 1, 0)
		FROM execution_logs
		WHERE attempt_id = $1
	`, attemptID).Scan(&next); err != nil {
		s.Logger.ErrorContext(ctx, "next execution log seq", "attempt_id", attemptID, "error", err)
		return 0
	}
	return next
}

func (s *Service) writeLogChunkCH(ctx context.Context, row JobLogRow) error {
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO "+s.CHDatabase+".job_logs")
	if err != nil {
		return fmt.Errorf("prepare batch: %w", err)
	}
	if err := batch.AppendStruct(&row); err != nil {
		return fmt.Errorf("append row: %w", err)
	}
	return batch.Send()
}

func (s *Service) writeJobEvent(ctx context.Context, event JobEventRow) {
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO "+s.CHDatabase+".job_events")
	if err != nil {
		s.Logger.ErrorContext(ctx, "prepare job event batch", "error", err)
		return
	}
	if err := batch.AppendStruct(&event); err != nil {
		s.Logger.ErrorContext(ctx, "append job event", "error", err)
		return
	}
	if err := batch.Send(); err != nil {
		s.Logger.ErrorContext(ctx, "send job event batch", "error", err)
	}
}

func (s *Service) recordExecutionCompletion(
	ctx context.Context,
	executionID, attemptID uuid.UUID,
	orgID uint64,
	actorID string,
	req SubmitRequest,
	reservation billingclient.Reservation,
	outcome executionOutcome,
	charge uint64,
) {
	if strings.TrimSpace(outcome.Logs) != "" {
		s.writeLogChunks(ctx, executionID, attemptID, outcome.Logs, outcome.StartedAt)
	}

	s.writeJobEvent(ctx, JobEventRow{
		ExecutionID:        executionID.String(),
		AttemptID:          attemptID.String(),
		OrgID:              orgID,
		ActorID:            actorID,
		Kind:               req.Kind,
		Provider:           req.Provider,
		ProductID:          req.ProductID,
		RepoID:             req.RepoID,
		GoldenGenerationID: uuidString(req.GoldenGenerationID),
		Repo:               req.Repo,
		RepoURL:            req.RepoURL,
		Ref:                req.Ref,
		DefaultBranch:      req.DefaultBranch,
		RunCommand:         req.RunCommand,
		CommitSHA:          outcome.CommitSHA,
		WorkflowPath:       req.WorkflowPath,
		WorkflowJobName:    req.WorkflowJobName,
		ProviderRunID:      req.ProviderRunID,
		ProviderJobID:      req.ProviderJobID,
		RunnerName:         outcome.RunnerName,
		Status:             outcome.State,
		ExitCode:           int32(outcome.ExitCode),
		DurationMs:         outcome.DurationMs,
		ZFSWritten:         outcome.ZFSWritten,
		StdoutBytes:        outcome.StdoutBytes,
		StderrBytes:        outcome.StderrBytes,
		GoldenSnapshot:     outcome.GoldenSnapshot,
		BillingJobID:       reservation.JobId,
		ChargeUnits:        charge,
		PricingPhase:       reservation.PricingPhase,
		CorrelationID:      CorrelationIDFromContext(ctx),
		VerificationRunID:  VerificationRunIDFromContext(ctx),
		StartedAt:          outcome.StartedAt,
		CompletedAt:        outcome.CompletedAt,
		CreatedAt:          outcome.StartedAt,
		TraceID:            traceIDFromContext(ctx),
	})
	s.Logger.InfoContext(ctx, "execution completed",
		"fm_correlation_id", CorrelationIDFromContext(ctx),
		"verification_run_id", VerificationRunIDFromContext(ctx),
		"execution_id", executionID,
		"attempt_id", attemptID,
		"state", outcome.State,
		"actual_quantity", actualSecondsForReservation(reservation, outcome.CompletedAt),
		"charge_units", charge,
		"pricing_phase", reservation.PricingPhase,
	)
}

func (s *Service) nextBillingJobID(ctx context.Context) (int64, error) {
	var id int64
	if err := s.PG.QueryRowContext(ctx, `SELECT nextval('execution_billing_job_id_seq')`).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Service) countActiveAttempts(ctx context.Context, orgID uint64) (int64, error) {
	var count int64
	if err := s.PG.QueryRowContext(ctx, `
		SELECT count(*)
		FROM executions e
		JOIN execution_attempts a ON a.execution_id = e.execution_id
		WHERE e.org_id = $1
		  AND a.state IN ('reserved', 'launching', 'running', 'finalizing')
	`, int64(orgID)).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func normalizeSubmitRequest(req SubmitRequest) (SubmitRequest, error) {
	req.Kind = strings.TrimSpace(req.Kind)
	req.ProductID = strings.TrimSpace(req.ProductID)
	req.Provider = strings.TrimSpace(req.Provider)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.RepoID = strings.TrimSpace(req.RepoID)
	req.Repo = strings.TrimSpace(req.Repo)
	req.RepoURL = strings.TrimSpace(req.RepoURL)
	req.Ref = strings.TrimSpace(req.Ref)
	req.DefaultBranch = strings.TrimSpace(req.DefaultBranch)
	req.RunCommand = strings.TrimSpace(req.RunCommand)
	req.WorkflowPath = strings.TrimSpace(req.WorkflowPath)
	req.WorkflowJobName = strings.TrimSpace(req.WorkflowJobName)
	req.ProviderRunID = strings.TrimSpace(req.ProviderRunID)
	req.ProviderJobID = strings.TrimSpace(req.ProviderJobID)

	if req.Kind == "" {
		req.Kind = KindDirect
	}
	if req.ProductID == "" {
		req.ProductID = defaultProductID
	}
	if req.Repo == "" && req.RepoURL != "" {
		req.Repo = defaultRepoName(req.RepoURL)
	}
	if req.RepoURL != "" {
		if err := validateGitCloneURLField("repo_url", req.RepoURL); err != nil {
			return SubmitRequest{}, err
		}
	}
	if req.DefaultBranch == "" {
		req.DefaultBranch = defaultBranchName
	}
	if req.Kind == KindDirect && req.RunCommand == "" {
		req.RunCommand = defaultRunCommand
	}

	switch req.Kind {
	case KindDirect:
		return req, nil
	case KindRepoExec:
		if req.Ref == "" {
			if req.RepoID == "" {
				return SubmitRequest{}, fmt.Errorf("ref is required for repo_exec")
			}
		}
		if req.RepoID == "" && req.RepoURL == "" {
			return SubmitRequest{}, fmt.Errorf("repo_url or repo_id is required for repo_exec")
		}
		if req.RepoID == "" && req.Repo == "" {
			return SubmitRequest{}, fmt.Errorf("repo is required for repo_exec")
		}
		return req, nil
	case KindWarmGolden:
		if req.RepoURL == "" {
			return SubmitRequest{}, fmt.Errorf("repo_url is required for warm_golden")
		}
		if req.Repo == "" {
			return SubmitRequest{}, fmt.Errorf("repo is required for warm_golden")
		}
		return req, nil
	case KindForgejoRunner:
		if req.RepoID == "" && req.RepoURL == "" {
			return SubmitRequest{}, fmt.Errorf("repo_id or repo_url is required for forgejo_runner")
		}
		if req.ProviderRunID == "" {
			return SubmitRequest{}, fmt.Errorf("provider_run_id is required for forgejo_runner")
		}
		return req, nil
	default:
		return SubmitRequest{}, fmt.Errorf("unsupported execution kind %q", req.Kind)
	}
}

func (s *Service) hydrateImportedRepoRequest(ctx context.Context, orgID uint64, req SubmitRequest) (SubmitRequest, error) {
	if strings.TrimSpace(req.RepoID) == "" {
		return req, nil
	}

	repoID, err := uuid.Parse(strings.TrimSpace(req.RepoID))
	if err != nil {
		return SubmitRequest{}, fmt.Errorf("invalid repo_id: %w", err)
	}
	repo, err := s.GetRepo(ctx, orgID, repoID)
	if err != nil {
		return SubmitRequest{}, err
	}

	req.RepoID = repo.RepoID.String()
	req.Repo = firstNonEmpty(req.Repo, repo.FullName)
	req.RepoURL = firstNonEmpty(req.RepoURL, repo.CloneURL)
	req.DefaultBranch = firstNonEmpty(req.DefaultBranch, repo.DefaultBranch)
	req.Provider = firstNonEmpty(req.Provider, repo.Provider)
	if req.Ref == "" {
		req.Ref = "refs/heads/" + repo.DefaultBranch
	}

	switch req.Kind {
	case KindRepoExec:
		if repo.State != RepoStateReady && repo.State != RepoStateDegraded {
			return SubmitRequest{}, ErrRepoNotReady
		}
		if repo.ActiveGoldenGenerationID == nil {
			return SubmitRequest{}, ErrRepoNotReady
		}
		req.GoldenGenerationID = repo.ActiveGoldenGenerationID
		generation, err := s.GetGoldenGeneration(ctx, repo.RepoID, *repo.ActiveGoldenGenerationID)
		if err != nil {
			return SubmitRequest{}, err
		}
		req.GoldenSnapshotRef = generation.SnapshotRef
	case KindWarmGolden:
		// Warm builds are the mechanism that moves a compatible repo toward
		// ready. Do not require an active generation here.
	case KindForgejoRunner:
		if repo.State != RepoStateReady && repo.State != RepoStateDegraded {
			return SubmitRequest{}, ErrRepoNotReady
		}
		if repo.ActiveGoldenGenerationID == nil {
			return SubmitRequest{}, ErrRepoNotReady
		}
		generation, err := s.GetGoldenGeneration(ctx, repo.RepoID, *repo.ActiveGoldenGenerationID)
		if err != nil {
			return SubmitRequest{}, err
		}
		if strings.TrimSpace(generation.SnapshotRef) == "" {
			return SubmitRequest{}, ErrRepoNotReady
		}
		req.GoldenGenerationID = repo.ActiveGoldenGenerationID
		req.GoldenSnapshotRef = generation.SnapshotRef
	}

	return req, nil
}

func defaultRepoName(repoURL string) string {
	base := filepath.Base(strings.TrimSuffix(strings.TrimSpace(repoURL), "/"))
	base = strings.TrimSuffix(base, ".git")
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "repo"
	}
	return base
}

func shouldWarmRepoGolden(err error, statusMessage string) bool {
	combined := strings.ToLower(strings.TrimSpace(statusMessage))
	if err != nil {
		if combined != "" {
			combined += " "
		}
		combined += strings.ToLower(err.Error())
	}
	return strings.Contains(combined, "repo golden") && strings.Contains(combined, "run warm first")
}

func reserveFailureReason(err error) string {
	switch {
	case errors.Is(err, billingclient.ErrForbidden):
		return "quota_denied"
	case errors.Is(err, billingclient.ErrPaymentRequired):
		return "insufficient_balance"
	default:
		return "billing_reserve_failed"
	}
}

func windowAdvanceFailureReason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, billingclient.ErrPaymentRequired):
		if strings.Contains(err.Error(), "spend cap") {
			return "spend_cap_exceeded"
		}
		return "insufficient_balance"
	case errors.Is(err, billingclient.ErrForbidden):
		if strings.Contains(err.Error(), "org suspended") {
			return "org_suspended"
		}
		return "billing_window_advance_denied"
	default:
		return "billing_window_advance_failed"
	}
}

func failureReasonFromError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "attempt_timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "attempt_canceled"
	}
	return strings.TrimSpace(err.Error())
}

func traceIDFromContext(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
		return sc.TraceID().String()
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func actualSecondsForReservation(reservation billingclient.Reservation, endedAt time.Time) int {
	return actualSecondsForWindow(reservation.WindowStart, endedAt, int(reservation.WindowSecs))
}

func actualSecondsForWindow(windowStart, endedAt time.Time, windowSeconds int) int {
	if windowSeconds <= 0 {
		return 1
	}
	if endedAt.IsZero() {
		endedAt = time.Now().UTC()
	}
	startedAt := windowStart.UTC()
	seconds := int(endedAt.UTC().Sub(startedAt) / time.Second)
	if seconds <= 0 {
		seconds = 1
	}
	if seconds > windowSeconds {
		return windowSeconds
	}
	return seconds
}

func chargeUnits(costPerSec int64, seconds int) uint64 {
	if costPerSec <= 0 || seconds <= 0 {
		return 0
	}
	return uint64(costPerSec) * uint64(seconds)
}

func uuidString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}
