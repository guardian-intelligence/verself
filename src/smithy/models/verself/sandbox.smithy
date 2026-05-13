$version: "2"
namespace verself.sandbox.v1
use smithy.api#http
use smithy.api#httpHeader
use smithy.api#httpLabel
use smithy.api#httpQuery
use smithy.api#idempotencyToken
use smithy.api#idempotent
use smithy.api#length
use smithy.api#paginated
use smithy.api#pattern
use smithy.api#range
use smithy.api#readonly
use smithy.api#required
use verself.common.v1#ConflictError
use verself.common.v1#DisplayName
use verself.common.v1#IdempotencyKey
use verself.common.v1#IdempotencyPayloadMismatchError
use verself.common.v1#PageSize
use verself.common.v1#PageToken
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
use verself.common.v1#protoField
use verself.common.v1#rateLimit
use verself.common.v1#requestBudget
use verself.common.v1#sdk
use verself.common.v1#serviceRuntime
@serviceRuntime(serviceName: "sandbox-rental-service", publicAudience: "sandbox-rental-service", internalAudience: "sandbox-rental-service")
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
        CreateExecutionSchedule,
        ListExecutionSchedules,
        GetExecutionSchedule,
        PauseExecutionSchedule,
        ResumeExecutionSchedule
    ]
    resources: [
        GitHubInstallation,
        Execution,
        Run,
        ExecutionSchedule
    ]
}
@serviceRuntime(serviceName: "sandbox-rental-service", publicAudience: "sandbox-rental-service", internalAudience: "sandbox-rental-service")
service SandboxRentalInternal {
    version: "2026-05-13"
    operations: [InternalRegisterRunnerRepository]
    resources: [RunnerRepository]
}
@pattern("^[0-9a-fA-F-]{36}$")
string ExecutionId
@pattern("^[0-9a-fA-F-]{36}$")
string AttemptId
@pattern("^[0-9a-fA-F-]{36}$")
string RunId
@pattern("^[0-9a-fA-F-]{36}$")
string ScheduleId
@pattern("^[0-9a-fA-F-]{36}$")
string ProjectId
@pattern("^[0-9a-fA-F-]{36}$")
string SourceRepositoryId
@length(min: 1, max: 128)
string OrgId
@length(min: 1, max: 512)
string ActorId
@length(min: 1, max: 64)
string Provider
@length(min: 1, max: 64)
string ExecutionStatus
@length(min: 1, max: 64)
string SourceKind
@length(min: 1, max: 64)
string WorkloadKind
@length(min: 1, max: 255)
string RepositoryFullName
@length(min: 1, max: 255)
string RunnerClass
@length(min: 1, max: 512)
string WorkflowPath
@length(min: 1, max: 255)
string GitRef
@length(max: 2048)
string LogQuery
@length(min: 1, max: 4096)
string LogChunk
@length(min: 1, max: 512)
string SetupURL
@length(min: 1, max: 512)
string ProviderRepositoryId
@length(min: 1, max: 255)
string ProviderOwner
@length(min: 1, max: 255)
string ProviderRepo
@length(min: 1, max: 512)
string ScheduleWorkflowId
@range(min: 15, max: 4294967295)
integer IntervalSeconds
@pattern("^[0-9]+$")
string DecimalUint64
list GitHubInstallations {
    member: GitHubInstallationRecord
}
list InstallationRepositories {
    member: GitHubInstallationRepository
}
list Runs {
    member: RunSummary
}
list RunLogResults {
    member: RunLogSearchResult
}
list ExecutionSchedules {
    member: ExecutionScheduleRecord
}
list AnalyticsBuckets {
    member: AnalyticsBucket
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
resource Execution {}
resource Run {}
resource ExecutionSchedule {}
resource RunnerRepository {}
structure GitHubInstallationConnectResponse {
    @required
    state: String
    @required
    setup_url: SetupURL
    @required
    expires_at: Timestamp
}
structure GitHubInstallationRecord {
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
    created_at: Timestamp
    @required
    updated_at: Timestamp
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
    synced_at: Timestamp
}
structure RunSummary {
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
    source_kind: SourceKind
    workload_kind: WorkloadKind
    runner_class: RunnerClass
    repository_full_name: RepositoryFullName
    @required
    status: ExecutionStatus
    @required
    created_at: Timestamp
    @required
    updated_at: Timestamp
}
structure ExecutionLogs {
    @required
    execution_id: ExecutionId
    @required
    attempt_id: AttemptId
    @required
    logs: String
}
structure RunLogSearchResult {
    @required
    execution_id: ExecutionId
    @required
    attempt_id: AttemptId
    @required
    chunk: LogChunk
    @required
    created_at: Timestamp
}
structure AnalyticsBucket {
    @required
    key: String
    @required
    count: DecimalUint64
}
structure AnalyticsWindow {
    @required
    window_start: Timestamp
    @required
    window_end: Timestamp
    @required
    by_source: AnalyticsBuckets
    @required
    by_runner_class: AnalyticsBuckets
}
structure ExecutionScheduleRecord {
    @required
    schedule_id: ScheduleId
    @required
    resourceName: ResourceName
    @required
    org_id: OrgId
    @required
    project_id: ProjectId
    @required
    source_repository_id: SourceRepositoryId
    @required
    workflow_path: WorkflowPath
    ref: GitRef
    @required
    interval_seconds: IntervalSeconds
    @required
    state: String
    @required
    created_at: Timestamp
    @required
    updated_at: Timestamp
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
@identity(mode: "bearer", audience: "sandbox-rental-service", principals: ["browser", "cli"])
@authz(permission: GitHubInstallationWritePermission, organization: {source: "token_org_id"})
@audit(event: GitHubInstallationConnectAuditEvent, resource: GitHubInstallation, action: "connect")
@rateLimit(bucket: "github_installation_mutation")
@requestBudget(maxBytes: 1024)
@sdk(module: "sandbox.githubInstallations", method: "beginConnect", paginated: false, retryable: false)
operation BeginGithubInstallation {
    input: EmptyInput
    output: BeginGithubInstallationOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
structure EmptyInput {}
structure BeginGithubInstallationOutput {
    @required
    connect: GitHubInstallationConnectResponse
}
@readonly
@http(method: "GET", uri: "/api/v1/github/installations")
@identity(mode: "bearer", audience: "sandbox-rental-service", principals: ["browser", "cli"])
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
structure ListGithubInstallationsOutput {
    @required
    installations: GitHubInstallations
}
@idempotent
@http(method: "POST", uri: "/api/v1/github/installations/{installation_id}/repositories/sync")
@identity(mode: "bearer", audience: "sandbox-rental-service", principals: ["browser", "cli"])
@authz(permission: GitHubInstallationWritePermission, organization: {source: "token_org_id"})
@audit(event: GitHubRepositoriesSyncAuditEvent, resource: GitHubInstallation, action: "sync")
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
    synced_at: Timestamp
    @required
    repositories: InstallationRepositories
}
@readonly
@http(method: "GET", uri: "/api/v1/executions/{execution_id}")
@identity(mode: "bearer", audience: "sandbox-rental-service", principals: ["browser", "cli", "workload"])
@authz(permission: ExecutionReadPermission, organization: {source: "token_org_id"})
@audit(event: ExecutionReadAuditEvent, resource: Execution, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.executions", method: "get", paginated: false, retryable: true)
operation GetExecution {
    input: ExecutionPathInput
    output: RunOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}
structure ExecutionPathInput {
    @required
    @httpLabel
    execution_id: ExecutionId
}
@readonly
@http(method: "GET", uri: "/api/v1/executions/{execution_id}/logs")
@identity(mode: "bearer", audience: "sandbox-rental-service", principals: ["browser", "cli", "workload"])
@authz(permission: LogsReadPermission, organization: {source: "token_org_id"})
@audit(event: ExecutionLogsReadAuditEvent, resource: Execution, action: "read")
@rateLimit(bucket: "logs_read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.executions", method: "getLogs", paginated: false, retryable: true)
operation GetExecutionLogs {
    input: ExecutionPathInput
    output: GetExecutionLogsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}
structure GetExecutionLogsOutput {
    @required
    logs: ExecutionLogs
}
@readonly
@http(method: "GET", uri: "/api/v1/runs")
@paginated(inputToken: "cursor", outputToken: "next_cursor", pageSize: "limit", items: "runs")
@identity(mode: "bearer", audience: "sandbox-rental-service", principals: ["browser", "cli", "workload"])
@authz(permission: ExecutionReadPermission, organization: {source: "token_org_id"})
@audit(event: RunListAuditEvent, resource: Run, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.runs", method: "list", paginated: true, retryable: true)
operation ListRuns {
    input: ListRunsInput
    output: ListRunsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
structure ListRunsInput {
    @httpQuery("limit")
    limit: PageSize
    @httpQuery("cursor")
    cursor: PageToken
    @httpQuery("source_kind")
    source_kind: SourceKind
    @httpQuery("status")
    status: ExecutionStatus
    @httpQuery("repository")
    repository: RepositoryFullName
    @httpQuery("runner_class")
    runner_class: RunnerClass
}
structure ListRunsOutput {
    @required
    runs: Runs
    next_cursor: PageToken
    @required
    limit: PageSize
}
@readonly
@http(method: "GET", uri: "/api/v1/runs/{run_id}")
@identity(mode: "bearer", audience: "sandbox-rental-service", principals: ["browser", "cli", "workload"])
@authz(permission: ExecutionReadPermission, organization: {source: "token_org_id"})
@audit(event: RunReadAuditEvent, resource: Run, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.runs", method: "get", paginated: false, retryable: true)
operation GetRun {
    input: RunPathInput
    output: RunOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}
structure RunPathInput {
    @required
    @httpLabel
    run_id: RunId
}
structure RunOutput {
    @required
    run: RunSummary
}
@readonly
@http(method: "GET", uri: "/api/v1/run-logs/search")
@paginated(inputToken: "cursor", outputToken: "next_cursor", pageSize: "limit", items: "results")
@identity(mode: "bearer", audience: "sandbox-rental-service", principals: ["browser", "cli", "workload"])
@authz(permission: LogsReadPermission, organization: {source: "token_org_id"})
@audit(event: RunLogsSearchAuditEvent, resource: Run, action: "search")
@rateLimit(bucket: "logs_read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.runLogs", method: "search", paginated: true, retryable: true)
operation SearchRunLogs {
    input: SearchRunLogsInput
    output: SearchRunLogsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
structure SearchRunLogsInput {
    @httpQuery("limit")
    limit: PageSize
    @httpQuery("cursor")
    cursor: PageToken
    @httpQuery("query")
    query: LogQuery
    @httpQuery("run_id")
    run_id: RunId
    @httpQuery("attempt_id")
    attempt_id: AttemptId
}
structure SearchRunLogsOutput {
    @required
    results: RunLogResults
    next_cursor: PageToken
    @required
    limit: PageSize
}
@readonly
@http(method: "GET", uri: "/api/v1/run-analytics/jobs")
@identity(mode: "bearer", audience: "sandbox-rental-service", principals: ["browser", "cli"])
@authz(permission: AnalyticsReadPermission, organization: {source: "token_org_id"})
@audit(event: JobsAnalyticsReadAuditEvent, resource: Run, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.analytics", method: "jobs", paginated: false, retryable: true)
operation GetJobsAnalytics {
    input: AnalyticsWindowInput
    output: AnalyticsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
@readonly
@http(method: "GET", uri: "/api/v1/run-analytics/costs")
@identity(mode: "bearer", audience: "sandbox-rental-service", principals: ["browser", "cli"])
@authz(permission: AnalyticsReadPermission, organization: {source: "token_org_id"})
@audit(event: CostsAnalyticsReadAuditEvent, resource: Run, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.analytics", method: "costs", paginated: false, retryable: true)
operation GetCostsAnalytics {
    input: AnalyticsWindowInput
    output: AnalyticsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
@readonly
@http(method: "GET", uri: "/api/v1/run-analytics/runner-sizing")
@identity(mode: "bearer", audience: "sandbox-rental-service", principals: ["browser", "cli"])
@authz(permission: AnalyticsReadPermission, organization: {source: "token_org_id"})
@audit(event: RunnerSizingAnalyticsReadAuditEvent, resource: Run, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.analytics", method: "runnerSizing", paginated: false, retryable: true)
operation GetRunnerSizingAnalytics {
    input: AnalyticsWindowInput
    output: AnalyticsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
structure AnalyticsWindowInput {
    @httpQuery("start")
    start: Timestamp
    @httpQuery("end")
    end: Timestamp
}
structure AnalyticsOutput {
    @required
    analytics: AnalyticsWindow
}
@idempotent
@http(method: "POST", uri: "/api/v1/execution-schedules", code: 201)
@identity(mode: "bearer", audience: "sandbox-rental-service", principals: ["browser", "cli"])
@authz(permission: ScheduleWritePermission, organization: {source: "token_org_id"})
@audit(event: ScheduleCreateAuditEvent, resource: ExecutionSchedule, action: "create")
@rateLimit(bucket: "execution_schedule_mutation")
@requestBudget(maxBytes: 8192)
@sdk(module: "sandbox.schedules", method: "create", paginated: false, retryable: false)
operation CreateExecutionSchedule {
    input: CreateExecutionScheduleInput
    output: ExecutionScheduleOutput
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
    @required
    interval_seconds: IntervalSeconds
    paused: Boolean
}
@readonly
@http(method: "GET", uri: "/api/v1/execution-schedules")
@identity(mode: "bearer", audience: "sandbox-rental-service", principals: ["browser", "cli"])
@authz(permission: ScheduleReadPermission, organization: {source: "token_org_id"})
@audit(event: ScheduleListAuditEvent, resource: ExecutionSchedule, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.schedules", method: "list", paginated: false, retryable: true)
operation ListExecutionSchedules {
    input: EmptyInput
    output: ListExecutionSchedulesOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
structure ListExecutionSchedulesOutput {
    @required
    schedules: ExecutionSchedules
}
@readonly
@http(method: "GET", uri: "/api/v1/execution-schedules/{schedule_id}")
@identity(mode: "bearer", audience: "sandbox-rental-service", principals: ["browser", "cli"])
@authz(permission: ScheduleReadPermission, organization: {source: "token_org_id"})
@audit(event: ScheduleReadAuditEvent, resource: ExecutionSchedule, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "sandbox.schedules", method: "get", paginated: false, retryable: true)
operation GetExecutionSchedule {
    input: ExecutionSchedulePathInput
    output: ExecutionScheduleOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}
structure ExecutionSchedulePathInput {
    @required
    @httpLabel
    schedule_id: ScheduleId
}
structure ExecutionScheduleOutput {
    @required
    schedule: ExecutionScheduleRecord
}
@idempotent
@http(method: "POST", uri: "/api/v1/execution-schedules/{schedule_id}/pause")
@identity(mode: "bearer", audience: "sandbox-rental-service", principals: ["browser", "cli"])
@authz(permission: ScheduleWritePermission, organization: {source: "token_org_id"})
@audit(event: SchedulePauseAuditEvent, resource: ExecutionSchedule, action: "pause")
@rateLimit(bucket: "execution_schedule_mutation")
@requestBudget(maxBytes: 1024)
@sdk(module: "sandbox.schedules", method: "pause", paginated: false, retryable: false)
operation PauseExecutionSchedule {
    input: ExecutionScheduleMutationInput
    output: ExecutionScheduleOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "POST", uri: "/api/v1/execution-schedules/{schedule_id}/resume")
@identity(mode: "bearer", audience: "sandbox-rental-service", principals: ["browser", "cli"])
@authz(permission: ScheduleWritePermission, organization: {source: "token_org_id"})
@audit(event: ScheduleResumeAuditEvent, resource: ExecutionSchedule, action: "resume")
@rateLimit(bucket: "execution_schedule_mutation")
@requestBudget(maxBytes: 1024)
@sdk(module: "sandbox.schedules", method: "resume", paginated: false, retryable: false)
operation ResumeExecutionSchedule {
    input: ExecutionScheduleMutationInput
    output: ExecutionScheduleOutput
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
@requestBudget(maxBytes: 8192)
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
