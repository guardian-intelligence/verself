export type GithubIntegrationProblem = {
  readonly code: string;
  readonly type: string;
  readonly title: string;
  readonly status: number | null;
  readonly phase: string;
  readonly retryable: boolean;
  readonly message: string;
};

export type GithubIntegrationProblemGroup = {
  readonly id: string;
  readonly label: string;
  readonly description: string;
  readonly problems: readonly GithubIntegrationProblem[];
};

const ERROR_BASE = "/docs/reference/github-integration/errors";

export function githubIntegrationProblemAnchor(code: string): string {
  return code.replaceAll(/[._]/g, "-");
}

export function githubIntegrationProblemPath(code: string): string {
  return `${ERROR_BASE}#${githubIntegrationProblemAnchor(code)}`;
}

const webhook = (
  code: string,
  title: string,
  status: number,
  phase: string,
  retryable: boolean,
  message: string,
  type = code.replace(".", ":"),
): GithubIntegrationProblem => ({
  code,
  type: `urn:verself:problem:${type}`,
  title,
  status,
  phase,
  retryable,
  message,
});

const runner = (
  code: string,
  title: string,
  phase: string,
  retryable: boolean,
  message: string,
): GithubIntegrationProblem => ({
  code,
  type: `urn:verself:problem:github-runner:${code
    .replace("github_runner.", "")
    .replaceAll(".", "_")}`,
  title,
  status: null,
  phase,
  retryable,
  message,
});

export const GITHUB_INTEGRATION_PROBLEM_GROUPS: readonly GithubIntegrationProblemGroup[] = [
  {
    id: "webhook-ingress",
    label: "Webhook Ingress",
    description:
      "Problems raised while authenticating, validating, or durably accepting GitHub webhooks.",
    problems: [
      webhook(
        "provider_webhook.method_not_allowed",
        "Method not allowed",
        405,
        "method_validation",
        false,
        "GitHub webhooks must use POST.",
        "provider_webhook:invalid_request",
      ),
      webhook(
        "provider_webhook.header_invalid",
        "Invalid webhook header",
        400,
        "header_validation",
        false,
        "A required GitHub webhook header is missing, duplicated, or malformed.",
        "provider_webhook:invalid_request",
      ),
      webhook(
        "provider_webhook.body_invalid",
        "Invalid webhook body",
        400,
        "body_read",
        false,
        "The GitHub webhook body could not be read.",
        "provider_webhook:invalid_request",
      ),
      webhook(
        "provider_webhook.signature_invalid",
        "Invalid webhook signature",
        401,
        "signature_verification",
        false,
        "The GitHub webhook signature did not validate against the configured secret.",
      ),
      webhook(
        "provider_webhook.payload_invalid",
        "Invalid webhook payload",
        400,
        "payload_parse",
        false,
        "The GitHub webhook payload could not be parsed or is missing required identity.",
        "provider_webhook:invalid_request",
      ),
      webhook(
        "provider_webhook.delivery_replay_conflict",
        "Webhook delivery replay conflict",
        409,
        "inbox_persist",
        false,
        "GitHub reused a delivery id with a different payload hash.",
      ),
      webhook(
        "provider_webhook.inbox_unavailable",
        "Webhook inbox unavailable",
        503,
        "inbox_persist",
        true,
        "Verself could not durably record the GitHub webhook delivery.",
      ),
    ],
  },
  {
    id: "delivery-processing",
    label: "Delivery Processing",
    description: "Problems raised after a webhook is accepted into the durable delivery ledger.",
    problems: [
      webhook(
        "provider_webhook.unsupported_event",
        "Unsupported webhook event",
        422,
        "delivery_dispatch",
        false,
        "Verself does not process this GitHub webhook event or action.",
      ),
      webhook(
        "provider_webhook.processing_failed",
        "Webhook delivery processing failed",
        503,
        "delivery_processing",
        true,
        "Verself could not process a durable GitHub webhook delivery.",
      ),
      webhook(
        "provider_webhook.processing_stale",
        "Webhook delivery processing was abandoned",
        503,
        "delivery_processing",
        true,
        "A worker left this GitHub webhook delivery in processing past its deadline.",
      ),
      webhook(
        "provider_webhook.processing_attempts_exhausted",
        "Webhook delivery retry budget exhausted",
        503,
        "delivery_processing",
        false,
        "The GitHub webhook delivery exceeded its processing retry budget.",
      ),
      webhook(
        "github.repository_not_enabled",
        "GitHub repository is not enabled",
        409,
        "repository_binding",
        false,
        "This repository is not enabled for Verself CI in the owning organization.",
        "github:repository_not_enabled",
      ),
      webhook(
        "github.sandbox_dispatch_failed",
        "Sandbox dispatch failed",
        503,
        "sandbox_dispatch",
        true,
        "Verself could not dispatch the GitHub job to sandbox-rental-service.",
        "github:sandbox_dispatch_failed",
      ),
    ],
  },
  {
    id: "runner-capacity",
    label: "Runner Capacity",
    description:
      "Problems raised while translating queued GitHub jobs into runner capacity and sandbox submissions.",
    problems: [
      runner(
        "github_runner.capacity_failed",
        "GitHub runner capacity failed",
        "runner_capacity",
        false,
        "Verself could not create runner capacity for the queued GitHub job.",
      ),
      runner(
        "github_runner.demand_record_failed",
        "GitHub provider demand record failed",
        "provider_demand",
        true,
        "Verself could not persist GitHub job demand before allocating runner capacity.",
      ),
      runner(
        "github_runner.workflow_run_fetch_failed",
        "GitHub workflow run refresh failed",
        "provider_refresh",
        true,
        "Verself could not refresh the GitHub workflow run before allocating runner capacity.",
      ),
      runner(
        "github_runner.workflow_run_persist_failed",
        "GitHub workflow run persist failed",
        "provider_refresh",
        true,
        "Verself could not persist refreshed GitHub workflow run evidence.",
      ),
      runner(
        "github_runner.workflow_job_fetch_failed",
        "GitHub workflow job refresh failed",
        "provider_refresh",
        true,
        "Verself could not refresh the GitHub workflow job before allocating runner capacity.",
      ),
      runner(
        "github_runner.workflow_job_not_found",
        "GitHub workflow job was not found",
        "provider_refresh",
        false,
        "GitHub did not return the queued workflow job for the expected run attempt.",
      ),
      runner(
        "github_runner.workflow_job_persist_failed",
        "GitHub workflow job persist failed",
        "provider_refresh",
        true,
        "Verself could not persist refreshed GitHub workflow job evidence.",
      ),
      runner(
        "github_runner.cache_manifest_fetch_failed",
        "GitHub cache manifest fetch failed",
        "cache_manifest",
        true,
        "Verself could not fetch the repository cache manifest for this workflow.",
      ),
      runner(
        "github_runner.job_shape_build_failed",
        "GitHub job shape build failed",
        "job_shape",
        false,
        "Verself could not derive a stable job shape for this GitHub job.",
      ),
      runner(
        "github_runner.job_shape_persist_failed",
        "GitHub job shape persist failed",
        "job_shape",
        true,
        "Verself could not persist the derived GitHub job shape.",
      ),
      runner(
        "github_runner.demand_update_failed",
        "GitHub provider demand update failed",
        "provider_demand",
        true,
        "Verself could not update GitHub provider demand state.",
      ),
      runner(
        "github_runner.runner_class_lock_failed",
        "GitHub runner class lock failed",
        "runner_class_lock",
        true,
        "Verself could not acquire the repository runner-class scheduling lock.",
      ),
      runner(
        "github_runner.runner_class_capacity_count_failed",
        "GitHub runner class capacity count failed",
        "runner_class_capacity",
        true,
        "Verself could not count active runner capacity for this repository runner class.",
      ),
      runner(
        "github_runner.demand_claim_failed",
        "GitHub provider demand claim failed",
        "provider_demand",
        true,
        "Verself could not claim the GitHub job demand for runner capacity allocation.",
      ),
      runner(
        "github_runner.runner_name_failed",
        "GitHub runner name generation failed",
        "runner_name",
        false,
        "Verself could not generate a valid GitHub runner name.",
      ),
      runner(
        "github_runner.jit_config_failed",
        "GitHub runner JIT config failed",
        "jit_config",
        false,
        "Verself could not create a GitHub JIT runner configuration.",
      ),
      runner(
        "github_runner.jit_config_invalid",
        "GitHub runner JIT config was incomplete",
        "jit_config",
        false,
        "GitHub returned an incomplete JIT runner configuration.",
      ),
      runner(
        "github_runner.instance_persist_failed",
        "GitHub runner instance persist failed",
        "runner_instance",
        false,
        "Verself could not persist the GitHub runner instance.",
      ),
      runner(
        "github_runner.sandbox_workflow_observe_failed",
        "Sandbox workflow observation failed",
        "sandbox_observe_workflow",
        true,
        "Verself could not send workflow-run evidence to sandbox-rental-service.",
      ),
      runner(
        "github_runner.sandbox_outbox_failed",
        "Sandbox runner submit outbox failed",
        "sandbox_outbox",
        false,
        "Verself could not record the sandbox submit outbox command.",
      ),
      runner(
        "github_runner.sandbox_submit_failed",
        "Sandbox runner submit failed",
        "sandbox_submit",
        false,
        "Verself could not submit the GitHub runner job to sandbox-rental-service.",
      ),
      runner(
        "github_runner.sandbox_rejected",
        "Sandbox runner submit was rejected",
        "sandbox_submit",
        false,
        "sandbox-rental-service rejected the GitHub runner job submission.",
      ),
      runner(
        "github_runner.sandbox_response_invalid",
        "Sandbox runner submit response was invalid",
        "sandbox_submit",
        false,
        "sandbox-rental-service returned an invalid runner submission response.",
      ),
      runner(
        "github_runner.submission_persist_failed",
        "Sandbox runner submission persist failed",
        "sandbox_submit",
        false,
        "Verself could not persist sandbox runner submission evidence.",
      ),
      runner(
        "github_runner.sandbox_outbox_complete_failed",
        "Sandbox runner submit outbox completion failed",
        "sandbox_outbox",
        false,
        "Verself could not mark the sandbox submit outbox command complete.",
      ),
      runner(
        "github_runner.submission_commit_failed",
        "Sandbox runner submission commit failed",
        "sandbox_submit",
        true,
        "Verself could not commit sandbox runner submission state.",
      ),
      runner(
        "github_runner.sandbox_allocation_terminal",
        "GitHub runner capacity failed before assignment",
        "sandbox_reconcile",
        false,
        "The sandbox allocation became terminal before GitHub assigned the runner.",
      ),
      runner(
        "github_runner.guest_time_sync_precision_gate_failed",
        "Guest time sync precision gate failed",
        "guest_time_sync",
        false,
        "Verself could not prove the restored runner VM clock was synchronized closely enough to the host PTP source before starting the GitHub job.",
      ),
      runner(
        "github_runner.sandbox_allocation_not_found",
        "GitHub runner capacity failed before assignment",
        "sandbox_reconcile",
        false,
        "sandbox-rental-service no longer has the expected runner allocation.",
      ),
      runner(
        "github_runner.sandbox_submit_not_observed",
        "GitHub runner capacity failed before assignment",
        "sandbox_submit",
        false,
        "Verself did not observe sandbox submission before the runner assignment deadline.",
      ),
      runner(
        "github_runner.assignment_deadline_exceeded",
        "GitHub runner capacity failed before assignment",
        "assignment_wait",
        false,
        "GitHub did not assign the runner before the assignment deadline.",
      ),
      runner(
        "github_runner.runner_capacity_displaced",
        "Created GitHub runner capacity was displaced",
        "sandbox_submit",
        false,
        "The runner capacity created by Verself was displaced by an existing sandbox runner.",
      ),
      runner(
        "github_runner.capacity_displaced",
        "GitHub runner capacity was assigned to a different job",
        "assignment",
        true,
        "GitHub assigned runner capacity to a different compatible job.",
      ),
    ],
  },
  {
    id: "provider-surface",
    label: "Provider Surface",
    description:
      "Problems raised while surfacing Verself failures back to GitHub so Actions runs do not stay pending.",
    problems: [
      runner(
        "github_runner.provider_surface_failed",
        "Failed to surface runner failure to GitHub",
        "provider_surface",
        true,
        "Verself could not surface the runner failure back to GitHub.",
      ),
      runner(
        "github_runner.provider_surface_worker_lost",
        "GitHub provider surface worker lost command",
        "provider_surface",
        true,
        "A provider-surface worker left a command running past its deadline.",
      ),
      runner(
        "github_runner.provider_surface_attempts_exhausted",
        "GitHub provider surface retry budget exhausted",
        "provider_surface",
        false,
        "The provider-surface command exceeded its retry budget.",
      ),
      runner(
        "github_runner.provider_surface_command_unsupported",
        "Unsupported GitHub provider surface command",
        "provider_surface",
        false,
        "Verself encountered an unsupported provider-surface command kind.",
      ),
      runner(
        "github_runner.provider_surface_target_missing",
        "GitHub provider surface target missing",
        "provider_surface",
        false,
        "The provider-surface command is missing the GitHub workflow-run identity needed to update GitHub.",
      ),
    ],
  },
];

export const GITHUB_INTEGRATION_PROBLEMS = GITHUB_INTEGRATION_PROBLEM_GROUPS.flatMap(
  (group) => group.problems,
);
