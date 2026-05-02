package deploydb

import (
	"context"
	"errors"
	"fmt"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const supplyChainPolicyEventsTable = "verself.supply_chain_policy_events"

type SupplyChainPolicyEventRow struct {
	EventAt        time.Time `ch:"event_at"`
	DeployRunKey   string    `ch:"deploy_run_key"`
	Site           string    `ch:"site"`
	SourcePath     string    `ch:"source_path"`
	Line           uint32    `ch:"line"`
	SourceKind     string    `ch:"source_kind"`
	Surface        string    `ch:"surface"`
	Artifact       string    `ch:"artifact"`
	UpstreamURL    string    `ch:"upstream_url"`
	Digest         string    `ch:"digest"`
	PolicyResult   string    `ch:"policy_result"`
	PolicyReason   string    `ch:"policy_reason"`
	AdmissionState string    `ch:"admission_state"`
	TUFTargetPath  string    `ch:"tuf_target_path"`
	StorageURI     string    `ch:"storage_uri"`
	TraceID        string    `ch:"trace_id"`
	SpanID         string    `ch:"span_id"`
	Evidence       string    `ch:"evidence"`
}

func (c *Client) InsertSupplyChainPolicyEvents(ctx context.Context, rows []SupplyChainPolicyEventRow) error {
	return insertStructs(ctx, c, supplyChainPolicyEventsTable, rows)
}

type SupplyChainEvidenceSummary struct {
	DeployRunKey       string
	RowCount           uint64
	Rejected           uint64
	Accepted           uint64
	Provisional        uint64
	EmptyTraceID       uint64
	DistinctTraceID    uint64
	TraceID            string
	PolicyCheckSpans   uint64
	PolicyRecordSpans  uint64
	DeploySucceeded    uint64
	DeployFailed       uint64
	LastSupplyChainRow time.Time
}

func (c *Client) SupplyChainEvidenceSummary(ctx context.Context, runKey string) (SupplyChainEvidenceSummary, error) {
	if c == nil {
		return SupplyChainEvidenceSummary{}, errors.New("deploydb: client is nil")
	}
	if runKey == "" {
		return SupplyChainEvidenceSummary{}, errors.New("deploydb: deploy run key is required")
	}
	ctx, span := c.tracer.Start(ctx, "verself_deploy.supply_chain.evidence_assertion_query",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "clickhouse"),
			attribute.String("verself.deploy_run_key", runKey),
		),
	)
	defer span.End()

	summary := SupplyChainEvidenceSummary{DeployRunKey: runKey}
	runKeyArg := ch.Named("run_key", runKey)
	// ClickHouse substitutes SELECT aliases inside aggregate expressions, so the trace sample alias must stay distinct.
	if err := c.conn.QueryRow(ctx, `
SELECT
  count() AS row_count,
  countIf(policy_result = 'rejected') AS rejected,
  countIf(policy_result = 'accepted') AS accepted,
  countIf(policy_result = 'provisional') AS provisional,
  countIf(trace_id = '') AS empty_trace_id,
  countDistinctIf(trace_id, trace_id != '') AS distinct_trace_id,
  anyIf(trace_id, trace_id != '') AS non_empty_trace_id,
  max(event_at) AS last_supply_chain_row
FROM verself.supply_chain_policy_events
WHERE deploy_run_key = {run_key:String}
`, runKeyArg).Scan(
		&summary.RowCount,
		&summary.Rejected,
		&summary.Accepted,
		&summary.Provisional,
		&summary.EmptyTraceID,
		&summary.DistinctTraceID,
		&summary.TraceID,
		&summary.LastSupplyChainRow,
	); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return SupplyChainEvidenceSummary{}, fmt.Errorf("deploydb: query supply-chain policy evidence: %w", err)
	}

	if err := c.conn.QueryRow(ctx, `
SELECT
  countIf(SpanName = 'verself_deploy.supply_chain.policy_check' AND StatusCode = 'Ok') AS policy_check_spans,
  countIf(SpanName = 'verself_deploy.supply_chain.policy_record' AND StatusCode = 'Ok') AS policy_record_spans
FROM default.otel_traces
WHERE ServiceName = 'verself-deploy'
  AND SpanAttributes['verself.deploy_run_key'] = {run_key:String}
`, runKeyArg).Scan(
		&summary.PolicyCheckSpans,
		&summary.PolicyRecordSpans,
	); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return SupplyChainEvidenceSummary{}, fmt.Errorf("deploydb: query supply-chain policy spans: %w", err)
	}

	if err := c.conn.QueryRow(ctx, `
SELECT
  countIf(event_kind = 'succeeded') AS deploy_succeeded,
  countIf(event_kind = 'failed') AS deploy_failed
FROM verself.deploy_events
WHERE deploy_run_key = {run_key:String}
`, runKeyArg).Scan(
		&summary.DeploySucceeded,
		&summary.DeployFailed,
	); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return SupplyChainEvidenceSummary{}, fmt.Errorf("deploydb: query deploy events: %w", err)
	}

	span.SetAttributes(
		attribute.Int64("supply_chain.row_count", int64(summary.RowCount)),
		attribute.Int64("supply_chain.rejected_count", int64(summary.Rejected)),
		attribute.Int64("supply_chain.provisional_count", int64(summary.Provisional)),
		attribute.Int64("supply_chain.accepted_count", int64(summary.Accepted)),
		attribute.Int64("supply_chain.empty_trace_id_count", int64(summary.EmptyTraceID)),
		attribute.Int64("supply_chain.distinct_trace_id_count", int64(summary.DistinctTraceID)),
		attribute.Int64("supply_chain.policy_check_span_count", int64(summary.PolicyCheckSpans)),
		attribute.Int64("supply_chain.policy_record_span_count", int64(summary.PolicyRecordSpans)),
		attribute.Int64("deploy.succeeded_event_count", int64(summary.DeploySucceeded)),
		attribute.Int64("deploy.failed_event_count", int64(summary.DeployFailed)),
	)
	span.SetStatus(codes.Ok, "")
	return summary, nil
}
