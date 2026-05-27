package notifications

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	crossServiceFailureWorkflowKey = "platform.cross_service_call.failed"
	durableCacheMissWorkflowKey    = "platform.durable_cache.missed"
	goldenVMMissWorkflowKey        = "platform.firecracker_snapshot_vm.missed"
	platformAlertActor             = "system:platform-alerts"
)

type PlatformAlertConfig struct {
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

type cacheMissAlert struct {
	Timestamp               time.Time
	Kind                    string
	OrgID                   string
	Provider                string
	ProviderRepositoryID    uint64
	ProviderRunID           uint64
	ProviderRunAttempt      uint64
	ProviderJobID           uint64
	ExecutionID             string
	AttemptID               string
	OperationID             string
	CacheName               string
	EventName               string
	Result                  string
	Reason                  string
	JobShapeID              string
	SourceGenerationSetHash string
	TraceID                 string
	SpanID                  string
}

func (s *Service) AlertCrossServiceFailures(ctx context.Context, cfg PlatformAlertConfig) (int, error) {
	cfg, err := normalizePlatformAlertConfig(cfg)
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

func (s *Service) AlertCacheMisses(ctx context.Context, cfg PlatformAlertConfig) (int, error) {
	cfg, err := normalizePlatformAlertConfig(cfg)
	if err != nil {
		return 0, err
	}
	misses, err := s.queryCacheMisses(ctx, cfg)
	if err != nil {
		return 0, err
	}
	for _, miss := range misses {
		if _, err := s.TriggerWorkflow(ctx, cacheMissWorkflow(cfg, miss)); err != nil {
			return 0, fmt.Errorf("trigger cache miss alert: %w", err)
		}
	}
	return len(misses), nil
}

func normalizePlatformAlertConfig(cfg PlatformAlertConfig) (PlatformAlertConfig, error) {
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

func (s *Service) queryCrossServiceFailures(ctx context.Context, cfg PlatformAlertConfig) (_ []crossServiceFailure, err error) {
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

func (s *Service) queryCacheMisses(ctx context.Context, cfg PlatformAlertConfig) (_ []cacheMissAlert, err error) {
	if s.CH == nil {
		return nil, fmt.Errorf("%w: clickhouse unavailable", ErrStoreUnavailable)
	}
	rows, err := s.CH.Query(ctx, `
SELECT
    observed_at,
    kind,
    org_id,
    provider,
    provider_repository_id,
    provider_run_id,
    provider_run_attempt,
    provider_job_id,
    execution_id,
    attempt_id,
    operation_id,
    cache_name,
    event_name,
    result,
    reason,
    job_shape_id,
    source_generation_set_hash,
    trace_id,
    span_id
FROM
(
    SELECT
        observed_at,
        'durable_cache' AS kind,
        org_id,
        provider,
        provider_repository_id,
        provider_run_id,
        provider_run_attempt,
        provider_job_id,
        toString(execution_id) AS execution_id,
        toString(attempt_id) AS attempt_id,
        toString(operation_id) AS operation_id,
        cache_name,
        event_name,
        result,
        reason,
        '' AS job_shape_id,
        '' AS source_generation_set_hash,
        trace_id,
        span_id
    FROM verself.durable_events
    WHERE observed_at >= now64(6) - toIntervalSecond($1)
      AND event_name = 'durable.cache.select'
      AND result = 'miss'

    UNION ALL

    SELECT
        observed_at,
        'firecracker_snapshot_vm' AS kind,
        org_id,
        provider,
        provider_repository_id,
        provider_run_id,
        provider_run_attempt,
        provider_job_id,
        toString(execution_id) AS execution_id,
        toString(attempt_id) AS attempt_id,
        toString(operation_id) AS operation_id,
        '' AS cache_name,
        event_name,
        result,
        reason,
        toString(job_shape_id) AS job_shape_id,
        source_generation_set_hash,
        trace_id,
        span_id
    FROM verself.golden_vm_events
    WHERE observed_at >= now64(6) - toIntervalSecond($1)
      AND event_name = 'golden.vm.lookup'
      AND result = 'miss'
)
ORDER BY observed_at ASC, kind ASC, operation_id ASC
LIMIT $2`, uint32(cfg.Lookback.Round(time.Second).Seconds()), cfg.Limit)
	if err != nil {
		return nil, fmt.Errorf("%w: query cache miss events: %v", ErrStoreUnavailable, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("%w: close cache miss rows: %v", ErrStoreUnavailable, closeErr)
		}
	}()
	misses := make([]cacheMissAlert, 0)
	for rows.Next() {
		var miss cacheMissAlert
		if err := rows.Scan(
			&miss.Timestamp,
			&miss.Kind,
			&miss.OrgID,
			&miss.Provider,
			&miss.ProviderRepositoryID,
			&miss.ProviderRunID,
			&miss.ProviderRunAttempt,
			&miss.ProviderJobID,
			&miss.ExecutionID,
			&miss.AttemptID,
			&miss.OperationID,
			&miss.CacheName,
			&miss.EventName,
			&miss.Result,
			&miss.Reason,
			&miss.JobShapeID,
			&miss.SourceGenerationSetHash,
			&miss.TraceID,
			&miss.SpanID,
		); err != nil {
			return nil, fmt.Errorf("%w: scan cache miss event: %v", ErrStoreUnavailable, err)
		}
		misses = append(misses, miss)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: read cache miss events: %v", ErrStoreUnavailable, err)
	}
	return misses, nil
}

func crossServiceFailureWorkflow(cfg PlatformAlertConfig, failure crossServiceFailure) WorkflowTriggerRequest {
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
		TriggeredBy:    platformAlertActor,
		IdempotencyKey: crossServiceFailureDedupeKey(failure),
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

func cacheMissWorkflow(cfg PlatformAlertConfig, miss cacheMissAlert) WorkflowTriggerRequest {
	data, _ := json.Marshal(map[string]any{
		"event_time":                 miss.Timestamp.UTC().Format(time.RFC3339Nano),
		"kind":                       miss.Kind,
		"event_name":                 miss.EventName,
		"result":                     miss.Result,
		"reason":                     miss.Reason,
		"org_id":                     miss.OrgID,
		"provider":                   miss.Provider,
		"provider_repository_id":     miss.ProviderRepositoryID,
		"provider_run_id":            miss.ProviderRunID,
		"provider_run_attempt":       miss.ProviderRunAttempt,
		"provider_job_id":            miss.ProviderJobID,
		"execution_id":               miss.ExecutionID,
		"attempt_id":                 miss.AttemptID,
		"operation_id":               miss.OperationID,
		"cache_name":                 miss.CacheName,
		"job_shape_id":               miss.JobShapeID,
		"source_generation_set_hash": miss.SourceGenerationSetHash,
		"trace_id":                   miss.TraceID,
		"span_id":                    miss.SpanID,
	})
	return WorkflowTriggerRequest{
		WorkflowKey:    miss.workflowKey(),
		OrgID:          cfg.OrgID,
		TriggeredBy:    platformAlertActor,
		IdempotencyKey: cacheMissDedupeKey(miss),
		Recipients: []WorkflowRecipient{
			{Email: cfg.Email},
		},
		Title:        truncateAlertText(miss.title(), 120),
		Body:         truncateAlertText(miss.body(), 500),
		Priority:     PriorityHigh,
		ResourceKind: miss.resourceKind(),
		ResourceID:   miss.resourceID(),
		Data:         data,
		Traceparent:  miss.traceparent(),
	}
}

func crossServiceFailureDedupeKey(failure crossServiceFailure) string {
	bucket := failure.Timestamp.UTC().Truncate(time.Hour).Format(time.RFC3339)
	sum := sha256.Sum256([]byte(strings.Join([]string{
		bucket,
		failure.ServiceName,
		failure.targetService(),
		failure.Method,
		failure.endpoint(),
	}, "\x00")))
	return "platform:cross_service_call_failed:" + hex.EncodeToString(sum[:8])
}

func cacheMissDedupeKey(miss cacheMissAlert) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		miss.Kind,
		miss.EventName,
		miss.OperationID,
		miss.ExecutionID,
		miss.AttemptID,
		miss.CacheName,
		miss.Timestamp.UTC().Format(time.RFC3339Nano),
	}, "\x00")))
	return "platform:cache_miss:" + hex.EncodeToString(sum[:8])
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

func (m cacheMissAlert) workflowKey() string {
	switch m.Kind {
	case "firecracker_snapshot_vm":
		return goldenVMMissWorkflowKey
	default:
		return durableCacheMissWorkflowKey
	}
}

func (m cacheMissAlert) title() string {
	switch m.Kind {
	case "firecracker_snapshot_vm":
		return "Firecracker snapshot VM missed"
	default:
		if cache := strings.TrimSpace(m.CacheName); cache != "" {
			return "Durable storage cache missed: " + cache
		}
		return "Durable storage cache missed"
	}
}

func (m cacheMissAlert) body() string {
	parts := []string{m.title()}
	if reason := strings.TrimSpace(m.Reason); reason != "" {
		parts = append(parts, "reason="+reason)
	}
	if provider := strings.TrimSpace(m.Provider); provider != "" {
		parts = append(parts, "provider="+provider)
	}
	if m.ProviderRepositoryID != 0 {
		parts = append(parts, fmt.Sprintf("repository_id=%d", m.ProviderRepositoryID))
	}
	if m.ProviderRunID != 0 {
		parts = append(parts, fmt.Sprintf("run_id=%d", m.ProviderRunID))
	}
	if m.ProviderRunAttempt != 0 {
		parts = append(parts, fmt.Sprintf("run_attempt=%d", m.ProviderRunAttempt))
	}
	if m.ProviderJobID != 0 {
		parts = append(parts, fmt.Sprintf("job_id=%d", m.ProviderJobID))
	}
	if m.JobShapeID != "" {
		parts = append(parts, "job_shape_id="+m.JobShapeID)
	}
	parts = append(parts, "execution_id="+m.ExecutionID, "attempt_id="+m.AttemptID, "operation_id="+m.OperationID)
	if m.TraceID != "" {
		parts = append(parts, "trace_id="+m.TraceID)
	}
	if m.SpanID != "" {
		parts = append(parts, "span_id="+m.SpanID)
	}
	return strings.Join(parts, " ")
}

func (m cacheMissAlert) resourceKind() string {
	switch m.Kind {
	case "firecracker_snapshot_vm":
		return "golden_vm_snapshot"
	default:
		return "durable_cache"
	}
}

func (m cacheMissAlert) resourceID() string {
	if m.CacheName == "" {
		return m.OperationID
	}
	return m.OperationID + ":" + m.CacheName
}

func (m cacheMissAlert) traceparent() string {
	if len(m.TraceID) != 32 || len(m.SpanID) != 16 {
		return ""
	}
	return "00-" + m.TraceID + "-" + m.SpanID + "-01"
}

func truncateAlertText(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}
