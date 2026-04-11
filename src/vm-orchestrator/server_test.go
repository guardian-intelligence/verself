package vmorchestrator

import (
	"strings"
	"testing"

	vmrpc "github.com/forge-metal/vm-orchestrator/proto/v1"
	"github.com/forge-metal/vm-orchestrator/vmproto"
)

func TestValidateWarmPromotionManifest(t *testing.T) {
	t.Parallel()

	validManifest := &RepoManifest{
		Kind:              vmproto.RepoOperationWarm,
		RequestedRef:      "refs/heads/main",
		ResolvedCommitSHA: "0123456789abcdef0123456789abcdef01234567",
		LockfileRelPath:   "pnpm-lock.yaml",
		LockfileSHA256:    strings.Repeat("a", 64),
		InstallNeeded:     true,
	}

	tests := []struct {
		name    string
		req     *vmrpc.WarmGoldenRequest
		result  JobResult
		wantErr string
	}{
		{
			name:    "accepts clean warm manifest",
			req:     &vmrpc.WarmGoldenRequest{DefaultBranch: "refs/heads/main", LockfileRelPath: "pnpm-lock.yaml"},
			result:  JobResult{RepoManifest: validManifest},
			wantErr: "",
		},
		{
			name:    "rejects forced shutdown",
			req:     &vmrpc.WarmGoldenRequest{LockfileRelPath: "pnpm-lock.yaml"},
			result:  JobResult{ForcedShutdown: true, RepoManifest: validManifest},
			wantErr: "forced shutdown",
		},
		{
			name:    "rejects missing manifest",
			req:     &vmrpc.WarmGoldenRequest{},
			result:  JobResult{},
			wantErr: "missing repo supervisor manifest",
		},
		{
			name: "rejects exec manifest",
			req:  &vmrpc.WarmGoldenRequest{},
			result: JobResult{RepoManifest: &RepoManifest{
				Kind:              vmproto.RepoOperationExec,
				ResolvedCommitSHA: "0123456789abcdef0123456789abcdef01234567",
			}},
			wantErr: "is not",
		},
		{
			name: "rejects invalid commit",
			req:  &vmrpc.WarmGoldenRequest{},
			result: JobResult{RepoManifest: &RepoManifest{
				Kind:              vmproto.RepoOperationWarm,
				ResolvedCommitSHA: "not-a-commit",
			}},
			wantErr: "invalid resolved commit",
		},
		{
			name: "rejects mismatched ref",
			req:  &vmrpc.WarmGoldenRequest{DefaultBranch: "refs/heads/release"},
			result: JobResult{RepoManifest: &RepoManifest{
				Kind:              vmproto.RepoOperationWarm,
				RequestedRef:      "refs/heads/main",
				ResolvedCommitSHA: "0123456789abcdef0123456789abcdef01234567",
			}},
			wantErr: "requested ref",
		},
		{
			name: "requires requested lockfile hash",
			req:  &vmrpc.WarmGoldenRequest{LockfileRelPath: "pnpm-lock.yaml"},
			result: JobResult{RepoManifest: &RepoManifest{
				Kind:              vmproto.RepoOperationWarm,
				ResolvedCommitSHA: "0123456789abcdef0123456789abcdef01234567",
				LockfileRelPath:   "pnpm-lock.yaml",
			}},
			wantErr: "missing lockfile hash",
		},
		{
			name: "rejects invalid requested lockfile hash",
			req:  &vmrpc.WarmGoldenRequest{LockfileRelPath: "pnpm-lock.yaml"},
			result: JobResult{RepoManifest: &RepoManifest{
				Kind:              vmproto.RepoOperationWarm,
				ResolvedCommitSHA: "0123456789abcdef0123456789abcdef01234567",
				LockfileRelPath:   "pnpm-lock.yaml",
				LockfileSHA256:    "not-a-sha",
			}},
			wantErr: "invalid lockfile hash",
		},
		{
			name: "rejects mismatched requested lockfile",
			req:  &vmrpc.WarmGoldenRequest{LockfileRelPath: "pnpm-lock.yaml"},
			result: JobResult{RepoManifest: &RepoManifest{
				Kind:              vmproto.RepoOperationWarm,
				ResolvedCommitSHA: "0123456789abcdef0123456789abcdef01234567",
				LockfileRelPath:   "package-lock.json",
				LockfileSHA256:    strings.Repeat("a", 64),
			}},
			wantErr: "requested lockfile",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateWarmPromotionManifest(tc.req, tc.result)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validateWarmPromotionManifest: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validateWarmPromotionManifest error: got %v want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateRepoExecManifest(t *testing.T) {
	t.Parallel()

	validManifest := &RepoManifest{
		Kind:                   vmproto.RepoOperationExec,
		RequestedRef:           "refs/pull/10/head",
		ResolvedCommitSHA:      "0123456789abcdef0123456789abcdef01234567",
		LockfileRelPath:        "pnpm-lock.yaml",
		LockfileSHA256:         strings.Repeat("b", 64),
		PreviousLockfileSHA256: strings.Repeat("c", 64),
		InstallNeeded:          false,
	}

	tests := []struct {
		name     string
		spec     *vmrpc.RepoExecSpec
		manifest *RepoManifest
		wantErr  string
	}{
		{
			name:     "accepts clean exec manifest",
			spec:     &vmrpc.RepoExecSpec{Ref: "refs/pull/10/head", LockfileRelPath: "pnpm-lock.yaml"},
			manifest: validManifest,
			wantErr:  "",
		},
		{
			name:    "rejects missing manifest",
			spec:    &vmrpc.RepoExecSpec{},
			wantErr: "without supervisor manifest",
		},
		{
			name: "rejects warm manifest",
			spec: &vmrpc.RepoExecSpec{},
			manifest: &RepoManifest{
				Kind:              vmproto.RepoOperationWarm,
				ResolvedCommitSHA: "0123456789abcdef0123456789abcdef01234567",
			},
			wantErr: "is not",
		},
		{
			name: "rejects invalid commit",
			spec: &vmrpc.RepoExecSpec{},
			manifest: &RepoManifest{
				Kind:              vmproto.RepoOperationExec,
				ResolvedCommitSHA: "not-a-commit",
			},
			wantErr: "invalid resolved commit",
		},
		{
			name: "rejects mismatched ref",
			spec: &vmrpc.RepoExecSpec{Ref: "refs/pull/10/head"},
			manifest: &RepoManifest{
				Kind:              vmproto.RepoOperationExec,
				RequestedRef:      "refs/heads/main",
				ResolvedCommitSHA: "0123456789abcdef0123456789abcdef01234567",
			},
			wantErr: "requested ref",
		},
		{
			name: "rejects mismatched lockfile",
			spec: &vmrpc.RepoExecSpec{LockfileRelPath: "pnpm-lock.yaml"},
			manifest: &RepoManifest{
				Kind:              vmproto.RepoOperationExec,
				ResolvedCommitSHA: "0123456789abcdef0123456789abcdef01234567",
				LockfileRelPath:   "package-lock.json",
			},
			wantErr: "requested lockfile",
		},
		{
			name: "rejects invalid lockfile hash",
			spec: &vmrpc.RepoExecSpec{},
			manifest: &RepoManifest{
				Kind:              vmproto.RepoOperationExec,
				ResolvedCommitSHA: "0123456789abcdef0123456789abcdef01234567",
				LockfileSHA256:    "not-a-sha",
			},
			wantErr: "invalid lockfile hash",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateRepoExecManifest(tc.spec, tc.manifest)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validateRepoExecManifest: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validateRepoExecManifest error: got %v want substring %q", err, tc.wantErr)
			}
		})
	}
}
