// Code generated from the Smithy API contract. DO NOT EDIT.

package internalcontractapi

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

type PermissionName string

type ProblemCode string

type ProblemDetail string

type ProblemType string

type RequestID string

type TraceParent string

type AuthorizationRelation string

type AuthorizationResourceID string

type AuthorizationResourceType string

type EmailAddress string

type FamilyName string

type GivenName string

type OrgSlug string

type OrganizationResourceName string

type OrganizationVersion int

type ProviderOrgID string

type ResourcePermissionName string

type SubjectID string

type ZedToken string

type IAMAuthorizationSubjectType string

const (
	IAMAuthorizationSubjectTypeServiceAccount IAMAuthorizationSubjectType = "service_account"
	IAMAuthorizationSubjectTypeUser           IAMAuthorizationSubjectType = "user"
	IAMAuthorizationSubjectTypeWorkload       IAMAuthorizationSubjectType = "workload"
)

type Permissions []PermissionName

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

type AuthorizeOperationInputBody struct {
	OrgID       ProviderOrgID           `json:"org_id" required:"true" minLength:"1" maxLength:"64" pattern:"^[0-9]+$"`
	Subject     IAMAuthorizationSubject `json:"subject" required:"true"`
	Permissions Permissions             `json:"permissions" required:"true"`
	MinZedToken *ZedToken               `json:"min_zed_token,omitempty" maxLength:"1024"`
}

type AuthorizeOperationInput struct {
	Body AuthorizeOperationInputBody
}

type AuthorizeOperationResult struct {
	OrgID       ProviderOrgID           `json:"org_id" required:"true" minLength:"1" maxLength:"64" pattern:"^[0-9]+$"`
	Subject     IAMAuthorizationSubject `json:"subject" required:"true"`
	Permissions Permissions             `json:"permissions" required:"true"`
	ZedToken    *ZedToken               `json:"zed_token,omitempty" maxLength:"1024"`
}

type AuthorizeResourceInputBody struct {
	OrgID               ProviderOrgID           `json:"org_id" required:"true" minLength:"1" maxLength:"64" pattern:"^[0-9]+$"`
	Subject             IAMAuthorizationSubject `json:"subject" required:"true"`
	OperationPermission *PermissionName         `json:"operation_permission,omitempty" pattern:"^[a-z][a-z0-9_-]*(?::[a-z][a-z0-9_-]*)+$"`
	Resource            IAMResourceRef          `json:"resource" required:"true"`
	ResourcePermission  ResourcePermissionName  `json:"resource_permission" required:"true" minLength:"1" maxLength:"128"`
	MinZedToken         *ZedToken               `json:"min_zed_token,omitempty" maxLength:"1024"`
}

type AuthorizeResourceInput struct {
	Body AuthorizeResourceInputBody
}

type AuthorizeResourceResult struct {
	OrgID               ProviderOrgID           `json:"org_id" required:"true" minLength:"1" maxLength:"64" pattern:"^[0-9]+$"`
	Subject             IAMAuthorizationSubject `json:"subject" required:"true"`
	OperationPermission *PermissionName         `json:"operation_permission,omitempty" pattern:"^[a-z][a-z0-9_-]*(?::[a-z][a-z0-9_-]*)+$"`
	Resource            IAMResourceRef          `json:"resource" required:"true"`
	ResourcePermission  ResourcePermissionName  `json:"resource_permission" required:"true" minLength:"1" maxLength:"128"`
	Allowed             bool                    `json:"allowed" required:"true"`
	ZedToken            *ZedToken               `json:"zed_token,omitempty" maxLength:"1024"`
}

type HumanProfileSummary struct {
	SubjectID   SubjectID    `json:"subject_id" required:"true" minLength:"1" maxLength:"512"`
	Email       EmailAddress `json:"email" required:"true" minLength:"3" maxLength:"320"`
	GivenName   GivenName    `json:"given_name" required:"true" minLength:"1" maxLength:"100"`
	FamilyName  FamilyName   `json:"family_name" required:"true" minLength:"1" maxLength:"100"`
	DisplayName DisplayName  `json:"display_name" required:"true" minLength:"1" maxLength:"120"`
	SyncedAt    string       `json:"synced_at" required:"true"`
}

type IAMAuthorizationSubject struct {
	Type IAMAuthorizationSubjectType `json:"type" required:"true"`
	ID   SubjectID                   `json:"id" required:"true" minLength:"1" maxLength:"512"`
}

type IAMResourceRef struct {
	Type AuthorizationResourceType `json:"type" required:"true" minLength:"1" maxLength:"128" pattern:"^[a-z][a-z0-9_]*$"`
	ID   AuthorizationResourceID   `json:"id" required:"true" minLength:"1" maxLength:"512"`
}

type OrganizationProfile struct {
	OrgID          ProviderOrgID             `json:"org_id" required:"true" minLength:"1" maxLength:"64" pattern:"^[0-9]+$"`
	Slug           OrgSlug                   `json:"slug" required:"true" minLength:"1" maxLength:"80" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
	DisplayName    DisplayName               `json:"display_name" required:"true" minLength:"1" maxLength:"120"`
	State          string                    `json:"state" required:"true"`
	Version        OrganizationVersion       `json:"version" required:"true" minimum:"1" maximum:"2147483647"`
	UpdatedAt      string                    `json:"updated_at" required:"true"`
	RedirectedFrom *OrgSlug                  `json:"redirected_from,omitempty" minLength:"1" maxLength:"80" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
	ResourceName   *OrganizationResourceName `json:"resourceName,omitempty" pattern:"^urn:verself:inst_[0-9A-HJKMNP-TV-Z]{26}:orgs/org_[0-9A-HJKMNP-TV-Z]{26}$"`
}

type ResolveOrganizationInputBody struct {
	OrgID         *ProviderOrgID `json:"org_id,omitempty" minLength:"1" maxLength:"64" pattern:"^[0-9]+$"`
	RequireActive bool           `json:"require_active" required:"true"`
	Slug          *OrgSlug       `json:"slug,omitempty" minLength:"1" maxLength:"80" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
}

type ResolveOrganizationInput struct {
	Body ResolveOrganizationInputBody
}

type UpdateHumanProfileInputBody struct {
	GivenName   GivenName    `json:"given_name" required:"true" minLength:"1" maxLength:"100"`
	FamilyName  FamilyName   `json:"family_name" required:"true" minLength:"1" maxLength:"100"`
	DisplayName *DisplayName `json:"display_name,omitempty" minLength:"1" maxLength:"120"`
}

type UpdateHumanProfileInput struct {
	SubjectID SubjectID `path:"subject_id" required:"true" minLength:"1" maxLength:"512"`
	Body      UpdateHumanProfileInputBody
}

type WriteResourceParentEdgeInputBody struct {
	OrgID     ProviderOrgID         `json:"org_id" required:"true" minLength:"1" maxLength:"64" pattern:"^[0-9]+$"`
	Resource  IAMResourceRef        `json:"resource" required:"true"`
	Relation  AuthorizationRelation `json:"relation" required:"true" minLength:"1" maxLength:"128" pattern:"^[a-z][a-z0-9_]*$"`
	Parent    IAMResourceRef        `json:"parent" required:"true"`
	Operation *PermissionName       `json:"operation,omitempty" pattern:"^[a-z][a-z0-9_-]*(?::[a-z][a-z0-9_-]*)+$"`
}

type WriteResourceParentEdgeInput struct {
	Body WriteResourceParentEdgeInputBody
}

type WriteResourceParentEdgeResult struct {
	Resource  IAMResourceRef        `json:"resource" required:"true"`
	Relation  AuthorizationRelation `json:"relation" required:"true" minLength:"1" maxLength:"128" pattern:"^[a-z][a-z0-9_]*$"`
	Parent    IAMResourceRef        `json:"parent" required:"true"`
	ZedToken  *ZedToken             `json:"zed_token,omitempty" maxLength:"1024"`
	Operation *PermissionName       `json:"operation,omitempty" pattern:"^[a-z][a-z0-9_-]*(?::[a-z][a-z0-9_-]*)+$"`
}

type AuthorizeOperationOutput struct {
	Body AuthorizeOperationResult
}

type AuthorizeResourceOutput struct {
	Body AuthorizeResourceResult
}

type WriteResourceParentEdgeOutput struct {
	Body WriteResourceParentEdgeResult
}

type ResolveOrganizationOutputBody struct {
	Organization OrganizationProfile `json:"organization" required:"true"`
}

type ResolveOrganizationOutput struct {
	Body ResolveOrganizationOutputBody
}

type UpdateHumanProfileOutput struct {
	Body HumanProfileSummary
}

var Operations = []OperationDescriptor{
	AuthorizeOperation.Descriptor,
	AuthorizeResource.Descriptor,
	WriteResourceParentEdge.Descriptor,
	ResolveOrganization.Descriptor,
	UpdateHumanProfile.Descriptor,
}

var AuthorizeOperation = Operation[AuthorizeOperationInput, AuthorizeOperationOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#AuthorizeOperation",
		OperationID:         "authorize-operation",
		Method:              "POST",
		Path:                "/internal/v1/authorization/authorize",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls", Audience: "iam-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:authorization:check", OrganizationSource: "input_member", OrganizationMember: "org_id"},
		Audit:               AuditDescriptor{Event: "iam.authorization.check", Resource: "authorization", Action: "test"},
		RateLimitBucket:     "internal",
		RequestBodyMaxBytes: 8192,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "iamInternal", Method: "authorizeOperation", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var AuthorizeResource = Operation[AuthorizeResourceInput, AuthorizeResourceOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#AuthorizeResource",
		OperationID:         "authorize-resource",
		Method:              "POST",
		Path:                "/internal/v1/authorization/resources/authorize",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls", Audience: "iam-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:authorization:resource_check", OrganizationSource: "input_member", OrganizationMember: "org_id"},
		Audit:               AuditDescriptor{Event: "iam.authorization.resource_check", Resource: "authorization", Action: "test"},
		RateLimitBucket:     "internal",
		RequestBodyMaxBytes: 8192,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "iamInternal", Method: "authorizeResource", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var WriteResourceParentEdge = Operation[WriteResourceParentEdgeInput, WriteResourceParentEdgeOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#WriteResourceParentEdge",
		OperationID:         "write-resource-parent-edge",
		Method:              "POST",
		Path:                "/internal/v1/authorization/resources/parent-edge",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls", Audience: "iam-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:authorization:parent_edge_write", OrganizationSource: "input_member", OrganizationMember: "org_id"},
		Audit:               AuditDescriptor{Event: "iam.authorization.resource_parent_edge.write", Resource: "authorization", Action: "write"},
		RateLimitBucket:     "internal_mutation",
		RequestBodyMaxBytes: 8192,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "iamInternal", Method: "writeResourceParentEdge", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var ResolveOrganization = Operation[ResolveOrganizationInput, ResolveOrganizationOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#ResolveOrganization",
		OperationID:         "resolve-organization",
		Method:              "POST",
		Path:                "/internal/v1/organizations/resolve",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls", Audience: "iam-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:organization:resolve", OrganizationSource: "request_subject", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "iam.organization.resolve", Resource: "organization", Action: "read"},
		RateLimitBucket:     "internal",
		RequestBodyMaxBytes: 8192,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "iamInternal", Method: "resolveOrganization", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var UpdateHumanProfile = Operation[UpdateHumanProfileInput, UpdateHumanProfileOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#UpdateHumanProfile",
		OperationID:         "update-human-profile",
		Method:              "PATCH",
		Path:                "/internal/v1/subjects/{subject_id}/human-profile",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls_bearer", Audience: "iam-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:human_profile:write", OrganizationSource: "request_subject", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "iam.human_profile.write", Resource: "human_profile", Action: "write"},
		RateLimitBucket:     "internal_mutation",
		RequestBodyMaxBytes: 8192,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "iamInternal", Method: "updateHumanProfile", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

type Handlers = InternalHandlers

type InternalHandlers interface {
	AuthorizeOperation(context.Context, *AuthorizeOperationInput) (*AuthorizeOperationOutput, error)
	AuthorizeResource(context.Context, *AuthorizeResourceInput) (*AuthorizeResourceOutput, error)
	WriteResourceParentEdge(context.Context, *WriteResourceParentEdgeInput) (*WriteResourceParentEdgeOutput, error)
	ResolveOrganization(context.Context, *ResolveOrganizationInput) (*ResolveOrganizationOutput, error)
	UpdateHumanProfile(context.Context, *UpdateHumanProfileInput) (*UpdateHumanProfileOutput, error)
}

type AuthorizeOperationHandler = Handler[AuthorizeOperationInput, AuthorizeOperationOutput]

type AuthorizeResourceHandler = Handler[AuthorizeResourceInput, AuthorizeResourceOutput]

type WriteResourceParentEdgeHandler = Handler[WriteResourceParentEdgeInput, WriteResourceParentEdgeOutput]

type ResolveOrganizationHandler = Handler[ResolveOrganizationInput, ResolveOrganizationOutput]

type UpdateHumanProfileHandler = Handler[UpdateHumanProfileInput, UpdateHumanProfileOutput]
