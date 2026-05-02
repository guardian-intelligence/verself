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
// Source of truth for principals, slot count, and well-known paths:
// src/cue-renderer/instances/prod/{config,operators}.cue.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
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
	case "-h", "--help", "help":
		printUsage(os.Stdout)
		return nil
	default:
		printUsage(os.Stderr)
		return fmt.Errorf("unknown subcommand: %s", sub)
	}
}

func printUsage(w *os.File) {
	fmt.Fprint(w, `aspect-operator <subcommand> [flags]

Subcommands:
  onboard           Interactive: keygen + trust-anchor fetch + OIDC + first cert
  refresh           Non-interactive: renew Vault token + re-sign SSH cert
  enroll-workload   Operator-side: claim slot + mint AppRole secret-id

Run 'aspect-operator <subcommand> -h' for subcommand-specific flags.
`)
}

// flagSet returns a FlagSet that prints to stderr and exits non-zero on
// error. Each subcommand wires it up the same way; callers parse rest
// args after their typed flags.
func flagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}
