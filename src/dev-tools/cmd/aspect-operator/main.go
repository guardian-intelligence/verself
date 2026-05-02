// Command aspect-operator manages operator-device + workload-VM
// onboarding for the Verself bare-metal node.
//
// Subcommands:
//
//	aspect-operator onboard --device=<name>
//	    Interactive: generate ed25519 SSH + WireGuard keypairs,
//	    fetch trust anchors from /.well-known/verself-*, configure
//	    wg-ops locally, run OIDC, sign first SSH cert, drop SSH
//	    config alias.
//
//	aspect-operator refresh
//	    Non-interactive: renew the cached Vault token and re-sign
//	    the SSH cert. Fails loudly with a clear recovery message
//	    when the token has exceeded token_explicit_max_ttl (30d
//	    by default) and OIDC re-auth is required. Invoked by
//	    aspect deploy pre-flight.
//
//	aspect-operator enroll-workload [--slot=<n>]
//	    Operator-side: claim a workload-pool slot, mint a 15-min
//	    single-use AppRole secret-id, print an env block to feed
//	    into a Devin / Cursor / CI VM.
//
//	aspect-operator db pg|ch|tb ...
//	    Operator-side database access over the operator SSH cert.
//
//	aspect-operator detect-intrusions
//	    Query accepted SSH auth events outside the authored trust set.
//
//	aspect-operator billing seed|clock|state|documents|finalizations|events
//	    Operator billing fixture and inspection tooling.
//
//	aspect-operator persona assume|user-state
//	    Operator persona credential and fixture tooling.
//
//	aspect-operator mail send|passwords
//	    Operator mail helpers.
//
//	aspect-operator dev verself-web
//	    Operator local-development tunnel helpers.
//
// Source of truth for principals, slot count, and well-known paths:
// src/host-configuration/ansible/group_vars/all/generated/ops.yml.
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
	case "onboard":
		return cmdOnboard(rest)
	case "refresh":
		return cmdRefresh(rest)
	case "enroll-workload":
		return cmdEnrollWorkload(rest)
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
	case "dev":
		return cmdDev(rest)
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
  onboard           Interactive: keygen + trust-anchor fetch + OIDC + first cert
  refresh           Non-interactive: renew Vault token + re-sign SSH cert
  enroll-workload   Operator-side: claim slot + mint AppRole secret-id
  db                Operator database access
  detect-intrusions Scan accepted SSH auth events outside authored trust
  billing           Billing fixture and inspection tooling
  persona           Persona credential and fixture tooling
  mail              Mail operator helpers
  dev               Local development helpers

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
