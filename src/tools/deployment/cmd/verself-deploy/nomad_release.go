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

	"github.com/hashicorp/nomad/api"
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

const artifactSourcePrefix = "verself-artifact://"

type nomadComponentDescriptor struct {
	SchemaVersion int                       `json:"schema_version"`
	Label         string                    `json:"label"`
	Component     string                    `json:"component"`
	DependsOn     []string                  `json:"depends_on"`
	JobID         string                    `json:"job_id"`
	JobSpec       string                    `json:"job_spec"`
	JobSpecPath   string                    `json:"job_spec_path"`
	Artifacts     []nomadDescriptorArtifact `json:"artifacts"`
}

type nomadDescriptorArtifact struct {
	Label  string `json:"label"`
	Output string `json:"output"`
	Path   string `json:"path"`
}

type nomadArtifactDeliveryPolicy struct {
	nomadrelease.ArtifactDelivery
	KeyPrefix         string `json:"key_prefix"`
	ChecksumAlgorithm string `json:"checksum_algorithm"`
	Public            *bool  `json:"public"`
}

type nomadSiteConfig struct {
	ArtifactDelivery nomadArtifactDeliveryPolicy `json:"artifact_delivery"`
}

type artifactBinding struct {
	Artifact nomadrelease.Artifact
	Checksum string
	Label    string
	Path     string
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
	release, err := assembleNomadRelease(repoRoot, site, descriptorPaths)
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

func assembleNomadRelease(repoRoot, site string, descriptorPaths []string) (*nomadrelease.Release, error) {
	components, err := loadNomadComponentDescriptors(descriptorPaths)
	if err != nil {
		return nil, err
	}
	ordered, err := orderNomadComponents(components)
	if err != nil {
		return nil, err
	}
	policy, err := loadNomadArtifactDeliveryPolicy(repoRoot, site)
	if err != nil {
		return nil, err
	}
	bindings, artifacts, err := bindNomadArtifacts(repoRoot, policy, components)
	if err != nil {
		return nil, err
	}

	referenced := map[string]bool{}
	jobs := make([]nomadrelease.Job, 0, len(ordered))
	submitOrder := make([]string, 0, len(ordered))
	for _, component := range ordered {
		specPath := resolveWorkspacePath(repoRoot, component.JobSpecPath)
		job, err := loadAuthoredNomadSpec(specPath)
		if err != nil {
			return nil, err
		}
		seen, err := bindArtifactsInSpec(job, bindings)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", component.JobID, err)
		}
		for output := range seen {
			referenced[output] = true
		}
		artifactDigestInput, err := json.Marshal(canonicalArtifactDigestInput(seen, bindings))
		if err != nil {
			return nil, fmt.Errorf("%s: encode artifact digest input: %w", component.JobID, err)
		}
		artifactDigest := nomadrelease.SHA256(artifactDigestInput)
		specSHA, err := stampNomadSpecMeta(job, artifactDigest)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", component.JobID, err)
		}
		specBody, err := json.Marshal(struct {
			Job *api.Job `json:"Job"`
		}{
			Job: job,
		})
		if err != nil {
			return nil, fmt.Errorf("%s: encode bound Nomad spec: %w", component.JobID, err)
		}
		jobs = append(jobs, nomadrelease.Job{
			JobID:          component.JobID,
			SpecSHA256:     specSHA,
			ArtifactSHA256: artifactDigest,
			Spec:           specBody,
		})
		submitOrder = append(submitOrder, component.JobID)
	}
	for output := range bindings {
		if !referenced[output] {
			return nil, fmt.Errorf("artifact %q was declared by a Nomad component but not referenced by any authored job", output)
		}
	}
	release := &nomadrelease.Release{
		SchemaVersion:    nomadrelease.SchemaVersion,
		Site:             site,
		ComponentsQuery:  nomadComponentsQuery,
		ArtifactDelivery: policy.ArtifactDelivery,
		Artifacts:        artifacts,
		Jobs:             jobs,
		SubmitOrder:      submitOrder,
	}
	if err := release.Validate(""); err != nil {
		return nil, err
	}
	return release, nil
}

func loadNomadComponentDescriptors(paths []string) ([]nomadComponentDescriptor, error) {
	if len(paths) == 0 {
		return nil, errors.New("at least one Nomad component descriptor is required")
	}
	components := make([]nomadComponentDescriptor, 0, len(paths))
	seenLabels := map[string]bool{}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		var component nomadComponentDescriptor
		if err := json.Unmarshal(body, &component); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		if component.SchemaVersion != 1 {
			return nil, fmt.Errorf("%s: unsupported component descriptor schema_version=%d", path, component.SchemaVersion)
		}
		if component.Label == "" || component.Component == "" || component.JobID == "" || component.JobSpec == "" || component.JobSpecPath == "" {
			return nil, fmt.Errorf("%s: component descriptor must include label, component, job_id, job_spec, and job_spec_path", path)
		}
		if seenLabels[component.Label] {
			return nil, fmt.Errorf("duplicate Nomad component descriptor label %s", component.Label)
		}
		seenLabels[component.Label] = true
		for _, artifact := range component.Artifacts {
			if artifact.Label == "" || artifact.Output == "" || artifact.Path == "" {
				return nil, fmt.Errorf("%s: artifact entries must include label, output, and path", path)
			}
		}
		components = append(components, component)
	}
	return components, nil
}

func orderNomadComponents(components []nomadComponentDescriptor) ([]nomadComponentDescriptor, error) {
	byJobID := make(map[string]nomadComponentDescriptor, len(components))
	for _, component := range components {
		if _, exists := byJobID[component.JobID]; exists {
			return nil, fmt.Errorf("duplicate Nomad job_id %s", component.JobID)
		}
		byJobID[component.JobID] = component
	}
	var out []nomadComponentDescriptor
	temporary := map[string]bool{}
	permanent := map[string]bool{}
	var visit func(jobID string, stack []string) error
	visit = func(jobID string, stack []string) error {
		if permanent[jobID] {
			return nil
		}
		if temporary[jobID] {
			return fmt.Errorf("Nomad job dependency cycle: %s", strings.Join(append(stack, jobID), " -> "))
		}
		component, exists := byJobID[jobID]
		if !exists {
			return fmt.Errorf("unknown Nomad job dependency %q", jobID)
		}
		temporary[jobID] = true
		deps := append([]string(nil), component.DependsOn...)
		sort.Strings(deps)
		for _, dep := range deps {
			if _, exists := byJobID[dep]; !exists {
				return fmt.Errorf("%s.depends_on references unknown Nomad job %q", jobID, dep)
			}
			if err := visit(dep, append(stack, jobID)); err != nil {
				return err
			}
		}
		temporary[jobID] = false
		permanent[jobID] = true
		out = append(out, component)
		return nil
	}
	var jobIDs []string
	for jobID := range byJobID {
		jobIDs = append(jobIDs, jobID)
	}
	sort.Strings(jobIDs)
	for _, jobID := range jobIDs {
		if err := visit(jobID, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func loadNomadArtifactDeliveryPolicy(repoRoot, site string) (nomadArtifactDeliveryPolicy, error) {
	siteConfigPath := filepath.Join(repoRoot, "src", "tools", "deployment", "nomad", "sites", site, "site.json")
	body, err := os.ReadFile(siteConfigPath)
	if err != nil {
		return nomadArtifactDeliveryPolicy{}, fmt.Errorf("read %s: %w", siteConfigPath, err)
	}
	var cfg nomadSiteConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return nomadArtifactDeliveryPolicy{}, fmt.Errorf("decode %s: %w", siteConfigPath, err)
	}
	if cfg.ArtifactDelivery.Public == nil || *cfg.ArtifactDelivery.Public {
		return nomadArtifactDeliveryPolicy{}, fmt.Errorf("%s: artifact_delivery.public must be false", siteConfigPath)
	}
	if cfg.ArtifactDelivery.Bucket == "" || cfg.ArtifactDelivery.GetterSourcePrefix == "" || cfg.ArtifactDelivery.KeyPrefix == "" {
		return nomadArtifactDeliveryPolicy{}, fmt.Errorf("%s: artifact_delivery must include bucket, getter_source_prefix, and key_prefix", siteConfigPath)
	}
	if !strings.HasPrefix(cfg.ArtifactDelivery.GetterSourcePrefix, "s3::https://") {
		return nomadArtifactDeliveryPolicy{}, fmt.Errorf("%s: artifact_delivery.getter_source_prefix must start with s3::https://", siteConfigPath)
	}
	if cfg.ArtifactDelivery.ChecksumAlgorithm != "sha256" {
		return nomadArtifactDeliveryPolicy{}, fmt.Errorf("%s: only sha256 artifact checksums are supported", siteConfigPath)
	}
	return cfg.ArtifactDelivery, nil
}

func bindNomadArtifacts(repoRoot string, policy nomadArtifactDeliveryPolicy, components []nomadComponentDescriptor) (map[string]artifactBinding, []nomadrelease.Artifact, error) {
	bindings := map[string]artifactBinding{}
	for _, component := range components {
		for _, declared := range component.Artifacts {
			if prior, exists := bindings[declared.Output]; exists {
				if prior.Label != declared.Label || prior.Path != declared.Path {
					return nil, nil, fmt.Errorf("Nomad artifact output %q is provided by both %s and %s", declared.Output, prior.Label, declared.Label)
				}
				continue
			}
			artifactPath := resolveWorkspacePath(repoRoot, declared.Path)
			body, err := os.ReadFile(artifactPath)
			if err != nil {
				return nil, nil, fmt.Errorf("read artifact %s: %w", declared.Path, err)
			}
			digest := nomadrelease.SHA256(body)
			key := strings.Trim(policy.KeyPrefix, "/") + "/" + digest + "/" + declared.Output + ".tar"
			artifact := nomadrelease.Artifact{
				Output:        declared.Output,
				LocalPath:     artifactPath,
				SHA256:        digest,
				Bucket:        policy.Bucket,
				Key:           key,
				GetterSource:  strings.TrimRight(policy.GetterSourcePrefix, "/") + "/" + key,
				GetterOptions: policy.GetterOptions,
			}
			bindings[declared.Output] = artifactBinding{
				Artifact: artifact,
				Checksum: policy.ChecksumAlgorithm + ":" + digest,
				Label:    declared.Label,
				Path:     declared.Path,
			}
		}
	}
	artifacts := make([]nomadrelease.Artifact, 0, len(bindings))
	for _, binding := range bindings {
		artifacts = append(artifacts, binding.Artifact)
	}
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].Bucket != artifacts[j].Bucket {
			return artifacts[i].Bucket < artifacts[j].Bucket
		}
		if artifacts[i].Key != artifacts[j].Key {
			return artifacts[i].Key < artifacts[j].Key
		}
		return artifacts[i].Output < artifacts[j].Output
	})
	return bindings, artifacts, nil
}

func loadAuthoredNomadSpec(path string) (*api.Job, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var spec struct {
		Job *api.Job `json:"Job"`
	}
	if err := json.Unmarshal(body, &spec); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if spec.Job == nil {
		return nil, fmt.Errorf("%s: missing top-level Job object", path)
	}
	if spec.Job.ID == nil || *spec.Job.ID == "" {
		return nil, fmt.Errorf("%s: Job.ID is required", path)
	}
	return spec.Job, nil
}

func bindArtifactsInSpec(job *api.Job, bindings map[string]artifactBinding) (map[string]bool, error) {
	seen := map[string]bool{}
	for _, group := range job.TaskGroups {
		for _, task := range group.Tasks {
			for _, artifact := range task.Artifacts {
				if artifact.GetterSource == nil {
					continue
				}
				source := *artifact.GetterSource
				if !strings.HasPrefix(source, artifactSourcePrefix) {
					continue
				}
				output := strings.TrimPrefix(source, artifactSourcePrefix)
				binding, ok := bindings[output]
				if !ok {
					return nil, fmt.Errorf("artifact %q is referenced by authored spec but not declared by nomad_component", output)
				}
				getterOptions := map[string]string{}
				for key, value := range binding.Artifact.GetterOptions {
					getterOptions[key] = value
				}
				getterOptions["checksum"] = binding.Checksum
				artifact.GetterSource = &binding.Artifact.GetterSource
				artifact.GetterOptions = getterOptions
				seen[output] = true
			}
		}
	}
	return seen, nil
}

func canonicalArtifactDigestInput(seen map[string]bool, bindings map[string]artifactBinding) []map[string]string {
	outputs := make([]string, 0, len(seen))
	for output := range seen {
		outputs = append(outputs, output)
	}
	sort.Strings(outputs)
	rows := make([]map[string]string, 0, len(outputs))
	for _, output := range outputs {
		binding := bindings[output]
		rows = append(rows, map[string]string{
			"getter_source": binding.Artifact.GetterSource,
			"output":        output,
			"sha256":        binding.Artifact.SHA256,
		})
	}
	return rows
}

func stampNomadSpecMeta(job *api.Job, artifactDigest string) (string, error) {
	if job.Meta == nil {
		job.Meta = map[string]string{}
	}
	job.Meta["artifact_sha256"] = artifactDigest
	delete(job.Meta, "spec_sha256")
	specBody, err := json.Marshal(struct {
		Job *api.Job `json:"Job"`
	}{
		Job: job,
	})
	if err != nil {
		return "", fmt.Errorf("encode spec digest input: %w", err)
	}
	specDigest := nomadrelease.SHA256(specBody)
	job.Meta["spec_sha256"] = specDigest
	return specDigest, nil
}

func resolveWorkspacePath(repoRoot, path string) string {
	if filepath.IsAbs(path) || repoRoot == "" {
		return path
	}
	return filepath.Join(repoRoot, filepath.FromSlash(path))
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
