// bazel-profile-to-otel parses a `bazelisk build --profile=<path>` JSON
// profile and emits one OTel span per kept event. The spans share the
// calling shell's OTLP endpoint + OTEL_RESOURCE_ATTRIBUTES (set by
// deploy_identity.sh), so each event is automatically tagged with
// verself.deploy_run_key and joins the surrounding ansible.task span on
// a single deploy timeline.
//
// Scope: this tool is the *timing* projection of a Bazel build —
// analysis-phase Starlark calls, skyframe evaluation, repository fetches,
// critical-path components. The *execution* projection (per-action
// target, mnemonic, runner, cache hit) lives in
// //src/otel/cmd/bazel-execlog-to-otel; the two pipelines coexist
// because the Bazel profile JSON does not carry the action's Bazel
// label, only its mnemonic and a free-form event name. Anything that
// needs an authoritative target label belongs in the execlog projection.
//
// Bazel writes the profile in Chrome Trace Event Format (one JSON
// object with `otherData` + `traceEvents`). otherData.profile_start_ts
// is the absolute epoch milliseconds at build start; each event's `ts`
// is microseconds since profile start, `dur` is microseconds.
//
// Filter: phase == "X" AND cat in keptCategories. There is no
// duration threshold — sub-millisecond Starlark events are useful for
// `Total time spent in <function>` aggregation queries, and a global
// threshold is the wrong knob for noise reduction (it silently masks
// short codegen actions, which is exactly the regression that motivated
// this rewrite). If profile volume ever becomes a real problem, the
// answer is per-category sampling, not a duration cliff.
//
// Usage:
//
//	bazel-profile-to-otel \
//	  --profile=/path/to/profile.json.gz \
//	  --service-name=bazel
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
// projecting as OTel spans. The dropped ones ("build phase marker",
// "gc notification", "bazel module processing") are sub-millisecond
// debug bread crumbs.
//
// A real cold-cache deploy commonly spends the bulk of its time in
// `skyframe evaluator` (analysis) rather than `action processing`
// (execution). The `skyframe evaluator` umbrella is monolithic — its
// 53s "Parallel Evaluator evaluation" wall is unactionable on its own;
// the children that explain it live in the categories below
// ("Fetching repository", "package creation", "Starlark *", "Conflict
// checking", "general information").
var keptCategories = map[string]bool{
	"action processing":              true,
	"complete action execution":      true,
	"critical path component":        true,
	"remote action execution":        true,
	"remote action upload":           true,
	"remote action download":         true,
	"local action execution":         true,
	"subprocess":                     true,
	"include scanning":               true,
	"skyframe evaluator":             true,
	"action dependency checking":     true,
	"Fetching repository":            true,
	"package creation":               true,
	"Starlark user function call":    true,
	"Starlark builtin function call": true,
	"Starlark thread context":        true,
	"Conflict checking":              true,
	"general information":            true,
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
	emitted := 0
	for _, e := range prof.TraceEvents {
		if e.Ph != "X" || !keptCategories[e.Cat] {
			continue
		}
		emitSpan(ctx, tracer, startBase, e)
		emitted++
	}
	fmt.Fprintf(os.Stderr, "bazel-profile-to-otel: emitted %d spans from %s\n", emitted, *profilePath)
}

func emitSpan(ctx context.Context, tracer trace.Tracer, base time.Time, e event) {
	start := base.Add(time.Duration(e.Ts) * time.Microsecond)
	end := start.Add(time.Duration(e.Dur) * time.Microsecond)

	mnemonic, _ := e.Args["mnemonic"].(string)

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
	case e.Cat == "Fetching repository":
		spanName = "bazel.repo_fetch"
	case e.Cat == "package creation":
		spanName = "bazel.package"
	case e.Cat == "Starlark user function call":
		spanName = "bazel.starlark_user"
	case e.Cat == "Starlark builtin function call":
		spanName = "bazel.starlark_builtin"
	case e.Cat == "Starlark thread context":
		spanName = "bazel.starlark_thread"
	case e.Cat == "Conflict checking":
		spanName = "bazel.conflict_check"
	case e.Cat == "general information":
		spanName = "bazel.info." + sanitizeName(e.Name)
	}

	attrs := []attribute.KeyValue{
		attribute.String("bazel.cat", e.Cat),
		attribute.String("bazel.event_name", e.Name),
		attribute.Int64("bazel.duration_ms", e.Dur/1000),
	}
	if mnemonic != "" {
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

// sanitizeName turns a free-form Bazel event name into a span-name suffix.
// Skyframe and "general information" events use camelCase identifiers like
// "skyframeExecutor.evaluateBuildDriverKeys"; we pass those through but
// strip whitespace and runs of punctuation so the resulting span name
// is stable to GROUP BY in ClickHouse.
func sanitizeName(s string) string {
	if s == "" {
		return "anon"
	}
	r := strings.NewReplacer(" ", "_", "/", "_", "\\", "_")
	return r.Replace(s)
}
