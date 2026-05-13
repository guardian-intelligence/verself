package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spiffe/go-spiffe/v2/workloadapi"
	governanceinternalclient "github.com/verself/governance-service/internalclient"
	workloadauth "github.com/verself/service-runtime/workload"
)

type auditSinkConfig struct {
	Client *governanceinternalclient.Client
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
	sinkClient, err := governanceinternalclient.NewClient(url, governanceinternalclient.WithHTTPClient(httpClient))
	if err != nil {
		slog.Default().Error("secrets governance audit client init failed", "error", err)
		return
	}
	configuredAuditSink.Store(&auditSinkConfig{
		Client: sinkClient,
	})
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
	reqCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	resp, err := sink.Client.AppendAuditEvent(reqCtx, governanceinternalclient.AppendAuditEventRequest{Body: governanceAuditRecordToContract(record)})
	if err != nil {
		slog.Default().ErrorContext(ctx, "secrets governance audit send failed", "error", err)
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Default().ErrorContext(ctx, "secrets governance audit rejected", "status", resp.StatusCode)
	}
}

func governanceAuditRecordToContract(record governanceAuditRecord) governanceinternalclient.AuditRecord {
	return governanceinternalclient.AuditRecord{
		OrgID:        record.OrgID,
		EventSource:  record.EventSource,
		EventName:    record.EventName,
		AuditEvent:   record.AuditEvent,
		ActorType:    record.ActorType,
		ActorID:      record.ActorID,
		CredentialID: optionalString(record.CredentialID),
		Permission:   record.Permission,
		TargetType:   record.TargetType,
		TargetID:     optionalString(record.TargetID),
		Outcome:      governanceinternalclient.AuditOutcome(record.Outcome),
		ErrorCode:    optionalString(record.ErrorCode),
		TraceID:      optionalString(record.TraceID),
		Detail:       optionalMap(record.Detail),
	}
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func optionalMap(value map[string]any) *map[string]any {
	if len(value) == 0 {
		return nil
	}
	return &value
}

func hashTextForAudit(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func compactAuditDetail(values map[string]any) map[string]any {
	detail := make(map[string]any, len(values))
	for key, value := range values {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				detail[key] = typed
			}
		case uint64:
			if typed != 0 {
				detail[key] = typed
			}
		case nil:
		default:
			detail[key] = value
		}
	}
	return detail
}
