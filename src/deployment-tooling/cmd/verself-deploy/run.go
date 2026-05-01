package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tooling/internal/canary"
	"github.com/verself/deployment-tooling/internal/identity"
	"github.com/verself/deployment-tooling/internal/layers"
	"github.com/verself/deployment-tooling/internal/ledger"
	"github.com/verself/deployment-tooling/internal/runtime"
)

// runRun is the `verself-deploy run` entry point — the in-process
// orchestrator that supersedes the per-step shell-outs the AXL
// deploy task previously sequenced. It owns identity, ledger writes,
// layered substrate, external reconcilers, Nomad submits, and the
// post-deploy divergence canary.
//
// AXL-side responsibilities preserved (because they sit outside the
// SSH/agent surface this binary owns):
//   - aspect-operator refresh (re-mints the operator's SSH cert)
//   - cue-renderer materialisation of .cache/render/<site>/
func runRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	site := fs.String("site", "prod", "deploy site")
	sha := fs.String("sha", "", "git SHA being deployed (empty = HEAD)")
	scope := fs.String("scope", "all", "all | affected (affected is a stub for now)")
	substrateMode := fs.String("substrate", "auto", "auto | always (skip is rejected — layer hashes gate per-layer)")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	switch *substrateMode {
	case "auto", "always":
	case "skip":
		fmt.Fprintln(os.Stderr, "verself-deploy run: --substrate=skip is unsupported; layer digests gate convergence per layer")
		return 1
	default:
		fmt.Fprintf(os.Stderr, "verself-deploy run: --substrate must be auto|always (got %q)\n", *substrateMode)
		return 2
	}

	rr := *repoRoot
	if rr == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy run: cwd: %v\n", err)
			return 1
		}
		rr = cwd
	}

	// Identity is derived once at process start. Generate is
	// idempotent on a pre-populated VERSELF_DEPLOY_RUN_KEY /
	// VERSELF_DEPLOY_ID, so a parent that has its own correlation
	// (e.g. an ultrareview or canary harness) keeps it intact.
	snap, err := identity.Generate(identity.GenerateOptions{
		Site:  *site,
		Sha:   *sha,
		Scope: *scope,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy run: derive identity: %v\n", err)
		return 1
	}
	snap.ApplyEnv()

	parentCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rt, err := runtime.Init(parentCtx, runtime.Options{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Site:           *site,
		RepoRoot:       rr,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy run: %v\n", err)
		return 1
	}
	defer rt.Close()

	ctx, span := rt.Tracer.Start(rt.Ctx, "verself_deploy.run",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("verself.site", *site),
			attribute.String("verself.deploy_scope", *scope),
			attribute.String("verself.substrate_mode", *substrateMode),
		),
	)
	defer span.End()

	led := ledger.New(rt.ClickHouse)
	resolvedSha := snap.Get("VERSELF_DEPLOY_SHA")
	if resolvedSha == "" {
		// Fallback to the commit sha so the ledger schema's
		// FixedString(40) requirement is satisfied even on a
		// detached-HEAD harness invocation.
		resolvedSha = snap.Get("VERSELF_COMMIT_SHA")
	}
	startedAt := time.Now()
	startedEvent := ledger.DeployEvent{
		RunKey: snap.RunKey(),
		Site:   *site,
		Sha:    resolvedSha,
		Actor:  snap.Get("VERSELF_AUTHOR"),
		Scope:  *scope,
		Kind:   ledger.EventStarted,
	}
	if err := led.RecordDeployEvent(ctx, startedEvent); err != nil {
		// Mirror the bash behaviour: the started row is best-effort
		// observability. The succeeded/failed row is the gating
		// record. Surface as a span event so the failure remains
		// queryable even though we proceed.
		span.AddEvent("started-row insert failed",
			trace.WithAttributes(attribute.String("error", err.Error())))
	}

	exitCode := runDeployBody(ctx, rt, led, *site, resolvedSha, *scope, *substrateMode, rr, span, startedAt, snap)
	span.SetAttributes(attribute.Int("verself.exit_code", exitCode))
	if exitCode != 0 {
		span.SetStatus(codes.Error, fmt.Sprintf("verself-deploy run exited %d", exitCode))
	} else {
		span.SetStatus(codes.Ok, "")
	}
	return exitCode
}

func runDeployBody(
	ctx context.Context,
	rt *runtime.Runtime,
	led *ledger.Writer,
	site, sha, scope, substrateMode, repoRoot string,
	span trace.Span,
	startedAt time.Time,
	snap identity.Snapshot,
) int {
	inventoryDir := filepath.Join(repoRoot, ".cache", "render", site, "inventory")
	if _, err := os.Stat(inventoryDir); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy run: rendered inventory missing at %s: %v (run `aspect render` first)\n", inventoryDir, err)
		writeFailedDeployEvent(ctx, led, site, sha, scope, snap, startedAt, nil, "rendered inventory missing")
		return 1
	}
	ansibleDir := filepath.Join(repoRoot, "src", "substrate", "ansible")

	// 1. Layered substrate convergence.
	layerRes := layers.RunAll(ctx, layers.Options{
		Site:         site,
		RepoRoot:     repoRoot,
		AnsibleDir:   ansibleDir,
		Inventory:    inventoryDir,
		Force:        substrateMode == "always",
		OTLPEndpoint: rt.OTLPEndpoint(),
		ChWriter:     rt.ClickHouse,
		Ledger:       led,
	})
	if layerRes.Err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy run: layered substrate failed at %s: %v\n", layerRes.FailedLayer, layerRes.Err)
		writeFailedDeployEvent(ctx, led, site, sha, scope, snap, startedAt, layerRes.LayersRan,
			"layer "+layerRes.FailedLayer+" failed: "+layerRes.Err.Error())
		return 1
	}

	// 2. External reconcilers (Cloudflare DNS today; future add-ons
	// land here). Each is a subprocess that emits its own ledger
	// rows; the run-level span captures them via baggage propagation.
	if err := runExternalReconcilers(ctx, rt, repoRoot, site); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy run: external reconcilers failed: %v\n", err)
		writeFailedDeployEvent(ctx, led, site, sha, scope, snap, startedAt, layerRes.LayersRan,
			"external reconcilers: "+err.Error())
		return 1
	}

	// 3. Nomad fan-out — same in-process function as standalone
	// `verself-deploy nomad deploy-all`, no subprocess.
	if err := deployAll(ctx, rt, span, site, repoRoot, false); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy run: nomad deploy-all failed: %v\n", err)
		writeFailedDeployEvent(ctx, led, site, sha, scope, snap, startedAt, layerRes.LayersRan,
			"nomad deploy-all: "+err.Error())
		return 1
	}

	// 4. Post-deploy divergence canary. Asserts the layer ledger is
	// consistent and no task ran 'changed' inside a layer the deploy
	// chose to skip.
	report, err := canary.CheckDivergence(ctx, rt.ClickHouse, site, snap.RunKey())
	if err != nil {
		var de *canary.DivergenceError
		if errors.As(err, &de) {
			fmt.Fprintf(os.Stderr, "DIVERGENCE: deploy_run_key=%s site=%s\n", report.RunKey, report.Site)
			for _, a := range report.Anomalies {
				fmt.Fprintf(os.Stderr, "  - %s\n", a)
			}
		} else {
			fmt.Fprintf(os.Stderr, "verself-deploy run: divergence canary errored: %v\n", err)
		}
		writeFailedDeployEvent(ctx, led, site, sha, scope, snap, startedAt, layerRes.LayersRan,
			"divergence-canary: "+err.Error())
		return 1
	}
	fmt.Fprintf(os.Stderr, "[divergence-canary] deploy_run_key=%s site=%s ledger=clean (%d layers)\n",
		report.RunKey, report.Site, report.RowCount)

	// 5. Succeeded.
	durationMs := uint32(time.Since(startedAt).Milliseconds())
	successEvent := ledger.DeployEvent{
		RunKey:             snap.RunKey(),
		Site:               site,
		Sha:                sha,
		Actor:              snap.Get("VERSELF_AUTHOR"),
		Scope:              scope,
		AffectedComponents: layerRes.LayersRan,
		Kind:               ledger.EventSucceeded,
		DurationMs:         durationMs,
	}
	if err := led.RecordDeployEvent(ctx, successEvent); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy run: WARN: deploy_events succeeded insert failed: %v\n", err)
	}
	return 0
}

// writeFailedDeployEvent is a defer-friendly companion to
// runDeployBody; it captures the components that did run before the
// failure so the deploy_events row reflects partial progress rather
// than recording a blanket failure.
func writeFailedDeployEvent(
	ctx context.Context,
	led *ledger.Writer,
	site, sha, scope string,
	snap identity.Snapshot,
	startedAt time.Time,
	components []string,
	errorMessage string,
) {
	durationMs := uint32(time.Since(startedAt).Milliseconds())
	ev := ledger.DeployEvent{
		RunKey:             snap.RunKey(),
		Site:               site,
		Sha:                sha,
		Actor:              snap.Get("VERSELF_AUTHOR"),
		Scope:              scope,
		AffectedComponents: components,
		Kind:               ledger.EventFailed,
		DurationMs:         durationMs,
		ErrorMessage:       truncateError(errorMessage),
	}
	if err := led.RecordDeployEvent(ctx, ev); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy run: WARN: deploy_events failed-row insert failed: %v\n", err)
	}
}

// truncateError trims the error message to a sane length so a
// stack-trace-bearing wrap doesn't blow the row size budget. The
// underlying span carries the full error.
func truncateError(s string) string {
	const maxLen = 512
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

// runExternalReconcilers shells out to the per-reconciler scripts.
// They live in src/substrate/scripts because they own their own
// row schema (verself.reconciler_runs) and SSH lifetime; collapsing
// them into the binary is a future cleanup, not a Phase 4 deliverable.
func runExternalReconcilers(ctx context.Context, rt *runtime.Runtime, repoRoot, site string) error {
	tracer := rt.Tracer
	ctx, span := tracer.Start(ctx, "verself_deploy.external_reconcilers",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attribute.String("verself.site", site)),
	)
	defer span.End()

	scripts := []string{"reconcile-cloudflare-dns.sh"}
	for _, name := range scripts {
		if err := runOneReconciler(ctx, rt, repoRoot, site, name); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

func runOneReconciler(ctx context.Context, rt *runtime.Runtime, repoRoot, site, scriptName string) error {
	scriptPath := filepath.Join(repoRoot, "src", "substrate", "scripts", scriptName)
	if _, err := os.Stat(scriptPath); err != nil {
		return fmt.Errorf("reconciler script missing at %s: %w", scriptPath, err)
	}
	cmd := exec.CommandContext(ctx, scriptPath, "--site="+site)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"OTEL_EXPORTER_OTLP_ENDPOINT=http://"+rt.OTLPEndpoint(),
		"VERSELF_OTLP_ENDPOINT="+rt.OTLPEndpoint(),
	)
	cmd.Dir = filepath.Join(repoRoot, "src", "substrate")
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return fmt.Errorf("%s exited %d", scriptName, ee.ExitCode())
		}
		return err
	}
	return nil
}
