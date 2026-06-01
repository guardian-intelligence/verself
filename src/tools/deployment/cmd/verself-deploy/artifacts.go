package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/hashicorp/nomad/api"

	r2controlplane "github.com/verself/integrations/cloudflare/r2-control-plane/client"

	"github.com/verself/deployment-tools/internal/deploymodel"
	"github.com/verself/deployment-tools/internal/runtime"
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

func publishArtifacts(ctx context.Context, rt *runtime.Runtime, inputs *deployInputs) error {
	if len(inputs.Artifacts) == 0 {
		return nil
	}
	token, err := readControlPlaneToken(inputs.SiteCfg.ArtifactDelivery.ControlPlaneTokenFile)
	if err != nil {
		return err
	}
	client, err := r2controlplane.New(r2controlplane.Config{
		Address: inputs.SiteCfg.ArtifactDelivery.ControlPlaneAddr,
		Token:   token,
	})
	if err != nil {
		return err
	}
	req := r2controlplane.CreateUploadSessionRequest{
		Site:         rt.Site,
		DeployRunKey: inputs.DeployRunKey,
		SHA:          inputs.SHA,
		Artifacts:    make([]r2controlplane.ArtifactUpload, 0, len(inputs.Artifacts)),
	}
	for _, artifact := range inputs.Artifacts {
		size, err := artifactSize(artifact, rt.RepoRoot)
		if err != nil {
			return err
		}
		req.Artifacts = append(req.Artifacts, r2controlplane.ArtifactUpload{
			Output:    artifact.Output,
			SHA256:    artifact.SHA256,
			SizeBytes: size,
		})
	}
	session, err := client.CreateUploadSession(ctx, req)
	if err != nil {
		return err
	}
	objects := mapUploadObjects(session.Objects)
	for _, artifact := range inputs.Artifacts {
		object, ok := objects[artifact.Output]
		if !ok {
			return fmt.Errorf("R2 control-plane upload session omitted artifact %q", artifact.Output)
		}
		if object.Bucket != artifact.Bucket || object.Key != artifact.Key || object.GetterSource != artifact.GetterSource {
			return fmt.Errorf("R2 control-plane returned mismatched object binding for %q", artifact.Output)
		}
		if err := uploadArtifact(ctx, artifact, object, rt.RepoRoot); err != nil {
			return fmt.Errorf("%s: %w", artifact.Output, err)
		}
	}
	completed, err := client.CompleteUploadSession(ctx, rt.Site, session.SessionID)
	if err != nil {
		return err
	}
	if len(completed.Objects) != len(inputs.Artifacts) {
		return fmt.Errorf("R2 control-plane completed %d artifacts, expected %d", len(completed.Objects), len(inputs.Artifacts))
	}
	return nil
}

func taskArtifactDestination(output string) string {
	return path.Join(taskArtifactRoot, output)
}

func artifactSize(artifact deploymodel.Artifact, repoRoot string) (int64, error) {
	info, err := os.Stat(artifact.ResolveLocalPath(repoRoot))
	if err != nil {
		return 0, fmt.Errorf("stat artifact: %w", err)
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("artifact path is not a regular file")
	}
	return info.Size(), nil
}

func uploadArtifact(ctx context.Context, artifact deploymodel.Artifact, object r2controlplane.UploadObject, repoRoot string) error {
	body, err := os.ReadFile(artifact.ResolveLocalPath(repoRoot))
	if err != nil {
		return fmt.Errorf("read artifact: %w", err)
	}
	digest := deploymodel.SHA256(body)
	if digest != artifact.SHA256 {
		return fmt.Errorf("artifact sha256=%s does not match descriptor sha256=%s", digest, artifact.SHA256)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, object.PutURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	for key, value := range object.Headers {
		req.Header.Set(key, value)
	}
	req.ContentLength = int64(len(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT presigned R2 artifact: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("PUT presigned R2 artifact status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func mapUploadObjects(objects []r2controlplane.UploadObject) map[string]r2controlplane.UploadObject {
	out := map[string]r2controlplane.UploadObject{}
	for _, object := range objects {
		out[object.Output] = object
	}
	return out
}

func readControlPlaneToken(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read R2 control-plane token file: %w", err)
	}
	token := strings.TrimSpace(string(body))
	if token == "" {
		return "", fmt.Errorf("R2 control-plane token file is empty")
	}
	return token, nil
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
