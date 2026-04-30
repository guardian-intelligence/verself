// bazel-execlog-to-otel parses a `bazelisk build --execution_log_json_file=<path>`
// JSON stream and emits one OTel span per spawn. The execution log is
// Bazel's authoritative record of "what action ran with what inputs and
// what outputs" — distinct from the Chrome-trace profile, which is a
// timing record that does *not* carry the action's Bazel target label.
//
// The emitter exists because the prior verification surface tried to
// read SpanAttributes['bazel.target'] off profile spans where that
// field was always empty (Bazel's profile JSON does not populate
// `target` in event args; only `mnemonic`). The execution log entries
// each carry `targetLabel`, `mnemonic`, `runner`, `cacheHit`,
// `actualOutputs`, and `metrics.totalTime` — exactly the surface every
// codegen-rebuild question wants to ask.
//
// Each span:
//
//	ServiceName: --service-name (default "bazel")
//	SpanName:    "bazel.spawn.<Mnemonic>" (or "bazel.spawn" if missing)
//	Start/End:   metrics.startTime + metrics.totalTime
//	Status:      Ok if exitCode == 0, Error otherwise
//	Attributes:
//	  bazel.target_label   — the Bazel label that requested this action
//	  bazel.mnemonic       — action mnemonic (OAPICodegen, GoCompilePkg, ...)
//	  bazel.runner         — execution strategy ("linux-sandbox", "remote
//	                         cache hit", "disk cache hit", "local", ...)
//	  bazel.cache_hit      — true iff the action was served from a cache
//	  bazel.exit_code      — process exit code
//	  bazel.duration_ms    — wall-clock execution time (ms)
//	  bazel.output_count   — number of declared outputs
//	  bazel.output_first   — first output path (best for grep/filter)
//
// The standard verselfotel.Init plumbing inherits OTLP endpoint and
// OTEL_RESOURCE_ATTRIBUTES from the calling shell, so spans land in
// default.otel_traces tagged with verself.deploy_run_key when invoked
// inside the deploy_profile play.
//
// Usage:
//
//	bazelisk build \
//	  --execution_log_json_file=/path/to/exec_log.json \
//	  //src/...
//	bazel-execlog-to-otel \
//	  --execution-log=/path/to/exec_log.json \
//	  --service-name=bazel
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	verselfotel "github.com/verself/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// spawnExec is the subset of bazel's SpawnExec proto (rendered to JSON
// via protojson by --execution_log_json_file) that this tool reads.
// Field names follow the proto's protojson convention (camelCase).
type spawnExec struct {
	Mnemonic      string        `json:"mnemonic"`
	TargetLabel   string        `json:"targetLabel"`
	Runner        string        `json:"runner"`
	CacheHit      bool          `json:"cacheHit"`
	ExitCode      int32         `json:"exitCode"`
	Status        string        `json:"status"`
	ActualOutputs []spawnOutput `json:"actualOutputs"`
	Metrics       *spawnMetrics `json:"metrics"`
}

type spawnOutput struct {
	Path string `json:"path"`
}

type spawnMetrics struct {
	// totalTime is a google.protobuf.Duration rendered as a string with
	// trailing 's' (e.g. "0.001s", "12.500s") per protojson conventions.
	TotalTime string `json:"totalTime"`
	// startTime is a google.protobuf.Timestamp rendered as RFC3339 with
	// optional fractional seconds (e.g. "2026-04-29T22:43:57.944Z").
	StartTime string `json:"startTime"`
}

func main() {
	logPath := flag.String("execution-log", "", "Path to bazelisk --execution_log_json_file output (required)")
	serviceName := flag.String("service-name", "bazel", "OTel service.name for emitted spans")
	flag.Parse()

	if *logPath == "" {
		fail("--execution-log is required")
	}

	f, err := os.Open(*logPath)
	if err != nil {
		fail("open execution log: %v", err)
	}
	defer f.Close()

	ctx := context.Background()
	shutdown, _, err := verselfotel.Init(ctx, verselfotel.Config{
		ServiceName:    *serviceName,
		ServiceVersion: "1.0.0",
	})
	if err != nil {
		fail("initialize otel: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "flush otel: %v\n", err)
		}
	}()

	tracer := otel.Tracer(*serviceName)
	dec := json.NewDecoder(f)
	emitted := 0
	for {
		var sp spawnExec
		if err := dec.Decode(&sp); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			fail("decode spawn entry %d: %v", emitted, err)
		}
		if err := emitSpawn(ctx, tracer, sp); err != nil {
			fail("emit spawn %q (%s): %v", sp.TargetLabel, sp.Mnemonic, err)
		}
		emitted++
	}
	fmt.Fprintf(os.Stderr, "bazel-execlog-to-otel: emitted %d spawn spans from %s\n", emitted, *logPath)
}

func emitSpawn(ctx context.Context, tracer trace.Tracer, sp spawnExec) error {
	start, err := parseStartTime(sp.Metrics)
	if err != nil {
		return fmt.Errorf("startTime: %w", err)
	}
	durMs, err := parseDurationMs(sp.Metrics)
	if err != nil {
		return fmt.Errorf("totalTime: %w", err)
	}
	end := start.Add(time.Duration(durMs) * time.Millisecond)

	spanName := "bazel.spawn"
	if sp.Mnemonic != "" {
		spanName = "bazel.spawn." + sp.Mnemonic
	}

	attrs := []attribute.KeyValue{
		attribute.String("bazel.target_label", sp.TargetLabel),
		attribute.String("bazel.mnemonic", sp.Mnemonic),
		attribute.String("bazel.runner", sp.Runner),
		attribute.Bool("bazel.cache_hit", sp.CacheHit),
		attribute.Int64("bazel.exit_code", int64(sp.ExitCode)),
		attribute.Int64("bazel.duration_ms", durMs),
		attribute.Int("bazel.output_count", len(sp.ActualOutputs)),
	}
	if len(sp.ActualOutputs) > 0 {
		attrs = append(attrs, attribute.String("bazel.output_first", sp.ActualOutputs[0].Path))
	}

	_, span := tracer.Start(ctx, spanName, trace.WithTimestamp(start))
	span.SetAttributes(attrs...)
	if sp.ExitCode != 0 {
		msg := sp.Status
		if msg == "" {
			msg = fmt.Sprintf("exit %d", sp.ExitCode)
		}
		span.SetStatus(codes.Error, msg)
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End(trace.WithTimestamp(end))
	return nil
}

// parseStartTime reads metrics.startTime, an RFC3339 timestamp written
// by protojson. Bazel emits with trailing 'Z' and optional fractional
// seconds; time.RFC3339Nano handles both.
func parseStartTime(m *spawnMetrics) (time.Time, error) {
	if m == nil || m.StartTime == "" {
		return time.Time{}, errors.New("missing")
	}
	return time.Parse(time.RFC3339Nano, m.StartTime)
}

// parseDurationMs reads metrics.totalTime, a google.protobuf.Duration
// rendered by protojson as a decimal-seconds string with trailing 's'
// (e.g. "0.001s", "12.500s", "0s"). Returns 0ms when the field is
// absent — Bazel records '0s' for cache hits where no spawn ran, and
// the span still wants a valid (zero-length) interval.
func parseDurationMs(m *spawnMetrics) (int64, error) {
	if m == nil || m.TotalTime == "" {
		return 0, nil
	}
	raw := strings.TrimSuffix(m.TotalTime, "s")
	if raw == "" {
		return 0, fmt.Errorf("malformed totalTime %q", m.TotalTime)
	}
	secs, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %q: %w", m.TotalTime, err)
	}
	return int64(secs * 1000), nil
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "bazel-execlog-to-otel: "+format+"\n", args...)
	os.Exit(1)
}
