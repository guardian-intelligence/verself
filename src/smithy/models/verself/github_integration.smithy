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
use smithy.api#trait
use verself.common.v1#ConflictError
use verself.common.v1#DateTime
use verself.common.v1#IdempotencyKey
use verself.common.v1#IdempotencyPayloadMismatchError
use verself.common.v1#PageRequest
use verself.common.v1#PageResponse
use verself.common.v1#PermissionDeniedError
use verself.common.v1#ProblemDetail
use verself.common.v1#ProblemDocsURL
use verself.common.v1#ProblemOccurrences
use verself.common.v1#ProblemType
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
        GithubProviderSurfaceCommand,
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
        GithubProviderSurfaceCommand,
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
resource GithubProviderSurfaceCommand {}
resource GithubJobAssignment {}
resource GithubTerminalJobEvidence {}
resource GithubGoldenSnapshotBarrier {}

/// Stable metadata for a github-integration-service lifecycle problem code.
@trait(selector: "enum > member")
structure githubIntegrationProblem {
    @required
    type: ProblemType

    @required
    title: String

    @required
    status: Integer

    @required
    phase: GithubIntegrationProblemPhase

    @required
    retryable: Boolean

    @required
    detail: ProblemDetail

    @required
    docs_url: ProblemDocsURL
}

enum GithubIntegrationProblemPhase {
    METHOD_VALIDATION = "method_validation"
    HEADER_VALIDATION = "header_validation"
    BODY_READ = "body_read"
    SIGNATURE_VERIFICATION = "signature_verification"
    PAYLOAD_PARSE = "payload_parse"
    INBOX_PERSIST = "inbox_persist"
    DELIVERY_DISPATCH = "delivery_dispatch"
    DELIVERY_PROCESSING = "delivery_processing"
    REPOSITORY_BINDING = "repository_binding"
    PROVIDER_REFRESH = "provider_refresh"
    PROVIDER_DEMAND = "provider_demand"
    CACHE_MANIFEST = "cache_manifest"
    JOB_SHAPE = "job_shape"
    RUNNER_CLASS_LOCK = "runner_class_lock"
    RUNNER_CLASS_CAPACITY = "runner_class_capacity"
    RUNNER_NAME = "runner_name"
    JIT_CONFIG = "jit_config"
    RUNNER_INSTANCE = "runner_instance"
    SANDBOX_OBSERVE_WORKFLOW = "sandbox_observe_workflow"
    SANDBOX_OUTBOX = "sandbox_outbox"
    SANDBOX_DISPATCH = "sandbox_dispatch"
    SANDBOX_SUBMIT = "sandbox_submit"
    SANDBOX_RECONCILE = "sandbox_reconcile"
    GUEST_TIME_SYNC = "guest_time_sync"
    ASSIGNMENT_WAIT = "assignment_wait"
    ASSIGNMENT = "assignment"
    PROVIDER_SURFACE = "provider_surface"
    RUNNER_CAPACITY = "runner_capacity"
}

enum GithubIntegrationProblemCode {
    @githubIntegrationProblem(type: "urn:verself:problem:provider_webhook:invalid_request", title: "Method not allowed", status: 405, phase: "method_validation", retryable: false, detail: "GitHub webhooks must use POST.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#provider-webhook-method-not-allowed")
    PROVIDER_WEBHOOK_METHOD_NOT_ALLOWED = "provider_webhook.method_not_allowed"

    @githubIntegrationProblem(type: "urn:verself:problem:provider_webhook:invalid_request", title: "Invalid webhook header", status: 400, phase: "header_validation", retryable: false, detail: "A required GitHub webhook header is missing, duplicated, or malformed.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#provider-webhook-header-invalid")
    PROVIDER_WEBHOOK_HEADER_INVALID = "provider_webhook.header_invalid"

    @githubIntegrationProblem(type: "urn:verself:problem:provider_webhook:invalid_request", title: "Invalid webhook body", status: 400, phase: "body_read", retryable: false, detail: "The GitHub webhook body could not be read.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#provider-webhook-body-invalid")
    PROVIDER_WEBHOOK_BODY_INVALID = "provider_webhook.body_invalid"

    @githubIntegrationProblem(type: "urn:verself:problem:provider_webhook:signature_invalid", title: "Invalid webhook signature", status: 401, phase: "signature_verification", retryable: false, detail: "The GitHub webhook signature did not validate against the configured secret.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#provider-webhook-signature-invalid")
    PROVIDER_WEBHOOK_SIGNATURE_INVALID = "provider_webhook.signature_invalid"

    @githubIntegrationProblem(type: "urn:verself:problem:provider_webhook:invalid_request", title: "Invalid webhook payload", status: 400, phase: "payload_parse", retryable: false, detail: "The GitHub webhook payload could not be parsed or is missing required identity.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#provider-webhook-payload-invalid")
    PROVIDER_WEBHOOK_PAYLOAD_INVALID = "provider_webhook.payload_invalid"

    @githubIntegrationProblem(type: "urn:verself:problem:provider_webhook:delivery_replay_conflict", title: "Webhook delivery replay conflict", status: 409, phase: "inbox_persist", retryable: false, detail: "GitHub reused a delivery id with a different payload hash.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#provider-webhook-delivery-replay-conflict")
    PROVIDER_WEBHOOK_DELIVERY_REPLAY_CONFLICT = "provider_webhook.delivery_replay_conflict"

    @githubIntegrationProblem(type: "urn:verself:problem:provider_webhook:inbox_unavailable", title: "Webhook inbox unavailable", status: 503, phase: "inbox_persist", retryable: true, detail: "Verself could not durably record the GitHub webhook delivery.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#provider-webhook-inbox-unavailable")
    PROVIDER_WEBHOOK_INBOX_UNAVAILABLE = "provider_webhook.inbox_unavailable"

    @githubIntegrationProblem(type: "urn:verself:problem:provider_webhook:unsupported_event", title: "Unsupported webhook event", status: 422, phase: "delivery_dispatch", retryable: false, detail: "Verself does not process this GitHub webhook event or action.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#provider-webhook-unsupported-event")
    PROVIDER_WEBHOOK_UNSUPPORTED_EVENT = "provider_webhook.unsupported_event"

    @githubIntegrationProblem(type: "urn:verself:problem:provider_webhook:processing_failed", title: "Webhook delivery processing failed", status: 503, phase: "delivery_processing", retryable: true, detail: "Verself could not process a durable GitHub webhook delivery.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#provider-webhook-processing-failed")
    PROVIDER_WEBHOOK_PROCESSING_FAILED = "provider_webhook.processing_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:provider_webhook:processing_stale", title: "Webhook delivery processing was abandoned", status: 503, phase: "delivery_processing", retryable: true, detail: "A worker left this GitHub webhook delivery in processing past its deadline.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#provider-webhook-processing-stale")
    PROVIDER_WEBHOOK_PROCESSING_STALE = "provider_webhook.processing_stale"

    @githubIntegrationProblem(type: "urn:verself:problem:provider_webhook:processing_attempts_exhausted", title: "Webhook delivery retry budget exhausted", status: 503, phase: "delivery_processing", retryable: false, detail: "The GitHub webhook delivery exceeded its processing retry budget.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#provider-webhook-processing-attempts-exhausted")
    PROVIDER_WEBHOOK_PROCESSING_ATTEMPTS_EXHAUSTED = "provider_webhook.processing_attempts_exhausted"

    @githubIntegrationProblem(type: "urn:verself:problem:github:repository_not_enabled", title: "GitHub repository is not enabled", status: 409, phase: "repository_binding", retryable: false, detail: "This repository is not enabled for Verself CI in the owning organization.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-repository-not-enabled")
    GITHUB_REPOSITORY_NOT_ENABLED = "github.repository_not_enabled"

    @githubIntegrationProblem(type: "urn:verself:problem:github:sandbox_dispatch_failed", title: "Sandbox dispatch failed", status: 503, phase: "sandbox_dispatch", retryable: true, detail: "Verself could not dispatch the GitHub job to sandbox-rental-service.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-sandbox-dispatch-failed")
    GITHUB_SANDBOX_DISPATCH_FAILED = "github.sandbox_dispatch_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:capacity_failed", title: "GitHub runner capacity failed", status: 0, phase: "runner_capacity", retryable: false, detail: "Verself could not create runner capacity for the queued GitHub job.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-capacity-failed")
    GITHUB_RUNNER_CAPACITY_FAILED = "github_runner.capacity_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:demand_record_failed", title: "GitHub provider demand record failed", status: 0, phase: "provider_demand", retryable: true, detail: "Verself could not persist GitHub job demand before allocating runner capacity.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-demand-record-failed")
    GITHUB_RUNNER_DEMAND_RECORD_FAILED = "github_runner.demand_record_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:workflow_run_fetch_failed", title: "GitHub workflow run refresh failed", status: 0, phase: "provider_refresh", retryable: true, detail: "Verself could not refresh the GitHub workflow run before allocating runner capacity.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-workflow-run-fetch-failed")
    GITHUB_RUNNER_WORKFLOW_RUN_FETCH_FAILED = "github_runner.workflow_run_fetch_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:workflow_run_persist_failed", title: "GitHub workflow run persist failed", status: 0, phase: "provider_refresh", retryable: true, detail: "Verself could not persist refreshed GitHub workflow run evidence.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-workflow-run-persist-failed")
    GITHUB_RUNNER_WORKFLOW_RUN_PERSIST_FAILED = "github_runner.workflow_run_persist_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:workflow_job_fetch_failed", title: "GitHub workflow job refresh failed", status: 0, phase: "provider_refresh", retryable: true, detail: "Verself could not refresh the GitHub workflow job before allocating runner capacity.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-workflow-job-fetch-failed")
    GITHUB_RUNNER_WORKFLOW_JOB_FETCH_FAILED = "github_runner.workflow_job_fetch_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:workflow_job_not_found", title: "GitHub workflow job was not found", status: 0, phase: "provider_refresh", retryable: false, detail: "GitHub did not return the queued workflow job for the expected run attempt.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-workflow-job-not-found")
    GITHUB_RUNNER_WORKFLOW_JOB_NOT_FOUND = "github_runner.workflow_job_not_found"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:workflow_job_persist_failed", title: "GitHub workflow job persist failed", status: 0, phase: "provider_refresh", retryable: true, detail: "Verself could not persist refreshed GitHub workflow job evidence.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-workflow-job-persist-failed")
    GITHUB_RUNNER_WORKFLOW_JOB_PERSIST_FAILED = "github_runner.workflow_job_persist_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:cache_manifest_fetch_failed", title: "GitHub cache manifest fetch failed", status: 0, phase: "cache_manifest", retryable: true, detail: "Verself could not fetch the repository cache manifest for this workflow.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-cache-manifest-fetch-failed")
    GITHUB_RUNNER_CACHE_MANIFEST_FETCH_FAILED = "github_runner.cache_manifest_fetch_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:job_shape_build_failed", title: "GitHub job shape build failed", status: 0, phase: "job_shape", retryable: false, detail: "Verself could not derive a stable job shape for this GitHub job.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-job-shape-build-failed")
    GITHUB_RUNNER_JOB_SHAPE_BUILD_FAILED = "github_runner.job_shape_build_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:job_shape_persist_failed", title: "GitHub job shape persist failed", status: 0, phase: "job_shape", retryable: true, detail: "Verself could not persist the derived GitHub job shape.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-job-shape-persist-failed")
    GITHUB_RUNNER_JOB_SHAPE_PERSIST_FAILED = "github_runner.job_shape_persist_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:demand_update_failed", title: "GitHub provider demand update failed", status: 0, phase: "provider_demand", retryable: true, detail: "Verself could not update GitHub provider demand state.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-demand-update-failed")
    GITHUB_RUNNER_DEMAND_UPDATE_FAILED = "github_runner.demand_update_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:runner_class_lock_failed", title: "GitHub runner class lock failed", status: 0, phase: "runner_class_lock", retryable: true, detail: "Verself could not acquire the repository runner-class scheduling lock.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-runner-class-lock-failed")
    GITHUB_RUNNER_RUNNER_CLASS_LOCK_FAILED = "github_runner.runner_class_lock_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:runner_class_capacity_count_failed", title: "GitHub runner class capacity count failed", status: 0, phase: "runner_class_capacity", retryable: true, detail: "Verself could not count active runner capacity for this repository runner class.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-runner-class-capacity-count-failed")
    GITHUB_RUNNER_RUNNER_CLASS_CAPACITY_COUNT_FAILED = "github_runner.runner_class_capacity_count_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:demand_claim_failed", title: "GitHub provider demand claim failed", status: 0, phase: "provider_demand", retryable: true, detail: "Verself could not claim the GitHub job demand for runner capacity allocation.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-demand-claim-failed")
    GITHUB_RUNNER_DEMAND_CLAIM_FAILED = "github_runner.demand_claim_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:runner_name_failed", title: "GitHub runner name generation failed", status: 0, phase: "runner_name", retryable: false, detail: "Verself could not generate a valid GitHub runner name.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-runner-name-failed")
    GITHUB_RUNNER_RUNNER_NAME_FAILED = "github_runner.runner_name_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:jit_config_failed", title: "GitHub runner JIT config failed", status: 0, phase: "jit_config", retryable: false, detail: "Verself could not create a GitHub JIT runner configuration.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-jit-config-failed")
    GITHUB_RUNNER_JIT_CONFIG_FAILED = "github_runner.jit_config_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:jit_config_invalid", title: "GitHub runner JIT config was incomplete", status: 0, phase: "jit_config", retryable: false, detail: "GitHub returned an incomplete JIT runner configuration.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-jit-config-invalid")
    GITHUB_RUNNER_JIT_CONFIG_INVALID = "github_runner.jit_config_invalid"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:instance_persist_failed", title: "GitHub runner instance persist failed", status: 0, phase: "runner_instance", retryable: false, detail: "Verself could not persist the GitHub runner instance.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-instance-persist-failed")
    GITHUB_RUNNER_INSTANCE_PERSIST_FAILED = "github_runner.instance_persist_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:sandbox_workflow_observe_failed", title: "Sandbox workflow observation failed", status: 0, phase: "sandbox_observe_workflow", retryable: true, detail: "Verself could not send workflow-run evidence to sandbox-rental-service.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-sandbox-workflow-observe-failed")
    GITHUB_RUNNER_SANDBOX_WORKFLOW_OBSERVE_FAILED = "github_runner.sandbox_workflow_observe_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:sandbox_outbox_failed", title: "Sandbox runner submit outbox failed", status: 0, phase: "sandbox_outbox", retryable: false, detail: "Verself could not record the sandbox submit outbox command.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-sandbox-outbox-failed")
    GITHUB_RUNNER_SANDBOX_OUTBOX_FAILED = "github_runner.sandbox_outbox_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:sandbox_submit_failed", title: "Sandbox runner submit failed", status: 0, phase: "sandbox_submit", retryable: false, detail: "Verself could not submit the GitHub runner job to sandbox-rental-service.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-sandbox-submit-failed")
    GITHUB_RUNNER_SANDBOX_SUBMIT_FAILED = "github_runner.sandbox_submit_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:sandbox_rejected", title: "Sandbox runner submit was rejected", status: 0, phase: "sandbox_submit", retryable: false, detail: "sandbox-rental-service rejected the GitHub runner job submission.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-sandbox-rejected")
    GITHUB_RUNNER_SANDBOX_REJECTED = "github_runner.sandbox_rejected"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:sandbox_response_invalid", title: "Sandbox runner submit response was invalid", status: 0, phase: "sandbox_submit", retryable: false, detail: "sandbox-rental-service returned an invalid runner submission response.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-sandbox-response-invalid")
    GITHUB_RUNNER_SANDBOX_RESPONSE_INVALID = "github_runner.sandbox_response_invalid"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:submission_persist_failed", title: "Sandbox runner submission persist failed", status: 0, phase: "sandbox_submit", retryable: false, detail: "Verself could not persist sandbox runner submission evidence.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-submission-persist-failed")
    GITHUB_RUNNER_SUBMISSION_PERSIST_FAILED = "github_runner.submission_persist_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:sandbox_outbox_complete_failed", title: "Sandbox runner submit outbox completion failed", status: 0, phase: "sandbox_outbox", retryable: false, detail: "Verself could not mark the sandbox submit outbox command complete.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-sandbox-outbox-complete-failed")
    GITHUB_RUNNER_SANDBOX_OUTBOX_COMPLETE_FAILED = "github_runner.sandbox_outbox_complete_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:submission_commit_failed", title: "Sandbox runner submission commit failed", status: 0, phase: "sandbox_submit", retryable: true, detail: "Verself could not commit sandbox runner submission state.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-submission-commit-failed")
    GITHUB_RUNNER_SUBMISSION_COMMIT_FAILED = "github_runner.submission_commit_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:sandbox_allocation_terminal", title: "GitHub runner capacity failed before assignment", status: 0, phase: "sandbox_reconcile", retryable: false, detail: "The sandbox allocation became terminal before GitHub assigned the runner.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-sandbox-allocation-terminal")
    GITHUB_RUNNER_SANDBOX_ALLOCATION_TERMINAL = "github_runner.sandbox_allocation_terminal"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:guest_time_sync_precision_gate_failed", title: "Guest time sync precision gate failed", status: 0, phase: "guest_time_sync", retryable: false, detail: "Verself could not prove the restored runner VM clock was synchronized closely enough to the host PTP source before starting the GitHub job.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-guest-time-sync-precision-gate-failed")
    GITHUB_RUNNER_GUEST_TIME_SYNC_PRECISION_GATE_FAILED = "github_runner.guest_time_sync_precision_gate_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:sandbox_allocation_not_found", title: "GitHub runner capacity failed before assignment", status: 0, phase: "sandbox_reconcile", retryable: false, detail: "sandbox-rental-service no longer has the expected runner allocation.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-sandbox-allocation-not-found")
    GITHUB_RUNNER_SANDBOX_ALLOCATION_NOT_FOUND = "github_runner.sandbox_allocation_not_found"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:sandbox_submit_not_observed", title: "GitHub runner capacity failed before assignment", status: 0, phase: "sandbox_submit", retryable: false, detail: "Verself did not observe sandbox submission before the runner assignment deadline.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-sandbox-submit-not-observed")
    GITHUB_RUNNER_SANDBOX_SUBMIT_NOT_OBSERVED = "github_runner.sandbox_submit_not_observed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:assignment_deadline_exceeded", title: "GitHub runner capacity failed before assignment", status: 0, phase: "assignment_wait", retryable: false, detail: "GitHub did not assign the runner before the assignment deadline.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-assignment-deadline-exceeded")
    GITHUB_RUNNER_ASSIGNMENT_DEADLINE_EXCEEDED = "github_runner.assignment_deadline_exceeded"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:runner_capacity_displaced", title: "Created GitHub runner capacity was displaced", status: 0, phase: "sandbox_submit", retryable: false, detail: "The runner capacity created by Verself was displaced by an existing sandbox runner.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-runner-capacity-displaced")
    GITHUB_RUNNER_RUNNER_CAPACITY_DISPLACED = "github_runner.runner_capacity_displaced"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:capacity_displaced", title: "GitHub runner capacity was assigned to a different job", status: 0, phase: "assignment", retryable: true, detail: "GitHub assigned runner capacity to a different compatible job.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-capacity-displaced")
    GITHUB_RUNNER_CAPACITY_DISPLACED = "github_runner.capacity_displaced"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:provider_surface_failed", title: "Failed to surface runner failure to GitHub", status: 0, phase: "provider_surface", retryable: true, detail: "Verself could not surface the runner failure back to GitHub.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-provider-surface-failed")
    GITHUB_RUNNER_PROVIDER_SURFACE_FAILED = "github_runner.provider_surface_failed"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:provider_surface_worker_lost", title: "GitHub provider surface worker lost command", status: 0, phase: "provider_surface", retryable: true, detail: "A provider-surface worker left a command running past its deadline.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-provider-surface-worker-lost")
    GITHUB_RUNNER_PROVIDER_SURFACE_WORKER_LOST = "github_runner.provider_surface_worker_lost"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:provider_surface_attempts_exhausted", title: "GitHub provider surface retry budget exhausted", status: 0, phase: "provider_surface", retryable: false, detail: "The provider-surface command exceeded its retry budget.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-provider-surface-attempts-exhausted")
    GITHUB_RUNNER_PROVIDER_SURFACE_ATTEMPTS_EXHAUSTED = "github_runner.provider_surface_attempts_exhausted"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:provider_surface_command_unsupported", title: "Unsupported GitHub provider surface command", status: 0, phase: "provider_surface", retryable: false, detail: "Verself encountered an unsupported provider-surface command kind.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-provider-surface-command-unsupported")
    GITHUB_RUNNER_PROVIDER_SURFACE_COMMAND_UNSUPPORTED = "github_runner.provider_surface_command_unsupported"

    @githubIntegrationProblem(type: "urn:verself:problem:github-runner:provider_surface_target_missing", title: "GitHub provider surface target missing", status: 0, phase: "provider_surface", retryable: false, detail: "The provider-surface command is missing the GitHub workflow-run identity needed to update GitHub.", docs_url: "https://verself.sh/docs/reference/github-integration/errors#github-runner-provider-surface-target-missing")
    GITHUB_RUNNER_PROVIDER_SURFACE_TARGET_MISSING = "github_runner.provider_surface_target_missing"
}

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

enum GithubProviderSurfaceCommandKind {
    CREATE_FAILURE_CHECK_RUN = "create_failure_check_run"
}

enum GithubProviderSurfaceCommandState {
    PENDING = "pending"
    RUNNING = "running"
    RETRYABLE = "retryable"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
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
    problems: ProblemOccurrences

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

    problems: ProblemOccurrences

    @required
    state: GithubRunnerInstanceState
}

structure GithubProviderSurfaceCommandRecord {
    @required
    surface_id: String

    @required
    command_key: String

    @required
    command_kind: GithubProviderSurfaceCommandKind

    @required
    state: GithubProviderSurfaceCommandState

    org_id: OrgId
    installation_binding_id: GithubInstallationBindingId
    repository_binding_id: GithubRepositoryBindingId
    provider_installation_id: SafeNonNegativeLong
    provider_repository_id: ProviderRepositoryId
    repository_full_name: RepositoryFullName
    provider_run_id: SafeNonNegativeLong
    provider_run_attempt: SafeNonNegativeLong
    provider_job_id: SafeNonNegativeLong
    runner_id: SafeNonNegativeLong
    runner_name: RunnerName
    runner_class: RunnerClass
    problems: ProblemOccurrences
    attempt_count: SafeNonNegativeLong
    next_attempt_at: DateTime
    completed_at: DateTime
    failed_at: DateTime
    created_at: DateTime
    updated_at: DateTime
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
