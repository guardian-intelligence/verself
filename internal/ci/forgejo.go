package ci

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
)

type ForgejoClient struct {
	baseURL  string
	token    string
	username string
	password string
	client   *http.Client
}

type Repository struct {
	Name        string
	Description string
	Private     bool
}

type PullRequest struct {
	Number int `json:"number"`
}

type WorkflowRun struct {
	ID         int64  `json:"id"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Event      string `json:"event"`
	HeadBranch string `json:"head_branch"`
	CommitSHA  string `json:"commit_sha"`
}

func NewForgejoTokenClient(baseURL, token string) *ForgejoClient {
	return &ForgejoClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func NewForgejoBasicClient(baseURL, username, password string) *ForgejoClient {
	return &ForgejoClient{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *ForgejoClient) CreateToken(ctx context.Context, name string) (string, error) {
	body := map[string]any{
		"name":   name,
		"scopes": []string{"all"},
	}
	var resp struct {
		SHA1 string `json:"sha1"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/users/"+url.PathEscape(c.username)+"/tokens", body, &resp); err != nil {
		return "", err
	}
	if resp.SHA1 == "" {
		return "", fmt.Errorf("forgejo token response did not include sha1")
	}
	return resp.SHA1, nil
}

func (c *ForgejoClient) EnsureRepository(ctx context.Context, owner string, repo Repository) error {
	path := fmt.Sprintf("/api/v1/repos/%s/%s", url.PathEscape(owner), url.PathEscape(repo.Name))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	c.authorize(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("forgejo get repo: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	createBody := map[string]any{
		"name":           repo.Name,
		"auto_init":      false,
		"default_branch": "main",
		"description":    repo.Description,
		"private":        repo.Private,
	}
	return c.doJSON(ctx, http.MethodPost, "/api/v1/user/repos", createBody, nil)
}

func (c *ForgejoClient) CreatePullRequest(ctx context.Context, owner, repo, title, head, base string) (*PullRequest, error) {
	body := map[string]any{
		"title": title,
		"head":  head,
		"base":  base,
	}
	var pr PullRequest
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/api/v1/repos/%s/%s/pulls", url.PathEscape(owner), url.PathEscape(repo)), body, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

func (c *ForgejoClient) ListWorkflowRuns(ctx context.Context, owner, repo string) ([]WorkflowRun, error) {
	var payload struct {
		WorkflowRuns []WorkflowRun `json:"workflow_runs"`
	}
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/repos/%s/%s/actions/runs", url.PathEscape(owner), url.PathEscape(repo)), nil, &payload); err != nil {
		return nil, err
	}
	return payload.WorkflowRuns, nil
}

func (c *ForgejoClient) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.authorize(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("forgejo %s %s: http %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *ForgejoClient) authorize(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "token "+c.token)
		return
	}
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}
}
