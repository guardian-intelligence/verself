package jobs

import (
	"context"

	"github.com/google/uuid"
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
	WorkflowName           string
	JobName                string
	HeadSHA                string
	RepositoryFullName     string
	HeadBranch             string
	RunnerClass            string
	RunnerName             string
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
		WorkflowName:           row.WorkflowName,
		JobName:                row.JobName,
		HeadSHA:                row.HeadSha,
		RepositoryFullName:     row.RepositoryFullName,
		HeadBranch:             row.HeadBranch,
		RunnerClass:            row.RunnerClass,
		RunnerName:             row.RunnerName,
	}, nil
}
