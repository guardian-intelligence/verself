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
	mu       sync.Mutex
	delay    time.Duration
	exitCode int
	logs     string
	err      error
	lastJob  vmorchestrator.JobConfig
}

type fakeRunnerSnapshot struct {
	delay    time.Duration
	exitCode int
	logs     string
	err      error
}

func (f *fakeRunner) Run(ctx context.Context, job vmorchestrator.JobConfig) (vmorchestrator.JobResult, error) {
	f.recordInvocation(job)
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

func (f *fakeRunner) recordInvocation(job vmorchestrator.JobConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastJob = copyJobConfig(job)
}

func (f *fakeRunner) snapshot() fakeRunnerSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fakeRunnerSnapshot{
		delay:    f.delay,
		exitCode: f.exitCode,
		logs:     f.logs,
		err:      f.err,
	}
}

func (f *fakeRunner) setError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeRunner) lastInvocation() vmorchestrator.JobConfig {
	f.mu.Lock()
	defer f.mu.Unlock()
	return copyJobConfig(f.lastJob)
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
