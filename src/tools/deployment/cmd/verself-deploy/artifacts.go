package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/verself/deployment-tools/internal/deploymodel"
	"github.com/verself/deployment-tools/internal/runtime"
	"github.com/verself/deployment-tools/internal/s3artifact"
)

const taskArtifactRoot = "local/verself-artifacts"

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

func publishArtifacts(ctx context.Context, rt *runtime.Runtime, delivery deploymodel.ArtifactDelivery, artifacts []deploymodel.Artifact) error {
	if len(artifacts) == 0 {
		return nil
	}
	pub, err := newArtifactPublisher(delivery)
	if err != nil {
		return err
	}
	return pub.PublishAll(ctx, artifacts, rt.RepoRoot)
}

func taskArtifactDestination(output string) string {
	return path.Join(taskArtifactRoot, output)
}

func newArtifactPublisher(delivery deploymodel.ArtifactDelivery) (*s3artifact.Publisher, error) {
	access, secret, err := publisherCredentialPair(delivery.PublisherCredentials)
	if err != nil {
		return nil, err
	}
	return s3artifact.New(delivery, s3artifact.Config{AccessKeyID: access, SecretAccessKey: secret})
}

func publisherCredentialPair(creds deploymodel.Credentials) (string, string, error) {
	switch creds.Source {
	case "", "controller_environment":
		if creds.AccessKeyIDEnv == "" || creds.SecretAccessKeyEnv == "" {
			return "", "", errors.New("artifact_delivery.publisher_credentials requires access_key_id_env and secret_access_key_env")
		}
		access := strings.TrimSpace(os.Getenv(creds.AccessKeyIDEnv))
		secret := strings.TrimSpace(os.Getenv(creds.SecretAccessKeyEnv))
		if access == "" || secret == "" {
			return "", "", fmt.Errorf("controller environment missing %s and/or %s", creds.AccessKeyIDEnv, creds.SecretAccessKeyEnv)
		}
		return access, secret, nil
	case "controller_environment_file":
		if creds.EnvironmentFile == "" {
			return "", "", errors.New("artifact_delivery.publisher_credentials.environment_file is required")
		}
		body, err := os.ReadFile(creds.EnvironmentFile)
		if err != nil {
			return "", "", fmt.Errorf("read publisher environment file: %w", err)
		}
		return s3artifact.ParseEnvFile(body, creds.AccessKeyIDEnv, creds.SecretAccessKeyEnv)
	default:
		return "", "", fmt.Errorf("unsupported artifact_delivery.publisher_credentials.source %q", creds.Source)
	}
}
