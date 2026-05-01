package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tooling/internal/identity"
	"github.com/verself/deployment-tooling/internal/layers"
	"github.com/verself/deployment-tooling/internal/ledger"
	"github.com/verself/deployment-tooling/internal/runtime"
)

// runSubstrate is the `verself-deploy substrate <subcommand>`
// dispatcher. Substrate operations are the per-layer primitives
// `aspect substrate ...` exposes; full deploys go through `run`.
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

// runSubstrateConverge walks the layered substrate plan once, with
// or without hash-gating. It does NOT emit deploy_events rows —
// that's `verself-deploy run`'s job.
func runSubstrateConverge(args []string) int {
	fs := flag.NewFlagSet("substrate converge", flag.ContinueOnError)
	site := fs.String("site", "prod", "deploy site")
	force := fs.Bool("force", false, "ignore the per-layer hash gate; re-run every layer")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	var extraArgs stringSliceFlag
	fs.Var(&extraArgs, "ansible-arg", "extra arg passed to ansible-playbook for every layer (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rr := *repoRoot
	if rr == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy substrate converge: cwd: %v\n", err)
			return 1
		}
		rr = cwd
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
		trace.WithAttributes(
			attribute.String("verself.site", *site),
			attribute.Bool("verself.force", *force),
		),
	)
	defer span.End()

	inventoryDir := filepath.Join(rr, ".cache", "render", *site, "inventory")
	if _, err := os.Stat(inventoryDir); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy substrate converge: rendered inventory missing at %s: %v\n", inventoryDir, err)
		span.SetStatus(codes.Error, "missing inventory")
		return 1
	}
	ansibleDir := filepath.Join(rr, "src", "substrate", "ansible")

	res := layers.RunAll(ctx, layers.Options{
		Site:             *site,
		RepoRoot:         rr,
		AnsibleDir:       ansibleDir,
		Inventory:        inventoryDir,
		Force:            *force,
		OTLPEndpoint:     rt.OTLPEndpoint(),
		ChWriter:         rt.ClickHouse,
		Ledger:           ledger.New(rt.ClickHouse),
		ExtraAnsibleArgs: extraArgs,
	})
	if res.Err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy substrate converge: %v failed: %v\n", res.FailedLayer, res.Err)
		span.SetStatus(codes.Error, res.Err.Error())
		return 1
	}
	span.SetStatus(codes.Ok, "")
	span.SetAttributes(
		attribute.StringSlice("verself.layers_ran", res.LayersRan),
		attribute.StringSlice("verself.layers_skipped", res.LayersSkipped),
	)
	return 0
}

// runSubstrateVerify reports each layer's current input_hash vs its
// last_applied_hash. Exits 10 on at least one mismatch — same
// contract `aspect substrate verify` had via the bash script.
func runSubstrateVerify(args []string) int {
	fs := flag.NewFlagSet("substrate verify", flag.ContinueOnError)
	site := fs.String("site", "prod", "deploy site")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rr := *repoRoot
	if rr == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy substrate verify: cwd: %v\n", err)
			return 1
		}
		rr = cwd
	}

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

	digests, err := layers.LayerDigests(ctx, rr, *site)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy substrate verify: %v\n", err)
		span.SetStatus(codes.Error, err.Error())
		return 1
	}
	led := ledger.New(rt.ClickHouse)
	mismatched := []string{}
	for _, layer := range layers.Plan {
		current := digests[layer.Name]
		last, err := led.LastAppliedHash(ctx, *site, layer.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy substrate verify: %s last-applied query failed: %v\n", layer.Name, err)
			span.SetStatus(codes.Error, err.Error())
			return 1
		}
		if last == current {
			fmt.Fprintf(os.Stderr, "[%s] fresh (%s…)\n", layer.Name, current[:12])
			continue
		}
		shortLast := "<none>"
		if last != "" {
			shortLast = last[:12] + "…"
		}
		fmt.Fprintf(os.Stderr, "[%s] STALE current=%s… last_applied=%s\n", layer.Name, current[:12], shortLast)
		mismatched = append(mismatched, layer.Name)
	}
	span.SetAttributes(attribute.StringSlice("verself.mismatched_layers", mismatched))
	if len(mismatched) > 0 {
		span.SetStatus(codes.Error, "stale layers")
		return 10
	}
	span.SetStatus(codes.Ok, "")
	return 0
}
