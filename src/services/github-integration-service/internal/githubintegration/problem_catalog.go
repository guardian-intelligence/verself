package githubintegration

import (
	"net/http"
	"strings"
	"time"
)

const githubIntegrationProblemDocsBaseURL = "https://verself.sh/docs/reference/github-integration/errors"

type githubProblemCode string

const (
	problemProviderWebhookMethodNotAllowed               githubProblemCode = "provider_webhook.method_not_allowed"
	problemProviderWebhookHeaderInvalid                  githubProblemCode = "provider_webhook.header_invalid"
	problemProviderWebhookBodyInvalid                    githubProblemCode = "provider_webhook.body_invalid"
	problemProviderWebhookSignatureInvalid               githubProblemCode = "provider_webhook.signature_invalid"
	problemProviderWebhookPayloadInvalid                 githubProblemCode = "provider_webhook.payload_invalid"
	problemProviderWebhookDeliveryReplayConflict         githubProblemCode = "provider_webhook.delivery_replay_conflict"
	problemProviderWebhookInboxUnavailable               githubProblemCode = "provider_webhook.inbox_unavailable"
	problemProviderWebhookUnsupportedEvent               githubProblemCode = "provider_webhook.unsupported_event"
	problemProviderWebhookProcessingFailed               githubProblemCode = "provider_webhook.processing_failed"
	problemProviderWebhookProcessingStale                githubProblemCode = "provider_webhook.processing_stale"
	problemProviderWebhookProcessingAttemptsExhausted    githubProblemCode = "provider_webhook.processing_attempts_exhausted"
	problemGithubRepositoryNotEnabled                    githubProblemCode = "github.repository_not_enabled"
	problemGithubSandboxDispatchFailed                   githubProblemCode = "github.sandbox_dispatch_failed"
	problemGithubRunnerCapacityFailed                    githubProblemCode = "github_runner.capacity_failed"
	problemGithubRunnerDemandRecordFailed                githubProblemCode = "github_runner.demand_record_failed"
	problemGithubRunnerWorkflowRunFetchFailed            githubProblemCode = "github_runner.workflow_run_fetch_failed"
	problemGithubRunnerWorkflowRunPersistFailed          githubProblemCode = "github_runner.workflow_run_persist_failed"
	problemGithubRunnerWorkflowJobFetchFailed            githubProblemCode = "github_runner.workflow_job_fetch_failed"
	problemGithubRunnerWorkflowJobNotFound               githubProblemCode = "github_runner.workflow_job_not_found"
	problemGithubRunnerWorkflowJobPersistFailed          githubProblemCode = "github_runner.workflow_job_persist_failed"
	problemGithubRunnerCacheManifestFetchFailed          githubProblemCode = "github_runner.cache_manifest_fetch_failed"
	problemGithubRunnerJobShapeBuildFailed               githubProblemCode = "github_runner.job_shape_build_failed"
	problemGithubRunnerJobShapePersistFailed             githubProblemCode = "github_runner.job_shape_persist_failed"
	problemGithubRunnerDemandUpdateFailed                githubProblemCode = "github_runner.demand_update_failed"
	problemGithubRunnerRunnerClassLockFailed             githubProblemCode = "github_runner.runner_class_lock_failed"
	problemGithubRunnerRunnerClassCapacityCountFailed    githubProblemCode = "github_runner.runner_class_capacity_count_failed"
	problemGithubRunnerDemandClaimFailed                 githubProblemCode = "github_runner.demand_claim_failed"
	problemGithubRunnerRunnerNameFailed                  githubProblemCode = "github_runner.runner_name_failed"
	problemGithubRunnerJITConfigFailed                   githubProblemCode = "github_runner.jit_config_failed"
	problemGithubRunnerJITConfigInvalid                  githubProblemCode = "github_runner.jit_config_invalid"
	problemGithubRunnerInstancePersistFailed             githubProblemCode = "github_runner.instance_persist_failed"
	problemGithubRunnerSandboxWorkflowObserveFailed      githubProblemCode = "github_runner.sandbox_workflow_observe_failed"
	problemGithubRunnerSandboxOutboxFailed               githubProblemCode = "github_runner.sandbox_outbox_failed"
	problemGithubRunnerSandboxSubmitFailed               githubProblemCode = "github_runner.sandbox_submit_failed"
	problemGithubRunnerSandboxRejected                   githubProblemCode = "github_runner.sandbox_rejected"
	problemGithubRunnerSandboxResponseInvalid            githubProblemCode = "github_runner.sandbox_response_invalid"
	problemGithubRunnerSubmissionPersistFailed           githubProblemCode = "github_runner.submission_persist_failed"
	problemGithubRunnerSandboxOutboxCompleteFailed       githubProblemCode = "github_runner.sandbox_outbox_complete_failed"
	problemGithubRunnerSubmissionCommitFailed            githubProblemCode = "github_runner.submission_commit_failed"
	problemGithubRunnerSandboxAllocationTerminal         githubProblemCode = "github_runner.sandbox_allocation_terminal"
	problemGithubRunnerGuestTimeSyncPrecisionGateFailed  githubProblemCode = "github_runner.guest_time_sync_precision_gate_failed"
	problemGithubRunnerSandboxAllocationNotFound         githubProblemCode = "github_runner.sandbox_allocation_not_found"
	problemGithubRunnerSandboxSubmitNotObserved          githubProblemCode = "github_runner.sandbox_submit_not_observed"
	problemGithubRunnerAssignmentDeadlineExceeded        githubProblemCode = "github_runner.assignment_deadline_exceeded"
	problemGithubRunnerRunnerCapacityDisplaced           githubProblemCode = "github_runner.runner_capacity_displaced"
	problemGithubRunnerCapacityDisplaced                 githubProblemCode = "github_runner.capacity_displaced"
	problemGithubRunnerProviderSurfaceFailed             githubProblemCode = "github_runner.provider_surface_failed"
	problemGithubRunnerProviderSurfaceWorkerLost         githubProblemCode = "github_runner.provider_surface_worker_lost"
	problemGithubRunnerProviderSurfaceAttemptsExhausted  githubProblemCode = "github_runner.provider_surface_attempts_exhausted"
	problemGithubRunnerProviderSurfaceCommandUnsupported githubProblemCode = "github_runner.provider_surface_command_unsupported"
	problemGithubRunnerProviderSurfaceTargetMissing      githubProblemCode = "github_runner.provider_surface_target_missing"
)

type githubProblemDefinition struct {
	Type      string
	Code      githubProblemCode
	Title     string
	Detail    string
	Status    int32
	Phase     string
	Retryable bool
	DocsURL   string
}

type githubProblemOccurrence struct {
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

type githubProblemOptions struct {
	diagnostic string
	pointer    string
	phase      string
	retryable  *bool
}

type githubProblemOption func(*githubProblemOptions)

func withProblemDiagnostic(diagnostic string) githubProblemOption {
	return func(options *githubProblemOptions) {
		options.diagnostic = strings.TrimSpace(diagnostic)
	}
}

func withProblemPointer(pointer string) githubProblemOption {
	return func(options *githubProblemOptions) {
		options.pointer = strings.TrimSpace(pointer)
	}
}

func withProblemRetryable(retryable bool) githubProblemOption {
	return func(options *githubProblemOptions) {
		options.retryable = &retryable
	}
}

func withProblemPhase(phase string) githubProblemOption {
	return func(options *githubProblemOptions) {
		options.phase = strings.TrimSpace(phase)
	}
}

func githubProblemFromCatalog(code githubProblemCode, opts ...githubProblemOption) githubProblemOccurrence {
	definition, ok := githubProblemCatalog[code]
	if !ok {
		definition = githubProblemCatalog[problemGithubRunnerCapacityFailed]
	}
	options := githubProblemOptions{}
	for _, opt := range opts {
		opt(&options)
	}
	retryable := definition.Retryable
	if options.retryable != nil {
		retryable = *options.retryable
	}
	phase := definition.Phase
	if options.phase != "" {
		phase = options.phase
	}
	return githubProblemOccurrence{
		Type:       definition.Type,
		Code:       string(definition.Code),
		Title:      definition.Title,
		Detail:     problemDetailWithDocs(definition.Detail, options.diagnostic, definition.DocsURL),
		Status:     definition.Status,
		Phase:      phase,
		Retryable:  retryable,
		Pointer:    options.pointer,
		DocsURL:    definition.DocsURL,
		ObservedAt: time.Now().UTC(),
	}
}

func githubWebhookProblem(code githubProblemCode, opts ...githubProblemOption) webhookProblem {
	problem := githubProblemFromCatalog(code, opts...)
	return webhookProblem(problem)
}

func githubRunnerProblemFromCatalog(code githubProblemCode, opts ...githubProblemOption) runnerProblem {
	problem := githubProblemFromCatalog(code, opts...)
	return runnerProblem(problem)
}

func githubRunnerProblemFromError(code githubProblemCode, err error, opts ...githubProblemOption) runnerProblem {
	if err != nil {
		opts = append(opts, withProblemDiagnostic(err.Error()))
	}
	return githubRunnerProblemFromCatalog(code, opts...)
}

func problemDetailWithDocs(detail string, diagnostic string, docsURL string) string {
	parts := []string{strings.TrimSpace(detail)}
	if diagnostic = strings.TrimSpace(diagnostic); diagnostic != "" {
		parts = append(parts, "Diagnostic: "+truncate(diagnostic, 768))
	}
	if docsURL = strings.TrimSpace(docsURL); docsURL != "" {
		parts = append(parts, "See "+docsURL)
	}
	return joinProblemSentences(parts...)
}

func joinProblemSentences(parts ...string) string {
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		part = strings.TrimRight(part, ".")
		out = append(out, part+".")
	}
	return strings.Join(out, " ")
}

func githubProblemDocsURL(code githubProblemCode) string {
	replacer := strings.NewReplacer(".", "-", "_", "-")
	return githubIntegrationProblemDocsBaseURL + "#" + replacer.Replace(string(code))
}

func githubRunnerProblemType(code githubProblemCode) string {
	return "urn:verself:problem:github-runner:" + strings.TrimPrefix(strings.ReplaceAll(string(code), ".", "_"), "github_runner_")
}

func providerWebhookProblemType(suffix string) string {
	return "urn:verself:problem:provider_webhook:" + suffix
}

func githubProblemType(suffix string) string {
	return "urn:verself:problem:github:" + suffix
}

var githubProblemCatalog = map[githubProblemCode]githubProblemDefinition{
	problemProviderWebhookMethodNotAllowed: {
		Type: providerWebhookProblemType("invalid_request"), Code: problemProviderWebhookMethodNotAllowed, Title: "Method not allowed", Detail: "GitHub webhooks must use POST.", Status: http.StatusMethodNotAllowed, Phase: "method_validation", DocsURL: githubProblemDocsURL(problemProviderWebhookMethodNotAllowed),
	},
	problemProviderWebhookHeaderInvalid: {
		Type: providerWebhookProblemType("invalid_request"), Code: problemProviderWebhookHeaderInvalid, Title: "Invalid webhook header", Detail: "A required GitHub webhook header is missing, duplicated, or malformed.", Status: http.StatusBadRequest, Phase: "header_validation", DocsURL: githubProblemDocsURL(problemProviderWebhookHeaderInvalid),
	},
	problemProviderWebhookBodyInvalid: {
		Type: providerWebhookProblemType("invalid_request"), Code: problemProviderWebhookBodyInvalid, Title: "Invalid webhook body", Detail: "The GitHub webhook body could not be read.", Status: http.StatusBadRequest, Phase: "body_read", DocsURL: githubProblemDocsURL(problemProviderWebhookBodyInvalid),
	},
	problemProviderWebhookSignatureInvalid: {
		Type: providerWebhookProblemType("signature_invalid"), Code: problemProviderWebhookSignatureInvalid, Title: "Invalid webhook signature", Detail: "The GitHub webhook signature did not validate against the configured secret.", Status: http.StatusUnauthorized, Phase: "signature_verification", DocsURL: githubProblemDocsURL(problemProviderWebhookSignatureInvalid),
	},
	problemProviderWebhookPayloadInvalid: {
		Type: providerWebhookProblemType("invalid_request"), Code: problemProviderWebhookPayloadInvalid, Title: "Invalid webhook payload", Detail: "The GitHub webhook payload could not be parsed or is missing required identity.", Status: http.StatusBadRequest, Phase: "payload_parse", DocsURL: githubProblemDocsURL(problemProviderWebhookPayloadInvalid),
	},
	problemProviderWebhookDeliveryReplayConflict: {
		Type: providerWebhookProblemType("delivery_replay_conflict"), Code: problemProviderWebhookDeliveryReplayConflict, Title: "Webhook delivery replay conflict", Detail: "GitHub reused a delivery id with a different payload hash.", Status: http.StatusConflict, Phase: "inbox_persist", DocsURL: githubProblemDocsURL(problemProviderWebhookDeliveryReplayConflict),
	},
	problemProviderWebhookInboxUnavailable: {
		Type: providerWebhookProblemType("inbox_unavailable"), Code: problemProviderWebhookInboxUnavailable, Title: "Webhook inbox unavailable", Detail: "Verself could not durably record the GitHub webhook delivery.", Status: http.StatusServiceUnavailable, Phase: "inbox_persist", Retryable: true, DocsURL: githubProblemDocsURL(problemProviderWebhookInboxUnavailable),
	},
	problemProviderWebhookUnsupportedEvent: {
		Type: providerWebhookProblemType("unsupported_event"), Code: problemProviderWebhookUnsupportedEvent, Title: "Unsupported webhook event", Detail: "Verself does not process this GitHub webhook event or action.", Status: http.StatusUnprocessableEntity, Phase: "delivery_dispatch", DocsURL: githubProblemDocsURL(problemProviderWebhookUnsupportedEvent),
	},
	problemProviderWebhookProcessingFailed: {
		Type: providerWebhookProblemType("processing_failed"), Code: problemProviderWebhookProcessingFailed, Title: "Webhook delivery processing failed", Detail: "Verself could not process a durable GitHub webhook delivery.", Status: http.StatusServiceUnavailable, Phase: "delivery_processing", Retryable: true, DocsURL: githubProblemDocsURL(problemProviderWebhookProcessingFailed),
	},
	problemProviderWebhookProcessingStale: {
		Type: providerWebhookProblemType("processing_stale"), Code: problemProviderWebhookProcessingStale, Title: "Webhook delivery processing was abandoned", Detail: "A worker left this GitHub webhook delivery in processing past its deadline.", Status: http.StatusServiceUnavailable, Phase: "delivery_processing", Retryable: true, DocsURL: githubProblemDocsURL(problemProviderWebhookProcessingStale),
	},
	problemProviderWebhookProcessingAttemptsExhausted: {
		Type: providerWebhookProblemType("processing_attempts_exhausted"), Code: problemProviderWebhookProcessingAttemptsExhausted, Title: "Webhook delivery retry budget exhausted", Detail: "The GitHub webhook delivery exceeded its processing retry budget.", Status: http.StatusServiceUnavailable, Phase: "delivery_processing", DocsURL: githubProblemDocsURL(problemProviderWebhookProcessingAttemptsExhausted),
	},
	problemGithubRepositoryNotEnabled: {
		Type: githubProblemType("repository_not_enabled"), Code: problemGithubRepositoryNotEnabled, Title: "GitHub repository is not enabled", Detail: "This repository is not enabled for Verself CI in the owning organization.", Status: http.StatusConflict, Phase: "repository_binding", DocsURL: githubProblemDocsURL(problemGithubRepositoryNotEnabled),
	},
	problemGithubSandboxDispatchFailed: {
		Type: githubProblemType("sandbox_dispatch_failed"), Code: problemGithubSandboxDispatchFailed, Title: "Sandbox dispatch failed", Detail: "Verself could not dispatch the GitHub job to sandbox-rental-service.", Status: http.StatusServiceUnavailable, Phase: "sandbox_dispatch", Retryable: true, DocsURL: githubProblemDocsURL(problemGithubSandboxDispatchFailed),
	},
	problemGithubRunnerCapacityFailed: {
		Type: githubRunnerProblemType(problemGithubRunnerCapacityFailed), Code: problemGithubRunnerCapacityFailed, Title: "GitHub runner capacity failed", Detail: "Verself could not create runner capacity for the queued GitHub job.", Phase: "runner_capacity", DocsURL: githubProblemDocsURL(problemGithubRunnerCapacityFailed),
	},
	problemGithubRunnerDemandRecordFailed:                runnerProblemDefinition(problemGithubRunnerDemandRecordFailed, "GitHub provider demand record failed", "Verself could not persist GitHub job demand before allocating runner capacity.", "provider_demand", true),
	problemGithubRunnerWorkflowRunFetchFailed:            runnerProblemDefinition(problemGithubRunnerWorkflowRunFetchFailed, "GitHub workflow run refresh failed", "Verself could not refresh the GitHub workflow run before allocating runner capacity.", "provider_refresh", true),
	problemGithubRunnerWorkflowRunPersistFailed:          runnerProblemDefinition(problemGithubRunnerWorkflowRunPersistFailed, "GitHub workflow run persist failed", "Verself could not persist refreshed GitHub workflow run evidence.", "provider_refresh", true),
	problemGithubRunnerWorkflowJobFetchFailed:            runnerProblemDefinition(problemGithubRunnerWorkflowJobFetchFailed, "GitHub workflow job refresh failed", "Verself could not refresh the GitHub workflow job before allocating runner capacity.", "provider_refresh", true),
	problemGithubRunnerWorkflowJobNotFound:               runnerProblemDefinition(problemGithubRunnerWorkflowJobNotFound, "GitHub workflow job was not found", "GitHub did not return the queued workflow job for the expected run attempt.", "provider_refresh", false),
	problemGithubRunnerWorkflowJobPersistFailed:          runnerProblemDefinition(problemGithubRunnerWorkflowJobPersistFailed, "GitHub workflow job persist failed", "Verself could not persist refreshed GitHub workflow job evidence.", "provider_refresh", true),
	problemGithubRunnerCacheManifestFetchFailed:          runnerProblemDefinition(problemGithubRunnerCacheManifestFetchFailed, "GitHub cache manifest fetch failed", "Verself could not fetch the repository cache manifest for this workflow.", "cache_manifest", true),
	problemGithubRunnerJobShapeBuildFailed:               runnerProblemDefinition(problemGithubRunnerJobShapeBuildFailed, "GitHub job shape build failed", "Verself could not derive a stable job shape for this GitHub job.", "job_shape", false),
	problemGithubRunnerJobShapePersistFailed:             runnerProblemDefinition(problemGithubRunnerJobShapePersistFailed, "GitHub job shape persist failed", "Verself could not persist the derived GitHub job shape.", "job_shape", true),
	problemGithubRunnerDemandUpdateFailed:                runnerProblemDefinition(problemGithubRunnerDemandUpdateFailed, "GitHub provider demand update failed", "Verself could not update GitHub provider demand state.", "provider_demand", true),
	problemGithubRunnerRunnerClassLockFailed:             runnerProblemDefinition(problemGithubRunnerRunnerClassLockFailed, "GitHub runner class lock failed", "Verself could not acquire the repository runner-class scheduling lock.", "runner_class_lock", true),
	problemGithubRunnerRunnerClassCapacityCountFailed:    runnerProblemDefinition(problemGithubRunnerRunnerClassCapacityCountFailed, "GitHub runner class capacity count failed", "Verself could not count active runner capacity for this repository runner class.", "runner_class_capacity", true),
	problemGithubRunnerDemandClaimFailed:                 runnerProblemDefinition(problemGithubRunnerDemandClaimFailed, "GitHub provider demand claim failed", "Verself could not claim the GitHub job demand for runner capacity allocation.", "provider_demand", true),
	problemGithubRunnerRunnerNameFailed:                  runnerProblemDefinition(problemGithubRunnerRunnerNameFailed, "GitHub runner name generation failed", "Verself could not generate a valid GitHub runner name.", "runner_name", false),
	problemGithubRunnerJITConfigFailed:                   runnerProblemDefinition(problemGithubRunnerJITConfigFailed, "GitHub runner JIT config failed", "Verself could not create a GitHub JIT runner configuration.", "jit_config", false),
	problemGithubRunnerJITConfigInvalid:                  runnerProblemDefinition(problemGithubRunnerJITConfigInvalid, "GitHub runner JIT config was incomplete", "GitHub returned an incomplete JIT runner configuration.", "jit_config", false),
	problemGithubRunnerInstancePersistFailed:             runnerProblemDefinition(problemGithubRunnerInstancePersistFailed, "GitHub runner instance persist failed", "Verself could not persist the GitHub runner instance.", "runner_instance", false),
	problemGithubRunnerSandboxWorkflowObserveFailed:      runnerProblemDefinition(problemGithubRunnerSandboxWorkflowObserveFailed, "Sandbox workflow observation failed", "Verself could not send workflow-run evidence to sandbox-rental-service.", "sandbox_observe_workflow", true),
	problemGithubRunnerSandboxOutboxFailed:               runnerProblemDefinition(problemGithubRunnerSandboxOutboxFailed, "Sandbox runner submit outbox failed", "Verself could not record the sandbox submit outbox command.", "sandbox_outbox", false),
	problemGithubRunnerSandboxSubmitFailed:               runnerProblemDefinition(problemGithubRunnerSandboxSubmitFailed, "Sandbox runner submit failed", "Verself could not submit the GitHub runner job to sandbox-rental-service.", "sandbox_submit", false),
	problemGithubRunnerSandboxRejected:                   runnerProblemDefinition(problemGithubRunnerSandboxRejected, "Sandbox runner submit was rejected", "sandbox-rental-service rejected the GitHub runner job submission.", "sandbox_submit", false),
	problemGithubRunnerSandboxResponseInvalid:            runnerProblemDefinition(problemGithubRunnerSandboxResponseInvalid, "Sandbox runner submit response was invalid", "sandbox-rental-service returned an invalid runner submission response.", "sandbox_submit", false),
	problemGithubRunnerSubmissionPersistFailed:           runnerProblemDefinition(problemGithubRunnerSubmissionPersistFailed, "Sandbox runner submission persist failed", "Verself could not persist sandbox runner submission evidence.", "sandbox_submit", false),
	problemGithubRunnerSandboxOutboxCompleteFailed:       runnerProblemDefinition(problemGithubRunnerSandboxOutboxCompleteFailed, "Sandbox runner submit outbox completion failed", "Verself could not mark the sandbox submit outbox command complete.", "sandbox_outbox", false),
	problemGithubRunnerSubmissionCommitFailed:            runnerProblemDefinition(problemGithubRunnerSubmissionCommitFailed, "Sandbox runner submission commit failed", "Verself could not commit sandbox runner submission state.", "sandbox_submit", true),
	problemGithubRunnerSandboxAllocationTerminal:         runnerProblemDefinition(problemGithubRunnerSandboxAllocationTerminal, "GitHub runner capacity failed before assignment", "The sandbox allocation became terminal before GitHub assigned the runner.", "sandbox_reconcile", false),
	problemGithubRunnerGuestTimeSyncPrecisionGateFailed:  runnerProblemDefinition(problemGithubRunnerGuestTimeSyncPrecisionGateFailed, "Guest time sync precision gate failed", "Verself could not prove the restored runner VM clock was synchronized closely enough to the host PTP source before starting the GitHub job.", "guest_time_sync", false),
	problemGithubRunnerSandboxAllocationNotFound:         runnerProblemDefinition(problemGithubRunnerSandboxAllocationNotFound, "GitHub runner capacity failed before assignment", "sandbox-rental-service no longer has the expected runner allocation.", "sandbox_reconcile", false),
	problemGithubRunnerSandboxSubmitNotObserved:          runnerProblemDefinition(problemGithubRunnerSandboxSubmitNotObserved, "GitHub runner capacity failed before assignment", "Verself did not observe sandbox submission before the runner assignment deadline.", "sandbox_submit", false),
	problemGithubRunnerAssignmentDeadlineExceeded:        runnerProblemDefinition(problemGithubRunnerAssignmentDeadlineExceeded, "GitHub runner capacity failed before assignment", "GitHub did not assign the runner before the assignment deadline.", "assignment_wait", false),
	problemGithubRunnerRunnerCapacityDisplaced:           runnerProblemDefinition(problemGithubRunnerRunnerCapacityDisplaced, "Created GitHub runner capacity was displaced", "The runner capacity created by Verself was displaced by an existing sandbox runner.", "sandbox_submit", false),
	problemGithubRunnerCapacityDisplaced:                 runnerProblemDefinition(problemGithubRunnerCapacityDisplaced, "GitHub runner capacity was assigned to a different job", "GitHub assigned runner capacity to a different compatible job.", "assignment", true),
	problemGithubRunnerProviderSurfaceFailed:             runnerProblemDefinition(problemGithubRunnerProviderSurfaceFailed, "Failed to surface runner failure to GitHub", "Verself could not surface the runner failure back to GitHub.", "provider_surface", true),
	problemGithubRunnerProviderSurfaceWorkerLost:         runnerProblemDefinition(problemGithubRunnerProviderSurfaceWorkerLost, "GitHub provider surface worker lost command", "A provider-surface worker left a command running past its deadline.", "provider_surface", true),
	problemGithubRunnerProviderSurfaceAttemptsExhausted:  runnerProblemDefinition(problemGithubRunnerProviderSurfaceAttemptsExhausted, "GitHub provider surface retry budget exhausted", "The provider-surface command exceeded its retry budget.", "provider_surface", false),
	problemGithubRunnerProviderSurfaceCommandUnsupported: runnerProblemDefinition(problemGithubRunnerProviderSurfaceCommandUnsupported, "Unsupported GitHub provider surface command", "Verself encountered an unsupported provider-surface command kind.", "provider_surface", false),
	problemGithubRunnerProviderSurfaceTargetMissing:      runnerProblemDefinition(problemGithubRunnerProviderSurfaceTargetMissing, "GitHub provider surface target missing", "The provider-surface command is missing the GitHub workflow-run identity needed to update GitHub.", "provider_surface", false),
}

func runnerProblemDefinition(code githubProblemCode, title string, detail string, phase string, retryable bool) githubProblemDefinition {
	return githubProblemDefinition{
		Type:      githubRunnerProblemType(code),
		Code:      code,
		Title:     title,
		Detail:    detail,
		Phase:     phase,
		Retryable: retryable,
		DocsURL:   githubProblemDocsURL(code),
	}
}
