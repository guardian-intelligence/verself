package secretsinternalclient

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

type CredentialDisplayName = string

type CredentialId = string

type CredentialKind = string

type CredentialScope = string

type CredentialStatus = string

type EnvId = string

type InjectionAttemptId = string

type InjectionEnvName = string

type InjectionExecutionId = string

type InjectionGrantId = string

type OrgId = string

type ProblemCode = string

type ProblemDetail = string

type ProblemType = string

type RequestId = string

type ResourceName = string

type ScopeLevel = string

type SecretName = string

type SecretValue = string

type SecretVersion = string

type SourceId = string

type SubjectId = string

type TraceParent = string

type CredentialScopes []CredentialScope

type InjectionEnvValues []InjectionEnvValue

type InjectionSecretRequests []InjectionSecretRequest

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

type InjectionEnvValue struct {
	Name  InjectionEnvName `json:"name"`
	Value SecretValue      `json:"value"`
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

type InjectionSecretRequest struct {
	Branch     *Branch          `json:"branch,omitempty"`
	EnvID      *EnvId           `json:"env_id,omitempty"`
	EnvName    InjectionEnvName `json:"env_name"`
	GrantID    InjectionGrantId `json:"grant_id"`
	Kind       *string          `json:"kind,omitempty"`
	ScopeLevel *ScopeLevel      `json:"scope_level,omitempty"`
	SecretName SecretName       `json:"secret_name"`
	SourceID   *SourceId        `json:"source_id,omitempty"`
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

type InternalOpaqueCredentialInputBody struct {
	ActorID     SubjectId              `json:"actor_id"`
	DisplayName *CredentialDisplayName `json:"display_name,omitempty"`
	ExpiresAt   *string                `json:"expires_at,omitempty"`
	Kind        CredentialKind         `json:"kind"`
	Metadata    *CredentialMetadata    `json:"metadata,omitempty"`
	OrgID       OrgId                  `json:"org_id"`
	Scopes      CredentialScopes       `json:"scopes"`
}

type CreateInternalOpaqueCredentialRequest struct {
	Body InternalOpaqueCredentialInputBody `json:"body"`
}

type InternalOpaqueCredentialOutputBody struct {
	Credential OpaqueCredentialDTO `json:"credential"`
	Token      SecretValue         `json:"token"`
}

type CreateInternalOpaqueCredentialResponse struct {
	StatusCode   int
	Body         []byte
	Result       *InternalOpaqueCredentialOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type InjectionResolveInputBody struct {
	ActorID     SubjectId               `json:"actor_id"`
	AttemptID   InjectionAttemptId      `json:"attempt_id"`
	ExecutionID InjectionExecutionId    `json:"execution_id"`
	OrgID       OrgId                   `json:"org_id"`
	Secrets     InjectionSecretRequests `json:"secrets"`
}

type ResolveInjectionRequest struct {
	Body InjectionResolveInputBody `json:"body"`
}

type InjectionResolveOutputBody struct {
	Env InjectionEnvValues `json:"env"`
}

type ResolveInjectionResponse struct {
	StatusCode   int
	Body         []byte
	Result       *InjectionResolveOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type GetRuntimeSecretRequest struct {
	SecretName SecretName
}

type GetRuntimeSecretResponse struct {
	StatusCode   int
	Body         []byte
	Result       *SecretValueDTO
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type VerifyInternalOpaqueCredentialInputBody struct {
	ActorID        *SubjectId        `json:"actor_id,omitempty"`
	Kind           CredentialKind    `json:"kind"`
	OrgID          OrgId             `json:"org_id"`
	RequiredScopes *CredentialScopes `json:"required_scopes,omitempty"`
	Token          SecretValue       `json:"token"`
}

type VerifyInternalOpaqueCredentialRequest struct {
	Body VerifyInternalOpaqueCredentialInputBody `json:"body"`
}

type VerifyInternalOpaqueCredentialOutputBody struct {
	Active       bool                 `json:"active"`
	Credential   *OpaqueCredentialDTO `json:"credential,omitempty"`
	DenialReason *string              `json:"denial_reason,omitempty"`
}

type VerifyInternalOpaqueCredentialResponse struct {
	StatusCode   int
	Body         []byte
	Result       *VerifyInternalOpaqueCredentialOutputBody
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

func (c *Client) CreateInternalOpaqueCredential(ctx context.Context, request CreateInternalOpaqueCredentialRequest, reqEditors ...RequestEditorFn) (*CreateInternalOpaqueCredentialResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newCreateInternalOpaqueCredentialRequest(ctx, request)
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
	return parseCreateInternalOpaqueCredentialResponse(resp)
}

func (c *Client) newCreateInternalOpaqueCredentialRequest(ctx context.Context, request CreateInternalOpaqueCredentialRequest) (*http.Request, error) {
	path := "/internal/v1/credentials"
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

func (c *Client) ResolveInjection(ctx context.Context, request ResolveInjectionRequest, reqEditors ...RequestEditorFn) (*ResolveInjectionResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newResolveInjectionRequest(ctx, request)
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
	return parseResolveInjectionResponse(resp)
}

func (c *Client) newResolveInjectionRequest(ctx context.Context, request ResolveInjectionRequest) (*http.Request, error) {
	path := "/internal/v1/injections/resolve"
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

func (c *Client) VerifyInternalOpaqueCredential(ctx context.Context, request VerifyInternalOpaqueCredentialRequest, reqEditors ...RequestEditorFn) (*VerifyInternalOpaqueCredentialResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newVerifyInternalOpaqueCredentialRequest(ctx, request)
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
	return parseVerifyInternalOpaqueCredentialResponse(resp)
}

func (c *Client) GetRuntimeSecret(ctx context.Context, request GetRuntimeSecretRequest, reqEditors ...RequestEditorFn) (*GetRuntimeSecretResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newGetRuntimeSecretRequest(ctx, request)
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
	return parseGetRuntimeSecretResponse(resp)
}

func (c *Client) newGetRuntimeSecretRequest(ctx context.Context, request GetRuntimeSecretRequest) (*http.Request, error) {
	secretName := strings.TrimSpace(string(request.SecretName))
	if secretName == "" {
		return nil, fmt.Errorf("runtime secret name is required")
	}
	endpoint, err := url.Parse(c.server + "/api/v1/secrets/" + url.PathEscape(secretName))
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("kind", "secret")
	query.Set("scope_level", "org")
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) newVerifyInternalOpaqueCredentialRequest(ctx context.Context, request VerifyInternalOpaqueCredentialRequest) (*http.Request, error) {
	path := "/internal/v1/credentials:verify"
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

func parseCreateInternalOpaqueCredentialResponse(resp *http.Response) (*CreateInternalOpaqueCredentialResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &CreateInternalOpaqueCredentialResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 201 {
		var decoded InternalOpaqueCredentialOutputBody
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

func parseResolveInjectionResponse(resp *http.Response) (*ResolveInjectionResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ResolveInjectionResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded InjectionResolveOutputBody
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

func parseVerifyInternalOpaqueCredentialResponse(resp *http.Response) (*VerifyInternalOpaqueCredentialResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &VerifyInternalOpaqueCredentialResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded VerifyInternalOpaqueCredentialOutputBody
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

func parseGetRuntimeSecretResponse(resp *http.Response) (*GetRuntimeSecretResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &GetRuntimeSecretResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
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
