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
	OrgID                  string
	Provider               string
	ProviderInstallationID int64
	ProviderRepositoryID   int64
	ProviderRunID          int64
	ProviderRunAttempt     int64
	ProviderJobID          int64
	WorkflowName           string
	JobName                string
	HeadSHA                string
	RepositoryFullName     string
	HeadBranch             string
	RunEventName           string
	RunHeadSHA             string
	RunHeadBranch          string
	RunHeadRepository      string
	RunBaseSHA             string
	RunBaseBranch          string
	WorkflowPath           string
	PullRequestNumber      int64
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
		ProviderRunAttempt:     row.ProviderRunAttempt,
		ProviderJobID:          row.ProviderJobID,
		WorkflowName:           row.WorkflowName,
		JobName:                row.JobName,
		HeadSHA:                row.HeadSha,
		RepositoryFullName:     row.RepositoryFullName,
		HeadBranch:             row.HeadBranch,
		RunEventName:           row.RunEventName,
		RunHeadSHA:             row.RunHeadSha,
		RunHeadBranch:          row.RunHeadBranch,
		RunHeadRepository:      row.RunHeadRepositoryFullName,
		RunBaseSHA:             row.RunBaseSha,
		RunBaseBranch:          row.RunBaseBranch,
		WorkflowPath:           row.WorkflowPath,
		PullRequestNumber:      row.PullRequestNumber,
		RunnerClass:            row.RunnerClass,
		RunnerName:             row.RunnerName,
	}, nil
}
