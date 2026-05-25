package jobs

import (
	"context"
	"errors"
	"testing"
)

func TestRunnerExecutionSubmitProblemUsesDeadlineCode(t *testing.T) {
	problem := runnerExecutionSubmitProblem(context.DeadlineExceeded)
	if problem.Code != "runner_allocation.execution_submit_deadline_exceeded" {
		t.Fatalf("deadline problem code = %q", problem.Code)
	}
	if problem.Phase != "execution_submit" {
		t.Fatalf("deadline problem phase = %q", problem.Phase)
	}
}

func TestRunnerExecutionSubmitProblemUsesGenericCode(t *testing.T) {
	problem := runnerExecutionSubmitProblem(errors.New("scheduler unavailable"))
	if problem.Code != "runner_allocation.execution_submit_failed" {
		t.Fatalf("submit problem code = %q", problem.Code)
	}
	if problem.Phase != "execution_submit" {
		t.Fatalf("submit problem phase = %q", problem.Phase)
	}
}

func TestDeadlineProblemCodeTracksAllocationPhase(t *testing.T) {
	if got := deadlineProblemCode("vm_submitted"); got != "runner_allocation.runner_listening_deadline_exceeded" {
		t.Fatalf("vm_submitted deadline problem code = %q", got)
	}
	if got := deadlineProblemPhase("vm_submitted"); got != "runner_listening" {
		t.Fatalf("vm_submitted deadline phase = %q", got)
	}
}
