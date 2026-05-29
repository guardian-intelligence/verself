package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	opch "github.com/verself/operator-runtime/clickhouse"
	opruntime "github.com/verself/operator-runtime/runtime"
)

const (
	discoveryCanaryTarget = "//src/tools/operator/cmd/discovery-canary:discovery-canary"
	discoveryCanaryUser   = "billing"
)

var discoveryCanaryOrgIDRE = regexp.MustCompile(`^org_[0-9A-HJKMNP-TV-Z]{26}$`)

type discoveryCanaryOptions struct {
	operatorRuntimeOptions
	duration    time.Duration
	rps         int
	concurrency int
	timeout     time.Duration
	slug        string
	format      string
	authzOrgID  string
}

type discoveryCanarySiteVars struct {
	ServiceDiscoveryCanaryOrgSlug string `yaml:"service_discovery_canary_org_slug"`
}

func cmdDiscoveryCanary(args []string) error {
	fs := flagSet("service-discovery-canary")
	opts := discoveryCanaryOptions{}
	fs.StringVar(&opts.repoRoot, "repo-root", "", "Repository root.")
	fs.StringVar(&opts.site, "site", "", "Deployment site.")
	fs.DurationVar(&opts.duration, "duration", 180*time.Second, "How long to drive traffic.")
	fs.IntVar(&opts.rps, "rps", 10, "Target requests per second.")
	fs.IntVar(&opts.concurrency, "concurrency", 4, "Number of parallel workers on the host.")
	fs.DurationVar(&opts.timeout, "timeout", 5*time.Second, "Per-request timeout.")
	fs.StringVar(&opts.slug, "slug", "", "Org slug to resolve through IAM.")
	fs.StringVar(&opts.format, "format", "json", "Output format on the host: json | table.")
	fs.StringVar(&opts.authzOrgID, "authz-org-id", "", "Canonical organization ID fallback used for the authorization probe.")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	addOperatorRuntimeFlags(&opts.operatorRuntimeOptions)
	totalBudget := opts.duration + 60*time.Second
	if totalBudget < 2*time.Minute {
		totalBudget = 2 * time.Minute
	}
	return runOperatorRuntime("canary.service_discovery", opts.operatorRuntimeOptions, totalBudget, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, _ *opch.Client) error {
		var vars discoveryCanarySiteVars
		if strings.TrimSpace(opts.slug) == "" {
			if err := readYAMLFile(siteVarsPath(rt.RepoRoot, rt.Site), &vars); err != nil {
				return err
			}
		}
		authzOrgID := strings.TrimSpace(opts.authzOrgID)
		if authzOrgID != "" && !discoveryCanaryOrgIDRE.MatchString(authzOrgID) {
			return fmt.Errorf("service-discovery canary: authz org id must match %s", discoveryCanaryOrgIDRE.String())
		}
		slug := strings.TrimSpace(opts.slug)
		if slug == "" {
			slug = strings.TrimSpace(vars.ServiceDiscoveryCanaryOrgSlug)
		}
		if slug == "" {
			return fmt.Errorf("service-discovery canary: org slug is required")
		}
		canaryArgs := []string{
			"--duration", opts.duration.String(),
			"--rps", strconv.Itoa(opts.rps),
			"--concurrency", strconv.Itoa(opts.concurrency),
			"--timeout", opts.timeout.String(),
			"--slug", slug,
			"--format", opts.format,
		}
		if authzOrgID != "" {
			canaryArgs = append(canaryArgs, "--authz-org-id", authzOrgID)
		}
		fmt.Fprintf(os.Stderr, "service-discovery canary: target=%s peer=%s rps=%d duration=%s\n",
			discoveryCanaryTarget, "iam-service", opts.rps, opts.duration)
		return runRemoteBazelExecutable(rt, discoveryCanaryTarget, "discovery-canary", discoveryCanaryUser, canaryArgs)
	})
}
