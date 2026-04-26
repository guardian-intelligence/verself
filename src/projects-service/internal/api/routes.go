package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/verself/apiwire"
	"github.com/verself/projects-service/internal/projects"
)

type projectPath struct {
	ProjectID string `path:"project_id" format:"uuid"`
}

type listProjectsInput struct {
	State  string `query:"state,omitempty" enum:"active,archived"`
	Limit  int    `query:"limit,omitempty" minimum:"1" maximum:"100"`
	Cursor string `query:"cursor,omitempty" maxLength:"256"`
}

type createProjectInput struct {
	Body apiwire.CreateProjectRequest
}

type updateProjectInput struct {
	ProjectID string `path:"project_id" format:"uuid"`
	Body      apiwire.UpdateProjectRequest
}

type projectLifecycleInput struct {
	ProjectID string `path:"project_id" format:"uuid"`
	Body      apiwire.ProjectLifecycleRequest
}

type listEnvironmentsInput struct {
	ProjectID string `path:"project_id" format:"uuid"`
}

type createEnvironmentInput struct {
	ProjectID string `path:"project_id" format:"uuid"`
	Body      apiwire.CreateProjectEnvironmentRequest
}

type updateEnvironmentInput struct {
	ProjectID     string `path:"project_id" format:"uuid"`
	EnvironmentID string `path:"environment_id" format:"uuid"`
	Body          apiwire.UpdateProjectEnvironmentRequest
}

type environmentLifecycleInput struct {
	ProjectID     string `path:"project_id" format:"uuid"`
	EnvironmentID string `path:"environment_id" format:"uuid"`
	Body          apiwire.ProjectLifecycleRequest
}

type projectOutput struct {
	Body apiwire.Project
}

type projectListOutput struct {
	Body apiwire.ProjectList
}

type environmentOutput struct {
	Body apiwire.ProjectEnvironment
}

type environmentListOutput struct {
	Body apiwire.ProjectEnvironmentList
}

func RegisterRoutes(api huma.API, svc *projects.Service) {
	registerProjectsRoute(api, huma.Operation{
		OperationID:   "create-project",
		Method:        http.MethodPost,
		Path:          "/api/v1/projects",
		Summary:       "Create a project",
		DefaultStatus: http.StatusCreated,
	}, operationPolicy{
		Permission:       permissionProjectWrite,
		Resource:         "project",
		Action:           "create",
		OrgScope:         "token_org_id",
		RateLimitClass:   "projects_mutation",
		Idempotency:      idempotencyHeaderKey,
		AuditEvent:       "projects.project.create",
		OperationDisplay: "create project",
		OperationType:    "write",
		RiskLevel:        "medium",
		BodyLimitBytes:   bodyLimitSmallJSON,
	}, createProject(svc))

	registerProjectsRoute(api, huma.Operation{
		OperationID: "list-projects",
		Method:      http.MethodGet,
		Path:        "/api/v1/projects",
		Summary:     "List projects",
	}, operationPolicy{
		Permission:       permissionProjectRead,
		Resource:         "project",
		Action:           "list",
		OrgScope:         "token_org_id",
		RateLimitClass:   "read",
		AuditEvent:       "projects.project.list",
		OperationDisplay: "list projects",
		OperationType:    "read",
		RiskLevel:        "low",
	}, listProjects(svc))

	registerProjectsRoute(api, huma.Operation{
		OperationID: "get-project",
		Method:      http.MethodGet,
		Path:        "/api/v1/projects/{project_id}",
		Summary:     "Get a project",
	}, operationPolicy{
		Permission:       permissionProjectRead,
		Resource:         "project",
		Action:           "read",
		OrgScope:         "token_org_id",
		RateLimitClass:   "read",
		AuditEvent:       "projects.project.read",
		OperationDisplay: "read project",
		OperationType:    "read",
		RiskLevel:        "low",
	}, getProject(svc))

	registerProjectsRoute(api, huma.Operation{
		OperationID:   "patch-project",
		Method:        http.MethodPatch,
		Path:          "/api/v1/projects/{project_id}",
		Summary:       "Update a project",
		DefaultStatus: http.StatusOK,
	}, operationPolicy{
		Permission:       permissionProjectWrite,
		Resource:         "project",
		Action:           "update",
		OrgScope:         "token_org_id",
		RateLimitClass:   "projects_mutation",
		Idempotency:      idempotencyHeaderKey,
		AuditEvent:       "projects.project.update",
		OperationDisplay: "update project",
		OperationType:    "write",
		RiskLevel:        "medium",
		BodyLimitBytes:   bodyLimitSmallJSON,
	}, updateProject(svc))

	registerProjectsRoute(api, huma.Operation{
		OperationID:   "archive-project",
		Method:        http.MethodPost,
		Path:          "/api/v1/projects/{project_id}/archive",
		Summary:       "Archive a project",
		DefaultStatus: http.StatusOK,
	}, operationPolicy{
		Permission:       permissionProjectWrite,
		Resource:         "project",
		Action:           "archive",
		OrgScope:         "token_org_id",
		RateLimitClass:   "projects_mutation",
		Idempotency:      idempotencyHeaderKey,
		AuditEvent:       "projects.project.archive",
		OperationDisplay: "archive project",
		OperationType:    "delete",
		RiskLevel:        "medium",
		BodyLimitBytes:   bodyLimitSmallJSON,
	}, archiveProject(svc))

	registerProjectsRoute(api, huma.Operation{
		OperationID:   "restore-project",
		Method:        http.MethodPost,
		Path:          "/api/v1/projects/{project_id}/restore",
		Summary:       "Restore a project",
		DefaultStatus: http.StatusOK,
	}, operationPolicy{
		Permission:       permissionProjectWrite,
		Resource:         "project",
		Action:           "restore",
		OrgScope:         "token_org_id",
		RateLimitClass:   "projects_mutation",
		Idempotency:      idempotencyHeaderKey,
		AuditEvent:       "projects.project.restore",
		OperationDisplay: "restore project",
		OperationType:    "write",
		RiskLevel:        "medium",
		BodyLimitBytes:   bodyLimitSmallJSON,
	}, restoreProject(svc))

	registerProjectsRoute(api, huma.Operation{
		OperationID: "list-project-environments",
		Method:      http.MethodGet,
		Path:        "/api/v1/projects/{project_id}/environments",
		Summary:     "List project environments",
	}, operationPolicy{
		Permission:       permissionEnvironmentRead,
		Resource:         "project_environment",
		Action:           "list",
		OrgScope:         "token_org_id",
		RateLimitClass:   "read",
		AuditEvent:       "projects.environment.list",
		OperationDisplay: "list project environments",
		OperationType:    "read",
		RiskLevel:        "low",
	}, listEnvironments(svc))

	registerProjectsRoute(api, huma.Operation{
		OperationID:   "create-project-environment",
		Method:        http.MethodPost,
		Path:          "/api/v1/projects/{project_id}/environments",
		Summary:       "Create a project environment",
		DefaultStatus: http.StatusCreated,
	}, operationPolicy{
		Permission:       permissionEnvironmentWrite,
		Resource:         "project_environment",
		Action:           "create",
		OrgScope:         "token_org_id",
		RateLimitClass:   "projects_mutation",
		Idempotency:      idempotencyHeaderKey,
		AuditEvent:       "projects.environment.create",
		OperationDisplay: "create project environment",
		OperationType:    "write",
		RiskLevel:        "medium",
		BodyLimitBytes:   bodyLimitSmallJSON,
	}, createEnvironment(svc))

	registerProjectsRoute(api, huma.Operation{
		OperationID:   "patch-project-environment",
		Method:        http.MethodPatch,
		Path:          "/api/v1/projects/{project_id}/environments/{environment_id}",
		Summary:       "Update a project environment",
		DefaultStatus: http.StatusOK,
	}, operationPolicy{
		Permission:       permissionEnvironmentWrite,
		Resource:         "project_environment",
		Action:           "update",
		OrgScope:         "token_org_id",
		RateLimitClass:   "projects_mutation",
		Idempotency:      idempotencyHeaderKey,
		AuditEvent:       "projects.environment.update",
		OperationDisplay: "update project environment",
		OperationType:    "write",
		RiskLevel:        "medium",
		BodyLimitBytes:   bodyLimitSmallJSON,
	}, updateEnvironment(svc))

	registerProjectsRoute(api, huma.Operation{
		OperationID:   "archive-project-environment",
		Method:        http.MethodPost,
		Path:          "/api/v1/projects/{project_id}/environments/{environment_id}/archive",
		Summary:       "Archive a project environment",
		DefaultStatus: http.StatusOK,
	}, operationPolicy{
		Permission:       permissionEnvironmentWrite,
		Resource:         "project_environment",
		Action:           "archive",
		OrgScope:         "token_org_id",
		RateLimitClass:   "projects_mutation",
		Idempotency:      idempotencyHeaderKey,
		AuditEvent:       "projects.environment.archive",
		OperationDisplay: "archive project environment",
		OperationType:    "delete",
		RiskLevel:        "medium",
		BodyLimitBytes:   bodyLimitSmallJSON,
	}, archiveEnvironment(svc))
}

func createProject(svc *projects.Service) func(context.Context, projects.Principal, *createProjectInput) (*projectOutput, error) {
	return func(ctx context.Context, principal projects.Principal, input *createProjectInput) (*projectOutput, error) {
		project, err := svc.CreateProject(ctx, principal, projects.CreateProjectRequest{
			Slug:           input.Body.Slug,
			DisplayName:    input.Body.DisplayName,
			Description:    input.Body.Description,
			IdempotencyKey: operationRequestInfoFromContext(ctx).IdempotencyKey,
		})
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &projectOutput{Body: projectDTO(project)}, nil
	}
}

func listProjects(svc *projects.Service) func(context.Context, projects.Principal, *listProjectsInput) (*projectListOutput, error) {
	return func(ctx context.Context, principal projects.Principal, input *listProjectsInput) (*projectListOutput, error) {
		records, nextCursor, err := svc.ListProjects(ctx, principal, projects.ListProjectsRequest{
			State:  input.State,
			Limit:  input.Limit,
			Cursor: input.Cursor,
		})
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &projectListOutput{Body: apiwire.ProjectList{Projects: projectDTOs(records), NextCursor: nextCursor}}, nil
	}
}

func getProject(svc *projects.Service) func(context.Context, projects.Principal, *projectPath) (*projectOutput, error) {
	return func(ctx context.Context, principal projects.Principal, input *projectPath) (*projectOutput, error) {
		projectID, err := parseUUID(ctx, input.ProjectID, "project_id")
		if err != nil {
			return nil, err
		}
		project, err := svc.GetProject(ctx, principal, projectID)
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &projectOutput{Body: projectDTO(project)}, nil
	}
}

func updateProject(svc *projects.Service) func(context.Context, projects.Principal, *updateProjectInput) (*projectOutput, error) {
	return func(ctx context.Context, principal projects.Principal, input *updateProjectInput) (*projectOutput, error) {
		projectID, err := parseUUID(ctx, input.ProjectID, "project_id")
		if err != nil {
			return nil, err
		}
		project, err := svc.UpdateProject(ctx, principal, projects.UpdateProjectRequest{
			ProjectID:      projectID,
			Version:        input.Body.Version.Int64(),
			Slug:           input.Body.Slug,
			DisplayName:    input.Body.DisplayName,
			Description:    input.Body.Description,
			IdempotencyKey: operationRequestInfoFromContext(ctx).IdempotencyKey,
		})
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &projectOutput{Body: projectDTO(project)}, nil
	}
}

func archiveProject(svc *projects.Service) func(context.Context, projects.Principal, *projectLifecycleInput) (*projectOutput, error) {
	return func(ctx context.Context, principal projects.Principal, input *projectLifecycleInput) (*projectOutput, error) {
		projectID, err := parseUUID(ctx, input.ProjectID, "project_id")
		if err != nil {
			return nil, err
		}
		project, err := svc.ArchiveProject(ctx, principal, projects.ProjectLifecycleRequest{
			ProjectID:      projectID,
			Version:        input.Body.Version.Int64(),
			IdempotencyKey: operationRequestInfoFromContext(ctx).IdempotencyKey,
		})
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &projectOutput{Body: projectDTO(project)}, nil
	}
}

func restoreProject(svc *projects.Service) func(context.Context, projects.Principal, *projectLifecycleInput) (*projectOutput, error) {
	return func(ctx context.Context, principal projects.Principal, input *projectLifecycleInput) (*projectOutput, error) {
		projectID, err := parseUUID(ctx, input.ProjectID, "project_id")
		if err != nil {
			return nil, err
		}
		project, err := svc.RestoreProject(ctx, principal, projects.ProjectLifecycleRequest{
			ProjectID:      projectID,
			Version:        input.Body.Version.Int64(),
			IdempotencyKey: operationRequestInfoFromContext(ctx).IdempotencyKey,
		})
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &projectOutput{Body: projectDTO(project)}, nil
	}
}

func listEnvironments(svc *projects.Service) func(context.Context, projects.Principal, *listEnvironmentsInput) (*environmentListOutput, error) {
	return func(ctx context.Context, principal projects.Principal, input *listEnvironmentsInput) (*environmentListOutput, error) {
		projectID, err := parseUUID(ctx, input.ProjectID, "project_id")
		if err != nil {
			return nil, err
		}
		envs, err := svc.ListEnvironments(ctx, principal, projectID)
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &environmentListOutput{Body: apiwire.ProjectEnvironmentList{Environments: environmentDTOs(envs)}}, nil
	}
}

func createEnvironment(svc *projects.Service) func(context.Context, projects.Principal, *createEnvironmentInput) (*environmentOutput, error) {
	return func(ctx context.Context, principal projects.Principal, input *createEnvironmentInput) (*environmentOutput, error) {
		projectID, err := parseUUID(ctx, input.ProjectID, "project_id")
		if err != nil {
			return nil, err
		}
		env, err := svc.CreateEnvironment(ctx, principal, projects.CreateEnvironmentRequest{
			ProjectID:        projectID,
			Slug:             input.Body.Slug,
			DisplayName:      input.Body.DisplayName,
			Kind:             string(input.Body.Kind),
			ProtectionPolicy: input.Body.ProtectionPolicy,
			IdempotencyKey:   operationRequestInfoFromContext(ctx).IdempotencyKey,
		})
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &environmentOutput{Body: environmentDTO(env)}, nil
	}
}

func updateEnvironment(svc *projects.Service) func(context.Context, projects.Principal, *updateEnvironmentInput) (*environmentOutput, error) {
	return func(ctx context.Context, principal projects.Principal, input *updateEnvironmentInput) (*environmentOutput, error) {
		projectID, err := parseUUID(ctx, input.ProjectID, "project_id")
		if err != nil {
			return nil, err
		}
		environmentID, err := parseUUID(ctx, input.EnvironmentID, "environment_id")
		if err != nil {
			return nil, err
		}
		env, err := svc.UpdateEnvironment(ctx, principal, projects.UpdateEnvironmentRequest{
			ProjectID:        projectID,
			EnvironmentID:    environmentID,
			Version:          input.Body.Version.Int64(),
			DisplayName:      input.Body.DisplayName,
			ProtectionPolicy: input.Body.ProtectionPolicy,
			IdempotencyKey:   operationRequestInfoFromContext(ctx).IdempotencyKey,
		})
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &environmentOutput{Body: environmentDTO(env)}, nil
	}
}

func archiveEnvironment(svc *projects.Service) func(context.Context, projects.Principal, *environmentLifecycleInput) (*environmentOutput, error) {
	return func(ctx context.Context, principal projects.Principal, input *environmentLifecycleInput) (*environmentOutput, error) {
		projectID, err := parseUUID(ctx, input.ProjectID, "project_id")
		if err != nil {
			return nil, err
		}
		environmentID, err := parseUUID(ctx, input.EnvironmentID, "environment_id")
		if err != nil {
			return nil, err
		}
		env, err := svc.ArchiveEnvironment(ctx, principal, projects.EnvironmentLifecycleRequest{
			ProjectID:      projectID,
			EnvironmentID:  environmentID,
			Version:        input.Body.Version.Int64(),
			IdempotencyKey: operationRequestInfoFromContext(ctx).IdempotencyKey,
		})
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &environmentOutput{Body: environmentDTO(env)}, nil
	}
}

func parseUUID(ctx context.Context, value, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, badRequest(ctx, "invalid-"+field, field+" must be a UUID", err)
	}
	return id, nil
}

func projectDTO(project projects.Project) apiwire.Project {
	return apiwire.Project{
		ProjectID:          project.ID.String(),
		OrgID:              apiwire.Uint64(project.OrgID),
		Slug:               project.Slug,
		RedirectedFromSlug: project.RedirectedFromSlug,
		DisplayName:        project.DisplayName,
		Description:        project.Description,
		State:              apiwire.ProjectState(project.State),
		Version:            apiwire.Int64(project.Version),
		CreatedBy:          project.CreatedBy,
		UpdatedBy:          project.UpdatedBy,
		CreatedAt:          project.CreatedAt,
		UpdatedAt:          project.UpdatedAt,
		ArchivedAt:         project.ArchivedAt,
	}
}

func projectDTOs(records []projects.Project) []apiwire.Project {
	out := make([]apiwire.Project, 0, len(records))
	for _, record := range records {
		out = append(out, projectDTO(record))
	}
	return out
}

func environmentDTO(env projects.Environment) apiwire.ProjectEnvironment {
	return apiwire.ProjectEnvironment{
		EnvironmentID:    env.ID.String(),
		ProjectID:        env.ProjectID.String(),
		OrgID:            apiwire.Uint64(env.OrgID),
		Slug:             env.Slug,
		DisplayName:      env.DisplayName,
		Kind:             apiwire.ProjectEnvironmentKind(env.Kind),
		State:            apiwire.ProjectEnvironmentState(env.State),
		ProtectionPolicy: env.ProtectionPolicy,
		Version:          apiwire.Int64(env.Version),
		CreatedBy:        env.CreatedBy,
		UpdatedBy:        env.UpdatedBy,
		CreatedAt:        env.CreatedAt,
		UpdatedAt:        env.UpdatedAt,
		ArchivedAt:       env.ArchivedAt,
	}
}

func environmentDTOs(records []projects.Environment) []apiwire.ProjectEnvironment {
	out := make([]apiwire.ProjectEnvironment, 0, len(records))
	for _, record := range records {
		out = append(out, environmentDTO(record))
	}
	return out
}
