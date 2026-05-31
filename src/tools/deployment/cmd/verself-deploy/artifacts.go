package main

import (
	"context"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/nomad/api"

	"github.com/verself/deployment-tools/internal/deploymodel"
	"github.com/verself/deployment-tools/internal/runtime"
	"github.com/verself/deployment-tools/internal/s3artifact"
	"github.com/verself/deployment-tools/r2control"
)

const taskArtifactRoot = "local/verself-artifacts"

type artifactBinding struct {
	Artifact deploymodel.Artifact
	Checksum string
	Label    string
	Path     string
}

func bindNomadArtifacts(repoRoot string, policy artifactDeliveryPolicy, components []nomadComponentDescriptor) (map[string]artifactBinding, []deploymodel.Artifact, error) {
	bindings := map[string]artifactBinding{}
	for _, component := range components {
		for _, declared := range component.Artifacts {
			if err := bindNomadArtifact(repoRoot, policy, declared, bindings); err != nil {
				return nil, nil, err
			}
		}
		for _, declared := range component.PreArtifacts {
			if err := bindNomadArtifact(repoRoot, policy, declared, bindings); err != nil {
				return nil, nil, err
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

func bindNomadArtifact(repoRoot string, policy artifactDeliveryPolicy, declared nomadDescriptorArtifact, bindings map[string]artifactBinding) error {
	if prior, exists := bindings[declared.Output]; exists {
		if prior.Label != declared.Label || prior.Path != declared.Path {
			return fmt.Errorf("nomad artifact output %q is provided by both %s and %s", declared.Output, prior.Label, declared.Label)
		}
		return nil
	}
	artifactPath := resolveWorkspacePath(repoRoot, declared.Path)
	body, err := os.ReadFile(artifactPath)
	if err != nil {
		return fmt.Errorf("read artifact %s: %w", declared.Path, err)
	}
	digest := deploymodel.SHA256(body)
	key := artifactKey(policy, digest, declared.Output)
	getterSource := artifactGetterSource(policy, key)
	artifact := deploymodel.Artifact{
		Output:        declared.Output,
		LocalPath:     artifactPath,
		SHA256:        digest,
		Bucket:        policy.Bucket,
		Key:           key,
		GetterSource:  getterSource,
		GetterOptions: policy.GetterOptions,
	}
	bindings[declared.Output] = artifactBinding{
		Artifact: artifact,
		Checksum: policy.ChecksumAlgorithm + ":" + digest,
		Label:    declared.Label,
		Path:     declared.Path,
	}
	return nil
}

func artifactKey(policy artifactDeliveryPolicy, digest, output string) string {
	return strings.Trim(policy.KeyPrefix, "/") + "/" + digest + "/" + output + ".tar"
}

func artifactGetterSource(policy artifactDeliveryPolicy, key string) string {
	return strings.TrimRight(policy.GetterSourcePrefix, "/") + "/" + key
}

func publishArtifacts(ctx context.Context, rt *runtime.Runtime, delivery artifactDeliveryPolicy, artifacts []deploymodel.Artifact) error {
	if len(artifacts) == 0 {
		return nil
	}
	pub, err := newArtifactPublisher(ctx, delivery)
	if err != nil {
		return err
	}
	return pub.PublishAll(ctx, artifacts, rt.RepoRoot)
}

func taskArtifactDestination(output string) string {
	return path.Join(taskArtifactRoot, output)
}

func newArtifactPublisher(ctx context.Context, delivery artifactDeliveryPolicy) (*s3artifact.Publisher, error) {
	parent, err := r2control.LoadParentCredentials(ctx, r2control.ParentCredentialConfig{})
	if err != nil {
		return nil, err
	}
	if parent.SessionToken != "" {
		return nil, fmt.Errorf("R2 artifact publisher requires a non-temporary parent credential")
	}
	if strings.TrimSpace(parent.APIToken) == "" {
		return nil, fmt.Errorf("R2 artifact publisher requires the parent Cloudflare API token value")
	}
	prefix := strings.Trim(strings.TrimSpace(delivery.KeyPrefix), "/")
	if prefix == "" {
		return nil, fmt.Errorf("artifact_delivery.key_prefix is required")
	}
	apiClient, err := r2control.NewCloudflareAPIClient(parent.APIToken, 30*time.Second)
	if err != nil {
		return nil, err
	}
	temp, err := apiClient.CreateTemporaryCredentials(ctx, delivery.CloudflareAccountID, r2control.TemporaryCredentialRequest{
		ParentAccessKeyID: parent.AccessKeyID,
		Bucket:            delivery.Bucket,
		Permission:        r2control.TemporaryPermissionObjectReadWrite,
		Prefixes:          []string{prefix + "/"},
		TTL:               30 * time.Minute,
	})
	if err != nil {
		return nil, err
	}
	return s3artifact.New(delivery.ArtifactDelivery, s3artifact.Config{
		AccessKeyID:      temp.AccessKeyID,
		SecretAccessKey:  temp.SecretAccessKey,
		SessionToken:     temp.SessionToken,
		SkipBucketEnsure: true,
	})
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
				task.Artifacts = append(task.Artifacts, taskArtifact(binding, destination))
				task.Env[key] = destination
				seen[output] = true
			}
		}
	}
	return seen, nil
}

func taskArtifact(binding artifactBinding, destination string) *api.TaskArtifact {
	getterOptions := map[string]string{}
	for key, value := range binding.Artifact.GetterOptions {
		getterOptions[key] = value
	}
	getterOptions["checksum"] = binding.Checksum
	source := binding.Artifact.GetterSource
	return &api.TaskArtifact{
		GetterSource:  &source,
		GetterOptions: getterOptions,
		RelativeDest:  &destination,
		Chown:         true,
	}
}
