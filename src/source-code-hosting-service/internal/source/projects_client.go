package source

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	projectsclient "github.com/verself/projects-service/internalclient"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var projectsTracer = otel.Tracer("source-code-hosting-service/projects")

type ProjectsClient struct {
	Client *projectsclient.ClientWithResponses
}

func NewProjectsClient(baseURL string, httpClient projectsclient.HttpRequestDoer) (ProjectsClient, error) {
	client, err := projectsclient.NewClientWithResponses(strings.TrimRight(baseURL, "/"), projectsclient.WithHTTPClient(httpClient))
	if err != nil {
		return ProjectsClient{}, err
	}
	return ProjectsClient{Client: client}, nil
}

func (c ProjectsClient) ResolveSourceProject(ctx context.Context, orgID uint64, projectID uuid.UUID) (err error) {
	ctx, span := projectsTracer.Start(ctx, "source.projects.resolve")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if c.Client == nil {
		return ErrStoreUnavailable
	}
	if orgID == 0 || projectID == uuid.Nil {
		return ErrInvalid
	}
	span.SetAttributes(
		attribute.Int64("verself.org_id", int64(orgID)),
		attribute.String("verself.project_id", projectID.String()),
	)
	resp, err := c.Client.ResolveProjectWithResponse(ctx, projectsclient.ResolveProjectJSONRequestBody{
		OrgId:         strconv.FormatUint(orgID, 10),
		ProjectId:     &projectID,
		RequireActive: true,
	})
	if err != nil {
		return fmt.Errorf("%w: resolve project: %v", ErrStoreUnavailable, err)
	}
	if resp.JSON200 == nil {
		status := 0
		body := ""
		if resp.HTTPResponse != nil {
			status = resp.HTTPResponse.StatusCode
			body = strings.TrimSpace(string(resp.Body))
		}
		switch status {
		case http.StatusNotFound:
			return ErrNotFound
		case http.StatusConflict, http.StatusBadRequest:
			return ErrInvalid
		default:
			return fmt.Errorf("%w: resolve project unexpected status %d: %s", ErrStoreUnavailable, status, body)
		}
	}
	return nil
}
