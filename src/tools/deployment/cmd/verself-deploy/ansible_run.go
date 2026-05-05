package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/verself/deployment-tools/internal/ansible"
	"github.com/verself/deployment-tools/internal/identity"
	"github.com/verself/deployment-tools/internal/runtime"
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
	phase := fs.String("phase", "ad-hoc", "Ansible phase label for telemetry")
	playbook := fs.String("playbook", "", "playbook path relative to --ansible-dir")
	inventory := fs.String("inventory", "", "absolute inventory path or directory")
	ansibleDir := fs.String("ansible-dir", "", "working dir for ansible-playbook (defaults to <repo>/src/host-configuration/ansible)")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
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
		ad = filepath.Join(rr, "src", "host-configuration", "ansible")
	}

	snap, err := identity.Generate(identity.GenerateOptions{
		Site:  *site,
		Scope: "ansible",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy ansible run: derive identity: %v\n", err)
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
		fmt.Fprintf(os.Stderr, "verself-deploy ansible run: %v\n", err)
		return 1
	}
	defer rt.Close()

	res, err := ansible.Run(rt.Ctx, rt.DeployDB, ansible.Options{
		Playbook:     *playbook,
		Inventory:    resolveInventoryPath(*inventory),
		AnsibleDir:   ad,
		Site:         *site,
		Phase:        *phase,
		RunKey:       rt.Identity.RunKey(),
		ExtraArgs:    append(extraArgs, "-e", fmt.Sprintf("ansible_port=%d", rt.SSHPort)),
		OTLPEndpoint: rt.OTLPEndpoint(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy ansible run: %v\n", err)
		return 1
	}

	return res.ExitCode
}

// resolveInventoryPath accepts either a directory or an inventory file
// directly. Ansible itself accepts both, but the parser we hand to
// runtime.Init expects an absolute path; keep the hosts.ini fallback for
// one-shot callers that stage an inventory directory.
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
