package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/verself/apiwire"
	workloadauth "github.com/verself/auth-middleware/workload"
	"github.com/verself/projects-service/internal/projects"
)

type resolveProjectInput struct {
	Body apiwire.ResolveProjectRequest
}

type resolveEnvironmentInput struct {
	Body apiwire.ResolveProjectEnvironmentRequest
}

type projectEventsInput struct {
	OrgID  apiwire.OrgID `query:"org_id" required:"true"`
	Cursor string        `query:"cursor,omitempty" maxLength:"256"`
	Limit  int           `query:"limit,omitempty" minimum:"1" maximum:"100"`
}

type resolveProjectOutput struct {
	Body apiwire.ResolveProjectResponse
}

type resolveEnvironmentOutput struct {
	Body apiwire.ResolveProjectEnvironmentResponse
}

type projectEventsOutput struct {
	Body apiwire.ProjectEventList
}

func RegisterInternalRoutes(api huma.API, svc *projects.Service) {
	registerProjectsRoute(api, huma.Operation{
		OperationID:   "resolve-project",
		Method:        http.MethodPost,
		Path:          "/internal/v1/projects/resolve",
		Summary:       "Resolve a project for a repo-owned service",
		DefaultStatus: http.StatusOK,
	}, operationPolicy{
		Permission:       permissionProjectResolve,
		Resource:         "project",
		Action:           "resolve",
		OrgScope:         "request_org_id",
		RateLimitClass:   "internal_read",
		AuditEvent:       "projects.project.resolve",
		OperationDisplay: "resolve project",
		OperationType:    "read",
		RiskLevel:        "medium",
		BodyLimitBytes:   bodyLimitSmallJSON,
		Internal:         true,
		InternalPeers: []string{
			workloadauth.ServiceSourceCodeHosting,
			workloadauth.ServiceSandboxRental,
		},
	}, resolveProject(svc))

	registerProjectsRoute(api, huma.Operation{
		OperationID:   "resolve-project-environment",
		Method:        http.MethodPost,
		Path:          "/internal/v1/project-environments/resolve",
		Summary:       "Resolve a project environment for a repo-owned service",
		DefaultStatus: http.StatusOK,
	}, operationPolicy{
		Permission:       permissionProjectResolve,
		Resource:         "project_environment",
		Action:           "resolve",
		OrgScope:         "request_org_id",
		RateLimitClass:   "internal_read",
		AuditEvent:       "projects.environment.resolve",
		OperationDisplay: "resolve project environment",
		OperationType:    "read",
		RiskLevel:        "medium",
		BodyLimitBytes:   bodyLimitSmallJSON,
		Internal:         true,
		InternalPeers: []string{
			workloadauth.ServiceSourceCodeHosting,
			workloadauth.ServiceSandboxRental,
		},
	}, resolveEnvironment(svc))

	registerProjectsRoute(api, huma.Operation{
		OperationID: "list-project-events-internal",
		Method:      http.MethodGet,
		Path:        "/internal/v1/project-events",
		Summary:     "List project domain events",
	}, operationPolicy{
		Permission:       permissionProjectEventRead,
		Resource:         "project_event",
		Action:           "list",
		OrgScope:         "request_org_id",
		RateLimitClass:   "internal_read",
		AuditEvent:       "projects.event.list",
		OperationDisplay: "list project events",
		OperationType:    "read",
		RiskLevel:        "medium",
		Internal:         true,
		InternalPeers:    []string{workloadauth.ServiceGovernance},
	}, listEvents(svc))
}

func resolveProject(svc *projects.Service) func(context.Context, projects.Principal, *resolveProjectInput) (*resolveProjectOutput, error) {
	return func(ctx context.Context, _ projects.Principal, input *resolveProjectInput) (*resolveProjectOutput, error) {
		projectID, err := optionalUUID(ctx, input.Body.ProjectID, "project_id")
		if err != nil {
			return nil, err
		}
		project, err := svc.ResolveProject(ctx, projects.ResolveProjectRequest{
			OrgID:         input.Body.OrgID.Uint64(),
			ProjectID:     projectID,
			Slug:          input.Body.Slug,
			RequireActive: input.Body.RequireActive,
		})
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &resolveProjectOutput{Body: apiwire.ResolveProjectResponse{Project: projectDTO(project)}}, nil
	}
}

func resolveEnvironment(svc *projects.Service) func(context.Context, projects.Principal, *resolveEnvironmentInput) (*resolveEnvironmentOutput, error) {
	return func(ctx context.Context, _ projects.Principal, input *resolveEnvironmentInput) (*resolveEnvironmentOutput, error) {
		projectID, err := parseUUID(ctx, input.Body.ProjectID, "project_id")
		if err != nil {
			return nil, err
		}
		environmentID, err := optionalUUID(ctx, input.Body.EnvironmentID, "environment_id")
		if err != nil {
			return nil, err
		}
		env, err := svc.ResolveEnvironment(ctx, projects.ResolveEnvironmentRequest{
			OrgID:         input.Body.OrgID.Uint64(),
			ProjectID:     projectID,
			EnvironmentID: environmentID,
			Slug:          input.Body.Slug,
			RequireActive: input.Body.RequireActive,
		})
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &resolveEnvironmentOutput{Body: apiwire.ResolveProjectEnvironmentResponse{Environment: environmentDTO(env)}}, nil
	}
}

func listEvents(svc *projects.Service) func(context.Context, projects.Principal, *projectEventsInput) (*projectEventsOutput, error) {
	return func(ctx context.Context, _ projects.Principal, input *projectEventsInput) (*projectEventsOutput, error) {
		events, nextCursor, err := svc.ListEvents(ctx, input.OrgID.Uint64(), input.Cursor, input.Limit)
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &projectEventsOutput{Body: apiwire.ProjectEventList{Events: eventDTOs(events), NextCursor: nextCursor}}, nil
	}
}

func optionalUUID(ctx context.Context, value, field string) (uuid.UUID, error) {
	if value == "" {
		return uuid.Nil, nil
	}
	return parseUUID(ctx, value, field)
}

func eventDTO(event projects.Event) apiwire.ProjectEvent {
	environmentID := ""
	if event.EnvironmentID != uuid.Nil {
		environmentID = event.EnvironmentID.String()
	}
	return apiwire.ProjectEvent{
		EventID:       event.ID.String(),
		OrgID:         apiwire.Uint64(event.OrgID),
		ProjectID:     event.ProjectID.String(),
		EnvironmentID: environmentID,
		EventType:     event.EventType,
		ActorID:       event.ActorID,
		Payload:       event.Payload,
		TraceID:       event.TraceID,
		CreatedAt:     event.CreatedAt,
	}
}

func eventDTOs(records []projects.Event) []apiwire.ProjectEvent {
	out := make([]apiwire.ProjectEvent, 0, len(records))
	for _, record := range records {
		out = append(out, eventDTO(record))
	}
	return out
}
