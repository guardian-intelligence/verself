package deployment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/verself/deployment-service/deployengine"
)

var gitSHARegex = regexp.MustCompile(`^[0-9a-f]{40}$`)

const dependencyProbeDeadline = 900 * time.Millisecond

type Config struct {
	Site                        string
	RepoRoot                    string
	R2ControlPlaneToken         string
	SubstrateControlPlaneMarker string
	R2ControlPlaneAddr          string
	NomadAddr                   string
	NomadAllocID                string
	RecoverySSHReady            string
	BazelJobs                   int
}

type Service struct {
	Store  Store
	Config Config

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
		{Stage: "S4", Name: "openbao_runtime_secret_delivery", Run: func(context.Context) DependencyCheck { return s4OpenBaoToken(s.Config.R2ControlPlaneToken) }},
		{Stage: "S5", Name: "substrate_control_plane_applied", Run: func(context.Context) DependencyCheck {
			return s5SubstrateControlPlane(s.Config.SubstrateControlPlaneMarker)
		}},
		{Stage: "S6", Name: "nomad", Run: func(ctx context.Context) DependencyCheck { return s6Nomad(ctx, s.Config.NomadAddr) }},
		{Stage: "S7", Name: "postgres", Run: func(ctx context.Context) DependencyCheck { return s7Postgres(ctx, s.Store) }},
		{Stage: "S7", Name: "repo_root", Run: func(ctx context.Context) DependencyCheck { return s7RepoRoot(ctx, s.Config.RepoRoot) }},
		{Stage: "S7", Name: "r2_control_plane", Run: func(ctx context.Context) DependencyCheck { return s7R2ControlPlane(ctx, s.Config.R2ControlPlaneAddr) }},
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
	if err := s.Store.MarkRunning(ctx, record.DeploymentID); err != nil {
		return
	}
	result, err := deployengine.Run(ctx, deployengine.Options{
		Site:                record.Site,
		SHA:                 record.SHA,
		DeployRunKey:        record.DeployRunKey,
		RepoRoot:            s.Config.RepoRoot,
		R2ControlPlaneToken: s.Config.R2ControlPlaneToken,
		R2ControlPlaneAddr:  s.Config.R2ControlPlaneAddr,
		NomadAddr:           s.Config.NomadAddr,
		BazelBuildFlags:     bazelBuildFlags(s.Config.BazelJobs),
	})
	if err != nil {
		_ = s.Store.MarkFailed(ctx, record.DeploymentID, err)
		return
	}
	_ = s.Store.MarkSucceeded(ctx, record.DeploymentID, DeploymentResult{
		ControlPlaneBundleSHA256: result.ControlPlaneSHA256,
		NomadSubmittedJobs:       result.NomadSubmittedJobs,
		NomadDispatchedJobs:      result.NomadDispatchedJobs,
	})
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

func s4OpenBaoToken(token string) DependencyCheck {
	if strings.TrimSpace(token) != "" {
		return dependencyOK("S4", "openbao_runtime_secret_delivery")
	}
	return dependencyFailed("S4", "openbao_runtime_secret_delivery", "deployment-service.r2_control_plane_token was not delivered")
}

func s5SubstrateControlPlane(token string) DependencyCheck {
	if strings.TrimSpace(token) != "" {
		return dependencyOK("S5", "substrate_control_plane_applied")
	}
	return dependencyFailed("S5", "substrate_control_plane_applied", "substrate-control-plane apply marker was not delivered from OpenBao")
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

func s7R2ControlPlane(ctx context.Context, addr string) DependencyCheck {
	if strings.TrimSpace(addr) == "" {
		return dependencyFailed("S7", "r2_control_plane", "VERSELF_R2_CONTROL_PLANE_ADDR is required")
	}
	reqCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, strings.TrimRight(addr, "/")+"/healthz", http.NoBody)
	if err != nil {
		return dependency("S7", "r2_control_plane", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return dependency("S7", "r2_control_plane", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return dependency("S7", "r2_control_plane", fmt.Errorf("status %d", resp.StatusCode))
	}
	return dependency("S7", "r2_control_plane", nil)
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
