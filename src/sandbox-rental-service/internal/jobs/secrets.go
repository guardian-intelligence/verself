package jobs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	secretsinternalclient "github.com/forge-metal/secrets-service/internalclient"
)

const maxSecretEnvVars = 64

type SecretEnvVar struct {
	EnvName    string
	Kind       string
	SecretName string
	ScopeLevel string
	SourceID   string
	EnvID      string
	Branch     string
	GrantID    string
}

type SecretResolveRequest struct {
	OrgID       uint64
	ActorID     string
	ExecutionID uuid.UUID
	AttemptID   uuid.UUID
	Secrets     []SecretEnvVar
}

type SecretResolver interface {
	ResolveSandboxSecrets(ctx context.Context, request SecretResolveRequest) (map[string]string, error)
}

type SecretsResolver struct {
	Client *secretsinternalclient.ClientWithResponses
}

func NewSecretsResolver(client *secretsinternalclient.ClientWithResponses) *SecretsResolver {
	if client == nil {
		return nil
	}
	return &SecretsResolver{Client: client}
}

func (r *SecretsResolver) ResolveSandboxSecrets(ctx context.Context, request SecretResolveRequest) (map[string]string, error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.secrets.resolve")
	defer span.End()
	if r == nil || r.Client == nil {
		return nil, ErrSecretInjectionUnavailable
	}
	body := secretsinternalclient.InjectionResolveRequest{
		OrgId:       fmt.Sprintf("%d", request.OrgID),
		ActorId:     request.ActorID,
		ExecutionId: request.ExecutionID.String(),
		AttemptId:   request.AttemptID.String(),
		Secrets:     make([]secretsinternalclient.InjectionSecretRequest, 0, len(request.Secrets)),
	}
	for _, secret := range request.Secrets {
		item := secretsinternalclient.InjectionSecretRequest{
			EnvName:    secret.EnvName,
			SecretName: secret.SecretName,
			GrantId:    secret.GrantID,
		}
		if secret.Kind != "" {
			item.Kind = stringPtr(secret.Kind)
		}
		if secret.ScopeLevel != "" {
			item.ScopeLevel = stringPtr(secret.ScopeLevel)
		}
		if secret.SourceID != "" {
			item.SourceId = stringPtr(secret.SourceID)
		}
		if secret.EnvID != "" {
			item.EnvId = stringPtr(secret.EnvID)
		}
		if secret.Branch != "" {
			item.Branch = stringPtr(secret.Branch)
		}
		body.Secrets = append(body.Secrets, item)
	}
	resp, err := r.Client.ResolveInjectionWithResponse(ctx, body)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("%w: %v", ErrSecretInjectionUnavailable, err)
	}
	if resp.JSON200 == nil {
		err := fmt.Errorf("%w: secrets-service returned HTTP %d", ErrSecretInjectionUnavailable, resp.StatusCode())
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	out := make(map[string]string, len(resp.JSON200.Env))
	for _, item := range resp.JSON200.Env {
		out[item.Name] = item.Value
	}
	span.SetAttributes(attribute.Int("forge_metal.secret_env_count", len(out)))
	return out, nil
}

func normalizeSecretEnvVars(vars []SecretEnvVar) ([]SecretEnvVar, error) {
	if len(vars) == 0 {
		return nil, nil
	}
	if len(vars) > maxSecretEnvVars {
		return nil, fmt.Errorf("%w: at most %d secret environment variables are allowed", ErrInvalidSecretInjection, maxSecretEnvVars)
	}
	seen := map[string]struct{}{}
	out := make([]SecretEnvVar, 0, len(vars))
	for _, item := range vars {
		item.EnvName = strings.TrimSpace(item.EnvName)
		item.Kind = strings.TrimSpace(strings.ToLower(item.Kind))
		item.SecretName = strings.TrimSpace(item.SecretName)
		item.ScopeLevel = strings.TrimSpace(strings.ToLower(item.ScopeLevel))
		item.SourceID = strings.TrimSpace(item.SourceID)
		item.EnvID = strings.TrimSpace(item.EnvID)
		item.Branch = strings.TrimSpace(item.Branch)
		item.GrantID = strings.TrimSpace(item.GrantID)
		if item.Kind == "" {
			item.Kind = "secret"
		}
		if item.ScopeLevel == "" {
			item.ScopeLevel = "org"
		}
		if !validEnvName(item.EnvName) {
			return nil, fmt.Errorf("%w: invalid secret env_name %q", ErrInvalidSecretInjection, item.EnvName)
		}
		if strings.HasPrefix(item.EnvName, "FORGE_METAL_") {
			return nil, fmt.Errorf("%w: secret env_name %q uses reserved FORGE_METAL_ prefix", ErrInvalidSecretInjection, item.EnvName)
		}
		if _, exists := seen[item.EnvName]; exists {
			return nil, fmt.Errorf("%w: duplicate secret env_name %q", ErrInvalidSecretInjection, item.EnvName)
		}
		seen[item.EnvName] = struct{}{}
		if item.SecretName == "" {
			return nil, fmt.Errorf("%w: secret_name is required for env_name %q", ErrInvalidSecretInjection, item.EnvName)
		}
		if item.Kind != "secret" && item.Kind != "variable" {
			return nil, fmt.Errorf("%w: unsupported secret kind %q", ErrInvalidSecretInjection, item.Kind)
		}
		if err := validateSecretScope(item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func validEnvName(name string) bool {
	if name == "" || len(name) > 255 {
		return false
	}
	for idx, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r == '_':
		case idx > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	first := name[0]
	return (first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z') || first == '_'
}

func validateSecretScope(item SecretEnvVar) error {
	switch item.ScopeLevel {
	case "org":
		if item.SourceID != "" || item.EnvID != "" || item.Branch != "" {
			return fmt.Errorf("%w: org secret scope cannot include source_id, env_id, or branch", ErrInvalidSecretInjection)
		}
	case "source":
		if item.SourceID == "" || item.EnvID != "" || item.Branch != "" {
			return fmt.Errorf("%w: source secret scope requires source_id only", ErrInvalidSecretInjection)
		}
	case "environment":
		if item.SourceID == "" || item.EnvID == "" || item.Branch != "" {
			return fmt.Errorf("%w: environment secret scope requires source_id and env_id", ErrInvalidSecretInjection)
		}
	case "branch":
		if item.SourceID == "" || item.EnvID == "" || item.Branch == "" {
			return fmt.Errorf("%w: branch secret scope requires source_id, env_id, and branch", ErrInvalidSecretInjection)
		}
	default:
		return fmt.Errorf("%w: unsupported secret scope_level %q", ErrInvalidSecretInjection, item.ScopeLevel)
	}
	return nil
}

func assignSecretEnvGrantIDs(vars []SecretEnvVar) []SecretEnvVar {
	if len(vars) == 0 {
		return vars
	}
	out := make([]SecretEnvVar, len(vars))
	copy(out, vars)
	for idx := range out {
		if strings.TrimSpace(out[idx].GrantID) == "" {
			out[idx].GrantID = uuid.NewString()
		}
	}
	return out
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func verifySecretEnvGrants(ctx context.Context, item executionWorkItem) error {
	_, span := tracer.Start(ctx, "secrets.injection.reference.verify")
	defer span.End()
	span.SetAttributes(
		attribute.Int("forge_metal.secret_env_count", len(item.SecretEnv)),
		attribute.String("forge_metal.org_id", fmt.Sprintf("%d", item.OrgID)),
		attribute.String("forge_metal.actor_id", item.ActorID),
		attribute.String("forge_metal.execution_id", item.ExecutionID.String()),
		attribute.String("forge_metal.attempt_id", item.AttemptID.String()),
	)
	seen := map[string]struct{}{}
	for _, secret := range item.SecretEnv {
		grantID := strings.TrimSpace(secret.GrantID)
		if grantID == "" {
			err := fmt.Errorf("%w: missing secret injection grant", ErrInvalidSecretInjection)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		if _, err := uuid.Parse(grantID); err != nil {
			err := fmt.Errorf("%w: malformed secret injection grant", ErrInvalidSecretInjection)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		if _, exists := seen[grantID]; exists {
			err := fmt.Errorf("%w: duplicate secret injection grant", ErrInvalidSecretInjection)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		seen[grantID] = struct{}{}
	}
	return nil
}

func (s *Service) insertExecutionSecretEnv(ctx context.Context, tx pgx.Tx, executionID uuid.UUID, vars []SecretEnvVar) error {
	for idx, item := range vars {
		if _, err := tx.Exec(ctx, `INSERT INTO execution_secret_env (
			execution_id, env_name, kind, secret_name, scope_level, source_id, env_id, branch, grant_id, sort_order, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			executionID, item.EnvName, item.Kind, item.SecretName, item.ScopeLevel, item.SourceID, item.EnvID, item.Branch, item.GrantID, idx, time.Now().UTC()); err != nil {
			return fmt.Errorf("insert execution secret env %s: %w", item.EnvName, err)
		}
	}
	return nil
}

func (s *Service) loadExecutionSecretEnv(ctx context.Context, executionID uuid.UUID) ([]SecretEnvVar, error) {
	rows, err := s.PGX.Query(ctx, `SELECT env_name, kind, secret_name, scope_level, source_id, env_id, branch, grant_id
		FROM execution_secret_env
		WHERE execution_id = $1
		ORDER BY sort_order, env_name`, executionID)
	if err != nil {
		return nil, fmt.Errorf("load execution secret env: %w", err)
	}
	defer rows.Close()
	out := []SecretEnvVar{}
	for rows.Next() {
		var item SecretEnvVar
		if err := rows.Scan(&item.EnvName, &item.Kind, &item.SecretName, &item.ScopeLevel, &item.SourceID, &item.EnvID, &item.Branch, &item.GrantID); err != nil {
			return nil, fmt.Errorf("scan execution secret env: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate execution secret env: %w", err)
	}
	return out, nil
}

func (s *Service) resolveExecutionSecretEnv(ctx context.Context, item executionWorkItem) (map[string]string, error) {
	if len(item.SecretEnv) == 0 {
		return nil, nil
	}
	if s.Secrets == nil {
		return nil, ErrSecretInjectionUnavailable
	}
	if err := verifySecretEnvGrants(ctx, item); err != nil {
		return nil, err
	}
	return s.Secrets.ResolveSandboxSecrets(ctx, SecretResolveRequest{
		OrgID:       item.OrgID,
		ActorID:     item.ActorID,
		ExecutionID: item.ExecutionID,
		AttemptID:   item.AttemptID,
		Secrets:     item.SecretEnv,
	})
}
