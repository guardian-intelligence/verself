package projects

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	projectstore "github.com/verself/projects-service/internal/store"
)

const defaultListLimit = 50

const (
	idempotencyResultProject     = "project"
	idempotencyResultEnvironment = "environment"
)

var storeTracer = otel.Tracer("projects-service/store")

type SQLStore struct {
	PG  *pgxpool.Pool
	Now func() time.Time
}

func (s SQLStore) q() *projectstore.Queries {
	return projectstore.New(s.PG)
}

type idempotencyProjectPayload struct {
	ID          uuid.UUID  `json:"id"`
	OrgID       uint64     `json:"org_id"`
	Slug        string     `json:"slug"`
	DisplayName string     `json:"display_name"`
	Description string     `json:"description"`
	State       string     `json:"state"`
	Version     int64      `json:"version"`
	CreatedBy   string     `json:"created_by"`
	UpdatedBy   string     `json:"updated_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ArchivedAt  *time.Time `json:"archived_at,omitempty"`
}

type idempotencyEnvironmentPayload struct {
	ID               uuid.UUID         `json:"id"`
	ProjectID        uuid.UUID         `json:"project_id"`
	OrgID            uint64            `json:"org_id"`
	Slug             string            `json:"slug"`
	DisplayName      string            `json:"display_name"`
	Kind             string            `json:"kind"`
	State            string            `json:"state"`
	ProtectionPolicy map[string]string `json:"protection_policy,omitempty"`
	Version          int64             `json:"version"`
	CreatedBy        string            `json:"created_by"`
	UpdatedBy        string            `json:"updated_by"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
	ArchivedAt       *time.Time        `json:"archived_at,omitempty"`
}

func (s SQLStore) Ready(ctx context.Context) error {
	if s.PG == nil {
		return ErrStoreUnavailable
	}
	if _, err := s.q().Ping(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) CreateProject(ctx context.Context, principal Principal, input CreateProjectRequest) (project Project, err error) {
	ctx, span := storeTracer.Start(ctx, "projects.pg.project.create")
	defer endSpan(span, err)
	if err := ValidatePrincipal(principal); err != nil {
		return Project{}, err
	}
	input, err = NormalizeCreateProject(input)
	if err != nil {
		return Project{}, err
	}
	orgID, err := pgOrgID(principal.OrgID)
	if err != nil {
		return Project{}, err
	}
	keyHash, err := requireIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return Project{}, err
	}
	requestHash, err := idempotencyRequestHash(map[string]any{
		"display_name": input.DisplayName,
		"description":  input.Description,
		"slug":         input.Slug,
	})
	if err != nil {
		return Project{}, err
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Project{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	q := projectstore.New(tx)
	operation := "project.create"
	if err := lockIdempotencyKey(ctx, q, orgID, operation, keyHash); err != nil {
		return Project{}, err
	}
	existing, found, err := s.loadIdempotentProject(ctx, q, orgID, operation, keyHash, requestHash)
	if err != nil || found {
		return existing, err
	}
	available, err := s.projectSlugAvailable(ctx, q, orgID, input.Slug, uuid.Nil)
	if err != nil {
		return Project{}, err
	}
	if !available {
		return Project{}, ErrConflict
	}
	now := s.now()
	projectID := uuid.New()
	project = Project{
		ID:          projectID,
		OrgID:       principal.OrgID,
		Slug:        input.Slug,
		DisplayName: input.DisplayName,
		Description: input.Description,
		State:       StateActive,
		Version:     1,
		CreatedBy:   principal.Subject,
		UpdatedBy:   principal.Subject,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := q.InsertProject(ctx, projectstore.InsertProjectParams{
		ProjectID:   project.ID,
		OrgID:       orgID,
		Slug:        project.Slug,
		DisplayName: project.DisplayName,
		Description: project.Description,
		State:       project.State,
		Version:     project.Version,
		CreatedBy:   project.CreatedBy,
		UpdatedBy:   project.UpdatedBy,
		CreatedAt:   timestamptz(project.CreatedAt),
		UpdatedAt:   timestamptz(project.UpdatedAt),
	}); err != nil {
		if uniqueViolation(err) {
			return Project{}, ErrConflict
		}
		return Project{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defaults := []Environment{
		defaultEnvironment(project, "production", "Production", EnvironmentKindProduction, principal.Subject, now),
		defaultEnvironment(project, "preview", "Preview", EnvironmentKindPreview, principal.Subject, now),
		defaultEnvironment(project, "development", "Development", EnvironmentKindDevelopment, principal.Subject, now),
	}
	for _, env := range defaults {
		if err := s.insertEnvironment(ctx, q, env); err != nil {
			return Project{}, err
		}
	}
	if err := s.insertProjectEvent(ctx, q, project.OrgID, project.ID, uuid.Nil, "project.created", principal.Subject, map[string]string{
		"slug":         project.Slug,
		"display_name": project.DisplayName,
	}); err != nil {
		return Project{}, err
	}
	if err := s.insertIdempotentProject(ctx, q, orgID, operation, keyHash, requestHash, project, now); err != nil {
		if uniqueViolation(err) {
			existing, found, loadErr := s.loadIdempotentProject(ctx, q, orgID, operation, keyHash, requestHash)
			if loadErr != nil || found {
				return existing, loadErr
			}
		}
		return Project{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Project{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return project, nil
}

func (s SQLStore) ListProjects(ctx context.Context, principal Principal, input ListProjectsRequest) (projects []Project, nextCursor string, err error) {
	ctx, span := storeTracer.Start(ctx, "projects.pg.project.list")
	defer endSpan(span, err)
	if err := ValidatePrincipal(principal); err != nil {
		return nil, "", err
	}
	orgID, err := pgOrgID(principal.OrgID)
	if err != nil {
		return nil, "", err
	}
	limit := normalizeLimit(input.Limit)
	createdBefore, idBefore, err := parseCursor(input.Cursor)
	if err != nil {
		return nil, "", err
	}
	q := s.q()
	var rows []projectstore.Project
	if input.State != "" {
		switch input.State {
		case StateActive, StateArchived:
			if !createdBefore.IsZero() && idBefore != uuid.Nil {
				rows, err = q.ListProjectsByStateAfterCursor(ctx, projectstore.ListProjectsByStateAfterCursorParams{
					OrgID:         orgID,
					State:         input.State,
					CreatedBefore: timestamptz(createdBefore),
					IDBefore:      idBefore,
					LimitCount:    int32(limit + 1),
				})
			} else {
				rows, err = q.ListProjectsByState(ctx, projectstore.ListProjectsByStateParams{
					OrgID: orgID,
					State: input.State,
					Limit: int32(limit + 1),
				})
			}
		default:
			return nil, "", fmt.Errorf("%w: invalid project state", ErrInvalid)
		}
	} else if !createdBefore.IsZero() && idBefore != uuid.Nil {
		rows, err = q.ListProjectsAfterCursor(ctx, projectstore.ListProjectsAfterCursorParams{
			OrgID:         orgID,
			CreatedBefore: timestamptz(createdBefore),
			IDBefore:      idBefore,
			LimitCount:    int32(limit + 1),
		})
	} else {
		rows, err = q.ListProjects(ctx, projectstore.ListProjectsParams{
			OrgID: orgID,
			Limit: int32(limit + 1),
		})
	}
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	for _, row := range rows {
		project, err := projectFromStore(row)
		if err != nil {
			return nil, "", err
		}
		projects = append(projects, project)
	}
	if len(projects) > limit {
		last := projects[limit-1]
		nextCursor = encodeCursor(last.CreatedAt, last.ID)
		projects = projects[:limit]
	}
	return projects, nextCursor, nil
}

func (s SQLStore) GetProject(ctx context.Context, principal Principal, projectID uuid.UUID) (project Project, err error) {
	ctx, span := storeTracer.Start(ctx, "projects.pg.project.get")
	defer endSpan(span, err)
	if err := ValidatePrincipal(principal); err != nil {
		return Project{}, err
	}
	return s.loadProjectByID(ctx, s.q(), principal.OrgID, projectID)
}

func (s SQLStore) UpdateProject(ctx context.Context, principal Principal, input UpdateProjectRequest) (project Project, err error) {
	ctx, span := storeTracer.Start(ctx, "projects.pg.project.update")
	defer endSpan(span, err)
	if err := ValidatePrincipal(principal); err != nil {
		return Project{}, err
	}
	input, err = NormalizeUpdateProject(input)
	if err != nil {
		return Project{}, err
	}
	orgID, err := pgOrgID(principal.OrgID)
	if err != nil {
		return Project{}, err
	}
	keyHash, err := requireIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return Project{}, err
	}
	requestHash, err := idempotencyRequestHash(map[string]any{
		"description":  input.Description,
		"display_name": input.DisplayName,
		"project_id":   input.ProjectID.String(),
		"slug":         input.Slug,
		"version":      input.Version,
	})
	if err != nil {
		return Project{}, err
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Project{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	q := projectstore.New(tx)
	operation := "project.update"
	if err := lockIdempotencyKey(ctx, q, orgID, operation, keyHash); err != nil {
		return Project{}, err
	}
	existing, found, err := s.loadIdempotentProject(ctx, q, orgID, operation, keyHash, requestHash)
	if err != nil || found {
		return existing, err
	}
	old, err := s.loadProjectByID(ctx, q, principal.OrgID, input.ProjectID)
	if err != nil {
		return Project{}, err
	}
	if old.State == StateArchived {
		return Project{}, ErrArchived
	}
	if old.Version != input.Version {
		return Project{}, ErrConflict
	}
	if input.Slug == "" {
		input.Slug = old.Slug
	}
	if input.DisplayName == "" {
		input.DisplayName = old.DisplayName
	}
	if input.Slug != old.Slug {
		available, err := s.projectSlugAvailable(ctx, q, orgID, input.Slug, old.ID)
		if err != nil {
			return Project{}, err
		}
		if !available {
			return Project{}, ErrConflict
		}
	}
	now := s.now()
	nextVersion := old.Version + 1
	rowsAffected, err := q.UpdateProject(ctx, projectstore.UpdateProjectParams{
		OrgID:       int64(old.OrgID),
		ProjectID:   old.ID,
		Slug:        input.Slug,
		DisplayName: input.DisplayName,
		Description: input.Description,
		UpdatedBy:   principal.Subject,
		UpdatedAt:   timestamptz(now),
		Version:     input.Version,
	})
	if err != nil {
		if uniqueViolation(err) {
			return Project{}, ErrConflict
		}
		return Project{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if rowsAffected != 1 {
		return Project{}, ErrConflict
	}
	if input.Slug != old.Slug {
		if err := s.insertProjectSlugRedirect(ctx, q, old.OrgID, old.ID, old.Slug, principal.Subject, now); err != nil {
			return Project{}, err
		}
	}
	if err := s.insertProjectEvent(ctx, q, old.OrgID, old.ID, uuid.Nil, "project.updated", principal.Subject, map[string]string{
		"version": strconv.FormatInt(nextVersion, 10),
		"slug":    input.Slug,
	}); err != nil {
		return Project{}, err
	}
	project, err = s.loadProjectByID(ctx, q, principal.OrgID, input.ProjectID)
	if err != nil {
		return Project{}, err
	}
	if err := s.insertIdempotentProject(ctx, q, orgID, operation, keyHash, requestHash, project, now); err != nil {
		if uniqueViolation(err) {
			existing, found, loadErr := s.loadIdempotentProject(ctx, q, orgID, operation, keyHash, requestHash)
			if loadErr != nil || found {
				return existing, loadErr
			}
		}
		return Project{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Project{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return project, nil
}

func (s SQLStore) ArchiveProject(ctx context.Context, principal Principal, input ProjectLifecycleRequest) (project Project, err error) {
	return s.setProjectState(ctx, principal, input, StateArchived, "project.archived")
}

func (s SQLStore) RestoreProject(ctx context.Context, principal Principal, input ProjectLifecycleRequest) (project Project, err error) {
	return s.setProjectState(ctx, principal, input, StateActive, "project.restored")
}

func (s SQLStore) CreateEnvironment(ctx context.Context, principal Principal, input CreateEnvironmentRequest) (env Environment, err error) {
	ctx, span := storeTracer.Start(ctx, "projects.pg.environment.create")
	defer endSpan(span, err)
	if err := ValidatePrincipal(principal); err != nil {
		return Environment{}, err
	}
	input, err = NormalizeCreateEnvironment(input)
	if err != nil {
		return Environment{}, err
	}
	orgID, err := pgOrgID(principal.OrgID)
	if err != nil {
		return Environment{}, err
	}
	keyHash, err := requireIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return Environment{}, err
	}
	requestHash, err := idempotencyRequestHash(map[string]any{
		"display_name":      input.DisplayName,
		"kind":              input.Kind,
		"project_id":        input.ProjectID.String(),
		"protection_policy": input.ProtectionPolicy,
		"slug":              input.Slug,
	})
	if err != nil {
		return Environment{}, err
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Environment{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	q := projectstore.New(tx)
	operation := "environment.create"
	if err := lockIdempotencyKey(ctx, q, orgID, operation, keyHash); err != nil {
		return Environment{}, err
	}
	existing, found, err := s.loadIdempotentEnvironment(ctx, q, orgID, operation, keyHash, requestHash)
	if err != nil || found {
		return existing, err
	}
	project, err := s.loadProjectByID(ctx, q, principal.OrgID, input.ProjectID)
	if err != nil {
		return Environment{}, err
	}
	if project.State != StateActive {
		return Environment{}, ErrArchived
	}
	now := s.now()
	env = Environment{
		ID:               uuid.New(),
		ProjectID:        project.ID,
		OrgID:            project.OrgID,
		Slug:             input.Slug,
		DisplayName:      input.DisplayName,
		Kind:             input.Kind,
		State:            StateActive,
		ProtectionPolicy: input.ProtectionPolicy,
		Version:          1,
		CreatedBy:        principal.Subject,
		UpdatedBy:        principal.Subject,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := s.insertEnvironment(ctx, q, env); err != nil {
		return Environment{}, err
	}
	if err := s.insertProjectEvent(ctx, q, project.OrgID, project.ID, env.ID, "project.environment.created", principal.Subject, map[string]string{
		"slug": env.Slug,
		"kind": env.Kind,
	}); err != nil {
		return Environment{}, err
	}
	if err := s.insertIdempotentEnvironment(ctx, q, orgID, operation, keyHash, requestHash, env, now); err != nil {
		if uniqueViolation(err) {
			existing, found, loadErr := s.loadIdempotentEnvironment(ctx, q, orgID, operation, keyHash, requestHash)
			if loadErr != nil || found {
				return existing, loadErr
			}
		}
		return Environment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Environment{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return env, nil
}

func (s SQLStore) ListEnvironments(ctx context.Context, principal Principal, projectID uuid.UUID) (envs []Environment, err error) {
	ctx, span := storeTracer.Start(ctx, "projects.pg.environment.list")
	defer endSpan(span, err)
	if err := ValidatePrincipal(principal); err != nil {
		return nil, err
	}
	q := s.q()
	project, err := s.loadProjectByID(ctx, q, principal.OrgID, projectID)
	if err != nil {
		return nil, err
	}
	rows, err := q.ListEnvironments(ctx, projectstore.ListEnvironmentsParams{
		OrgID:     int64(project.OrgID),
		ProjectID: project.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	for _, row := range rows {
		env, err := environmentFromStore(row)
		if err != nil {
			return nil, err
		}
		envs = append(envs, env)
	}
	return envs, nil
}

func (s SQLStore) UpdateEnvironment(ctx context.Context, principal Principal, input UpdateEnvironmentRequest) (env Environment, err error) {
	ctx, span := storeTracer.Start(ctx, "projects.pg.environment.update")
	defer endSpan(span, err)
	if err := ValidatePrincipal(principal); err != nil {
		return Environment{}, err
	}
	input, err = NormalizeUpdateEnvironment(input)
	if err != nil {
		return Environment{}, err
	}
	orgID, err := pgOrgID(principal.OrgID)
	if err != nil {
		return Environment{}, err
	}
	keyHash, err := requireIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return Environment{}, err
	}
	requestHash, err := idempotencyRequestHash(map[string]any{
		"display_name":      input.DisplayName,
		"environment_id":    input.EnvironmentID.String(),
		"project_id":        input.ProjectID.String(),
		"protection_policy": input.ProtectionPolicy,
		"version":           input.Version,
	})
	if err != nil {
		return Environment{}, err
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Environment{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	q := projectstore.New(tx)
	operation := "environment.update"
	if err := lockIdempotencyKey(ctx, q, orgID, operation, keyHash); err != nil {
		return Environment{}, err
	}
	existing, found, err := s.loadIdempotentEnvironment(ctx, q, orgID, operation, keyHash, requestHash)
	if err != nil || found {
		return existing, err
	}
	old, err := s.loadEnvironment(ctx, q, principal.OrgID, input.ProjectID, input.EnvironmentID)
	if err != nil {
		return Environment{}, err
	}
	if old.State != StateActive {
		return Environment{}, ErrArchived
	}
	if old.Version != input.Version {
		return Environment{}, ErrConflict
	}
	if input.DisplayName == "" {
		input.DisplayName = old.DisplayName
	}
	if input.ProtectionPolicy == nil {
		input.ProtectionPolicy = old.ProtectionPolicy
	}
	now := s.now()
	nextVersion := old.Version + 1
	policyJSON, err := json.Marshal(input.ProtectionPolicy)
	if err != nil {
		return Environment{}, fmt.Errorf("%w: invalid protection policy", ErrInvalid)
	}
	rowsAffected, err := q.UpdateEnvironment(ctx, projectstore.UpdateEnvironmentParams{
		OrgID:            int64(old.OrgID),
		ProjectID:        old.ProjectID,
		EnvironmentID:    old.ID,
		DisplayName:      input.DisplayName,
		ProtectionPolicy: policyJSON,
		UpdatedBy:        principal.Subject,
		UpdatedAt:        timestamptz(now),
		Version:          input.Version,
	})
	if err != nil {
		return Environment{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if rowsAffected != 1 {
		return Environment{}, ErrConflict
	}
	if err := s.insertProjectEvent(ctx, q, old.OrgID, old.ProjectID, old.ID, "project.environment.updated", principal.Subject, map[string]string{
		"version": strconv.FormatInt(nextVersion, 10),
		"slug":    old.Slug,
	}); err != nil {
		return Environment{}, err
	}
	env, err = s.loadEnvironment(ctx, q, principal.OrgID, input.ProjectID, input.EnvironmentID)
	if err != nil {
		return Environment{}, err
	}
	if err := s.insertIdempotentEnvironment(ctx, q, orgID, operation, keyHash, requestHash, env, now); err != nil {
		if uniqueViolation(err) {
			existing, found, loadErr := s.loadIdempotentEnvironment(ctx, q, orgID, operation, keyHash, requestHash)
			if loadErr != nil || found {
				return existing, loadErr
			}
		}
		return Environment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Environment{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return env, nil
}

func (s SQLStore) ArchiveEnvironment(ctx context.Context, principal Principal, input EnvironmentLifecycleRequest) (env Environment, err error) {
	return s.setEnvironmentState(ctx, principal, input, StateArchived, "project.environment.archived")
}

func (s SQLStore) ResolveProject(ctx context.Context, input ResolveProjectRequest) (project Project, err error) {
	ctx, span := storeTracer.Start(ctx, "projects.pg.project.resolve")
	defer endSpan(span, err)
	span.SetAttributes(
		attribute.Int64("verself.org_id", int64(input.OrgID)),
		attribute.String("projects.slug.requested", input.Slug),
		attribute.String("verself.project_id", input.ProjectID.String()),
	)
	if input.OrgID == 0 {
		return Project{}, fmt.Errorf("%w: org_id is required", ErrInvalid)
	}
	if input.ProjectID == uuid.Nil && strings.TrimSpace(input.Slug) == "" {
		return Project{}, fmt.Errorf("%w: project_id or slug is required", ErrInvalid)
	}
	q := s.q()
	if input.ProjectID != uuid.Nil {
		project, err = s.loadProjectByID(ctx, q, input.OrgID, input.ProjectID)
	} else {
		requestedSlug := normalizeSlug(input.Slug)
		project, err = s.loadProjectBySlug(ctx, q, input.OrgID, requestedSlug)
		if errors.Is(err, ErrNotFound) {
			project, err = s.loadProjectByRedirectSlug(ctx, q, input.OrgID, requestedSlug)
		}
	}
	if err != nil {
		return Project{}, err
	}
	span.SetAttributes(
		attribute.String("projects.slug.canonical", project.Slug),
		attribute.String("projects.slug.redirected_from", project.RedirectedFromSlug),
	)
	if input.RequireActive && project.State != StateActive {
		return Project{}, ErrArchived
	}
	return project, nil
}

func (s SQLStore) ResolveEnvironment(ctx context.Context, input ResolveEnvironmentRequest) (env Environment, err error) {
	ctx, span := storeTracer.Start(ctx, "projects.pg.environment.resolve")
	defer endSpan(span, err)
	if input.OrgID == 0 || input.ProjectID == uuid.Nil {
		return Environment{}, fmt.Errorf("%w: org_id and project_id are required", ErrInvalid)
	}
	if input.EnvironmentID == uuid.Nil && strings.TrimSpace(input.Slug) == "" {
		return Environment{}, fmt.Errorf("%w: environment_id or slug is required", ErrInvalid)
	}
	if _, err := s.ResolveProject(ctx, ResolveProjectRequest{OrgID: input.OrgID, ProjectID: input.ProjectID, RequireActive: input.RequireActive}); err != nil {
		return Environment{}, err
	}
	q := s.q()
	if input.EnvironmentID != uuid.Nil {
		env, err = s.loadEnvironment(ctx, q, input.OrgID, input.ProjectID, input.EnvironmentID)
	} else {
		env, err = s.loadEnvironmentBySlug(ctx, q, input.OrgID, input.ProjectID, normalizeSlug(input.Slug))
	}
	if err != nil {
		return Environment{}, err
	}
	if input.RequireActive && env.State != StateActive {
		return Environment{}, ErrArchived
	}
	return env, nil
}

func (s SQLStore) ListEvents(ctx context.Context, orgID uint64, cursor string, limit int) (events []Event, nextCursor string, err error) {
	ctx, span := storeTracer.Start(ctx, "projects.pg.event.list")
	defer endSpan(span, err)
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return nil, "", err
	}
	limit = normalizeLimit(limit)
	createdBefore, idBefore, err := parseCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	q := s.q()
	var rows []projectstore.ProjectEvent
	if !createdBefore.IsZero() && idBefore != uuid.Nil {
		rows, err = q.ListEventsAfterCursor(ctx, projectstore.ListEventsAfterCursorParams{
			OrgID:         pgOrg,
			CreatedBefore: timestamptz(createdBefore),
			IDBefore:      idBefore,
			LimitCount:    int32(limit + 1),
		})
	} else {
		rows, err = q.ListEvents(ctx, projectstore.ListEventsParams{
			OrgID: pgOrg,
			Limit: int32(limit + 1),
		})
	}
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	for _, row := range rows {
		event, err := eventFromStore(row)
		if err != nil {
			return nil, "", err
		}
		events = append(events, event)
	}
	if len(events) > limit {
		last := events[limit-1]
		nextCursor = encodeCursor(last.CreatedAt, last.ID)
		events = events[:limit]
	}
	return events, nextCursor, nil
}

func (s SQLStore) setProjectState(ctx context.Context, principal Principal, input ProjectLifecycleRequest, state, eventType string) (project Project, err error) {
	ctx, span := storeTracer.Start(ctx, "projects.pg.project.lifecycle", trace.WithAttributes(attribute.String("projects.target_state", state)))
	defer endSpan(span, err)
	if err := ValidatePrincipal(principal); err != nil {
		return Project{}, err
	}
	orgID, err := pgOrgID(principal.OrgID)
	if err != nil {
		return Project{}, err
	}
	keyHash, err := requireIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return Project{}, err
	}
	requestHash, err := idempotencyRequestHash(map[string]any{
		"project_id": input.ProjectID.String(),
		"state":      state,
		"version":    input.Version,
	})
	if err != nil {
		return Project{}, err
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Project{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	q := projectstore.New(tx)
	operation := eventType
	if err := lockIdempotencyKey(ctx, q, orgID, operation, keyHash); err != nil {
		return Project{}, err
	}
	existing, found, err := s.loadIdempotentProject(ctx, q, orgID, operation, keyHash, requestHash)
	if err != nil || found {
		return existing, err
	}
	old, err := s.loadProjectByID(ctx, q, principal.OrgID, input.ProjectID)
	if err != nil {
		return Project{}, err
	}
	if old.Version != input.Version {
		return Project{}, ErrConflict
	}
	now := s.now()
	var archivedAt pgtype.Timestamptz
	if state == StateArchived {
		archivedAt = timestamptz(now)
	}
	rowsAffected, err := q.SetProjectState(ctx, projectstore.SetProjectStateParams{
		OrgID:      int64(old.OrgID),
		ProjectID:  old.ID,
		State:      state,
		UpdatedBy:  principal.Subject,
		UpdatedAt:  timestamptz(now),
		Version:    input.Version,
		ArchivedAt: archivedAt,
	})
	if err != nil {
		return Project{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if rowsAffected != 1 {
		return Project{}, ErrConflict
	}
	if err := s.insertProjectEvent(ctx, q, old.OrgID, old.ID, uuid.Nil, eventType, principal.Subject, map[string]string{
		"state": state,
	}); err != nil {
		return Project{}, err
	}
	project, err = s.loadProjectByID(ctx, q, principal.OrgID, input.ProjectID)
	if err != nil {
		return Project{}, err
	}
	if err := s.insertIdempotentProject(ctx, q, orgID, operation, keyHash, requestHash, project, now); err != nil {
		if uniqueViolation(err) {
			existing, found, loadErr := s.loadIdempotentProject(ctx, q, orgID, operation, keyHash, requestHash)
			if loadErr != nil || found {
				return existing, loadErr
			}
		}
		return Project{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Project{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return project, nil
}

func (s SQLStore) setEnvironmentState(ctx context.Context, principal Principal, input EnvironmentLifecycleRequest, state, eventType string) (env Environment, err error) {
	ctx, span := storeTracer.Start(ctx, "projects.pg.environment.lifecycle", trace.WithAttributes(attribute.String("projects.target_state", state)))
	defer endSpan(span, err)
	if err := ValidatePrincipal(principal); err != nil {
		return Environment{}, err
	}
	orgID, err := pgOrgID(principal.OrgID)
	if err != nil {
		return Environment{}, err
	}
	keyHash, err := requireIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return Environment{}, err
	}
	requestHash, err := idempotencyRequestHash(map[string]any{
		"environment_id": input.EnvironmentID.String(),
		"project_id":     input.ProjectID.String(),
		"state":          state,
		"version":        input.Version,
	})
	if err != nil {
		return Environment{}, err
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Environment{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	q := projectstore.New(tx)
	operation := eventType
	if err := lockIdempotencyKey(ctx, q, orgID, operation, keyHash); err != nil {
		return Environment{}, err
	}
	existing, found, err := s.loadIdempotentEnvironment(ctx, q, orgID, operation, keyHash, requestHash)
	if err != nil || found {
		return existing, err
	}
	old, err := s.loadEnvironment(ctx, q, principal.OrgID, input.ProjectID, input.EnvironmentID)
	if err != nil {
		return Environment{}, err
	}
	if old.Version != input.Version {
		return Environment{}, ErrConflict
	}
	now := s.now()
	var archivedAt pgtype.Timestamptz
	if state == StateArchived {
		archivedAt = timestamptz(now)
	}
	rowsAffected, err := q.SetEnvironmentState(ctx, projectstore.SetEnvironmentStateParams{
		OrgID:         int64(old.OrgID),
		ProjectID:     old.ProjectID,
		EnvironmentID: old.ID,
		State:         state,
		UpdatedBy:     principal.Subject,
		UpdatedAt:     timestamptz(now),
		Version:       input.Version,
		ArchivedAt:    archivedAt,
	})
	if err != nil {
		return Environment{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if rowsAffected != 1 {
		return Environment{}, ErrConflict
	}
	if err := s.insertProjectEvent(ctx, q, old.OrgID, old.ProjectID, old.ID, eventType, principal.Subject, map[string]string{
		"state": state,
		"slug":  old.Slug,
	}); err != nil {
		return Environment{}, err
	}
	env, err = s.loadEnvironment(ctx, q, principal.OrgID, input.ProjectID, input.EnvironmentID)
	if err != nil {
		return Environment{}, err
	}
	if err := s.insertIdempotentEnvironment(ctx, q, orgID, operation, keyHash, requestHash, env, now); err != nil {
		if uniqueViolation(err) {
			existing, found, loadErr := s.loadIdempotentEnvironment(ctx, q, orgID, operation, keyHash, requestHash)
			if loadErr != nil || found {
				return existing, loadErr
			}
		}
		return Environment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Environment{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return env, nil
}

func (s SQLStore) insertEnvironment(ctx context.Context, q *projectstore.Queries, env Environment) error {
	policyJSON, err := json.Marshal(env.ProtectionPolicy)
	if err != nil {
		return fmt.Errorf("%w: invalid protection policy", ErrInvalid)
	}
	if err := q.InsertEnvironment(ctx, projectstore.InsertEnvironmentParams{
		EnvironmentID:    env.ID,
		ProjectID:        env.ProjectID,
		OrgID:            int64(env.OrgID),
		Slug:             env.Slug,
		DisplayName:      env.DisplayName,
		Kind:             env.Kind,
		State:            env.State,
		ProtectionPolicy: policyJSON,
		Version:          env.Version,
		CreatedBy:        env.CreatedBy,
		UpdatedBy:        env.UpdatedBy,
		CreatedAt:        timestamptz(env.CreatedAt),
		UpdatedAt:        timestamptz(env.UpdatedAt),
	}); err != nil {
		if uniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) insertProjectEvent(ctx context.Context, q *projectstore.Queries, orgID uint64, projectID, environmentID uuid.UUID, eventType, actorID string, payload map[string]string) error {
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("%w: invalid event payload", ErrInvalid)
	}
	traceID, traceparent := traceContext(ctx)
	if err := q.InsertProjectEvent(ctx, projectstore.InsertProjectEventParams{
		EventID:       uuid.New(),
		OrgID:         pgOrg,
		ProjectID:     projectID,
		EventType:     eventType,
		ActorID:       actorID,
		Payload:       payloadJSON,
		TraceID:       traceID,
		Traceparent:   traceparent,
		CreatedAt:     timestamptz(s.now()),
		EnvironmentID: nullableUUID(environmentID),
	}); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) loadProjectByID(ctx context.Context, q *projectstore.Queries, orgID uint64, projectID uuid.UUID) (Project, error) {
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return Project{}, err
	}
	row, err := q.GetProjectByID(ctx, projectstore.GetProjectByIDParams{OrgID: pgOrg, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return projectFromStore(row)
}

func (s SQLStore) loadProjectBySlug(ctx context.Context, q *projectstore.Queries, orgID uint64, slug string) (Project, error) {
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return Project{}, err
	}
	row, err := q.GetProjectBySlug(ctx, projectstore.GetProjectBySlugParams{OrgID: pgOrg, Slug: slug})
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return projectFromStore(row)
}

func (s SQLStore) loadProjectByRedirectSlug(ctx context.Context, q *projectstore.Queries, orgID uint64, slug string) (Project, error) {
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return Project{}, err
	}
	row, err := q.GetProjectByRedirectSlug(ctx, projectstore.GetProjectByRedirectSlugParams{OrgID: pgOrg, Slug: slug})
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	project, err := projectFromStore(row)
	if err != nil {
		return Project{}, err
	}
	project.RedirectedFromSlug = slug
	return project, nil
}

func (s SQLStore) projectSlugAvailable(ctx context.Context, q *projectstore.Queries, orgID int64, slug string, currentProjectID uuid.UUID) (bool, error) {
	var unavailable pgtype.Bool
	var err error
	if currentProjectID != uuid.Nil {
		unavailable, err = q.ProjectSlugUnavailableForOtherProject(ctx, projectstore.ProjectSlugUnavailableForOtherProjectParams{OrgID: orgID, Slug: slug, ProjectID: currentProjectID})
	} else {
		unavailable, err = q.ProjectSlugUnavailable(ctx, projectstore.ProjectSlugUnavailableParams{OrgID: orgID, Slug: slug})
	}
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return !unavailable.Bool, nil
}

func (s SQLStore) insertProjectSlugRedirect(ctx context.Context, q *projectstore.Queries, orgID uint64, projectID uuid.UUID, slug string, actorID string, createdAt time.Time) error {
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return err
	}
	if err := q.InsertProjectSlugRedirect(ctx, projectstore.InsertProjectSlugRedirectParams{
		OrgID:     pgOrg,
		Slug:      slug,
		ProjectID: projectID,
		CreatedBy: strings.TrimSpace(actorID),
		CreatedAt: timestamptz(createdAt),
	}); err != nil {
		if uniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) loadEnvironment(ctx context.Context, q *projectstore.Queries, orgID uint64, projectID, environmentID uuid.UUID) (Environment, error) {
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return Environment{}, err
	}
	row, err := q.GetEnvironmentByID(ctx, projectstore.GetEnvironmentByIDParams{OrgID: pgOrg, ProjectID: projectID, EnvironmentID: environmentID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Environment{}, ErrNotFound
	}
	if err != nil {
		return Environment{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return environmentFromStore(row)
}

func (s SQLStore) loadEnvironmentBySlug(ctx context.Context, q *projectstore.Queries, orgID uint64, projectID uuid.UUID, slug string) (Environment, error) {
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return Environment{}, err
	}
	row, err := q.GetEnvironmentBySlug(ctx, projectstore.GetEnvironmentBySlugParams{OrgID: pgOrg, ProjectID: projectID, Slug: slug})
	if errors.Is(err, pgx.ErrNoRows) {
		return Environment{}, ErrNotFound
	}
	if err != nil {
		return Environment{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return environmentFromStore(row)
}

func (s SQLStore) loadIdempotentProject(ctx context.Context, q *projectstore.Queries, orgID int64, operation, keyHash, requestHash string) (Project, bool, error) {
	row, err := q.GetProjectIdempotencyRecord(ctx, projectstore.GetProjectIdempotencyRecordParams{OrgID: orgID, Operation: operation, KeyHash: keyHash})
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, false, nil
	}
	if err != nil {
		return Project{}, false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if row.RequestHash != requestHash || row.ResultKind != idempotencyResultProject {
		return Project{}, true, fmt.Errorf("%w: idempotency key reused with a different project request", ErrConflict)
	}
	var stored idempotencyProjectPayload
	if err := json.Unmarshal(row.ResultPayload, &stored); err != nil {
		return Project{}, true, fmt.Errorf("%w: invalid idempotent project payload", ErrStoreUnavailable)
	}
	return projectFromPayload(stored), true, nil
}

func (s SQLStore) loadIdempotentEnvironment(ctx context.Context, q *projectstore.Queries, orgID int64, operation, keyHash, requestHash string) (Environment, bool, error) {
	row, err := q.GetProjectIdempotencyRecord(ctx, projectstore.GetProjectIdempotencyRecordParams{OrgID: orgID, Operation: operation, KeyHash: keyHash})
	if errors.Is(err, pgx.ErrNoRows) {
		return Environment{}, false, nil
	}
	if err != nil {
		return Environment{}, false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if row.RequestHash != requestHash || row.ResultKind != idempotencyResultEnvironment {
		return Environment{}, true, fmt.Errorf("%w: idempotency key reused with a different environment request", ErrConflict)
	}
	var stored idempotencyEnvironmentPayload
	if err := json.Unmarshal(row.ResultPayload, &stored); err != nil {
		return Environment{}, true, fmt.Errorf("%w: invalid idempotent environment payload", ErrStoreUnavailable)
	}
	return environmentFromPayload(stored), true, nil
}

func (s SQLStore) insertIdempotentProject(ctx context.Context, q *projectstore.Queries, orgID int64, operation, keyHash, requestHash string, project Project, createdAt time.Time) error {
	payload, err := json.Marshal(projectPayload(project))
	if err != nil {
		return fmt.Errorf("%w: invalid idempotent project payload", ErrInvalid)
	}
	if err := q.InsertProjectIdempotencyRecord(ctx, projectstore.InsertProjectIdempotencyRecordParams{
		OrgID:           orgID,
		Operation:       operation,
		KeyHash:         keyHash,
		RequestHash:     requestHash,
		ResultKind:      idempotencyResultProject,
		ResultProjectID: project.ID,
		ResultPayload:   payload,
		CreatedAt:       timestamptz(createdAt),
	}); err != nil {
		if uniqueViolation(err) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) insertIdempotentEnvironment(ctx context.Context, q *projectstore.Queries, orgID int64, operation, keyHash, requestHash string, env Environment, createdAt time.Time) error {
	payload, err := json.Marshal(environmentPayload(env))
	if err != nil {
		return fmt.Errorf("%w: invalid idempotent environment payload", ErrInvalid)
	}
	if err := q.InsertEnvironmentIdempotencyRecord(ctx, projectstore.InsertEnvironmentIdempotencyRecordParams{
		OrgID:               orgID,
		Operation:           operation,
		KeyHash:             keyHash,
		RequestHash:         requestHash,
		ResultKind:          idempotencyResultEnvironment,
		ResultProjectID:     env.ProjectID,
		ResultEnvironmentID: nullableUUID(env.ID),
		ResultPayload:       payload,
		CreatedAt:           timestamptz(createdAt),
	}); err != nil {
		if uniqueViolation(err) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func projectFromStore(row projectstore.Project) (Project, error) {
	orgID, err := uint64FromPGOrg(row.OrgID)
	if err != nil {
		return Project{}, err
	}
	project := Project{
		ID:          row.ProjectID,
		OrgID:       orgID,
		Slug:        row.Slug,
		DisplayName: row.DisplayName,
		Description: row.Description,
		State:       row.State,
		Version:     row.Version,
		CreatedBy:   row.CreatedBy,
		UpdatedBy:   row.UpdatedBy,
		CreatedAt:   requiredTime(row.CreatedAt),
		UpdatedAt:   requiredTime(row.UpdatedAt),
	}
	if row.ArchivedAt.Valid {
		t := row.ArchivedAt.Time.UTC()
		project.ArchivedAt = &t
	}
	return project, nil
}

func environmentFromStore(row projectstore.ProjectEnvironment) (Environment, error) {
	orgID, err := uint64FromPGOrg(row.OrgID)
	if err != nil {
		return Environment{}, err
	}
	env := Environment{
		ID:          row.EnvironmentID,
		ProjectID:   row.ProjectID,
		OrgID:       orgID,
		Slug:        row.Slug,
		DisplayName: row.DisplayName,
		Kind:        row.Kind,
		State:       row.State,
		Version:     row.Version,
		CreatedBy:   row.CreatedBy,
		UpdatedBy:   row.UpdatedBy,
		CreatedAt:   requiredTime(row.CreatedAt),
		UpdatedAt:   requiredTime(row.UpdatedAt),
	}
	env.ProtectionPolicy = map[string]string{}
	if len(row.ProtectionPolicy) > 0 {
		if err := json.Unmarshal(row.ProtectionPolicy, &env.ProtectionPolicy); err != nil {
			return Environment{}, fmt.Errorf("%w: invalid protection policy json", ErrStoreUnavailable)
		}
	}
	if row.ArchivedAt.Valid {
		t := row.ArchivedAt.Time.UTC()
		env.ArchivedAt = &t
	}
	return env, nil
}

func eventFromStore(row projectstore.ProjectEvent) (Event, error) {
	orgID, err := uint64FromPGOrg(row.OrgID)
	if err != nil {
		return Event{}, err
	}
	event := Event{
		ID:          row.EventID,
		OrgID:       orgID,
		ProjectID:   row.ProjectID,
		EventType:   row.EventType,
		ActorID:     row.ActorID,
		TraceID:     row.TraceID,
		Traceparent: row.Traceparent,
		CreatedAt:   requiredTime(row.CreatedAt),
	}
	if row.EnvironmentID.Valid {
		event.EnvironmentID = uuid.UUID(row.EnvironmentID.Bytes)
	}
	event.Payload = map[string]string{}
	if len(row.Payload) > 0 {
		if err := json.Unmarshal(row.Payload, &event.Payload); err != nil {
			return Event{}, fmt.Errorf("%w: invalid event payload json", ErrStoreUnavailable)
		}
	}
	return event, nil
}

func uint64FromPGOrg(value int64) (uint64, error) {
	if value <= 0 {
		return 0, fmt.Errorf("%w: org_id is out of range", ErrStoreUnavailable)
	}
	return uint64(value), nil
}

func requiredTime(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func timestamptz(value time.Time) pgtype.Timestamptz {
	if value.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func nullableUUID(value uuid.UUID) pgtype.UUID {
	if value == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: value, Valid: true}
}

func defaultEnvironment(project Project, slug, displayName, kind, actorID string, now time.Time) Environment {
	return Environment{
		ID:               uuid.New(),
		ProjectID:        project.ID,
		OrgID:            project.OrgID,
		Slug:             slug,
		DisplayName:      displayName,
		Kind:             kind,
		State:            StateActive,
		ProtectionPolicy: map[string]string{},
		Version:          1,
		CreatedBy:        actorID,
		UpdatedBy:        actorID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

func (s SQLStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func pgOrgID(orgID uint64) (int64, error) {
	if orgID == 0 || orgID > math.MaxInt64 {
		return 0, fmt.Errorf("%w: org_id is out of range", ErrInvalid)
	}
	return int64(orgID), nil
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return defaultListLimit
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func requireIdempotencyKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%w: idempotency_key is required", ErrInvalid)
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:]), nil
}

func idempotencyRequestHash(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("%w: invalid idempotency request hash payload", ErrInvalid)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func projectPayload(project Project) idempotencyProjectPayload {
	return idempotencyProjectPayload{
		ID:          project.ID,
		OrgID:       project.OrgID,
		Slug:        project.Slug,
		DisplayName: project.DisplayName,
		Description: project.Description,
		State:       project.State,
		Version:     project.Version,
		CreatedBy:   project.CreatedBy,
		UpdatedBy:   project.UpdatedBy,
		CreatedAt:   project.CreatedAt,
		UpdatedAt:   project.UpdatedAt,
		ArchivedAt:  project.ArchivedAt,
	}
}

func projectFromPayload(payload idempotencyProjectPayload) Project {
	return Project{
		ID:          payload.ID,
		OrgID:       payload.OrgID,
		Slug:        payload.Slug,
		DisplayName: payload.DisplayName,
		Description: payload.Description,
		State:       payload.State,
		Version:     payload.Version,
		CreatedBy:   payload.CreatedBy,
		UpdatedBy:   payload.UpdatedBy,
		CreatedAt:   payload.CreatedAt,
		UpdatedAt:   payload.UpdatedAt,
		ArchivedAt:  payload.ArchivedAt,
	}
}

func environmentPayload(env Environment) idempotencyEnvironmentPayload {
	return idempotencyEnvironmentPayload{
		ID:               env.ID,
		ProjectID:        env.ProjectID,
		OrgID:            env.OrgID,
		Slug:             env.Slug,
		DisplayName:      env.DisplayName,
		Kind:             env.Kind,
		State:            env.State,
		ProtectionPolicy: cloneStringMap(env.ProtectionPolicy),
		Version:          env.Version,
		CreatedBy:        env.CreatedBy,
		UpdatedBy:        env.UpdatedBy,
		CreatedAt:        env.CreatedAt,
		UpdatedAt:        env.UpdatedAt,
		ArchivedAt:       env.ArchivedAt,
	}
}

func environmentFromPayload(payload idempotencyEnvironmentPayload) Environment {
	return Environment{
		ID:               payload.ID,
		ProjectID:        payload.ProjectID,
		OrgID:            payload.OrgID,
		Slug:             payload.Slug,
		DisplayName:      payload.DisplayName,
		Kind:             payload.Kind,
		State:            payload.State,
		ProtectionPolicy: cloneStringMap(payload.ProtectionPolicy),
		Version:          payload.Version,
		CreatedBy:        payload.CreatedBy,
		UpdatedBy:        payload.UpdatedBy,
		CreatedAt:        payload.CreatedAt,
		UpdatedAt:        payload.UpdatedAt,
		ArchivedAt:       payload.ArchivedAt,
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func encodeCursor(createdAt time.Time, id uuid.UUID) string {
	payload := strconv.FormatInt(createdAt.UTC().UnixNano(), 10) + "|" + id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func parseCursor(cursor string) (time.Time, uuid.UUID, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return time.Time{}, uuid.Nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("%w: invalid cursor", ErrInvalid)
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, fmt.Errorf("%w: invalid cursor", ErrInvalid)
	}
	nanos, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("%w: invalid cursor", ErrInvalid)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("%w: invalid cursor", ErrInvalid)
	}
	return time.Unix(0, nanos).UTC(), id, nil
}

func uniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func lockIdempotencyKey(ctx context.Context, q *projectstore.Queries, orgID int64, operation, keyHash string) error {
	// Serialize by idempotency key before touching domain unique indexes.
	if err := q.LockIdempotencyKey(ctx, projectstore.LockIdempotencyKeyParams{LockKey: strconv.FormatInt(orgID, 10) + "|" + operation + "|" + keyHash}); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func traceContext(ctx context.Context) (string, string) {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return "", ""
	}
	flags := "00"
	if spanContext.TraceFlags().IsSampled() {
		flags = "01"
	}
	traceID := spanContext.TraceID().String()
	return traceID, "00-" + traceID + "-" + spanContext.SpanID().String() + "-" + flags
}

func endSpan(span trace.Span, err error) {
	if span == nil {
		return
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
