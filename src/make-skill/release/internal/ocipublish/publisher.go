package ocipublish

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

const (
	ArtifactTypeRelease = "application/vnd.verself.release.artifact.v1"
	MediaTypeReleaseTar = "application/vnd.verself.mksk.release.tar"
	MediaTypeInToto     = "application/vnd.in-toto+json"
	MediaTypeSPDX       = "application/spdx+json"

	AnnotationEvidenceKind = "sh.verself.release.evidence.kind"

	reproducibleCreated = "1970-01-01T00:00:00Z"
)

type RegistryAuth struct {
	Username string
	Password string
}

type File struct {
	Path        string
	MediaType   string
	Annotations map[string]string
}

type Manifest struct {
	ArtifactType string
	Files        []File
	Annotations  map[string]string
}

type Request struct {
	Reference string
	Subject   Manifest
	Referrers []Manifest
}

type PublishedManifest struct {
	ArtifactType string
	Descriptor   v1.Descriptor
	Layers       []v1.Descriptor
}

type Result struct {
	Reference string
	Subject   PublishedManifest
	Referrers []PublishedManifest
}

func NewRemoteTarget(registryURL string, repository string, credential RegistryAuth) (*remote.Repository, error) {
	endpoint, plainHTTP, err := parseRegistryEndpoint(registryURL)
	if err != nil {
		return nil, err
	}
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	if repository == "" {
		return nil, fmt.Errorf("OCI repository is required")
	}
	repo, err := remote.NewRepository(endpoint + "/" + repository)
	if err != nil {
		return nil, err
	}
	repo.PlainHTTP = plainHTTP
	repo.Client = &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.NewCache(),
		Credential: auth.StaticCredential(endpoint, auth.Credential{
			Username: credential.Username,
			Password: credential.Password,
		}),
	}
	if credential.Username == "" && credential.Password == "" {
		repo.Client = &auth.Client{Client: retry.DefaultClient, Cache: auth.NewCache()}
	}
	if err := repo.SetReferrersCapability(true); err != nil {
		return nil, err
	}
	return repo, nil
}

func Publish(ctx context.Context, target oras.GraphTarget, req Request) (Result, error) {
	if err := validateRequest(req); err != nil {
		return Result{}, err
	}
	staging := memory.New()
	subject, err := packManifest(ctx, staging, req.Subject, nil)
	if err != nil {
		return Result{}, err
	}
	if err := assertReferenceCanPointTo(ctx, target, req.Reference, subject.Descriptor); err != nil {
		return Result{}, err
	}
	if err := copyGraph(ctx, staging, target, subject.Descriptor); err != nil {
		return Result{}, err
	}
	if err := target.Tag(ctx, subject.Descriptor, req.Reference); err != nil {
		return Result{}, fmt.Errorf("tag OCI release %s: %w", req.Reference, err)
	}
	referrers := make([]PublishedManifest, 0, len(req.Referrers))
	for _, referrer := range req.Referrers {
		published, err := packManifest(ctx, staging, referrer, &subject.Descriptor)
		if err != nil {
			return Result{}, err
		}
		if err := copyGraph(ctx, staging, target, published.Descriptor); err != nil {
			return Result{}, err
		}
		referrers = append(referrers, published)
	}
	result := Result{Reference: req.Reference, Subject: subject, Referrers: referrers}
	if err := Verify(ctx, target, result); err != nil {
		return Result{}, err
	}
	return result, nil
}

func Verify(ctx context.Context, target oras.GraphTarget, result Result) error {
	resolved, err := target.Resolve(ctx, result.Reference)
	if err != nil {
		return fmt.Errorf("resolve published reference %s: %w", result.Reference, err)
	}
	if resolved.Digest != result.Subject.Descriptor.Digest {
		return fmt.Errorf("published reference %s points at %s, want %s", result.Reference, resolved.Digest, result.Subject.Descriptor.Digest)
	}
	if err := requireExists(ctx, target, result.Subject.Descriptor, "subject manifest"); err != nil {
		return err
	}
	predecessors, err := target.Predecessors(ctx, result.Subject.Descriptor)
	if err != nil {
		return fmt.Errorf("list OCI referrers for %s: %w", result.Subject.Descriptor.Digest, err)
	}
	seen := map[string]v1.Descriptor{}
	for _, descriptor := range predecessors {
		seen[descriptor.Digest.String()] = descriptor
	}
	for _, referrer := range result.Referrers {
		if _, ok := seen[referrer.Descriptor.Digest.String()]; !ok {
			return fmt.Errorf("missing OCI referrer %s for subject %s", referrer.Descriptor.Digest, result.Subject.Descriptor.Digest)
		}
		if err := requireExists(ctx, target, referrer.Descriptor, "referrer manifest"); err != nil {
			return err
		}
	}
	return nil
}

func parseRegistryEndpoint(raw string) (endpoint string, plainHTTP bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false, fmt.Errorf("registry URL is required")
	}
	if !strings.Contains(raw, "://") {
		endpoint := strings.Trim(raw, "/")
		if endpoint == "" {
			return "", false, fmt.Errorf("registry URL host is required")
		}
		if strings.Contains(endpoint, "/") {
			return "", false, fmt.Errorf("registry URL must not include a repository path")
		}
		return endpoint, false, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false, fmt.Errorf("parse registry URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false, fmt.Errorf("registry URL scheme must be http or https")
	}
	if parsed.Host == "" {
		return "", false, fmt.Errorf("registry URL host is required")
	}
	if strings.Trim(parsed.Path, "/") != "" {
		return "", false, fmt.Errorf("registry URL must not include a repository path")
	}
	return parsed.Host, parsed.Scheme == "http", nil
}

func validateRequest(req Request) error {
	if strings.TrimSpace(req.Reference) == "" {
		return fmt.Errorf("OCI reference tag is required")
	}
	if err := validateManifest(req.Subject, "release subject"); err != nil {
		return err
	}
	for i, referrer := range req.Referrers {
		if err := validateManifest(referrer, fmt.Sprintf("release referrer %d", i+1)); err != nil {
			return err
		}
	}
	return nil
}

func validateManifest(manifest Manifest, label string) error {
	if strings.TrimSpace(manifest.ArtifactType) == "" {
		return fmt.Errorf("%s artifact type is required", label)
	}
	if len(manifest.Files) == 0 {
		return fmt.Errorf("%s requires at least one file", label)
	}
	for _, file := range manifest.Files {
		if strings.TrimSpace(file.Path) == "" {
			return fmt.Errorf("%s file path is required", label)
		}
		if strings.TrimSpace(file.MediaType) == "" {
			return fmt.Errorf("%s file media type is required", label)
		}
	}
	return nil
}

func packManifest(ctx context.Context, target oras.Target, spec Manifest, subject *v1.Descriptor) (PublishedManifest, error) {
	layers := make([]v1.Descriptor, 0, len(spec.Files))
	for _, file := range spec.Files {
		data, err := os.ReadFile(file.Path)
		if err != nil {
			return PublishedManifest{}, fmt.Errorf("read OCI layer %s: %w", file.Path, err)
		}
		descriptor, err := oras.PushBytes(ctx, target, file.MediaType, data)
		if errors.Is(err, errdef.ErrAlreadyExists) {
			descriptor = content.NewDescriptorFromBytes(file.MediaType, data)
		} else if err != nil {
			return PublishedManifest{}, fmt.Errorf("push OCI layer %s: %w", file.Path, err)
		}
		descriptor.Annotations = cloneAnnotations(file.Annotations)
		layers = append(layers, descriptor)
	}
	annotations := cloneAnnotations(spec.Annotations)
	if _, ok := annotations[v1.AnnotationCreated]; !ok {
		annotations[v1.AnnotationCreated] = reproducibleCreated
	}
	manifest, err := oras.PackManifest(ctx, target, oras.PackManifestVersion1_1, spec.ArtifactType, oras.PackManifestOptions{
		Subject:             subject,
		Layers:              layers,
		ManifestAnnotations: annotations,
	})
	if err != nil {
		return PublishedManifest{}, fmt.Errorf("pack OCI manifest %s: %w", spec.ArtifactType, err)
	}
	return PublishedManifest{ArtifactType: spec.ArtifactType, Descriptor: manifest, Layers: layers}, nil
}

func assertReferenceCanPointTo(ctx context.Context, target oras.Target, reference string, descriptor v1.Descriptor) error {
	existing, err := target.Resolve(ctx, reference)
	if err == nil {
		if existing.Digest != descriptor.Digest {
			return fmt.Errorf("OCI reference %s already points at %s, refusing to retag %s", reference, existing.Digest, descriptor.Digest)
		}
		return nil
	}
	if errors.Is(err, errdef.ErrNotFound) {
		return nil
	}
	return fmt.Errorf("resolve OCI reference %s: %w", reference, err)
}

func copyGraph(ctx context.Context, source content.ReadOnlyStorage, target content.Storage, root v1.Descriptor) error {
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
		return fmt.Errorf("copy OCI graph rooted at %s: %w", root.Digest, err)
	}
	return nil
}

func requireExists(ctx context.Context, target content.Storage, descriptor v1.Descriptor, label string) error {
	exists, err := target.Exists(ctx, descriptor)
	if err != nil {
		return fmt.Errorf("check %s %s: %w", label, descriptor.Digest, err)
	}
	if !exists {
		return fmt.Errorf("%s %s does not exist in target registry", label, descriptor.Digest)
	}
	return nil
}

func cloneAnnotations(input map[string]string) map[string]string {
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
