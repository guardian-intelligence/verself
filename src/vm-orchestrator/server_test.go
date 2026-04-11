package vmorchestrator

import (
	"testing"
)

func TestJobObserverEmitsBillablePhaseEvents(t *testing.T) {
	job := &managedJob{
		id:             "job-1",
		state:          JobStateRunning,
		billablePhases: map[string]struct{}{"run": {}},
	}
	observer := &jobObserver{job: job}

	observer.OnGuestPhaseStart("job-1", "run")
	observer.OnGuestPhaseEnd("job-1", PhaseResult{Name: "run", ExitCode: 0, DurationMS: 1234})

	events, terminal := job.guestEventSnapshot(0)
	if terminal {
		t.Fatal("running job reported terminal guest event stream")
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 phase events, got %d", len(events))
	}

	start := events[0]
	if start.Kind != "phase_started" {
		t.Fatalf("expected first event kind=phase_started, got %q", start.Kind)
	}
	if start.Attrs["phase"] != "run" {
		t.Fatalf("expected phase attr run, got %q", start.Attrs["phase"])
	}
	if start.Attrs["billable"] != "true" {
		t.Fatalf("expected billable attr true, got %q", start.Attrs["billable"])
	}
	if start.Attrs["host_received_unix_nano"] == "" {
		t.Fatal("expected host_received_unix_nano on phase start")
	}

	end := events[1]
	if end.Kind != "phase_ended" {
		t.Fatalf("expected second event kind=phase_ended, got %q", end.Kind)
	}
	if end.Attrs["phase"] != "run" {
		t.Fatalf("expected phase attr run, got %q", end.Attrs["phase"])
	}
	if end.Attrs["billable"] != "true" {
		t.Fatalf("expected billable attr true, got %q", end.Attrs["billable"])
	}
	if end.Attrs["exit_code"] != "0" {
		t.Fatalf("expected exit_code attr 0, got %q", end.Attrs["exit_code"])
	}
	if end.Attrs["duration_ms"] != "1234" {
		t.Fatalf("expected duration_ms attr 1234, got %q", end.Attrs["duration_ms"])
	}
	if end.Attrs["host_received_unix_nano"] == "" {
		t.Fatal("expected host_received_unix_nano on phase end")
	}
}
