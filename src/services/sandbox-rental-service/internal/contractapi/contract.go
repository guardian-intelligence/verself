package contractapi

import (
	"context"
)

type OperationDescriptor struct {
	ShapeID             string
	OperationID         string
	Method              string
	Path                string
	DefaultStatus       int
	Readonly            bool
	Paginated           bool
	Identity            IdentityDescriptor
	Authorization       AuthorizationDescriptor
	Audit               AuditDescriptor
	RateLimitBucket     string
	RequestBodyMaxBytes int64
	RequestPayload      PayloadDescriptor
	ResponsePayload     PayloadDescriptor
	ResponseHeaders     []HeaderDescriptor
	Idempotency         IdempotencyDescriptor
	SDK                 SDKDescriptor
	Problems            []ProblemDescriptor
}

type IdentityDescriptor struct {
	Mode       string
	Audience   string
	Principals []string
}

type AuthorizationDescriptor struct {
	Permission         string
	OrganizationSource string
	OrganizationMember string
}

type AuditDescriptor struct {
	Event    string
	Resource string
	Action   string
}

type IdempotencyDescriptor struct {
	Policy string
	Header string
	Member string
}

type PayloadDescriptor struct {
	Member    string
	Target    string
	Kind      string
	MediaType string
	Streaming bool
	Sensitive bool
	Required  bool
}

type HeaderDescriptor struct {
	Member string
	Name   string
}

type SDKDescriptor struct {
	Module    string
	Method    string
	Paginated bool
	Retryable bool
}

type ProblemDescriptor struct {
	ShapeID string
	Type    string
	Code    string
	Status  int
}

type Operation[Input any, Output any] struct {
	Descriptor OperationDescriptor
}

type Handler[Input any, Output any] func(context.Context, *Input) (*Output, error)

type DisplayName string

type IdempotencyKey string

type ProblemCode string

type ProblemDetail string

type ProblemType string

type RequestID string

type ResourceName string

type TraceParent string

type ActorID string

type AttemptID string

type AttemptState string

type BillingWindowID string

type CacheGenerationID string

type CachePath string

type CacheID string

type CacheName string

type CorrelationID string

type DecimalUint64 string

type DispatchID string

type ExecID string

type ExecutionID string

type ExecutionKind string

type ExecutionStatus string

type ExternalProvider string

type ExternalTaskID string

type FailureReason string

type GitRef string

type HeadBranch string

type HeadSHA string

type IntervalSeconds int

type JobName string

type LeaseID string

type LogChunk string

type LogQuery string

type OrgID string

type PricingPhase string

type ProductID string

type ProjectID string

type Provider string

type ProviderOwner string

type ProviderRepo string

type ProviderRepositoryID string

type RepositoryFullName string

type ReservationShape string

type RunCommand string

type RunID string

type RunListCursor string

type RunListPageSize int

type RunLogSearchCursor string

type RunLogSearchPageSize int

type RunnerClass string

type SafeExitCode int

type SafeNonNegativeLong int64

type ScheduleID string

type ScheduleInputName string

type ScheduleInputValue string

type ScheduleListCursor string

type ScheduleListPageSize int

type ScheduleState string

type SourceKind string

type SourceRef string

type SourceRepositoryID string

type SourceWorkflowRunID string

type StreamName string

type TaskQueue string

type TemporalID string

type TemporalNamespace string

type TraceID string

type WorkflowName string

type WorkflowPath string

type WorkflowState string

type WorkloadKind string

type Runs []SandboxExecutionRecord

type SandboxAnalyticsBuckets []SandboxAnalyticsBucket

type SandboxBillingWindows []SandboxBillingWindow

type SandboxExecutionScheduleDispatches []SandboxExecutionScheduleDispatchRecord

type SandboxExecutionSchedules []SandboxExecutionScheduleRecord

type SandboxRunDurationSamples []SandboxRunDurationSample

type SandboxRunLogSearchResults []SandboxRunLogSearchResult

type SandboxRunnerSizingSamples []SandboxRunnerSizingSample

type SandboxCacheGenerations []SandboxCacheGeneration

type SandboxCachePaths []CachePath

type SandboxCaches []SandboxCache

type ScheduleInputs map[string]ScheduleInputValue

type ConflictError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type IdempotencyPayloadMismatchError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type PermissionDeniedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type RateLimitedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type ResourceNotFoundError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type ServiceUnavailableError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type UnauthenticatedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type ValidationFailedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type AnalyticsWindowInput struct {
	End   string `query:"end"`
	Start string `query:"start"`
}

type CreateExecutionScheduleInputBody struct {
	DisplayName        *DisplayName       `json:"display_name,omitempty" minLength:"1" maxLength:"120"`
	IdempotencyKey     IdempotencyKey     `json:"idempotency_key" required:"true" minLength:"8" maxLength:"128"`
	Inputs             *ScheduleInputs    `json:"inputs,omitempty" maxLength:"64"`
	IntervalSeconds    IntervalSeconds    `json:"interval_seconds" required:"true" minimum:"15" maximum:"4294967295"`
	Paused             *bool              `json:"paused,omitempty"`
	ProjectID          ProjectID          `json:"project_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	Ref                *GitRef            `json:"ref,omitempty" minLength:"1" maxLength:"1024"`
	SourceRepositoryID SourceRepositoryID `json:"source_repository_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	WorkflowPath       WorkflowPath       `json:"workflow_path" required:"true" minLength:"1" maxLength:"4096"`
}

type CreateExecutionScheduleInput struct {
	Body CreateExecutionScheduleInputBody
}

type EmptyInput struct{}

type ExecutionPathInput struct {
	ExecutionID ExecutionID `path:"execution_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
}

type ExecutionScheduleMutationInput struct {
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	ScheduleID     ScheduleID     `path:"schedule_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
}

type ExecutionSchedulePathInput struct {
	ScheduleID ScheduleID `path:"schedule_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
}

type ListExecutionSchedulesInput struct {
	Cursor ScheduleListCursor   `query:"cursor" maxLength:"4096"`
	Limit  ScheduleListPageSize `query:"limit" minimum:"1" maximum:"500"`
}

type ListRunsInput struct {
	Branch      GitRef             `query:"branch" minLength:"1" maxLength:"1024"`
	Cursor      RunListCursor      `query:"cursor" maxLength:"4096"`
	Limit       RunListPageSize    `query:"limit" minimum:"1" maximum:"200"`
	Repository  RepositoryFullName `query:"repository" minLength:"1" maxLength:"1024"`
	RunnerClass RunnerClass        `query:"runner_class" minLength:"1" maxLength:"255"`
	SourceKind  SourceKind         `query:"source_kind" minLength:"1" maxLength:"64"`
	Status      ExecutionStatus    `query:"status" minLength:"1" maxLength:"64"`
	Workflow    WorkflowName       `query:"workflow" minLength:"1" maxLength:"1024"`
}

type RunPathInput struct {
	RunID RunID `path:"run_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
}

type SandboxAnalyticsBucket struct {
	BilledChargeUnits   *DecimalUint64 `json:"billed_charge_units,omitempty" pattern:"^[0-9]+$"`
	Count               DecimalUint64  `json:"count" required:"true" pattern:"^[0-9]+$"`
	Key                 string         `json:"key" required:"true"`
	ReservedChargeUnits *DecimalUint64 `json:"reserved_charge_units,omitempty" pattern:"^[0-9]+$"`
	WriteoffChargeUnits *DecimalUint64 `json:"writeoff_charge_units,omitempty" pattern:"^[0-9]+$"`
}

type SandboxAttemptRecord struct {
	AttemptID              AttemptID            `json:"attempt_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	AttemptSeq             SafeNonNegativeLong  `json:"attempt_seq" required:"true" minimum:"0" maximum:"9007199254740991"`
	BillingJobID           *SafeNonNegativeLong `json:"billing_job_id,omitempty" minimum:"0" maximum:"9007199254740991"`
	BlockReadBytes         *SafeNonNegativeLong `json:"block_read_bytes,omitempty" minimum:"0" maximum:"9007199254740991"`
	BlockWriteBytes        *SafeNonNegativeLong `json:"block_write_bytes,omitempty" minimum:"0" maximum:"9007199254740991"`
	BootTimeUs             *SafeNonNegativeLong `json:"boot_time_us,omitempty" minimum:"0" maximum:"9007199254740991"`
	CompletedAt            *string              `json:"completed_at,omitempty"`
	CreatedAt              string               `json:"created_at" required:"true"`
	DurationMs             *SafeNonNegativeLong `json:"duration_ms,omitempty" minimum:"0" maximum:"9007199254740991"`
	ExecID                 *ExecID              `json:"exec_id,omitempty" minLength:"1" maxLength:"512"`
	ExitCode               *SafeExitCode        `json:"exit_code,omitempty" minimum:"0" maximum:"255"`
	FailureReason          *FailureReason       `json:"failure_reason,omitempty" minLength:"1" maxLength:"8192"`
	LeaseID                *LeaseID             `json:"lease_id,omitempty" minLength:"1" maxLength:"512"`
	NetRxBytes             *SafeNonNegativeLong `json:"net_rx_bytes,omitempty" minimum:"0" maximum:"9007199254740991"`
	NetTxBytes             *SafeNonNegativeLong `json:"net_tx_bytes,omitempty" minimum:"0" maximum:"9007199254740991"`
	ResourceName           *ResourceName        `json:"resourceName,omitempty" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	RootfsProvisionedBytes *SafeNonNegativeLong `json:"rootfs_provisioned_bytes,omitempty" minimum:"0" maximum:"9007199254740991"`
	StartedAt              *string              `json:"started_at,omitempty"`
	State                  AttemptState         `json:"state" required:"true" minLength:"1" maxLength:"64"`
	StderrBytes            *SafeNonNegativeLong `json:"stderr_bytes,omitempty" minimum:"0" maximum:"9007199254740991"`
	StdoutBytes            *SafeNonNegativeLong `json:"stdout_bytes,omitempty" minimum:"0" maximum:"9007199254740991"`
	TraceID                *TraceID             `json:"trace_id,omitempty" minLength:"1" maxLength:"512"`
	UpdatedAt              string               `json:"updated_at" required:"true"`
	VcpuExitCount          *SafeNonNegativeLong `json:"vcpu_exit_count,omitempty" minimum:"0" maximum:"9007199254740991"`
}

type SandboxBillingWindow struct {
	ActualQuantity      *SafeNonNegativeLong `json:"actual_quantity,omitempty" minimum:"0" maximum:"9007199254740991"`
	AttemptID           AttemptID            `json:"attempt_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	BilledChargeUnits   DecimalUint64        `json:"billed_charge_units" required:"true" pattern:"^[0-9]+$"`
	BillingWindowID     BillingWindowID      `json:"billing_window_id" required:"true" minLength:"1" maxLength:"512"`
	CostPerUnit         DecimalUint64        `json:"cost_per_unit" required:"true" pattern:"^[0-9]+$"`
	CreatedAt           string               `json:"created_at" required:"true"`
	PricingPhase        *PricingPhase        `json:"pricing_phase,omitempty" minLength:"1" maxLength:"128"`
	ReservationShape    ReservationShape     `json:"reservation_shape" required:"true" minLength:"1" maxLength:"1024"`
	ReservedChargeUnits DecimalUint64        `json:"reserved_charge_units" required:"true" pattern:"^[0-9]+$"`
	ReservedQuantity    SafeNonNegativeLong  `json:"reserved_quantity" required:"true" minimum:"0" maximum:"9007199254740991"`
	SettledAt           *string              `json:"settled_at,omitempty"`
	State               string               `json:"state" required:"true"`
	WindowSeq           SafeNonNegativeLong  `json:"window_seq" required:"true" minimum:"0" maximum:"9007199254740991"`
	WindowStart         string               `json:"window_start" required:"true"`
	WriteoffChargeUnits DecimalUint64        `json:"writeoff_charge_units" required:"true" pattern:"^[0-9]+$"`
}

type SandboxCostsAnalytics struct {
	BilledChargeUnits   DecimalUint64           `json:"billed_charge_units" required:"true" pattern:"^[0-9]+$"`
	ByRepository        SandboxAnalyticsBuckets `json:"by_repository" required:"true"`
	ByRunnerClass       SandboxAnalyticsBuckets `json:"by_runner_class" required:"true"`
	BySource            SandboxAnalyticsBuckets `json:"by_source" required:"true"`
	ReservedChargeUnits DecimalUint64           `json:"reserved_charge_units" required:"true" pattern:"^[0-9]+$"`
	WindowEnd           string                  `json:"window_end" required:"true"`
	WindowStart         string                  `json:"window_start" required:"true"`
	WriteoffChargeUnits DecimalUint64           `json:"writeoff_charge_units" required:"true" pattern:"^[0-9]+$"`
}

type SandboxExecutionLogs struct {
	AttemptID   AttemptID   `json:"attempt_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	ExecutionID ExecutionID `json:"execution_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	Logs        string      `json:"logs" required:"true"`
}

type SandboxExecutionRecord struct {
	ActorID          ActorID                     `json:"actor_id" required:"true" minLength:"1" maxLength:"128"`
	BillingSummary   *SandboxRunBillingSummary   `json:"billing_summary,omitempty"`
	BillingWindows   *SandboxBillingWindows      `json:"billing_windows,omitempty"`
	CorrelationID    *CorrelationID              `json:"correlation_id,omitempty" minLength:"1" maxLength:"512"`
	CreatedAt        string                      `json:"created_at" required:"true"`
	ExecutionID      ExecutionID                 `json:"execution_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	ExternalProvider *ExternalProvider           `json:"external_provider,omitempty" minLength:"1" maxLength:"255"`
	ExternalTaskID   *ExternalTaskID             `json:"external_task_id,omitempty" minLength:"1" maxLength:"255"`
	IdempotencyKey   *IdempotencyKey             `json:"idempotency_key,omitempty" minLength:"8" maxLength:"128"`
	Kind             ExecutionKind               `json:"kind" required:"true" minLength:"1" maxLength:"64"`
	LatestAttempt    SandboxAttemptRecord        `json:"latest_attempt" required:"true"`
	OrgID            OrgID                       `json:"org_id" required:"true" minLength:"1" maxLength:"128"`
	ProductID        ProductID                   `json:"product_id" required:"true" minLength:"1" maxLength:"64"`
	Provider         *Provider                   `json:"provider,omitempty" minLength:"1" maxLength:"64"`
	ResourceName     ResourceName                `json:"resourceName" required:"true" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	RunCommand       *RunCommand                 `json:"run_command,omitempty" minLength:"1" maxLength:"8192"`
	RunID            RunID                       `json:"run_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	Runner           *SandboxRunnerRunMetadata   `json:"runner,omitempty"`
	RunnerClass      *RunnerClass                `json:"runner_class,omitempty" minLength:"1" maxLength:"255"`
	Schedule         *SandboxScheduleRunMetadata `json:"schedule,omitempty"`
	SourceKind       *SourceKind                 `json:"source_kind,omitempty" minLength:"1" maxLength:"64"`
	SourceRef        *SourceRef                  `json:"source_ref,omitempty" minLength:"1" maxLength:"4096"`
	Status           ExecutionStatus             `json:"status" required:"true" minLength:"1" maxLength:"64"`
	UpdatedAt        string                      `json:"updated_at" required:"true"`
	WorkloadKind     *WorkloadKind               `json:"workload_kind,omitempty" minLength:"1" maxLength:"64"`
}

type SandboxExecutionScheduleDispatchRecord struct {
	CreatedAt           string               `json:"created_at" required:"true"`
	DispatchID          DispatchID           `json:"dispatch_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	FailureReason       *FailureReason       `json:"failure_reason,omitempty" minLength:"1" maxLength:"8192"`
	ScheduleID          ScheduleID           `json:"schedule_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	ScheduledAt         string               `json:"scheduled_at" required:"true"`
	SourceWorkflowRunID *SourceWorkflowRunID `json:"source_workflow_run_id,omitempty" pattern:"^[0-9a-fA-F-]{36}$"`
	State               ScheduleState        `json:"state" required:"true" minLength:"1" maxLength:"64"`
	SubmittedAt         *string              `json:"submitted_at,omitempty"`
	TemporalRunID       TemporalID           `json:"temporal_run_id" required:"true" minLength:"1" maxLength:"512"`
	TemporalWorkflowID  TemporalID           `json:"temporal_workflow_id" required:"true" minLength:"1" maxLength:"512"`
	UpdatedAt           string               `json:"updated_at" required:"true"`
	WorkflowState       *WorkflowState       `json:"workflow_state,omitempty" minLength:"1" maxLength:"64"`
}

type SandboxExecutionScheduleRecord struct {
	ActorID                      ActorID                             `json:"actor_id" required:"true" minLength:"1" maxLength:"128"`
	CreatedAt                    string                              `json:"created_at" required:"true"`
	Dispatches                   *SandboxExecutionScheduleDispatches `json:"dispatches,omitempty"`
	DisplayName                  *DisplayName                        `json:"display_name,omitempty" minLength:"1" maxLength:"120"`
	IdempotencyKey               *IdempotencyKey                     `json:"idempotency_key,omitempty" minLength:"8" maxLength:"128"`
	Inputs                       *ScheduleInputs                     `json:"inputs,omitempty" maxLength:"64"`
	IntervalSeconds              IntervalSeconds                     `json:"interval_seconds" required:"true" minimum:"15" maximum:"4294967295"`
	OrgID                        OrgID                               `json:"org_id" required:"true" minLength:"1" maxLength:"128"`
	ProjectResourceName          ResourceName                        `json:"projectResourceName" required:"true" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	ProjectID                    ProjectID                           `json:"project_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	Ref                          *GitRef                             `json:"ref,omitempty" minLength:"1" maxLength:"1024"`
	ResourceName                 ResourceName                        `json:"resourceName" required:"true" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	ScheduleID                   ScheduleID                          `json:"schedule_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	SourceRepositoryResourceName ResourceName                        `json:"sourceRepositoryResourceName" required:"true" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	SourceRepositoryID           SourceRepositoryID                  `json:"source_repository_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	State                        ScheduleState                       `json:"state" required:"true" minLength:"1" maxLength:"64"`
	TaskQueue                    TaskQueue                           `json:"task_queue" required:"true" minLength:"1" maxLength:"255"`
	TemporalNamespace            TemporalNamespace                   `json:"temporal_namespace" required:"true" minLength:"1" maxLength:"255"`
	TemporalScheduleID           TemporalID                          `json:"temporal_schedule_id" required:"true" minLength:"1" maxLength:"512"`
	UpdatedAt                    string                              `json:"updated_at" required:"true"`
	WorkflowPath                 WorkflowPath                        `json:"workflow_path" required:"true" minLength:"1" maxLength:"4096"`
}

type SandboxJobsAnalytics struct {
	ByRunnerClass SandboxAnalyticsBuckets   `json:"by_runner_class" required:"true"`
	BySource      SandboxAnalyticsBuckets   `json:"by_source" required:"true"`
	FailedRuns    DecimalUint64             `json:"failed_runs" required:"true" pattern:"^[0-9]+$"`
	P50DurationMs DecimalUint64             `json:"p50_duration_ms" required:"true" pattern:"^[0-9]+$"`
	P95DurationMs DecimalUint64             `json:"p95_duration_ms" required:"true" pattern:"^[0-9]+$"`
	P99DurationMs DecimalUint64             `json:"p99_duration_ms" required:"true" pattern:"^[0-9]+$"`
	SlowestRuns   SandboxRunDurationSamples `json:"slowest_runs" required:"true"`
	SucceededRuns DecimalUint64             `json:"succeeded_runs" required:"true" pattern:"^[0-9]+$"`
	TotalRuns     DecimalUint64             `json:"total_runs" required:"true" pattern:"^[0-9]+$"`
	WindowEnd     string                    `json:"window_end" required:"true"`
	WindowStart   string                    `json:"window_start" required:"true"`
}

type SandboxRunBillingSummary struct {
	BilledChargeUnits   DecimalUint64       `json:"billed_charge_units" required:"true" pattern:"^[0-9]+$"`
	CostPerUnit         DecimalUint64       `json:"cost_per_unit" required:"true" pattern:"^[0-9]+$"`
	PricingPhase        *PricingPhase       `json:"pricing_phase,omitempty" minLength:"1" maxLength:"128"`
	ReservedChargeUnits DecimalUint64       `json:"reserved_charge_units" required:"true" pattern:"^[0-9]+$"`
	WindowCount         SafeNonNegativeLong `json:"window_count" required:"true" minimum:"0" maximum:"9007199254740991"`
	WriteoffChargeUnits DecimalUint64       `json:"writeoff_charge_units" required:"true" pattern:"^[0-9]+$"`
}

type SandboxRunDurationSample struct {
	CompletedAt        string              `json:"completed_at" required:"true"`
	DurationMs         SafeNonNegativeLong `json:"duration_ms" required:"true" minimum:"0" maximum:"9007199254740991"`
	ExecutionID        ExecutionID         `json:"execution_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	JobName            *JobName            `json:"job_name,omitempty" minLength:"1" maxLength:"1024"`
	RepositoryFullName *RepositoryFullName `json:"repository_full_name,omitempty" minLength:"1" maxLength:"1024"`
	RunnerClass        *RunnerClass        `json:"runner_class,omitempty" minLength:"1" maxLength:"255"`
	Status             ExecutionStatus     `json:"status" required:"true" minLength:"1" maxLength:"64"`
	WorkflowName       *WorkflowName       `json:"workflow_name,omitempty" minLength:"1" maxLength:"1024"`
}

type SandboxRunLogSearchFilters struct {
	AttemptID   *AttemptID          `json:"attempt_id,omitempty" pattern:"^[0-9a-fA-F-]{36}$"`
	Branch      *GitRef             `json:"branch,omitempty" minLength:"1" maxLength:"1024"`
	Query       *LogQuery           `json:"query,omitempty" maxLength:"4096"`
	Repository  *RepositoryFullName `json:"repository,omitempty" minLength:"1" maxLength:"1024"`
	RunID       *RunID              `json:"run_id,omitempty" pattern:"^[0-9a-fA-F-]{36}$"`
	RunnerClass *RunnerClass        `json:"runner_class,omitempty" minLength:"1" maxLength:"255"`
	SourceKind  *SourceKind         `json:"source_kind,omitempty" minLength:"1" maxLength:"64"`
	Workflow    *WorkflowName       `json:"workflow,omitempty" minLength:"1" maxLength:"1024"`
}

type SandboxRunLogSearchResult struct {
	AttemptID          AttemptID           `json:"attempt_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	Chunk              LogChunk            `json:"chunk" required:"true" minLength:"1" maxLength:"4096"`
	CreatedAt          string              `json:"created_at" required:"true"`
	ExecutionID        ExecutionID         `json:"execution_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	HeadBranch         *HeadBranch         `json:"head_branch,omitempty" minLength:"1" maxLength:"1024"`
	JobName            *JobName            `json:"job_name,omitempty" minLength:"1" maxLength:"1024"`
	RepositoryFullName *RepositoryFullName `json:"repository_full_name,omitempty" minLength:"1" maxLength:"1024"`
	RunnerClass        *RunnerClass        `json:"runner_class,omitempty" minLength:"1" maxLength:"255"`
	ScheduleID         *ScheduleID         `json:"schedule_id,omitempty" pattern:"^[0-9a-fA-F-]{36}$"`
	Seq                SafeNonNegativeLong `json:"seq" required:"true" minimum:"0" maximum:"9007199254740991"`
	SourceKind         *SourceKind         `json:"source_kind,omitempty" minLength:"1" maxLength:"64"`
	Stream             StreamName          `json:"stream" required:"true" minLength:"1" maxLength:"64"`
	WorkflowName       *WorkflowName       `json:"workflow_name,omitempty" minLength:"1" maxLength:"1024"`
	WorkloadKind       *WorkloadKind       `json:"workload_kind,omitempty" minLength:"1" maxLength:"64"`
}

type SandboxRunnerRunMetadata struct {
	HeadBranch             *HeadBranch         `json:"head_branch,omitempty" minLength:"1" maxLength:"1024"`
	HeadSha                *HeadSHA            `json:"head_sha,omitempty" minLength:"1" maxLength:"128"`
	JobName                *JobName            `json:"job_name,omitempty" minLength:"1" maxLength:"1024"`
	ProviderInstallationID *DecimalUint64      `json:"provider_installation_id,omitempty" pattern:"^[0-9]+$"`
	ProviderJobID          *DecimalUint64      `json:"provider_job_id,omitempty" pattern:"^[0-9]+$"`
	ProviderRunID          *DecimalUint64      `json:"provider_run_id,omitempty" pattern:"^[0-9]+$"`
	RepositoryFullName     *RepositoryFullName `json:"repository_full_name,omitempty" minLength:"1" maxLength:"1024"`
	WorkflowName           *WorkflowName       `json:"workflow_name,omitempty" minLength:"1" maxLength:"1024"`
}

type SandboxRunnerSizingAnalytics struct {
	ByRunnerClass SandboxRunnerSizingSamples `json:"by_runner_class" required:"true"`
	WindowEnd     string                     `json:"window_end" required:"true"`
	WindowStart   string                     `json:"window_start" required:"true"`
}

type SandboxRunnerSizingSample struct {
	AvgBlockWriteBytes        DecimalUint64 `json:"avg_block_write_bytes" required:"true" pattern:"^[0-9]+$"`
	AvgBootTimeUs             DecimalUint64 `json:"avg_boot_time_us" required:"true" pattern:"^[0-9]+$"`
	AvgNetTxBytes             DecimalUint64 `json:"avg_net_tx_bytes" required:"true" pattern:"^[0-9]+$"`
	AvgRootfsProvisionedBytes DecimalUint64 `json:"avg_rootfs_provisioned_bytes" required:"true" pattern:"^[0-9]+$"`
	P95DurationMs             DecimalUint64 `json:"p95_duration_ms" required:"true" pattern:"^[0-9]+$"`
	RunCount                  DecimalUint64 `json:"run_count" required:"true" pattern:"^[0-9]+$"`
	RunnerClass               RunnerClass   `json:"runner_class" required:"true" minLength:"1" maxLength:"255"`
}

type SandboxRunsFilters struct {
	Branch      *GitRef             `json:"branch,omitempty" minLength:"1" maxLength:"1024"`
	Repository  *RepositoryFullName `json:"repository,omitempty" minLength:"1" maxLength:"1024"`
	RunnerClass *RunnerClass        `json:"runner_class,omitempty" minLength:"1" maxLength:"255"`
	SourceKind  *SourceKind         `json:"source_kind,omitempty" minLength:"1" maxLength:"64"`
	Status      *ExecutionStatus    `json:"status,omitempty" minLength:"1" maxLength:"64"`
	Workflow    *WorkflowName       `json:"workflow,omitempty" minLength:"1" maxLength:"1024"`
}

type SandboxScheduleRunMetadata struct {
	DisplayName          *DisplayName  `json:"display_name,omitempty" minLength:"1" maxLength:"120"`
	ScheduleResourceName *ResourceName `json:"scheduleResourceName,omitempty" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	ScheduleID           *ScheduleID   `json:"schedule_id,omitempty" pattern:"^[0-9a-fA-F-]{36}$"`
	TemporalRunID        *TemporalID   `json:"temporal_run_id,omitempty" minLength:"1" maxLength:"512"`
	TemporalWorkflowID   *TemporalID   `json:"temporal_workflow_id,omitempty" minLength:"1" maxLength:"512"`
}

type SandboxCache struct {
	CacheID              CacheID              `json:"cache_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	CreatedAt            string               `json:"created_at" required:"true"`
	CurrentGenerationID  *CacheGenerationID   `json:"current_generation_id,omitempty" pattern:"^[0-9a-fA-F-]{36}$"`
	GenerationCount      SafeNonNegativeLong  `json:"generation_count" required:"true" minimum:"0"`
	LastUsedAt           string               `json:"last_used_at" required:"true"`
	Name                 CacheName            `json:"name" required:"true" minLength:"1" maxLength:"128"`
	OrgID                OrgID                `json:"org_id" required:"true" minLength:"1" maxLength:"128" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	Provider             Provider             `json:"provider" required:"true" minLength:"1" maxLength:"64"`
	ProviderRepositoryID ProviderRepositoryID `json:"provider_repository_id" required:"true" minLength:"1" maxLength:"512"`
	RepositoryFullName   *RepositoryFullName  `json:"repository_full_name,omitempty" minLength:"1" maxLength:"1024"`
	ResourceName         ResourceName         `json:"resourceName" required:"true" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	ScopeRef             GitRef               `json:"scope_ref" required:"true" minLength:"1" maxLength:"1024"`
	UsedBytes            SafeNonNegativeLong  `json:"used_bytes" required:"true" minimum:"0"`
	WrittenBytes         SafeNonNegativeLong  `json:"written_bytes" required:"true" minimum:"0"`
}

type SandboxCacheGeneration struct {
	BindPaths            SandboxCachePaths    `json:"bind_paths" required:"true"`
	CacheGenerationID    CacheGenerationID    `json:"cache_generation_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	CacheID              CacheID              `json:"cache_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	CommittedAt          *string              `json:"committed_at,omitempty"`
	Current              bool                 `json:"current" required:"true"`
	ExpiresAt            *string              `json:"expires_at,omitempty"`
	HeadSha              *HeadSHA             `json:"head_sha,omitempty" minLength:"1" maxLength:"128"`
	LastUsedAt           *string              `json:"last_used_at,omitempty"`
	OrgID                OrgID                `json:"org_id" required:"true" minLength:"1" maxLength:"128" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	Provider             Provider             `json:"provider" required:"true" minLength:"1" maxLength:"64"`
	ProviderJobID        DecimalUint64        `json:"provider_job_id" required:"true" pattern:"^[0-9]+$"`
	ProviderRepositoryID ProviderRepositoryID `json:"provider_repository_id" required:"true" minLength:"1" maxLength:"512"`
	ProviderRunAttempt   DecimalUint64        `json:"provider_run_attempt" required:"true" pattern:"^[0-9]+$"`
	ProviderRunID        DecimalUint64        `json:"provider_run_id" required:"true" pattern:"^[0-9]+$"`
	RepositoryFullName   *RepositoryFullName  `json:"repository_full_name,omitempty" minLength:"1" maxLength:"1024"`
	Result               string               `json:"result" required:"true"`
	ScopeRef             GitRef               `json:"scope_ref" required:"true" minLength:"1" maxLength:"1024"`
	SealedAt             *string              `json:"sealed_at,omitempty"`
	SourceGenerationID   *CacheGenerationID   `json:"source_generation_id,omitempty" pattern:"^[0-9a-fA-F-]{36}$"`
	State                string               `json:"state" required:"true"`
	TreeHash             *string              `json:"tree_hash,omitempty"`
	UsedBytes            SafeNonNegativeLong  `json:"used_bytes" required:"true" minimum:"0"`
	CacheName            CacheName            `json:"cache_name" required:"true" minLength:"1" maxLength:"128"`
	WrittenBytes         SafeNonNegativeLong  `json:"written_bytes" required:"true" minimum:"0"`
	ZFSSnapshotRef       string               `json:"zfs_snapshot_ref" required:"true"`
}

type SearchRunLogsInput struct {
	AttemptID   AttemptID            `query:"attempt_id" pattern:"^[0-9a-fA-F-]{36}$"`
	Branch      GitRef               `query:"branch" minLength:"1" maxLength:"1024"`
	Cursor      RunLogSearchCursor   `query:"cursor" maxLength:"4096"`
	Limit       RunLogSearchPageSize `query:"limit" minimum:"1" maximum:"500"`
	Query       LogQuery             `query:"query" maxLength:"4096"`
	Repository  RepositoryFullName   `query:"repository" minLength:"1" maxLength:"1024"`
	RunID       RunID                `query:"run_id" pattern:"^[0-9a-fA-F-]{36}$"`
	RunnerClass RunnerClass          `query:"runner_class" minLength:"1" maxLength:"255"`
	SourceKind  SourceKind           `query:"source_kind" minLength:"1" maxLength:"64"`
	Workflow    WorkflowName         `query:"workflow" minLength:"1" maxLength:"1024"`
}

type CachePathInput struct {
	CacheID CacheID `path:"cache_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
}

type DeleteCacheGenerationInput struct {
	CacheGenerationID CacheGenerationID `path:"cache_generation_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey    IdempotencyKey    `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
}

type DeleteCachePathInputBody struct {
	Path CachePath `json:"path" required:"true" minLength:"1" maxLength:"255"`
}

type DeleteCachePathInput struct {
	Body           DeleteCachePathInputBody
	CacheID        CacheID        `path:"cache_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
}

type SandboxExecutionSchedulesPageBody struct {
	Limit      ScheduleListPageSize      `json:"limit" required:"true" minimum:"1" maximum:"500"`
	NextCursor *ScheduleListCursor       `json:"next_cursor,omitempty" maxLength:"4096"`
	Schedules  SandboxExecutionSchedules `json:"schedules" required:"true"`
}

type SandboxExecutionSchedulesPage struct {
	Body SandboxExecutionSchedulesPageBody
}

type SandboxExecutionScheduleOutput struct {
	Body SandboxExecutionScheduleRecord
}

type SandboxExecutionOutput struct {
	Body SandboxExecutionRecord
}

type SandboxExecutionLogsOutput struct {
	Body SandboxExecutionLogs
}

type SandboxCostsAnalyticsOutput struct {
	Body SandboxCostsAnalytics
}

type SandboxJobsAnalyticsOutput struct {
	Body SandboxJobsAnalytics
}

type SandboxRunnerSizingAnalyticsOutput struct {
	Body SandboxRunnerSizingAnalytics
}

type SandboxRunLogSearchPageBody struct {
	Filters    SandboxRunLogSearchFilters `json:"filters" required:"true"`
	Limit      RunLogSearchPageSize       `json:"limit" required:"true" minimum:"1" maximum:"500"`
	NextCursor *RunLogSearchCursor        `json:"next_cursor,omitempty" maxLength:"4096"`
	Results    SandboxRunLogSearchResults `json:"results" required:"true"`
}

type SandboxRunLogSearchPage struct {
	Body SandboxRunLogSearchPageBody
}

type SandboxRunsPageBody struct {
	Filters    SandboxRunsFilters `json:"filters" required:"true"`
	Limit      RunListPageSize    `json:"limit" required:"true" minimum:"1" maximum:"200"`
	NextCursor *RunListCursor     `json:"next_cursor,omitempty" maxLength:"4096"`
	Runs       Runs               `json:"runs" required:"true"`
}

type SandboxRunsPage struct {
	Body SandboxRunsPageBody
}

type SandboxCachesPageBody struct {
	Caches SandboxCaches `json:"caches" required:"true"`
}

type SandboxCachesPage struct {
	Body SandboxCachesPageBody
}

type SandboxCacheGenerationsPageBody struct {
	Generations SandboxCacheGenerations `json:"generations" required:"true"`
}

type SandboxCacheGenerationsPage struct {
	Body SandboxCacheGenerationsPageBody
}

type SandboxCacheGenerationDeleteResult struct {
	Generation SandboxCacheGeneration `json:"generation" required:"true"`
}

type SandboxCacheGenerationDeleteOutput struct {
	Body SandboxCacheGenerationDeleteResult
}

type SandboxCachePathDeleteResult struct {
	CacheID     CacheID                 `json:"cache_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	Generations SandboxCacheGenerations `json:"generations" required:"true"`
	Path        CachePath               `json:"path" required:"true" minLength:"1" maxLength:"255"`
}

type SandboxCachePathDeleteOutput struct {
	Body SandboxCachePathDeleteResult
}

var Operations = []OperationDescriptor{
	ListCaches.Descriptor,
	ListCacheGenerations.Descriptor,
	DeleteCacheGeneration.Descriptor,
	DeleteCachePath.Descriptor,
	ListExecutionSchedules.Descriptor,
	CreateExecutionSchedule.Descriptor,
	GetExecutionSchedule.Descriptor,
	PauseExecutionSchedule.Descriptor,
	ResumeExecutionSchedule.Descriptor,
	GetExecution.Descriptor,
	GetExecutionLogs.Descriptor,
	GetCostsAnalytics.Descriptor,
	GetJobsAnalytics.Descriptor,
	GetRunnerSizingAnalytics.Descriptor,
	SearchRunLogs.Descriptor,
	ListRuns.Descriptor,
	GetRun.Descriptor,
}

var ListCaches = Operation[EmptyInput, SandboxCachesPage]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#ListCaches",
		OperationID:         "list-caches",
		Method:              "GET",
		Path:                "/api/v1/caches",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:cache:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "sandbox.cache.list", Resource: "cache", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "sandbox.cache", Method: "list", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var ListCacheGenerations = Operation[CachePathInput, SandboxCacheGenerationsPage]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#ListCacheGenerations",
		OperationID:         "list-cache-generations",
		Method:              "GET",
		Path:                "/api/v1/caches/{cache_id}/generations",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:cache:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "sandbox.cache_generation.list", Resource: "cache_generation", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "sandbox.cache", Method: "listGenerations", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var DeleteCacheGeneration = Operation[DeleteCacheGenerationInput, SandboxCacheGenerationDeleteOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#DeleteCacheGeneration",
		OperationID:         "delete-cache-generation",
		Method:              "POST",
		Path:                "/api/v1/cache/generations/{cache_generation_id}/delete",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:cache:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "sandbox.cache_generation.delete", Resource: "cache_generation", Action: "delete"},
		RateLimitBucket:     "cache_mutation",
		RequestBodyMaxBytes: 1024,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "sandbox.cache", Method: "deleteGeneration", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:idempotency:payload_mismatch", Code: "idempotency.payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var DeleteCachePath = Operation[DeleteCachePathInput, SandboxCachePathDeleteOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#DeleteCachePath",
		OperationID:         "delete-cache-path",
		Method:              "POST",
		Path:                "/api/v1/caches/{cache_id}/paths/delete",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:cache:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "sandbox.cache_path.delete", Resource: "cache", Action: "delete"},
		RateLimitBucket:     "cache_mutation",
		RequestBodyMaxBytes: 4096,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "sandbox.cache", Method: "deletePath", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:idempotency:payload_mismatch", Code: "idempotency.payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var ListExecutionSchedules = Operation[ListExecutionSchedulesInput, SandboxExecutionSchedulesPage]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#ListExecutionSchedules",
		OperationID:         "list-execution-schedules",
		Method:              "GET",
		Path:                "/api/v1/execution-schedules",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           true,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:execution_schedule:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "sandbox.execution_schedule.list", Resource: "execution_schedule", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "sandbox.schedules", Method: "list", Paginated: true, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var CreateExecutionSchedule = Operation[CreateExecutionScheduleInput, SandboxExecutionScheduleOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#CreateExecutionSchedule",
		OperationID:         "create-execution-schedule",
		Method:              "POST",
		Path:                "/api/v1/execution-schedules",
		DefaultStatus:       201,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:execution_schedule:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "sandbox.execution_schedule.create", Resource: "execution_schedule", Action: "create"},
		RateLimitBucket:     "execution_schedule_mutation",
		RequestBodyMaxBytes: 262144,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "request_body_idempotency_key", Header: "", Member: "idempotency_key"},
		SDK:                 SDKDescriptor{Module: "sandbox.schedules", Method: "create", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var GetExecutionSchedule = Operation[ExecutionSchedulePathInput, SandboxExecutionScheduleOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#GetExecutionSchedule",
		OperationID:         "get-execution-schedule",
		Method:              "GET",
		Path:                "/api/v1/execution-schedules/{schedule_id}",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:execution_schedule:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "sandbox.execution_schedule.read", Resource: "execution_schedule", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "sandbox.schedules", Method: "get", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var PauseExecutionSchedule = Operation[ExecutionScheduleMutationInput, SandboxExecutionScheduleOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#PauseExecutionSchedule",
		OperationID:         "pause-execution-schedule",
		Method:              "POST",
		Path:                "/api/v1/execution-schedules/{schedule_id}/pause",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:execution_schedule:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "sandbox.execution_schedule.pause", Resource: "execution_schedule", Action: "pause"},
		RateLimitBucket:     "execution_schedule_mutation",
		RequestBodyMaxBytes: 1024,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "sandbox.schedules", Method: "pause", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var ResumeExecutionSchedule = Operation[ExecutionScheduleMutationInput, SandboxExecutionScheduleOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#ResumeExecutionSchedule",
		OperationID:         "resume-execution-schedule",
		Method:              "POST",
		Path:                "/api/v1/execution-schedules/{schedule_id}/resume",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:execution_schedule:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "sandbox.execution_schedule.resume", Resource: "execution_schedule", Action: "resume"},
		RateLimitBucket:     "execution_schedule_mutation",
		RequestBodyMaxBytes: 1024,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "sandbox.schedules", Method: "resume", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var GetExecution = Operation[ExecutionPathInput, SandboxExecutionOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#GetExecution",
		OperationID:         "get-execution",
		Method:              "GET",
		Path:                "/api/v1/executions/{execution_id}",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:execution:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "sandbox.execution.read", Resource: "execution", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "sandbox.executions", Method: "get", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var GetExecutionLogs = Operation[ExecutionPathInput, SandboxExecutionLogsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#GetExecutionLogs",
		OperationID:         "get-execution-logs",
		Method:              "GET",
		Path:                "/api/v1/executions/{execution_id}/logs",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:logs:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "sandbox.execution.logs.read", Resource: "execution_logs_resource", Action: "read"},
		RateLimitBucket:     "logs_read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "sandbox.executions", Method: "getLogs", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var GetCostsAnalytics = Operation[AnalyticsWindowInput, SandboxCostsAnalyticsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#GetCostsAnalytics",
		OperationID:         "get-costs-analytics",
		Method:              "GET",
		Path:                "/api/v1/run-analytics/costs",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:analytics:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "sandbox.run_analytics.costs.read", Resource: "run_analytics_costs_resource", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "sandbox.analytics", Method: "costs", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var GetJobsAnalytics = Operation[AnalyticsWindowInput, SandboxJobsAnalyticsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#GetJobsAnalytics",
		OperationID:         "get-jobs-analytics",
		Method:              "GET",
		Path:                "/api/v1/run-analytics/jobs",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:analytics:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "sandbox.run_analytics.jobs.read", Resource: "run_analytics_jobs_resource", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "sandbox.analytics", Method: "jobs", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var GetRunnerSizingAnalytics = Operation[AnalyticsWindowInput, SandboxRunnerSizingAnalyticsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#GetRunnerSizingAnalytics",
		OperationID:         "get-runner-sizing-analytics",
		Method:              "GET",
		Path:                "/api/v1/run-analytics/runner-sizing",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:analytics:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "sandbox.run_analytics.runner_sizing.read", Resource: "run_analytics_runner_sizing_resource", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "sandbox.analytics", Method: "runnerSizing", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var SearchRunLogs = Operation[SearchRunLogsInput, SandboxRunLogSearchPage]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#SearchRunLogs",
		OperationID:         "search-run-logs",
		Method:              "GET",
		Path:                "/api/v1/run-logs/search",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           true,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:logs:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "sandbox.run_logs.search", Resource: "run_logs_resource", Action: "search"},
		RateLimitBucket:     "logs_read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "sandbox.runLogs", Method: "search", Paginated: true, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var ListRuns = Operation[ListRunsInput, SandboxRunsPage]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#ListRuns",
		OperationID:         "list-runs",
		Method:              "GET",
		Path:                "/api/v1/runs",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           true,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:execution:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "sandbox.run.list", Resource: "run", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "sandbox.runs", Method: "list", Paginated: true, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var GetRun = Operation[RunPathInput, SandboxExecutionOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#GetRun",
		OperationID:         "get-run",
		Method:              "GET",
		Path:                "/api/v1/runs/{run_id}",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:execution:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "sandbox.run.read", Resource: "run", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "sandbox.runs", Method: "get", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

type Handlers = PublicHandlers

type PublicHandlers interface {
	ListCaches(context.Context, *EmptyInput) (*SandboxCachesPage, error)
	ListCacheGenerations(context.Context, *CachePathInput) (*SandboxCacheGenerationsPage, error)
	DeleteCacheGeneration(context.Context, *DeleteCacheGenerationInput) (*SandboxCacheGenerationDeleteOutput, error)
	DeleteCachePath(context.Context, *DeleteCachePathInput) (*SandboxCachePathDeleteOutput, error)
	ListExecutionSchedules(context.Context, *ListExecutionSchedulesInput) (*SandboxExecutionSchedulesPage, error)
	CreateExecutionSchedule(context.Context, *CreateExecutionScheduleInput) (*SandboxExecutionScheduleOutput, error)
	GetExecutionSchedule(context.Context, *ExecutionSchedulePathInput) (*SandboxExecutionScheduleOutput, error)
	PauseExecutionSchedule(context.Context, *ExecutionScheduleMutationInput) (*SandboxExecutionScheduleOutput, error)
	ResumeExecutionSchedule(context.Context, *ExecutionScheduleMutationInput) (*SandboxExecutionScheduleOutput, error)
	GetExecution(context.Context, *ExecutionPathInput) (*SandboxExecutionOutput, error)
	GetExecutionLogs(context.Context, *ExecutionPathInput) (*SandboxExecutionLogsOutput, error)
	GetCostsAnalytics(context.Context, *AnalyticsWindowInput) (*SandboxCostsAnalyticsOutput, error)
	GetJobsAnalytics(context.Context, *AnalyticsWindowInput) (*SandboxJobsAnalyticsOutput, error)
	GetRunnerSizingAnalytics(context.Context, *AnalyticsWindowInput) (*SandboxRunnerSizingAnalyticsOutput, error)
	SearchRunLogs(context.Context, *SearchRunLogsInput) (*SandboxRunLogSearchPage, error)
	ListRuns(context.Context, *ListRunsInput) (*SandboxRunsPage, error)
	GetRun(context.Context, *RunPathInput) (*SandboxExecutionOutput, error)
}

type ListCachesHandler = Handler[EmptyInput, SandboxCachesPage]

type ListCacheGenerationsHandler = Handler[CachePathInput, SandboxCacheGenerationsPage]

type DeleteCacheGenerationHandler = Handler[DeleteCacheGenerationInput, SandboxCacheGenerationDeleteOutput]

type DeleteCachePathHandler = Handler[DeleteCachePathInput, SandboxCachePathDeleteOutput]

type ListExecutionSchedulesHandler = Handler[ListExecutionSchedulesInput, SandboxExecutionSchedulesPage]

type CreateExecutionScheduleHandler = Handler[CreateExecutionScheduleInput, SandboxExecutionScheduleOutput]

type GetExecutionScheduleHandler = Handler[ExecutionSchedulePathInput, SandboxExecutionScheduleOutput]

type PauseExecutionScheduleHandler = Handler[ExecutionScheduleMutationInput, SandboxExecutionScheduleOutput]

type ResumeExecutionScheduleHandler = Handler[ExecutionScheduleMutationInput, SandboxExecutionScheduleOutput]

type GetExecutionHandler = Handler[ExecutionPathInput, SandboxExecutionOutput]

type GetExecutionLogsHandler = Handler[ExecutionPathInput, SandboxExecutionLogsOutput]

type GetCostsAnalyticsHandler = Handler[AnalyticsWindowInput, SandboxCostsAnalyticsOutput]

type GetJobsAnalyticsHandler = Handler[AnalyticsWindowInput, SandboxJobsAnalyticsOutput]

type GetRunnerSizingAnalyticsHandler = Handler[AnalyticsWindowInput, SandboxRunnerSizingAnalyticsOutput]

type SearchRunLogsHandler = Handler[SearchRunLogsInput, SandboxRunLogSearchPage]

type ListRunsHandler = Handler[ListRunsInput, SandboxRunsPage]

type GetRunHandler = Handler[RunPathInput, SandboxExecutionOutput]
