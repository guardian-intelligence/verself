package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	pathpkg "path"
	"strings"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"gopkg.in/yaml.v3"
)

var (
	ErrGitHubWorkflowContentsPermission = errors.New("github app contents permission required")
	ErrGitHubContentNotFound            = errors.New("github repository content not found")
)

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
	ref := firstNonEmpty(allocation.HeadSHA, run.HeadSHA)
	workflow, err := r.fetchWorkflowFile(ctx, allocation.InstallationID, allocation.RepositoryFullName, run.Path, ref)
	if err != nil {
		return nil, err
	}
	loadContent := func(relativePath string) ([]byte, error) {
		return r.fetchRepositoryFile(ctx, allocation.InstallationID, allocation.RepositoryFullName, relativePath, ref)
	}
	decls, err := stickyDiskDeclarationsForJob(workflow, allocation.JobName, allocation.RunnerClass, allocation.RepositoryFullName, loadContent)
	if err != nil {
		return nil, err
	}
	mounts := make([]StickyDiskMountSpec, 0, len(decls))
	restoreHits := 0
	restoreMisses := 0
	seenPaths := map[string]struct{}{}
	for idx, decl := range decls {
		key, err := normalizeStickyDiskKey(decl.Key)
		if err != nil {
			return nil, err
		}
		mountPath, err := resolveStickyDiskPath(decl.Path, allocation.RepositoryFullName)
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
		if generation > 0 && sourceRef != stickyDiskEmptySourceRef {
			restoreHits++
		} else {
			restoreMisses++
		}
		mounts = append(mounts, stickyDiskMountSpec(allocation.AllocationID, attemptID, idx, allocation.InstallationID, allocation.RepositoryID, key, mountPath, generation, sourceRef))
	}
	span.SetAttributes(
		traceOrgID(allocation.OrgID),
		traceInt64("github.run_id", allocation.RunID),
		traceInt64("github.job_id", allocation.RequestedJobID),
		attribute.String("github.repository", allocation.RepositoryFullName),
		attribute.String("github.runner_class", allocation.RunnerClass),
		attribute.Int("github.stickydisk.restore_hit_count", restoreHits),
		attribute.Int("github.stickydisk.restore_miss_count", restoreMisses),
	)
	return mounts, nil
}

type workflowContentLoader func(relativePath string) ([]byte, error)

func stickyDiskDeclarationsForJob(data []byte, jobName, runnerClass, repositoryFullName string, loadContent workflowContentLoader) ([]stickyDiskDeclaration, error) {
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
		switch {
		case isVerselfStickyDiskAction(step.Uses):
			key, _ := workflowString(step.With["key"])
			mountPath, _ := workflowString(step.With["path"])
			key = expandGitHubExpression(key, repositoryFullName)
			mountPath = expandGitHubExpression(mountPath, repositoryFullName)
			if strings.TrimSpace(key) == "" || strings.TrimSpace(mountPath) == "" {
				return nil, fmt.Errorf("%w: sticky disk step requires key and path", ErrStickyDiskInvalid)
			}
			decls = append(decls, stickyDiskDeclaration{Key: key, Path: mountPath})
		case isVerselfSetupNodeAction(step.Uses):
			setupDecls, err := setupNodeStickyDiskDeclarations(step.With, runnerClass, repositoryFullName, loadContent)
			if err != nil {
				return nil, err
			}
			decls = append(decls, setupDecls...)
		}
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

func isVerselfStickyDiskAction(uses string) bool {
	uses = strings.TrimSpace(strings.ToLower(uses))
	return uses == "./.github/actions/stickydisk" ||
		strings.HasPrefix(uses, "guardian-intelligence/verself/.github/actions/stickydisk@") ||
		strings.HasPrefix(uses, "verself/stickydisk@")
}

func isVerselfSetupNodeAction(uses string) bool {
	uses = strings.TrimSpace(strings.ToLower(uses))
	return uses == "./.github/actions/setup-node" ||
		strings.HasPrefix(uses, "guardian-intelligence/verself/.github/actions/setup-node@") ||
		strings.HasPrefix(uses, "verself/setup-node@")
}

type setupNodeCacheSpec struct {
	RepositoryFullName string
	RunnerClass        string
	NodeVersion        string
	PackageManager     string
	PackageManagerSpec string
	WorkingDirectory   string
	LockHash           string
}

func setupNodeStickyDiskDeclarations(with map[string]any, runnerClass, repositoryFullName string, loadContent workflowContentLoader) ([]stickyDiskDeclaration, error) {
	if loadContent == nil {
		return nil, fmt.Errorf("%w: setup-node cache requires repository content loader", ErrStickyDiskInvalid)
	}
	nodeVersion, _ := workflowString(with["node-version"])
	packageManager, _ := workflowString(with["package-manager"])
	workingDirectory, _ := workflowString(with["working-directory"])
	cache := workflowBool(with["cache"], true)
	nodeModules := workflowBool(with["node-modules"], false)
	if !cache && !nodeModules {
		return nil, nil
	}
	workingDirectory, err := normalizeWorkflowRelativePath(firstNonEmpty(workingDirectory, "."))
	if err != nil {
		return nil, err
	}
	packageManager = strings.ToLower(strings.TrimSpace(packageManager))
	if packageManager != "pnpm" {
		return nil, fmt.Errorf("%w: setup-node package-manager %q is not supported yet", ErrStickyDiskInvalid, packageManager)
	}
	lockPath := workflowJoin(workingDirectory, "pnpm-lock.yaml")
	lockBytes, err := loadContent(lockPath)
	if err != nil {
		return nil, fmt.Errorf("%w: setup-node requires %s for cache key: %v", ErrStickyDiskInvalid, lockPath, err)
	}
	packageManagerSpec := packageManager
	if packageJSON, err := loadContent(workflowJoin(workingDirectory, "package.json")); err == nil {
		packageManagerSpec = packageManagerSpecFromPackageJSON(packageJSON, packageManager)
	} else if !errors.Is(err, ErrGitHubContentNotFound) {
		return nil, err
	}
	spec := setupNodeCacheSpec{
		RepositoryFullName: repositoryFullName,
		RunnerClass:        runnerClass,
		NodeVersion:        normalizeSetupNodeVersion(nodeVersion),
		PackageManager:     packageManager,
		PackageManagerSpec: packageManagerSpec,
		WorkingDirectory:   workingDirectory,
		LockHash:           sha256Hex(lockBytes),
	}
	if spec.NodeVersion == "" {
		return nil, fmt.Errorf("%w: setup-node node-version is required", ErrStickyDiskInvalid)
	}
	decls := []stickyDiskDeclaration{}
	if cache {
		decls = append(decls, stickyDiskDeclaration{Key: setupNodeStickyKey(spec, "store"), Path: "~/.pnpm-store"})
	}
	if nodeModules {
		decls = append(decls, stickyDiskDeclaration{Key: setupNodeStickyKey(spec, "node_modules"), Path: workflowJoin(workingDirectory, "node_modules")})
	}
	return decls, nil
}

func setupNodeStickyKey(spec setupNodeCacheSpec, kind string) string {
	return strings.Join([]string{
		"setup-node:v1",
		"repo=" + spec.RepositoryFullName,
		"runner=" + spec.RunnerClass,
		"node=" + spec.NodeVersion,
		"pm=" + spec.PackageManagerSpec,
		"workdir=" + spec.WorkingDirectory,
		"lock=" + spec.LockHash,
		kind,
	}, ":")
}

func packageManagerSpecFromPackageJSON(data []byte, fallback string) string {
	var parsed struct {
		PackageManager string `json:"packageManager"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fallback
	}
	if strings.TrimSpace(parsed.PackageManager) == "" {
		return fallback
	}
	return strings.TrimSpace(parsed.PackageManager)
}

func workflowBool(value any, fallback bool) bool {
	raw, ok := workflowString(value)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func normalizeWorkflowRelativePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "."
	}
	cleaned := pathpkg.Clean(value)
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("%w: setup-node working-directory must stay inside the GitHub workspace", ErrStickyDiskInvalid)
	}
	return cleaned, nil
}

func normalizeSetupNodeVersion(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "v"), "V"))
	value = strings.TrimSuffix(strings.TrimSuffix(value, ".x"), ".X")
	return value
}

func workflowJoin(base, elem string) string {
	if base == "." || strings.TrimSpace(base) == "" {
		return elem
	}
	return pathpkg.Join(base, elem)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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
	data, err := r.fetchRepositoryFile(ctx, installationID, repoFullName, workflowPath, ref)
	if err != nil {
		if strings.Contains(err.Error(), "status 403") && strings.Contains(err.Error(), "Resource not accessible by integration") {
			return nil, fmt.Errorf("%w: repository Contents read permission is required to compile preboot sticky disks from workflow YAML: %v", ErrGitHubWorkflowContentsPermission, err)
		}
		return nil, err
	}
	return data, nil
}

func (r *GitHubRunner) fetchRepositoryFile(ctx context.Context, installationID int64, repoFullName, repoPath, ref string) ([]byte, error) {
	repoPath = strings.TrimPrefix(strings.TrimSpace(repoPath), "/")
	ref = strings.TrimSpace(ref)
	if repoPath == "" || ref == "" {
		return nil, fmt.Errorf("repository path and ref are required")
	}
	token, err := r.installationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}
	var resp githubContentResponse
	path := githubRepoAPIPath(repoFullName, "/contents/"+escapeGitHubPath(repoPath)+"?ref="+url.QueryEscape(ref))
	if err := r.githubRequest(ctx, http.MethodGet, path, token, nil, &resp, http.StatusOK); err != nil {
		if strings.Contains(err.Error(), "status 404") {
			return nil, fmt.Errorf("%w: %s", ErrGitHubContentNotFound, repoPath)
		}
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(resp.Encoding), "base64") {
		return nil, fmt.Errorf("github contents response uses unsupported encoding %q", resp.Encoding)
	}
	content := strings.NewReplacer("\n", "", "\r", "").Replace(resp.Content)
	data, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return nil, fmt.Errorf("decode repository content %s: %w", repoPath, err)
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
