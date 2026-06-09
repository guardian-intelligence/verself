package deployengine

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/nomad/api"
	"github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

var (
	ociDigestRE       = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	ociRegistryHostRE = regexp.MustCompile(`^[a-zA-Z0-9.-]+(?::[0-9]+)?$`)
	ociRepoSegmentRE  = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)
)

const (
	deploymentOCIPolicyRef   = "distribution-policies/deployments/oci-digest-v1"
	mediaTypeInTotoStatement = "application/vnd.in-toto+json"
	predicateSLSAProvenance  = "https://slsa.dev/provenance/v1"
)

type ociRegistryPolicy struct {
	OriginRegistryURL string `json:"origin_registry_url"`
	PublicRegistryURL string `json:"public_registry_url"`
	PushRegistry      string `json:"push_registry"`
	PullRegistry      string `json:"pull_registry"`
	RepositoryPrefix  string `json:"repository_prefix"`
	DockerConfigDir   string `json:"docker_config_dir"`
	Insecure          bool   `json:"insecure"`
}

type ociImageBinding struct {
	Output            string
	ImageLabel        string
	PushLabel         string
	DigestPath        string
	ManifestPath      string
	Digest            string
	MediaType         string
	SizeBytes         int64
	Repository        string
	PushRepository    string
	PullReference     string
	OriginRegistryURL string
	PublicRegistryURL string
	DockerConfigDir   string
	Insecure          bool
}

type OCIPusher interface {
	PushDeploymentImage(context.Context, OCIPushRequest) error
}

type OCIPushRequest struct {
	Site            string
	SHA             string
	DeployRunKey    string
	Output          string
	Digest          string
	Repository      string
	PushRepository  string
	PullReference   string
	PushLabel       string
	DockerConfigDir string
	Insecure        bool
	BazelBuildFlags []string
}

type OCIEvidencePublisher interface {
	PublishDeploymentEvidence(context.Context, DeploymentEvidencePublishRequest) ([]DeploymentImageEvidence, error)
}

type DistributionAdmitter interface {
	AdmitDeploymentImage(context.Context, DeploymentImageAdmissionRequest) (DeploymentImageAdmissionResult, error)
}

type DeploymentEvidencePublishRequest struct {
	Site              string
	SHA               string
	DeployRunKey      string
	Output            string
	Digest            string
	MediaType         string
	SizeBytes         int64
	Repository        string
	PushRepository    string
	PullReference     string
	ImageLabel        string
	PushLabel         string
	BuilderID         string
	SourceRepository  string
	SourceRef         string
	OriginRegistryURL string
	PublicRegistryURL string
	DockerConfigDir   string
	Insecure          bool
}

type DeploymentImageEvidence struct {
	EvidenceKind      string
	PredicateType     string
	SubjectDigest     string
	DocumentDigest    string
	OCIReferrerDigest string
}

type DeploymentImageAdmissionRequest struct {
	Site              string
	SHA               string
	DeployRunKey      string
	Output            string
	Digest            string
	MediaType         string
	SizeBytes         int64
	Repository        string
	PullReference     string
	ImageLabel        string
	PushLabel         string
	BuilderID         string
	SourceRepository  string
	SourceRef         string
	OriginRegistryURL string
	PublicRegistryURL string
	PolicyRef         string
	Evidence          []DeploymentImageEvidence
}

type DeploymentImageAdmissionResult struct {
	PublicOCIReference string
}

func validateOCIRegistryPolicy(siteConfigPath string, policy ociRegistryPolicy) error {
	if err := validateRegistryURL(policy.OriginRegistryURL, "oci_registry.origin_registry_url"); err != nil {
		return fmt.Errorf("%s: %w", siteConfigPath, err)
	}
	if err := validateRegistryURL(policy.PublicRegistryURL, "oci_registry.public_registry_url"); err != nil {
		return fmt.Errorf("%s: %w", siteConfigPath, err)
	}
	if err := validateRegistryHost(policy.PushRegistry, "oci_registry.push_registry"); err != nil {
		return fmt.Errorf("%s: %w", siteConfigPath, err)
	}
	if err := validateRegistryHost(policy.PullRegistry, "oci_registry.pull_registry"); err != nil {
		return fmt.Errorf("%s: %w", siteConfigPath, err)
	}
	prefix := strings.Trim(policy.RepositoryPrefix, "/")
	if prefix == "" {
		return fmt.Errorf("%s: oci_registry.repository_prefix is required", siteConfigPath)
	}
	for _, segment := range strings.Split(prefix, "/") {
		if !ociRepoSegmentRE.MatchString(segment) {
			return fmt.Errorf("%s: oci_registry.repository_prefix contains invalid segment %q", siteConfigPath, segment)
		}
	}
	if strings.TrimSpace(policy.DockerConfigDir) == "" {
		return fmt.Errorf("%s: oci_registry.docker_config_dir is required", siteConfigPath)
	}
	return nil
}

func validateRegistryURL(raw, field string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("%s is required", field)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse %s: %w", field, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s scheme must be http or https", field)
	}
	if parsed.Host == "" || strings.Trim(parsed.Path, "/") != "" {
		return fmt.Errorf("%s must be a registry URL without a repository path", field)
	}
	return nil
}

func validateRegistryHost(raw, field string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("%s is required", field)
	}
	if strings.Contains(raw, "://") || strings.Contains(raw, "/") || !ociRegistryHostRE.MatchString(raw) {
		return fmt.Errorf("%s must be a registry host[:port] without scheme or path", field)
	}
	return nil
}

func bindNomadOCIImages(repoRoot string, policy ociRegistryPolicy, components []nomadComponentDescriptor) (map[string]ociImageBinding, error) {
	bindings := map[string]ociImageBinding{}
	for _, component := range components {
		for _, declared := range component.OCIImages {
			if err := bindNomadOCIImage(repoRoot, policy, declared, bindings); err != nil {
				return nil, err
			}
		}
	}
	return bindings, nil
}

func bindNomadOCIImage(repoRoot string, policy ociRegistryPolicy, declared nomadDescriptorOCIImage, bindings map[string]ociImageBinding) error {
	if prior, exists := bindings[declared.Output]; exists {
		if prior.ImageLabel != declared.ImageLabel || prior.DigestPath != declared.DigestPath || prior.PushLabel != declared.PushLabel {
			return fmt.Errorf("OCI image output %q is provided by both %s and %s", declared.Output, prior.ImageLabel, declared.ImageLabel)
		}
		return nil
	}
	digestPath := resolveWorkspacePath(repoRoot, declared.DigestPath)
	body, err := os.ReadFile(digestPath)
	if err != nil {
		return fmt.Errorf("read OCI digest %s: %w", declared.DigestPath, err)
	}
	digest := strings.TrimSpace(string(body))
	if !ociDigestRE.MatchString(digest) {
		return fmt.Errorf("OCI digest %s for %q must match sha256:<64 lowercase hex>", digest, declared.Output)
	}
	manifestPath, manifestBody, err := readOCIManifestForDigest(digestPath, digest)
	if err != nil {
		return fmt.Errorf("read OCI manifest for %q: %w", declared.Output, err)
	}
	var manifest struct {
		MediaType string `json:"mediaType"`
	}
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		return fmt.Errorf("decode OCI manifest %s: %w", manifestPath, err)
	}
	manifest.MediaType = strings.TrimSpace(manifest.MediaType)
	if manifest.MediaType == "" {
		return fmt.Errorf("OCI manifest %s for %q must declare mediaType", manifestPath, declared.Output)
	}
	if !ociRepoSegmentRE.MatchString(declared.Output) {
		return fmt.Errorf("OCI image output %q cannot be used as a repository segment", declared.Output)
	}
	repository := path.Join(strings.Trim(policy.RepositoryPrefix, "/"), declared.Output)
	bindings[declared.Output] = ociImageBinding{
		Output:            declared.Output,
		ImageLabel:        declared.ImageLabel,
		PushLabel:         declared.PushLabel,
		DigestPath:        declared.DigestPath,
		ManifestPath:      manifestPath,
		Digest:            digest,
		MediaType:         manifest.MediaType,
		SizeBytes:         int64(len(manifestBody)),
		Repository:        repository,
		PushRepository:    strings.TrimRight(policy.PushRegistry, "/") + "/" + repository,
		PullReference:     strings.TrimRight(policy.PullRegistry, "/") + "/" + repository + "@" + digest,
		OriginRegistryURL: policy.OriginRegistryURL,
		PublicRegistryURL: policy.PublicRegistryURL,
		DockerConfigDir:   policy.DockerConfigDir,
		Insecure:          policy.Insecure,
	}
	return nil
}

func readOCIManifestForDigest(digestPath, rawDigest string) (string, []byte, error) {
	if !strings.HasSuffix(digestPath, ".sha256") {
		return "", nil, fmt.Errorf("OCI digest path %s must end in .sha256", digestPath)
	}
	digestHex := strings.TrimPrefix(rawDigest, "sha256:")
	manifestPath := strings.TrimSuffix(digestPath, ".sha256")
	candidates := []string{manifestPath}
	if strings.HasSuffix(manifestPath, ".json") {
		layoutDir := strings.TrimSuffix(manifestPath, ".json")
		candidates = append([]string{filepath.Join(layoutDir, "blobs", "sha256", digestHex)}, candidates...)
	}
	var missing []string
	for _, candidate := range candidates {
		body, err := os.ReadFile(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				missing = append(missing, candidate)
				continue
			}
			return "", nil, fmt.Errorf("%s: %w", candidate, err)
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != digestHex {
			return "", nil, fmt.Errorf("%s digest does not match %s", candidate, rawDigest)
		}
		return candidate, body, nil
	}
	return "", nil, fmt.Errorf("manifest not found; tried %s", strings.Join(missing, ", "))
}

func publishOCIImages(ctx context.Context, exec execution, inputs *deployInputs) error {
	if len(inputs.OCIImages) == 0 {
		return nil
	}
	keys := make([]string, 0, len(inputs.OCIImages))
	for output := range inputs.OCIImages {
		keys = append(keys, output)
	}
	sort.Strings(keys)
	ctx, span := exec.Tracer.Start(ctx, "verself_deploy.oci.publish",
		trace.WithAttributes(
			attribute.String("verself.site", exec.Site),
			attribute.String("verself.deploy_run_key", inputs.DeployRunKey),
			attribute.String("verself.deploy_sha", inputs.SHA),
			attribute.Int("verself.oci_image_count", len(keys)),
		),
	)
	defer span.End()
	for _, output := range keys {
		binding := inputs.OCIImages[output]
		req := OCIPushRequest{
			Site:            exec.Site,
			SHA:             inputs.SHA,
			DeployRunKey:    inputs.DeployRunKey,
			Output:          binding.Output,
			Digest:          binding.Digest,
			Repository:      binding.Repository,
			PushRepository:  binding.PushRepository,
			PullReference:   binding.PullReference,
			PushLabel:       binding.PushLabel,
			DockerConfigDir: binding.DockerConfigDir,
			Insecure:        binding.Insecure,
			BazelBuildFlags: append([]string(nil), exec.BazelBuildFlags...),
		}
		if err := pushOCIImage(ctx, exec, req); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

func admitOCIImages(ctx context.Context, exec execution, inputs *deployInputs) error {
	if len(inputs.OCIImages) == 0 {
		return nil
	}
	if exec.DistributionAdmitter == nil && !exec.AllowBootstrapOCIWithoutDistributionAdmission {
		return fmt.Errorf("distribution admission is required for OCI images")
	}
	if strings.TrimSpace(exec.BuilderID) == "" {
		return fmt.Errorf("builder id is required for OCI image admission")
	}
	if strings.TrimSpace(exec.SourceRepository) == "" || strings.TrimSpace(exec.SourceRef) == "" {
		return fmt.Errorf("source repository and ref are required for OCI image admission")
	}
	keys := make([]string, 0, len(inputs.OCIImages))
	for output := range inputs.OCIImages {
		keys = append(keys, output)
	}
	sort.Strings(keys)
	ctx, span := exec.Tracer.Start(ctx, "verself_deploy.oci.admit",
		trace.WithAttributes(
			attribute.String("verself.site", exec.Site),
			attribute.String("verself.deploy_run_key", inputs.DeployRunKey),
			attribute.String("verself.deploy_sha", inputs.SHA),
			attribute.Int("verself.oci_image_count", len(keys)),
		),
	)
	defer span.End()
	for _, output := range keys {
		binding := inputs.OCIImages[output]
		evidence, err := publishOCIEvidence(ctx, exec, binding, inputs)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		if exec.DistributionAdmitter == nil {
			if _, err := fmt.Fprintf(exec.stdout(), "deployment-service: bootstrap OCI distribution admission skipped output=%s digest=%s reason=distribution-service-unavailable-during-bootstrap\n", binding.Output, binding.Digest); err != nil {
				return fmt.Errorf("write bootstrap OCI admission status: %w", err)
			}
			continue
		}
		req := DeploymentImageAdmissionRequest{
			Site:              exec.Site,
			SHA:               inputs.SHA,
			DeployRunKey:      inputs.DeployRunKey,
			Output:            binding.Output,
			Digest:            binding.Digest,
			MediaType:         binding.MediaType,
			SizeBytes:         binding.SizeBytes,
			Repository:        binding.Repository,
			PullReference:     binding.PullReference,
			ImageLabel:        binding.ImageLabel,
			PushLabel:         binding.PushLabel,
			BuilderID:         strings.TrimSpace(exec.BuilderID),
			SourceRepository:  strings.TrimSpace(exec.SourceRepository),
			SourceRef:         strings.TrimSpace(exec.SourceRef),
			OriginRegistryURL: binding.OriginRegistryURL,
			PublicRegistryURL: binding.PublicRegistryURL,
			PolicyRef:         deploymentOCIPolicyRef,
			Evidence:          evidence,
		}
		if _, err := admitOCIImage(ctx, exec, req); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

func publishOCIEvidence(ctx context.Context, exec execution, binding ociImageBinding, inputs *deployInputs) ([]DeploymentImageEvidence, error) {
	req := DeploymentEvidencePublishRequest{
		Site:              exec.Site,
		SHA:               inputs.SHA,
		DeployRunKey:      inputs.DeployRunKey,
		Output:            binding.Output,
		Digest:            binding.Digest,
		MediaType:         binding.MediaType,
		SizeBytes:         binding.SizeBytes,
		Repository:        binding.Repository,
		PushRepository:    binding.PushRepository,
		PullReference:     binding.PullReference,
		ImageLabel:        binding.ImageLabel,
		PushLabel:         binding.PushLabel,
		BuilderID:         strings.TrimSpace(exec.BuilderID),
		SourceRepository:  strings.TrimSpace(exec.SourceRepository),
		SourceRef:         strings.TrimSpace(exec.SourceRef),
		OriginRegistryURL: binding.OriginRegistryURL,
		PublicRegistryURL: binding.PublicRegistryURL,
		DockerConfigDir:   binding.DockerConfigDir,
		Insecure:          binding.Insecure,
	}
	if exec.OCIEvidencePublisher != nil {
		return exec.OCIEvidencePublisher.PublishDeploymentEvidence(ctx, req)
	}
	return DefaultOCIEvidencePublisher{}.PublishDeploymentEvidence(ctx, req)
}

func admitOCIImage(ctx context.Context, exec execution, req DeploymentImageAdmissionRequest) (DeploymentImageAdmissionResult, error) {
	ctx, span := exec.Tracer.Start(ctx, "verself_deploy.oci.admit_image",
		trace.WithAttributes(
			attribute.String("verself.site", req.Site),
			attribute.String("verself.deploy_run_key", req.DeployRunKey),
			attribute.String("verself.deploy_sha", req.SHA),
			attribute.String("verself.oci_output", req.Output),
			attribute.String("verself.oci_digest", req.Digest),
			attribute.String("verself.oci_repository", req.Repository),
			attribute.String("verself.oci_pull_reference", req.PullReference),
			attribute.String("bazel.target", req.ImageLabel),
		),
	)
	defer span.End()
	result, err := exec.DistributionAdmitter.AdmitDeploymentImage(ctx, req)
	if err != nil {
		err = fmt.Errorf("admit OCI image %s %s: %w", req.Output, req.Digest, err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return DeploymentImageAdmissionResult{}, err
	}
	if strings.TrimSpace(result.PublicOCIReference) != "" {
		span.SetAttributes(attribute.String("verself.oci_public_reference", result.PublicOCIReference))
	}
	span.SetStatus(codes.Ok, "")
	return result, nil
}

func pushOCIImage(ctx context.Context, exec execution, req OCIPushRequest) error {
	if exec.OCIPusher != nil {
		return exec.OCIPusher.PushDeploymentImage(ctx, req)
	}
	ctx, span := exec.Tracer.Start(ctx, "verself_deploy.oci.push",
		trace.WithAttributes(
			attribute.String("verself.site", req.Site),
			attribute.String("verself.deploy_run_key", req.DeployRunKey),
			attribute.String("verself.deploy_sha", req.SHA),
			attribute.String("verself.oci_output", req.Output),
			attribute.String("verself.oci_digest", req.Digest),
			attribute.String("verself.oci_repository", req.Repository),
			attribute.String("verself.oci_push_repository", req.PushRepository),
			attribute.String("verself.oci_pull_reference", req.PullReference),
			attribute.String("bazel.target", req.PushLabel),
		),
	)
	defer span.End()
	args := make([]string, 0, len(req.BazelBuildFlags)+8)
	args = append(args, "run")
	args = append(args, req.BazelBuildFlags...)
	args = append(args, req.PushLabel, "--", "--repository", req.PushRepository)
	if req.Insecure {
		args = append(args, "--insecure")
	}
	cmd := execCommandContext(ctx, "bazelisk", args...)
	cmd.Dir = exec.RepoRoot
	cmd.Env = os.Environ()
	if strings.TrimSpace(req.DockerConfigDir) != "" {
		cmd.Env = append(cmd.Env, "DOCKER_CONFIG="+req.DockerConfigDir)
	}
	cmd.Stdout = exec.stdout()
	cmd.Stderr = exec.stdout()
	if err := cmd.Run(); err != nil {
		err = fmt.Errorf("push OCI image %s to %s: %w", req.Output, req.PushRepository, err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

// DefaultOCIEvidencePublisher publishes deployment SLSA provenance as an OCI
// referrer next to the pushed workload image.
type DefaultOCIEvidencePublisher struct{}

func (DefaultOCIEvidencePublisher) PublishDeploymentEvidence(ctx context.Context, req DeploymentEvidencePublishRequest) ([]DeploymentImageEvidence, error) {
	if err := validateDeploymentEvidenceRequest(req); err != nil {
		return nil, err
	}
	target, err := newDeploymentEvidenceTarget(req)
	if err != nil {
		return nil, err
	}
	statement, err := deploymentSLSAStatement(req)
	if err != nil {
		return nil, err
	}
	documentSum := sha256.Sum256(statement)
	documentDigest := "sha256:" + hex.EncodeToString(documentSum[:])
	staging := memory.New()
	layer, err := oras.PushBytes(ctx, staging, mediaTypeInTotoStatement, statement)
	if errors.Is(err, errdef.ErrAlreadyExists) {
		layer = content.NewDescriptorFromBytes(mediaTypeInTotoStatement, statement)
	} else if err != nil {
		return nil, fmt.Errorf("push SLSA provenance layer: %w", err)
	}
	layer.Annotations = map[string]string{
		v1.AnnotationTitle: "deployment.slsa.intoto.json",
	}
	subject := v1.Descriptor{
		MediaType: req.MediaType,
		Digest:    digest.Digest(req.Digest),
		Size:      req.SizeBytes,
	}
	manifest, err := oras.PackManifest(ctx, staging, oras.PackManifestVersion1_1, mediaTypeInTotoStatement, oras.PackManifestOptions{
		Subject: &subject,
		Layers:  []v1.Descriptor{layer},
		ManifestAnnotations: map[string]string{
			v1.AnnotationCreated:              "1970-01-01T00:00:00Z",
			v1.AnnotationTitle:                "deployment.slsa.intoto.json",
			"sh.verself.deploy.evidence.kind": "slsa_provenance",
			"sh.verself.deploy.run_key":       req.DeployRunKey,
			"sh.verself.deploy.source_sha":    req.SHA,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("pack SLSA provenance referrer: %w", err)
	}
	if err := copyOCIGraph(ctx, staging, target, manifest); err != nil {
		return nil, err
	}
	if err := requireOCIReferrer(ctx, target, subject, manifest); err != nil {
		return nil, err
	}
	return []DeploymentImageEvidence{{
		EvidenceKind:      "slsa_provenance",
		PredicateType:     predicateSLSAProvenance,
		SubjectDigest:     req.Digest,
		DocumentDigest:    documentDigest,
		OCIReferrerDigest: manifest.Digest.String(),
	}}, nil
}

func validateDeploymentEvidenceRequest(req DeploymentEvidencePublishRequest) error {
	if req.Output == "" || req.Digest == "" || req.MediaType == "" || req.SizeBytes <= 0 || req.PushRepository == "" {
		return fmt.Errorf("OCI evidence publication requires output, digest, media type, size, and push repository")
	}
	if !ociDigestRE.MatchString(req.Digest) {
		return fmt.Errorf("OCI evidence digest %s must match sha256:<64 lowercase hex>", req.Digest)
	}
	if req.BuilderID == "" || req.SourceRepository == "" || req.SourceRef == "" || req.SHA == "" || req.ImageLabel == "" {
		return fmt.Errorf("OCI evidence publication requires builder, source, sha, and image label")
	}
	return nil
}

func newDeploymentEvidenceTarget(req DeploymentEvidencePublishRequest) (*remote.Repository, error) {
	repo, err := remote.NewRepository(req.PushRepository)
	if err != nil {
		return nil, fmt.Errorf("create OCI repository %s: %w", req.PushRepository, err)
	}
	repo.PlainHTTP = req.Insecure
	credential, err := dockerConfigCredential(req.DockerConfigDir, registryHost(req.PushRepository))
	if err != nil {
		return nil, err
	}
	repo.Client = &auth.Client{
		Client:     retry.DefaultClient,
		Cache:      auth.NewCache(),
		Credential: auth.StaticCredential(registryHost(req.PushRepository), credential),
	}
	if credential.Username == "" && credential.Password == "" {
		repo.Client = &auth.Client{Client: retry.DefaultClient, Cache: auth.NewCache()}
	}
	if err := repo.SetReferrersCapability(true); err != nil {
		return nil, fmt.Errorf("enable OCI referrers for %s: %w", req.PushRepository, err)
	}
	return repo, nil
}

func registryHost(repository string) string {
	host, _, _ := strings.Cut(strings.TrimSpace(repository), "/")
	return host
}

func dockerConfigCredential(configDir string, registry string) (auth.Credential, error) {
	if strings.TrimSpace(configDir) == "" {
		return auth.Credential{}, nil
	}
	path := path.Join(configDir, "config.json")
	body, err := os.ReadFile(path)
	if err != nil {
		return auth.Credential{}, fmt.Errorf("read Docker auth config %s: %w", path, err)
	}
	var cfg struct {
		Auths map[string]struct {
			Auth     string `json:"auth"`
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		return auth.Credential{}, fmt.Errorf("decode Docker auth config %s: %w", path, err)
	}
	for _, key := range []string{registry, "http://" + registry, "https://" + registry} {
		entry, ok := cfg.Auths[key]
		if !ok {
			continue
		}
		if entry.Username != "" || entry.Password != "" {
			return auth.Credential{Username: entry.Username, Password: entry.Password}, nil
		}
		if entry.Auth == "" {
			return auth.Credential{}, nil
		}
		raw, err := base64.StdEncoding.DecodeString(entry.Auth)
		if err != nil {
			return auth.Credential{}, fmt.Errorf("decode Docker auth for %s: %w", registry, err)
		}
		username, password, ok := strings.Cut(string(raw), ":")
		if !ok {
			return auth.Credential{}, fmt.Errorf("docker auth for %s must be username:password", registry)
		}
		return auth.Credential{Username: username, Password: password}, nil
	}
	return auth.Credential{}, fmt.Errorf("docker auth config %s has no credentials for %s", path, registry)
}

func deploymentSLSAStatement(req DeploymentEvidencePublishRequest) ([]byte, error) {
	subjectDigest := strings.TrimPrefix(req.Digest, "sha256:")
	statement := map[string]any{
		"_type": "https://in-toto.io/Statement/v1",
		"subject": []map[string]any{
			{
				"name": req.PullReference,
				"digest": map[string]string{
					"sha256": subjectDigest,
				},
			},
		},
		"predicateType": predicateSLSAProvenance,
		"predicate": map[string]any{
			"buildDefinition": map[string]any{
				"buildType": "https://bazel.build/rules_oci/oci_image",
				"externalParameters": map[string]any{
					"bazelTarget":     req.ImageLabel,
					"bazelPushTarget": req.PushLabel,
					"ociRepository":   req.Repository,
					"site":            req.Site,
				},
				"resolvedDependencies": []map[string]any{
					{
						"uri": "git+https://github.com/" + req.SourceRepository + "@" + req.SHA,
						"digest": map[string]string{
							"gitCommit": req.SHA,
						},
					},
				},
			},
			"runDetails": map[string]any{
				"builder": map[string]any{
					"id": req.BuilderID,
				},
				"metadata": map[string]any{
					"invocationId": req.DeployRunKey,
				},
			},
		},
	}
	return json.Marshal(statement)
}

func copyOCIGraph(ctx context.Context, source content.ReadOnlyStorage, target content.Storage, root v1.Descriptor) error {
	err := oras.CopyGraph(ctx, source, target, root, oras.CopyGraphOptions{
		PreCopy: func(ctx context.Context, descriptor v1.Descriptor) error {
			exists, err := target.Exists(ctx, descriptor)
			if err != nil {
				return err
			}
			if exists {
				return oras.SkipNode
			}
			return nil
		},
	})
	if err != nil {
		return fmt.Errorf("copy OCI evidence graph rooted at %s: %w", root.Digest, err)
	}
	return nil
}

func requireOCIReferrer(ctx context.Context, target oras.GraphTarget, subject v1.Descriptor, referrer v1.Descriptor) error {
	predecessors, err := target.Predecessors(ctx, subject)
	if err != nil {
		return fmt.Errorf("list OCI referrers for %s: %w", subject.Digest, err)
	}
	for _, predecessor := range predecessors {
		if predecessor.Digest == referrer.Digest {
			return nil
		}
	}
	return fmt.Errorf("missing OCI referrer %s for subject %s", referrer.Digest, subject.Digest)
}

func bindOCIImagesInSpec(job *api.Job, bindings map[string]ociImageBinding) (map[string]bool, error) {
	seen := map[string]bool{}
	for _, group := range job.TaskGroups {
		for _, task := range group.Tasks {
			if task == nil || task.Config == nil {
				continue
			}
			image, ok := task.Config["image"].(string)
			if !ok || !strings.HasPrefix(image, ociImageSourcePrefix) {
				continue
			}
			output := strings.TrimPrefix(image, ociImageSourcePrefix)
			binding, ok := bindings[output]
			if !ok {
				return nil, fmt.Errorf("OCI image %q is referenced by authored spec but not declared by nomad_component", output)
			}
			task.Config["image"] = binding.PullReference
			seen[output] = true
		}
	}
	return seen, nil
}

var execCommandContext = exec.CommandContext
