package githubintegration

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/verself/github-integration-service/internal/store"
)

const deliveryProcessingStaleAfter = 2 * time.Minute

type workflowJobDeliveryFailureRef struct {
	EventName              string
	DeliveryID             string
	OrgID                  string
	InstallationBindingID  uuid.UUID
	RepositoryBindingID    uuid.UUID
	ProviderInstallationID int64
	ProviderRepositoryID   int64
	RepositoryFullName     string
	ProviderRunID          int64
	ProviderRunAttempt     int64
	ProviderJobID          int64
}

func (s *Service) ProcessStaleDeliveries(ctx context.Context) error {
	rows, err := s.queries.ListStaleProcessingDeliveries(ctx, store.ListStaleProcessingDeliveriesParams{
		StaleBefore: pgTime(time.Now().UTC().Add(-deliveryProcessingStaleAfter)),
		LimitCount:  s.cfg.WorkerBatchSize,
	})
	if err != nil {
		return err
	}
	var firstErr error
	for _, row := range rows {
		if err := s.processStaleDelivery(ctx, row); err != nil {
			firstErr = errors.Join(firstErr, err)
		}
	}
	return firstErr
}

func (s *Service) processStaleDelivery(ctx context.Context, row store.ListStaleProcessingDeliveriesRow) error {
	problems := newWebhookProblemSet(providerWebhookProcessingStaleProblem())
	terminal := row.AttemptCount >= s.cfg.MaxDeliveryTries
	if terminal {
		problems.add(providerWebhookAttemptsExhaustedProblem())
	}
	now := time.Now().UTC()
	ref := deliveryFailureRefFromStale(row)
	if terminal {
		if err := s.terminalizeWorkflowJobDeliveryFailure(ctx, ref, problems); err != nil {
			return err
		}
		if err := s.updateDeliveryWithProblems(ctx, row.DeliveryID, problems, func(q *store.Queries) error {
			return q.MarkDeliveryFailed(ctx, store.MarkDeliveryFailedParams{
				DeliveryID: row.DeliveryID,
				FailedAt:   pgTime(now),
			})
		}); err != nil {
			return err
		}
		s.writeEvent(ctx, githubEventFromMetadata(deliveryFailureMetadata(ref), "github.delivery.failed", "failed", problems.reason(), now, time.Now().UTC()))
		return nil
	}
	if err := s.updateDeliveryWithProblems(ctx, row.DeliveryID, problems, func(q *store.Queries) error {
		return q.MarkDeliveryRetryable(ctx, store.MarkDeliveryRetryableParams{
			DeliveryID:    row.DeliveryID,
			NextAttemptAt: pgTime(now.Add(retryDelay(row.AttemptCount))),
			UpdatedAt:     pgTime(now),
		})
	}); err != nil {
		return err
	}
	s.writeEvent(ctx, githubEventFromMetadata(deliveryFailureMetadata(ref), "github.delivery.retryable", "retryable", problems.reason(), now, time.Now().UTC()))
	return nil
}

func (s *Service) terminalizeWorkflowJobDeliveryFailure(ctx context.Context, ref workflowJobDeliveryFailureRef, problems webhookProblemSet) error {
	if ref.EventName != "workflow_job" || ref.ProviderJobID == 0 {
		return nil
	}
	enriched, err := s.enrichWorkflowJobDeliveryFailureRef(ctx, ref)
	if err != nil {
		return err
	}
	ref = enriched
	runnerProblems := runnerProblemSetFromWebhookProblems(problems)
	now := time.Now().UTC()
	tx, err := s.cfg.PG.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	qtx := store.New(tx)

	rows, err := qtx.MarkProviderDemandFailed(ctx, store.MarkProviderDemandFailedParams{
		State:         deliveryDemandFailureState(problems),
		UpdatedAt:     pgTime(now),
		ProviderJobID: ref.ProviderJobID,
	})
	if err != nil {
		return err
	}
	if rows > 0 {
		if err := appendProviderDemandProblems(ctx, qtx, ref.ProviderJobID, runnerProblems); err != nil {
			return err
		}
	}
	queued, err := enqueueProviderSurfaceCommandTx(ctx, qtx, runnerCapacityRef{
		OrgID:                  ref.OrgID,
		InstallationBindingID:  ref.InstallationBindingID,
		RepositoryBindingID:    ref.RepositoryBindingID,
		ProviderInstallationID: ref.ProviderInstallationID,
		ProviderRepositoryID:   ref.ProviderRepositoryID,
		RepositoryFullName:     ref.RepositoryFullName,
		ProviderRunID:          ref.ProviderRunID,
		ProviderRunAttempt:     ref.ProviderRunAttempt,
		ProviderJobID:          ref.ProviderJobID,
	}, runnerProblems, now)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	meta := deliveryFailureMetadata(ref)
	if rows > 0 {
		s.writeEvent(ctx, githubEventFromMetadata(meta, "github.job.demand.failed", "failed", runnerProblems.reason(), now, time.Now().UTC()))
	}
	if queued {
		s.writeEvent(ctx, githubEventFromMetadata(meta, "github.provider_surface.enqueued", "pending", runnerProblems.reason(), now, time.Now().UTC()))
	}
	return nil
}

func (s *Service) enrichWorkflowJobDeliveryFailureRef(ctx context.Context, ref workflowJobDeliveryFailureRef) (workflowJobDeliveryFailureRef, error) {
	if strings.TrimSpace(ref.OrgID) != "" || ref.ProviderInstallationID == 0 || ref.ProviderRepositoryID == 0 {
		return ref, nil
	}
	binding, err := s.lookupRuntimeBinding(ctx, ref.ProviderInstallationID, ref.ProviderRepositoryID)
	if errors.Is(err, ErrRepositoryNotEnabled) {
		return ref, nil
	}
	if err != nil {
		return ref, err
	}
	ref.OrgID = binding.OrgID
	ref.InstallationBindingID = binding.InstallationBindingID
	ref.RepositoryBindingID = binding.RepositoryBindingID
	return ref, nil
}

func runnerProblemSetFromWebhookProblems(problems webhookProblemSet) runnerProblemSet {
	out := runnerProblemSet{}
	for _, problem := range problems.problems {
		out.add(runnerProblem(problem))
	}
	return out
}

func deliveryDemandFailureState(problems webhookProblemSet) string {
	primary := problems.primary()
	switch primary.Code {
	case "github.sandbox_dispatch_failed":
		return "sandbox_failed"
	default:
		return "capacity_failed"
	}
}

func deliveryFailureRefFromLocked(row store.LockReadyDeliveriesRow) workflowJobDeliveryFailureRef {
	return workflowJobDeliveryFailureRef{
		EventName:              row.EventName,
		DeliveryID:             row.DeliveryID,
		ProviderInstallationID: row.ProviderInstallationID,
		ProviderRepositoryID:   row.ProviderRepositoryID,
		RepositoryFullName:     row.RepositoryFullName,
		ProviderRunID:          row.ProviderRunID,
		ProviderRunAttempt:     row.ProviderRunAttempt,
		ProviderJobID:          row.ProviderJobID,
	}
}

func deliveryFailureRefFromStale(row store.ListStaleProcessingDeliveriesRow) workflowJobDeliveryFailureRef {
	return workflowJobDeliveryFailureRef{
		EventName:              row.EventName,
		DeliveryID:             row.DeliveryID,
		ProviderInstallationID: row.ProviderInstallationID,
		ProviderRepositoryID:   row.ProviderRepositoryID,
		RepositoryFullName:     row.RepositoryFullName,
		ProviderRunID:          row.ProviderRunID,
		ProviderRunAttempt:     row.ProviderRunAttempt,
		ProviderJobID:          row.ProviderJobID,
	}
}

func deliveryFailureMetadata(ref workflowJobDeliveryFailureRef) webhookMetadata {
	return webhookMetadata{
		EventName:             ref.EventName,
		DeliveryID:            ref.DeliveryID,
		OrgID:                 ref.OrgID,
		InstallationBindingID: ref.InstallationBindingID,
		RepositoryBindingID:   ref.RepositoryBindingID,
		InstallationID:        ref.ProviderInstallationID,
		RepositoryID:          ref.ProviderRepositoryID,
		RepositoryFullName:    ref.RepositoryFullName,
		RunID:                 ref.ProviderRunID,
		RunAttempt:            ref.ProviderRunAttempt,
		JobID:                 ref.ProviderJobID,
	}
}
