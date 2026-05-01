// Command verself-deploy is the typed orchestrator for verself
// deploys. It owns Bazel-driven artifact discovery (via BEP),
// SSH-tunneled Garage publish, Nomad submit/monitor, and
// ansible-playbook supervision.
//
// Subcommand surface mirrors `aspect <group> <action>`:
//
//	verself-deploy nomad submit     --spec=<path> [--nomad-addr=<url>] [--site=<site>]
//	verself-deploy nomad deploy-all --site=<site> [--repo-root=<path>]
//	verself-deploy ansible run      --site=<site> --layer=<layer> --playbook=<path> --inventory=<dir>
//
// Every subcommand routes through internal/runtime.Init, which owns
// the start ordering: SSH dial → OTLP forward channel →
// otelcol-contrib supervisor → OTel SDK init. Shutdown reverses
// that order so spans drain through the agent's persistent queue
// before the SSH tunnel closes.
package main

import (
	"fmt"
	"os"
)

const (
	serviceName    = "verself-deploy"
	serviceVersion = "0.3.0"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		os.Exit(runRun(os.Args[2:]))
	case "nomad":
		os.Exit(runNomad(os.Args[2:]))
	case "ansible":
		os.Exit(runAnsible(os.Args[2:]))
	case "substrate":
		os.Exit(runSubstrate(os.Args[2:]))
	case "with-agent":
		os.Exit(runWithAgent(os.Args[2:]))
	case "-h", "--help", "help":
		usage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "verself-deploy: unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `verself-deploy — typed orchestrator for verself deploys

usage:
  verself-deploy run                  --site=<site> [--sha=<rev>] [--scope=all|affected] [--substrate=auto|always]
  verself-deploy nomad submit         --spec=<path> [--nomad-addr=<url>] [--site=<site>] [--timeout=5m]
  verself-deploy nomad deploy-all     --site=<site> [--repo-root=<path>]
  verself-deploy ansible run          --site=<site> --layer=<layer> --playbook=<path> --inventory=<dir>
  verself-deploy substrate converge   --site=<site> [--force]
  verself-deploy substrate verify     --site=<site>
  verself-deploy with-agent           --site=<site> -- <cmd> [args...]

`+
		"`run` is the AXL deploy entry point: identity, ledger, layered\nsubstrate, external reconcilers, Nomad fan-out, and the post-deploy\ndivergence canary all happen inside this single process. Spans land\nin default.otel_traces under service.name=verself-deploy.\n")
}

func runNomad(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "verself-deploy nomad: missing subcommand (try `submit` or `deploy-all`)")
		return 2
	}
	switch args[0] {
	case "submit":
		return runNomadSubmit(args[1:])
	case "deploy-all":
		return runNomadDeployAll(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "verself-deploy nomad: unknown subcommand: %s\n", args[0])
		return 2
	}
}
