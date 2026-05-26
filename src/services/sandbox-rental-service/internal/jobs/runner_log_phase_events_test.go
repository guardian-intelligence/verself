package jobs

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSandboxPhaseRowsForRunnerLog(t *testing.T) {
	startedAt := time.Date(2026, 5, 21, 0, 57, 50, 95_000_000, time.UTC)
	completedAt := time.Date(2026, 5, 21, 0, 58, 1, 979_000_000, time.UTC)
	record := ExecutionRecord{
		ExecutionID:      uuid.MustParse("4f514ad6-a61e-47bd-be55-1164ac569e79"),
		OrgID:            "org_test",
		Provider:         RunnerProviderGitHub,
		ExternalProvider: RunnerProviderGitHub,
		RunnerClass:      DefaultRunnerClassLabel,
		LatestAttempt: AttemptRecord{
			AttemptID:   uuid.MustParse("0ad7ca87-fae2-413f-93e5-e6bb09a3295c"),
			StartedAt:   &startedAt,
			CompletedAt: &completedAt,
			TraceID:     "trace",
		},
		Runner: RunnerRunMetadata{
			ProviderInstallationID: 1,
			ProviderRunID:          2,
			ProviderJobID:          3,
			RepositoryFullName:     "guardian-intelligence/verself",
			WorkflowName:           "Verself CI",
			JobName:                "Build",
			HeadBranch:             "main",
			HeadSHA:                "9b5eb6fd702d5e238bfda50c194ae44a83be902f",
		},
	}
	logs := `::verself-phase name=runner.bootstrap.start ts=2026-05-21T00:57:50.100Z
::verself-phase name=runner.bootstrap.jit_fetched ts=2026-05-21T00:57:50.300Z
::verself-phase name=runner.bootstrap.runtime_ready ts=2026-05-21T00:57:53.000Z
::verself-phase name=github.runner.process_start ts=2026-05-21T00:57:53.100Z
2026-05-21 00:57:54Z: Listening for Jobs
2026-05-21 00:57:56Z: Running job: Build
2026-05-21 00:58:01Z: Job Build completed with result: Succeeded`

	rows := sandboxPhaseRowsForRunnerLog(record, logs)
	if len(rows) != 8 {
		t.Fatalf("rows = %d, want 8", len(rows))
	}
	byName := map[string]sandboxPhaseRow{}
	for _, row := range rows {
		byName[row.PhaseName] = row
	}
	if got := byName["runner.bootstrap.fetch_jit"].DurationMs; got != 200 {
		t.Fatalf("fetch_jit duration = %d, want 200", got)
	}
	if got := byName["github.runner.assignment_wait"].DurationMs; got != 2000 {
		t.Fatalf("assignment_wait duration = %d, want 2000", got)
	}
	if got := byName["github.runner.job"].ProviderRunID; got != 2 {
		t.Fatalf("provider_run_id = %d, want 2", got)
	}
}

func TestGitHubRunnerCommandBuildsWritableRuntimeWithoutSudo(t *testing.T) {
	command := githubRunnerCommand()
	for _, disallowed := range []string{"sudo ", "mount -t overlay"} {
		if strings.Contains(command, disallowed) {
			t.Fatalf("runner bootstrap command contains %q", disallowed)
		}
	}
	for _, required := range []string{
		`toolchain_dir="` + githubRunnerToolchainDir + `"`,
		`cp -a "$toolchain_dir/bin" "$runtime_dir/bin"`,
		`cp -a "$toolchain_dir/run.sh" "$toolchain_dir/run-helper.sh.template" "$runtime_dir/"`,
		"ln -s \"$entry\" \"$runtime_dir/$base\"",
		"ln -s \"$durable_work_dir\" \"$runtime_dir/_work\"",
		"./run.sh --jitconfig",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("runner bootstrap command missing %q", required)
		}
	}
}
