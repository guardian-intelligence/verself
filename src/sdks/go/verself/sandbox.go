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
	ZfsWritten             *int64     `json:"zfs_written,omitempty"`
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
	ScheduleID         *string `json:"schedule_id,omitempty"`
	DisplayName        *string `json:"display_name,omitempty"`
	TemporalScheduleID *string `json:"temporal_schedule_id,omitempty"`
	TemporalWorkflowID *string `json:"temporal_workflow_id,omitempty"`
	TemporalRunID      *string `json:"temporal_run_id,omitempty"`
}

type SandboxExecutionStickyDiskMount struct {
	MountID             string     `json:"mount_id"`
	MountName           string     `json:"mount_name"`
	MountPath           string     `json:"mount_path"`
	KeyHash             string     `json:"key_hash"`
	BaseGeneration      string     `json:"base_generation"`
	CommittedGeneration string     `json:"committed_generation"`
	SaveRequested       bool       `json:"save_requested"`
	SaveState           string     `json:"save_state"`
	RequestedAt         *time.Time `json:"requested_at,omitempty"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
	FailureReason       *string    `json:"failure_reason,omitempty"`
}

type SandboxExecution struct {
	ExecutionID      string                            `json:"execution_id"`
	RunID            string                            `json:"run_id"`
	OrgID            string                            `json:"org_id"`
	ActorID          string                            `json:"actor_id"`
	ProductID        string                            `json:"product_id"`
	Kind             string                            `json:"kind"`
	Status           string                            `json:"status"`
	SourceKind       *string                           `json:"source_kind,omitempty"`
	WorkloadKind     *string                           `json:"workload_kind,omitempty"`
	SourceRef        *string                           `json:"source_ref,omitempty"`
	RunnerClass      *string                           `json:"runner_class,omitempty"`
	Provider         *string                           `json:"provider,omitempty"`
	ExternalProvider *string                           `json:"external_provider,omitempty"`
	ExternalTaskID   *string                           `json:"external_task_id,omitempty"`
	CorrelationID    *string                           `json:"correlation_id,omitempty"`
	IdempotencyKey   *string                           `json:"idempotency_key,omitempty"`
	RunCommand       *string                           `json:"run_command,omitempty"`
	Runner           *SandboxRunnerRunMetadata         `json:"runner,omitempty"`
	Schedule         *SandboxScheduleRunMetadata       `json:"schedule,omitempty"`
	BillingSummary   *SandboxRunBillingSummary         `json:"billing_summary,omitempty"`
	BillingWindows   []SandboxBillingWindow            `json:"billing_windows,omitempty"`
	StickyDiskMounts []SandboxExecutionStickyDiskMount `json:"sticky_disk_mounts,omitempty"`
	LatestAttempt    SandboxAttempt                    `json:"latest_attempt"`
	CreatedAt        time.Time                         `json:"created_at"`
	UpdatedAt        time.Time                         `json:"updated_at"`
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
	ScheduleID         string                             `json:"schedule_id"`
	OrgID              string                             `json:"org_id"`
	ProjectID          string                             `json:"project_id"`
	SourceRepositoryID string                             `json:"source_repository_id"`
	ActorID            string                             `json:"actor_id"`
	DisplayName        *string                            `json:"display_name,omitempty"`
	WorkflowPath       string                             `json:"workflow_path"`
	Ref                *string                            `json:"ref,omitempty"`
	Inputs             map[string]string                  `json:"inputs,omitempty"`
	IntervalSeconds    int32                              `json:"interval_seconds"`
	State              string                             `json:"state"`
	TaskQueue          string                             `json:"task_queue"`
	TemporalNamespace  string                             `json:"temporal_namespace"`
	TemporalScheduleID string                             `json:"temporal_schedule_id"`
	IdempotencyKey     *string                            `json:"idempotency_key,omitempty"`
	Dispatches         []SandboxExecutionScheduleDispatch `json:"dispatches,omitempty"`
	CreatedAt          time.Time                          `json:"created_at"`
	UpdatedAt          time.Time                          `json:"updated_at"`
}

type SandboxExecutionSchedules struct {
	Schedules []SandboxExecutionSchedule `json:"schedules"`
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
	client *sandboxcore.ClientWithResponses
}

func (c *SandboxClient) ListRuns(ctx context.Context, options ListSandboxRunsOptions) (SandboxRunsPage, error) {
	if c == nil || c.client == nil {
		return SandboxRunsPage{}, fmt.Errorf("verself sdk: sandbox client is not initialized")
	}
	params := &sandboxcore.ListRunsParams{}
	if options.Limit > 0 {
		params.Limit = &options.Limit
	}
	if strings.TrimSpace(options.Cursor) != "" {
		cursor := strings.TrimSpace(options.Cursor)
		params.Cursor = &cursor
	}
	if strings.TrimSpace(options.SourceKind) != "" {
		sourceKind := strings.TrimSpace(options.SourceKind)
		params.SourceKind = &sourceKind
	}
	if strings.TrimSpace(options.Status) != "" {
		status := strings.TrimSpace(options.Status)
		params.Status = &status
	}
	if strings.TrimSpace(options.Repository) != "" {
		repository := strings.TrimSpace(options.Repository)
		params.Repository = &repository
	}
	if strings.TrimSpace(options.Workflow) != "" {
		workflow := strings.TrimSpace(options.Workflow)
		params.Workflow = &workflow
	}
	if strings.TrimSpace(options.Branch) != "" {
		branch := strings.TrimSpace(options.Branch)
		params.Branch = &branch
	}
	if strings.TrimSpace(options.RunnerClass) != "" {
		runnerClass := strings.TrimSpace(options.RunnerClass)
		params.RunnerClass = &runnerClass
	}
	response, err := c.client.ListRunsWithResponse(ctx, params)
	if err != nil {
		return SandboxRunsPage{}, err
	}
	if response.JSON200 == nil {
		return SandboxRunsPage{}, sandboxAPIError("list runs", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return sandboxFromGenerated[SandboxRunsPage](*response.JSON200)
}

func (c *SandboxClient) GetExecution(ctx context.Context, executionID string) (SandboxExecution, error) {
	if c == nil || c.client == nil {
		return SandboxExecution{}, fmt.Errorf("verself sdk: sandbox client is not initialized")
	}
	id, err := parseUUIDInput(executionID, "execution id")
	if err != nil {
		return SandboxExecution{}, err
	}
	response, err := c.client.GetExecutionWithResponse(ctx, id.String())
	if err != nil {
		return SandboxExecution{}, err
	}
	if response.JSON200 == nil {
		return SandboxExecution{}, sandboxAPIError("get execution", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return sandboxFromGenerated[SandboxExecution](*response.JSON200)
}

func (c *SandboxClient) GetExecutionLogs(ctx context.Context, executionID string) (SandboxExecutionLogs, error) {
	if c == nil || c.client == nil {
		return SandboxExecutionLogs{}, fmt.Errorf("verself sdk: sandbox client is not initialized")
	}
	id, err := parseUUIDInput(executionID, "execution id")
	if err != nil {
		return SandboxExecutionLogs{}, err
	}
	response, err := c.client.GetExecutionLogsWithResponse(ctx, id.String())
	if err != nil {
		return SandboxExecutionLogs{}, err
	}
	if response.JSON200 == nil {
		return SandboxExecutionLogs{}, sandboxAPIError("get execution logs", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return sandboxFromGenerated[SandboxExecutionLogs](*response.JSON200)
}

func (c *SandboxClient) ListSchedules(ctx context.Context) (SandboxExecutionSchedules, error) {
	if c == nil || c.client == nil {
		return SandboxExecutionSchedules{}, fmt.Errorf("verself sdk: sandbox client is not initialized")
	}
	response, err := c.client.ListExecutionSchedulesWithResponse(ctx)
	if err != nil {
		return SandboxExecutionSchedules{}, err
	}
	if response.JSON200 == nil {
		return SandboxExecutionSchedules{}, sandboxAPIError("list schedules", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return sandboxFromGenerated[SandboxExecutionSchedules](map[string]any{"schedules": response.JSON200})
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
	body := sandboxcore.SandboxExecutionScheduleCreateRequest{
		ProjectId:          projectID.String(),
		SourceRepositoryId: repoID.String(),
		WorkflowPath:       workflowPath,
		IntervalSeconds:    input.IntervalSeconds,
		IdempotencyKey:     key,
	}
	if strings.TrimSpace(input.DisplayName) != "" {
		displayName := strings.TrimSpace(input.DisplayName)
		body.DisplayName = &displayName
	}
	if strings.TrimSpace(input.Ref) != "" {
		ref := strings.TrimSpace(input.Ref)
		body.Ref = &ref
	}
	if input.Inputs != nil {
		inputs := copyStringMap(input.Inputs)
		body.Inputs = &inputs
	}
	if input.Paused {
		paused := true
		body.Paused = &paused
	}
	response, err := c.client.CreateExecutionScheduleWithResponse(ctx, body)
	if err != nil {
		return SandboxExecutionSchedule{}, err
	}
	if response.JSON201 == nil {
		return SandboxExecutionSchedule{}, sandboxAPIError("create schedule", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return sandboxFromGenerated[SandboxExecutionSchedule](*response.JSON201)
}

func (c *SandboxClient) GetSchedule(ctx context.Context, scheduleID string) (SandboxExecutionSchedule, error) {
	if c == nil || c.client == nil {
		return SandboxExecutionSchedule{}, fmt.Errorf("verself sdk: sandbox client is not initialized")
	}
	id, err := parseUUIDInput(scheduleID, "schedule id")
	if err != nil {
		return SandboxExecutionSchedule{}, err
	}
	response, err := c.client.GetExecutionScheduleWithResponse(ctx, id.String())
	if err != nil {
		return SandboxExecutionSchedule{}, err
	}
	if response.JSON200 == nil {
		return SandboxExecutionSchedule{}, sandboxAPIError("get schedule", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return sandboxFromGenerated[SandboxExecutionSchedule](*response.JSON200)
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
		response, err := c.client.PauseExecutionScheduleWithResponse(ctx, id.String(), &sandboxcore.PauseExecutionScheduleParams{IdempotencyKey: key})
		if err != nil {
			return SandboxExecutionSchedule{}, err
		}
		if response.JSON200 == nil {
			return SandboxExecutionSchedule{}, sandboxAPIError(operation, response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
		}
		return sandboxFromGenerated[SandboxExecutionSchedule](*response.JSON200)
	}
	response, err := c.client.ResumeExecutionScheduleWithResponse(ctx, id.String(), &sandboxcore.ResumeExecutionScheduleParams{IdempotencyKey: key})
	if err != nil {
		return SandboxExecutionSchedule{}, err
	}
	if response.JSON200 == nil {
		return SandboxExecutionSchedule{}, sandboxAPIError(operation, response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return sandboxFromGenerated[SandboxExecutionSchedule](*response.JSON200)
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
