package sourceworkflow

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	sourceclient "github.com/verself/source-code-hosting-service/internalclient"

	"github.com/verself/sandbox-rental-service/internal/recurring"
)

var tracer = otel.Tracer("sandbox-rental-service/sourceworkflow")

type Dispatcher struct {
	client *sourceclient.ClientWithResponses
}

func NewDispatcher(baseURL string, httpClient sourceclient.HttpRequestDoer) (*Dispatcher, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, errors.New("source internal base URL is required")
	}
	if httpClient == nil {
		return nil, errors.New("source internal HTTP client is required")
	}
	client, err := sourceclient.NewClientWithResponses(baseURL, sourceclient.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("create source internal client: %w", err)
	}
	return &Dispatcher{client: client}, nil
}

func (d *Dispatcher) DispatchWorkflow(ctx context.Context, req recurring.WorkflowDispatchRequest) (_ recurring.WorkflowDispatchResult, err error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.source.workflow.dispatch", trace.WithSpanKind(trace.SpanKindClient))
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if d == nil || d.client == nil {
		return recurring.WorkflowDispatchResult{}, errors.New("source workflow dispatcher is not configured")
	}
	span.SetAttributes(
		attribute.String("verself.project_id", req.ProjectID.String()),
		attribute.String("source.repo_id", req.SourceRepositoryID.String()),
		attribute.String("source.workflow_path", req.WorkflowPath),
		attribute.String("source.ref", req.Ref),
	)
	body := sourceclient.InternalCreateSourceWorkflowRunJSONRequestBody{
		OrgId:          strconv.FormatUint(req.OrgID, 10),
		ActorId:        strings.TrimSpace(req.ActorID),
		ProjectId:      req.ProjectID.String(),
		RepoId:         req.SourceRepositoryID.String(),
		WorkflowPath:   strings.TrimSpace(req.WorkflowPath),
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
	}
	ref := strings.TrimSpace(req.Ref)
	if ref != "" {
		body.Ref = &ref
	}
	if req.Inputs != nil {
		inputs := make(map[string]string, len(req.Inputs))
		for key, value := range req.Inputs {
			inputs[key] = value
		}
		body.Inputs = &inputs
	}
	resp, err := d.client.InternalCreateSourceWorkflowRunWithResponse(ctx, body)
	if err != nil {
		return recurring.WorkflowDispatchResult{}, fmt.Errorf("dispatch source workflow: %w", err)
	}
	if resp == nil || resp.HTTPResponse == nil {
		return recurring.WorkflowDispatchResult{}, errors.New("dispatch source workflow: missing response")
	}
	span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode()))
	if resp.StatusCode() != http.StatusCreated || resp.JSON201 == nil {
		return recurring.WorkflowDispatchResult{}, fmt.Errorf("dispatch source workflow status %d: %s", resp.StatusCode(), strings.TrimSpace(string(resp.Body)))
	}
	if resp.JSON201.State != "dispatched" {
		return recurring.WorkflowDispatchResult{}, fmt.Errorf("dispatch source workflow returned state %q", resp.JSON201.State)
	}
	workflowRunID, err := uuid.Parse(resp.JSON201.WorkflowRunId)
	if err != nil {
		return recurring.WorkflowDispatchResult{}, fmt.Errorf("parse source workflow run id: %w", err)
	}
	span.SetAttributes(
		attribute.String("source.workflow_run_id", workflowRunID.String()),
		attribute.String("source.workflow_state", resp.JSON201.State),
	)
	return recurring.WorkflowDispatchResult{
		WorkflowRunID: workflowRunID,
		State:         resp.JSON201.State,
	}, nil
}

func (d *Dispatcher) ResolveRepository(ctx context.Context, orgID uint64, repoID uuid.UUID) (_ recurring.SourceRepositoryReference, err error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.source.repo.resolve", trace.WithSpanKind(trace.SpanKindClient))
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if d == nil || d.client == nil {
		return recurring.SourceRepositoryReference{}, errors.New("source workflow dispatcher is not configured")
	}
	if orgID == 0 || repoID == uuid.Nil {
		return recurring.SourceRepositoryReference{}, recurring.ErrInvalid
	}
	span.SetAttributes(
		attribute.Int64("verself.org_id", int64(orgID)),
		attribute.String("source.repo_id", repoID.String()),
	)
	resp, err := d.client.InternalResolveSourceRepositoryWithResponse(ctx, sourceclient.InternalResolveRepositoryRequest{
		OrgId:  strconv.FormatUint(orgID, 10),
		RepoId: repoID.String(),
	})
	if err != nil {
		return recurring.SourceRepositoryReference{}, fmt.Errorf("resolve source repository: %w", err)
	}
	if resp == nil || resp.HTTPResponse == nil {
		return recurring.SourceRepositoryReference{}, errors.New("resolve source repository: missing response")
	}
	span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode()))
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return recurring.SourceRepositoryReference{}, fmt.Errorf("resolve source repository status %d: %s", resp.StatusCode(), strings.TrimSpace(string(resp.Body)))
	}
	orgValue, err := strconv.ParseUint(strings.TrimSpace(resp.JSON200.OrgId), 10, 64)
	if err != nil {
		return recurring.SourceRepositoryReference{}, fmt.Errorf("parse source repository org id: %w", err)
	}
	if orgValue == 0 {
		return recurring.SourceRepositoryReference{}, errors.New("parse source repository org id: zero value")
	}
	projectID, err := uuid.Parse(resp.JSON200.ProjectId)
	if err != nil {
		return recurring.SourceRepositoryReference{}, fmt.Errorf("parse source repository project id: %w", err)
	}
	resolvedRepoID, err := uuid.Parse(resp.JSON200.RepoId)
	if err != nil {
		return recurring.SourceRepositoryReference{}, fmt.Errorf("parse source repository id: %w", err)
	}
	if resolvedRepoID != repoID {
		return recurring.SourceRepositoryReference{}, fmt.Errorf("resolve source repository returned repo %s for requested repo %s", resolvedRepoID, repoID)
	}
	span.SetAttributes(attribute.String("verself.project_id", projectID.String()))
	return recurring.SourceRepositoryReference{
		RepoID:    resolvedRepoID,
		OrgID:     orgValue,
		ProjectID: projectID,
	}, nil
}
