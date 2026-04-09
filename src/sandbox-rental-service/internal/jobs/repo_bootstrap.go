package jobs

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type RepoBootstrapRecord struct {
	Repo          *RepoRecord             `json:"repo"`
	Generation    *GoldenGenerationRecord `json:"generation"`
	ExecutionID   uuid.UUID               `json:"execution_id"`
	AttemptID     uuid.UUID               `json:"attempt_id"`
	TriggerReason string                  `json:"trigger_reason"`
}

func (s *Service) QueueRepoBootstrap(ctx context.Context, orgID uint64, actorID string, repoID uuid.UUID, triggerReason string) (*RepoBootstrapRecord, error) {
	repo, err := s.GetRepo(ctx, orgID, repoID)
	if err != nil {
		return nil, err
	}
	if repo.State == RepoStateArchived {
		return nil, fmt.Errorf("repo %s is archived", repoID)
	}
	if repo.CompatibilityStatus != CompatibilityStatusCompatible {
		return nil, ErrRepoNotReady
	}
	if strings.TrimSpace(repo.LastScannedSHA) == "" {
		repo, err = s.RescanRepo(ctx, orgID, repoID)
		if err != nil {
			return nil, err
		}
		if repo.CompatibilityStatus != CompatibilityStatusCompatible {
			return nil, ErrRepoNotReady
		}
	}

	triggerReason = firstNonEmpty(strings.TrimSpace(triggerReason), GenerationTriggerBootstrap)
	generation, err := s.CreateGoldenGeneration(ctx, repoID, CreateGoldenGenerationRequest{
		RunnerProfileSlug: repo.RunnerProfileSlug,
		SourceRef:         "refs/heads/" + repo.DefaultBranch,
		SourceSHA:         repo.LastScannedSHA,
		TriggerReason:     triggerReason,
	})
	if err != nil {
		return nil, err
	}

	req := SubmitRequest{
		Kind:               KindWarmGolden,
		ProductID:          defaultProductID,
		Provider:           repo.Provider,
		RepoID:             repo.RepoID.String(),
		Repo:               repo.FullName,
		RepoURL:            repo.CloneURL,
		DefaultBranch:      repo.DefaultBranch,
		GoldenGenerationID: &generation.GoldenGenerationID,
	}
	executionID, attemptID, err := s.Submit(ctx, orgID, actorID, req)
	if err != nil {
		_ = s.SetGoldenGenerationState(ctx, generation.GoldenGenerationID, GenerationStateFailed, "bootstrap_submit_failed", err.Error())
		return nil, err
	}
	if err := s.AttachGoldenGenerationExecution(ctx, generation.GoldenGenerationID, executionID, attemptID, attemptID.String()); err != nil {
		return nil, err
	}

	updatedRepo, repoErr := s.GetRepo(ctx, orgID, repoID)
	if repoErr != nil {
		return nil, repoErr
	}
	updatedGeneration, genErr := s.GetGoldenGeneration(ctx, repoID, generation.GoldenGenerationID)
	if genErr != nil {
		return nil, genErr
	}
	return &RepoBootstrapRecord{
		Repo:          updatedRepo,
		Generation:    updatedGeneration,
		ExecutionID:   executionID,
		AttemptID:     attemptID,
		TriggerReason: triggerReason,
	}, nil
}

func (s *Service) finalizeWarmGoldenGeneration(ctx context.Context, req SubmitRequest, outcome executionOutcome) error {
	if strings.TrimSpace(req.RepoID) == "" || req.GoldenGenerationID == nil {
		return nil
	}
	repoID, err := uuid.Parse(strings.TrimSpace(req.RepoID))
	if err != nil {
		return err
	}
	if outcome.State == StateSucceeded {
		if err := s.SetGoldenGenerationState(ctx, *req.GoldenGenerationID, GenerationStateSanitizing, "", ""); err != nil {
			return err
		}
		if err := s.ActivateGoldenGeneration(ctx, repoID, *req.GoldenGenerationID, outcome.GoldenSnapshot); err != nil {
			_ = s.SetGoldenGenerationState(ctx, *req.GoldenGenerationID, GenerationStateFailed, "activation_failed", err.Error())
			return err
		}
		return nil
	}
	return s.SetGoldenGenerationState(ctx, *req.GoldenGenerationID, GenerationStateFailed, firstNonEmpty(outcome.FailureReason, "warm_golden_failed"), firstNonEmpty(outcome.FailureReason, outcome.State))
}
