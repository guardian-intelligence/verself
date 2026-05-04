package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spiffe/go-spiffe/v2/workloadapi"
	governanceinternalclient "github.com/verself/governance-service/internalclient"
	workloadauth "github.com/verself/service-runtime/workload"
)

type auditSinkConfig struct {
	Client *governanceinternalclient.ClientWithResponses
}

var configuredAuditSink atomic.Pointer[auditSinkConfig]

func ConfigureAuditSink(url string, source *workloadapi.X509Source) {
	url = strings.TrimSpace(url)
	if url == "" || source == nil {
		return
	}
	httpClient, err := workloadauth.MTLSClientForService(source, workloadauth.ServiceGovernance, nil)
	if err != nil {
		slog.Default().Error("secrets governance audit mtls client init failed", "error", err)
		return
	}
	sinkClient, err := governanceinternalclient.NewClientWithResponses(url, governanceinternalclient.WithHTTPClient(httpClient))
	if err != nil {
		slog.Default().Error("secrets governance audit client init failed", "error", err)
		return
	}
	configuredAuditSink.Store(&auditSinkConfig{
		Client: sinkClient,
	})
}

type governanceAuditRecord struct {
	OrgID                 string `json:"org_id"`
	SourceProductArea     string `json:"source_product_area"`
	ServiceName           string `json:"service_name"`
	OperationID           string `json:"operation_id"`
	AuditEvent            string `json:"audit_event"`
	OperationDisplay      string `json:"operation_display"`
	OperationType         string `json:"operation_type"`
	EventCategory         string `json:"event_category"`
	RiskLevel             string `json:"risk_level"`
	DataClassification    string `json:"data_classification,omitempty"`
	ActorType             string `json:"actor_type"`
	ActorID               string `json:"actor_id"`
	ActorDisplay          string `json:"actor_display,omitempty"`
	ActorOwnerID          string `json:"actor_owner_id,omitempty"`
	ActorOwnerDisplay     string `json:"actor_owner_display,omitempty"`
	ActorSPIFFEID         string `json:"actor_spiffe_id,omitempty"`
	CredentialID          string `json:"credential_id,omitempty"`
	CredentialName        string `json:"credential_name,omitempty"`
	CredentialFingerprint string `json:"credential_fingerprint,omitempty"`
	AuthMethod            string `json:"auth_method,omitempty"`
	Permission            string `json:"permission"`
	TargetKind            string `json:"target_kind"`
	TargetID              string `json:"target_id,omitempty"`
	TargetDisplay         string `json:"target_display,omitempty"`
	TargetScope           string `json:"target_scope,omitempty"`
	TargetPathHash        string `json:"target_path_hash,omitempty"`
	Action                string `json:"action"`
	OrgScope              string `json:"org_scope"`
	RateLimitClass        string `json:"rate_limit_class"`
	Decision              string `json:"decision"`
	Result                string `json:"result"`
	DenialReason          string `json:"denial_reason,omitempty"`
	ErrorCode             string `json:"error_code,omitempty"`
	ErrorClass            string `json:"error_class,omitempty"`
	ErrorMessage          string `json:"error_message,omitempty"`
	ClientIP              string `json:"client_ip,omitempty"`
	IPChain               string `json:"ip_chain,omitempty"`
	IPChainTrustedHops    uint8  `json:"ip_chain_trusted_hops,omitempty"`
	UserAgentRaw          string `json:"user_agent_raw,omitempty"`
	RefererOrigin         string `json:"referer_origin,omitempty"`
	Origin                string `json:"origin,omitempty"`
	Host                  string `json:"host,omitempty"`
	RequestID             string `json:"request_id,omitempty"`
	RouteTemplate         string `json:"route_template,omitempty"`
	HTTPMethod            string `json:"http_method,omitempty"`
	HTTPStatus            uint16 `json:"http_status,omitempty"`
	IdempotencyKeyHash    string `json:"idempotency_key_hash,omitempty"`
	TrustClass            string `json:"trust_class,omitempty"`
	ContentSHA256         string `json:"content_sha256,omitempty"`
	SecretMount           string `json:"secret_mount,omitempty"`
	SecretPathHash        string `json:"secret_path_hash,omitempty"`
	SecretVersion         uint64 `json:"secret_version,omitempty"`
	SecretOperation       string `json:"secret_operation,omitempty"`
	KeyID                 string `json:"key_id,omitempty"`
	OpenBaoRequestID      string `json:"openbao_request_id,omitempty"`
	OpenBaoAccessorHash   string `json:"openbao_accessor_hash,omitempty"`
}

func sendGovernanceAudit(ctx context.Context, record governanceAuditRecord) {
	sink := configuredAuditSink.Load()
	if sink == nil || record.OrgID == "" {
		return
	}
	body, err := json.Marshal(record)
	if err != nil {
		slog.Default().ErrorContext(ctx, "secrets governance audit marshal failed", "error", err)
		return
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	resp, err := sink.Client.AppendAuditEventWithBodyWithResponse(reqCtx, "application/json", bytes.NewReader(body))
	if err != nil {
		slog.Default().ErrorContext(ctx, "secrets governance audit send failed", "error", err)
		return
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		slog.Default().ErrorContext(ctx, "secrets governance audit rejected", "status", resp.StatusCode())
	}
}

func hashTextForAudit(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
