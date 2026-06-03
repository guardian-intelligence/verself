package deployengine

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/hashicorp/nomad/api"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	r2controlplane "github.com/verself/integrations/cloudflare/r2-control-plane/client"

	"github.com/verself/deployment-service/internal/deploymodel"
)

const taskArtifactRoot = "local/verself-artifacts"
const controlPlaneBundleOutput = "substrate-control-plane-bundle"

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
	digest, err := fileSHA256(artifactPath)
	if err != nil {
		return fmt.Errorf("hash artifact %s: %w", declared.Path, err)
	}
	key := artifactKey(policy, digest, declared.Output)
	artifact := deploymodel.Artifact{
		Output:    declared.Output,
		LocalPath: artifactPath,
		SHA256:    digest,
		Bucket:    policy.Bucket,
		Key:       key,
	}
	bindings[declared.Output] = artifactBinding{
		Artifact: artifact,
		Checksum: policy.ChecksumAlgorithm + ":" + digest,
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

func artifactKey(policy artifactDeliveryPolicy, digest, output string) string {
	return path.Join(policy.SitePrefix, strings.Trim(policy.KeyPrefix, "/"), digest, output+".tar")
}

func bindControlPlaneBundleArtifact(policy artifactDeliveryPolicy, bundle any) (objectArtifact, error) {
	raw, err := json.Marshal(bundle)
	if err != nil {
		return objectArtifact{}, fmt.Errorf("encode substrate control-plane bundle: %w", err)
	}
	body, err := gzipPayload(raw)
	if err != nil {
		return objectArtifact{}, fmt.Errorf("compress substrate control-plane bundle: %w", err)
	}
	digest := deploymodel.SHA256(body)
	key := artifactKey(policy, digest, controlPlaneBundleOutput)
	return objectArtifact{
		Artifact: deploymodel.Artifact{
			Output: controlPlaneBundleOutput,
			SHA256: digest,
			Bucket: policy.Bucket,
			Key:    key,
		},
		Body:  body,
		Label: "substrate-control-plane bundle",
	}, nil
}

func gzipPayload(body []byte) ([]byte, error) {
	var out bytes.Buffer
	zw := gzip.NewWriter(&out)
	if _, err := zw.Write(body); err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func publishArtifacts(ctx context.Context, exec execution, inputs *deployInputs) error {
	candidates, err := uploadCandidates(inputs, exec.RepoRoot)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return nil
	}
	if exec.ArtifactPublisher != nil {
		return publishArtifactsWithCustomPublisher(ctx, exec, inputs, candidates)
	}
	ctx, span := exec.Tracer.Start(ctx, "verself_deploy.artifacts.publish",
		trace.WithAttributes(
			attribute.String("verself.site", exec.Site),
			attribute.String("verself.deploy_run_key", inputs.DeployRunKey),
			attribute.String("verself.deploy_sha", inputs.SHA),
			attribute.String("verself.r2_control_plane.addr", inputs.SiteCfg.ArtifactDelivery.ControlPlaneAddr),
			attribute.String("verself.artifact_bucket", inputs.SiteCfg.ArtifactDelivery.Bucket),
			attribute.Int("verself.artifact_count", len(candidates)),
		),
	)
	defer span.End()
	clientCfg := r2controlplane.Config{
		Address:    inputs.SiteCfg.ArtifactDelivery.ControlPlaneAddr,
		HTTPClient: exec.R2ControlPlaneHTTPClient,
	}
	if token := strings.TrimSpace(exec.R2ControlPlaneBearerToken); token != "" {
		clientCfg.BootstrapBearerToken = token
	} else if exec.R2ControlPlaneHTTPClient == nil {
		err := fmt.Errorf("R2 control-plane mTLS HTTP client is required")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	client, err := r2controlplane.New(clientCfg)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	req := r2controlplane.CreateUploadSessionRequest{
		Site:         exec.Site,
		DeployRunKey: inputs.DeployRunKey,
		SHA:          inputs.SHA,
		Artifacts:    make([]r2controlplane.ArtifactUpload, 0, len(candidates)),
	}
	for _, candidate := range candidates {
		req.Artifacts = append(req.Artifacts, r2controlplane.ArtifactUpload{
			Output:    candidate.Artifact.Output,
			SHA256:    candidate.Artifact.SHA256,
			SizeBytes: candidate.SizeBytes,
		})
	}
	session, err := createUploadSession(ctx, exec, client, req, inputs.SiteCfg.ArtifactDelivery)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.String("verself.r2_upload_session_id", session.SessionID))
	objects := mapUploadObjects(session.Objects)
	for _, candidate := range candidates {
		object, ok := objects[candidate.Artifact.Output]
		if !ok {
			err := fmt.Errorf("R2 control-plane upload session omitted artifact %q", candidate.Artifact.Output)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		if object.Bucket != candidate.Artifact.Bucket || object.Key != candidate.Artifact.Key {
			err := fmt.Errorf("R2 control-plane returned mismatched object binding for %q", candidate.Artifact.Output)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		switch object.Action {
		case r2controlplane.UploadActionPresent:
			continue
		case r2controlplane.UploadActionPut:
			if err := uploadArtifact(ctx, exec, candidate, object); err != nil {
				err = fmt.Errorf("%s: %w", candidate.Artifact.Output, err)
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return err
			}
		default:
			err := fmt.Errorf("R2 control-plane returned unknown upload action %q for %q", object.Action, candidate.Artifact.Output)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	completed, err := completeUploadSession(ctx, exec, client, session.SessionID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if len(completed.Objects) != len(candidates) {
		err := fmt.Errorf("R2 control-plane completed %d artifacts, expected %d", len(completed.Objects), len(candidates))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := applyCompletedArtifactSources(inputs, completed.Objects); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

func publishArtifactsWithCustomPublisher(ctx context.Context, exec execution, inputs *deployInputs, candidates []uploadCandidate) error {
	ctx, span := exec.Tracer.Start(ctx, "verself_deploy.artifacts.publish_custom",
		trace.WithAttributes(
			attribute.String("verself.site", exec.Site),
			attribute.String("verself.deploy_run_key", inputs.DeployRunKey),
			attribute.String("verself.deploy_sha", inputs.SHA),
			attribute.Int("verself.artifact_count", len(candidates)),
		),
	)
	defer span.End()
	req := ArtifactPublishRequest{
		Site:         exec.Site,
		SHA:          inputs.SHA,
		DeployRunKey: inputs.DeployRunKey,
		Artifacts:    make([]ArtifactPublishCandidate, 0, len(candidates)),
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

func applyCompletedArtifactSources(inputs *deployInputs, objects []r2controlplane.UploadObject) error {
	byOutput := mapUploadObjects(objects)
	sources := map[string]string{}
	for output, binding := range inputs.Bindings {
		object, ok := byOutput[output]
		if !ok {
			return fmt.Errorf("R2 control-plane completed response omitted artifact %q", output)
		}
		getterSource, err := completedArtifactGetterSource(output, object, binding.Artifact.Bucket, binding.Artifact.Key)
		if err != nil {
			return err
		}
		sources[output] = getterSource
	}
	if inputs.ControlPlaneObject.Artifact.Output != "" {
		output := inputs.ControlPlaneObject.Artifact.Output
		object, ok := byOutput[output]
		if !ok {
			return fmt.Errorf("R2 control-plane completed response omitted artifact %q", output)
		}
		getterSource, err := completedArtifactGetterSource(output, object, inputs.ControlPlaneObject.Artifact.Bucket, inputs.ControlPlaneObject.Artifact.Key)
		if err != nil {
			return err
		}
		sources[output] = getterSource
	}
	return applyArtifactGetterSources(inputs, sources)
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
	if inputs.ControlPlaneObject.Artifact.Output != "" {
		output := inputs.ControlPlaneObject.Artifact.Output
		getterSource := strings.TrimSpace(sources[output])
		if getterSource == "" {
			return fmt.Errorf("artifact publisher omitted getter source for %q", output)
		}
		inputs.ControlPlaneObject.Artifact.GetterSource = getterSource
	}
	return nil
}

func completedArtifactGetterSource(output string, object r2controlplane.UploadObject, expectedBucket, expectedKey string) (string, error) {
	if object.Bucket != expectedBucket || object.Key != expectedKey {
		return "", fmt.Errorf("R2 control-plane completed response returned mismatched object binding for %q", output)
	}
	getterSource := strings.TrimSpace(object.GetterSource)
	if getterSource == "" {
		return "", fmt.Errorf("R2 control-plane completed artifact %q without getter source", output)
	}
	parsed, err := url.Parse(getterSource)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", fmt.Errorf("R2 control-plane completed artifact %q with invalid download source", output)
	}
	return getterSource, nil
}

func uploadCandidates(inputs *deployInputs, repoRoot string) ([]uploadCandidate, error) {
	candidates := make([]uploadCandidate, 0, len(inputs.Artifacts)+1)
	seen := map[string]bool{}
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
		seen[artifact.Output] = true
	}
	if inputs.ControlPlaneObject.Artifact.Output != "" {
		if seen[inputs.ControlPlaneObject.Artifact.Output] {
			return nil, fmt.Errorf("artifact output %q is reserved for the substrate control-plane bundle", inputs.ControlPlaneObject.Artifact.Output)
		}
		candidates = append(candidates, uploadCandidate{
			Artifact:  inputs.ControlPlaneObject.Artifact,
			Body:      inputs.ControlPlaneObject.Body,
			SizeBytes: int64(len(inputs.ControlPlaneObject.Body)),
			Label:     inputs.ControlPlaneObject.Label,
		})
	}
	return candidates, nil
}

func taskArtifactDestination(output string) string {
	return path.Join(taskArtifactRoot, output)
}

func createUploadSession(ctx context.Context, exec execution, client *r2controlplane.Client, req r2controlplane.CreateUploadSessionRequest, delivery artifactDeliveryPolicy) (r2controlplane.CreateUploadSessionResponse, error) {
	ctx, span := exec.Tracer.Start(ctx, "verself_deploy.artifacts.upload_session.create",
		trace.WithAttributes(
			attribute.String("verself.site", exec.Site),
			attribute.String("verself.deploy_run_key", req.DeployRunKey),
			attribute.String("verself.deploy_sha", req.SHA),
			attribute.String("verself.artifact_bucket", delivery.Bucket),
			attribute.String("verself.r2_control_plane.addr", delivery.ControlPlaneAddr),
			attribute.Int("verself.artifact_count", len(req.Artifacts)),
		),
	)
	defer span.End()
	session, err := client.CreateUploadSession(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return r2controlplane.CreateUploadSessionResponse{}, err
	}
	span.SetAttributes(
		attribute.String("verself.r2_upload_session_id", session.SessionID),
		attribute.Int("verself.r2_upload_object_count", len(session.Objects)),
	)
	span.SetStatus(codes.Ok, "")
	return session, nil
}

func completeUploadSession(ctx context.Context, exec execution, client *r2controlplane.Client, sessionID string) (r2controlplane.CompleteUploadSessionResponse, error) {
	ctx, span := exec.Tracer.Start(ctx, "verself_deploy.artifacts.upload_session.complete",
		trace.WithAttributes(
			attribute.String("verself.site", exec.Site),
			attribute.String("verself.r2_upload_session_id", sessionID),
		),
	)
	defer span.End()
	completed, err := client.CompleteUploadSession(ctx, exec.Site, sessionID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return r2controlplane.CompleteUploadSessionResponse{}, err
	}
	span.SetAttributes(attribute.Int("verself.r2_upload_object_count", len(completed.Objects)))
	span.SetStatus(codes.Ok, "")
	return completed, nil
}

func uploadArtifact(ctx context.Context, exec execution, candidate uploadCandidate, object r2controlplane.UploadObject) error {
	ctx, span := exec.Tracer.Start(ctx, "verself_deploy.artifacts.upload",
		trace.WithAttributes(
			attribute.String("verself.site", exec.Site),
			attribute.String("verself.artifact_output", candidate.Artifact.Output),
			attribute.String("verself.artifact_label", candidate.Label),
			attribute.String("verself.artifact_sha256", candidate.Artifact.SHA256),
			attribute.String("verself.artifact_bucket", candidate.Artifact.Bucket),
			attribute.String("verself.artifact_key", candidate.Artifact.Key),
		),
	)
	defer span.End()
	digest, err := candidateSHA256(candidate)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.Int64("verself.artifact_size_bytes", candidate.SizeBytes))
	if digest != candidate.Artifact.SHA256 {
		err := fmt.Errorf("artifact sha256=%s does not match descriptor sha256=%s", digest, candidate.Artifact.SHA256)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	reader, err := candidateReader(candidate)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	defer func() { _ = reader.Close() }()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, object.PutURL, reader)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	for key, value := range object.Headers {
		req.Header.Set(key, value)
	}
	req.ContentLength = candidate.SizeBytes
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("PUT presigned R2 artifact: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		err := fmt.Errorf("PUT presigned R2 artifact status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	span.SetStatus(codes.Ok, "")
	return nil
}

func candidateSHA256(candidate uploadCandidate) (string, error) {
	if candidate.Body != nil {
		return deploymodel.SHA256(candidate.Body), nil
	}
	if candidate.LocalPath == "" {
		return "", fmt.Errorf("artifact %s has no local path", candidate.Artifact.Output)
	}
	digest, err := fileSHA256(candidate.LocalPath)
	if err != nil {
		return "", fmt.Errorf("hash artifact: %w", err)
	}
	return digest, nil
}

func candidateReader(candidate uploadCandidate) (io.ReadCloser, error) {
	if candidate.Body != nil {
		return io.NopCloser(bytes.NewReader(candidate.Body)), nil
	}
	if candidate.LocalPath == "" {
		return nil, fmt.Errorf("artifact %s has no local path", candidate.Artifact.Output)
	}
	file, err := os.Open(candidate.LocalPath)
	if err != nil {
		return nil, fmt.Errorf("open artifact: %w", err)
	}
	return file, nil
}

func mapUploadObjects(objects []r2controlplane.UploadObject) map[string]r2controlplane.UploadObject {
	out := map[string]r2controlplane.UploadObject{}
	for _, object := range objects {
		out[object.Output] = object
	}
	return out
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
	getterOptions["checksum"] = binding.Checksum
	source := binding.Artifact.GetterSource
	return &api.TaskArtifact{
		GetterSource:  &source,
		GetterOptions: getterOptions,
		RelativeDest:  &destination,
		Chown:         true,
	}
}
