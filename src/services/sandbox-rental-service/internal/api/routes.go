// Package api registers sandbox-rental-service HTTP routes on a Huma API.
package api

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	auth "github.com/verself/service-runtime/auth"

	"github.com/verself/sandbox-rental-service/internal/contractapi"
	"github.com/verself/sandbox-rental-service/internal/jobs"
	"github.com/verself/sandbox-rental-service/internal/recurring"
)

// RegisterRoutes wires all sandbox-rental-service endpoints onto the Huma API.
func RegisterRoutes(api huma.API, svc *jobs.Service, recurringSvc *recurring.Service, publicConfig PublicAPIConfig) {
	registerSandboxContractRoute(api, publicConfig.Authorizer, contractapi.BeginGithubInstallation, "Start GitHub App installation for the current org", beginGitHubInstallation(svc))
	registerSandboxContractRoute(api, publicConfig.Authorizer, contractapi.ListGithubInstallations, "List GitHub App installations for the current org", listGitHubInstallations(svc, publicConfig.InstallationID))
	registerSandboxContractRoute(api, publicConfig.Authorizer, contractapi.SyncGithubInstallationRepositories, "Sync GitHub App repositories into runner ownership", syncGitHubInstallationRepositories(svc))
	registerSandboxContractRoute(api, publicConfig.Authorizer, contractapi.GetExecution, "Get execution status and latest attempt", getExecution(svc, publicConfig.InstallationID))
	registerSandboxContractRoute(api, publicConfig.Authorizer, contractapi.GetExecutionLogs, "Get latest execution attempt log output", getExecutionLogs(svc))
	registerSandboxContractRoute(api, publicConfig.Authorizer, contractapi.ListRuns, "List CI and scheduled runs for the current org", listRuns(svc, publicConfig.InstallationID))
	registerSandboxContractRoute(api, publicConfig.Authorizer, contractapi.GetRun, "Get a CI or scheduled run", getRun(svc, publicConfig.InstallationID))
	registerSandboxContractRoute(api, publicConfig.Authorizer, contractapi.SearchRunLogs, "Search logs across CI and scheduled runs", searchRunLogs(svc))
	registerSandboxContractRoute(api, publicConfig.Authorizer, contractapi.GetJobsAnalytics, "Get run duration and success analytics", getJobsAnalytics(svc))
	registerSandboxContractRoute(api, publicConfig.Authorizer, contractapi.GetCostsAnalytics, "Get run cost analytics", getCostsAnalytics(svc))
	registerSandboxContractRoute(api, publicConfig.Authorizer, contractapi.GetRunnerSizingAnalytics, "Get runner sizing analytics", getRunnerSizingAnalytics(svc))
	registerSandboxContractRoute(api, publicConfig.Authorizer, contractapi.CreateExecutionSchedule, "Create a recurring execution schedule", createExecutionSchedule(recurringSvc, publicConfig.InstallationID))
	registerSandboxContractRoute(api, publicConfig.Authorizer, contractapi.ListExecutionSchedules, "List recurring execution schedules", listExecutionSchedules(recurringSvc, publicConfig.InstallationID))
	registerSandboxContractRoute(api, publicConfig.Authorizer, contractapi.GetExecutionSchedule, "Get a recurring execution schedule", getExecutionSchedule(recurringSvc, publicConfig.InstallationID))
	registerSandboxContractRoute(api, publicConfig.Authorizer, contractapi.PauseExecutionSchedule, "Pause a recurring execution schedule", pauseExecutionSchedule(recurringSvc, publicConfig.InstallationID))
	registerSandboxContractRoute(api, publicConfig.Authorizer, contractapi.ResumeExecutionSchedule, "Resume a recurring execution schedule", resumeExecutionSchedule(recurringSvc, publicConfig.InstallationID))
}

func requireIdentity(ctx context.Context) (*auth.Identity, error) {
	identity := auth.FromContext(ctx)
	if identity == nil {
		return nil, unauthorized(ctx)
	}
	return identity, nil
}

func requireOrgID(ctx context.Context) (uint64, error) {
	identity, err := requireIdentity(ctx)
	if err != nil {
		return 0, err
	}
	orgID, err := parseUnsignedDecimal(identity.OrgID)
	if err != nil {
		return 0, badRequest(ctx, "invalid-token-org", "token org_id must be an unsigned integer", err)
	}
	return orgID, nil
}

func beginGitHubInstallation(svc *jobs.Service) func(context.Context, *contractapi.BeginGithubInstallationInput) (*contractapi.BeginGithubInstallationOutput, error) {
	return func(ctx context.Context, _ *contractapi.BeginGithubInstallationInput) (*contractapi.BeginGithubInstallationOutput, error) {
		identity, err := requireIdentity(ctx)
		if err != nil {
			return nil, err
		}
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		if svc.GitHubRunner == nil || !svc.GitHubRunner.Configured() {
			return nil, serviceUnavailable(ctx, "github-runner-not-configured", "github runner is not configured", jobs.ErrGitHubRunnerNotConfigured)
		}
		connect, err := svc.GitHubRunner.BeginInstallation(ctx, orgID, identity.Subject)
		if err != nil {
			switch {
			case errors.Is(err, jobs.ErrGitHubRunnerNotConfigured):
				return nil, serviceUnavailable(ctx, "github-runner-not-configured", "github runner is not configured", err)
			case errors.Is(err, jobs.ErrGitHubInstallationInvalid):
				return nil, badRequest(ctx, "github-installation-invalid", "github installation must be an active organization installation", err)
			case errors.Is(err, jobs.ErrGitHubInstallationStateInvalid):
				return nil, badRequest(ctx, "github-installation-state-invalid", "github installation state is invalid", err)
			default:
				return nil, internalFailure(ctx, "github-installation-connect-failed", "start github installation failed", err)
			}
		}
		return &contractapi.BeginGithubInstallationOutput{Body: githubInstallationConnect(connect)}, nil
	}
}

func listGitHubInstallations(svc *jobs.Service, installationID string) func(context.Context, *contractapi.EmptyInput) (*contractapi.ListGithubInstallationsOutput, error) {
	return func(ctx context.Context, _ *contractapi.EmptyInput) (*contractapi.ListGithubInstallationsOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		records, err := svc.ListGitHubInstallations(ctx, orgID)
		if err != nil {
			return nil, internalFailure(ctx, "github-installation-list-failed", "list github installations failed", err)
		}
		return &contractapi.ListGithubInstallationsOutput{Body: githubInstallationRecords(records, installationID)}, nil
	}
}

func syncGitHubInstallationRepositories(svc *jobs.Service) func(context.Context, *contractapi.SyncGithubInstallationRepositoriesInput) (*contractapi.SyncGithubInstallationRepositoriesOutput, error) {
	return func(ctx context.Context, input *contractapi.SyncGithubInstallationRepositoriesInput) (*contractapi.SyncGithubInstallationRepositoriesOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		if svc == nil || svc.GitHubRunner == nil || !svc.GitHubRunner.Configured() {
			return nil, serviceUnavailable(ctx, "github-runner-not-configured", "github runner is not configured", jobs.ErrGitHubRunnerNotConfigured)
		}
		installationID, err := strconv.ParseInt(string(input.InstallationID), 10, 64)
		if err != nil || installationID <= 0 {
			return nil, badRequest(ctx, "invalid-github-installation-id", "installation_id must be a positive int64", err)
		}
		records, err := svc.GitHubRunner.SyncInstallationRepositories(ctx, orgID, installationID)
		if err != nil {
			switch {
			case errors.Is(err, jobs.ErrGitHubRunnerNotConfigured):
				return nil, serviceUnavailable(ctx, "github-runner-not-configured", "github runner is not configured", err)
			case errors.Is(err, jobs.ErrGitHubInstallationInvalid):
				return nil, badRequest(ctx, "github-installation-invalid", "github installation or repository selection is invalid", err)
			default:
				return nil, internalFailure(ctx, "github-installation-repository-sync-failed", "sync github installation repositories failed", err)
			}
		}
		return &contractapi.SyncGithubInstallationRepositoriesOutput{Body: githubInstallationRepositorySync(strconv.FormatInt(installationID, 10), records)}, nil
	}
}

func getExecution(svc *jobs.Service, installationID string) func(context.Context, *contractapi.ExecutionPathInput) (*contractapi.SandboxExecutionOutput, error) {
	return func(ctx context.Context, input *contractapi.ExecutionPathInput) (*contractapi.SandboxExecutionOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		executionID, err := uuid.Parse(string(input.ExecutionID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-execution-id", "execution_id must be a UUID", err)
		}

		execution, err := svc.GetExecution(ctx, orgID, executionID)
		if err != nil {
			if errors.Is(err, jobs.ErrExecutionMissing) {
				return nil, notFound(ctx, "execution-not-found", "execution not found")
			}
			return nil, internalFailure(ctx, "get-execution-failed", "get execution failed", err)
		}

		out := &contractapi.SandboxExecutionOutput{}
		out.Body = executionRecord(*execution, installationID)
		return out, nil
	}
}

func getExecutionLogs(svc *jobs.Service) func(context.Context, *contractapi.ExecutionPathInput) (*contractapi.SandboxExecutionLogsOutput, error) {
	return func(ctx context.Context, input *contractapi.ExecutionPathInput) (*contractapi.SandboxExecutionLogsOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		executionID, err := uuid.Parse(string(input.ExecutionID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-execution-id", "execution_id must be a UUID", err)
		}

		attemptID, logs, err := svc.GetExecutionLogs(ctx, orgID, executionID)
		if err != nil {
			if errors.Is(err, jobs.ErrExecutionMissing) {
				return nil, notFound(ctx, "execution-not-found", "execution not found")
			}
			return nil, internalFailure(ctx, "get-execution-logs-failed", "get execution logs failed", err)
		}

		out := &contractapi.SandboxExecutionLogsOutput{}
		out.Body = contractapi.SandboxExecutionLogs{
			ExecutionID: contractapi.ExecutionID(executionID.String()),
			AttemptID:   contractapi.AttemptID(attemptID.String()),
			Logs:        logs,
		}
		return out, nil
	}
}

func listRuns(svc *jobs.Service, installationID string) func(context.Context, *contractapi.ListRunsInput) (*contractapi.SandboxRunsPage, error) {
	return func(ctx context.Context, input *contractapi.ListRunsInput) (*contractapi.SandboxRunsPage, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		filters := jobs.RunListFilters{
			Limit:       int(input.Limit),
			Cursor:      string(input.Cursor),
			SourceKind:  string(input.SourceKind),
			Status:      string(input.Status),
			Repository:  string(input.Repository),
			Workflow:    string(input.Workflow),
			Branch:      string(input.Branch),
			RunnerClass: string(input.RunnerClass),
		}
		page, err := svc.ListRuns(ctx, orgID, filters)
		if err != nil {
			if errors.Is(err, jobs.ErrRunCursorInvalid) {
				return nil, badRequest(ctx, "invalid-run-cursor", "cursor must be a valid run pagination cursor", err)
			}
			return nil, internalFailure(ctx, "list-runs-failed", "list runs failed", err)
		}
		out := runPage(page, filters, installationID)
		return &out, nil
	}
}

func getRun(svc *jobs.Service, installationID string) func(context.Context, *contractapi.RunPathInput) (*contractapi.SandboxExecutionOutput, error) {
	return func(ctx context.Context, input *contractapi.RunPathInput) (*contractapi.SandboxExecutionOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		runID, err := uuid.Parse(string(input.RunID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-run-id", "run_id must be a UUID", err)
		}
		run, err := svc.GetRun(ctx, orgID, runID)
		if err != nil {
			if errors.Is(err, jobs.ErrExecutionMissing) {
				return nil, notFound(ctx, "run-not-found", "run not found")
			}
			return nil, internalFailure(ctx, "get-run-failed", "get run failed", err)
		}
		return &contractapi.SandboxExecutionOutput{Body: executionRecord(*run, installationID)}, nil
	}
}

func searchRunLogs(svc *jobs.Service) func(context.Context, *contractapi.SearchRunLogsInput) (*contractapi.SandboxRunLogSearchPage, error) {
	return func(ctx context.Context, input *contractapi.SearchRunLogsInput) (*contractapi.SandboxRunLogSearchPage, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		filters := jobs.RunLogSearchFilters{
			Limit:       int(input.Limit),
			Cursor:      string(input.Cursor),
			Query:       string(input.Query),
			SourceKind:  string(input.SourceKind),
			Repository:  string(input.Repository),
			Workflow:    string(input.Workflow),
			Branch:      string(input.Branch),
			RunnerClass: string(input.RunnerClass),
		}
		if input.RunID != "" {
			filters.ExecutionID, err = uuid.Parse(string(input.RunID))
			if err != nil {
				return nil, badRequest(ctx, "invalid-run-id", "run_id must be a UUID", err)
			}
		}
		if input.AttemptID != "" {
			filters.AttemptID, err = uuid.Parse(string(input.AttemptID))
			if err != nil {
				return nil, badRequest(ctx, "invalid-attempt-id", "attempt_id must be a UUID", err)
			}
		}
		page, err := svc.SearchRunLogs(ctx, orgID, filters)
		if err != nil {
			if errors.Is(err, jobs.ErrRunLogCursorInvalid) {
				return nil, badRequest(ctx, "invalid-run-log-cursor", "cursor must be a valid run log pagination cursor", err)
			}
			return nil, internalFailure(ctx, "search-run-logs-failed", "search run logs failed", err)
		}
		out := runLogSearchPage(page, filters)
		return &out, nil
	}
}

func getJobsAnalytics(svc *jobs.Service) func(context.Context, *contractapi.AnalyticsWindowInput) (*contractapi.SandboxJobsAnalyticsOutput, error) {
	return func(ctx context.Context, input *contractapi.AnalyticsWindowInput) (*contractapi.SandboxJobsAnalyticsOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		window, err := analyticsWindowInput(ctx, input)
		if err != nil {
			return nil, err
		}
		analytics, err := svc.GetJobsAnalytics(ctx, orgID, window)
		if err != nil {
			return nil, internalFailure(ctx, "get-jobs-analytics-failed", "get jobs analytics failed", err)
		}
		return &contractapi.SandboxJobsAnalyticsOutput{Body: jobsAnalytics(analytics)}, nil
	}
}

func getCostsAnalytics(svc *jobs.Service) func(context.Context, *contractapi.AnalyticsWindowInput) (*contractapi.SandboxCostsAnalyticsOutput, error) {
	return func(ctx context.Context, input *contractapi.AnalyticsWindowInput) (*contractapi.SandboxCostsAnalyticsOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		window, err := analyticsWindowInput(ctx, input)
		if err != nil {
			return nil, err
		}
		analytics, err := svc.GetCostsAnalytics(ctx, orgID, window)
		if err != nil {
			return nil, internalFailure(ctx, "get-costs-analytics-failed", "get costs analytics failed", err)
		}
		return &contractapi.SandboxCostsAnalyticsOutput{Body: costsAnalytics(analytics)}, nil
	}
}

func getRunnerSizingAnalytics(svc *jobs.Service) func(context.Context, *contractapi.AnalyticsWindowInput) (*contractapi.SandboxRunnerSizingAnalyticsOutput, error) {
	return func(ctx context.Context, input *contractapi.AnalyticsWindowInput) (*contractapi.SandboxRunnerSizingAnalyticsOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		window, err := analyticsWindowInput(ctx, input)
		if err != nil {
			return nil, err
		}
		analytics, err := svc.GetRunnerSizingAnalytics(ctx, orgID, window)
		if err != nil {
			return nil, internalFailure(ctx, "get-runner-sizing-analytics-failed", "get runner sizing analytics failed", err)
		}
		return &contractapi.SandboxRunnerSizingAnalyticsOutput{Body: runnerSizingAnalytics(analytics)}, nil
	}
}

func createExecutionSchedule(recurringSvc *recurring.Service, installationID string) func(context.Context, *contractapi.CreateExecutionScheduleInput) (*contractapi.SandboxExecutionScheduleOutput, error) {
	return func(ctx context.Context, input *contractapi.CreateExecutionScheduleInput) (*contractapi.SandboxExecutionScheduleOutput, error) {
		identity, err := requireIdentity(ctx)
		if err != nil {
			return nil, err
		}
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		request, err := executionScheduleCreateRequest(ctx, input.Body)
		if err != nil {
			return nil, err
		}
		record, err := recurringSvc.CreateSchedule(ctx, orgID, identity.Subject, request)
		if err != nil {
			if errors.Is(err, recurring.ErrInvalid) {
				return nil, badRequest(ctx, "invalid-execution-schedule", err.Error(), err)
			}
			if errors.Is(err, recurring.ErrConflict) {
				return nil, conflict(ctx, "execution-schedule-conflict", "execution schedule idempotency key conflicts with an existing schedule")
			}
			return nil, internalFailure(ctx, "create-execution-schedule-failed", "create execution schedule failed", err)
		}
		return &contractapi.SandboxExecutionScheduleOutput{Body: executionScheduleRecord(record, installationID)}, nil
	}
}

func listExecutionSchedules(recurringSvc *recurring.Service, installationID string) func(context.Context, *contractapi.ListExecutionSchedulesInput) (*contractapi.SandboxExecutionSchedulesPage, error) {
	return func(ctx context.Context, input *contractapi.ListExecutionSchedulesInput) (*contractapi.SandboxExecutionSchedulesPage, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		page, err := recurringSvc.ListSchedules(ctx, orgID, recurring.ScheduleListFilters{
			Limit:  int(input.Limit),
			Cursor: string(input.Cursor),
		})
		if err != nil {
			if errors.Is(err, recurring.ErrScheduleCursorInvalid) {
				return nil, badRequest(ctx, "invalid-schedule-cursor", "cursor must be a valid execution schedule pagination cursor", err)
			}
			return nil, internalFailure(ctx, "list-execution-schedules-failed", "list execution schedules failed", err)
		}
		out := executionSchedulePage(page, installationID)
		return &out, nil
	}
}

func getExecutionSchedule(recurringSvc *recurring.Service, installationID string) func(context.Context, *contractapi.ExecutionSchedulePathInput) (*contractapi.SandboxExecutionScheduleOutput, error) {
	return func(ctx context.Context, input *contractapi.ExecutionSchedulePathInput) (*contractapi.SandboxExecutionScheduleOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		scheduleID, err := uuid.Parse(string(input.ScheduleID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-schedule-id", "schedule_id must be a UUID", err)
		}
		record, err := recurringSvc.GetSchedule(ctx, orgID, scheduleID)
		if err != nil {
			if errors.Is(err, recurring.ErrScheduleMissing) {
				return nil, notFound(ctx, "execution-schedule-not-found", "execution schedule not found")
			}
			return nil, internalFailure(ctx, "get-execution-schedule-failed", "get execution schedule failed", err)
		}
		return &contractapi.SandboxExecutionScheduleOutput{Body: executionScheduleRecord(*record, installationID)}, nil
	}
}

func pauseExecutionSchedule(recurringSvc *recurring.Service, installationID string) func(context.Context, *contractapi.ExecutionScheduleMutationInput) (*contractapi.SandboxExecutionScheduleOutput, error) {
	return func(ctx context.Context, input *contractapi.ExecutionScheduleMutationInput) (*contractapi.SandboxExecutionScheduleOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		scheduleID, err := uuid.Parse(string(input.ScheduleID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-schedule-id", "schedule_id must be a UUID", err)
		}
		record, err := recurringSvc.PauseSchedule(ctx, orgID, scheduleID)
		if err != nil {
			if errors.Is(err, recurring.ErrScheduleMissing) {
				return nil, notFound(ctx, "execution-schedule-not-found", "execution schedule not found")
			}
			return nil, internalFailure(ctx, "pause-execution-schedule-failed", "pause execution schedule failed", err)
		}
		return &contractapi.SandboxExecutionScheduleOutput{Body: executionScheduleRecord(*record, installationID)}, nil
	}
}

func resumeExecutionSchedule(recurringSvc *recurring.Service, installationID string) func(context.Context, *contractapi.ExecutionScheduleMutationInput) (*contractapi.SandboxExecutionScheduleOutput, error) {
	return func(ctx context.Context, input *contractapi.ExecutionScheduleMutationInput) (*contractapi.SandboxExecutionScheduleOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		scheduleID, err := uuid.Parse(string(input.ScheduleID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-schedule-id", "schedule_id must be a UUID", err)
		}
		record, err := recurringSvc.ResumeSchedule(ctx, orgID, scheduleID)
		if err != nil {
			if errors.Is(err, recurring.ErrScheduleMissing) {
				return nil, notFound(ctx, "execution-schedule-not-found", "execution schedule not found")
			}
			return nil, internalFailure(ctx, "resume-execution-schedule-failed", "resume execution schedule failed", err)
		}
		return &contractapi.SandboxExecutionScheduleOutput{Body: executionScheduleRecord(*record, installationID)}, nil
	}
}

func analyticsWindowInput(ctx context.Context, input *contractapi.AnalyticsWindowInput) (jobs.AnalyticsWindow, error) {
	var window jobs.AnalyticsWindow
	if input == nil {
		return window, nil
	}
	var err error
	if input.Start != "" {
		window.Start, err = time.Parse(time.RFC3339, input.Start)
		if err != nil {
			return jobs.AnalyticsWindow{}, badRequest(ctx, "invalid-window-start", "start must be an RFC3339 timestamp", err)
		}
	}
	if input.End != "" {
		window.End, err = time.Parse(time.RFC3339, input.End)
		if err != nil {
			return jobs.AnalyticsWindow{}, badRequest(ctx, "invalid-window-end", "end must be an RFC3339 timestamp", err)
		}
	}
	if !window.Start.IsZero() && !window.End.IsZero() && window.End.Before(window.Start) {
		return jobs.AnalyticsWindow{}, badRequest(ctx, "invalid-window-range", "end must be greater than or equal to start", nil)
	}
	return window, nil
}
