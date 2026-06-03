package artifactpublish

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	objectstorageclient "github.com/verself/object-storage-service/client"

	"github.com/verself/deployment-service/deployengine"
)

const defaultWriteSessionTTL = time.Hour

type Publisher struct {
	Client          *objectstorageclient.Client
	TransferHTTP    *http.Client
	WriteSessionTTL time.Duration
}

func (p Publisher) PublishDeploymentArtifacts(ctx context.Context, req deployengine.ArtifactPublishRequest) (deployengine.ArtifactPublishResult, error) {
	if p.Client == nil {
		return deployengine.ArtifactPublishResult{}, fmt.Errorf("object-storage client is required")
	}
	ttl := p.WriteSessionTTL
	if ttl <= 0 {
		ttl = defaultWriteSessionTTL
	}
	objects := make([]objectstorageclient.ObjectWriteRequest, 0, len(req.Artifacts))
	for _, artifact := range req.Artifacts {
		if err := verifyArtifactCandidate(artifact); err != nil {
			return deployengine.ArtifactPublishResult{}, err
		}
		objects = append(objects, objectstorageclient.ObjectWriteRequest{
			Name:      artifact.Output,
			SHA256:    artifact.SHA256,
			SizeBytes: artifact.SizeBytes,
		})
	}
	created, err := p.Client.CreateObjectWriteSession(ctx, objectstorageclient.CreateObjectWriteSessionRequest{
		Site:           objectstorageclient.SiteName(req.Site),
		IdempotencyKey: objectstorageclient.IdempotencyKey(req.DeployRunKey),
		Body: objectstorageclient.CreateObjectWriteSessionInputBody{
			Capability: objectstorageclient.ObjectStorageCapabilityDeploymentArtifacts,
			Namespace:  objectstorageclient.ObjectNamespace(req.ArtifactNamespace),
			Objects:    objects,
			TTLSeconds: int(ttl / time.Second),
		},
	})
	if err != nil {
		return deployengine.ArtifactPublishResult{}, err
	}
	if created.Result == nil {
		return deployengine.ArtifactPublishResult{}, responseError("create object write session", created.StatusCode, created.Problem, created.Body)
	}
	sessionObjects := mapSessionObjects(created.Result.Objects)
	for _, artifact := range req.Artifacts {
		object, ok := sessionObjects[artifact.Output]
		if !ok {
			return deployengine.ArtifactPublishResult{}, fmt.Errorf("object-storage write session omitted artifact %q", artifact.Output)
		}
		switch object.Action {
		case objectstorageclient.ObjectWriteActionPresent:
			continue
		case objectstorageclient.ObjectWriteActionPut:
			if object.Write == nil {
				return deployengine.ArtifactPublishResult{}, fmt.Errorf("object-storage write session omitted write handle for %q", artifact.Output)
			}
			if err := p.uploadArtifact(ctx, artifact, *object.Write); err != nil {
				return deployengine.ArtifactPublishResult{}, fmt.Errorf("%s: %w", artifact.Output, err)
			}
		default:
			return deployengine.ArtifactPublishResult{}, fmt.Errorf("object-storage write session returned unknown action %q for %q", object.Action, artifact.Output)
		}
	}
	completed, err := p.Client.CompleteObjectWriteSession(ctx, objectstorageclient.CompleteObjectWriteSessionRequest{
		Site:      objectstorageclient.SiteName(req.Site),
		SessionID: created.Result.SessionID,
	})
	if err != nil {
		return deployengine.ArtifactPublishResult{}, err
	}
	if completed.Result == nil {
		return deployengine.ArtifactPublishResult{}, responseError("complete object write session", completed.StatusCode, completed.Problem, completed.Body)
	}
	completedObjects := mapSessionObjects(completed.Result.Objects)
	sources := map[string]string{}
	for _, artifact := range req.Artifacts {
		object, ok := completedObjects[artifact.Output]
		if !ok {
			return deployengine.ArtifactPublishResult{}, fmt.Errorf("object-storage completed session omitted artifact %q", artifact.Output)
		}
		if object.GetterSource == nil || strings.TrimSpace(string(*object.GetterSource)) == "" {
			return deployengine.ArtifactPublishResult{}, fmt.Errorf("object-storage completed artifact %q without getter source", artifact.Output)
		}
		sources[artifact.Output] = string(*object.GetterSource)
	}
	return deployengine.ArtifactPublishResult{GetterSources: sources}, nil
}

func (p Publisher) uploadArtifact(ctx context.Context, artifact deployengine.ArtifactPublishCandidate, object objectstorageclient.S3CompatibleObject) error {
	reader, err := candidateReader(artifact)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, string(object.URL), reader)
	if err != nil {
		return err
	}
	for key, value := range object.Headers {
		req.Header.Set(key, value)
	}
	req.ContentLength = artifact.SizeBytes
	client := p.TransferHTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("PUT object-storage artifact: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("PUT object-storage artifact status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func mapSessionObjects(objects objectstorageclient.ObjectWriteSessionObjects) map[string]objectstorageclient.ObjectWriteSessionObject {
	out := map[string]objectstorageclient.ObjectWriteSessionObject{}
	for _, object := range objects {
		out[string(object.Name)] = object
	}
	return out
}

func verifyArtifactCandidate(artifact deployengine.ArtifactPublishCandidate) error {
	digest, err := candidateSHA256(artifact)
	if err != nil {
		return fmt.Errorf("%s: %w", artifact.Output, err)
	}
	if digest != strings.ToLower(strings.TrimSpace(artifact.SHA256)) {
		return fmt.Errorf("%s: sha256=%s does not match expected %s", artifact.Output, digest, artifact.SHA256)
	}
	return nil
}

func candidateSHA256(candidate deployengine.ArtifactPublishCandidate) (string, error) {
	if candidate.Body != nil {
		sum := sha256.Sum256(candidate.Body)
		return hex.EncodeToString(sum[:]), nil
	}
	if strings.TrimSpace(candidate.LocalPath) == "" {
		return "", fmt.Errorf("artifact has no local path")
	}
	file, err := os.Open(candidate.LocalPath)
	if err != nil {
		return "", fmt.Errorf("open artifact: %w", err)
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash artifact: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func candidateReader(candidate deployengine.ArtifactPublishCandidate) (io.ReadCloser, error) {
	if candidate.Body != nil {
		return io.NopCloser(bytes.NewReader(candidate.Body)), nil
	}
	if strings.TrimSpace(candidate.LocalPath) == "" {
		return nil, fmt.Errorf("artifact has no local path")
	}
	file, err := os.Open(candidate.LocalPath)
	if err != nil {
		return nil, fmt.Errorf("open artifact: %w", err)
	}
	return file, nil
}

func responseError(op string, status int, problem *objectstorageclient.ErrorModel, body []byte) error {
	if problem != nil {
		detail := ""
		if problem.Detail != nil {
			detail = strings.TrimSpace(*problem.Detail)
		}
		if detail == "" {
			if problem.Title != nil {
				detail = strings.TrimSpace(*problem.Title)
			}
		}
		if detail != "" {
			return fmt.Errorf("%s failed status %d: %s", op, status, detail)
		}
	}
	return fmt.Errorf("%s failed status %d: %s", op, status, strings.TrimSpace(string(body)))
}
