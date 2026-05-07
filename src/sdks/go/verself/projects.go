package verself

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	projectsclient "github.com/verself/projects-service/client"
	projectsinternalclient "github.com/verself/projects-service/internalclient"
)

type ProjectState string

const (
	ProjectStateActive   ProjectState = "active"
	ProjectStateArchived ProjectState = "archived"
)

type Project struct {
	ProjectID          string     `json:"project_id"`
	OrgID              string     `json:"org_id"`
	Slug               string     `json:"slug"`
	RedirectedFromSlug string     `json:"redirected_from_slug,omitempty"`
	DisplayName        string     `json:"display_name"`
	Description        string     `json:"description"`
	State              string     `json:"state"`
	Version            string     `json:"version"`
	CreatedBy          string     `json:"created_by"`
	UpdatedBy          string     `json:"updated_by"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	ArchivedAt         *time.Time `json:"archived_at,omitempty"`
}

type ProjectList struct {
	Projects   []Project `json:"projects"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

type ListProjectsOptions struct {
	State  ProjectState
	Limit  int
	Cursor string
}

type CreateProjectInput struct {
	Slug           string
	DisplayName    string
	Description    string
	IdempotencyKey string
}

type APIError struct {
	Service    string
	Operation  string
	StatusCode int
	Title      string
	Detail     string
	Body       string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	detail := strings.TrimSpace(e.Detail)
	if detail == "" {
		detail = strings.TrimSpace(e.Title)
	}
	if detail == "" {
		detail = strings.TrimSpace(e.Body)
	}
	if detail == "" {
		detail = http.StatusText(e.StatusCode)
	}
	return fmt.Sprintf("%s %s failed with HTTP %d: %s", e.Service, e.Operation, e.StatusCode, detail)
}

type ProjectsClient struct {
	client   *projectsclient.ClientWithResponses
	internal *projectsinternalclient.ClientWithResponses
	origin   Origin
}

func (c *ProjectsClient) List(ctx context.Context, options ListProjectsOptions) (ProjectList, error) {
	if c == nil || (c.client == nil && c.internal == nil) {
		return ProjectList{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	if c.internal != nil {
		return c.listInternal(ctx, options)
	}
	params := &projectsclient.ListProjectsParams{}
	if options.State != "" {
		state := projectsclient.ListProjectsParamsState(options.State)
		params.State = &state
	}
	if options.Limit > 0 {
		limit := int64(options.Limit)
		params.Limit = &limit
	}
	if strings.TrimSpace(options.Cursor) != "" {
		cursor := strings.TrimSpace(options.Cursor)
		params.Cursor = &cursor
	}
	response, err := c.client.ListProjectsWithResponse(ctx, params)
	if err != nil {
		return ProjectList{}, err
	}
	if response.JSON200 == nil {
		return ProjectList{}, apiError("Projects API", "list projects", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return projectListFromGenerated(*response.JSON200), nil
}

func (c *ProjectsClient) Create(ctx context.Context, input CreateProjectInput) (Project, error) {
	if c == nil || (c.client == nil && c.internal == nil) {
		return Project{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	key := strings.TrimSpace(input.IdempotencyKey)
	if key == "" {
		generated, err := generateIdempotencyKey("project")
		if err != nil {
			return Project{}, err
		}
		key = generated
	}
	if c.internal != nil {
		return c.createInternal(ctx, input, key)
	}
	body := projectsclient.CreateProjectJSONRequestBody{
		DisplayName: strings.TrimSpace(input.DisplayName),
	}
	if strings.TrimSpace(input.Slug) != "" {
		slug := strings.TrimSpace(input.Slug)
		body.Slug = &slug
	}
	if strings.TrimSpace(input.Description) != "" {
		description := strings.TrimSpace(input.Description)
		body.Description = &description
	}
	response, err := c.client.CreateProjectWithResponse(ctx, &projectsclient.CreateProjectParams{
		IdempotencyKey: key,
	}, body)
	if err != nil {
		return Project{}, err
	}
	if response.JSON201 == nil {
		return Project{}, apiError("Projects API", "create project", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return projectFromGenerated(*response.JSON201), nil
}

func (c *ProjectsClient) Get(ctx context.Context, projectID string) (Project, error) {
	if c == nil || (c.client == nil && c.internal == nil) {
		return Project{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	id, err := uuid.Parse(strings.TrimSpace(projectID))
	if err != nil {
		return Project{}, fmt.Errorf("verself sdk: invalid project id: %w", err)
	}
	if c.internal != nil {
		return c.getInternal(ctx, id)
	}
	response, err := c.client.GetProjectWithResponse(ctx, id)
	if err != nil {
		return Project{}, err
	}
	if response.JSON200 == nil {
		return Project{}, apiError("Projects API", "get project", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return projectFromGenerated(*response.JSON200), nil
}

func (c *ProjectsClient) listInternal(ctx context.Context, options ListProjectsOptions) (ProjectList, error) {
	params := c.internalListParams()
	if options.State != "" {
		state := projectsinternalclient.ListProjectsParamsState(options.State)
		params.State = &state
	}
	if options.Limit > 0 {
		limit := int64(options.Limit)
		params.Limit = &limit
	}
	if strings.TrimSpace(options.Cursor) != "" {
		cursor := strings.TrimSpace(options.Cursor)
		params.Cursor = &cursor
	}
	response, err := c.internal.ListProjectsWithResponse(ctx, &params)
	if err != nil {
		return ProjectList{}, err
	}
	if response.JSON200 == nil {
		return ProjectList{}, apiErrorFields("Projects API", "list projects", response.StatusCode(), internalProblemTitle(response.ApplicationproblemJSONDefault), internalProblemDetail(response.ApplicationproblemJSONDefault), response.Body)
	}
	return projectListFromInternalGenerated(*response.JSON200), nil
}

func (c *ProjectsClient) createInternal(ctx context.Context, input CreateProjectInput, key string) (Project, error) {
	params := projectsinternalclient.CreateProjectParams{
		IdempotencyKey:        key,
		XVerselfOriginOrgID:   strconv.FormatUint(c.origin.OrgID, 10),
		XVerselfOriginSubject: strings.TrimSpace(c.origin.Subject),
	}
	if strings.TrimSpace(c.origin.Email) != "" {
		email := strings.TrimSpace(c.origin.Email)
		params.XVerselfOriginEmail = &email
	}
	body := projectsinternalclient.CreateProjectJSONRequestBody{
		DisplayName: strings.TrimSpace(input.DisplayName),
	}
	if strings.TrimSpace(input.Slug) != "" {
		slug := strings.TrimSpace(input.Slug)
		body.Slug = &slug
	}
	if strings.TrimSpace(input.Description) != "" {
		description := strings.TrimSpace(input.Description)
		body.Description = &description
	}
	response, err := c.internal.CreateProjectWithResponse(ctx, &params, body)
	if err != nil {
		return Project{}, err
	}
	if response.JSON201 == nil {
		return Project{}, apiErrorFields("Projects API", "create project", response.StatusCode(), internalProblemTitle(response.ApplicationproblemJSONDefault), internalProblemDetail(response.ApplicationproblemJSONDefault), response.Body)
	}
	return projectFromInternalGenerated(*response.JSON201), nil
}

func (c *ProjectsClient) getInternal(ctx context.Context, projectID uuid.UUID) (Project, error) {
	params := projectsinternalclient.GetProjectParams{
		XVerselfOriginOrgID:   strconv.FormatUint(c.origin.OrgID, 10),
		XVerselfOriginSubject: strings.TrimSpace(c.origin.Subject),
	}
	if strings.TrimSpace(c.origin.Email) != "" {
		email := strings.TrimSpace(c.origin.Email)
		params.XVerselfOriginEmail = &email
	}
	response, err := c.internal.GetProjectWithResponse(ctx, projectID, &params)
	if err != nil {
		return Project{}, err
	}
	if response.JSON200 == nil {
		return Project{}, apiErrorFields("Projects API", "get project", response.StatusCode(), internalProblemTitle(response.ApplicationproblemJSONDefault), internalProblemDetail(response.ApplicationproblemJSONDefault), response.Body)
	}
	return projectFromInternalGenerated(*response.JSON200), nil
}

func (c *ProjectsClient) internalListParams() projectsinternalclient.ListProjectsParams {
	params := projectsinternalclient.ListProjectsParams{
		XVerselfOriginOrgID:   strconv.FormatUint(c.origin.OrgID, 10),
		XVerselfOriginSubject: strings.TrimSpace(c.origin.Subject),
	}
	if strings.TrimSpace(c.origin.Email) != "" {
		email := strings.TrimSpace(c.origin.Email)
		params.XVerselfOriginEmail = &email
	}
	return params
}

func projectListFromGenerated(input projectsclient.ProjectList) ProjectList {
	out := ProjectList{}
	if input.NextCursor != nil {
		out.NextCursor = *input.NextCursor
	}
	if input.Projects != nil {
		out.Projects = make([]Project, 0, len(*input.Projects))
		for _, project := range *input.Projects {
			out.Projects = append(out.Projects, projectFromGenerated(project))
		}
	}
	return out
}

func projectFromGenerated(input projectsclient.Project) Project {
	redirectedFromSlug := ""
	if input.RedirectedFromSlug != nil {
		redirectedFromSlug = *input.RedirectedFromSlug
	}
	return Project{
		ProjectID:          input.ProjectId.String(),
		OrgID:              input.OrgId,
		Slug:               input.Slug,
		RedirectedFromSlug: redirectedFromSlug,
		DisplayName:        input.DisplayName,
		Description:        input.Description,
		State:              input.State,
		Version:            input.Version,
		CreatedBy:          input.CreatedBy,
		UpdatedBy:          input.UpdatedBy,
		CreatedAt:          input.CreatedAt,
		UpdatedAt:          input.UpdatedAt,
		ArchivedAt:         input.ArchivedAt,
	}
}

func projectListFromInternalGenerated(input projectsinternalclient.ProjectList) ProjectList {
	out := ProjectList{}
	if input.NextCursor != nil {
		out.NextCursor = *input.NextCursor
	}
	if input.Projects != nil {
		out.Projects = make([]Project, 0, len(*input.Projects))
		for _, project := range *input.Projects {
			out.Projects = append(out.Projects, projectFromInternalGenerated(project))
		}
	}
	return out
}

func projectFromInternalGenerated(input projectsinternalclient.Project) Project {
	redirectedFromSlug := ""
	if input.RedirectedFromSlug != nil {
		redirectedFromSlug = *input.RedirectedFromSlug
	}
	return Project{
		ProjectID:          input.ProjectId.String(),
		OrgID:              input.OrgId,
		Slug:               input.Slug,
		RedirectedFromSlug: redirectedFromSlug,
		DisplayName:        input.DisplayName,
		Description:        input.Description,
		State:              input.State,
		Version:            input.Version,
		CreatedBy:          input.CreatedBy,
		UpdatedBy:          input.UpdatedBy,
		CreatedAt:          input.CreatedAt,
		UpdatedAt:          input.UpdatedAt,
		ArchivedAt:         input.ArchivedAt,
	}
}

func apiError(service, operation string, statusCode int, model *projectsclient.ErrorModel, body []byte) error {
	var title *string
	var detail *string
	if model != nil {
		title = model.Title
		detail = model.Detail
	}
	return apiErrorFields(service, operation, statusCode, title, detail, body)
}

func apiErrorFields(service, operation string, statusCode int, title *string, detail *string, body []byte) error {
	err := &APIError{
		Service:    service,
		Operation:  operation,
		StatusCode: statusCode,
		Body:       strings.TrimSpace(string(body)),
	}
	if title != nil {
		err.Title = *title
	}
	if detail != nil {
		err.Detail = *detail
	}
	return err
}

func internalProblemTitle(model *projectsinternalclient.ErrorModel) *string {
	if model == nil {
		return nil
	}
	return model.Title
}

func internalProblemDetail(model *projectsinternalclient.ErrorModel) *string {
	if model == nil {
		return nil
	}
	return model.Detail
}

func generateIdempotencyKey(namespace string) (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("verself sdk: generate idempotency key: %w", err)
	}
	return namespace + ":" + hex.EncodeToString(random[:]), nil
}
