package source

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	projectsclient "github.com/verself/projects-service/client"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var projectsTracer = otel.Tracer("source-code-hosting-service/projects")

type ProjectsClient struct {
	Client *projectsclient.Client
}

func NewProjectsClient(baseURL string, httpClient projectsclient.HTTPRequestDoer) (ProjectsClient, error) {
	client, err := projectsclient.NewClient(strings.TrimRight(baseURL, "/"), projectsclient.WithHTTPClient(httpClient))
	if err != nil {
		return ProjectsClient{}, err
	}
	return ProjectsClient{Client: client}, nil
}

func (c ProjectsClient) ResolveSourceProject(ctx context.Context, orgID string, projectID uuid.UUID) (_ ProjectReference, err error) {
	return c.resolve(ctx, orgID, projectID, "")
}

func (c ProjectsClient) ResolveSourceProjectSlug(ctx context.Context, orgID string, slug string) (_ ProjectReference, err error) {
	return c.resolve(ctx, orgID, uuid.Nil, slug)
}

func (c ProjectsClient) resolve(ctx context.Context, orgID string, projectID uuid.UUID, slug string) (_ ProjectReference, err error) {
	ctx, span := projectsTracer.Start(ctx, "source.projects.resolve")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if c.Client == nil {
		return ProjectReference{}, ErrStoreUnavailable
	}
	slug = NormalizeSlug(slug)
	orgID = strings.TrimSpace(orgID)
	if orgID == "" || (projectID == uuid.Nil && slug == "") {
		return ProjectReference{}, ErrInvalid
	}
	span.SetAttributes(
		attribute.String("verself.org_id", orgID),
		attribute.String("verself.project_id", projectID.String()),
		attribute.String("source.project_slug", slug),
	)
	var projectIDValue *projectsclient.ProjectId
	if projectID != uuid.Nil {
		converted := projectsclient.ProjectId(projectID.String())
		projectIDValue = &converted
	}
	var slugValue *projectsclient.ProjectSlug
	if slug != "" {
		converted := projectsclient.ProjectSlug(slug)
		slugValue = &converted
	}
	resp, err := c.Client.ResolveProject(ctx, projectsclient.ResolveProjectRequest{Body: projectsclient.ResolveProjectInputBody{
		OrgID:         projectsclient.OrgId(orgID),
		ProjectID:     projectIDValue,
		RequireActive: true,
		Slug:          slugValue,
	}})
	if err != nil {
		return ProjectReference{}, fmt.Errorf("%w: resolve project: %v", ErrStoreUnavailable, err)
	}
	if resp.Result == nil {
		status := 0
		body := ""
		if resp.HTTPResponse != nil {
			status = resp.HTTPResponse.StatusCode
			body = strings.TrimSpace(string(resp.Body))
		}
		switch status {
		case http.StatusNotFound:
			return ProjectReference{}, ErrNotFound
		case http.StatusConflict, http.StatusBadRequest:
			return ProjectReference{}, ErrInvalid
		default:
			return ProjectReference{}, fmt.Errorf("%w: resolve project unexpected status %d: %s", ErrStoreUnavailable, status, body)
		}
	}
	project := resp.Result.Project
	parsedID, err := uuid.Parse(string(project.ProjectID))
	if err != nil {
		return ProjectReference{}, fmt.Errorf("%w: parse project id: %v", ErrStoreUnavailable, err)
	}
	if parsedID == uuid.Nil {
		return ProjectReference{}, fmt.Errorf("%w: project resolver returned empty project id", ErrStoreUnavailable)
	}
	projectOrgID := strings.TrimSpace(string(project.OrgID))
	if projectOrgID == "" {
		return ProjectReference{}, fmt.Errorf("%w: project resolver returned empty org id", ErrStoreUnavailable)
	}
	ref := ProjectReference{
		ProjectID:          parsedID,
		OrgID:              projectOrgID,
		Slug:               strings.TrimSpace(string(project.Slug)),
		RedirectedFromSlug: trimOptionalString(project.RedirectedFromSlug),
		DisplayName:        strings.TrimSpace(string(project.DisplayName)),
	}
	span.SetAttributes(
		attribute.String("verself.project_id", ref.ProjectID.String()),
		attribute.String("source.project_slug", ref.Slug),
		attribute.String("source.project_slug.redirected_from", ref.RedirectedFromSlug),
	)
	return ref, nil
}
