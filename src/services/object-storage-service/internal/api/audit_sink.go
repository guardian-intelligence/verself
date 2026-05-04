package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spiffe/go-spiffe/v2/workloadapi"
	workloadauth "github.com/verself/auth-middleware/workload"
	governanceinternalclient "github.com/verself/governance-service/internalclient"
	"github.com/verself/object-storage-service/internal/objectstorage"
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
		slog.Default().Error("object-storage governance audit mtls client init failed", "error", err)
		return
	}
	sinkClient, err := governanceinternalclient.NewClientWithResponses(url, governanceinternalclient.WithHTTPClient(httpClient))
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
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	resp, err := sink.Client.AppendAuditEventWithBodyWithResponse(reqCtx, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		err := fmt.Errorf("governance audit rejected with status %d", resp.StatusCode())
		slog.Default().ErrorContext(ctx, "object-storage governance audit rejected", "status", resp.StatusCode())
		return err
	}
	return nil
}

type objectstorageAuditRecord = objectstorage.AuditRecord
