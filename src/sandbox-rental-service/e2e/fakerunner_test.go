// Fake SandboxRunner for e2e tests. Returns a canned JobResult after a
// configurable delay, replacing the real Firecracker orchestrator.
package e2e_test

import (
	"context"
	"time"

	vmorchestrator "github.com/forge-metal/vm-orchestrator"
)

// fakeRunner implements jobs.SandboxRunner for e2e tests.
// Returns a canned successful result without spawning VMs.
type fakeRunner struct {
	delay    time.Duration // simulated execution time (default 200ms)
	exitCode int           // exit code to return (default 0)
	logs     string        // log output to return
	err      error         // if set, Run returns this error
}

func (f *fakeRunner) Run(ctx context.Context, job vmorchestrator.JobConfig) (vmorchestrator.JobResult, error) {
	delay := f.delay
	if delay == 0 {
		delay = 200 * time.Millisecond
	}

	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return vmorchestrator.JobResult{}, ctx.Err()
	}

	if f.err != nil {
		return vmorchestrator.JobResult{}, f.err
	}

	logs := f.logs
	if logs == "" {
		logs = "hello from e2e\n"
	}

	return vmorchestrator.JobResult{
		ExitCode:    f.exitCode,
		Logs:        logs,
		Duration:    delay,
		RunDuration: delay,
		ZFSWritten:  4096, // simulated minimal COW write
		StdoutBytes: uint64(len(logs)),
	}, nil
}
