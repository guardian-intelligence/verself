package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// cacheStateForRunIndex names the per-run-index cache state stored in
// benchmark_runs.cache_state. The default 3-run cadence is cold→warm→dirty;
// extra runs beyond runs-per-case=3 land as warm so re-running the same
// commit accumulates warm samples instead of polluting state names.
func cacheStateForRunIndex(runIndex int) string {
	switch runIndex {
	case 0:
		return "cold"
	case 2:
		return "dirty"
	default:
		return "warm"
	}
}

const (
	checkpointCanaryWorkflowName = "Verself Checkpoint Canary"
	checkpointCanaryWorkflowPath = ".github/workflows/checkpoint-canary.yml"
	githubAPIVersion             = "2022-11-28"
)

type checkpointCanaryConfig struct {
	providersRaw   string
	workloadsRaw   string
	runID          string
	repoPrefix     string
	runnerLabel    string
	projectsURL    string
	sourceURL      string
	sandboxURL     string
	verselfToken   string
	githubOwner    string
	githubToken    string
	githubAPIURL   string
	githubWebURL   string
	timeout        time.Duration
	pollInterval   time.Duration
	outputFormat   string
	keepWorkspaces bool
	// runsPerCase controls how many times each (workload, provider) case
	// dispatches in sequence. Default 3 produces cold (miss) → warm (restore
	// + save) → dirty (restore + mutated source + save). The warm-vs-cold
	// delta is the "did Checkpoint help" signal; the dirty case checks that
	// restore + small modification + reuse is the realistic PR shape, not
	// the artificial identity-rerun shape. Extra runs beyond 3 land as
	// additional warm samples.
	runsPerCase int
}

type checkpointCanaryRunner struct {
	cfg    checkpointCanaryConfig
	client *http.Client
}

type checkpointWorkload struct {
	Slug        string
	DisplayName string
	Upstream    string
	Generate    func(string) error
	Workflow    func(string) string
}

type checkpointCanaryReport struct {
	RunID       string                       `json:"run_id"`
	StartedAt   time.Time                    `json:"started_at"`
	CompletedAt time.Time                    `json:"completed_at"`
	Status      string                       `json:"status"`
	Providers   []string                     `json:"providers"`
	Workloads   []string                     `json:"workloads"`
	Cases       []checkpointCanaryCaseResult `json:"cases"`
}

type checkpointCanaryCaseResult struct {
	Provider      string            `json:"provider"`
	Workload      string            `json:"workload"`
	Repository    string            `json:"repository"`
	RepositoryURL string            `json:"repository_url,omitempty"`
	CommitSHA     string            `json:"commit_sha"`
	DispatchID    string            `json:"dispatch_id,omitempty"`
	SandboxRun    *sandboxRunRecord `json:"sandbox_run,omitempty"`
	GitHubRunID   string            `json:"github_run_id,omitempty"`
	GitHubRunURL  string            `json:"github_run_url,omitempty"`
	Status        string            `json:"status"`
	Error         string            `json:"error,omitempty"`
	// RunIndex is the 0-based dispatch index for the same (workload, provider)
	// pair within this canary run. RunIndex == 0 is "cold" (Checkpoint miss
	// expected); RunIndex > 0 is "warm" (Checkpoint restored from the
	// generation the previous dispatch saved).
	RunIndex      int       `json:"run_index"`
	CacheState    string    `json:"cache_state"`
	StartedAt     time.Time `json:"started_at"`
	CompletedAt   time.Time `json:"completed_at"`
	WallClockMs   int64     `json:"wall_clock_ms"`
}

type preparedWorkload struct {
	Spec      checkpointWorkload
	Directory string
	CommitSHA string
}

type projectListResponse struct {
	Projects   []projectRecord `json:"projects"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

type projectRecord struct {
	ProjectID   string `json:"project_id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	State       string `json:"state"`
}

type sourceRepositoryListResponse struct {
	Repositories []sourceRepository `json:"repositories"`
}

type sourceRepository struct {
	RepoID        string `json:"repo_id"`
	ProjectID     string `json:"project_id"`
	OrgSlug       string `json:"org_slug"`
	ProjectSlug   string `json:"project_slug"`
	GitHTTPURL    string `json:"git_http_url"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
}

type sourceGitCredential struct {
	Username string `json:"username"`
	Token    string `json:"token"`
}

type sourceWorkflowRun struct {
	WorkflowRunID     string `json:"workflow_run_id"`
	State             string `json:"state"`
	BackendDispatchID string `json:"backend_dispatch_id"`
}

type githubRepository struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
	Private  bool   `json:"private"`
}

type githubInstallation struct {
	InstallationID string `json:"installation_id"`
	AccountLogin   string `json:"account_login"`
	Active         bool   `json:"active"`
}

type githubRepositorySync struct {
	Repositories []struct {
		ProviderRepositoryID string `json:"provider_repository_id"`
		RepositoryFullName   string `json:"repository_full_name"`
		Active               bool   `json:"active"`
	} `json:"repositories"`
}

type githubWorkflowRunsResponse struct {
	WorkflowRuns []githubWorkflowRun `json:"workflow_runs"`
}

type githubWorkflowRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	HeadSHA    string `json:"head_sha"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
}

type sandboxRunsPage struct {
	Runs []sandboxRunRecord `json:"runs"`
}

type sandboxRunRecord struct {
	RunID       string               `json:"run_id"`
	ExecutionID string               `json:"execution_id"`
	Status      string               `json:"status"`
	Runner      *sandboxRunnerRecord `json:"runner,omitempty"`
	Latest      sandboxAttemptRecord `json:"latest_attempt"`
	UpdatedAt   time.Time            `json:"updated_at"`
}

type sandboxRunnerRecord struct {
	ProviderRunID      string `json:"provider_run_id,omitempty"`
	ProviderJobID      string `json:"provider_job_id,omitempty"`
	RepositoryFullName string `json:"repository_full_name,omitempty"`
	WorkflowName       string `json:"workflow_name,omitempty"`
	JobName            string `json:"job_name,omitempty"`
	HeadBranch         string `json:"head_branch,omitempty"`
	HeadSHA            string `json:"head_sha,omitempty"`
}

type sandboxAttemptRecord struct {
	State         string `json:"state"`
	FailureReason string `json:"failure_reason,omitempty"`
	ExitCode      *int   `json:"exit_code,omitempty"`
}

func cmdCheckpointCanary(args []string) error {
	cfg := checkpointCanaryConfig{
		providersRaw: envOr("CHECKPOINT_CANARY_PROVIDERS", "source,github"),
		workloadsRaw: envOr("CHECKPOINT_CANARY_WORKLOADS", "express-npm,python-click,db-seed"),
		runID:        envOr("CHECKPOINT_CANARY_RUN_ID", time.Now().UTC().Format("20060102T150405Z")),
		repoPrefix:   envOr("CHECKPOINT_CANARY_REPO_PREFIX", "checkpoint-canary"),
		runnerLabel:  envOr("CHECKPOINT_CANARY_RUNNER_LABEL", "verself-2vcpu-ubuntu-2404"),
		projectsURL:  envOr("PROJECTS_SERVICE_BASE_URL", "https://projects.api.verself.sh"),
		sourceURL:    envOr("SOURCE_SERVICE_BASE_URL", "https://source.api.verself.sh"),
		sandboxURL:   envOr("SANDBOX_SERVICE_BASE_URL", "https://sandbox.api.verself.sh"),
		verselfToken: firstNonEmpty(os.Getenv("VERSELF_TOKEN"), os.Getenv("CHECKPOINT_CANARY_VERSELF_TOKEN")),
		githubOwner:  envOr("CHECKPOINT_CANARY_GITHUB_OWNER", ""),
		githubToken:  firstNonEmpty(os.Getenv("CHECKPOINT_CANARY_GITHUB_TOKEN"), os.Getenv("GITHUB_TOKEN")),
		githubAPIURL: envOr("CHECKPOINT_CANARY_GITHUB_API_URL", "https://api.github.com"),
		githubWebURL: envOr("CHECKPOINT_CANARY_GITHUB_WEB_URL", "https://github.com"),
		timeout:      45 * time.Minute,
		pollInterval: 10 * time.Second,
		outputFormat: "json",
		runsPerCase:  3,
	}
	fs := flagSet("checkpoint-canary")
	fs.StringVar(&cfg.providersRaw, "providers", cfg.providersRaw, "Comma-separated providers: source,github")
	fs.StringVar(&cfg.workloadsRaw, "workloads", cfg.workloadsRaw, "Comma-separated workloads or all")
	fs.StringVar(&cfg.runID, "run-id", cfg.runID, "Unique canary run id")
	fs.StringVar(&cfg.repoPrefix, "repo-prefix", cfg.repoPrefix, "Repository/project slug prefix")
	fs.StringVar(&cfg.runnerLabel, "runner-label", cfg.runnerLabel, "Verself runner class label")
	fs.StringVar(&cfg.projectsURL, "projects-url", cfg.projectsURL, "Projects service base URL")
	fs.StringVar(&cfg.sourceURL, "source-url", cfg.sourceURL, "Source-code-hosting service base URL")
	fs.StringVar(&cfg.sandboxURL, "sandbox-url", cfg.sandboxURL, "Sandbox-rental service base URL")
	fs.StringVar(&cfg.verselfToken, "verself-token", cfg.verselfToken, "Verself bearer token")
	fs.StringVar(&cfg.githubOwner, "github-owner", cfg.githubOwner, "GitHub organization owner")
	fs.StringVar(&cfg.githubToken, "github-token", cfg.githubToken, "GitHub token with repo/workflow permissions")
	fs.StringVar(&cfg.githubAPIURL, "github-api-url", cfg.githubAPIURL, "GitHub API base URL")
	fs.StringVar(&cfg.githubWebURL, "github-web-url", cfg.githubWebURL, "GitHub web base URL")
	fs.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "Maximum time to wait for dispatched jobs")
	fs.DurationVar(&cfg.pollInterval, "poll-interval", cfg.pollInterval, "Polling interval")
	fs.StringVar(&cfg.outputFormat, "output", cfg.outputFormat, "Output format: json or table")
	fs.BoolVar(&cfg.keepWorkspaces, "keep-workspaces", cfg.keepWorkspaces, "Keep temporary repositories after completion")
	fs.IntVar(&cfg.runsPerCase, "runs", cfg.runsPerCase, "Number of dispatches per (workload, provider) case (cold then warm)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitError{code: 0}
		}
		return err
	}
	r := checkpointCanaryRunner{cfg: cfg, client: &http.Client{Timeout: 30 * time.Second}}
	report, err := r.run(context.Background())
	if printErr := printCheckpointCanaryReport(os.Stdout, report, cfg.outputFormat); printErr != nil && err == nil {
		err = printErr
	}
	return err
}

func (r checkpointCanaryRunner) run(parent context.Context) (checkpointCanaryReport, error) {
	providers, err := parseCheckpointCanaryList(r.cfg.providersRaw, []string{"source", "github"})
	if err != nil {
		return checkpointCanaryReport{}, err
	}
	workloads, workloadSlugs, err := selectedCheckpointWorkloads(r.cfg.workloadsRaw)
	if err != nil {
		return checkpointCanaryReport{}, err
	}
	if containsString(providers, "source") || containsString(providers, "github") {
		if strings.TrimSpace(r.cfg.verselfToken) == "" {
			return checkpointCanaryReport{}, fmt.Errorf("VERSELF_TOKEN or --verself-token is required")
		}
	}
	if containsString(providers, "github") {
		if strings.TrimSpace(r.cfg.githubOwner) == "" {
			return checkpointCanaryReport{}, fmt.Errorf("CHECKPOINT_CANARY_GITHUB_OWNER or --github-owner is required for github provider")
		}
		if strings.TrimSpace(r.cfg.githubToken) == "" {
			return checkpointCanaryReport{}, fmt.Errorf("CHECKPOINT_CANARY_GITHUB_TOKEN, GITHUB_TOKEN, or --github-token is required for github provider")
		}
	}
	ctx, cancel := context.WithTimeout(parent, r.cfg.timeout+5*time.Minute)
	defer cancel()
	tempRoot, err := os.MkdirTemp("", "verself-checkpoint-canary-*")
	if err != nil {
		return checkpointCanaryReport{}, err
	}
	if !r.cfg.keepWorkspaces {
		defer func() { _ = os.RemoveAll(tempRoot) }()
	}
	started := time.Now().UTC()
	report := checkpointCanaryReport{
		RunID:     r.cfg.runID,
		StartedAt: started,
		Status:    "succeeded",
		Providers: providers,
		Workloads: workloadSlugs,
	}
	runsPerCase := r.cfg.runsPerCase
	if runsPerCase <= 0 {
		runsPerCase = 1
	}
	type preparedRun struct {
		spec    checkpointWorkload
		prep    preparedWorkload
		err     error
	}
	prepared := make([]preparedRun, len(workloads))
	for i, workload := range workloads {
		prep, err := r.prepareWorkload(ctx, tempRoot, workload)
		prepared[i] = preparedRun{spec: workload, prep: prep, err: err}
	}
	var failures []string
	// Outer loop is runIndex so the dirty mutation that runs before runIndex
	// 2 applies once per workload and every provider sees the same mutated
	// commit. With provider as outer the second provider would observe
	// mutations from the first provider's dirty pass.
	for runIndex := 0; runIndex < runsPerCase; runIndex++ {
		for i := range prepared {
			pr := &prepared[i]
			if pr.err == nil && runIndex == 2 {
				if err := r.mutateWorkloadForDirty(ctx, &pr.prep); err != nil {
					pr.err = err
				}
			}
			for _, provider := range providers {
				result := checkpointCanaryCaseResult{
					Provider:   provider,
					Workload:   pr.spec.Slug,
					Status:     "failed",
					RunIndex:   runIndex,
					CacheState: cacheStateForRunIndex(runIndex),
				}
				if pr.err != nil {
					result.Error = pr.err.Error()
					result.StartedAt = time.Now().UTC()
					result.CompletedAt = result.StartedAt
					report.Cases = append(report.Cases, result)
					failures = append(failures, pr.spec.Slug+"/"+provider+"#"+strconv.Itoa(runIndex)+": "+pr.err.Error())
					continue
				}
				started := time.Now().UTC()
				var cased checkpointCanaryCaseResult
				switch provider {
				case "source":
					cased = r.runSourceCase(ctx, pr.prep)
				case "github":
					cased = r.runGitHubCase(ctx, pr.prep)
				default:
					cased = result
					cased.Error = "unsupported provider"
				}
				cased.RunIndex = runIndex
				cased.CacheState = cacheStateForRunIndex(runIndex)
				cased.StartedAt = started
				cased.CompletedAt = time.Now().UTC()
				cased.WallClockMs = cased.CompletedAt.Sub(cased.StartedAt).Milliseconds()
				if cased.Status != "succeeded" {
					failures = append(failures, pr.spec.Slug+"/"+provider+"#"+strconv.Itoa(runIndex)+": "+cased.Error)
				}
				report.Cases = append(report.Cases, cased)
			}
		}
	}
	report.CompletedAt = time.Now().UTC()
	if len(failures) > 0 {
		report.Status = "failed"
		return report, fmt.Errorf("checkpoint canary failed: %s", strings.Join(failures, "; "))
	}
	return report, nil
}

// mutateWorkloadForDirty rewrites a sentinel file inside the prepared
// directory and creates a fresh commit so the dirty dispatch runs against a
// new HEAD. The mutation does not change anything that feeds into the
// Checkpoint cache key (lockfiles, package manifests), so the workflow's
// hashFiles() input stays stable and the warm Checkpoint generation is
// expected to hit. Each call advances prepared.CommitSHA to the new HEAD;
// runSourceCase/runGitHubCase always force-push prepared.Directory so the
// new commit ships to every provider remote.
func (r checkpointCanaryRunner) mutateWorkloadForDirty(ctx context.Context, prepared *preparedWorkload) error {
	marker := fmt.Sprintf("run_id=%s\nworkload=%s\nmutated_at=%s\n",
		r.cfg.runID, prepared.Spec.Slug, time.Now().UTC().Format(time.RFC3339Nano))
	if err := os.WriteFile(filepath.Join(prepared.Directory, "VERSELF_CHECKPOINT_CANARY_DIRTY.txt"), []byte(marker), 0o644); err != nil {
		return fmt.Errorf("write dirty marker: %w", err)
	}
	if err := runCmd(ctx, prepared.Directory, "git", "add", "VERSELF_CHECKPOINT_CANARY_DIRTY.txt"); err != nil {
		return err
	}
	if err := runCmd(ctx, prepared.Directory, "git", "commit", "-m", "Verself checkpoint canary dirty "+r.cfg.runID); err != nil {
		return err
	}
	sha, err := commandOutput(ctx, prepared.Directory, "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	prepared.CommitSHA = strings.TrimSpace(sha)
	return nil
}

func (r checkpointCanaryRunner) prepareWorkload(ctx context.Context, tempRoot string, workload checkpointWorkload) (preparedWorkload, error) {
	dir := filepath.Join(tempRoot, workload.Slug)
	if workload.Upstream != "" {
		if err := runCmd(ctx, "", "git", "clone", "--depth=1", workload.Upstream, dir); err != nil {
			return preparedWorkload{}, err
		}
	} else {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return preparedWorkload{}, err
		}
		if workload.Generate == nil {
			return preparedWorkload{}, fmt.Errorf("workload %s has no upstream or generator", workload.Slug)
		}
		if err := workload.Generate(dir); err != nil {
			return preparedWorkload{}, err
		}
		if err := runCmd(ctx, dir, "git", "init", "-b", "main"); err != nil {
			return preparedWorkload{}, err
		}
	}
	if err := runCmd(ctx, dir, "git", "checkout", "-B", "main"); err != nil {
		return preparedWorkload{}, err
	}
	if err := os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755); err != nil {
		return preparedWorkload{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, checkpointCanaryWorkflowPath), []byte(workload.Workflow(r.cfg.runnerLabel)), 0o644); err != nil {
		return preparedWorkload{}, err
	}
	marker := fmt.Sprintf("run_id=%s\nworkload=%s\ncreated_at=%s\n", r.cfg.runID, workload.Slug, time.Now().UTC().Format(time.RFC3339Nano))
	if err := os.WriteFile(filepath.Join(dir, "VERSELF_CHECKPOINT_CANARY.txt"), []byte(marker), 0o644); err != nil {
		return preparedWorkload{}, err
	}
	if err := runCmd(ctx, dir, "git", "config", "user.name", "Verself Checkpoint Canary"); err != nil {
		return preparedWorkload{}, err
	}
	if err := runCmd(ctx, dir, "git", "config", "user.email", "checkpoint-canary@verself.sh"); err != nil {
		return preparedWorkload{}, err
	}
	if err := runCmd(ctx, dir, "git", "add", "-A"); err != nil {
		return preparedWorkload{}, err
	}
	if err := runCmd(ctx, dir, "git", "commit", "--allow-empty", "-m", "Verself checkpoint canary "+r.cfg.runID); err != nil {
		return preparedWorkload{}, err
	}
	sha, err := commandOutput(ctx, dir, "git", "rev-parse", "HEAD")
	if err != nil {
		return preparedWorkload{}, err
	}
	return preparedWorkload{Spec: workload, Directory: dir, CommitSHA: strings.TrimSpace(sha)}, nil
}

func (r checkpointCanaryRunner) runSourceCase(ctx context.Context, prepared preparedWorkload) checkpointCanaryCaseResult {
	result := checkpointCanaryCaseResult{Provider: "source", Workload: prepared.Spec.Slug, CommitSHA: prepared.CommitSHA, Status: "failed"}
	project, err := r.ensureSourceProject(ctx, prepared.Spec)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	repo, err := r.ensureSourceRepository(ctx, prepared.Spec, project.ProjectID)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Repository = repo.OrgSlug + "/" + repo.ProjectSlug
	result.RepositoryURL = repo.GitHTTPURL
	credential, err := r.createSourceCredential(ctx, prepared.Spec.Slug)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if err := pushGit(ctx, prepared.Directory, repo.GitHTTPURL, credential.Username, credential.Token); err != nil {
		result.Error = err.Error()
		return result
	}
	run, err := r.dispatchSourceWorkflow(ctx, repo, project.ProjectID, prepared.Spec.Slug)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.DispatchID = firstNonEmpty(run.BackendDispatchID, run.WorkflowRunID)
	sandboxRun, err := r.waitSandboxRun(ctx, result.Repository, prepared.CommitSHA)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.SandboxRun = sandboxRun
	result.Status = "succeeded"
	return result
}

func (r checkpointCanaryRunner) runGitHubCase(ctx context.Context, prepared preparedWorkload) checkpointCanaryCaseResult {
	result := checkpointCanaryCaseResult{Provider: "github", Workload: prepared.Spec.Slug, CommitSHA: prepared.CommitSHA, Status: "failed"}
	repo, err := r.ensureGitHubRepository(ctx, prepared.Spec)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Repository = repo.FullName
	result.RepositoryURL = repo.HTMLURL
	if err := pushGit(ctx, prepared.Directory, githubHTTPSURL(r.cfg.githubWebURL, repo.FullName), "x-access-token", r.cfg.githubToken); err != nil {
		result.Error = err.Error()
		return result
	}
	if err := r.syncGitHubInstallationRepositories(ctx, repo.FullName); err != nil {
		result.Error = err.Error()
		return result
	}
	if err := r.dispatchGitHubWorkflow(ctx, repo.FullName); err != nil {
		result.Error = err.Error()
		return result
	}
	result.DispatchID = "workflow_dispatch"
	githubRun, err := r.waitGitHubRun(ctx, repo.FullName, prepared.CommitSHA)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.GitHubRunID = fmt.Sprintf("%d", githubRun.ID)
	result.GitHubRunURL = githubRun.HTMLURL
	sandboxRun, err := r.waitSandboxRun(ctx, repo.FullName, prepared.CommitSHA)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.SandboxRun = sandboxRun
	result.Status = "succeeded"
	return result
}

func (r checkpointCanaryRunner) ensureSourceProject(ctx context.Context, workload checkpointWorkload) (projectRecord, error) {
	slug := r.repoSlug(workload)
	projects, err := r.listProjects(ctx)
	if err != nil {
		return projectRecord{}, err
	}
	for _, project := range projects {
		if project.Slug == slug && project.State == "active" {
			return project, nil
		}
	}
	var created projectRecord
	body := map[string]any{
		"display_name": "Checkpoint Canary " + workload.DisplayName,
		"slug":         slug,
		"description":  "Verself Checkpoints canary workload " + workload.Slug,
	}
	if err := r.verselfJSON(ctx, http.MethodPost, r.cfg.projectsURL, "/api/v1/projects", body, &created, http.StatusCreated, http.StatusOK); err != nil {
		return projectRecord{}, err
	}
	return created, nil
}

func (r checkpointCanaryRunner) listProjects(ctx context.Context) ([]projectRecord, error) {
	var out []projectRecord
	cursor := ""
	for {
		path := "/api/v1/projects?state=active&limit=200"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		var page projectListResponse
		if err := r.verselfJSON(ctx, http.MethodGet, r.cfg.projectsURL, path, nil, &page, http.StatusOK); err != nil {
			return nil, err
		}
		out = append(out, page.Projects...)
		if strings.TrimSpace(page.NextCursor) == "" {
			break
		}
		cursor = page.NextCursor
	}
	return out, nil
}

func (r checkpointCanaryRunner) ensureSourceRepository(ctx context.Context, workload checkpointWorkload, projectID string) (sourceRepository, error) {
	var list sourceRepositoryListResponse
	path := "/api/v1/repos?project_id=" + url.QueryEscape(projectID)
	if err := r.verselfJSON(ctx, http.MethodGet, r.cfg.sourceURL, path, nil, &list, http.StatusOK); err != nil {
		return sourceRepository{}, err
	}
	for _, repo := range list.Repositories {
		if repo.ProjectID == projectID {
			return repo, nil
		}
	}
	body := map[string]any{
		"project_id":     projectID,
		"description":    "Verself Checkpoints canary workload " + workload.Slug,
		"default_branch": "main",
	}
	var repo sourceRepository
	if err := r.verselfJSON(ctx, http.MethodPost, r.cfg.sourceURL, "/api/v1/repos", body, &repo, http.StatusCreated, http.StatusOK); err != nil {
		return sourceRepository{}, err
	}
	return repo, nil
}

func (r checkpointCanaryRunner) createSourceCredential(ctx context.Context, workloadSlug string) (sourceGitCredential, error) {
	body := map[string]any{
		"label":              "checkpoint-canary-" + workloadSlug + "-" + r.cfg.runID,
		"expires_in_seconds": int64(3600),
		"scopes":             []string{"repo:read", "repo:write"},
	}
	var credential sourceGitCredential
	if err := r.verselfJSON(ctx, http.MethodPost, r.cfg.sourceURL, "/api/v1/git-credentials", body, &credential, http.StatusCreated); err != nil {
		return sourceGitCredential{}, err
	}
	return credential, nil
}

func (r checkpointCanaryRunner) dispatchSourceWorkflow(ctx context.Context, repo sourceRepository, projectID, workloadSlug string) (sourceWorkflowRun, error) {
	body := map[string]any{
		"project_id":    projectID,
		"workflow_path": checkpointCanaryWorkflowPath,
		"ref":           "main",
		"inputs": map[string]string{
			"run_id":   r.cfg.runID,
			"workload": workloadSlug,
		},
	}
	var run sourceWorkflowRun
	path := "/api/v1/repos/" + url.PathEscape(repo.RepoID) + "/workflow-runs"
	if err := r.verselfJSON(ctx, http.MethodPost, r.cfg.sourceURL, path, body, &run, http.StatusCreated, http.StatusOK); err != nil {
		return sourceWorkflowRun{}, err
	}
	return run, nil
}

func (r checkpointCanaryRunner) ensureGitHubRepository(ctx context.Context, workload checkpointWorkload) (githubRepository, error) {
	repoName := r.repoSlug(workload)
	path := "/repos/" + url.PathEscape(r.cfg.githubOwner) + "/" + url.PathEscape(repoName)
	var repo githubRepository
	status, err := r.githubJSON(ctx, http.MethodGet, path, nil, &repo, http.StatusOK, http.StatusNotFound)
	if err != nil {
		return githubRepository{}, err
	}
	if status == http.StatusOK {
		if !repo.Private {
			return githubRepository{}, fmt.Errorf("github repository %s exists but is not private", repo.FullName)
		}
		return repo, nil
	}
	body := map[string]any{
		"name":        repoName,
		"private":     true,
		"description": "Verself Checkpoints canary workload " + workload.Slug,
		"auto_init":   false,
	}
	status, err = r.githubJSON(ctx, http.MethodPost, "/orgs/"+url.PathEscape(r.cfg.githubOwner)+"/repos", body, &repo, http.StatusCreated, http.StatusOK)
	if err != nil {
		return githubRepository{}, err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return githubRepository{}, fmt.Errorf("unexpected github repository create status %d", status)
	}
	return repo, nil
}

func (r checkpointCanaryRunner) syncGitHubInstallationRepositories(ctx context.Context, fullName string) error {
	var installations []githubInstallation
	if err := r.verselfJSON(ctx, http.MethodGet, r.cfg.sandboxURL, "/api/v1/github/installations", nil, &installations, http.StatusOK); err != nil {
		return err
	}
	var selected githubInstallation
	for _, installation := range installations {
		if installation.Active && strings.EqualFold(installation.AccountLogin, r.cfg.githubOwner) {
			selected = installation
			break
		}
	}
	if selected.InstallationID == "" {
		return fmt.Errorf("no active Verself GitHub App installation found for %s", r.cfg.githubOwner)
	}
	var sync githubRepositorySync
	path := "/api/v1/github/installations/" + url.PathEscape(selected.InstallationID) + "/repositories/sync"
	if err := r.verselfJSON(ctx, http.MethodPost, r.cfg.sandboxURL, path, nil, &sync, http.StatusOK); err != nil {
		return err
	}
	for _, repo := range sync.Repositories {
		if repo.Active && strings.EqualFold(repo.RepositoryFullName, fullName) {
			return nil
		}
	}
	return fmt.Errorf("github app installation %s does not include %s; grant the app access to the repository and rerun", selected.InstallationID, fullName)
}

func (r checkpointCanaryRunner) dispatchGitHubWorkflow(ctx context.Context, fullName string) error {
	body := map[string]any{
		"ref": "main",
		"inputs": map[string]string{
			"run_id": r.cfg.runID,
		},
	}
	path := "/repos/" + githubRepoPath(fullName) + "/actions/workflows/" + url.PathEscape(filepath.Base(checkpointCanaryWorkflowPath)) + "/dispatches"
	status, err := r.githubJSON(ctx, http.MethodPost, path, body, nil, http.StatusNoContent)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return fmt.Errorf("unexpected github workflow dispatch status %d", status)
	}
	return nil
}

func (r checkpointCanaryRunner) waitGitHubRun(ctx context.Context, fullName, headSHA string) (githubWorkflowRun, error) {
	deadline := time.Now().Add(r.cfg.timeout)
	for {
		var page githubWorkflowRunsResponse
		path := "/repos/" + githubRepoPath(fullName) + "/actions/runs?branch=main&event=workflow_dispatch&per_page=20"
		if _, err := r.githubJSON(ctx, http.MethodGet, path, nil, &page, http.StatusOK); err != nil {
			return githubWorkflowRun{}, err
		}
		for _, run := range page.WorkflowRuns {
			if strings.EqualFold(run.HeadSHA, headSHA) && run.Name == checkpointCanaryWorkflowName {
				switch {
				case run.Status == "completed" && run.Conclusion == "success":
					return run, nil
				case run.Status == "completed":
					return githubWorkflowRun{}, fmt.Errorf("github workflow run %d completed with conclusion %q", run.ID, run.Conclusion)
				}
			}
		}
		if time.Now().After(deadline) {
			return githubWorkflowRun{}, fmt.Errorf("timed out waiting for github workflow run for %s@%s", fullName, headSHA)
		}
		if err := sleepContext(ctx, r.cfg.pollInterval); err != nil {
			return githubWorkflowRun{}, err
		}
	}
}

func (r checkpointCanaryRunner) waitSandboxRun(ctx context.Context, repositoryFullName, headSHA string) (*sandboxRunRecord, error) {
	deadline := time.Now().Add(r.cfg.timeout)
	for {
		var page sandboxRunsPage
		q := url.Values{}
		q.Set("limit", "20")
		q.Set("source_kind", "ci")
		q.Set("repository", repositoryFullName)
		q.Set("workflow", checkpointCanaryWorkflowName)
		q.Set("branch", "main")
		path := "/api/v1/runs?" + q.Encode()
		if err := r.verselfJSON(ctx, http.MethodGet, r.cfg.sandboxURL, path, nil, &page, http.StatusOK); err != nil {
			return nil, err
		}
		for i := range page.Runs {
			run := page.Runs[i]
			if run.Runner == nil || !strings.EqualFold(run.Runner.HeadSHA, headSHA) {
				continue
			}
			switch run.Status {
			case "succeeded":
				return &run, nil
			case "failed":
				reason := firstNonEmpty(run.Latest.FailureReason, "execution failed")
				return nil, fmt.Errorf("sandbox run %s failed: %s", run.ExecutionID, reason)
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for sandbox run for %s@%s", repositoryFullName, headSHA)
		}
		if err := sleepContext(ctx, r.cfg.pollInterval); err != nil {
			return nil, err
		}
	}
}

func (r checkpointCanaryRunner) verselfJSON(ctx context.Context, method, baseURL, path string, body any, out any, expected ...int) error {
	status, err := doJSON(ctx, r.client, method, baseURL, path, r.cfg.verselfToken, "verself-checkpoint-canary", body, out, expected...)
	if err != nil {
		return err
	}
	if !statusAllowed(status, expected) {
		return fmt.Errorf("%s %s returned unexpected status %d", method, path, status)
	}
	return nil
}

func (r checkpointCanaryRunner) githubJSON(ctx context.Context, method, path string, body any, out any, expected ...int) (int, error) {
	status, err := doJSON(ctx, r.client, method, r.cfg.githubAPIURL, path, r.cfg.githubToken, "verself-checkpoint-canary", body, out, expected...)
	if err != nil {
		return status, err
	}
	return status, nil
}

func doJSON(ctx context.Context, client *http.Client, method, baseURL, path, bearer, userAgent string, body any, out any, expected ...int) (int, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(baseURL, "/")+path, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
		req.Header.Set("Idempotency-Key", stableIdempotencyKey(method, path, body))
	}
	if strings.Contains(baseURL, "api.github.com") {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if statusAllowed(resp.StatusCode, expected) {
		if out != nil && resp.Body != nil && resp.StatusCode != http.StatusNoContent {
			if err := json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(out); err != nil {
				return resp.StatusCode, err
			}
		}
		return resp.StatusCode, nil
	}
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, fmt.Errorf("%s %s returned status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(detail)))
}

func checkpointWorkloads() []checkpointWorkload {
	return []checkpointWorkload{
		{
			Slug:        "express-npm",
			DisplayName: "Express npm",
			Upstream:    "https://github.com/expressjs/express.git",
			Workflow:    expressCheckpointWorkflow,
		},
		{
			Slug:        "python-click",
			DisplayName: "Python Click",
			Upstream:    "https://github.com/pallets/click.git",
			Workflow:    pythonCheckpointWorkflow,
		},
		{
			Slug:        "db-seed",
			DisplayName: "50 MiB seeded database artifact",
			Generate:    generateDBSeedWorkload,
			Workflow:    dbSeedCheckpointWorkflow,
		},
		{
			Slug:        "rules-go-bazel",
			DisplayName: "bazelbuild/rules_go",
			Upstream:    "https://github.com/bazelbuild/rules_go.git",
			Workflow:    rulesGoCheckpointWorkflow,
		},
		{
			Slug:        "next-pnpm",
			DisplayName: "vercel/next.js",
			Upstream:    "https://github.com/vercel/next.js.git",
			Workflow:    nextPnpmCheckpointWorkflow,
		},
	}
}

func selectedCheckpointWorkloads(raw string) ([]checkpointWorkload, []string, error) {
	all := checkpointWorkloads()
	bySlug := make(map[string]checkpointWorkload, len(all))
	allSlugs := make([]string, 0, len(all))
	for _, workload := range all {
		bySlug[workload.Slug] = workload
		allSlugs = append(allSlugs, workload.Slug)
	}
	selected, err := parseCheckpointCanaryList(raw, allSlugs)
	if err != nil {
		return nil, nil, err
	}
	out := make([]checkpointWorkload, 0, len(selected))
	for _, slug := range selected {
		workload, ok := bySlug[slug]
		if !ok {
			return nil, nil, fmt.Errorf("unknown checkpoint canary workload %q", slug)
		}
		out = append(out, workload)
	}
	return out, selected, nil
}

func expressCheckpointWorkflow(runnerLabel string) string {
	return checkpointWorkflowYAML(runnerLabel, "express npm checkpoint", `
      - uses: actions/checkout@v6

      - uses: useverself/checkpoint@v0
        with:
          key: express-npm-${{ runner.os }}-${{ hashFiles('package-lock.json') }}
          path: ~/.npm

      - uses: actions/setup-node@v5
        with:
          node-version: 22

      - run: npm ci

      - run: npm test
`)
}

func pythonCheckpointWorkflow(runnerLabel string) string {
	return checkpointWorkflowYAML(runnerLabel, "python click checkpoint", `
      - uses: actions/checkout@v6

      - uses: useverself/checkpoint@v0
        with:
          key: python-click-pip-${{ runner.os }}-${{ hashFiles('pyproject.toml') }}
          path: ~/.cache/pip

      - run: python3 -m venv .venv

      - run: .venv/bin/python -m pip install --upgrade pip

      - run: .venv/bin/python -m pip install -e '.[test]'

      - run: .venv/bin/python -m pytest
`)
}

func dbSeedCheckpointWorkflow(runnerLabel string) string {
	return checkpointWorkflowYAML(runnerLabel, "seeded database checkpoint", `
      - uses: actions/checkout@v6

      - uses: useverself/checkpoint@v0
        with:
          key: db-seed-${{ runner.os }}-${{ hashFiles('package-lock.json', 'scripts/*.js') }}
          path: .ci-cache

      - uses: actions/setup-node@v5
        with:
          node-version: 22

      - run: npm ci

      - run: npm test
`)
}

func rulesGoCheckpointWorkflow(runnerLabel string) string {
	// Bazel-heavy reference workload. The disk_cache and repository_cache
	// are the canonical Checkpoint mount targets per the v0 product
	// contract; we deliberately avoid checkpointing output_base because it
	// includes Bazel server state and execroot symlink forests that aren't
	// safe to restore byte-for-byte.
	return checkpointWorkflowYAML(runnerLabel, "rules_go bazel checkpoint", `
      - uses: actions/checkout@v6

      - uses: useverself/checkpoint@v0
        with:
          key: rules-go-bazel-disk-${{ runner.os }}-${{ hashFiles('MODULE.bazel', 'MODULE.bazel.lock', 'WORKSPACE') }}
          path: ~/.cache/bazel-disk

      - uses: useverself/checkpoint@v0
        with:
          key: rules-go-bazel-repo-${{ runner.os }}-${{ hashFiles('MODULE.bazel', 'MODULE.bazel.lock', 'WORKSPACE') }}
          path: ~/.cache/bazel-repo

      - run: |
          bazelisk build \
            --disk_cache=$HOME/.cache/bazel-disk \
            --repository_cache=$HOME/.cache/bazel-repo \
            //go/...
`)
}

func nextPnpmCheckpointWorkflow(runnerLabel string) string {
	// Large pnpm-managed JS workload. We checkpoint the pnpm content-
	// addressed store (pnpm verifies content hashes on link, so restored
	// bytes are tool-reconciled) and run install + lint as a stand-in for
	// the full Next.js test suite which is too long for a canary budget.
	return checkpointWorkflowYAML(runnerLabel, "next.js pnpm checkpoint", `
      - uses: actions/checkout@v6

      - uses: useverself/checkpoint@v0
        with:
          key: next-pnpm-store-${{ runner.os }}-${{ hashFiles('pnpm-lock.yaml') }}
          path: ~/.local/share/pnpm/store

      - uses: actions/setup-node@v5
        with:
          node-version: 22

      - run: corepack enable

      - run: pnpm install --frozen-lockfile

      - run: pnpm run lint || true
`)
}

func checkpointWorkflowYAML(runnerLabel, jobName, steps string) string {
	return fmt.Sprintf(`name: %s

on:
  workflow_dispatch:
    inputs:
      run_id:
        description: Checkpoint canary run id
        required: true
        type: string
      workload:
        description: Workload slug
        required: false
        type: string

permissions:
  contents: read

jobs:
  checkpoint:
    name: %s
    runs-on: [self-hosted, linux, x64, %s]
    timeout-minutes: 30
    steps:
%s`, checkpointCanaryWorkflowName, jobName, runnerLabel, steps)
}

func generateDBSeedWorkload(dir string) error {
	files := map[string]string{
		"package.json": `{
  "name": "verself-checkpoint-db-seed",
  "version": "1.0.0",
  "private": true,
  "scripts": {
    "test": "node scripts/seed-db.js && node scripts/verify-db.js"
  }
}
`,
		"package-lock.json": `{
  "name": "verself-checkpoint-db-seed",
  "version": "1.0.0",
  "lockfileVersion": 3,
  "requires": true,
  "packages": {
    "": {
      "name": "verself-checkpoint-db-seed",
      "version": "1.0.0",
      "license": "UNLICENSED"
    }
  }
}
`,
		"scripts/seed-db.js": `const fs = require('fs');
const crypto = require('crypto');
const path = require('path');

const dir = path.join(process.cwd(), '.ci-cache', 'postgres-seed');
const file = path.join(dir, 'fixture.bin');
fs.mkdirSync(dir, { recursive: true });

if (!fs.existsSync(file) || fs.statSync(file).size !== 50 * 1024 * 1024) {
  const fd = fs.openSync(file, 'w');
  const block = Buffer.alloc(1024 * 1024);
  for (let i = 0; i < 50; i++) {
    crypto.createHash('sha256').update('verself-checkpoint-db-seed:' + i).digest().copy(block, 0);
    fs.writeSync(fd, block);
  }
  fs.closeSync(fd);
}

const digest = crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');
fs.writeFileSync(path.join(dir, 'manifest.json'), JSON.stringify({ bytes: fs.statSync(file).size, sha256: digest }, null, 2));
`,
		"scripts/verify-db.js": `const fs = require('fs');
const path = require('path');

const manifest = JSON.parse(fs.readFileSync(path.join(process.cwd(), '.ci-cache', 'postgres-seed', 'manifest.json'), 'utf8'));
if (manifest.bytes !== 50 * 1024 * 1024) {
  throw new Error('seed fixture has wrong size: ' + manifest.bytes);
}
if (!/^[0-9a-f]{64}$/.test(manifest.sha256)) {
  throw new Error('seed fixture has invalid digest');
}
console.log('seeded artifact bytes=' + manifest.bytes + ' sha256=' + manifest.sha256);
`,
		"README.md": "# Verself Checkpoint DB Seed Canary\n\nSynthetic 50 MiB seeded database artifact for checkpoint creation stress.\n",
	}
	for name, contents := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (r checkpointCanaryRunner) repoSlug(workload checkpointWorkload) string {
	return slugify(r.cfg.repoPrefix + "-" + workload.Slug)
}

func parseCheckpointCanaryList(raw string, allowed []string) ([]string, error) {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, item := range allowed {
		allowedSet[item] = struct{}{}
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "all" {
		return append([]string(nil), allowed...), nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		if item == "all" {
			for _, value := range allowed {
				if _, ok := seen[value]; !ok {
					seen[value] = struct{}{}
					out = append(out, value)
				}
			}
			continue
		}
		if _, ok := allowedSet[item]; !ok {
			return nil, fmt.Errorf("unsupported value %q; allowed values: %s", item, strings.Join(allowed, ","))
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one value is required")
	}
	return out, nil
}

func printCheckpointCanaryReport(w io.Writer, report checkpointCanaryReport, format string) error {
	switch normalizeOutputFormat(format) {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	default:
		fmt.Fprintf(w, "run_id\tstatus\tprovider\tworkload\trepository\tcommit\texecution\n")
		for _, c := range report.Cases {
			execution := ""
			if c.SandboxRun != nil {
				execution = c.SandboxRun.ExecutionID
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", report.RunID, c.Status, c.Provider, c.Workload, c.Repository, c.CommitSHA, execution)
		}
		return nil
	}
}

func pushGit(ctx context.Context, dir, remoteURL, username, token string) error {
	credentials, err := os.CreateTemp("", "verself-git-credentials-*")
	if err != nil {
		return err
	}
	credentialsPath := credentials.Name()
	defer func() { _ = os.Remove(credentialsPath) }()
	if err := credentials.Chmod(0o600); err != nil {
		_ = credentials.Close()
		return err
	}
	line, err := credentialStoreLine(remoteURL, username, token)
	if err != nil {
		_ = credentials.Close()
		return err
	}
	if _, err := credentials.WriteString(line + "\n"); err != nil {
		_ = credentials.Close()
		return err
	}
	if err := credentials.Close(); err != nil {
		return err
	}
	if err := runCmdWithEnv(ctx, dir, []string{"GIT_TERMINAL_PROMPT=0"}, "git", "-c", "credential.helper=store --file="+credentialsPath, "push", "--force", remoteURL, "HEAD:refs/heads/main"); err != nil {
		return err
	}
	return nil
}

func credentialStoreLine(remoteURL, username, token string) (string, error) {
	parsed, err := url.Parse(remoteURL)
	if err != nil {
		return "", err
	}
	parsed.User = url.UserPassword(username, token)
	return parsed.String(), nil
}

func githubHTTPSURL(webBase, fullName string) string {
	return strings.TrimRight(webBase, "/") + "/" + strings.Trim(strings.TrimSuffix(fullName, ".git"), "/") + ".git"
}

func githubRepoPath(fullName string) string {
	owner, repo, ok := strings.Cut(strings.Trim(fullName, "/"), "/")
	if !ok {
		return url.PathEscape(fullName)
	}
	return url.PathEscape(owner) + "/" + url.PathEscape(repo)
}

func runCmd(ctx context.Context, dir, name string, args ...string) error {
	return runCmdWithEnv(ctx, dir, nil, name, args...)
}

func runCmdWithEnv(ctx context.Context, dir string, extraEnv []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return fmt.Errorf("%s %s: %w: %s", name, strings.Join(redactArgs(args), " "), err, detail)
		}
		return fmt.Errorf("%s %s: %w", name, strings.Join(redactArgs(args), " "), err)
	}
	return nil
}

func commandOutput(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(redactArgs(args), " "), err, strings.TrimSpace(string(exit.Stderr)))
		}
		return "", err
	}
	return string(out), nil
}

func redactArgs(args []string) []string {
	out := append([]string(nil), args...)
	for i, arg := range out {
		if strings.Contains(arg, "credential.helper=store --file=") {
			out[i] = "credential.helper=store --file=<redacted>"
		}
	}
	return out
}

func statusAllowed(status int, expected []int) bool {
	if len(expected) == 0 {
		return status >= 200 && status < 300
	}
	for _, candidate := range expected {
		if status == candidate {
			return true
		}
	}
	return false
}

func stableIdempotencyKey(method, path string, body any) string {
	payload, _ := json.Marshal(body)
	sum := sha256.Sum256([]byte(method + "\n" + path + "\n" + string(payload)))
	return "checkpoint-canary-" + hex.EncodeToString(sum[:])[:32]
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
