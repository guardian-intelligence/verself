package githubintegration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

const githubCacheManifestPath = ".verself/cache.yml"

type githubAccessTokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type githubJITConfigResponse struct {
	Runner struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		OS     string `json:"os"`
		Status string `json:"status"`
		Busy   bool   `json:"busy"`
	} `json:"runner"`
	EncodedJITConfig string `json:"encoded_jit_config"`
}

type githubRunnerListResponse struct {
	TotalCount int `json:"total_count"`
	Runners    []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"runners"`
}

type githubWorkflowRunResponse struct {
	ID         int64  `json:"id"`
	RunAttempt int64  `json:"run_attempt"`
	Event      string `json:"event"`
	HeadSHA    string `json:"head_sha"`
	HeadBranch string `json:"head_branch"`
	Path       string `json:"path"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	HeadRepository struct {
		FullName string `json:"full_name"`
	} `json:"head_repository"`
	PullRequests []struct {
		Number int64 `json:"number"`
		Head   struct {
			SHA  string `json:"sha"`
			Ref  string `json:"ref"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
		Base struct {
			SHA  string `json:"sha"`
			Ref  string `json:"ref"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"base"`
	} `json:"pull_requests"`
}

type githubWorkflowRunJobsResponse struct {
	TotalCount int                 `json:"total_count"`
	Jobs       []githubWorkflowJob `json:"jobs"`
}

type githubWorkflowJob struct {
	ID           int64     `json:"id"`
	RunID        int64     `json:"run_id"`
	RunAttempt   int64     `json:"run_attempt"`
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	Labels       []string  `json:"labels"`
	RunnerID     int64     `json:"runner_id"`
	RunnerName   string    `json:"runner_name"`
	HeadSHA      string    `json:"head_sha"`
	HeadBranch   string    `json:"head_branch"`
	WorkflowName string    `json:"workflow_name"`
	StartedAt    time.Time `json:"started_at"`
	CompletedAt  time.Time `json:"completed_at"`
}

func (s *Service) installationToken(ctx context.Context, installationID int64) (string, error) {
	s.tokenMu.Lock()
	cached, ok := s.tokens[installationID]
	s.tokenMu.Unlock()
	if ok && time.Now().UTC().Before(cached.ExpiresAt.Add(-time.Minute)) {
		return cached.Token, nil
	}
	appJWT, err := createAppJWT(s.cfg.AppID, s.privateKey)
	if err != nil {
		return "", err
	}
	var resp githubAccessTokenResponse
	if err := s.githubRequest(ctx, installationID, http.MethodPost, fmt.Sprintf("/app/installations/%d/access_tokens", installationID), appJWT, nil, &resp, http.StatusCreated); err != nil {
		return "", err
	}
	if strings.TrimSpace(resp.Token) == "" {
		return "", fmt.Errorf("github installation token response missing token")
	}
	s.tokenMu.Lock()
	s.tokens[installationID] = githubInstallationToken(resp)
	s.tokenMu.Unlock()
	return resp.Token, nil
}

func (s *Service) createJITConfig(ctx context.Context, installationID int64, org string, runnerName string, runnerClass string) (githubJITConfigResponse, error) {
	token, err := s.installationToken(ctx, installationID)
	if err != nil {
		return githubJITConfigResponse{}, err
	}
	body := map[string]any{
		"name":            runnerName,
		"runner_group_id": s.cfg.RunnerGroupID,
		"labels":          []string{"self-hosted", "linux", "x64", runnerClass},
		"work_folder":     defaultRunnerWorkFolder,
	}
	var resp githubJITConfigResponse
	if err := s.githubRequest(ctx, installationID, http.MethodPost, "/orgs/"+url.PathEscape(org)+"/actions/runners/generate-jitconfig", token, body, &resp, http.StatusCreated); err != nil {
		return githubJITConfigResponse{}, err
	}
	if strings.TrimSpace(resp.EncodedJITConfig) == "" {
		return githubJITConfigResponse{}, fmt.Errorf("github jit config response missing encoded_jit_config")
	}
	return resp, nil
}

func (s *Service) fetchWorkflowRun(ctx context.Context, installationID int64, repositoryFullName string, runID int64) (githubWorkflowRunResponse, error) {
	owner, repo, ok := strings.Cut(strings.TrimSpace(repositoryFullName), "/")
	if !ok || owner == "" || repo == "" {
		return githubWorkflowRunResponse{}, fmt.Errorf("github repository must be owner/name")
	}
	token, err := s.installationToken(ctx, installationID)
	if err != nil {
		return githubWorkflowRunResponse{}, err
	}
	var out githubWorkflowRunResponse
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d", url.PathEscape(owner), url.PathEscape(repo), runID)
	if err := s.githubRequest(ctx, installationID, http.MethodGet, path, token, nil, &out, http.StatusOK); err != nil {
		return githubWorkflowRunResponse{}, err
	}
	return out, nil
}

func (s *Service) cancelWorkflowRun(ctx context.Context, installationID int64, repositoryFullName string, runID int64) error {
	owner, repo, ok := strings.Cut(strings.TrimSpace(repositoryFullName), "/")
	if !ok || owner == "" || repo == "" {
		return fmt.Errorf("github repository must be owner/name")
	}
	if runID <= 0 {
		return fmt.Errorf("github run id is required")
	}
	token, err := s.installationToken(ctx, installationID)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/cancel", url.PathEscape(owner), url.PathEscape(repo), runID)
	return s.githubRequest(ctx, installationID, http.MethodPost, path, token, nil, nil, http.StatusAccepted, http.StatusConflict)
}

func (s *Service) fetchWorkflowRunJobs(ctx context.Context, installationID, repositoryID int64, repositoryFullName string, runID, runAttempt int64) ([]githubWorkflowJob, error) {
	owner, repo, ok := strings.Cut(strings.TrimSpace(repositoryFullName), "/")
	if !ok || owner == "" || repo == "" {
		return nil, fmt.Errorf("github repository must be owner/name")
	}
	if runID <= 0 || runAttempt <= 0 {
		return nil, fmt.Errorf("github run id and attempt are required")
	}
	token, err := s.installationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}
	const perPage = 100
	var out []githubWorkflowJob
	for page := 1; ; page++ {
		var resp githubWorkflowRunJobsResponse
		path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/attempts/%d/jobs?per_page=%d&page=%d", url.PathEscape(owner), url.PathEscape(repo), runID, runAttempt, perPage, page)
		if err := s.githubRequest(ctx, installationID, http.MethodGet, path, token, nil, &resp, http.StatusOK); err != nil {
			return nil, err
		}
		for i := range resp.Jobs {
			if resp.Jobs[i].RunID == 0 {
				resp.Jobs[i].RunID = runID
			}
			if resp.Jobs[i].RunAttempt == 0 {
				resp.Jobs[i].RunAttempt = runAttempt
			}
		}
		out = append(out, resp.Jobs...)
		if len(resp.Jobs) < perPage || page*perPage >= resp.TotalCount {
			break
		}
	}
	_ = repositoryID
	return out, nil
}

func (s *Service) fetchRepositoryFile(ctx context.Context, installationID int64, repositoryFullName, filePath, ref string) ([]byte, bool, error) {
	owner, repo, ok := strings.Cut(strings.TrimSpace(repositoryFullName), "/")
	if !ok || owner == "" || repo == "" {
		return nil, false, fmt.Errorf("%w: repository full_name must be owner/name", ErrWebhookRejected)
	}
	token, err := s.installationToken(ctx, installationID)
	if err != nil {
		return nil, false, err
	}
	var resp struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	path := fmt.Sprintf("/repos/%s/%s/contents/%s", url.PathEscape(owner), url.PathEscape(repo), escapeGitHubContentPath(filePath))
	if strings.TrimSpace(ref) != "" {
		path += "?ref=" + url.QueryEscape(ref)
	}
	if err := s.githubRequest(ctx, installationID, http.MethodGet, path, token, nil, &resp, http.StatusOK); err != nil {
		if strings.Contains(err.Error(), "status 404") {
			return nil, false, nil
		}
		return nil, false, err
	}
	if resp.Type != "file" || resp.Encoding != "base64" {
		return nil, true, fmt.Errorf("github content %s is not a base64 file", filePath)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(resp.Content, "\n", ""))
	if err != nil {
		return nil, true, fmt.Errorf("decode github content %s: %w", filePath, err)
	}
	return decoded, true, nil
}

func escapeGitHubContentPath(filePath string) string {
	parts := strings.Split(strings.Trim(filePath, "/"), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func (s *Service) deleteRunner(ctx context.Context, installationID int64, org string, runnerID int64) error {
	if runnerID <= 0 {
		return nil
	}
	token, err := s.installationToken(ctx, installationID)
	if err != nil {
		return err
	}
	return s.githubRequest(ctx, installationID, http.MethodDelete, fmt.Sprintf("/orgs/%s/actions/runners/%d", url.PathEscape(org), runnerID), token, nil, nil, http.StatusNoContent, http.StatusNotFound)
}

func (s *Service) deleteRunnerByName(ctx context.Context, installationID int64, org string, runnerName string) error {
	runnerName = strings.TrimSpace(runnerName)
	if runnerName == "" {
		return nil
	}
	token, err := s.installationToken(ctx, installationID)
	if err != nil {
		return err
	}
	var list githubRunnerListResponse
	if err := s.githubRequest(ctx, installationID, http.MethodGet, "/orgs/"+url.PathEscape(org)+"/actions/runners?per_page=100", token, nil, &list, http.StatusOK); err != nil {
		return err
	}
	for _, runner := range list.Runners {
		if runner.Name == runnerName {
			return s.deleteRunner(ctx, installationID, org, runner.ID)
		}
	}
	return nil
}

func (s *Service) githubRequest(ctx context.Context, installationID int64, method, path, bearer string, body any, out any, expected ...int) error {
	ctx, span := tracer.Start(ctx, "github.api.request")
	defer span.End()
	started := time.Now().UTC()
	span.SetAttributes(
		attribute.String("github.api.method", method),
		attribute.String("github.api.path", path),
	)
	apiBase := strings.TrimRight(s.cfg.APIBaseURL, "/")
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", "verself-github-integration-service")
	req.Header.Set("Authorization", "Bearer "+bearer)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.writeGitHubAPIEvent(ctx, installationID, method, path, 0, "", "", started, err)
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))
	for _, status := range expected {
		if resp.StatusCode == status {
			s.writeGitHubAPIEvent(ctx, installationID, method, path, resp.StatusCode, resp.Header.Get("x-ratelimit-remaining"), resp.Header.Get("x-ratelimit-reset"), started, nil)
			if out != nil && resp.Body != nil {
				return json.NewDecoder(resp.Body).Decode(out)
			}
			return nil
		}
	}
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	err = fmt.Errorf("github api %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(detail)))
	s.writeGitHubAPIEvent(ctx, installationID, method, path, resp.StatusCode, resp.Header.Get("x-ratelimit-remaining"), resp.Header.Get("x-ratelimit-reset"), started, err)
	return err
}

func (s *Service) writeGitHubAPIEvent(ctx context.Context, installationID int64, method, path string, status int, remaining, reset string, started time.Time, err error) {
	result := "succeeded"
	reason := ""
	if err != nil {
		result = "failed"
		reason = err.Error()
	}
	s.writeEvent(ctx, githubEvent{
		ObservedAt:             time.Now().UTC(),
		EventName:              "github.api.request",
		Result:                 result,
		Reason:                 truncate(reason, 1024),
		Action:                 method,
		ProviderInstallationID: uint64FromInt64(installationID),
		StartedAt:              started,
		CompletedAt:            time.Now().UTC(),
		AttributesJSON: mustJSON(map[string]string{
			"path":                path,
			"status_code":         fmt.Sprintf("%d", status),
			"ratelimit_remaining": remaining,
			"ratelimit_reset":     reset,
		}),
	})
}

func workflowObservationFromRun(installationID, repositoryID int64, repositoryFullName string, run githubWorkflowRunResponse) workflowObservation {
	out := workflowObservation{
		ProviderInstallationID: installationID,
		ProviderRepositoryID:   repositoryID,
		ProviderRunID:          run.ID,
		ProviderRunAttempt:     run.RunAttempt,
		RepositoryFullName:     firstNonEmpty(run.Repository.FullName, repositoryFullName),
		EventName:              strings.TrimSpace(run.Event),
		HeadSHA:                strings.TrimSpace(run.HeadSHA),
		HeadBranch:             strings.TrimSpace(run.HeadBranch),
		HeadRepositoryFullName: strings.TrimSpace(run.HeadRepository.FullName),
		WorkflowPath:           strings.TrimSpace(run.Path),
	}
	if out.HeadRepositoryFullName == "" {
		out.HeadRepositoryFullName = out.RepositoryFullName
	}
	if len(run.PullRequests) > 0 {
		pr := run.PullRequests[0]
		out.PullRequestNumber = pr.Number
		out.BaseSHA = strings.TrimSpace(pr.Base.SHA)
		out.BaseBranch = strings.TrimSpace(pr.Base.Ref)
		if pr.Head.SHA != "" {
			out.HeadSHA = strings.TrimSpace(pr.Head.SHA)
		}
		if pr.Head.Ref != "" {
			out.HeadBranch = strings.TrimSpace(pr.Head.Ref)
		}
		if pr.Head.Repo.FullName != "" {
			out.HeadRepositoryFullName = strings.TrimSpace(pr.Head.Repo.FullName)
		}
	}
	return out
}
