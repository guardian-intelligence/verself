package jobs

import (
	"context"
	"fmt"
	"strings"
	"time"

	vmorchestrator "github.com/forge-metal/vm-orchestrator"
	"github.com/google/uuid"
)

func (s *Service) runForgejoRunner(ctx context.Context, executionID, attemptID uuid.UUID, req SubmitRequest) (executionOutcome, error) {
	if err := s.validateForgejoRunnerConfig(); err != nil {
		return executionOutcome{FailureReason: "forgejo_runner_config_invalid"}, err
	}

	runnerName := forgejoRunnerName(attemptID)
	if err := s.updateAttemptRunnerName(ctx, attemptID, runnerName); err != nil {
		return executionOutcome{FailureReason: "persist_runner_name_failed"}, err
	}

	startedAt := time.Now().UTC()
	if err := s.markRunning(ctx, executionID, attemptID, startedAt); err != nil {
		return executionOutcome{FailureReason: "mark_running_failed"}, err
	}
	s.writeSystemLog(ctx, executionID, attemptID, "running forgejo runner runner_name=%s provider_run_id=%s provider_job_id=%s", runnerName, req.ProviderRunID, req.ProviderJobID)

	cfg, err := forgejoRunnerConfigFromSnapshot(req.GoldenSnapshotRef)
	if err != nil {
		return executionOutcome{FailureReason: "forgejo_runner_snapshot_invalid"}, err
	}

	job := vmorchestrator.JobConfig{
		JobID:      attemptID.String(),
		RunCommand: []string{"sh", "-c", forgejoRunnerScript()},
		RunWorkDir: "/workspace",
		Env: map[string]string{
			"CI":                                 "true",
			"FORGEJO_INSTANCE_URL":               strings.TrimSpace(s.ForgejoURL),
			"FORGEJO_RUNNER_LABEL":               strings.TrimSpace(s.ForgejoRunnerLabel),
			"FORGEJO_RUNNER_NAME":                runnerName,
			"FORGEJO_RUNNER_REGISTRATION_TOKEN":  strings.TrimSpace(s.ForgejoRunnerToken),
			"FORGEJO_RUNNER_BINARY_URL":          strings.TrimSpace(s.ForgejoRunnerBinaryURL),
			"FORGEJO_RUNNER_BINARY_SHA256":       strings.TrimSpace(s.ForgejoRunnerBinarySHA256),
			"FORGEJO_RUNNER_JOB_TIMEOUT_SECONDS": fmt.Sprintf("%d", maxAttemptRunSeconds),
			"FORGE_METAL_PROVIDER_RUN_ID":        strings.TrimSpace(req.ProviderRunID),
			"FORGE_METAL_PROVIDER_JOB_ID":        strings.TrimSpace(req.ProviderJobID),
			"FORGE_METAL_WORKFLOW_JOB_NAME":      strings.TrimSpace(req.WorkflowJobName),
		},
	}

	result, err := s.Orchestrator.RunWithConfig(ctx, cfg, job)
	outcome := executionOutcome{
		StartedAt:      startedAt,
		CompletedAt:    time.Now().UTC(),
		Logs:           result.Logs,
		ExitCode:       result.ExitCode,
		RunnerName:     runnerName,
		ZFSWritten:     result.ZFSWritten,
		StdoutBytes:    result.StdoutBytes,
		StderrBytes:    result.StderrBytes,
		GoldenSnapshot: strings.TrimSpace(req.GoldenSnapshotRef),
	}
	outcome.DurationMs = outcome.CompletedAt.Sub(startedAt).Milliseconds()
	if err != nil {
		outcome.State = StateFailed
		outcome.FailureReason = failureReasonFromError(err)
		return outcome, err
	}
	if result.ExitCode != 0 {
		outcome.State = StateFailed
		outcome.FailureReason = "forgejo_runner_failed"
	}
	return outcome, nil
}

func (s *Service) validateForgejoRunnerConfig() error {
	switch {
	case strings.TrimSpace(s.ForgejoURL) == "":
		return fmt.Errorf("forgejo url is required for forgejo_runner executions")
	case strings.TrimSpace(s.ForgejoRunnerLabel) == "":
		return fmt.Errorf("forgejo runner label is required for forgejo_runner executions")
	case strings.TrimSpace(s.ForgejoRunnerToken) == "":
		return fmt.Errorf("forgejo runner token is required for forgejo_runner executions")
	case strings.TrimSpace(s.ForgejoRunnerBinaryURL) == "":
		return fmt.Errorf("forgejo runner binary url is required for forgejo_runner executions")
	case strings.TrimSpace(s.ForgejoRunnerBinarySHA256) == "":
		return fmt.Errorf("forgejo runner binary sha256 is required for forgejo_runner executions")
	default:
		return nil
	}
}

func (s *Service) updateAttemptRunnerName(ctx context.Context, attemptID uuid.UUID, runnerName string) error {
	_, err := s.PG.ExecContext(ctx, `
		UPDATE execution_attempts
		SET runner_name = $2, updated_at = now()
		WHERE attempt_id = $1
	`, attemptID, strings.TrimSpace(runnerName))
	return err
}

func forgejoRunnerConfigFromSnapshot(snapshotRef string) (vmorchestrator.Config, error) {
	snapshotRef = strings.TrimSpace(snapshotRef)
	if snapshotRef == "" {
		return vmorchestrator.Config{}, fmt.Errorf("snapshot_ref is required")
	}
	dataset := snapshotRef
	if idx := strings.Index(dataset, "@"); idx >= 0 {
		dataset = dataset[:idx]
	}
	if slash := strings.Index(dataset, "/"); slash >= 0 {
		parts := strings.SplitN(dataset, "/", 2)
		if len(parts) == 2 {
			dataset = parts[1]
		}
	}
	dataset = strings.Trim(dataset, "/")
	if dataset == "" {
		return vmorchestrator.Config{}, fmt.Errorf("invalid snapshot_ref %q", snapshotRef)
	}
	return vmorchestrator.Config{
		GoldenZvol: dataset,
	}, nil
}

func forgejoRunnerName(attemptID uuid.UUID) string {
	return "forge-metal-runner-" + attemptID.String()
}

func forgejoRunnerScript() string {
	return strings.TrimSpace(`
set -eu

emit_event() {
  if [ -n "${FORGE_METAL_GUEST_EVENT_FIFO:-}" ]; then
    printf '%s\n' "$1" >"${FORGE_METAL_GUEST_EVENT_FIFO}"
  fi
}

BIN=/usr/local/bin/forgejo-runner
RUNNER_HOME=/home/runner/forgejo-runner
RUNNER_LABEL_WITH_KIND="${FORGEJO_RUNNER_LABEL}:host"

mkdir -p "$RUNNER_HOME"
chown runner:runner "$RUNNER_HOME"

curl -fsSL -o "$BIN" "$FORGEJO_RUNNER_BINARY_URL"
chmod 0755 "$BIN"
printf '%s  %s\n' "$FORGEJO_RUNNER_BINARY_SHA256" "$BIN" | sha256sum -c -

su runner -c "cd $RUNNER_HOME && rm -f .runner runner-config.yaml && $BIN generate-config > runner-config.yaml"
su runner -c "cd $RUNNER_HOME && $BIN register \
  --config runner-config.yaml \
  --no-interactive \
  --instance \"$FORGEJO_INSTANCE_URL\" \
  --token \"$FORGEJO_RUNNER_REGISTRATION_TOKEN\" \
  --name \"$FORGEJO_RUNNER_NAME\" \
  --labels \"$RUNNER_LABEL_WITH_KIND\""

emit_event "$(printf '{"kind":"runner_registered","attrs":{"runner_name":"%s","provider_run_id":"%s","provider_job_id":"%s"}}' \
  "$FORGEJO_RUNNER_NAME" "$FORGE_METAL_PROVIDER_RUN_ID" "$FORGE_METAL_PROVIDER_JOB_ID")"

rc=0
su runner -c "cd $RUNNER_HOME && timeout ${FORGEJO_RUNNER_JOB_TIMEOUT_SECONDS}s $BIN one-job \
  --config runner-config.yaml \
  --wait" || rc=$?

emit_event "$(printf '{"kind":"runner_job_completed","attrs":{"runner_name":"%s","provider_run_id":"%s","provider_job_id":"%s","exit_code":"%s"}}' \
  "$FORGEJO_RUNNER_NAME" "$FORGE_METAL_PROVIDER_RUN_ID" "$FORGE_METAL_PROVIDER_JOB_ID" "$rc")"

exit "$rc"
`)
}
