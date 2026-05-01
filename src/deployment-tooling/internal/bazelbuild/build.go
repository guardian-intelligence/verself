// Package bazelbuild wraps `bazelisk build` so the deploy flow can
// resolve target outputs through the Build Event Protocol rather
// than `bazelisk cquery --output=files | tail -1`.
//
// The cquery path's documented multi-config caveat means a target
// that produces outputs in two configurations silently picks the
// wrong one when piped through `tail -1`; BEP is unambiguous because
// the build is what produced the file and BEP is the build's own
// witness.
//
// See: https://bazel.build/query/cquery (#output-files multi-config note)
//
//	https://bazel.build/remote/bep                (BEP overview)
package bazelbuild

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tooling/internal/bep"
)

const tracerName = "github.com/verself/deployment-tooling/internal/bazelbuild"

// Result is the indexed BEP for the build that just ran.
type Result struct {
	Stream *bep.Stream
	BEPath string
}

// Build invokes `bazelisk build <extraFlags...> --build_event_json_file=<tmp> <targets...>`
// from cwd, then parses the resulting BEP into a *bep.Stream.
//
// Stdout/stderr inherit so operator-facing progress lines (the only
// thing console-watching humans care about) flow normally; the BEP
// file is the typed channel.
func Build(ctx context.Context, cwd string, targets []string, extraFlags ...string) (*Result, error) {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "verself_deploy.bazel.build",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.StringSlice("bazel.targets", targets),
			attribute.StringSlice("bazel.flags", extraFlags),
		),
	)
	defer span.End()

	bepFile, err := os.CreateTemp("", "verself-deploy-bep-*.ndjson")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("create bep tempfile: %w", err)
	}
	bepPath := bepFile.Name()
	_ = bepFile.Close()
	defer os.Remove(bepPath)

	args := append([]string{"build"}, extraFlags...)
	args = append(args, "--build_event_json_file="+bepPath)
	args = append(args, targets...)
	span.SetAttributes(attribute.String("bazel.bep_path", bepPath))

	cmd := exec.CommandContext(ctx, "bazelisk", args...)
	cmd.Dir = cwd
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("bazelisk build: %w", err)
	}

	parseCtx, parseSpan := tracer.Start(ctx, "verself_deploy.bazel.bep.parse",
		trace.WithAttributes(attribute.String("bazel.bep_path", bepPath)),
	)
	_ = parseCtx
	stream, err := bep.Parse(bepPath)
	if err != nil {
		parseSpan.RecordError(err)
		parseSpan.SetStatus(codes.Error, err.Error())
		parseSpan.End()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	failed := stream.FailedTargets()
	parseSpan.SetAttributes(
		attribute.Int("bep.named_set_count", stream.CountNamedSets()),
		attribute.Int("bep.target_complete_count", stream.CountTargetCompletes()),
		attribute.StringSlice("bep.failed_targets", failed),
	)
	if len(failed) > 0 {
		err := fmt.Errorf("bazel build: %d target(s) failed: %v", len(failed), failed)
		parseSpan.RecordError(err)
		parseSpan.SetStatus(codes.Error, err.Error())
		parseSpan.End()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	parseSpan.SetStatus(codes.Ok, "")
	parseSpan.End()

	span.SetAttributes(
		attribute.Int("bep.target_complete_count", stream.CountTargetCompletes()),
	)
	span.SetStatus(codes.Ok, "")
	return &Result{Stream: stream, BEPath: bepPath}, nil
}
