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

	"github.com/forge-metal/object-storage-service/internal/objectstorage"
)

type auditSinkConfig struct {
	URL    string
	Client *http.Client
}

var configuredAuditSink atomic.Pointer[auditSinkConfig]

func ConfigureAuditSink(url string, client *http.Client) {
	url = strings.TrimSpace(url)
	if url == "" || client == nil {
		return
	}
	configuredAuditSink.Store(&auditSinkConfig{
		URL:    url,
		Client: client,
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
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, sink.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := sink.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("governance audit rejected with status %d", resp.StatusCode)
		slog.Default().ErrorContext(ctx, "object-storage governance audit rejected", "status", resp.StatusCode)
		return err
	}
	return nil
}

type objectstorageAuditRecord = objectstorage.AuditRecord
