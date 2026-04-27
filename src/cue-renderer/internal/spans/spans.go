// Package spans emits OTel spans that wrap the codegen pipeline. The
// pipeline is one trace; per-renderer events get attached to it as child
// spans rather than living as standalone smoke-test artifacts. Lives outside internal/
// render/ because tracing is cross-cutting — it's not a thing the codegen
// produces, it's how we observe the production.
//
// Span names match the existing topology observability contract
// (topology.cue.export_graph, topology.generated.render_artifact, ...).
package spans

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	verselfotel "github.com/verself/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const serviceName = "cue-renderer"

// Pipeline runs `fn` inside a single root span named "cue_renderer.run"
// with the deploy-correlation attributes the controller's environment
// carries. The fn receives a context that carries the active trace, and
// records child spans against it via Record (or directly via OTel).
//
// All errors propagate; if span flush fails after fn returns, the flush
// error is logged but does not mask fn's own error.
func Pipeline(ctx context.Context, topologyDir, instance string, fn func(context.Context, *Recorder) error) (err error) {
	if strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")) == "" {
		otel.SetTracerProvider(noop.NewTracerProvider())
	} else {
		shutdown, _, initErr := verselfotel.Init(ctx, verselfotel.Config{
			ServiceName:    serviceName,
			ServiceVersion: "1.0.0",
		})
		if initErr != nil {
			return fmt.Errorf("init otel: %w", initErr)
		}
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if shutdownErr := shutdown(shutdownCtx); shutdownErr != nil && err == nil {
				err = fmt.Errorf("flush otel: %w", shutdownErr)
			}
		}()
	}

	tracer := otel.Tracer(serviceName)
	base := baseAttributes(topologyDir, instance)
	ctx, root := tracer.Start(ctx, "cue_renderer.run", trace.WithAttributes(base...))
	defer root.End()

	rec := &Recorder{tracer: tracer, root: root, base: base}
	if runErr := fn(ctx, rec); runErr != nil {
		root.RecordError(runErr)
		return runErr
	}
	return nil
}

// Recorder lets callers emit a child span without re-deriving the base
// attributes for every record.
type Recorder struct {
	tracer trace.Tracer
	root   trace.Span
	base   []attribute.KeyValue
}

// SetRootAttributes attaches attributes discovered after the root span starts.
func (r *Recorder) SetRootAttributes(attrs ...attribute.KeyValue) {
	r.root.SetAttributes(attrs...)
}

// Record creates a child span on the recorder's tracer, attaches the base
// attributes plus any extras, and ends it immediately. Use this for
// instantaneous facts (a CUE value loaded, an artefact rendered) where
// duration isn't interesting.
func (r *Recorder) Record(ctx context.Context, name string, extras ...attribute.KeyValue) {
	attrs := make([]attribute.KeyValue, 0, len(r.base)+len(extras))
	attrs = append(attrs, r.base...)
	attrs = append(attrs, extras...)
	_, span := r.tracer.Start(ctx, name, trace.WithAttributes(attrs...))
	span.End()
}

func baseAttributes(topologyDir, instance string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("topology.instance", instance),
		attribute.String("topology.module", topologyDir),
	}
	for _, kv := range []struct {
		env, key string
	}{
		{"VERSELF_DEPLOY_ID", "verself.deploy_id"},
		{"VERSELF_DEPLOY_RUN_KEY", "verself.deploy_run_key"},
		{"VERSELF_DEPLOY_KIND", "verself.deploy_kind"},
		{"VERSELF_VERIFICATION_RUN", "verself.verification_run"},
	} {
		if v := strings.TrimSpace(os.Getenv(kv.env)); v != "" {
			attrs = append(attrs, attribute.String(kv.key, v))
		}
	}
	return attrs
}
