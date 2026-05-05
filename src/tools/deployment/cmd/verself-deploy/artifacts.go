package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/verself/deployment-tools/internal/deploymodel"
	"github.com/verself/deployment-tools/internal/garage"
	"github.com/verself/deployment-tools/internal/runtime"
	"github.com/verself/deployment-tools/internal/sshtun"
)

func bindNomadArtifacts(repoRoot string, policy artifactDeliveryPolicy, components []nomadComponentDescriptor) (map[string]artifactBinding, []deploymodel.Artifact, error) {
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

func publishPlanArtifacts(ctx context.Context, rt *runtime.Runtime, plan *deployPlan) error {
	if len(plan.Artifacts) == 0 {
		return nil
	}
	pub, err := newGaragePublisher(ctx, rt.SSH, plan.SiteCfg.ArtifactDelivery.ArtifactDelivery)
	if err != nil {
		return err
	}
	return pub.PublishAll(ctx, plan.Artifacts, rt.RepoRoot)
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
		_ = forward.Close()
		return nil, fmt.Errorf("read publisher credentials: %w", err)
	}
	access, secret, err := garage.ParseEnvFile(
		credBytes,
		delivery.PublisherCredentials.AccessKeyIDEnv,
		delivery.PublisherCredentials.SecretAccessKeyEnv,
	)
	if err != nil {
		_ = forward.Close()
		return nil, err
	}
	caPEM, err := sudoCat(ctx, sshClient, delivery.Origin.CABundlePath)
	if err != nil {
		_ = forward.Close()
		return nil, fmt.Errorf("read artifact origin CA bundle: %w", err)
	}
	pub, err := garage.New(delivery, garage.Config{
		ConnectAddress:  forward.ListenAddr,
		CABundlePEM:     caPEM,
		AccessKeyID:     access,
		SecretAccessKey: secret,
	})
	if err != nil {
		_ = forward.Close()
		return nil, err
	}
	return pub, nil
}

func sudoCat(ctx context.Context, sshClient *sshtun.Client, path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("sudoCat: path is empty")
	}
	if strings.ContainsAny(path, "'\\") {
		return nil, fmt.Errorf("sudoCat: refusing path with special chars: %q", path)
	}
	return sshClient.Exec(ctx, "sudo /bin/cat -- "+strconv.Quote(path))
}
