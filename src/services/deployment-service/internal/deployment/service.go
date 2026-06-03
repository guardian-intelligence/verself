package deployment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/verself/deployment-service/deployengine"
	"github.com/verself/deployment-service/deployengine/artifactpublish"
	objectstorageclient "github.com/verself/object-storage-service/client"
)

var gitSHARegex = regexp.MustCompile(`^[0-9a-f]{40}$`)

const dependencyProbeDeadline = 900 * time.Millisecond
const deploymentStateUpdateRetryDeadline = 30 * time.Second
const deploymentStateUpdateAttemptTimeout = 5 * time.Second
const deploymentStateUpdateRetryInterval = 500 * time.Millisecond

type Config struct {
	Site              string
	RepoRoot          string
	ObjectStorageAddr string
	NomadAddr         string
	NomadAllocID      string
	RecoverySSHReady  string
	BazelJobs         int
}

type Service struct {
	Store                   Store
	Config                  Config
	ObjectStorageHTTPClient *http.Client

	mu      sync.Mutex
	running bool
}

type dependencyProbe struct {
	Stage string
	Name  string
	Run   func(context.Context) DependencyCheck
}

type dependencyProbeResult struct {
	Index int
	Check DependencyCheck
}

func (s *Service) Ready(ctx context.Context) error {
	checks := s.DependencyChecks(ctx)
	for _, check := range checks {
		if !check.OK {
			return coded(check.Code, fmt.Errorf("%w: %s/%s: %s", ErrUnavailable, check.Stage, check.Name, check.Detail))
		}
	}
	return nil
}

func (s *Service) DependencyChecks(ctx context.Context) []DependencyCheck {
	probes := []dependencyProbe{
		{Stage: "S0", Name: "site", Run: func(context.Context) DependencyCheck {
			return s0("site", s.Config.Site != "", "VERSELF_SITE is required")
		}},
		{Stage: "S1", Name: "host_allocated", Run: func(context.Context) DependencyCheck { return s1HostAllocated(s.Config.NomadAllocID) }},
		{Stage: "S2", Name: "recovery_ssh_ready", Run: func(context.Context) DependencyCheck { return s2RecoverySSHReady(s.Config.RecoverySSHReady) }},
		{Stage: "S3", Name: "bazelisk", Run: func(context.Context) DependencyCheck { return s3Bazelisk() }},
		{Stage: "S3", Name: "git", Run: func(context.Context) DependencyCheck { return s3Git() }},
		{Stage: "S6", Name: "nomad", Run: func(ctx context.Context) DependencyCheck { return s6Nomad(ctx, s.Config.NomadAddr) }},
		{Stage: "S7", Name: "postgres", Run: func(ctx context.Context) DependencyCheck { return s7Postgres(ctx, s.Store) }},
		{Stage: "S7", Name: "repo_root", Run: func(ctx context.Context) DependencyCheck { return s7RepoRoot(ctx, s.Config.RepoRoot) }},
		{Stage: "S7", Name: "object_storage", Run: func(ctx context.Context) DependencyCheck {
			return s7ObjectStorage(ctx, s.Config.ObjectStorageAddr, s.ObjectStorageHTTPClient)
		}},
	}
	return runDependencyProbes(ctx, probes, dependencyProbeDeadline)
}

func runDependencyProbes(ctx context.Context, probes []dependencyProbe, timeout time.Duration) []DependencyCheck {
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	checks := make([]DependencyCheck, len(probes))
	results := make(chan dependencyProbeResult, len(probes))
	for i, probe := range probes {
		i, probe := i, probe
		go func() {
			results <- dependencyProbeResult{Index: i, Check: probe.Run(checkCtx)}
		}()
	}
	pending := len(probes)
	for pending > 0 {
		select {
		case result := <-results:
			checks[result.Index] = result.Check
			pending--
		case <-checkCtx.Done():
			for i, probe := range probes {
				if checks[i].Stage == "" {
					checks[i] = dependency(probe.Stage, probe.Name, checkCtx.Err())
				}
			}
			return checks
		}
	}
	return checks
}

func (s *Service) Submit(ctx context.Context, req SubmitRequest, source Source) (Record, error) {
	if err := s.validateRequest(req, source); err != nil {
		return Record{}, err
	}
	if record, ok, err := s.findExistingDeployment(ctx, req, source); err != nil {
		return Record{}, err
	} else if ok {
		return record, nil
	}
	if err := s.Ready(ctx); err != nil {
		return Record{}, err
	}
	if !s.acquire() {
		if record, ok, err := s.findExistingDeployment(ctx, req, source); err != nil {
			return Record{}, err
		} else if ok {
			return record, nil
		}
		return Record{}, ErrBusy
	}
	deploymentID, err := randomPrefixedID("dep")
	if err != nil {
		s.release()
		return Record{}, err
	}
	deployRunKey, err := randomPrefixedID("deploy")
	if err != nil {
		s.release()
		return Record{}, err
	}
	record := Record{
		DeploymentID:       deploymentID,
		Site:               req.Site,
		SHA:                strings.ToLower(req.SHA),
		DeployRunKey:       deployRunKey,
		SourceKind:         source.Kind,
		SourceSubject:      source.Subject,
		Repository:         repositoryForRecord(source),
		Ref:                source.Ref,
		Workflow:           source.Workflow,
		JobWorkflowRef:     source.JobWorkflowRef,
		ProviderRunID:      source.RunID,
		ProviderRunAttempt: source.RunAttempt,
		State:              StateQueued,
	}
	if err := s.Store.InsertQueued(ctx, record); err != nil {
		s.release()
		if errors.Is(err, ErrDuplicate) {
			if existing, ok, findErr := s.findExistingDeployment(ctx, req, source); findErr != nil {
				return Record{}, findErr
			} else if ok {
				return existing, nil
			}
		}
		return Record{}, err
	}
	go s.runDeployment(context.Background(), record)
	return record, nil
}

func (s *Service) runDeployment(ctx context.Context, record Record) {
	defer s.release()
	if !s.updateDeploymentStateWithRetry(ctx, record.DeploymentID, StateRunning, func(attemptCtx context.Context) error {
		return s.Store.MarkRunning(attemptCtx, record.DeploymentID)
	}) {
		return
	}
	if err := CheckoutSourceSHA(ctx, s.Config.RepoRoot, record.SHA); err != nil {
		_ = s.updateDeploymentStateWithRetry(ctx, record.DeploymentID, StateFailed, func(attemptCtx context.Context) error {
			return s.Store.MarkFailed(attemptCtx, record.DeploymentID, err)
		})
		return
	}
	labels, err := deployengine.QueryNomadComponentLabels(ctx, s.Config.RepoRoot)
	if err != nil {
		_ = s.updateDeploymentStateWithRetry(ctx, record.DeploymentID, StateFailed, func(attemptCtx context.Context) error {
			return s.Store.MarkFailed(attemptCtx, record.DeploymentID, err)
		})
		return
	}
	descriptors, err := deployengine.BuildNomadComponentDescriptors(ctx, s.Config.RepoRoot, labels, bazelBuildFlags(s.Config.BazelJobs)...)
	if err != nil {
		_ = s.updateDeploymentStateWithRetry(ctx, record.DeploymentID, StateFailed, func(attemptCtx context.Context) error {
			return s.Store.MarkFailed(attemptCtx, record.DeploymentID, err)
		})
		return
	}
	clientOptions := []objectstorageclient.ClientOption{}
	if s.ObjectStorageHTTPClient != nil {
		clientOptions = append(clientOptions, objectstorageclient.WithHTTPClient(s.ObjectStorageHTTPClient))
	}
	objectStorageClient, err := objectstorageclient.NewClient(s.Config.ObjectStorageAddr, clientOptions...)
	if err != nil {
		_ = s.updateDeploymentStateWithRetry(ctx, record.DeploymentID, StateFailed, func(attemptCtx context.Context) error {
			return s.Store.MarkFailed(attemptCtx, record.DeploymentID, err)
		})
		return
	}
	result, err := deployengine.Submit(ctx, deployengine.Options{
		Site:                      record.Site,
		ArtifactNamespace:         record.SHA,
		DeployRunKey:              record.DeployRunKey,
		RepoRoot:                  s.Config.RepoRoot,
		NomadComponentDescriptors: descriptors,
		ArtifactPublisher:         artifactpublish.Publisher{Client: objectStorageClient},
		NomadAddr:                 s.Config.NomadAddr,
	})
	if err != nil {
		_ = s.updateDeploymentStateWithRetry(ctx, record.DeploymentID, StateFailed, func(attemptCtx context.Context) error {
			return s.Store.MarkFailed(attemptCtx, record.DeploymentID, err)
		})
		return
	}
	_ = s.updateDeploymentStateWithRetry(ctx, record.DeploymentID, StateSucceeded, func(attemptCtx context.Context) error {
		return s.Store.MarkSucceeded(attemptCtx, record.DeploymentID, DeploymentResult{
			NomadSubmittedJobs: result.NomadSubmittedJobs,
		})
	})
}

func (s *Service) updateDeploymentStateWithRetry(ctx context.Context, deploymentID string, state string, update func(context.Context) error) bool {
	deadline := time.Now().Add(deploymentStateUpdateRetryDeadline)
	var attempts uint32
	for {
		attemptCtx, cancel := context.WithTimeout(ctx, deploymentStateUpdateAttemptTimeout)
		err := update(attemptCtx)
		cancel()
		if err == nil {
			if attempts > 0 {
				slog.InfoContext(ctx, "deployment state update recovered", "deployment_id", deploymentID, "state", state, "attempts", attempts+1)
			}
			return true
		}
		attempts++
		if attempts == 1 {
			slog.WarnContext(ctx, "deployment state update failed; retrying", "deployment_id", deploymentID, "state", state, "error", err)
		}
		if time.Now().After(deadline) {
			slog.ErrorContext(ctx, "deployment state update failed permanently", "deployment_id", deploymentID, "state", state, "attempts", attempts, "error", err)
			return false
		}
		select {
		case <-ctx.Done():
			slog.ErrorContext(ctx, "deployment state update canceled", "deployment_id", deploymentID, "state", state, "attempts", attempts, "error", ctx.Err())
			return false
		case <-time.After(deploymentStateUpdateRetryInterval):
		}
	}
}

func (s *Service) validateRequest(req SubmitRequest, source Source) error {
	if strings.TrimSpace(req.Site) == "" {
		return coded("deployment.site_required", ErrInvalid)
	}
	if req.Site != s.Config.Site {
		return coded("deployment.site_mismatch", fmt.Errorf("%w: request site %q does not match service site %q", ErrInvalid, req.Site, s.Config.Site))
	}
	if !gitSHARegex.MatchString(strings.ToLower(strings.TrimSpace(req.SHA))) {
		return coded("deployment.sha_invalid", fmt.Errorf("%w: sha must be a 40-character lowercase git sha", ErrInvalid))
	}
	if source.Kind == "" || source.Subject == "" {
		return coded("deployment.source_required", ErrUnauthorized)
	}
	if source.SHA != "" && !strings.EqualFold(source.SHA, req.SHA) {
		return coded("deployment.source_sha_mismatch", fmt.Errorf("%w: source sha does not match request sha", ErrUnauthorized))
	}
	if source.Kind == SourceGitHubActionsOIDC {
		if strings.TrimSpace(source.Repository) == "" {
			return coded("deployment.source_repository_missing", fmt.Errorf("%w: github oidc repository claim is required", ErrUnauthorized))
		}
		if strings.TrimSpace(source.Ref) == "" {
			return coded("deployment.source_ref_missing", fmt.Errorf("%w: github oidc ref claim is required", ErrUnauthorized))
		}
		if strings.TrimSpace(source.SHA) == "" {
			return coded("deployment.source_sha_missing", fmt.Errorf("%w: github oidc sha claim is required", ErrUnauthorized))
		}
		if strings.TrimSpace(source.RunID) == "" {
			return coded("deployment.source_run_id_missing", fmt.Errorf("%w: github oidc run_id claim is required", ErrUnauthorized))
		}
		if source.RunAttempt == 0 {
			return coded("deployment.source_run_attempt_missing", fmt.Errorf("%w: github oidc run_attempt claim is required", ErrUnauthorized))
		}
	}
	return nil
}

func (s *Service) findExistingDeployment(ctx context.Context, req SubmitRequest, source Source) (Record, bool, error) {
	if !sourceHasIdempotencyKey(source) {
		return Record{}, false, nil
	}
	record, ok, err := s.Store.FindBySourceAttempt(ctx, req.Site, source)
	if err != nil {
		return Record{}, false, err
	}
	if !ok || !strings.EqualFold(record.SHA, req.SHA) {
		if ok {
			return Record{}, false, coded("deployment.source_attempt_sha_mismatch", fmt.Errorf("%w: deployment source attempt already exists for sha %q", ErrDuplicate, record.SHA))
		}
		return Record{}, false, nil
	}
	return record, true, nil
}

func sourceHasIdempotencyKey(source Source) bool {
	return source.Kind == SourceGitHubActionsOIDC &&
		strings.TrimSpace(source.Repository) != "" &&
		strings.TrimSpace(source.RunID) != "" &&
		source.RunAttempt != 0
}

func bazelBuildFlags(jobs int) []string {
	return []string{fmt.Sprintf("--jobs=%d", jobs)}
}

func (s *Service) acquire() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	return true
}

func (s *Service) release() {
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
}

func randomPrefixedID(prefix string) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("random deployment id: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(raw[:]), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func repositoryForRecord(source Source) string {
	return strings.TrimSpace(source.Repository)
}

func s0(name string, ok bool, detail string) DependencyCheck {
	if ok {
		return dependencyOK("S0", name)
	}
	return dependencyFailed("S0", name, detail)
}

func s1HostAllocated(nomadAllocID string) DependencyCheck {
	if strings.TrimSpace(nomadAllocID) != "" {
		return dependencyOK("S1", "host_allocated")
	}
	return dependencyFailed("S1", "host_allocated", "deployment-service must run inside a Nomad allocation; NOMAD_ALLOC_ID is required")
}

func s2RecoverySSHReady(value string) DependencyCheck {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes":
		return dependencyOK("S2", "recovery_ssh_ready")
	default:
		return dependencyFailed("S2", "recovery_ssh_ready", "host bootstrap must declare VERSELF_RECOVERY_SSH_READY=true after recovery access handoff")
	}
}

func s7Postgres(ctx context.Context, store Store) DependencyCheck {
	checkCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	err := store.Ready(checkCtx)
	return dependency("S7", "postgres", err)
}

func s7RepoRoot(ctx context.Context, repoRoot string) DependencyCheck {
	if strings.TrimSpace(repoRoot) == "" {
		return dependencyFailed("S7", "repo_root", "VERSELF_DEPLOY_REPO_ROOT is required")
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".git")); err != nil {
		return dependencyFailed("S7", "repo_root", err.Error())
	}
	if err := runQuickCommand(ctx, repoRoot, "git", "rev-parse", "--is-inside-work-tree"); err != nil {
		return dependencyFailed("S7", "repo_root", "git worktree check failed: "+err.Error())
	}
	if err := runQuickCommand(ctx, repoRoot, "git", "remote", "get-url", "origin"); err != nil {
		return dependencyFailed("S7", "repo_root", "git origin remote is required: "+err.Error())
	}
	return dependencyOK("S7", "repo_root")
}

func runQuickCommand(parent context.Context, dir, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(parent, 200*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0")
	err := cmd.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func s3Bazelisk() DependencyCheck {
	_, err := exec.LookPath("bazelisk")
	return dependency("S3", "bazelisk", err)
}

func s3Git() DependencyCheck {
	_, err := exec.LookPath("git")
	return dependency("S3", "git", err)
}

func s6Nomad(ctx context.Context, addr string) DependencyCheck {
	if strings.TrimSpace(addr) == "" {
		return dependencyFailed("S6", "nomad", "VERSELF_NOMAD_ADDR is required")
	}
	reqCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, strings.TrimRight(addr, "/")+"/v1/status/leader", http.NoBody)
	if err != nil {
		return dependency("S6", "nomad", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return dependency("S6", "nomad", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return dependency("S6", "nomad", fmt.Errorf("status %d", resp.StatusCode))
	}
	return dependency("S6", "nomad", nil)
}

func s7ObjectStorage(ctx context.Context, addr string, client *http.Client) DependencyCheck {
	if strings.TrimSpace(addr) == "" {
		return dependencyFailed("S7", "object_storage", "VERSELF_OBJECT_STORAGE_ADDR is required")
	}
	if client == nil {
		client = http.DefaultClient
	}
	reqCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, strings.TrimRight(addr, "/")+"/healthz", http.NoBody)
	if err != nil {
		return dependency("S7", "object_storage", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return dependency("S7", "object_storage", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return dependency("S7", "object_storage", fmt.Errorf("status %d", resp.StatusCode))
	}
	return dependency("S7", "object_storage", nil)
}

func dependency(stage, name string, err error) DependencyCheck {
	if err == nil {
		return dependencyOK(stage, name)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return dependencyFailed(stage, name, "deadline exceeded")
	}
	return dependencyFailed(stage, name, err.Error())
}

func dependencyOK(stage, name string) DependencyCheck {
	return DependencyCheck{Stage: stage, Name: name, OK: true, Code: "ok", Detail: "ok"}
}

func dependencyFailed(stage, name string, detail string) DependencyCheck {
	return DependencyCheck{Stage: stage, Name: name, OK: false, Code: dependencyCode(stage, name), Detail: detail}
}

func dependencyCode(stage, name string) string {
	return "deployment.bootstrap." + strings.ToLower(stage) + "." + name
}
