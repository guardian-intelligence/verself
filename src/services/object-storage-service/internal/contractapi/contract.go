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

type ProblemCode string

type ProblemDetail string

type ProblemType string

type RequestID string

type ResourceName string

type TraceParent string

type AccessKeyID string

type AuthMode string

type BucketAliasName string

type BucketID string

type BucketName string

type CredentialFingerprint string

type CredentialID string

type CredentialStatus string

type DecimalUint64 string

type ObjectPrefix string

type OrgID string

type SPIFFESubject string

type SecretAccessKey string

type ServiceTag string

type BucketAliases []BucketAliasView

type Buckets []BucketView

type ObjectStorageCredentials []ObjectStorageCredentialView

type ObjectStorageLifecycleRules []map[string]any

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

type IdempotencyPayloadMismatchError struct {
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

type RateLimitedError struct {
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

type UnauthenticatedError struct {
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

type AccessKeyIdempotentInput struct {
	AccessKeyID    AccessKeyID    `path:"access_key_id" required:"true" minLength:"1" maxLength:"255"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
}

type BucketAliasView struct {
	BucketID   BucketID        `json:"bucket_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	Alias      BucketAliasName `json:"alias" required:"true" minLength:"1" maxLength:"255"`
	Prefix     *ObjectPrefix   `json:"prefix,omitempty" maxLength:"1024"`
	ServiceTag *ServiceTag     `json:"service_tag,omitempty" maxLength:"128"`
	CreatedAt  string          `json:"created_at" required:"true"`
}

type BucketIdempotentInput struct {
	BucketID       BucketID       `path:"bucket_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
}

type BucketPathInput struct {
	BucketID BucketID `path:"bucket_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
}

type BucketView struct {
	BucketID       BucketID                    `json:"bucket_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	ResourceName   ResourceName                `json:"resourceName" required:"true" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	OrgID          OrgID                       `json:"org_id" required:"true" minLength:"1" maxLength:"128"`
	BucketName     BucketName                  `json:"bucket_name" required:"true" minLength:"3" maxLength:"63" pattern:"^[a-z0-9][a-z0-9.-]*[a-z0-9]$"`
	QuotaBytes     *DecimalUint64              `json:"quota_bytes,omitempty" pattern:"^[0-9]+$"`
	QuotaObjects   *DecimalUint64              `json:"quota_objects,omitempty" pattern:"^[0-9]+$"`
	LifecycleRules ObjectStorageLifecycleRules `json:"lifecycle_rules" required:"true"`
	CreatedAt      string                      `json:"created_at" required:"true"`
	UpdatedAt      string                      `json:"updated_at" required:"true"`
}

type CreateObjectStorageAccessKeyInputBody struct {
	DisplayName DisplayName `json:"display_name" required:"true" minLength:"1" maxLength:"120"`
	ExpiresAt   *string     `json:"expires_at,omitempty"`
}

type CreateObjectStorageAccessKeyInput struct {
	BucketID       BucketID       `path:"bucket_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           CreateObjectStorageAccessKeyInputBody
}

type CreateObjectStorageBucketAliasInputBody struct {
	Alias      BucketAliasName `json:"alias" required:"true" minLength:"1" maxLength:"255"`
	Prefix     *ObjectPrefix   `json:"prefix,omitempty" maxLength:"1024"`
	ServiceTag *ServiceTag     `json:"service_tag,omitempty" maxLength:"128"`
}

type CreateObjectStorageBucketAliasInput struct {
	BucketID       BucketID       `path:"bucket_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           CreateObjectStorageBucketAliasInputBody
}

type CreateObjectStorageBucketInputBody struct {
	BucketName     BucketName                   `json:"bucket_name" required:"true" minLength:"3" maxLength:"63" pattern:"^[a-z0-9][a-z0-9.-]*[a-z0-9]$"`
	LifecycleRules *ObjectStorageLifecycleRules `json:"lifecycle_rules,omitempty"`
	QuotaBytes     *DecimalUint64               `json:"quota_bytes,omitempty" pattern:"^[0-9]+$"`
	QuotaObjects   *DecimalUint64               `json:"quota_objects,omitempty" pattern:"^[0-9]+$"`
}

type CreateObjectStorageBucketInput struct {
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           CreateObjectStorageBucketInputBody
}

type CreateObjectStorageMTLSPrincipalInputBody struct {
	DisplayName   DisplayName   `json:"display_name" required:"true" minLength:"1" maxLength:"120"`
	SpiffeSubject SPIFFESubject `json:"spiffe_subject" required:"true" minLength:"1" maxLength:"512"`
}

type CreateObjectStorageMTLSPrincipalInput struct {
	BucketID       BucketID       `path:"bucket_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           CreateObjectStorageMTLSPrincipalInputBody
}

type DeleteObjectStorageBucketAliasInput struct {
	Alias          BucketAliasName `path:"alias" required:"true" minLength:"1" maxLength:"255"`
	BucketID       BucketID        `path:"bucket_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey IdempotencyKey  `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
}

type DeleteObjectStorageMTLSPrincipalInput struct {
	BucketID       BucketID       `path:"bucket_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	CredentialID   CredentialID   `path:"credential_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
}

type EmptyInput struct{}

type ObjectStorageAccessKeySecret struct {
	AccessKeyID           AccessKeyID                 `json:"access_key_id" required:"true" minLength:"1" maxLength:"255"`
	SecretAccessKey       SecretAccessKey             `json:"secret_access_key" required:"true" minLength:"1" maxLength:"255"`
	CredentialFingerprint CredentialFingerprint       `json:"credential_fingerprint" required:"true" minLength:"1" maxLength:"255"`
	Credential            ObjectStorageCredentialView `json:"credential" required:"true"`
}

type ObjectStorageCredentialView struct {
	CredentialID          CredentialID           `json:"credential_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	BucketID              BucketID               `json:"bucket_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	AuthMode              AuthMode               `json:"auth_mode" required:"true" minLength:"1" maxLength:"64"`
	DisplayName           DisplayName            `json:"display_name" required:"true" minLength:"1" maxLength:"120"`
	AccessKeyID           *AccessKeyID           `json:"access_key_id,omitempty" minLength:"1" maxLength:"255"`
	SpiffeSubject         *SPIFFESubject         `json:"spiffe_subject,omitempty" minLength:"1" maxLength:"512"`
	CredentialFingerprint *CredentialFingerprint `json:"credential_fingerprint,omitempty" minLength:"1" maxLength:"255"`
	Status                CredentialStatus       `json:"status" required:"true" minLength:"1" maxLength:"64"`
	ExpiresAt             *string                `json:"expires_at,omitempty"`
	CreatedAt             string                 `json:"created_at" required:"true"`
	RevokedAt             *string                `json:"revoked_at,omitempty"`
}

type UpdateObjectStorageBucketInputBody struct {
	LifecycleRules *ObjectStorageLifecycleRules `json:"lifecycle_rules,omitempty"`
	QuotaBytes     *DecimalUint64               `json:"quota_bytes,omitempty" pattern:"^[0-9]+$"`
	QuotaObjects   *DecimalUint64               `json:"quota_objects,omitempty" pattern:"^[0-9]+$"`
}

type UpdateObjectStorageBucketInput struct {
	BucketID       BucketID       `path:"bucket_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           UpdateObjectStorageBucketInputBody
}

type EmptyOutputBody struct{}

type EmptyOutput struct {
	Body EmptyOutputBody
}

type ObjectStorageAccessKeySecretOutput struct {
	Body ObjectStorageAccessKeySecret
}

type ListObjectStorageBucketsOutputBody struct {
	Buckets Buckets `json:"buckets" required:"true"`
}

type ListObjectStorageBucketsOutput struct {
	Body ListObjectStorageBucketsOutputBody
}

type BucketOutput struct {
	Body BucketView
}

type ListObjectStorageBucketAliasesOutputBody struct {
	Aliases BucketAliases `json:"aliases" required:"true"`
}

type ListObjectStorageBucketAliasesOutput struct {
	Body ListObjectStorageBucketAliasesOutputBody
}

type BucketAliasOutput struct {
	Body BucketAliasView
}

type ListObjectStorageCredentialsOutputBody struct {
	Credentials ObjectStorageCredentials `json:"credentials" required:"true"`
}

type ListObjectStorageCredentialsOutput struct {
	Body ListObjectStorageCredentialsOutputBody
}

var Operations = []OperationDescriptor{
	DeleteObjectStorageAccessKey.Descriptor,
	RollObjectStorageAccessKey.Descriptor,
	ListObjectStorageBuckets.Descriptor,
	CreateObjectStorageBucket.Descriptor,
	DeleteObjectStorageBucket.Descriptor,
	GetObjectStorageBucket.Descriptor,
	UpdateObjectStorageBucket.Descriptor,
	CreateObjectStorageAccessKey.Descriptor,
	ListObjectStorageBucketAliases.Descriptor,
	CreateObjectStorageBucketAlias.Descriptor,
	DeleteObjectStorageBucketAlias.Descriptor,
	ListObjectStorageCredentials.Descriptor,
	CreateObjectStorageMtlsPrincipal.Descriptor,
	DeleteObjectStorageMtlsPrincipal.Descriptor,
}

var DeleteObjectStorageAccessKey = Operation[AccessKeyIdempotentInput, EmptyOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.objectstorage.v1#DeleteObjectStorageAccessKey",
		OperationID:         "delete-object-storage-access-key",
		Method:              "DELETE",
		Path:                "/api/v1/access-keys/{access_key_id}",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "object-storage:access-key:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "object_storage.access_key.delete", Resource: "object_storage_access_key", Action: "delete"},
		RateLimitBucket:     "object_storage_mutation",
		RequestBodyMaxBytes: 1024,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "objectStorage.accessKeys", Method: "delete", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var RollObjectStorageAccessKey = Operation[AccessKeyIdempotentInput, ObjectStorageAccessKeySecretOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.objectstorage.v1#RollObjectStorageAccessKey",
		OperationID:         "roll-object-storage-access-key",
		Method:              "POST",
		Path:                "/api/v1/access-keys/{access_key_id}/roll",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "object-storage:access-key:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "object_storage.access_key.roll", Resource: "object_storage_access_key", Action: "roll"},
		RateLimitBucket:     "object_storage_mutation",
		RequestBodyMaxBytes: 1024,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "objectStorage.accessKeys", Method: "roll", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var ListObjectStorageBuckets = Operation[EmptyInput, ListObjectStorageBucketsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.objectstorage.v1#ListObjectStorageBuckets",
		OperationID:         "list-object-storage-buckets",
		Method:              "GET",
		Path:                "/api/v1/buckets",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "object-storage:bucket:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "object_storage.bucket.list", Resource: "bucket", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "objectStorage.buckets", Method: "list", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var CreateObjectStorageBucket = Operation[CreateObjectStorageBucketInput, BucketOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.objectstorage.v1#CreateObjectStorageBucket",
		OperationID:         "create-object-storage-bucket",
		Method:              "POST",
		Path:                "/api/v1/buckets",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "object-storage:bucket:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "object_storage.bucket.create", Resource: "bucket", Action: "create"},
		RateLimitBucket:     "object_storage_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "objectStorage.buckets", Method: "create", Paginated: false, Retryable: false},
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

var DeleteObjectStorageBucket = Operation[BucketIdempotentInput, EmptyOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.objectstorage.v1#DeleteObjectStorageBucket",
		OperationID:         "delete-object-storage-bucket",
		Method:              "DELETE",
		Path:                "/api/v1/buckets/{bucket_id}",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "object-storage:bucket:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "object_storage.bucket.delete", Resource: "bucket", Action: "delete"},
		RateLimitBucket:     "object_storage_mutation",
		RequestBodyMaxBytes: 1024,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "objectStorage.buckets", Method: "delete", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var GetObjectStorageBucket = Operation[BucketPathInput, BucketOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.objectstorage.v1#GetObjectStorageBucket",
		OperationID:         "get-object-storage-bucket",
		Method:              "GET",
		Path:                "/api/v1/buckets/{bucket_id}",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "object-storage:bucket:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "object_storage.bucket.read", Resource: "bucket", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "objectStorage.buckets", Method: "get", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var UpdateObjectStorageBucket = Operation[UpdateObjectStorageBucketInput, BucketOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.objectstorage.v1#UpdateObjectStorageBucket",
		OperationID:         "update-object-storage-bucket",
		Method:              "PUT",
		Path:                "/api/v1/buckets/{bucket_id}",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "object-storage:bucket:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "object_storage.bucket.update", Resource: "bucket", Action: "update"},
		RateLimitBucket:     "object_storage_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "objectStorage.buckets", Method: "update", Paginated: false, Retryable: false},
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

var CreateObjectStorageAccessKey = Operation[CreateObjectStorageAccessKeyInput, ObjectStorageAccessKeySecretOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.objectstorage.v1#CreateObjectStorageAccessKey",
		OperationID:         "create-object-storage-access-key",
		Method:              "POST",
		Path:                "/api/v1/buckets/{bucket_id}/access-keys",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "object-storage:access-key:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "object_storage.access_key.create", Resource: "object_storage_access_key", Action: "create"},
		RateLimitBucket:     "object_storage_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "objectStorage.accessKeys", Method: "create", Paginated: false, Retryable: false},
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

var ListObjectStorageBucketAliases = Operation[BucketPathInput, ListObjectStorageBucketAliasesOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.objectstorage.v1#ListObjectStorageBucketAliases",
		OperationID:         "list-object-storage-bucket-aliases",
		Method:              "GET",
		Path:                "/api/v1/buckets/{bucket_id}/aliases",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "object-storage:bucket:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "object_storage.bucket_alias.list", Resource: "bucket_alias", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "objectStorage.aliases", Method: "list", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var CreateObjectStorageBucketAlias = Operation[CreateObjectStorageBucketAliasInput, BucketAliasOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.objectstorage.v1#CreateObjectStorageBucketAlias",
		OperationID:         "create-object-storage-bucket-alias",
		Method:              "POST",
		Path:                "/api/v1/buckets/{bucket_id}/aliases",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "object-storage:bucket:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "object_storage.bucket_alias.create", Resource: "bucket_alias", Action: "create"},
		RateLimitBucket:     "object_storage_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "objectStorage.aliases", Method: "create", Paginated: false, Retryable: false},
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

var DeleteObjectStorageBucketAlias = Operation[DeleteObjectStorageBucketAliasInput, EmptyOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.objectstorage.v1#DeleteObjectStorageBucketAlias",
		OperationID:         "delete-object-storage-bucket-alias",
		Method:              "DELETE",
		Path:                "/api/v1/buckets/{bucket_id}/aliases/{alias}",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "object-storage:bucket:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "object_storage.bucket_alias.delete", Resource: "bucket_alias", Action: "delete"},
		RateLimitBucket:     "object_storage_mutation",
		RequestBodyMaxBytes: 1024,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "objectStorage.aliases", Method: "delete", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var ListObjectStorageCredentials = Operation[BucketPathInput, ListObjectStorageCredentialsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.objectstorage.v1#ListObjectStorageCredentials",
		OperationID:         "list-object-storage-credentials",
		Method:              "GET",
		Path:                "/api/v1/buckets/{bucket_id}/credentials",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "object-storage:access-key:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "object_storage.credential.list", Resource: "object_storage_credential", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "objectStorage.credentials", Method: "list", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var CreateObjectStorageMtlsPrincipal = Operation[CreateObjectStorageMTLSPrincipalInput, BucketOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.objectstorage.v1#CreateObjectStorageMtlsPrincipal",
		OperationID:         "create-object-storage-mtls-principal",
		Method:              "POST",
		Path:                "/api/v1/buckets/{bucket_id}/mtls-principals",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "object-storage:access-key:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "object_storage.mtls_principal.create", Resource: "object_storage_mtls_principal", Action: "create"},
		RateLimitBucket:     "object_storage_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "objectStorage.mtlsPrincipals", Method: "create", Paginated: false, Retryable: false},
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

var DeleteObjectStorageMtlsPrincipal = Operation[DeleteObjectStorageMTLSPrincipalInput, EmptyOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.objectstorage.v1#DeleteObjectStorageMtlsPrincipal",
		OperationID:         "delete-object-storage-mtls-principal",
		Method:              "DELETE",
		Path:                "/api/v1/buckets/{bucket_id}/mtls-principals/{credential_id}",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "object-storage:access-key:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "object_storage.mtls_principal.delete", Resource: "object_storage_mtls_principal", Action: "delete"},
		RateLimitBucket:     "object_storage_mutation",
		RequestBodyMaxBytes: 1024,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "objectStorage.mtlsPrincipals", Method: "delete", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

type Handlers = PublicHandlers

type PublicHandlers interface {
	DeleteObjectStorageAccessKey(context.Context, *AccessKeyIdempotentInput) (*EmptyOutput, error)
	RollObjectStorageAccessKey(context.Context, *AccessKeyIdempotentInput) (*ObjectStorageAccessKeySecretOutput, error)
	ListObjectStorageBuckets(context.Context, *EmptyInput) (*ListObjectStorageBucketsOutput, error)
	CreateObjectStorageBucket(context.Context, *CreateObjectStorageBucketInput) (*BucketOutput, error)
	DeleteObjectStorageBucket(context.Context, *BucketIdempotentInput) (*EmptyOutput, error)
	GetObjectStorageBucket(context.Context, *BucketPathInput) (*BucketOutput, error)
	UpdateObjectStorageBucket(context.Context, *UpdateObjectStorageBucketInput) (*BucketOutput, error)
	CreateObjectStorageAccessKey(context.Context, *CreateObjectStorageAccessKeyInput) (*ObjectStorageAccessKeySecretOutput, error)
	ListObjectStorageBucketAliases(context.Context, *BucketPathInput) (*ListObjectStorageBucketAliasesOutput, error)
	CreateObjectStorageBucketAlias(context.Context, *CreateObjectStorageBucketAliasInput) (*BucketAliasOutput, error)
	DeleteObjectStorageBucketAlias(context.Context, *DeleteObjectStorageBucketAliasInput) (*EmptyOutput, error)
	ListObjectStorageCredentials(context.Context, *BucketPathInput) (*ListObjectStorageCredentialsOutput, error)
	CreateObjectStorageMtlsPrincipal(context.Context, *CreateObjectStorageMTLSPrincipalInput) (*BucketOutput, error)
	DeleteObjectStorageMtlsPrincipal(context.Context, *DeleteObjectStorageMTLSPrincipalInput) (*EmptyOutput, error)
}

type DeleteObjectStorageAccessKeyHandler = Handler[AccessKeyIdempotentInput, EmptyOutput]

type RollObjectStorageAccessKeyHandler = Handler[AccessKeyIdempotentInput, ObjectStorageAccessKeySecretOutput]

type ListObjectStorageBucketsHandler = Handler[EmptyInput, ListObjectStorageBucketsOutput]

type CreateObjectStorageBucketHandler = Handler[CreateObjectStorageBucketInput, BucketOutput]

type DeleteObjectStorageBucketHandler = Handler[BucketIdempotentInput, EmptyOutput]

type GetObjectStorageBucketHandler = Handler[BucketPathInput, BucketOutput]

type UpdateObjectStorageBucketHandler = Handler[UpdateObjectStorageBucketInput, BucketOutput]

type CreateObjectStorageAccessKeyHandler = Handler[CreateObjectStorageAccessKeyInput, ObjectStorageAccessKeySecretOutput]

type ListObjectStorageBucketAliasesHandler = Handler[BucketPathInput, ListObjectStorageBucketAliasesOutput]

type CreateObjectStorageBucketAliasHandler = Handler[CreateObjectStorageBucketAliasInput, BucketAliasOutput]

type DeleteObjectStorageBucketAliasHandler = Handler[DeleteObjectStorageBucketAliasInput, EmptyOutput]

type ListObjectStorageCredentialsHandler = Handler[BucketPathInput, ListObjectStorageCredentialsOutput]

type CreateObjectStorageMtlsPrincipalHandler = Handler[CreateObjectStorageMTLSPrincipalInput, BucketOutput]

type DeleteObjectStorageMtlsPrincipalHandler = Handler[DeleteObjectStorageMTLSPrincipalInput, EmptyOutput]
