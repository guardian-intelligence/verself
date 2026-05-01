package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/verself/deployment-tooling/internal/ansible"
	"github.com/verself/deployment-tooling/internal/runtime"
)

// runAnsible is the `verself-deploy ansible <subcommand>` dispatcher.
// Today the only subcommand is `run`; future surfaces (e.g. `recap`,
// `validate`) plug in here.
func runAnsible(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "verself-deploy ansible: missing subcommand (try `run`)")
		return 2
	}
	switch args[0] {
	case "run":
		return runAnsibleRun(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "verself-deploy ansible: unknown subcommand: %s\n", args[0])
		return 2
	}
}

func runAnsibleRun(args []string) int {
	fs := flag.NewFlagSet("ansible run", flag.ContinueOnError)
	site := fs.String("site", "prod", "site label")
	layer := fs.String("layer", "", "substrate layer label (l1_os|l2_userspace|l3_binaries|l4a_components|empty)")
	playbook := fs.String("playbook", "", "playbook path relative to --ansible-dir")
	inventory := fs.String("inventory", "", "absolute inventory path or directory")
	ansibleDir := fs.String("ansible-dir", "", "working dir for ansible-playbook (defaults to <repo>/src/substrate/ansible)")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	changedFile := fs.String("changed-file", "", "write the PLAY RECAP changed total here (compat with run-layer.sh)")
	var extraArgs stringSliceFlag
	fs.Var(&extraArgs, "ansible-arg", "extra arg passed through to ansible-playbook (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *playbook == "" {
		fmt.Fprintln(os.Stderr, "verself-deploy ansible run: --playbook is required")
		return 2
	}
	if *inventory == "" {
		fmt.Fprintln(os.Stderr, "verself-deploy ansible run: --inventory is required")
		return 2
	}
	rr := *repoRoot
	if rr == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy ansible run: cwd: %v\n", err)
			return 1
		}
		rr = cwd
	}
	ad := *ansibleDir
	if ad == "" {
		ad = filepath.Join(rr, "src", "substrate", "ansible")
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
		fmt.Fprintf(os.Stderr, "verself-deploy ansible run: %v\n", err)
		return 1
	}
	defer rt.Close()

	res, err := ansible.Run(rt.Ctx, rt.ClickHouse, ansible.Options{
		Playbook:      *playbook,
		Inventory:     resolveInventoryPath(*inventory),
		AnsibleDir:    ad,
		Site:          *site,
		Layer:         *layer,
		ExtraArgs:     extraArgs,
		AgentEndpoint: rt.AgentEndpoint(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy ansible run: %v\n", err)
		return 1
	}

	if err := ansible.WriteChangedFile(*changedFile, res.ChangedCount); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy ansible run: %v\n", err)
		// Don't override the playbook's exit code — the legacy bash
		// pipeline only consumed the file in the success path.
	}
	return res.ExitCode
}

// resolveInventoryPath accepts either a directory (as `aspect render`
// produces under .cache/render/<site>/inventory) or a hosts.ini file
// directly. Ansible itself accepts both, but the parser we hand to
// runtime.Init expects an absolute path; mirror the existing bash
// hosts.ini fallback for clarity in error messages.
func resolveInventoryPath(p string) string {
	if !filepath.IsAbs(p) {
		// Caller bug — surface with a clear message at the next step
		// rather than silently rewriting.
		return p
	}
	st, err := os.Stat(p)
	if err == nil && st.IsDir() {
		return filepath.Join(p, "hosts.ini")
	}
	return p
}

// stringSliceFlag is a repeatable string flag (like --ansible-arg).
// flag.Var requires a flag.Value, which a []string does not satisfy
// directly.
type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return fmt.Sprintf("%v", []string(*s)) }
func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}
