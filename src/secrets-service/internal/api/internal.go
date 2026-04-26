package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	workloadauth "github.com/verself/auth-middleware/workload"
	"github.com/verself/secrets-service/internal/secrets"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const injectionRequestMaxBytes = 128 << 10

// InternalRoutesConfig names peers by catalog service name (workloadauth.Service*).
// RegisterInternalRoutes resolves them against the caller's trust domain, so
// there is a single source of truth for which peers reach the internal plane.
type InternalRoutesConfig struct {
	PlatformOrgID              string
	SandboxService             string
	SourceService              string
	RuntimeSecretReadPolicies  []RuntimeSecretPolicy
	RuntimeSecretWritePolicies []RuntimeSecretPolicy
}

type RuntimeSecretPolicy struct {
	Service     string
	SecretNames []string
}

type runtimeSecretPolicy struct {
	credentialName string
	secretNames    map[string]struct{}
}

type injectionResolveRequest struct {
	OrgID       string                   `json:"org_id"`
	ActorID     string                   `json:"actor_id"`
	ExecutionID string                   `json:"execution_id"`
	AttemptID   string                   `json:"attempt_id"`
	Secrets     []injectionSecretRequest `json:"secrets"`
}

type injectionSecretRequest struct {
	EnvName    string `json:"env_name"`
	Kind       string `json:"kind,omitempty"`
	SecretName string `json:"secret_name"`
	ScopeLevel string `json:"scope_level,omitempty"`
	SourceID   string `json:"source_id,omitempty"`
	EnvID      string `json:"env_id,omitempty"`
	Branch     string `json:"branch,omitempty"`
	GrantID    string `json:"grant_id"`
}

type injectionResolveResponse struct {
	Env []injectionEnvValue `json:"env"`
}

type injectionEnvValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type internalCreateCredentialRequest struct {
	OrgID       string            `json:"org_id"`
	ActorID     string            `json:"actor_id"`
	Kind        string            `json:"kind"`
	DisplayName string            `json:"display_name,omitempty"`
	Scopes      []string          `json:"scopes"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	ExpiresAt   string            `json:"expires_at,omitempty"`
}

type internalCreateCredentialResponse struct {
	Credential opaqueCredentialWire `json:"credential"`
	Token      string               `json:"token"`
}

type internalVerifyCredentialRequest struct {
	OrgID          string   `json:"org_id"`
	ActorID        string   `json:"actor_id,omitempty"`
	Kind           string   `json:"kind"`
	Token          string   `json:"token"`
	RequiredScopes []string `json:"required_scopes,omitempty"`
}

type internalVerifyCredentialResponse struct {
	Active       bool                 `json:"active"`
	DenialReason string               `json:"denial_reason,omitempty"`
	Credential   opaqueCredentialWire `json:"credential,omitempty"`
}

type opaqueCredentialWire struct {
	CredentialID string            `json:"credential_id,omitempty"`
	OrgID        string            `json:"org_id,omitempty"`
	Kind         string            `json:"kind,omitempty"`
	Subject      string            `json:"subject,omitempty"`
	DisplayName  string            `json:"display_name,omitempty"`
	Status       string            `json:"status,omitempty"`
	TokenPrefix  string            `json:"token_prefix,omitempty"`
	Scopes       []string          `json:"scopes,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	ExpiresAt    string            `json:"expires_at,omitempty"`
	LastUsedAt   string            `json:"last_used_at,omitempty"`
	CreatedAt    string            `json:"created_at,omitempty"`
	UpdatedAt    string            `json:"updated_at,omitempty"`
	RevokedAt    string            `json:"revoked_at,omitempty"`
}

// RegisterInternalRoutes installs every internal handler on mux and returns
// the deduplicated peer allowlist that must be enforced at the mTLS
// handshake and per-request authorization layers. Returning the allowlist
// here guarantees the TLS layer and the authz layer derive their accepted
// identities from the same data, closing the parallel-list drift hazard that
// bit the original design.
func RegisterInternalRoutes(mux *http.ServeMux, svc *secrets.Service, source *workloadapi.X509Source, cfg InternalRoutesConfig) ([]spiffeid.ID, error) {
	if mux == nil {
		return nil, fmt.Errorf("internal routes mux is required")
	}
	if source == nil {
		return nil, fmt.Errorf("spiffe x509 source is required")
	}
	cfg.PlatformOrgID = strings.TrimSpace(cfg.PlatformOrgID)
	if cfg.PlatformOrgID == "" {
		return nil, fmt.Errorf("platform org id is required")
	}
	cfg.SandboxService = strings.TrimSpace(cfg.SandboxService)
	if cfg.SandboxService == "" {
		return nil, fmt.Errorf("sandbox service is required")
	}
	sandboxPeerID, err := workloadauth.PeerIDForSource(source, cfg.SandboxService)
	if err != nil {
		return nil, fmt.Errorf("resolve sandbox peer id: %w", err)
	}
	cfg.SourceService = strings.TrimSpace(cfg.SourceService)
	if cfg.SourceService == "" {
		return nil, fmt.Errorf("source service is required")
	}
	sourcePeerID, err := workloadauth.PeerIDForSource(source, cfg.SourceService)
	if err != nil {
		return nil, fmt.Errorf("resolve source peer id: %w", err)
	}
	resolvePolicies, err := normalizeRuntimeSecretPolicies(source, cfg.RuntimeSecretReadPolicies)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime read policies: %w", err)
	}
	writePolicies, err := normalizeRuntimeSecretPolicies(source, cfg.RuntimeSecretWritePolicies)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime write policies: %w", err)
	}
	mux.HandleFunc("/internal/v1/injections/resolve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		peerID, ok := workloadauth.PeerIDFromContext(r.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if peerID != sandboxPeerID {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		defer r.Body.Close()
		var request injectionResolveRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, injectionRequestMaxBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			http.Error(w, "invalid injection request", http.StatusBadRequest)
			return
		}
		response, err := resolveInjection(r.Context(), svc, request)
		if err != nil {
			switch {
			case errors.Is(err, secrets.ErrInvalidArgument):
				http.Error(w, "invalid injection request", http.StatusBadRequest)
			case errors.Is(err, secrets.ErrForbidden):
				http.Error(w, "forbidden", http.StatusForbidden)
			case errors.Is(err, secrets.ErrNotFound):
				http.Error(w, "secret not found", http.StatusNotFound)
			default:
				http.Error(w, "resolve injection failed", http.StatusInternalServerError)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(response)
	})
	mux.HandleFunc("/internal/v1/credentials", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		peerID, ok := workloadauth.PeerIDFromContext(r.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if peerID != sourcePeerID {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		defer r.Body.Close()
		var request internalCreateCredentialRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, injectionRequestMaxBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			http.Error(w, "invalid credential request", http.StatusBadRequest)
			return
		}
		response, err := createInternalCredential(r.Context(), svc, request)
		if err != nil {
			writeInternalCredentialError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(response)
	})
	mux.HandleFunc("/internal/v1/credentials:verify", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		peerID, ok := workloadauth.PeerIDFromContext(r.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if peerID != sourcePeerID {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		defer r.Body.Close()
		var request internalVerifyCredentialRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, injectionRequestMaxBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			http.Error(w, "invalid credential verification request", http.StatusBadRequest)
			return
		}
		response, err := verifyInternalCredential(r.Context(), svc, request)
		if err != nil {
			writeInternalCredentialError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(response)
	})
	mux.HandleFunc("/api/v1/secrets/", func(w http.ResponseWriter, r *http.Request) {
		secretName, err := runtimeSecretNameFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodGet:
			if _, ok := workloadauth.PeerIDFromContext(r.Context()); !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if err := validateRuntimeSecretReadQuery(r); err != nil {
				http.Error(w, "invalid runtime secret request", http.StatusBadRequest)
				return
			}
			value, err := readRuntimeSecret(r.Context(), svc, cfg.PlatformOrgID, resolvePolicies, secretName)
			if err != nil {
				switch {
				case errors.Is(err, secrets.ErrInvalidArgument):
					http.Error(w, "invalid runtime secret request", http.StatusBadRequest)
				case errors.Is(err, secrets.ErrForbidden):
					http.Error(w, "forbidden", http.StatusForbidden)
				case errors.Is(err, secrets.ErrNotFound):
					http.Error(w, "runtime secret not found", http.StatusNotFound)
				default:
					http.Error(w, "resolve runtime secret failed", http.StatusInternalServerError)
				}
				return
			}
			dto := secretDTO(value.Record)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(SecretValueDTO{SecretDTO: dto, Value: value.Value})
		case http.MethodPut:
			if _, ok := workloadauth.PeerIDFromContext(r.Context()); !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			defer r.Body.Close()
			var body putSecretBody
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, bodyLimitSmallJSON))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&body); err != nil {
				http.Error(w, "invalid runtime secret upsert request", http.StatusBadRequest)
				return
			}
			if err := validateRuntimeSecretWriteRequest(r, body); err != nil {
				http.Error(w, "invalid runtime secret upsert request", http.StatusBadRequest)
				return
			}
			record, err := writeRuntimeSecret(r.Context(), svc, cfg.PlatformOrgID, writePolicies, secretName, body.Value)
			if err != nil {
				switch {
				case errors.Is(err, secrets.ErrInvalidArgument):
					http.Error(w, "invalid runtime secret upsert request", http.StatusBadRequest)
				case errors.Is(err, secrets.ErrForbidden):
					http.Error(w, "forbidden", http.StatusForbidden)
				default:
					http.Error(w, "upsert runtime secret failed", http.StatusInternalServerError)
				}
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(secretDTO(record))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	allowlist := make([]spiffeid.ID, 0, 2+len(resolvePolicies)+len(writePolicies))
	seen := map[spiffeid.ID]struct{}{sandboxPeerID: {}, sourcePeerID: {}}
	allowlist = append(allowlist, sandboxPeerID, sourcePeerID)
	for peerID := range resolvePolicies {
		if _, ok := seen[peerID]; ok {
			continue
		}
		seen[peerID] = struct{}{}
		allowlist = append(allowlist, peerID)
	}
	for peerID := range writePolicies {
		if _, ok := seen[peerID]; ok {
			continue
		}
		seen[peerID] = struct{}{}
		allowlist = append(allowlist, peerID)
	}
	return allowlist, nil
}

func runtimeSecretNameFromPath(path string) (string, error) {
	const prefix = "/api/v1/secrets/"
	if !strings.HasPrefix(path, prefix) {
		return "", fmt.Errorf("%w: runtime secret path is invalid", secrets.ErrInvalidArgument)
	}
	name := strings.TrimSpace(strings.TrimPrefix(path, prefix))
	if name == "" || strings.Contains(name, "/") {
		return "", fmt.Errorf("%w: runtime secret name is invalid", secrets.ErrInvalidArgument)
	}
	return name, nil
}

func validateRuntimeSecretReadQuery(r *http.Request) error {
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	scopeLevel := strings.TrimSpace(r.URL.Query().Get("scope_level"))
	sourceID := strings.TrimSpace(r.URL.Query().Get("source_id"))
	envID := strings.TrimSpace(r.URL.Query().Get("env_id"))
	branch := strings.TrimSpace(r.URL.Query().Get("branch"))
	if kind != "" && kind != secrets.KindSecret {
		return fmt.Errorf("%w: runtime secrets only support kind=secret", secrets.ErrInvalidArgument)
	}
	if scopeLevel != "" && scopeLevel != secrets.ScopeOrg {
		return fmt.Errorf("%w: runtime secrets only support scope_level=org", secrets.ErrInvalidArgument)
	}
	if sourceID != "" || envID != "" || branch != "" {
		return fmt.Errorf("%w: runtime secrets only support org-scoped reads", secrets.ErrInvalidArgument)
	}
	return nil
}

func validateRuntimeSecretWriteRequest(r *http.Request, body putSecretBody) error {
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		return fmt.Errorf("%w: idempotency key is required", secrets.ErrInvalidArgument)
	}
	if body.ScopeLevel != "" && body.ScopeLevel != secrets.ScopeOrg {
		return fmt.Errorf("%w: runtime secrets only support scope_level=org", secrets.ErrInvalidArgument)
	}
	if strings.TrimSpace(body.SourceID) != "" || strings.TrimSpace(body.EnvID) != "" || strings.TrimSpace(body.Branch) != "" {
		return fmt.Errorf("%w: runtime secrets only support org-scoped writes", secrets.ErrInvalidArgument)
	}
	return nil
}

func readRuntimeSecret(ctx context.Context, svc *secrets.Service, platformOrgID string, policies map[spiffeid.ID]runtimeSecretPolicy, secretName string) (secrets.SecretValue, error) {
	ctx, span := apiTracer.Start(ctx, "secrets.platform.resolve")
	defer span.End()
	ctx = secrets.ContextWithOpenBaoAuditInfo(ctx)

	peerID, ok := workloadauth.PeerIDFromContext(ctx)
	if !ok {
		err := fmt.Errorf("%w: missing SPIFFE peer identity", secrets.ErrForbidden)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return secrets.SecretValue{}, err
	}
	policy, ok := policies[peerID]
	if !ok {
		err := fmt.Errorf("%w: SPIFFE peer is not allowed to resolve platform runtime secrets", secrets.ErrForbidden)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return secrets.SecretValue{}, err
	}
	secretName = strings.TrimSpace(secretName)
	if secretName == "" {
		err := fmt.Errorf("%w: runtime secret name is required", secrets.ErrInvalidArgument)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return secrets.SecretValue{}, err
	}
	if _, allowed := policy.secretNames[secretName]; !allowed {
		err := fmt.Errorf("%w: platform runtime secret %q is not allowed for %s", secrets.ErrForbidden, secretName, policy.credentialName)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return secrets.SecretValue{}, err
	}
	span.SetAttributes(
		attribute.String("verself.org_id", platformOrgID),
		attribute.String("spiffe.peer_id", peerID.String()),
		attribute.String("verself.runtime_secret_consumer", policy.credentialName),
		attribute.Int("verself.secret_count", 1),
	)
	principal := secrets.Principal{
		OrgID:           platformOrgID,
		Subject:         peerID.String(),
		Type:            "service_workload",
		AuthMethod:      "spiffe_mtls",
		CredentialID:    peerID.String(),
		CredentialName:  policy.credentialName,
		UseWorkloadSVID: true,
	}
	value, err := svc.ReadSecret(ctx, principal, secrets.KindSecret, secretName, secrets.Scope{Level: secrets.ScopeOrg})
	auditRuntimeSecret(ctx, platformOrgID, peerID, policy.credentialName, secretName, value, err)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return secrets.SecretValue{}, err
	}
	return value, nil
}

func writeRuntimeSecret(ctx context.Context, svc *secrets.Service, platformOrgID string, policies map[spiffeid.ID]runtimeSecretPolicy, secretName string, value string) (secrets.SecretRecord, error) {
	ctx, span := apiTracer.Start(ctx, "secrets.platform.upsert")
	defer span.End()
	ctx = secrets.ContextWithOpenBaoAuditInfo(ctx)

	peerID, ok := workloadauth.PeerIDFromContext(ctx)
	if !ok {
		err := fmt.Errorf("%w: missing SPIFFE peer identity", secrets.ErrForbidden)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return secrets.SecretRecord{}, err
	}
	policy, ok := policies[peerID]
	if !ok {
		err := fmt.Errorf("%w: SPIFFE peer is not allowed to upsert platform runtime secrets", secrets.ErrForbidden)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return secrets.SecretRecord{}, err
	}
	secretName = strings.TrimSpace(secretName)
	if secretName == "" {
		err := fmt.Errorf("%w: runtime secret name is required", secrets.ErrInvalidArgument)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return secrets.SecretRecord{}, err
	}
	if _, allowed := policy.secretNames[secretName]; !allowed {
		err := fmt.Errorf("%w: platform runtime secret %q is not allowed for %s", secrets.ErrForbidden, secretName, policy.credentialName)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return secrets.SecretRecord{}, err
	}
	span.SetAttributes(
		attribute.String("verself.org_id", platformOrgID),
		attribute.String("spiffe.peer_id", peerID.String()),
		attribute.String("verself.runtime_secret_consumer", policy.credentialName),
		attribute.Int("verself.secret_count", 1),
	)
	principal := secrets.Principal{
		OrgID:           platformOrgID,
		Subject:         peerID.String(),
		Type:            "service_workload",
		AuthMethod:      "spiffe_mtls",
		CredentialID:    peerID.String(),
		CredentialName:  policy.credentialName,
		OpenBaoRole:     runtimeSecretWriteOpenBaoRole(platformOrgID),
		UseWorkloadSVID: true,
	}
	record, err := svc.PutSecret(ctx, principal, secrets.PutSecretRequest{
		Kind:  secrets.KindSecret,
		Name:  secretName,
		Scope: secrets.Scope{Level: secrets.ScopeOrg},
		Value: value,
	})
	auditRuntimeSecretWrite(ctx, platformOrgID, peerID, policy.credentialName, secretName, record, err)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return secrets.SecretRecord{}, err
	}
	return record, nil
}

func createInternalCredential(ctx context.Context, svc *secrets.Service, request internalCreateCredentialRequest) (internalCreateCredentialResponse, error) {
	ctx, span := apiTracer.Start(ctx, "secrets.credential.internal_create")
	defer span.End()
	ctx = secrets.ContextWithOpenBaoAuditInfo(ctx)
	request.OrgID = strings.TrimSpace(request.OrgID)
	request.ActorID = strings.TrimSpace(request.ActorID)
	request.Kind = strings.TrimSpace(request.Kind)
	request.DisplayName = strings.TrimSpace(request.DisplayName)
	expiresAt := time.Time{}
	if strings.TrimSpace(request.ExpiresAt) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(request.ExpiresAt))
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, strings.TrimSpace(request.ExpiresAt))
		}
		if err != nil {
			return internalCreateCredentialResponse{}, fmt.Errorf("%w: expires_at must be RFC3339", secrets.ErrInvalidArgument)
		}
		expiresAt = parsed.UTC()
	}
	peerID, _ := workloadauth.PeerIDFromContext(ctx)
	span.SetAttributes(
		attribute.String("verself.org_id", request.OrgID),
		attribute.String("verself.subject_id", request.ActorID),
		attribute.String("verself.credential_kind", request.Kind),
		attribute.String("spiffe.peer_id", peerID.String()),
	)
	if request.OrgID == "" || request.ActorID == "" || request.Kind == "" {
		return internalCreateCredentialResponse{}, fmt.Errorf("%w: org_id, actor_id, and kind are required", secrets.ErrInvalidArgument)
	}
	principal := secrets.Principal{
		OrgID:           request.OrgID,
		Subject:         request.ActorID,
		Type:            "delegated_user",
		AuthMethod:      "spiffe_mtls",
		CredentialID:    peerID.String(),
		CredentialName:  workloadauth.ServiceSourceCodeHosting,
		OpenBaoRole:     credentialManageOpenBaoRole(request.OrgID),
		UseWorkloadSVID: true,
	}
	material, err := svc.CreateOpaqueCredential(ctx, principal, secrets.CreateOpaqueCredentialRequest{
		Kind:        request.Kind,
		Subject:     request.ActorID,
		DisplayName: request.DisplayName,
		Scopes:      request.Scopes,
		Metadata:    request.Metadata,
		ExpiresAt:   expiresAt,
	})
	auditInternalCredential(ctx, "create-internal-opaque-credential", "secrets.credential.create", "create opaque credential", "write", "critical", "create", request.OrgID, request.ActorID, request.Kind, material.Credential, err)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return internalCreateCredentialResponse{}, err
	}
	return internalCreateCredentialResponse{
		Credential: opaqueCredentialWireDTO(material.Credential),
		Token:      material.Token,
	}, nil
}

func verifyInternalCredential(ctx context.Context, svc *secrets.Service, request internalVerifyCredentialRequest) (internalVerifyCredentialResponse, error) {
	ctx, span := apiTracer.Start(ctx, "secrets.credential.internal_verify")
	defer span.End()
	ctx = secrets.ContextWithOpenBaoAuditInfo(ctx)
	request.OrgID = strings.TrimSpace(request.OrgID)
	request.ActorID = strings.TrimSpace(request.ActorID)
	request.Kind = strings.TrimSpace(request.Kind)
	peerID, _ := workloadauth.PeerIDFromContext(ctx)
	span.SetAttributes(
		attribute.String("verself.org_id", request.OrgID),
		attribute.String("verself.subject_id", request.ActorID),
		attribute.String("verself.credential_kind", request.Kind),
		attribute.String("spiffe.peer_id", peerID.String()),
		attribute.Int("verself.required_scope_count", len(request.RequiredScopes)),
	)
	if request.OrgID == "" || request.Kind == "" || strings.TrimSpace(request.Token) == "" {
		return internalVerifyCredentialResponse{}, fmt.Errorf("%w: org_id, kind, and token are required", secrets.ErrInvalidArgument)
	}
	subject := request.ActorID
	if subject == "" {
		subject = peerID.String()
	}
	principal := secrets.Principal{
		OrgID:           request.OrgID,
		Subject:         subject,
		Type:            "service_workload",
		AuthMethod:      "spiffe_mtls",
		CredentialID:    peerID.String(),
		CredentialName:  workloadauth.ServiceSourceCodeHosting,
		OpenBaoRole:     credentialVerifyOpenBaoRole(request.OrgID),
		UseWorkloadSVID: true,
	}
	result, err := svc.VerifyOpaqueCredential(ctx, principal, secrets.VerifyOpaqueCredentialRequest{
		Kind:           request.Kind,
		Token:          request.Token,
		RequiredScopes: request.RequiredScopes,
	})
	auditInternalCredential(ctx, "verify-internal-opaque-credential", "secrets.credential.verify", "verify opaque credential", "authz", "high", "verify", request.OrgID, subject, request.Kind, result.Credential, err)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return internalVerifyCredentialResponse{}, err
	}
	span.SetAttributes(attribute.Bool("verself.credential_active", result.Active))
	return internalVerifyCredentialResponse{
		Active:       result.Active,
		DenialReason: result.DenialReason,
		Credential:   opaqueCredentialWireDTO(result.Credential),
	}, nil
}

func writeInternalCredentialError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, secrets.ErrInvalidArgument):
		http.Error(w, "invalid credential request", http.StatusBadRequest)
	case errors.Is(err, secrets.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, secrets.ErrNotFound):
		http.Error(w, "credential not found", http.StatusNotFound)
	case errors.Is(err, secrets.ErrConflict):
		http.Error(w, "credential conflict", http.StatusConflict)
	default:
		http.Error(w, "credential operation failed", http.StatusInternalServerError)
	}
}

func credentialManageOpenBaoRole(orgID string) string {
	return "secrets-credential-manage-" + strings.TrimSpace(orgID)
}

func credentialVerifyOpenBaoRole(orgID string) string {
	return "secrets-credential-verify-" + strings.TrimSpace(orgID)
}

func opaqueCredentialWireDTO(credential secrets.OpaqueCredential) opaqueCredentialWire {
	return opaqueCredentialWire{
		CredentialID: credential.CredentialID,
		OrgID:        credential.OrgID,
		Kind:         credential.Kind,
		Subject:      credential.Subject,
		DisplayName:  credential.DisplayName,
		Status:       credential.Status,
		TokenPrefix:  credential.TokenPrefix,
		Scopes:       append([]string(nil), credential.Scopes...),
		Metadata:     copyWireMap(credential.Metadata),
		ExpiresAt:    formatWireTime(credential.ExpiresAt),
		LastUsedAt:   formatWireTimePtr(credential.LastUsedAt),
		CreatedAt:    formatWireTime(credential.CreatedAt),
		UpdatedAt:    formatWireTime(credential.UpdatedAt),
		RevokedAt:    formatWireTimePtr(credential.RevokedAt),
	}
}

func auditInternalCredential(ctx context.Context, operationID, auditEvent, display, operationType, risk, action, orgID, actorID, kind string, credential secrets.OpaqueCredential, err error) {
	baoInfo, _ := secrets.OpenBaoAuditInfoFromContext(ctx)
	secretMount := "openbao"
	openBaoRequestID := ""
	openBaoAccessorHash := ""
	if baoInfo != nil {
		secretMount = firstNonEmpty(baoInfo.Mount, secretMount)
		openBaoRequestID = baoInfo.RequestID
		openBaoAccessorHash = baoInfo.AccessorHash
	}
	targetID := credential.CredentialID
	if targetID == "" {
		targetID = kind
	}
	record := governanceAuditRecord{
		OrgID:               orgID,
		SourceProductArea:   "Secrets",
		ServiceName:         "secrets-service",
		OperationID:         operationID,
		AuditEvent:          auditEvent,
		OperationDisplay:    display,
		OperationType:       operationType,
		EventCategory:       "secrets",
		RiskLevel:           risk,
		DataClassification:  "credential_metadata",
		ActorType:           "service_delegated",
		ActorID:             actorID,
		ActorSPIFFEID:       spiffePeerID(ctx),
		CredentialID:        spiffePeerID(ctx),
		CredentialName:      workloadauth.ServiceSourceCodeHosting,
		AuthMethod:          "spiffe_mtls",
		Permission:          "secrets:credential:" + action,
		TargetKind:          "opaque_credential",
		TargetID:            targetID,
		TargetDisplay:       targetID,
		TargetScope:         "org",
		Action:              action,
		OrgScope:            "delegated_request_org_id",
		RateLimitClass:      "internal",
		Decision:            "allow",
		Result:              "allowed",
		TrustClass:          "service_internal",
		SecretMount:         secretMount,
		SecretOperation:     "credential_" + action,
		OpenBaoRequestID:    openBaoRequestID,
		OpenBaoAccessorHash: openBaoAccessorHash,
	}
	if err != nil {
		record.Decision = "deny"
		record.Result = "error"
		record.ErrorCode = "credential-operation-failed"
		record.ErrorClass = "application"
		record.ErrorMessage = err.Error()
	}
	sendGovernanceAudit(ctx, record)
}

func copyWireMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func formatWireTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func formatWireTimePtr(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatWireTime(*value)
}

func resolveInjection(ctx context.Context, svc *secrets.Service, request injectionResolveRequest) (injectionResolveResponse, error) {
	ctx, span := apiTracer.Start(ctx, "secrets.injection.resolve")
	defer span.End()
	ctx = secrets.ContextWithOpenBaoAuditInfo(ctx)
	request.OrgID = strings.TrimSpace(request.OrgID)
	request.ActorID = strings.TrimSpace(request.ActorID)
	request.ExecutionID = strings.TrimSpace(request.ExecutionID)
	request.AttemptID = strings.TrimSpace(request.AttemptID)
	peerID, _ := workloadauth.PeerIDFromContext(ctx)
	span.SetAttributes(
		attribute.String("verself.org_id", request.OrgID),
		attribute.String("verself.execution_id", request.ExecutionID),
		attribute.String("verself.attempt_id", request.AttemptID),
		attribute.String("spiffe.peer_id", peerID.String()),
		attribute.Int("verself.secret_env_count", len(request.Secrets)),
	)
	if request.OrgID == "" || request.ActorID == "" || request.ExecutionID == "" || request.AttemptID == "" {
		err := fmt.Errorf("%w: org_id, actor_id, execution_id, and attempt_id are required", secrets.ErrInvalidArgument)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return injectionResolveResponse{}, err
	}
	if len(request.Secrets) == 0 {
		return injectionResolveResponse{Env: []injectionEnvValue{}}, nil
	}
	if len(request.Secrets) > 64 {
		err := fmt.Errorf("%w: at most 64 secret injections are allowed", secrets.ErrInvalidArgument)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return injectionResolveResponse{}, err
	}
	if err := verifyInjectionRequest(ctx, request); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return injectionResolveResponse{}, err
	}
	principal := secrets.Principal{
		OrgID:           request.OrgID,
		Subject:         request.ActorID,
		Type:            "sandbox_execution",
		AuthMethod:      "spiffe_mtls",
		CredentialID:    peerID.String(),
		UseWorkloadSVID: true,
	}
	response := injectionResolveResponse{Env: make([]injectionEnvValue, 0, len(request.Secrets))}
	names := map[string]struct{}{}
	for _, requested := range request.Secrets {
		requested.EnvName = strings.TrimSpace(requested.EnvName)
		requested.SecretName = strings.TrimSpace(requested.SecretName)
		if requested.EnvName == "" || requested.SecretName == "" {
			return injectionResolveResponse{}, fmt.Errorf("%w: env_name and secret_name are required", secrets.ErrInvalidArgument)
		}
		if _, exists := names[requested.EnvName]; exists {
			return injectionResolveResponse{}, fmt.Errorf("%w: duplicate env_name %q", secrets.ErrInvalidArgument, requested.EnvName)
		}
		names[requested.EnvName] = struct{}{}
		scope := secrets.Scope{
			Level:    requested.ScopeLevel,
			SourceID: requested.SourceID,
			EnvID:    requested.EnvID,
			Branch:   requested.Branch,
		}
		value, err := svc.ReadSecret(ctx, principal, requested.Kind, requested.SecretName, scope)
		auditInjection(ctx, request, requested, value, err)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return injectionResolveResponse{}, err
		}
		response.Env = append(response.Env, injectionEnvValue{Name: requested.EnvName, Value: value.Value})
	}
	return response, nil
}

func verifyInjectionRequest(ctx context.Context, request injectionResolveRequest) error {
	_, span := apiTracer.Start(ctx, "secrets.injection.service_token.exchange")
	defer span.End()
	peerID, _ := workloadauth.PeerIDFromContext(ctx)
	span.SetAttributes(
		attribute.String("verself.org_id", request.OrgID),
		attribute.String("verself.execution_id", request.ExecutionID),
		attribute.String("verself.attempt_id", request.AttemptID),
		attribute.String("spiffe.peer_id", peerID.String()),
		attribute.String("bao.namespace", request.OrgID),
		attribute.String("verself.cache_outcome", "delegated"),
		attribute.Int("verself.secret_env_count", len(request.Secrets)),
	)
	seen := map[string]struct{}{}
	for _, requested := range request.Secrets {
		grantID := strings.TrimSpace(requested.GrantID)
		if grantID == "" {
			err := fmt.Errorf("%w: injection grant_id is required", secrets.ErrForbidden)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		if _, err := uuid.Parse(grantID); err != nil {
			err := fmt.Errorf("%w: injection grant_id is malformed", secrets.ErrForbidden)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		if _, exists := seen[grantID]; exists {
			err := fmt.Errorf("%w: duplicate injection grant_id", secrets.ErrForbidden)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		seen[grantID] = struct{}{}
	}
	return nil
}

func auditInjection(ctx context.Context, request injectionResolveRequest, requested injectionSecretRequest, value secrets.SecretValue, err error) {
	kind := requested.Kind
	if strings.TrimSpace(kind) == "" {
		kind = secrets.KindSecret
	}
	scope := secrets.Scope{
		Level:    requested.ScopeLevel,
		SourceID: requested.SourceID,
		EnvID:    requested.EnvID,
		Branch:   requested.Branch,
	}
	version := uint64(0)
	if err == nil {
		version = value.Record.CurrentVersion
		if value.Record.Scope.Level != "" {
			scope = value.Record.Scope
		}
	}
	baoInfo, _ := secrets.OpenBaoAuditInfoFromContext(ctx)
	secretMount := "openbao"
	openBaoRequestID := ""
	openBaoAccessorHash := ""
	if baoInfo != nil {
		secretMount = firstNonEmpty(baoInfo.Mount, secretMount)
		openBaoRequestID = baoInfo.RequestID
		openBaoAccessorHash = baoInfo.AccessorHash
	}
	record := governanceAuditRecord{
		OrgID:               request.OrgID,
		SourceProductArea:   "Secrets",
		ServiceName:         "secrets-service",
		OperationID:         "resolve-sandbox-secret-injection",
		AuditEvent:          "secrets.secret.inject",
		OperationDisplay:    "inject secret into sandbox execution",
		OperationType:       "read",
		EventCategory:       "data_access",
		RiskLevel:           "critical",
		DataClassification:  "secret",
		ActorType:           "sandbox_execution",
		ActorID:             request.ActorID,
		ActorSPIFFEID:       spiffePeerID(ctx),
		CredentialID:        spiffePeerID(ctx),
		CredentialName:      "sandbox-rental-service",
		AuthMethod:          "spiffe_mtls",
		Permission:          "secrets:secret:read",
		TargetKind:          "secret",
		TargetScope:         scope.Level,
		TargetPathHash:      secrets.SecretPathHash(request.OrgID, kind, requested.SecretName, scope),
		Action:              "inject",
		OrgScope:            "sandbox_execution_org_id",
		RateLimitClass:      "internal",
		Decision:            "allow",
		Result:              "allowed",
		TrustClass:          "service_internal",
		SecretMount:         secretMount,
		SecretPathHash:      secrets.SecretPathHash(request.OrgID, kind, requested.SecretName, scope),
		SecretVersion:       version,
		SecretOperation:     "inject",
		OpenBaoRequestID:    openBaoRequestID,
		OpenBaoAccessorHash: openBaoAccessorHash,
		RequestID:           request.AttemptID,
		ContentSHA256:       hashTextForAudit(request.ExecutionID + "\x00" + request.AttemptID + "\x00" + requested.EnvName),
	}
	if err != nil {
		record.Decision = "deny"
		record.Result = "error"
		record.ErrorCode = "secret-injection-failed"
		record.ErrorClass = "application"
		record.ErrorMessage = err.Error()
	}
	sendGovernanceAudit(ctx, record)
}

func runtimeSecretWriteOpenBaoRole(orgID string) string {
	return "secrets-runtime-write-" + strings.TrimSpace(orgID)
}

func normalizeRuntimeSecretPolicies(source *workloadapi.X509Source, items []RuntimeSecretPolicy) (map[spiffeid.ID]runtimeSecretPolicy, error) {
	out := make(map[spiffeid.ID]runtimeSecretPolicy, len(items))
	for _, item := range items {
		service := strings.TrimSpace(item.Service)
		if service == "" {
			return nil, fmt.Errorf("runtime secret policy service is required")
		}
		peerID, err := workloadauth.PeerIDForSource(source, service)
		if err != nil {
			return nil, fmt.Errorf("resolve runtime secret peer %q: %w", service, err)
		}
		if _, exists := out[peerID]; exists {
			return nil, fmt.Errorf("duplicate runtime secret policy for %s", peerID.String())
		}
		secretNames, err := normalizeRuntimeSecretNames(item.SecretNames)
		if err != nil {
			return nil, err
		}
		allowed := make(map[string]struct{}, len(secretNames))
		for _, secretName := range secretNames {
			allowed[secretName] = struct{}{}
		}
		out[peerID] = runtimeSecretPolicy{
			credentialName: service,
			secretNames:    allowed,
		}
	}
	return out, nil
}

func normalizeRuntimeSecretNames(secretNames []string) ([]string, error) {
	if len(secretNames) == 0 {
		return nil, fmt.Errorf("%w: at least one runtime secret is required", secrets.ErrInvalidArgument)
	}
	if len(secretNames) > 32 {
		return nil, fmt.Errorf("%w: at most 32 runtime secrets are allowed", secrets.ErrInvalidArgument)
	}
	out := make([]string, 0, len(secretNames))
	seen := make(map[string]struct{}, len(secretNames))
	for _, name := range secretNames {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("%w: runtime secret name is required", secrets.ErrInvalidArgument)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("%w: duplicate runtime secret %q", secrets.ErrInvalidArgument, name)
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out, nil
}

func auditRuntimeSecret(ctx context.Context, platformOrgID string, peerID spiffeid.ID, credentialName string, secretName string, value secrets.SecretValue, err error) {
	baoInfo, _ := secrets.OpenBaoAuditInfoFromContext(ctx)
	secretMount := "openbao"
	openBaoRequestID := ""
	openBaoAccessorHash := ""
	if baoInfo != nil {
		secretMount = firstNonEmpty(baoInfo.Mount, secretMount)
		openBaoRequestID = baoInfo.RequestID
		openBaoAccessorHash = baoInfo.AccessorHash
	}
	record := governanceAuditRecord{
		OrgID:               platformOrgID,
		SourceProductArea:   "Secrets",
		ServiceName:         "secrets-service",
		OperationID:         "resolve-platform-runtime-secret",
		AuditEvent:          "secrets.secret.read",
		OperationDisplay:    "resolve platform runtime secret",
		OperationType:       "read",
		EventCategory:       "data_access",
		RiskLevel:           "high",
		DataClassification:  "secret",
		ActorType:           "service_workload",
		ActorID:             credentialName,
		ActorDisplay:        credentialName,
		ActorSPIFFEID:       peerID.String(),
		CredentialID:        peerID.String(),
		CredentialName:      credentialName,
		AuthMethod:          "spiffe_mtls",
		Permission:          "secrets:secret:read",
		TargetKind:          "secret",
		TargetPathHash:      secrets.SecretPathHash(platformOrgID, secrets.KindSecret, secretName, secrets.Scope{Level: secrets.ScopeOrg}),
		TargetScope:         secrets.ScopeOrg,
		Action:              "read",
		OrgScope:            "platform_org_id",
		RateLimitClass:      "internal",
		Decision:            "allow",
		Result:              "allowed",
		TrustClass:          "service_internal",
		ContentSHA256:       hashTextForAudit(secretName),
		SecretMount:         secretMount,
		SecretPathHash:      secrets.SecretPathHash(platformOrgID, secrets.KindSecret, secretName, secrets.Scope{Level: secrets.ScopeOrg}),
		SecretOperation:     "read",
		OpenBaoRequestID:    openBaoRequestID,
		OpenBaoAccessorHash: openBaoAccessorHash,
	}
	if err == nil {
		record.SecretVersion = value.Record.CurrentVersion
		record.TargetID = value.Record.SecretID
		record.TargetDisplay = value.Record.Name
	}
	if err != nil {
		record.Decision = "deny"
		record.Result = "error"
		record.ErrorCode = "platform-runtime-secret-read-failed"
		record.ErrorClass = "application"
		record.ErrorMessage = err.Error()
	}
	sendGovernanceAudit(ctx, record)
}

func auditRuntimeSecretWrite(ctx context.Context, platformOrgID string, peerID spiffeid.ID, credentialName string, secretName string, record secrets.SecretRecord, err error) {
	baoInfo, _ := secrets.OpenBaoAuditInfoFromContext(ctx)
	secretMount := "openbao"
	openBaoRequestID := ""
	openBaoAccessorHash := ""
	if baoInfo != nil {
		secretMount = firstNonEmpty(baoInfo.Mount, secretMount)
		openBaoRequestID = baoInfo.RequestID
		openBaoAccessorHash = baoInfo.AccessorHash
	}
	auditRecord := governanceAuditRecord{
		OrgID:               platformOrgID,
		SourceProductArea:   "Secrets",
		ServiceName:         "secrets-service",
		OperationID:         "upsert-platform-runtime-secret",
		AuditEvent:          "secrets.secret.write",
		OperationDisplay:    "upsert platform runtime secret",
		OperationType:       "write",
		EventCategory:       "configuration",
		RiskLevel:           "critical",
		DataClassification:  "secret",
		ActorType:           "service_workload",
		ActorID:             credentialName,
		ActorDisplay:        credentialName,
		ActorSPIFFEID:       peerID.String(),
		CredentialID:        peerID.String(),
		CredentialName:      credentialName,
		AuthMethod:          "spiffe_mtls",
		Permission:          "secrets:secret:write",
		TargetKind:          "secret",
		TargetPathHash:      secrets.SecretPathHash(platformOrgID, secrets.KindSecret, secretName, secrets.Scope{Level: secrets.ScopeOrg}),
		TargetScope:         secrets.ScopeOrg,
		Action:              "write",
		OrgScope:            "platform_org_id",
		RateLimitClass:      "internal",
		Decision:            "allow",
		Result:              "allowed",
		TrustClass:          "service_internal",
		ContentSHA256:       hashTextForAudit(secretName),
		SecretMount:         secretMount,
		SecretPathHash:      secrets.SecretPathHash(platformOrgID, secrets.KindSecret, secretName, secrets.Scope{Level: secrets.ScopeOrg}),
		SecretOperation:     "write",
		OpenBaoRequestID:    openBaoRequestID,
		OpenBaoAccessorHash: openBaoAccessorHash,
	}
	if err == nil {
		auditRecord.SecretVersion = record.CurrentVersion
		auditRecord.TargetID = record.SecretID
		auditRecord.TargetDisplay = record.Name
	}
	if err != nil {
		auditRecord.Decision = "deny"
		auditRecord.Result = "error"
		auditRecord.ErrorCode = "platform-runtime-secret-write-failed"
		auditRecord.ErrorClass = "application"
		auditRecord.ErrorMessage = err.Error()
	}
	sendGovernanceAudit(ctx, auditRecord)
}

func spiffePeerID(ctx context.Context) string {
	id, ok := workloadauth.PeerIDFromContext(ctx)
	if !ok {
		return ""
	}
	return id.String()
}
