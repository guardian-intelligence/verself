package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/verself/deployment-service/deployengine"
	"github.com/verself/service-runtime/envconfig"
	workloadauth "github.com/verself/service-runtime/workload"
)

type options struct {
	sha          string
	site         string
	repoRoot     string
	r2Addr       string
	nomadAddr    string
	deployRunKey string
	bazelJobs    int
	timeout      time.Duration
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "deployment-recovery-deploy: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	cfg := envconfig.New()
	opts.site = requiredString(cfg, opts.site, "VERSELF_SITE")
	opts.repoRoot = requiredString(cfg, opts.repoRoot, "VERSELF_DEPLOY_REPO_ROOT")
	opts.r2Addr = requiredString(cfg, opts.r2Addr, "VERSELF_R2_CONTROL_PLANE_ADDR")
	opts.nomadAddr = requiredString(cfg, opts.nomadAddr, "VERSELF_NOMAD_ADDR")
	if opts.bazelJobs == 0 {
		opts.bazelJobs, err = parsePositiveInt("VERSELF_DEPLOY_BAZEL_JOBS", cfg.RequireString("VERSELF_DEPLOY_BAZEL_JOBS"))
		if err != nil {
			return err
		}
	}
	if err := cfg.Err(); err != nil {
		return err
	}
	if opts.bazelJobs <= 0 {
		return fmt.Errorf("--bazel-jobs must be positive")
	}
	if opts.deployRunKey == "" {
		opts.deployRunKey, err = randomPrefixedID("recovery")
		if err != nil {
			return err
		}
	}
	spiffeSource, err := workloadauth.Source(ctx, "")
	if err != nil {
		return fmt.Errorf("recovery deploy spiffe source: %w", err)
	}
	defer func() { _ = spiffeSource.Close() }()
	r2HTTPClient, err := workloadauth.MTLSClientForServiceWithTimeouts(spiffeSource, workloadauth.ServiceCloudflareR2, nil, workloadauth.ServiceClientTimeouts{
		Dial:           500 * time.Millisecond,
		TLSHandshake:   time.Second,
		ResponseHeader: 2 * time.Minute,
		Total:          2 * time.Minute,
	})
	if err != nil {
		return fmt.Errorf("recovery deploy R2 control-plane mtls: %w", err)
	}
	runCtx := ctx
	cancel := func() {}
	if opts.timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, opts.timeout)
	}
	defer cancel()
	result, err := deployengine.Run(runCtx, deployengine.Options{
		Site:                     opts.site,
		SHA:                      opts.sha,
		DeployRunKey:             opts.deployRunKey,
		RepoRoot:                 opts.repoRoot,
		R2ControlPlaneAddr:       opts.r2Addr,
		R2ControlPlaneHTTPClient: r2HTTPClient,
		NomadAddr:                opts.nomadAddr,
		BazelBuildFlags:          []string{fmt.Sprintf("--jobs=%d", opts.bazelJobs)},
	})
	if err != nil {
		return err
	}
	fmt.Printf("recovery_deploy_id=%s site=%s sha=%s control_plane_bundle_sha256=%s nomad_submitted_jobs=%d nomad_dispatched_jobs=%d\n",
		opts.deployRunKey,
		result.Site,
		result.SHA,
		result.ControlPlaneSHA256,
		result.NomadSubmittedJobs,
		result.NomadDispatchedJobs,
	)
	return nil
}

func parseOptions(args []string) (options, error) {
	var opts options
	fs := flag.NewFlagSet("deployment-recovery-deploy", flag.ContinueOnError)
	fs.StringVar(&opts.sha, "sha", "", "Git SHA to deploy.")
	fs.StringVar(&opts.site, "site", "", "Deployment site. Defaults to VERSELF_SITE.")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "Verself checkout root. Defaults to VERSELF_DEPLOY_REPO_ROOT.")
	fs.StringVar(&opts.r2Addr, "r2-control-plane-addr", "", "R2 control-plane URL. Defaults to VERSELF_R2_CONTROL_PLANE_ADDR.")
	fs.StringVar(&opts.nomadAddr, "nomad-addr", "", "Nomad API URL. Defaults to VERSELF_NOMAD_ADDR.")
	fs.StringVar(&opts.deployRunKey, "deploy-run-key", "", "Deployment run key. Generated when omitted.")
	fs.IntVar(&opts.bazelJobs, "bazel-jobs", 0, "Bazel job count. Defaults to VERSELF_DEPLOY_BAZEL_JOBS.")
	fs.DurationVar(&opts.timeout, "timeout", 15*time.Minute, "Deployment timeout.")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	opts.sha = strings.ToLower(strings.TrimSpace(opts.sha))
	if opts.sha == "" {
		return options{}, fmt.Errorf("--sha is required")
	}
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	opts.site = strings.TrimSpace(opts.site)
	opts.repoRoot = strings.TrimSpace(opts.repoRoot)
	opts.r2Addr = strings.TrimRight(strings.TrimSpace(opts.r2Addr), "/")
	opts.nomadAddr = strings.TrimRight(strings.TrimSpace(opts.nomadAddr), "/")
	opts.deployRunKey = strings.TrimSpace(opts.deployRunKey)
	return opts, nil
}

func requiredString(cfg *envconfig.Loader, value, env string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return cfg.RequireString(env)
}

func parsePositiveInt(name, raw string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return value, nil
}

func randomPrefixedID(prefix string) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("random deploy id: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(raw[:]), nil
}
