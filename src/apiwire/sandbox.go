package apiwire

import (
	"time"

	"github.com/google/uuid"
)

type SandboxGitHubInstallationConnectResponse struct {
	State     string    `json:"state" doc:"Opaque installation state token embedded in the GitHub App setup URL."`
	SetupURL  string    `json:"setup_url" doc:"GitHub App installation URL for the current Verself organization."`
	ExpiresAt time.Time `json:"expires_at" doc:"Time after which the setup URL state is no longer accepted."`
}

type SandboxGitHubInstallationRecord struct {
	InstallationID string    `json:"installation_id" doc:"GitHub App installation ID encoded as a string for JavaScript-safe transport."`
	OrgID          OrgID     `json:"org_id"`
	AccountLogin   string    `json:"account_login"`
	AccountType    string    `json:"account_type"`
	Active         bool      `json:"active"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type SandboxExecutionRecord struct {
	RunID            uuid.UUID                         `json:"run_id"`
	ExecutionID      uuid.UUID                         `json:"execution_id"`
	OrgID            OrgID                             `json:"org_id"`
	ActorID          string                            `json:"actor_id"`
	Kind             string                            `json:"kind"`
	SourceKind       string                            `json:"source_kind,omitempty"`
	WorkloadKind     string                            `json:"workload_kind,omitempty"`
	SourceRef        string                            `json:"source_ref,omitempty"`
	RunnerClass      string                            `json:"runner_class,omitempty"`
	ExternalProvider string                            `json:"external_provider,omitempty"`
	ExternalTaskID   string                            `json:"external_task_id,omitempty"`
	Provider         string                            `json:"provider,omitempty"`
	ProductID        string                            `json:"product_id"`
	Status           string                            `json:"status"`
	CorrelationID    string                            `json:"correlation_id,omitempty"`
	IdempotencyKey   string                            `json:"idempotency_key,omitempty"`
	RunCommand       string                            `json:"run_command,omitempty"`
	LatestAttempt    SandboxAttemptRecord              `json:"latest_attempt"`
	CreatedAt        time.Time                         `json:"created_at"`
	UpdatedAt        time.Time                         `json:"updated_at"`
	BillingWindows   []SandboxBillingWindow            `json:"billing_windows,omitempty"`
	BillingSummary   *SandboxRunBillingSummary         `json:"billing_summary,omitempty"`
	Runner           *SandboxRunnerRunMetadata         `json:"runner,omitempty"`
	Schedule         *SandboxScheduleRunMetadata       `json:"schedule,omitempty"`
	StickyDiskMounts []SandboxExecutionStickyDiskMount `json:"sticky_disk_mounts,omitempty"`
}

type SandboxExecutionLogs struct {
	ExecutionID string `json:"execution_id"`
	AttemptID   string `json:"attempt_id"`
	Logs        string `json:"logs"`
}

type SandboxRunnerRunMetadata struct {
	ProviderInstallationID string `json:"provider_installation_id,omitempty"`
	ProviderRunID          string `json:"provider_run_id,omitempty"`
	ProviderJobID          string `json:"provider_job_id,omitempty"`
	RepositoryFullName     string `json:"repository_full_name,omitempty"`
	WorkflowName           string `json:"workflow_name,omitempty"`
	JobName                string `json:"job_name,omitempty"`
	HeadBranch             string `json:"head_branch,omitempty"`
	HeadSHA                string `json:"head_sha,omitempty"`
}

type SandboxScheduleRunMetadata struct {
	ScheduleID         *uuid.UUID `json:"schedule_id,omitempty"`
	DisplayName        string     `json:"display_name,omitempty"`
	TemporalWorkflowID string     `json:"temporal_workflow_id,omitempty"`
	TemporalRunID      string     `json:"temporal_run_id,omitempty"`
}

type SandboxRunBillingSummary struct {
	WindowCount         int32         `json:"window_count"`
	ReservedChargeUnits DecimalUint64 `json:"reserved_charge_units"`
	BilledChargeUnits   DecimalUint64 `json:"billed_charge_units"`
	WriteoffChargeUnits DecimalUint64 `json:"writeoff_charge_units"`
	CostPerUnit         DecimalUint64 `json:"cost_per_unit"`
	PricingPhase        string        `json:"pricing_phase,omitempty"`
}

type SandboxExecutionStickyDiskMount struct {
	MountID             uuid.UUID     `json:"mount_id"`
	MountName           string        `json:"mount_name"`
	KeyHash             string        `json:"key_hash"`
	MountPath           string        `json:"mount_path"`
	BaseGeneration      DecimalUint64 `json:"base_generation"`
	CommittedGeneration DecimalUint64 `json:"committed_generation"`
	SaveRequested       bool          `json:"save_requested"`
	SaveState           string        `json:"save_state"`
	FailureReason       string        `json:"failure_reason,omitempty"`
	RequestedAt         *time.Time    `json:"requested_at,omitempty"`
	CompletedAt         *time.Time    `json:"completed_at,omitempty"`
}

type SandboxRunsPage struct {
	Runs       []SandboxExecutionRecord `json:"runs"`
	NextCursor string                   `json:"next_cursor,omitempty"`
	Limit      int32                    `json:"limit"`
	Filters    SandboxRunsFilters       `json:"filters"`
}

type SandboxRunsFilters struct {
	SourceKind  string `json:"source_kind,omitempty"`
	Status      string `json:"status,omitempty"`
	Repository  string `json:"repository,omitempty"`
	Workflow    string `json:"workflow,omitempty"`
	Branch      string `json:"branch,omitempty"`
	RunnerClass string `json:"runner_class,omitempty"`
}

type SandboxRunLogSearchResult struct {
	ExecutionID        uuid.UUID `json:"execution_id"`
	AttemptID          uuid.UUID `json:"attempt_id"`
	SourceKind         string    `json:"source_kind,omitempty"`
	WorkloadKind       string    `json:"workload_kind,omitempty"`
	RunnerClass        string    `json:"runner_class,omitempty"`
	RepositoryFullName string    `json:"repository_full_name,omitempty"`
	WorkflowName       string    `json:"workflow_name,omitempty"`
	JobName            string    `json:"job_name,omitempty"`
	HeadBranch         string    `json:"head_branch,omitempty"`
	ScheduleID         string    `json:"schedule_id,omitempty"`
	Seq                uint32    `json:"seq"`
	Stream             string    `json:"stream"`
	Chunk              string    `json:"chunk"`
	CreatedAt          time.Time `json:"created_at"`
}

type SandboxRunLogSearchPage struct {
	Results    []SandboxRunLogSearchResult `json:"results"`
	NextCursor string                      `json:"next_cursor,omitempty"`
	Limit      int32                       `json:"limit"`
	Filters    SandboxRunLogSearchFilters  `json:"filters"`
}

type SandboxRunLogSearchFilters struct {
	Query       string `json:"query,omitempty"`
	RunID       string `json:"run_id,omitempty"`
	AttemptID   string `json:"attempt_id,omitempty"`
	SourceKind  string `json:"source_kind,omitempty"`
	Repository  string `json:"repository,omitempty"`
	Workflow    string `json:"workflow,omitempty"`
	Branch      string `json:"branch,omitempty"`
	RunnerClass string `json:"runner_class,omitempty"`
}

type SandboxAnalyticsBucket struct {
	Key                 string        `json:"key"`
	Count               DecimalUint64 `json:"count"`
	ReservedChargeUnits DecimalUint64 `json:"reserved_charge_units,omitempty"`
	BilledChargeUnits   DecimalUint64 `json:"billed_charge_units,omitempty"`
	WriteoffChargeUnits DecimalUint64 `json:"writeoff_charge_units,omitempty"`
}

type SandboxRunDurationSample struct {
	ExecutionID        uuid.UUID `json:"execution_id"`
	Status             string    `json:"status"`
	RunnerClass        string    `json:"runner_class,omitempty"`
	RepositoryFullName string    `json:"repository_full_name,omitempty"`
	WorkflowName       string    `json:"workflow_name,omitempty"`
	JobName            string    `json:"job_name,omitempty"`
	DurationMs         int64     `json:"duration_ms" minimum:"0" maximum:"9007199254740991"`
	CompletedAt        time.Time `json:"completed_at"`
}

type SandboxJobsAnalytics struct {
	WindowStart   time.Time                  `json:"window_start"`
	WindowEnd     time.Time                  `json:"window_end"`
	TotalRuns     DecimalUint64              `json:"total_runs"`
	SucceededRuns DecimalUint64              `json:"succeeded_runs"`
	FailedRuns    DecimalUint64              `json:"failed_runs"`
	P50DurationMs DecimalUint64              `json:"p50_duration_ms"`
	P95DurationMs DecimalUint64              `json:"p95_duration_ms"`
	P99DurationMs DecimalUint64              `json:"p99_duration_ms"`
	BySource      []SandboxAnalyticsBucket   `json:"by_source"`
	ByRunnerClass []SandboxAnalyticsBucket   `json:"by_runner_class"`
	SlowestRuns   []SandboxRunDurationSample `json:"slowest_runs"`
}

type SandboxCostsAnalytics struct {
	WindowStart         time.Time                `json:"window_start"`
	WindowEnd           time.Time                `json:"window_end"`
	ReservedChargeUnits DecimalUint64            `json:"reserved_charge_units"`
	BilledChargeUnits   DecimalUint64            `json:"billed_charge_units"`
	WriteoffChargeUnits DecimalUint64            `json:"writeoff_charge_units"`
	BySource            []SandboxAnalyticsBucket `json:"by_source"`
	ByRunnerClass       []SandboxAnalyticsBucket `json:"by_runner_class"`
	ByRepository        []SandboxAnalyticsBucket `json:"by_repository"`
}

type SandboxCachesAnalytics struct {
	WindowStart         time.Time                `json:"window_start"`
	WindowEnd           time.Time                `json:"window_end"`
	CheckoutRequests    DecimalUint64            `json:"checkout_requests"`
	CheckoutHits        DecimalUint64            `json:"checkout_hits"`
	CheckoutMisses      DecimalUint64            `json:"checkout_misses"`
	StickyRestoreHits   DecimalUint64            `json:"sticky_restore_hits"`
	StickyRestoreMisses DecimalUint64            `json:"sticky_restore_misses"`
	StickySaveRequests  DecimalUint64            `json:"sticky_save_requests"`
	StickyCommits       DecimalUint64            `json:"sticky_commits"`
	ByRepository        []SandboxAnalyticsBucket `json:"by_repository"`
}

type SandboxRunnerSizingSample struct {
	RunnerClass               string        `json:"runner_class"`
	RunCount                  DecimalUint64 `json:"run_count"`
	P95DurationMs             DecimalUint64 `json:"p95_duration_ms"`
	AvgRootfsProvisionedBytes DecimalUint64 `json:"avg_rootfs_provisioned_bytes"`
	AvgBootTimeUs             DecimalUint64 `json:"avg_boot_time_us"`
	AvgBlockWriteBytes        DecimalUint64 `json:"avg_block_write_bytes"`
	AvgNetTxBytes             DecimalUint64 `json:"avg_net_tx_bytes"`
}

type SandboxRunnerSizingAnalytics struct {
	WindowStart   time.Time                   `json:"window_start"`
	WindowEnd     time.Time                   `json:"window_end"`
	ByRunnerClass []SandboxRunnerSizingSample `json:"by_runner_class"`
}

type SandboxStickyDiskRecord struct {
	InstallationID     string        `json:"installation_id"`
	RepositoryID       string        `json:"repository_id"`
	RepositoryFullName string        `json:"repository_full_name,omitempty"`
	KeyHash            string        `json:"key_hash"`
	Key                string        `json:"key"`
	CurrentGeneration  DecimalUint64 `json:"current_generation"`
	CurrentSourceRef   string        `json:"current_source_ref"`
	LastUsedAt         *time.Time    `json:"last_used_at,omitempty"`
	LastCompletedAt    *time.Time    `json:"last_completed_at,omitempty"`
	LastSaveState      string        `json:"last_save_state,omitempty"`
	LastExecutionID    *uuid.UUID    `json:"last_execution_id,omitempty"`
	LastAttemptID      *uuid.UUID    `json:"last_attempt_id,omitempty"`
	LastRunnerClass    string        `json:"last_runner_class,omitempty"`
	LastWorkflowName   string        `json:"last_workflow_name,omitempty"`
	LastJobName        string        `json:"last_job_name,omitempty"`
	LastMountPath      string        `json:"last_mount_path,omitempty"`
}

type SandboxStickyDisksPage struct {
	Disks      []SandboxStickyDiskRecord `json:"disks"`
	NextCursor string                    `json:"next_cursor,omitempty"`
	Limit      int32                     `json:"limit"`
	Filters    SandboxStickyDiskFilters  `json:"filters"`
}

type SandboxStickyDiskFilters struct {
	Repository string `json:"repository,omitempty"`
}

type SandboxStickyDiskResetResult struct {
	InstallationID   string    `json:"installation_id"`
	RepositoryID     string    `json:"repository_id"`
	KeyHash          string    `json:"key_hash"`
	DeletedSourceRef string    `json:"deleted_source_ref,omitempty"`
	ResetAt          time.Time `json:"reset_at"`
}

type SandboxExecutionScheduleCreateRequest struct {
	IdempotencyKey     string            `json:"idempotency_key" required:"true" maxLength:"128"`
	DisplayName        string            `json:"display_name,omitempty" maxLength:"255"`
	SourceRepositoryID uuid.UUID         `json:"source_repository_id" required:"true"`
	WorkflowPath       string            `json:"workflow_path" required:"true" minLength:"1" maxLength:"512"`
	Ref                string            `json:"ref,omitempty" maxLength:"255"`
	Inputs             map[string]string `json:"inputs,omitempty"`
	IntervalSeconds    uint32            `json:"interval_seconds" required:"true" minimum:"15" maximum:"4294967295"`
	Paused             bool              `json:"paused,omitempty"`
}

type SandboxExecutionScheduleRecord struct {
	ScheduleID         uuid.UUID                                `json:"schedule_id"`
	OrgID              OrgID                                    `json:"org_id"`
	ActorID            string                                   `json:"actor_id"`
	DisplayName        string                                   `json:"display_name,omitempty"`
	IdempotencyKey     string                                   `json:"idempotency_key,omitempty"`
	TemporalScheduleID string                                   `json:"temporal_schedule_id"`
	TemporalNamespace  string                                   `json:"temporal_namespace"`
	TaskQueue          string                                   `json:"task_queue"`
	State              string                                   `json:"state"`
	ProjectID          uuid.UUID                                `json:"project_id"`
	SourceRepositoryID uuid.UUID                                `json:"source_repository_id"`
	WorkflowPath       string                                   `json:"workflow_path"`
	Ref                string                                   `json:"ref,omitempty"`
	Inputs             map[string]string                        `json:"inputs,omitempty"`
	IntervalSeconds    uint32                                   `json:"interval_seconds" minimum:"15" maximum:"4294967295"`
	CreatedAt          time.Time                                `json:"created_at"`
	UpdatedAt          time.Time                                `json:"updated_at"`
	Dispatches         []SandboxExecutionScheduleDispatchRecord `json:"dispatches,omitempty"`
}

type SandboxExecutionScheduleDispatchRecord struct {
	DispatchID          uuid.UUID  `json:"dispatch_id"`
	ScheduleID          uuid.UUID  `json:"schedule_id"`
	TemporalWorkflowID  string     `json:"temporal_workflow_id"`
	TemporalRunID       string     `json:"temporal_run_id"`
	SourceWorkflowRunID *uuid.UUID `json:"source_workflow_run_id,omitempty"`
	WorkflowState       string     `json:"workflow_state,omitempty"`
	State               string     `json:"state"`
	FailureReason       string     `json:"failure_reason,omitempty"`
	ScheduledAt         time.Time  `json:"scheduled_at"`
	SubmittedAt         *time.Time `json:"submitted_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type SandboxBillingCheckoutRequest struct {
	ProductID   string `json:"product_id" required:"true" maxLength:"255" doc:"Product context for checkout display and metadata; purchased top-ups are account-scoped"`
	AmountCents int64  `json:"amount_cents" required:"true" minimum:"1" maximum:"9007199254740991" doc:"Amount in cents"`
	SuccessURL  string `json:"success_url" required:"true" maxLength:"2048"`
	CancelURL   string `json:"cancel_url" required:"true" maxLength:"2048"`
}

type SandboxBillingContractRequest struct {
	PlanID     string `json:"plan_id" required:"true" maxLength:"255" doc:"Plan to activate"`
	Cadence    string `json:"cadence,omitempty" enum:"monthly" doc:"Billing cadence (default monthly)"`
	SuccessURL string `json:"success_url" required:"true" maxLength:"2048"`
	CancelURL  string `json:"cancel_url" required:"true" maxLength:"2048"`
}

type SandboxBillingContractChangeRequest struct {
	TargetPlanID string `json:"target_plan_id" required:"true" maxLength:"255" doc:"Plan to upgrade into immediately"`
	SuccessURL   string `json:"success_url" required:"true" maxLength:"2048"`
	CancelURL    string `json:"cancel_url" required:"true" maxLength:"2048"`
}

type SandboxBillingPortalRequest struct {
	ReturnURL string `json:"return_url" required:"true" maxLength:"2048"`
}

type SandboxAttemptRecord struct {
	AttemptID              uuid.UUID  `json:"attempt_id"`
	AttemptSeq             int        `json:"attempt_seq" minimum:"0" maximum:"9007199254740991"`
	State                  string     `json:"state"`
	LeaseID                string     `json:"lease_id,omitempty"`
	ExecID                 string     `json:"exec_id,omitempty"`
	BillingJobID           int64      `json:"billing_job_id,omitempty" minimum:"0" maximum:"9007199254740991"`
	FailureReason          string     `json:"failure_reason,omitempty"`
	ExitCode               *int       `json:"exit_code,omitempty" minimum:"0" maximum:"255"`
	DurationMs             int64      `json:"duration_ms,omitempty" minimum:"0" maximum:"9007199254740991"`
	ZFSWritten             int64      `json:"zfs_written,omitempty" minimum:"0" maximum:"9007199254740991"`
	StdoutBytes            int64      `json:"stdout_bytes,omitempty" minimum:"0" maximum:"9007199254740991"`
	StderrBytes            int64      `json:"stderr_bytes,omitempty" minimum:"0" maximum:"9007199254740991"`
	RootfsProvisionedBytes int64      `json:"rootfs_provisioned_bytes,omitempty" minimum:"0" maximum:"9007199254740991"`
	BootTimeUs             int64      `json:"boot_time_us,omitempty" minimum:"0" maximum:"9007199254740991"`
	BlockReadBytes         int64      `json:"block_read_bytes,omitempty" minimum:"0" maximum:"9007199254740991"`
	BlockWriteBytes        int64      `json:"block_write_bytes,omitempty" minimum:"0" maximum:"9007199254740991"`
	NetRXBytes             int64      `json:"net_rx_bytes,omitempty" minimum:"0" maximum:"9007199254740991"`
	NetTXBytes             int64      `json:"net_tx_bytes,omitempty" minimum:"0" maximum:"9007199254740991"`
	VCPUExitCount          int64      `json:"vcpu_exit_count,omitempty" minimum:"0" maximum:"9007199254740991"`
	TraceID                string     `json:"trace_id,omitempty"`
	StartedAt              *time.Time `json:"started_at,omitempty"`
	CompletedAt            *time.Time `json:"completed_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type SandboxBillingWindow struct {
	AttemptID           uuid.UUID     `json:"attempt_id"`
	BillingWindowID     string        `json:"billing_window_id"`
	WindowSeq           int           `json:"window_seq" minimum:"0" maximum:"9007199254740991"`
	ReservationShape    string        `json:"reservation_shape"`
	ReservedQuantity    int           `json:"reserved_quantity" minimum:"0" maximum:"9007199254740991"`
	ActualQuantity      int           `json:"actual_quantity,omitempty" minimum:"0" maximum:"9007199254740991"`
	ReservedChargeUnits DecimalUint64 `json:"reserved_charge_units"`
	BilledChargeUnits   DecimalUint64 `json:"billed_charge_units"`
	WriteoffChargeUnits DecimalUint64 `json:"writeoff_charge_units"`
	CostPerUnit         DecimalUint64 `json:"cost_per_unit"`
	PricingPhase        string        `json:"pricing_phase,omitempty"`
	State               string        `json:"state"`
	WindowStart         time.Time     `json:"window_start"`
	CreatedAt           time.Time     `json:"created_at"`
	SettledAt           *time.Time    `json:"settled_at,omitempty"`
}
