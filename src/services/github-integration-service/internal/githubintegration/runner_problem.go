package githubintegration

import (
	"context"
	"strings"
	"time"

	"github.com/verself/github-integration-service/internal/store"
)

type runnerProblem struct {
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
	problems []runnerProblem
}

func (s *runnerProblemSet) add(problem runnerProblem) {
	problem.Type = strings.TrimSpace(problem.Type)
	problem.Code = strings.TrimSpace(problem.Code)
	problem.Title = strings.TrimSpace(problem.Title)
	problem.Detail = truncate(strings.TrimSpace(problem.Detail), 2048)
	problem.Phase = strings.TrimSpace(problem.Phase)
	problem.Pointer = strings.TrimSpace(problem.Pointer)
	if problem.Type == "" {
		problem.Type = "urn:verself:problem:github-runner:capacity_failed"
	}
	if problem.Code == "" {
		problem.Code = "github_runner.capacity_failed"
	}
	if problem.Title == "" {
		problem.Title = "GitHub runner capacity failed"
	}
	if problem.Phase == "" {
		problem.Phase = "runner_capacity"
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
		return "github_runner.capacity_failed"
	}
	first := s.problems[0]
	if first.Detail != "" {
		return first.Code + ":" + truncate(first.Detail, 512)
	}
	return first.Code
}

func githubRunnerProblem(phase, code, title, detail string, retryable bool) runnerProblem {
	code = strings.TrimSpace(code)
	return runnerProblem{
		Type:      "urn:verself:problem:github-runner:" + strings.TrimPrefix(strings.ReplaceAll(code, ".", "_"), "github_runner_"),
		Code:      code,
		Title:     title,
		Detail:    detail,
		Phase:     phase,
		Retryable: retryable,
	}
}

func githubRunnerProblemFromError(phase, code, title string, err error, retryable bool) runnerProblem {
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	return githubRunnerProblem(phase, code, title, detail, retryable)
}

func appendProviderDemandProblems(ctx context.Context, q *store.Queries, providerJobID int64, problems runnerProblemSet) error {
	if providerJobID == 0 {
		return nil
	}
	for _, problem := range problems.problems {
		if problem.ObservedAt.IsZero() {
			problem.ObservedAt = time.Now().UTC()
		}
		if err := q.AppendProviderDemandProblem(ctx, store.AppendProviderDemandProblemParams{
			ProviderJobID: providerJobID,
			Phase:         problem.Phase,
			ProblemType:   problem.Type,
			ProblemCode:   problem.Code,
			Title:         problem.Title,
			Detail:        problem.Detail,
			Status:        problem.Status,
			Retryable:     problem.Retryable,
			Pointer:       problem.Pointer,
			ObservedAt:    pgTime(problem.ObservedAt),
		}); err != nil {
			return err
		}
	}
	return nil
}

func appendRunnerInstanceProblems(ctx context.Context, q *store.Queries, runnerName string, problems runnerProblemSet) error {
	runnerName = strings.TrimSpace(runnerName)
	if runnerName == "" {
		return nil
	}
	for _, problem := range problems.problems {
		if problem.ObservedAt.IsZero() {
			problem.ObservedAt = time.Now().UTC()
		}
		if err := q.AppendRunnerInstanceProblem(ctx, store.AppendRunnerInstanceProblemParams{
			RunnerName:  runnerName,
			Phase:       problem.Phase,
			ProblemType: problem.Type,
			ProblemCode: problem.Code,
			Title:       problem.Title,
			Detail:      problem.Detail,
			Status:      problem.Status,
			Retryable:   problem.Retryable,
			Pointer:     problem.Pointer,
			ObservedAt:  pgTime(problem.ObservedAt),
		}); err != nil {
			return err
		}
	}
	return nil
}
