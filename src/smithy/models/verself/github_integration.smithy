$version: "2"
namespace verself.githubintegration.v1

use aws.protocols#restJson1

use smithy.api#http
use smithy.api#httpHeader
use smithy.api#httpLabel
use smithy.api#httpPayload
use smithy.api#httpQuery
use smithy.api#idempotencyToken
use smithy.api#idempotent
use smithy.api#length
use smithy.api#mediaType
use smithy.api#paginated
use smithy.api#pattern
use smithy.api#range
use smithy.api#readonly
use smithy.api#required
use smithy.api#sensitive
use verself.common.v1#ConflictError
use verself.common.v1#DateTime
use verself.common.v1#IdempotencyKey
use verself.common.v1#IdempotencyPayloadMismatchError
use verself.common.v1#PageRequest
use verself.common.v1#PageResponse
use verself.common.v1#PermissionDeniedError
use verself.common.v1#ProblemOccurrences
use verself.common.v1#ProviderWebhookDeliveryReplayConflictError
use verself.common.v1#ProviderWebhookInboxUnavailableError
use verself.common.v1#ProviderWebhookInvalidRequestError
use verself.common.v1#ProviderWebhookSignatureInvalidError
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

@serviceRuntime(serviceName: "github-integration-service", publicAudience: "verself-api", internalAudience: "github-integration-service")
@restJson1
service GithubIntegration {
    version: "2026-05-21"
    operations: [
        StartGithubAppSetup,
        GetGithubSetupSession,
        CompleteGithubAppSetup,
        StartGithubUserAuthorization,
        CompleteGithubUserAuthorization,
        ListGithubInstallations,
        GetGithubInstallation,
        SyncGithubInstallation,
        DisconnectGithubInstallation,
        ListGithubRepositories,
        GetGithubRepository,
        EnableGithubRepository,
        DisableGithubRepository,
        ReceiveGithubWebhook
    ]
    resources: [
        GithubSetupSession,
        GithubUserAuthorization,
        GithubAccount,
        GithubInstallationBinding,
        GithubInstallationRepository,
        GithubRepositoryBinding,
        GithubInstallation,
        GithubRepository,
        GithubWebhookDelivery,
        GithubWorkflowRun,
        GithubWorkflowJob,
        GithubJobShape,
        GithubProviderDemand,
        GithubRunnerInstance,
        GithubJobAssignment,
        GithubTerminalJobEvidence,
        GithubGoldenSnapshotBarrier
    ]
}

@serviceRuntime(serviceName: "github-integration-service", publicAudience: "verself-api", internalAudience: "github-integration-service")
@restJson1
service GithubIntegrationInternal {
    version: "2026-05-21"
    operations: []
    resources: [
        GithubSetupSession,
        GithubUserAuthorization,
        GithubAccount,
        GithubInstallationBinding,
        GithubInstallationRepository,
        GithubRepositoryBinding,
        GithubInstallation,
        GithubRepository,
        GithubWebhookDelivery,
        GithubWorkflowRun,
        GithubWorkflowJob,
        GithubJobShape,
        GithubProviderDemand,
        GithubRunnerInstance,
        GithubJobAssignment,
        GithubTerminalJobEvidence,
        GithubGoldenSnapshotBarrier
    ]
}

@pattern("^[0-9a-fA-F-]{36}$")
string AttemptId

@pattern("^[0-9a-fA-F-]{36}$")
string ExecutionId

@pattern("^[0-9a-fA-F-]{36}$")
string AllocationId

@pattern("^[0-9a-fA-F-]{36}$")
string TerminalEvidenceId

@pattern("^[0-9a-fA-F-]{36}$")
string GithubSetupSessionId

@pattern("^[0-9a-fA-F-]{36}$")
string GithubOAuthSessionId

@pattern("^[0-9a-fA-F-]{36}$")
string GithubUserAuthorizationId

@pattern("^[0-9a-fA-F-]{36}$")
string GithubInstallationBindingId

@pattern("^[0-9a-fA-F-]{36}$")
string GithubRepositoryBindingId

@length(min: 1, max: 128)
@pattern("^org_[0-9A-HJKMNP-TV-Z]{26}$")
string OrgId

@length(min: 1, max: 255)
string ActorId

@range(min: 0, max: 9007199254740991)
long SafeNonNegativeLong

@range(min: 1, max: 9007199254740991)
long GithubProviderId

@length(min: 1, max: 512)
string RunnerName

@length(min: 1, max: 255)
string RunnerClass

@length(min: 1, max: 512)
string ProviderRepositoryId

@length(min: 1, max: 1024)
string RepositoryFullName

@length(min: 1, max: 255)
string GithubLogin

@length(min: 1, max: 64)
string GithubAccountType

@length(min: 1, max: 64)
string GithubInstallationState

@length(min: 1, max: 64)
string GithubRepositoryState

@length(min: 1, max: 64)
string GithubSetupState

@length(min: 1, max: 64)
string GithubAuthorizationState

@length(min: 1, max: 64)
string GithubRepositorySelection

@length(min: 1, max: 128)
string GithubOAuthScope

list GithubOAuthScopes {
    member: GithubOAuthScope
}

@length(min: 1, max: 8192)
string URL

@length(min: 1, max: 4096)
@sensitive
string GithubOAuthCode

@length(min: 16, max: 512)
@sensitive
string GithubOAuthState

@length(max: 512)
string GithubAvatarURL

@length(max: 512)
string GithubHTMLURL

@length(min: 1, max: 4096)
string WorkflowPath

@length(min: 1, max: 1024)
string WorkflowName

@length(min: 1, max: 1024)
string JobName

@length(min: 1, max: 1024)
string HeadBranch

@length(min: 1, max: 128)
string HeadSha

@length(min: 1, max: 128)
string GithubWebhookEvent

@length(min: 1, max: 128)
string GithubDeliveryId

@length(min: 1, max: 255)
@sensitive
string GithubWebhookSignature

@mediaType("application/json")
blob GithubWebhookPayload

@length(min: 1, max: 255)
string GithubRunnerLabel

list GithubRunnerLabels {
    member: GithubRunnerLabel
}

@permission(name: "github:provider_webhook:receive")
string GithubProviderWebhookReceivePermission

@permission(name: "github:installation:read")
string GithubInstallationReadPermission

@permission(name: "github:installation:write")
string GithubInstallationWritePermission

@permission(name: "github:repository:read")
string GithubRepositoryReadPermission

@permission(name: "github:repository:write")
string GithubRepositoryWritePermission

@permission(name: "github:user_authorization:write")
string GithubUserAuthorizationWritePermission

@auditEvent(name: "github.webhook.receive")
string GithubWebhookReceiveAuditEvent

@auditEvent(name: "github.setup.start")
string GithubSetupStartAuditEvent

@auditEvent(name: "github.setup.read")
string GithubSetupReadAuditEvent

@auditEvent(name: "github.setup.complete")
string GithubSetupCompleteAuditEvent

@auditEvent(name: "github.user_authorization.start")
string GithubUserAuthorizationStartAuditEvent

@auditEvent(name: "github.user_authorization.complete")
string GithubUserAuthorizationCompleteAuditEvent

@auditEvent(name: "github.installation.list")
string GithubInstallationListAuditEvent

@auditEvent(name: "github.installation.read")
string GithubInstallationReadAuditEvent

@auditEvent(name: "github.installation.sync")
string GithubInstallationSyncAuditEvent

@auditEvent(name: "github.installation.disconnect")
string GithubInstallationDisconnectAuditEvent

@auditEvent(name: "github.repository.list")
string GithubRepositoryListAuditEvent

@auditEvent(name: "github.repository.read")
string GithubRepositoryReadAuditEvent

@auditEvent(name: "github.repository.enable")
string GithubRepositoryEnableAuditEvent

@auditEvent(name: "github.repository.disable")
string GithubRepositoryDisableAuditEvent

resource GithubSetupSession {}
resource GithubUserAuthorization {}
resource GithubAccount {}
resource GithubWebhookDelivery {}
resource GithubInstallation {}
resource GithubInstallationBinding {}
resource GithubInstallationRepository {}
resource GithubRepository {}
resource GithubRepositoryBinding {}
resource GithubWorkflowRun {}
resource GithubWorkflowJob {}
resource GithubJobShape {}
resource GithubProviderDemand {}
resource GithubRunnerInstance {}
resource GithubJobAssignment {}
resource GithubTerminalJobEvidence {}
resource GithubGoldenSnapshotBarrier {}

enum GithubWebhookDeliveryState {
    ACCEPTED = "accepted"
    PROCESSING = "processing"
    RETRYABLE = "retryable"
    PROCESSED = "processed"
    IGNORED = "ignored"
    REJECTED = "rejected"
    FAILED = "failed"
}

enum GithubWorkflowJobStatus {
    QUEUED = "queued"
    IN_PROGRESS = "in_progress"
    COMPLETED = "completed"
}

enum GithubWorkflowJobConclusion {
    SUCCESS = "success"
    FAILURE = "failure"
    CANCELLED = "cancelled"
    SKIPPED = "skipped"
    TIMED_OUT = "timed_out"
    ACTION_REQUIRED = "action_required"
    NEUTRAL = "neutral"
}

enum GithubProviderDemandState {
    DEMAND_RECORDED = "demand_recorded"
    CAPACITY_REQUESTED = "capacity_requested"
    CAPACITY_FAILED = "capacity_failed"
    JIT_FAILED = "jit_failed"
    SANDBOX_FAILED = "sandbox_failed"
    ASSIGNED = "assigned"
    COMPLETED = "completed"
}

enum GithubRunnerInstanceState {
    JIT_CREATED = "jit_created"
    SANDBOX_SUBMITTED = "sandbox_submitted"
    ASSIGNED = "assigned"
    JOB_COMPLETED = "job_completed"
    FAILED = "failed"
    CLEANED = "cleaned"
}

enum GithubGoldenSnapshotBarrierState {
    REQUESTED = "requested"
    BLOCKED = "blocked"
    SATISFIED = "satisfied"
    FAILED = "failed"
}

enum GithubSetupSessionState {
    PENDING_SETUP = "pending_setup"
    AWAITING_USER_AUTHORIZATION = "awaiting_user_authorization"
    COMPLETED = "completed"
    EXPIRED = "expired"
    FAILED = "failed"
}

enum GithubUserAuthorizationState {
    ACTIVE = "active"
    REVOKED = "revoked"
    EXPIRED = "expired"
}

enum GithubInstallationBindingState {
    ACTIVE = "active"
    DISCONNECTED = "disconnected"
    REVOKED = "revoked"
}

enum GithubRepositoryBindingState {
    ENABLED = "enabled"
    DISABLED = "disabled"
    UNAVAILABLE = "unavailable"
}

enum GithubInstallationRepositoryState {
    SELECTED = "selected"
    REMOVED = "removed"
}

structure GithubSetupSessionRecord {
    @required
    setup_session_id: GithubSetupSessionId

    @required
    resourceName: ResourceName

    @required
    org_id: OrgId

    @required
    actor_id: ActorId

    @required
    state: GithubSetupSessionState

    installation_url: URL
    callback_url: URL
    provider_installation_id: GithubProviderId
    user_authorization_id: GithubUserAuthorizationId
    installation_binding_id: GithubInstallationBindingId
    expires_at: DateTime
    completed_at: DateTime
    created_at: DateTime
    updated_at: DateTime
}

structure GithubUserAuthorizationRecord {
    @required
    user_authorization_id: GithubUserAuthorizationId

    @required
    resourceName: ResourceName

    @required
    org_id: OrgId

    @required
    actor_id: ActorId

    @required
    provider_user_id: GithubProviderId

    @required
    github_login: GithubLogin

    @required
    state: GithubUserAuthorizationState

    scopes: GithubOAuthScopes
    authorized_at: DateTime
    last_verified_at: DateTime
    revoked_at: DateTime
}

structure GithubAccountRecord {
    @required
    provider_account_id: GithubProviderId

    @required
    login: GithubLogin

    @required
    account_type: GithubAccountType

    avatar_url: GithubAvatarURL
    html_url: GithubHTMLURL
    state: String
    observed_from_api_at: DateTime
}

structure GithubInstallationBindingRecord {
    @required
    installation_binding_id: GithubInstallationBindingId

    @required
    resourceName: ResourceName

    @required
    org_id: OrgId

    @required
    provider_installation_id: GithubProviderId

    @required
    provider_account_id: GithubProviderId

    @required
    account_login: GithubLogin

    @required
    account_type: GithubAccountType

    @required
    repository_selection: GithubRepositorySelection

    @required
    state: GithubInstallationBindingState

    app_slug: String
    configuration_url: URL
    setup_session_id: GithubSetupSessionId
    connected_by_actor_id: ActorId
    connected_at: DateTime
    last_synced_at: DateTime
    disconnected_at: DateTime
}

structure GithubRepositoryBindingRecord {
    @required
    repository_binding_id: GithubRepositoryBindingId

    @required
    resourceName: ResourceName

    @required
    org_id: OrgId

    @required
    installation_binding_id: GithubInstallationBindingId

    @required
    provider_installation_id: GithubProviderId

    @required
    provider_repository_id: GithubProviderId

    @required
    repository_full_name: RepositoryFullName

    @required
    owner_login: GithubLogin

    @required
    repository_name: String

    @required
    state: GithubRepositoryBindingState

    default_branch: String
    private: Boolean
    enabled_by_actor_id: ActorId
    enabled_at: DateTime
    disabled_at: DateTime
    observed_from_api_at: DateTime
}

structure GithubRepositoryCandidateRecord {
    @required
    provider_installation_id: GithubProviderId

    @required
    provider_repository_id: GithubProviderId

    @required
    repository_full_name: RepositoryFullName

    @required
    owner_login: GithubLogin

    @required
    repository_name: String

    @required
    installation_repository_state: GithubInstallationRepositoryState

    repository_binding_id: GithubRepositoryBindingId
    repository_binding_state: GithubRepositoryBindingState
    default_branch: String
    private: Boolean
    observed_from_api_at: DateTime
}

list GithubInstallationBindings {
    member: GithubInstallationBindingRecord
}

list GithubRepositoryBindings {
    member: GithubRepositoryBindingRecord
}

list GithubRepositoryCandidates {
    member: GithubRepositoryCandidateRecord
}

structure GithubWebhookAccepted {
    @required
    status: String

    @required
    delivery_id: GithubDeliveryId
}

structure GithubWebhookDeliveryRecord {
    @required
    delivery_id: GithubDeliveryId

    @required
    event_name: GithubWebhookEvent

    @required
    action: String

    @required
    state: GithubWebhookDeliveryState

    @required
    payload_sha256: String

    provider_installation_id: SafeNonNegativeLong
    provider_repository_id: ProviderRepositoryId
    repository_full_name: RepositoryFullName
    provider_run_id: SafeNonNegativeLong
    provider_run_attempt: SafeNonNegativeLong
    provider_job_id: SafeNonNegativeLong
    problems: ProblemOccurrences
    received_at: DateTime
    verified_at: DateTime
    processed_at: DateTime
}

structure GithubInstallationRecord {
    @required
    provider_installation_id: SafeNonNegativeLong

    org_id: OrgId
    account_id: SafeNonNegativeLong
    account_login: String
    account_type: String
    app_slug: String
    repository_selection: String

    @required
    state: String

    observed_from_api_at: DateTime
}

structure GithubRepositoryRecord {
    @required
    provider_repository_id: ProviderRepositoryId

    provider_installation_id: SafeNonNegativeLong
    org_id: OrgId
    owner_login: String
    repository_name: String

    @required
    repository_full_name: RepositoryFullName

    default_branch: String
    private: Boolean

    @required
    state: String

    observed_from_api_at: DateTime
}

structure GithubWorkflowRunRecord {
    @required
    provider_installation_id: SafeNonNegativeLong

    @required
    provider_repository_id: ProviderRepositoryId

    @required
    provider_run_id: SafeNonNegativeLong

    @required
    provider_run_attempt: SafeNonNegativeLong

    @required
    repository_full_name: RepositoryFullName

    event_name: String
    head_sha: HeadSha
    head_branch: HeadBranch
    head_repository_full_name: RepositoryFullName
    base_sha: HeadSha
    base_branch: HeadBranch
    workflow_path: WorkflowPath
    pull_request_number: SafeNonNegativeLong
    commit_count: SafeNonNegativeLong
    observed_from_api_at: DateTime
}

structure GithubWorkflowJobRecord {
    @required
    provider_job_id: SafeNonNegativeLong

    @required
    provider_repository_id: ProviderRepositoryId

    @required
    provider_run_id: SafeNonNegativeLong

    @required
    provider_run_attempt: SafeNonNegativeLong

    @required
    status: GithubWorkflowJobStatus

    conclusion: GithubWorkflowJobConclusion
    job_name: JobName
    workflow_name: WorkflowName
    repository_full_name: RepositoryFullName
    head_sha: HeadSha
    head_branch: HeadBranch
    labels: GithubRunnerLabels
    runner_name: RunnerName
    observed_from_api_at: DateTime
}

structure GithubJobShapeRecord {
    @required
    job_shape_id: String

    @required
    provider_repository_id: ProviderRepositoryId

    repository_full_name: RepositoryFullName
    workflow_path: WorkflowPath
    workflow_name: WorkflowName
    job_name: JobName
    matrix_key: String
    runner_class: RunnerClass
    labels: GithubRunnerLabels
    cache_manifest_sha256: String
    trust_class: String
}

structure GithubProviderDemandRecord {
    @required
    provider_job_id: SafeNonNegativeLong

    @required
    provider_run_id: SafeNonNegativeLong

    @required
    provider_run_attempt: SafeNonNegativeLong

    runner_class: RunnerClass
    job_shape_id: String
    trust_class: String

    @required
    state: GithubProviderDemandState
}

structure GithubRunnerInstanceRecord {
    @required
    runner_name: RunnerName

    @required
    origin_provider_job_id: SafeNonNegativeLong

    runner_id: SafeNonNegativeLong
    runner_class: RunnerClass
    jit_config_sha256: String

    allocation_id: AllocationId
    execution_id: ExecutionId
    attempt_id: AttemptId

    @required
    assignment_deadline_at: Timestamp

    @required
    state: GithubRunnerInstanceState
}

structure GithubJobAssignmentRecord {
    @required
    provider_job_id: SafeNonNegativeLong

    @required
    runner_name: RunnerName

    runner_id: SafeNonNegativeLong

    @required
    observed_from: String

    @required
    observed_at: DateTime
}

structure GithubTerminalJobEvidenceRecord {
    @required
    terminal_evidence_id: TerminalEvidenceId

    @required
    provider_repository_id: ProviderRepositoryId

    @required
    provider_run_id: SafeNonNegativeLong

    @required
    provider_run_attempt: SafeNonNegativeLong

    @required
    provider_job_id: SafeNonNegativeLong

    @required
    status: GithubWorkflowJobStatus

    @required
    conclusion: GithubWorkflowJobConclusion

    @required
    observed_at: DateTime

    delivery_id: GithubDeliveryId
}

structure GithubGoldenSnapshotBarrierRecord {
    @required
    provider_job_id: SafeNonNegativeLong

    @required
    provider_run_id: SafeNonNegativeLong

    @required
    provider_run_attempt: SafeNonNegativeLong

    execution_id: ExecutionId
    attempt_id: AttemptId
    job_shape_id: String
    trust_class: String

    @required
    state: GithubGoldenSnapshotBarrierState

    failure_reason: String
    requested_at: DateTime
    completed_at: DateTime
}

@idempotent
@http(method: "POST", uri: "/api/v1/github/setup-sessions", code: 201)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: GithubInstallationWritePermission, organization: {source: "token_org_id"})
@audit(event: GithubSetupStartAuditEvent, resource: GithubSetupSession, action: "create")
@rateLimit(bucket: "github_mutation")
@requestBudget(maxBytes: 8192)
@sdk(module: "github.setup", method: "start", paginated: false, retryable: false)
operation StartGithubAppSetup {
    input: StartGithubAppSetupInput
    output: StartGithubAppSetupOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure StartGithubAppSetupInput {
    @required
    @idempotencyToken
    idempotency_key: IdempotencyKey

    callback_url: URL
}

structure StartGithubAppSetupOutput {
    @required
    session: GithubSetupSessionRecord
}

@readonly
@http(method: "GET", uri: "/api/v1/github/setup-sessions/{setup_session_id}", code: 200)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: GithubInstallationReadPermission, organization: {source: "token_org_id"})
@audit(event: GithubSetupReadAuditEvent, resource: GithubSetupSession, action: "read")
@rateLimit(bucket: "github_read")
@requestBudget(maxBytes: 0)
@sdk(module: "github.setup", method: "get", paginated: false, retryable: true)
operation GetGithubSetupSession {
    input: GetGithubSetupSessionInput
    output: GetGithubSetupSessionOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

structure GetGithubSetupSessionInput {
    @required
    @httpLabel
    setup_session_id: GithubSetupSessionId
}

structure GetGithubSetupSessionOutput {
    @required
    session: GithubSetupSessionRecord
}

@idempotent
@http(method: "POST", uri: "/api/v1/github/setup-sessions/{setup_session_id}/complete", code: 200)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: GithubInstallationWritePermission, organization: {source: "token_org_id"})
@audit(event: GithubSetupCompleteAuditEvent, resource: GithubInstallationBinding, action: "connect")
@rateLimit(bucket: "github_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "github.setup", method: "complete", paginated: false, retryable: false)
operation CompleteGithubAppSetup {
    input: CompleteGithubAppSetupInput
    output: CompleteGithubAppSetupOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ConflictError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure CompleteGithubAppSetupInput {
    @required
    @httpLabel
    setup_session_id: GithubSetupSessionId

    @required
    @idempotencyToken
    idempotency_key: IdempotencyKey

    @required
    state: GithubOAuthState

    @required
    provider_installation_id: GithubProviderId

    @required
    user_authorization_id: GithubUserAuthorizationId
}

structure CompleteGithubAppSetupOutput {
    @required
    session: GithubSetupSessionRecord

    @required
    installation: GithubInstallationBindingRecord

    repositories: GithubRepositoryCandidates
}

@idempotent
@http(method: "POST", uri: "/api/v1/github/user-authorizations", code: 201)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: GithubUserAuthorizationWritePermission, organization: {source: "token_org_id"})
@audit(event: GithubUserAuthorizationStartAuditEvent, resource: GithubUserAuthorization, action: "create")
@rateLimit(bucket: "github_mutation")
@requestBudget(maxBytes: 8192)
@sdk(module: "github.userAuthorizations", method: "start", paginated: false, retryable: false)
operation StartGithubUserAuthorization {
    input: StartGithubUserAuthorizationInput
    output: StartGithubUserAuthorizationOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure StartGithubUserAuthorizationInput {
    @required
    @idempotencyToken
    idempotency_key: IdempotencyKey

    callback_url: URL
}

structure StartGithubUserAuthorizationOutput {
    @required
    oauth_session_id: GithubOAuthSessionId

    @required
    authorization_url: URL

    @required
    expires_at: DateTime
}

@idempotent
@http(method: "POST", uri: "/api/v1/github/user-authorizations:complete", code: 200)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: GithubUserAuthorizationWritePermission, organization: {source: "token_org_id"})
@audit(event: GithubUserAuthorizationCompleteAuditEvent, resource: GithubUserAuthorization, action: "connect")
@rateLimit(bucket: "github_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "github.userAuthorizations", method: "complete", paginated: false, retryable: false)
operation CompleteGithubUserAuthorization {
    input: CompleteGithubUserAuthorizationInput
    output: CompleteGithubUserAuthorizationOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ConflictError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure CompleteGithubUserAuthorizationInput {
    @required
    @idempotencyToken
    idempotency_key: IdempotencyKey

    @required
    oauth_session_id: GithubOAuthSessionId

    @required
    state: GithubOAuthState

    @required
    code: GithubOAuthCode
}

structure CompleteGithubUserAuthorizationOutput {
    @required
    authorization: GithubUserAuthorizationRecord
}

@readonly
@paginated(inputToken: "pageToken", outputToken: "nextPageToken", pageSize: "pageSize", items: "installations")
@http(method: "GET", uri: "/api/v1/github/installations", code: 200)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: GithubInstallationReadPermission, organization: {source: "token_org_id"})
@audit(event: GithubInstallationListAuditEvent, resource: GithubInstallationBinding, action: "list")
@rateLimit(bucket: "github_read")
@requestBudget(maxBytes: 0)
@sdk(module: "github.installations", method: "list", paginated: true, retryable: true)
operation ListGithubInstallations {
    input: ListGithubInstallationsInput
    output: ListGithubInstallationsOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

structure ListGithubInstallationsInput with [PageRequest] {}

structure ListGithubInstallationsOutput with [PageResponse] {
    @required
    installations: GithubInstallationBindings
}

@readonly
@http(method: "GET", uri: "/api/v1/github/installations/{installation_binding_id}", code: 200)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: GithubInstallationReadPermission, organization: {source: "token_org_id"})
@audit(event: GithubInstallationReadAuditEvent, resource: GithubInstallationBinding, action: "read")
@rateLimit(bucket: "github_read")
@requestBudget(maxBytes: 0)
@sdk(module: "github.installations", method: "get", paginated: false, retryable: true)
operation GetGithubInstallation {
    input: GetGithubInstallationInput
    output: GetGithubInstallationOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

structure GetGithubInstallationInput {
    @required
    @httpLabel
    installation_binding_id: GithubInstallationBindingId
}

structure GetGithubInstallationOutput {
    @required
    installation: GithubInstallationBindingRecord
}

@idempotent
@http(method: "POST", uri: "/api/v1/github/installations/{installation_binding_id}/sync", code: 200)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: GithubInstallationWritePermission, organization: {source: "token_org_id"})
@audit(event: GithubInstallationSyncAuditEvent, resource: GithubInstallationBinding, action: "sync")
@rateLimit(bucket: "github_mutation")
@requestBudget(maxBytes: 8192)
@sdk(module: "github.installations", method: "sync", paginated: false, retryable: false)
operation SyncGithubInstallation {
    input: SyncGithubInstallationInput
    output: SyncGithubInstallationOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure SyncGithubInstallationInput {
    @required
    @httpLabel
    installation_binding_id: GithubInstallationBindingId

    @required
    @idempotencyToken
    idempotency_key: IdempotencyKey
}

structure SyncGithubInstallationOutput {
    @required
    installation: GithubInstallationBindingRecord

    repositories: GithubRepositoryCandidates
}

@idempotent
@http(method: "DELETE", uri: "/api/v1/github/installations/{installation_binding_id}", code: 200)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: GithubInstallationWritePermission, organization: {source: "token_org_id"})
@audit(event: GithubInstallationDisconnectAuditEvent, resource: GithubInstallationBinding, action: "disable")
@rateLimit(bucket: "github_mutation")
@requestBudget(maxBytes: 1024)
@sdk(module: "github.installations", method: "disconnect", paginated: false, retryable: false)
operation DisconnectGithubInstallation {
    input: DisconnectGithubInstallationInput
    output: DisconnectGithubInstallationOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure DisconnectGithubInstallationInput {
    @required
    @httpLabel
    installation_binding_id: GithubInstallationBindingId

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotency_key: IdempotencyKey
}

structure DisconnectGithubInstallationOutput {
    @required
    installation: GithubInstallationBindingRecord
}

@readonly
@paginated(inputToken: "pageToken", outputToken: "nextPageToken", pageSize: "pageSize", items: "repositories")
@http(method: "GET", uri: "/api/v1/github/repositories", code: 200)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: GithubRepositoryReadPermission, organization: {source: "token_org_id"})
@audit(event: GithubRepositoryListAuditEvent, resource: GithubRepositoryBinding, action: "list")
@rateLimit(bucket: "github_read")
@requestBudget(maxBytes: 0)
@sdk(module: "github.repositories", method: "list", paginated: true, retryable: true)
operation ListGithubRepositories {
    input: ListGithubRepositoriesInput
    output: ListGithubRepositoriesOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

structure ListGithubRepositoriesInput with [PageRequest] {
    @httpQuery("installation_binding_id")
    installation_binding_id: GithubInstallationBindingId
}

structure ListGithubRepositoriesOutput with [PageResponse] {
    @required
    repositories: GithubRepositoryCandidates
}

@readonly
@http(method: "GET", uri: "/api/v1/github/repositories/{repository_binding_id}", code: 200)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: GithubRepositoryReadPermission, organization: {source: "token_org_id"})
@audit(event: GithubRepositoryReadAuditEvent, resource: GithubRepositoryBinding, action: "read")
@rateLimit(bucket: "github_read")
@requestBudget(maxBytes: 0)
@sdk(module: "github.repositories", method: "get", paginated: false, retryable: true)
operation GetGithubRepository {
    input: GetGithubRepositoryInput
    output: GetGithubRepositoryOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

structure GetGithubRepositoryInput {
    @required
    @httpLabel
    repository_binding_id: GithubRepositoryBindingId
}

structure GetGithubRepositoryOutput {
    @required
    repository: GithubRepositoryBindingRecord
}

@idempotent
@http(method: "POST", uri: "/api/v1/github/repositories:enable", code: 200)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: GithubRepositoryWritePermission, organization: {source: "token_org_id"})
@audit(event: GithubRepositoryEnableAuditEvent, resource: GithubRepositoryBinding, action: "connect")
@rateLimit(bucket: "github_mutation")
@requestBudget(maxBytes: 8192)
@sdk(module: "github.repositories", method: "enable", paginated: false, retryable: false)
operation EnableGithubRepository {
    input: EnableGithubRepositoryInput
    output: EnableGithubRepositoryOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ConflictError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure EnableGithubRepositoryInput {
    @required
    @idempotencyToken
    idempotency_key: IdempotencyKey

    @required
    installation_binding_id: GithubInstallationBindingId

    @required
    provider_repository_id: GithubProviderId
}

structure EnableGithubRepositoryOutput {
    @required
    repository: GithubRepositoryBindingRecord
}

@idempotent
@http(method: "POST", uri: "/api/v1/github/repositories/{repository_binding_id}/disable", code: 200)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: GithubRepositoryWritePermission, organization: {source: "token_org_id"})
@audit(event: GithubRepositoryDisableAuditEvent, resource: GithubRepositoryBinding, action: "disable")
@rateLimit(bucket: "github_mutation")
@requestBudget(maxBytes: 8192)
@sdk(module: "github.repositories", method: "disable", paginated: false, retryable: false)
operation DisableGithubRepository {
    input: DisableGithubRepositoryInput
    output: DisableGithubRepositoryOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure DisableGithubRepositoryInput {
    @required
    @httpLabel
    repository_binding_id: GithubRepositoryBindingId

    @required
    @idempotencyToken
    idempotency_key: IdempotencyKey
}

structure DisableGithubRepositoryOutput {
    @required
    repository: GithubRepositoryBindingRecord
}

@http(method: "POST", uri: "/api/v1/github/webhooks", code: 202)
@identity(mode: "provider_webhook", audience: "github-integration-service", principals: ["provider"])
@authz(permission: GithubProviderWebhookReceivePermission, organization: {source: "request_id"})
@audit(event: GithubWebhookReceiveAuditEvent, resource: GithubWebhookDelivery, action: "ingest")
@rateLimit(bucket: "provider_webhook")
@requestBudget(maxBytes: 1048576)
@sdk(module: "github.webhooks", method: "receive", paginated: false, retryable: false)
operation ReceiveGithubWebhook {
    input: ReceiveGithubWebhookInput
    output: ReceiveGithubWebhookOutput
    errors: [
        ProviderWebhookInvalidRequestError,
        ProviderWebhookSignatureInvalidError,
        ProviderWebhookDeliveryReplayConflictError,
        RateLimitedError,
        ProviderWebhookInboxUnavailableError,
        ServiceUnavailableError
    ]
}

structure ReceiveGithubWebhookInput {
    @required
    @httpHeader("X-GitHub-Event")
    event: GithubWebhookEvent

    @required
    @httpHeader("X-GitHub-Delivery")
    delivery_id: GithubDeliveryId

    @required
    @httpHeader("X-Hub-Signature-256")
    signature: GithubWebhookSignature

    @required
    @httpPayload
    body: GithubWebhookPayload
}

structure ReceiveGithubWebhookOutput {
    @required
    accepted: GithubWebhookAccepted
}
