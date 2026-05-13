package api

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spiffe/go-spiffe/v2/workloadapi"
	governanceinternalclient "github.com/verself/governance-service/internalclient"
	"github.com/verself/object-storage-service/internal/objectstorage"
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
		slog.Default().Error("object-storage governance audit mtls client init failed", "error", err)
		return
	}
	sinkClient, err := governanceinternalclient.NewClient(url, governanceinternalclient.WithHTTPClient(httpClient))
	if err != nil {
		slog.Default().Error("object-storage governance audit client init failed", "error", err)
		return
	}
	configuredAuditSink.Store(&auditSinkConfig{
		Client: sinkClient,
	})
}

func SendGovernanceAudit(ctx context.Context, record objectstorageAuditRecord) error {
	sink := configuredAuditSink.Load()
	if sink == nil || strings.TrimSpace(record.OrgID) == "" {
		return nil
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	resp, err := sink.Client.AppendAuditEvent(reqCtx, governanceinternalclient.AppendAuditEventRequest{Body: governanceAuditRecordToContract(record)})
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("governance audit rejected with status %d", resp.StatusCode)
		slog.Default().ErrorContext(ctx, "object-storage governance audit rejected", "status", resp.StatusCode)
		return err
	}
	return nil
}

type objectstorageAuditRecord = objectstorage.AuditRecord

func governanceAuditRecordToContract(record objectstorageAuditRecord) governanceinternalclient.AuditRecord {
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
