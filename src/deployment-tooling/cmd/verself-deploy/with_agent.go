package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/verself/deployment-tooling/internal/runtime"
)

// runWithAgent supervises an arbitrary subprocess under the verself
// OTel agent. The wrapped command inherits OTEL_EXPORTER_OTLP_ENDPOINT
// pointed at the local agent, so its OTel SDK ships spans through
// the same buffered tunnel verself-deploy uses for its own spans.
//
// This is the typed replacement for scripts/with-otel-agent.sh — the
// only generic verb the deploy tooling exposes. Subcommand-specific
// agent wiring (ansible run, nomad deploy-all) goes through
// runtime.Init directly.
//
// Usage:
//   verself-deploy with-agent --site=<site> -- <cmd> [args...]
func runWithAgent(args []string) int {
	fs := flag.NewFlagSet("with-agent", flag.ContinueOnError)
	site := fs.String("site", "prod", "site label (selects inventory and agent queue dir)")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	wrapped := fs.Args()
	if len(wrapped) == 0 {
		fmt.Fprintln(os.Stderr, "verself-deploy with-agent: missing command (use `-- <cmd> [args...]`)")
		return 2
	}

	parentCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rt, err := runtime.Init(parentCtx, runtime.Options{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Site:           *site,
		RepoRoot:       *repoRoot,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy with-agent: %v\n", err)
		return 1
	}
	defer rt.Close()

	cmd := exec.CommandContext(rt.Ctx, wrapped[0], wrapped[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"OTEL_EXPORTER_OTLP_ENDPOINT=http://"+rt.AgentEndpoint(),
		"VERSELF_OTLP_ENDPOINT="+rt.AgentEndpoint(),
	)

	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "verself-deploy with-agent: %v\n", err)
		return 1
	}
	return 0
}
