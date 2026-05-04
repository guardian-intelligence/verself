package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/bazelbuild"
	"github.com/verself/deployment-tools/internal/garage"
	"github.com/verself/deployment-tools/internal/identity"
	"github.com/verself/deployment-tools/internal/nomadclient"
	"github.com/verself/deployment-tools/internal/nomadrelease"
	"github.com/verself/deployment-tools/internal/runtime"
	"github.com/verself/deployment-tools/internal/sshtun"
)

const (
	// defaultNomadRemotePort matches the loopback Nomad agent port the substrate role binds.
	defaultNomadRemotePort = 4646

	nomadReleaseSubmitTimeout = 5 * time.Minute
	nomadReleaseMaxBytes      = 64 * 1024 * 1024
)

func runRelease(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "verself-deploy release: missing subcommand (try `publish`)")
		return 2
	}
	switch args[0] {
	case "publish":
		return runReleasePublish(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "verself-deploy release: unknown subcommand: %s\n", args[0])
		return 2
	}
}

func runReleasePublish(args []string) int {
	fs := flag.NewFlagSet("release publish", flag.ContinueOnError)
	site := fs.String("site", "prod", "site whose Nomad release should be built and published")
	rawSHA := fs.String("sha", "HEAD", "git SHA or ref to publish")
	repoRoot := fs.String("repo-root", "", "path to the verself-sh repo root (defaults to cwd)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *repoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy release publish: cwd: %v\n", err)
			return 1
		}
		*repoRoot = cwd
	}
	absRepoRoot, err := filepath.Abs(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy release publish: repo-root: %v\n", err)
		return 1
	}
	*repoRoot = absRepoRoot
	if err := os.Chdir(*repoRoot); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy release publish: chdir %s: %v\n", *repoRoot, err)
		return 1
	}

	sha, err := resolveGitSHA(*repoRoot, *rawSHA)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy release publish: %v\n", err)
		return 1
	}
	if err := ensureRepoAtSHA(*repoRoot, sha); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy release publish: %v\n", err)
		return 1
	}

	snap, err := identity.Generate(identity.GenerateOptions{
		Site:  *site,
		Sha:   sha,
		Scope: "release",
		Kind:  "nomad-release-publish",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy release publish: identity: %v\n", err)
		return 1
	}
	snap.ApplyEnv()

	parentCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rt, err := runtime.Init(parentCtx, runtime.Options{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Site:           *site,
		RepoRoot:       *repoRoot,
		SkipClickHouse: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy release publish: %v\n", err)
		return 1
	}
	defer rt.Close()

	ctx, span := rt.Tracer.Start(rt.Ctx, "verself_deploy.release.publish",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("verself.site", *site),
			attribute.String("verself.deploy_sha", sha),
			attribute.String("verself.repo_root", *repoRoot),
		),
	)
	defer span.End()

	if err := publishNomadRelease(ctx, rt, span, *site, *repoRoot, sha); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		fmt.Fprintf(os.Stderr, "verself-deploy release publish: %v\n", err)
		return 1
	}
	span.SetStatus(codes.Ok, "")
	return 0
}

func publishNomadRelease(ctx context.Context, rt *runtime.Runtime, span trace.Span, site, repoRoot, sha string) error {
	closeViteplusRegistry, err := prepareViteplusWorkspace(ctx, rt, repoRoot)
	if err != nil {
		return err
	}
	defer closeViteplusRegistry()

	target := releaseTarget(site)
	build, err := bazelbuild.Build(ctx, repoRoot, []string{target}, "--config=remote-writer")
	if err != nil {
		return err
	}
	outputs, err := build.Stream.ResolveOutputs(target, repoRoot)
	if err != nil {
		return fmt.Errorf("resolve %s outputs: %w", target, err)
	}
	if len(outputs) != 1 {
		return fmt.Errorf("%s must produce exactly one release JSON output, got %d: %v", target, len(outputs), outputs)
	}

	release, err := nomadrelease.Load(outputs[0])
	if err != nil {
		return err
	}
	if release.Site != site {
		return fmt.Errorf("nomad release site=%s, want %s", release.Site, site)
	}
	if release.SHA != "" {
		return fmt.Errorf("built nomad release unexpectedly already has sha=%s", release.SHA)
	}
	span.SetAttributes(
		attribute.String("verself.nomad_release_path", outputs[0]),
		attribute.String("verself.nomad_release_target", target),
		attribute.Int("verself.artifact_count", len(release.Artifacts)),
		attribute.Int("verself.nomad_job_count", len(release.Jobs)),
	)

	pub, err := newGaragePublisher(ctx, rt.SSH, release.ArtifactDelivery)
	if err != nil {
		return err
	}
	if err := pub.PublishAll(ctx, release.Artifacts, repoRoot); err != nil {
		return err
	}
	if err := release.StampForPublish(sha, time.Now()); err != nil {
		return err
	}
	body, err := release.Encode()
	if err != nil {
		return err
	}
	releaseArtifact := nomadrelease.ReleaseArtifact(release.ArtifactDelivery, site, sha)
	releaseArtifact.SHA256 = nomadrelease.SHA256(body)
	span.SetAttributes(
		attribute.String("verself.nomad_release_key", releaseArtifact.Key),
		attribute.String("verself.nomad_release_sha256", releaseArtifact.SHA256),
	)
	if err := pub.PublishBytes(ctx, releaseArtifact, body, "application/json"); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "verself-deploy: published nomad release site=%s sha=%s key=%s digest=%s\n",
		site, sha, releaseArtifact.Key, releaseArtifact.SHA256)
	return nil
}

func deployPublishedRelease(ctx context.Context, rt *runtime.Runtime, span trace.Span, site, repoRoot, sha string) error {
	delivery, err := loadSiteArtifactDelivery(repoRoot, site)
	if err != nil {
		return err
	}
	pub, err := newGaragePublisher(ctx, rt.SSH, delivery)
	if err != nil {
		return err
	}
	releaseArtifact := nomadrelease.ReleaseArtifact(delivery, site, sha)
	body, err := pub.ReadBytes(ctx, releaseArtifact, nomadReleaseMaxBytes)
	if err != nil {
		if errors.Is(err, garage.ErrNotFound) {
			return fmt.Errorf("missing published Nomad release for site=%s sha=%s; run `aspect release publish --site=%s --sha=%s` before deploy", site, sha, site, sha)
		}
		return err
	}
	release, err := nomadrelease.Decode(body)
	if err != nil {
		return err
	}
	if err := release.Validate(sha); err != nil {
		return err
	}
	if release.Site != site {
		return fmt.Errorf("nomad release site=%s, want %s", release.Site, site)
	}
	releaseDigest := nomadrelease.SHA256(body)
	span.SetAttributes(
		attribute.String("verself.nomad_release_key", releaseArtifact.Key),
		attribute.String("verself.nomad_release_sha256", releaseDigest),
		attribute.Int("verself.artifact_count", len(release.Artifacts)),
		attribute.Int("verself.nomad_job_count", len(release.Jobs)),
	)
	for _, artifact := range release.Artifacts {
		if err := pub.Verify(ctx, artifact); err != nil {
			return fmt.Errorf("verify release artifact %s: %w", artifact.Output, err)
		}
	}
	return submitReleaseJobs(ctx, rt, release)
}

func submitReleaseJobs(ctx context.Context, rt *runtime.Runtime, release *nomadrelease.Release) error {
	forward, err := rt.SSH.Forward(ctx, "nomad", defaultNomadRemotePort)
	if err != nil {
		return err
	}
	client, err := nomadclient.New("http://" + forward.ListenAddr)
	if err != nil {
		return err
	}
	for _, jobID := range release.SubmitOrder {
		job, ok := release.JobByID(jobID)
		if !ok {
			return fmt.Errorf("nomad release submit_order references missing job %q", jobID)
		}
		if err := submitOneReleaseJob(ctx, rt, client, job); err != nil {
			return fmt.Errorf("%s: %w", jobID, err)
		}
	}
	return nil
}

func submitOneReleaseJob(ctx context.Context, rt *runtime.Runtime, client *nomadclient.Client, job nomadrelease.Job) error {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.nomad.submit",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("nomad.job_id", job.JobID),
			attribute.String("nomad.spec_sha256", job.SpecSHA256),
			attribute.String("nomad.artifact_sha256", job.ArtifactSHA256),
		),
	)
	defer span.End()

	spec, err := nomadclient.ParseSpec(job.Spec, "nomad release "+job.JobID)
	if err != nil {
		recordFailure(span, err)
		return err
	}
	if spec.SpecDigest != job.SpecSHA256 {
		err := fmt.Errorf("release job %s spec_sha256=%s, parsed spec has %s", job.JobID, job.SpecSHA256, spec.SpecDigest)
		recordFailure(span, err)
		return err
	}
	if spec.ArtifactDigest != job.ArtifactSHA256 {
		err := fmt.Errorf("release job %s artifact_sha256=%s, parsed spec has %s", job.JobID, job.ArtifactSHA256, spec.ArtifactDigest)
		recordFailure(span, err)
		return err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, nomadReleaseSubmitTimeout)
	defer cancel()
	evidence := nomadJobEvidenceWriter{
		db:     rt.DeployDB,
		runKey: rt.Identity.RunKey(),
		site:   rt.Site,
	}
	if err := submitSpec(timeoutCtx, span, client, spec, evidence); err != nil {
		recordFailure(span, err)
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

func loadSiteArtifactDelivery(repoRoot, site string) (nomadrelease.ArtifactDelivery, error) {
	indexPath := filepath.Join(repoRoot, "src", "tools", "deployment", "nomad", "sites", site, "release.json")
	body, err := os.ReadFile(indexPath)
	if err != nil {
		return nomadrelease.ArtifactDelivery{}, fmt.Errorf("read %s: %w", indexPath, err)
	}
	var index struct {
		ArtifactDelivery nomadrelease.ArtifactDelivery `json:"artifact_delivery"`
	}
	if err := json.Unmarshal(body, &index); err != nil {
		return nomadrelease.ArtifactDelivery{}, fmt.Errorf("decode %s: %w", indexPath, err)
	}
	if index.ArtifactDelivery.Bucket == "" || index.ArtifactDelivery.GetterSourcePrefix == "" {
		return nomadrelease.ArtifactDelivery{}, fmt.Errorf("%s: artifact_delivery is incomplete", indexPath)
	}
	return index.ArtifactDelivery, nil
}

func newGaragePublisher(ctx context.Context, sshClient *sshtun.Client, delivery nomadrelease.ArtifactDelivery) (*garage.Publisher, error) {
	if delivery.Origin.Port == 0 {
		return nil, errors.New("artifact delivery origin port is required")
	}
	forward, err := sshClient.Forward(ctx, "artifact", delivery.Origin.Port)
	if err != nil {
		return nil, err
	}

	credBytes, err := sudoCat(ctx, sshClient, delivery.PublisherCredentials.EnvironmentFile)
	if err != nil {
		return nil, fmt.Errorf("read publisher credentials: %w", err)
	}
	access, secret, err := garage.ParseEnvFile(
		credBytes,
		delivery.PublisherCredentials.AccessKeyIDEnv,
		delivery.PublisherCredentials.SecretAccessKeyEnv,
	)
	if err != nil {
		return nil, err
	}

	caPEM, err := sudoCat(ctx, sshClient, delivery.Origin.CABundlePath)
	if err != nil {
		return nil, fmt.Errorf("read artifact origin CA bundle: %w", err)
	}

	return garage.New(delivery, garage.Config{
		ConnectAddress:  forward.ListenAddr,
		CABundlePEM:     caPEM,
		AccessKeyID:     access,
		SecretAccessKey: secret,
	})
}

func releaseTarget(site string) string {
	return fmt.Sprintf("//src/tools/deployment/nomad:%s_nomad_release", site)
}

func resolveGitSHA(repoRoot, raw string) (string, error) {
	target := raw
	if strings.TrimSpace(target) == "" {
		target = "HEAD"
	}
	out, err := gitOutput(repoRoot, "rev-parse", "--verify", target+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve git ref %s: %w", target, err)
	}
	sha := strings.TrimSpace(out)
	if len(sha) != 40 {
		return "", fmt.Errorf("git ref %s resolved to non-40-char sha %q", target, sha)
	}
	return sha, nil
}

func ensureRepoAtSHA(repoRoot, sha string) error {
	head, err := gitOutput(repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(head) != sha {
		return fmt.Errorf("repo HEAD is %s, cannot publish release for %s from this checkout", strings.TrimSpace(head), sha)
	}
	status, err := gitOutput(repoRoot, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("repo has uncommitted changes; refusing to publish artifacts as immutable sha %s", sha)
	}
	return nil
}

func gitOutput(repoRoot string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	body, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(body)))
	}
	return string(body), nil
}

// sudoCat reads a controller-side file the operator user can read only via sudo.
func sudoCat(ctx context.Context, sshClient *sshtun.Client, path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("sudoCat: path is empty")
	}
	if strings.ContainsAny(path, "'\\") {
		return nil, fmt.Errorf("sudoCat: refusing to escape path with special chars: %q", path)
	}
	return sshClient.Exec(ctx, "sudo cat "+strconv.Quote(path))
}
