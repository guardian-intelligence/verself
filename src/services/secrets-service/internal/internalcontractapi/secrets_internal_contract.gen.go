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

type ProblemCode string

type ProblemDetail string

type ProblemType string

type RequestID string

type ResourceName string

type TraceParent string

type Branch string

type CredentialDisplayName string

type CredentialID string

type CredentialKind string

type CredentialScope string

type CredentialStatus string

type EnvID string

type InjectionAttemptID string

type InjectionEnvName string

type InjectionExecutionID string

type InjectionGrantID string

type OrgID string

type ScopeLevel string

type SecretName string

type SecretValue string

type SecretVersion string

type SourceID string

type SubjectID string

type CredentialScopes []CredentialScope

type InjectionEnvValues []InjectionEnvValue

type InjectionSecretRequests []InjectionSecretRequest

type CredentialMetadata map[string]string

type ConflictError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type PermissionDeniedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type ResourceNotFoundError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type ServiceUnavailableError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type ValidationFailedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type InjectionEnvValue struct {
	Name  InjectionEnvName `json:"name" required:"true" minLength:"1" maxLength:"255"`
	Value SecretValue      `json:"value" required:"true" minLength:"1" maxLength:"65536"`
}

type InjectionResolveInputBody struct {
	ActorID     SubjectID               `json:"actor_id" required:"true" minLength:"1" maxLength:"128"`
	AttemptID   InjectionAttemptID      `json:"attempt_id" required:"true" minLength:"1" maxLength:"255"`
	ExecutionID InjectionExecutionID    `json:"execution_id" required:"true" minLength:"1" maxLength:"255"`
	OrgID       OrgID                   `json:"org_id" required:"true" minLength:"1" maxLength:"128"`
	Secrets     InjectionSecretRequests `json:"secrets" required:"true"`
}

type InjectionResolveInput struct {
	Body InjectionResolveInputBody
}

type InjectionSecretRequest struct {
	Branch     *Branch          `json:"branch,omitempty" maxLength:"255"`
	EnvID      *EnvID           `json:"env_id,omitempty" maxLength:"255"`
	EnvName    InjectionEnvName `json:"env_name" required:"true" minLength:"1" maxLength:"255"`
	GrantID    InjectionGrantID `json:"grant_id" required:"true" minLength:"1" maxLength:"255"`
	Kind       *string          `json:"kind,omitempty"`
	ScopeLevel *ScopeLevel      `json:"scope_level,omitempty" minLength:"1" maxLength:"128"`
	SecretName SecretName       `json:"secret_name" required:"true" minLength:"1" maxLength:"255"`
	SourceID   *SourceID        `json:"source_id,omitempty" maxLength:"255"`
}

type InternalOpaqueCredentialInputBody struct {
	ActorID     SubjectID              `json:"actor_id" required:"true" minLength:"1" maxLength:"128"`
	DisplayName *CredentialDisplayName `json:"display_name,omitempty" maxLength:"128"`
	ExpiresAt   *string                `json:"expires_at,omitempty"`
	Kind        CredentialKind         `json:"kind" required:"true" minLength:"1" maxLength:"128"`
	Metadata    *CredentialMetadata    `json:"metadata,omitempty"`
	OrgID       OrgID                  `json:"org_id" required:"true" minLength:"1" maxLength:"128"`
	Scopes      CredentialScopes       `json:"scopes" required:"true" minLength:"1" maxLength:"64"`
}

type InternalOpaqueCredentialInput struct {
	Body InternalOpaqueCredentialInputBody
}

type OpaqueCredentialDTO struct {
	CreatedAt      string             `json:"created_at" required:"true"`
	CredentialID   CredentialID       `json:"credential_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	CurrentVersion SecretVersion      `json:"current_version" required:"true" pattern:"^[0-9]+$"`
	DisplayName    string             `json:"display_name" required:"true"`
	ExpiresAt      string             `json:"expires_at" required:"true"`
	Kind           CredentialKind     `json:"kind" required:"true" minLength:"1" maxLength:"128"`
	LastUsedAt     *string            `json:"last_used_at,omitempty"`
	Metadata       CredentialMetadata `json:"metadata" required:"true"`
	OrgID          OrgID              `json:"org_id" required:"true" minLength:"1" maxLength:"128"`
	ResourceName   *ResourceName      `json:"resourceName,omitempty" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	RevokedAt      *string            `json:"revoked_at,omitempty"`
	Scopes         CredentialScopes   `json:"scopes" required:"true" minLength:"1" maxLength:"64"`
	Status         CredentialStatus   `json:"status" required:"true" minLength:"1" maxLength:"128"`
	Subject        string             `json:"subject" required:"true"`
	TokenPrefix    string             `json:"token_prefix" required:"true"`
	UpdatedAt      string             `json:"updated_at" required:"true"`
}

type VerifyInternalOpaqueCredentialInputBody struct {
	ActorID        *SubjectID        `json:"actor_id,omitempty" minLength:"1" maxLength:"128"`
	Kind           CredentialKind    `json:"kind" required:"true" minLength:"1" maxLength:"128"`
	OrgID          OrgID             `json:"org_id" required:"true" minLength:"1" maxLength:"128"`
	RequiredScopes *CredentialScopes `json:"required_scopes,omitempty" minLength:"1" maxLength:"64"`
	Token          SecretValue       `json:"token" required:"true" minLength:"1" maxLength:"65536"`
}

type VerifyInternalOpaqueCredentialInput struct {
	Body VerifyInternalOpaqueCredentialInputBody
}

type InternalOpaqueCredentialOutputBody struct {
	Credential OpaqueCredentialDTO `json:"credential" required:"true"`
	Token      SecretValue         `json:"token" required:"true" minLength:"1" maxLength:"65536"`
}

type InternalOpaqueCredentialOutput struct {
	Body InternalOpaqueCredentialOutputBody
}

type VerifyInternalOpaqueCredentialOutputBody struct {
	Active       bool                 `json:"active" required:"true"`
	Credential   *OpaqueCredentialDTO `json:"credential,omitempty"`
	DenialReason *string              `json:"denial_reason,omitempty"`
}

type VerifyInternalOpaqueCredentialOutput struct {
	Body VerifyInternalOpaqueCredentialOutputBody
}

type InjectionResolveOutputBody struct {
	Env InjectionEnvValues `json:"env" required:"true"`
}

type InjectionResolveOutput struct {
	Body InjectionResolveOutputBody
}

var Operations = []OperationDescriptor{
	CreateInternalOpaqueCredential.Descriptor,
	VerifyInternalOpaqueCredential.Descriptor,
	ResolveInjection.Descriptor,
}

var CreateInternalOpaqueCredential = Operation[InternalOpaqueCredentialInput, InternalOpaqueCredentialOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#CreateInternalOpaqueCredential",
		OperationID:         "create-internal-opaque-credential",
		Method:              "POST",
		Path:                "/internal/v1/credentials",
		DefaultStatus:       201,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls", Audience: "secrets-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:credential:create", OrganizationSource: "body_org_id", OrganizationMember: "org_id"},
		Audit:               AuditDescriptor{Event: "secrets.credential.create", Resource: "opaque_credential", Action: "create"},
		RateLimitBucket:     "internal_mutation",
		RequestBodyMaxBytes: 131072,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "secretsInternal.credentials", Method: "create", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var VerifyInternalOpaqueCredential = Operation[VerifyInternalOpaqueCredentialInput, VerifyInternalOpaqueCredentialOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#VerifyInternalOpaqueCredential",
		OperationID:         "verify-internal-opaque-credential",
		Method:              "POST",
		Path:                "/internal/v1/credentials:verify",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls", Audience: "secrets-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:credential:read", OrganizationSource: "body_org_id", OrganizationMember: "org_id"},
		Audit:               AuditDescriptor{Event: "secrets.credential.read", Resource: "opaque_credential", Action: "verify"},
		RateLimitBucket:     "internal_read",
		RequestBodyMaxBytes: 131072,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "secretsInternal.credentials", Method: "verify", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var ResolveInjection = Operation[InjectionResolveInput, InjectionResolveOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#ResolveInjection",
		OperationID:         "resolve-injection",
		Method:              "POST",
		Path:                "/internal/v1/injections/resolve",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls", Audience: "secrets-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:secret:read", OrganizationSource: "body_org_id", OrganizationMember: "org_id"},
		Audit:               AuditDescriptor{Event: "secrets.injection.resolve", Resource: "secret", Action: "resolve"},
		RateLimitBucket:     "internal_read",
		RequestBodyMaxBytes: 131072,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "secretsInternal.injections", Method: "resolve", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

type Handlers = InternalHandlers

type InternalHandlers interface {
	CreateInternalOpaqueCredential(context.Context, *InternalOpaqueCredentialInput) (*InternalOpaqueCredentialOutput, error)
	VerifyInternalOpaqueCredential(context.Context, *VerifyInternalOpaqueCredentialInput) (*VerifyInternalOpaqueCredentialOutput, error)
	ResolveInjection(context.Context, *InjectionResolveInput) (*InjectionResolveOutput, error)
}

type CreateInternalOpaqueCredentialHandler = Handler[InternalOpaqueCredentialInput, InternalOpaqueCredentialOutput]

type VerifyInternalOpaqueCredentialHandler = Handler[VerifyInternalOpaqueCredentialInput, VerifyInternalOpaqueCredentialOutput]

type ResolveInjectionHandler = Handler[InjectionResolveInput, InjectionResolveOutput]
