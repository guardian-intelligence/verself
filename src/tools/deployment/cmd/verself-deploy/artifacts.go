package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/verself/deployment-tools/internal/deploymodel"
	"github.com/verself/deployment-tools/internal/garage"
	"github.com/verself/deployment-tools/internal/runtime"
	"github.com/verself/deployment-tools/internal/sshtun"
)

const preArtifactRemoteRoot = "/opt/verself/nomad-pre-artifacts"

func bindNomadArtifacts(repoRoot string, policy artifactDeliveryPolicy, components []nomadComponentDescriptor) (map[string]artifactBinding, []deploymodel.Artifact, error) {
	bindings := map[string]artifactBinding{}
	for _, component := range components {
		for _, declared := range component.Artifacts {
			pre := component.DeployPhase == deployPhasePreArtifact
			if err := bindNomadArtifact(repoRoot, policy, declared, pre, bindings); err != nil {
				return nil, nil, err
			}
		}
		for _, declared := range component.PreArtifacts {
			if err := bindNomadArtifact(repoRoot, policy, declared, true, bindings); err != nil {
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

func bindNomadArtifact(repoRoot string, policy artifactDeliveryPolicy, declared nomadDescriptorArtifact, pre bool, bindings map[string]artifactBinding) error {
	if prior, exists := bindings[declared.Output]; exists {
		if prior.Label != declared.Label || prior.Path != declared.Path || prior.Pre != pre {
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
	key := artifactKey(policy, digest, declared.Output, pre)
	getterSource := artifactGetterSource(policy, key, pre)
	getterOptions := policy.GetterOptions
	if pre {
		getterOptions = nil
	}
	artifact := deploymodel.Artifact{
		Output:        declared.Output,
		LocalPath:     artifactPath,
		SHA256:        digest,
		Bucket:        policy.Bucket,
		Key:           key,
		GetterSource:  getterSource,
		GetterOptions: getterOptions,
	}
	bindings[declared.Output] = artifactBinding{
		Artifact: artifact,
		Checksum: policy.ChecksumAlgorithm + ":" + digest,
		Label:    declared.Label,
		Path:     declared.Path,
		Pre:      pre,
	}
	return nil
}

func artifactKey(policy artifactDeliveryPolicy, digest, output string, pre bool) string {
	if pre {
		return path.Join(preArtifactRemoteRoot, digest, output)
	}
	return strings.Trim(policy.KeyPrefix, "/") + "/" + digest + "/" + output + ".tar"
}

func artifactGetterSource(policy artifactDeliveryPolicy, key string, pre bool) string {
	if pre {
		return key
	}
	return strings.TrimRight(policy.GetterSourcePrefix, "/") + "/" + key
}

func publishArtifacts(ctx context.Context, rt *runtime.Runtime, delivery deploymodel.ArtifactDelivery, artifacts []deploymodel.Artifact) error {
	if len(artifacts) == 0 {
		return nil
	}
	pub, err := newGaragePublisher(ctx, rt.SSH, delivery)
	if err != nil {
		return err
	}
	return pub.PublishAll(ctx, artifacts, rt.RepoRoot)
}

func publishPreArtifacts(ctx context.Context, rt *runtime.Runtime, artifacts []deploymodel.Artifact) error {
	for _, artifact := range artifacts {
		if !strings.HasPrefix(artifact.Key, preArtifactRemoteRoot+"/") {
			return fmt.Errorf("%s: pre-artifact key must live under %s", artifact.Output, preArtifactRemoteRoot)
		}
		local, err := os.Open(artifact.ResolveLocalPath(rt.RepoRoot))
		if err != nil {
			return fmt.Errorf("%s: open artifact: %w", artifact.Output, err)
		}
		if err := uploadPreArtifact(ctx, rt.SSH, artifact.Key, local); err != nil {
			_ = local.Close()
			return fmt.Errorf("%s: %w", artifact.Output, err)
		}
		if err := local.Close(); err != nil {
			return fmt.Errorf("%s: close artifact: %w", artifact.Output, err)
		}
	}
	return nil
}

func uploadPreArtifact(ctx context.Context, sshClient *sshtun.Client, remotePath string, body io.Reader) error {
	remoteDir := remotePath
	remoteTar := remotePath + ".tar"
	quotedDir, err := shellQuote(remoteDir)
	if err != nil {
		return err
	}
	quotedTar, err := shellQuote(remoteTar)
	if err != nil {
		return err
	}
	if _, err := sshClient.Exec(ctx, "sudo /usr/bin/install -d -m 0755 "+quotedDir); err != nil {
		return fmt.Errorf("prepare remote artifact directory: %w", err)
	}
	if err := sshClient.Run(ctx, "sudo /usr/bin/tee "+quotedTar+" >/dev/null", body); err != nil {
		return fmt.Errorf("write remote artifact: %w", err)
	}
	if _, err := sshClient.Exec(ctx, "sudo /bin/rm -rf "+quotedDir+" && sudo /usr/bin/install -d -m 0755 "+quotedDir+" && sudo /bin/tar -xf "+quotedTar+" -C "+quotedDir+" && sudo /bin/chmod -R a+rX "+quotedDir+" && sudo /bin/rm -f "+quotedTar); err != nil {
		return fmt.Errorf("extract remote artifact: %w", err)
	}
	return nil
}

func shellQuote(value string) (string, error) {
	if value == "" {
		return "", errors.New("shell quote: value is empty")
	}
	if strings.Contains(value, "\x00") {
		return "", fmt.Errorf("shell quote: value contains NUL: %q", value)
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'", nil
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
