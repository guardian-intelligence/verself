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

type PageSize int

type PageToken string

type ProblemCode string

type ProblemDetail string

type ProblemType string

type RequestID string

type ResourceName string

type TraceParent string

type ActorID string

type DecimalInt64 string

type EnvironmentID string

type OrgID string

type ProjectDescription string

type ProjectID string

type ProjectSlug string

type ProjectEnvironmentKind string

const (
	ProjectEnvironmentKindCustom      ProjectEnvironmentKind = "custom"
	ProjectEnvironmentKindDevelopment ProjectEnvironmentKind = "development"
	ProjectEnvironmentKindPreview     ProjectEnvironmentKind = "preview"
	ProjectEnvironmentKindProduction  ProjectEnvironmentKind = "production"
)

type ProjectEnvironmentState string

const (
	ProjectEnvironmentStateActive   ProjectEnvironmentState = "active"
	ProjectEnvironmentStateArchived ProjectEnvironmentState = "archived"
)

type ProjectState string

const (
	ProjectStateActive   ProjectState = "active"
	ProjectStateArchived ProjectState = "archived"
)

type ProjectEnvironments []ProjectEnvironmentSummary

type ProjectsList []ProjectSummary

type ProjectProtectionPolicy map[string]string

type ConflictError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type IdempotencyPayloadMismatchError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type PermissionDeniedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type RateLimitedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type ResourceNotFoundError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type ServiceUnavailableError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type UnauthenticatedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type ValidationFailedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type CreateProjectEnvironmentInputBody struct {
	Slug             ProjectSlug              `json:"slug" required:"true" minLength:"1" maxLength:"80" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
	DisplayName      DisplayName              `json:"display_name" required:"true" minLength:"1" maxLength:"120"`
	Kind             ProjectEnvironmentKind   `json:"kind" required:"true"`
	ProtectionPolicy *ProjectProtectionPolicy `json:"protection_policy,omitempty"`
}

type CreateProjectEnvironmentInput struct {
	ProjectID      ProjectID      `path:"project_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           CreateProjectEnvironmentInputBody
}

type CreateProjectInputBody struct {
	Slug        *ProjectSlug        `json:"slug,omitempty" minLength:"1" maxLength:"80" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
	DisplayName DisplayName         `json:"display_name" required:"true" minLength:"1" maxLength:"120"`
	Description *ProjectDescription `json:"description,omitempty" maxLength:"500"`
}

type CreateProjectInput struct {
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           CreateProjectInputBody
}

type ListProjectEnvironmentsInput struct {
	ProjectID ProjectID `path:"project_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
}

type ListProjectsInput struct {
	State  ProjectState `query:"state"`
	Limit  PageSize     `query:"limit" minimum:"1" maximum:"500"`
	Cursor PageToken    `query:"cursor" minLength:"1" maxLength:"4096"`
}

type PatchProjectEnvironmentInputBody struct {
	Version          DecimalInt64             `json:"version" required:"true" pattern:"^-?[0-9]+$"`
	DisplayName      *DisplayName             `json:"display_name,omitempty" minLength:"1" maxLength:"120"`
	ProtectionPolicy *ProjectProtectionPolicy `json:"protection_policy,omitempty"`
}

type PatchProjectEnvironmentInput struct {
	ProjectID      ProjectID      `path:"project_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	EnvironmentID  EnvironmentID  `path:"environment_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           PatchProjectEnvironmentInputBody
}

type PatchProjectInputBody struct {
	Version     DecimalInt64        `json:"version" required:"true" pattern:"^-?[0-9]+$"`
	Slug        *ProjectSlug        `json:"slug,omitempty" minLength:"1" maxLength:"80" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
	DisplayName *DisplayName        `json:"display_name,omitempty" minLength:"1" maxLength:"120"`
	Description *ProjectDescription `json:"description,omitempty" maxLength:"500"`
}

type PatchProjectInput struct {
	ProjectID      ProjectID      `path:"project_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           PatchProjectInputBody
}

type ProjectEnvironmentLifecycleInputBody struct {
	Version DecimalInt64 `json:"version" required:"true" pattern:"^-?[0-9]+$"`
}

type ProjectEnvironmentLifecycleInput struct {
	ProjectID      ProjectID      `path:"project_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	EnvironmentID  EnvironmentID  `path:"environment_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           ProjectEnvironmentLifecycleInputBody
}

type ProjectEnvironmentSummary struct {
	EnvironmentID       EnvironmentID            `json:"environment_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	ResourceName        ResourceName             `json:"resourceName" required:"true" minLength:"1" maxLength:"1024" pattern:"^urn:verself:.+$"`
	ProjectID           ProjectID                `json:"project_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	ProjectResourceName ResourceName             `json:"projectResourceName" required:"true" minLength:"1" maxLength:"1024" pattern:"^urn:verself:.+$"`
	OrgID               OrgID                    `json:"org_id" required:"true" minLength:"1" maxLength:"128"`
	Slug                ProjectSlug              `json:"slug" required:"true" minLength:"1" maxLength:"80" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
	DisplayName         DisplayName              `json:"display_name" required:"true" minLength:"1" maxLength:"120"`
	Kind                ProjectEnvironmentKind   `json:"kind" required:"true"`
	State               ProjectEnvironmentState  `json:"state" required:"true"`
	Version             DecimalInt64             `json:"version" required:"true" pattern:"^-?[0-9]+$"`
	ProtectionPolicy    *ProjectProtectionPolicy `json:"protection_policy,omitempty"`
	CreatedBy           ActorID                  `json:"created_by" required:"true" maxLength:"128"`
	UpdatedBy           ActorID                  `json:"updated_by" required:"true" maxLength:"128"`
	CreatedAt           string                   `json:"created_at" required:"true"`
	UpdatedAt           string                   `json:"updated_at" required:"true"`
	ArchivedAt          *string                  `json:"archived_at,omitempty"`
}

type ProjectLifecycleInputBody struct {
	Version DecimalInt64 `json:"version" required:"true" pattern:"^-?[0-9]+$"`
}

type ProjectLifecycleInput struct {
	ProjectID      ProjectID      `path:"project_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           ProjectLifecycleInputBody
}

type ProjectPathInput struct {
	ProjectID ProjectID `path:"project_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
}

type ProjectSummary struct {
	ProjectID          ProjectID           `json:"project_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	ResourceName       ResourceName        `json:"resourceName" required:"true" minLength:"1" maxLength:"1024" pattern:"^urn:verself:.+$"`
	OrgID              OrgID               `json:"org_id" required:"true" minLength:"1" maxLength:"128"`
	Slug               ProjectSlug         `json:"slug" required:"true" minLength:"1" maxLength:"80" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
	RedirectedFromSlug *ProjectSlug        `json:"redirected_from_slug,omitempty" minLength:"1" maxLength:"80" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
	DisplayName        DisplayName         `json:"display_name" required:"true" minLength:"1" maxLength:"120"`
	Description        *ProjectDescription `json:"description,omitempty" maxLength:"500"`
	State              ProjectState        `json:"state" required:"true"`
	Version            DecimalInt64        `json:"version" required:"true" pattern:"^-?[0-9]+$"`
	CreatedBy          ActorID             `json:"created_by" required:"true" maxLength:"128"`
	UpdatedBy          ActorID             `json:"updated_by" required:"true" maxLength:"128"`
	CreatedAt          string              `json:"created_at" required:"true"`
	UpdatedAt          string              `json:"updated_at" required:"true"`
	ArchivedAt         *string             `json:"archived_at,omitempty"`
}

type ListProjectsOutputBody struct {
	Projects   ProjectsList `json:"projects" required:"true"`
	NextCursor *PageToken   `json:"next_cursor,omitempty" minLength:"1" maxLength:"4096"`
}

type ListProjectsOutput struct {
	Body ListProjectsOutputBody
}

type ProjectOutput struct {
	Body ProjectSummary
}

type ListProjectEnvironmentsOutputBody struct {
	Environments ProjectEnvironments `json:"environments" required:"true"`
}

type ListProjectEnvironmentsOutput struct {
	Body ListProjectEnvironmentsOutputBody
}

type ProjectEnvironmentOutput struct {
	Body ProjectEnvironmentSummary
}

var Operations = []OperationDescriptor{
	ListProjects.Descriptor,
	CreateProject.Descriptor,
	GetProject.Descriptor,
	PatchProject.Descriptor,
	ArchiveProject.Descriptor,
	ListProjectEnvironments.Descriptor,
	CreateProjectEnvironment.Descriptor,
	PatchProjectEnvironment.Descriptor,
	ArchiveProjectEnvironment.Descriptor,
	RestoreProject.Descriptor,
}

var ListProjects = Operation[ListProjectsInput, ListProjectsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.projects.v1#ListProjects",
		OperationID:         "list-projects",
		Method:              "GET",
		Path:                "/api/v1/projects",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           true,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "projects-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "projects:project:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "projects.project.list", Resource: "project", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "projects", Method: "list", Paginated: true, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var CreateProject = Operation[CreateProjectInput, ProjectOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.projects.v1#CreateProject",
		OperationID:         "create-project",
		Method:              "POST",
		Path:                "/api/v1/projects",
		DefaultStatus:       201,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "projects-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "projects:project:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "projects.project.create", Resource: "project", Action: "create"},
		RateLimitBucket:     "projects_mutation",
		RequestBodyMaxBytes: 16384,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "projects", Method: "create", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var GetProject = Operation[ProjectPathInput, ProjectOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.projects.v1#GetProject",
		OperationID:         "get-project",
		Method:              "GET",
		Path:                "/api/v1/projects/{project_id}",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "projects-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "projects:project:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "projects.project.read", Resource: "project", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "projects", Method: "get", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var PatchProject = Operation[PatchProjectInput, ProjectOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.projects.v1#PatchProject",
		OperationID:         "patch-project",
		Method:              "PATCH",
		Path:                "/api/v1/projects/{project_id}",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "projects-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "projects:project:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "projects.project.update", Resource: "project", Action: "update"},
		RateLimitBucket:     "projects_mutation",
		RequestBodyMaxBytes: 16384,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "projects", Method: "update", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var ArchiveProject = Operation[ProjectLifecycleInput, ProjectOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.projects.v1#ArchiveProject",
		OperationID:         "archive-project",
		Method:              "POST",
		Path:                "/api/v1/projects/{project_id}/archive",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "projects-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "projects:project:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "projects.project.archive", Resource: "project", Action: "archive"},
		RateLimitBucket:     "projects_mutation",
		RequestBodyMaxBytes: 16384,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "projects", Method: "archive", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var ListProjectEnvironments = Operation[ListProjectEnvironmentsInput, ListProjectEnvironmentsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.projects.v1#ListProjectEnvironments",
		OperationID:         "list-project-environments",
		Method:              "GET",
		Path:                "/api/v1/projects/{project_id}/environments",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "projects-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "projects:environment:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "projects.environment.list", Resource: "project_environment", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "projectEnvironments", Method: "list", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var CreateProjectEnvironment = Operation[CreateProjectEnvironmentInput, ProjectEnvironmentOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.projects.v1#CreateProjectEnvironment",
		OperationID:         "create-project-environment",
		Method:              "POST",
		Path:                "/api/v1/projects/{project_id}/environments",
		DefaultStatus:       201,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "projects-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "projects:environment:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "projects.environment.create", Resource: "project_environment", Action: "create"},
		RateLimitBucket:     "projects_mutation",
		RequestBodyMaxBytes: 16384,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "projectEnvironments", Method: "create", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var PatchProjectEnvironment = Operation[PatchProjectEnvironmentInput, ProjectEnvironmentOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.projects.v1#PatchProjectEnvironment",
		OperationID:         "patch-project-environment",
		Method:              "PATCH",
		Path:                "/api/v1/projects/{project_id}/environments/{environment_id}",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "projects-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "projects:environment:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "projects.environment.update", Resource: "project_environment", Action: "update"},
		RateLimitBucket:     "projects_mutation",
		RequestBodyMaxBytes: 16384,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "projectEnvironments", Method: "update", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var ArchiveProjectEnvironment = Operation[ProjectEnvironmentLifecycleInput, ProjectEnvironmentOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.projects.v1#ArchiveProjectEnvironment",
		OperationID:         "archive-project-environment",
		Method:              "POST",
		Path:                "/api/v1/projects/{project_id}/environments/{environment_id}/archive",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "projects-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "projects:environment:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "projects.environment.archive", Resource: "project_environment", Action: "archive"},
		RateLimitBucket:     "projects_mutation",
		RequestBodyMaxBytes: 16384,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "projectEnvironments", Method: "archive", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var RestoreProject = Operation[ProjectLifecycleInput, ProjectOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.projects.v1#RestoreProject",
		OperationID:         "restore-project",
		Method:              "POST",
		Path:                "/api/v1/projects/{project_id}/restore",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "projects-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "projects:project:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "projects.project.restore", Resource: "project", Action: "restore"},
		RateLimitBucket:     "projects_mutation",
		RequestBodyMaxBytes: 16384,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "projects", Method: "restore", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

type Handlers = PublicHandlers

type PublicHandlers interface {
	ListProjects(context.Context, *ListProjectsInput) (*ListProjectsOutput, error)
	CreateProject(context.Context, *CreateProjectInput) (*ProjectOutput, error)
	GetProject(context.Context, *ProjectPathInput) (*ProjectOutput, error)
	PatchProject(context.Context, *PatchProjectInput) (*ProjectOutput, error)
	ArchiveProject(context.Context, *ProjectLifecycleInput) (*ProjectOutput, error)
	ListProjectEnvironments(context.Context, *ListProjectEnvironmentsInput) (*ListProjectEnvironmentsOutput, error)
	CreateProjectEnvironment(context.Context, *CreateProjectEnvironmentInput) (*ProjectEnvironmentOutput, error)
	PatchProjectEnvironment(context.Context, *PatchProjectEnvironmentInput) (*ProjectEnvironmentOutput, error)
	ArchiveProjectEnvironment(context.Context, *ProjectEnvironmentLifecycleInput) (*ProjectEnvironmentOutput, error)
	RestoreProject(context.Context, *ProjectLifecycleInput) (*ProjectOutput, error)
}

type ListProjectsHandler = Handler[ListProjectsInput, ListProjectsOutput]

type CreateProjectHandler = Handler[CreateProjectInput, ProjectOutput]

type GetProjectHandler = Handler[ProjectPathInput, ProjectOutput]

type PatchProjectHandler = Handler[PatchProjectInput, ProjectOutput]

type ArchiveProjectHandler = Handler[ProjectLifecycleInput, ProjectOutput]

type ListProjectEnvironmentsHandler = Handler[ListProjectEnvironmentsInput, ListProjectEnvironmentsOutput]

type CreateProjectEnvironmentHandler = Handler[CreateProjectEnvironmentInput, ProjectEnvironmentOutput]

type PatchProjectEnvironmentHandler = Handler[PatchProjectEnvironmentInput, ProjectEnvironmentOutput]

type ArchiveProjectEnvironmentHandler = Handler[ProjectEnvironmentLifecycleInput, ProjectEnvironmentOutput]

type RestoreProjectHandler = Handler[ProjectLifecycleInput, ProjectOutput]
