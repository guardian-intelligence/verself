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

func (c ProjectsClient) ResolveSourceProject(ctx context.Context, orgID uint64, projectID uuid.UUID) (_ ProjectReference, err error) {
	return c.resolve(ctx, orgID, projectID, "")
}

func (c ProjectsClient) ResolveSourceProjectSlug(ctx context.Context, orgID uint64, slug string) (_ ProjectReference, err error) {
	return c.resolve(ctx, orgID, uuid.Nil, slug)
}

func (c ProjectsClient) resolve(ctx context.Context, orgID uint64, projectID uuid.UUID, slug string) (_ ProjectReference, err error) {
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
	if orgID == 0 || (projectID == uuid.Nil && slug == "") {
		return ProjectReference{}, ErrInvalid
	}
	span.SetAttributes(
		attribute.Int64("verself.org_id", int64(orgID)),
		attribute.String("verself.project_id", projectID.String()),
		attribute.String("source.project_slug", slug),
	)
	var projectIDValue *uuid.UUID
	if projectID != uuid.Nil {
		projectIDValue = &projectID
	}
	var slugValue *string
	if slug != "" {
		slugValue = &slug
	}
	resp, err := c.Client.ResolveProjectWithResponse(ctx, projectsclient.ResolveProjectJSONRequestBody{
		OrgId:         strconv.FormatUint(orgID, 10),
		ProjectId:     projectIDValue,
		RequireActive: true,
		Slug:          slugValue,
	})
	if err != nil {
		return ProjectReference{}, fmt.Errorf("%w: resolve project: %v", ErrStoreUnavailable, err)
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
			return ProjectReference{}, ErrNotFound
		case http.StatusConflict, http.StatusBadRequest:
			return ProjectReference{}, ErrInvalid
		default:
			return ProjectReference{}, fmt.Errorf("%w: resolve project unexpected status %d: %s", ErrStoreUnavailable, status, body)
		}
	}
	project := resp.JSON200.Project
	parsedID := uuid.UUID(project.ProjectId)
	if parsedID == uuid.Nil {
		return ProjectReference{}, fmt.Errorf("%w: project resolver returned empty project id", ErrStoreUnavailable)
	}
	projectOrgID, err := strconv.ParseUint(strings.TrimSpace(project.OrgId), 10, 64)
	if err != nil || projectOrgID == 0 {
		return ProjectReference{}, fmt.Errorf("%w: parse project org id: %v", ErrStoreUnavailable, err)
	}
	ref := ProjectReference{
		ProjectID:          parsedID,
		OrgID:              projectOrgID,
		Slug:               strings.TrimSpace(project.Slug),
		RedirectedFromSlug: trimOptionalString(project.RedirectedFromSlug),
		DisplayName:        strings.TrimSpace(project.DisplayName),
	}
	span.SetAttributes(
		attribute.String("verself.project_id", ref.ProjectID.String()),
		attribute.String("source.project_slug", ref.Slug),
		attribute.String("source.project_slug.redirected_from", ref.RedirectedFromSlug),
	)
	return ref, nil
}
