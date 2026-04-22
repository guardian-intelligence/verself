package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	workloadauth "github.com/forge-metal/auth-middleware/workload"
	"github.com/forge-metal/secrets-service/internal/secrets"
	"github.com/google/uuid"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const injectionRequestMaxBytes = 128 << 10

type InternalRoutesConfig struct {
	SandboxPeerID              spiffeid.ID
	PlatformOrgID              string
	RuntimeSecretPolicies      []RuntimeSecretPolicy
	RuntimeSecretWritePolicies []RuntimeSecretPolicy
}

type RuntimeSecretPolicy struct {
	PeerID         spiffeid.ID
	CredentialName string
	SecretNames    []string
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

type runtimeSecretResolveRequest struct {
	SecretNames []string `json:"secret_names"`
}

type runtimeSecretUpsertRequest struct {
	Secrets []runtimeSecretValue `json:"secrets"`
}

type runtimeSecretResolveResponse struct {
	Secrets []runtimeSecretValue `json:"secrets"`
}

type runtimeSecretValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func RegisterInternalRoutes(mux *http.ServeMux, svc *secrets.Service, cfg InternalRoutesConfig) error {
	if mux == nil {
		return fmt.Errorf("internal routes mux is required")
	}
	resolvePolicies, err := normalizeRuntimeSecretPolicies(cfg.RuntimeSecretPolicies)
	if err != nil {
		return err
	}
	writePolicies, err := normalizeRuntimeSecretPolicies(cfg.RuntimeSecretWritePolicies)
	if err != nil {
		return err
	}
	if cfg.SandboxPeerID.IsZero() {
		return fmt.Errorf("sandbox SPIFFE ID is required")
	}
	cfg.PlatformOrgID = strings.TrimSpace(cfg.PlatformOrgID)
	if cfg.PlatformOrgID == "" {
		return fmt.Errorf("platform org id is required")
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
		if peerID != cfg.SandboxPeerID {
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
	mux.HandleFunc("/internal/v1/platform-secrets/resolve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if _, ok := workloadauth.PeerIDFromContext(r.Context()); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		defer r.Body.Close()
		var request runtimeSecretResolveRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, injectionRequestMaxBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			http.Error(w, "invalid runtime secret request", http.StatusBadRequest)
			return
		}
		response, err := resolveRuntimeSecrets(r.Context(), svc, cfg.PlatformOrgID, resolvePolicies, request)
		if err != nil {
			switch {
			case errors.Is(err, secrets.ErrInvalidArgument):
				http.Error(w, "invalid runtime secret request", http.StatusBadRequest)
			case errors.Is(err, secrets.ErrForbidden):
				http.Error(w, "forbidden", http.StatusForbidden)
			case errors.Is(err, secrets.ErrNotFound):
				http.Error(w, "runtime secret not found", http.StatusNotFound)
			default:
				http.Error(w, "resolve runtime secrets failed", http.StatusInternalServerError)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(response)
	})
	mux.HandleFunc("/internal/v1/platform-secrets/upsert", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if _, ok := workloadauth.PeerIDFromContext(r.Context()); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		defer r.Body.Close()
		var request runtimeSecretUpsertRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, injectionRequestMaxBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			http.Error(w, "invalid runtime secret upsert request", http.StatusBadRequest)
			return
		}
		if err := upsertRuntimeSecrets(r.Context(), svc, cfg.PlatformOrgID, writePolicies, request); err != nil {
			switch {
			case errors.Is(err, secrets.ErrInvalidArgument):
				http.Error(w, "invalid runtime secret upsert request", http.StatusBadRequest)
			case errors.Is(err, secrets.ErrForbidden):
				http.Error(w, "forbidden", http.StatusForbidden)
			default:
				http.Error(w, "upsert runtime secrets failed", http.StatusInternalServerError)
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return nil
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
		attribute.String("forge_metal.org_id", request.OrgID),
		attribute.String("forge_metal.execution_id", request.ExecutionID),
		attribute.String("forge_metal.attempt_id", request.AttemptID),
		attribute.String("spiffe.peer_id", peerID.String()),
		attribute.Int("forge_metal.secret_env_count", len(request.Secrets)),
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
		attribute.String("forge_metal.org_id", request.OrgID),
		attribute.String("forge_metal.execution_id", request.ExecutionID),
		attribute.String("forge_metal.attempt_id", request.AttemptID),
		attribute.String("spiffe.peer_id", peerID.String()),
		attribute.String("bao.namespace", request.OrgID),
		attribute.String("forge_metal.cache_outcome", "delegated"),
		attribute.Int("forge_metal.secret_env_count", len(request.Secrets)),
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

func resolveRuntimeSecrets(ctx context.Context, svc *secrets.Service, platformOrgID string, policies map[spiffeid.ID]runtimeSecretPolicy, request runtimeSecretResolveRequest) (runtimeSecretResolveResponse, error) {
	ctx, span := apiTracer.Start(ctx, "secrets.platform.resolve")
	defer span.End()
	ctx = secrets.ContextWithOpenBaoAuditInfo(ctx)

	peerID, ok := workloadauth.PeerIDFromContext(ctx)
	if !ok {
		err := fmt.Errorf("%w: missing SPIFFE peer identity", secrets.ErrForbidden)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return runtimeSecretResolveResponse{}, err
	}
	policy, ok := policies[peerID]
	if !ok {
		err := fmt.Errorf("%w: SPIFFE peer is not allowed to resolve platform runtime secrets", secrets.ErrForbidden)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return runtimeSecretResolveResponse{}, err
	}
	secretNames, err := normalizeRuntimeSecretNames(request.SecretNames)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return runtimeSecretResolveResponse{}, err
	}
	span.SetAttributes(
		attribute.String("forge_metal.org_id", platformOrgID),
		attribute.String("spiffe.peer_id", peerID.String()),
		attribute.String("forge_metal.runtime_secret_consumer", policy.credentialName),
		attribute.Int("forge_metal.secret_count", len(secretNames)),
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
	response := runtimeSecretResolveResponse{Secrets: make([]runtimeSecretValue, 0, len(secretNames))}
	for _, secretName := range secretNames {
		if _, allowed := policy.secretNames[secretName]; !allowed {
			err := fmt.Errorf("%w: platform runtime secret %q is not allowed for %s", secrets.ErrForbidden, secretName, policy.credentialName)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return runtimeSecretResolveResponse{}, err
		}
		value, err := svc.ReadSecret(ctx, principal, secrets.KindSecret, secretName, secrets.Scope{Level: secrets.ScopeOrg})
		auditRuntimeSecret(ctx, platformOrgID, peerID, policy.credentialName, secretName, value, err)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return runtimeSecretResolveResponse{}, err
		}
		response.Secrets = append(response.Secrets, runtimeSecretValue{Name: secretName, Value: value.Value})
	}
	return response, nil
}

func upsertRuntimeSecrets(ctx context.Context, svc *secrets.Service, platformOrgID string, policies map[spiffeid.ID]runtimeSecretPolicy, request runtimeSecretUpsertRequest) error {
	ctx, span := apiTracer.Start(ctx, "secrets.platform.upsert")
	defer span.End()
	ctx = secrets.ContextWithOpenBaoAuditInfo(ctx)

	peerID, ok := workloadauth.PeerIDFromContext(ctx)
	if !ok {
		err := fmt.Errorf("%w: missing SPIFFE peer identity", secrets.ErrForbidden)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	policy, ok := policies[peerID]
	if !ok {
		err := fmt.Errorf("%w: SPIFFE peer is not allowed to upsert platform runtime secrets", secrets.ErrForbidden)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	secretValues, err := normalizeRuntimeSecretValues(request.Secrets)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(
		attribute.String("forge_metal.org_id", platformOrgID),
		attribute.String("spiffe.peer_id", peerID.String()),
		attribute.String("forge_metal.runtime_secret_consumer", policy.credentialName),
		attribute.Int("forge_metal.secret_count", len(secretValues)),
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
	for _, secretValue := range secretValues {
		if _, allowed := policy.secretNames[secretValue.Name]; !allowed {
			err := fmt.Errorf("%w: platform runtime secret %q is not allowed for %s", secrets.ErrForbidden, secretValue.Name, policy.credentialName)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		currentValue, err := svc.ReadSecret(ctx, principal, secrets.KindSecret, secretValue.Name, secrets.Scope{Level: secrets.ScopeOrg})
		if err != nil && !errors.Is(err, secrets.ErrNotFound) {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		if err == nil && currentValue.Value == secretValue.Value {
			continue
		}
		record, err := svc.PutSecret(ctx, principal, secrets.PutSecretRequest{
			Kind:  secrets.KindSecret,
			Name:  secretValue.Name,
			Scope: secrets.Scope{Level: secrets.ScopeOrg},
			Value: secretValue.Value,
		})
		auditRuntimeSecretWrite(ctx, platformOrgID, peerID, policy.credentialName, secretValue.Name, record, err)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	return nil
}

func runtimeSecretWriteOpenBaoRole(orgID string) string {
	return "secrets-runtime-write-" + strings.TrimSpace(orgID)
}

func normalizeRuntimeSecretPolicies(items []RuntimeSecretPolicy) (map[spiffeid.ID]runtimeSecretPolicy, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("at least one runtime secret policy is required")
	}
	out := make(map[spiffeid.ID]runtimeSecretPolicy, len(items))
	for _, item := range items {
		if item.PeerID.IsZero() {
			return nil, fmt.Errorf("runtime secret peer ID is required")
		}
		if _, exists := out[item.PeerID]; exists {
			return nil, fmt.Errorf("duplicate runtime secret policy for %s", item.PeerID.String())
		}
		name := strings.TrimSpace(item.CredentialName)
		if name == "" {
			return nil, fmt.Errorf("runtime secret credential name is required for %s", item.PeerID.String())
		}
		secretNames, err := normalizeRuntimeSecretNames(item.SecretNames)
		if err != nil {
			return nil, err
		}
		allowed := make(map[string]struct{}, len(secretNames))
		for _, secretName := range secretNames {
			allowed[secretName] = struct{}{}
		}
		out[item.PeerID] = runtimeSecretPolicy{
			credentialName: name,
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

func normalizeRuntimeSecretValues(values []runtimeSecretValue) ([]runtimeSecretValue, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("%w: at least one runtime secret is required", secrets.ErrInvalidArgument)
	}
	if len(values) > 32 {
		return nil, fmt.Errorf("%w: at most 32 runtime secrets are allowed", secrets.ErrInvalidArgument)
	}
	out := make([]runtimeSecretValue, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: runtime secret name is required", secrets.ErrInvalidArgument)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("%w: duplicate runtime secret %q", secrets.ErrInvalidArgument, name)
		}
		if len(value.Value) > 64<<10 {
			return nil, fmt.Errorf("%w: runtime secret %q exceeds 64KiB", secrets.ErrInvalidArgument, name)
		}
		seen[name] = struct{}{}
		out = append(out, runtimeSecretValue{Name: name, Value: value.Value})
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
