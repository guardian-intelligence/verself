// Package canary is the post-deploy ledger sanity check. It asserts
// that every substrate layer produced exactly one deploy_layer_runs
// row for the deploy_run_key, none recorded `failed`, none had a
// zero input_hash (a recording bug), and no task ran `changed`
// inside a layer the deploy chose to skip on hash match.
//
// Replaces scripts/divergence-canary.sh. The SQL contract is
// preserved verbatim; only the dispatch path moves.
package canary

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tooling/internal/chwriter"
)

const tracerName = "github.com/verself/deployment-tooling/internal/canary"

// ExpectedLayers is the closed set the canary asserts ran for the
// deploy. Mirrors helpers.SUBSTRATE_LAYERS — keeping this list in
// sync with the AXL one is intentional: a layer added there must
// also land here so the canary fails loud when the new layer's row
// is missing.
var ExpectedLayers = []string{"l1_os", "l2_userspace", "l3_binaries", "l4a_components"}

// Report summarises the canary's findings. Anomalies is the
// (problem, evidence) list — empty means a clean ledger.
type Report struct {
	RunKey               string
	Site                 string
	RowCount             int
	FailedCount          int
	EmptyHashCount       int
	ChangedInsideSkipped int
	ObservedLayers       []string
	Anomalies            []string
}

// Clean is true when no anomalies were detected.
func (r Report) Clean() bool { return len(r.Anomalies) == 0 }

// DivergenceError is returned when the report is non-clean. The
// caller branches on errors.As to surface the structured anomalies
// list to the AXL exit code.
type DivergenceError struct {
	Report Report
}

func (e *DivergenceError) Error() string {
	return fmt.Sprintf("divergence detected for site=%s deploy_run_key=%s: %s",
		e.Report.Site, e.Report.RunKey, strings.Join(e.Report.Anomalies, "; "))
}

// IsDivergence is the typed branch helper.
func IsDivergence(err error) bool {
	var de *DivergenceError
	return errors.As(err, &de)
}

// CheckDivergence runs the same SQL the bash canary used and returns
// either a clean Report or a *DivergenceError. The canary writes its
// findings to a span attribute set so the diagnosis survives even if
// the AXL caller loses the report struct.
func CheckDivergence(ctx context.Context, ch *chwriter.Writer, site, runKey string) (Report, error) {
	if ch == nil {
		return Report{}, errors.New("canary: nil chwriter")
	}
	if site == "" || runKey == "" {
		return Report{}, errors.New("canary: site and runKey are required")
	}
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "verself_deploy.canary.divergence",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("verself.site", site),
			attribute.String("canary.deploy_run_key", runKey),
		),
	)
	defer span.End()

	query := fmt.Sprintf(`
WITH rows AS (
  SELECT
    layer,
    argMax(event_kind, event_at)    AS event_kind,
    argMax(input_hash, event_at)    AS input_hash,
    argMax(skipped, event_at)       AS skipped,
    argMax(changed_count, event_at) AS changed_count
  FROM verself.deploy_layer_runs
  WHERE site = %s AND deploy_run_key = %s
  GROUP BY layer
)
SELECT
  count()                                AS row_count,
  countIf(event_kind = 'failed')         AS failed_count,
  countIf(input_hash = repeat('0', 64))  AS empty_hash_count,
  arrayStringConcat(groupArray(layer), ',') AS observed_layers,
  sumIf(changed_count, skipped = 1)      AS changed_inside_skipped
FROM rows
FORMAT TSVRaw`, chRenderString(site), chRenderString(runKey))

	out, err := ch.QueryString(ctx, query)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Report{}, fmt.Errorf("canary: query: %w", err)
	}

	report, err := parseTSV(out, runKey, site)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Report{}, err
	}
	annotateAnomalies(&report)
	span.SetAttributes(
		attribute.Int("canary.row_count", report.RowCount),
		attribute.Int("canary.failed_count", report.FailedCount),
		attribute.Int("canary.empty_hash_count", report.EmptyHashCount),
		attribute.Int("canary.changed_inside_skipped", report.ChangedInsideSkipped),
		attribute.StringSlice("canary.observed_layers", report.ObservedLayers),
		attribute.StringSlice("canary.anomalies", report.Anomalies),
	)
	if !report.Clean() {
		err := &DivergenceError{Report: report}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return report, err
	}
	span.SetStatus(codes.Ok, "")
	return report, nil
}

func parseTSV(raw, runKey, site string) (Report, error) {
	line := strings.TrimRight(strings.SplitN(strings.TrimSpace(raw), "\n", 2)[0], "\r")
	cols := strings.Split(line, "\t")
	if len(cols) != 5 {
		return Report{}, fmt.Errorf("canary: expected 5 TSV columns, got %d (%q)", len(cols), line)
	}
	report := Report{RunKey: runKey, Site: site}
	var err error
	report.RowCount, err = parseInt(cols[0])
	if err != nil {
		return report, fmt.Errorf("canary: row_count: %w", err)
	}
	report.FailedCount, err = parseInt(cols[1])
	if err != nil {
		return report, fmt.Errorf("canary: failed_count: %w", err)
	}
	report.EmptyHashCount, err = parseInt(cols[2])
	if err != nil {
		return report, fmt.Errorf("canary: empty_hash_count: %w", err)
	}
	if cols[3] != "" {
		report.ObservedLayers = strings.Split(cols[3], ",")
	}
	report.ChangedInsideSkipped, err = parseInt(cols[4])
	if err != nil {
		return report, fmt.Errorf("canary: changed_inside_skipped: %w", err)
	}
	return report, nil
}

func annotateAnomalies(r *Report) {
	if r.RowCount != len(ExpectedLayers) {
		r.Anomalies = append(r.Anomalies,
			fmt.Sprintf("expected %d layer rows; observed %d (%s)",
				len(ExpectedLayers), r.RowCount, strings.Join(r.ObservedLayers, ",")))
	}
	if r.FailedCount > 0 {
		r.Anomalies = append(r.Anomalies, fmt.Sprintf("%d layer(s) recorded event_kind=failed", r.FailedCount))
	}
	if r.EmptyHashCount > 0 {
		r.Anomalies = append(r.Anomalies, fmt.Sprintf("%d layer row(s) had a zero input_hash (recording bug)", r.EmptyHashCount))
	}
	if r.ChangedInsideSkipped > 0 {
		r.Anomalies = append(r.Anomalies, fmt.Sprintf("%d task(s) ran changed inside a skipped layer (drift)", r.ChangedInsideSkipped))
	}
}

func parseInt(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.Atoi(strings.TrimSpace(s))
}

// chRenderString matches chwriter.String — duplicated here so canary
// doesn't depend on chwriter's internal escaping helper.
func chRenderString(s string) string {
	out := make([]byte, 0, len(s)+2)
	out = append(out, '\'')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' || c == '\'' {
			out = append(out, '\\')
		}
		out = append(out, c)
	}
	out = append(out, '\'')
	return string(out)
}
