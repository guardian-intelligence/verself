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

type IdempotencyKey string

type ProblemCode string

type ProblemDetail string

type ProblemType string

type RequestID string

type ResourceName string

type TraceParent string

type Branch string

type Ciphertext string

type CredentialDisplayName string

type CredentialExpiresInSeconds int64

type CredentialID string

type CredentialKind string

type CredentialScope string

type CredentialStatus string

type CredentialSubject string

type EnvID string

type KeyName string

type MessageBase64 string

type OrgID string

type PlaintextBase64 string

type ScopeLevel string

type SecretListLimit int

type SecretName string

type SecretValue string

type SecretVersion string

type Signature string

type SourceID string

type CredentialScopes []CredentialScope

type OpaqueCredentialDTOs []OpaqueCredentialDTO

type SecretDTOs []SecretDTO

type SecretNames []SecretName

type SecretValueDTOs []SecretValueDTO

type VariableDTOs []VariableDTO

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

type CreateOpaqueCredentialInputBody struct {
	DisplayName      *CredentialDisplayName      `json:"display_name,omitempty" maxLength:"128"`
	ExpiresInSeconds *CredentialExpiresInSeconds `json:"expires_in_seconds,omitempty" minimum:"60" maximum:"7776000"`
	Kind             CredentialKind              `json:"kind" required:"true" minLength:"1" maxLength:"128"`
	Metadata         *CredentialMetadata         `json:"metadata,omitempty"`
	Scopes           CredentialScopes            `json:"scopes" required:"true" minLength:"1" maxLength:"64"`
	Subject          *CredentialSubject          `json:"subject,omitempty" maxLength:"255"`
}

type CreateOpaqueCredentialInput struct {
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           CreateOpaqueCredentialInputBody
}

type CreateTransitKeyInputBody struct {
	Name KeyName `json:"name" required:"true" minLength:"1" maxLength:"255"`
}

type CreateTransitKeyInput struct {
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           CreateTransitKeyInputBody
}

type ListOpaqueCredentialsInput struct {
	Kind  CredentialKind  `query:"kind" minLength:"1" maxLength:"128"`
	Limit SecretListLimit `query:"limit" minimum:"1" maximum:"200"`
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

type OpaqueCredentialMaterialDTO struct {
	Credential OpaqueCredentialDTO `json:"credential" required:"true"`
	Token      SecretValue         `json:"token" required:"true" minLength:"1" maxLength:"65536"`
}

type OpaqueCredentialPathInput struct {
	CredentialID CredentialID `path:"credential_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
}

type OpaqueCredentialsDTO struct {
	Credentials OpaqueCredentialDTOs `json:"credentials" required:"true"`
}

type ResolveSecretsInputBody struct {
	Branch     *Branch          `json:"branch,omitempty" maxLength:"255"`
	EnvID      *EnvID           `json:"env_id,omitempty" maxLength:"255"`
	Limit      *SecretListLimit `json:"limit,omitempty" minimum:"1" maximum:"200"`
	Names      *SecretNames     `json:"names,omitempty" minLength:"1" maxLength:"200"`
	ScopeLevel *ScopeLevel      `json:"scope_level,omitempty" minLength:"1" maxLength:"128"`
	SourceID   *SourceID        `json:"source_id,omitempty" maxLength:"255"`
}

type ResolveSecretsInput struct {
	Body ResolveSecretsInputBody
}

type ResolvedSecretsDTO struct {
	Values SecretValueDTOs `json:"values" required:"true"`
}

type RevokeOpaqueCredentialInput struct {
	CredentialID   CredentialID   `path:"credential_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
}

type RollOpaqueCredentialInputBody struct {
	ExpiresInSeconds *CredentialExpiresInSeconds `json:"expires_in_seconds,omitempty" minimum:"60" maximum:"7776000"`
}

type RollOpaqueCredentialInput struct {
	CredentialID   CredentialID   `path:"credential_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           RollOpaqueCredentialInputBody
}

type SecretDTO struct {
	SecretID       string        `json:"secret_id" required:"true"`
	ResourceName   *ResourceName `json:"resourceName,omitempty" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	Kind           string        `json:"kind" required:"true"`
	Name           SecretName    `json:"name" required:"true" minLength:"1" maxLength:"255"`
	ScopeLevel     ScopeLevel    `json:"scope_level" required:"true" minLength:"1" maxLength:"128"`
	SourceID       *SourceID     `json:"source_id,omitempty" maxLength:"255"`
	EnvID          *EnvID        `json:"env_id,omitempty" maxLength:"255"`
	Branch         *Branch       `json:"branch,omitempty" maxLength:"255"`
	CurrentVersion SecretVersion `json:"current_version" required:"true" pattern:"^[0-9]+$"`
	CreatedAt      string        `json:"created_at" required:"true"`
	UpdatedAt      string        `json:"updated_at" required:"true"`
}

type SecretDeleteInput struct {
	Branch         Branch         `query:"branch" maxLength:"255"`
	EnvID          EnvID          `query:"env_id" maxLength:"255"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Name           SecretName     `path:"name" required:"true" minLength:"1" maxLength:"255"`
	ScopeLevel     ScopeLevel     `query:"scope_level" minLength:"1" maxLength:"128"`
	SourceID       SourceID       `query:"source_id" maxLength:"255"`
}

type SecretListInput struct {
	Limit SecretListLimit `query:"limit" minimum:"1" maximum:"200"`
}

type SecretMutationInputBody struct {
	Branch     *Branch     `json:"branch,omitempty" maxLength:"255"`
	EnvID      *EnvID      `json:"env_id,omitempty" maxLength:"255"`
	ScopeLevel *ScopeLevel `json:"scope_level,omitempty" minLength:"1" maxLength:"128"`
	SourceID   *SourceID   `json:"source_id,omitempty" maxLength:"255"`
	Value      SecretValue `json:"value" required:"true" minLength:"1" maxLength:"65536"`
}

type SecretMutationInput struct {
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Name           SecretName     `path:"name" required:"true" minLength:"1" maxLength:"255"`
	Body           SecretMutationInputBody
}

type SecretPathInput struct {
	Branch     Branch     `query:"branch" maxLength:"255"`
	EnvID      EnvID      `query:"env_id" maxLength:"255"`
	Name       SecretName `path:"name" required:"true" minLength:"1" maxLength:"255"`
	ScopeLevel ScopeLevel `query:"scope_level" minLength:"1" maxLength:"128"`
	SourceID   SourceID   `query:"source_id" maxLength:"255"`
}

type SecretValueDTO struct {
	Branch         *Branch       `json:"branch,omitempty" maxLength:"255"`
	CreatedAt      string        `json:"created_at" required:"true"`
	CurrentVersion SecretVersion `json:"current_version" required:"true" pattern:"^[0-9]+$"`
	EnvID          *EnvID        `json:"env_id,omitempty" maxLength:"255"`
	Kind           string        `json:"kind" required:"true"`
	Name           SecretName    `json:"name" required:"true" minLength:"1" maxLength:"255"`
	ResourceName   *ResourceName `json:"resourceName,omitempty" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	ScopeLevel     ScopeLevel    `json:"scope_level" required:"true" minLength:"1" maxLength:"128"`
	SecretID       string        `json:"secret_id" required:"true"`
	SourceID       *SourceID     `json:"source_id,omitempty" maxLength:"255"`
	UpdatedAt      string        `json:"updated_at" required:"true"`
	Value          SecretValue   `json:"value" required:"true" minLength:"1" maxLength:"65536"`
}

type SecretsDTO struct {
	Secrets SecretDTOs `json:"secrets" required:"true"`
}

type TransitDecryptInputBody struct {
	Ciphertext Ciphertext `json:"ciphertext" required:"true" minLength:"1" maxLength:"262144"`
}

type TransitDecryptInput struct {
	KeyName KeyName `path:"key_name" required:"true" minLength:"1" maxLength:"255"`
	Body    TransitDecryptInputBody
}

type TransitEncryptInputBody struct {
	PlaintextBase64 PlaintextBase64 `json:"plaintext_base64" required:"true" minLength:"1" maxLength:"262144"`
}

type TransitEncryptInput struct {
	KeyName KeyName `path:"key_name" required:"true" minLength:"1" maxLength:"255"`
	Body    TransitEncryptInputBody
}

type TransitKeyDTO struct {
	CreatedAt      string        `json:"created_at" required:"true"`
	CurrentVersion SecretVersion `json:"current_version" required:"true" pattern:"^[0-9]+$"`
	KeyID          string        `json:"key_id" required:"true"`
	Name           KeyName       `json:"name" required:"true" minLength:"1" maxLength:"255"`
	PublicKey      string        `json:"public_key" required:"true"`
	ResourceName   *ResourceName `json:"resourceName,omitempty" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	UpdatedAt      string        `json:"updated_at" required:"true"`
}

type TransitKeyIdempotentInput struct {
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	KeyName        KeyName        `path:"key_name" required:"true" minLength:"1" maxLength:"255"`
}

type TransitSignInputBody struct {
	MessageBase64 MessageBase64 `json:"message_base64" required:"true" minLength:"1" maxLength:"262144"`
}

type TransitSignInput struct {
	KeyName KeyName `path:"key_name" required:"true" minLength:"1" maxLength:"255"`
	Body    TransitSignInputBody
}

type TransitVerifyInputBody struct {
	MessageBase64 MessageBase64 `json:"message_base64" required:"true" minLength:"1" maxLength:"262144"`
	Signature     Signature     `json:"signature" required:"true" minLength:"1" maxLength:"262144"`
}

type TransitVerifyInput struct {
	KeyName KeyName `path:"key_name" required:"true" minLength:"1" maxLength:"255"`
	Body    TransitVerifyInputBody
}

type VariableDTO struct {
	Branch         *Branch       `json:"branch,omitempty" maxLength:"255"`
	CreatedAt      string        `json:"created_at" required:"true"`
	CurrentVersion SecretVersion `json:"current_version" required:"true" pattern:"^[0-9]+$"`
	EnvID          *EnvID        `json:"env_id,omitempty" maxLength:"255"`
	Kind           string        `json:"kind" required:"true"`
	Name           SecretName    `json:"name" required:"true" minLength:"1" maxLength:"255"`
	ResourceName   *ResourceName `json:"resourceName,omitempty" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	ScopeLevel     ScopeLevel    `json:"scope_level" required:"true" minLength:"1" maxLength:"128"`
	SourceID       *SourceID     `json:"source_id,omitempty" maxLength:"255"`
	UpdatedAt      string        `json:"updated_at" required:"true"`
	VariableID     string        `json:"variable_id" required:"true"`
}

type VariableValueDTO struct {
	Branch         *Branch       `json:"branch,omitempty" maxLength:"255"`
	CreatedAt      string        `json:"created_at" required:"true"`
	CurrentVersion SecretVersion `json:"current_version" required:"true" pattern:"^[0-9]+$"`
	EnvID          *EnvID        `json:"env_id,omitempty" maxLength:"255"`
	Kind           string        `json:"kind" required:"true"`
	Name           SecretName    `json:"name" required:"true" minLength:"1" maxLength:"255"`
	ResourceName   *ResourceName `json:"resourceName,omitempty" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	ScopeLevel     ScopeLevel    `json:"scope_level" required:"true" minLength:"1" maxLength:"128"`
	SourceID       *SourceID     `json:"source_id,omitempty" maxLength:"255"`
	UpdatedAt      string        `json:"updated_at" required:"true"`
	Value          SecretValue   `json:"value" required:"true" minLength:"1" maxLength:"65536"`
	VariableID     string        `json:"variable_id" required:"true"`
}

type VariablesDTO struct {
	Variables VariableDTOs `json:"variables" required:"true"`
}

type OpaqueCredentialsOutput struct {
	Body OpaqueCredentialsDTO
}

type OpaqueCredentialMaterialOutput struct {
	Body OpaqueCredentialMaterialDTO
}

type OpaqueCredentialOutput struct {
	Body OpaqueCredentialDTO
}

type SecretsOutput struct {
	Body SecretsDTO
}

type SecretOutput struct {
	Body SecretDTO
}

type SecretValueOutput struct {
	Body SecretValueDTO
}

type ResolvedSecretsOutput struct {
	Body ResolvedSecretsDTO
}

type TransitKeyOutput struct {
	Body TransitKeyDTO
}

type TransitPlaintextOutputBody struct {
	PlaintextBase64 PlaintextBase64 `json:"plaintext_base64" required:"true" minLength:"1" maxLength:"262144"`
}

type TransitPlaintextOutput struct {
	Body TransitPlaintextOutputBody
}

type TransitCiphertextOutputBody struct {
	Ciphertext Ciphertext    `json:"ciphertext" required:"true" minLength:"1" maxLength:"262144"`
	Version    SecretVersion `json:"version" required:"true" pattern:"^[0-9]+$"`
}

type TransitCiphertextOutput struct {
	Body TransitCiphertextOutputBody
}

type TransitSignatureOutputBody struct {
	Signature Signature `json:"signature" required:"true" minLength:"1" maxLength:"262144"`
}

type TransitSignatureOutput struct {
	Body TransitSignatureOutputBody
}

type TransitVerifyOutputBody struct {
	Valid bool `json:"valid" required:"true"`
}

type TransitVerifyOutput struct {
	Body TransitVerifyOutputBody
}

type VariablesOutput struct {
	Body VariablesDTO
}

type VariableOutput struct {
	Body VariableDTO
}

type VariableValueOutput struct {
	Body VariableValueDTO
}

var Operations = []OperationDescriptor{
	ListOpaqueCredentials.Descriptor,
	CreateOpaqueCredential.Descriptor,
	GetOpaqueCredential.Descriptor,
	RevokeOpaqueCredential.Descriptor,
	RollOpaqueCredential.Descriptor,
	ListSecrets.Descriptor,
	DeleteSecret.Descriptor,
	ReadSecret.Descriptor,
	PutSecret.Descriptor,
	ResolveSecrets.Descriptor,
	CreateTransitKey.Descriptor,
	DecryptWithTransitKey.Descriptor,
	EncryptWithTransitKey.Descriptor,
	RotateTransitKey.Descriptor,
	SignWithTransitKey.Descriptor,
	VerifyWithTransitKey.Descriptor,
	ListVariables.Descriptor,
	DeleteVariable.Descriptor,
	ReadVariable.Descriptor,
	PutVariable.Descriptor,
}

var ListOpaqueCredentials = Operation[ListOpaqueCredentialsInput, OpaqueCredentialsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#ListOpaqueCredentials",
		OperationID:         "list-opaque-credentials",
		Method:              "GET",
		Path:                "/api/v1/credentials",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "secrets-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:credential:list", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "secrets.credential.list", Resource: "opaque_credential", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "secrets.credentials", Method: "list", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var CreateOpaqueCredential = Operation[CreateOpaqueCredentialInput, OpaqueCredentialMaterialOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#CreateOpaqueCredential",
		OperationID:         "create-opaque-credential",
		Method:              "POST",
		Path:                "/api/v1/credentials",
		DefaultStatus:       201,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "secrets-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:credential:create", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "secrets.credential.create", Resource: "opaque_credential", Action: "create"},
		RateLimitBucket:     "credential_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "secrets.credentials", Method: "create", Paginated: false, Retryable: false},
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

var GetOpaqueCredential = Operation[OpaqueCredentialPathInput, OpaqueCredentialOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#GetOpaqueCredential",
		OperationID:         "get-opaque-credential",
		Method:              "GET",
		Path:                "/api/v1/credentials/{credential_id}",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "secrets-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:credential:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "secrets.credential.read", Resource: "opaque_credential", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "secrets.credentials", Method: "get", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var RevokeOpaqueCredential = Operation[RevokeOpaqueCredentialInput, OpaqueCredentialOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#RevokeOpaqueCredential",
		OperationID:         "revoke-opaque-credential",
		Method:              "POST",
		Path:                "/api/v1/credentials/{credential_id}/revoke",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "secrets-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:credential:revoke", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "secrets.credential.revoke", Resource: "opaque_credential", Action: "revoke"},
		RateLimitBucket:     "credential_mutation",
		RequestBodyMaxBytes: 1024,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "secrets.credentials", Method: "revoke", Paginated: false, Retryable: false},
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

var RollOpaqueCredential = Operation[RollOpaqueCredentialInput, OpaqueCredentialMaterialOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#RollOpaqueCredential",
		OperationID:         "roll-opaque-credential",
		Method:              "POST",
		Path:                "/api/v1/credentials/{credential_id}/roll",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "secrets-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:credential:roll", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "secrets.credential.roll", Resource: "opaque_credential", Action: "roll"},
		RateLimitBucket:     "credential_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "secrets.credentials", Method: "roll", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
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

var ListSecrets = Operation[SecretListInput, SecretsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#ListSecrets",
		OperationID:         "list-secrets",
		Method:              "GET",
		Path:                "/api/v1/secrets",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "secrets-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:secret:list", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "secrets.secret.list", Resource: "secret", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "secrets", Method: "list", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var DeleteSecret = Operation[SecretDeleteInput, SecretOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#DeleteSecret",
		OperationID:         "delete-secret",
		Method:              "DELETE",
		Path:                "/api/v1/secrets/{name}",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "secrets-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:secret:delete", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "secrets.secret.delete", Resource: "secret", Action: "delete"},
		RateLimitBucket:     "secret_mutation",
		RequestBodyMaxBytes: 1024,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "secrets", Method: "delete", Paginated: false, Retryable: false},
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

var ReadSecret = Operation[SecretPathInput, SecretValueOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#ReadSecret",
		OperationID:         "read-secret",
		Method:              "GET",
		Path:                "/api/v1/secrets/{name}",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "secrets-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:secret:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "secrets.secret.read", Resource: "secret", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "secrets", Method: "read", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var PutSecret = Operation[SecretMutationInput, SecretOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#PutSecret",
		OperationID:         "put-secret",
		Method:              "PUT",
		Path:                "/api/v1/secrets/{name}",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "secrets-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:secret:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "secrets.secret.write", Resource: "secret", Action: "write"},
		RateLimitBucket:     "secret_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "secrets", Method: "put", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var ResolveSecrets = Operation[ResolveSecretsInput, ResolvedSecretsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#ResolveSecrets",
		OperationID:         "resolve-secrets",
		Method:              "POST",
		Path:                "/api/v1/secrets:resolve",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "secrets-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:secret:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "secrets.secret.resolve", Resource: "secret", Action: "resolve"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "secrets", Method: "resolve", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var CreateTransitKey = Operation[CreateTransitKeyInput, TransitKeyOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#CreateTransitKey",
		OperationID:         "create-transit-key",
		Method:              "POST",
		Path:                "/api/v1/transit/keys",
		DefaultStatus:       201,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "secrets-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:transit_key:create", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "secrets.transit_key.create", Resource: "transit_key", Action: "create"},
		RateLimitBucket:     "key_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "secrets.transitKeys", Method: "create", Paginated: false, Retryable: false},
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

var DecryptWithTransitKey = Operation[TransitDecryptInput, TransitPlaintextOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#DecryptWithTransitKey",
		OperationID:         "decrypt-with-transit-key",
		Method:              "POST",
		Path:                "/api/v1/transit/keys/{key_name}/decrypt",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "secrets-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:transit:decrypt", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "secrets.transit_key.decrypt", Resource: "transit_key", Action: "decrypt"},
		RateLimitBucket:     "crypto",
		RequestBodyMaxBytes: 262144,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "secrets.transit", Method: "decrypt", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var EncryptWithTransitKey = Operation[TransitEncryptInput, TransitCiphertextOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#EncryptWithTransitKey",
		OperationID:         "encrypt-with-transit-key",
		Method:              "POST",
		Path:                "/api/v1/transit/keys/{key_name}/encrypt",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "secrets-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:transit:encrypt", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "secrets.transit_key.encrypt", Resource: "transit_key", Action: "encrypt"},
		RateLimitBucket:     "crypto",
		RequestBodyMaxBytes: 262144,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "secrets.transit", Method: "encrypt", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var RotateTransitKey = Operation[TransitKeyIdempotentInput, TransitKeyOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#RotateTransitKey",
		OperationID:         "rotate-transit-key",
		Method:              "POST",
		Path:                "/api/v1/transit/keys/{key_name}/rotate",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "secrets-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:transit_key:rotate", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "secrets.transit_key.rotate", Resource: "transit_key", Action: "rotate"},
		RateLimitBucket:     "key_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "secrets.transitKeys", Method: "rotate", Paginated: false, Retryable: false},
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

var SignWithTransitKey = Operation[TransitSignInput, TransitSignatureOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#SignWithTransitKey",
		OperationID:         "sign-with-transit-key",
		Method:              "POST",
		Path:                "/api/v1/transit/keys/{key_name}/sign",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "secrets-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:transit:sign", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "secrets.transit_key.sign", Resource: "transit_key", Action: "sign"},
		RateLimitBucket:     "crypto",
		RequestBodyMaxBytes: 262144,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "secrets.transit", Method: "sign", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var VerifyWithTransitKey = Operation[TransitVerifyInput, TransitVerifyOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#VerifyWithTransitKey",
		OperationID:         "verify-with-transit-key",
		Method:              "POST",
		Path:                "/api/v1/transit/keys/{key_name}/verify",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "secrets-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:transit:verify", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "secrets.transit_key.verify", Resource: "transit_key", Action: "verify"},
		RateLimitBucket:     "crypto",
		RequestBodyMaxBytes: 262144,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "secrets.transit", Method: "verify", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var ListVariables = Operation[SecretListInput, VariablesOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#ListVariables",
		OperationID:         "list-variables",
		Method:              "GET",
		Path:                "/api/v1/variables",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "secrets-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:variable:list", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "secrets.variable.list", Resource: "variable", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "variables", Method: "list", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var DeleteVariable = Operation[SecretDeleteInput, VariableOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#DeleteVariable",
		OperationID:         "delete-variable",
		Method:              "DELETE",
		Path:                "/api/v1/variables/{name}",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "secrets-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:variable:delete", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "secrets.variable.delete", Resource: "variable", Action: "delete"},
		RateLimitBucket:     "secret_mutation",
		RequestBodyMaxBytes: 1024,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "variables", Method: "delete", Paginated: false, Retryable: false},
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

var ReadVariable = Operation[SecretPathInput, VariableValueOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#ReadVariable",
		OperationID:         "read-variable",
		Method:              "GET",
		Path:                "/api/v1/variables/{name}",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "secrets-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:variable:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "secrets.variable.read", Resource: "variable", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "variables", Method: "read", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var PutVariable = Operation[SecretMutationInput, VariableOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.secrets.v1#PutVariable",
		OperationID:         "put-variable",
		Method:              "PUT",
		Path:                "/api/v1/variables/{name}",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "secrets-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "secrets:variable:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "secrets.variable.write", Resource: "variable", Action: "write"},
		RateLimitBucket:     "secret_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "variables", Method: "put", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

type Handlers = PublicHandlers

type PublicHandlers interface {
	ListOpaqueCredentials(context.Context, *ListOpaqueCredentialsInput) (*OpaqueCredentialsOutput, error)
	CreateOpaqueCredential(context.Context, *CreateOpaqueCredentialInput) (*OpaqueCredentialMaterialOutput, error)
	GetOpaqueCredential(context.Context, *OpaqueCredentialPathInput) (*OpaqueCredentialOutput, error)
	RevokeOpaqueCredential(context.Context, *RevokeOpaqueCredentialInput) (*OpaqueCredentialOutput, error)
	RollOpaqueCredential(context.Context, *RollOpaqueCredentialInput) (*OpaqueCredentialMaterialOutput, error)
	ListSecrets(context.Context, *SecretListInput) (*SecretsOutput, error)
	DeleteSecret(context.Context, *SecretDeleteInput) (*SecretOutput, error)
	ReadSecret(context.Context, *SecretPathInput) (*SecretValueOutput, error)
	PutSecret(context.Context, *SecretMutationInput) (*SecretOutput, error)
	ResolveSecrets(context.Context, *ResolveSecretsInput) (*ResolvedSecretsOutput, error)
	CreateTransitKey(context.Context, *CreateTransitKeyInput) (*TransitKeyOutput, error)
	DecryptWithTransitKey(context.Context, *TransitDecryptInput) (*TransitPlaintextOutput, error)
	EncryptWithTransitKey(context.Context, *TransitEncryptInput) (*TransitCiphertextOutput, error)
	RotateTransitKey(context.Context, *TransitKeyIdempotentInput) (*TransitKeyOutput, error)
	SignWithTransitKey(context.Context, *TransitSignInput) (*TransitSignatureOutput, error)
	VerifyWithTransitKey(context.Context, *TransitVerifyInput) (*TransitVerifyOutput, error)
	ListVariables(context.Context, *SecretListInput) (*VariablesOutput, error)
	DeleteVariable(context.Context, *SecretDeleteInput) (*VariableOutput, error)
	ReadVariable(context.Context, *SecretPathInput) (*VariableValueOutput, error)
	PutVariable(context.Context, *SecretMutationInput) (*VariableOutput, error)
}

type ListOpaqueCredentialsHandler = Handler[ListOpaqueCredentialsInput, OpaqueCredentialsOutput]

type CreateOpaqueCredentialHandler = Handler[CreateOpaqueCredentialInput, OpaqueCredentialMaterialOutput]

type GetOpaqueCredentialHandler = Handler[OpaqueCredentialPathInput, OpaqueCredentialOutput]

type RevokeOpaqueCredentialHandler = Handler[RevokeOpaqueCredentialInput, OpaqueCredentialOutput]

type RollOpaqueCredentialHandler = Handler[RollOpaqueCredentialInput, OpaqueCredentialMaterialOutput]

type ListSecretsHandler = Handler[SecretListInput, SecretsOutput]

type DeleteSecretHandler = Handler[SecretDeleteInput, SecretOutput]

type ReadSecretHandler = Handler[SecretPathInput, SecretValueOutput]

type PutSecretHandler = Handler[SecretMutationInput, SecretOutput]

type ResolveSecretsHandler = Handler[ResolveSecretsInput, ResolvedSecretsOutput]

type CreateTransitKeyHandler = Handler[CreateTransitKeyInput, TransitKeyOutput]

type DecryptWithTransitKeyHandler = Handler[TransitDecryptInput, TransitPlaintextOutput]

type EncryptWithTransitKeyHandler = Handler[TransitEncryptInput, TransitCiphertextOutput]

type RotateTransitKeyHandler = Handler[TransitKeyIdempotentInput, TransitKeyOutput]

type SignWithTransitKeyHandler = Handler[TransitSignInput, TransitSignatureOutput]

type VerifyWithTransitKeyHandler = Handler[TransitVerifyInput, TransitVerifyOutput]

type ListVariablesHandler = Handler[SecretListInput, VariablesOutput]

type DeleteVariableHandler = Handler[SecretDeleteInput, VariableOutput]

type ReadVariableHandler = Handler[SecretPathInput, VariableValueOutput]

type PutVariableHandler = Handler[SecretMutationInput, VariableOutput]
