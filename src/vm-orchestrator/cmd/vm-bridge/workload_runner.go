package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/forge-metal/vm-orchestrator/vmproto"
)

const (
	forgejoWorkflowPath = defaultWorkDir + "/.forgejo/workflows/job.yml"
	actRuntimeDir       = defaultWorkDir + "/.forge-metal/act"
)

func (s *agentSession) runWorkload(ctx context.Context, controlCh <-chan vmproto.Envelope, runReq vmproto.RunRequest, env []string) (time.Duration, int, error) {
	switch vmproto.NormalizeWorkloadKind(runReq.WorkloadKind) {
	case vmproto.WorkloadKindDirect:
		return s.runPhase(ctx, controlCh, "run", runReq.RunCommand, normalizeWorkDir(runReq.RunWorkDir), env)
	case vmproto.WorkloadKindForgejoWorkflow:
		argv, workDir, secretEnv, err := prepareForgejoWorkflow(runReq)
		if err != nil {
			s.sendLogString("system", fmt.Sprintf("%s forgejo workflow prepare: %v\n", logPrefix, err))
			return 0, 127, nil
		}
		if len(secretEnv) > 0 {
			env = append(env, secretEnv...)
		}
		return s.runPhase(ctx, controlCh, "run", argv, workDir, env)
	case vmproto.WorkloadKindGitHubRunner:
		argv, workDir, err := prepareGitHubRunner(runReq)
		if err != nil {
			s.sendLogString("system", fmt.Sprintf("%s github runner prepare: %v\n", logPrefix, err))
			return 0, 127, nil
		}
		return s.runPhase(ctx, controlCh, "run", argv, workDir, env)
	default:
		return 0, 127, nil
	}
}

func prepareForgejoWorkflow(runReq vmproto.RunRequest) ([]string, string, []string, error) {
	if strings.TrimSpace(runReq.WorkflowYAML) == "" {
		return nil, "", nil, fmt.Errorf("workflow_yaml is required")
	}
	if err := writeRunnerOwnedFile(forgejoWorkflowPath, []byte(runReq.WorkflowYAML), 0o600); err != nil {
		return nil, "", nil, fmt.Errorf("write workflow: %w", err)
	}

	eventName := strings.TrimSpace(runReq.WorkflowEventName)
	if eventName == "" {
		eventName = "push"
	}
	argv := []string{
		"forgejo-runner",
		"exec",
		"--event",
		eventName,
		"--workflows",
		forgejoWorkflowPath,
		"--no-recurse",
		"--image",
		"-self-hosted",
	}

	if len(runReq.WorkflowEnv) > 0 {
		path := filepath.Join(actRuntimeDir, "env")
		if err := writeKeyValueFile(path, runReq.WorkflowEnv); err != nil {
			return nil, "", nil, fmt.Errorf("write env file: %w", err)
		}
		argv = append(argv, "--env-file", path)
	}
	secretEnv := make([]string, 0, len(runReq.WorkflowSecrets))
	if len(runReq.WorkflowSecrets) > 0 {
		keys := make([]string, 0, len(runReq.WorkflowSecrets))
		for key := range runReq.WorkflowSecrets {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := runReq.WorkflowSecrets[key]
			if err := validateActKeyValue(key, value); err != nil {
				return nil, "", nil, fmt.Errorf("validate secret: %w", err)
			}
			argv = append(argv, "--secret", key)
			secretEnv = append(secretEnv, key+"="+value)
		}
	}

	return argv, defaultWorkDir, secretEnv, nil
}

func prepareGitHubRunner(runReq vmproto.RunRequest) ([]string, string, error) {
	jitConfig := strings.TrimSpace(runReq.GitHubJITConfig)
	if jitConfig == "" {
		return nil, "", fmt.Errorf("github_jit_config is required")
	}
	runnerDir, err := findActionsRunnerDir()
	if err != nil {
		return nil, "", err
	}
	return []string{"./run.sh", "--jitconfig", jitConfig}, runnerDir, nil
}

func findActionsRunnerDir() (string, error) {
	for _, dir := range []string{
		"/opt/actions-runner",
		"/actions-runner",
		"/home/runner/actions-runner",
		filepath.Join(defaultWorkDir, "actions-runner"),
	} {
		info, err := os.Stat(filepath.Join(dir, "run.sh"))
		if err == nil && !info.IsDir() {
			return dir, nil
		}
	}
	return "", fmt.Errorf("actions runner run.sh not found")
}

func writeKeyValueFile(path string, values map[string]string) error {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	for _, key := range keys {
		value := values[key]
		if err := validateActKeyValue(key, value); err != nil {
			return err
		}
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(value)
		builder.WriteByte('\n')
	}
	return writeRunnerOwnedFile(path, []byte(builder.String()), 0o600)
}

func validateActKeyValue(key, value string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("empty key")
	}
	if !isPortableEnvKey(key) {
		return fmt.Errorf("invalid environment key %q", key)
	}
	if strings.ContainsAny(key, "=\r\n") {
		return fmt.Errorf("invalid key %q", key)
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("value for %q contains a newline", key)
	}
	return nil
}

func isPortableEnvKey(key string) bool {
	for i := 0; i < len(key); i++ {
		ch := key[i]
		if ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch == '_' || i > 0 && ch >= '0' && ch <= '9' {
			continue
		}
		return false
	}
	return len(key) > 0
}

func writeRunnerOwnedFile(path string, contents []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := chownRunnerPathAncestors(dir); err != nil {
		return err
	}
	if err := os.WriteFile(path, contents, mode); err != nil {
		return err
	}
	return os.Chown(path, runnerUID, runnerGID)
}

func chownRunnerPathAncestors(dir string) error {
	dir = filepath.Clean(dir)
	base := filepath.Clean(defaultWorkDir)
	rel, err := filepath.Rel(base, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return os.Chown(dir, runnerUID, runnerGID)
	}
	if err := os.Chown(base, runnerUID, runnerGID); err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	current := base
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		if err := os.Chown(current, runnerUID, runnerGID); err != nil {
			return err
		}
	}
	return nil
}
