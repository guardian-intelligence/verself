$version: "2"
namespace verself.deployment.v1

use aws.protocols#restJson1
use smithy.api#error
use smithy.api#http
use smithy.api#httpError
use smithy.api#httpLabel
use smithy.api#length
use smithy.api#pattern
use smithy.api#range
use smithy.api#readonly
use smithy.api#required
use verself.common.v1#DateTime
use verself.common.v1#PermissionDeniedError
use verself.common.v1#UnauthenticatedError
use verself.common.v1#ValidationFailedError
use verself.common.v1#ProblemDetails
use verself.common.v1#audit
use verself.common.v1#auditEvent
use verself.common.v1#authz
use verself.common.v1#identity
use verself.common.v1#permission
use verself.common.v1#problem
use verself.common.v1#protoField
use verself.common.v1#rateLimit
use verself.common.v1#requestBudget
use verself.common.v1#sdk
use verself.common.v1#serviceRuntime

@serviceRuntime(serviceName: "deployment-service", publicAudience: "deployment-service", internalAudience: "deployment-service")
@restJson1
service Deployment {
    version: "2026-06-01"
    operations: [
        CheckDeploymentBootstrap,
        SubmitDeployment,
        GetDeployment,
        ListDeploymentEvents
    ]
    resources: [DeploymentRequest]
}

@length(min: 1, max: 64)
@pattern("^[a-z][a-z0-9_-]*$")
string SiteName

@length(min: 40, max: 40)
@pattern("^[0-9a-fA-F]{40}$")
string GitSha

@length(min: 36, max: 36)
@pattern("^dep_[0-9a-f]{32}$")
string DeploymentID

@length(min: 1, max: 128)
@pattern("^deploy_[0-9a-f]{32}$")
string DeployRunKey

@length(min: 0, max: 4096)
string DeploymentDetail

@length(min: 0, max: 255)
string DeploymentTraceparent

@length(min: 0, max: 256)
string DeploymentSource

@length(min: 0, max: 512)
string RepositoryName

@length(min: 0, max: 512)
string GitRef

@length(min: 0, max: 512)
string WorkflowName

@length(min: 0, max: 1024)
string WorkflowRef

@length(min: 0, max: 128)
string ProviderRunID

@range(min: 0, max: 2147483647)
integer ProviderRunAttempt

@range(min: 0, max: 2147483647)
integer NomadJobCount

@range(min: 0, max: 9223372036854775807)
long DeploymentEventID

enum DeploymentState {
    QUEUED = "queued"
    RUNNING = "running"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
}

@permission(name: "deployment:request")
string DeploymentRequestPermission

@permission(name: "deployment:read")
string DeploymentReadPermission

@permission(name: "deployment:bootstrap:read")
string DeploymentBootstrapReadPermission

@auditEvent(name: "deployment.request.submit")
string DeploymentRequestSubmitAuditEvent

@auditEvent(name: "deployment.request.read")
string DeploymentRequestReadAuditEvent

@auditEvent(name: "deployment.events.list")
string DeploymentEventsListAuditEvent

@auditEvent(name: "deployment.bootstrap.check")
string DeploymentBootstrapCheckAuditEvent

resource DeploymentRequest {}

structure DeploymentRecord {
    @required
    @protoField(number: 1)
    deployment_id: DeploymentID

    @required
    @protoField(number: 2)
    site: SiteName

    @required
    @protoField(number: 3)
    sha: GitSha

    @required
    @protoField(number: 4)
    deploy_run_key: DeployRunKey

    @required
    @protoField(number: 5)
    source_kind: DeploymentSource

    @required
    @protoField(number: 6)
    source_subject: DeploymentSource

    @required
    @protoField(number: 7)
    repository: RepositoryName

    @required
    @protoField(number: 8)
    ref: GitRef

    @required
    @protoField(number: 9)
    workflow: WorkflowName

    @required
    @protoField(number: 10)
    job_workflow_ref: WorkflowRef

    @required
    @protoField(number: 11)
    provider_run_id: ProviderRunID

    @required
    @protoField(number: 12)
    provider_run_attempt: ProviderRunAttempt

    @required
    @protoField(number: 13)
    state: DeploymentState

    @required
    @protoField(number: 14)
    error_code: String

    @required
    @protoField(number: 15)
    error_detail: DeploymentDetail

    @required
    @protoField(number: 16)
    control_plane_bundle_sha256: String

    @required
    @protoField(number: 17)
    nomad_submitted_jobs: NomadJobCount

    @required
    @protoField(number: 18)
    nomad_dispatched_jobs: NomadJobCount

    @required
    @protoField(number: 19)
    created_at: DateTime

    @required
    @protoField(number: 20)
    updated_at: DateTime

    @required
    @protoField(number: 21)
    started_at: DateTime

    @required
    @protoField(number: 22)
    finished_at: DateTime
}

structure DeploymentEvent {
    @required
    @protoField(number: 1)
    event_id: DeploymentEventID

    @required
    @protoField(number: 2)
    deployment_id: DeploymentID

    @required
    @protoField(number: 3)
    at: DateTime

    @required
    @protoField(number: 4)
    state: DeploymentState

    @required
    @protoField(number: 5)
    code: String

    @required
    @protoField(number: 6)
    detail: DeploymentDetail
}

list DeploymentEvents {
    member: DeploymentEvent
}

structure DependencyCheck {
    @required
    @protoField(number: 1)
    stage: String

    @required
    @protoField(number: 2)
    name: String

    @required
    @protoField(number: 3)
    ok: Boolean

    @required
    @protoField(number: 4)
    code: String

    @required
    @protoField(number: 5)
    detail: DeploymentDetail
}

list DependencyChecks {
    member: DependencyCheck
}

@readonly
@http(method: "GET", uri: "/api/v1/deployments/bootstrap:check")
@identity(mode: "bearer", audience: "deployment-service", principals: ["cli", "github_action"])
@authz(permission: DeploymentBootstrapReadPermission, organization: {source: "request_subject"})
@audit(event: DeploymentBootstrapCheckAuditEvent, resource: DeploymentRequest, action: "test")
@rateLimit(bucket: "deployment_read")
@requestBudget(maxBytes: 0)
@sdk(module: "deployments.bootstrap", method: "check", paginated: false, retryable: true)
operation CheckDeploymentBootstrap {
    input: CheckDeploymentBootstrapInput
    output: CheckDeploymentBootstrapOutput
    errors: [
        UnauthenticatedError,
        PermissionDeniedError,
        DeploymentUnauthorizedError,
        DeploymentServiceUnavailableError
    ]
}

structure CheckDeploymentBootstrapInput {}

structure CheckDeploymentBootstrapOutput {
    @required
    @protoField(number: 1)
    checks: DependencyChecks

    @required
    @protoField(number: 2)
    traceparent: DeploymentTraceparent
}

@http(method: "POST", uri: "/api/v1/deployments", code: 202)
@identity(mode: "bearer", audience: "deployment-service", principals: ["cli", "github_action"])
@authz(permission: DeploymentRequestPermission, organization: {source: "request_subject"})
@audit(event: DeploymentRequestSubmitAuditEvent, resource: DeploymentRequest, action: "create")
@rateLimit(bucket: "deployment_write")
@requestBudget(maxBytes: 65536)
@sdk(module: "deployments", method: "submit", paginated: false, retryable: false)
operation SubmitDeployment {
    input: SubmitDeploymentInput
    output: SubmitDeploymentOutput
    errors: [
        ValidationFailedError,
        UnauthenticatedError,
        PermissionDeniedError,
        DeploymentUnauthorizedError,
        RequestInvalidJsonError,
        RequestReadFailedError,
        RequestTooLargeError,
        DeploymentAdmissionBusyError,
        DeploymentBootstrapS0SiteError,
        DeploymentBootstrapS1HostAllocatedError,
        DeploymentBootstrapS2RecoverySshReadyError,
        DeploymentBootstrapS3BazeliskError,
        DeploymentBootstrapS3GitError,
        DeploymentBootstrapS4OpenBaoRuntimeSecretDeliveryError,
        DeploymentBootstrapS5SubstrateControlPlaneAppliedError,
        DeploymentBootstrapS6NomadError,
        DeploymentBootstrapS7PostgresError,
        DeploymentBootstrapS7RepoRootError,
        DeploymentBootstrapS7R2ControlPlaneError,
        DeploymentBusyError,
        DeploymentDuplicateError,
        DeploymentSiteRequiredError,
        DeploymentSiteMismatchError,
        DeploymentShaInvalidError,
        DeploymentSourceRequiredError,
        DeploymentSourceShaMismatchError,
        DeploymentSourceRepositoryMissingError,
        DeploymentSourceRefMissingError,
        DeploymentSourceShaMissingError,
        DeploymentSourceRunIDMissingError,
        DeploymentSourceRunAttemptMissingError,
        DeploymentNotReadyError,
        DeploymentServiceUnavailableError
    ]
}

structure SubmitDeploymentInput {
    @required
    @protoField(number: 1)
    site: SiteName

    @required
    @protoField(number: 2)
    sha: GitSha

    @protoField(number: 3)
    repository: RepositoryName
}

structure SubmitDeploymentOutput {
    @required
    @protoField(number: 1)
    deployment: DeploymentRecord

    @required
    @protoField(number: 2)
    traceparent: DeploymentTraceparent
}

@readonly
@http(method: "GET", uri: "/api/v1/deployments/{deployment_id}")
@identity(mode: "bearer", audience: "deployment-service", principals: ["cli", "github_action"])
@authz(permission: DeploymentReadPermission, organization: {source: "request_subject"})
@audit(event: DeploymentRequestReadAuditEvent, resource: DeploymentRequest, action: "read")
@rateLimit(bucket: "deployment_read")
@requestBudget(maxBytes: 0)
@sdk(module: "deployments", method: "get", paginated: false, retryable: true)
operation GetDeployment {
    input: GetDeploymentInput
    output: GetDeploymentOutput
    errors: [
        UnauthenticatedError,
        PermissionDeniedError,
        DeploymentUnauthorizedError,
        DeploymentNotFoundError,
        DeploymentServiceUnavailableError
    ]
}

structure GetDeploymentInput {
    @required
    @httpLabel
    @protoField(number: 1)
    deployment_id: DeploymentID
}

structure GetDeploymentOutput {
    @required
    @protoField(number: 1)
    deployment: DeploymentRecord

    @required
    @protoField(number: 2)
    traceparent: DeploymentTraceparent
}

@readonly
@http(method: "GET", uri: "/api/v1/deployments/{deployment_id}/events")
@identity(mode: "bearer", audience: "deployment-service", principals: ["cli", "github_action"])
@authz(permission: DeploymentReadPermission, organization: {source: "request_subject"})
@audit(event: DeploymentEventsListAuditEvent, resource: DeploymentRequest, action: "read")
@rateLimit(bucket: "deployment_read")
@requestBudget(maxBytes: 0)
@sdk(module: "deployments.events", method: "list", paginated: false, retryable: true)
operation ListDeploymentEvents {
    input: ListDeploymentEventsInput
    output: ListDeploymentEventsOutput
    errors: [
        UnauthenticatedError,
        PermissionDeniedError,
        DeploymentUnauthorizedError,
        DeploymentNotFoundError,
        DeploymentServiceUnavailableError
    ]
}

structure ListDeploymentEventsInput {
    @required
    @httpLabel
    @protoField(number: 1)
    deployment_id: DeploymentID
}

structure ListDeploymentEventsOutput {
    @required
    @protoField(number: 1)
    events: DeploymentEvents

    @required
    @protoField(number: 2)
    traceparent: DeploymentTraceparent
}

@error("client")
@httpError(400)
@problem(type: "urn:verself:problem:request:invalid_json", code: "request.invalid_json")
structure RequestInvalidJsonError with [ProblemDetails] {}

@error("client")
@httpError(400)
@problem(type: "urn:verself:problem:request:read_failed", code: "request.read_failed")
structure RequestReadFailedError with [ProblemDetails] {}

@error("client")
@httpError(413)
@problem(type: "urn:verself:problem:request:too_large", code: "request.too_large")
structure RequestTooLargeError with [ProblemDetails] {}

@error("client")
@httpError(401)
@problem(type: "urn:verself:problem:deployment:unauthorized", code: "deployment.unauthorized")
structure DeploymentUnauthorizedError with [ProblemDetails] {}

@error("client")
@httpError(404)
@problem(type: "urn:verself:problem:deployment:not_found", code: "deployment.not_found")
structure DeploymentNotFoundError with [ProblemDetails] {}

@error("client")
@httpError(409)
@problem(type: "urn:verself:problem:deployment:busy", code: "deployment.busy")
structure DeploymentBusyError with [ProblemDetails] {}

@error("client")
@httpError(409)
@problem(type: "urn:verself:problem:deployment:duplicate", code: "deployment.duplicate")
structure DeploymentDuplicateError with [ProblemDetails] {}

@error("client")
@httpError(400)
@problem(type: "urn:verself:problem:deployment:site_required", code: "deployment.site_required")
structure DeploymentSiteRequiredError with [ProblemDetails] {}

@error("client")
@httpError(400)
@problem(type: "urn:verself:problem:deployment:site_mismatch", code: "deployment.site_mismatch")
structure DeploymentSiteMismatchError with [ProblemDetails] {}

@error("client")
@httpError(400)
@problem(type: "urn:verself:problem:deployment:sha_invalid", code: "deployment.sha_invalid")
structure DeploymentShaInvalidError with [ProblemDetails] {}

@error("client")
@httpError(401)
@problem(type: "urn:verself:problem:deployment:source_required", code: "deployment.source_required")
structure DeploymentSourceRequiredError with [ProblemDetails] {}

@error("client")
@httpError(401)
@problem(type: "urn:verself:problem:deployment:source_sha_mismatch", code: "deployment.source_sha_mismatch")
structure DeploymentSourceShaMismatchError with [ProblemDetails] {}

@error("client")
@httpError(401)
@problem(type: "urn:verself:problem:deployment:source_repository_missing", code: "deployment.source_repository_missing")
structure DeploymentSourceRepositoryMissingError with [ProblemDetails] {}

@error("client")
@httpError(401)
@problem(type: "urn:verself:problem:deployment:source_ref_missing", code: "deployment.source_ref_missing")
structure DeploymentSourceRefMissingError with [ProblemDetails] {}

@error("client")
@httpError(401)
@problem(type: "urn:verself:problem:deployment:source_sha_missing", code: "deployment.source_sha_missing")
structure DeploymentSourceShaMissingError with [ProblemDetails] {}

@error("client")
@httpError(401)
@problem(type: "urn:verself:problem:deployment:source_run_id_missing", code: "deployment.source_run_id_missing")
structure DeploymentSourceRunIDMissingError with [ProblemDetails] {}

@error("client")
@httpError(401)
@problem(type: "urn:verself:problem:deployment:source_run_attempt_missing", code: "deployment.source_run_attempt_missing")
structure DeploymentSourceRunAttemptMissingError with [ProblemDetails] {}

@error("client")
@httpError(429)
@problem(type: "urn:verself:problem:deployment:admission_busy", code: "deployment.admission_busy")
structure DeploymentAdmissionBusyError with [ProblemDetails] {}

@error("server")
@httpError(503)
@problem(type: "urn:verself:problem:deployment:bootstrap:s0:site", code: "deployment.bootstrap.s0.site")
structure DeploymentBootstrapS0SiteError with [ProblemDetails] {}

@error("server")
@httpError(503)
@problem(type: "urn:verself:problem:deployment:bootstrap:s1:host_allocated", code: "deployment.bootstrap.s1.host_allocated")
structure DeploymentBootstrapS1HostAllocatedError with [ProblemDetails] {}

@error("server")
@httpError(503)
@problem(type: "urn:verself:problem:deployment:bootstrap:s2:recovery_ssh_ready", code: "deployment.bootstrap.s2.recovery_ssh_ready")
structure DeploymentBootstrapS2RecoverySshReadyError with [ProblemDetails] {}

@error("server")
@httpError(503)
@problem(type: "urn:verself:problem:deployment:bootstrap:s3:bazelisk", code: "deployment.bootstrap.s3.bazelisk")
structure DeploymentBootstrapS3BazeliskError with [ProblemDetails] {}

@error("server")
@httpError(503)
@problem(type: "urn:verself:problem:deployment:bootstrap:s3:git", code: "deployment.bootstrap.s3.git")
structure DeploymentBootstrapS3GitError with [ProblemDetails] {}

@error("server")
@httpError(503)
@problem(type: "urn:verself:problem:deployment:bootstrap:s4:openbao_runtime_secret_delivery", code: "deployment.bootstrap.s4.openbao_runtime_secret_delivery")
structure DeploymentBootstrapS4OpenBaoRuntimeSecretDeliveryError with [ProblemDetails] {}

@error("server")
@httpError(503)
@problem(type: "urn:verself:problem:deployment:bootstrap:s5:substrate_control_plane_applied", code: "deployment.bootstrap.s5.substrate_control_plane_applied")
structure DeploymentBootstrapS5SubstrateControlPlaneAppliedError with [ProblemDetails] {}

@error("server")
@httpError(503)
@problem(type: "urn:verself:problem:deployment:bootstrap:s6:nomad", code: "deployment.bootstrap.s6.nomad")
structure DeploymentBootstrapS6NomadError with [ProblemDetails] {}

@error("server")
@httpError(503)
@problem(type: "urn:verself:problem:deployment:bootstrap:s7:postgres", code: "deployment.bootstrap.s7.postgres")
structure DeploymentBootstrapS7PostgresError with [ProblemDetails] {}

@error("server")
@httpError(503)
@problem(type: "urn:verself:problem:deployment:bootstrap:s7:repo_root", code: "deployment.bootstrap.s7.repo_root")
structure DeploymentBootstrapS7RepoRootError with [ProblemDetails] {}

@error("server")
@httpError(503)
@problem(type: "urn:verself:problem:deployment:bootstrap:s7:r2_control_plane", code: "deployment.bootstrap.s7.r2_control_plane")
structure DeploymentBootstrapS7R2ControlPlaneError with [ProblemDetails] {}

@error("server")
@httpError(503)
@problem(type: "urn:verself:problem:deployment:not_ready", code: "deployment.not_ready")
structure DeploymentNotReadyError with [ProblemDetails] {}

@error("server")
@httpError(503)
@problem(type: "urn:verself:problem:deployment:service_unavailable", code: "deployment.service_unavailable")
structure DeploymentServiceUnavailableError with [ProblemDetails] {}
