package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/verself/deployment-service/siteconfig"
)

type checkOptions struct {
	Site     string
	RepoRoot string
}

func runCheck(args []string) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	site := fs.String("site", "prod", "deployment site")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rr := *repoRoot
	if rr == "" {
		cwd, err := os.Getwd()
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "verself-deploy check: cwd: %v\n", err)
			return 1
		}
		rr = cwd
	}
	if err := check(context.Background(), checkOptions{Site: *site, RepoRoot: rr}); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "verself-deploy check: %v\n", err)
		return 1
	}
	return 0
}

func check(ctx context.Context, opts checkOptions) error {
	if strings.TrimSpace(opts.Site) == "" {
		return fmt.Errorf("site is required")
	}
	model, err := siteconfig.Load(opts.RepoRoot, opts.Site)
	if err != nil {
		return err
	}
	baseURL := deploymentServiceBaseURL(model)
	if err := deploymentServiceReachable(ctx, baseURL); err != nil {
		return err
	}
	token, err := deploymentBearerToken(ctx, baseURL)
	if err != nil {
		return err
	}
	if err := bootstrapCheck(ctx, baseURL, token); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "deployment_service=%s healthz=ok bootstrap=ok\n", baseURL); err != nil {
		return fmt.Errorf("write deployment check response: %w", err)
	}
	return nil
}
