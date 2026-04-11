// Fake SandboxRunner for e2e tests. It simulates the vm-orchestrator streaming
// a billable phase event before the final job result.
package e2e_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	vmorchestrator "github.com/forge-metal/vm-orchestrator"
)

type fakeRunner struct {
	mu             sync.Mutex
	delay          time.Duration
	preStartDelay  time.Duration
	exitCode       int
	logs           string
	err            error
	errBeforeStart bool
	lastJob        vmorchestrator.JobConfig
	lastStartedAt  time.Time
	runs           map[string]*fakeRun
}

type fakeRun struct {
	jobID  string
	events chan vmorchestrator.JobGuestEvent
	done   chan struct{}

	mu     sync.Mutex
	result vmorchestrator.JobResult
	err    error
}

type fakeRunnerSnapshot struct {
	delay          time.Duration
	preStartDelay  time.Duration
	exitCode       int
	logs           string
	err            error
	errBeforeStart bool
}

func (f *fakeRunner) StartDirectJob(ctx context.Context, job vmorchestrator.JobConfig) (string, error) {
	f.recordInvocation(job)
	if job.JobID == "" {
		return "", fmt.Errorf("job_id is required")
	}
	run := &fakeRun{
		jobID:  job.JobID,
		events: make(chan vmorchestrator.JobGuestEvent, 8),
		done:   make(chan struct{}),
	}
	f.mu.Lock()
	if f.runs == nil {
		f.runs = map[string]*fakeRun{}
	}
	f.runs[job.JobID] = run
	f.mu.Unlock()

	go f.execute(ctx, run)
	return job.JobID, nil
}

func (f *fakeRunner) StreamGuestEvents(ctx context.Context, jobID string, _ bool, handler func(vmorchestrator.JobGuestEvent) error) error {
	run := f.run(jobID)
	if run == nil {
		return fmt.Errorf("fake runner job %s not found", jobID)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-run.events:
			if !ok {
				if handler != nil {
					return handler(vmorchestrator.JobGuestEvent{JobID: jobID, Terminal: true})
				}
				return nil
			}
			if handler != nil {
				if err := handler(event); err != nil {
					return err
				}
			}
		}
	}
}

func (f *fakeRunner) WaitJob(ctx context.Context, jobID string, _ bool) (vmorchestrator.JobStatus, error) {
	run := f.run(jobID)
	if run == nil {
		return vmorchestrator.JobStatus{}, fmt.Errorf("fake runner job %s not found", jobID)
	}
	select {
	case <-ctx.Done():
		return vmorchestrator.JobStatus{}, ctx.Err()
	case <-run.done:
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	status := vmorchestrator.JobStatus{
		JobID:    jobID,
		Terminal: true,
		State:    vmorchestrator.JobStateSucceeded,
		Result:   copyJobResult(run.result),
	}
	if run.err != nil {
		status.State = vmorchestrator.JobStateFailed
		status.ErrorMessage = run.err.Error()
		return status, run.err
	}
	if run.result.ExitCode != 0 {
		status.State = vmorchestrator.JobStateFailed
	}
	return status, nil
}

func (f *fakeRunner) CancelJob(_ context.Context, jobID string) (bool, error) {
	run := f.run(jobID)
	if run == nil {
		return false, nil
	}
	run.finish(vmorchestrator.JobResult{}, context.Canceled)
	return true, nil
}

func (f *fakeRunner) execute(ctx context.Context, run *fakeRun) {
	defer close(run.events)
	state := f.snapshot()
	preStartDelay := state.preStartDelay
	if preStartDelay == 0 {
		preStartDelay = 50 * time.Millisecond
	}
	select {
	case <-time.After(preStartDelay):
	case <-ctx.Done():
		run.finish(vmorchestrator.JobResult{}, ctx.Err())
		return
	}
	if state.err != nil && state.errBeforeStart {
		run.finish(vmorchestrator.JobResult{}, state.err)
		return
	}

	startedAt := time.Now().UTC()
	f.setLastStartedAt(startedAt)
	run.events <- vmorchestrator.JobGuestEvent{
		Seq:   1,
		JobID: run.jobID,
		Kind:  "phase_started",
		Attrs: map[string]string{
			"phase":                   "run",
			"billable":                "true",
			"host_received_unix_nano": fmt.Sprintf("%d", startedAt.UnixNano()),
		},
	}

	delay := state.delay
	if delay == 0 {
		delay = 200 * time.Millisecond
	}
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		run.finish(vmorchestrator.JobResult{}, ctx.Err())
		return
	}
	if state.err != nil {
		run.finish(vmorchestrator.JobResult{}, state.err)
		return
	}
	run.finish(f.result(delay), nil)
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
		Metrics: &vmorchestrator.VMMetrics{
			BlockReadBytes:  1024,
			BlockWriteBytes: 2048,
			NetRxBytes:      512,
			NetTxBytes:      256,
		},
	}
}

func (f *fakeRunner) recordInvocation(job vmorchestrator.JobConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastJob = copyJobConfig(job)
}

func (f *fakeRunner) setLastStartedAt(startedAt time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastStartedAt = startedAt
}

func (f *fakeRunner) snapshot() fakeRunnerSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fakeRunnerSnapshot{
		delay:          f.delay,
		preStartDelay:  f.preStartDelay,
		exitCode:       f.exitCode,
		logs:           f.logs,
		err:            f.err,
		errBeforeStart: f.errBeforeStart,
	}
}

func (f *fakeRunner) setError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
	f.errBeforeStart = true
}

func (f *fakeRunner) lastInvocation() vmorchestrator.JobConfig {
	f.mu.Lock()
	defer f.mu.Unlock()
	return copyJobConfig(f.lastJob)
}

func (f *fakeRunner) billableStartedAt() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastStartedAt
}

func (f *fakeRunner) run(jobID string) *fakeRun {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runs[jobID]
}

func (r *fakeRun) finish(result vmorchestrator.JobResult, err error) {
	r.mu.Lock()
	select {
	case <-r.done:
		r.mu.Unlock()
		return
	default:
	}
	r.result = result
	r.err = err
	close(r.done)
	r.mu.Unlock()
}

func copyJobResult(result vmorchestrator.JobResult) *vmorchestrator.JobResult {
	cloned := result
	if result.Metrics != nil {
		metrics := *result.Metrics
		cloned.Metrics = &metrics
	}
	if result.PhaseResults != nil {
		cloned.PhaseResults = append([]vmorchestrator.PhaseResult(nil), result.PhaseResults...)
	}
	return &cloned
}

func copyJobConfig(job vmorchestrator.JobConfig) vmorchestrator.JobConfig {
	cloned := job
	if job.RunCommand != nil {
		cloned.RunCommand = append([]string(nil), job.RunCommand...)
	}
	if job.BillablePhases != nil {
		cloned.BillablePhases = append([]string(nil), job.BillablePhases...)
	}
	if job.Env != nil {
		cloned.Env = make(map[string]string, len(job.Env))
		for key, value := range job.Env {
			cloned.Env[key] = value
		}
	}
	return cloned
}
