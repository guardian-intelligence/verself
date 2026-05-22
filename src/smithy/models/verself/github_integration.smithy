$version: "2"
namespace verself.githubintegration.v1

use aws.protocols#restJson1

use smithy.api#http
use smithy.api#httpHeader
use smithy.api#httpPayload
use smithy.api#length
use smithy.api#mediaType
use smithy.api#pattern
use smithy.api#range
use smithy.api#required
use smithy.api#sensitive
use verself.common.v1#DateTime
use verself.common.v1#RateLimitedError
use verself.common.v1#ServiceUnavailableError
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
        ReceiveGithubWebhook
    ]
    resources: [
        GithubWebhookDelivery,
        GithubWorkflowRun,
        GithubWorkflowJob,
        GithubRunnerRegistration,
        GithubTerminalJobEvidence
    ]
}

@serviceRuntime(serviceName: "github-integration-service", publicAudience: "verself-api", internalAudience: "github-integration-service")
@restJson1
service GithubIntegrationInternal {
    version: "2026-05-21"
    operations: []
    resources: [
        GithubWebhookDelivery,
        GithubWorkflowRun,
        GithubWorkflowJob,
        GithubRunnerRegistration,
        GithubTerminalJobEvidence
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

@length(min: 1, max: 128)
@pattern("^org_[0-9A-HJKMNP-TV-Z]{26}$")
string OrgId

@range(min: 0, max: 9007199254740991)
long SafeNonNegativeLong

@length(min: 1, max: 512)
string RunnerName

@length(min: 1, max: 255)
string RunnerClass

@length(min: 1, max: 512)
string ProviderRepositoryId

@length(min: 1, max: 1024)
string RepositoryFullName

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

@auditEvent(name: "github.webhook.receive")
string GithubWebhookReceiveAuditEvent

resource GithubWebhookDelivery {}
resource GithubWorkflowRun {}
resource GithubWorkflowJob {}
resource GithubRunnerRegistration {}
resource GithubTerminalJobEvidence {}

enum GithubWebhookDeliveryState {
    VERIFIED = "verified"
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
    failure_reason: String
    received_at: DateTime
    verified_at: DateTime
    processed_at: DateTime
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

structure GithubRunnerRegistrationRecord {
    @required
    provider_job_id: SafeNonNegativeLong

    @required
    runner_name: RunnerName

    @required
    runner_class: RunnerClass

    allocation_id: AllocationId
    execution_id: ExecutionId
    attempt_id: AttemptId
    state: String
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
    errors: [ValidationFailedError, RateLimitedError, ServiceUnavailableError]
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
