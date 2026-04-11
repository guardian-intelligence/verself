package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	WebhookProviderForgejo = "forgejo"
	WebhookProviderGitHub  = "github"
	WebhookProviderGitLab  = "gitlab"

	GitIntegrationModeManualWebhook = "manual_webhook"
)

var (
	ErrWebhookEndpointMissing     = errors.New("sandbox-rental: webhook endpoint not found")
	ErrWebhookEndpointInvalid     = errors.New("sandbox-rental: invalid webhook endpoint")
	ErrWebhookProviderUnsupported = errors.New("sandbox-rental: webhook provider unsupported")
)

type CreateWebhookEndpointRequest struct {
	OrgID        uint64
	ActorID      string
	Provider     string
	ProviderHost string
	Label        string
}

type WebhookEndpointRecord struct {
	EndpointID        uuid.UUID
	IntegrationID     uuid.UUID
	OrgID             uint64
	Provider          string
	ProviderHost      string
	Label             string
	Active            bool
	CreatedBy         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	LastDeliveryAt    *time.Time
	DeliveryCount     int64
	SecretFingerprint string
}

type CreateWebhookEndpointResult struct {
	Endpoint WebhookEndpointRecord
	Secret   string
}

type RotateWebhookEndpointSecretResult struct {
	EndpointID         uuid.UUID
	Secret             string
	SecretFingerprint  string
	RotatedAt          time.Time
	PreviousRetiringAt time.Time
}

type WebhookEndpointForIngest struct {
	EndpointID    uuid.UUID
	IntegrationID uuid.UUID
	OrgID         uint64
	Provider      string
	ProviderHost  string
	Secrets       []string
}

func (s *Service) CreateWebhookEndpoint(ctx context.Context, req CreateWebhookEndpointRequest) (CreateWebhookEndpointResult, error) {
	req, err := normalizeCreateWebhookEndpointRequest(req)
	if err != nil {
		return CreateWebhookEndpointResult{}, err
	}
	secret, err := GenerateWebhookSecret()
	if err != nil {
		return CreateWebhookEndpointResult{}, err
	}
	ciphertext, fingerprint, err := s.encryptWebhookSecret(secret)
	if err != nil {
		return CreateWebhookEndpointResult{}, err
	}

	now := time.Now().UTC()
	endpointID := uuid.New()
	secretID := uuid.New()
	tx, err := s.PG.BeginTx(ctx, nil)
	if err != nil {
		return CreateWebhookEndpointResult{}, fmt.Errorf("begin webhook endpoint create: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	integrationID, err := s.findOrCreateWebhookIntegration(ctx, tx, req, now)
	if err != nil {
		return CreateWebhookEndpointResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO webhook_endpoints (
			endpoint_id, integration_id, org_id, provider, provider_host, label,
			active, created_by, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			true, $7, $8, $8
		)
	`, endpointID, integrationID, int64(req.OrgID), req.Provider, req.ProviderHost, req.Label, req.ActorID, now); err != nil {
		return CreateWebhookEndpointResult{}, fmt.Errorf("insert webhook endpoint: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO webhook_endpoint_secrets (
			secret_id, endpoint_id, secret_ciphertext, secret_fingerprint,
			active_from, created_by, created_at
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $5
		)
	`, secretID, endpointID, ciphertext, fingerprint, now, req.ActorID); err != nil {
		return CreateWebhookEndpointResult{}, fmt.Errorf("insert webhook endpoint secret: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return CreateWebhookEndpointResult{}, fmt.Errorf("commit webhook endpoint create: %w", err)
	}
	record, err := s.GetWebhookEndpoint(ctx, req.OrgID, endpointID)
	if err != nil {
		return CreateWebhookEndpointResult{}, err
	}
	return CreateWebhookEndpointResult{Endpoint: *record, Secret: secret}, nil
}

func (s *Service) ListWebhookEndpoints(ctx context.Context, orgID uint64) ([]WebhookEndpointRecord, error) {
	rows, err := s.PG.QueryContext(ctx, `
		SELECT
			e.endpoint_id,
			e.integration_id,
			e.org_id,
			e.provider,
			e.provider_host,
			e.label,
			e.active,
			e.created_by,
			e.created_at,
			e.updated_at,
			e.last_delivery_at,
			e.delivery_count,
			COALESCE(s.secret_fingerprint, '')
		FROM webhook_endpoints e
		LEFT JOIN LATERAL (
			SELECT secret_fingerprint
			FROM webhook_endpoint_secrets s
			WHERE s.endpoint_id = e.endpoint_id
			  AND s.revoked_at IS NULL
			ORDER BY s.active_from DESC
			LIMIT 1
		) s ON true
		WHERE e.org_id = $1
		ORDER BY e.active DESC, e.created_at DESC
	`, int64(orgID))
	if err != nil {
		return nil, fmt.Errorf("query webhook endpoints: %w", err)
	}
	defer rows.Close()

	var endpoints []WebhookEndpointRecord
	for rows.Next() {
		record, err := scanWebhookEndpointRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan webhook endpoint: %w", err)
		}
		endpoints = append(endpoints, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return endpoints, nil
}

func (s *Service) GetWebhookEndpoint(ctx context.Context, orgID uint64, endpointID uuid.UUID) (*WebhookEndpointRecord, error) {
	record, err := scanWebhookEndpointRow(s.PG.QueryRowContext(ctx, `
		SELECT
			e.endpoint_id,
			e.integration_id,
			e.org_id,
			e.provider,
			e.provider_host,
			e.label,
			e.active,
			e.created_by,
			e.created_at,
			e.updated_at,
			e.last_delivery_at,
			e.delivery_count,
			COALESCE(s.secret_fingerprint, '')
		FROM webhook_endpoints e
		LEFT JOIN LATERAL (
			SELECT secret_fingerprint
			FROM webhook_endpoint_secrets s
			WHERE s.endpoint_id = e.endpoint_id
			  AND s.revoked_at IS NULL
			ORDER BY s.active_from DESC
			LIMIT 1
		) s ON true
		WHERE e.org_id = $1 AND e.endpoint_id = $2
	`, int64(orgID), endpointID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWebhookEndpointMissing
		}
		return nil, fmt.Errorf("scan webhook endpoint: %w", err)
	}
	return record, nil
}

func (s *Service) RotateWebhookEndpointSecret(ctx context.Context, orgID uint64, endpointID uuid.UUID, actorID string) (RotateWebhookEndpointSecretResult, error) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return RotateWebhookEndpointSecretResult{}, fmt.Errorf("actor_id is required")
	}
	secret, err := GenerateWebhookSecret()
	if err != nil {
		return RotateWebhookEndpointSecretResult{}, err
	}
	ciphertext, fingerprint, err := s.encryptWebhookSecret(secret)
	if err != nil {
		return RotateWebhookEndpointSecretResult{}, err
	}

	now := time.Now().UTC()
	retiringAt := now.Add(24 * time.Hour)
	tx, err := s.PG.BeginTx(ctx, nil)
	if err != nil {
		return RotateWebhookEndpointSecretResult{}, fmt.Errorf("begin webhook secret rotate: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT true
		FROM webhook_endpoints
		WHERE org_id = $1 AND endpoint_id = $2 AND active = true
		FOR UPDATE
	`, int64(orgID), endpointID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RotateWebhookEndpointSecretResult{}, ErrWebhookEndpointMissing
		}
		return RotateWebhookEndpointSecretResult{}, fmt.Errorf("lock webhook endpoint for rotate: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE webhook_endpoint_secrets
		SET retiring_at = COALESCE(retiring_at, $2)
		WHERE endpoint_id = $1 AND revoked_at IS NULL
	`, endpointID, retiringAt); err != nil {
		return RotateWebhookEndpointSecretResult{}, fmt.Errorf("mark previous webhook secrets retiring: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO webhook_endpoint_secrets (
			secret_id, endpoint_id, secret_ciphertext, secret_fingerprint,
			active_from, created_by, created_at
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $5
		)
	`, uuid.New(), endpointID, ciphertext, fingerprint, now, actorID); err != nil {
		return RotateWebhookEndpointSecretResult{}, fmt.Errorf("insert rotated webhook secret: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE webhook_endpoints
		SET updated_at = $2
		WHERE endpoint_id = $1
	`, endpointID, now); err != nil {
		return RotateWebhookEndpointSecretResult{}, fmt.Errorf("touch webhook endpoint after rotate: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return RotateWebhookEndpointSecretResult{}, fmt.Errorf("commit webhook secret rotate: %w", err)
	}
	return RotateWebhookEndpointSecretResult{
		EndpointID:         endpointID,
		Secret:             secret,
		SecretFingerprint:  fingerprint,
		RotatedAt:          now,
		PreviousRetiringAt: retiringAt,
	}, nil
}

func (s *Service) DeactivateWebhookEndpoint(ctx context.Context, orgID uint64, endpointID uuid.UUID) error {
	res, err := s.PG.ExecContext(ctx, `
		UPDATE webhook_endpoints
		SET active = false,
		    updated_at = $3
		WHERE org_id = $1 AND endpoint_id = $2 AND active = true
	`, int64(orgID), endpointID, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("deactivate webhook endpoint: %w", err)
	}
	return ensureRowsAffected(res, ErrWebhookEndpointMissing)
}

func (s *Service) LookupWebhookEndpointForIngest(ctx context.Context, endpointID uuid.UUID) (*WebhookEndpointForIngest, error) {
	var endpoint WebhookEndpointForIngest
	if err := s.PG.QueryRowContext(ctx, `
		SELECT endpoint_id, integration_id, org_id, provider, provider_host
		FROM webhook_endpoints
		WHERE endpoint_id = $1 AND active = true
	`, endpointID).Scan(
		&endpoint.EndpointID,
		&endpoint.IntegrationID,
		&endpoint.OrgID,
		&endpoint.Provider,
		&endpoint.ProviderHost,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWebhookEndpointMissing
		}
		return nil, fmt.Errorf("lookup webhook endpoint: %w", err)
	}

	rows, err := s.PG.QueryContext(ctx, `
		SELECT secret_ciphertext
		FROM webhook_endpoint_secrets
		WHERE endpoint_id = $1
		  AND revoked_at IS NULL
		  AND (retiring_at IS NULL OR retiring_at > now())
		ORDER BY active_from DESC
	`, endpointID)
	if err != nil {
		return nil, fmt.Errorf("query webhook endpoint secrets: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ciphertext string
		if err := rows.Scan(&ciphertext); err != nil {
			return nil, fmt.Errorf("scan webhook endpoint secret: %w", err)
		}
		secret, err := s.decryptWebhookSecret(ciphertext)
		if err != nil {
			return nil, err
		}
		endpoint.Secrets = append(endpoint.Secrets, secret)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(endpoint.Secrets) == 0 {
		return nil, fmt.Errorf("webhook endpoint has no active secrets")
	}
	return &endpoint, nil
}

func (s *Service) findOrCreateWebhookIntegration(ctx context.Context, tx *sql.Tx, req CreateWebhookEndpointRequest, now time.Time) (uuid.UUID, error) {
	var integrationID uuid.UUID
	err := tx.QueryRowContext(ctx, `
		SELECT integration_id
		FROM git_integrations
		WHERE org_id = $1
		  AND provider = $2
		  AND provider_host = $3
		  AND mode = $4
		  AND active = true
	`, int64(req.OrgID), req.Provider, req.ProviderHost, GitIntegrationModeManualWebhook).Scan(&integrationID)
	if err == nil {
		return integrationID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("find webhook integration: %w", err)
	}
	integrationID = uuid.New()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO git_integrations (
			integration_id, org_id, provider, provider_host, mode, label,
			active, created_by, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			true, $7, $8, $8
		)
	`, integrationID, int64(req.OrgID), req.Provider, req.ProviderHost, GitIntegrationModeManualWebhook, req.ProviderHost, req.ActorID, now); err != nil {
		return uuid.Nil, fmt.Errorf("insert webhook integration: %w", err)
	}
	return integrationID, nil
}

func (s *Service) encryptWebhookSecret(secret string) (string, string, error) {
	if s == nil || s.WebhookSecretCodec == nil {
		return "", "", fmt.Errorf("webhook secret codec is not configured")
	}
	ciphertext, err := s.WebhookSecretCodec.Encrypt(secret)
	if err != nil {
		return "", "", err
	}
	return ciphertext, SecretFingerprint(secret), nil
}

func (s *Service) decryptWebhookSecret(ciphertext string) (string, error) {
	if s == nil || s.WebhookSecretCodec == nil {
		return "", fmt.Errorf("webhook secret codec is not configured")
	}
	return s.WebhookSecretCodec.Decrypt(ciphertext)
}

func normalizeCreateWebhookEndpointRequest(req CreateWebhookEndpointRequest) (CreateWebhookEndpointRequest, error) {
	req.ActorID = strings.TrimSpace(req.ActorID)
	req.Provider = strings.TrimSpace(strings.ToLower(req.Provider))
	req.ProviderHost = normalizeProviderHost(req.ProviderHost)
	req.Label = strings.TrimSpace(req.Label)
	if req.OrgID == 0 {
		return CreateWebhookEndpointRequest{}, fmt.Errorf("%w: org_id is required", ErrWebhookEndpointInvalid)
	}
	if req.ActorID == "" {
		return CreateWebhookEndpointRequest{}, fmt.Errorf("%w: actor_id is required", ErrWebhookEndpointInvalid)
	}
	if req.Provider == "" {
		req.Provider = WebhookProviderForgejo
	}
	if req.Provider != WebhookProviderForgejo {
		return CreateWebhookEndpointRequest{}, ErrWebhookProviderUnsupported
	}
	if req.ProviderHost == "" {
		return CreateWebhookEndpointRequest{}, fmt.Errorf("%w: provider_host is required", ErrWebhookEndpointInvalid)
	}
	if isLocalGitHostName(req.ProviderHost) {
		return CreateWebhookEndpointRequest{}, fmt.Errorf("%w: provider_host %q is not public", ErrWebhookEndpointInvalid, req.ProviderHost)
	}
	if addr, err := netip.ParseAddr(req.ProviderHost); err == nil {
		if err := validatePublicGitIP("provider_host", addr); err != nil {
			return CreateWebhookEndpointRequest{}, fmt.Errorf("%w: %v", ErrWebhookEndpointInvalid, err)
		}
	} else if !strings.Contains(req.ProviderHost, ".") {
		return CreateWebhookEndpointRequest{}, fmt.Errorf("%w: provider_host %q must be a public DNS name", ErrWebhookEndpointInvalid, req.ProviderHost)
	}
	return req, nil
}

func normalizeProviderHost(raw string) string {
	value := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	value = strings.Trim(value, "/")
	if strings.Contains(value, "/") {
		return ""
	}
	return value
}

func scanWebhookEndpointRow(scanner rowScanner) (*WebhookEndpointRecord, error) {
	var (
		record         WebhookEndpointRecord
		lastDeliveryAt sql.NullTime
	)
	if err := scanner.Scan(
		&record.EndpointID,
		&record.IntegrationID,
		&record.OrgID,
		&record.Provider,
		&record.ProviderHost,
		&record.Label,
		&record.Active,
		&record.CreatedBy,
		&record.CreatedAt,
		&record.UpdatedAt,
		&lastDeliveryAt,
		&record.DeliveryCount,
		&record.SecretFingerprint,
	); err != nil {
		return nil, err
	}
	if lastDeliveryAt.Valid {
		record.LastDeliveryAt = &lastDeliveryAt.Time
	}
	return &record, nil
}
