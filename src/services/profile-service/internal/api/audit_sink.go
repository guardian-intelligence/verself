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
	workloadauth "github.com/verself/auth-middleware/workload"
	governanceinternalclient "github.com/verself/governance-service/internalclient"
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
		slog.Default().Error("profile governance audit mtls client init failed", "error", err)
		return
	}
	client, err := governanceinternalclient.NewClientWithResponses(url, governanceinternalclient.WithHTTPClient(httpClient))
	if err != nil {
		slog.Default().Error("profile governance audit client init failed", "error", err)
		return
	}
	configuredAuditSink.Store(&auditSinkConfig{Client: client})
}

type governanceAuditRecord struct {
	OrgID              string `json:"org_id"`
	SourceProductArea  string `json:"source_product_area"`
	ServiceName        string `json:"service_name"`
	OperationID        string `json:"operation_id"`
	AuditEvent         string `json:"audit_event"`
	OperationDisplay   string `json:"operation_display"`
	OperationType      string `json:"operation_type"`
	EventCategory      string `json:"event_category"`
	RiskLevel          string `json:"risk_level"`
	DataClassification string `json:"data_classification,omitempty"`
	ActorType          string `json:"actor_type"`
	ActorID            string `json:"actor_id"`
	ActorDisplay       string `json:"actor_display,omitempty"`
	AuthMethod         string `json:"auth_method,omitempty"`
	Permission         string `json:"permission"`
	TargetKind         string `json:"target_kind"`
	TargetID           string `json:"target_id,omitempty"`
	Action             string `json:"action"`
	OrgScope           string `json:"org_scope"`
	RateLimitClass     string `json:"rate_limit_class"`
	Decision           string `json:"decision"`
	Result             string `json:"result"`
	DenialReason       string `json:"denial_reason,omitempty"`
	ErrorCode          string `json:"error_code,omitempty"`
	ErrorClass         string `json:"error_class,omitempty"`
	ErrorMessage       string `json:"error_message,omitempty"`
	ClientIP           string `json:"client_ip,omitempty"`
	UserAgentRaw       string `json:"user_agent_raw,omitempty"`
	RequestID          string `json:"request_id,omitempty"`
	RouteTemplate      string `json:"route_template,omitempty"`
	HTTPMethod         string `json:"http_method,omitempty"`
	HTTPStatus         uint16 `json:"http_status,omitempty"`
	IdempotencyKeyHash string `json:"idempotency_key_hash,omitempty"`
	ChangedFields      string `json:"changed_fields,omitempty"`
	BeforeHash         string `json:"before_hash,omitempty"`
	AfterHash          string `json:"after_hash,omitempty"`
	ArtifactSHA256     string `json:"artifact_sha256,omitempty"`
	ArtifactBytes      uint64 `json:"artifact_bytes,omitempty"`
}

func sendGovernanceAudit(ctx context.Context, record governanceAuditRecord) {
	sink := configuredAuditSink.Load()
	if sink == nil || record.OrgID == "" {
		return
	}
	body, err := json.Marshal(record)
	if err != nil {
		slog.Default().ErrorContext(ctx, "profile governance audit marshal failed", "error", err)
		return
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	resp, err := sink.Client.AppendAuditEventWithBodyWithResponse(reqCtx, "application/json", bytes.NewReader(body))
	if err != nil {
		slog.Default().ErrorContext(ctx, "profile governance audit send failed", "error", err)
		return
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		slog.Default().ErrorContext(ctx, "profile governance audit rejected", "status", resp.StatusCode())
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
