package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	repoScanGitCommandTimeout       = 30 * time.Second
	defaultRepoScanConcurrency      = 2
	repoScanAllowFileProtocolForE2E = "FORGE_METAL_REPO_SCAN_E2E_ALLOW_FILE_PROTOCOL"
)

type ImportRepoRequest struct {
	IntegrationID  *uuid.UUID `json:"integration_id,omitempty"`
	Provider       string     `json:"provider,omitempty"`
	ProviderHost   string     `json:"provider_host,omitempty"`
	ProviderRepoID string     `json:"provider_repo_id,omitempty"`
	Owner          string     `json:"owner,omitempty"`
	Name           string     `json:"name,omitempty"`
	FullName       string     `json:"full_name,omitempty"`
	CloneURL       string     `json:"clone_url"`
	DefaultBranch  string     `json:"default_branch,omitempty"`
}

type RepoScanIssue struct {
	Path    string   `json:"path"`
	JobID   string   `json:"job_id,omitempty"`
	Reason  string   `json:"reason"`
	Labels  []string `json:"labels,omitempty"`
	Details string   `json:"details,omitempty"`
}

func (s *Service) ImportRepo(ctx context.Context, orgID uint64, req ImportRepoRequest) (*RepoRecord, error) {
	req, err := normalizeImportRepoRequest(req)
	if err != nil {
		return nil, err
	}

	if existing, ok, err := s.findRepoByExternalKey(ctx, orgID, req.Provider, req.ProviderHost, req.ProviderRepoID, req.FullName); err != nil {
		return nil, err
	} else if ok {
		if err := s.UpdateRepoImportMetadata(ctx, existing.RepoID, req); err != nil {
			return nil, err
		}
		repo, err := s.RescanRepo(ctx, orgID, existing.RepoID)
		if err != nil {
			return nil, err
		}
		return repo, nil
	}

	repo, err := s.CreateRepo(ctx, CreateRepoRequest{
		OrgID:          orgID,
		IntegrationID:  req.IntegrationID,
		Provider:       req.Provider,
		ProviderHost:   req.ProviderHost,
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
	repo, err = s.RescanRepo(ctx, orgID, repo.RepoID)
	if err != nil {
		return nil, err
	}
	return repo, nil
}

func (s *Service) RescanRepo(ctx context.Context, orgID uint64, repoID uuid.UUID) (*RepoRecord, error) {
	repo, err := s.GetRepo(ctx, orgID, repoID)
	if err != nil {
		return nil, err
	}

	releaseScanSlot, err := s.acquireRepoScanSlot(ctx)
	if err != nil {
		return nil, err
	}
	defer releaseScanSlot()

	result, err := scanRepoCompatibility(ctx, repo.CloneURL, repo.DefaultBranch)
	if err != nil {
		summary := map[string]any{
			"issues": []RepoScanIssue{{
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

func (s *Service) acquireRepoScanSlot(ctx context.Context) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	sem := s.repoScanSemaphore()
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	default:
		return nil, ErrRepoScanCapacity
	}
}

func (s *Service) repoScanSemaphore() chan struct{} {
	s.repoScanMu.Lock()
	defer s.repoScanMu.Unlock()
	if s.repoScanSem != nil {
		return s.repoScanSem
	}
	concurrency := s.RepoScanConcurrency
	if concurrency <= 0 {
		concurrency = defaultRepoScanConcurrency
	}
	s.repoScanSem = make(chan struct{}, concurrency)
	return s.repoScanSem
}

func (s *Service) FindRepoByExternalKey(ctx context.Context, orgID uint64, provider, providerHost, providerRepoID, fullName string) (*RepoRecord, bool, error) {
	return s.findRepoByExternalKey(ctx, orgID, provider, providerHost, providerRepoID, fullName)
}

func (s *Service) findRepoByExternalKey(ctx context.Context, orgID uint64, provider, providerHost, providerRepoID, fullName string) (*RepoRecord, bool, error) {
	var (
		query string
		args  []any
	)
	providerHost = strings.TrimSpace(providerHost)
	switch {
	case strings.TrimSpace(providerRepoID) != "":
		query = `
			SELECT
					repo_id,
					org_id,
					COALESCE(integration_id::text, ''),
					provider,
					provider_host,
					provider_repo_id,
				owner,
					name,
					full_name,
					clone_url,
					default_branch,
					state,
					compatibility_status,
					compatibility_summary,
					last_scanned_sha,
					last_error,
					created_at,
				updated_at,
				archived_at
			FROM repos
				WHERE org_id = $1 AND provider = $2 AND provider_host = $3 AND provider_repo_id = $4
			`
		args = []any{int64(orgID), provider, providerHost, providerRepoID}
	default:
		query = `
			SELECT
					repo_id,
					org_id,
					COALESCE(integration_id::text, ''),
					provider,
					provider_host,
					provider_repo_id,
				owner,
					name,
					full_name,
					clone_url,
					default_branch,
					state,
					compatibility_status,
					compatibility_summary,
					last_scanned_sha,
					last_error,
					created_at,
				updated_at,
				archived_at
			FROM repos
				WHERE org_id = $1 AND provider = $2 AND provider_host = $3 AND full_name = $4
			`
		args = []any{int64(orgID), provider, providerHost, fullName}
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
	req.ProviderHost = strings.TrimSpace(strings.ToLower(req.ProviderHost))
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
	if err := validateGitCloneURLField("clone_url", req.CloneURL); err != nil {
		return ImportRepoRequest{}, err
	}
	cloneProviderHost := providerHostFromCloneURL(req.CloneURL)
	if req.ProviderHost == "" {
		req.ProviderHost = cloneProviderHost
	}
	if req.ProviderHost == "" {
		return ImportRepoRequest{}, fmt.Errorf("provider_host is required")
	}
	if cloneProviderHost != "" && req.ProviderHost != cloneProviderHost {
		return ImportRepoRequest{}, fmt.Errorf("provider_host %q must match clone_url host %q", req.ProviderHost, cloneProviderHost)
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

func scanRepoCompatibility(ctx context.Context, repoURL, branch string) (RepoCompatibilityResult, error) {
	repoURL = strings.TrimSpace(repoURL)
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = defaultBranchName
	}
	if err := validateGitCloneURLFieldWithResolver(ctx, net.DefaultResolver, "clone_url", repoURL); err != nil {
		return RepoCompatibilityResult{}, err
	}
	root, err := cloneRepoBranch(ctx, repoURL, branch)
	if err != nil {
		return RepoCompatibilityResult{}, err
	}
	defer os.RemoveAll(root)

	commitSHA, err := gitHeadSHA(ctx, root)
	if err != nil {
		return RepoCompatibilityResult{}, err
	}

	data, err := json.Marshal(map[string]string{"mode": "metadata_only"})
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

func cloneRepoBranch(ctx context.Context, repoURL, branch string) (string, error) {
	tmp, err := os.MkdirTemp("", "forge-metal-repo-scan-*")
	if err != nil {
		return "", fmt.Errorf("create scan dir: %w", err)
	}
	cmd, commandCtx, cancel := repoScanGitCommand(ctx,
		"-c", "protocol.ext.allow=never",
		"-c", "protocol.file.allow=never",
		"-c", "protocol.git.allow=never",
		"-c", "protocol.http.allow=never",
		"-c", "protocol.https.allow=always",
		"-c", "protocol.ssh.allow=never",
		"-c", "http.followRedirects=false",
		"clone",
		"--depth", "1",
		"--single-branch",
		"--branch", branch,
		"--", repoURL, tmp,
	)
	defer cancel()
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(tmp)
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("git clone timed out after %s: %w", repoScanGitCommandTimeout, err)
		}
		return "", fmt.Errorf("git clone --branch %s %s: %s: %w", branch, repoURL, strings.TrimSpace(string(out)), err)
	}
	return tmp, nil
}

func gitHeadSHA(ctx context.Context, repoRoot string) (string, error) {
	cmd, commandCtx, cancel := repoScanGitCommand(ctx, "rev-parse", "HEAD")
	defer cancel()
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("git rev-parse HEAD timed out after %s: %w", repoScanGitCommandTimeout, err)
		}
		return "", fmt.Errorf("git rev-parse HEAD: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func repoScanGitCommand(ctx context.Context, args ...string) (*exec.Cmd, context.Context, context.CancelFunc) {
	commandCtx, cancel := context.WithTimeout(ctx, repoScanGitCommandTimeout)
	cmd := exec.CommandContext(commandCtx, "git", args...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PROTOCOL_FROM_USER=0",
		"GIT_ALLOW_PROTOCOL="+repoScanAllowedProtocols(),
	)
	return cmd, commandCtx, cancel
}

func repoScanAllowedProtocols() string {
	if strings.TrimSpace(os.Getenv(repoScanAllowFileProtocolForE2E)) == "1" {
		return "https:file"
	}
	return "https"
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
