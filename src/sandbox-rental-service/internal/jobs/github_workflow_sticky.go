package jobs

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

var ErrGitHubWorkflowContentsPermission = errors.New("github app contents permission required")

type githubWorkflowRunResponse struct {
	ID         int64  `json:"id"`
	Path       string `json:"path"`
	HeadSHA    string `json:"head_sha"`
	HeadBranch string `json:"head_branch"`
}

type githubContentResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

type workflowFile struct {
	Jobs map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	Name   string         `yaml:"name"`
	RunsOn any            `yaml:"runs-on"`
	Steps  []workflowStep `yaml:"steps"`
}

type workflowStep struct {
	Uses string         `yaml:"uses"`
	With map[string]any `yaml:"with"`
}

type stickyDiskDeclaration struct {
	Key  string
	Path string
}

func (r *GitHubRunner) prepareStickyDiskMounts(ctx context.Context, allocation githubAllocation, attemptID uuid.UUID) ([]StickyDiskMountSpec, error) {
	ctx, span := tracer.Start(ctx, "github.stickydisk.compile")
	defer span.End()
	if r == nil || r.service == nil || r.service.PGX == nil || allocation.RunID == 0 || strings.TrimSpace(allocation.RepositoryFullName) == "" {
		return nil, nil
	}
	run, err := r.fetchWorkflowRun(ctx, allocation.InstallationID, allocation.RepositoryFullName, allocation.RunID)
	if err != nil {
		return nil, err
	}
	workflow, err := r.fetchWorkflowFile(ctx, allocation.InstallationID, allocation.RepositoryFullName, run.Path, firstNonEmpty(allocation.HeadSHA, run.HeadSHA))
	if err != nil {
		return nil, err
	}
	decls, err := stickyDiskDeclarationsForJob(workflow, allocation.JobName, allocation.RunnerClass, allocation.RepositoryFullName)
	if err != nil {
		return nil, err
	}
	mounts := make([]StickyDiskMountSpec, 0, len(decls))
	seenPaths := map[string]struct{}{}
	for idx, decl := range decls {
		key, err := normalizeStickyDiskKey(decl.Key)
		if err != nil {
			return nil, err
		}
		mountPath, err := resolveStickyDiskPath(decl.Path)
		if err != nil {
			return nil, err
		}
		if _, ok := seenPaths[mountPath]; ok {
			return nil, fmt.Errorf("%w: duplicate sticky disk path %s", ErrStickyDiskInvalid, mountPath)
		}
		seenPaths[mountPath] = struct{}{}
		keyHash := stickyDiskKeyHash(key)
		generation, sourceRef, err := r.currentStickyDiskGeneration(ctx, allocation.InstallationID, allocation.RepositoryID, key, keyHash)
		if err != nil {
			return nil, err
		}
		mounts = append(mounts, stickyDiskMountSpec(allocation.AllocationID, attemptID, idx, allocation.InstallationID, allocation.RepositoryID, key, mountPath, generation, sourceRef))
	}
	span.SetAttributes(traceInt64("github.run_id", allocation.RunID), traceInt64("github.job_id", allocation.RequestedJobID))
	return mounts, nil
}

func stickyDiskDeclarationsForJob(data []byte, jobName, runnerClass, repositoryFullName string) ([]stickyDiskDeclaration, error) {
	var wf workflowFile
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("parse workflow yaml: %w", err)
	}
	jobKey, job, ok := selectWorkflowJob(wf, jobName, runnerClass)
	if !ok {
		return nil, nil
	}
	_ = jobKey
	decls := []stickyDiskDeclaration{}
	for _, step := range job.Steps {
		if !isForgeMetalStickyDiskAction(step.Uses) {
			continue
		}
		key, _ := workflowString(step.With["key"])
		mountPath, _ := workflowString(step.With["path"])
		key = expandGitHubExpression(key, repositoryFullName)
		mountPath = expandGitHubExpression(mountPath, repositoryFullName)
		if strings.TrimSpace(key) == "" || strings.TrimSpace(mountPath) == "" {
			return nil, fmt.Errorf("%w: sticky disk step requires key and path", ErrStickyDiskInvalid)
		}
		decls = append(decls, stickyDiskDeclaration{Key: key, Path: mountPath})
	}
	return decls, nil
}

func selectWorkflowJob(wf workflowFile, jobName, runnerClass string) (string, workflowJob, bool) {
	jobName = strings.TrimSpace(jobName)
	if jobName != "" {
		for key, job := range wf.Jobs {
			if key == jobName || strings.TrimSpace(job.Name) == jobName {
				return key, job, true
			}
		}
	}
	var selectedKey string
	var selected workflowJob
	matches := 0
	for key, job := range wf.Jobs {
		if workflowRunsOnMatches(job.RunsOn, runnerClass) {
			selectedKey = key
			selected = job
			matches++
		}
	}
	if matches == 1 {
		return selectedKey, selected, true
	}
	return "", workflowJob{}, false
}

func workflowRunsOnMatches(value any, runnerClass string) bool {
	runnerClass = strings.TrimSpace(runnerClass)
	switch v := value.(type) {
	case string:
		return v == runnerClass
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s == runnerClass {
				return true
			}
		}
	}
	return false
}

func isForgeMetalStickyDiskAction(uses string) bool {
	uses = strings.TrimSpace(strings.ToLower(uses))
	return uses == "./.github/actions/stickydisk" ||
		strings.HasPrefix(uses, "guardian-intelligence/forge-metal/.github/actions/stickydisk@") ||
		strings.HasPrefix(uses, "forge-metal/stickydisk@")
}

func workflowString(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case fmt.Stringer:
		return v.String(), true
	case nil:
		return "", false
	default:
		return fmt.Sprint(v), true
	}
}

func expandGitHubExpression(value, repositoryFullName string) string {
	value = strings.ReplaceAll(value, "${{ github.repository }}", repositoryFullName)
	value = strings.ReplaceAll(value, "${{github.repository}}", repositoryFullName)
	return strings.TrimSpace(value)
}

func (r *GitHubRunner) fetchWorkflowRun(ctx context.Context, installationID int64, repoFullName string, runID int64) (githubWorkflowRunResponse, error) {
	token, err := r.installationToken(ctx, installationID)
	if err != nil {
		return githubWorkflowRunResponse{}, err
	}
	var resp githubWorkflowRunResponse
	if err := r.githubRequest(ctx, http.MethodGet, githubRepoAPIPath(repoFullName, fmt.Sprintf("/actions/runs/%d", runID)), token, nil, &resp, http.StatusOK); err != nil {
		return githubWorkflowRunResponse{}, err
	}
	return resp, nil
}

func (r *GitHubRunner) fetchWorkflowFile(ctx context.Context, installationID int64, repoFullName, workflowPath, ref string) ([]byte, error) {
	workflowPath = strings.TrimPrefix(strings.TrimSpace(workflowPath), "/")
	ref = strings.TrimSpace(ref)
	if workflowPath == "" || ref == "" {
		return nil, fmt.Errorf("workflow path and ref are required")
	}
	token, err := r.installationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}
	var resp githubContentResponse
	path := githubRepoAPIPath(repoFullName, "/contents/"+escapeGitHubPath(workflowPath)+"?ref="+url.QueryEscape(ref))
	if err := r.githubRequest(ctx, http.MethodGet, path, token, nil, &resp, http.StatusOK); err != nil {
		if strings.Contains(err.Error(), "status 403") && strings.Contains(err.Error(), "Resource not accessible by integration") {
			return nil, fmt.Errorf("%w: repository Contents read permission is required to compile preboot sticky disks from workflow YAML: %v", ErrGitHubWorkflowContentsPermission, err)
		}
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(resp.Encoding), "base64") {
		return nil, fmt.Errorf("github contents response uses unsupported encoding %q", resp.Encoding)
	}
	content := strings.NewReplacer("\n", "", "\r", "").Replace(resp.Content)
	data, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return nil, fmt.Errorf("decode workflow content: %w", err)
	}
	return data, nil
}

func githubRepoAPIPath(repoFullName, suffix string) string {
	owner, repo, ok := strings.Cut(strings.TrimSpace(repoFullName), "/")
	if !ok || owner == "" || repo == "" {
		return "/repos/invalid/invalid" + suffix
	}
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + suffix
}

func escapeGitHubPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}
