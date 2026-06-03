package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/verself/deployment-service/deployengine"
	"github.com/verself/deployment-service/deployengine/artifactpublish"
	objectstorageclient "github.com/verself/object-storage-service/client"
	"github.com/verself/service-runtime/envconfig"
	workloadauth "github.com/verself/service-runtime/workload"
)

type options struct {
	sha               string
	site              string
	repoRoot          string
	objectStorageAddr string
	nomadAddr         string
	deployRunKey      string
	bazelJobs         int
	timeout           time.Duration
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
	opts.objectStorageAddr = requiredString(cfg, opts.objectStorageAddr, "VERSELF_OBJECT_STORAGE_ADDR")
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
	objectStorageHTTPClient, err := workloadauth.MTLSClientForServiceWithTimeouts(spiffeSource, workloadauth.ServiceObjectStorageAdmin, nil, workloadauth.ServiceClientTimeouts{
		Dial:           500 * time.Millisecond,
		TLSHandshake:   time.Second,
		ResponseHeader: 2 * time.Minute,
		Total:          2 * time.Minute,
	})
	if err != nil {
		return fmt.Errorf("recovery deploy object-storage mtls: %w", err)
	}
	objectStorageClient, err := objectstorageclient.NewClient(opts.objectStorageAddr, objectstorageclient.WithHTTPClient(objectStorageHTTPClient))
	if err != nil {
		return err
	}
	runCtx := ctx
	cancel := func() {}
	if opts.timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, opts.timeout)
	}
	defer cancel()
	if err := checkoutSourceSHA(runCtx, opts.repoRoot, opts.sha); err != nil {
		return err
	}
	labels, err := deployengine.QueryNomadComponentLabels(runCtx, opts.repoRoot)
	if err != nil {
		return err
	}
	descriptors, err := deployengine.BuildNomadComponentDescriptors(runCtx, opts.repoRoot, labels, fmt.Sprintf("--jobs=%d", opts.bazelJobs))
	if err != nil {
		return err
	}
	result, err := deployengine.Submit(runCtx, deployengine.Options{
		Site:                      opts.site,
		ArtifactNamespace:         opts.sha,
		DeployRunKey:              opts.deployRunKey,
		RepoRoot:                  opts.repoRoot,
		NomadComponentDescriptors: descriptors,
		ArtifactPublisher:         artifactpublish.Publisher{Client: objectStorageClient},
		NomadAddr:                 opts.nomadAddr,
	})
	if err != nil {
		return err
	}
	fmt.Printf("recovery_deploy_id=%s site=%s sha=%s nomad_submitted_jobs=%d\n",
		opts.deployRunKey,
		result.Site,
		opts.sha,
		result.NomadSubmittedJobs,
	)
	return nil
}

func checkoutSourceSHA(ctx context.Context, repoRoot, sha string) error {
	if err := gitRun(ctx, repoRoot, "rev-parse", "--is-inside-work-tree"); err != nil {
		return err
	}
	status, err := gitOutput(ctx, repoRoot, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("deployment source repo root %s is dirty", repoRoot)
	}
	current, err := gitOutput(ctx, repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if strings.EqualFold(current, sha) {
		return nil
	}
	if err := gitRun(ctx, repoRoot, "fetch", "--no-tags", "--depth=1", "origin", sha); err != nil {
		if fallbackErr := gitRun(ctx, repoRoot, "fetch", "--no-tags", "origin"); fallbackErr != nil {
			return fmt.Errorf("%w; fallback fetch failed: %v", err, fallbackErr)
		}
	}
	if err := gitRun(ctx, repoRoot, "checkout", "--detach", sha); err != nil {
		return err
	}
	current, err = gitOutput(ctx, repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if !strings.EqualFold(current, sha) {
		return fmt.Errorf("deployment source checkout resolved %s, expected %s", current, sha)
	}
	return nil
}

func gitRun(ctx context.Context, repoRoot string, args ...string) error {
	_, err := gitOutput(ctx, repoRoot, args...)
	return err
}

func gitOutput(ctx context.Context, repoRoot string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", repoRoot}, args...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(cmdArgs, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func parseOptions(args []string) (options, error) {
	var opts options
	fs := flag.NewFlagSet("deployment-recovery-deploy", flag.ContinueOnError)
	fs.StringVar(&opts.sha, "sha", "", "Git SHA to deploy.")
	fs.StringVar(&opts.site, "site", "", "Deployment site. Defaults to VERSELF_SITE.")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "Verself checkout root. Defaults to VERSELF_DEPLOY_REPO_ROOT.")
	fs.StringVar(&opts.objectStorageAddr, "object-storage-addr", "", "object-storage internal URL. Defaults to VERSELF_OBJECT_STORAGE_ADDR.")
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
	opts.objectStorageAddr = strings.TrimRight(strings.TrimSpace(opts.objectStorageAddr), "/")
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
