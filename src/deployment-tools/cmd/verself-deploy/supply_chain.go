package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/deploydb"
	"github.com/verself/deployment-tools/internal/identity"
	"github.com/verself/deployment-tools/internal/runtime"
	"github.com/verself/deployment-tools/internal/supplychain"
)

const supplyChainComponent = "supply-chain"

func runSupplyChain(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "verself-deploy supply-chain: missing subcommand (try `inventory`, `check`, `record`, or `assert-evidence`)")
		return 2
	}
	switch args[0] {
	case "inventory":
		return runSupplyChainInventory(args[1:])
	case "check":
		return runSupplyChainCheck(args[1:])
	case "record":
		return runSupplyChainRecord(args[1:])
	case "assert-evidence":
		return runSupplyChainAssertEvidence(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "verself-deploy supply-chain: unknown subcommand: %s\n", args[0])
		return 2
	}
}

func runSupplyChainInventory(args []string) int {
	fs := flag.NewFlagSet("supply-chain inventory", flag.ContinueOnError)
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	format := fs.String("format", "json", "json | policy | catalog")
	writePolicy := fs.String("write-policy", "", "write generated policy JSON to path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rr, ok := resolveRepoRoot("verself-deploy supply-chain inventory", *repoRoot)
	if !ok {
		return 1
	}
	report, err := supplychain.Scan(context.Background(), supplychain.ScanOptions{RepoRoot: rr})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy supply-chain inventory: %v\n", err)
		return 1
	}
	var payload any = report
	switch *format {
	case "policy":
		policy := supplychain.NewPolicyFromReport(report)
		payload = policy
		if *writePolicy != "" {
			path := *writePolicy
			if !filepath.IsAbs(path) {
				path = filepath.Join(rr, path)
			}
			if err := supplychain.WritePolicy(path, policy); err != nil {
				fmt.Fprintf(os.Stderr, "verself-deploy supply-chain inventory: %v\n", err)
				return 1
			}
			fmt.Fprintf(os.Stderr, "wrote %s with %d tracked artifact sources\n", path, len(policy.Artifacts))
			return 0
		}
	case "catalog":
		policy, err := supplychain.LoadPolicy(rr, supplychain.DefaultPolicyPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy supply-chain inventory: %v\n", err)
			return 1
		}
		payload = supplychain.CatalogFromPolicy(policy)
	case "json":
	default:
		fmt.Fprintf(os.Stderr, "verself-deploy supply-chain inventory: unsupported --format=%s\n", *format)
		return 2
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy supply-chain inventory: encode: %v\n", err)
		return 1
	}
	return 0
}

func runSupplyChainCheck(args []string) int {
	fs := flag.NewFlagSet("supply-chain check", flag.ContinueOnError)
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	policyPath := fs.String("policy", supplychain.DefaultPolicyPath, "artifact policy path")
	strictAdmitted := fs.Bool("strict-admitted", false, "fail when tracked artifacts are not fully admitted")
	format := fs.String("format", "text", "text | json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rr, ok := resolveRepoRoot("verself-deploy supply-chain check", *repoRoot)
	if !ok {
		return 1
	}
	report, eval, err := evaluateSupplyChain(context.Background(), rr, *policyPath, *strictAdmitted)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy supply-chain check: %v\n", err)
		return 1
	}
	switch *format {
	case "text":
		printSupplyChainSummary(os.Stdout, report, eval)
	case "json":
		if err := json.NewEncoder(os.Stdout).Encode(eval); err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy supply-chain check: encode: %v\n", err)
			return 1
		}
	default:
		fmt.Fprintf(os.Stderr, "verself-deploy supply-chain check: unsupported --format=%s\n", *format)
		return 2
	}
	if eval.HasRejected() {
		return 1
	}
	if *strictAdmitted && eval.HasProvisional() {
		return 1
	}
	return 0
}

func runSupplyChainRecord(args []string) int {
	fs := flag.NewFlagSet("supply-chain record", flag.ContinueOnError)
	site := fs.String("site", "prod", "deployment site")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	policyPath := fs.String("policy", supplychain.DefaultPolicyPath, "artifact policy path")
	strictAdmitted := fs.Bool("strict-admitted", false, "fail when tracked artifacts are not fully admitted")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rr, ok := resolveRepoRoot("verself-deploy supply-chain record", *repoRoot)
	if !ok {
		return 1
	}
	snap := identity.FromEnv()
	if snap.RunKey() == "" {
		generated, err := identity.Generate(identity.GenerateOptions{
			Site:  *site,
			Scope: "supply-chain",
			Kind:  "supply-chain-record",
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy supply-chain record: derive identity: %v\n", err)
			return 1
		}
		generated.ApplyEnv()
		snap = generated
	}
	rt, err := runtime.Init(context.Background(), runtime.Options{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Site:           *site,
		RepoRoot:       rr,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy supply-chain record: %v\n", err)
		return 1
	}
	defer rt.Close()
	if err := runSupplyChainGate(rt.Ctx, rt, *site, rr, *policyPath, snap.RunKey(), *strictAdmitted); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy supply-chain record: %v\n", err)
		return 1
	}
	return 0
}

func runSupplyChainAssertEvidence(args []string) int {
	fs := flag.NewFlagSet("supply-chain assert-evidence", flag.ContinueOnError)
	site := fs.String("site", "prod", "deployment site")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	policyPath := fs.String("policy", supplychain.DefaultPolicyPath, "artifact policy path")
	runKey := fs.String("run-key", "", "deploy run key to assert")
	requireSucceeded := fs.Bool("require-succeeded", true, "require a succeeded deploy_events row and no failed row")
	wait := fs.Duration("wait", 5*time.Second, "maximum time to wait for ClickHouse evidence")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *runKey == "" {
		fmt.Fprintln(os.Stderr, "verself-deploy supply-chain assert-evidence: --run-key is required")
		return 2
	}
	if *wait <= 0 || *wait > 5*time.Second {
		fmt.Fprintln(os.Stderr, "verself-deploy supply-chain assert-evidence: --wait must be >0 and <=5s")
		return 2
	}
	rr, ok := resolveRepoRoot("verself-deploy supply-chain assert-evidence", *repoRoot)
	if !ok {
		return 1
	}
	_, eval, err := evaluateSupplyChain(context.Background(), rr, *policyPath, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy supply-chain assert-evidence: %v\n", err)
		return 1
	}
	if eval.HasRejected() {
		fmt.Fprintf(os.Stderr, "verself-deploy supply-chain assert-evidence: local policy rejects %d artifact source(s)\n", eval.Rejected)
		return 1
	}
	generated, err := identity.Generate(identity.GenerateOptions{
		Site:  *site,
		Scope: "supply-chain",
		Kind:  "supply-chain-assert-evidence",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy supply-chain assert-evidence: derive identity: %v\n", err)
		return 1
	}
	generated.ApplyEnv()
	rt, err := runtime.Init(context.Background(), runtime.Options{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Site:           *site,
		RepoRoot:       rr,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy supply-chain assert-evidence: %v\n", err)
		return 1
	}
	defer rt.Close()
	summary, err := waitForSupplyChainEvidence(rt.Ctx, rt, *runKey, len(eval.Results), *requireSucceeded, *wait)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy supply-chain assert-evidence: %v\n", err)
		return 1
	}
	fmt.Printf(
		"supply-chain evidence ok: run_key=%s rows=%d rejected=%d provisional=%d accepted=%d policy_check_ok_spans=%d policy_check_error_spans=%d policy_record_spans=%d breakglass_rows=%d breakglass_spans=%d trace_id=%s\n",
		summary.DeployRunKey,
		summary.RowCount,
		summary.Rejected,
		summary.Provisional,
		summary.Accepted,
		summary.PolicyCheckSpans,
		summary.PolicyCheckErrorSpans,
		summary.PolicyRecordSpans,
		summary.BreakglassRows,
		summary.BreakglassSpans,
		summary.TraceID,
	)
	return 0
}

func runSupplyChainGate(ctx context.Context, rt *runtime.Runtime, site, repoRoot, policyPath, runKey string, strictAdmitted bool) error {
	_, eval, err := checkSupplyChainPolicy(ctx, rt, site, repoRoot, policyPath, strictAdmitted)
	if err != nil {
		return err
	}
	return recordSupplyChainEvaluation(ctx, rt, site, runKey, eval)
}

func checkSupplyChainPolicy(ctx context.Context, rt *runtime.Runtime, site, repoRoot, policyPath string, strictAdmitted bool) (supplychain.Report, supplychain.Evaluation, error) {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.supply_chain.policy_check",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("verself.site", site),
			attribute.Bool("supply_chain.strict_admitted", strictAdmitted),
		),
	)
	defer span.End()
	report, eval, err := evaluateSupplyChain(ctx, repoRoot, policyPath, strictAdmitted)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return supplychain.Report{}, supplychain.Evaluation{}, err
	}
	span.SetAttributes(
		attribute.Int("supply_chain.finding_count", len(report.Findings)),
		attribute.Int64("supply_chain.accepted_count", int64(eval.Accepted)),
		attribute.Int64("supply_chain.provisional_count", int64(eval.Provisional)),
		attribute.Int64("supply_chain.rejected_count", int64(eval.Rejected)),
	)
	if eval.HasRejected() {
		err := fmt.Errorf("supply-chain policy rejected %d artifact source(s)", eval.Rejected)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return report, eval, err
	}
	if strictAdmitted && eval.HasProvisional() {
		err := fmt.Errorf("supply-chain policy found %d provisional artifact source(s)", eval.Provisional)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return report, eval, err
	}
	span.SetStatus(codes.Ok, "")
	return report, eval, nil
}

func recordSupplyChainEvaluation(ctx context.Context, rt *runtime.Runtime, site, runKey string, eval supplychain.Evaluation) error {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.supply_chain.policy_record",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("verself.site", site),
			attribute.Int("supply_chain.row_count", len(eval.Results)),
		),
	)
	defer span.End()
	rows := supplyChainPolicyRows(site, runKey, eval, span.SpanContext())
	if err := rt.DeployDB.InsertSupplyChainPolicyEvents(ctx, rows); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

func waitForSupplyChainEvidence(ctx context.Context, rt *runtime.Runtime, runKey string, expectedRows int, requireSucceeded bool, wait time.Duration) (deploydb.SupplyChainEvidenceSummary, error) {
	if rt == nil || rt.DeployDB == nil {
		return deploydb.SupplyChainEvidenceSummary{}, errors.New("runtime ClickHouse client is required")
	}
	deadline := time.Now().Add(wait)
	var lastErr error
	for {
		summary, err := rt.DeployDB.SupplyChainEvidenceSummary(ctx, runKey)
		if err != nil {
			lastErr = err
		} else if err := validateSupplyChainEvidence(summary, expectedRows, requireSucceeded); err != nil {
			lastErr = err
		} else {
			return summary, nil
		}
		if time.Now().After(deadline) {
			return deploydb.SupplyChainEvidenceSummary{}, lastErr
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return deploydb.SupplyChainEvidenceSummary{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func validateSupplyChainEvidence(summary deploydb.SupplyChainEvidenceSummary, expectedRows int, requireSucceeded bool) error {
	var issues []string
	if summary.RowCount != uint64(expectedRows) {
		issues = append(issues, fmt.Sprintf("expected %d supply-chain rows, observed %d", expectedRows, summary.RowCount))
	}
	if summary.EmptyTraceID != 0 {
		issues = append(issues, fmt.Sprintf("expected zero empty trace IDs, observed %d", summary.EmptyTraceID))
	}
	if summary.DistinctTraceID != 1 {
		issues = append(issues, fmt.Sprintf("expected one supply-chain trace ID, observed %d", summary.DistinctTraceID))
	}
	if summary.Rejected == 0 {
		if summary.PolicyCheckSpans == 0 {
			issues = append(issues, "missing OK policy_check span")
		}
		if summary.BreakglassRows != 0 {
			issues = append(issues, fmt.Sprintf("expected zero breakglass rows, observed %d", summary.BreakglassRows))
		}
	} else {
		if summary.PolicyCheckErrorSpans == 0 {
			issues = append(issues, "missing Error policy_check span for fail-closed policy gate")
		}
		if summary.BreakglassRows != 1 {
			issues = append(issues, fmt.Sprintf("expected one breakglass row for rejected policy rows, observed %d", summary.BreakglassRows))
		}
		if summary.BreakglassPolicyRejected != summary.Rejected {
			issues = append(issues, fmt.Sprintf("breakglass rejected count %d did not match rejected policy rows %d", summary.BreakglassPolicyRejected, summary.Rejected))
		}
		if summary.BreakglassSpans == 0 {
			issues = append(issues, "missing OK breakglass.allow span for rejected policy rows")
		}
	}
	if summary.PolicyRecordSpans == 0 {
		issues = append(issues, "missing OK policy_record span")
	}
	if requireSucceeded {
		if summary.DeploySucceeded == 0 {
			issues = append(issues, "missing succeeded deploy_events row")
		}
		if summary.DeployFailed != 0 {
			issues = append(issues, fmt.Sprintf("expected zero failed deploy_events rows, observed %d", summary.DeployFailed))
		}
	}
	if len(issues) > 0 {
		return errors.New(strings.Join(issues, "; "))
	}
	return nil
}

func evaluateSupplyChain(ctx context.Context, repoRoot, policyPath string, strictAdmitted bool) (supplychain.Report, supplychain.Evaluation, error) {
	report, err := supplychain.Scan(ctx, supplychain.ScanOptions{RepoRoot: repoRoot})
	if err != nil {
		return supplychain.Report{}, supplychain.Evaluation{}, err
	}
	policy, err := supplychain.LoadPolicy(repoRoot, policyPath)
	if err != nil {
		return supplychain.Report{}, supplychain.Evaluation{}, err
	}
	eval, err := supplychain.Evaluate(report, policy, strictAdmitted)
	if err != nil {
		return supplychain.Report{}, supplychain.Evaluation{}, err
	}
	return report, eval, nil
}

func supplyChainPolicyRows(site, runKey string, eval supplychain.Evaluation, spanCtx trace.SpanContext) []deploydb.SupplyChainPolicyEventRow {
	rows := make([]deploydb.SupplyChainPolicyEventRow, 0, len(eval.Results))
	now := time.Now().UTC()
	traceID, spanID := "", ""
	if spanCtx.IsValid() {
		traceID = spanCtx.TraceID().String()
		spanID = spanCtx.SpanID().String()
	}
	for _, result := range eval.Results {
		f := result.Finding
		rows = append(rows, deploydb.SupplyChainPolicyEventRow{
			EventAt:               now,
			DeployRunKey:          runKey,
			Site:                  site,
			SourcePath:            f.SourcePath,
			Line:                  f.Line,
			SourceKind:            f.SourceKind,
			Surface:               f.Surface,
			Artifact:              f.Artifact,
			UpstreamURL:           f.UpstreamURL,
			Digest:                f.Digest,
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
			Evidence:              f.Evidence,
		})
	}
	return rows
}

func printSupplyChainSummary(w *os.File, report supplychain.Report, eval supplychain.Evaluation) {
	fmt.Fprintf(w, "supply-chain findings: %d accepted=%d provisional=%d rejected=%d\n", len(report.Findings), eval.Accepted, eval.Provisional, eval.Rejected)
	if eval.Rejected == 0 {
		return
	}
	for _, result := range eval.Results {
		if result.PolicyResult != supplychain.ResultRejected {
			continue
		}
		f := result.Finding
		fmt.Fprintf(w, "REJECTED %s:%d %s %s: %s\n", f.SourcePath, f.Line, f.SourceKind, f.Artifact, result.PolicyReason)
	}
}
