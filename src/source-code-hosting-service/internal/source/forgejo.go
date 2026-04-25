package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var forgejoTracer = otel.Tracer("source-code-hosting-service/forgejo")

type ForgejoClient struct {
	BaseURL string
	Token   string
	Owner   string
	Client  *http.Client
}

type forgejoRepo struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}

type forgejoGitRef struct {
	Ref    string `json:"ref"`
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

type forgejoContents struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	SHA         string `json:"sha"`
	Size        int64  `json:"size"`
	Encoding    string `json:"encoding"`
	Content     string `json:"content"`
	DownloadURL string `json:"download_url"`
}

func (c ForgejoClient) Ready(ctx context.Context) error {
	if c.BaseURL == "" || c.Token == "" || c.Owner == "" {
		return fmt.Errorf("%w: forgejo client is incomplete", ErrForgejo)
	}
	var version struct {
		Version string `json:"version"`
	}
	return c.doJSON(ctx, http.MethodGet, "/api/v1/version", nil, &version)
}

func (c ForgejoClient) CreateRepository(ctx context.Context, repoName, description, defaultBranch string) (forgejoRepo, error) {
	ctx, span := forgejoTracer.Start(ctx, "source.forgejo.repo.create")
	defer span.End()
	span.SetAttributes(attribute.String("source.forgejo_repo", repoName))

	body := map[string]any{
		"name":           repoName,
		"description":    description,
		"private":        true,
		"auto_init":      false,
		"default_branch": defaultBranch,
	}
	var repo forgejoRepo
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/user/repos", body, &repo)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return forgejoRepo{}, err
	}
	span.SetAttributes(attribute.Int64("source.forgejo_repo_id", repo.ID))
	return repo, nil
}

func (c ForgejoClient) GetRepository(ctx context.Context, owner, repoName string) (forgejoRepo, error) {
	ctx, span := forgejoTracer.Start(ctx, "source.forgejo.repo.get")
	defer span.End()
	span.SetAttributes(attribute.String("source.forgejo_repo", repoName))

	var repo forgejoRepo
	path := fmt.Sprintf("/api/v1/repos/%s/%s", url.PathEscape(owner), url.PathEscape(repoName))
	err := c.doJSON(ctx, http.MethodGet, path, nil, &repo)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return forgejoRepo{}, err
	}
	span.SetAttributes(attribute.Int64("source.forgejo_repo_id", repo.ID))
	return repo, nil
}

func (c ForgejoClient) ListBranches(ctx context.Context, repo Repository) ([]Ref, error) {
	ctx, span := forgejoTracer.Start(ctx, "source.forgejo.refs.list")
	defer span.End()
	span.SetAttributes(attribute.String("source.repo_id", repo.RepoID.String()))

	var gitRefs []forgejoGitRef
	path := fmt.Sprintf("/api/v1/repos/%s/%s/git/refs/heads", url.PathEscape(repo.Backend.BackendOwner), url.PathEscape(repo.Backend.BackendRepo))
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &gitRefs); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	refs := make([]Ref, 0, len(gitRefs))
	for _, gitRef := range gitRefs {
		name := strings.TrimPrefix(strings.TrimSpace(gitRef.Ref), "refs/heads/")
		commit := strings.TrimSpace(gitRef.Object.SHA)
		if name == "" || commit == "" {
			continue
		}
		refs = append(refs, Ref{Name: name, Commit: commit})
	}
	return refs, nil
}

func (c ForgejoClient) Contents(ctx context.Context, repo Repository, ref, path string) ([]TreeEntry, *Blob, error) {
	ctx, span := forgejoTracer.Start(ctx, "source.forgejo.contents.get")
	defer span.End()
	span.SetAttributes(
		attribute.String("source.repo_id", repo.RepoID.String()),
		attribute.String("source.ref", ref),
		attribute.String("source.path", path),
	)

	endpoint := fmt.Sprintf("/api/v1/repos/%s/%s/contents/%s", url.PathEscape(repo.Backend.BackendOwner), url.PathEscape(repo.Backend.BackendRepo), strings.TrimLeft(path, "/"))
	values := url.Values{}
	if strings.TrimSpace(ref) != "" {
		values.Set("ref", ref)
	}
	if encoded := values.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	raw, err := c.doBytes(ctx, http.MethodGet, endpoint, nil, "application/json")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, nil, err
	}
	var entries []forgejoContents
	if err := json.Unmarshal(raw, &entries); err == nil {
		out := make([]TreeEntry, 0, len(entries))
		for _, entry := range entries {
			out = append(out, TreeEntry{Path: entry.Path, Type: entry.Type, Size: entry.Size, SHA: entry.SHA})
		}
		return out, nil, nil
	}
	var file forgejoContents
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, nil, fmt.Errorf("%w: decode forgejo contents: %v", ErrForgejo, err)
	}
	blob := Blob{
		Path:        file.Path,
		Name:        file.Name,
		Encoding:    file.Encoding,
		Content:     file.Content,
		Size:        file.Size,
		SHA:         file.SHA,
		DownloadURL: file.DownloadURL,
	}
	return nil, &blob, nil
}

func (c ForgejoClient) Archive(ctx context.Context, repo Repository, ref string) ([]byte, string, error) {
	ctx, span := forgejoTracer.Start(ctx, "source.forgejo.archive.get")
	defer span.End()
	span.SetAttributes(attribute.String("source.repo_id", repo.RepoID.String()), attribute.String("source.ref", ref))

	archive := url.PathEscape(ref + ".tar.gz")
	endpoint := fmt.Sprintf("/api/v1/repos/%s/%s/archive/%s", url.PathEscape(repo.Backend.BackendOwner), url.PathEscape(repo.Backend.BackendRepo), archive)
	data, err := c.doBytes(ctx, http.MethodGet, endpoint, nil, "application/gzip")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, "", err
	}
	return data, "application/gzip", nil
}

func (c ForgejoClient) DispatchWorkflow(ctx context.Context, repo Repository, workflowRunID uuid.UUID, workflowPath, ref string, inputs map[string]string) (BackendWorkflowDispatch, error) {
	ctx, span := forgejoTracer.Start(ctx, "source.forgejo.workflow.dispatch")
	defer span.End()
	span.SetAttributes(
		attribute.String("source.repo_id", repo.RepoID.String()),
		attribute.String("source.workflow_run_id", workflowRunID.String()),
		attribute.String("source.workflow_path", workflowPath),
		attribute.String("source.ref", ref),
	)
	body := map[string]any{
		"ref":    ref,
		"inputs": inputs,
	}
	workflowName := strings.TrimPrefix(strings.TrimPrefix(workflowPath, ".forgejo/workflows/"), ".github/workflows/")
	endpoint := fmt.Sprintf("/api/v1/repos/%s/%s/actions/workflows/%s/dispatches", url.PathEscape(repo.Backend.BackendOwner), url.PathEscape(repo.Backend.BackendRepo), url.PathEscape(workflowName))
	if err := c.doStatus(ctx, http.MethodPost, endpoint, body, http.StatusNoContent, http.StatusCreated, http.StatusOK); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return BackendWorkflowDispatch{}, err
	}
	return BackendWorkflowDispatch{BackendDispatchID: repo.Backend.Backend + ":" + workflowName + ":" + ref}, nil
}

func (c ForgejoClient) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	data, err := c.doBytes(ctx, method, path, reader, "application/json")
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: decode forgejo response: %v", ErrForgejo, err)
	}
	return nil
}

func (c ForgejoClient) doStatus(ctx context.Context, method, path string, body any, expected ...int) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	base := strings.TrimRight(c.BaseURL, "/")
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: forgejo %s %s: %v", ErrForgejo, method, path, err)
	}
	defer resp.Body.Close()
	for _, status := range expected {
		if resp.StatusCode == status {
			return nil
		}
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return fmt.Errorf("%w: forgejo %s %s status %d: %s", ErrForgejo, method, path, resp.StatusCode, string(data))
}

func (c ForgejoClient) doBytes(ctx context.Context, method, path string, body io.Reader, accept string) ([]byte, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+c.Token)
	req.Header.Set("Accept", accept)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: forgejo %s %s: %v", ErrForgejo, method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: forgejo %s %s status %d: %s", ErrForgejo, method, path, resp.StatusCode, string(data))
	}
	return data, nil
}
