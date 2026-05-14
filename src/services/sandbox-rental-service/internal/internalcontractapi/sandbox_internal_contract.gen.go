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

type TraceParent string

type OrgID string

type ProjectID string

type Provider string

type ProviderOwner string

type ProviderRepo string

type ProviderRepositoryID string

type RepositoryFullName string

type SourceRepositoryID string

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

type InternalRegisterRunnerRepositoryInputBody struct {
	OrgID                OrgID                `json:"org_id" required:"true" minLength:"1" maxLength:"128"`
	ProjectID            ProjectID            `json:"project_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	Provider             Provider             `json:"provider" required:"true" minLength:"1" maxLength:"64"`
	ProviderOwner        ProviderOwner        `json:"provider_owner" required:"true" minLength:"1" maxLength:"255"`
	ProviderRepo         ProviderRepo         `json:"provider_repo" required:"true" minLength:"1" maxLength:"255"`
	ProviderRepositoryID ProviderRepositoryID `json:"provider_repository_id" required:"true" minLength:"1" maxLength:"512"`
	RepositoryFullName   *RepositoryFullName  `json:"repository_full_name,omitempty" minLength:"1" maxLength:"1024"`
	SourceRepositoryID   *SourceRepositoryID  `json:"source_repository_id,omitempty" pattern:"^[0-9a-fA-F-]{36}$"`
}

type InternalRegisterRunnerRepositoryInput struct {
	Body InternalRegisterRunnerRepositoryInputBody
}

type RunnerRepositoryRegistration struct {
	ProjectID            ProjectID            `json:"project_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	Provider             Provider             `json:"provider" required:"true" minLength:"1" maxLength:"64"`
	ProviderRepositoryID ProviderRepositoryID `json:"provider_repository_id" required:"true" minLength:"1" maxLength:"512"`
	SourceRepositoryID   *SourceRepositoryID  `json:"source_repository_id,omitempty" pattern:"^[0-9a-fA-F-]{36}$"`
	State                string               `json:"state" required:"true"`
}

type InternalRegisterRunnerRepositoryOutputBody struct {
	Registration RunnerRepositoryRegistration `json:"registration" required:"true"`
}

type InternalRegisterRunnerRepositoryOutput struct {
	Body InternalRegisterRunnerRepositoryOutputBody
}

var Operations = []OperationDescriptor{
	InternalRegisterRunnerRepository.Descriptor,
}

var InternalRegisterRunnerRepository = Operation[InternalRegisterRunnerRepositoryInput, InternalRegisterRunnerRepositoryOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#InternalRegisterRunnerRepository",
		OperationID:         "internal-register-runner-repository",
		Method:              "POST",
		Path:                "/internal/v1/runner/repositories",
		DefaultStatus:       201,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls", Audience: "sandbox-rental-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:runner_repository:register", OrganizationSource: "body_org_id", OrganizationMember: "org_id"},
		Audit:               AuditDescriptor{Event: "sandbox.runner_repository.register", Resource: "runner_repository", Action: "register"},
		RateLimitBucket:     "internal_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "sandboxInternal.runnerRepositories", Method: "register", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

type Handlers = PublicHandlers

type PublicHandlers interface {
	InternalRegisterRunnerRepository(context.Context, *InternalRegisterRunnerRepositoryInput) (*InternalRegisterRunnerRepositoryOutput, error)
}

type InternalRegisterRunnerRepositoryHandler = Handler[InternalRegisterRunnerRepositoryInput, InternalRegisterRunnerRepositoryOutput]
