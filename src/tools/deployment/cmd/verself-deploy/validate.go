package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/verself/deployment-service/deploycontract"
	"github.com/verself/deployment-service/siteconfig"
)

type validateOptions struct {
	RepoRoot string
	Site     string
}

func runValidate(args []string) int {
	var opts validateOptions
	fs := flag.NewFlagSet("verself-deploy validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&opts.RepoRoot, "repo-root", ".", "Repository root.")
	fs.StringVar(&opts.Site, "site", "prod", "Deployment site.")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if opts.Site == "" {
		fmt.Fprintln(os.Stderr, "validate: --site is required")
		return 2
	}
	if _, err := siteconfig.Load(opts.RepoRoot, opts.Site); err != nil {
		fmt.Fprintln(os.Stderr, "validate:", err)
		return 1
	}
	if _, err := deploycontract.ValidateRepo(opts.RepoRoot); err != nil {
		fmt.Fprintln(os.Stderr, "validate:", err)
		return 1
	}
	if _, err := fmt.Fprintf(os.Stdout, "site %s deployment contracts are valid\n", opts.Site); err != nil {
		fmt.Fprintln(os.Stderr, "validate:", err)
		return 1
	}
	return 0
}
