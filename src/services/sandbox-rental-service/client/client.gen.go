// Code generated from the Smithy API contract. DO NOT EDIT.

package sandboxrentalclient

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

const ServiceName = "sandbox-rental-service"

type OrgId = string

type ProblemCode = string

type ProblemDetail = string

type ProblemType = string

type ProjectId = string

type Provider = string

type ProviderOwner = string

type ProviderRepo = string

type ProviderRepositoryId = string

type RepositoryFullName = string

type RequestId = string

type SourceRepositoryId = string

type TraceParent = string

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

type RunnerRepositoryRegistration struct {
	ProjectID            ProjectId            `json:"project_id"`
	Provider             Provider             `json:"provider"`
	ProviderRepositoryID ProviderRepositoryId `json:"provider_repository_id"`
	SourceRepositoryID   *SourceRepositoryId  `json:"source_repository_id,omitempty"`
	State                string               `json:"state"`
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

type InternalRegisterRunnerRepositoryInputBody struct {
	OrgID                OrgId                `json:"org_id"`
	ProjectID            ProjectId            `json:"project_id"`
	Provider             Provider             `json:"provider"`
	ProviderOwner        ProviderOwner        `json:"provider_owner"`
	ProviderRepo         ProviderRepo         `json:"provider_repo"`
	ProviderRepositoryID ProviderRepositoryId `json:"provider_repository_id"`
	RepositoryFullName   *RepositoryFullName  `json:"repository_full_name,omitempty"`
	SourceRepositoryID   *SourceRepositoryId  `json:"source_repository_id,omitempty"`
}

type InternalRegisterRunnerRepositoryRequest struct {
	Body InternalRegisterRunnerRepositoryInputBody `json:"body"`
}

type InternalRegisterRunnerRepositoryOutputBody struct {
	Registration RunnerRepositoryRegistration `json:"registration"`
}

type InternalRegisterRunnerRepositoryResponse struct {
	StatusCode   int
	Body         []byte
	Result       *InternalRegisterRunnerRepositoryOutputBody
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

func (c *Client) InternalRegisterRunnerRepository(ctx context.Context, request InternalRegisterRunnerRepositoryRequest, reqEditors ...RequestEditorFn) (*InternalRegisterRunnerRepositoryResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newInternalRegisterRunnerRepositoryRequest(ctx, request)
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
	return parseInternalRegisterRunnerRepositoryResponse(resp)
}

func (c *Client) newInternalRegisterRunnerRepositoryRequest(ctx context.Context, request InternalRegisterRunnerRepositoryRequest) (*http.Request, error) {
	path := "/internal/v1/runner/repositories"
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

func parseInternalRegisterRunnerRepositoryResponse(resp *http.Response) (*InternalRegisterRunnerRepositoryResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &InternalRegisterRunnerRepositoryResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 201 {
		var decoded InternalRegisterRunnerRepositoryOutputBody
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
