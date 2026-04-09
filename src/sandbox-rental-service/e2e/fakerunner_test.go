// Fake SandboxRunner for e2e tests. Returns canned results after a
// configurable delay, replacing the real Firecracker orchestrator.
package e2e_test

import (
	"context"
	"sync"
	"time"

	vmorchestrator "github.com/forge-metal/vm-orchestrator"
)

type fakeRunner struct {
	mu          sync.Mutex
	delay       time.Duration
	exitCode    int
	logs        string
	err         error
	commitSHA   string
	requireWarm bool
	warmed      bool
	lastConfig  vmorchestrator.Config
	lastJob     vmorchestrator.JobConfig
}

type fakeRunnerSnapshot struct {
	delay       time.Duration
	exitCode    int
	logs        string
	err         error
	commitSHA   string
	requireWarm bool
	warmed      bool
}

func (f *fakeRunner) Run(ctx context.Context, job vmorchestrator.JobConfig) (vmorchestrator.JobResult, error) {
	return f.RunWithConfig(ctx, vmorchestrator.Config{}, job)
}

func (f *fakeRunner) RunWithConfig(ctx context.Context, cfg vmorchestrator.Config, job vmorchestrator.JobConfig) (vmorchestrator.JobResult, error) {
	f.recordInvocation(cfg, job)
	delay := f.executionDelay()
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return vmorchestrator.JobResult{}, ctx.Err()
	}
	state := f.snapshot()
	if state.err != nil {
		return vmorchestrator.JobResult{}, state.err
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
	snapshot := f.snapshot()
	if snapshot.err != nil {
		return vmorchestrator.JobStatus{}, snapshot.err
	}
	if snapshot.requireWarm && !snapshot.warmed {
		return vmorchestrator.JobStatus{
			JobID:        req.JobTemplate.JobID,
			State:        vmorchestrator.JobStateFailed,
			Terminal:     true,
			ErrorMessage: "repo golden for toy-next-bun-monorepo does not exist; run warm first",
		}, nil
	}
	result := f.result(delay)
	jobState := vmorchestrator.JobStateSucceeded
	if snapshot.exitCode != 0 {
		jobState = vmorchestrator.JobStateFailed
	}
	return vmorchestrator.JobStatus{
		JobID:    req.JobTemplate.JobID,
		State:    jobState,
		Terminal: true,
		Result:   &result,
		RepoExec: &vmorchestrator.RepoExecMetadata{
			Repo:           req.Repo,
			RepoURL:        req.RepoURL,
			Ref:            req.Ref,
			GoldenSnapshot: "golden/toy-next-bun-monorepo@0001",
			CloneDuration:  125 * time.Millisecond,
			InstallNeeded:  true,
			CommitSHA:      snapshot.commitSHA,
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
	state := f.snapshot()
	if state.err != nil {
		return vmorchestrator.WarmGoldenResult{}, state.err
	}
	f.mu.Lock()
	f.warmed = true
	f.mu.Unlock()
	return vmorchestrator.WarmGoldenResult{
		TargetDataset:     "golden/toy-next-bun-monorepo@0002",
		Promoted:          true,
		CommitSHA:         state.commitSHA,
		JobResult:         f.result(delay),
		FilesystemCheckOK: true,
	}, nil
}

func (f *fakeRunner) executionDelay() time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.delay == 0 {
		return 200 * time.Millisecond
	}
	return f.delay
}

func (f *fakeRunner) result(delay time.Duration) vmorchestrator.JobResult {
	state := f.snapshot()
	logs := state.logs
	if logs == "" {
		logs = "hello from e2e\n"
	}
	return vmorchestrator.JobResult{
		ExitCode:    state.exitCode,
		Logs:        logs,
		Duration:    delay,
		RunDuration: delay,
		ZFSWritten:  4096,
		StdoutBytes: uint64(len(logs)),
	}
}

func (f *fakeRunner) recordInvocation(cfg vmorchestrator.Config, job vmorchestrator.JobConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastConfig = cfg
	f.lastJob = copyJobConfig(job)
}

func (f *fakeRunner) snapshot() fakeRunnerSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fakeRunnerSnapshot{
		delay:       f.delay,
		exitCode:    f.exitCode,
		logs:        f.logs,
		err:         f.err,
		commitSHA:   f.commitSHA,
		requireWarm: f.requireWarm,
		warmed:      f.warmed,
	}
}

func (f *fakeRunner) setCommitSHA(commitSHA string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commitSHA = commitSHA
}

func (f *fakeRunner) setError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeRunner) lastInvocation() (vmorchestrator.Config, vmorchestrator.JobConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastConfig, copyJobConfig(f.lastJob)
}

func copyJobConfig(job vmorchestrator.JobConfig) vmorchestrator.JobConfig {
	cloned := job
	if job.PrepareCommand != nil {
		cloned.PrepareCommand = append([]string(nil), job.PrepareCommand...)
	}
	if job.RunCommand != nil {
		cloned.RunCommand = append([]string(nil), job.RunCommand...)
	}
	if job.Services != nil {
		cloned.Services = append([]string(nil), job.Services...)
	}
	if job.Env != nil {
		cloned.Env = make(map[string]string, len(job.Env))
		for key, value := range job.Env {
			cloned.Env[key] = value
		}
	}
	return cloned
}
