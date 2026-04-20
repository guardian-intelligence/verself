package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/forge-metal/secrets-service/internal/secrets"
)

const injectionRequestMaxBytes = 128 << 10

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
}

type injectionResolveResponse struct {
	Env []injectionEnvValue `json:"env"`
}

type injectionEnvValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func RegisterInternalRoutes(mux *http.ServeMux, svc *secrets.Service, token string) {
	mux.HandleFunc("/internal/v1/injections/resolve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !validInternalBearer(token, r.Header.Get("Authorization")) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
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
}

func resolveInjection(ctx context.Context, svc *secrets.Service, request injectionResolveRequest) (injectionResolveResponse, error) {
	ctx = secrets.ContextWithOpenBaoAuditInfo(ctx)
	request.OrgID = strings.TrimSpace(request.OrgID)
	request.ActorID = strings.TrimSpace(request.ActorID)
	request.ExecutionID = strings.TrimSpace(request.ExecutionID)
	request.AttemptID = strings.TrimSpace(request.AttemptID)
	if request.OrgID == "" || request.ActorID == "" || request.ExecutionID == "" || request.AttemptID == "" {
		return injectionResolveResponse{}, fmt.Errorf("%w: org_id, actor_id, execution_id, and attempt_id are required", secrets.ErrInvalidArgument)
	}
	if len(request.Secrets) == 0 {
		return injectionResolveResponse{Env: []injectionEnvValue{}}, nil
	}
	if len(request.Secrets) > 64 {
		return injectionResolveResponse{}, fmt.Errorf("%w: at most 64 secret injections are allowed", secrets.ErrInvalidArgument)
	}
	principal := secrets.Principal{
		OrgID:        request.OrgID,
		Subject:      request.ActorID,
		Type:         "sandbox_execution",
		AuthMethod:   "internal_token",
		CredentialID: "sandbox-rental-service",
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
			return injectionResolveResponse{}, err
		}
		response.Env = append(response.Env, injectionEnvValue{Name: requested.EnvName, Value: value.Value})
	}
	return response, nil
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
		CredentialID:        "sandbox-rental-service",
		CredentialName:      "sandbox-rental-service",
		AuthMethod:          "internal_token",
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

func validInternalBearer(expected, header string) bool {
	expected = strings.TrimSpace(expected)
	header = strings.TrimSpace(header)
	if expected == "" || !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if len(got) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}
