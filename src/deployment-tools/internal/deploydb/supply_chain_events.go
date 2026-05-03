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
	EventAt               time.Time `ch:"event_at"`
	DeployRunKey          string    `ch:"deploy_run_key"`
	Site                  string    `ch:"site"`
	SourcePath            string    `ch:"source_path"`
	Line                  uint32    `ch:"line"`
	SourceKind            string    `ch:"source_kind"`
	Surface               string    `ch:"surface"`
	Artifact              string    `ch:"artifact"`
	UpstreamURL           string    `ch:"upstream_url"`
	Digest                string    `ch:"digest"`
	PolicyResult          string    `ch:"policy_result"`
	PolicyReason          string    `ch:"policy_reason"`
	AdmissionState        string    `ch:"admission_state"`
	MinimumAgeResult      string    `ch:"minimum_age_result"`
	ScannerResults        string    `ch:"scanner_results"`
	OCIRepository         string    `ch:"oci_repository"`
	OCIManifestDigest     string    `ch:"oci_manifest_digest"`
	OCIMediaType          string    `ch:"oci_media_type"`
	SignatureDigest       string    `ch:"signature_digest"`
	AttestationDigest     string    `ch:"attestation_digest"`
	SBOMDigest            string    `ch:"sbom_digest"`
	ProvenanceDigest      string    `ch:"provenance_digest"`
	ScannerResultDigest   string    `ch:"scanner_result_digest"`
	ScannerName           string    `ch:"scanner_name"`
	ScannerVersion        string    `ch:"scanner_version"`
	ScannerDatabaseDigest string    `ch:"scanner_database_digest"`
	GUACSubject           string    `ch:"guac_subject"`
	TUFTargetPath         string    `ch:"tuf_target_path"`
	StorageURI            string    `ch:"storage_uri"`
	TraceID               string    `ch:"trace_id"`
	SpanID                string    `ch:"span_id"`
	Evidence              string    `ch:"evidence"`
}

func (c *Client) InsertSupplyChainPolicyEvents(ctx context.Context, rows []SupplyChainPolicyEventRow) error {
	return insertStructs(ctx, c, supplyChainPolicyEventsTable, rows)
}

const artifactAdmissionEventsTable = "verself.artifact_admission_events"

type ArtifactAdmissionEventRow struct {
	EventAt               time.Time `ch:"event_at"`
	DeployRunKey          string    `ch:"deploy_run_key"`
	Site                  string    `ch:"site"`
	Artifact              string    `ch:"artifact"`
	SourcePath            string    `ch:"source_path"`
	SourceKind            string    `ch:"source_kind"`
	UpstreamURL           string    `ch:"upstream_url"`
	UpstreamDigest        string    `ch:"upstream_digest"`
	ReleasedAt            time.Time `ch:"released_at"`
	ObservedAt            time.Time `ch:"observed_at"`
	MinimumAgeResult      string    `ch:"minimum_age_result"`
	OCIRepository         string    `ch:"oci_repository"`
	OCIManifestDigest     string    `ch:"oci_manifest_digest"`
	OCIMediaType          string    `ch:"oci_media_type"`
	SignatureDigest       string    `ch:"signature_digest"`
	AttestationDigest     string    `ch:"attestation_digest"`
	SBOMDigest            string    `ch:"sbom_digest"`
	ProvenanceDigest      string    `ch:"provenance_digest"`
	ScannerResultDigest   string    `ch:"scanner_result_digest"`
	ScannerName           string    `ch:"scanner_name"`
	ScannerVersion        string    `ch:"scanner_version"`
	ScannerDatabaseDigest string    `ch:"scanner_database_digest"`
	TUFTargetPath         string    `ch:"tuf_target_path"`
	StorageURI            string    `ch:"storage_uri"`
	PolicyResult          string    `ch:"policy_result"`
	PolicyReason          string    `ch:"policy_reason"`
	GUACSubject           string    `ch:"guac_subject"`
	TraceID               string    `ch:"trace_id"`
	SpanID                string    `ch:"span_id"`
	Evidence              string    `ch:"evidence"`
}

func (c *Client) InsertArtifactAdmissionEvents(ctx context.Context, rows []ArtifactAdmissionEventRow) error {
	return insertStructs(ctx, c, artifactAdmissionEventsTable, rows)
}

const artifactInstallVerificationEventsTable = "verself.artifact_install_verification_events"

type ArtifactInstallVerificationEventRow struct {
	EventAt           time.Time `ch:"event_at"`
	DeployRunKey      string    `ch:"deploy_run_key"`
	Site              string    `ch:"site"`
	Surface           string    `ch:"surface"`
	Installer         string    `ch:"installer"`
	Artifact          string    `ch:"artifact"`
	OCIReference      string    `ch:"oci_reference"`
	OCIRepository     string    `ch:"oci_repository"`
	OCIManifestDigest string    `ch:"oci_manifest_digest"`
	SignatureDigest   string    `ch:"signature_digest"`
	AttestationDigest string    `ch:"attestation_digest"`
	PolicyResult      string    `ch:"policy_result"`
	PolicyReason      string    `ch:"policy_reason"`
	TraceID           string    `ch:"trace_id"`
	SpanID            string    `ch:"span_id"`
	Evidence          string    `ch:"evidence"`
}

func (c *Client) InsertArtifactInstallVerificationEvents(ctx context.Context, rows []ArtifactInstallVerificationEventRow) error {
	return insertStructs(ctx, c, artifactInstallVerificationEventsTable, rows)
}

type ArtifactEvidenceSummary struct {
	DeployRunKey       string
	AdmissionRows      uint64
	InstallRows        uint64
	RejectedAdmissions uint64
	EmptyTraceID       uint64
	DistinctTraceID    uint64
	TraceID            string
	AdmissionSpans     uint64
	InstallSpans       uint64
}

func (c *Client) ArtifactEvidenceSummary(ctx context.Context, runKey string) (ArtifactEvidenceSummary, error) {
	if c == nil {
		return ArtifactEvidenceSummary{}, errors.New("deploydb: client is nil")
	}
	if runKey == "" {
		return ArtifactEvidenceSummary{}, errors.New("deploydb: deploy run key is required")
	}
	ctx, span := c.tracer.Start(ctx, "verself_deploy.artifacts.evidence_assertion_query",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "clickhouse"),
			attribute.String("verself.deploy_run_key", runKey),
		),
	)
	defer span.End()

	summary := ArtifactEvidenceSummary{DeployRunKey: runKey}
	runKeyArg := ch.Named("run_key", runKey)
	if err := c.conn.QueryRow(ctx, `
SELECT
  count() AS admission_rows,
  countIf(policy_result = 'rejected') AS rejected_admissions,
  countIf(trace_id = '') AS empty_trace_id,
  countDistinctIf(trace_id, trace_id != '') AS distinct_trace_id,
  anyIf(trace_id, trace_id != '') AS non_empty_trace_id
FROM verself.artifact_admission_events
WHERE deploy_run_key = {run_key:String}
`, runKeyArg).Scan(
		&summary.AdmissionRows,
		&summary.RejectedAdmissions,
		&summary.EmptyTraceID,
		&summary.DistinctTraceID,
		&summary.TraceID,
	); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return ArtifactEvidenceSummary{}, fmt.Errorf("deploydb: query artifact admission evidence: %w", err)
	}

	if err := c.conn.QueryRow(ctx, `
SELECT
  count() AS install_rows
FROM verself.artifact_install_verification_events
WHERE deploy_run_key = {run_key:String}
`, runKeyArg).Scan(&summary.InstallRows); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return ArtifactEvidenceSummary{}, fmt.Errorf("deploydb: query artifact install evidence: %w", err)
	}

	if err := c.conn.QueryRow(ctx, `
SELECT
  countIf(SpanName = 'verself_deploy.artifacts.admit' AND StatusCode = 'Ok') AS admission_spans,
  countIf(SpanName = 'verself_deploy.artifacts.install_verify' AND StatusCode = 'Ok') AS install_spans
FROM default.otel_traces
WHERE ServiceName = 'verself-deploy'
  AND SpanAttributes['verself.deploy_run_key'] = {run_key:String}
`, runKeyArg).Scan(
		&summary.AdmissionSpans,
		&summary.InstallSpans,
	); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return ArtifactEvidenceSummary{}, fmt.Errorf("deploydb: query artifact spans: %w", err)
	}

	span.SetAttributes(
		attribute.Int64("artifact.admission_row_count", int64(summary.AdmissionRows)),
		attribute.Int64("artifact.install_row_count", int64(summary.InstallRows)),
		attribute.Int64("artifact.rejected_admission_count", int64(summary.RejectedAdmissions)),
		attribute.Int64("artifact.empty_trace_id_count", int64(summary.EmptyTraceID)),
		attribute.Int64("artifact.distinct_trace_id_count", int64(summary.DistinctTraceID)),
		attribute.Int64("artifact.admission_span_count", int64(summary.AdmissionSpans)),
		attribute.Int64("artifact.install_span_count", int64(summary.InstallSpans)),
	)
	span.SetStatus(codes.Ok, "")
	return summary, nil
}

type SupplyChainEvidenceSummary struct {
	DeployRunKey                string
	RowCount                    uint64
	Rejected                    uint64
	Accepted                    uint64
	Provisional                 uint64
	EmptyTraceID                uint64
	DistinctTraceID             uint64
	TraceID                     string
	PolicyCheckSpans            uint64
	PolicyCheckErrorSpans       uint64
	PolicyRecordSpans           uint64
	BreakglassRows              uint64
	BreakglassPolicyRejected    uint64
	BreakglassPolicyProvisional uint64
	BreakglassSpans             uint64
	DeploySucceeded             uint64
	DeployFailed                uint64
	LastSupplyChainRow          time.Time
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
  countIf(SpanName = 'verself_deploy.supply_chain.policy_check' AND StatusCode = 'Error') AS policy_check_error_spans,
  countIf(SpanName = 'verself_deploy.supply_chain.policy_record' AND StatusCode = 'Ok') AS policy_record_spans,
  countIf(SpanName = 'verself_deploy.breakglass.allow' AND StatusCode = 'Ok') AS breakglass_spans
FROM default.otel_traces
WHERE ServiceName = 'verself-deploy'
  AND SpanAttributes['verself.deploy_run_key'] = {run_key:String}
`, runKeyArg).Scan(
		&summary.PolicyCheckSpans,
		&summary.PolicyCheckErrorSpans,
		&summary.PolicyRecordSpans,
		&summary.BreakglassSpans,
	); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return SupplyChainEvidenceSummary{}, fmt.Errorf("deploydb: query supply-chain policy spans: %w", err)
	}

	if err := c.conn.QueryRow(ctx, `
SELECT
  count() AS breakglass_rows,
  sum(toUInt64(policy_rejected)) AS breakglass_policy_rejected,
  sum(toUInt64(policy_provisional)) AS breakglass_policy_provisional
FROM verself.breakglass_events
WHERE deploy_run_key = {run_key:String}
`, runKeyArg).Scan(
		&summary.BreakglassRows,
		&summary.BreakglassPolicyRejected,
		&summary.BreakglassPolicyProvisional,
	); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return SupplyChainEvidenceSummary{}, fmt.Errorf("deploydb: query breakglass evidence: %w", err)
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
		attribute.Int64("supply_chain.policy_check_error_span_count", int64(summary.PolicyCheckErrorSpans)),
		attribute.Int64("supply_chain.policy_record_span_count", int64(summary.PolicyRecordSpans)),
		attribute.Int64("breakglass.row_count", int64(summary.BreakglassRows)),
		attribute.Int64("breakglass.policy_rejected_count", int64(summary.BreakglassPolicyRejected)),
		attribute.Int64("breakglass.policy_provisional_count", int64(summary.BreakglassPolicyProvisional)),
		attribute.Int64("breakglass.span_count", int64(summary.BreakglassSpans)),
		attribute.Int64("deploy.succeeded_event_count", int64(summary.DeploySucceeded)),
		attribute.Int64("deploy.failed_event_count", int64(summary.DeployFailed)),
	)
	span.SetStatus(codes.Ok, "")
	return summary, nil
}
