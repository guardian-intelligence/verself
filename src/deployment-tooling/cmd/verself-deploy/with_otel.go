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

// runWithOTel supervises an arbitrary subprocess inside a verself
// SSH-forwarded OTLP channel. The wrapped command inherits
// OTEL_EXPORTER_OTLP_ENDPOINT pointed at the forward's loopback
// listen address, so its OTel SDK ships spans through the same
// tunnel verself-deploy uses for its own spans.
//
// This is the generic verb for "run something controller-side and
// keep its OTel traces on the same path." Subcommand-specific OTel
// wiring (ansible run, nomad deploy-all) goes through runtime.Init
// directly.
//
// Usage:
//
//	verself-deploy with-otel --site=<site> -- <cmd> [args...]
func runWithOTel(args []string) int {
	fs := flag.NewFlagSet("with-otel", flag.ContinueOnError)
	site := fs.String("site", "prod", "site label (selects inventory)")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	wrapped := fs.Args()
	if len(wrapped) == 0 {
		fmt.Fprintln(os.Stderr, "verself-deploy with-otel: missing command (use `-- <cmd> [args...]`)")
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
		fmt.Fprintf(os.Stderr, "verself-deploy with-otel: %v\n", err)
		return 1
	}
	defer rt.Close()

	cmd := exec.CommandContext(rt.Ctx, wrapped[0], wrapped[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"OTEL_EXPORTER_OTLP_ENDPOINT=http://"+rt.OTLPEndpoint(),
		"VERSELF_OTLP_ENDPOINT="+rt.OTLPEndpoint(),
	)

	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "verself-deploy with-otel: %v\n", err)
		return 1
	}
	return 0
}
