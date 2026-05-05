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
	"github.com/verself/deployment-tools/internal/bazelbuild"
	"github.com/verself/deployment-tools/internal/deploydb"
	"github.com/verself/deployment-tools/internal/identity"
	"github.com/verself/deployment-tools/internal/runtime"
	"github.com/verself/deployment-tools/internal/supplychain"
)

const (
	hostConfigurationSitePlaybook    = "playbooks/site.yml"
	hostConfigurationPhase           = "host_configuration_site"
	hostConfigurationComponent       = "host-configuration"
	spireIdentityRegistryTarget      = "//src/host-configuration:spire_identity_registry"
	componentSubstrateRegistryTarget = "//src/host-configuration:component_substrate_registry"
	canonicalDeployScope             = "affected"
)

// runRun is the `verself-deploy run` entry point. It owns identity,
// deploy evidence writes, host configuration convergence through the canonical
// Ansible site playbook, and affected Nomad submits.
//
// AXL-side responsibilities preserved because they sit outside the deploy
// binary's SSH, evidence, and Nomad orchestration surface.
func runRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	site := fs.String("site", "prod", "deploy site")
	sha := fs.String("sha", "", "git SHA being deployed (empty = HEAD)")
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
		Scope: canonicalDeployScope,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy run: derive identity: %v\n", err)
		return 1
	}
	snap.ApplyEnv()

	resolvedSha := snap.Get("VERSELF_DEPLOY_SHA")
	if resolvedSha == "" {
		// Fallback to the commit sha so the deploy_events schema's
		// FixedString(40) requirement is satisfied even on a
		// detached-HEAD harness invocation.
		resolvedSha = snap.Get("VERSELF_COMMIT_SHA")
	}

	parentCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	securityPatchRes := runSecurityPatchPreflight(parentCtx, *site, rr, snap.RunKey())
	if !securityPatchOK(securityPatchRes) {
		msg := securityPatchFailureMessage(securityPatchRes)
		fmt.Fprintf(os.Stderr, "verself-deploy run: security patch preflight failed: %s\n", msg)
		recordSecurityPatchFailureBestEffort(parentCtx, *site, rr, resolvedSha, canonicalDeployScope, snap, securityPatchRes)
		return 1
	}

	startedAt := time.Now()
	var bootstrapHost *hostConfigurationBootstrapResult
	rt, err := runtime.Init(parentCtx, runtime.Options{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Site:           *site,
		RepoRoot:       rr,
	})
	if err != nil {
		if !requiresHostConfigurationBootstrap(err) {
			fmt.Fprintf(os.Stderr, "verself-deploy run: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "verself-deploy run: ClickHouse evidence substrate unavailable; bootstrapping host configuration first: %v\n", err)
		bootstrapHost, err = runHostConfigurationBootstrap(parentCtx, *site, rr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy run: bootstrap host configuration failed: %v\n", err)
			return 1
		}
		rt, err = runtime.Init(parentCtx, runtime.Options{
			ServiceName:    serviceName,
			ServiceVersion: serviceVersion,
			Site:           *site,
			RepoRoot:       rr,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy run: runtime after bootstrap: %v\n", err)
			return 1
		}
	}
	defer rt.Close()

	ctx, span := rt.Tracer.Start(rt.Ctx, "verself_deploy.run",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("verself.site", *site),
			attribute.String("verself.deploy_scope", canonicalDeployScope),
		),
	)
	defer span.End()
	span.SetAttributes(
		attribute.Int("security_patch.task_count", securityPatchRes.Result.TaskCount),
		attribute.Int("security_patch.changed_total", securityPatchRes.Result.ChangedCount),
		attribute.Int("security_patch.failed_count", securityPatchRes.Result.FailedCount),
		attribute.Int64("security_patch.duration_ms", securityPatchRes.EndedAt.Sub(securityPatchRes.StartedAt).Milliseconds()),
		attribute.Bool("host_configuration.bootstrap_ran", bootstrapHost != nil),
	)
	if bootstrapHost != nil && bootstrapHost.Result != nil {
		span.SetAttributes(
			attribute.Int("host_configuration.bootstrap.task_count", bootstrapHost.Result.TaskCount),
			attribute.Int("host_configuration.bootstrap.changed_total", bootstrapHost.Result.ChangedCount),
			attribute.Int("host_configuration.bootstrap.failed_count", bootstrapHost.Result.FailedCount),
			attribute.Int64("host_configuration.bootstrap.duration_ms", bootstrapHost.EndedAt.Sub(bootstrapHost.StartedAt).Milliseconds()),
		)
	}
	if err := runSecurityPostPreflight(ctx, rt, *site, securityPatchRes); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		fmt.Fprintf(os.Stderr, "verself-deploy run: security post-preflight failed: %v\n", err)
		return 1
	}

	startedEvent := deploydb.DeployEvent{
		EventAt: startedAt,
		RunKey:  snap.RunKey(),
		Site:    *site,
		Sha:     resolvedSha,
		Actor:   snap.Get("VERSELF_AUTHOR"),
		Scope:   canonicalDeployScope,
		Kind:    deploydb.EventStarted,
	}
	if err := rt.DeployDB.RecordDeployEvent(ctx, startedEvent); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		fmt.Fprintf(os.Stderr, "verself-deploy run: deploy_events started insert failed: %v\n", err)
		return 1
	}

	exitCode := runDeployBody(ctx, rt, rt.DeployDB, *site, resolvedSha, canonicalDeployScope, rr, span, startedAt, snap, bootstrapHost)
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
	bootstrapHost *hostConfigurationBootstrapResult,
) int {
	// 1. Supply-chain policy gate. The gate is intentionally before host
	// convergence so install-source drift fails before Ansible mutates the box.
	// Admission rollout allows tracked provisional artifacts; untracked sources still fail closed.
	_, supplyChainEval, err := checkSupplyChainPolicy(ctx, rt, site, repoRoot, supplychain.DefaultPolicyPath, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy run: supply-chain policy failed: %v\n", err)
		writeFailedDeployEvent(ctx, db, site, sha, scope, snap, startedAt, []string{securityPatchComponent, supplyChainComponent}, err.Error())
		return 1
	}

	// 2. Host configuration convergence. Bazel materializes generated inputs
	// such as the SPIRE identity registry; Ansible applies the site playbook.
	// Nomad-component digests remain owned by the Nomad path below.
	components := []string{securityPatchComponent, supplyChainComponent}
	if bootstrapHost != nil {
		components = append(components, hostConfigurationComponent)
	} else {
		hostInputs, err := buildHostGeneratedInputs(ctx, repoRoot)
		if err != nil {
			msg := fmt.Sprintf("host generated input build failed: %v", err)
			fmt.Fprintf(os.Stderr, "verself-deploy run: %s\n", msg)
			writeFailedDeployEvent(ctx, db, site, sha, scope, snap, startedAt, components, msg)
			return 1
		}
		span.SetAttributes(
			attribute.String("component.substrate_registry.path", hostInputs.ComponentSubstrateRegistry),
			attribute.String("spire.identity_registry.path", hostInputs.SpireIdentityRegistry),
		)
		hostStarted := time.Now()
		hostRes, err := runHostConfigurationSitePlaybook(ctx, rt, site, repoRoot, []string{
			"-e", "verself_repo_root=" + repoRoot,
			"-e", "spire_registrations_file=" + hostInputs.SpireIdentityRegistry,
			"-e", "component_substrate_registry_file=" + hostInputs.ComponentSubstrateRegistry,
		})
		if err != nil || hostRes == nil || hostRes.ExitCode != 0 {
			msg := fmt.Sprintf("host configuration failed: %v", err)
			if err == nil {
				msg = ansibleFailureMessage(hostConfigurationSitePlaybook, hostRes, nil)
			}
			fmt.Fprintf(os.Stderr, "verself-deploy run: %s\n", msg)
			writeFailedDeployEvent(ctx, db, site, sha, scope, snap, startedAt, append(components, hostConfigurationComponent), msg)
			return 1
		}
		components = append(components, hostConfigurationComponent)
		span.SetAttributes(
			attribute.Int("ansible.task_count", hostRes.TaskCount),
			attribute.Int("ansible.changed_total", hostRes.ChangedCount),
			attribute.Int("ansible.failed_count", hostRes.FailedCount),
			attribute.Int64("ansible.duration_ms", time.Since(hostStarted).Milliseconds()),
		)
	}
	if err := recordSupplyChainEvaluation(ctx, rt, site, snap.RunKey(), supplyChainEval); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy run: supply-chain evidence insert failed: %v\n", err)
		writeFailedDeployEvent(ctx, db, site, sha, scope, snap, startedAt, components, err.Error())
		return 1
	}

	// 3. Nomad deploy: Bazel discovers the checked-in nomad_component targets,
	// Garage receives any missing content-addressed artifacts, and Nomad gets
	// per-job resolved payloads directly.
	if err := deployNomadComponents(ctx, rt, span, site, repoRoot); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy run: nomad deploy failed: %v\n", err)
		writeFailedDeployEvent(ctx, db, site, sha, scope, snap, startedAt, append(components, "nomad"),
			"nomad deploy: "+err.Error())
		return 1
	}
	components = append(components, "nomad")

	// 4. Succeeded.
	durationMs := durationMillis(time.Since(startedAt), "deploy duration")
	successEvent := deploydb.DeployEvent{
		RunKey:             snap.RunKey(),
		Site:               site,
		Sha:                sha,
		Actor:              snap.Get("VERSELF_AUTHOR"),
		Scope:              scope,
		AffectedComponents: components,
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

type hostGeneratedInputs struct {
	ComponentSubstrateRegistry string
	SpireIdentityRegistry      string
}

func buildHostGeneratedInputs(ctx context.Context, repoRoot string) (hostGeneratedInputs, error) {
	build, err := bazelbuild.Build(ctx, repoRoot, []string{
		componentSubstrateRegistryTarget,
		spireIdentityRegistryTarget,
	}, "--config=remote-writer")
	if err != nil {
		return hostGeneratedInputs{}, err
	}
	spireOutputs, err := build.Stream.ResolveOutputs(spireIdentityRegistryTarget, repoRoot)
	if err != nil {
		return hostGeneratedInputs{}, fmt.Errorf("resolve %s output: %w", spireIdentityRegistryTarget, err)
	}
	if len(spireOutputs) != 1 {
		return hostGeneratedInputs{}, fmt.Errorf("%s must produce exactly one output, got %d: %v", spireIdentityRegistryTarget, len(spireOutputs), spireOutputs)
	}
	componentOutputs, err := build.Stream.ResolveOutputs(componentSubstrateRegistryTarget, repoRoot)
	if err != nil {
		return hostGeneratedInputs{}, fmt.Errorf("resolve %s output: %w", componentSubstrateRegistryTarget, err)
	}
	componentRegistry, err := selectBazelOutput(componentSubstrateRegistryTarget, componentOutputs, ".component_substrate_registry.json")
	if err != nil {
		return hostGeneratedInputs{}, err
	}
	return hostGeneratedInputs{
		ComponentSubstrateRegistry: componentRegistry,
		SpireIdentityRegistry:      spireOutputs[0],
	}, nil
}

func selectBazelOutput(label string, outputs []string, suffix string) (string, error) {
	matches := make([]string, 0, 1)
	for _, output := range outputs {
		if strings.HasSuffix(output, suffix) {
			matches = append(matches, output)
		}
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("%s must produce exactly one %s output, got %d from %d outputs: %v", label, suffix, len(matches), len(outputs), outputs)
	}
	return matches[0], nil
}

type hostConfigurationBootstrapResult struct {
	StartedAt time.Time
	EndedAt   time.Time
	Result    *ansible.Result
}

func requiresHostConfigurationBootstrap(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "runtime: clickhouse open: deploydb: read operator config") ||
		strings.Contains(msg, "runtime: clickhouse open: deploydb: open native client") ||
		strings.Contains(msg, "runtime: clickhouse open: deploydb: ping native client")
}

func runHostConfigurationBootstrap(ctx context.Context, site, repoRoot string) (*hostConfigurationBootstrapResult, error) {
	rt, err := runtime.Init(ctx, runtime.Options{
		ServiceName:     serviceName,
		ServiceVersion:  serviceVersion,
		Site:            site,
		RepoRoot:        repoRoot,
		SkipOTLPForward: true,
		SkipClickHouse:  true,
	})
	if err != nil {
		return nil, err
	}
	defer rt.Close()

	ctx, span := rt.Tracer.Start(rt.Ctx, "verself_deploy.host_configuration.bootstrap",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("verself.site", site),
			attribute.Bool("host_configuration.bootstrap", true),
		),
	)
	defer span.End()

	hostInputs, err := buildHostGeneratedInputs(ctx, repoRoot)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("component.substrate_registry.path", hostInputs.ComponentSubstrateRegistry),
		attribute.String("spire.identity_registry.path", hostInputs.SpireIdentityRegistry),
	)

	startedAt := time.Now()
	res, runErr := runHostConfigurationSitePlaybook(ctx, rt, site, repoRoot, []string{
		"-e", "verself_repo_root=" + repoRoot,
		"-e", "spire_registrations_file=" + hostInputs.SpireIdentityRegistry,
		"-e", "component_substrate_registry_file=" + hostInputs.ComponentSubstrateRegistry,
	})
	endedAt := time.Now()
	result := &hostConfigurationBootstrapResult{
		StartedAt: startedAt,
		EndedAt:   endedAt,
		Result:    res,
	}
	if runErr != nil || res == nil || res.ExitCode != 0 {
		msg := ansibleFailureMessage(hostConfigurationSitePlaybook, res, runErr)
		span.RecordError(fmt.Errorf("%s", msg))
		span.SetStatus(codes.Error, msg)
		return result, fmt.Errorf("%s", msg)
	}
	span.SetAttributes(
		attribute.Int("ansible.task_count", res.TaskCount),
		attribute.Int("ansible.changed_total", res.ChangedCount),
		attribute.Int("ansible.failed_count", res.FailedCount),
	)
	span.SetStatus(codes.Ok, "")
	return result, nil
}

func runHostConfigurationSitePlaybook(ctx context.Context, rt *runtime.Runtime, site, repoRoot string, extraArgs []string) (*ansible.Result, error) {
	inventoryPath := authoredInventoryPath(repoRoot, site)
	if _, err := os.Stat(inventoryPath); err != nil {
		return nil, fmt.Errorf("inventory missing at %s: %w", inventoryPath, err)
	}
	ansibleDir := filepath.Join(repoRoot, "src", "host-configuration", "ansible")
	args := append([]string{}, extraArgs...)
	args = append(args, "-e", "verself_site="+site)
	if rt.SSHPort > 0 {
		args = append(args, "-e", fmt.Sprintf("ansible_port=%d", rt.SSHPort))
	}
	return ansible.Run(ctx, rt.DeployDB, ansible.Options{
		Playbook:      hostConfigurationSitePlaybook,
		Inventory:     inventoryPath,
		AnsibleDir:    ansibleDir,
		ExtraArgs:     args,
		Site:          site,
		Phase:         hostConfigurationPhase,
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
