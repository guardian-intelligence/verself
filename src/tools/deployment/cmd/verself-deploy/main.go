// Command verself-deploy is the deployment-service client used by aspect deploy.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "check":
		os.Exit(runCheck(os.Args[2:]))
	case "run":
		os.Exit(runRun(os.Args[2:]))
	case "status":
		os.Exit(runStatus(os.Args[2:]))
	case "validate":
		os.Exit(runValidate(os.Args[2:]))
	case "-h", "--help", "help":
		usage()
		os.Exit(0)
	default:
		_, _ = fmt.Fprintf(os.Stderr, "verself-deploy: unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	_, _ = fmt.Fprint(os.Stderr, `verself-deploy

usage:
  verself-deploy check --site=<site> [--repo-root=<path>]
  verself-deploy run --site=<site> [--sha=<rev>] [--repo-root=<path>]
  verself-deploy status --site=<site> --deployment-id=<id> [--events]
  verself-deploy validate --site=<site> [--repo-root=<path>]

The check command verifies that the selected site's deployment-service
endpoint is reachable, authenticates, and runs the read-only S0-S7 bootstrap
readiness check without submitting a deployment.

The run command checks that the selected site is past S7
deployment-service-ready, authenticates the request, and submits the desired
Git SHA to that site's deployment-service. It does not SSH, run recovery, or
mutate Nomad directly.

The status command fetches deployment state and, with --events, the deployment
event ledger from that site's deployment-service. It does not SSH, run recovery,
or query Nomad directly.

The validate command checks site metadata, provider catalog entries,
owner-local deploy declarations, deploy gates, and authored Nomad specs without
opening a host connection.
`)
}
