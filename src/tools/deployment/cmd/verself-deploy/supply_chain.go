package main

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/supplychain"
)

func checkSupplyChainPolicy(ctx context.Context, rtTracer trace.Tracer, repoRoot, runKey, site string) error {
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
	recordSupplyChainEvaluation(checkSpan, runKey, site, eval)
	if eval.Rejected > 0 {
		err := fmt.Errorf("supply-chain policy rejected %d source(s)", eval.Rejected)
		checkSpan.RecordError(err)
		checkSpan.SetStatus(codes.Error, err.Error())
		checkSpan.End()
		return err
	}
	checkSpan.SetStatus(codes.Ok, "")
	checkSpan.End()
	return nil
}

func recordSupplyChainEvaluation(span trace.Span, runKey, site string, eval supplychain.Evaluation) {
	span.SetAttributes(
		attribute.Int("supply_chain.finding_count", len(eval.Results)),
		attribute.Int64("supply_chain.accepted_count", int64(eval.Accepted)),
		attribute.Int64("supply_chain.provisional_count", int64(eval.Provisional)),
		attribute.Int64("supply_chain.rejected_count", int64(eval.Rejected)),
	)
	span.AddEvent("verself.supply_chain.policy_evaluated", trace.WithAttributes(
		attribute.String("verself.deploy_run_key", runKey),
		attribute.String("verself.site", site),
		attribute.Int("supply_chain.finding_count", len(eval.Results)),
		attribute.Int64("supply_chain.accepted_count", int64(eval.Accepted)),
		attribute.Int64("supply_chain.provisional_count", int64(eval.Provisional)),
		attribute.Int64("supply_chain.rejected_count", int64(eval.Rejected)),
	))
	for _, result := range eval.Results {
		finding := result.Finding
		span.AddEvent("verself.supply_chain.policy_finding", trace.WithAttributes(
			attribute.String("verself.deploy_run_key", runKey),
			attribute.String("verself.site", site),
			attribute.String("supply_chain.source_path", finding.SourcePath),
			attribute.Int64("supply_chain.line", int64(finding.Line)),
			attribute.String("supply_chain.surface", finding.Surface),
			attribute.String("supply_chain.source_kind", finding.SourceKind),
			attribute.String("supply_chain.artifact", finding.Artifact),
			attribute.String("supply_chain.upstream_url", finding.UpstreamURL),
			attribute.String("supply_chain.install_url", result.InstallURL),
			attribute.String("supply_chain.policy_result", result.PolicyResult),
			attribute.String("supply_chain.admission_state", result.AdmissionState),
			attribute.String("supply_chain.policy_reason", result.PolicyReason),
			attribute.String("supply_chain.digest", finding.Digest),
			attribute.String("supply_chain.tuf_target_path", result.TUFTargetPath),
			attribute.String("supply_chain.storage_uri", result.StorageURI),
		))
	}
}
