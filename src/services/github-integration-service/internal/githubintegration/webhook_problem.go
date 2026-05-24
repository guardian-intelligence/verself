package githubintegration

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

const providerWebhookProblemTypePrefix = "urn:verself:problem:"

type webhookProblem struct {
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

type webhookProblemSet struct {
	problems []webhookProblem
}

type webhookProblemDocument struct {
	Type   string                  `json:"type"`
	Title  string                  `json:"title"`
	Status int32                   `json:"status"`
	Detail string                  `json:"detail,omitempty"`
	Code   string                  `json:"code"`
	Errors []webhookProblemPayload `json:"errors,omitempty"`
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
	if problem.Type == "" && problem.Code != "" {
		problem.Type = providerWebhookProblemTypePrefix + strings.ReplaceAll(problem.Code, ".", ":")
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
		return webhookProblem{
			Type:       providerWebhookProblemTypePrefix + "provider_webhook:processing_failed",
			Code:       "provider_webhook.processing_failed",
			Title:      "Webhook delivery processing failed",
			Detail:     "Webhook delivery processing failed.",
			Status:     http.StatusInternalServerError,
			Phase:      "webhook",
			Retryable:  true,
			ObservedAt: time.Now().UTC(),
		}
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
		Type:   primary.Type,
		Title:  primary.Title,
		Status: primary.Status,
		Detail: primary.Detail,
		Code:   primary.Code,
		Errors: make([]webhookProblemPayload, 0, len(s.problems)),
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
	return webhookProblem{
		Type:      "urn:verself:problem:provider_webhook:invalid_request",
		Code:      "provider_webhook.header_invalid",
		Title:     "Invalid webhook header",
		Detail:    "GitHub webhook request is missing a required singular header.",
		Status:    http.StatusBadRequest,
		Phase:     "header_validation",
		Retryable: false,
		Pointer:   "header:" + headerName,
	}
}

func providerWebhookBodyProblem(detail string) webhookProblem {
	return webhookProblem{
		Type:      "urn:verself:problem:provider_webhook:invalid_request",
		Code:      "provider_webhook.body_invalid",
		Title:     "Invalid webhook body",
		Detail:    detail,
		Status:    http.StatusBadRequest,
		Phase:     "body_read",
		Retryable: false,
		Pointer:   "body",
	}
}

func providerWebhookSignatureProblem() webhookProblem {
	return webhookProblem{
		Type:      "urn:verself:problem:provider_webhook:signature_invalid",
		Code:      "provider_webhook.signature_invalid",
		Title:     "Invalid webhook signature",
		Detail:    "GitHub webhook signature verification failed.",
		Status:    http.StatusUnauthorized,
		Phase:     "signature_verification",
		Retryable: false,
		Pointer:   "header:X-Hub-Signature-256",
	}
}

func providerWebhookPayloadProblem(detail string) webhookProblem {
	return webhookProblem{
		Type:      "urn:verself:problem:provider_webhook:invalid_request",
		Code:      "provider_webhook.payload_invalid",
		Title:     "Invalid webhook payload",
		Detail:    detail,
		Status:    http.StatusBadRequest,
		Phase:     "payload_parse",
		Retryable: false,
		Pointer:   "body",
	}
}

func providerWebhookReplayProblem() webhookProblem {
	return webhookProblem{
		Type:      "urn:verself:problem:provider_webhook:delivery_replay_conflict",
		Code:      "provider_webhook.delivery_replay_conflict",
		Title:     "Webhook delivery replay conflict",
		Detail:    "GitHub delivery id was reused with a different payload.",
		Status:    http.StatusConflict,
		Phase:     "inbox_persist",
		Retryable: false,
	}
}

func providerWebhookInboxProblem() webhookProblem {
	return webhookProblem{
		Type:      "urn:verself:problem:provider_webhook:inbox_unavailable",
		Code:      "provider_webhook.inbox_unavailable",
		Title:     "Webhook inbox unavailable",
		Detail:    "Webhook delivery could not be durably recorded.",
		Status:    http.StatusServiceUnavailable,
		Phase:     "inbox_persist",
		Retryable: true,
	}
}

func providerWebhookUnsupportedProblem(detail string) webhookProblem {
	return webhookProblem{
		Type:      "urn:verself:problem:provider_webhook:unsupported_event",
		Code:      "provider_webhook.unsupported_event",
		Title:     "Unsupported webhook event",
		Detail:    detail,
		Status:    http.StatusUnprocessableEntity,
		Phase:     "delivery_dispatch",
		Retryable: false,
	}
}

func githubRepositoryNotEnabledProblem(detail string) webhookProblem {
	return webhookProblem{
		Type:      "urn:verself:problem:github:repository_not_enabled",
		Code:      "github.repository_not_enabled",
		Title:     "GitHub repository is not enabled",
		Detail:    detail,
		Status:    http.StatusConflict,
		Phase:     "repository_binding",
		Retryable: false,
	}
}

func githubSandboxDispatchProblem(detail string, retryable bool) webhookProblem {
	return webhookProblem{
		Type:      "urn:verself:problem:github:sandbox_dispatch_failed",
		Code:      "github.sandbox_dispatch_failed",
		Title:     "Sandbox dispatch failed",
		Detail:    detail,
		Status:    http.StatusServiceUnavailable,
		Phase:     "sandbox_dispatch",
		Retryable: retryable,
	}
}

func providerWebhookProcessingProblem(detail string, retryable bool) webhookProblem {
	return webhookProblem{
		Type:      "urn:verself:problem:provider_webhook:processing_failed",
		Code:      "provider_webhook.processing_failed",
		Title:     "Webhook delivery processing failed",
		Detail:    detail,
		Status:    http.StatusServiceUnavailable,
		Phase:     "delivery_processing",
		Retryable: retryable,
	}
}

func providerWebhookAttemptsExhaustedProblem() webhookProblem {
	return webhookProblem{
		Type:      "urn:verself:problem:provider_webhook:processing_attempts_exhausted",
		Code:      "provider_webhook.processing_attempts_exhausted",
		Title:     "Webhook delivery retry budget exhausted",
		Detail:    "Webhook delivery processing exceeded its retry budget.",
		Status:    http.StatusServiceUnavailable,
		Phase:     "delivery_processing",
		Retryable: false,
	}
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
