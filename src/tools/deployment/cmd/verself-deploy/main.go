// Command verself-deploy is the Bazel-to-Nomad deploy adapter.
package main

import (
	"fmt"
	"os"
)

const (
	serviceName    = "verself-deploy"
	serviceVersion = "0.5.0"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "canary":
		os.Exit(runCanary(os.Args[2:]))
	case "run":
		os.Exit(runRun(os.Args[2:]))
	case "validate":
		os.Exit(runValidate(os.Args[2:]))
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
	fmt.Fprint(os.Stderr, `verself-deploy

usage:
  verself-deploy canary --site=<site> --size=medium|large|all [--repo-root=<path>]
  verself-deploy run --site=<site> [--sha=<rev>] [--repo-root=<path>]
  verself-deploy validate --site=<site> [--repo-root=<path>]

The run command assumes host bootstrap is complete. It discovers Bazel
nomad_component targets, runs component-owned tests, builds their artifacts,
publishes missing artifacts, submits changed Nomad jobs by deploy wave,
monitors deployments, runs selected post-deploy canaries, reverts changed jobs
when canaries fail, and emits deploy spans.

The canary command runs declared post-deploy checks against the current site
without submitting or reverting Nomad jobs.

The validate command checks site metadata, provider catalog entries,
owner-local deploy declarations, deploy gates, and authored Nomad specs without
opening a host connection.
`)
}
