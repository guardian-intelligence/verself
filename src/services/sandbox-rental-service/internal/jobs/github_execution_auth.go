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
	OrgID              uint64
	Installation       int64
	RepositoryID       int64
	RepositoryFullName string
	GitHubJobID        int64
	RunnerName         string
}

func (r *GitHubRunner) AuthenticateCheckout(ctx context.Context, executionID, attemptID, bearer string) (GitHubExecutionIdentity, error) {
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
	return r.authenticateGitHubExecution(ctx, execUUID, attemptUUID, bearer, r.deriveCheckoutToken(execUUID, attemptUUID), ErrCheckoutUnauthorized)
}

func (r *GitHubRunner) authenticateGitHubExecution(ctx context.Context, executionID, attemptID uuid.UUID, bearer, expected string, unauthorizedErr error) (GitHubExecutionIdentity, error) {
	if bearer == "" || !hmac.Equal([]byte(bearer), []byte(expected)) {
		return GitHubExecutionIdentity{}, unauthorizedErr
	}

	row, err := r.service.storeQueries().GetGitHubExecutionIdentity(ctx, store.GetGitHubExecutionIdentityParams{
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
		GitHubJobID:        row.ProviderJobID,
		RunnerName:         row.RunnerName,
	}, nil
}
