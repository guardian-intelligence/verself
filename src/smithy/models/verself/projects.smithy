$version: "2"
namespace verself.projects.v1

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
use verself.common.v1#DateTime
@serviceRuntime(serviceName: "projects-service", publicAudience: "projects-service", internalAudience: "projects-service")
@restJson1
service Projects {
    version: "2026-05-13"
    operations: [
        CreateProject,
        ListProjects,
        GetProject,
        PatchProject,
        ArchiveProject,
        RestoreProject,
        ListProjectEnvironments,
        CreateProjectEnvironment,
        PatchProjectEnvironment,
        ArchiveProjectEnvironment
    ]
    resources: [Project]
}
@serviceRuntime(serviceName: "projects-service", publicAudience: "projects-service", internalAudience: "projects-service")
@restJson1
service ProjectsInternal {
    version: "2026-05-13"
    operations: [
        ResolveProject,
        ResolveProjectEnvironment,
        ListProjectEventsService
    ]
    resources: [
        Project,
        ProjectEvent
    ]
}
@pattern("^[0-9a-fA-F-]{36}$")
string ProjectId
@pattern("^[0-9a-fA-F-]{36}$")
string EnvironmentId
@pattern("^[0-9a-fA-F-]{36}$")
string ProjectEventId
@length(min: 1, max: 128)
@pattern("^org_[0-9A-HJKMNP-TV-Z]{26}$")
string OrgId
@length(min: 1, max: 80)
@pattern("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$")
string ProjectSlug
@length(max: 500)
string ProjectDescription
@pattern("^-?[0-9]+$")
string DecimalInt64
@length(max: 128)
string ActorId
@length(max: 128)
string EventType
@length(max: 255)
string TraceId
map ProjectProtectionPolicy {
    key: String
    value: String
}
map ProjectEventPayload {
    key: String
    value: String
}
enum ProjectState {
    ACTIVE = "active"
    ARCHIVED = "archived"
}
enum ProjectEnvironmentKind {
    PRODUCTION = "production"
    PREVIEW = "preview"
    DEVELOPMENT = "development"
    CUSTOM = "custom"
}
enum ProjectEnvironmentState {
    ACTIVE = "active"
    ARCHIVED = "archived"
}
list ProjectsList {
    member: ProjectSummary
}
list ProjectEnvironments {
    member: ProjectEnvironmentSummary
}
list ProjectEvents {
    member: ProjectEventSummary
}
@permission(name: "projects:project:read")
string ProjectReadPermission
@permission(name: "projects:project:write")
string ProjectWritePermission
@permission(name: "projects:environment:read")
string EnvironmentReadPermission
@permission(name: "projects:environment:write")
string EnvironmentWritePermission
@permission(name: "projects:event:read")
string ProjectEventReadPermission
@permission(name: "projects:resolve")
string ProjectResolvePermission
@auditEvent(name: "projects.project.create")
string ProjectCreateAuditEvent
@auditEvent(name: "projects.project.list")
string ProjectListAuditEvent
@auditEvent(name: "projects.project.read")
string ProjectReadAuditEvent
@auditEvent(name: "projects.project.update")
string ProjectUpdateAuditEvent
@auditEvent(name: "projects.project.archive")
string ProjectArchiveAuditEvent
@auditEvent(name: "projects.project.restore")
string ProjectRestoreAuditEvent
@auditEvent(name: "projects.environment.list")
string EnvironmentListAuditEvent
@auditEvent(name: "projects.environment.create")
string EnvironmentCreateAuditEvent
@auditEvent(name: "projects.environment.update")
string EnvironmentUpdateAuditEvent
@auditEvent(name: "projects.environment.archive")
string EnvironmentArchiveAuditEvent
@auditEvent(name: "projects.project.resolve")
string ProjectResolveAuditEvent
@auditEvent(name: "projects.environment.resolve")
string EnvironmentResolveAuditEvent
@auditEvent(name: "projects.event.list")
string ProjectEventListAuditEvent
resource Project {}
resource ProjectEnvironment {}
resource ProjectEvent {}
structure ProjectSummary {
    @required
    @protoField(number: 1)
    project_id: ProjectId
    @required
    @protoField(number: 2)
    resourceName: ResourceName
    @required
    @protoField(number: 3)
    org_id: OrgId
    @required
    @protoField(number: 4)
    slug: ProjectSlug
    @protoField(number: 5)
    redirected_from_slug: ProjectSlug
    @required
    @protoField(number: 6)
    display_name: DisplayName
    @protoField(number: 7)
    description: ProjectDescription
    @required
    @protoField(number: 8)
    state: ProjectState
    @required
    @protoField(number: 9)
    version: DecimalInt64
    @required
    @protoField(number: 10)
    created_by: ActorId
    @required
    @protoField(number: 11)
    updated_by: ActorId
    @required
    @protoField(number: 12)
    created_at: DateTime
    @required
    @protoField(number: 13)
    updated_at: DateTime
    @protoField(number: 14)
    archived_at: DateTime
}
structure ProjectEnvironmentSummary {
    @required
    @protoField(number: 1)
    environment_id: EnvironmentId
    @required
    @protoField(number: 2)
    resourceName: ResourceName
    @required
    @protoField(number: 3)
    project_id: ProjectId
    @required
    @protoField(number: 4)
    projectResourceName: ResourceName
    @required
    @protoField(number: 5)
    org_id: OrgId
    @required
    @protoField(number: 6)
    slug: ProjectSlug
    @required
    @protoField(number: 7)
    display_name: DisplayName
    @required
    @protoField(number: 8)
    kind: ProjectEnvironmentKind
    @required
    @protoField(number: 9)
    state: ProjectEnvironmentState
    @required
    @protoField(number: 10)
    version: DecimalInt64
    @protoField(number: 11)
    protection_policy: ProjectProtectionPolicy
    @required
    @protoField(number: 12)
    created_by: ActorId
    @required
    @protoField(number: 13)
    updated_by: ActorId
    @required
    @protoField(number: 14)
    created_at: DateTime
    @required
    @protoField(number: 15)
    updated_at: DateTime
    @protoField(number: 16)
    archived_at: DateTime
}
structure ProjectEventSummary {
    @required
    @protoField(number: 1)
    event_id: ProjectEventId
    @required
    @protoField(number: 2)
    org_id: OrgId
    @required
    @protoField(number: 3)
    project_id: ProjectId
    @required
    @protoField(number: 4)
    projectResourceName: ResourceName
    @protoField(number: 5)
    environment_id: EnvironmentId
    @protoField(number: 6)
    environmentResourceName: ResourceName
    @required
    @protoField(number: 7)
    event_type: EventType
    @required
    @protoField(number: 8)
    actor_id: ActorId
    @required
    @protoField(number: 9)
    created_at: DateTime
    @protoField(number: 10)
    payload: ProjectEventPayload
    @protoField(number: 11)
    trace_id: TraceId
}
@idempotent
@http(method: "POST", uri: "/api/v1/projects", code: 201)
@identity(mode: "bearer", audience: "projects-service", principals: ["browser", "cli", "workload"])
@authz(permission: ProjectWritePermission, organization: {source: "token_org_id"})
@audit(event: ProjectCreateAuditEvent, resource: Project, action: "create")
@rateLimit(bucket: "projects_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "projects", method: "create", paginated: false, retryable: false)
operation CreateProject {
    input: CreateProjectInput
    output: ProjectOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
structure CreateProjectInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    @protoField(number: 100)
    idempotencyKey: IdempotencyKey
    @protoField(number: 1)
    slug: ProjectSlug
    @required
    @protoField(number: 2)
    display_name: DisplayName
    @protoField(number: 3)
    description: ProjectDescription
}
@readonly
@http(method: "GET", uri: "/api/v1/projects")
@paginated(inputToken: "cursor", outputToken: "next_cursor", pageSize: "limit", items: "projects")
@identity(mode: "bearer", audience: "projects-service", principals: ["browser", "cli", "workload"])
@authz(permission: ProjectReadPermission, organization: {source: "token_org_id"})
@audit(event: ProjectListAuditEvent, resource: Project, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "projects", method: "list", paginated: true, retryable: true)
operation ListProjects {
    input: ListProjectsInput
    output: ListProjectsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
structure ListProjectsInput {
    @httpQuery("state")
    @protoField(number: 1)
    state: ProjectState
    @httpQuery("limit")
    @protoField(number: 2)
    limit: PageSize
    @httpQuery("cursor")
    @protoField(number: 3)
    cursor: PageToken
}
structure ListProjectsOutput {
    @required
    @protoField(number: 1)
    projects: ProjectsList
    @protoField(number: 2)
    next_cursor: PageToken
}
@readonly
@http(method: "GET", uri: "/api/v1/projects/{project_id}")
@identity(mode: "bearer", audience: "projects-service", principals: ["browser", "cli", "workload"])
@authz(permission: ProjectReadPermission, organization: {source: "token_org_id"})
@audit(event: ProjectReadAuditEvent, resource: Project, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "projects", method: "get", paginated: false, retryable: true)
operation GetProject {
    input: ProjectPathInput
    output: ProjectOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "PATCH", uri: "/api/v1/projects/{project_id}")
@identity(mode: "bearer", audience: "projects-service", principals: ["browser", "cli", "workload"])
@authz(permission: ProjectWritePermission, organization: {source: "token_org_id"})
@audit(event: ProjectUpdateAuditEvent, resource: Project, action: "update")
@rateLimit(bucket: "projects_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "projects", method: "update", paginated: false, retryable: false)
operation PatchProject {
    input: PatchProjectInput
    output: ProjectOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
structure PatchProjectInput {
    @required
    @httpLabel
    @protoField(number: 1)
    project_id: ProjectId
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    @protoField(number: 100)
    idempotencyKey: IdempotencyKey
    @required
    @protoField(number: 2)
    version: DecimalInt64
    @protoField(number: 3)
    slug: ProjectSlug
    @protoField(number: 4)
    display_name: DisplayName
    @protoField(number: 5)
    description: ProjectDescription
}
@idempotent
@http(method: "POST", uri: "/api/v1/projects/{project_id}/archive")
@identity(mode: "bearer", audience: "projects-service", principals: ["browser", "cli", "workload"])
@authz(permission: ProjectWritePermission, organization: {source: "token_org_id"})
@audit(event: ProjectArchiveAuditEvent, resource: Project, action: "archive")
@rateLimit(bucket: "projects_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "projects", method: "archive", paginated: false, retryable: false)
operation ArchiveProject {
    input: ProjectLifecycleInput
    output: ProjectOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "POST", uri: "/api/v1/projects/{project_id}/restore")
@identity(mode: "bearer", audience: "projects-service", principals: ["browser", "cli", "workload"])
@authz(permission: ProjectWritePermission, organization: {source: "token_org_id"})
@audit(event: ProjectRestoreAuditEvent, resource: Project, action: "restore")
@rateLimit(bucket: "projects_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "projects", method: "restore", paginated: false, retryable: false)
operation RestoreProject {
    input: ProjectLifecycleInput
    output: ProjectOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
structure ProjectPathInput {
    @required
    @httpLabel
    @protoField(number: 1)
    project_id: ProjectId
}
structure ProjectLifecycleInput {
    @required
    @httpLabel
    @protoField(number: 1)
    project_id: ProjectId
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    @protoField(number: 100)
    idempotencyKey: IdempotencyKey
    @required
    @protoField(number: 2)
    version: DecimalInt64
}
structure ProjectOutput {
    @required
    @httpPayload
    @nestedProperties
    @protoField(number: 1)
    project: ProjectSummary
}
@readonly
@http(method: "GET", uri: "/api/v1/projects/{project_id}/environments")
@identity(mode: "bearer", audience: "projects-service", principals: ["browser", "cli", "workload"])
@authz(permission: EnvironmentReadPermission, organization: {source: "token_org_id"})
@audit(event: EnvironmentListAuditEvent, resource: ProjectEnvironment, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "projectEnvironments", method: "list", paginated: false, retryable: true)
operation ListProjectEnvironments {
    input: ListProjectEnvironmentsInput
    output: ListProjectEnvironmentsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}
structure ListProjectEnvironmentsInput {
    @required
    @httpLabel
    @protoField(number: 1)
    project_id: ProjectId
}
structure ListProjectEnvironmentsOutput {
    @required
    @protoField(number: 1)
    environments: ProjectEnvironments
}
@idempotent
@http(method: "POST", uri: "/api/v1/projects/{project_id}/environments", code: 201)
@identity(mode: "bearer", audience: "projects-service", principals: ["browser", "cli", "workload"])
@authz(permission: EnvironmentWritePermission, organization: {source: "token_org_id"})
@audit(event: EnvironmentCreateAuditEvent, resource: ProjectEnvironment, action: "create")
@rateLimit(bucket: "projects_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "projectEnvironments", method: "create", paginated: false, retryable: false)
operation CreateProjectEnvironment {
    input: CreateProjectEnvironmentInput
    output: ProjectEnvironmentOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
structure CreateProjectEnvironmentInput {
    @required
    @httpLabel
    @protoField(number: 1)
    project_id: ProjectId
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    @protoField(number: 100)
    idempotencyKey: IdempotencyKey
    @required
    @protoField(number: 2)
    slug: ProjectSlug
    @required
    @protoField(number: 3)
    display_name: DisplayName
    @required
    @protoField(number: 4)
    kind: ProjectEnvironmentKind
    @protoField(number: 5)
    protection_policy: ProjectProtectionPolicy
}
@idempotent
@http(method: "PATCH", uri: "/api/v1/projects/{project_id}/environments/{environment_id}")
@identity(mode: "bearer", audience: "projects-service", principals: ["browser", "cli", "workload"])
@authz(permission: EnvironmentWritePermission, organization: {source: "token_org_id"})
@audit(event: EnvironmentUpdateAuditEvent, resource: ProjectEnvironment, action: "update")
@rateLimit(bucket: "projects_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "projectEnvironments", method: "update", paginated: false, retryable: false)
operation PatchProjectEnvironment {
    input: PatchProjectEnvironmentInput
    output: ProjectEnvironmentOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
structure PatchProjectEnvironmentInput {
    @required
    @httpLabel
    @protoField(number: 1)
    project_id: ProjectId
    @required
    @httpLabel
    @protoField(number: 2)
    environment_id: EnvironmentId
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    @protoField(number: 100)
    idempotencyKey: IdempotencyKey
    @required
    @protoField(number: 3)
    version: DecimalInt64
    @protoField(number: 4)
    display_name: DisplayName
    @protoField(number: 5)
    protection_policy: ProjectProtectionPolicy
}
@idempotent
@http(method: "POST", uri: "/api/v1/projects/{project_id}/environments/{environment_id}/archive")
@identity(mode: "bearer", audience: "projects-service", principals: ["browser", "cli", "workload"])
@authz(permission: EnvironmentWritePermission, organization: {source: "token_org_id"})
@audit(event: EnvironmentArchiveAuditEvent, resource: ProjectEnvironment, action: "archive")
@rateLimit(bucket: "projects_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "projectEnvironments", method: "archive", paginated: false, retryable: false)
operation ArchiveProjectEnvironment {
    input: ProjectEnvironmentLifecycleInput
    output: ProjectEnvironmentOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
structure ProjectEnvironmentLifecycleInput {
    @required
    @httpLabel
    @protoField(number: 1)
    project_id: ProjectId
    @required
    @httpLabel
    @protoField(number: 2)
    environment_id: EnvironmentId
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    @protoField(number: 100)
    idempotencyKey: IdempotencyKey
    @required
    @protoField(number: 3)
    version: DecimalInt64
}
structure ProjectEnvironmentOutput {
    @required
    @httpPayload
    @nestedProperties
    @protoField(number: 1)
    environment: ProjectEnvironmentSummary
}
@http(method: "POST", uri: "/internal/v1/projects/resolve")
@identity(mode: "spiffe_mtls", audience: "projects-service", principals: ["workload"])
@authz(permission: ProjectResolvePermission, organization: {source: "body_org_id", member: "org_id"})
@audit(event: ProjectResolveAuditEvent, resource: Project, action: "resolve")
@rateLimit(bucket: "internal")
@requestBudget(maxBytes: 8192)
@sdk(module: "projectsInternal", method: "resolveProject", paginated: false, retryable: true)
operation ResolveProject {
    input: ResolveProjectInput
    output: ResolveProjectOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ServiceUnavailableError]
}
structure ResolveProjectInput {
    @required
    @protoField(number: 1)
    org_id: OrgId
    @protoField(number: 2)
    project_id: ProjectId
    @protoField(number: 3)
    slug: ProjectSlug
    @required
    @protoField(number: 4)
    require_active: Boolean
}
structure ResolveProjectOutput {
    @required
    @protoField(number: 1)
    project: ProjectSummary
}
@http(method: "POST", uri: "/internal/v1/project-environments/resolve")
@identity(mode: "spiffe_mtls", audience: "projects-service", principals: ["workload"])
@authz(permission: ProjectResolvePermission, organization: {source: "body_org_id", member: "org_id"})
@audit(event: EnvironmentResolveAuditEvent, resource: ProjectEnvironment, action: "resolve")
@rateLimit(bucket: "internal")
@requestBudget(maxBytes: 8192)
@sdk(module: "projectsInternal", method: "resolveProjectEnvironment", paginated: false, retryable: true)
operation ResolveProjectEnvironment {
    input: ResolveProjectEnvironmentInput
    output: ResolveProjectEnvironmentOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ServiceUnavailableError]
}
structure ResolveProjectEnvironmentInput {
    @required
    @protoField(number: 1)
    org_id: OrgId
    @required
    @protoField(number: 2)
    project_id: ProjectId
    @protoField(number: 3)
    environment_id: EnvironmentId
    @protoField(number: 4)
    slug: ProjectSlug
    @required
    @protoField(number: 5)
    require_active: Boolean
}
structure ResolveProjectEnvironmentOutput {
    @required
    @protoField(number: 1)
    environment: ProjectEnvironmentSummary
}
@readonly
@http(method: "GET", uri: "/internal/v1/project-events")
@paginated(inputToken: "cursor", outputToken: "next_cursor", pageSize: "limit", items: "events")
@identity(mode: "spiffe_mtls", audience: "projects-service", principals: ["workload"])
@authz(permission: ProjectEventReadPermission, organization: {source: "request_org_id"})
@audit(event: ProjectEventListAuditEvent, resource: ProjectEvent, action: "list")
@rateLimit(bucket: "internal")
@requestBudget(maxBytes: 0)
@sdk(module: "projectsInternal", method: "listProjectEvents", paginated: true, retryable: true)
operation ListProjectEventsService {
    input: ListProjectEventsServiceInput
    output: ListProjectEventsServiceOutput
    errors: [PermissionDeniedError, ServiceUnavailableError]
}
structure ListProjectEventsServiceInput {
    @required
    @httpQuery("org_id")
    @protoField(number: 1)
    org_id: OrgId
    @httpQuery("limit")
    @protoField(number: 2)
    limit: PageSize
    @httpQuery("cursor")
    @protoField(number: 3)
    cursor: PageToken
}
structure ListProjectEventsServiceOutput {
    @required
    @protoField(number: 1)
    events: ProjectEvents
    @protoField(number: 2)
    next_cursor: PageToken
}
