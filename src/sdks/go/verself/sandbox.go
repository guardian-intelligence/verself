package verself

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sandboxcore "github.com/verself/verself-go/internal/generated/sandbox"
)

type SandboxAttempt struct {
	AttemptID              string     `json:"attempt_id"`
	ResourceName           *string    `json:"resourceName,omitempty"`
	AttemptSeq             int64      `json:"attempt_seq"`
	State                  string     `json:"state"`
	LeaseID                *string    `json:"lease_id,omitempty"`
	ExecID                 *string    `json:"exec_id,omitempty"`
	BillingJobID           *int64     `json:"billing_job_id,omitempty"`
	TraceID                *string    `json:"trace_id,omitempty"`
	ExitCode               *int64     `json:"exit_code,omitempty"`
	FailureReason          *string    `json:"failure_reason,omitempty"`
	DurationMs             *int64     `json:"duration_ms,omitempty"`
	BootTimeUs             *int64     `json:"boot_time_us,omitempty"`
	StdoutBytes            *int64     `json:"stdout_bytes,omitempty"`
	StderrBytes            *int64     `json:"stderr_bytes,omitempty"`
	NetRxBytes             *int64     `json:"net_rx_bytes,omitempty"`
	NetTxBytes             *int64     `json:"net_tx_bytes,omitempty"`
	BlockReadBytes         *int64     `json:"block_read_bytes,omitempty"`
	BlockWriteBytes        *int64     `json:"block_write_bytes,omitempty"`
	RootfsProvisionedBytes *int64     `json:"rootfs_provisioned_bytes,omitempty"`
	VcpuExitCount          *int64     `json:"vcpu_exit_count,omitempty"`
	StartedAt              *time.Time `json:"started_at,omitempty"`
	CompletedAt            *time.Time `json:"completed_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type SandboxRunBillingSummary struct {
	ReservedChargeUnits string  `json:"reserved_charge_units"`
	BilledChargeUnits   string  `json:"billed_charge_units"`
	WriteoffChargeUnits string  `json:"writeoff_charge_units"`
	CostPerUnit         string  `json:"cost_per_unit"`
	PricingPhase        *string `json:"pricing_phase,omitempty"`
	WindowCount         int32   `json:"window_count"`
}

type SandboxBillingWindow struct {
	BillingWindowID     string     `json:"billing_window_id"`
	AttemptID           string     `json:"attempt_id"`
	WindowSeq           int64      `json:"window_seq"`
	WindowStart         time.Time  `json:"window_start"`
	State               string     `json:"state"`
	ReservationShape    string     `json:"reservation_shape"`
	ReservedQuantity    int64      `json:"reserved_quantity"`
	ActualQuantity      *int64     `json:"actual_quantity,omitempty"`
	ReservedChargeUnits string     `json:"reserved_charge_units"`
	BilledChargeUnits   string     `json:"billed_charge_units"`
	WriteoffChargeUnits string     `json:"writeoff_charge_units"`
	CostPerUnit         string     `json:"cost_per_unit"`
	PricingPhase        *string    `json:"pricing_phase,omitempty"`
	SettledAt           *time.Time `json:"settled_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
}

type SandboxRunnerRunMetadata struct {
	RepositoryFullName     *string `json:"repository_full_name,omitempty"`
	WorkflowName           *string `json:"workflow_name,omitempty"`
	HeadBranch             *string `json:"head_branch,omitempty"`
	HeadSha                *string `json:"head_sha,omitempty"`
	JobName                *string `json:"job_name,omitempty"`
	ProviderInstallationID *string `json:"provider_installation_id,omitempty"`
	ProviderRunID          *string `json:"provider_run_id,omitempty"`
	ProviderJobID          *string `json:"provider_job_id,omitempty"`
}

type SandboxScheduleRunMetadata struct {
	ScheduleID           *string `json:"schedule_id,omitempty"`
	ScheduleResourceName *string `json:"scheduleResourceName,omitempty"`
	DisplayName          *string `json:"display_name,omitempty"`
	TemporalScheduleID   *string `json:"temporal_schedule_id,omitempty"`
	TemporalWorkflowID   *string `json:"temporal_workflow_id,omitempty"`
	TemporalRunID        *string `json:"temporal_run_id,omitempty"`
}

type SandboxExecution struct {
	ExecutionID      string                      `json:"execution_id"`
	ResourceName     string                      `json:"resourceName"`
	RunID            string                      `json:"run_id"`
	OrgID            string                      `json:"org_id"`
	ActorID          string                      `json:"actor_id"`
	ProductID        string                      `json:"product_id"`
	Kind             string                      `json:"kind"`
	Status           string                      `json:"status"`
	SourceKind       *string                     `json:"source_kind,omitempty"`
	WorkloadKind     *string                     `json:"workload_kind,omitempty"`
	SourceRef        *string                     `json:"source_ref,omitempty"`
	RunnerClass      *string                     `json:"runner_class,omitempty"`
	Provider         *string                     `json:"provider,omitempty"`
	ExternalProvider *string                     `json:"external_provider,omitempty"`
	ExternalTaskID   *string                     `json:"external_task_id,omitempty"`
	CorrelationID    *string                     `json:"correlation_id,omitempty"`
	IdempotencyKey   *string                     `json:"idempotency_key,omitempty"`
	RunCommand       *string                     `json:"run_command,omitempty"`
	Runner           *SandboxRunnerRunMetadata   `json:"runner,omitempty"`
	Schedule         *SandboxScheduleRunMetadata `json:"schedule,omitempty"`
	BillingSummary   *SandboxRunBillingSummary   `json:"billing_summary,omitempty"`
	BillingWindows   []SandboxBillingWindow      `json:"billing_windows,omitempty"`
	LatestAttempt    SandboxAttempt              `json:"latest_attempt"`
	CreatedAt        time.Time                   `json:"created_at"`
	UpdatedAt        time.Time                   `json:"updated_at"`
}

type SandboxRunsFilters struct {
	SourceKind  *string `json:"source_kind,omitempty"`
	Status      *string `json:"status,omitempty"`
	Repository  *string `json:"repository,omitempty"`
	Workflow    *string `json:"workflow,omitempty"`
	Branch      *string `json:"branch,omitempty"`
	RunnerClass *string `json:"runner_class,omitempty"`
}

type SandboxRunsPage struct {
	Runs       []SandboxExecution `json:"runs"`
	Filters    SandboxRunsFilters `json:"filters"`
	Limit      int32              `json:"limit"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

type SandboxExecutionLogs struct {
	ExecutionID string `json:"execution_id"`
	AttemptID   string `json:"attempt_id"`
	Logs        string `json:"logs"`
}

type SandboxAnalyticsBucket struct {
	Key                 string  `json:"key"`
	Count               string  `json:"count"`
	ReservedChargeUnits *string `json:"reserved_charge_units,omitempty"`
	BilledChargeUnits   *string `json:"billed_charge_units,omitempty"`
	WriteoffChargeUnits *string `json:"writeoff_charge_units,omitempty"`
}

type SandboxRunDurationSample struct {
	ExecutionID        string    `json:"execution_id"`
	Status             string    `json:"status"`
	DurationMs         int64     `json:"duration_ms"`
	CompletedAt        time.Time `json:"completed_at"`
	RepositoryFullName *string   `json:"repository_full_name,omitempty"`
	WorkflowName       *string   `json:"workflow_name,omitempty"`
	JobName            *string   `json:"job_name,omitempty"`
	RunnerClass        *string   `json:"runner_class,omitempty"`
}

type SandboxJobsAnalytics struct {
	WindowStart   time.Time                  `json:"window_start"`
	WindowEnd     time.Time                  `json:"window_end"`
	TotalRuns     string                     `json:"total_runs"`
	SucceededRuns string                     `json:"succeeded_runs"`
	FailedRuns    string                     `json:"failed_runs"`
	P50DurationMs string                     `json:"p50_duration_ms"`
	P95DurationMs string                     `json:"p95_duration_ms"`
	P99DurationMs string                     `json:"p99_duration_ms"`
	BySource      []SandboxAnalyticsBucket   `json:"by_source,omitempty"`
	ByRunnerClass []SandboxAnalyticsBucket   `json:"by_runner_class,omitempty"`
	SlowestRuns   []SandboxRunDurationSample `json:"slowest_runs,omitempty"`
}

type SandboxCostsAnalytics struct {
	WindowStart         time.Time                `json:"window_start"`
	WindowEnd           time.Time                `json:"window_end"`
	ReservedChargeUnits string                   `json:"reserved_charge_units"`
	BilledChargeUnits   string                   `json:"billed_charge_units"`
	WriteoffChargeUnits string                   `json:"writeoff_charge_units"`
	BySource            []SandboxAnalyticsBucket `json:"by_source,omitempty"`
	ByRunnerClass       []SandboxAnalyticsBucket `json:"by_runner_class,omitempty"`
	ByRepository        []SandboxAnalyticsBucket `json:"by_repository,omitempty"`
}

type SandboxRunnerSizingSample struct {
	RunnerClass               string `json:"runner_class"`
	RunCount                  string `json:"run_count"`
	P95DurationMs             string `json:"p95_duration_ms"`
	AvgRootfsProvisionedBytes string `json:"avg_rootfs_provisioned_bytes"`
	AvgBootTimeUs             string `json:"avg_boot_time_us"`
	AvgBlockWriteBytes        string `json:"avg_block_write_bytes"`
	AvgNetTxBytes             string `json:"avg_net_tx_bytes"`
}

type SandboxRunnerSizingAnalytics struct {
	WindowStart   time.Time                   `json:"window_start"`
	WindowEnd     time.Time                   `json:"window_end"`
	ByRunnerClass []SandboxRunnerSizingSample `json:"by_runner_class,omitempty"`
}

type SandboxRunLogSearchFilters struct {
	Query       *string `json:"query,omitempty"`
	RunID       *string `json:"run_id,omitempty"`
	AttemptID   *string `json:"attempt_id,omitempty"`
	SourceKind  *string `json:"source_kind,omitempty"`
	Repository  *string `json:"repository,omitempty"`
	Workflow    *string `json:"workflow,omitempty"`
	Branch      *string `json:"branch,omitempty"`
	RunnerClass *string `json:"runner_class,omitempty"`
}

type SandboxRunLogSearchResult struct {
	ExecutionID        string    `json:"execution_id"`
	AttemptID          string    `json:"attempt_id"`
	Seq                int32     `json:"seq"`
	Stream             string    `json:"stream"`
	Chunk              string    `json:"chunk"`
	CreatedAt          time.Time `json:"created_at"`
	SourceKind         *string   `json:"source_kind,omitempty"`
	WorkloadKind       *string   `json:"workload_kind,omitempty"`
	RepositoryFullName *string   `json:"repository_full_name,omitempty"`
	WorkflowName       *string   `json:"workflow_name,omitempty"`
	JobName            *string   `json:"job_name,omitempty"`
	HeadBranch         *string   `json:"head_branch,omitempty"`
	RunnerClass        *string   `json:"runner_class,omitempty"`
	ScheduleID         *string   `json:"schedule_id,omitempty"`
}

type SandboxRunLogSearchPage struct {
	Results    []SandboxRunLogSearchResult `json:"results"`
	Filters    SandboxRunLogSearchFilters  `json:"filters"`
	Limit      int32                       `json:"limit"`
	NextCursor string                      `json:"next_cursor,omitempty"`
}

type SandboxGitHubInstallation struct {
	InstallationID string    `json:"installation_id"`
	ResourceName   string    `json:"resourceName"`
	OrgID          string    `json:"org_id"`
	AccountLogin   string    `json:"account_login"`
	AccountType    string    `json:"account_type"`
	Active         bool      `json:"active"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type SandboxGitHubInstallationConnect struct {
	SetupURL  string    `json:"setup_url"`
	State     string    `json:"state"`
	ExpiresAt time.Time `json:"expires_at"`
}

type SandboxExecutionScheduleDispatch struct {
	DispatchID          string     `json:"dispatch_id"`
	ScheduleID          string     `json:"schedule_id"`
	TemporalScheduleID  string     `json:"temporal_schedule_id"`
	TemporalWorkflowID  string     `json:"temporal_workflow_id"`
	TemporalRunID       string     `json:"temporal_run_id"`
	State               string     `json:"state"`
	WorkflowState       *string    `json:"workflow_state,omitempty"`
	FailureReason       *string    `json:"failure_reason,omitempty"`
	SourceWorkflowRunID *string    `json:"source_workflow_run_id,omitempty"`
	ScheduledAt         time.Time  `json:"scheduled_at"`
	SubmittedAt         *time.Time `json:"submitted_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type SandboxExecutionSchedule struct {
	ScheduleID                   string                             `json:"schedule_id"`
	ResourceName                 string                             `json:"resourceName"`
	OrgID                        string                             `json:"org_id"`
	ProjectID                    string                             `json:"project_id"`
	ProjectResourceName          string                             `json:"projectResourceName"`
	SourceRepositoryID           string                             `json:"source_repository_id"`
	SourceRepositoryResourceName string                             `json:"sourceRepositoryResourceName"`
	ActorID                      string                             `json:"actor_id"`
	DisplayName                  *string                            `json:"display_name,omitempty"`
	WorkflowPath                 string                             `json:"workflow_path"`
	Ref                          *string                            `json:"ref,omitempty"`
	Inputs                       map[string]string                  `json:"inputs,omitempty"`
	IntervalSeconds              int32                              `json:"interval_seconds"`
	State                        string                             `json:"state"`
	TaskQueue                    string                             `json:"task_queue"`
	TemporalNamespace            string                             `json:"temporal_namespace"`
	TemporalScheduleID           string                             `json:"temporal_schedule_id"`
	IdempotencyKey               *string                            `json:"idempotency_key,omitempty"`
	Dispatches                   []SandboxExecutionScheduleDispatch `json:"dispatches,omitempty"`
	CreatedAt                    time.Time                          `json:"created_at"`
	UpdatedAt                    time.Time                          `json:"updated_at"`
}

type SandboxExecutionSchedules struct {
	Schedules  []SandboxExecutionSchedule `json:"schedules"`
	Limit      int32                      `json:"limit"`
	NextCursor string                     `json:"next_cursor,omitempty"`
}

type SandboxMutationOptions struct {
	IdempotencyKey string
}

type ListSandboxRunsOptions struct {
	Limit       int64
	Cursor      string
	SourceKind  string
	Status      string
	Repository  string
	Workflow    string
	Branch      string
	RunnerClass string
}

type ListSandboxExecutionSchedulesOptions struct {
	Limit  int64
	Cursor string
}

type SearchSandboxRunLogsOptions struct {
	Limit       int64
	Cursor      string
	Query       string
	RunID       string
	AttemptID   string
	SourceKind  string
	Repository  string
	Workflow    string
	Branch      string
	RunnerClass string
}

type SandboxAnalyticsOptions struct {
	Start time.Time
	End   time.Time
}

type CreateSandboxExecutionScheduleInput struct {
	ProjectID          string
	SourceRepositoryID string
	WorkflowPath       string
	IntervalSeconds    int32
	DisplayName        string
	Ref                string
	Inputs             map[string]string
	Paused             bool
	IdempotencyKey     string
}

type SandboxClient struct {
	client *sandboxcore.Client
}

func (c *SandboxClient) ListRuns(ctx context.Context, options ListSandboxRunsOptions) (SandboxRunsPage, error) {
	if c == nil || c.client == nil {
		return SandboxRunsPage{}, fmt.Errorf("verself sdk: sandbox client is not initialized")
	}
	request := sandboxcore.ListRunsRequest{}
	if options.Limit > 0 {
		limit := sandboxcore.RunListPageSize(options.Limit)
		request.Limit = &limit
	}
	if strings.TrimSpace(options.Cursor) != "" {
		cursor := sandboxcore.RunListCursor(strings.TrimSpace(options.Cursor))
		request.Cursor = &cursor
	}
	if strings.TrimSpace(options.SourceKind) != "" {
		sourceKind := sandboxcore.SourceKind(strings.TrimSpace(options.SourceKind))
		request.SourceKind = &sourceKind
	}
	if strings.TrimSpace(options.Status) != "" {
		status := sandboxcore.ExecutionStatus(strings.TrimSpace(options.Status))
		request.Status = &status
	}
	if strings.TrimSpace(options.Repository) != "" {
		repository := sandboxcore.RepositoryFullName(strings.TrimSpace(options.Repository))
		request.Repository = &repository
	}
	if strings.TrimSpace(options.Workflow) != "" {
		workflow := sandboxcore.WorkflowName(strings.TrimSpace(options.Workflow))
		request.Workflow = &workflow
	}
	if strings.TrimSpace(options.Branch) != "" {
		branch := sandboxcore.GitRef(strings.TrimSpace(options.Branch))
		request.Branch = &branch
	}
	if strings.TrimSpace(options.RunnerClass) != "" {
		runnerClass := sandboxcore.RunnerClass(strings.TrimSpace(options.RunnerClass))
		request.RunnerClass = &runnerClass
	}
	response, err := c.client.ListRuns(ctx, request)
	if err != nil {
		return SandboxRunsPage{}, err
	}
	if response.Result == nil {
		return SandboxRunsPage{}, sandboxAPIError("list runs", response.StatusCode, response.Problem, response.Body)
	}
	return sandboxFromGenerated[SandboxRunsPage](*response.Result)
}

func (c *SandboxClient) GetExecution(ctx context.Context, executionID string) (SandboxExecution, error) {
	if c == nil || c.client == nil {
		return SandboxExecution{}, fmt.Errorf("verself sdk: sandbox client is not initialized")
	}
	id, err := parseUUIDInput(executionID, "execution id")
	if err != nil {
		return SandboxExecution{}, err
	}
	response, err := c.client.GetExecution(ctx, sandboxcore.GetExecutionRequest{ExecutionID: sandboxcore.ExecutionId(id.String())})
	if err != nil {
		return SandboxExecution{}, err
	}
	if response.Result == nil {
		return SandboxExecution{}, sandboxAPIError("get execution", response.StatusCode, response.Problem, response.Body)
	}
	return sandboxFromGenerated[SandboxExecution](*response.Result)
}

func (c *SandboxClient) GetExecutionLogs(ctx context.Context, executionID string) (SandboxExecutionLogs, error) {
	if c == nil || c.client == nil {
		return SandboxExecutionLogs{}, fmt.Errorf("verself sdk: sandbox client is not initialized")
	}
	id, err := parseUUIDInput(executionID, "execution id")
	if err != nil {
		return SandboxExecutionLogs{}, err
	}
	response, err := c.client.GetExecutionLogs(ctx, sandboxcore.GetExecutionLogsRequest{ExecutionID: sandboxcore.ExecutionId(id.String())})
	if err != nil {
		return SandboxExecutionLogs{}, err
	}
	if response.Result == nil {
		return SandboxExecutionLogs{}, sandboxAPIError("get execution logs", response.StatusCode, response.Problem, response.Body)
	}
	return sandboxFromGenerated[SandboxExecutionLogs](*response.Result)
}

func (c *SandboxClient) GetRun(ctx context.Context, runID string) (SandboxExecution, error) {
	if c == nil || c.client == nil {
		return SandboxExecution{}, fmt.Errorf("verself sdk: sandbox client is not initialized")
	}
	id, err := parseUUIDInput(runID, "run id")
	if err != nil {
		return SandboxExecution{}, err
	}
	response, err := c.client.GetRun(ctx, sandboxcore.GetRunRequest{RunID: sandboxcore.RunId(id.String())})
	if err != nil {
		return SandboxExecution{}, err
	}
	if response.Result == nil {
		return SandboxExecution{}, sandboxAPIError("get run", response.StatusCode, response.Problem, response.Body)
	}
	return sandboxFromGenerated[SandboxExecution](*response.Result)
}

func (c *SandboxClient) SearchRunLogs(ctx context.Context, options SearchSandboxRunLogsOptions) (SandboxRunLogSearchPage, error) {
	if c == nil || c.client == nil {
		return SandboxRunLogSearchPage{}, fmt.Errorf("verself sdk: sandbox client is not initialized")
	}
	request := sandboxcore.SearchRunLogsRequest{}
	if options.Limit > 0 {
		limit := sandboxcore.RunLogSearchPageSize(options.Limit)
		request.Limit = &limit
	}
	setRunLogCursorParam(strings.TrimSpace(options.Cursor), &request.Cursor)
	setLogQueryParam(strings.TrimSpace(options.Query), &request.Query)
	if strings.TrimSpace(options.RunID) != "" {
		id, err := parseUUIDInput(options.RunID, "run id")
		if err != nil {
			return SandboxRunLogSearchPage{}, err
		}
		runID := sandboxcore.RunId(id.String())
		request.RunID = &runID
	}
	if strings.TrimSpace(options.AttemptID) != "" {
		id, err := parseUUIDInput(options.AttemptID, "attempt id")
		if err != nil {
			return SandboxRunLogSearchPage{}, err
		}
		attemptID := sandboxcore.AttemptId(id.String())
		request.AttemptID = &attemptID
	}
	setSourceKindParam(strings.TrimSpace(options.SourceKind), &request.SourceKind)
	setRepositoryParam(strings.TrimSpace(options.Repository), &request.Repository)
	setWorkflowParam(strings.TrimSpace(options.Workflow), &request.Workflow)
	setGitRefParam(strings.TrimSpace(options.Branch), &request.Branch)
	setRunnerClassParam(strings.TrimSpace(options.RunnerClass), &request.RunnerClass)
	response, err := c.client.SearchRunLogs(ctx, request)
	if err != nil {
		return SandboxRunLogSearchPage{}, err
	}
	if response.Result == nil {
		return SandboxRunLogSearchPage{}, sandboxAPIError("search run logs", response.StatusCode, response.Problem, response.Body)
	}
	return sandboxFromGenerated[SandboxRunLogSearchPage](*response.Result)
}

func (c *SandboxClient) GetJobsAnalytics(ctx context.Context, options SandboxAnalyticsOptions) (SandboxJobsAnalytics, error) {
	if c == nil || c.client == nil {
		return SandboxJobsAnalytics{}, fmt.Errorf("verself sdk: sandbox client is not initialized")
	}
	response, err := c.client.GetJobsAnalytics(ctx, jobsAnalyticsRequest(options))
	if err != nil {
		return SandboxJobsAnalytics{}, err
	}
	if response.Result == nil {
		return SandboxJobsAnalytics{}, sandboxAPIError("get jobs analytics", response.StatusCode, response.Problem, response.Body)
	}
	return sandboxFromGenerated[SandboxJobsAnalytics](*response.Result)
}

func (c *SandboxClient) GetCostsAnalytics(ctx context.Context, options SandboxAnalyticsOptions) (SandboxCostsAnalytics, error) {
	if c == nil || c.client == nil {
		return SandboxCostsAnalytics{}, fmt.Errorf("verself sdk: sandbox client is not initialized")
	}
	response, err := c.client.GetCostsAnalytics(ctx, costsAnalyticsRequest(options))
	if err != nil {
		return SandboxCostsAnalytics{}, err
	}
	if response.Result == nil {
		return SandboxCostsAnalytics{}, sandboxAPIError("get costs analytics", response.StatusCode, response.Problem, response.Body)
	}
	return sandboxFromGenerated[SandboxCostsAnalytics](*response.Result)
}

func (c *SandboxClient) GetRunnerSizingAnalytics(ctx context.Context, options SandboxAnalyticsOptions) (SandboxRunnerSizingAnalytics, error) {
	if c == nil || c.client == nil {
		return SandboxRunnerSizingAnalytics{}, fmt.Errorf("verself sdk: sandbox client is not initialized")
	}
	response, err := c.client.GetRunnerSizingAnalytics(ctx, runnerSizingAnalyticsRequest(options))
	if err != nil {
		return SandboxRunnerSizingAnalytics{}, err
	}
	if response.Result == nil {
		return SandboxRunnerSizingAnalytics{}, sandboxAPIError("get runner sizing analytics", response.StatusCode, response.Problem, response.Body)
	}
	return sandboxFromGenerated[SandboxRunnerSizingAnalytics](*response.Result)
}

func (c *SandboxClient) ListGitHubInstallations(ctx context.Context) ([]SandboxGitHubInstallation, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("verself sdk: sandbox client is not initialized")
	}
	response, err := c.client.ListGithubInstallations(ctx, sandboxcore.ListGithubInstallationsRequest{})
	if err != nil {
		return nil, err
	}
	if response.Result == nil {
		return nil, sandboxAPIError("list GitHub installations", response.StatusCode, response.Problem, response.Body)
	}
	return sandboxFromGenerated[[]SandboxGitHubInstallation](response.Result.Body)
}

func (c *SandboxClient) BeginGitHubInstallation(ctx context.Context, options SandboxMutationOptions) (SandboxGitHubInstallationConnect, error) {
	if c == nil || c.client == nil {
		return SandboxGitHubInstallationConnect{}, fmt.Errorf("verself sdk: sandbox client is not initialized")
	}
	key, err := mutationKey("sandbox-github-installation", options.IdempotencyKey)
	if err != nil {
		return SandboxGitHubInstallationConnect{}, err
	}
	response, err := c.client.BeginGithubInstallation(ctx, sandboxcore.BeginGithubInstallationRequest{IdempotencyKey: sandboxcore.IdempotencyKey(key)})
	if err != nil {
		return SandboxGitHubInstallationConnect{}, err
	}
	if response.Result == nil {
		return SandboxGitHubInstallationConnect{}, sandboxAPIError("begin GitHub installation", response.StatusCode, response.Problem, response.Body)
	}
	return sandboxFromGenerated[SandboxGitHubInstallationConnect](*response.Result)
}

func (c *SandboxClient) ListSchedules(ctx context.Context, options ListSandboxExecutionSchedulesOptions) (SandboxExecutionSchedules, error) {
	if c == nil || c.client == nil {
		return SandboxExecutionSchedules{}, fmt.Errorf("verself sdk: sandbox client is not initialized")
	}
	request := sandboxcore.ListExecutionSchedulesRequest{}
	if options.Limit > 0 {
		limit := sandboxcore.ScheduleListPageSize(options.Limit)
		request.Limit = &limit
	}
	if strings.TrimSpace(options.Cursor) != "" {
		cursor := sandboxcore.ScheduleListCursor(strings.TrimSpace(options.Cursor))
		request.Cursor = &cursor
	}
	response, err := c.client.ListExecutionSchedules(ctx, request)
	if err != nil {
		return SandboxExecutionSchedules{}, err
	}
	if response.Result == nil {
		return SandboxExecutionSchedules{}, sandboxAPIError("list schedules", response.StatusCode, response.Problem, response.Body)
	}
	return sandboxFromGenerated[SandboxExecutionSchedules](*response.Result)
}

func (c *SandboxClient) CreateSchedule(ctx context.Context, input CreateSandboxExecutionScheduleInput) (SandboxExecutionSchedule, error) {
	if c == nil || c.client == nil {
		return SandboxExecutionSchedule{}, fmt.Errorf("verself sdk: sandbox client is not initialized")
	}
	projectID, err := parseUUIDInput(input.ProjectID, "project id")
	if err != nil {
		return SandboxExecutionSchedule{}, err
	}
	repoID, err := parseUUIDInput(input.SourceRepositoryID, "source repository id")
	if err != nil {
		return SandboxExecutionSchedule{}, err
	}
	workflowPath := strings.TrimSpace(input.WorkflowPath)
	if workflowPath == "" {
		return SandboxExecutionSchedule{}, fmt.Errorf("verself sdk: sandbox schedule workflow path is required")
	}
	if input.IntervalSeconds <= 0 {
		return SandboxExecutionSchedule{}, fmt.Errorf("verself sdk: sandbox schedule interval seconds must be positive")
	}
	key, err := mutationKey("sandbox-schedule", input.IdempotencyKey)
	if err != nil {
		return SandboxExecutionSchedule{}, err
	}
	body := sandboxcore.CreateExecutionScheduleInputBody{
		ProjectID:          sandboxcore.ProjectId(projectID.String()),
		SourceRepositoryID: sandboxcore.SourceRepositoryId(repoID.String()),
		WorkflowPath:       sandboxcore.WorkflowPath(workflowPath),
		IntervalSeconds:    sandboxcore.IntervalSeconds(input.IntervalSeconds),
		IdempotencyKey:     sandboxcore.IdempotencyKey(key),
	}
	if strings.TrimSpace(input.DisplayName) != "" {
		displayName := sandboxcore.DisplayName(strings.TrimSpace(input.DisplayName))
		body.DisplayName = &displayName
	}
	if strings.TrimSpace(input.Ref) != "" {
		ref := sandboxcore.GitRef(strings.TrimSpace(input.Ref))
		body.Ref = &ref
	}
	if input.Inputs != nil {
		inputs := sandboxcore.ScheduleInputs(copyStringMap(input.Inputs))
		body.Inputs = &inputs
	}
	if input.Paused {
		paused := true
		body.Paused = &paused
	}
	response, err := c.client.CreateExecutionSchedule(ctx, sandboxcore.CreateExecutionScheduleRequest{Body: body})
	if err != nil {
		return SandboxExecutionSchedule{}, err
	}
	if response.Result == nil {
		return SandboxExecutionSchedule{}, sandboxAPIError("create schedule", response.StatusCode, response.Problem, response.Body)
	}
	return sandboxFromGenerated[SandboxExecutionSchedule](*response.Result)
}

func (c *SandboxClient) GetSchedule(ctx context.Context, scheduleID string) (SandboxExecutionSchedule, error) {
	if c == nil || c.client == nil {
		return SandboxExecutionSchedule{}, fmt.Errorf("verself sdk: sandbox client is not initialized")
	}
	id, err := parseUUIDInput(scheduleID, "schedule id")
	if err != nil {
		return SandboxExecutionSchedule{}, err
	}
	response, err := c.client.GetExecutionSchedule(ctx, sandboxcore.GetExecutionScheduleRequest{ScheduleID: sandboxcore.ScheduleId(id.String())})
	if err != nil {
		return SandboxExecutionSchedule{}, err
	}
	if response.Result == nil {
		return SandboxExecutionSchedule{}, sandboxAPIError("get schedule", response.StatusCode, response.Problem, response.Body)
	}
	return sandboxFromGenerated[SandboxExecutionSchedule](*response.Result)
}

func (c *SandboxClient) PauseSchedule(ctx context.Context, scheduleID string, options SandboxMutationOptions) (SandboxExecutionSchedule, error) {
	return c.scheduleLifecycle(ctx, "pause schedule", "sandbox-schedule-pause", scheduleID, options, true)
}

func (c *SandboxClient) ResumeSchedule(ctx context.Context, scheduleID string, options SandboxMutationOptions) (SandboxExecutionSchedule, error) {
	return c.scheduleLifecycle(ctx, "resume schedule", "sandbox-schedule-resume", scheduleID, options, false)
}

func (c *SandboxClient) scheduleLifecycle(ctx context.Context, operation, namespace, scheduleID string, options SandboxMutationOptions, pause bool) (SandboxExecutionSchedule, error) {
	if c == nil || c.client == nil {
		return SandboxExecutionSchedule{}, fmt.Errorf("verself sdk: sandbox client is not initialized")
	}
	id, err := parseUUIDInput(scheduleID, "schedule id")
	if err != nil {
		return SandboxExecutionSchedule{}, err
	}
	key, err := mutationKey(namespace, options.IdempotencyKey)
	if err != nil {
		return SandboxExecutionSchedule{}, err
	}
	if pause {
		response, err := c.client.PauseExecutionSchedule(ctx, sandboxcore.PauseExecutionScheduleRequest{
			ScheduleID:     sandboxcore.ScheduleId(id.String()),
			IdempotencyKey: sandboxcore.IdempotencyKey(key),
		})
		if err != nil {
			return SandboxExecutionSchedule{}, err
		}
		if response.Result == nil {
			return SandboxExecutionSchedule{}, sandboxAPIError(operation, response.StatusCode, response.Problem, response.Body)
		}
		return sandboxFromGenerated[SandboxExecutionSchedule](*response.Result)
	}
	response, err := c.client.ResumeExecutionSchedule(ctx, sandboxcore.ResumeExecutionScheduleRequest{
		ScheduleID:     sandboxcore.ScheduleId(id.String()),
		IdempotencyKey: sandboxcore.IdempotencyKey(key),
	})
	if err != nil {
		return SandboxExecutionSchedule{}, err
	}
	if response.Result == nil {
		return SandboxExecutionSchedule{}, sandboxAPIError(operation, response.StatusCode, response.Problem, response.Body)
	}
	return sandboxFromGenerated[SandboxExecutionSchedule](*response.Result)
}

func sandboxAPIError(operation string, statusCode int, model *sandboxcore.ErrorModel, body []byte) error {
	var title *string
	var detail *string
	if model != nil {
		title = model.Title
		detail = model.Detail
	}
	return apiErrorFields("Sandbox Rental API", operation, statusCode, title, detail, body)
}

func sandboxFromGenerated[T any](input any) (T, error) {
	var out T
	raw, err := json.Marshal(input)
	if err != nil {
		return out, fmt.Errorf("verself sdk: encode sandbox response: %w", err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("verself sdk: decode sandbox response: %w", err)
	}
	return out, nil
}

func setRunLogCursorParam(value string, target **sandboxcore.RunLogSearchCursor) {
	if value == "" {
		return
	}
	typed := sandboxcore.RunLogSearchCursor(value)
	*target = &typed
}

func setLogQueryParam(value string, target **sandboxcore.LogQuery) {
	if value == "" {
		return
	}
	typed := sandboxcore.LogQuery(value)
	*target = &typed
}

func setSourceKindParam(value string, target **sandboxcore.SourceKind) {
	if value == "" {
		return
	}
	typed := sandboxcore.SourceKind(value)
	*target = &typed
}

func setRepositoryParam(value string, target **sandboxcore.RepositoryFullName) {
	if value == "" {
		return
	}
	typed := sandboxcore.RepositoryFullName(value)
	*target = &typed
}

func setWorkflowParam(value string, target **sandboxcore.WorkflowName) {
	if value == "" {
		return
	}
	typed := sandboxcore.WorkflowName(value)
	*target = &typed
}

func setGitRefParam(value string, target **sandboxcore.GitRef) {
	if value == "" {
		return
	}
	typed := sandboxcore.GitRef(value)
	*target = &typed
}

func setRunnerClassParam(value string, target **sandboxcore.RunnerClass) {
	if value == "" {
		return
	}
	typed := sandboxcore.RunnerClass(value)
	*target = &typed
}

func analyticsWindow(options SandboxAnalyticsOptions) (*string, *string) {
	var start *string
	var end *string
	if !options.Start.IsZero() {
		value := options.Start.UTC().Format(time.RFC3339Nano)
		start = &value
	}
	if !options.End.IsZero() {
		value := options.End.UTC().Format(time.RFC3339Nano)
		end = &value
	}
	return start, end
}

func jobsAnalyticsRequest(options SandboxAnalyticsOptions) sandboxcore.GetJobsAnalyticsRequest {
	start, end := analyticsWindow(options)
	return sandboxcore.GetJobsAnalyticsRequest{Start: start, End: end}
}

func costsAnalyticsRequest(options SandboxAnalyticsOptions) sandboxcore.GetCostsAnalyticsRequest {
	start, end := analyticsWindow(options)
	return sandboxcore.GetCostsAnalyticsRequest{Start: start, End: end}
}

func runnerSizingAnalyticsRequest(options SandboxAnalyticsOptions) sandboxcore.GetRunnerSizingAnalyticsRequest {
	start, end := analyticsWindow(options)
	return sandboxcore.GetRunnerSizingAnalyticsRequest{Start: start, End: end}
}
