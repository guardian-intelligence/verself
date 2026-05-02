package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/ansible"
	"github.com/verself/deployment-tools/internal/deploydb"
	"github.com/verself/deployment-tools/internal/identity"
	"github.com/verself/deployment-tools/internal/runtime"
)

const (
	substrateSitePlaybook = "playbooks/site.yml"
	substratePhase        = "substrate_site"
)

// runRun is the `verself-deploy run` entry point. It owns identity,
// deploy evidence writes, substrate convergence through the canonical
// Ansible site playbook, and Nomad submits.
//
// AXL-side responsibilities preserved (because they sit outside the
// SSH/agent surface this binary owns):
//   - aspect-operator refresh (re-mints the operator's SSH cert)
func runRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	site := fs.String("site", "prod", "deploy site")
	sha := fs.String("sha", "", "git SHA being deployed (empty = HEAD)")
	scope := fs.String("scope", "all", "all | affected (affected is a stub for now)")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	if err := fs.Parse(args); err != nil {
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
	// (e.g. an ultrareview harness) keeps it intact.
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
		),
	)
	defer span.End()

	resolvedSha := snap.Get("VERSELF_DEPLOY_SHA")
	if resolvedSha == "" {
		// Fallback to the commit sha so the deploy_events schema's
		// FixedString(40) requirement is satisfied even on a
		// detached-HEAD harness invocation.
		resolvedSha = snap.Get("VERSELF_COMMIT_SHA")
	}
	startedAt := time.Now()
	startedEvent := deploydb.DeployEvent{
		RunKey: snap.RunKey(),
		Site:   *site,
		Sha:    resolvedSha,
		Actor:  snap.Get("VERSELF_AUTHOR"),
		Scope:  *scope,
		Kind:   deploydb.EventStarted,
	}
	if err := rt.DeployDB.RecordDeployEvent(ctx, startedEvent); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		fmt.Fprintf(os.Stderr, "verself-deploy run: deploy_events started insert failed: %v\n", err)
		return 1
	}

	exitCode := runDeployBody(ctx, rt, rt.DeployDB, *site, resolvedSha, *scope, rr, span, startedAt, snap)
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
	db *deploydb.Client,
	site, sha, scope, repoRoot string,
	span trace.Span,
	startedAt time.Time,
	snap identity.Snapshot,
) int {
	// 1. Substrate convergence. Ansible owns ordering via play order and
	// role order inside the canonical site playbook.
	substrateRes, err := runSubstrateSitePlaybook(ctx, rt, site, repoRoot, nil)
	if err != nil || substrateRes == nil || substrateRes.ExitCode != 0 {
		msg := ansibleFailureMessage(substrateSitePlaybook, substrateRes, err)
		fmt.Fprintf(os.Stderr, "verself-deploy run: substrate failed: %s\n", msg)
		writeFailedDeployEvent(ctx, db, site, sha, scope, snap, startedAt, []string{"substrate"}, msg)
		return 1
	}
	span.SetAttributes(
		attribute.Int("ansible.task_count", substrateRes.TaskCount),
		attribute.Int("ansible.changed_total", substrateRes.ChangedCount),
		attribute.Int("ansible.failed_count", substrateRes.FailedCount),
	)

	// 2. Nomad fan-out — same in-process function as standalone
	// `verself-deploy nomad deploy-all`, no subprocess.
	if err := deployAll(ctx, rt, span, site, repoRoot, false); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy run: nomad deploy-all failed: %v\n", err)
		writeFailedDeployEvent(ctx, db, site, sha, scope, snap, startedAt, []string{"substrate"},
			"nomad deploy-all: "+err.Error())
		return 1
	}

	// 3. Succeeded.
	durationMs := durationMillis(time.Since(startedAt), "deploy duration")
	successEvent := deploydb.DeployEvent{
		RunKey:             snap.RunKey(),
		Site:               site,
		Sha:                sha,
		Actor:              snap.Get("VERSELF_AUTHOR"),
		Scope:              scope,
		AffectedComponents: []string{"substrate", "nomad"},
		Kind:               deploydb.EventSucceeded,
		DurationMs:         durationMs,
	}
	if err := db.RecordDeployEvent(ctx, successEvent); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		fmt.Fprintf(os.Stderr, "verself-deploy run: deploy_events succeeded insert failed: %v\n", err)
		return 1
	}
	return 0
}

func runSubstrateSitePlaybook(ctx context.Context, rt *runtime.Runtime, site, repoRoot string, extraArgs []string) (*ansible.Result, error) {
	inventoryPath := authoredInventoryPath(repoRoot, site)
	if _, err := os.Stat(inventoryPath); err != nil {
		return nil, fmt.Errorf("inventory missing at %s: %w", inventoryPath, err)
	}
	ansibleDir := filepath.Join(repoRoot, "src", "host-configuration", "ansible")
	args := append([]string{}, extraArgs...)
	args = append(args, "-e", "verself_site="+site)
	return ansible.Run(ctx, rt.DeployDB, ansible.Options{
		Playbook:      substrateSitePlaybook,
		Inventory:     inventoryPath,
		AnsibleDir:    ansibleDir,
		ExtraArgs:     args,
		Site:          site,
		Phase:         substratePhase,
		RunKey:        rt.Identity.RunKey(),
		OTLPEndpoint:  rt.OTLPEndpoint(),
		AdditionalEnv: []string{"VERSELF_SITE=" + site},
	})
}

func ansibleFailureMessage(playbook string, res *ansible.Result, err error) string {
	if err != nil {
		return err.Error()
	}
	if res == nil {
		return "ansible-playbook " + playbook + " failed without a result"
	}
	return fmt.Sprintf("ansible-playbook %s exited %d", playbook, res.ExitCode)
}

// writeFailedDeployEvent is a defer-friendly companion to
// runDeployBody; it captures the components that did run before the
// failure so the deploy_events row reflects partial progress rather
// than recording a blanket failure.
func writeFailedDeployEvent(
	ctx context.Context,
	db *deploydb.Client,
	site, sha, scope string,
	snap identity.Snapshot,
	startedAt time.Time,
	components []string,
	errorMessage string,
) {
	durationMs := durationMillis(time.Since(startedAt), "deploy duration")
	ev := deploydb.DeployEvent{
		RunKey:             snap.RunKey(),
		Site:               site,
		Sha:                sha,
		Actor:              snap.Get("VERSELF_AUTHOR"),
		Scope:              scope,
		AffectedComponents: components,
		Kind:               deploydb.EventFailed,
		DurationMs:         durationMs,
		ErrorMessage:       truncateError(errorMessage),
	}
	if err := db.RecordDeployEvent(ctx, ev); err != nil {
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
