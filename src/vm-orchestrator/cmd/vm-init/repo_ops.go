package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/forge-metal/vm-orchestrator/vmproto"
)

const repoLockfileHashPath = "/var/lib/forge-metal/repo/lockfile.sha256"

type repoOperationResult struct {
	ExitCode        int
	PrepareDuration time.Duration
	RunDuration     time.Duration
	Manifest        *vmproto.RepoManifest
}

func (s *agentSession) runRepoOperation(ctx context.Context, controlCh <-chan vmproto.Envelope, op *vmproto.RepoOperation, env []string) (repoOperationResult, error) {
	switch strings.TrimSpace(op.Kind) {
	case vmproto.RepoOperationWarm:
		return s.runRepoWarm(ctx, controlCh, op, env)
	case vmproto.RepoOperationExec:
		return s.runRepoExec(ctx, controlCh, op, env)
	default:
		return repoOperationResult{}, fmt.Errorf("unsupported repo operation kind %q", op.Kind)
	}
}

func (s *agentSession) runRepoWarm(ctx context.Context, controlCh <-chan vmproto.Envelope, op *vmproto.RepoOperation, env []string) (repoOperationResult, error) {
	manifest, checkoutExit, err := s.runRepoCheckoutPhase(ctx, "repo_checkout", op, env, true)
	if err != nil || checkoutExit != 0 {
		return repoOperationResult{ExitCode: checkoutExit, Manifest: manifest}, err
	}

	userEnv := repoUserEnv(env)
	prepareDuration, exitCode, err := s.runRunnerPhase(ctx, controlCh, "prepare", op.UserPrepareCommand, normalizeWorkDir(defaultString(op.UserPrepareWorkDir, defaultWorkDir)), userEnv)
	if err != nil {
		return repoOperationResult{ExitCode: exitCode, PrepareDuration: prepareDuration, Manifest: manifest}, err
	}

	var runDuration time.Duration
	if exitCode == 0 {
		runDuration, exitCode, err = s.runRunnerPhase(ctx, controlCh, "run", op.UserRunCommand, normalizeWorkDir(defaultString(op.UserRunWorkDir, defaultWorkDir)), userEnv)
		if err != nil {
			return repoOperationResult{ExitCode: exitCode, PrepareDuration: prepareDuration, RunDuration: runDuration, Manifest: manifest}, err
		}
	}

	if exitCode == 0 {
		lockfileSHA, err := persistWarmLockfileHash(ctx, op.LockfileRelPath, userEnv)
		if err != nil {
			s.sendLogString("stderr", fmt.Sprintf("[init] repo warm manifest error: %v\n", err))
			return repoOperationResult{ExitCode: 1, PrepareDuration: prepareDuration, RunDuration: runDuration, Manifest: manifest}, nil
		}
		manifest.LockfileSHA256 = lockfileSHA
	}

	return repoOperationResult{
		ExitCode:        exitCode,
		PrepareDuration: prepareDuration,
		RunDuration:     runDuration,
		Manifest:        manifest,
	}, nil
}

func (s *agentSession) runRepoExec(ctx context.Context, controlCh <-chan vmproto.Envelope, op *vmproto.RepoOperation, env []string) (repoOperationResult, error) {
	manifest, checkoutExit, err := s.runRepoCheckoutPhase(ctx, "repo_checkout", op, env, false)
	if err != nil || checkoutExit != 0 {
		return repoOperationResult{ExitCode: checkoutExit, Manifest: manifest}, err
	}

	userEnv := repoUserEnv(env)
	installNeeded, currentLockfileSHA, previousLockfileSHA, err := repoExecInstallDecision(ctx, op.LockfileRelPath, userEnv)
	if err != nil {
		s.sendLogString("stderr", fmt.Sprintf("[init] repo exec lockfile decision error: %v\n", err))
		return repoOperationResult{ExitCode: 1, Manifest: manifest}, nil
	}
	manifest.InstallNeeded = installNeeded
	manifest.LockfileSHA256 = currentLockfileSHA
	manifest.PreviousLockfileSHA256 = previousLockfileSHA

	var (
		prepareDuration time.Duration
		exitCode        int
	)
	if installNeeded {
		prepareDuration, exitCode, err = s.runRunnerPhase(ctx, controlCh, "prepare", op.UserPrepareCommand, normalizeWorkDir(defaultString(op.UserPrepareWorkDir, defaultWorkDir)), userEnv)
		if err != nil {
			return repoOperationResult{ExitCode: exitCode, PrepareDuration: prepareDuration, Manifest: manifest}, err
		}
	}

	var runDuration time.Duration
	if exitCode == 0 {
		runDuration, exitCode, err = s.runRunnerPhase(ctx, controlCh, "run", op.UserRunCommand, normalizeWorkDir(defaultString(op.UserRunWorkDir, defaultWorkDir)), userEnv)
		if err != nil {
			return repoOperationResult{ExitCode: exitCode, PrepareDuration: prepareDuration, RunDuration: runDuration, Manifest: manifest}, err
		}
	}

	return repoOperationResult{
		ExitCode:        exitCode,
		PrepareDuration: prepareDuration,
		RunDuration:     runDuration,
		Manifest:        manifest,
	}, nil
}

func (s *agentSession) runRepoCheckoutPhase(ctx context.Context, phase string, op *vmproto.RepoOperation, env []string, resetWorkspace bool) (*vmproto.RepoManifest, int, error) {
	start := time.Now()
	if err := s.sendControl(vmproto.TypePhaseStart, vmproto.PhaseStart{Name: phase}); err != nil {
		return nil, 0, err
	}

	manifest, exitCode, err := s.runRepoCheckout(ctx, op, env, resetWorkspace)
	duration := time.Since(start)
	if phaseErr := s.sendControl(vmproto.TypePhaseEnd, vmproto.PhaseEnd{
		Name:       phase,
		ExitCode:   exitCode,
		DurationMS: duration.Milliseconds(),
	}); phaseErr != nil {
		return manifest, exitCode, phaseErr
	}
	return manifest, exitCode, err
}

func (s *agentSession) runRepoCheckout(ctx context.Context, op *vmproto.RepoOperation, env []string, resetWorkspace bool) (*vmproto.RepoManifest, int, error) {
	if strings.TrimSpace(op.RepoURL) == "" {
		return nil, 1, nil
	}
	if strings.TrimSpace(op.OriginURL) == "" {
		return nil, 1, nil
	}
	if strings.TrimSpace(op.Ref) == "" {
		return nil, 1, nil
	}

	if err := prepareWorkspace(resetWorkspace); err != nil {
		s.sendLogString("stderr", fmt.Sprintf("[init] repo workspace setup failed: %v\n", err))
		return nil, 1, nil
	}

	runnerEnv := append(repoUserEnv(env),
		"REPO_URL="+op.RepoURL,
		"REPO_ORIGIN_URL="+op.OriginURL,
	)
	commands := [][]string{
		{"git", "init"},
		{"git", "remote", "remove", "origin"},
		{"git", "remote", "add", "origin", op.OriginURL},
		{"git", "fetch", "--depth", "1", op.RepoURL, op.Ref},
		{"git", "checkout", "--force", "FETCH_HEAD"},
		{"rm", "-f", ".git/FETCH_HEAD"},
	}
	for _, argv := range commands {
		if _, err := runRepoSupervisorCommand(ctx, argv, defaultWorkDir, runnerEnv); err != nil {
			if len(argv) >= 3 && argv[1] == "remote" && argv[2] == "remove" {
				continue
			}
			s.sendLogString("stderr", fmt.Sprintf("[init] repo command failed %q: %v\n", strings.Join(argv, " "), err))
			return nil, 1, nil
		}
	}

	commitSHA, err := runRepoSupervisorCommand(ctx, []string{"git", "rev-parse", "HEAD"}, defaultWorkDir, runnerEnv)
	if err != nil {
		s.sendLogString("stderr", fmt.Sprintf("[init] repo commit resolve failed: %v\n", err))
		return nil, 1, nil
	}

	return &vmproto.RepoManifest{
		Kind:              op.Kind,
		RequestedRef:      op.Ref,
		ResolvedCommitSHA: strings.TrimSpace(commitSHA),
		LockfileRelPath:   strings.TrimSpace(op.LockfileRelPath),
		InstallNeeded:     op.Kind == vmproto.RepoOperationWarm,
	}, 0, nil
}

func (s *agentSession) runRunnerPhase(ctx context.Context, controlCh <-chan vmproto.Envelope, label string, argv []string, workDir string, env []string) (time.Duration, int, error) {
	if len(argv) == 0 {
		return 0, 0, nil
	}
	spec, err := phaseCommandWithCredential(argv, workDir, env, runnerCredential())
	if err != nil {
		s.sendLogString("system", fmt.Sprintf("[init] %s resolve command: %v\n", label, err))
		return 0, 127, nil
	}
	return s.runCommand(ctx, label, spec, controlCh)
}

func runRepoSupervisorCommand(ctx context.Context, argv []string, workDir string, env []string) (string, error) {
	if len(argv) == 0 {
		return "", fmt.Errorf("empty repo supervisor command")
	}
	path, err := resolveCommand(argv[0])
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, path, argv[1:]...)
	cmd.Dir = workDir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: runnerCredential()}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %s: %w", strings.Join(argv, " "), strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}

func prepareWorkspace(reset bool) error {
	if reset {
		if err := os.RemoveAll(defaultWorkDir); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(defaultWorkDir, 0o755); err != nil {
		return err
	}
	return os.Chown(defaultWorkDir, runnerUID, runnerGID)
}

func persistWarmLockfileHash(ctx context.Context, lockfileRelPath string, env []string) (string, error) {
	lockfileRelPath = strings.TrimSpace(lockfileRelPath)
	if lockfileRelPath == "" {
		return "", nil
	}
	lockfileSHA, err := repoLockfileSHA(ctx, lockfileRelPath, env)
	if err != nil {
		return "", err
	}
	// Keep warm metadata outside the checkout so a later git checkout cannot forge the install decision.
	if err := os.MkdirAll(filepath.Dir(repoLockfileHashPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(repoLockfileHashPath, []byte(lockfileSHA+"\n"), 0o644); err != nil {
		return "", err
	}
	return lockfileSHA, nil
}

func repoExecInstallDecision(ctx context.Context, lockfileRelPath string, env []string) (bool, string, string, error) {
	lockfileRelPath = strings.TrimSpace(lockfileRelPath)
	if lockfileRelPath == "" {
		return true, "", "", nil
	}
	currentSHA, currentErr := repoLockfileSHA(ctx, lockfileRelPath, env)
	if currentErr != nil {
		if os.IsNotExist(currentErr) {
			return true, "", "", nil
		}
		return true, "", "", currentErr
	}
	data, err := os.ReadFile(repoLockfileHashPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, currentSHA, "", nil
		}
		return true, currentSHA, "", err
	}
	previousSHA := strings.TrimSpace(string(data))
	if !isSHA256Hex(previousSHA) {
		return true, currentSHA, previousSHA, fmt.Errorf("recorded lockfile hash is invalid")
	}
	return currentSHA != previousSHA, currentSHA, previousSHA, nil
}

func repoLockfileSHA(ctx context.Context, lockfileRelPath string, env []string) (string, error) {
	clean, err := workspaceRelPath(lockfileRelPath)
	if err != nil {
		return "", err
	}
	absPath, err := workspaceFilePath(clean)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(absPath)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("lockfile %s is not a regular file", clean)
	}
	shaCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := runRepoSupervisorCommand(shaCtx, []string{"sha256sum", "--", clean}, defaultWorkDir, env)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(out)
	if len(fields) == 0 || len(fields[0]) != sha256.Size*2 {
		return "", fmt.Errorf("unexpected sha256sum output for %s", clean)
	}
	if !isSHA256Hex(fields[0]) {
		return "", fmt.Errorf("unexpected sha256sum digest for %s", clean)
	}
	return fields[0], nil
}

func workspaceFilePath(relPath string) (string, error) {
	clean, err := workspaceRelPath(relPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(defaultWorkDir, clean), nil
}

func workspaceRelPath(relPath string) (string, error) {
	clean := filepath.Clean(strings.TrimSpace(relPath))
	if clean == "." || clean == "" {
		return "", fmt.Errorf("workspace path is empty")
	}
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workspace path %q escapes /workspace", relPath)
	}
	return filepath.ToSlash(clean), nil
}

func repoUserEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, guestEventFIFOEnv+"=") {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func isSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, ch := range value {
		switch {
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return false
		}
	}
	return true
}

func runnerCredential() *syscall.Credential {
	return &syscall.Credential{Uid: runnerUID, Gid: runnerGID}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
