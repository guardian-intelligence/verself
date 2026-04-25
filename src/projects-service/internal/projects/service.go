package projects

import (
	"context"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var serviceTracer = otel.Tracer("projects-service/projects")

type Store interface {
	Ready(context.Context) error
	CreateProject(context.Context, Principal, CreateProjectRequest) (Project, error)
	ListProjects(context.Context, Principal, ListProjectsRequest) ([]Project, string, error)
	GetProject(context.Context, Principal, uuid.UUID) (Project, error)
	UpdateProject(context.Context, Principal, UpdateProjectRequest) (Project, error)
	ArchiveProject(context.Context, Principal, ProjectLifecycleRequest) (Project, error)
	RestoreProject(context.Context, Principal, ProjectLifecycleRequest) (Project, error)
	CreateEnvironment(context.Context, Principal, CreateEnvironmentRequest) (Environment, error)
	ListEnvironments(context.Context, Principal, uuid.UUID) ([]Environment, error)
	UpdateEnvironment(context.Context, Principal, UpdateEnvironmentRequest) (Environment, error)
	ArchiveEnvironment(context.Context, Principal, EnvironmentLifecycleRequest) (Environment, error)
	ResolveProject(context.Context, ResolveProjectRequest) (Project, error)
	ResolveEnvironment(context.Context, ResolveEnvironmentRequest) (Environment, error)
	ListEvents(context.Context, uint64, string, int) ([]Event, string, error)
}

type Service struct {
	Store Store
}

func (s *Service) Ready(ctx context.Context) error {
	if s == nil || s.Store == nil {
		return ErrStoreUnavailable
	}
	return s.Store.Ready(ctx)
}

func (s *Service) CreateProject(ctx context.Context, principal Principal, input CreateProjectRequest) (project Project, err error) {
	ctx, span := startServiceSpan(ctx, "projects.project.create", principal)
	defer finishServiceSpan(span, err)
	return s.Store.CreateProject(ctx, principal, input)
}

func (s *Service) ListProjects(ctx context.Context, principal Principal, input ListProjectsRequest) (projects []Project, nextCursor string, err error) {
	ctx, span := startServiceSpan(ctx, "projects.project.list", principal)
	defer finishServiceSpan(span, err)
	return s.Store.ListProjects(ctx, principal, input)
}

func (s *Service) GetProject(ctx context.Context, principal Principal, projectID uuid.UUID) (project Project, err error) {
	ctx, span := startServiceSpan(ctx, "projects.project.get", principal, attribute.String("verself.project_id", projectID.String()))
	defer finishServiceSpan(span, err)
	return s.Store.GetProject(ctx, principal, projectID)
}

func (s *Service) UpdateProject(ctx context.Context, principal Principal, input UpdateProjectRequest) (project Project, err error) {
	ctx, span := startServiceSpan(ctx, "projects.project.update", principal, attribute.String("verself.project_id", input.ProjectID.String()))
	defer finishServiceSpan(span, err)
	return s.Store.UpdateProject(ctx, principal, input)
}

func (s *Service) ArchiveProject(ctx context.Context, principal Principal, input ProjectLifecycleRequest) (project Project, err error) {
	ctx, span := startServiceSpan(ctx, "projects.project.archive", principal, attribute.String("verself.project_id", input.ProjectID.String()))
	defer finishServiceSpan(span, err)
	return s.Store.ArchiveProject(ctx, principal, input)
}

func (s *Service) RestoreProject(ctx context.Context, principal Principal, input ProjectLifecycleRequest) (project Project, err error) {
	ctx, span := startServiceSpan(ctx, "projects.project.restore", principal, attribute.String("verself.project_id", input.ProjectID.String()))
	defer finishServiceSpan(span, err)
	return s.Store.RestoreProject(ctx, principal, input)
}

func (s *Service) CreateEnvironment(ctx context.Context, principal Principal, input CreateEnvironmentRequest) (env Environment, err error) {
	ctx, span := startServiceSpan(ctx, "projects.environment.create", principal, attribute.String("verself.project_id", input.ProjectID.String()))
	defer finishServiceSpan(span, err)
	return s.Store.CreateEnvironment(ctx, principal, input)
}

func (s *Service) ListEnvironments(ctx context.Context, principal Principal, projectID uuid.UUID) (envs []Environment, err error) {
	ctx, span := startServiceSpan(ctx, "projects.environment.list", principal, attribute.String("verself.project_id", projectID.String()))
	defer finishServiceSpan(span, err)
	return s.Store.ListEnvironments(ctx, principal, projectID)
}

func (s *Service) UpdateEnvironment(ctx context.Context, principal Principal, input UpdateEnvironmentRequest) (env Environment, err error) {
	ctx, span := startServiceSpan(ctx, "projects.environment.update", principal, attribute.String("verself.project_id", input.ProjectID.String()), attribute.String("verself.environment_id", input.EnvironmentID.String()))
	defer finishServiceSpan(span, err)
	return s.Store.UpdateEnvironment(ctx, principal, input)
}

func (s *Service) ArchiveEnvironment(ctx context.Context, principal Principal, input EnvironmentLifecycleRequest) (env Environment, err error) {
	ctx, span := startServiceSpan(ctx, "projects.environment.archive", principal, attribute.String("verself.project_id", input.ProjectID.String()), attribute.String("verself.environment_id", input.EnvironmentID.String()))
	defer finishServiceSpan(span, err)
	return s.Store.ArchiveEnvironment(ctx, principal, input)
}

func (s *Service) ResolveProject(ctx context.Context, input ResolveProjectRequest) (project Project, err error) {
	ctx, span := serviceTracer.Start(ctx, "projects.project.resolve", trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(
		attribute.String("verself.project_id", input.ProjectID.String()),
		attribute.Bool("projects.require_active", input.RequireActive),
	))
	defer finishServiceSpan(span, err)
	return s.Store.ResolveProject(ctx, input)
}

func (s *Service) ResolveEnvironment(ctx context.Context, input ResolveEnvironmentRequest) (env Environment, err error) {
	ctx, span := serviceTracer.Start(ctx, "projects.environment.resolve", trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(
		attribute.String("verself.project_id", input.ProjectID.String()),
		attribute.String("verself.environment_id", input.EnvironmentID.String()),
		attribute.Bool("projects.require_active", input.RequireActive),
	))
	defer finishServiceSpan(span, err)
	return s.Store.ResolveEnvironment(ctx, input)
}

func (s *Service) ListEvents(ctx context.Context, orgID uint64, cursor string, limit int) (events []Event, nextCursor string, err error) {
	ctx, span := serviceTracer.Start(ctx, "projects.event.list", trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attribute.Int64("verself.org_id", int64(orgID))))
	defer finishServiceSpan(span, err)
	return s.Store.ListEvents(ctx, orgID, cursor, limit)
}

func startServiceSpan(ctx context.Context, name string, principal Principal, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	all := []attribute.KeyValue{attribute.Int64("verself.org_id", int64(principal.OrgID))}
	if principal.Subject != "" {
		all = append(all, attribute.String("verself.subject_id", principal.Subject))
	}
	all = append(all, attrs...)
	return serviceTracer.Start(ctx, name, trace.WithAttributes(all...))
}

func finishServiceSpan(span trace.Span, err error) {
	if span == nil {
		return
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
