package verself

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	projectscore "github.com/verself/verself-go/internal/generated/projects"
)

type ProjectState string

const maxIdempotencyKeyLength = 128

const (
	ProjectStateActive   ProjectState = "active"
	ProjectStateArchived ProjectState = "archived"
)

type ProjectEnvironmentKind string

const (
	ProjectEnvironmentKindProduction  ProjectEnvironmentKind = "production"
	ProjectEnvironmentKindPreview     ProjectEnvironmentKind = "preview"
	ProjectEnvironmentKindDevelopment ProjectEnvironmentKind = "development"
	ProjectEnvironmentKindCustom      ProjectEnvironmentKind = "custom"
)

type ProjectEnvironmentState string

const (
	ProjectEnvironmentStateActive   ProjectEnvironmentState = "active"
	ProjectEnvironmentStateArchived ProjectEnvironmentState = "archived"
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

type ProjectEnvironment struct {
	EnvironmentID    string            `json:"environment_id"`
	ProjectID        string            `json:"project_id"`
	OrgID            string            `json:"org_id"`
	Slug             string            `json:"slug"`
	DisplayName      string            `json:"display_name"`
	Kind             string            `json:"kind"`
	State            string            `json:"state"`
	ProtectionPolicy map[string]string `json:"protection_policy,omitempty"`
	Version          string            `json:"version"`
	CreatedBy        string            `json:"created_by"`
	UpdatedBy        string            `json:"updated_by"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
	ArchivedAt       *time.Time        `json:"archived_at,omitempty"`
}

type ProjectList struct {
	Projects   []Project `json:"projects"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

type ProjectEnvironmentList struct {
	Environments []ProjectEnvironment `json:"environments"`
	NextCursor   string               `json:"next_cursor,omitempty"`
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

type UpdateProjectInput struct {
	ProjectID      string
	Version        string
	Slug           *string
	DisplayName    *string
	Description    *string
	IdempotencyKey string
}

type ProjectLifecycleInput struct {
	ProjectID      string
	Version        string
	IdempotencyKey string
}

type CreateProjectEnvironmentInput struct {
	ProjectID        string
	Slug             string
	DisplayName      string
	Kind             ProjectEnvironmentKind
	ProtectionPolicy map[string]string
	IdempotencyKey   string
}

type UpdateProjectEnvironmentInput struct {
	ProjectID        string
	EnvironmentID    string
	Version          string
	DisplayName      *string
	ProtectionPolicy map[string]string
	IdempotencyKey   string
}

type ProjectEnvironmentLifecycleInput struct {
	ProjectID      string
	EnvironmentID  string
	Version        string
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
	client *projectscore.ClientWithResponses
}

func (c *ProjectsClient) List(ctx context.Context, options ListProjectsOptions) (ProjectList, error) {
	if c == nil || c.client == nil {
		return ProjectList{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	params := &projectscore.ListProjectsParams{}
	if options.State != "" {
		state := projectscore.ListProjectsParamsState(options.State)
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
	if c == nil || c.client == nil {
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
	body := projectscore.CreateProjectJSONRequestBody{
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
	response, err := c.client.CreateProjectWithResponse(ctx, &projectscore.CreateProjectParams{
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
	if c == nil || c.client == nil {
		return Project{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	id, err := uuid.Parse(strings.TrimSpace(projectID))
	if err != nil {
		return Project{}, fmt.Errorf("verself sdk: invalid project id: %w", err)
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

func (c *ProjectsClient) Update(ctx context.Context, input UpdateProjectInput) (Project, error) {
	if c == nil || c.client == nil {
		return Project{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	projectID, err := parseUUIDInput(input.ProjectID, "project id")
	if err != nil {
		return Project{}, err
	}
	version, err := requireVersion(input.Version)
	if err != nil {
		return Project{}, err
	}
	key, err := mutationKey("project", input.IdempotencyKey)
	if err != nil {
		return Project{}, err
	}
	body := projectscore.UpdateProjectRequest{
		Version: version,
	}
	if input.Slug != nil {
		body.Slug = trimStringPointer(input.Slug)
	}
	if input.DisplayName != nil {
		body.DisplayName = trimStringPointer(input.DisplayName)
	}
	if input.Description != nil {
		body.Description = trimStringPointer(input.Description)
	}
	response, err := c.client.PatchProjectWithResponse(ctx, projectID, &projectscore.PatchProjectParams{
		IdempotencyKey: key,
	}, body)
	if err != nil {
		return Project{}, err
	}
	if response.JSON200 == nil {
		return Project{}, apiError("Projects API", "update project", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return projectFromGenerated(*response.JSON200), nil
}

func (c *ProjectsClient) Archive(ctx context.Context, input ProjectLifecycleInput) (Project, error) {
	if c == nil || c.client == nil {
		return Project{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	projectID, err := parseUUIDInput(input.ProjectID, "project id")
	if err != nil {
		return Project{}, err
	}
	version, err := requireVersion(input.Version)
	if err != nil {
		return Project{}, err
	}
	key, err := mutationKey("project", input.IdempotencyKey)
	if err != nil {
		return Project{}, err
	}
	response, err := c.client.ArchiveProjectWithResponse(ctx, projectID, &projectscore.ArchiveProjectParams{
		IdempotencyKey: key,
	}, projectscore.ProjectLifecycleRequest{Version: version})
	if err != nil {
		return Project{}, err
	}
	if response.JSON200 == nil {
		return Project{}, apiError("Projects API", "archive project", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return projectFromGenerated(*response.JSON200), nil
}

func (c *ProjectsClient) Restore(ctx context.Context, input ProjectLifecycleInput) (Project, error) {
	if c == nil || c.client == nil {
		return Project{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	projectID, err := parseUUIDInput(input.ProjectID, "project id")
	if err != nil {
		return Project{}, err
	}
	version, err := requireVersion(input.Version)
	if err != nil {
		return Project{}, err
	}
	key, err := mutationKey("project", input.IdempotencyKey)
	if err != nil {
		return Project{}, err
	}
	response, err := c.client.RestoreProjectWithResponse(ctx, projectID, &projectscore.RestoreProjectParams{
		IdempotencyKey: key,
	}, projectscore.ProjectLifecycleRequest{Version: version})
	if err != nil {
		return Project{}, err
	}
	if response.JSON200 == nil {
		return Project{}, apiError("Projects API", "restore project", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return projectFromGenerated(*response.JSON200), nil
}

func (c *ProjectsClient) ListEnvironments(ctx context.Context, projectID string) (ProjectEnvironmentList, error) {
	if c == nil || c.client == nil {
		return ProjectEnvironmentList{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	id, err := parseUUIDInput(projectID, "project id")
	if err != nil {
		return ProjectEnvironmentList{}, err
	}
	response, err := c.client.ListProjectEnvironmentsWithResponse(ctx, id)
	if err != nil {
		return ProjectEnvironmentList{}, err
	}
	if response.JSON200 == nil {
		return ProjectEnvironmentList{}, apiError("Projects API", "list project environments", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return environmentListFromGenerated(*response.JSON200), nil
}

func (c *ProjectsClient) CreateEnvironment(ctx context.Context, input CreateProjectEnvironmentInput) (ProjectEnvironment, error) {
	if c == nil || c.client == nil {
		return ProjectEnvironment{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	projectID, err := parseUUIDInput(input.ProjectID, "project id")
	if err != nil {
		return ProjectEnvironment{}, err
	}
	key, err := mutationKey("project-environment", input.IdempotencyKey)
	if err != nil {
		return ProjectEnvironment{}, err
	}
	body := projectscore.CreateProjectEnvironmentRequest{
		Slug:        strings.TrimSpace(input.Slug),
		DisplayName: strings.TrimSpace(input.DisplayName),
		Kind:        strings.TrimSpace(string(input.Kind)),
	}
	if policy := copyStringMapPointer(input.ProtectionPolicy); policy != nil {
		body.ProtectionPolicy = policy
	}
	response, err := c.client.CreateProjectEnvironmentWithResponse(ctx, projectID, &projectscore.CreateProjectEnvironmentParams{
		IdempotencyKey: key,
	}, body)
	if err != nil {
		return ProjectEnvironment{}, err
	}
	if response.JSON201 == nil {
		return ProjectEnvironment{}, apiError("Projects API", "create project environment", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return environmentFromGenerated(*response.JSON201), nil
}

func (c *ProjectsClient) UpdateEnvironment(ctx context.Context, input UpdateProjectEnvironmentInput) (ProjectEnvironment, error) {
	if c == nil || c.client == nil {
		return ProjectEnvironment{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	projectID, err := parseUUIDInput(input.ProjectID, "project id")
	if err != nil {
		return ProjectEnvironment{}, err
	}
	environmentID, err := parseUUIDInput(input.EnvironmentID, "environment id")
	if err != nil {
		return ProjectEnvironment{}, err
	}
	version, err := requireVersion(input.Version)
	if err != nil {
		return ProjectEnvironment{}, err
	}
	key, err := mutationKey("project-environment", input.IdempotencyKey)
	if err != nil {
		return ProjectEnvironment{}, err
	}
	body := projectscore.UpdateProjectEnvironmentRequest{
		Version: version,
	}
	if input.DisplayName != nil {
		body.DisplayName = trimStringPointer(input.DisplayName)
	}
	if policy := copyStringMapPointer(input.ProtectionPolicy); policy != nil {
		body.ProtectionPolicy = policy
	}
	response, err := c.client.PatchProjectEnvironmentWithResponse(ctx, projectID, environmentID, &projectscore.PatchProjectEnvironmentParams{
		IdempotencyKey: key,
	}, body)
	if err != nil {
		return ProjectEnvironment{}, err
	}
	if response.JSON200 == nil {
		return ProjectEnvironment{}, apiError("Projects API", "update project environment", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return environmentFromGenerated(*response.JSON200), nil
}

func (c *ProjectsClient) ArchiveEnvironment(ctx context.Context, input ProjectEnvironmentLifecycleInput) (ProjectEnvironment, error) {
	if c == nil || c.client == nil {
		return ProjectEnvironment{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	projectID, err := parseUUIDInput(input.ProjectID, "project id")
	if err != nil {
		return ProjectEnvironment{}, err
	}
	environmentID, err := parseUUIDInput(input.EnvironmentID, "environment id")
	if err != nil {
		return ProjectEnvironment{}, err
	}
	version, err := requireVersion(input.Version)
	if err != nil {
		return ProjectEnvironment{}, err
	}
	key, err := mutationKey("project-environment", input.IdempotencyKey)
	if err != nil {
		return ProjectEnvironment{}, err
	}
	response, err := c.client.ArchiveProjectEnvironmentWithResponse(ctx, projectID, environmentID, &projectscore.ArchiveProjectEnvironmentParams{
		IdempotencyKey: key,
	}, projectscore.ProjectLifecycleRequest{Version: version})
	if err != nil {
		return ProjectEnvironment{}, err
	}
	if response.JSON200 == nil {
		return ProjectEnvironment{}, apiError("Projects API", "archive project environment", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return environmentFromGenerated(*response.JSON200), nil
}

func projectListFromGenerated(input projectscore.ProjectList) ProjectList {
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

func environmentListFromGenerated(input projectscore.ProjectEnvironmentList) ProjectEnvironmentList {
	out := ProjectEnvironmentList{}
	if input.NextCursor != nil {
		out.NextCursor = *input.NextCursor
	}
	if input.Environments != nil {
		out.Environments = make([]ProjectEnvironment, 0, len(*input.Environments))
		for _, environment := range *input.Environments {
			out.Environments = append(out.Environments, environmentFromGenerated(environment))
		}
	}
	return out
}

func projectFromGenerated(input projectscore.Project) Project {
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

func environmentFromGenerated(input projectscore.ProjectEnvironment) ProjectEnvironment {
	protectionPolicy := map[string]string(nil)
	if input.ProtectionPolicy != nil {
		protectionPolicy = copyStringMap(*input.ProtectionPolicy)
	}
	return ProjectEnvironment{
		EnvironmentID:    input.EnvironmentId.String(),
		ProjectID:        input.ProjectId.String(),
		OrgID:            input.OrgId,
		Slug:             input.Slug,
		DisplayName:      input.DisplayName,
		Kind:             input.Kind,
		State:            input.State,
		ProtectionPolicy: protectionPolicy,
		Version:          input.Version,
		CreatedBy:        input.CreatedBy,
		UpdatedBy:        input.UpdatedBy,
		CreatedAt:        input.CreatedAt,
		UpdatedAt:        input.UpdatedAt,
		ArchivedAt:       input.ArchivedAt,
	}
}

func apiError(service, operation string, statusCode int, model *projectscore.ErrorModel, body []byte) error {
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

func parseUUIDInput(value, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil, fmt.Errorf("verself sdk: invalid %s: %w", field, err)
	}
	return id, nil
}

func requireVersion(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("verself sdk: version is required")
	}
	return trimmed, nil
}

func mutationKey(namespace, explicit string) (string, error) {
	key := strings.TrimSpace(explicit)
	if key != "" {
		if len(key) > maxIdempotencyKeyLength {
			return "", fmt.Errorf("verself sdk: Idempotency-Key must be %d characters or fewer", maxIdempotencyKeyLength)
		}
		return key, nil
	}
	return generateIdempotencyKey(namespace)
}

func trimStringPointer(input *string) *string {
	trimmed := strings.TrimSpace(*input)
	return &trimmed
}

func copyStringMapPointer(input map[string]string) *map[string]string {
	if input == nil {
		return nil
	}
	copied := copyStringMap(input)
	return &copied
}

func copyStringMap(input map[string]string) map[string]string {
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func generateIdempotencyKey(namespace string) (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("verself sdk: generate idempotency key: %w", err)
	}
	return namespace + ":" + hex.EncodeToString(random[:]), nil
}
