package jobs

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/verself/sandbox-rental-service/internal/store"
)

type GitHubExecutionIdentity struct {
	ExecutionID        uuid.UUID
	AttemptID          uuid.UUID
	AllocationID       uuid.UUID
	OrgID              string
	Installation       int64
	RepositoryID       int64
	RepositoryFullName string
	GitHubRunID        int64
	GitHubJobID        int64
	HeadBranch         string
	RunnerClass        string
	RunnerName         string
}

func (s *Service) AuthenticateCheckout(ctx context.Context, executionID, attemptID, bearer string) (GitHubExecutionIdentity, error) {
	executionID = strings.TrimSpace(executionID)
	attemptID = strings.TrimSpace(attemptID)
	bearer = strings.TrimSpace(strings.TrimPrefix(bearer, "Bearer "))
	execUUID, err := uuid.Parse(executionID)
	if err != nil {
		return GitHubExecutionIdentity{}, fmt.Errorf("%w: invalid execution id", ErrCheckoutUnauthorized)
	}
	attemptUUID, err := uuid.Parse(attemptID)
	if err != nil {
		return GitHubExecutionIdentity{}, fmt.Errorf("%w: invalid attempt id", ErrCheckoutUnauthorized)
	}
	return s.authenticateGitHubExecution(ctx, execUUID, attemptUUID, bearer, s.deriveScopedExecutionToken("verself-checkout", execUUID, attemptUUID), ErrCheckoutUnauthorized)
}

func (s *Service) authenticateGitHubExecution(ctx context.Context, executionID, attemptID uuid.UUID, bearer, expected string, unauthorizedErr error) (GitHubExecutionIdentity, error) {
	if bearer == "" || !hmac.Equal([]byte(bearer), []byte(expected)) {
		return GitHubExecutionIdentity{}, unauthorizedErr
	}

	row, err := s.storeQueries().GetProviderExecutionIdentity(ctx, store.GetProviderExecutionIdentityParams{
		Provider:    RunnerProviderGitHub,
		ExecutionID: &executionID,
		AttemptID:   &attemptID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return GitHubExecutionIdentity{}, unauthorizedErr
	}
	if err != nil {
		return GitHubExecutionIdentity{}, err
	}
	return GitHubExecutionIdentity{
		ExecutionID:        executionID,
		AttemptID:          attemptID,
		AllocationID:       row.AllocationID,
		OrgID:              orgIDFromDB(row.OrgID),
		Installation:       row.ProviderInstallationID,
		RepositoryID:       row.ProviderRepositoryID,
		RepositoryFullName: row.RepositoryFullName,
		GitHubRunID:        row.ProviderRunID,
		GitHubJobID:        row.ProviderJobID,
		HeadBranch:         row.HeadBranch,
		RunnerClass:        row.RunnerClass,
		RunnerName:         row.RunnerName,
	}, nil
}
