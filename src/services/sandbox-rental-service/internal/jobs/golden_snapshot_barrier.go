package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/verself/sandbox-rental-service/internal/scheduler"
	"github.com/verself/sandbox-rental-service/internal/store"
	vmorchestrator "github.com/verself/vm-orchestrator"
)

type GoldenSnapshotBarrierEvidence struct {
	Provider                 string
	ProviderInstallationID   int64
	ProviderRepositoryID     int64
	ProviderRunID            int64
	ProviderRunAttempt       int64
	ProviderJobID            int64
	RepositoryFullName       string
	HeadSHA                  string
	Conclusion               string
	RunnerID                 int64
	RunnerName               string
	ExecutionID              uuid.UUID
	AttemptID                uuid.UUID
	LeaseID                  string
	JobShapeID               string
	DurableGenerationSetHash string
	TrustClass               string
	PromotionPolicy          string
}

type GoldenSnapshotBarrierResult struct {
	State  string
	Reason string
}

func (s *Service) RequestGoldenSnapshotBarrier(ctx context.Context, evidence GoldenSnapshotBarrierEvidence) (GoldenSnapshotBarrierResult, error) {
	if s == nil || s.PGX == nil {
		return GoldenSnapshotBarrierResult{}, ErrRunnerUnavailable
	}
	evidence.Provider = strings.TrimSpace(evidence.Provider)
	if evidence.Provider == "" || evidence.ProviderRepositoryID <= 0 || evidence.ProviderRunID <= 0 || evidence.ProviderRunAttempt <= 0 || evidence.ProviderJobID <= 0 {
		return GoldenSnapshotBarrierResult{}, fmt.Errorf("%w: provider run/job identity is required", ErrRunnerUnavailable)
	}
	if evidence.ExecutionID == uuid.Nil || evidence.AttemptID == uuid.Nil {
		return GoldenSnapshotBarrierResult{}, fmt.Errorf("%w: execution and attempt identity is required", ErrRunnerUnavailable)
	}
	if strings.TrimSpace(evidence.Conclusion) != "success" {
		return GoldenSnapshotBarrierResult{State: "blocked", Reason: "provider_conclusion_not_success"}, nil
	}
	identity, err := s.storeQueries().GetRunnerExecutionIdentity(ctx, store.GetRunnerExecutionIdentityParams{
		ExecutionID: &evidence.ExecutionID,
		AttemptID:   &evidence.AttemptID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GoldenSnapshotBarrierResult{State: "blocked", Reason: "sandbox_identity_missing"}, nil
		}
		return GoldenSnapshotBarrierResult{}, err
	}
	if reason := goldenSnapshotBarrierIdentityMismatch(evidence, identity); reason != "" {
		return GoldenSnapshotBarrierResult{State: "blocked", Reason: reason}, nil
	}
	attempt, err := s.storeQueries().GetGoldenSnapshotBarrierAttempt(ctx, store.GetGoldenSnapshotBarrierAttemptParams{
		ExecutionID: evidence.ExecutionID,
		AttemptID:   evidence.AttemptID,
	})
	if err != nil {
		return GoldenSnapshotBarrierResult{}, err
	}
	if attempt.AttemptState != StateFinalizing && attempt.AttemptState != StateSucceeded {
		return GoldenSnapshotBarrierResult{State: "blocked", Reason: "attempt_not_finalizing"}, nil
	}
	leaseID := strings.TrimSpace(attempt.LeaseID)
	if leaseID == "" {
		return GoldenSnapshotBarrierResult{State: "blocked", Reason: "lease_missing"}, nil
	}
	if inputLeaseID := strings.TrimSpace(evidence.LeaseID); inputLeaseID != "" && inputLeaseID != leaseID {
		return GoldenSnapshotBarrierResult{State: "blocked", Reason: "lease_mismatch"}, nil
	}
	if s.Orchestrator == nil {
		return GoldenSnapshotBarrierResult{State: "blocked", Reason: "orchestrator_unavailable"}, nil
	}
	lease, err := s.Orchestrator.GetLease(ctx, leaseID)
	if err != nil {
		return GoldenSnapshotBarrierResult{State: "blocked", Reason: "lease_lookup_failed"}, nil
	}
	switch lease.State {
	case vmorchestrator.LeaseStateReady, vmorchestrator.LeaseStateDraining:
	default:
		return GoldenSnapshotBarrierResult{State: "blocked", Reason: "lease_not_active"}, nil
	}
	if s.Scheduler == nil {
		return GoldenSnapshotBarrierResult{State: "blocked", Reason: "scheduler_unavailable"}, nil
	}
	_, err = s.Scheduler.EnqueueGoldenRunPromote(ctx, scheduler.GoldenRunPromoteRequest{
		Provider:               evidence.Provider,
		ProviderInstallationID: firstNonZero(evidence.ProviderInstallationID, identity.ProviderInstallationID),
		ProviderRepositoryID:   evidence.ProviderRepositoryID,
		ProviderRunID:          evidence.ProviderRunID,
		ProviderRunAttempt:     evidence.ProviderRunAttempt,
		ProviderJobID:          evidence.ProviderJobID,
		RepositoryFullName:     firstNonEmpty(evidence.RepositoryFullName, identity.RepositoryFullName),
		HeadSHA:                firstNonEmpty(evidence.HeadSHA, identity.HeadSha, identity.RunHeadSha),
		CorrelationID:          CorrelationIDFromContext(ctx),
		TraceParent:            traceParent(ctx),
	})
	if err != nil {
		return GoldenSnapshotBarrierResult{}, err
	}
	return GoldenSnapshotBarrierResult{State: "requested"}, nil
}

func goldenSnapshotBarrierIdentityMismatch(evidence GoldenSnapshotBarrierEvidence, identity store.GetRunnerExecutionIdentityRow) string {
	if identity.Provider != evidence.Provider {
		return "provider_mismatch"
	}
	if identity.ProviderRepositoryID != evidence.ProviderRepositoryID {
		return "provider_repository_mismatch"
	}
	if identity.ProviderRunID != evidence.ProviderRunID {
		return "provider_run_mismatch"
	}
	if identity.ProviderRunAttempt != evidence.ProviderRunAttempt {
		return "provider_run_attempt_mismatch"
	}
	if identity.ProviderJobID != evidence.ProviderJobID {
		return "provider_job_mismatch"
	}
	if evidence.ProviderInstallationID != 0 && identity.ProviderInstallationID != evidence.ProviderInstallationID {
		return "provider_installation_mismatch"
	}
	if evidence.RunnerName != "" && identity.RunnerName != evidence.RunnerName {
		return "runner_name_mismatch"
	}
	return ""
}
