package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type auditSinkConfig struct {
	URL    string
	Token  string
	Client *http.Client
}

var configuredAuditSink atomic.Pointer[auditSinkConfig]

func ConfigureAuditSink(url, token string) {
	url = strings.TrimSpace(url)
	token = strings.TrimSpace(token)
	if url == "" || token == "" {
		return
	}
	configuredAuditSink.Store(&auditSinkConfig{
		URL:   url,
		Token: token,
		Client: &http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
			Timeout:   1 * time.Second,
		},
	})
}

type governanceAuditRecord struct {
	OrgID              string `json:"org_id"`
	ServiceName        string `json:"service_name"`
	OperationID        string `json:"operation_id"`
	AuditEvent         string `json:"audit_event"`
	PrincipalType      string `json:"principal_type"`
	PrincipalID        string `json:"principal_id"`
	PrincipalEmail     string `json:"principal_email,omitempty"`
	Permission         string `json:"permission"`
	ResourceKind       string `json:"resource_kind"`
	Action             string `json:"action"`
	OrgScope           string `json:"org_scope"`
	RateLimitClass     string `json:"rate_limit_class"`
	Result             string `json:"result"`
	ErrorCode          string `json:"error_code,omitempty"`
	ErrorMessage       string `json:"error_message,omitempty"`
	ClientIP           string `json:"client_ip,omitempty"`
	UserAgent          string `json:"user_agent,omitempty"`
	IdempotencyKeyHash string `json:"idempotency_key_hash,omitempty"`
}

func sendGovernanceAudit(ctx context.Context, record governanceAuditRecord) {
	sink := configuredAuditSink.Load()
	if sink == nil || record.OrgID == "" {
		return
	}
	body, err := json.Marshal(record)
	if err != nil {
		slog.Default().ErrorContext(ctx, "sandbox governance audit marshal failed", "error", err)
		return
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, sink.URL, bytes.NewReader(body))
	if err != nil {
		slog.Default().ErrorContext(ctx, "sandbox governance audit request failed", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+sink.Token)
	resp, err := sink.Client.Do(req)
	if err != nil {
		slog.Default().ErrorContext(ctx, "sandbox governance audit send failed", "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Default().ErrorContext(ctx, "sandbox governance audit rejected", "status", resp.StatusCode)
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
