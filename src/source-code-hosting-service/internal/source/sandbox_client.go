package source

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	sandboxclient "github.com/forge-metal/sandbox-rental-service/internalclient"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var sandboxTracer = otel.Tracer("source-code-hosting-service/sandbox")

type SandboxClient struct {
	Client *sandboxclient.ClientWithResponses
}

func NewSandboxClient(baseURL string, httpClient sandboxclient.HttpRequestDoer) (SandboxClient, error) {
	client, err := sandboxclient.NewClientWithResponses(strings.TrimRight(baseURL, "/"), sandboxclient.WithHTTPClient(httpClient))
	if err != nil {
		return SandboxClient{}, err
	}
	return SandboxClient{Client: client}, nil
}

func (c SandboxClient) SubmitSourceCIRun(ctx context.Context, repo Repository, run CIRun) (_ SandboxCISubmission, err error) {
	ctx, span := sandboxTracer.Start(ctx, "source.sandbox.ci_submit")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if c.Client == nil {
		return SandboxCISubmission{}, ErrSandbox
	}
	span.SetAttributes(
		attribute.Int64("forge_metal.org_id", int64(run.OrgID)),
		attribute.String("forge_metal.subject_id", run.ActorID),
		attribute.String("source.repo_id", repo.RepoID.String()),
		attribute.String("source.ci_run_id", run.CIRunID.String()),
		attribute.String("source.ref_name", run.RefName),
	)
	resp, err := c.Client.InternalSubmitSourceCiRunWithResponse(ctx, sandboxclient.InternalSubmitSourceCiRunJSONRequestBody{
		OrgId:          fmt.Sprintf("%d", run.OrgID),
		ActorId:        run.ActorID,
		RepoId:         repo.RepoID.String(),
		CiRunId:        run.CIRunID.String(),
		RefName:        run.RefName,
		CommitSha:      run.CommitSHA,
		IdempotencyKey: "source-ci:" + run.CIRunID.String(),
		RunCommand:     stringPtr(DefaultSourceCIRunCommand),
	})
	if err != nil {
		return SandboxCISubmission{}, fmt.Errorf("%w: submit source ci run: %v", ErrSandbox, err)
	}
	if resp.JSON201 == nil {
		status := 0
		body := ""
		if resp.HTTPResponse != nil {
			status = resp.HTTPResponse.StatusCode
			body = strings.TrimSpace(string(resp.Body))
		}
		if status == http.StatusConflict {
			return SandboxCISubmission{}, fmt.Errorf("%w: sandbox source ci conflict: %s", ErrSandbox, body)
		}
		return SandboxCISubmission{}, fmt.Errorf("%w: sandbox source ci unexpected status %d: %s", ErrSandbox, status, body)
	}
	span.SetAttributes(
		attribute.String("sandbox.execution_id", resp.JSON201.ExecutionId),
		attribute.String("sandbox.attempt_id", resp.JSON201.AttemptId),
	)
	executionID, err := uuid.Parse(resp.JSON201.ExecutionId)
	if err != nil {
		return SandboxCISubmission{}, fmt.Errorf("%w: sandbox returned invalid execution_id: %v", ErrSandbox, err)
	}
	attemptID, err := uuid.Parse(resp.JSON201.AttemptId)
	if err != nil {
		return SandboxCISubmission{}, fmt.Errorf("%w: sandbox returned invalid attempt_id: %v", ErrSandbox, err)
	}
	return SandboxCISubmission{
		ExecutionID: executionID,
		AttemptID:   attemptID,
	}, nil
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
