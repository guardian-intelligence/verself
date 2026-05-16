package jobs

import (
	"errors"
	"testing"

	vmorchestrator "github.com/verself/vm-orchestrator"
)

func TestParseCacheManifestNormalizesHomePathsAndSorts(t *testing.T) {
	decl, err := parseCacheManifest([]byte(`
version: 1
cache:
  - name: bazel
    paths:
      - ~/.cache/bazel-repo
      - ~/.cache/bazel-disk
`), "manifest", ".verself/cache.yml", "abc123", "", "", "")
	if err != nil {
		t.Fatalf("parse cache manifest: %v", err)
	}
	if got, want := len(decl.Volumes), 1; got != want {
		t.Fatalf("volume count = %d, want %d", got, want)
	}
	volume := decl.Volumes[0]
	wantPaths := []string{"/home/runner/.cache/bazel-disk", "/home/runner/.cache/bazel-repo"}
	if got := volume.Paths; len(got) != len(wantPaths) || got[0] != wantPaths[0] || got[1] != wantPaths[1] {
		t.Fatalf("paths = %#v, want %#v", got, wantPaths)
	}
}

func TestParseCacheManifestRejectsSize(t *testing.T) {
	_, err := parseCacheManifest([]byte(`
version: 1
cache:
  - name: bazel
    size: 100GiB
    paths:
      - ~/.cache/bazel-repo
`), "manifest", ".verself/cache.yml", "abc123", "", "", "")
	if !errors.Is(err, ErrCacheDeclarationInvalid) {
		t.Fatalf("error = %v, want ErrCacheDeclarationInvalid", err)
	}
}

func TestParseCacheManifestRejectsWorkspacePaths(t *testing.T) {
	_, err := parseCacheManifest([]byte(`
version: 1
cache:
  - name: workspace-cache
    paths:
      - /workspace/project/cache
`), "manifest", ".verself/cache.yml", "abc123", "", "", "")
	if !errors.Is(err, ErrCacheDeclarationInvalid) {
		t.Fatalf("error = %v, want ErrCacheDeclarationInvalid", err)
	}
}

func TestParseCacheManifestRejectsNestedPaths(t *testing.T) {
	_, err := parseCacheManifest([]byte(`
version: 1
cache:
  - name: nested
    paths:
      - /verself/cache
      - /verself/cache/bazel
`), "manifest", ".verself/cache.yml", "abc123", "", "", "")
	if !errors.Is(err, ErrCacheDeclarationInvalid) {
		t.Fatalf("error = %v, want ErrCacheDeclarationInvalid", err)
	}
}

func TestDurableSealDecisionForExec(t *testing.T) {
	tests := []struct {
		name       string
		finalExec  vmorchestrator.ExecRecord
		wantCommit bool
		wantReason string
	}{
		{
			name:       "clean exit",
			finalExec:  vmorchestrator.ExecRecord{State: vmorchestrator.ExecStateExited, ExitCode: 0},
			wantCommit: true,
		},
		{
			name:       "nonzero exit",
			finalExec:  vmorchestrator.ExecRecord{State: vmorchestrator.ExecStateFailed, ExitCode: 1, TerminalReason: "tests failed"},
			wantReason: "exec_failed: tests failed",
		},
		{
			name:       "canceled",
			finalExec:  vmorchestrator.ExecRecord{State: vmorchestrator.ExecStateCanceled},
			wantReason: "exec_canceled",
		},
		{
			name:       "lease expiry",
			finalExec:  vmorchestrator.ExecRecord{State: vmorchestrator.ExecStateKilledByLeaseExpiry},
			wantReason: "exec_killed_by_lease_expiry",
		},
		{
			name:       "nonterminal state",
			finalExec:  vmorchestrator.ExecRecord{State: vmorchestrator.ExecStateRunning},
			wantReason: "exec_not_success",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := durableSealDecisionForExec(tt.finalExec)
			if got.Commit != tt.wantCommit {
				t.Fatalf("commit = %t, want %t", got.Commit, tt.wantCommit)
			}
			if got.SkipReason != tt.wantReason {
				t.Fatalf("skip reason = %q, want %q", got.SkipReason, tt.wantReason)
			}
		})
	}
}

func TestExecutionOutcomeFromGitHubJobResult(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		conclusion string
		observed   bool
		wantState  string
		wantCommit bool
		wantReason string
	}{
		{name: "success", status: "completed", conclusion: "success", observed: true, wantState: StateSucceeded, wantCommit: true},
		{name: "skipped", status: "completed", conclusion: "skipped", observed: true, wantState: StateFailed, wantReason: "github_job_skipped"},
		{name: "failure", status: "completed", conclusion: "failure", observed: true, wantState: StateFailed, wantReason: "github_job_failure"},
		{name: "missing conclusion", status: "completed", observed: true, wantState: StateFailed, wantReason: "github_job_conclusion_missing"},
		{name: "still running", status: "in_progress", observed: true, wantState: StateFailed, wantReason: "github_job_not_completed: in_progress"},
		{name: "not observed", observed: false, wantState: StateFailed, wantReason: "github_job_result_missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := executionOutcomeFromGitHubJobResult(tt.status, tt.conclusion, tt.observed)
			if got.State != tt.wantState {
				t.Fatalf("state = %q, want %q", got.State, tt.wantState)
			}
			if got.SealDecision.Commit != tt.wantCommit {
				t.Fatalf("commit = %t, want %t", got.SealDecision.Commit, tt.wantCommit)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason, tt.wantReason)
			}
			if !tt.wantCommit && got.SealDecision.SkipReason != tt.wantReason {
				t.Fatalf("skip reason = %q, want %q", got.SealDecision.SkipReason, tt.wantReason)
			}
		})
	}
}

func TestGitHubWorkflowJobResultTerminal(t *testing.T) {
	tests := []struct {
		name string
		in   githubWorkflowJobResult
		want bool
	}{
		{name: "completed observed", in: githubWorkflowJobResult{Status: "completed", Conclusion: "success", Observed: true}, want: true},
		{name: "completed with whitespace", in: githubWorkflowJobResult{Status: " completed ", Conclusion: "success", Observed: true}, want: true},
		{name: "in progress", in: githubWorkflowJobResult{Status: "in_progress", Observed: true}},
		{name: "not observed", in: githubWorkflowJobResult{Status: "completed", Conclusion: "success"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.terminal(); got != tt.want {
				t.Fatalf("terminal = %t, want %t", got, tt.want)
			}
		})
	}
}
