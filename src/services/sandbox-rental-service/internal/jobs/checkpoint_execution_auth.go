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

type RunnerExecutionIdentity struct {
	ExecutionID            uuid.UUID
	AttemptID              uuid.UUID
	AllocationID           uuid.UUID
	OrgID                  uint64
	Provider               string
	ProviderInstallationID int64
	ProviderRepositoryID   int64
	ProviderRunID          int64
	ProviderJobID          int64
	RepositoryFullName     string
	HeadBranch             string
	RunnerClass            string
	RunnerName             string
}

func (s *Service) AuthenticateCheckpoint(ctx context.Context, executionID, attemptID, bearer string) (RunnerExecutionIdentity, error) {
	executionID = strings.TrimSpace(executionID)
	attemptID = strings.TrimSpace(attemptID)
	bearer = strings.TrimSpace(strings.TrimPrefix(bearer, "Bearer "))
	execUUID, err := uuid.Parse(executionID)
	if err != nil {
		return RunnerExecutionIdentity{}, fmt.Errorf("%w: invalid execution id", ErrCheckpointUnauthorized)
	}
	attemptUUID, err := uuid.Parse(attemptID)
	if err != nil {
		return RunnerExecutionIdentity{}, fmt.Errorf("%w: invalid attempt id", ErrCheckpointUnauthorized)
	}
	identity, err := s.runnerExecutionIdentity(ctx, execUUID, attemptUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		return RunnerExecutionIdentity{}, ErrCheckpointUnauthorized
	}
	if err != nil {
		return RunnerExecutionIdentity{}, err
	}
	expected, err := s.deriveCheckpointToken(identity.Provider, execUUID, attemptUUID)
	if err != nil {
		return RunnerExecutionIdentity{}, err
	}
	if bearer == "" || !hmac.Equal([]byte(bearer), []byte(expected)) {
		return RunnerExecutionIdentity{}, ErrCheckpointUnauthorized
	}
	return identity, nil
}

func (s *Service) runnerExecutionIdentity(ctx context.Context, executionID, attemptID uuid.UUID) (RunnerExecutionIdentity, error) {
	row, err := s.storeQueries().GetRunnerExecutionIdentity(ctx, store.GetRunnerExecutionIdentityParams{
		ExecutionID: &executionID,
		AttemptID:   &attemptID,
	})
	if err != nil {
		return RunnerExecutionIdentity{}, err
	}
	return RunnerExecutionIdentity{
		ExecutionID:            executionID,
		AttemptID:              attemptID,
		AllocationID:           row.AllocationID,
		OrgID:                  orgIDFromDB(row.OrgID),
		Provider:               row.Provider,
		ProviderInstallationID: row.ProviderInstallationID,
		ProviderRepositoryID:   row.ProviderRepositoryID,
		ProviderRunID:          row.ProviderRunID,
		ProviderJobID:          row.ProviderJobID,
		RepositoryFullName:     row.RepositoryFullName,
		HeadBranch:             row.HeadBranch,
		RunnerClass:            row.RunnerClass,
		RunnerName:             row.RunnerName,
	}, nil
}

func (s *Service) deriveCheckpointToken(provider string, executionID, attemptID uuid.UUID) (string, error) {
	switch strings.TrimSpace(provider) {
	case RunnerProviderGitHub:
		if s.GitHubRunner == nil {
			return "", ErrGitHubRunnerNotConfigured
		}
		return s.GitHubRunner.deriveCheckpointToken(executionID, attemptID), nil
	case RunnerProviderForgejo:
		if s.ForgejoRunner == nil {
			return "", ErrForgejoRunnerNotConfigured
		}
		return s.ForgejoRunner.deriveCheckpointToken(executionID, attemptID), nil
	default:
		return "", fmt.Errorf("%w: unsupported runner provider %q", ErrRunnerUnavailable, provider)
	}
}
