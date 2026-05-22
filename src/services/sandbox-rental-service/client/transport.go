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

type AttemptId = string

type CorrelationId = string

type DecimalUint64 = string

type ExecutionId = string

type GenerationSetHash = string

type HeadBranch = string

type HeadSha = string

type JobName = string

type JobShapeId = string

type RunnerBootstrapKind = string

type RunnerBootstrapPayload = string

type RunnerName = string

type PromotionPolicy = string

type WorkflowName = string

type WorkflowPath = string

type TrustClass = string

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

type RunnerLabels []string

type RunnerJobObservation struct {
	Provider               Provider             `json:"provider"`
	ProviderJobID          DecimalUint64        `json:"provider_job_id"`
	ProviderRepositoryID   ProviderRepositoryId `json:"provider_repository_id"`
	ProviderInstallationID *DecimalUint64       `json:"provider_installation_id,omitempty"`
	RepositoryFullName     *RepositoryFullName  `json:"repository_full_name,omitempty"`
	ProviderRunID          *DecimalUint64       `json:"provider_run_id,omitempty"`
	ProviderRunAttempt     *DecimalUint64       `json:"provider_run_attempt,omitempty"`
	JobName                *JobName             `json:"job_name,omitempty"`
	HeadSHA                *HeadSha             `json:"head_sha,omitempty"`
	HeadBranch             *HeadBranch          `json:"head_branch,omitempty"`
	WorkflowName           *WorkflowName        `json:"workflow_name,omitempty"`
	Status                 string               `json:"status"`
	Conclusion             *string              `json:"conclusion,omitempty"`
	Labels                 RunnerLabels         `json:"labels,omitempty"`
	RunnerID               *DecimalUint64       `json:"runner_id,omitempty"`
	RunnerName             *RunnerName          `json:"runner_name,omitempty"`
	StartedAt              *string              `json:"started_at,omitempty"`
	CompletedAt            *string              `json:"completed_at,omitempty"`
	DeliveryID             *CorrelationId       `json:"delivery_id,omitempty"`
}

type RunnerWorkflowRunObservation struct {
	Provider               Provider             `json:"provider"`
	ProviderRepositoryID   ProviderRepositoryId `json:"provider_repository_id"`
	ProviderInstallationID *DecimalUint64       `json:"provider_installation_id,omitempty"`
	ProviderRunID          *DecimalUint64       `json:"provider_run_id,omitempty"`
	ProviderRunAttempt     *DecimalUint64       `json:"provider_run_attempt,omitempty"`
	RepositoryFullName     *RepositoryFullName  `json:"repository_full_name,omitempty"`
	EventName              *string              `json:"event_name,omitempty"`
	HeadSHA                *HeadSha             `json:"head_sha,omitempty"`
	HeadBranch             *HeadBranch          `json:"head_branch,omitempty"`
	HeadRepositoryFullName *RepositoryFullName  `json:"head_repository_full_name,omitempty"`
	BaseSHA                *HeadSha             `json:"base_sha,omitempty"`
	BaseBranch             *HeadBranch          `json:"base_branch,omitempty"`
	WorkflowPath           *WorkflowPath        `json:"workflow_path,omitempty"`
	PullRequestNumber      *DecimalUint64       `json:"pull_request_number,omitempty"`
	CommitCount            *DecimalUint64       `json:"commit_count,omitempty"`
}

type RunnerCacheManifest struct {
	SourceKind    string                           `json:"source_kind"`
	SourcePath    string                           `json:"source_path"`
	SourceSHA     string                           `json:"source_sha"`
	ContentBase64 RunnerCacheManifestContentBase64 `json:"content_base64"`
}

type RunnerCacheManifestContentBase64 = string

type InternalSubmitRunnerJobInputBody struct {
	Observation      RunnerJobObservation          `json:"observation"`
	RunnerName       RunnerName                    `json:"runner_name"`
	RunnerID         *DecimalUint64                `json:"runner_id,omitempty"`
	BootstrapKind    RunnerBootstrapKind           `json:"bootstrap_kind"`
	BootstrapPayload RunnerBootstrapPayload        `json:"bootstrap_payload"`
	WorkflowRun      *RunnerWorkflowRunObservation `json:"workflow_run,omitempty"`
	CacheManifest    *RunnerCacheManifest          `json:"cache_manifest,omitempty"`
}

type InternalSubmitRunnerJobRequest struct {
	Body InternalSubmitRunnerJobInputBody `json:"body"`
}

type InternalSubmitRunnerJobOutputBody struct {
	AllocationID AttemptId      `json:"allocation_id"`
	ExecutionID  ExecutionId    `json:"execution_id"`
	AttemptID    AttemptId      `json:"attempt_id"`
	RunnerName   RunnerName     `json:"runner_name"`
	RunnerID     *DecimalUint64 `json:"runner_id,omitempty"`
	Created      bool           `json:"created"`
}

type InternalSubmitRunnerJobResponse struct {
	StatusCode   int
	Body         []byte
	Result       *InternalSubmitRunnerJobOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type InternalObserveRunnerJobInputBody struct {
	Observation RunnerJobObservation          `json:"observation"`
	WorkflowRun *RunnerWorkflowRunObservation `json:"workflow_run,omitempty"`
}

type InternalObserveRunnerJobRequest struct {
	Body InternalObserveRunnerJobInputBody `json:"body"`
}

type InternalObserveRunnerJobOutputBody struct {
	State        string       `json:"state"`
	AllocationID *AttemptId   `json:"allocation_id,omitempty"`
	ExecutionID  *ExecutionId `json:"execution_id,omitempty"`
	AttemptID    *AttemptId   `json:"attempt_id,omitempty"`
}

type InternalObserveRunnerJobResponse struct {
	StatusCode   int
	Body         []byte
	Result       *InternalObserveRunnerJobOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type InternalObserveRunnerWorkflowRunInputBody struct {
	WorkflowRun RunnerWorkflowRunObservation `json:"workflow_run"`
}

type InternalObserveRunnerWorkflowRunRequest struct {
	Body InternalObserveRunnerWorkflowRunInputBody `json:"body"`
}

type InternalObserveRunnerWorkflowRunOutputBody struct {
	State string `json:"state"`
}

type InternalObserveRunnerWorkflowRunResponse struct {
	StatusCode   int
	Body         []byte
	Result       *InternalObserveRunnerWorkflowRunOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type GoldenSnapshotBarrierEvidence struct {
	Provider                 Provider             `json:"provider"`
	ProviderInstallationID   *DecimalUint64       `json:"provider_installation_id,omitempty"`
	ProviderRepositoryID     ProviderRepositoryId `json:"provider_repository_id"`
	ProviderRunID            DecimalUint64        `json:"provider_run_id"`
	ProviderRunAttempt       DecimalUint64        `json:"provider_run_attempt"`
	ProviderJobID            DecimalUint64        `json:"provider_job_id"`
	RepositoryFullName       *RepositoryFullName  `json:"repository_full_name,omitempty"`
	HeadSHA                  *HeadSha             `json:"head_sha,omitempty"`
	Conclusion               *string              `json:"conclusion,omitempty"`
	RunnerID                 *DecimalUint64       `json:"runner_id,omitempty"`
	RunnerName               *RunnerName          `json:"runner_name,omitempty"`
	ExecutionID              ExecutionId          `json:"execution_id"`
	AttemptID                AttemptId            `json:"attempt_id"`
	LeaseID                  *string              `json:"lease_id,omitempty"`
	JobShapeID               *JobShapeId          `json:"job_shape_id,omitempty"`
	DurableGenerationSetHash *GenerationSetHash   `json:"durable_generation_set_hash,omitempty"`
	TrustClass               *TrustClass          `json:"trust_class,omitempty"`
	PromotionPolicy          *PromotionPolicy     `json:"promotion_policy,omitempty"`
}

type InternalRequestGoldenSnapshotBarrierInputBody struct {
	Evidence GoldenSnapshotBarrierEvidence `json:"evidence"`
}

type InternalRequestGoldenSnapshotBarrierRequest struct {
	Body InternalRequestGoldenSnapshotBarrierInputBody `json:"body"`
}

type InternalRequestGoldenSnapshotBarrierOutputBody struct {
	State  string  `json:"state"`
	Reason *string `json:"reason,omitempty"`
}

type InternalRequestGoldenSnapshotBarrierResponse struct {
	StatusCode   int
	Body         []byte
	Result       *InternalRequestGoldenSnapshotBarrierOutputBody
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

func (c *Client) InternalSubmitRunnerJob(ctx context.Context, request InternalSubmitRunnerJobRequest, reqEditors ...RequestEditorFn) (*InternalSubmitRunnerJobResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newJSONRequest(ctx, "POST", "/internal/v1/runner/jobs", request.Body)
	if err != nil {
		return nil, err
	}
	if err := c.applyEditors(ctx, req, reqEditors...); err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseInternalSubmitRunnerJobResponse(resp)
}

func (c *Client) InternalObserveRunnerJob(ctx context.Context, request InternalObserveRunnerJobRequest, reqEditors ...RequestEditorFn) (*InternalObserveRunnerJobResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newJSONRequest(ctx, "POST", "/internal/v1/runner/jobs/observations", request.Body)
	if err != nil {
		return nil, err
	}
	if err := c.applyEditors(ctx, req, reqEditors...); err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseInternalObserveRunnerJobResponse(resp)
}

func (c *Client) InternalObserveRunnerWorkflowRun(ctx context.Context, request InternalObserveRunnerWorkflowRunRequest, reqEditors ...RequestEditorFn) (*InternalObserveRunnerWorkflowRunResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newJSONRequest(ctx, "POST", "/internal/v1/runner/workflow-runs/observations", request.Body)
	if err != nil {
		return nil, err
	}
	if err := c.applyEditors(ctx, req, reqEditors...); err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseInternalObserveRunnerWorkflowRunResponse(resp)
}

func (c *Client) InternalRequestGoldenSnapshotBarrier(ctx context.Context, request InternalRequestGoldenSnapshotBarrierRequest, reqEditors ...RequestEditorFn) (*InternalRequestGoldenSnapshotBarrierResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newJSONRequest(ctx, "POST", "/internal/v1/golden-snapshot-barriers", request.Body)
	if err != nil {
		return nil, err
	}
	if err := c.applyEditors(ctx, req, reqEditors...); err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseInternalRequestGoldenSnapshotBarrierResponse(resp)
}

func (c *Client) newInternalRegisterRunnerRepositoryRequest(ctx context.Context, request InternalRegisterRunnerRepositoryRequest) (*http.Request, error) {
	return c.newJSONRequest(ctx, "POST", "/internal/v1/runner/repositories", request.Body)
}

func (c *Client) newJSONRequest(ctx context.Context, method string, path string, body any) (*http.Request, error) {
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (c *Client) applyEditors(ctx context.Context, req *http.Request, reqEditors ...RequestEditorFn) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseInternalRegisterRunnerRepositoryResponse(resp *http.Response) (*InternalRegisterRunnerRepositoryResponse, error) {
	defer func() { _ = resp.Body.Close() }()
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

func parseInternalSubmitRunnerJobResponse(resp *http.Response) (*InternalSubmitRunnerJobResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &InternalSubmitRunnerJobResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 201 {
		var decoded InternalSubmitRunnerJobOutputBody
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

func parseInternalObserveRunnerJobResponse(resp *http.Response) (*InternalObserveRunnerJobResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &InternalObserveRunnerJobResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 202 {
		var decoded InternalObserveRunnerJobOutputBody
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

func parseInternalObserveRunnerWorkflowRunResponse(resp *http.Response) (*InternalObserveRunnerWorkflowRunResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &InternalObserveRunnerWorkflowRunResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 202 {
		var decoded InternalObserveRunnerWorkflowRunOutputBody
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

func parseInternalRequestGoldenSnapshotBarrierResponse(resp *http.Response) (*InternalRequestGoldenSnapshotBarrierResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &InternalRequestGoldenSnapshotBarrierResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 202 {
		var decoded InternalRequestGoldenSnapshotBarrierOutputBody
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
