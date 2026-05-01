// bazel-profile-to-otel parses a `bazelisk build --profile=<path>` JSON
// profile and emits one OTel span per kept event. The spans share the
// calling shell's OTLP endpoint + OTEL_RESOURCE_ATTRIBUTES (set by
// `verself-deploy run` via internal/identity.Generate), so each event
// is automatically tagged with verself.deploy_run_key and joins the
// surrounding ansible.task span on a single deploy timeline.
//
// Scope: this tool is the *analysis-phase timing* projection of a
// Bazel build — Starlark calls, skyframe evaluation, repository
// fetches, package loading, conflict checking, critical-path
// components. Per-action timing and presence are NOT in scope here:
// they live in //src/otel/cmd/bazel-execlog-to-otel, which reads
// Bazel's authoritative execution log and emits one span per spawn
// with target_label, runner, cache_hit, and duration. The two
// pipelines coexist because the profile JSON does not carry the
// action's Bazel label.
//
// Bazel writes the profile in Chrome Trace Event Format (one JSON
// object with `otherData` + `traceEvents`). otherData.profile_start_ts
// is the absolute epoch milliseconds at build start; each event's `ts`
// is microseconds since profile start, `dur` is microseconds.
//
// Filter: phase == "X" AND cat in keptCategories AND dur >= floor.
// keptCategories deliberately excludes every per-action category
// (action processing, complete action execution, local/remote action
// execution, subprocess, include scanning, action dependency
// checking) because the execlog projection covers those
// authoritatively. The "general information" category is also
// excluded — it is sub-millisecond Bazel-internal bookkeeping
// (checkOutputs, ParallelEvaluator.eval) that adds thousands of
// no-signal spans per noop deploy. The 50ms floor cuts the long
// tail of micro-events the kept categories still produce; a code
// hot-path that takes <50ms is not interesting timing data, and the
// "did codegen run" question is now owned by the execlog.
//
// Usage:
//
//	bazel-profile-to-otel \
//	  --profile=/path/to/profile.json.gz \
//	  --service-name=bazel \
//	  --min-duration-ms=50
package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	verselfotel "github.com/verself/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// keptCategories is the set of Bazel profile event categories worth
// projecting as OTel spans. Every per-action category is omitted on
// purpose — the execlog projection (//src/otel/cmd/bazel-execlog-to-otel)
// owns "what action ran with what target/runner/cache_hit", and a
// duplicate timing-only span here adds nothing.
//
// A cold-cache deploy commonly spends the bulk of its time in
// `skyframe evaluator` (analysis) rather than action execution. The
// `skyframe evaluator` umbrella is monolithic — its multi-second
// "Parallel Evaluator evaluation" wall is unactionable on its own;
// the children that explain it live in the other kept categories
// ("Fetching repository", "package creation", "Starlark *", "Conflict
// checking", "critical path component").
var keptCategories = map[string]bool{
	"critical path component":        true,
	"skyframe evaluator":             true,
	"Fetching repository":            true,
	"package creation":               true,
	"Starlark user function call":    true,
	"Starlark builtin function call": true,
	"Starlark thread context":        true,
	"Conflict checking":              true,
}

type profile struct {
	OtherData struct {
		// profile_start_ts is the build start time in *milliseconds*
		// since epoch (Bazel writes Java's System.currentTimeMillis()).
		ProfileStartTs int64 `json:"profile_start_ts"`
	} `json:"otherData"`
	TraceEvents []event `json:"traceEvents"`
}

type event struct {
	Cat  string                 `json:"cat"`
	Name string                 `json:"name"`
	Ph   string                 `json:"ph"`
	Ts   int64                  `json:"ts"`
	Dur  int64                  `json:"dur"`
	Args map[string]interface{} `json:"args"`
}

func main() {
	profilePath := flag.String("profile", "", "Path to Bazel profile JSON (gz or plain)")
	serviceName := flag.String("service-name", "bazel", "OTel service.name for emitted spans")
	minDurationMs := flag.Int64("min-duration-ms", 50, "Skip kept-category events shorter than this (ms); see package doc for why action presence is not at risk")
	flag.Parse()

	if *profilePath == "" {
		fail("--profile is required")
	}

	prof, err := readProfile(*profilePath)
	if err != nil {
		fail("read profile: %v", err)
	}
	if prof.OtherData.ProfileStartTs == 0 {
		fail("profile %s has no otherData.profile_start_ts; not a Bazel profile?", *profilePath)
	}
	startBase := time.UnixMilli(prof.OtherData.ProfileStartTs)

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
	minDurMicros := *minDurationMs * 1000
	emitted, droppedShort, droppedCategory := 0, 0, 0
	for _, e := range prof.TraceEvents {
		if e.Ph != "X" {
			continue
		}
		if !keptCategories[e.Cat] {
			droppedCategory++
			continue
		}
		if e.Dur < minDurMicros {
			droppedShort++
			continue
		}
		emitSpan(ctx, tracer, startBase, e)
		emitted++
	}
	fmt.Fprintf(os.Stderr,
		"bazel-profile-to-otel: emitted %d spans (min-duration-ms=%d); dropped %d below threshold, %d outside kept categories; from %s\n",
		emitted, *minDurationMs, droppedShort, droppedCategory, *profilePath)
}

func emitSpan(ctx context.Context, tracer trace.Tracer, base time.Time, e event) {
	start := base.Add(time.Duration(e.Ts) * time.Microsecond)
	end := start.Add(time.Duration(e.Dur) * time.Microsecond)

	spanName := "bazel.event"
	switch e.Cat {
	case "critical path component":
		spanName = "bazel.critical_path"
	case "skyframe evaluator":
		spanName = "bazel.skyframe"
	case "Fetching repository":
		spanName = "bazel.repo_fetch"
	case "package creation":
		spanName = "bazel.package"
	case "Starlark user function call":
		spanName = "bazel.starlark_user"
	case "Starlark builtin function call":
		spanName = "bazel.starlark_builtin"
	case "Starlark thread context":
		spanName = "bazel.starlark_thread"
	case "Conflict checking":
		spanName = "bazel.conflict_check"
	}

	attrs := []attribute.KeyValue{
		attribute.String("bazel.cat", e.Cat),
		attribute.String("bazel.event_name", e.Name),
		attribute.Int64("bazel.duration_ms", e.Dur/1000),
	}
	if mnemonic, ok := e.Args["mnemonic"].(string); ok && mnemonic != "" {
		attrs = append(attrs, attribute.String("bazel.mnemonic", mnemonic))
	}

	_, span := tracer.Start(ctx, spanName, trace.WithTimestamp(start))
	span.SetAttributes(attrs...)
	span.SetStatus(codes.Ok, "")
	span.End(trace.WithTimestamp(end))
}

func readProfile(path string) (*profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("gunzip: %w", err)
		}
		defer gz.Close()
		r = gz
	}

	var p profile
	if err := json.NewDecoder(r).Decode(&p); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &p, nil
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "bazel-profile-to-otel: "+format+"\n", args...)
	os.Exit(1)
}

