package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

type ImportRepoRequest struct {
	Provider       string `json:"provider"`
	ProviderRepoID string `json:"provider_repo_id,omitempty"`
	Owner          string `json:"owner,omitempty"`
	Name           string `json:"name,omitempty"`
	FullName       string `json:"full_name,omitempty"`
	CloneURL       string `json:"clone_url"`
	DefaultBranch  string `json:"default_branch,omitempty"`
}

type WorkflowScanIssue struct {
	Path    string   `json:"path"`
	JobID   string   `json:"job_id,omitempty"`
	Reason  string   `json:"reason"`
	Labels  []string `json:"labels,omitempty"`
	Details string   `json:"details,omitempty"`
}

type workflowCompatibilitySummary struct {
	WorkflowPaths     []string            `json:"workflow_paths,omitempty"`
	UnsupportedLabels []string            `json:"unsupported_labels,omitempty"`
	Issues            []WorkflowScanIssue `json:"issues,omitempty"`
}

type workflowFile struct {
	Jobs map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	RunsOn yaml.Node `yaml:"runs-on"`
}

func (s *Service) ImportRepo(ctx context.Context, orgID uint64, req ImportRepoRequest) (*RepoRecord, error) {
	req, err := normalizeImportRepoRequest(req)
	if err != nil {
		return nil, err
	}

	if existing, ok, err := s.findRepoByExternalKey(ctx, orgID, req.Provider, req.ProviderRepoID, req.FullName); err != nil {
		return nil, err
	} else if ok {
		return s.RescanRepo(ctx, orgID, existing.RepoID)
	}

	repo, err := s.CreateRepo(ctx, CreateRepoRequest{
		OrgID:          orgID,
		Provider:       req.Provider,
		ProviderRepoID: req.ProviderRepoID,
		Owner:          req.Owner,
		Name:           req.Name,
		FullName:       req.FullName,
		CloneURL:       req.CloneURL,
		DefaultBranch:  req.DefaultBranch,
	})
	if err != nil {
		return nil, err
	}
	return s.RescanRepo(ctx, orgID, repo.RepoID)
}

func (s *Service) RescanRepo(ctx context.Context, orgID uint64, repoID uuid.UUID) (*RepoRecord, error) {
	repo, err := s.GetRepo(ctx, orgID, repoID)
	if err != nil {
		return nil, err
	}

	result, err := scanRepoCompatibility(repo.CloneURL, repo.DefaultBranch)
	if err != nil {
		summary := map[string]any{
			"issues": []WorkflowScanIssue{{
				Reason:  "scan_failed",
				Details: err.Error(),
			}},
		}
		data, _ := json.Marshal(summary)
		return s.RecordRepoCompatibility(ctx, repoID, RepoCompatibilityResult{
			Compatible:           false,
			CompatibilityStatus:  "scan_failed",
			CompatibilitySummary: data,
		})
	}
	return s.RecordRepoCompatibility(ctx, repoID, result)
}

func (s *Service) findRepoByExternalKey(ctx context.Context, orgID uint64, provider, providerRepoID, fullName string) (*RepoRecord, bool, error) {
	var (
		query string
		args  []any
	)
	switch {
	case strings.TrimSpace(providerRepoID) != "":
		query = `
			SELECT
				repo_id,
				org_id,
				provider,
				provider_repo_id,
				owner,
				name,
				full_name,
				clone_url,
				default_branch,
				runner_profile_slug,
				state,
				compatibility_status,
				compatibility_summary,
				last_scanned_sha,
				COALESCE(active_golden_generation_id::text, ''),
				last_ready_sha,
				last_error,
				created_at,
				updated_at,
				archived_at
			FROM repos
			WHERE org_id = $1 AND provider = $2 AND provider_repo_id = $3
		`
		args = []any{int64(orgID), provider, providerRepoID}
	default:
		query = `
			SELECT
				repo_id,
				org_id,
				provider,
				provider_repo_id,
				owner,
				name,
				full_name,
				clone_url,
				default_branch,
				runner_profile_slug,
				state,
				compatibility_status,
				compatibility_summary,
				last_scanned_sha,
				COALESCE(active_golden_generation_id::text, ''),
				last_ready_sha,
				last_error,
				created_at,
				updated_at,
				archived_at
			FROM repos
			WHERE org_id = $1 AND provider = $2 AND full_name = $3
		`
		args = []any{int64(orgID), provider, fullName}
	}

	record, err := scanRepoRow(s.PG.QueryRowContext(ctx, query, args...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return record, true, nil
}

func normalizeImportRepoRequest(req ImportRepoRequest) (ImportRepoRequest, error) {
	req.Provider = strings.TrimSpace(req.Provider)
	req.ProviderRepoID = strings.TrimSpace(req.ProviderRepoID)
	req.Owner = strings.TrimSpace(req.Owner)
	req.Name = strings.TrimSpace(req.Name)
	req.FullName = strings.TrimSpace(req.FullName)
	req.CloneURL = strings.TrimSpace(req.CloneURL)
	req.DefaultBranch = strings.TrimSpace(req.DefaultBranch)

	if req.Provider == "" {
		req.Provider = "forgejo"
	}
	if req.CloneURL == "" {
		return ImportRepoRequest{}, fmt.Errorf("clone_url is required")
	}
	if req.FullName == "" {
		req.FullName = repoFullNameFromCloneURL(req.CloneURL)
	}
	if req.FullName == "" {
		return ImportRepoRequest{}, fmt.Errorf("full_name is required")
	}
	if req.Owner == "" || req.Name == "" {
		owner, name := splitRepoFullName(req.FullName)
		req.Owner = firstNonEmpty(req.Owner, owner)
		req.Name = firstNonEmpty(req.Name, name)
	}
	if req.Owner == "" {
		return ImportRepoRequest{}, fmt.Errorf("owner is required")
	}
	if req.Name == "" {
		return ImportRepoRequest{}, fmt.Errorf("name is required")
	}
	if req.ProviderRepoID == "" {
		req.ProviderRepoID = req.FullName
	}
	if req.DefaultBranch == "" {
		req.DefaultBranch = defaultBranchName
	}
	return req, nil
}

func scanRepoCompatibility(repoURL, branch string) (RepoCompatibilityResult, error) {
	repoURL = strings.TrimSpace(repoURL)
	branch = defaultBranch(branch)
	root, err := cloneRepoBranch(repoURL, branch)
	if err != nil {
		return RepoCompatibilityResult{}, err
	}
	defer os.RemoveAll(root)

	commitSHA, err := gitHeadSHA(root)
	if err != nil {
		return RepoCompatibilityResult{}, err
	}

	workflowPaths, err := workflowFiles(root)
	if err != nil {
		return RepoCompatibilityResult{}, err
	}
	if len(workflowPaths) == 0 {
		return compatibilityFailure(commitSHA, workflowCompatibilitySummary{
			Issues: []WorkflowScanIssue{{
				Reason:  "no_workflows",
				Details: "no workflow files found under .github/workflows or .forgejo/workflows",
			}},
		}), nil
	}

	summary := workflowCompatibilitySummary{
		WorkflowPaths: workflowPaths,
	}
	unsupported := make(map[string]struct{})
	for _, relPath := range workflowPaths {
		issues, labels, err := scanWorkflowFile(filepath.Join(root, relPath), relPath)
		if err != nil {
			summary.Issues = append(summary.Issues, WorkflowScanIssue{
				Path:    relPath,
				Reason:  "parse_failed",
				Details: err.Error(),
			})
			continue
		}
		summary.Issues = append(summary.Issues, issues...)
		for _, label := range labels {
			unsupported[label] = struct{}{}
		}
	}

	if len(summary.Issues) > 0 {
		summary.UnsupportedLabels = sortedKeys(unsupported)
		return compatibilityFailure(commitSHA, summary), nil
	}

	data, err := json.Marshal(summary)
	if err != nil {
		return RepoCompatibilityResult{}, err
	}
	return RepoCompatibilityResult{
		Compatible:           true,
		CompatibilityStatus:  CompatibilityStatusCompatible,
		CompatibilitySummary: data,
		LastScannedSHA:       commitSHA,
	}, nil
}

func compatibilityFailure(commitSHA string, summary workflowCompatibilitySummary) RepoCompatibilityResult {
	data, _ := json.Marshal(summary)
	return RepoCompatibilityResult{
		Compatible:           false,
		CompatibilityStatus:  CompatibilityStatusActionRequired,
		CompatibilitySummary: data,
		LastScannedSHA:       commitSHA,
	}
}

func scanWorkflowFile(path, relPath string) ([]WorkflowScanIssue, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read workflow: %w", err)
	}

	var wf workflowFile
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, nil, fmt.Errorf("parse workflow yaml: %w", err)
	}
	if len(wf.Jobs) == 0 {
		return []WorkflowScanIssue{{
			Path:    relPath,
			Reason:  "no_jobs",
			Details: "workflow has no jobs",
		}}, nil, nil
	}

	var (
		issues    []WorkflowScanIssue
		labelsOut []string
	)
	for jobID, job := range wf.Jobs {
		labels, issue := extractRunsOnLabels(job.RunsOn)
		if issue != "" {
			issues = append(issues, WorkflowScanIssue{
				Path:    relPath,
				JobID:   jobID,
				Reason:  issue,
				Details: "runs-on must resolve to forge-metal",
			})
			continue
		}
		if !supportedRunsOn(labels) {
			issues = append(issues, WorkflowScanIssue{
				Path:   relPath,
				JobID:  jobID,
				Reason: "unsupported_runs_on",
				Labels: labels,
			})
			labelsOut = append(labelsOut, labels...)
		}
	}
	return issues, labelsOut, nil
}

func extractRunsOnLabels(node yaml.Node) ([]string, string) {
	switch node.Kind {
	case 0:
		return nil, "missing_runs_on"
	case yaml.ScalarNode:
		value := strings.TrimSpace(node.Value)
		if value == "" {
			return nil, "missing_runs_on"
		}
		if strings.Contains(value, "${{") {
			return nil, "dynamic_runs_on"
		}
		return []string{value}, ""
	case yaml.SequenceNode:
		labels := make([]string, 0, len(node.Content))
		for _, item := range node.Content {
			if item.Kind != yaml.ScalarNode {
				return nil, "dynamic_runs_on"
			}
			value := strings.TrimSpace(item.Value)
			if value == "" || strings.Contains(value, "${{") {
				return nil, "dynamic_runs_on"
			}
			labels = append(labels, value)
		}
		if len(labels) == 0 {
			return nil, "missing_runs_on"
		}
		return labels, ""
	default:
		return nil, "dynamic_runs_on"
	}
}

func supportedRunsOn(labels []string) bool {
	if len(labels) != 1 {
		return false
	}
	return strings.TrimSpace(labels[0]) == RunnerProfileForgeMetal
}

func workflowFiles(root string) ([]string, error) {
	var out []string
	for _, dir := range []string{
		filepath.Join(root, ".github", "workflows"),
		filepath.Join(root, ".forgejo", "workflows"),
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read workflow dir %s: %w", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
				continue
			}
			out = append(out, filepath.ToSlash(strings.TrimPrefix(filepath.Join(dir, name), root+string(filepath.Separator))))
		}
	}
	slices.Sort(out)
	return out, nil
}

func cloneRepoBranch(repoURL, branch string) (string, error) {
	tmp, err := os.MkdirTemp("", "forge-metal-repo-scan-*")
	if err != nil {
		return "", fmt.Errorf("create scan dir: %w", err)
	}
	cmd := exec.Command("git", "clone", "--depth", "1", "--branch", branch, repoURL, tmp)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(tmp)
		return "", fmt.Errorf("git clone --branch %s %s: %s: %w", branch, repoURL, strings.TrimSpace(string(out)), err)
	}
	return tmp, nil
}

func gitHeadSHA(repoRoot string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func repoFullNameFromCloneURL(cloneURL string) string {
	trimmed := strings.TrimSpace(strings.TrimSuffix(cloneURL, "/"))
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimSuffix(trimmed, ".git")
	idx := strings.LastIndex(trimmed, "://")
	if idx >= 0 {
		trimmed = trimmed[idx+3:]
		if slash := strings.Index(trimmed, "/"); slash >= 0 {
			trimmed = trimmed[slash+1:]
		} else {
			return ""
		}
	} else if colon := strings.Index(trimmed, ":"); colon >= 0 && !strings.HasPrefix(trimmed, "/") {
		trimmed = trimmed[colon+1:]
	}
	trimmed = strings.Trim(trimmed, "/")
	return trimmed
}

func splitRepoFullName(fullName string) (string, string) {
	fullName = strings.Trim(strings.TrimSpace(fullName), "/")
	parts := strings.Split(fullName, "/")
	if len(parts) < 2 {
		return "", fullName
	}
	return strings.Join(parts[:len(parts)-1], "/"), parts[len(parts)-1]
}

func sortedKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	slices.Sort(out)
	return out
}
