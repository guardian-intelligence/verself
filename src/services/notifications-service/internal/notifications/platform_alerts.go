package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	crossServiceFailureWorkflowKey = "platform.cross_service_call.failed"
	crossServiceFailureActor       = "system:platform-alerts"
)

type CrossServiceFailureAlertConfig struct {
	OrgID    string
	Email    string
	Lookback time.Duration
	Limit    uint64
}

type crossServiceFailure struct {
	Timestamp        time.Time
	ServiceName      string
	TraceID          string
	SpanID           string
	LocalID          string
	ExpectedServerID string
	PeerService      string
	Method           string
	URLScheme        string
	ServerAddress    string
	ServerPort       string
	URLPath          string
	StatusMessage    string
	Duration         time.Duration
}

func (s *Service) AlertCrossServiceFailures(ctx context.Context, cfg CrossServiceFailureAlertConfig) (int, error) {
	cfg, err := normalizeCrossServiceFailureAlertConfig(cfg)
	if err != nil {
		return 0, err
	}
	failures, err := s.queryCrossServiceFailures(ctx, cfg)
	if err != nil {
		return 0, err
	}
	for _, failure := range failures {
		if _, err := s.TriggerWorkflow(ctx, crossServiceFailureWorkflow(cfg, failure)); err != nil {
			return 0, fmt.Errorf("trigger cross-service failure alert: %w", err)
		}
	}
	return len(failures), nil
}

func normalizeCrossServiceFailureAlertConfig(cfg CrossServiceFailureAlertConfig) (CrossServiceFailureAlertConfig, error) {
	cfg.OrgID = strings.TrimSpace(cfg.OrgID)
	cfg.Email = strings.TrimSpace(cfg.Email)
	if cfg.OrgID == "" {
		return cfg, fmt.Errorf("%w: platform alert org_id is required", ErrInvalidInput)
	}
	if cfg.Email == "" {
		return cfg, fmt.Errorf("%w: platform alert email is required", ErrInvalidInput)
	}
	if cfg.Lookback <= 0 {
		cfg.Lookback = 2 * time.Minute
	}
	if cfg.Limit == 0 {
		cfg.Limit = 100
	}
	return cfg, nil
}

func (s *Service) queryCrossServiceFailures(ctx context.Context, cfg CrossServiceFailureAlertConfig) (_ []crossServiceFailure, err error) {
	if s.CH == nil {
		return nil, fmt.Errorf("%w: clickhouse unavailable", ErrStoreUnavailable)
	}
	rows, err := s.CH.Query(ctx, `
SELECT
    Timestamp,
    ServiceName,
    TraceId,
    SpanId,
    SpanAttributes['spiffe.local_id'] AS local_id,
    SpanAttributes['spiffe.expected_server_id'] AS expected_server_id,
    SpanAttributes['peer.service'] AS peer_service,
    SpanAttributes['http.request.method'] AS method,
    SpanAttributes['url.scheme'] AS url_scheme,
    SpanAttributes['server.address'] AS server_address,
    SpanAttributes['server.port'] AS server_port,
    SpanAttributes['url.path'] AS url_path,
    StatusMessage,
    Duration
FROM default.otel_traces
WHERE Timestamp >= now64(9) - toIntervalSecond($1)
  AND ResourceAttributes['verself.supervisor'] = 'nomad'
  AND SpanKind = 'Client'
  AND SpanName = 'auth.spiffe.mtls.client'
  AND StatusCode = 'Error'
ORDER BY Timestamp ASC, TraceId ASC, SpanId ASC
LIMIT $2`, uint32(cfg.Lookback.Round(time.Second).Seconds()), cfg.Limit)
	if err != nil {
		return nil, fmt.Errorf("%w: query cross-service failure spans: %v", ErrStoreUnavailable, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("%w: close cross-service failure rows: %v", ErrStoreUnavailable, closeErr)
		}
	}()
	failures := make([]crossServiceFailure, 0)
	for rows.Next() {
		var failure crossServiceFailure
		var durationNS uint64
		if err := rows.Scan(
			&failure.Timestamp,
			&failure.ServiceName,
			&failure.TraceID,
			&failure.SpanID,
			&failure.LocalID,
			&failure.ExpectedServerID,
			&failure.PeerService,
			&failure.Method,
			&failure.URLScheme,
			&failure.ServerAddress,
			&failure.ServerPort,
			&failure.URLPath,
			&failure.StatusMessage,
			&durationNS,
		); err != nil {
			return nil, fmt.Errorf("%w: scan cross-service failure span: %v", ErrStoreUnavailable, err)
		}
		failure.Duration = time.Duration(durationNS)
		failures = append(failures, failure)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: read cross-service failure spans: %v", ErrStoreUnavailable, err)
	}
	return failures, nil
}

func crossServiceFailureWorkflow(cfg CrossServiceFailureAlertConfig, failure crossServiceFailure) WorkflowTriggerRequest {
	data, _ := json.Marshal(map[string]any{
		"event_time":         failure.Timestamp.UTC().Format(time.RFC3339Nano),
		"service_name":       failure.ServiceName,
		"local_id":           failure.LocalID,
		"target_service":     failure.targetService(),
		"expected_server_id": failure.ExpectedServerID,
		"method":             failure.Method,
		"endpoint":           failure.endpoint(),
		"status_message":     failure.StatusMessage,
		"duration_ms":        failure.Duration.Milliseconds(),
		"trace_id":           failure.TraceID,
		"span_id":            failure.SpanID,
	})
	return WorkflowTriggerRequest{
		WorkflowKey:    crossServiceFailureWorkflowKey,
		OrgID:          cfg.OrgID,
		TriggeredBy:    crossServiceFailureActor,
		IdempotencyKey: "platform:cross_service_call_failed:" + failure.TraceID + ":" + failure.SpanID,
		Recipients: []WorkflowRecipient{
			{Email: cfg.Email},
		},
		Title:        truncateAlertText("Cross-service call failed: "+failure.ServiceName+" -> "+failure.targetService(), 120),
		Body:         truncateAlertText(failure.body(), 500),
		Priority:     PriorityHigh,
		ResourceKind: "otel_span",
		ResourceID:   failure.TraceID + ":" + failure.SpanID,
		Data:         data,
		Traceparent:  failure.traceparent(),
	}
}

func (f crossServiceFailure) targetService() string {
	if service := strings.TrimSpace(f.PeerService); service != "" {
		return service
	}
	id := strings.TrimSpace(f.ExpectedServerID)
	const marker = "/svc/"
	if idx := strings.LastIndex(id, marker); idx >= 0 {
		return strings.TrimSpace(id[idx+len(marker):])
	}
	if id != "" {
		return id
	}
	return "unknown-service"
}

func (f crossServiceFailure) endpoint() string {
	address := strings.TrimSpace(f.ServerAddress)
	if address == "" {
		return ""
	}
	port := strings.TrimSpace(f.ServerPort)
	if port != "" {
		address += ":" + port
	}
	scheme := strings.TrimSpace(f.URLScheme)
	if scheme == "" {
		scheme = "https"
	}
	path := strings.TrimSpace(f.URLPath)
	if path == "" {
		path = "/"
	}
	return scheme + "://" + address + path
}

func (f crossServiceFailure) body() string {
	parts := []string{
		fmt.Sprintf("%s failed calling %s", f.ServiceName, f.targetService()),
		fmt.Sprintf("status=%q", strings.TrimSpace(f.StatusMessage)),
		fmt.Sprintf("duration_ms=%d", f.Duration.Milliseconds()),
	}
	if endpoint := f.endpoint(); endpoint != "" {
		parts = append(parts, "endpoint="+endpoint)
	}
	if method := strings.TrimSpace(f.Method); method != "" {
		parts = append(parts, "method="+method)
	}
	parts = append(parts, "trace_id="+f.TraceID, "span_id="+f.SpanID)
	return strings.Join(parts, " ")
}

func (f crossServiceFailure) traceparent() string {
	if len(f.TraceID) != 32 || len(f.SpanID) != 16 {
		return ""
	}
	return "00-" + f.TraceID + "-" + f.SpanID + "-01"
}

func truncateAlertText(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}
