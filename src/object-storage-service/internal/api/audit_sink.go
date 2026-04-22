package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	governanceinternalclient "github.com/forge-metal/governance-service/internalclient"
	"github.com/forge-metal/object-storage-service/internal/objectstorage"
)

type auditSinkConfig struct {
	Client *governanceinternalclient.ClientWithResponses
}

var configuredAuditSink atomic.Pointer[auditSinkConfig]

func ConfigureAuditSink(url string, client *http.Client) {
	url = strings.TrimSpace(url)
	if url == "" || client == nil {
		return
	}
	sinkClient, err := governanceinternalclient.NewClientWithResponses(url, governanceinternalclient.WithHTTPClient(client))
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
