package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	opch "github.com/verself/operator-runtime/clickhouse"
	opruntime "github.com/verself/operator-runtime/runtime"
)

const (
	discoveryCanaryTarget = "//src/tools/operator/cmd/discovery-canary:discovery-canary"
	discoveryCanaryBin    = "bazel-bin/src/tools/operator/cmd/discovery-canary/discovery-canary_/discovery-canary"
	discoveryCanaryUser   = "billing"
)

type discoveryCanaryOptions struct {
	operatorRuntimeOptions
	duration    time.Duration
	rps         int
	concurrency int
	timeout     time.Duration
	slug        string
	format      string
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
	fs.StringVar(&opts.slug, "slug", "platform", "Org slug to resolve through IAM.")
	fs.StringVar(&opts.format, "format", "json", "Output format on the host: json | table.")
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
	return runOperatorRuntime("canary.service_discovery", opts.operatorRuntimeOptions, totalBudget > operatorCommandBudget, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, _ *opch.Client) error {
		canaryArgs := []string{
			"--duration", opts.duration.String(),
			"--rps", strconv.Itoa(opts.rps),
			"--concurrency", strconv.Itoa(opts.concurrency),
			"--timeout", opts.timeout.String(),
			"--slug", opts.slug,
			"--format", opts.format,
		}
		fmt.Fprintf(os.Stderr, "service-discovery canary: target=%s peer=%s rps=%d duration=%s\n",
			discoveryCanaryTarget, "iam-service", opts.rps, opts.duration)
		return runRemoteBazelExecutable(rt, discoveryCanaryTarget, discoveryCanaryBin, "discovery-canary", discoveryCanaryUser, canaryArgs)
	})
}
