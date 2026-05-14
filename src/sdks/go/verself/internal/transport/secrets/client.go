package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const ServiceName = "secrets-service"

type Branch = string

type Ciphertext = string

type CredentialDisplayName = string

type CredentialExpiresInSeconds = int64

type CredentialId = string

type CredentialKind = string

type CredentialScope = string

type CredentialStatus = string

type CredentialSubject = string

type EnvId = string

type IdempotencyKey = string

type KeyName = string

type MessageBase64 = string

type OrgId = string

type PlaintextBase64 = string

type ProblemCode = string

type ProblemDetail = string

type ProblemType = string

type RequestId = string

type ResourceName = string

type ScopeLevel = string

type SecretListLimit = int64

type SecretName = string

type SecretValue = string

type SecretVersion = string

type Signature = string

type SourceId = string

type TraceParent = string

type CredentialScopes []CredentialScope

type OpaqueCredentialDTOs []OpaqueCredentialDTO

type SecretDTOs []SecretDTO

type SecretNames []SecretName

type SecretValueDTOs []SecretValueDTO

type VariableDTOs []VariableDTO

type CredentialMetadata map[string]string

type ConflictError struct {
	Type        ProblemType    `json:"type"`
	Title       string         `json:"title"`
	Status      int64          `json:"status"`
	Detail      *ProblemDetail `json:"detail,omitempty"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code"`
	RequestID   *RequestId     `json:"requestId,omitempty"`
	Traceparent *TraceParent   `json:"traceparent,omitempty"`
}

type IdempotencyPayloadMismatchError struct {
	Type        ProblemType    `json:"type"`
	Title       string         `json:"title"`
	Status      int64          `json:"status"`
	Detail      *ProblemDetail `json:"detail,omitempty"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code"`
	RequestID   *RequestId     `json:"requestId,omitempty"`
	Traceparent *TraceParent   `json:"traceparent,omitempty"`
}

type OpaqueCredentialDTO struct {
	CreatedAt      string             `json:"created_at"`
	CredentialID   CredentialId       `json:"credential_id"`
	CurrentVersion SecretVersion      `json:"current_version"`
	DisplayName    string             `json:"display_name"`
	ExpiresAt      string             `json:"expires_at"`
	Kind           CredentialKind     `json:"kind"`
	LastUsedAt     *string            `json:"last_used_at,omitempty"`
	Metadata       CredentialMetadata `json:"metadata"`
	OrgID          OrgId              `json:"org_id"`
	ResourceName   *ResourceName      `json:"resourceName,omitempty"`
	RevokedAt      *string            `json:"revoked_at,omitempty"`
	Scopes         CredentialScopes   `json:"scopes"`
	Status         CredentialStatus   `json:"status"`
	Subject        string             `json:"subject"`
	TokenPrefix    string             `json:"token_prefix"`
	UpdatedAt      string             `json:"updated_at"`
}

type OpaqueCredentialMaterialDTO struct {
	Credential OpaqueCredentialDTO `json:"credential"`
	Token      SecretValue         `json:"token"`
}

type OpaqueCredentialsDTO struct {
	Credentials OpaqueCredentialDTOs `json:"credentials"`
}

type PermissionDeniedError struct {
	Type        ProblemType    `json:"type"`
	Title       string         `json:"title"`
	Status      int64          `json:"status"`
	Detail      *ProblemDetail `json:"detail,omitempty"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code"`
	RequestID   *RequestId     `json:"requestId,omitempty"`
	Traceparent *TraceParent   `json:"traceparent,omitempty"`
}

type RateLimitedError struct {
	Type        ProblemType    `json:"type"`
	Title       string         `json:"title"`
	Status      int64          `json:"status"`
	Detail      *ProblemDetail `json:"detail,omitempty"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code"`
	RequestID   *RequestId     `json:"requestId,omitempty"`
	Traceparent *TraceParent   `json:"traceparent,omitempty"`
}

type ResolvedSecretsDTO struct {
	Values SecretValueDTOs `json:"values"`
}

type ResourceNotFoundError struct {
	Type        ProblemType    `json:"type"`
	Title       string         `json:"title"`
	Status      int64          `json:"status"`
	Detail      *ProblemDetail `json:"detail,omitempty"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code"`
	RequestID   *RequestId     `json:"requestId,omitempty"`
	Traceparent *TraceParent   `json:"traceparent,omitempty"`
}

type SecretDTO struct {
	SecretID       string        `json:"secret_id"`
	ResourceName   *ResourceName `json:"resourceName,omitempty"`
	Kind           string        `json:"kind"`
	Name           SecretName    `json:"name"`
	ScopeLevel     ScopeLevel    `json:"scope_level"`
	SourceID       *SourceId     `json:"source_id,omitempty"`
	EnvID          *EnvId        `json:"env_id,omitempty"`
	Branch         *Branch       `json:"branch,omitempty"`
	CurrentVersion SecretVersion `json:"current_version"`
	CreatedAt      string        `json:"created_at"`
	UpdatedAt      string        `json:"updated_at"`
}

type SecretValueDTO struct {
	Branch         *Branch       `json:"branch,omitempty"`
	CreatedAt      string        `json:"created_at"`
	CurrentVersion SecretVersion `json:"current_version"`
	EnvID          *EnvId        `json:"env_id,omitempty"`
	Kind           string        `json:"kind"`
	Name           SecretName    `json:"name"`
	ResourceName   *ResourceName `json:"resourceName,omitempty"`
	ScopeLevel     ScopeLevel    `json:"scope_level"`
	SecretID       string        `json:"secret_id"`
	SourceID       *SourceId     `json:"source_id,omitempty"`
	UpdatedAt      string        `json:"updated_at"`
	Value          SecretValue   `json:"value"`
}

type SecretsDTO struct {
	Secrets SecretDTOs `json:"secrets"`
}

type ServiceUnavailableError struct {
	Type        ProblemType    `json:"type"`
	Title       string         `json:"title"`
	Status      int64          `json:"status"`
	Detail      *ProblemDetail `json:"detail,omitempty"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code"`
	RequestID   *RequestId     `json:"requestId,omitempty"`
	Traceparent *TraceParent   `json:"traceparent,omitempty"`
}

type TransitKeyDTO struct {
	CreatedAt      string        `json:"created_at"`
	CurrentVersion SecretVersion `json:"current_version"`
	KeyID          string        `json:"key_id"`
	Name           KeyName       `json:"name"`
	PublicKey      string        `json:"public_key"`
	ResourceName   *ResourceName `json:"resourceName,omitempty"`
	UpdatedAt      string        `json:"updated_at"`
}

type UnauthenticatedError struct {
	Type        ProblemType    `json:"type"`
	Title       string         `json:"title"`
	Status      int64          `json:"status"`
	Detail      *ProblemDetail `json:"detail,omitempty"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code"`
	RequestID   *RequestId     `json:"requestId,omitempty"`
	Traceparent *TraceParent   `json:"traceparent,omitempty"`
}

type ValidationFailedError struct {
	Type        ProblemType    `json:"type"`
	Title       string         `json:"title"`
	Status      int64          `json:"status"`
	Detail      *ProblemDetail `json:"detail,omitempty"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code"`
	RequestID   *RequestId     `json:"requestId,omitempty"`
	Traceparent *TraceParent   `json:"traceparent,omitempty"`
}

type VariableDTO struct {
	Branch         *Branch       `json:"branch,omitempty"`
	CreatedAt      string        `json:"created_at"`
	CurrentVersion SecretVersion `json:"current_version"`
	EnvID          *EnvId        `json:"env_id,omitempty"`
	Kind           string        `json:"kind"`
	Name           SecretName    `json:"name"`
	ResourceName   *ResourceName `json:"resourceName,omitempty"`
	ScopeLevel     ScopeLevel    `json:"scope_level"`
	SourceID       *SourceId     `json:"source_id,omitempty"`
	UpdatedAt      string        `json:"updated_at"`
	VariableID     string        `json:"variable_id"`
}

type VariableValueDTO struct {
	Branch         *Branch       `json:"branch,omitempty"`
	CreatedAt      string        `json:"created_at"`
	CurrentVersion SecretVersion `json:"current_version"`
	EnvID          *EnvId        `json:"env_id,omitempty"`
	Kind           string        `json:"kind"`
	Name           SecretName    `json:"name"`
	ResourceName   *ResourceName `json:"resourceName,omitempty"`
	ScopeLevel     ScopeLevel    `json:"scope_level"`
	SourceID       *SourceId     `json:"source_id,omitempty"`
	UpdatedAt      string        `json:"updated_at"`
	Value          SecretValue   `json:"value"`
	VariableID     string        `json:"variable_id"`
}

type VariablesDTO struct {
	Variables VariableDTOs `json:"variables"`
}

type CreateOpaqueCredentialInputBody struct {
	DisplayName      *CredentialDisplayName      `json:"display_name,omitempty"`
	ExpiresInSeconds *CredentialExpiresInSeconds `json:"expires_in_seconds,omitempty"`
	Kind             CredentialKind              `json:"kind"`
	Metadata         *CredentialMetadata         `json:"metadata,omitempty"`
	Scopes           CredentialScopes            `json:"scopes"`
	Subject          *CredentialSubject          `json:"subject,omitempty"`
}

type CreateOpaqueCredentialRequest struct {
	IdempotencyKey IdempotencyKey
	Body           CreateOpaqueCredentialInputBody `json:"body"`
}

type CreateOpaqueCredentialResponse struct {
	StatusCode   int
	Body         []byte
	Result       *OpaqueCredentialMaterialDTO
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type CreateTransitKeyInputBody struct {
	Name KeyName `json:"name"`
}

type CreateTransitKeyRequest struct {
	IdempotencyKey IdempotencyKey
	Body           CreateTransitKeyInputBody `json:"body"`
}

type CreateTransitKeyResponse struct {
	StatusCode   int
	Body         []byte
	Result       *TransitKeyDTO
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type TransitDecryptInputBody struct {
	Ciphertext Ciphertext `json:"ciphertext"`
}

type DecryptWithTransitKeyRequest struct {
	KeyName KeyName
	Body    TransitDecryptInputBody `json:"body"`
}

type TransitPlaintextOutputBody struct {
	PlaintextBase64 PlaintextBase64 `json:"plaintext_base64"`
}

type DecryptWithTransitKeyResponse struct {
	StatusCode   int
	Body         []byte
	Result       *TransitPlaintextOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type DeleteSecretRequest struct {
	Branch         *Branch
	EnvID          *EnvId
	IdempotencyKey IdempotencyKey
	Name           SecretName
	ScopeLevel     *ScopeLevel
	SourceID       *SourceId
}

type DeleteSecretResponse struct {
	StatusCode   int
	Body         []byte
	Result       *SecretDTO
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type DeleteVariableRequest struct {
	Branch         *Branch
	EnvID          *EnvId
	IdempotencyKey IdempotencyKey
	Name           SecretName
	ScopeLevel     *ScopeLevel
	SourceID       *SourceId
}

type DeleteVariableResponse struct {
	StatusCode   int
	Body         []byte
	Result       *VariableDTO
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type TransitEncryptInputBody struct {
	PlaintextBase64 PlaintextBase64 `json:"plaintext_base64"`
}

type EncryptWithTransitKeyRequest struct {
	KeyName KeyName
	Body    TransitEncryptInputBody `json:"body"`
}

type TransitCiphertextOutputBody struct {
	Ciphertext Ciphertext    `json:"ciphertext"`
	Version    SecretVersion `json:"version"`
}

type EncryptWithTransitKeyResponse struct {
	StatusCode   int
	Body         []byte
	Result       *TransitCiphertextOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type GetOpaqueCredentialRequest struct {
	CredentialID CredentialId
}

type GetOpaqueCredentialResponse struct {
	StatusCode   int
	Body         []byte
	Result       *OpaqueCredentialDTO
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ListOpaqueCredentialsRequest struct {
	Kind  *CredentialKind
	Limit *SecretListLimit
}

type ListOpaqueCredentialsResponse struct {
	StatusCode   int
	Body         []byte
	Result       *OpaqueCredentialsDTO
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ListSecretsRequest struct {
	Limit *SecretListLimit
}

type ListSecretsResponse struct {
	StatusCode   int
	Body         []byte
	Result       *SecretsDTO
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ListVariablesRequest struct {
	Limit *SecretListLimit
}

type ListVariablesResponse struct {
	StatusCode   int
	Body         []byte
	Result       *VariablesDTO
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type SecretMutationInputBody struct {
	Branch     *Branch     `json:"branch,omitempty"`
	EnvID      *EnvId      `json:"env_id,omitempty"`
	ScopeLevel *ScopeLevel `json:"scope_level,omitempty"`
	SourceID   *SourceId   `json:"source_id,omitempty"`
	Value      SecretValue `json:"value"`
}

type PutSecretRequest struct {
	IdempotencyKey IdempotencyKey
	Name           SecretName
	Body           SecretMutationInputBody `json:"body"`
}

type PutSecretResponse struct {
	StatusCode   int
	Body         []byte
	Result       *SecretDTO
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type PutVariableRequest struct {
	IdempotencyKey IdempotencyKey
	Name           SecretName
	Body           SecretMutationInputBody `json:"body"`
}

type PutVariableResponse struct {
	StatusCode   int
	Body         []byte
	Result       *VariableDTO
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ReadSecretRequest struct {
	Branch     *Branch
	EnvID      *EnvId
	Name       SecretName
	ScopeLevel *ScopeLevel
	SourceID   *SourceId
}

type ReadSecretResponse struct {
	StatusCode   int
	Body         []byte
	Result       *SecretValueDTO
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ReadVariableRequest struct {
	Branch     *Branch
	EnvID      *EnvId
	Name       SecretName
	ScopeLevel *ScopeLevel
	SourceID   *SourceId
}

type ReadVariableResponse struct {
	StatusCode   int
	Body         []byte
	Result       *VariableValueDTO
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ResolveSecretsInputBody struct {
	Branch     *Branch          `json:"branch,omitempty"`
	EnvID      *EnvId           `json:"env_id,omitempty"`
	Limit      *SecretListLimit `json:"limit,omitempty"`
	Names      *SecretNames     `json:"names,omitempty"`
	ScopeLevel *ScopeLevel      `json:"scope_level,omitempty"`
	SourceID   *SourceId        `json:"source_id,omitempty"`
}

type ResolveSecretsRequest struct {
	Body ResolveSecretsInputBody `json:"body"`
}

type ResolveSecretsResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ResolvedSecretsDTO
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type RevokeOpaqueCredentialRequest struct {
	CredentialID   CredentialId
	IdempotencyKey IdempotencyKey
}

type RevokeOpaqueCredentialResponse struct {
	StatusCode   int
	Body         []byte
	Result       *OpaqueCredentialDTO
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type RollOpaqueCredentialInputBody struct {
	ExpiresInSeconds *CredentialExpiresInSeconds `json:"expires_in_seconds,omitempty"`
}

type RollOpaqueCredentialRequest struct {
	CredentialID   CredentialId
	IdempotencyKey IdempotencyKey
	Body           RollOpaqueCredentialInputBody `json:"body"`
}

type RollOpaqueCredentialResponse struct {
	StatusCode   int
	Body         []byte
	Result       *OpaqueCredentialMaterialDTO
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type RotateTransitKeyRequest struct {
	IdempotencyKey IdempotencyKey
	KeyName        KeyName
}

type RotateTransitKeyResponse struct {
	StatusCode   int
	Body         []byte
	Result       *TransitKeyDTO
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type TransitSignInputBody struct {
	MessageBase64 MessageBase64 `json:"message_base64"`
}

type SignWithTransitKeyRequest struct {
	KeyName KeyName
	Body    TransitSignInputBody `json:"body"`
}

type TransitSignatureOutputBody struct {
	Signature Signature `json:"signature"`
}

type SignWithTransitKeyResponse struct {
	StatusCode   int
	Body         []byte
	Result       *TransitSignatureOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type TransitVerifyInputBody struct {
	MessageBase64 MessageBase64 `json:"message_base64"`
	Signature     Signature     `json:"signature"`
}

type VerifyWithTransitKeyRequest struct {
	KeyName KeyName
	Body    TransitVerifyInputBody `json:"body"`
}

type TransitVerifyOutputBody struct {
	Valid bool `json:"valid"`
}

type VerifyWithTransitKeyResponse struct {
	StatusCode   int
	Body         []byte
	Result       *TransitVerifyOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type RequestEditorFn func(ctx context.Context, req *http.Request) error

type HTTPRequestDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type ClientOption func(*Client)

type Client struct {
	server         string
	client         HTTPRequestDoer
	requestEditors []RequestEditorFn
}

func NewClient(server string, opts ...ClientOption) (*Client, error) {
	server = strings.TrimRight(strings.TrimSpace(server), "/")
	if server == "" {
		return nil, fmt.Errorf("%s SDK transport: server URL is required", ServiceName)
	}
	client := &Client{server: server, client: http.DefaultClient}
	for _, opt := range opts {
		opt(client)
	}
	if client.client == nil {
		return nil, fmt.Errorf("%s SDK transport: HTTP client is required", ServiceName)
	}
	return client, nil
}

func WithHTTPClient(client HTTPRequestDoer) ClientOption {
	return func(c *Client) { c.client = client }
}

func WithRequestEditorFn(editor RequestEditorFn) ClientOption {
	return func(c *Client) {
		if editor != nil {
			c.requestEditors = append(c.requestEditors, editor)
		}
	}
}

func (c *Client) CreateOpaqueCredential(ctx context.Context, request CreateOpaqueCredentialRequest, reqEditors ...RequestEditorFn) (*CreateOpaqueCredentialResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newCreateOpaqueCredentialRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseCreateOpaqueCredentialResponse(resp)
}

func (c *Client) newCreateOpaqueCredentialRequest(ctx context.Context, request CreateOpaqueCredentialRequest) (*http.Request, error) {
	path := "/api/v1/credentials"
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(request.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) CreateTransitKey(ctx context.Context, request CreateTransitKeyRequest, reqEditors ...RequestEditorFn) (*CreateTransitKeyResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newCreateTransitKeyRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseCreateTransitKeyResponse(resp)
}

func (c *Client) newCreateTransitKeyRequest(ctx context.Context, request CreateTransitKeyRequest) (*http.Request, error) {
	path := "/api/v1/transit/keys"
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(request.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) DecryptWithTransitKey(ctx context.Context, request DecryptWithTransitKeyRequest, reqEditors ...RequestEditorFn) (*DecryptWithTransitKeyResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newDecryptWithTransitKeyRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseDecryptWithTransitKeyResponse(resp)
}

func (c *Client) newDecryptWithTransitKeyRequest(ctx context.Context, request DecryptWithTransitKeyRequest) (*http.Request, error) {
	path := "/api/v1/transit/keys/{key_name}/decrypt"
	path = strings.ReplaceAll(path, "{key_name}", url.PathEscape(fmt.Sprint(request.KeyName)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(request.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (c *Client) DeleteSecret(ctx context.Context, request DeleteSecretRequest, reqEditors ...RequestEditorFn) (*DeleteSecretResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newDeleteSecretRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseDeleteSecretResponse(resp)
}

func (c *Client) newDeleteSecretRequest(ctx context.Context, request DeleteSecretRequest) (*http.Request, error) {
	path := "/api/v1/secrets/{name}"
	path = strings.ReplaceAll(path, "{name}", url.PathEscape(fmt.Sprint(request.Name)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	if request.Branch != nil {
		query.Set("branch", fmt.Sprint(*request.Branch))
	}
	if request.EnvID != nil {
		query.Set("env_id", fmt.Sprint(*request.EnvID))
	}
	if request.ScopeLevel != nil {
		query.Set("scope_level", fmt.Sprint(*request.ScopeLevel))
	}
	if request.SourceID != nil {
		query.Set("source_id", fmt.Sprint(*request.SourceID))
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, "DELETE", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) DeleteVariable(ctx context.Context, request DeleteVariableRequest, reqEditors ...RequestEditorFn) (*DeleteVariableResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newDeleteVariableRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseDeleteVariableResponse(resp)
}

func (c *Client) newDeleteVariableRequest(ctx context.Context, request DeleteVariableRequest) (*http.Request, error) {
	path := "/api/v1/variables/{name}"
	path = strings.ReplaceAll(path, "{name}", url.PathEscape(fmt.Sprint(request.Name)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	if request.Branch != nil {
		query.Set("branch", fmt.Sprint(*request.Branch))
	}
	if request.EnvID != nil {
		query.Set("env_id", fmt.Sprint(*request.EnvID))
	}
	if request.ScopeLevel != nil {
		query.Set("scope_level", fmt.Sprint(*request.ScopeLevel))
	}
	if request.SourceID != nil {
		query.Set("source_id", fmt.Sprint(*request.SourceID))
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, "DELETE", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) EncryptWithTransitKey(ctx context.Context, request EncryptWithTransitKeyRequest, reqEditors ...RequestEditorFn) (*EncryptWithTransitKeyResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newEncryptWithTransitKeyRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseEncryptWithTransitKeyResponse(resp)
}

func (c *Client) newEncryptWithTransitKeyRequest(ctx context.Context, request EncryptWithTransitKeyRequest) (*http.Request, error) {
	path := "/api/v1/transit/keys/{key_name}/encrypt"
	path = strings.ReplaceAll(path, "{key_name}", url.PathEscape(fmt.Sprint(request.KeyName)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(request.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (c *Client) GetOpaqueCredential(ctx context.Context, request GetOpaqueCredentialRequest, reqEditors ...RequestEditorFn) (*GetOpaqueCredentialResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newGetOpaqueCredentialRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseGetOpaqueCredentialResponse(resp)
}

func (c *Client) newGetOpaqueCredentialRequest(ctx context.Context, request GetOpaqueCredentialRequest) (*http.Request, error) {
	path := "/api/v1/credentials/{credential_id}"
	path = strings.ReplaceAll(path, "{credential_id}", url.PathEscape(fmt.Sprint(request.CredentialID)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) ListOpaqueCredentials(ctx context.Context, request ListOpaqueCredentialsRequest, reqEditors ...RequestEditorFn) (*ListOpaqueCredentialsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newListOpaqueCredentialsRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseListOpaqueCredentialsResponse(resp)
}

func (c *Client) newListOpaqueCredentialsRequest(ctx context.Context, request ListOpaqueCredentialsRequest) (*http.Request, error) {
	path := "/api/v1/credentials"
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	if request.Kind != nil {
		query.Set("kind", fmt.Sprint(*request.Kind))
	}
	if request.Limit != nil {
		query.Set("limit", fmt.Sprint(*request.Limit))
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) ListSecrets(ctx context.Context, request ListSecretsRequest, reqEditors ...RequestEditorFn) (*ListSecretsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newListSecretsRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseListSecretsResponse(resp)
}

func (c *Client) newListSecretsRequest(ctx context.Context, request ListSecretsRequest) (*http.Request, error) {
	path := "/api/v1/secrets"
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	if request.Limit != nil {
		query.Set("limit", fmt.Sprint(*request.Limit))
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) ListVariables(ctx context.Context, request ListVariablesRequest, reqEditors ...RequestEditorFn) (*ListVariablesResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newListVariablesRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseListVariablesResponse(resp)
}

func (c *Client) newListVariablesRequest(ctx context.Context, request ListVariablesRequest) (*http.Request, error) {
	path := "/api/v1/variables"
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	if request.Limit != nil {
		query.Set("limit", fmt.Sprint(*request.Limit))
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) PutSecret(ctx context.Context, request PutSecretRequest, reqEditors ...RequestEditorFn) (*PutSecretResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newPutSecretRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parsePutSecretResponse(resp)
}

func (c *Client) newPutSecretRequest(ctx context.Context, request PutSecretRequest) (*http.Request, error) {
	path := "/api/v1/secrets/{name}"
	path = strings.ReplaceAll(path, "{name}", url.PathEscape(fmt.Sprint(request.Name)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(request.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "PUT", endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) PutVariable(ctx context.Context, request PutVariableRequest, reqEditors ...RequestEditorFn) (*PutVariableResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newPutVariableRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parsePutVariableResponse(resp)
}

func (c *Client) newPutVariableRequest(ctx context.Context, request PutVariableRequest) (*http.Request, error) {
	path := "/api/v1/variables/{name}"
	path = strings.ReplaceAll(path, "{name}", url.PathEscape(fmt.Sprint(request.Name)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(request.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "PUT", endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) ReadSecret(ctx context.Context, request ReadSecretRequest, reqEditors ...RequestEditorFn) (*ReadSecretResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newReadSecretRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseReadSecretResponse(resp)
}

func (c *Client) newReadSecretRequest(ctx context.Context, request ReadSecretRequest) (*http.Request, error) {
	path := "/api/v1/secrets/{name}"
	path = strings.ReplaceAll(path, "{name}", url.PathEscape(fmt.Sprint(request.Name)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	if request.Branch != nil {
		query.Set("branch", fmt.Sprint(*request.Branch))
	}
	if request.EnvID != nil {
		query.Set("env_id", fmt.Sprint(*request.EnvID))
	}
	if request.ScopeLevel != nil {
		query.Set("scope_level", fmt.Sprint(*request.ScopeLevel))
	}
	if request.SourceID != nil {
		query.Set("source_id", fmt.Sprint(*request.SourceID))
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) ReadVariable(ctx context.Context, request ReadVariableRequest, reqEditors ...RequestEditorFn) (*ReadVariableResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newReadVariableRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseReadVariableResponse(resp)
}

func (c *Client) newReadVariableRequest(ctx context.Context, request ReadVariableRequest) (*http.Request, error) {
	path := "/api/v1/variables/{name}"
	path = strings.ReplaceAll(path, "{name}", url.PathEscape(fmt.Sprint(request.Name)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	if request.Branch != nil {
		query.Set("branch", fmt.Sprint(*request.Branch))
	}
	if request.EnvID != nil {
		query.Set("env_id", fmt.Sprint(*request.EnvID))
	}
	if request.ScopeLevel != nil {
		query.Set("scope_level", fmt.Sprint(*request.ScopeLevel))
	}
	if request.SourceID != nil {
		query.Set("source_id", fmt.Sprint(*request.SourceID))
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) ResolveSecrets(ctx context.Context, request ResolveSecretsRequest, reqEditors ...RequestEditorFn) (*ResolveSecretsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newResolveSecretsRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseResolveSecretsResponse(resp)
}

func (c *Client) newResolveSecretsRequest(ctx context.Context, request ResolveSecretsRequest) (*http.Request, error) {
	path := "/api/v1/secrets:resolve"
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(request.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (c *Client) RevokeOpaqueCredential(ctx context.Context, request RevokeOpaqueCredentialRequest, reqEditors ...RequestEditorFn) (*RevokeOpaqueCredentialResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newRevokeOpaqueCredentialRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseRevokeOpaqueCredentialResponse(resp)
}

func (c *Client) newRevokeOpaqueCredentialRequest(ctx context.Context, request RevokeOpaqueCredentialRequest) (*http.Request, error) {
	path := "/api/v1/credentials/{credential_id}/revoke"
	path = strings.ReplaceAll(path, "{credential_id}", url.PathEscape(fmt.Sprint(request.CredentialID)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) RollOpaqueCredential(ctx context.Context, request RollOpaqueCredentialRequest, reqEditors ...RequestEditorFn) (*RollOpaqueCredentialResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newRollOpaqueCredentialRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseRollOpaqueCredentialResponse(resp)
}

func (c *Client) newRollOpaqueCredentialRequest(ctx context.Context, request RollOpaqueCredentialRequest) (*http.Request, error) {
	path := "/api/v1/credentials/{credential_id}/roll"
	path = strings.ReplaceAll(path, "{credential_id}", url.PathEscape(fmt.Sprint(request.CredentialID)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(request.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) RotateTransitKey(ctx context.Context, request RotateTransitKeyRequest, reqEditors ...RequestEditorFn) (*RotateTransitKeyResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newRotateTransitKeyRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseRotateTransitKeyResponse(resp)
}

func (c *Client) newRotateTransitKeyRequest(ctx context.Context, request RotateTransitKeyRequest) (*http.Request, error) {
	path := "/api/v1/transit/keys/{key_name}/rotate"
	path = strings.ReplaceAll(path, "{key_name}", url.PathEscape(fmt.Sprint(request.KeyName)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) SignWithTransitKey(ctx context.Context, request SignWithTransitKeyRequest, reqEditors ...RequestEditorFn) (*SignWithTransitKeyResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newSignWithTransitKeyRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseSignWithTransitKeyResponse(resp)
}

func (c *Client) newSignWithTransitKeyRequest(ctx context.Context, request SignWithTransitKeyRequest) (*http.Request, error) {
	path := "/api/v1/transit/keys/{key_name}/sign"
	path = strings.ReplaceAll(path, "{key_name}", url.PathEscape(fmt.Sprint(request.KeyName)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(request.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (c *Client) VerifyWithTransitKey(ctx context.Context, request VerifyWithTransitKeyRequest, reqEditors ...RequestEditorFn) (*VerifyWithTransitKeyResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newVerifyWithTransitKeyRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseVerifyWithTransitKeyResponse(resp)
}

func (c *Client) newVerifyWithTransitKeyRequest(ctx context.Context, request VerifyWithTransitKeyRequest) (*http.Request, error) {
	path := "/api/v1/transit/keys/{key_name}/verify"
	path = strings.ReplaceAll(path, "{key_name}", url.PathEscape(fmt.Sprint(request.KeyName)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(request.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func parseCreateOpaqueCredentialResponse(resp *http.Response) (*CreateOpaqueCredentialResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &CreateOpaqueCredentialResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 201 {
		var decoded OpaqueCredentialMaterialDTO
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseCreateTransitKeyResponse(resp *http.Response) (*CreateTransitKeyResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &CreateTransitKeyResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 201 {
		var decoded TransitKeyDTO
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseDecryptWithTransitKeyResponse(resp *http.Response) (*DecryptWithTransitKeyResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &DecryptWithTransitKeyResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded TransitPlaintextOutputBody
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseDeleteSecretResponse(resp *http.Response) (*DeleteSecretResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &DeleteSecretResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded SecretDTO
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseDeleteVariableResponse(resp *http.Response) (*DeleteVariableResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &DeleteVariableResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded VariableDTO
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseEncryptWithTransitKeyResponse(resp *http.Response) (*EncryptWithTransitKeyResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &EncryptWithTransitKeyResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded TransitCiphertextOutputBody
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseGetOpaqueCredentialResponse(resp *http.Response) (*GetOpaqueCredentialResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &GetOpaqueCredentialResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded OpaqueCredentialDTO
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseListOpaqueCredentialsResponse(resp *http.Response) (*ListOpaqueCredentialsResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ListOpaqueCredentialsResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded OpaqueCredentialsDTO
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseListSecretsResponse(resp *http.Response) (*ListSecretsResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ListSecretsResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded SecretsDTO
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseListVariablesResponse(resp *http.Response) (*ListVariablesResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ListVariablesResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded VariablesDTO
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parsePutSecretResponse(resp *http.Response) (*PutSecretResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &PutSecretResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded SecretDTO
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parsePutVariableResponse(resp *http.Response) (*PutVariableResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &PutVariableResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded VariableDTO
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseReadSecretResponse(resp *http.Response) (*ReadSecretResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ReadSecretResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded SecretValueDTO
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseReadVariableResponse(resp *http.Response) (*ReadVariableResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ReadVariableResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded VariableValueDTO
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseResolveSecretsResponse(resp *http.Response) (*ResolveSecretsResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ResolveSecretsResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ResolvedSecretsDTO
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseRevokeOpaqueCredentialResponse(resp *http.Response) (*RevokeOpaqueCredentialResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &RevokeOpaqueCredentialResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded OpaqueCredentialDTO
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseRollOpaqueCredentialResponse(resp *http.Response) (*RollOpaqueCredentialResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &RollOpaqueCredentialResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded OpaqueCredentialMaterialDTO
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseRotateTransitKeyResponse(resp *http.Response) (*RotateTransitKeyResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &RotateTransitKeyResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded TransitKeyDTO
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseSignWithTransitKeyResponse(resp *http.Response) (*SignWithTransitKeyResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &SignWithTransitKeyResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded TransitSignatureOutputBody
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseVerifyWithTransitKeyResponse(resp *http.Response) (*VerifyWithTransitKeyResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &VerifyWithTransitKeyResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded TransitVerifyOutputBody
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

type ErrorModel struct {
	Schema      *string          `json:"$schema,omitempty"`
	Type        *string          `json:"type,omitempty"`
	Title       *string          `json:"title,omitempty"`
	Status      *int64           `json:"status,omitempty"`
	Detail      *string          `json:"detail,omitempty"`
	Instance    *string          `json:"instance,omitempty"`
	Code        *string          `json:"code,omitempty"`
	RequestID   *string          `json:"requestId,omitempty"`
	Traceparent *string          `json:"traceparent,omitempty"`
	Errors      []map[string]any `json:"errors,omitempty"`
}

func decodeProblem(body []byte) *ErrorModel {
	if len(body) == 0 {
		return nil
	}
	var problem ErrorModel
	if err := json.Unmarshal(body, &problem); err != nil {
		return nil
	}
	return &problem
}
