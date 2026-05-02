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

// runSubstrate is the `verself-deploy substrate <subcommand>` dispatcher.
// Substrate operations are direct executions of the canonical Ansible site
// playbook; deploy orchestration still goes through `run`.
func runSubstrate(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "verself-deploy substrate: missing subcommand (try `converge` or `verify`)")
		return 2
	}
	switch args[0] {
	case "converge":
		return runSubstrateConverge(args[1:])
	case "verify":
		return runSubstrateVerify(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "verself-deploy substrate: unknown subcommand: %s\n", args[0])
		return 2
	}
}

func runSubstrateConverge(args []string) int {
	fs := flag.NewFlagSet("substrate converge", flag.ContinueOnError)
	site := fs.String("site", "prod", "deploy site")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	var extraArgs stringSliceFlag
	fs.Var(&extraArgs, "ansible-arg", "extra arg passed to ansible-playbook (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rr, ok := resolveRepoRoot("verself-deploy substrate converge", *repoRoot)
	if !ok {
		return 1
	}

	snap, err := identity.Generate(identity.GenerateOptions{
		Site:  *site,
		Scope: "substrate",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy substrate converge: derive identity: %v\n", err)
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
		fmt.Fprintf(os.Stderr, "verself-deploy substrate converge: %v\n", err)
		return 1
	}
	defer rt.Close()

	ctx, span := rt.Tracer.Start(rt.Ctx, "verself_deploy.substrate.converge",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attribute.String("verself.site", *site)),
	)
	defer span.End()

	res, err := runSubstrateSitePlaybook(ctx, rt, *site, rr, extraArgs)
	if err != nil || res == nil || res.ExitCode != 0 {
		msg := ansibleFailureMessage(substrateSitePlaybook, res, err)
		fmt.Fprintf(os.Stderr, "verself-deploy substrate converge: %s\n", msg)
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

func runSubstrateVerify(args []string) int {
	fs := flag.NewFlagSet("substrate verify", flag.ContinueOnError)
	site := fs.String("site", "prod", "deploy site")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rr, ok := resolveRepoRoot("verself-deploy substrate verify", *repoRoot)
	if !ok {
		return 1
	}

	snap, err := identity.Generate(identity.GenerateOptions{
		Site:  *site,
		Scope: "substrate-verify",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy substrate verify: derive identity: %v\n", err)
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
		fmt.Fprintf(os.Stderr, "verself-deploy substrate verify: %v\n", err)
		return 1
	}
	defer rt.Close()

	ctx, span := rt.Tracer.Start(rt.Ctx, "verself_deploy.substrate.verify",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attribute.String("verself.site", *site)),
	)
	defer span.End()

	res, err := runSubstrateSitePlaybook(ctx, rt, *site, rr, []string{"--syntax-check"})
	if err != nil || res == nil || res.ExitCode != 0 {
		msg := ansibleFailureMessage(substrateSitePlaybook, res, err)
		fmt.Fprintf(os.Stderr, "verself-deploy substrate verify: %s\n", msg)
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
