// Fake SandboxRunner for e2e tests. Returns canned results after a
// configurable delay, replacing the real Firecracker orchestrator.
package e2e_test

import (
	"context"
	"time"

	vmorchestrator "github.com/forge-metal/vm-orchestrator"
)

type fakeRunner struct {
	delay     time.Duration
	exitCode  int
	logs      string
	err       error
	commitSHA string
	requireWarm bool
	warmed      bool
}

func (f *fakeRunner) Run(ctx context.Context, job vmorchestrator.JobConfig) (vmorchestrator.JobResult, error) {
	delay := f.executionDelay()
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return vmorchestrator.JobResult{}, ctx.Err()
	}
	if f.err != nil {
		return vmorchestrator.JobResult{}, f.err
	}
	return f.result(delay), nil
}

func (f *fakeRunner) ExecRepo(ctx context.Context, req vmorchestrator.RepoExecRequest) (vmorchestrator.JobStatus, error) {
	delay := f.executionDelay()
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return vmorchestrator.JobStatus{}, ctx.Err()
	}
	if f.err != nil {
		return vmorchestrator.JobStatus{}, f.err
	}
	if f.requireWarm && !f.warmed {
		return vmorchestrator.JobStatus{
			JobID:        req.JobTemplate.JobID,
			State:        vmorchestrator.JobStateFailed,
			Terminal:     true,
			ErrorMessage: "repo golden for toy-next-bun-monorepo does not exist; run warm first",
		}, nil
	}
	result := f.result(delay)
	state := vmorchestrator.JobStateSucceeded
	if f.exitCode != 0 {
		state = vmorchestrator.JobStateFailed
	}
	return vmorchestrator.JobStatus{
		JobID:    req.JobTemplate.JobID,
		State:    state,
		Terminal: true,
		Result:   &result,
		RepoExec: &vmorchestrator.RepoExecMetadata{
			Repo:           req.Repo,
			RepoURL:        req.RepoURL,
			Ref:            req.Ref,
			GoldenSnapshot: "golden/toy-next-bun-monorepo@0001",
			CloneDuration:  125 * time.Millisecond,
			InstallNeeded:  true,
			CommitSHA:      f.commitSHA,
		},
	}, nil
}

func (f *fakeRunner) WarmGolden(ctx context.Context, req vmorchestrator.WarmGoldenRequest) (vmorchestrator.WarmGoldenResult, error) {
	delay := f.executionDelay() / 4
	if delay <= 0 {
		delay = 50 * time.Millisecond
	}
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return vmorchestrator.WarmGoldenResult{}, ctx.Err()
	}
	if f.err != nil {
		return vmorchestrator.WarmGoldenResult{}, f.err
	}
	f.warmed = true
	return vmorchestrator.WarmGoldenResult{
		TargetDataset:     "golden/toy-next-bun-monorepo@0002",
		Promoted:          true,
		CommitSHA:         f.commitSHA,
		JobResult:         f.result(delay),
		FilesystemCheckOK: true,
	}, nil
}

func (f *fakeRunner) executionDelay() time.Duration {
	if f.delay == 0 {
		return 200 * time.Millisecond
	}
	return f.delay
}

func (f *fakeRunner) result(delay time.Duration) vmorchestrator.JobResult {
	logs := f.logs
	if logs == "" {
		logs = "hello from e2e\n"
	}
	return vmorchestrator.JobResult{
		ExitCode:    f.exitCode,
		Logs:        logs,
		Duration:    delay,
		RunDuration: delay,
		ZFSWritten:  4096,
		StdoutBytes: uint64(len(logs)),
	}
}
