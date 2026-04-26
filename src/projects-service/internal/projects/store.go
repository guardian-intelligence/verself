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

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
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
	var one int
	if err := s.PG.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
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
	operation := "project.create"
	if err := lockIdempotencyKey(ctx, tx, orgID, operation, keyHash); err != nil {
		return Project{}, err
	}
	existing, found, err := s.loadIdempotentProject(ctx, tx, orgID, operation, keyHash, requestHash)
	if err != nil || found {
		return existing, err
	}
	available, err := s.projectSlugAvailable(ctx, tx, orgID, input.Slug, uuid.Nil)
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
	_, err = tx.Exec(ctx, `
INSERT INTO projects (
    project_id, org_id, slug, display_name, description, state, version, created_by, updated_by, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		project.ID, orgID, project.Slug, project.DisplayName, project.Description, project.State, project.Version, project.CreatedBy, project.UpdatedBy, project.CreatedAt, project.UpdatedAt)
	if err != nil {
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
		if err := s.insertEnvironment(ctx, tx, env); err != nil {
			return Project{}, err
		}
	}
	if err := s.insertProjectEvent(ctx, tx, project.OrgID, project.ID, uuid.Nil, "project.created", principal.Subject, map[string]string{
		"slug":         project.Slug,
		"display_name": project.DisplayName,
	}); err != nil {
		return Project{}, err
	}
	if err := s.insertIdempotentProject(ctx, tx, orgID, operation, keyHash, requestHash, project, now); err != nil {
		if uniqueViolation(err) {
			existing, found, loadErr := s.loadIdempotentProject(ctx, tx, orgID, operation, keyHash, requestHash)
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
	args := []any{orgID, limit + 1}
	where := []string{"org_id = $1"}
	if input.State != "" {
		switch input.State {
		case StateActive, StateArchived:
			args = append(args, input.State)
			where = append(where, fmt.Sprintf("state = $%d", len(args)))
		default:
			return nil, "", fmt.Errorf("%w: invalid project state", ErrInvalid)
		}
	}
	if !createdBefore.IsZero() && idBefore != uuid.Nil {
		args = append(args, createdBefore, idBefore)
		where = append(where, fmt.Sprintf("(created_at, project_id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	rows, err := s.PG.Query(ctx, `
SELECT project_id, org_id, slug, display_name, description, state, version, created_by, updated_by, created_at, updated_at, archived_at
FROM projects
WHERE `+strings.Join(where, " AND ")+`
ORDER BY created_at DESC, project_id DESC
LIMIT $2`, args...)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rows.Close()
	for rows.Next() {
		project, err := scanProjectRows(rows)
		if err != nil {
			return nil, "", err
		}
		projects = append(projects, project)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
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
	return s.loadProjectByID(ctx, s.PG, principal.OrgID, projectID)
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
	operation := "project.update"
	if err := lockIdempotencyKey(ctx, tx, orgID, operation, keyHash); err != nil {
		return Project{}, err
	}
	existing, found, err := s.loadIdempotentProject(ctx, tx, orgID, operation, keyHash, requestHash)
	if err != nil || found {
		return existing, err
	}
	old, err := s.loadProjectByID(ctx, tx, principal.OrgID, input.ProjectID)
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
		available, err := s.projectSlugAvailable(ctx, tx, orgID, input.Slug, old.ID)
		if err != nil {
			return Project{}, err
		}
		if !available {
			return Project{}, ErrConflict
		}
	}
	now := s.now()
	nextVersion := old.Version + 1
	tag, err := tx.Exec(ctx, `
UPDATE projects
SET slug = $3,
    display_name = $4,
    description = $5,
    version = version + 1,
    updated_by = $6,
    updated_at = $7
WHERE org_id = $1 AND project_id = $2 AND version = $8 AND state = 'active'`,
		int64(old.OrgID), old.ID, input.Slug, input.DisplayName, input.Description, principal.Subject, now, input.Version)
	if err != nil {
		if uniqueViolation(err) {
			return Project{}, ErrConflict
		}
		return Project{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if tag.RowsAffected() != 1 {
		return Project{}, ErrConflict
	}
	if input.Slug != old.Slug {
		if err := s.insertProjectSlugRedirect(ctx, tx, old.OrgID, old.ID, old.Slug, principal.Subject, now); err != nil {
			return Project{}, err
		}
	}
	if err := s.insertProjectEvent(ctx, tx, old.OrgID, old.ID, uuid.Nil, "project.updated", principal.Subject, map[string]string{
		"version": strconv.FormatInt(nextVersion, 10),
		"slug":    input.Slug,
	}); err != nil {
		return Project{}, err
	}
	project, err = s.loadProjectByID(ctx, tx, principal.OrgID, input.ProjectID)
	if err != nil {
		return Project{}, err
	}
	if err := s.insertIdempotentProject(ctx, tx, orgID, operation, keyHash, requestHash, project, now); err != nil {
		if uniqueViolation(err) {
			existing, found, loadErr := s.loadIdempotentProject(ctx, tx, orgID, operation, keyHash, requestHash)
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
	operation := "environment.create"
	if err := lockIdempotencyKey(ctx, tx, orgID, operation, keyHash); err != nil {
		return Environment{}, err
	}
	existing, found, err := s.loadIdempotentEnvironment(ctx, tx, orgID, operation, keyHash, requestHash)
	if err != nil || found {
		return existing, err
	}
	project, err := s.loadProjectByID(ctx, tx, principal.OrgID, input.ProjectID)
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
	if err := s.insertEnvironment(ctx, tx, env); err != nil {
		return Environment{}, err
	}
	if err := s.insertProjectEvent(ctx, tx, project.OrgID, project.ID, env.ID, "project.environment.created", principal.Subject, map[string]string{
		"slug": env.Slug,
		"kind": env.Kind,
	}); err != nil {
		return Environment{}, err
	}
	if err := s.insertIdempotentEnvironment(ctx, tx, orgID, operation, keyHash, requestHash, env, now); err != nil {
		if uniqueViolation(err) {
			existing, found, loadErr := s.loadIdempotentEnvironment(ctx, tx, orgID, operation, keyHash, requestHash)
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
	project, err := s.loadProjectByID(ctx, s.PG, principal.OrgID, projectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.PG.Query(ctx, `
SELECT environment_id, project_id, org_id, slug, display_name, kind, state, protection_policy, version, created_by, updated_by, created_at, updated_at, archived_at
FROM project_environments
WHERE org_id = $1 AND project_id = $2
ORDER BY kind, slug`, int64(project.OrgID), project.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rows.Close()
	for rows.Next() {
		env, err := scanEnvironmentRows(rows)
		if err != nil {
			return nil, err
		}
		envs = append(envs, env)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
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
	operation := "environment.update"
	if err := lockIdempotencyKey(ctx, tx, orgID, operation, keyHash); err != nil {
		return Environment{}, err
	}
	existing, found, err := s.loadIdempotentEnvironment(ctx, tx, orgID, operation, keyHash, requestHash)
	if err != nil || found {
		return existing, err
	}
	old, err := s.loadEnvironment(ctx, tx, principal.OrgID, input.ProjectID, input.EnvironmentID)
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
	tag, err := tx.Exec(ctx, `
UPDATE project_environments
SET display_name = $4,
    protection_policy = $5,
    version = version + 1,
    updated_by = $6,
    updated_at = $7
WHERE org_id = $1 AND project_id = $2 AND environment_id = $3 AND version = $8 AND state = 'active'`,
		int64(old.OrgID), old.ProjectID, old.ID, input.DisplayName, policyJSON, principal.Subject, now, input.Version)
	if err != nil {
		return Environment{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if tag.RowsAffected() != 1 {
		return Environment{}, ErrConflict
	}
	if err := s.insertProjectEvent(ctx, tx, old.OrgID, old.ProjectID, old.ID, "project.environment.updated", principal.Subject, map[string]string{
		"version": strconv.FormatInt(nextVersion, 10),
		"slug":    old.Slug,
	}); err != nil {
		return Environment{}, err
	}
	env, err = s.loadEnvironment(ctx, tx, principal.OrgID, input.ProjectID, input.EnvironmentID)
	if err != nil {
		return Environment{}, err
	}
	if err := s.insertIdempotentEnvironment(ctx, tx, orgID, operation, keyHash, requestHash, env, now); err != nil {
		if uniqueViolation(err) {
			existing, found, loadErr := s.loadIdempotentEnvironment(ctx, tx, orgID, operation, keyHash, requestHash)
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
	if input.ProjectID != uuid.Nil {
		project, err = s.loadProjectByID(ctx, s.PG, input.OrgID, input.ProjectID)
	} else {
		requestedSlug := normalizeSlug(input.Slug)
		project, err = s.loadProjectBySlug(ctx, s.PG, input.OrgID, requestedSlug)
		if errors.Is(err, ErrNotFound) {
			project, err = s.loadProjectByRedirectSlug(ctx, s.PG, input.OrgID, requestedSlug)
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
	if input.EnvironmentID != uuid.Nil {
		env, err = s.loadEnvironment(ctx, s.PG, input.OrgID, input.ProjectID, input.EnvironmentID)
	} else {
		env, err = s.loadEnvironmentBySlug(ctx, s.PG, input.OrgID, input.ProjectID, normalizeSlug(input.Slug))
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
	args := []any{pgOrg, limit + 1}
	where := []string{"org_id = $1"}
	if !createdBefore.IsZero() && idBefore != uuid.Nil {
		args = append(args, createdBefore, idBefore)
		where = append(where, fmt.Sprintf("(created_at, event_id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	rows, err := s.PG.Query(ctx, `
SELECT event_id, org_id, project_id, environment_id, event_type, actor_id, payload, trace_id, traceparent, created_at
FROM project_events
WHERE `+strings.Join(where, " AND ")+`
ORDER BY created_at DESC, event_id DESC
LIMIT $2`, args...)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rows.Close()
	for rows.Next() {
		event, err := scanEventRows(rows)
		if err != nil {
			return nil, "", err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
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
	operation := eventType
	if err := lockIdempotencyKey(ctx, tx, orgID, operation, keyHash); err != nil {
		return Project{}, err
	}
	existing, found, err := s.loadIdempotentProject(ctx, tx, orgID, operation, keyHash, requestHash)
	if err != nil || found {
		return existing, err
	}
	old, err := s.loadProjectByID(ctx, tx, principal.OrgID, input.ProjectID)
	if err != nil {
		return Project{}, err
	}
	if old.Version != input.Version {
		return Project{}, ErrConflict
	}
	now := s.now()
	var archivedAt any
	if state == StateArchived {
		archivedAt = now
	}
	tag, err := tx.Exec(ctx, `
UPDATE projects
SET state = $3,
    version = version + 1,
    updated_by = $4,
    updated_at = $5,
    archived_at = $6
WHERE org_id = $1 AND project_id = $2 AND version = $7`,
		int64(old.OrgID), old.ID, state, principal.Subject, now, archivedAt, input.Version)
	if err != nil {
		return Project{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if tag.RowsAffected() != 1 {
		return Project{}, ErrConflict
	}
	if err := s.insertProjectEvent(ctx, tx, old.OrgID, old.ID, uuid.Nil, eventType, principal.Subject, map[string]string{
		"state": state,
	}); err != nil {
		return Project{}, err
	}
	project, err = s.loadProjectByID(ctx, tx, principal.OrgID, input.ProjectID)
	if err != nil {
		return Project{}, err
	}
	if err := s.insertIdempotentProject(ctx, tx, orgID, operation, keyHash, requestHash, project, now); err != nil {
		if uniqueViolation(err) {
			existing, found, loadErr := s.loadIdempotentProject(ctx, tx, orgID, operation, keyHash, requestHash)
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
	operation := eventType
	if err := lockIdempotencyKey(ctx, tx, orgID, operation, keyHash); err != nil {
		return Environment{}, err
	}
	existing, found, err := s.loadIdempotentEnvironment(ctx, tx, orgID, operation, keyHash, requestHash)
	if err != nil || found {
		return existing, err
	}
	old, err := s.loadEnvironment(ctx, tx, principal.OrgID, input.ProjectID, input.EnvironmentID)
	if err != nil {
		return Environment{}, err
	}
	if old.Version != input.Version {
		return Environment{}, ErrConflict
	}
	now := s.now()
	var archivedAt any
	if state == StateArchived {
		archivedAt = now
	}
	tag, err := tx.Exec(ctx, `
UPDATE project_environments
SET state = $4,
    version = version + 1,
    updated_by = $5,
    updated_at = $6,
    archived_at = $7
WHERE org_id = $1 AND project_id = $2 AND environment_id = $3 AND version = $8`,
		int64(old.OrgID), old.ProjectID, old.ID, state, principal.Subject, now, archivedAt, input.Version)
	if err != nil {
		return Environment{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if tag.RowsAffected() != 1 {
		return Environment{}, ErrConflict
	}
	if err := s.insertProjectEvent(ctx, tx, old.OrgID, old.ProjectID, old.ID, eventType, principal.Subject, map[string]string{
		"state": state,
		"slug":  old.Slug,
	}); err != nil {
		return Environment{}, err
	}
	env, err = s.loadEnvironment(ctx, tx, principal.OrgID, input.ProjectID, input.EnvironmentID)
	if err != nil {
		return Environment{}, err
	}
	if err := s.insertIdempotentEnvironment(ctx, tx, orgID, operation, keyHash, requestHash, env, now); err != nil {
		if uniqueViolation(err) {
			existing, found, loadErr := s.loadIdempotentEnvironment(ctx, tx, orgID, operation, keyHash, requestHash)
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

func (s SQLStore) insertEnvironment(ctx context.Context, q queryer, env Environment) error {
	policyJSON, err := json.Marshal(env.ProtectionPolicy)
	if err != nil {
		return fmt.Errorf("%w: invalid protection policy", ErrInvalid)
	}
	_, err = q.Exec(ctx, `
INSERT INTO project_environments (
    environment_id, project_id, org_id, slug, display_name, kind, state, protection_policy, version, created_by, updated_by, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		env.ID, env.ProjectID, int64(env.OrgID), env.Slug, env.DisplayName, env.Kind, env.State, policyJSON, env.Version, env.CreatedBy, env.UpdatedBy, env.CreatedAt, env.UpdatedAt)
	if err != nil {
		if uniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) insertProjectEvent(ctx context.Context, q queryer, orgID uint64, projectID, environmentID uuid.UUID, eventType, actorID string, payload map[string]string) error {
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("%w: invalid event payload", ErrInvalid)
	}
	var envID any
	if environmentID != uuid.Nil {
		envID = environmentID
	}
	traceID, traceparent := traceContext(ctx)
	_, err = q.Exec(ctx, `
INSERT INTO project_events (
    event_id, org_id, project_id, environment_id, event_type, actor_id, payload, trace_id, traceparent, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		uuid.New(), pgOrg, projectID, envID, eventType, actorID, payloadJSON, traceID, traceparent, s.now())
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) loadProjectByID(ctx context.Context, q queryer, orgID uint64, projectID uuid.UUID) (Project, error) {
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return Project{}, err
	}
	return scanProjectRow(q.QueryRow(ctx, `
SELECT project_id, org_id, slug, display_name, description, state, version, created_by, updated_by, created_at, updated_at, archived_at
FROM projects
WHERE org_id = $1 AND project_id = $2`, pgOrg, projectID))
}

func (s SQLStore) loadProjectBySlug(ctx context.Context, q queryer, orgID uint64, slug string) (Project, error) {
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return Project{}, err
	}
	return scanProjectRow(q.QueryRow(ctx, `
SELECT project_id, org_id, slug, display_name, description, state, version, created_by, updated_by, created_at, updated_at, archived_at
FROM projects
WHERE org_id = $1 AND slug = $2`, pgOrg, slug))
}

func (s SQLStore) loadProjectByRedirectSlug(ctx context.Context, q queryer, orgID uint64, slug string) (Project, error) {
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return Project{}, err
	}
	project, err := scanProjectRow(q.QueryRow(ctx, `
SELECT p.project_id, p.org_id, p.slug, p.display_name, p.description, p.state, p.version, p.created_by, p.updated_by, p.created_at, p.updated_at, p.archived_at
FROM project_slug_redirects r
JOIN projects p ON p.project_id = r.project_id AND p.org_id = r.org_id
WHERE r.org_id = $1 AND r.slug = $2`, pgOrg, slug))
	if err != nil {
		return Project{}, err
	}
	project.RedirectedFromSlug = slug
	return project, nil
}

func (s SQLStore) projectSlugAvailable(ctx context.Context, q queryer, orgID int64, slug string, currentProjectID uuid.UUID) (bool, error) {
	var current any
	if currentProjectID != uuid.Nil {
		current = currentProjectID
	}
	var unavailable bool
	if err := q.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM projects WHERE org_id = $1 AND slug = $2 AND ($3::uuid IS NULL OR project_id <> $3::uuid)
) OR EXISTS (
    SELECT 1 FROM project_slug_redirects WHERE org_id = $1 AND slug = $2
)`, orgID, slug, current).Scan(&unavailable); err != nil {
		return false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return !unavailable, nil
}

func (s SQLStore) insertProjectSlugRedirect(ctx context.Context, q queryer, orgID uint64, projectID uuid.UUID, slug string, actorID string, createdAt time.Time) error {
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
INSERT INTO project_slug_redirects (org_id, slug, project_id, created_by, created_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT DO NOTHING`, pgOrg, slug, projectID, strings.TrimSpace(actorID), createdAt)
	if err != nil {
		if uniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) loadEnvironment(ctx context.Context, q queryer, orgID uint64, projectID, environmentID uuid.UUID) (Environment, error) {
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return Environment{}, err
	}
	return scanEnvironmentRow(q.QueryRow(ctx, `
SELECT environment_id, project_id, org_id, slug, display_name, kind, state, protection_policy, version, created_by, updated_by, created_at, updated_at, archived_at
FROM project_environments
WHERE org_id = $1 AND project_id = $2 AND environment_id = $3`, pgOrg, projectID, environmentID))
}

func (s SQLStore) loadEnvironmentBySlug(ctx context.Context, q queryer, orgID uint64, projectID uuid.UUID, slug string) (Environment, error) {
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return Environment{}, err
	}
	return scanEnvironmentRow(q.QueryRow(ctx, `
SELECT environment_id, project_id, org_id, slug, display_name, kind, state, protection_policy, version, created_by, updated_by, created_at, updated_at, archived_at
FROM project_environments
WHERE org_id = $1 AND project_id = $2 AND slug = $3`, pgOrg, projectID, slug))
}

func (s SQLStore) loadIdempotentProject(ctx context.Context, q queryer, orgID int64, operation, keyHash, requestHash string) (Project, bool, error) {
	var resultKind string
	var recordedRequestHash string
	var payload []byte
	err := q.QueryRow(ctx, `
SELECT result_kind, request_hash, result_payload
FROM project_idempotency_records
WHERE org_id = $1 AND operation = $2 AND key_hash = $3`, orgID, operation, keyHash).Scan(&resultKind, &recordedRequestHash, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, false, nil
	}
	if err != nil {
		return Project{}, false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if recordedRequestHash != requestHash || resultKind != idempotencyResultProject {
		return Project{}, true, fmt.Errorf("%w: idempotency key reused with a different project request", ErrConflict)
	}
	var stored idempotencyProjectPayload
	if err := json.Unmarshal(payload, &stored); err != nil {
		return Project{}, true, fmt.Errorf("%w: invalid idempotent project payload", ErrStoreUnavailable)
	}
	return projectFromPayload(stored), true, nil
}

func (s SQLStore) loadIdempotentEnvironment(ctx context.Context, q queryer, orgID int64, operation, keyHash, requestHash string) (Environment, bool, error) {
	var resultKind string
	var recordedRequestHash string
	var payload []byte
	err := q.QueryRow(ctx, `
SELECT result_kind, request_hash, result_payload
FROM project_idempotency_records
WHERE org_id = $1 AND operation = $2 AND key_hash = $3`, orgID, operation, keyHash).Scan(&resultKind, &recordedRequestHash, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return Environment{}, false, nil
	}
	if err != nil {
		return Environment{}, false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if recordedRequestHash != requestHash || resultKind != idempotencyResultEnvironment {
		return Environment{}, true, fmt.Errorf("%w: idempotency key reused with a different environment request", ErrConflict)
	}
	var stored idempotencyEnvironmentPayload
	if err := json.Unmarshal(payload, &stored); err != nil {
		return Environment{}, true, fmt.Errorf("%w: invalid idempotent environment payload", ErrStoreUnavailable)
	}
	return environmentFromPayload(stored), true, nil
}

func (s SQLStore) insertIdempotentProject(ctx context.Context, q queryer, orgID int64, operation, keyHash, requestHash string, project Project, createdAt time.Time) error {
	payload, err := json.Marshal(projectPayload(project))
	if err != nil {
		return fmt.Errorf("%w: invalid idempotent project payload", ErrInvalid)
	}
	_, err = q.Exec(ctx, `
INSERT INTO project_idempotency_records (
    org_id, operation, key_hash, request_hash, result_kind, result_project_id, result_environment_id, result_payload, created_at
) VALUES ($1, $2, $3, $4, $5, $6, NULL, $7, $8)`,
		orgID, operation, keyHash, requestHash, idempotencyResultProject, project.ID, payload, createdAt)
	if err != nil {
		if uniqueViolation(err) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) insertIdempotentEnvironment(ctx context.Context, q queryer, orgID int64, operation, keyHash, requestHash string, env Environment, createdAt time.Time) error {
	payload, err := json.Marshal(environmentPayload(env))
	if err != nil {
		return fmt.Errorf("%w: invalid idempotent environment payload", ErrInvalid)
	}
	_, err = q.Exec(ctx, `
INSERT INTO project_idempotency_records (
    org_id, operation, key_hash, request_hash, result_kind, result_project_id, result_environment_id, result_payload, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		orgID, operation, keyHash, requestHash, idempotencyResultEnvironment, env.ProjectID, env.ID, payload, createdAt)
	if err != nil {
		if uniqueViolation(err) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func scanProjectRow(row pgx.Row) (Project, error) {
	var project Project
	var orgID int64
	var archived pgtype.Timestamptz
	err := row.Scan(&project.ID, &orgID, &project.Slug, &project.DisplayName, &project.Description, &project.State, &project.Version, &project.CreatedBy, &project.UpdatedBy, &project.CreatedAt, &project.UpdatedAt, &archived)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	project.OrgID = uint64(orgID)
	if archived.Valid {
		t := archived.Time
		project.ArchivedAt = &t
	}
	return project, nil
}

func scanProjectRows(rows pgx.Rows) (Project, error) {
	var project Project
	var orgID int64
	var archived pgtype.Timestamptz
	if err := rows.Scan(&project.ID, &orgID, &project.Slug, &project.DisplayName, &project.Description, &project.State, &project.Version, &project.CreatedBy, &project.UpdatedBy, &project.CreatedAt, &project.UpdatedAt, &archived); err != nil {
		return Project{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	project.OrgID = uint64(orgID)
	if archived.Valid {
		t := archived.Time
		project.ArchivedAt = &t
	}
	return project, nil
}

func scanEnvironmentRow(row pgx.Row) (Environment, error) {
	var env Environment
	var orgID int64
	var policy []byte
	var archived pgtype.Timestamptz
	err := row.Scan(&env.ID, &env.ProjectID, &orgID, &env.Slug, &env.DisplayName, &env.Kind, &env.State, &policy, &env.Version, &env.CreatedBy, &env.UpdatedBy, &env.CreatedAt, &env.UpdatedAt, &archived)
	if errors.Is(err, pgx.ErrNoRows) {
		return Environment{}, ErrNotFound
	}
	if err != nil {
		return Environment{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	env.OrgID = uint64(orgID)
	env.ProtectionPolicy = map[string]string{}
	if len(policy) > 0 {
		if err := json.Unmarshal(policy, &env.ProtectionPolicy); err != nil {
			return Environment{}, fmt.Errorf("%w: invalid protection policy json", ErrStoreUnavailable)
		}
	}
	if archived.Valid {
		t := archived.Time
		env.ArchivedAt = &t
	}
	return env, nil
}

func scanEnvironmentRows(rows pgx.Rows) (Environment, error) {
	var env Environment
	var orgID int64
	var policy []byte
	var archived pgtype.Timestamptz
	if err := rows.Scan(&env.ID, &env.ProjectID, &orgID, &env.Slug, &env.DisplayName, &env.Kind, &env.State, &policy, &env.Version, &env.CreatedBy, &env.UpdatedBy, &env.CreatedAt, &env.UpdatedAt, &archived); err != nil {
		return Environment{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	env.OrgID = uint64(orgID)
	env.ProtectionPolicy = map[string]string{}
	if len(policy) > 0 {
		if err := json.Unmarshal(policy, &env.ProtectionPolicy); err != nil {
			return Environment{}, fmt.Errorf("%w: invalid protection policy json", ErrStoreUnavailable)
		}
	}
	if archived.Valid {
		t := archived.Time
		env.ArchivedAt = &t
	}
	return env, nil
}

func scanEventRows(rows pgx.Rows) (Event, error) {
	var event Event
	var orgID int64
	var envID pgtype.UUID
	var payload []byte
	if err := rows.Scan(&event.ID, &orgID, &event.ProjectID, &envID, &event.EventType, &event.ActorID, &payload, &event.TraceID, &event.Traceparent, &event.CreatedAt); err != nil {
		return Event{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	event.OrgID = uint64(orgID)
	if envID.Valid {
		event.EnvironmentID = uuid.UUID(envID.Bytes)
	}
	event.Payload = map[string]string{}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &event.Payload); err != nil {
			return Event{}, fmt.Errorf("%w: invalid event payload json", ErrStoreUnavailable)
		}
	}
	return event, nil
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

func lockIdempotencyKey(ctx context.Context, q queryer, orgID int64, operation, keyHash string) error {
	// Serialize by idempotency key before touching domain unique indexes.
	_, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0::bigint))`, strconv.FormatInt(orgID, 10)+"|"+operation+"|"+keyHash)
	if err != nil {
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
