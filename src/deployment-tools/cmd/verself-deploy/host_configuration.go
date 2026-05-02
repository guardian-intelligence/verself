package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/identity"
	"github.com/verself/deployment-tools/internal/runtime"
)

// runHostConfiguration is the `verself-deploy host-configuration <subcommand>` dispatcher.
// Host configuration operations are direct executions of the canonical Ansible
// site playbook; deploy orchestration still goes through `run`.
func runHostConfiguration(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "verself-deploy host-configuration: missing subcommand (try `converge`, `verify`, or `security-patch`)")
		return 2
	}
	switch args[0] {
	case "converge":
		return runHostConfigurationConverge(args[1:])
	case "verify":
		return runHostConfigurationVerify(args[1:])
	case "security-patch":
		return runHostConfigurationSecurityPatch(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "verself-deploy host-configuration: unknown subcommand: %s\n", args[0])
		return 2
	}
}

func runHostConfigurationConverge(args []string) int {
	fs := flag.NewFlagSet("host-configuration converge", flag.ContinueOnError)
	site := fs.String("site", "prod", "deploy site")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	var extraArgs stringSliceFlag
	fs.Var(&extraArgs, "ansible-arg", "extra arg passed to ansible-playbook (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rr, ok := resolveRepoRoot("verself-deploy host-configuration converge", *repoRoot)
	if !ok {
		return 1
	}

	snap, err := identity.Generate(identity.GenerateOptions{
		Site:  *site,
		Scope: "host-configuration",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy host-configuration converge: derive identity: %v\n", err)
		return 1
	}
	snap.ApplyEnv()

	parentCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	securityPatchRes := runSecurityPatchPreflight(parentCtx, *site, rr, snap.RunKey())
	if !securityPatchOK(securityPatchRes) {
		msg := securityPatchFailureMessage(securityPatchRes)
		fmt.Fprintf(os.Stderr, "verself-deploy host-configuration converge: security patch preflight failed: %s\n", msg)
		recordSecurityPatchFailureBestEffort(parentCtx, *site, rr, snap.Get("VERSELF_DEPLOY_SHA"), "host-configuration", snap, securityPatchRes)
		return 1
	}

	rt, err := runtime.Init(parentCtx, runtime.Options{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Site:           *site,
		RepoRoot:       rr,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy host-configuration converge: %v\n", err)
		return 1
	}
	defer rt.Close()

	ctx, span := rt.Tracer.Start(rt.Ctx, "verself_deploy.host_configuration.converge",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attribute.String("verself.site", *site)),
	)
	defer span.End()

	if err := runSecurityPostPreflight(ctx, rt, *site, securityPatchRes); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy host-configuration converge: security post-preflight failed: %v\n", err)
		span.SetStatus(codes.Error, err.Error())
		return 1
	}

	res, err := runSubstrateSitePlaybook(ctx, rt, *site, rr, extraArgs)
	if err != nil || res == nil || res.ExitCode != 0 {
		msg := ansibleFailureMessage(substrateSitePlaybook, res, err)
		fmt.Fprintf(os.Stderr, "verself-deploy host-configuration converge: %s\n", msg)
		span.SetStatus(codes.Error, msg)
		return 1
	}
	span.SetAttributes(
		attribute.Int("ansible.task_count", res.TaskCount),
		attribute.Int("ansible.changed_total", res.ChangedCount),
		attribute.Int("ansible.failed_count", res.FailedCount),
	)
	span.SetStatus(codes.Ok, "")
	return 0
}

func runHostConfigurationSecurityPatch(args []string) int {
	fs := flag.NewFlagSet("host-configuration security-patch", flag.ContinueOnError)
	site := fs.String("site", "prod", "deploy site")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rr, ok := resolveRepoRoot("verself-deploy host-configuration security-patch", *repoRoot)
	if !ok {
		return 1
	}

	snap, err := identity.Generate(identity.GenerateOptions{
		Site:  *site,
		Scope: "security-patch",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy host-configuration security-patch: derive identity: %v\n", err)
		return 1
	}
	snap.ApplyEnv()

	parentCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	securityPatchRes := runSecurityPatchPreflight(parentCtx, *site, rr, snap.RunKey())
	if !securityPatchOK(securityPatchRes) {
		msg := securityPatchFailureMessage(securityPatchRes)
		fmt.Fprintf(os.Stderr, "verself-deploy host-configuration security-patch: %s\n", msg)
		recordSecurityPatchFailureBestEffort(parentCtx, *site, rr, snap.Get("VERSELF_DEPLOY_SHA"), "security-patch", snap, securityPatchRes)
		return 1
	}

	rt, err := runtime.Init(parentCtx, runtime.Options{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Site:           *site,
		RepoRoot:       rr,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy host-configuration security-patch: %v\n", err)
		return 1
	}
	defer rt.Close()

	ctx, span := rt.Tracer.Start(rt.Ctx, "verself_deploy.host_configuration.security_patch",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attribute.String("verself.site", *site)),
	)
	defer span.End()

	if err := runSecurityPostPreflight(ctx, rt, *site, securityPatchRes); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy host-configuration security-patch: post-preflight failed: %v\n", err)
		span.SetStatus(codes.Error, err.Error())
		return 1
	}
	span.SetStatus(codes.Ok, "")
	return 0
}

func runHostConfigurationVerify(args []string) int {
	fs := flag.NewFlagSet("host-configuration verify", flag.ContinueOnError)
	site := fs.String("site", "prod", "deploy site")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rr, ok := resolveRepoRoot("verself-deploy host-configuration verify", *repoRoot)
	if !ok {
		return 1
	}

	snap, err := identity.Generate(identity.GenerateOptions{
		Site:  *site,
		Scope: "host-configuration-verify",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy host-configuration verify: derive identity: %v\n", err)
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
		fmt.Fprintf(os.Stderr, "verself-deploy host-configuration verify: %v\n", err)
		return 1
	}
	defer rt.Close()

	ctx, span := rt.Tracer.Start(rt.Ctx, "verself_deploy.host_configuration.verify",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attribute.String("verself.site", *site)),
	)
	defer span.End()

	res, err := runSubstrateSitePlaybook(ctx, rt, *site, rr, []string{"--syntax-check"})
	if err != nil || res == nil || res.ExitCode != 0 {
		msg := ansibleFailureMessage(substrateSitePlaybook, res, err)
		fmt.Fprintf(os.Stderr, "verself-deploy host-configuration verify: %s\n", msg)
		span.SetStatus(codes.Error, msg)
		return 1
	}
	span.SetStatus(codes.Ok, "")
	return 0
}

func resolveRepoRoot(prefix, repoRoot string) (string, bool) {
	if repoRoot != "" {
		return repoRoot, true
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: cwd: %v\n", prefix, err)
		return "", false
	}
	return cwd, true
}
