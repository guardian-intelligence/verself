$version: "2"
namespace verself.source.v1

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
use smithy.api#mediaType
use smithy.api#pattern
use smithy.api#range
use smithy.api#readonly
use smithy.api#required
use smithy.api#sensitive
use verself.common.v1#ConflictError
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
use verself.common.v1#protoField
use verself.common.v1#rateLimit
use verself.common.v1#requestBudget
use verself.common.v1#sdk
use verself.common.v1#serviceRuntime
use verself.common.v1#DateTime

@serviceRuntime(serviceName: "source-code-hosting-service", publicAudience: "source-code-hosting-service", internalAudience: "source-code-hosting-service")
@restJson1
service SourceHosting {
    version: "2026-05-13"
    operations: [
        CreateSourceRepository,
        CreateSourceGitCredential,
        ListSourceRepositories,
        GetSourceRepository,
        ListSourceRefs,
        GetSourceTree,
        GetSourceBlob,
        CreateSourceCheckoutGrant,
        CreateSourceWorkflowRun,
        ListSourceWorkflowRuns,
        GetSourceWorkflowRun
    ]
    resources: [
        SourceRepository,
        SourceGitCredential,
        SourceRef,
        SourceTree,
        SourceBlob,
        SourceCheckoutGrant,
        SourceWorkflowRun
    ]
}

@serviceRuntime(serviceName: "source-code-hosting-service", publicAudience: "source-code-hosting-service", internalAudience: "source-code-hosting-service")
@restJson1
service SourceHostingInternal {
    version: "2026-05-13"
    operations: [
        InternalCreateSourceWorkflowRun,
        DownloadSourceCheckoutArchive
    ]
    resources: [
        SourceWorkflowRun,
        SourceCheckoutArchive
    ]
}

@pattern("^[0-9a-fA-F-]{36}$")
string RepositoryId

@pattern("^[0-9a-fA-F-]{36}$")
string ProjectId

@pattern("^[0-9a-fA-F-]{36}$")
string GitCredentialId

@pattern("^[0-9a-fA-F-]{36}$")
string CheckoutGrantId

@pattern("^[0-9a-fA-F-]{36}$")
string WorkflowRunId

@length(min: 1, max: 128)
@pattern("^org_[0-9A-HJKMNP-TV-Z]{26}$")
string OrgId

@length(min: 1, max: 512)
string ActorId

@length(min: 1, max: 128)
string OrgSlug

@length(min: 1, max: 128)
string ProjectSlug

@length(min: 1, max: 255)
string RepositoryName

@length(min: 1, max: 1024)
string GitRef

@length(min: 1, max: 8192)
string RepositoryPath

@length(min: 1, max: 4096)
string WorkflowPath

@length(max: 1024)
string RepositoryDescription

@length(min: 1, max: 128)
string BranchName

@length(min: 1, max: 8192)
string GitURL

@length(min: 1, max: 128)
string GitScope

@length(min: 1, max: 128)
string GitCredentialLabel

@range(min: 60, max: 7776000)
long CredentialExpirySeconds

@length(min: 1, max: 128)
string GitUsername

@length(min: 1, max: 4096)
@sensitive
string GitToken

@length(min: 1, max: 4096)
@sensitive
string CheckoutGrantToken

@length(min: 1, max: 32)
string GitTokenPrefix

@length(min: 1, max: 128)
string SourceState

@length(min: 1, max: 128)
string SourceBackend

@length(min: 1, max: 128)
string RepositoryVisibility

@range(min: 0, max: 2147483647)
integer RepositoryVersion

@length(min: 1, max: 512)
string ProviderRepositoryId

@pattern("^[0-9a-fA-F]{40,64}$")
string GitObjectSha

@length(min: 1, max: 255)
string GitObjectName

@length(min: 1, max: 128)
string GitObjectType

@length(min: 1, max: 128)
string GitObjectEncoding

@range(min: 0, max: 9007199254740991)
long SafeByteCount

@length(min: 1, max: 8192)
string BlobDownloadURL

@length(min: 1, max: 128)
string WorkflowInputName

@length(max: 8192)
string WorkflowInputValue

@length(min: 1, max: 1024)
string BackendDispatchId

@length(max: 8192)
string FailureReason

@length(min: 1, max: 128)
string TraceId

@length(min: 1, max: 128)
string MediaType

@length(min: 1, max: 1024)
string ContentDisposition

@mediaType("application/gzip")
blob SourceArchive

list Repositories {
    member: RepositorySummary
}

list GitScopes {
    member: GitScope
}

list Refs {
    member: SourceRefView
}

list TreeEntries {
    member: TreeEntry
}

list WorkflowRuns {
    member: WorkflowRunSummary
}

@length(max: 64)
map WorkflowInputs {
    key: WorkflowInputName
    value: WorkflowInputValue
}

@permission(name: "source:repo:read")
string RepoReadPermission

@permission(name: "source:repo:write")
string RepoWritePermission

@permission(name: "source:checkout:write")
string CheckoutWritePermission

@permission(name: "source:git_credential:write")
string GitCredentialWritePermission

@permission(name: "source:workflow:read")
string WorkflowReadPermission

@permission(name: "source:workflow:write")
string WorkflowWritePermission

@auditEvent(name: "source.repo.create")
string RepoCreateAuditEvent

@auditEvent(name: "source.git_credential.create")
string GitCredentialCreateAuditEvent

@auditEvent(name: "source.repo.list")
string RepoListAuditEvent

@auditEvent(name: "source.repo.read")
string RepoReadAuditEvent

@auditEvent(name: "source.refs.list")
string RefsListAuditEvent

@auditEvent(name: "source.tree.get")
string TreeGetAuditEvent

@auditEvent(name: "source.blob.get")
string BlobGetAuditEvent

@auditEvent(name: "source.checkout_grant.create")
string CheckoutGrantCreateAuditEvent

@auditEvent(name: "source.workflow.dispatch")
string WorkflowDispatchAuditEvent

@auditEvent(name: "source.workflow_run.list")
string WorkflowRunListAuditEvent

@auditEvent(name: "source.workflow_run.read")
string WorkflowRunReadAuditEvent

@auditEvent(name: "source.workflow.dispatch.internal")
string InternalWorkflowDispatchAuditEvent

@auditEvent(name: "source.archive.stream")
string ArchiveStreamAuditEvent

resource SourceRepository {}
resource SourceGitCredential {}
resource SourceRef {}
resource SourceTree {}
resource SourceBlob {}
resource SourceCheckoutGrant {}
resource SourceWorkflowRun {}
resource SourceCheckoutArchive {}

structure RepositorySummary {
    @required
    @protoField(number: 1)
    repo_id: RepositoryId
    @required
    @protoField(number: 2)
    resourceName: ResourceName
    @required
    @protoField(number: 3)
    org_id: OrgId
    @required
    @protoField(number: 4)
    project_id: ProjectId
    @required
    @protoField(number: 5)
    projectResourceName: ResourceName
    @required
    @protoField(number: 6)
    org_slug: OrgSlug
    @required
    @protoField(number: 7)
    project_slug: ProjectSlug
    @required
    @protoField(number: 8)
    git_http_url: GitURL
    @required
    @protoField(number: 9)
    name: RepositoryName
    @required
    @protoField(number: 10)
    description: RepositoryDescription
    @required
    @protoField(number: 11)
    default_branch: BranchName
    @required
    @protoField(number: 12)
    backend: SourceBackend
    @required
    @protoField(number: 13)
    visibility: RepositoryVisibility
    @required
    @protoField(number: 14)
    state: SourceState
    @required
    @protoField(number: 15)
    version: RepositoryVersion
    @protoField(number: 16)
    last_pushed_at: DateTime
    @required
    @protoField(number: 17)
    created_at: DateTime
    @required
    @protoField(number: 18)
    updated_at: DateTime
}

structure GitCredentialSummary {
    @required
    @protoField(number: 1)
    credential_id: GitCredentialId
    @required
    @protoField(number: 2)
    resourceName: ResourceName
    @required
    @protoField(number: 3)
    org_id: OrgId
    @required
    @protoField(number: 4)
    username: GitUsername
    @required
    @protoField(number: 5)
    token: GitToken
    @required
    @protoField(number: 6)
    scopes: GitScopes
    @required
    @protoField(number: 7)
    token_prefix: GitTokenPrefix
    @required
    @protoField(number: 8)
    expires_at: DateTime
    @required
    @protoField(number: 9)
    created_at: DateTime
}

structure SourceRefView {
    @required
    @protoField(number: 1)
    name: GitRef
    @required
    @protoField(number: 2)
    commit: GitObjectSha
}

structure TreeEntry {
    @required
    @protoField(number: 1)
    path: RepositoryPath
    @required
    @protoField(number: 2)
    type: GitObjectType
    @required
    @protoField(number: 3)
    size: SafeByteCount
    @required
    @protoField(number: 4)
    sha: GitObjectSha
}

structure SourceBlobView {
    @required
    @protoField(number: 1)
    path: RepositoryPath
    @required
    @protoField(number: 2)
    name: GitObjectName
    @required
    @protoField(number: 3)
    encoding: GitObjectEncoding
    @required
    @protoField(number: 4)
    content: String
    @required
    @protoField(number: 5)
    size: SafeByteCount
    @required
    @protoField(number: 6)
    sha: GitObjectSha
    @protoField(number: 7)
    download_url: BlobDownloadURL
}

structure CheckoutGrantSummary {
    @required
    @protoField(number: 1)
    grant_id: CheckoutGrantId
    @required
    @protoField(number: 2)
    resourceName: ResourceName
    @required
    @protoField(number: 3)
    repo_id: RepositoryId
    @required
    @protoField(number: 4)
    ref: GitRef
    @required
    @protoField(number: 5)
    token: CheckoutGrantToken
    @required
    @protoField(number: 6)
    expires_at: DateTime
}

structure WorkflowRunSummary {
    @required
    @protoField(number: 1)
    workflow_run_id: WorkflowRunId
    @required
    @protoField(number: 2)
    resourceName: ResourceName
    @required
    @protoField(number: 3)
    org_id: OrgId
    @required
    @protoField(number: 4)
    project_id: ProjectId
    @required
    @protoField(number: 5)
    projectResourceName: ResourceName
    @required
    @protoField(number: 6)
    repo_id: RepositoryId
    @required
    @protoField(number: 7)
    repositoryResourceName: ResourceName
    @required
    @protoField(number: 8)
    actor_id: ActorId
    @required
    @protoField(number: 9)
    backend: SourceBackend
    @required
    @protoField(number: 10)
    workflow_path: WorkflowPath
    @required
    @protoField(number: 11)
    ref: GitRef
    @required
    @protoField(number: 12)
    inputs: WorkflowInputs
    @required
    @protoField(number: 13)
    state: SourceState
    @protoField(number: 14)
    backend_dispatch_id: BackendDispatchId
    @protoField(number: 15)
    failure_reason: FailureReason
    @protoField(number: 16)
    trace_id: TraceId
    @protoField(number: 17)
    dispatched_at: DateTime
    @required
    @protoField(number: 18)
    created_at: DateTime
    @required
    @protoField(number: 19)
    updated_at: DateTime
}

@idempotent
@http(method: "POST", uri: "/api/v1/repos", code: 201)
@identity(mode: "bearer", audience: "source-code-hosting-service", principals: ["browser", "cli"])
@authz(permission: RepoWritePermission, organization: {source: "token_org_id"})
@audit(event: RepoCreateAuditEvent, resource: SourceRepository, action: "create")
@rateLimit(bucket: "source_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "source.repositories", method: "create", paginated: false, retryable: false)
operation CreateSourceRepository {
    input: CreateSourceRepositoryInput
    output: RepositoryOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure CreateSourceRepositoryInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
    @required
    project_id: ProjectId
    description: RepositoryDescription
    default_branch: BranchName
}

@readonly
@http(method: "GET", uri: "/api/v1/repos")
@identity(mode: "bearer", audience: "source-code-hosting-service", principals: ["browser", "cli", "workload"])
@authz(permission: RepoReadPermission, organization: {source: "token_org_id"})
@audit(event: RepoListAuditEvent, resource: SourceRepository, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "source.repositories", method: "list", paginated: false, retryable: true)
operation ListSourceRepositories {
    input: ListSourceRepositoriesInput
    output: ListSourceRepositoriesOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

structure ListSourceRepositoriesInput {
    @httpQuery("project_id")
    project_id: ProjectId
}

structure ListSourceRepositoriesOutput {
    @required
    repositories: Repositories
}

@readonly
@http(method: "GET", uri: "/api/v1/repos/{repo_id}")
@identity(mode: "bearer", audience: "source-code-hosting-service", principals: ["browser", "cli", "workload"])
@authz(permission: RepoReadPermission, organization: {source: "token_org_id"})
@audit(event: RepoReadAuditEvent, resource: SourceRepository, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "source.repositories", method: "get", paginated: false, retryable: true)
operation GetSourceRepository {
    input: RepositoryPathInput
    output: RepositoryOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

structure RepositoryOutput {
    @required
    @httpPayload
    @nestedProperties
    repository: RepositorySummary
}

structure RepositoryPathInput {
    @required
    @httpLabel
    repo_id: RepositoryId
}

@idempotent
@http(method: "POST", uri: "/api/v1/git-credentials", code: 201)
@identity(mode: "bearer", audience: "source-code-hosting-service", principals: ["browser", "cli"])
@authz(permission: GitCredentialWritePermission, organization: {source: "token_org_id"})
@audit(event: GitCredentialCreateAuditEvent, resource: SourceGitCredential, action: "create")
@rateLimit(bucket: "source_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "source.gitCredentials", method: "create", paginated: false, retryable: false)
operation CreateSourceGitCredential {
    input: CreateSourceGitCredentialInput
    output: GitCredentialOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure CreateSourceGitCredentialInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
    label: GitCredentialLabel
    expires_in_seconds: CredentialExpirySeconds
    @required
    scopes: GitScopes
}

structure GitCredentialOutput {
    @required
    @httpPayload
    @nestedProperties
    credential: GitCredentialSummary
}

@readonly
@http(method: "GET", uri: "/api/v1/repos/{repo_id}/refs")
@identity(mode: "bearer", audience: "source-code-hosting-service", principals: ["browser", "cli", "workload"])
@authz(permission: RepoReadPermission, organization: {source: "token_org_id"})
@audit(event: RefsListAuditEvent, resource: SourceRef, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "source.refs", method: "list", paginated: false, retryable: true)
operation ListSourceRefs {
    input: RepositoryPathInput
    output: ListSourceRefsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

structure ListSourceRefsOutput {
    @required
    refs: Refs
}

@readonly
@http(method: "GET", uri: "/api/v1/repos/{repo_id}/tree")
@identity(mode: "bearer", audience: "source-code-hosting-service", principals: ["browser", "cli", "workload"])
@authz(permission: RepoReadPermission, organization: {source: "token_org_id"})
@audit(event: TreeGetAuditEvent, resource: SourceTree, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "source.trees", method: "get", paginated: false, retryable: true)
operation GetSourceTree {
    input: GetSourceTreeInput
    output: GetSourceTreeOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

structure GetSourceTreeInput {
    @required
    @httpLabel
    repo_id: RepositoryId
    @httpQuery("ref")
    ref: GitRef
    @httpQuery("path")
    path: RepositoryPath
}

structure GetSourceTreeOutput {
    @required
    entries: TreeEntries
}

@readonly
@http(method: "GET", uri: "/api/v1/repos/{repo_id}/blob")
@identity(mode: "bearer", audience: "source-code-hosting-service", principals: ["browser", "cli", "workload"])
@authz(permission: RepoReadPermission, organization: {source: "token_org_id"})
@audit(event: BlobGetAuditEvent, resource: SourceBlob, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "source.blobs", method: "get", paginated: false, retryable: true)
operation GetSourceBlob {
    input: GetSourceBlobInput
    output: GetSourceBlobOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

structure GetSourceBlobInput {
    @required
    @httpLabel
    repo_id: RepositoryId
    @httpQuery("ref")
    ref: GitRef
    @required
    @httpQuery("path")
    path: RepositoryPath
}

structure GetSourceBlobOutput {
    @required
    @httpPayload
    @nestedProperties
    blob: SourceBlobView
}

@idempotent
@http(method: "POST", uri: "/api/v1/repos/{repo_id}/checkout-grants", code: 201)
@identity(mode: "bearer", audience: "source-code-hosting-service", principals: ["browser", "cli"])
@authz(permission: CheckoutWritePermission, organization: {source: "token_org_id"})
@audit(event: CheckoutGrantCreateAuditEvent, resource: SourceCheckoutGrant, action: "create")
@rateLimit(bucket: "source_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "source.checkoutGrants", method: "create", paginated: false, retryable: false)
operation CreateSourceCheckoutGrant {
    input: CreateSourceCheckoutGrantInput
    output: CheckoutGrantOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure CreateSourceCheckoutGrantInput {
    @required
    @httpLabel
    repo_id: RepositoryId
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
    ref: GitRef
}

structure CheckoutGrantOutput {
    @required
    @httpPayload
    @nestedProperties
    grant: CheckoutGrantSummary
}

@idempotent
@http(method: "POST", uri: "/api/v1/repos/{repo_id}/workflow-runs", code: 201)
@identity(mode: "bearer", audience: "source-code-hosting-service", principals: ["browser", "cli"])
@authz(permission: WorkflowWritePermission, organization: {source: "token_org_id"})
@audit(event: WorkflowDispatchAuditEvent, resource: SourceWorkflowRun, action: "create")
@rateLimit(bucket: "source_mutation")
@requestBudget(maxBytes: 262144)
@sdk(module: "source.workflowRuns", method: "create", paginated: false, retryable: false)
operation CreateSourceWorkflowRun {
    input: CreateSourceWorkflowRunInput
    output: WorkflowRunOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure CreateSourceWorkflowRunInput {
    @required
    @httpLabel
    repo_id: RepositoryId
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
    @required
    project_id: ProjectId
    @required
    workflow_path: WorkflowPath
    ref: GitRef
    inputs: WorkflowInputs
}

@readonly
@http(method: "GET", uri: "/api/v1/repos/{repo_id}/workflow-runs")
@identity(mode: "bearer", audience: "source-code-hosting-service", principals: ["browser", "cli", "workload"])
@authz(permission: WorkflowReadPermission, organization: {source: "token_org_id"})
@audit(event: WorkflowRunListAuditEvent, resource: SourceWorkflowRun, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "source.workflowRuns", method: "list", paginated: false, retryable: true)
operation ListSourceWorkflowRuns {
    input: RepositoryPathInput
    output: ListSourceWorkflowRunsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

structure ListSourceWorkflowRunsOutput {
    @required
    workflow_runs: WorkflowRuns
}

@readonly
@http(method: "GET", uri: "/api/v1/workflow-runs/{workflow_run_id}")
@identity(mode: "bearer", audience: "source-code-hosting-service", principals: ["browser", "cli", "workload"])
@authz(permission: WorkflowReadPermission, organization: {source: "token_org_id"})
@audit(event: WorkflowRunReadAuditEvent, resource: SourceWorkflowRun, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "source.workflowRuns", method: "get", paginated: false, retryable: true)
operation GetSourceWorkflowRun {
    input: WorkflowRunPathInput
    output: WorkflowRunOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

structure WorkflowRunPathInput {
    @required
    @httpLabel
    workflow_run_id: WorkflowRunId
}

structure WorkflowRunOutput {
    @required
    @httpPayload
    @nestedProperties
    workflow_run: WorkflowRunSummary
}

@idempotent
@http(method: "POST", uri: "/internal/v1/workflow-runs", code: 201)
@identity(mode: "spiffe_mtls", audience: "source-code-hosting-service", principals: ["workload"])
@authz(permission: WorkflowWritePermission, organization: {source: "body_org_id", member: "org_id"})
@audit(event: InternalWorkflowDispatchAuditEvent, resource: SourceWorkflowRun, action: "create")
@rateLimit(bucket: "internal_mutation")
@requestBudget(maxBytes: 262144)
@sdk(module: "sourceInternal.workflowRuns", method: "create", paginated: false, retryable: false)
operation InternalCreateSourceWorkflowRun {
    input: InternalCreateSourceWorkflowRunInput
    output: WorkflowRunOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, ServiceUnavailableError]
}

structure InternalCreateSourceWorkflowRunInput {
    @required
    org_id: OrgId
    @required
    actor_id: ActorId
    @required
    repo_id: RepositoryId
    @required
    project_id: ProjectId
    @required
    workflow_path: WorkflowPath
    ref: GitRef
    inputs: WorkflowInputs
    @required
    idempotency_key: IdempotencyKey
}

@readonly
@http(method: "GET", uri: "/internal/v1/checkouts/{grant_id}/archive")
@identity(mode: "checkout_grant", audience: "source-code-hosting-service", principals: ["workload"])
@authz(permission: CheckoutWritePermission, organization: {source: "checkout_grant"})
@audit(event: ArchiveStreamAuditEvent, resource: SourceCheckoutArchive, action: "download")
@rateLimit(bucket: "checkout_download")
@requestBudget(maxBytes: 0)
@sdk(module: "sourceInternal.checkouts", method: "downloadArchive", paginated: false, retryable: true)
operation DownloadSourceCheckoutArchive {
    input: DownloadSourceCheckoutArchiveInput
    output: DownloadSourceCheckoutArchiveOutput
    errors: [PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

structure DownloadSourceCheckoutArchiveInput {
    @required
    @httpLabel
    grant_id: CheckoutGrantId
    @required
    @httpHeader("X-Verself-Checkout-Token")
    token: CheckoutGrantToken
}

structure DownloadSourceCheckoutArchiveOutput {
    @required
    @httpHeader("Content-Type")
    contentType: MediaType
    @required
    @httpHeader("Content-Disposition")
    contentDisposition: ContentDisposition
    @required
    @httpPayload
    body: SourceArchive
}
