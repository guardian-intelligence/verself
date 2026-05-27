// Command aspect-operator exposes operator-side helpers for the Verself
// bare-metal node.
//
// Subcommands:
//
//	aspect-operator db pg|ch|tb ...
//	    Operator-side database access over the configured SSH access plane.
//
//	aspect-operator detect-intrusions
//	    Query accepted SSH auth events that bypassed Pomerium.
//
//	aspect-operator billing seed|clock|state|documents|finalizations|events
//	    Operator billing fixture and inspection tooling.
//
//	aspect-operator persona assume|user-state
//	    Operator persona credential and fixture tooling.
//
//	aspect-operator mail addresses
//	    Operator email inventory helpers.
//
//	aspect-operator device
//	    Configure this checkout/device to reach the operator access plane.
//
//	aspect-operator service-discovery-canary
//	    Drive billing API traffic through the IAM authorize round-trip while
//	    iam-service rolls; report any non-2xx-or-4xx outcome as a deploy gap.
//
//	aspect-operator release npm-probe
//	    Probe npm registry state for SDK release publication.
//
//	aspect-operator release mksk
//	    Dispatch make-skill release workflows through distribution-service.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

const (
	serviceName    = "aspect-operator"
	serviceVersion = "dev"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		var exit exitError
		if errors.As(err, &exit) {
			os.Exit(exit.code)
		}
		fmt.Fprintln(os.Stderr, "aspect-operator: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage(os.Stderr)
		return fmt.Errorf("missing subcommand")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "db":
		return cmdDB(rest)
	case "detect-intrusions":
		return cmdDetectIntrusions(rest)
	case "billing":
		return cmdBilling(rest)
	case "persona":
		return cmdPersona(rest)
	case "mail":
		return cmdMail(rest)
	case "device":
		return cmdDevice(rest)
	case "promote-org":
		return cmdPromoteOrg(rest)
	case "service-discovery-canary":
		return cmdDiscoveryCanary(rest)
	case "release":
		return cmdRelease(rest)
	case "-h", "--help", "help":
		printUsage(os.Stdout)
		return nil
	default:
		printUsage(os.Stderr)
		return fmt.Errorf("unknown subcommand: %s", sub)
	}
}

func printUsage(w *os.File) {
	_, _ = fmt.Fprint(w, `aspect-operator <subcommand> [flags]

Subcommands:
  db                Operator database access
  detect-intrusions Scan accepted SSH auth events that bypassed Pomerium
  billing           Billing fixture and inspection tooling
  persona           Persona credential and fixture tooling
  mail              Mail operator helpers
  device            Configure this device for operator access
  promote-org       Promote an existing organization trust tier
  service-discovery-canary  Drive billing -> IAM traffic through the resolver
  release           Release publication helpers

Run 'aspect-operator <subcommand> -h' for subcommand-specific flags.
`)
}

type exitError struct {
	code int
}

func (e exitError) Error() string {
	return fmt.Sprintf("exit %d", e.code)
}

// flagSet returns a FlagSet that prints to stderr and exits non-zero on
// error. Each subcommand wires it up the same way; callers parse rest
// args after their typed flags.
func flagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}
