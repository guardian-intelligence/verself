package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/hashicorp/nomad/api"

	"github.com/verself/deployment-tools/internal/deploymodel"
	"github.com/verself/deployment-tools/internal/runtime"
	"github.com/verself/deployment-tools/internal/s3artifact"
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
	cfg, err := publisherCredentials(delivery.PublisherCredentials)
	if err != nil {
		return nil, err
	}
	return s3artifact.New(delivery, cfg)
}

func publisherCredentials(creds deploymodel.Credentials) (s3artifact.Config, error) {
	switch creds.Source {
	case "", "controller_environment":
		if creds.AccessKeyIDEnv == "" || creds.SecretAccessKeyEnv == "" {
			return s3artifact.Config{}, errors.New("artifact_delivery.publisher_credentials requires access_key_id_env and secret_access_key_env")
		}
		access := strings.TrimSpace(os.Getenv(creds.AccessKeyIDEnv))
		secret := strings.TrimSpace(os.Getenv(creds.SecretAccessKeyEnv))
		if access == "" || secret == "" {
			return s3artifact.Config{}, fmt.Errorf("controller environment missing %s and/or %s", creds.AccessKeyIDEnv, creds.SecretAccessKeyEnv)
		}
		session := ""
		if creds.SessionTokenEnv != "" {
			session = strings.TrimSpace(os.Getenv(creds.SessionTokenEnv))
			if session == "" {
				return s3artifact.Config{}, fmt.Errorf("controller environment missing %s", creds.SessionTokenEnv)
			}
		}
		return s3artifact.Config{AccessKeyID: access, SecretAccessKey: secret, SessionToken: session}, nil
	case "controller_environment_file":
		if creds.EnvironmentFile == "" {
			return s3artifact.Config{}, errors.New("artifact_delivery.publisher_credentials.environment_file is required")
		}
		body, err := os.ReadFile(creds.EnvironmentFile)
		if err != nil {
			return s3artifact.Config{}, fmt.Errorf("read publisher environment file: %w", err)
		}
		access, secret, session, err := s3artifact.ParseEnvFile(body, creds.AccessKeyIDEnv, creds.SecretAccessKeyEnv, creds.SessionTokenEnv)
		if err != nil {
			return s3artifact.Config{}, err
		}
		return s3artifact.Config{AccessKeyID: access, SecretAccessKey: secret, SessionToken: session}, nil
	default:
		return s3artifact.Config{}, fmt.Errorf("unsupported artifact_delivery.publisher_credentials.source %q", creds.Source)
	}
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
