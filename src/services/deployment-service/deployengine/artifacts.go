package deployengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/hashicorp/nomad/api"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-service/internal/deploymodel"
)

const taskArtifactRoot = "local/verself-artifacts"

type artifactBinding struct {
	Artifact deploymodel.Artifact
	Checksum string
	Label    string
	Path     string
}

type uploadCandidate struct {
	Artifact  deploymodel.Artifact
	Body      []byte
	LocalPath string
	SizeBytes int64
	Label     string
}

func bindNomadArtifacts(repoRoot string, components []nomadComponentDescriptor) (map[string]artifactBinding, []deploymodel.Artifact, error) {
	bindings := map[string]artifactBinding{}
	for _, component := range components {
		for _, declared := range component.Artifacts {
			if err := bindNomadArtifact(repoRoot, declared, bindings); err != nil {
				return nil, nil, err
			}
		}
		for _, declared := range component.PreArtifacts {
			if err := bindNomadArtifact(repoRoot, declared, bindings); err != nil {
				return nil, nil, err
			}
		}
	}
	artifacts := make([]deploymodel.Artifact, 0, len(bindings))
	for _, binding := range bindings {
		artifacts = append(artifacts, binding.Artifact)
	}
	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].Output < artifacts[j].Output
	})
	return bindings, artifacts, nil
}

func bindNomadArtifact(repoRoot string, declared nomadDescriptorArtifact, bindings map[string]artifactBinding) error {
	if prior, exists := bindings[declared.Output]; exists {
		if prior.Label != declared.Label || prior.Path != declared.Path {
			return fmt.Errorf("nomad artifact output %q is provided by both %s and %s", declared.Output, prior.Label, declared.Label)
		}
		return nil
	}
	artifactPath := resolveWorkspacePath(repoRoot, declared.Path)
	digest, err := fileSHA256(artifactPath)
	if err != nil {
		return fmt.Errorf("hash artifact %s: %w", declared.Path, err)
	}
	artifact := deploymodel.Artifact{
		Output:    declared.Output,
		LocalPath: artifactPath,
		SHA256:    digest,
	}
	bindings[declared.Output] = artifactBinding{
		Artifact: artifact,
		Checksum: "sha256:" + digest,
		Label:    declared.Label,
		Path:     declared.Path,
	}
	return nil
}

func fileSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func publishArtifacts(ctx context.Context, exec execution, inputs *deployInputs) error {
	candidates, err := uploadCandidates(inputs, exec.RepoRoot)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return nil
	}
	return publishArtifactsWithCustomPublisher(ctx, exec, inputs, candidates)
}

func publishArtifactsWithCustomPublisher(ctx context.Context, exec execution, inputs *deployInputs, candidates []uploadCandidate) error {
	ctx, span := exec.Tracer.Start(ctx, "verself_deploy.artifacts.publish_custom",
		trace.WithAttributes(
			attribute.String("verself.site", exec.Site),
			attribute.String("verself.deploy_run_key", inputs.DeployRunKey),
			attribute.String("verself.artifact_namespace", inputs.ArtifactNamespace),
			attribute.Int("verself.artifact_count", len(candidates)),
		),
	)
	defer span.End()
	req := ArtifactPublishRequest{
		Site:              exec.Site,
		ArtifactNamespace: inputs.ArtifactNamespace,
		DeployRunKey:      inputs.DeployRunKey,
		Artifacts:         make([]ArtifactPublishCandidate, 0, len(candidates)),
	}
	for _, candidate := range candidates {
		req.Artifacts = append(req.Artifacts, ArtifactPublishCandidate{
			Output:    candidate.Artifact.Output,
			SHA256:    candidate.Artifact.SHA256,
			LocalPath: candidate.LocalPath,
			Body:      candidate.Body,
			SizeBytes: candidate.SizeBytes,
			Label:     candidate.Label,
		})
	}
	result, err := exec.ArtifactPublisher.PublishDeploymentArtifacts(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := applyArtifactGetterSources(inputs, result.GetterSources); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

func applyArtifactGetterSources(inputs *deployInputs, sources map[string]string) error {
	for output, binding := range inputs.Bindings {
		getterSource := strings.TrimSpace(sources[output])
		if getterSource == "" {
			return fmt.Errorf("artifact publisher omitted getter source for %q", output)
		}
		binding.Artifact.GetterSource = getterSource
		inputs.Bindings[output] = binding
	}
	return nil
}

func uploadCandidates(inputs *deployInputs, repoRoot string) ([]uploadCandidate, error) {
	candidates := make([]uploadCandidate, 0, len(inputs.Artifacts))
	for _, artifact := range inputs.Artifacts {
		localPath := artifact.ResolveLocalPath(repoRoot)
		info, err := os.Stat(localPath)
		if err != nil {
			return nil, fmt.Errorf("stat artifact %s: %w", artifact.Output, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("artifact %s is not a regular file", artifact.Output)
		}
		candidates = append(candidates, uploadCandidate{Artifact: artifact, LocalPath: localPath, SizeBytes: info.Size(), Label: artifact.Output})
	}
	return candidates, nil
}

func taskArtifactDestination(output string) string {
	return path.Join(taskArtifactRoot, output)
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
				getterOptions["checksum"] = binding.Checksum
				artifact.GetterSource = &binding.Artifact.GetterSource
				artifact.GetterOptions = getterOptions
				seen[output] = true
			}
			for key, value := range task.Env {
				if !strings.HasPrefix(value, artifactSourcePrefix) {
					continue
				}
				output := strings.TrimPrefix(value, artifactSourcePrefix)
				binding, ok := bindings[output]
				if !ok {
					return nil, fmt.Errorf("artifact %q is referenced by authored spec but not declared by nomad_component", output)
				}
				destination := taskArtifactDestination(output)
				task.Artifacts = append(task.Artifacts, taskArtifact(binding, destination, task.User))
				task.Env[key] = destination
				seen[output] = true
			}
		}
	}
	return seen, nil
}

func taskArtifact(binding artifactBinding, destination, taskUser string) *api.TaskArtifact {
	getterOptions := map[string]string{}
	getterOptions["checksum"] = binding.Checksum
	source := binding.Artifact.GetterSource
	return &api.TaskArtifact{
		GetterSource:  &source,
		GetterOptions: getterOptions,
		RelativeDest:  &destination,
		Chown:         taskArtifactChown(taskUser),
	}
}

func taskArtifactChown(taskUser string) bool {
	return strings.TrimSpace(taskUser) != "" && strings.TrimSpace(taskUser) != "root"
}
