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
	OrgID        string         `json:"org_id"`
	EventSource  string         `json:"event_source"`
	EventName    string         `json:"event_name"`
	AuditEvent   string         `json:"audit_event"`
	ActorType    string         `json:"actor_type"`
	ActorID      string         `json:"actor_id"`
	CredentialID string         `json:"credential_id,omitempty"`
	Permission   string         `json:"permission"`
	TargetType   string         `json:"target_type"`
	TargetID     string         `json:"target_id,omitempty"`
	Outcome      string         `json:"outcome"`
	ErrorCode    string         `json:"error_code,omitempty"`
	TraceID      string         `json:"trace_id,omitempty"`
	Detail       map[string]any `json:"detail,omitempty"`
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
