package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/nomad/api"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/bazelbuild"
	"github.com/verself/deployment-tools/internal/deploymodel"
	"github.com/verself/deployment-tools/internal/garage"
	"github.com/verself/deployment-tools/internal/nomadclient"
	"github.com/verself/deployment-tools/internal/runtime"
	"github.com/verself/deployment-tools/internal/sshtun"
)

const (
	// defaultNomadRemotePort matches the loopback Nomad agent port the substrate role binds.
	defaultNomadRemotePort = 4646

	nomadSubmitTimeout   = 5 * time.Minute
	nomadComponentsQuery = `kind("nomad_component rule", //src/...)`
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
	Provides      []string                  `json:"provides"`
	Requires      []string                  `json:"requires"`
	Sites         []string                  `json:"sites"`
	UnitID        string                    `json:"unit_id"`
	Artifacts     []nomadDescriptorArtifact `json:"artifacts"`
}

type nomadDescriptorArtifact struct {
	Label  string `json:"label"`
	Output string `json:"output"`
	Path   string `json:"path"`
}

type nomadArtifactDeliveryPolicy struct {
	deploymodel.ArtifactDelivery
	KeyPrefix         string `json:"key_prefix"`
	ChecksumAlgorithm string `json:"checksum_algorithm"`
	Public            *bool  `json:"public"`
}

type nomadSiteConfig struct {
	ArtifactDelivery nomadArtifactDeliveryPolicy `json:"artifact_delivery"`
}

type artifactBinding struct {
	Artifact deploymodel.Artifact
	Checksum string
	Label    string
	Path     string
}

type nomadDeployment struct {
	Site             string
	ArtifactDelivery deploymodel.ArtifactDelivery
	Artifacts        []deploymodel.Artifact
	Jobs             []deploymodel.NomadJob
	SubmitOrder      []string
}

type authoredNomadSpecParser interface {
	ParseJobHCL(context.Context, []byte, string) (*api.Job, error)
}

func (d *nomadDeployment) validate() error {
	if d == nil {
		return fmt.Errorf("nomad deployment is nil")
	}
	if d.Site == "" {
		return fmt.Errorf("nomad deployment site is required")
	}
	if d.ArtifactDelivery.Bucket == "" || d.ArtifactDelivery.GetterSourcePrefix == "" {
		return fmt.Errorf("nomad deployment artifact_delivery is incomplete")
	}
	artifacts := map[string]bool{}
	for _, artifact := range d.Artifacts {
		if artifact.Output == "" || artifact.SHA256 == "" || artifact.Bucket == "" || artifact.Key == "" || artifact.GetterSource == "" {
			return fmt.Errorf("nomad deployment artifact is incomplete: %q", artifact.Output)
		}
		if artifacts[artifact.Output] {
			return fmt.Errorf("nomad deployment duplicate artifact output %q", artifact.Output)
		}
		artifacts[artifact.Output] = true
	}
	jobs := map[string]bool{}
	for _, job := range d.Jobs {
		if job.JobID == "" || job.SpecSHA256 == "" || job.ArtifactSHA256 == "" || len(job.Spec) == 0 {
			return fmt.Errorf("nomad deployment job is incomplete: %q", job.JobID)
		}
		if jobs[job.JobID] {
			return fmt.Errorf("nomad deployment duplicate job_id %q", job.JobID)
		}
		jobs[job.JobID] = true
	}
	if len(d.SubmitOrder) != len(d.Jobs) {
		return fmt.Errorf("nomad deployment submit_order has %d entries, jobs has %d", len(d.SubmitOrder), len(d.Jobs))
	}
	for _, jobID := range d.SubmitOrder {
		if !jobs[jobID] {
			return fmt.Errorf("nomad deployment submit_order references unknown job %q", jobID)
		}
	}
	return nil
}

func (d *nomadDeployment) jobByID(jobID string) (deploymodel.NomadJob, bool) {
	for _, job := range d.Jobs {
		if job.JobID == jobID {
			return job, true
		}
	}
	return deploymodel.NomadJob{}, false
}

func deployNomadComponents(ctx context.Context, rt *runtime.Runtime, span trace.Span, site, repoRoot string) error {
	componentLabels, descriptorPaths, err := buildNomadComponentDescriptors(ctx, repoRoot)
	if err != nil {
		return err
	}
	nomadForward, err := rt.SSH.Forward(ctx, "nomad", defaultNomadRemotePort)
	if err != nil {
		return err
	}
	defer func() { _ = nomadForward.Close() }()
	nomad, err := nomadclient.New("http://" + nomadForward.ListenAddr)
	if err != nil {
		return err
	}

	deployment, err := assembleNomadDeployment(ctx, nomad, repoRoot, site, descriptorPaths)
	if err != nil {
		return err
	}
	if deployment.Site != site {
		return fmt.Errorf("nomad deployment site=%s, want %s", deployment.Site, site)
	}
	span.SetAttributes(
		attribute.String("verself.nomad_components_query", nomadComponentsQuery),
		attribute.Int("verself.artifact_count", len(deployment.Artifacts)),
		attribute.Int("verself.nomad_component_count", len(componentLabels)),
		attribute.Int("verself.nomad_job_count", len(deployment.Jobs)),
	)

	pub, err := newGaragePublisher(ctx, rt.SSH, deployment.ArtifactDelivery)
	if err != nil {
		return err
	}
	if err := pub.PublishAll(ctx, deployment.Artifacts, repoRoot); err != nil {
		return err
	}
	return submitDeploymentJobs(ctx, rt, nomad, deployment)
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

func assembleNomadDeployment(ctx context.Context, parser authoredNomadSpecParser, repoRoot, site string, descriptorPaths []string) (*nomadDeployment, error) {
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
	jobs := make([]deploymodel.NomadJob, 0, len(ordered))
	submitOrder := make([]string, 0, len(ordered))
	for _, component := range ordered {
		specPath := resolveWorkspacePath(repoRoot, component.JobSpecPath)
		job, err := loadAuthoredNomadSpec(ctx, parser, specPath)
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
		artifactDigest := deploymodel.SHA256(artifactDigestInput)
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
		jobs = append(jobs, deploymodel.NomadJob{
			JobID:          component.JobID,
			Component:      component.Component,
			DependsOn:      append([]string(nil), component.Requires...),
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
	deployment := &nomadDeployment{
		Site:             site,
		ArtifactDelivery: policy.ArtifactDelivery,
		Artifacts:        artifacts,
		Jobs:             jobs,
		SubmitOrder:      submitOrder,
	}
	if err := deployment.validate(); err != nil {
		return nil, err
	}
	return deployment, nil
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
		if component.SchemaVersion != 1 && component.SchemaVersion != 2 {
			return nil, fmt.Errorf("%s: unsupported component descriptor schema_version=%d", path, component.SchemaVersion)
		}
		if component.Label == "" || component.Component == "" || component.JobID == "" || component.JobSpec == "" || component.JobSpecPath == "" {
			return nil, fmt.Errorf("%s: component descriptor must include label, component, job_id, job_spec, and job_spec_path", path)
		}
		if component.UnitID == "" {
			component.UnitID = component.JobID
		}
		if len(component.Provides) == 0 {
			component.Provides = []string{"nomad:job:" + component.JobID}
		}
		if len(component.Requires) == 0 && len(component.DependsOn) > 0 {
			for _, dep := range component.DependsOn {
				if strings.HasPrefix(dep, "nomad:job:") {
					component.Requires = append(component.Requires, dep)
				} else {
					component.Requires = append(component.Requires, "nomad:job:"+dep)
				}
			}
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
	providerByResource := map[string]string{}
	for _, component := range components {
		if _, exists := byJobID[component.JobID]; exists {
			return nil, fmt.Errorf("duplicate Nomad job_id %s", component.JobID)
		}
		byJobID[component.JobID] = component
		for _, provided := range component.Provides {
			if prior := providerByResource[provided]; prior != "" && prior != component.JobID {
				return nil, fmt.Errorf("resource %q is provided by both %s and %s", provided, prior, component.JobID)
			}
			providerByResource[provided] = component.JobID
		}
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
			return fmt.Errorf("nomad job dependency cycle: %s", strings.Join(append(stack, jobID), " -> "))
		}
		component, exists := byJobID[jobID]
		if !exists {
			return fmt.Errorf("unknown Nomad job dependency %q", jobID)
		}
		temporary[jobID] = true
		depJobs := map[string]bool{}
		for _, dep := range component.DependsOn {
			depJob := strings.TrimPrefix(dep, "nomad:job:")
			if depJob != "" {
				depJobs[depJob] = true
			}
		}
		for _, required := range component.Requires {
			if provider := providerByResource[required]; provider != "" {
				depJobs[provider] = true
			}
		}
		deps := make([]string, 0, len(depJobs))
		for dep := range depJobs {
			deps = append(deps, dep)
		}
		sort.Strings(deps)
		for _, dep := range deps {
			if _, exists := byJobID[dep]; !exists {
				return fmt.Errorf("%s requires unknown Nomad dependency %q", jobID, dep)
			}
			if dep == jobID {
				return fmt.Errorf("%s depends on itself", jobID)
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
	siteConfigPath := deploymentSiteConfigPath(repoRoot, site)
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

func bindNomadArtifacts(repoRoot string, policy nomadArtifactDeliveryPolicy, components []nomadComponentDescriptor) (map[string]artifactBinding, []deploymodel.Artifact, error) {
	bindings := map[string]artifactBinding{}
	for _, component := range components {
		for _, declared := range component.Artifacts {
			if prior, exists := bindings[declared.Output]; exists {
				if prior.Label != declared.Label || prior.Path != declared.Path {
					return nil, nil, fmt.Errorf("nomad artifact output %q is provided by both %s and %s", declared.Output, prior.Label, declared.Label)
				}
				continue
			}
			artifactPath := resolveWorkspacePath(repoRoot, declared.Path)
			body, err := os.ReadFile(artifactPath)
			if err != nil {
				return nil, nil, fmt.Errorf("read artifact %s: %w", declared.Path, err)
			}
			digest := deploymodel.SHA256(body)
			key := strings.Trim(policy.KeyPrefix, "/") + "/" + digest + "/" + declared.Output + ".tar"
			artifact := deploymodel.Artifact{
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
	artifacts := make([]deploymodel.Artifact, 0, len(bindings))
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

func loadAuthoredNomadSpec(ctx context.Context, parser authoredNomadSpecParser, path string) (*api.Job, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if parser == nil {
		return nil, fmt.Errorf("nomad HCL parser is required for %s", path)
	}
	return parser.ParseJobHCL(ctx, body, path)
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
	specDigest := deploymodel.SHA256(specBody)
	job.Meta["spec_sha256"] = specDigest
	return specDigest, nil
}

func resolveWorkspacePath(repoRoot, path string) string {
	if filepath.IsAbs(path) || repoRoot == "" {
		return path
	}
	return filepath.Join(repoRoot, filepath.FromSlash(path))
}

func submitDeploymentJobs(ctx context.Context, rt *runtime.Runtime, client *nomadclient.Client, deployment *nomadDeployment) error {
	for _, jobID := range deployment.SubmitOrder {
		job, ok := deployment.jobByID(jobID)
		if !ok {
			return fmt.Errorf("nomad deployment submit_order references missing job %q", jobID)
		}
		if err := submitOneDeploymentJob(ctx, rt, client, job); err != nil {
			return fmt.Errorf("%s: %w", jobID, err)
		}
	}
	return nil
}

func submitOneDeploymentJob(ctx context.Context, rt *runtime.Runtime, client *nomadclient.Client, job deploymodel.NomadJob) error {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.nomad.submit",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("nomad.job_id", job.JobID),
			attribute.String("nomad.spec_sha256", job.SpecSHA256),
			attribute.String("nomad.artifact_sha256", job.ArtifactSHA256),
		),
	)
	defer span.End()

	spec, err := nomadclient.ParseSpec(job.Spec, "nomad job "+job.JobID)
	if err != nil {
		recordFailure(span, err)
		return err
	}
	if spec.SpecDigest != job.SpecSHA256 {
		err := fmt.Errorf("nomad job %s spec_sha256=%s, parsed spec has %s", job.JobID, job.SpecSHA256, spec.SpecDigest)
		recordFailure(span, err)
		return err
	}
	if spec.ArtifactDigest != job.ArtifactSHA256 {
		err := fmt.Errorf("nomad job %s artifact_sha256=%s, parsed spec has %s", job.JobID, job.ArtifactSHA256, spec.ArtifactDigest)
		recordFailure(span, err)
		return err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, nomadSubmitTimeout)
	defer cancel()
	evidence := nomadJobEvidenceWriter{
		db:               rt.DeployDB,
		runKey:           rt.Identity.RunKey(),
		site:             rt.Site,
		unitID:           job.JobID,
		unitDependencies: job.DependsOn,
		unitPayloadKind:  "nomad_job",
	}
	if err := submitSpec(timeoutCtx, span, client, spec, evidence); err != nil {
		recordFailure(span, err)
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

func newGaragePublisher(ctx context.Context, sshClient *sshtun.Client, delivery deploymodel.ArtifactDelivery) (*garage.Publisher, error) {
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
