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
	ResourceName       string     `json:"resourceName"`
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
	EnvironmentID       string            `json:"environment_id"`
	ResourceName        string            `json:"resourceName"`
	ProjectID           string            `json:"project_id"`
	ProjectResourceName string            `json:"projectResourceName"`
	OrgID               string            `json:"org_id"`
	Slug                string            `json:"slug"`
	DisplayName         string            `json:"display_name"`
	Kind                string            `json:"kind"`
	State               string            `json:"state"`
	ProtectionPolicy    map[string]string `json:"protection_policy,omitempty"`
	Version             string            `json:"version"`
	CreatedBy           string            `json:"created_by"`
	UpdatedBy           string            `json:"updated_by"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
	ArchivedAt          *time.Time        `json:"archived_at,omitempty"`
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
	client *projectscore.Client
}

func (c *ProjectsClient) List(ctx context.Context, options ListProjectsOptions) (ProjectList, error) {
	if c == nil || c.client == nil {
		return ProjectList{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	request := projectscore.ListProjectsRequest{}
	if options.State != "" {
		state := projectscore.ProjectState(options.State)
		request.State = &state
	}
	if options.Limit > 0 {
		limit := projectscore.PageSize(options.Limit)
		request.Limit = &limit
	}
	if strings.TrimSpace(options.Cursor) != "" {
		cursor := projectscore.PageToken(strings.TrimSpace(options.Cursor))
		request.Cursor = &cursor
	}
	response, err := c.client.ListProjects(ctx, request)
	if err != nil {
		return ProjectList{}, err
	}
	if response.Result == nil {
		return ProjectList{}, apiError("Projects API", "list projects", response.StatusCode, response.Problem, response.Body)
	}
	return projectListFromGenerated(*response.Result), nil
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
	body := projectscore.CreateProjectInputBody{
		DisplayName: projectscore.DisplayName(strings.TrimSpace(input.DisplayName)),
	}
	if strings.TrimSpace(input.Slug) != "" {
		slug := projectscore.ProjectSlug(strings.TrimSpace(input.Slug))
		body.Slug = &slug
	}
	if strings.TrimSpace(input.Description) != "" {
		description := projectscore.ProjectDescription(strings.TrimSpace(input.Description))
		body.Description = &description
	}
	response, err := c.client.CreateProject(ctx, projectscore.CreateProjectRequest{
		IdempotencyKey: projectscore.IdempotencyKey(key),
		Body:           body,
	})
	if err != nil {
		return Project{}, err
	}
	if response.Result == nil {
		return Project{}, apiError("Projects API", "create project", response.StatusCode, response.Problem, response.Body)
	}
	return projectFromGenerated(*response.Result), nil
}

func (c *ProjectsClient) Get(ctx context.Context, projectID string) (Project, error) {
	if c == nil || c.client == nil {
		return Project{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	id, err := parseProjectID(projectID, "project id")
	if err != nil {
		return Project{}, fmt.Errorf("verself sdk: invalid project id: %w", err)
	}
	response, err := c.client.GetProject(ctx, projectscore.GetProjectRequest{ProjectID: id})
	if err != nil {
		return Project{}, err
	}
	if response.Result == nil {
		return Project{}, apiError("Projects API", "get project", response.StatusCode, response.Problem, response.Body)
	}
	return projectFromGenerated(*response.Result), nil
}

func (c *ProjectsClient) Update(ctx context.Context, input UpdateProjectInput) (Project, error) {
	if c == nil || c.client == nil {
		return Project{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	projectID, err := parseProjectID(input.ProjectID, "project id")
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
	body := projectscore.PatchProjectInputBody{
		Version: version,
	}
	if input.Slug != nil {
		body.Slug = trimAliasPointer[projectscore.ProjectSlug](*input.Slug)
	}
	if input.DisplayName != nil {
		body.DisplayName = trimAliasPointer[projectscore.DisplayName](*input.DisplayName)
	}
	if input.Description != nil {
		body.Description = trimAliasPointer[projectscore.ProjectDescription](*input.Description)
	}
	response, err := c.client.PatchProject(ctx, projectscore.PatchProjectRequest{
		ProjectID:      projectID,
		IdempotencyKey: projectscore.IdempotencyKey(key),
		Body:           body,
	})
	if err != nil {
		return Project{}, err
	}
	if response.Result == nil {
		return Project{}, apiError("Projects API", "update project", response.StatusCode, response.Problem, response.Body)
	}
	return projectFromGenerated(*response.Result), nil
}

func (c *ProjectsClient) Archive(ctx context.Context, input ProjectLifecycleInput) (Project, error) {
	if c == nil || c.client == nil {
		return Project{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	projectID, err := parseProjectID(input.ProjectID, "project id")
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
	response, err := c.client.ArchiveProject(ctx, projectscore.ArchiveProjectRequest{
		ProjectID:      projectID,
		IdempotencyKey: projectscore.IdempotencyKey(key),
		Body:           projectscore.ProjectLifecycleInputBody{Version: version},
	})
	if err != nil {
		return Project{}, err
	}
	if response.Result == nil {
		return Project{}, apiError("Projects API", "archive project", response.StatusCode, response.Problem, response.Body)
	}
	return projectFromGenerated(*response.Result), nil
}

func (c *ProjectsClient) Restore(ctx context.Context, input ProjectLifecycleInput) (Project, error) {
	if c == nil || c.client == nil {
		return Project{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	projectID, err := parseProjectID(input.ProjectID, "project id")
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
	response, err := c.client.RestoreProject(ctx, projectscore.RestoreProjectRequest{
		ProjectID:      projectID,
		IdempotencyKey: projectscore.IdempotencyKey(key),
		Body:           projectscore.ProjectLifecycleInputBody{Version: version},
	})
	if err != nil {
		return Project{}, err
	}
	if response.Result == nil {
		return Project{}, apiError("Projects API", "restore project", response.StatusCode, response.Problem, response.Body)
	}
	return projectFromGenerated(*response.Result), nil
}

func (c *ProjectsClient) ListEnvironments(ctx context.Context, projectID string) (ProjectEnvironmentList, error) {
	if c == nil || c.client == nil {
		return ProjectEnvironmentList{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	id, err := parseProjectID(projectID, "project id")
	if err != nil {
		return ProjectEnvironmentList{}, err
	}
	response, err := c.client.ListProjectEnvironments(ctx, projectscore.ListProjectEnvironmentsRequest{ProjectID: id})
	if err != nil {
		return ProjectEnvironmentList{}, err
	}
	if response.Result == nil {
		return ProjectEnvironmentList{}, apiError("Projects API", "list project environments", response.StatusCode, response.Problem, response.Body)
	}
	return environmentListFromGenerated(*response.Result), nil
}

func (c *ProjectsClient) CreateEnvironment(ctx context.Context, input CreateProjectEnvironmentInput) (ProjectEnvironment, error) {
	if c == nil || c.client == nil {
		return ProjectEnvironment{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	projectID, err := parseProjectID(input.ProjectID, "project id")
	if err != nil {
		return ProjectEnvironment{}, err
	}
	key, err := mutationKey("project-environment", input.IdempotencyKey)
	if err != nil {
		return ProjectEnvironment{}, err
	}
	body := projectscore.CreateProjectEnvironmentInputBody{
		Slug:        projectscore.ProjectSlug(strings.TrimSpace(input.Slug)),
		DisplayName: projectscore.DisplayName(strings.TrimSpace(input.DisplayName)),
		Kind:        projectscore.ProjectEnvironmentKind(strings.TrimSpace(string(input.Kind))),
	}
	if policy := copyStringMapPointer(input.ProtectionPolicy); policy != nil {
		converted := projectscore.ProjectProtectionPolicy(*policy)
		body.ProtectionPolicy = &converted
	}
	response, err := c.client.CreateProjectEnvironment(ctx, projectscore.CreateProjectEnvironmentRequest{
		ProjectID:      projectID,
		IdempotencyKey: projectscore.IdempotencyKey(key),
		Body:           body,
	})
	if err != nil {
		return ProjectEnvironment{}, err
	}
	if response.Result == nil {
		return ProjectEnvironment{}, apiError("Projects API", "create project environment", response.StatusCode, response.Problem, response.Body)
	}
	return environmentFromGenerated(*response.Result), nil
}

func (c *ProjectsClient) UpdateEnvironment(ctx context.Context, input UpdateProjectEnvironmentInput) (ProjectEnvironment, error) {
	if c == nil || c.client == nil {
		return ProjectEnvironment{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	projectID, err := parseProjectID(input.ProjectID, "project id")
	if err != nil {
		return ProjectEnvironment{}, err
	}
	environmentID, err := parseEnvironmentID(input.EnvironmentID, "environment id")
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
	body := projectscore.PatchProjectEnvironmentInputBody{
		Version: version,
	}
	if input.DisplayName != nil {
		body.DisplayName = trimAliasPointer[projectscore.DisplayName](*input.DisplayName)
	}
	if policy := copyStringMapPointer(input.ProtectionPolicy); policy != nil {
		converted := projectscore.ProjectProtectionPolicy(*policy)
		body.ProtectionPolicy = &converted
	}
	response, err := c.client.PatchProjectEnvironment(ctx, projectscore.PatchProjectEnvironmentRequest{
		ProjectID:      projectID,
		EnvironmentID:  environmentID,
		IdempotencyKey: projectscore.IdempotencyKey(key),
		Body:           body,
	})
	if err != nil {
		return ProjectEnvironment{}, err
	}
	if response.Result == nil {
		return ProjectEnvironment{}, apiError("Projects API", "update project environment", response.StatusCode, response.Problem, response.Body)
	}
	return environmentFromGenerated(*response.Result), nil
}

func (c *ProjectsClient) ArchiveEnvironment(ctx context.Context, input ProjectEnvironmentLifecycleInput) (ProjectEnvironment, error) {
	if c == nil || c.client == nil {
		return ProjectEnvironment{}, fmt.Errorf("verself sdk: projects client is not initialized")
	}
	projectID, err := parseProjectID(input.ProjectID, "project id")
	if err != nil {
		return ProjectEnvironment{}, err
	}
	environmentID, err := parseEnvironmentID(input.EnvironmentID, "environment id")
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
	response, err := c.client.ArchiveProjectEnvironment(ctx, projectscore.ArchiveProjectEnvironmentRequest{
		ProjectID:      projectID,
		EnvironmentID:  environmentID,
		IdempotencyKey: projectscore.IdempotencyKey(key),
		Body:           projectscore.ProjectEnvironmentLifecycleInputBody{Version: version},
	})
	if err != nil {
		return ProjectEnvironment{}, err
	}
	if response.Result == nil {
		return ProjectEnvironment{}, apiError("Projects API", "archive project environment", response.StatusCode, response.Problem, response.Body)
	}
	return environmentFromGenerated(*response.Result), nil
}

func projectListFromGenerated(input projectscore.ListProjectsOutputBody) ProjectList {
	out := ProjectList{}
	if input.NextCursor != nil {
		out.NextCursor = string(*input.NextCursor)
	}
	out.Projects = make([]Project, 0, len(input.Projects))
	for _, project := range input.Projects {
		out.Projects = append(out.Projects, projectFromGenerated(project))
	}
	return out
}

func environmentListFromGenerated(input projectscore.ListProjectEnvironmentsOutputBody) ProjectEnvironmentList {
	out := ProjectEnvironmentList{Environments: make([]ProjectEnvironment, 0, len(input.Environments))}
	for _, environment := range input.Environments {
		out.Environments = append(out.Environments, environmentFromGenerated(environment))
	}
	return out
}

func projectFromGenerated(input projectscore.ProjectSummary) Project {
	redirectedFromSlug := ""
	if input.RedirectedFromSlug != nil {
		redirectedFromSlug = string(*input.RedirectedFromSlug)
	}
	return Project{
		ProjectID:          string(input.ProjectID),
		ResourceName:       string(input.ResourceName),
		OrgID:              string(input.OrgID),
		Slug:               string(input.Slug),
		RedirectedFromSlug: redirectedFromSlug,
		DisplayName:        string(input.DisplayName),
		Description:        stringFromAliasPointer(input.Description),
		State:              string(input.State),
		Version:            string(input.Version),
		CreatedBy:          string(input.CreatedBy),
		UpdatedBy:          string(input.UpdatedBy),
		CreatedAt:          parseSDKTime(input.CreatedAt),
		UpdatedAt:          parseSDKTime(input.UpdatedAt),
		ArchivedAt:         parseSDKTimePointer(input.ArchivedAt),
	}
}

func environmentFromGenerated(input projectscore.ProjectEnvironmentSummary) ProjectEnvironment {
	protectionPolicy := map[string]string(nil)
	if input.ProtectionPolicy != nil {
		protectionPolicy = copyStringMap(*input.ProtectionPolicy)
	}
	return ProjectEnvironment{
		EnvironmentID:       string(input.EnvironmentID),
		ResourceName:        string(input.ResourceName),
		ProjectID:           string(input.ProjectID),
		ProjectResourceName: string(input.ProjectResourceName),
		OrgID:               string(input.OrgID),
		Slug:                string(input.Slug),
		DisplayName:         string(input.DisplayName),
		Kind:                string(input.Kind),
		State:               string(input.State),
		ProtectionPolicy:    protectionPolicy,
		Version:             string(input.Version),
		CreatedBy:           string(input.CreatedBy),
		UpdatedBy:           string(input.UpdatedBy),
		CreatedAt:           parseSDKTime(input.CreatedAt),
		UpdatedAt:           parseSDKTime(input.UpdatedAt),
		ArchivedAt:          parseSDKTimePointer(input.ArchivedAt),
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

func parseProjectID(value, field string) (projectscore.ProjectId, error) {
	id, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("verself sdk: invalid %s: %w", field, err)
	}
	return projectscore.ProjectId(id.String()), nil
}

func parseEnvironmentID(value, field string) (projectscore.EnvironmentId, error) {
	id, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("verself sdk: invalid %s: %w", field, err)
	}
	return projectscore.EnvironmentId(id.String()), nil
}

func parseUUIDInput(value, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil, fmt.Errorf("verself sdk: invalid %s: %w", field, err)
	}
	return id, nil
}

func requireVersion(value string) (projectscore.DecimalInt64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("verself sdk: version is required")
	}
	return projectscore.DecimalInt64(trimmed), nil
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

func parseSDKTimePointer(value *string) *time.Time {
	if value == nil {
		return nil
	}
	parsed := parseSDKTime(*value)
	return &parsed
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
