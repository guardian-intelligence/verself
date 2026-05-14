// Code generated from the Smithy API contract. DO NOT EDIT.

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

type IdempotencyKey string

type PageSize int

type PageToken string

type ProblemCode string

type ProblemDetail string

type ProblemType string

type RequestID string

type TraceParent string

type EmailAddress string

type MemberID string

type MemberResourceName string

type OrgAclVersion int

type OrgID string

type OrgSlug string

type OrganizationResourceName string

type OrganizationVersion int

type OrganizationRole string

const (
	OrganizationRoleAdmin  OrganizationRole = "admin"
	OrganizationRoleMember OrganizationRole = "member"
	OrganizationRoleOwner  OrganizationRole = "owner"
)

type Members []MemberSummary

type Organizations []OrganizationSummary

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

type GetMemberInput struct {
	OrgID    OrgID    `path:"orgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	MemberID MemberID `path:"memberId" required:"true" pattern:"^member_[0-9A-HJKMNP-TV-Z]{26}$"`
}

type GetOrganizationInput struct {
	OrgID OrgID `path:"orgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
}

type ListMembersInput struct {
	OrgID     OrgID     `path:"orgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	PageSize  PageSize  `query:"page_size" minimum:"1" maximum:"500"`
	PageToken PageToken `query:"page_token" minLength:"1" maxLength:"4096"`
}

type ListOrganizationsInput struct {
	PageSize  PageSize  `query:"page_size" minimum:"1" maximum:"500"`
	PageToken PageToken `query:"page_token" minLength:"1" maxLength:"4096"`
}

type MemberSummary struct {
	OrgID        OrgID              `json:"orgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	MemberID     MemberID           `json:"memberId" required:"true" pattern:"^member_[0-9A-HJKMNP-TV-Z]{26}$"`
	ResourceName MemberResourceName `json:"resourceName" required:"true" pattern:"^urn:verself:inst_[0-9A-HJKMNP-TV-Z]{26}:orgs/org_[0-9A-HJKMNP-TV-Z]{26}/members/member_[0-9A-HJKMNP-TV-Z]{26}$"`
	Email        EmailAddress       `json:"email" required:"true" minLength:"3" maxLength:"320"`
	DisplayName  DisplayName        `json:"displayName" required:"true" minLength:"1" maxLength:"120"`
	Role         OrganizationRole   `json:"role" required:"true"`
}

type OrganizationSummary struct {
	OrgID         OrgID                    `json:"orgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	ResourceName  OrganizationResourceName `json:"resourceName" required:"true" pattern:"^urn:verself:inst_[0-9A-HJKMNP-TV-Z]{26}:orgs/org_[0-9A-HJKMNP-TV-Z]{26}$"`
	Slug          *OrgSlug                 `json:"slug,omitempty" minLength:"1" maxLength:"80" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
	DisplayName   DisplayName              `json:"displayName" required:"true" minLength:"1" maxLength:"120"`
	CallerRole    OrganizationRole         `json:"callerRole" required:"true"`
	Version       OrganizationVersion      `json:"version" required:"true" minimum:"1" maximum:"2147483647"`
	OrgAclVersion OrgAclVersion            `json:"orgAclVersion" required:"true" minimum:"1" maximum:"2147483647"`
}

type UpdateMemberRoleInputBody struct {
	Role                  OrganizationRole `json:"role" required:"true"`
	ExpectedRole          OrganizationRole `json:"expectedRole" required:"true"`
	ExpectedOrgAclVersion OrgAclVersion    `json:"expectedOrgAclVersion" required:"true" minimum:"1" maximum:"2147483647"`
}

type UpdateMemberRoleInput struct {
	OrgID          OrgID          `path:"orgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	MemberID       MemberID       `path:"memberId" required:"true" pattern:"^member_[0-9A-HJKMNP-TV-Z]{26}$"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           UpdateMemberRoleInputBody
}

type UpdateOrganizationInputBody struct {
	Slug        *OrgSlug            `json:"slug,omitempty" minLength:"1" maxLength:"80" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
	DisplayName *DisplayName        `json:"displayName,omitempty" minLength:"1" maxLength:"120"`
	Version     OrganizationVersion `json:"version" required:"true" minimum:"1" maximum:"2147483647"`
}

type UpdateOrganizationInput struct {
	OrgID          OrgID          `path:"orgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           UpdateOrganizationInputBody
}

type ListOrganizationsOutputBody struct {
	Organizations Organizations `json:"organizations" required:"true"`
	NextPageToken *PageToken    `json:"nextPageToken,omitempty" minLength:"1" maxLength:"4096"`
}

type ListOrganizationsOutput struct {
	Body ListOrganizationsOutputBody
}

type GetOrganizationOutput struct {
	Body OrganizationSummary
}

type UpdateOrganizationOutput struct {
	Body OrganizationSummary
}

type ListMembersOutputBody struct {
	Members       Members    `json:"members" required:"true"`
	NextPageToken *PageToken `json:"nextPageToken,omitempty" minLength:"1" maxLength:"4096"`
}

type ListMembersOutput struct {
	Body ListMembersOutputBody
}

type GetMemberOutput struct {
	Body MemberSummary
}

type UpdateMemberRoleOutput struct {
	Body MemberSummary
}

var Operations = []OperationDescriptor{
	ListOrganizations.Descriptor,
	GetOrganization.Descriptor,
	UpdateOrganization.Descriptor,
	ListMembers.Descriptor,
	GetMember.Descriptor,
	UpdateMemberRole.Descriptor,
}

var ListOrganizations = Operation[ListOrganizationsInput, ListOrganizationsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#ListOrganizations",
		OperationID:         "list-organizations",
		Method:              "GET",
		Path:                "/api/v1/orgs",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           true,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "iam-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:organization:list", OrganizationSource: "token_role_assignments", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "iam.organization.list", Resource: "organization", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "orgs", Method: "list", Paginated: true, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var GetOrganization = Operation[GetOrganizationInput, GetOrganizationOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#GetOrganization",
		OperationID:         "get-organization",
		Method:              "GET",
		Path:                "/api/v1/orgs/{orgId}",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "iam-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:organization:read", OrganizationSource: "input_member", OrganizationMember: "orgId"},
		Audit:               AuditDescriptor{Event: "iam.organization.read", Resource: "organization", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "orgs", Method: "get", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var UpdateOrganization = Operation[UpdateOrganizationInput, UpdateOrganizationOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#UpdateOrganization",
		OperationID:         "update-organization",
		Method:              "PATCH",
		Path:                "/api/v1/orgs/{orgId}",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "iam-service", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:organization:update", OrganizationSource: "input_member", OrganizationMember: "orgId"},
		Audit:               AuditDescriptor{Event: "iam.organization.update", Resource: "organization", Action: "update"},
		RateLimitBucket:     "iam_mutation",
		RequestBodyMaxBytes: 16384,
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "orgs", Method: "update", Paginated: false, Retryable: false},
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

var ListMembers = Operation[ListMembersInput, ListMembersOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#ListMembers",
		OperationID:         "list-members",
		Method:              "GET",
		Path:                "/api/v1/orgs/{orgId}/members",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           true,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "iam-service", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:member:list", OrganizationSource: "input_member", OrganizationMember: "orgId"},
		Audit:               AuditDescriptor{Event: "iam.member.list", Resource: "member", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "members", Method: "list", Paginated: true, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var GetMember = Operation[GetMemberInput, GetMemberOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#GetMember",
		OperationID:         "get-member",
		Method:              "GET",
		Path:                "/api/v1/orgs/{orgId}/members/{memberId}",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "iam-service", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:member:read", OrganizationSource: "input_member", OrganizationMember: "orgId"},
		Audit:               AuditDescriptor{Event: "iam.member.read", Resource: "member", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "members", Method: "get", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var UpdateMemberRole = Operation[UpdateMemberRoleInput, UpdateMemberRoleOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#UpdateMemberRole",
		OperationID:         "update-member-role",
		Method:              "PATCH",
		Path:                "/api/v1/orgs/{orgId}/members/{memberId}/role",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "iam-service", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:member:update_role", OrganizationSource: "input_member", OrganizationMember: "orgId"},
		Audit:               AuditDescriptor{Event: "iam.member.update_role", Resource: "member", Action: "update"},
		RateLimitBucket:     "iam_mutation",
		RequestBodyMaxBytes: 8192,
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "members", Method: "updateRole", Paginated: false, Retryable: false},
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
	ListOrganizations(context.Context, *ListOrganizationsInput) (*ListOrganizationsOutput, error)
	GetOrganization(context.Context, *GetOrganizationInput) (*GetOrganizationOutput, error)
	UpdateOrganization(context.Context, *UpdateOrganizationInput) (*UpdateOrganizationOutput, error)
	ListMembers(context.Context, *ListMembersInput) (*ListMembersOutput, error)
	GetMember(context.Context, *GetMemberInput) (*GetMemberOutput, error)
	UpdateMemberRole(context.Context, *UpdateMemberRoleInput) (*UpdateMemberRoleOutput, error)
}

type ListOrganizationsHandler = Handler[ListOrganizationsInput, ListOrganizationsOutput]

type GetOrganizationHandler = Handler[GetOrganizationInput, GetOrganizationOutput]

type UpdateOrganizationHandler = Handler[UpdateOrganizationInput, UpdateOrganizationOutput]

type ListMembersHandler = Handler[ListMembersInput, ListMembersOutput]

type GetMemberHandler = Handler[GetMemberInput, GetMemberOutput]

type UpdateMemberRoleHandler = Handler[UpdateMemberRoleInput, UpdateMemberRoleOutput]
