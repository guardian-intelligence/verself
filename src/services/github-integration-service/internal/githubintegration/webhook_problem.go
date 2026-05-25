package githubintegration

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

type webhookProblem struct {
	Type       string
	Code       string
	Title      string
	Detail     string
	Status     int32
	Phase      string
	Retryable  bool
	Pointer    string
	DocsURL    string
	ObservedAt time.Time
}

type webhookProblemSet struct {
	problems []webhookProblem
}

type webhookProblemDocument struct {
	Type    string                  `json:"type"`
	Title   string                  `json:"title"`
	Status  int32                   `json:"status"`
	Detail  string                  `json:"detail,omitempty"`
	Code    string                  `json:"code"`
	DocsURL string                  `json:"docs_url,omitempty"`
	Errors  []webhookProblemPayload `json:"errors,omitempty"`
}

type webhookProblemPayload struct {
	Type       string `json:"type"`
	Code       string `json:"code"`
	Title      string `json:"title"`
	Detail     string `json:"detail,omitempty"`
	Status     int32  `json:"status,omitempty"`
	Phase      string `json:"phase,omitempty"`
	Retryable  bool   `json:"retryable,omitempty"`
	Pointer    string `json:"pointer,omitempty"`
	DocsURL    string `json:"docs_url,omitempty"`
	ObservedAt string `json:"observed_at,omitempty"`
}

func newWebhookProblemSet(problems ...webhookProblem) webhookProblemSet {
	var out webhookProblemSet
	for _, problem := range problems {
		out.add(problem)
	}
	return out
}

func (s *webhookProblemSet) add(problem webhookProblem) {
	if problem.ObservedAt.IsZero() {
		problem.ObservedAt = time.Now().UTC()
	}
	if problem.Title == "" {
		problem.Title = http.StatusText(int(problem.Status))
	}
	if problem.Phase == "" {
		problem.Phase = "webhook"
	}
	s.problems = append(s.problems, problem)
}

func (s webhookProblemSet) empty() bool {
	return len(s.problems) == 0
}

func (s webhookProblemSet) primary() webhookProblem {
	if len(s.problems) == 0 {
		return githubWebhookProblem(problemProviderWebhookProcessingFailed)
	}
	return s.problems[0]
}

func (s webhookProblemSet) reason() string {
	primary := s.primary()
	if primary.Detail == "" {
		return primary.Code
	}
	return primary.Code + ": " + primary.Detail
}

func (s webhookProblemSet) httpStatus() int {
	status := int(s.primary().Status)
	if status < 100 || status > 599 {
		return http.StatusInternalServerError
	}
	return status
}

func (s webhookProblemSet) document() webhookProblemDocument {
	primary := s.primary()
	doc := webhookProblemDocument{
		Type:    primary.Type,
		Title:   primary.Title,
		Status:  primary.Status,
		Detail:  primary.Detail,
		Code:    primary.Code,
		DocsURL: primary.DocsURL,
		Errors:  make([]webhookProblemPayload, 0, len(s.problems)),
	}
	for _, problem := range s.problems {
		payload := webhookProblemPayload{
			Type:      problem.Type,
			Code:      problem.Code,
			Title:     problem.Title,
			Detail:    problem.Detail,
			Status:    problem.Status,
			Phase:     problem.Phase,
			Retryable: problem.Retryable,
			Pointer:   problem.Pointer,
			DocsURL:   problem.DocsURL,
		}
		if !problem.ObservedAt.IsZero() {
			payload.ObservedAt = problem.ObservedAt.UTC().Format(time.RFC3339Nano)
		}
		doc.Errors = append(doc.Errors, payload)
	}
	return doc
}

func writeWebhookProblem(w http.ResponseWriter, problems webhookProblemSet) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(problems.httpStatus())
	_ = json.NewEncoder(w).Encode(problems.document())
}

func providerWebhookHeaderProblem(headerName string) webhookProblem {
	return githubWebhookProblem(
		problemProviderWebhookHeaderInvalid,
		withProblemPointer("header:"+headerName),
	)
}

func providerWebhookBodyProblem(detail string) webhookProblem {
	return githubWebhookProblem(
		problemProviderWebhookBodyInvalid,
		withProblemDiagnostic(detail),
		withProblemPointer("body"),
	)
}

func providerWebhookSignatureProblem() webhookProblem {
	return githubWebhookProblem(
		problemProviderWebhookSignatureInvalid,
		withProblemPointer("header:X-Hub-Signature-256"),
	)
}

func providerWebhookPayloadProblem(detail string) webhookProblem {
	return githubWebhookProblem(
		problemProviderWebhookPayloadInvalid,
		withProblemDiagnostic(detail),
		withProblemPointer("body"),
	)
}

func providerWebhookReplayProblem() webhookProblem {
	return githubWebhookProblem(problemProviderWebhookDeliveryReplayConflict)
}

func providerWebhookInboxProblem() webhookProblem {
	return githubWebhookProblem(problemProviderWebhookInboxUnavailable)
}

func providerWebhookUnsupportedProblem(detail string) webhookProblem {
	return githubWebhookProblem(
		problemProviderWebhookUnsupportedEvent,
		withProblemDiagnostic(detail),
	)
}

func githubRepositoryNotEnabledProblem(detail string) webhookProblem {
	return githubWebhookProblem(
		problemGithubRepositoryNotEnabled,
		withProblemDiagnostic(detail),
	)
}

func githubSandboxDispatchProblem(detail string, retryable bool) webhookProblem {
	return githubWebhookProblem(
		problemGithubSandboxDispatchFailed,
		withProblemDiagnostic(detail),
		withProblemRetryable(retryable),
	)
}

func providerWebhookProcessingProblem(detail string, retryable bool) webhookProblem {
	return githubWebhookProblem(
		problemProviderWebhookProcessingFailed,
		withProblemDiagnostic(detail),
		withProblemRetryable(retryable),
	)
}

func providerWebhookProcessingStaleProblem() webhookProblem {
	return githubWebhookProblem(problemProviderWebhookProcessingStale)
}

func providerWebhookAttemptsExhaustedProblem() webhookProblem {
	return githubWebhookProblem(problemProviderWebhookProcessingAttemptsExhausted)
}

func problemSetForDeliveryError(err error, retryable bool) webhookProblemSet {
	detail := truncate(strings.TrimSpace(err.Error()), 1024)
	switch {
	case errors.Is(err, ErrUnsupportedWebhook):
		return newWebhookProblemSet(providerWebhookUnsupportedProblem(detail))
	case errors.Is(err, ErrRepositoryNotEnabled):
		return newWebhookProblemSet(githubRepositoryNotEnabledProblem(detail))
	case errors.Is(err, ErrWebhookRejected):
		return newWebhookProblemSet(providerWebhookPayloadProblem(detail))
	case errors.Is(err, ErrSandboxRejected):
		return newWebhookProblemSet(githubSandboxDispatchProblem(detail, retryable))
	default:
		return newWebhookProblemSet(providerWebhookProcessingProblem(detail, retryable))
	}
}
