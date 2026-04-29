// bazel-profile-to-otel parses a `bazelisk build --profile=<path>` JSON
// profile and emits one OTel span per build action it contains. The
// spans share the calling shell's OTLP endpoint + OTEL_RESOURCE_ATTRIBUTES
// (set by deploy_identity.sh), so each action is automatically tagged
// with verself.deploy_run_key and joins the surrounding ansible.task
// span on a single deploy timeline.
//
// We use this where ansible's `Build topology Go artifacts with Bazel`
// task hides ~50s of build inside an opaque shell-out span. The profile
// breaks that down per target / mnemonic, so a deploy bottleneck query
// can identify which Go binary (or which action mnemonic) dominates.
//
// Bazel writes the profile in Chrome Trace Event Format (one JSON
// object with `otherData` + `traceEvents`). otherData.profile_start_ts
// is the absolute epoch milliseconds at build start; each event's `ts`
// is microseconds since profile start, `dur` is microseconds.
//
// Filter: phase == "X" AND cat in ACTION_CATEGORIES AND dur >=
// --min-duration-ms. Bazel emits hundreds of micro-events for trivial
// internal phases; the threshold keeps the span volume sane.
//
// Usage:
//
//	bazel-profile-to-otel \
//	  --profile=/path/to/profile.json.gz \
//	  --service-name=bazel \
//	  --min-duration-ms=200
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
// projecting as OTel spans. The ones we drop ("general information",
// "build phase marker", "package creation", "Starlark*", "Conflict
// checking", "Fetching repository", "gc notification", "bazel module
// processing") are either span-shaped duplicates of the phases we
// already keep, sub-millisecond debug bread crumbs, or coarse
// build-spanning events that would double-count against the
// per-action signal.
//
// A real cold-cache deploy commonly spends the bulk of its time in
// `skyframe evaluator` (analysis) rather than `action processing`
// (execution). Keeping both makes "where did the 54s go" answerable
// from one query: ServiceName='bazel' GROUP BY bazel.cat.
var keptCategories = map[string]bool{
	"action processing":          true,
	"complete action execution":  true,
	"critical path component":    true,
	"remote action execution":    true,
	"remote action upload":       true,
	"remote action download":     true,
	"local action execution":     true,
	"subprocess":                 true,
	"include scanning":           true,
	"skyframe evaluator":         true,
	"action dependency checking": true,
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
	minDurationMs := flag.Int64("min-duration-ms", 200, "Skip events shorter than this; small actions add up to noise")
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
	emitted := 0
	for _, e := range prof.TraceEvents {
		if e.Ph != "X" || !keptCategories[e.Cat] {
			continue
		}
		if e.Dur < minDurMicros {
			continue
		}
		emitSpan(ctx, tracer, startBase, e)
		emitted++
	}
	fmt.Fprintf(os.Stderr, "bazel-profile-to-otel: emitted %d spans (min-duration-ms=%d) from %s\n",
		emitted, *minDurationMs, *profilePath)
}

func emitSpan(ctx context.Context, tracer trace.Tracer, base time.Time, e event) {
	start := base.Add(time.Duration(e.Ts) * time.Microsecond)
	end := start.Add(time.Duration(e.Dur) * time.Microsecond)

	mnemonic, _ := e.Args["mnemonic"].(string)
	target, _ := e.Args["target"].(string)

	spanName := "bazel.event"
	switch {
	case mnemonic != "":
		spanName = "bazel.action." + mnemonic
	case e.Cat == "critical path component":
		spanName = "bazel.critical_path"
	case e.Cat == "skyframe evaluator":
		spanName = "bazel.skyframe"
	case e.Cat == "action dependency checking":
		spanName = "bazel.action_dep_check"
	}

	attrs := []attribute.KeyValue{
		attribute.String("bazel.cat", e.Cat),
		attribute.String("bazel.event_name", e.Name),
		attribute.Int64("bazel.duration_ms", e.Dur/1000),
	}
	if mnemonic != "" {
		attrs = append(attrs, attribute.String("bazel.mnemonic", mnemonic))
	}
	if target != "" {
		attrs = append(attrs, attribute.String("bazel.target", target))
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
