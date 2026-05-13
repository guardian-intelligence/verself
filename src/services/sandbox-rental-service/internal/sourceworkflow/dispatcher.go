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

	sourceclient "github.com/verself/source-code-hosting-service/client"

	"github.com/verself/sandbox-rental-service/internal/recurring"
)

var tracer = otel.Tracer("sandbox-rental-service/sourceworkflow")

type Dispatcher struct {
	client *sourceclient.Client
}

func NewDispatcher(baseURL string, httpClient sourceclient.HTTPRequestDoer) (*Dispatcher, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, errors.New("source internal base URL is required")
	}
	if httpClient == nil {
		return nil, errors.New("source internal HTTP client is required")
	}
	client, err := sourceclient.NewClient(baseURL, sourceclient.WithHTTPClient(httpClient))
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
	body := sourceclient.InternalCreateSourceWorkflowRunInputBody{
		OrgID:          strconv.FormatUint(req.OrgID, 10),
		ActorID:        strings.TrimSpace(req.ActorID),
		ProjectID:      req.ProjectID.String(),
		RepoID:         req.SourceRepositoryID.String(),
		WorkflowPath:   strings.TrimSpace(req.WorkflowPath),
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
	}
	ref := strings.TrimSpace(req.Ref)
	if ref != "" {
		body.Ref = &ref
	}
	if req.Inputs != nil {
		inputs := make(sourceclient.WorkflowInputs, len(req.Inputs))
		for key, value := range req.Inputs {
			inputs[key] = sourceclient.WorkflowInputValue(value)
		}
		body.Inputs = &inputs
	}
	resp, err := d.client.InternalCreateSourceWorkflowRun(ctx, sourceclient.InternalCreateSourceWorkflowRunRequest{Body: body})
	if err != nil {
		return recurring.WorkflowDispatchResult{}, fmt.Errorf("dispatch source workflow: %w", err)
	}
	if resp == nil || resp.HTTPResponse == nil {
		return recurring.WorkflowDispatchResult{}, errors.New("dispatch source workflow: missing response")
	}
	span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode))
	if resp.StatusCode != http.StatusCreated || resp.Result == nil {
		return recurring.WorkflowDispatchResult{}, fmt.Errorf("dispatch source workflow status %d: %s", resp.StatusCode, strings.TrimSpace(string(resp.Body)))
	}
	if resp.Result.State != "dispatched" {
		return recurring.WorkflowDispatchResult{}, fmt.Errorf("dispatch source workflow returned state %q", resp.Result.State)
	}
	workflowRunID, err := uuid.Parse(resp.Result.WorkflowRunID)
	if err != nil {
		return recurring.WorkflowDispatchResult{}, fmt.Errorf("parse source workflow run id: %w", err)
	}
	span.SetAttributes(
		attribute.String("source.workflow_run_id", workflowRunID.String()),
		attribute.String("source.workflow_state", resp.Result.State),
	)
	return recurring.WorkflowDispatchResult{
		WorkflowRunID: workflowRunID,
		State:         resp.Result.State,
	}, nil
}
