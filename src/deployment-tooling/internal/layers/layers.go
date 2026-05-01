// Package layers walks the substrate convergence layers in order,
// hash-gating each. Replaces helpers.run_layered_substrate +
// scripts/run-layer.sh + scripts/layer-last-applied.sh.
//
// For each layer:
//   1. Build the per-layer Bazel digest target and read its output as
//      a 64-char sha256 hex string.
//   2. Read last_applied_hash from verself.deploy_layer_runs (via
//      ledger.LastAppliedHash).
//   3. If hash matches and not Force: emit a 'skipped' row, return.
//   4. Else: invoke ansible.Run with the layer's playbook; emit
//      'succeeded' or 'failed' to the ledger and propagate errors.
//
// The plan is the closed substrate.SUBSTRATE_LAYERS set; any layer
// added there must also land here so the canary's expected-row count
// stays in lockstep.
package layers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tooling/internal/ansible"
	"github.com/verself/deployment-tooling/internal/bazelbuild"
	"github.com/verself/deployment-tooling/internal/chwriter"
	"github.com/verself/deployment-tooling/internal/ledger"
)

const tracerName = "github.com/verself/deployment-tooling/internal/layers"

// Plan is the ordered set of layers and their playbooks. Order matters:
// L1 sets up host primitives every later layer depends on; L2 creates
// users L3 daemons claim; L3 starts daemons L4a reconciles against.
// The hash-gate decision for each layer is independent.
var Plan = []Layer{
	{Name: "l1_os", Playbook: "playbooks/l1_os.yml"},
	{Name: "l2_userspace", Playbook: "playbooks/l2_userspace.yml"},
	{Name: "l3_binaries", Playbook: "playbooks/l3_binaries.yml"},
	{Name: "l4a_components", Playbook: "playbooks/l4a_components.yml"},
}

// Layer is one entry in Plan: the layer's name and the playbook
// `verself-deploy ansible run` invokes when the hash gate misses.
type Layer struct {
	Name     string
	Playbook string
}

// DigestTarget is the Bazel target whose output file holds the
// layer's input_hash. The format mirrors helpers.SUBSTRATE_LAYERS.
func (l Layer) DigestTarget(site string) string {
	return fmt.Sprintf("//src/substrate:%s_%s_digest", site, l.Name)
}

// Options configure a Run. Site, RepoRoot, AnsibleDir, Inventory are
// required; Force flips the hash gate off.
type Options struct {
	Site       string
	RepoRoot   string
	AnsibleDir string
	Inventory  string

	// Force re-runs every layer regardless of hash. Mirrors the
	// `aspect substrate converge` semantics; `aspect deploy` leaves
	// it false so converged layers short-circuit.
	Force bool

	// OTLPEndpoint is the SSH-forwarded OTLP endpoint to point
	// ansible-playbook at. Threaded through ansible.Options.OTLPEndpoint.
	OTLPEndpoint string

	// ChWriter is the typed ClickHouse writer (verself db); both the
	// ansible task-event recorder and the ledger writer share it.
	ChWriter *chwriter.Writer

	// Ledger is the typed deploy_layer_runs writer.
	Ledger *ledger.Writer

	// ExtraAnsibleArgs are appended to every ansible-playbook
	// invocation (e.g. `-e temporal_force_schema_reset=true` from a
	// substrate reset).
	ExtraAnsibleArgs []string
}

// Result is the outcome of RunAll. LayersRan are layers whose
// playbook actually ran; LayersSkipped short-circuited on hash
// match. FailedLayer is the first layer to error (empty on success).
type Result struct {
	LayersRan     []string
	LayersSkipped []string
	FailedLayer   string
	Err           error
}

// LayerDigests returns each layer's input_hash. Builds all
// per-layer digest targets in one bazelisk invocation so the BEP
// stream covers them in one round-trip.
func LayerDigests(ctx context.Context, repoRoot, site string) (map[string]string, error) {
	targets := make([]string, len(Plan))
	for i, layer := range Plan {
		targets[i] = layer.DigestTarget(site)
	}
	build, err := bazelbuild.Build(ctx, repoRoot, targets, "--config=remote-writer")
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(Plan))
	for _, layer := range Plan {
		target := layer.DigestTarget(site)
		files, err := build.Stream.ResolveOutputs(target, repoRoot)
		if err != nil {
			return nil, fmt.Errorf("resolve outputs for %s: %w", target, err)
		}
		if len(files) != 1 {
			return nil, fmt.Errorf("layer digest target %s produced %d files, want 1: %v", target, len(files), files)
		}
		raw, err := os.ReadFile(files[0])
		if err != nil {
			return nil, fmt.Errorf("read layer digest %s: %w", files[0], err)
		}
		digest := strings.TrimSpace(string(raw))
		if !sha64Re.MatchString(digest) {
			return nil, fmt.Errorf("layer digest target %s produced non-hex content %q", target, digest)
		}
		out[layer.Name] = digest
	}
	return out, nil
}

// RunAll walks Plan in order, hash-gating each layer.
//
// Failure semantics: the first layer to error stops the walk and
// returns; subsequent layers are not attempted. This matches the
// run_layered_substrate behaviour the AXL caller previously got
// from looping `run-layer.sh` until a non-zero exit.
func RunAll(ctx context.Context, opts Options) Result {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "verself_deploy.layers.run_all",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("verself.site", opts.Site),
			attribute.Bool("verself.force", opts.Force),
			attribute.Int("verself.layer_count", len(Plan)),
		),
	)
	defer span.End()

	if err := validateOptions(opts); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Result{Err: err}
	}

	digests, err := LayerDigests(ctx, opts.RepoRoot, opts.Site)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Result{Err: fmt.Errorf("compute layer digests: %w", err)}
	}

	var res Result
	for _, layer := range Plan {
		digest := digests[layer.Name]
		if err := runOne(ctx, tracer, opts, layer, digest, &res); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			res.FailedLayer = layer.Name
			res.Err = err
			return res
		}
	}
	span.SetStatus(codes.Ok, "")
	span.SetAttributes(
		attribute.StringSlice("verself.layers_ran", res.LayersRan),
		attribute.StringSlice("verself.layers_skipped", res.LayersSkipped),
	)
	return res
}

func runOne(ctx context.Context, tracer trace.Tracer, opts Options, layer Layer, digest string, res *Result) error {
	ctx, span := tracer.Start(ctx, "verself_deploy.layers.run",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("verself.layer", layer.Name),
			attribute.String("verself.input_hash", digest),
			attribute.Bool("verself.force", opts.Force),
		),
	)
	defer span.End()

	lastApplied, err := opts.Ledger.LastAppliedHash(ctx, opts.Site, layer.Name)
	if err != nil {
		// Mirror the bash behaviour: ledger read errors degrade to
		// "no prior evidence; force the run" so a transient ClickHouse
		// outage doesn't gate the deploy on stale hashes.
		span.AddEvent("layer-last-applied query failed; forcing run", trace.WithAttributes(
			attribute.String("error", err.Error()),
		))
		lastApplied = ""
	}
	span.SetAttributes(attribute.String("verself.last_applied_hash", lastApplied))

	willSkip := !opts.Force && lastApplied != "" && lastApplied == digest
	if willSkip {
		row := ledger.LayerRun{
			RunKey:          os.Getenv("VERSELF_DEPLOY_RUN_KEY"),
			Site:            opts.Site,
			Layer:           layer.Name,
			InputHash:       digest,
			LastAppliedHash: lastApplied,
			Kind:            ledger.LayerSkipped,
			Skipped:         true,
			SkipReason:      "input_hash matches last_applied_hash",
			DurationMs:      0,
			ChangedCount:    0,
		}
		if err := opts.Ledger.RecordLayerRun(ctx, row); err != nil {
			return fmt.Errorf("record layer-skipped: %w", err)
		}
		res.LayersSkipped = append(res.LayersSkipped, layer.Name)
		span.SetAttributes(attribute.Bool("verself.skipped", true))
		span.SetStatus(codes.Ok, "")
		return nil
	}

	start := time.Now()
	runRes, runErr := ansible.Run(ctx, opts.ChWriter, ansible.Options{
		Playbook:     layer.Playbook,
		Inventory:    opts.Inventory,
		AnsibleDir:   opts.AnsibleDir,
		ExtraArgs:    opts.ExtraAnsibleArgs,
		Site:         opts.Site,
		Layer:        layer.Name,
		OTLPEndpoint: opts.OTLPEndpoint,
	})
	durationMs := uint32(time.Since(start).Milliseconds())

	changedCount := uint32(0)
	if runRes != nil {
		changedCount = uint32(runRes.ChangedCount)
	}

	if runErr != nil || (runRes != nil && runRes.ExitCode != 0) {
		errMsg := ""
		exitCode := 0
		if runRes != nil {
			exitCode = runRes.ExitCode
		}
		if runErr != nil {
			errMsg = runErr.Error()
		} else {
			errMsg = fmt.Sprintf("ansible-playbook %s exited %d", layer.Playbook, exitCode)
		}
		row := ledger.LayerRun{
			RunKey:          os.Getenv("VERSELF_DEPLOY_RUN_KEY"),
			Site:            opts.Site,
			Layer:           layer.Name,
			InputHash:       digest,
			LastAppliedHash: lastApplied,
			Kind:            ledger.LayerFailed,
			DurationMs:      durationMs,
			ChangedCount:    changedCount,
			ErrorMessage:    errMsg,
		}
		if recordErr := opts.Ledger.RecordLayerRun(ctx, row); recordErr != nil {
			// The ansible failure is the load-bearing error to
			// surface; the ledger failure is a secondary signal.
			span.RecordError(recordErr)
		}
		if runErr != nil {
			return runErr
		}
		return fmt.Errorf("ansible-playbook %s exited %d", layer.Playbook, exitCode)
	}

	row := ledger.LayerRun{
		RunKey:          os.Getenv("VERSELF_DEPLOY_RUN_KEY"),
		Site:            opts.Site,
		Layer:           layer.Name,
		InputHash:       digest,
		LastAppliedHash: lastApplied,
		Kind:            ledger.LayerSucceeded,
		DurationMs:      durationMs,
		ChangedCount:    changedCount,
	}
	if err := opts.Ledger.RecordLayerRun(ctx, row); err != nil {
		return fmt.Errorf("record layer-succeeded: %w", err)
	}
	res.LayersRan = append(res.LayersRan, layer.Name)
	span.SetAttributes(
		attribute.Bool("verself.skipped", false),
		attribute.Int("verself.changed_count", int(changedCount)),
		attribute.Int64("verself.duration_ms", int64(durationMs)),
	)
	span.SetStatus(codes.Ok, "")
	return nil
}

func validateOptions(opts Options) error {
	if opts.Site == "" {
		return errors.New("layers: Site is required")
	}
	if opts.RepoRoot == "" {
		return errors.New("layers: RepoRoot is required")
	}
	if opts.AnsibleDir == "" {
		return errors.New("layers: AnsibleDir is required")
	}
	if opts.Inventory == "" {
		return errors.New("layers: Inventory is required")
	}
	if !filepath.IsAbs(opts.Inventory) {
		return fmt.Errorf("layers: Inventory must be absolute: %q", opts.Inventory)
	}
	if opts.Ledger == nil {
		return errors.New("layers: Ledger is required")
	}
	return nil
}

var sha64Re = regexp.MustCompile(`^[0-9a-f]{64}$`)
