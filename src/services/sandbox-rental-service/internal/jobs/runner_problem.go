package jobs

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/verself/sandbox-rental-service/internal/store"
)

type RunnerProblem struct {
	Type       string
	Code       string
	Title      string
	Detail     string
	Status     int32
	Phase      string
	Retryable  bool
	Pointer    string
	ObservedAt time.Time
}

type runnerProblemSet struct {
	problems []RunnerProblem
}

func (s *runnerProblemSet) add(problem RunnerProblem) {
	problem.Type = strings.TrimSpace(problem.Type)
	problem.Code = strings.TrimSpace(problem.Code)
	problem.Title = strings.TrimSpace(problem.Title)
	problem.Detail = truncateReason(strings.TrimSpace(problem.Detail))
	problem.Phase = strings.TrimSpace(problem.Phase)
	problem.Pointer = strings.TrimSpace(problem.Pointer)
	if problem.Type == "" {
		problem.Type = "urn:verself:problem:runner-allocation:failed"
	}
	if problem.Code == "" {
		problem.Code = "runner_allocation.failed"
	}
	if problem.Title == "" {
		problem.Title = "Runner allocation failed"
	}
	if problem.Phase == "" {
		problem.Phase = "runner_allocation"
	}
	if problem.ObservedAt.IsZero() {
		problem.ObservedAt = time.Now().UTC()
	}
	s.problems = append(s.problems, problem)
}

func (s runnerProblemSet) empty() bool {
	return len(s.problems) == 0
}

func (s runnerProblemSet) reason() string {
	if len(s.problems) == 0 {
		return "runner_allocation.failed"
	}
	first := s.problems[0]
	if first.Detail != "" {
		return first.Code + ":" + first.Detail
	}
	return first.Code
}

func runnerAllocationProblem(phase, code, title, detail string, retryable bool) RunnerProblem {
	code = strings.TrimSpace(code)
	return RunnerProblem{
		Type:      "urn:verself:problem:runner-allocation:" + strings.TrimPrefix(strings.ReplaceAll(code, ".", "_"), "runner_allocation_"),
		Code:      code,
		Title:     title,
		Detail:    detail,
		Phase:     phase,
		Retryable: retryable,
	}
}

func runnerAllocationProblemFromError(phase, code, title string, err error, retryable bool) RunnerProblem {
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	return runnerAllocationProblem(phase, code, title, detail, retryable)
}

func appendRunnerAllocationProblems(ctx context.Context, q *store.Queries, allocationID uuid.UUID, problems runnerProblemSet) error {
	if allocationID == uuid.Nil {
		return nil
	}
	for _, problem := range problems.problems {
		if problem.ObservedAt.IsZero() {
			problem.ObservedAt = time.Now().UTC()
		}
		if err := q.AppendRunnerAllocationProblem(ctx, store.AppendRunnerAllocationProblemParams{
			AllocationID: allocationID,
			Phase:        problem.Phase,
			ProblemType:  problem.Type,
			ProblemCode:  problem.Code,
			Title:        problem.Title,
			Detail:       problem.Detail,
			Status:       problem.Status,
			Retryable:    problem.Retryable,
			Pointer:      problem.Pointer,
			ObservedAt:   pgTime(problem.ObservedAt),
		}); err != nil {
			return err
		}
	}
	return nil
}
