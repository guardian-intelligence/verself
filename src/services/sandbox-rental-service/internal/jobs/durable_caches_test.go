package jobs

import (
	"errors"
	"testing"

	"github.com/google/uuid"
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
	if got, want := len(decl.Caches), 1; got != want {
		t.Fatalf("cache count = %d, want %d", got, want)
	}
	cache := decl.Caches[0]
	wantPaths := []string{"/home/runner/.cache/bazel-disk", "/home/runner/.cache/bazel-repo"}
	if got := cache.Paths; len(got) != len(wantPaths) || got[0] != wantPaths[0] || got[1] != wantPaths[1] {
		t.Fatalf("paths = %#v, want %#v", got, wantPaths)
	}
}

func TestParseCacheManifestRejectsUnknownFields(t *testing.T) {
	_, err := parseCacheManifest([]byte(`
version: 1
cache:
  - name: bazel
    ttl: 7d
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

func TestDurableCommitSkipReason(t *testing.T) {
	tests := []struct {
		name         string
		plan         durableCachePlan
		sealDecision durableSealDecision
		want         string
	}{
		{
			name:         "exec failure skips with original reason",
			sealDecision: durableSealDecision{SkipReason: "exec_failed"},
			want:         "exec_failed",
		},
		{
			name:         "successful non-promotable run is informational",
			plan:         durableCachePlan{GoldenVM: goldenVMPlan{Enabled: true}, Operations: []durableCacheOperation{{PromotionEligible: false}}},
			sealDecision: durableSealDecision{Commit: true},
			want:         "non_promotable_scope",
		},
		{
			name:         "promotable durable operation commits",
			plan:         durableCachePlan{Operations: []durableCacheOperation{{PromotionEligible: true}}},
			sealDecision: durableSealDecision{Commit: true},
		},
		{
			name:         "promotable golden VM commits",
			plan:         durableCachePlan{GoldenVM: goldenVMPlan{Enabled: true, PromotionEligible: true}},
			sealDecision: durableSealDecision{Commit: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := durableCommitSkipReason(tt.plan, tt.sealDecision); got != tt.want {
				t.Fatalf("skip reason = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGoldenVMGenerationSetHashMatchesCommittedSources(t *testing.T) {
	scopeID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	generationID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	ops := []durableCacheOperation{{
		DurableScopeID:     scopeID,
		SourceGenerationID: &generationID,
		SourceSnapshotRef:  "vspool/orgs/org-1/goldens/scope-1/generations/gen-1@sealed",
		SourceSnapshotGUID: "123456",
		MountName:          "workspace",
		MountPath:          "/home/runner/work",
		BindPaths:          []string{"/home/runner/work"},
		CacheName:          durableWorkspaceCacheName,
		Required:           true,
	}}
	gens := []goldenVMSnapshotGeneration{{
		DurableScopeID:      scopeID,
		DurableGenerationID: generationID,
		ZFSSnapshotRef:      "vspool/orgs/org-1/goldens/scope-1/generations/gen-1@sealed",
		ZFSSnapshotGUID:     "123456",
		DriveID:             "workspace",
		MountPath:           "/home/runner/work",
		BindPaths:           []string{"/home/runner/work"},
		CacheName:           durableWorkspaceCacheName,
		FSType:              "ext4",
		Required:            true,
	}}
	if got, want := goldenVMSourceGenerationSetHash(ops), goldenVMCandidateGenerationSetHash(gens); got != want {
		t.Fatalf("generation set hash = %s, want %s", got, want)
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
