package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
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
	nomadComponentsQuery      = `kind("nomad_component rule", //src/...)`
)

type nomadComponentDescriptor struct {
	SchemaVersion     int                       `json:"schema_version"`
	Label             string                    `json:"label"`
	Component         string                    `json:"component"`
	JobID             string                    `json:"job_id"`
	JobSpec           string                    `json:"job_spec"`
	JobSpecPath       string                    `json:"job_spec_path"`
	DependsOn         []string                  `json:"depends_on"`
	Artifacts         []nomadDescriptorArtifact `json:"artifacts"`
	EmbeddedTemplates []nomadDescriptorTemplate `json:"embedded_templates"`
}

type nomadDescriptorArtifact struct {
	Label  string `json:"label"`
	Output string `json:"output"`
	Path   string `json:"path"`
}

type nomadDescriptorTemplate struct {
	Label       string `json:"label"`
	Placeholder string `json:"placeholder"`
	Path        string `json:"path"`
}

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

func runNomadComponentIndex(args []string) int {
	fs := flag.NewFlagSet("nomad component-index", flag.ContinueOnError)
	site := fs.String("site", "prod", "deployment site label")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	out := fs.String("out", "", "path to write the discovered Nomad component index JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *out == "" {
		fmt.Fprintln(os.Stderr, "verself-deploy nomad component-index: --out is required")
		fs.Usage()
		return 2
	}
	if *repoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy nomad component-index: cwd: %v\n", err)
			return 1
		}
		*repoRoot = cwd
	}
	absRepoRoot, err := filepath.Abs(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy nomad component-index: repo-root: %v\n", err)
		return 1
	}
	*repoRoot = absRepoRoot
	parentCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := writeNomadComponentIndex(parentCtx, *repoRoot, *out); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy nomad component-index: site=%s: %v\n", *site, err)
		return 1
	}
	_, _ = fmt.Fprintf(os.Stdout, "verself-deploy: wrote Nomad component index site=%s path=%s\n", *site, *out)
	return 0
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

	componentLabels, descriptorPaths, err := buildNomadComponentDescriptors(ctx, repoRoot)
	if err != nil {
		return err
	}
	releasePath, cleanup, err := linkNomadRelease(ctx, repoRoot, site, descriptorPaths)
	if err != nil {
		return err
	}
	defer cleanup()

	release, err := nomadrelease.Load(releasePath)
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
		attribute.String("verself.nomad_release_path", releasePath),
		attribute.String("verself.nomad_components_query", nomadComponentsQuery),
		attribute.Int("verself.artifact_count", len(release.Artifacts)),
		attribute.Int("verself.nomad_component_count", len(componentLabels)),
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

func discoverNomadComponents(ctx context.Context, repoRoot string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "bazelisk", "query", nomadComponentsQuery)
	cmd.Dir = repoRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	body, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("bazelisk query %s: %w: %s", nomadComponentsQuery, err, strings.TrimSpace(stderr.String()))
	}
	var labels []string
	for _, line := range strings.Split(string(body), "\n") {
		label := strings.TrimSpace(line)
		if label == "" {
			continue
		}
		labels = append(labels, label)
	}
	sort.Strings(labels)
	if len(labels) == 0 {
		return nil, fmt.Errorf("bazel query %s returned no Nomad components", nomadComponentsQuery)
	}
	return labels, nil
}

func buildNomadComponentDescriptors(ctx context.Context, repoRoot string) ([]string, []string, error) {
	labels, err := discoverNomadComponents(ctx, repoRoot)
	if err != nil {
		return nil, nil, err
	}
	build, err := bazelbuild.Build(ctx, repoRoot, labels, "--config=remote-writer")
	if err != nil {
		return nil, nil, err
	}
	descriptorPaths := make([]string, 0, len(labels))
	for _, label := range labels {
		outputs, err := build.Stream.ResolveOutputs(label, repoRoot)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve %s descriptor output: %w", label, err)
		}
		if len(outputs) != 1 {
			return nil, nil, fmt.Errorf("%s must produce exactly one component descriptor output, got %d: %v", label, len(outputs), outputs)
		}
		descriptorPaths = append(descriptorPaths, outputs[0])
	}
	return labels, descriptorPaths, nil
}

func linkNomadRelease(ctx context.Context, repoRoot, site string, descriptorPaths []string) (string, func(), error) {
	out, err := os.CreateTemp("", "verself-nomad-release-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary Nomad release file: %w", err)
	}
	outPath := out.Name()
	if err := out.Close(); err != nil {
		_ = os.Remove(outPath)
		return "", nil, fmt.Errorf("close temporary Nomad release file: %w", err)
	}
	cleanup := func() { _ = os.Remove(outPath) }

	args := []string{
		filepath.Join(repoRoot, "src", "tools", "deployment", "nomad", "link_release.py"),
		"--site", site,
		"--components-query", nomadComponentsQuery,
		"--site-config", filepath.Join(repoRoot, "src", "tools", "deployment", "nomad", "sites", site, "site.json"),
		"--out", outPath,
	}
	for _, path := range descriptorPaths {
		args = append(args, "--component-descriptor", path)
	}
	cmd := exec.CommandContext(ctx, "python3", args...)
	cmd.Dir = repoRoot
	body, err := cmd.CombinedOutput()
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("link Nomad release: %w: %s", err, strings.TrimSpace(string(body)))
	}
	return outPath, cleanup, nil
}

func writeNomadComponentIndex(ctx context.Context, repoRoot, outPath string) error {
	_, descriptorPaths, err := buildNomadComponentDescriptors(ctx, repoRoot)
	if err != nil {
		return err
	}
	components := make([]nomadComponentDescriptor, 0, len(descriptorPaths))
	for _, descriptorPath := range descriptorPaths {
		body, err := os.ReadFile(descriptorPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", descriptorPath, err)
		}
		var component nomadComponentDescriptor
		if err := json.Unmarshal(body, &component); err != nil {
			return fmt.Errorf("decode %s: %w", descriptorPath, err)
		}
		if component.SchemaVersion != 1 || component.Component == "" || component.JobID == "" || component.JobSpec == "" {
			return fmt.Errorf("%s: incomplete Nomad component descriptor", descriptorPath)
		}
		components = append(components, component)
	}
	sort.Slice(components, func(i, j int) bool {
		return components[i].JobID < components[j].JobID
	})
	rows := make([]map[string]string, 0, len(components))
	for _, component := range components {
		rows = append(rows, map[string]string{
			"component": component.Component,
			"job_id":    component.JobID,
			"job_spec":  component.JobSpec,
		})
	}
	body, err := json.Marshal(struct {
		Components []map[string]string `json:"components"`
	}{Components: rows})
	if err != nil {
		return fmt.Errorf("encode Nomad component index: %w", err)
	}
	body = append(body, '\n')
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(outPath), err)
	}
	if err := os.WriteFile(outPath, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}
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
	siteConfigPath := filepath.Join(repoRoot, "src", "tools", "deployment", "nomad", "sites", site, "site.json")
	body, err := os.ReadFile(siteConfigPath)
	if err != nil {
		return nomadrelease.ArtifactDelivery{}, fmt.Errorf("read %s: %w", siteConfigPath, err)
	}
	var index struct {
		ArtifactDelivery nomadrelease.ArtifactDelivery `json:"artifact_delivery"`
	}
	if err := json.Unmarshal(body, &index); err != nil {
		return nomadrelease.ArtifactDelivery{}, fmt.Errorf("decode %s: %w", siteConfigPath, err)
	}
	if index.ArtifactDelivery.Bucket == "" || index.ArtifactDelivery.GetterSourcePrefix == "" {
		return nomadrelease.ArtifactDelivery{}, fmt.Errorf("%s: artifact_delivery is incomplete", siteConfigPath)
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
