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

type forgejoBranch struct {
	Name   string `json:"name"`
	Commit struct {
		ID string `json:"id"`
	} `json:"commit"`
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
		"auto_init":      true,
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

func (c ForgejoClient) ListBranches(ctx context.Context, repo Repository) ([]Ref, error) {
	ctx, span := forgejoTracer.Start(ctx, "source.forgejo.refs.list")
	defer span.End()
	span.SetAttributes(attribute.String("source.repo_id", repo.RepoID.String()))

	var branches []forgejoBranch
	path := fmt.Sprintf("/api/v1/repos/%s/%s/branches", url.PathEscape(repo.ForgejoOwner), url.PathEscape(repo.ForgejoRepo))
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &branches); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	refs := make([]Ref, 0, len(branches))
	for _, branch := range branches {
		refs = append(refs, Ref{Name: branch.Name, Commit: branch.Commit.ID})
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

	endpoint := fmt.Sprintf("/api/v1/repos/%s/%s/contents/%s", url.PathEscape(repo.ForgejoOwner), url.PathEscape(repo.ForgejoRepo), strings.TrimLeft(path, "/"))
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
	endpoint := fmt.Sprintf("/api/v1/repos/%s/%s/archive/%s", url.PathEscape(repo.ForgejoOwner), url.PathEscape(repo.ForgejoRepo), archive)
	data, err := c.doBytes(ctx, http.MethodGet, endpoint, nil, "application/gzip")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, "", err
	}
	return data, "application/gzip", nil
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
