package main

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/deploydb"
	"github.com/verself/deployment-tools/internal/supplychain"
)

func checkSupplyChainPolicy(ctx context.Context, rtTracer trace.Tracer, db *deploydb.Client, repoRoot, runKey, site string) error {
	checkCtx, checkSpan := rtTracer.Start(ctx, "verself_deploy.supply_chain.policy_check",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("verself.deploy_run_key", runKey),
			attribute.String("verself.site", site),
		),
	)
	report, err := supplychain.Scan(checkCtx, supplychain.ScanOptions{RepoRoot: repoRoot})
	if err != nil {
		checkSpan.RecordError(err)
		checkSpan.SetStatus(codes.Error, err.Error())
		checkSpan.End()
		return fmt.Errorf("scan supply-chain sources: %w", err)
	}
	policy, err := supplychain.LoadPolicy(repoRoot, "")
	if err != nil {
		checkSpan.RecordError(err)
		checkSpan.SetStatus(codes.Error, err.Error())
		checkSpan.End()
		return fmt.Errorf("load supply-chain policy: %w", err)
	}
	policy, err = supplychain.RefreshGeneratedPolicyIfStale(repoRoot, "", policy, report)
	if err != nil {
		checkSpan.RecordError(err)
		checkSpan.SetStatus(codes.Error, err.Error())
		checkSpan.End()
		return fmt.Errorf("refresh supply-chain policy: %w", err)
	}
	eval, err := supplychain.Evaluate(report, policy, false)
	if err != nil {
		checkSpan.RecordError(err)
		checkSpan.SetStatus(codes.Error, err.Error())
		checkSpan.End()
		return fmt.Errorf("evaluate supply-chain policy: %w", err)
	}
	checkSpan.SetAttributes(
		attribute.Int("supply_chain.finding_count", len(report.Findings)),
		attribute.Int64("supply_chain.accepted_count", int64(eval.Accepted)),
		attribute.Int64("supply_chain.provisional_count", int64(eval.Provisional)),
		attribute.Int64("supply_chain.rejected_count", int64(eval.Rejected)),
	)
	if eval.Rejected > 0 {
		err := fmt.Errorf("supply-chain policy rejected %d source(s)", eval.Rejected)
		checkSpan.RecordError(err)
		checkSpan.SetStatus(codes.Error, err.Error())
		checkSpan.End()
		return err
	}
	checkSpan.SetStatus(codes.Ok, "")
	checkSpan.End()

	recordCtx, recordSpan := rtTracer.Start(ctx, "verself_deploy.supply_chain.policy_record",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "clickhouse"),
			attribute.String("verself.deploy_run_key", runKey),
			attribute.String("verself.site", site),
			attribute.Int("supply_chain.row_count", len(eval.Results)),
		),
	)
	defer recordSpan.End()
	rows := supplyChainPolicyEventRows(eval.Results, runKey, site, recordSpan.SpanContext(), time.Now())
	if err := db.InsertSupplyChainPolicyEvents(recordCtx, rows); err != nil {
		recordSpan.RecordError(err)
		recordSpan.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("record supply-chain policy evidence: %w", err)
	}
	recordSpan.SetStatus(codes.Ok, "")
	return nil
}

func supplyChainPolicyEventRows(results []supplychain.FindingResult, runKey, site string, spanContext trace.SpanContext, eventAt time.Time) []deploydb.SupplyChainPolicyEventRow {
	rows := make([]deploydb.SupplyChainPolicyEventRow, 0, len(results))
	traceID := spanContext.TraceID().String()
	spanID := spanContext.SpanID().String()
	for _, result := range results {
		finding := result.Finding
		rows = append(rows, deploydb.SupplyChainPolicyEventRow{
			EventAt:               eventAt,
			DeployRunKey:          runKey,
			Site:                  site,
			SourcePath:            finding.SourcePath,
			Line:                  finding.Line,
			SourceKind:            finding.SourceKind,
			Surface:               finding.Surface,
			Artifact:              finding.Artifact,
			UpstreamURL:           finding.UpstreamURL,
			Digest:                finding.Digest,
			PolicyResult:          result.PolicyResult,
			PolicyReason:          result.PolicyReason,
			AdmissionState:        result.AdmissionState,
			MinimumAgeResult:      result.MinimumAgeResult,
			ScannerResults:        result.ScannerResults,
			OCIRepository:         result.OCIRepository,
			OCIManifestDigest:     result.OCIManifestDigest,
			OCIMediaType:          result.OCIMediaType,
			SignatureDigest:       result.SignatureDigest,
			AttestationDigest:     result.AttestationDigest,
			SBOMDigest:            result.SBOMDigest,
			ProvenanceDigest:      result.ProvenanceDigest,
			ScannerResultDigest:   result.ScannerResultDigest,
			ScannerName:           result.ScannerName,
			ScannerVersion:        result.ScannerVersion,
			ScannerDatabaseDigest: result.ScannerDatabaseDigest,
			GUACSubject:           result.GUACSubject,
			TUFTargetPath:         result.TUFTargetPath,
			StorageURI:            result.StorageURI,
			TraceID:               traceID,
			SpanID:                spanID,
			Evidence:              finding.Evidence,
		})
	}
	return rows
}
