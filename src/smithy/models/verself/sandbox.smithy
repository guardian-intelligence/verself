$version: "2"
namespace verself.sandbox.v1

use aws.protocols#restJson1

use smithy.api#http
use smithy.api#httpHeader
use smithy.api#httpLabel
use smithy.api#httpPayload
use smithy.api#nestedProperties
use smithy.api#httpQuery
use smithy.api#idempotencyToken
use smithy.api#idempotent
use smithy.api#length
use smithy.api#paginated
use smithy.api#pattern
use smithy.api#range
use smithy.api#readonly
use smithy.api#required
use smithy.api#sensitive
use verself.common.v1#ConflictError
use verself.common.v1#DisplayName
use verself.common.v1#IdempotencyKey
use verself.common.v1#IdempotencyPayloadMismatchError
use verself.common.v1#PermissionDeniedError
use verself.common.v1#RateLimitedError
use verself.common.v1#ResourceName
use verself.common.v1#ResourceNotFoundError
use verself.common.v1#ServiceUnavailableError
use verself.common.v1#UnauthenticatedError
use verself.common.v1#ValidationFailedError
use verself.common.v1#audit
use verself.common.v1#auditEvent
use verself.common.v1#authz
use verself.common.v1#identity
use verself.common.v1#permission
use verself.common.v1#rateLimit
use verself.common.v1#requestBudget
use verself.common.v1#sdk
use verself.common.v1#serviceRuntime
use verself.common.v1#DateTime

@serviceRuntime(serviceName: "sandbox-rental-service", publicAudience: "verself-api", internalAudience: "sandbox-rental-service")
@restJson1
service SandboxRental {
    version: "2026-05-13"
    operations: [
        BeginGithubInstallation,
        ListGithubInstallations,
        SyncGithubInstallationRepositories,
        GetExecution,
        GetExecutionLogs,
        ListRuns,
        GetRun,
        SearchRunLogs,
        GetJobsAnalytics,
        GetCostsAnalytics,
        GetRunnerSizingAnalytics,
        ListCacheVolumes,
        ListCacheGenerations,
        DeleteCacheGeneration,
        DeleteCachePath,
        CreateExecutionSchedule,
        ListExecutionSchedules,
        GetExecutionSchedule,
        PauseExecutionSchedule,
        ResumeExecutionSchedule
    ]
    resources: [
        GitHubInstallation,
        GitHubInstallationRepositoryResource,
        Execution,
        ExecutionLogsResource,
        Run,
        RunLogsResource,
        RunAnalyticsJobsResource,
        RunAnalyticsCostsResource,
        RunAnalyticsRunnerSizingResource,
        CacheVolume,
        CacheGeneration,
        ExecutionSchedule
    ]
}

@serviceRuntime(serviceName: "sandbox-rental-service", publicAudience: "verself-api", internalAudience: "sandbox-rental-service")
@restJson1
service SandboxRentalInternal {
    version: "2026-05-13"
    operations: [InternalRegisterRunnerRepository]
    resources: [RunnerRepository]
}

@pattern("^[0-9a-fA-F-]{36}$")
string AttemptId

@pattern("^[0-9a-fA-F-]{36}$")
string DispatchId

@pattern("^[0-9a-fA-F-]{36}$")
string ExecutionId

@pattern("^[0-9a-fA-F-]{36}$")
string ProjectId

@pattern("^[0-9a-fA-F-]{36}$")
string RunId

@pattern("^[0-9a-fA-F-]{36}$")
string ScheduleId

@pattern("^[0-9a-fA-F-]{36}$")
string SourceRepositoryId

@pattern("^[0-9a-fA-F-]{36}$")
string SourceWorkflowRunId

@length(min: 1, max: 128)
string ActorId

@length(min: 1, max: 512)
string BillingWindowId

@pattern("^[0-9a-fA-F-]{36}$")
string CacheGenerationId

@length(min: 1, max: 255)
string CachePath

@length(min: 1, max: 128)
string CacheVolumeName

@pattern("^[0-9a-fA-F-]{36}$")
string CacheVolumeId

@length(min: 1, max: 512)
string CorrelationId

@pattern("^[0-9]+$")
string DecimalUint64

@length(min: 1, max: 255)
string ExternalProvider

@length(min: 1, max: 255)
string ExternalTaskId

@length(min: 1, max: 1024)
string GitRef

@length(min: 1, max: 4096)
string LogChunk

@length(max: 4096)
string LogQuery

@length(min: 1, max: 128)
@pattern("^org_[0-9A-HJKMNP-TV-Z]{26}$")
string OrgId

@length(min: 1, max: 128)
string PricingPhase

@length(min: 1, max: 64)
string ProductId

@length(min: 1, max: 64)
string Provider

@length(min: 1, max: 255)
string ProviderOwner

@length(min: 1, max: 255)
string ProviderRepo

@length(min: 1, max: 512)
string ProviderRepositoryId

@length(min: 1, max: 1024)
string ReservationShape

@length(max: 4096)
string RunLogSearchCursor

@range(min: 1, max: 500)
integer RunLogSearchPageSize

@length(max: 4096)
string RunListCursor

@range(min: 1, max: 200)
integer RunListPageSize

@length(min: 1, max: 8192)
string RunCommand

@length(min: 1, max: 255)
string RunnerClass

@range(min: 0, max: 9007199254740991)
long SafeNonNegativeLong

@range(min: 0, max: 255)
integer SafeExitCode

@length(min: 1, max: 8192)
string SetupURL

@length(min: 1, max: 64)
string SourceKind

@length(min: 1, max: 4096)
string SourceRef

@length(min: 1, max: 64)
string StreamName

@length(min: 1, max: 64)
string WorkloadKind

@length(min: 1, max: 64)
string ExecutionKind

@length(min: 1, max: 64)
string ExecutionStatus

@length(min: 1, max: 64)
string AttemptState

@length(min: 1, max: 8192)
string FailureReason

@length(min: 1, max: 512)
string LeaseId

@length(min: 1, max: 512)
string ExecId

@length(min: 1, max: 128)
string BillingJobId

@length(min: 1, max: 512)
string TraceId

@length(min: 1, max: 1024)
string RepositoryFullName

@length(min: 1, max: 1024)
string WorkflowName

@length(min: 1, max: 4096)
string WorkflowPath

@length(min: 1, max: 1024)
string JobName

@length(min: 1, max: 1024)
string HeadBranch

@length(min: 1, max: 128)
string HeadSHA

@length(min: 1, max: 512)
string TemporalId

@length(min: 1, max: 255)
string TaskQueue

@length(min: 1, max: 255)
string TemporalNamespace

@length(min: 1, max: 64)
string ScheduleState

@length(min: 1, max: 64)
string WorkflowState

@range(min: 15, max: 4294967295)
integer IntervalSeconds

@length(max: 4096)
@sensitive
string ScheduleListCursor

@range(min: 1, max: 500)
integer ScheduleListPageSize

@length(min: 1, max: 256)
string ScheduleInputName

@length(max: 8192)
string ScheduleInputValue

@length(max: 64)
map ScheduleInputs {
    key: ScheduleInputName
    value: ScheduleInputValue
}

list GitHubInstallations {
    member: SandboxGitHubInstallationRecord
}

list InstallationRepositories {
    member: GitHubInstallationRepository
}

list Runs {
    member: SandboxExecutionRecord
}

list SandboxBillingWindows {
    member: SandboxBillingWindow
}

list SandboxExecutionScheduleDispatches {
    member: SandboxExecutionScheduleDispatchRecord
}

list SandboxExecutionSchedules {
    member: SandboxExecutionScheduleRecord
}

list SandboxRunLogSearchResults {
    member: SandboxRunLogSearchResult
}

list SandboxAnalyticsBuckets {
    member: SandboxAnalyticsBucket
}

list SandboxRunDurationSamples {
    member: SandboxRunDurationSample
}

list SandboxRunnerSizingSamples {
    member: SandboxRunnerSizingSample
}

list SandboxCacheVolumes {
    member: SandboxCacheVolume
}

list SandboxCacheGenerations {
    member: SandboxCacheGeneration
}

list SandboxCachePaths {
    member: CachePath
}

@permission(name: "sandbox:github_installation:read")
string GitHubInstallationReadPermission

@permission(name: "sandbox:github_installation:write")
string GitHubInstallationWritePermission

@permission(name: "sandbox:execution:read")
string ExecutionReadPermission

@permission(name: "sandbox:execution_schedule:read")
string ScheduleReadPermission

@permission(name: "sandbox:execution_schedule:write")
string ScheduleWritePermission

@permission(name: "sandbox:logs:read")
string LogsReadPermission

@permission(name: "sandbox:analytics:read")
string AnalyticsReadPermission

@permission(name: "sandbox:cache:read")
string CacheReadPermission

@permission(name: "sandbox:cache:write")
string CacheWritePermission

@permission(name: "sandbox:runner_repository:register")
string RunnerRepositoryRegisterPermission

@auditEvent(name: "sandbox.github_installation.connect")
string GitHubInstallationConnectAuditEvent

@auditEvent(name: "sandbox.github_installation.list")
string GitHubInstallationListAuditEvent

@auditEvent(name: "sandbox.github_installation.repositories.sync")
string GitHubRepositoriesSyncAuditEvent

@auditEvent(name: "sandbox.execution.read")
string ExecutionReadAuditEvent

@auditEvent(name: "sandbox.execution.logs.read")
string ExecutionLogsReadAuditEvent

@auditEvent(name: "sandbox.run.list")
string RunListAuditEvent

@auditEvent(name: "sandbox.run.read")
string RunReadAuditEvent

@auditEvent(name: "sandbox.run_logs.search")
string RunLogsSearchAuditEvent

@auditEvent(name: "sandbox.run_analytics.jobs.read")
string JobsAnalyticsReadAuditEvent

@auditEvent(name: "sandbox.run_analytics.costs.read")
string CostsAnalyticsReadAuditEvent

@auditEvent(name: "sandbox.run_analytics.runner_sizing.read")
string RunnerSizingAnalyticsReadAuditEvent

@auditEvent(name: "sandbox.cache_volume.list")
string CacheVolumeListAuditEvent

@auditEvent(name: "sandbox.cache_generation.list")
string CacheGenerationListAuditEvent

@auditEvent(name: "sandbox.cache_generation.delete")
string CacheGenerationDeleteAuditEvent

@auditEvent(name: "sandbox.cache_path.delete")
string CachePathDeleteAuditEvent

@auditEvent(name: "sandbox.execution_schedule.create")
string ScheduleCreateAuditEvent

@auditEvent(name: "sandbox.execution_schedule.list")
string ScheduleListAuditEvent

@auditEvent(name: "sandbox.execution_schedule.read")
string ScheduleReadAuditEvent

@auditEvent(name: "sandbox.execution_schedule.pause")
string SchedulePauseAuditEvent

@auditEvent(name: "sandbox.execution_schedule.resume")
string ScheduleResumeAuditEvent

@auditEvent(name: "sandbox.runner_repository.register")
string RunnerRepositoryRegisterAuditEvent

resource GitHubInstallation {}
resource GitHubInstallationRepositoryResource {}
resource Execution {}
resource ExecutionLogsResource {}
resource Run {}
resource RunLogsResource {}
resource RunAnalyticsJobsResource {}
resource RunAnalyticsCostsResource {}
resource RunAnalyticsRunnerSizingResource {}
resource CacheVolume {}
resource CacheGeneration {}
resource ExecutionSchedule {}
resource RunnerRepository {}

structure SandboxGitHubInstallationConnectResponse {
    @required
    state: String

    @required
    setup_url: SetupURL

    @required
    expires_at: DateTime
}

structure SandboxGitHubInstallationRecord {
    @required
    installation_id: DecimalUint64

    @required
    resourceName: ResourceName

    @required
    org_id: OrgId

    @required
    account_login: String

    @required
    account_type: String

    @required
    active: Boolean

    @required
    created_at: DateTime

    @required
    updated_at: DateTime
}

structure GitHubInstallationRepository {
    @required
    provider_repository_id: ProviderRepositoryId

    @required
    provider_owner: ProviderOwner

    @required
    provider_repo: ProviderRepo

    @required
    repository_full_name: RepositoryFullName

    @required
    private: Boolean

    @required
    active: Boolean

    @required
    synced_at: DateTime
}

structure SandboxExecutionRecord {
    @required
    run_id: RunId

    @required
    resourceName: ResourceName

    @required
    execution_id: ExecutionId

    @required
    org_id: OrgId

    @required
    actor_id: ActorId

    @required
    kind: ExecutionKind

    source_kind: SourceKind
    workload_kind: WorkloadKind
    source_ref: SourceRef
    runner_class: RunnerClass
    external_provider: ExternalProvider
    external_task_id: ExternalTaskId
    provider: Provider

    @required
    product_id: ProductId

    @required
    status: ExecutionStatus

    correlation_id: CorrelationId
    idempotency_key: IdempotencyKey
    run_command: RunCommand

    @required
    latest_attempt: SandboxAttemptRecord

    @required
    created_at: DateTime

    @required
    updated_at: DateTime

    billing_windows: SandboxBillingWindows
    billing_summary: SandboxRunBillingSummary
    runner: SandboxRunnerRunMetadata
    schedule: SandboxScheduleRunMetadata
}

structure SandboxAttemptRecord {
    @required
    attempt_id: AttemptId

    resourceName: ResourceName

    @required
    attempt_seq: SafeNonNegativeLong

    @required
    state: AttemptState

    lease_id: LeaseId
    exec_id: ExecId
    billing_job_id: SafeNonNegativeLong
    failure_reason: FailureReason
    exit_code: SafeExitCode
    duration_ms: SafeNonNegativeLong
    stdout_bytes: SafeNonNegativeLong
    stderr_bytes: SafeNonNegativeLong
    rootfs_provisioned_bytes: SafeNonNegativeLong
    boot_time_us: SafeNonNegativeLong
    block_read_bytes: SafeNonNegativeLong
    block_write_bytes: SafeNonNegativeLong
    net_rx_bytes: SafeNonNegativeLong
    net_tx_bytes: SafeNonNegativeLong
    vcpu_exit_count: SafeNonNegativeLong
    trace_id: TraceId
    started_at: DateTime
    completed_at: DateTime

    @required
    created_at: DateTime

    @required
    updated_at: DateTime
}

structure SandboxBillingWindow {
    @required
    attempt_id: AttemptId

    @required
    billing_window_id: BillingWindowId

    @required
    window_seq: SafeNonNegativeLong

    @required
    reservation_shape: ReservationShape

    @required
    reserved_quantity: SafeNonNegativeLong

    actual_quantity: SafeNonNegativeLong

    @required
    reserved_charge_units: DecimalUint64

    @required
    billed_charge_units: DecimalUint64

    @required
    writeoff_charge_units: DecimalUint64

    @required
    cost_per_unit: DecimalUint64

    pricing_phase: PricingPhase

    @required
    state: String

    @required
    window_start: DateTime

    @required
    created_at: DateTime

    settled_at: DateTime
}

structure SandboxRunBillingSummary {
    @required
    window_count: SafeNonNegativeLong

    @required
    reserved_charge_units: DecimalUint64

    @required
    billed_charge_units: DecimalUint64

    @required
    writeoff_charge_units: DecimalUint64

    @required
    cost_per_unit: DecimalUint64

    pricing_phase: PricingPhase
}

structure SandboxRunnerRunMetadata {
    provider_installation_id: DecimalUint64
    provider_run_id: DecimalUint64
    provider_job_id: DecimalUint64
    repository_full_name: RepositoryFullName
    workflow_name: WorkflowName
    job_name: JobName
    head_branch: HeadBranch
    head_sha: HeadSHA
}

structure SandboxScheduleRunMetadata {
    schedule_id: ScheduleId
    scheduleResourceName: ResourceName
    display_name: DisplayName
    temporal_workflow_id: TemporalId
    temporal_run_id: TemporalId
}

structure SandboxExecutionLogs {
    @required
    execution_id: ExecutionId

    @required
    attempt_id: AttemptId

    @required
    logs: String
}

structure SandboxRunsFilters {
    source_kind: SourceKind
    status: ExecutionStatus
    repository: RepositoryFullName
    workflow: WorkflowName
    branch: GitRef
    runner_class: RunnerClass
}

structure SandboxRunsPage {
    @required
    runs: Runs

    next_cursor: RunListCursor

    @required
    limit: RunListPageSize

    @required
    filters: SandboxRunsFilters
}

structure SandboxExecutionSchedulesPage {
    @required
    schedules: SandboxExecutionSchedules

    next_cursor: ScheduleListCursor

    @required
    limit: ScheduleListPageSize
}

structure SandboxCacheVolume {
    @required
    cache_volume_id: CacheVolumeId

    @required
    resourceName: ResourceName

    @required
    org_id: OrgId

    @required
    provider: Provider

    @required
    provider_repository_id: ProviderRepositoryId

    repository_full_name: RepositoryFullName

    @required
    scope_ref: GitRef

    @required
    name: CacheVolumeName

    current_generation_id: CacheGenerationId

    @required
    generation_count: SafeNonNegativeLong

    @required
    used_bytes: SafeNonNegativeLong

    @required
    written_bytes: SafeNonNegativeLong

    @required
    created_at: DateTime

    @required
    last_used_at: DateTime
}

structure SandboxCacheGeneration {
    @required
    cache_generation_id: CacheGenerationId

    @required
    cache_volume_id: CacheVolumeId

    source_generation_id: CacheGenerationId

    @required
    org_id: OrgId

    @required
    provider: Provider

    @required
    provider_repository_id: ProviderRepositoryId

    repository_full_name: RepositoryFullName

    @required
    scope_ref: GitRef

    @required
    volume_name: CacheVolumeName

    @required
    bind_paths: SandboxCachePaths

    @required
    provider_run_id: DecimalUint64

    @required
    provider_run_attempt: DecimalUint64

    @required
    provider_job_id: DecimalUint64

    head_sha: HeadSHA
    tree_hash: String

    @required
    result: String

    @required
    state: String

    @required
    zfs_snapshot_ref: String

    @required
    used_bytes: SafeNonNegativeLong

    @required
    written_bytes: SafeNonNegativeLong

    sealed_at: DateTime
    committed_at: DateTime
    last_used_at: DateTime
    expires_at: DateTime

    @required
    current: Boolean
}

structure SandboxCacheVolumesPage {
    @required
    volumes: SandboxCacheVolumes
}

structure SandboxCacheGenerationsPage {
    @required
    generations: SandboxCacheGenerations
}

structure SandboxCacheGenerationDeleteResult {
    @required
    generation: SandboxCacheGeneration
}

structure SandboxCachePathDeleteResult {
    @required
    cache_volume_id: CacheVolumeId

    @required
    path: CachePath

    @required
    generations: SandboxCacheGenerations
}

structure SandboxRunLogSearchResult {
    @required
    execution_id: ExecutionId

    @required
    attempt_id: AttemptId

    source_kind: SourceKind
    workload_kind: WorkloadKind
    runner_class: RunnerClass
    repository_full_name: RepositoryFullName
    workflow_name: WorkflowName
    job_name: JobName
    head_branch: HeadBranch
    schedule_id: ScheduleId

    @required
    seq: SafeNonNegativeLong

    @required
    stream: StreamName

    @required
    chunk: LogChunk

    @required
    created_at: DateTime
}

structure SandboxRunLogSearchFilters {
    query: LogQuery
    run_id: RunId
    attempt_id: AttemptId
    source_kind: SourceKind
    repository: RepositoryFullName
    workflow: WorkflowName
    branch: GitRef
    runner_class: RunnerClass
}

structure SandboxRunLogSearchPage {
    @required
    results: SandboxRunLogSearchResults

    next_cursor: RunLogSearchCursor

    @required
    limit: RunLogSearchPageSize

    @required
    filters: SandboxRunLogSearchFilters
}

structure SandboxAnalyticsBucket {
    @required
    key: String

    @required
    count: DecimalUint64

    reserved_charge_units: DecimalUint64
    billed_charge_units: DecimalUint64
    writeoff_charge_units: DecimalUint64
}

structure SandboxRunDurationSample {
    @required
    execution_id: ExecutionId

    @required
    status: ExecutionStatus

    runner_class: RunnerClass
    repository_full_name: RepositoryFullName
    workflow_name: WorkflowName
    job_name: JobName

    @required
    duration_ms: SafeNonNegativeLong

    @required
    completed_at: DateTime
}

structure SandboxJobsAnalytics {
    @required
    window_start: DateTime

    @required
    window_end: DateTime

    @required
    total_runs: DecimalUint64

    @required
    succeeded_runs: DecimalUint64

    @required
    failed_runs: DecimalUint64

    @required
    p50_duration_ms: DecimalUint64

    @required
    p95_duration_ms: DecimalUint64

    @required
    p99_duration_ms: DecimalUint64

    @required
    by_source: SandboxAnalyticsBuckets

    @required
    by_runner_class: SandboxAnalyticsBuckets

    @required
    slowest_runs: SandboxRunDurationSamples
}

structure SandboxCostsAnalytics {
    @required
    window_start: DateTime

    @required
    window_end: DateTime

    @required
    reserved_charge_units: DecimalUint64

    @required
    billed_charge_units: DecimalUint64

    @required
    writeoff_charge_units: DecimalUint64

    @required
    by_source: SandboxAnalyticsBuckets

    @required
    by_runner_class: SandboxAnalyticsBuckets

    @required
    by_repository: SandboxAnalyticsBuckets
}

structure SandboxRunnerSizingSample {
    @required
    runner_class: RunnerClass

    @required
    run_count: DecimalUint64

    @required
    p95_duration_ms: DecimalUint64

    @required
    avg_rootfs_provisioned_bytes: DecimalUint64

    @required
    avg_boot_time_us: DecimalUint64

    @required
    avg_block_write_bytes: DecimalUint64

    @required
    avg_net_tx_bytes: DecimalUint64
}

structure SandboxRunnerSizingAnalytics {
    @required
    window_start: DateTime

    @required
    window_end: DateTime

    @required
    by_runner_class: SandboxRunnerSizingSamples
}

structure SandboxExecutionScheduleRecord {
    @required
    schedule_id: ScheduleId

    @required
    resourceName: ResourceName

    @required
    org_id: OrgId

    @required
    actor_id: ActorId

    display_name: DisplayName
    idempotency_key: IdempotencyKey

    @required
    temporal_schedule_id: TemporalId

    @required
    temporal_namespace: TemporalNamespace

    @required
    task_queue: TaskQueue

    @required
    state: ScheduleState

    @required
    project_id: ProjectId

    @required
    projectResourceName: ResourceName

    @required
    source_repository_id: SourceRepositoryId

    @required
    sourceRepositoryResourceName: ResourceName

    @required
    workflow_path: WorkflowPath

    ref: GitRef
    inputs: ScheduleInputs

    @required
    interval_seconds: IntervalSeconds

    @required
    created_at: DateTime

    @required
    updated_at: DateTime

    dispatches: SandboxExecutionScheduleDispatches
}

structure SandboxExecutionScheduleDispatchRecord {
    @required
    dispatch_id: DispatchId

    @required
    schedule_id: ScheduleId

    @required
    temporal_workflow_id: TemporalId

    @required
    temporal_run_id: TemporalId

    source_workflow_run_id: SourceWorkflowRunId
    workflow_state: WorkflowState

    @required
    state: ScheduleState

    failure_reason: FailureReason

    @required
    scheduled_at: DateTime

    submitted_at: DateTime

    @required
    created_at: DateTime

    @required
    updated_at: DateTime
}

structure RunnerRepositoryRegistration {
    @required
    provider: Provider

    @required
    provider_repository_id: ProviderRepositoryId

    @required
    project_id: ProjectId

    source_repository_id: SourceRepositoryId

    @required
    state: String
}

@idempotent
@http(method: "POST", uri: "/api/v1/github/installations/connect", code: 201)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: GitHubInstallationWritePermission, organization: {source: "token_org_id"})
@audit(event: GitHubInstallationConnectAuditEvent, resource: GitHubInstallation, action: "connect")
@rateLimit(bucket: "github_installation_mutation")
@requestBudget(maxBytes: 1024)
@sdk(module: "sandbox.githubInstallations", method: "beginConnect", paginated: false, retryable: false)
operation BeginGithubInstallation {
    input: BeginGithubInstallationInput
    output: BeginGithubInstallationOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

structure BeginGithubInstallationInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
}

structure BeginGithubInstallationOutput {
    @required
    @httpPayload
    @nestedProperties
    connect: SandboxGitHubInstallationConnectResponse
}

@readonly
@http(method: "GET", uri: "/api/v1/github/installations")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: GitHubInstallationReadPermission, organization: {source: "token_org_id"})
@audit(event: GitHubInstallationListAuditEvent, resource: GitHubInstallation, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.githubInstallations", method: "list", paginated: false, retryable: true)
operation ListGithubInstallations {
    input: EmptyInput
    output: ListGithubInstallationsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

structure EmptyInput {}

structure ListGithubInstallationsOutput {
    @required
    installations: GitHubInstallations
}

@idempotent
@http(method: "POST", uri: "/api/v1/github/installations/{installation_id}/repositories/sync")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: GitHubInstallationWritePermission, organization: {source: "token_org_id"})
@audit(event: GitHubRepositoriesSyncAuditEvent, resource: GitHubInstallationRepositoryResource, action: "sync")
@rateLimit(bucket: "github_installation_mutation")
@requestBudget(maxBytes: 1024)
@sdk(module: "sandbox.githubInstallations", method: "syncRepositories", paginated: false, retryable: false)
operation SyncGithubInstallationRepositories {
    input: SyncGithubInstallationRepositoriesInput
    output: SyncGithubInstallationRepositoriesOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

structure SyncGithubInstallationRepositoriesInput {
    @required
    @httpLabel
    installation_id: DecimalUint64

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
}

structure SyncGithubInstallationRepositoriesOutput {
    @required
    installation_id: DecimalUint64

    @required
    synced_at: DateTime

    @required
    repositories: InstallationRepositories
}

@readonly
@http(method: "GET", uri: "/api/v1/executions/{execution_id}")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: ExecutionReadPermission, organization: {source: "token_org_id"})
@audit(event: ExecutionReadAuditEvent, resource: Execution, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.executions", method: "get", paginated: false, retryable: true)
operation GetExecution {
    input: ExecutionPathInput
    output: SandboxExecutionOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

structure ExecutionPathInput {
    @required
    @httpLabel
    execution_id: ExecutionId
}

structure SandboxExecutionOutput {
    @required
    @httpPayload
    @nestedProperties
    execution: SandboxExecutionRecord
}

@readonly
@http(method: "GET", uri: "/api/v1/executions/{execution_id}/logs")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: LogsReadPermission, organization: {source: "token_org_id"})
@audit(event: ExecutionLogsReadAuditEvent, resource: ExecutionLogsResource, action: "read")
@rateLimit(bucket: "logs_read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.executions", method: "getLogs", paginated: false, retryable: true)
operation GetExecutionLogs {
    input: ExecutionPathInput
    output: SandboxExecutionLogsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

structure SandboxExecutionLogsOutput {
    @required
    @httpPayload
    @nestedProperties
    logs: SandboxExecutionLogs
}

@readonly
@http(method: "GET", uri: "/api/v1/runs")
@paginated(inputToken: "cursor", outputToken: "next_cursor", pageSize: "limit", items: "runs")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: ExecutionReadPermission, organization: {source: "token_org_id"})
@audit(event: RunListAuditEvent, resource: Run, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.runs", method: "list", paginated: true, retryable: true)
operation ListRuns {
    input: ListRunsInput
    output: SandboxRunsPage
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

structure ListRunsInput {
    @httpQuery("limit")
    limit: RunListPageSize

    @httpQuery("cursor")
    cursor: RunListCursor

    @httpQuery("source_kind")
    source_kind: SourceKind

    @httpQuery("status")
    status: ExecutionStatus

    @httpQuery("repository")
    repository: RepositoryFullName

    @httpQuery("workflow")
    workflow: WorkflowName

    @httpQuery("branch")
    branch: GitRef

    @httpQuery("runner_class")
    runner_class: RunnerClass
}

@readonly
@http(method: "GET", uri: "/api/v1/runs/{run_id}")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: ExecutionReadPermission, organization: {source: "token_org_id"})
@audit(event: RunReadAuditEvent, resource: Run, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.runs", method: "get", paginated: false, retryable: true)
operation GetRun {
    input: RunPathInput
    output: SandboxExecutionOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

structure RunPathInput {
    @required
    @httpLabel
    run_id: RunId
}

@readonly
@http(method: "GET", uri: "/api/v1/run-logs/search")
@paginated(inputToken: "cursor", outputToken: "next_cursor", pageSize: "limit", items: "results")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: LogsReadPermission, organization: {source: "token_org_id"})
@audit(event: RunLogsSearchAuditEvent, resource: RunLogsResource, action: "search")
@rateLimit(bucket: "logs_read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.runLogs", method: "search", paginated: true, retryable: true)
operation SearchRunLogs {
    input: SearchRunLogsInput
    output: SandboxRunLogSearchPage
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

structure SearchRunLogsInput {
    @httpQuery("limit")
    limit: RunLogSearchPageSize

    @httpQuery("cursor")
    cursor: RunLogSearchCursor

    @httpQuery("query")
    query: LogQuery

    @httpQuery("run_id")
    run_id: RunId

    @httpQuery("attempt_id")
    attempt_id: AttemptId

    @httpQuery("source_kind")
    source_kind: SourceKind

    @httpQuery("repository")
    repository: RepositoryFullName

    @httpQuery("workflow")
    workflow: WorkflowName

    @httpQuery("branch")
    branch: GitRef

    @httpQuery("runner_class")
    runner_class: RunnerClass
}

@readonly
@http(method: "GET", uri: "/api/v1/run-analytics/jobs")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: AnalyticsReadPermission, organization: {source: "token_org_id"})
@audit(event: JobsAnalyticsReadAuditEvent, resource: RunAnalyticsJobsResource, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.analytics", method: "jobs", paginated: false, retryable: true)
operation GetJobsAnalytics {
    input: AnalyticsWindowInput
    output: SandboxJobsAnalyticsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

@readonly
@http(method: "GET", uri: "/api/v1/run-analytics/costs")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: AnalyticsReadPermission, organization: {source: "token_org_id"})
@audit(event: CostsAnalyticsReadAuditEvent, resource: RunAnalyticsCostsResource, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.analytics", method: "costs", paginated: false, retryable: true)
operation GetCostsAnalytics {
    input: AnalyticsWindowInput
    output: SandboxCostsAnalyticsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

@readonly
@http(method: "GET", uri: "/api/v1/run-analytics/runner-sizing")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: AnalyticsReadPermission, organization: {source: "token_org_id"})
@audit(event: RunnerSizingAnalyticsReadAuditEvent, resource: RunAnalyticsRunnerSizingResource, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.analytics", method: "runnerSizing", paginated: false, retryable: true)
operation GetRunnerSizingAnalytics {
    input: AnalyticsWindowInput
    output: SandboxRunnerSizingAnalyticsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

structure AnalyticsWindowInput {
    @httpQuery("start")
    start: DateTime

    @httpQuery("end")
    end: DateTime
}

structure SandboxJobsAnalyticsOutput {
    @required
    @httpPayload
    @nestedProperties
    analytics: SandboxJobsAnalytics
}

structure SandboxCostsAnalyticsOutput {
    @required
    @httpPayload
    @nestedProperties
    analytics: SandboxCostsAnalytics
}

structure SandboxRunnerSizingAnalyticsOutput {
    @required
    @httpPayload
    @nestedProperties
    analytics: SandboxRunnerSizingAnalytics
}

@readonly
@http(method: "GET", uri: "/api/v1/cache/volumes")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: CacheReadPermission, organization: {source: "token_org_id"})
@audit(event: CacheVolumeListAuditEvent, resource: CacheVolume, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.cache", method: "listVolumes", paginated: false, retryable: true)
operation ListCacheVolumes {
    input: EmptyInput
    output: SandboxCacheVolumesPage
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

@readonly
@http(method: "GET", uri: "/api/v1/cache/volumes/{cache_volume_id}/generations")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: CacheReadPermission, organization: {source: "token_org_id"})
@audit(event: CacheGenerationListAuditEvent, resource: CacheGeneration, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.cache", method: "listGenerations", paginated: false, retryable: true)
operation ListCacheGenerations {
    input: CacheVolumePathInput
    output: SandboxCacheGenerationsPage
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

structure CacheVolumePathInput {
    @required
    @httpLabel
    cache_volume_id: CacheVolumeId
}

@idempotent
@http(method: "POST", uri: "/api/v1/cache/generations/{cache_generation_id}/delete")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: CacheWritePermission, organization: {source: "token_org_id"})
@audit(event: CacheGenerationDeleteAuditEvent, resource: CacheGeneration, action: "delete")
@rateLimit(bucket: "cache_mutation")
@requestBudget(maxBytes: 1024)
@sdk(module: "sandbox.cache", method: "deleteGeneration", paginated: false, retryable: false)
operation DeleteCacheGeneration {
    input: DeleteCacheGenerationInput
    output: SandboxCacheGenerationDeleteOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure DeleteCacheGenerationInput {
    @required
    @httpLabel
    cache_generation_id: CacheGenerationId

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
}

structure SandboxCacheGenerationDeleteOutput {
    @required
    @httpPayload
    @nestedProperties
    result: SandboxCacheGenerationDeleteResult
}

@idempotent
@http(method: "POST", uri: "/api/v1/cache/volumes/{cache_volume_id}/paths/delete")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: CacheWritePermission, organization: {source: "token_org_id"})
@audit(event: CachePathDeleteAuditEvent, resource: CacheVolume, action: "delete")
@rateLimit(bucket: "cache_mutation")
@requestBudget(maxBytes: 4096)
@sdk(module: "sandbox.cache", method: "deletePath", paginated: false, retryable: false)
operation DeleteCachePath {
    input: DeleteCachePathInput
    output: SandboxCachePathDeleteOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure DeleteCachePathInput {
    @required
    @httpLabel
    cache_volume_id: CacheVolumeId

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    path: CachePath
}

structure SandboxCachePathDeleteOutput {
    @required
    @httpPayload
    @nestedProperties
    result: SandboxCachePathDeleteResult
}

@idempotent
@http(method: "POST", uri: "/api/v1/execution-schedules", code: 201)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: ScheduleWritePermission, organization: {source: "token_org_id"})
@audit(event: ScheduleCreateAuditEvent, resource: ExecutionSchedule, action: "create")
@rateLimit(bucket: "execution_schedule_mutation")
@requestBudget(maxBytes: 262144)
@sdk(module: "sandbox.schedules", method: "create", paginated: false, retryable: false)
operation CreateExecutionSchedule {
    input: CreateExecutionScheduleInput
    output: SandboxExecutionScheduleOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ConflictError, RateLimitedError, ServiceUnavailableError]
}

structure CreateExecutionScheduleInput {
    @required
    @idempotencyToken
    idempotency_key: IdempotencyKey

    display_name: DisplayName

    @required
    project_id: ProjectId

    @required
    source_repository_id: SourceRepositoryId

    @required
    workflow_path: WorkflowPath

    ref: GitRef
    inputs: ScheduleInputs

    @required
    interval_seconds: IntervalSeconds

    paused: Boolean
}

@readonly
@http(method: "GET", uri: "/api/v1/execution-schedules")
@paginated(inputToken: "cursor", outputToken: "next_cursor", pageSize: "limit", items: "schedules")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: ScheduleReadPermission, organization: {source: "token_org_id"})
@audit(event: ScheduleListAuditEvent, resource: ExecutionSchedule, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.schedules", method: "list", paginated: true, retryable: true)
operation ListExecutionSchedules {
    input: ListExecutionSchedulesInput
    output: SandboxExecutionSchedulesPage
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

structure ListExecutionSchedulesInput {
    @httpQuery("limit")
    limit: ScheduleListPageSize

    @httpQuery("cursor")
    cursor: ScheduleListCursor
}

@readonly
@http(method: "GET", uri: "/api/v1/execution-schedules/{schedule_id}")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: ScheduleReadPermission, organization: {source: "token_org_id"})
@audit(event: ScheduleReadAuditEvent, resource: ExecutionSchedule, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.schedules", method: "get", paginated: false, retryable: true)
operation GetExecutionSchedule {
    input: ExecutionSchedulePathInput
    output: SandboxExecutionScheduleOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

structure ExecutionSchedulePathInput {
    @required
    @httpLabel
    schedule_id: ScheduleId
}

structure SandboxExecutionScheduleOutput {
    @required
    @httpPayload
    @nestedProperties
    schedule: SandboxExecutionScheduleRecord
}

@idempotent
@http(method: "POST", uri: "/api/v1/execution-schedules/{schedule_id}/pause")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: ScheduleWritePermission, organization: {source: "token_org_id"})
@audit(event: SchedulePauseAuditEvent, resource: ExecutionSchedule, action: "pause")
@rateLimit(bucket: "execution_schedule_mutation")
@requestBudget(maxBytes: 1024)
@sdk(module: "sandbox.schedules", method: "pause", paginated: false, retryable: false)
operation PauseExecutionSchedule {
    input: ExecutionScheduleMutationInput
    output: SandboxExecutionScheduleOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

@idempotent
@http(method: "POST", uri: "/api/v1/execution-schedules/{schedule_id}/resume")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: ScheduleWritePermission, organization: {source: "token_org_id"})
@audit(event: ScheduleResumeAuditEvent, resource: ExecutionSchedule, action: "resume")
@rateLimit(bucket: "execution_schedule_mutation")
@requestBudget(maxBytes: 1024)
@sdk(module: "sandbox.schedules", method: "resume", paginated: false, retryable: false)
operation ResumeExecutionSchedule {
    input: ExecutionScheduleMutationInput
    output: SandboxExecutionScheduleOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure ExecutionScheduleMutationInput {
    @required
    @httpLabel
    schedule_id: ScheduleId

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
}

@http(method: "POST", uri: "/internal/v1/runner/repositories", code: 201)
@identity(mode: "spiffe_mtls", audience: "sandbox-rental-service", principals: ["workload"])
@authz(permission: RunnerRepositoryRegisterPermission, organization: {source: "body_org_id", member: "org_id"})
@audit(event: RunnerRepositoryRegisterAuditEvent, resource: RunnerRepository, action: "register")
@rateLimit(bucket: "internal_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "sandboxInternal.runnerRepositories", method: "register", paginated: false, retryable: false)
operation InternalRegisterRunnerRepository {
    input: InternalRegisterRunnerRepositoryInput
    output: InternalRegisterRunnerRepositoryOutput
    errors: [ValidationFailedError, PermissionDeniedError, ConflictError, ServiceUnavailableError]
}

structure InternalRegisterRunnerRepositoryInput {
    @required
    provider: Provider

    @required
    org_id: OrgId

    @required
    project_id: ProjectId

    source_repository_id: SourceRepositoryId

    @required
    provider_owner: ProviderOwner

    @required
    provider_repo: ProviderRepo

    @required
    provider_repository_id: ProviderRepositoryId

    repository_full_name: RepositoryFullName
}

structure InternalRegisterRunnerRepositoryOutput {
    @required
    registration: RunnerRepositoryRegistration
}
