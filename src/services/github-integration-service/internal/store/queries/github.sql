-- name: RecordWebhookDelivery :one
INSERT INTO github_webhook_deliveries (
    delivery_id,
    event_name,
    action,
    state,
    payload_sha256,
    payload_json,
    provider_installation_id,
    provider_repository_id,
    repository_full_name,
    provider_run_id,
    provider_run_attempt,
    provider_job_id,
    received_at,
    verified_at,
    next_attempt_at,
    updated_at
) VALUES (
    @delivery_id,
    @event_name,
    @action,
    'verified',
    @payload_sha256,
    @payload_json,
    @provider_installation_id,
    @provider_repository_id,
    @repository_full_name,
    @provider_run_id,
    @provider_run_attempt,
    @provider_job_id,
    @received_at,
    @verified_at,
    @received_at,
    @received_at
)
ON CONFLICT (delivery_id) DO UPDATE SET
    state = CASE
        WHEN github_webhook_deliveries.state = 'rejected' THEN EXCLUDED.state
        ELSE github_webhook_deliveries.state
    END,
    event_name = CASE
        WHEN github_webhook_deliveries.state = 'rejected' THEN EXCLUDED.event_name
        ELSE github_webhook_deliveries.event_name
    END,
    action = CASE
        WHEN github_webhook_deliveries.state = 'rejected' THEN EXCLUDED.action
        ELSE github_webhook_deliveries.action
    END,
    failure_reason = CASE
        WHEN github_webhook_deliveries.state = 'rejected' THEN ''
        ELSE github_webhook_deliveries.failure_reason
    END,
    payload_json = CASE
        WHEN github_webhook_deliveries.state = 'rejected' THEN EXCLUDED.payload_json
        ELSE github_webhook_deliveries.payload_json
    END,
    provider_installation_id = CASE
        WHEN github_webhook_deliveries.state = 'rejected' THEN EXCLUDED.provider_installation_id
        ELSE github_webhook_deliveries.provider_installation_id
    END,
    provider_repository_id = CASE
        WHEN github_webhook_deliveries.state = 'rejected' THEN EXCLUDED.provider_repository_id
        ELSE github_webhook_deliveries.provider_repository_id
    END,
    repository_full_name = CASE
        WHEN github_webhook_deliveries.state = 'rejected' THEN EXCLUDED.repository_full_name
        ELSE github_webhook_deliveries.repository_full_name
    END,
    provider_run_id = CASE
        WHEN github_webhook_deliveries.state = 'rejected' THEN EXCLUDED.provider_run_id
        ELSE github_webhook_deliveries.provider_run_id
    END,
    provider_run_attempt = CASE
        WHEN github_webhook_deliveries.state = 'rejected' THEN EXCLUDED.provider_run_attempt
        ELSE github_webhook_deliveries.provider_run_attempt
    END,
    provider_job_id = CASE
        WHEN github_webhook_deliveries.state = 'rejected' THEN EXCLUDED.provider_job_id
        ELSE github_webhook_deliveries.provider_job_id
    END,
    verified_at = CASE
        WHEN github_webhook_deliveries.state = 'rejected' THEN EXCLUDED.verified_at
        ELSE github_webhook_deliveries.verified_at
    END,
    next_attempt_at = CASE
        WHEN github_webhook_deliveries.state = 'rejected' THEN EXCLUDED.next_attempt_at
        ELSE github_webhook_deliveries.next_attempt_at
    END,
    updated_at = EXCLUDED.updated_at
WHERE github_webhook_deliveries.payload_sha256 = EXCLUDED.payload_sha256
RETURNING delivery_id, state, attempt_count, payload_sha256;

-- name: MarkDeliveryRejected :exec
INSERT INTO github_webhook_deliveries (
    delivery_id,
    event_name,
    action,
    state,
    failure_reason,
    payload_sha256,
    payload_json,
    received_at,
    verified_at,
    updated_at
) VALUES (
    @delivery_id,
    @event_name,
    @action,
    'rejected',
    @failure_reason,
    @payload_sha256,
    @payload_json,
    @received_at,
    @received_at,
    @received_at
)
ON CONFLICT (delivery_id) DO UPDATE SET
    state = 'rejected',
    failure_reason = EXCLUDED.failure_reason,
    updated_at = EXCLUDED.updated_at
WHERE github_webhook_deliveries.state = 'rejected'
  AND github_webhook_deliveries.payload_sha256 = EXCLUDED.payload_sha256;

-- name: LockReadyDeliveries :many
UPDATE github_webhook_deliveries AS d
SET
    state = 'processing',
    attempt_count = d.attempt_count + 1,
    processing_started_at = @locked_at,
    updated_at = @locked_at
FROM (
    SELECT delivery_id
    FROM github_webhook_deliveries
    WHERE state IN ('verified', 'retryable')
      AND (next_attempt_at IS NULL OR next_attempt_at <= @locked_at)
    ORDER BY received_at
    LIMIT @limit_count
    FOR UPDATE SKIP LOCKED
) AS ready
WHERE d.delivery_id = ready.delivery_id
RETURNING d.delivery_id, d.event_name, d.action, d.payload_json, d.provider_installation_id,
          d.provider_repository_id, d.repository_full_name, d.provider_run_id,
          d.provider_run_attempt, d.provider_job_id, d.attempt_count;

-- name: MarkDeliveryProcessed :exec
UPDATE github_webhook_deliveries
SET state = 'processed',
    failure_reason = '',
    processed_at = @processed_at,
    updated_at = @processed_at
WHERE delivery_id = @delivery_id;

-- name: MarkDeliveryIgnored :exec
UPDATE github_webhook_deliveries
SET state = 'ignored',
    failure_reason = @failure_reason,
    processed_at = @processed_at,
    updated_at = @processed_at
WHERE delivery_id = @delivery_id;

-- name: MarkDeliveryRetryable :exec
UPDATE github_webhook_deliveries
SET state = 'retryable',
    failure_reason = @failure_reason,
    next_attempt_at = @next_attempt_at,
    updated_at = @updated_at
WHERE delivery_id = @delivery_id;

-- name: MarkDeliveryFailed :exec
UPDATE github_webhook_deliveries
SET state = 'failed',
    failure_reason = @failure_reason,
    processed_at = @failed_at,
    updated_at = @failed_at
WHERE delivery_id = @delivery_id;

-- name: UpsertWorkflowRun :exec
INSERT INTO github_workflow_runs (
    provider_installation_id,
    provider_repository_id,
    provider_run_id,
    provider_run_attempt,
    repository_full_name,
    event_name,
    head_sha,
    head_branch,
    head_repository_full_name,
    base_sha,
    base_branch,
    workflow_path,
    pull_request_number,
    commit_count,
    last_delivery_id,
    observed_from_api_at,
    updated_at
) VALUES (
    @provider_installation_id,
    @provider_repository_id,
    @provider_run_id,
    @provider_run_attempt,
    @repository_full_name,
    @event_name,
    @head_sha,
    @head_branch,
    @head_repository_full_name,
    @base_sha,
    @base_branch,
    @workflow_path,
    @pull_request_number,
    @commit_count,
    @last_delivery_id,
    @observed_from_api_at,
    @updated_at
)
ON CONFLICT (provider_installation_id, provider_repository_id, provider_run_id, provider_run_attempt) DO UPDATE SET
    repository_full_name = EXCLUDED.repository_full_name,
    event_name = EXCLUDED.event_name,
    head_sha = EXCLUDED.head_sha,
    head_branch = EXCLUDED.head_branch,
    head_repository_full_name = EXCLUDED.head_repository_full_name,
    base_sha = EXCLUDED.base_sha,
    base_branch = EXCLUDED.base_branch,
    workflow_path = EXCLUDED.workflow_path,
    pull_request_number = EXCLUDED.pull_request_number,
    commit_count = EXCLUDED.commit_count,
    last_delivery_id = EXCLUDED.last_delivery_id,
    observed_from_api_at = EXCLUDED.observed_from_api_at,
    updated_at = EXCLUDED.updated_at;

-- name: UpsertWorkflowJob :exec
INSERT INTO github_workflow_jobs (
    provider_job_id,
    provider_installation_id,
    provider_repository_id,
    repository_full_name,
    provider_run_id,
    provider_run_attempt,
    job_name,
    head_sha,
    head_branch,
    workflow_name,
    status,
    conclusion,
    labels_json,
    runner_id,
    runner_name,
    started_at,
    completed_at,
    last_delivery_id,
    observed_from_api_at,
    updated_at
) VALUES (
    @provider_job_id,
    @provider_installation_id,
    @provider_repository_id,
    @repository_full_name,
    @provider_run_id,
    @provider_run_attempt,
    @job_name,
    @head_sha,
    @head_branch,
    @workflow_name,
    @status,
    @conclusion,
    @labels_json,
    @runner_id,
    @runner_name,
    @started_at,
    @completed_at,
    @last_delivery_id,
    @observed_from_api_at,
    @updated_at
)
ON CONFLICT (provider_job_id) DO UPDATE SET
    provider_installation_id = EXCLUDED.provider_installation_id,
    provider_repository_id = EXCLUDED.provider_repository_id,
    repository_full_name = EXCLUDED.repository_full_name,
    provider_run_id = EXCLUDED.provider_run_id,
    provider_run_attempt = EXCLUDED.provider_run_attempt,
    job_name = EXCLUDED.job_name,
    head_sha = EXCLUDED.head_sha,
    head_branch = EXCLUDED.head_branch,
    workflow_name = EXCLUDED.workflow_name,
    status = EXCLUDED.status,
    conclusion = EXCLUDED.conclusion,
    labels_json = EXCLUDED.labels_json,
    runner_id = EXCLUDED.runner_id,
    runner_name = EXCLUDED.runner_name,
    started_at = EXCLUDED.started_at,
    completed_at = EXCLUDED.completed_at,
    last_delivery_id = EXCLUDED.last_delivery_id,
    observed_from_api_at = EXCLUDED.observed_from_api_at,
    updated_at = EXCLUDED.updated_at;

-- name: UpsertRunnerRegistration :exec
INSERT INTO github_runner_registrations (
    provider_job_id,
    provider_installation_id,
    provider_repository_id,
    runner_id,
    runner_name,
    runner_class,
    jit_config_sha256,
    state,
    updated_at
) VALUES (
    @provider_job_id,
    @provider_installation_id,
    @provider_repository_id,
    @runner_id,
    @runner_name,
    @runner_class,
    @jit_config_sha256,
    @state,
    @updated_at
)
ON CONFLICT (provider_job_id) DO UPDATE SET
    runner_id = EXCLUDED.runner_id,
    runner_name = EXCLUDED.runner_name,
    runner_class = EXCLUDED.runner_class,
    jit_config_sha256 = EXCLUDED.jit_config_sha256,
    state = EXCLUDED.state,
    failure_reason = '',
    updated_at = EXCLUDED.updated_at;

-- name: MarkRunnerRegistrationSubmitted :exec
UPDATE github_runner_registrations
SET sandbox_allocation_id = @sandbox_allocation_id,
    sandbox_execution_id = @sandbox_execution_id,
    sandbox_attempt_id = @sandbox_attempt_id,
    runner_id = @runner_id,
    runner_name = @runner_name,
    state = 'sandbox_submitted',
    failure_reason = '',
    updated_at = @updated_at
WHERE provider_job_id = @provider_job_id;

-- name: MarkRunnerRegistrationFailed :exec
UPDATE github_runner_registrations
SET state = 'failed',
    failure_reason = @failure_reason,
    updated_at = @updated_at
WHERE provider_job_id = @provider_job_id;

-- name: MarkRunnerRegistrationCleaned :exec
UPDATE github_runner_registrations
SET state = 'cleaned',
    updated_at = @updated_at
WHERE provider_job_id = @provider_job_id;

-- name: GetRunnerRegistrationForJob :one
SELECT provider_job_id, provider_installation_id, provider_repository_id, runner_id,
       runner_name, runner_class, sandbox_allocation_id, sandbox_execution_id,
       sandbox_attempt_id, state
FROM github_runner_registrations
WHERE provider_job_id = @provider_job_id;

-- name: InsertTerminalJobEvidence :exec
INSERT INTO github_terminal_job_evidence (
    terminal_evidence_id,
    provider_job_id,
    provider_run_id,
    provider_run_attempt,
    status,
    conclusion,
    source,
    delivery_id,
    observed_at
) VALUES (
    @terminal_evidence_id,
    @provider_job_id,
    @provider_run_id,
    @provider_run_attempt,
    @status,
    @conclusion,
    @source,
    @delivery_id,
    @observed_at
)
ON CONFLICT (provider_job_id) DO UPDATE SET
    status = EXCLUDED.status,
    conclusion = EXCLUDED.conclusion,
    source = EXCLUDED.source,
    delivery_id = EXCLUDED.delivery_id,
    observed_at = EXCLUDED.observed_at;
