// Package api registers sandbox-rental-service HTTP routes on a Huma API.
package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/verself/domain-transfer-objects"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"

	"github.com/verself/sandbox-rental-service/internal/jobs"
	"github.com/verself/sandbox-rental-service/internal/recurring"
)

// RegisterRoutes wires all sandbox-rental-service endpoints onto the Huma API.
func RegisterRoutes(api huma.API, svc *jobs.Service, recurringSvc *recurring.Service, publicConfig PublicAPIConfig) {
	registerSecured(api, publicConfig.Authorizer, secured(huma.Operation{
		OperationID:   "begin-github-installation",
		Method:        http.MethodPost,
		Path:          "/api/v1/github/installations/connect",
		Summary:       "Start GitHub App installation for the current org",
		DefaultStatus: 201,
	}, runtimeiam.OperationPolicy{
		Permission:     permissionGitHubWrite,
		Resource:       "github_installation",
		Action:         runtimeiam.ActionConnect,
		OrgScope:       runtimeiam.OrgScopeTokenOrgID,
		RateLimitClass: "github_installation_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "sandbox.github_installation.connect",
		BodyLimitBytes: bodyLimitNoBody,
	}), beginGitHubInstallation(svc))

	registerSecured(api, publicConfig.Authorizer, secured(huma.Operation{
		OperationID: "list-github-installations",
		Method:      http.MethodGet,
		Path:        "/api/v1/github/installations",
		Summary:     "List GitHub App installations for the current org",
	}, runtimeiam.OperationPolicy{
		Permission:     permissionGitHubRead,
		Resource:       "github_installation",
		Action:         runtimeiam.ActionList,
		OrgScope:       runtimeiam.OrgScopeTokenOrgID,
		RateLimitClass: "read",
		AuditEvent:     "sandbox.github_installation.list",
	}), listGitHubInstallations(svc, publicConfig.InstallationID))

	registerSecured(api, publicConfig.Authorizer, secured(huma.Operation{
		OperationID:   "sync-github-installation-repositories",
		Method:        http.MethodPost,
		Path:          "/api/v1/github/installations/{installation_id}/repositories/sync",
		Summary:       "Sync GitHub App repositories into runner ownership",
		DefaultStatus: 200,
	}, runtimeiam.OperationPolicy{
		Permission:     permissionGitHubWrite,
		Resource:       "github_installation_repository",
		Action:         runtimeiam.ActionSync,
		OrgScope:       runtimeiam.OrgScopeTokenOrgID,
		RateLimitClass: "github_installation_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "sandbox.github_installation.repositories.sync",
		BodyLimitBytes: bodyLimitNoBody,
	}), syncGitHubInstallationRepositories(svc))

	registerSecured(api, publicConfig.Authorizer, secured(huma.Operation{
		OperationID: "get-execution",
		Method:      http.MethodGet,
		Path:        "/api/v1/executions/{execution_id}",
		Summary:     "Get execution status and latest attempt",
	}, runtimeiam.OperationPolicy{
		Permission:     permissionExecutionRead,
		Resource:       "execution",
		Action:         runtimeiam.ActionRead,
		OrgScope:       runtimeiam.OrgScopeTokenOrgID,
		RateLimitClass: "read",
		AuditEvent:     "sandbox.execution.read",
	}), getExecution(svc, publicConfig.InstallationID))

	registerSecured(api, publicConfig.Authorizer, secured(huma.Operation{
		OperationID: "get-execution-logs",
		Method:      http.MethodGet,
		Path:        "/api/v1/executions/{execution_id}/logs",
		Summary:     "Get latest execution attempt log output",
	}, runtimeiam.OperationPolicy{
		Permission:     permissionLogsRead,
		Resource:       "execution_logs",
		Action:         runtimeiam.ActionRead,
		OrgScope:       runtimeiam.OrgScopeTokenOrgID,
		RateLimitClass: "logs_read",
		AuditEvent:     "sandbox.execution.logs.read",
	}), getExecutionLogs(svc))

	registerSecured(api, publicConfig.Authorizer, secured(huma.Operation{
		OperationID: "list-runs",
		Method:      http.MethodGet,
		Path:        "/api/v1/runs",
		Summary:     "List CI and scheduled runs for the current org",
	}, runtimeiam.OperationPolicy{
		Permission:     permissionExecutionRead,
		Resource:       "run",
		Action:         runtimeiam.ActionList,
		OrgScope:       runtimeiam.OrgScopeTokenOrgID,
		RateLimitClass: "read",
		AuditEvent:     "sandbox.run.list",
	}), listRuns(svc, publicConfig.InstallationID))

	registerSecured(api, publicConfig.Authorizer, secured(huma.Operation{
		OperationID: "get-run",
		Method:      http.MethodGet,
		Path:        "/api/v1/runs/{run_id}",
		Summary:     "Get a CI or scheduled run",
	}, runtimeiam.OperationPolicy{
		Permission:     permissionExecutionRead,
		Resource:       "run",
		Action:         runtimeiam.ActionRead,
		OrgScope:       runtimeiam.OrgScopeTokenOrgID,
		RateLimitClass: "read",
		AuditEvent:     "sandbox.run.read",
	}), getRun(svc, publicConfig.InstallationID))

	registerSecured(api, publicConfig.Authorizer, secured(huma.Operation{
		OperationID: "search-run-logs",
		Method:      http.MethodGet,
		Path:        "/api/v1/run-logs/search",
		Summary:     "Search logs across CI and scheduled runs",
	}, runtimeiam.OperationPolicy{
		Permission:     permissionLogsRead,
		Resource:       "run_logs",
		Action:         runtimeiam.ActionSearch,
		OrgScope:       runtimeiam.OrgScopeTokenOrgID,
		RateLimitClass: "logs_read",
		AuditEvent:     "sandbox.run_logs.search",
	}), searchRunLogs(svc))

	registerSecured(api, publicConfig.Authorizer, secured(huma.Operation{
		OperationID: "get-jobs-analytics",
		Method:      http.MethodGet,
		Path:        "/api/v1/run-analytics/jobs",
		Summary:     "Get run duration and success analytics",
	}, runtimeiam.OperationPolicy{
		Permission:     permissionAnalyticsRead,
		Resource:       "run_analytics_jobs",
		Action:         runtimeiam.ActionRead,
		OrgScope:       runtimeiam.OrgScopeTokenOrgID,
		RateLimitClass: "read",
		AuditEvent:     "sandbox.run_analytics.jobs.read",
	}), getJobsAnalytics(svc))

	registerSecured(api, publicConfig.Authorizer, secured(huma.Operation{
		OperationID: "get-costs-analytics",
		Method:      http.MethodGet,
		Path:        "/api/v1/run-analytics/costs",
		Summary:     "Get run cost analytics",
	}, runtimeiam.OperationPolicy{
		Permission:     permissionAnalyticsRead,
		Resource:       "run_analytics_costs",
		Action:         runtimeiam.ActionRead,
		OrgScope:       runtimeiam.OrgScopeTokenOrgID,
		RateLimitClass: "read",
		AuditEvent:     "sandbox.run_analytics.costs.read",
	}), getCostsAnalytics(svc))

	registerSecured(api, publicConfig.Authorizer, secured(huma.Operation{
		OperationID: "get-runner-sizing-analytics",
		Method:      http.MethodGet,
		Path:        "/api/v1/run-analytics/runner-sizing",
		Summary:     "Get runner sizing analytics",
	}, runtimeiam.OperationPolicy{
		Permission:     permissionAnalyticsRead,
		Resource:       "run_analytics_runner_sizing",
		Action:         runtimeiam.ActionRead,
		OrgScope:       runtimeiam.OrgScopeTokenOrgID,
		RateLimitClass: "read",
		AuditEvent:     "sandbox.run_analytics.runner_sizing.read",
	}), getRunnerSizingAnalytics(svc))

	registerSecured(api, publicConfig.Authorizer, secured(huma.Operation{
		OperationID:   "create-execution-schedule",
		Method:        http.MethodPost,
		Path:          "/api/v1/execution-schedules",
		Summary:       "Create a recurring execution schedule",
		DefaultStatus: 201,
	}, runtimeiam.OperationPolicy{
		Permission:     permissionScheduleWrite,
		Resource:       "execution_schedule",
		Action:         runtimeiam.ActionCreate,
		OrgScope:       runtimeiam.OrgScopeTokenOrgID,
		RateLimitClass: "execution_schedule_mutation",
		Idempotency:    idempotencyRequestBodyKey,
		AuditEvent:     "sandbox.execution_schedule.create",
		BodyLimitBytes: bodyLimitSmallJSON,
	}), createExecutionSchedule(recurringSvc, publicConfig.InstallationID))

	registerSecured(api, publicConfig.Authorizer, secured(huma.Operation{
		OperationID: "list-execution-schedules",
		Method:      http.MethodGet,
		Path:        "/api/v1/execution-schedules",
		Summary:     "List recurring execution schedules",
	}, runtimeiam.OperationPolicy{
		Permission:     permissionScheduleRead,
		Resource:       "execution_schedule",
		Action:         runtimeiam.ActionList,
		OrgScope:       runtimeiam.OrgScopeTokenOrgID,
		RateLimitClass: "read",
		AuditEvent:     "sandbox.execution_schedule.list",
	}), listExecutionSchedules(recurringSvc, publicConfig.InstallationID))

	registerSecured(api, publicConfig.Authorizer, secured(huma.Operation{
		OperationID: "get-execution-schedule",
		Method:      http.MethodGet,
		Path:        "/api/v1/execution-schedules/{schedule_id}",
		Summary:     "Get a recurring execution schedule",
	}, runtimeiam.OperationPolicy{
		Permission:     permissionScheduleRead,
		Resource:       "execution_schedule",
		Action:         runtimeiam.ActionRead,
		OrgScope:       runtimeiam.OrgScopeTokenOrgID,
		RateLimitClass: "read",
		AuditEvent:     "sandbox.execution_schedule.read",
	}), getExecutionSchedule(recurringSvc, publicConfig.InstallationID))

	registerSecured(api, publicConfig.Authorizer, secured(huma.Operation{
		OperationID:   "pause-execution-schedule",
		Method:        http.MethodPost,
		Path:          "/api/v1/execution-schedules/{schedule_id}/pause",
		Summary:       "Pause a recurring execution schedule",
		DefaultStatus: 200,
	}, runtimeiam.OperationPolicy{
		Permission:     permissionScheduleWrite,
		Resource:       "execution_schedule",
		Action:         runtimeiam.ActionPause,
		OrgScope:       runtimeiam.OrgScopeTokenOrgID,
		RateLimitClass: "execution_schedule_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "sandbox.execution_schedule.pause",
		BodyLimitBytes: bodyLimitNoBody,
	}), pauseExecutionSchedule(recurringSvc, publicConfig.InstallationID))

	registerSecured(api, publicConfig.Authorizer, secured(huma.Operation{
		OperationID:   "resume-execution-schedule",
		Method:        http.MethodPost,
		Path:          "/api/v1/execution-schedules/{schedule_id}/resume",
		Summary:       "Resume a recurring execution schedule",
		DefaultStatus: 200,
	}, runtimeiam.OperationPolicy{
		Permission:     permissionScheduleWrite,
		Resource:       "execution_schedule",
		Action:         runtimeiam.ActionResume,
		OrgScope:       runtimeiam.OrgScopeTokenOrgID,
		RateLimitClass: "execution_schedule_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "sandbox.execution_schedule.resume",
		BodyLimitBytes: bodyLimitNoBody,
	}), resumeExecutionSchedule(recurringSvc, publicConfig.InstallationID))

}

type GitHubInstallationConnectOutput struct {
	Body dto.SandboxGitHubInstallationConnectResponse
}

type ListGitHubInstallationsOutput struct {
	Body []dto.SandboxGitHubInstallationRecord
}

type SyncGitHubInstallationRepositoriesInput struct {
	InstallationID string `path:"installation_id" required:"true"`
}

type SyncGitHubInstallationRepositoriesOutput struct {
	Body GitHubInstallationRepositorySync
}

type GitHubInstallationRepositorySync struct {
	InstallationID string                         `json:"installation_id"`
	SyncedAt       time.Time                      `json:"synced_at"`
	Repositories   []GitHubInstallationRepository `json:"repositories"`
}

type GitHubInstallationRepository struct {
	ProviderRepositoryID string    `json:"provider_repository_id"`
	ProviderOwner        string    `json:"provider_owner"`
	ProviderRepo         string    `json:"provider_repo"`
	RepositoryFullName   string    `json:"repository_full_name"`
	Private              bool      `json:"private"`
	Active               bool      `json:"active"`
	SyncedAt             time.Time `json:"synced_at"`
}

type ExecutionIDPath struct {
	ExecutionID string `path:"execution_id" doc:"Execution UUID"`
}

type GetExecutionOutput struct {
	Body dto.SandboxExecutionRecord
}

type GetExecutionLogsOutput struct {
	Body dto.SandboxExecutionLogs
}

type RunIDPath struct {
	RunID string `path:"run_id" doc:"Run UUID"`
}

type RunsInput struct {
	Limit       int    `query:"limit,omitempty" minimum:"1" maximum:"200" doc:"Maximum runs to return."`
	Cursor      string `query:"cursor,omitempty" maxLength:"128" doc:"Opaque pagination cursor returned by the previous page."`
	SourceKind  string `query:"source_kind,omitempty" maxLength:"64"`
	Status      string `query:"status,omitempty" maxLength:"64"`
	Repository  string `query:"repository,omitempty" maxLength:"255"`
	Workflow    string `query:"workflow,omitempty" maxLength:"255"`
	Branch      string `query:"branch,omitempty" maxLength:"255"`
	RunnerClass string `query:"runner_class,omitempty" maxLength:"255"`
}

type RunsOutput struct {
	Body dto.SandboxRunsPage
}

type RunOutput struct {
	Body dto.SandboxExecutionRecord
}

type RunLogSearchInput struct {
	Limit       int    `query:"limit,omitempty" minimum:"1" maximum:"500" doc:"Maximum log matches to return."`
	Cursor      string `query:"cursor,omitempty" maxLength:"160" doc:"Opaque pagination cursor returned by the previous page."`
	Query       string `query:"query,omitempty" maxLength:"2048" doc:"Case-insensitive substring to search for."`
	RunID       string `query:"run_id,omitempty" doc:"Filter to a specific run UUID."`
	AttemptID   string `query:"attempt_id,omitempty" doc:"Filter to a specific attempt UUID."`
	SourceKind  string `query:"source_kind,omitempty" maxLength:"64"`
	Repository  string `query:"repository,omitempty" maxLength:"255"`
	Workflow    string `query:"workflow,omitempty" maxLength:"255"`
	Branch      string `query:"branch,omitempty" maxLength:"255"`
	RunnerClass string `query:"runner_class,omitempty" maxLength:"255"`
}

type RunLogSearchOutput struct {
	Body dto.SandboxRunLogSearchPage
}

type AnalyticsWindowInput struct {
	Start string `query:"start,omitempty" format:"date-time" doc:"Inclusive RFC3339 window start."`
	End   string `query:"end,omitempty" format:"date-time" doc:"Inclusive RFC3339 window end."`
}

type JobsAnalyticsOutput struct {
	Body dto.SandboxJobsAnalytics
}

type CostsAnalyticsOutput struct {
	Body dto.SandboxCostsAnalytics
}

type RunnerSizingAnalyticsOutput struct {
	Body dto.SandboxRunnerSizingAnalytics
}

type ExecutionScheduleIDPath struct {
	ScheduleID string `path:"schedule_id" doc:"Execution schedule UUID"`
}

type CreateExecutionScheduleInput struct {
	Body dto.SandboxExecutionScheduleCreateRequest
}

type ExecutionScheduleOutput struct {
	Body dto.SandboxExecutionScheduleRecord
}

type ListExecutionSchedulesOutput struct {
	Body []dto.SandboxExecutionScheduleRecord
}

type EmptyInput struct{}

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
	orgID, err := dto.ParseUint64(identity.OrgID)
	if err != nil {
		return 0, badRequest(ctx, "invalid-token-org", "token org_id must be an unsigned integer", err)
	}
	return orgID, nil
}

func beginGitHubInstallation(svc *jobs.Service) func(context.Context, *EmptyInput) (*GitHubInstallationConnectOutput, error) {
	return func(ctx context.Context, _ *EmptyInput) (*GitHubInstallationConnectOutput, error) {
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
		return &GitHubInstallationConnectOutput{Body: githubInstallationConnect(connect)}, nil
	}
}

func listGitHubInstallations(svc *jobs.Service, installationID string) func(context.Context, *EmptyInput) (*ListGitHubInstallationsOutput, error) {
	return func(ctx context.Context, _ *EmptyInput) (*ListGitHubInstallationsOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		records, err := svc.ListGitHubInstallations(ctx, orgID)
		if err != nil {
			return nil, internalFailure(ctx, "github-installation-list-failed", "list github installations failed", err)
		}
		return &ListGitHubInstallationsOutput{Body: githubInstallationRecords(records, installationID)}, nil
	}
}

func syncGitHubInstallationRepositories(svc *jobs.Service) func(context.Context, *SyncGitHubInstallationRepositoriesInput) (*SyncGitHubInstallationRepositoriesOutput, error) {
	return func(ctx context.Context, input *SyncGitHubInstallationRepositoriesInput) (*SyncGitHubInstallationRepositoriesOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		if svc == nil || svc.GitHubRunner == nil || !svc.GitHubRunner.Configured() {
			return nil, serviceUnavailable(ctx, "github-runner-not-configured", "github runner is not configured", jobs.ErrGitHubRunnerNotConfigured)
		}
		installationID, err := strconv.ParseInt(input.InstallationID, 10, 64)
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
		return &SyncGitHubInstallationRepositoriesOutput{Body: githubInstallationRepositorySync(strconv.FormatInt(installationID, 10), records)}, nil
	}
}

func getExecution(svc *jobs.Service, installationID string) func(context.Context, *ExecutionIDPath) (*GetExecutionOutput, error) {
	return func(ctx context.Context, input *ExecutionIDPath) (*GetExecutionOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		executionID, err := uuid.Parse(input.ExecutionID)
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

		out := &GetExecutionOutput{}
		out.Body = executionRecord(*execution, installationID)
		return out, nil
	}
}

func getExecutionLogs(svc *jobs.Service) func(context.Context, *ExecutionIDPath) (*GetExecutionLogsOutput, error) {
	return func(ctx context.Context, input *ExecutionIDPath) (*GetExecutionLogsOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		executionID, err := uuid.Parse(input.ExecutionID)
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

		out := &GetExecutionLogsOutput{}
		out.Body = dto.SandboxExecutionLogs{
			ExecutionID: executionID.String(),
			AttemptID:   attemptID.String(),
			Logs:        logs,
		}
		return out, nil
	}
}

func listRuns(svc *jobs.Service, installationID string) func(context.Context, *RunsInput) (*RunsOutput, error) {
	return func(ctx context.Context, input *RunsInput) (*RunsOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		filters := jobs.RunListFilters{
			Limit:       input.Limit,
			Cursor:      input.Cursor,
			SourceKind:  input.SourceKind,
			Status:      input.Status,
			Repository:  input.Repository,
			Workflow:    input.Workflow,
			Branch:      input.Branch,
			RunnerClass: input.RunnerClass,
		}
		page, err := svc.ListRuns(ctx, orgID, filters)
		if err != nil {
			if errors.Is(err, jobs.ErrRunCursorInvalid) {
				return nil, badRequest(ctx, "invalid-run-cursor", "cursor must be a valid run pagination cursor", err)
			}
			return nil, internalFailure(ctx, "list-runs-failed", "list runs failed", err)
		}
		return &RunsOutput{Body: runPage(page, filters, installationID)}, nil
	}
}

func getRun(svc *jobs.Service, installationID string) func(context.Context, *RunIDPath) (*RunOutput, error) {
	return func(ctx context.Context, input *RunIDPath) (*RunOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		runID, err := uuid.Parse(input.RunID)
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
		return &RunOutput{Body: executionRecord(*run, installationID)}, nil
	}
}

func searchRunLogs(svc *jobs.Service) func(context.Context, *RunLogSearchInput) (*RunLogSearchOutput, error) {
	return func(ctx context.Context, input *RunLogSearchInput) (*RunLogSearchOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		filters := jobs.RunLogSearchFilters{
			Limit:       input.Limit,
			Cursor:      input.Cursor,
			Query:       input.Query,
			SourceKind:  input.SourceKind,
			Repository:  input.Repository,
			Workflow:    input.Workflow,
			Branch:      input.Branch,
			RunnerClass: input.RunnerClass,
		}
		if input.RunID != "" {
			filters.ExecutionID, err = uuid.Parse(input.RunID)
			if err != nil {
				return nil, badRequest(ctx, "invalid-run-id", "run_id must be a UUID", err)
			}
		}
		if input.AttemptID != "" {
			filters.AttemptID, err = uuid.Parse(input.AttemptID)
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
		return &RunLogSearchOutput{Body: runLogSearchPage(page, filters)}, nil
	}
}

func getJobsAnalytics(svc *jobs.Service) func(context.Context, *AnalyticsWindowInput) (*JobsAnalyticsOutput, error) {
	return func(ctx context.Context, input *AnalyticsWindowInput) (*JobsAnalyticsOutput, error) {
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
		return &JobsAnalyticsOutput{Body: jobsAnalytics(analytics)}, nil
	}
}

func getCostsAnalytics(svc *jobs.Service) func(context.Context, *AnalyticsWindowInput) (*CostsAnalyticsOutput, error) {
	return func(ctx context.Context, input *AnalyticsWindowInput) (*CostsAnalyticsOutput, error) {
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
		return &CostsAnalyticsOutput{Body: costsAnalytics(analytics)}, nil
	}
}

func getRunnerSizingAnalytics(svc *jobs.Service) func(context.Context, *AnalyticsWindowInput) (*RunnerSizingAnalyticsOutput, error) {
	return func(ctx context.Context, input *AnalyticsWindowInput) (*RunnerSizingAnalyticsOutput, error) {
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
		return &RunnerSizingAnalyticsOutput{Body: runnerSizingAnalytics(analytics)}, nil
	}
}

func createExecutionSchedule(recurringSvc *recurring.Service, installationID string) func(context.Context, *CreateExecutionScheduleInput) (*ExecutionScheduleOutput, error) {
	return func(ctx context.Context, input *CreateExecutionScheduleInput) (*ExecutionScheduleOutput, error) {
		identity, err := requireIdentity(ctx)
		if err != nil {
			return nil, err
		}
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		record, err := recurringSvc.CreateSchedule(ctx, orgID, identity.Subject, executionScheduleCreateRequest(input.Body))
		if err != nil {
			if errors.Is(err, recurring.ErrInvalid) {
				return nil, badRequest(ctx, "invalid-execution-schedule", err.Error(), err)
			}
			if errors.Is(err, recurring.ErrConflict) {
				return nil, conflict(ctx, "execution-schedule-conflict", "execution schedule idempotency key conflicts with an existing schedule")
			}
			return nil, internalFailure(ctx, "create-execution-schedule-failed", "create execution schedule failed", err)
		}
		return &ExecutionScheduleOutput{Body: executionScheduleRecord(record, installationID)}, nil
	}
}

func listExecutionSchedules(recurringSvc *recurring.Service, installationID string) func(context.Context, *EmptyInput) (*ListExecutionSchedulesOutput, error) {
	return func(ctx context.Context, _ *EmptyInput) (*ListExecutionSchedulesOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		records, err := recurringSvc.ListSchedules(ctx, orgID)
		if err != nil {
			return nil, internalFailure(ctx, "list-execution-schedules-failed", "list execution schedules failed", err)
		}
		out := make([]dto.SandboxExecutionScheduleRecord, 0, len(records))
		for _, record := range records {
			out = append(out, executionScheduleRecord(record, installationID))
		}
		return &ListExecutionSchedulesOutput{Body: out}, nil
	}
}

func getExecutionSchedule(recurringSvc *recurring.Service, installationID string) func(context.Context, *ExecutionScheduleIDPath) (*ExecutionScheduleOutput, error) {
	return func(ctx context.Context, input *ExecutionScheduleIDPath) (*ExecutionScheduleOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		scheduleID, err := uuid.Parse(input.ScheduleID)
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
		return &ExecutionScheduleOutput{Body: executionScheduleRecord(*record, installationID)}, nil
	}
}

func pauseExecutionSchedule(recurringSvc *recurring.Service, installationID string) func(context.Context, *ExecutionScheduleIDPath) (*ExecutionScheduleOutput, error) {
	return func(ctx context.Context, input *ExecutionScheduleIDPath) (*ExecutionScheduleOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		scheduleID, err := uuid.Parse(input.ScheduleID)
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
		return &ExecutionScheduleOutput{Body: executionScheduleRecord(*record, installationID)}, nil
	}
}

func resumeExecutionSchedule(recurringSvc *recurring.Service, installationID string) func(context.Context, *ExecutionScheduleIDPath) (*ExecutionScheduleOutput, error) {
	return func(ctx context.Context, input *ExecutionScheduleIDPath) (*ExecutionScheduleOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		scheduleID, err := uuid.Parse(input.ScheduleID)
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
		return &ExecutionScheduleOutput{Body: executionScheduleRecord(*record, installationID)}, nil
	}
}

func analyticsWindowInput(ctx context.Context, input *AnalyticsWindowInput) (jobs.AnalyticsWindow, error) {
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
