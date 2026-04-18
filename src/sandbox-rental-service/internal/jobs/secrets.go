package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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

type SecretsHTTPResolver struct {
	URL    string
	Token  string
	Client *http.Client
}

func NewSecretsHTTPResolver(url, token string) *SecretsHTTPResolver {
	url = strings.TrimRight(strings.TrimSpace(url), "/")
	token = strings.TrimSpace(token)
	if url == "" || token == "" {
		return nil
	}
	return &SecretsHTTPResolver{
		URL:   url,
		Token: token,
		Client: &http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
			Timeout:   3 * time.Second,
		},
	}
}

func (r *SecretsHTTPResolver) ResolveSandboxSecrets(ctx context.Context, request SecretResolveRequest) (map[string]string, error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.secrets.resolve")
	defer span.End()
	if r == nil || r.URL == "" || r.Token == "" {
		return nil, ErrSecretInjectionUnavailable
	}
	if r.Client == nil {
		r.Client = &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport), Timeout: 3 * time.Second}
	}
	body := secretsResolveRequest{
		OrgID:       fmt.Sprintf("%d", request.OrgID),
		ActorID:     request.ActorID,
		ExecutionID: request.ExecutionID.String(),
		AttemptID:   request.AttemptID.String(),
		Secrets:     make([]secretsResolveItem, 0, len(request.Secrets)),
	}
	for _, secret := range request.Secrets {
		body.Secrets = append(body.Secrets, secretsResolveItem{
			EnvName:    secret.EnvName,
			Kind:       secret.Kind,
			SecretName: secret.SecretName,
			ScopeLevel: secret.ScopeLevel,
			SourceID:   secret.SourceID,
			EnvID:      secret.EnvID,
			Branch:     secret.Branch,
		})
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.URL+"/internal/v1/injections/resolve", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.Client.Do(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("%w: %v", ErrSecretInjectionUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("%w: secrets-service returned HTTP %d", ErrSecretInjectionUnavailable, resp.StatusCode)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	var decoded secretsResolveResponse
	decoder := json.NewDecoder(resp.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("%w: decode injection response: %v", ErrSecretInjectionUnavailable, err)
	}
	out := make(map[string]string, len(decoded.Env))
	for _, item := range decoded.Env {
		out[item.Name] = item.Value
	}
	span.SetAttributes(attribute.Int("forge_metal.secret_env_count", len(out)))
	return out, nil
}

type secretsResolveRequest struct {
	OrgID       string               `json:"org_id"`
	ActorID     string               `json:"actor_id"`
	ExecutionID string               `json:"execution_id"`
	AttemptID   string               `json:"attempt_id"`
	Secrets     []secretsResolveItem `json:"secrets"`
}

type secretsResolveItem struct {
	EnvName    string `json:"env_name"`
	Kind       string `json:"kind,omitempty"`
	SecretName string `json:"secret_name"`
	ScopeLevel string `json:"scope_level,omitempty"`
	SourceID   string `json:"source_id,omitempty"`
	EnvID      string `json:"env_id,omitempty"`
	Branch     string `json:"branch,omitempty"`
}

type secretsResolveResponse struct {
	Env []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"env"`
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

func (s *Service) insertExecutionSecretEnv(ctx context.Context, tx pgx.Tx, executionID uuid.UUID, vars []SecretEnvVar) error {
	for idx, item := range vars {
		if _, err := tx.Exec(ctx, `INSERT INTO execution_secret_env (
			execution_id, env_name, kind, secret_name, scope_level, source_id, env_id, branch, sort_order, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			executionID, item.EnvName, item.Kind, item.SecretName, item.ScopeLevel, item.SourceID, item.EnvID, item.Branch, idx, time.Now().UTC()); err != nil {
			return fmt.Errorf("insert execution secret env %s: %w", item.EnvName, err)
		}
	}
	return nil
}

func (s *Service) loadExecutionSecretEnv(ctx context.Context, executionID uuid.UUID) ([]SecretEnvVar, error) {
	rows, err := s.PGX.Query(ctx, `SELECT env_name, kind, secret_name, scope_level, source_id, env_id, branch
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
		if err := rows.Scan(&item.EnvName, &item.Kind, &item.SecretName, &item.ScopeLevel, &item.SourceID, &item.EnvID, &item.Branch); err != nil {
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
	return s.Secrets.ResolveSandboxSecrets(ctx, SecretResolveRequest{
		OrgID:       item.OrgID,
		ActorID:     item.ActorID,
		ExecutionID: item.ExecutionID,
		AttemptID:   item.AttemptID,
		Secrets:     item.SecretEnv,
	})
}
