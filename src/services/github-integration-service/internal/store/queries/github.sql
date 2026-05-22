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

-- name: UpsertInstallation :exec
INSERT INTO github_installations (
    provider_installation_id,
    state,
    last_event_delivery_id,
    updated_at
) VALUES (
    @provider_installation_id,
    @state,
    @last_event_delivery_id,
    @updated_at
)
ON CONFLICT (provider_installation_id) DO UPDATE SET
    state = EXCLUDED.state,
    last_event_delivery_id = COALESCE(NULLIF(EXCLUDED.last_event_delivery_id, ''), github_installations.last_event_delivery_id),
    updated_at = EXCLUDED.updated_at;

-- name: UpsertRepository :exec
INSERT INTO github_repositories (
    provider_repository_id,
    provider_installation_id,
    owner_login,
    repository_name,
    repository_full_name,
    state,
    last_event_delivery_id,
    updated_at
) VALUES (
    @provider_repository_id,
    @provider_installation_id,
    @owner_login,
    @repository_name,
    @repository_full_name,
    @state,
    @last_event_delivery_id,
    @updated_at
)
ON CONFLICT (provider_repository_id) DO UPDATE SET
    provider_installation_id = CASE WHEN EXCLUDED.provider_installation_id <> 0 THEN EXCLUDED.provider_installation_id ELSE github_repositories.provider_installation_id END,
    owner_login = COALESCE(NULLIF(EXCLUDED.owner_login, ''), github_repositories.owner_login),
    repository_name = COALESCE(NULLIF(EXCLUDED.repository_name, ''), github_repositories.repository_name),
    repository_full_name = COALESCE(NULLIF(EXCLUDED.repository_full_name, ''), github_repositories.repository_full_name),
    state = EXCLUDED.state,
    last_event_delivery_id = COALESCE(NULLIF(EXCLUDED.last_event_delivery_id, ''), github_repositories.last_event_delivery_id),
    updated_at = EXCLUDED.updated_at;

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

-- name: UpsertJobShape :exec
INSERT INTO github_job_shapes (
    job_shape_id,
    provider_installation_id,
    provider_repository_id,
    repository_full_name,
    workflow_path,
    workflow_name,
    job_name,
    matrix_key,
    runner_class,
    runner_labels_json,
    cache_manifest_sha256,
    trust_class,
    canonical_json,
    updated_at
) VALUES (
    @job_shape_id,
    @provider_installation_id,
    @provider_repository_id,
    @repository_full_name,
    @workflow_path,
    @workflow_name,
    @job_name,
    @matrix_key,
    @runner_class,
    @runner_labels_json,
    @cache_manifest_sha256,
    @trust_class,
    @canonical_json,
    @updated_at
)
ON CONFLICT (job_shape_id) DO UPDATE SET
    provider_installation_id = EXCLUDED.provider_installation_id,
    provider_repository_id = EXCLUDED.provider_repository_id,
    repository_full_name = EXCLUDED.repository_full_name,
    workflow_path = EXCLUDED.workflow_path,
    workflow_name = EXCLUDED.workflow_name,
    job_name = EXCLUDED.job_name,
    matrix_key = EXCLUDED.matrix_key,
    runner_class = EXCLUDED.runner_class,
    runner_labels_json = EXCLUDED.runner_labels_json,
    cache_manifest_sha256 = EXCLUDED.cache_manifest_sha256,
    trust_class = EXCLUDED.trust_class,
    canonical_json = EXCLUDED.canonical_json,
    updated_at = EXCLUDED.updated_at;

-- name: EnsureProviderDemand :one
INSERT INTO github_provider_demands (
    demand_id,
    provider_job_id,
    provider_installation_id,
    provider_repository_id,
    repository_full_name,
    provider_run_id,
    provider_run_attempt,
    job_shape_id,
    trust_class,
    runner_class,
    runner_name,
    state,
    last_delivery_id,
    created_at,
    updated_at
) VALUES (
    @demand_id,
    @provider_job_id,
    @provider_installation_id,
    @provider_repository_id,
    @repository_full_name,
    @provider_run_id,
    @provider_run_attempt,
    @job_shape_id,
    @trust_class,
    @runner_class,
    @runner_name,
    'demand_recorded',
    @last_delivery_id,
    @updated_at,
    @updated_at
)
ON CONFLICT (provider_job_id) DO UPDATE SET
    provider_installation_id = EXCLUDED.provider_installation_id,
    provider_repository_id = EXCLUDED.provider_repository_id,
    repository_full_name = EXCLUDED.repository_full_name,
    provider_run_id = EXCLUDED.provider_run_id,
    provider_run_attempt = EXCLUDED.provider_run_attempt,
    job_shape_id = COALESCE(NULLIF(EXCLUDED.job_shape_id, ''), github_provider_demands.job_shape_id),
    trust_class = COALESCE(NULLIF(EXCLUDED.trust_class, ''), github_provider_demands.trust_class),
    runner_class = COALESCE(NULLIF(EXCLUDED.runner_class, ''), github_provider_demands.runner_class),
    last_delivery_id = COALESCE(NULLIF(EXCLUDED.last_delivery_id, ''), github_provider_demands.last_delivery_id),
    updated_at = EXCLUDED.updated_at
RETURNING demand_id, provider_job_id, runner_name, runner_id, runner_class, job_shape_id, trust_class,
          state, jit_config_sha256, sandbox_allocation_id, sandbox_execution_id, sandbox_attempt_id;

-- name: ClaimProviderDemandForJIT :one
UPDATE github_provider_demands
SET state = 'jit_requested',
    failure_reason = '',
    claimed_at = @claimed_at,
    updated_at = @claimed_at
WHERE github_provider_demands.provider_job_id = @provider_job_id
  AND github_provider_demands.state IN ('demand_recorded', 'jit_failed', 'sandbox_failed')
  AND (
      SELECT count(*)
      FROM github_runner_registrations active
      WHERE active.provider_repository_id = github_provider_demands.provider_repository_id
        AND active.runner_class = github_provider_demands.runner_class
        AND active.provider_job_id <> github_provider_demands.provider_job_id
        AND active.state IN ('jit_created', 'sandbox_submitted')
        AND NOT EXISTS (
            SELECT 1
            FROM github_workflow_jobs active_job
            WHERE active_job.provider_job_id = active.provider_job_id
              AND active_job.status = 'completed'
        )
  ) < @repository_runner_class_active_limit
RETURNING demand_id, provider_job_id, provider_installation_id, provider_repository_id,
          repository_full_name, provider_run_id, provider_run_attempt, runner_name,
          runner_class, job_shape_id, trust_class, state;

-- name: MarkProviderDemandJITCreated :exec
UPDATE github_provider_demands
SET runner_id = @runner_id,
    runner_name = @runner_name,
    jit_config_sha256 = @jit_config_sha256,
    state = 'jit_created',
    failure_reason = '',
    updated_at = @updated_at
WHERE provider_job_id = @provider_job_id
  AND state = 'jit_requested';

-- name: MarkProviderDemandSandboxSubmitted :exec
UPDATE github_provider_demands
SET sandbox_allocation_id = @sandbox_allocation_id,
    sandbox_execution_id = @sandbox_execution_id,
    sandbox_attempt_id = @sandbox_attempt_id,
    runner_id = @runner_id,
    runner_name = @runner_name,
    state = 'sandbox_submitted',
    failure_reason = '',
    updated_at = @updated_at
WHERE provider_job_id = @provider_job_id
  AND state IN ('jit_created', 'sandbox_submitting', 'sandbox_failed');

-- name: AssignProviderDemandToRunnerFromDemand :execrows
WITH source AS (
    SELECT runner_id, runner_name, jit_config_sha256, sandbox_allocation_id,
           sandbox_execution_id, sandbox_attempt_id
    FROM github_provider_demands
    WHERE github_provider_demands.provider_job_id = @from_provider_job_id
)
UPDATE github_provider_demands target
SET runner_id = source.runner_id,
    runner_name = source.runner_name,
    jit_config_sha256 = source.jit_config_sha256,
    sandbox_allocation_id = source.sandbox_allocation_id,
    sandbox_execution_id = source.sandbox_execution_id,
    sandbox_attempt_id = source.sandbox_attempt_id,
    state = 'sandbox_submitted',
    failure_reason = '',
    updated_at = @updated_at
FROM source
WHERE target.provider_job_id = @to_provider_job_id;

-- name: ResetProviderDemandAfterRunnerReassignment :execrows
UPDATE github_provider_demands
SET runner_id = 0,
    runner_name = @runner_name,
    jit_config_sha256 = '',
    sandbox_allocation_id = NULL,
    sandbox_execution_id = NULL,
    sandbox_attempt_id = NULL,
    state = 'demand_recorded',
    failure_reason = @failure_reason,
    updated_at = @updated_at
WHERE provider_job_id = @provider_job_id;

-- name: SwapProviderDemandRunnerAssignments :execrows
WITH source AS (
    SELECT runner_id, runner_name, jit_config_sha256, sandbox_allocation_id,
           sandbox_execution_id, sandbox_attempt_id, state
    FROM github_provider_demands
    WHERE github_provider_demands.provider_job_id = @from_provider_job_id
), target AS (
    SELECT runner_id, runner_name, jit_config_sha256, sandbox_allocation_id,
           sandbox_execution_id, sandbox_attempt_id, state
    FROM github_provider_demands
    WHERE github_provider_demands.provider_job_id = @to_provider_job_id
)
UPDATE github_provider_demands demand
SET runner_id = CASE WHEN demand.provider_job_id = @to_provider_job_id THEN source.runner_id ELSE target.runner_id END,
    runner_name = CASE WHEN demand.provider_job_id = @to_provider_job_id THEN source.runner_name ELSE target.runner_name END,
    jit_config_sha256 = CASE WHEN demand.provider_job_id = @to_provider_job_id THEN source.jit_config_sha256 ELSE target.jit_config_sha256 END,
    sandbox_allocation_id = CASE WHEN demand.provider_job_id = @to_provider_job_id THEN source.sandbox_allocation_id ELSE target.sandbox_allocation_id END,
    sandbox_execution_id = CASE WHEN demand.provider_job_id = @to_provider_job_id THEN source.sandbox_execution_id ELSE target.sandbox_execution_id END,
    sandbox_attempt_id = CASE WHEN demand.provider_job_id = @to_provider_job_id THEN source.sandbox_attempt_id ELSE target.sandbox_attempt_id END,
    state = CASE WHEN demand.provider_job_id = @to_provider_job_id THEN 'sandbox_submitted' ELSE target.state END,
    failure_reason = CASE WHEN demand.provider_job_id = @from_provider_job_id THEN @failure_reason ELSE '' END,
    updated_at = @updated_at
FROM source, target
WHERE demand.provider_job_id IN (@from_provider_job_id, @to_provider_job_id);

-- name: MarkProviderDemandFailed :exec
UPDATE github_provider_demands
SET state = @state,
    failure_reason = @failure_reason,
    updated_at = @updated_at
WHERE provider_job_id = @provider_job_id;

-- name: GetProviderDemandForJob :one
SELECT demand_id, provider_job_id, provider_installation_id, provider_repository_id,
       repository_full_name, provider_run_id, provider_run_attempt, runner_name,
       runner_id, runner_class, job_shape_id, trust_class, state, sandbox_allocation_id,
       sandbox_execution_id, sandbox_attempt_id
FROM github_provider_demands
WHERE provider_job_id = @provider_job_id;

-- name: UpsertProviderOutboxCommand :exec
INSERT INTO github_provider_outbox (
    outbox_id,
    command_kind,
    provider_job_id,
    provider_run_id,
    provider_run_attempt,
    provider_installation_id,
    provider_repository_id,
    state,
    command_sha256,
    payload_json,
    next_attempt_at,
    created_at,
    updated_at
) VALUES (
    @outbox_id,
    @command_kind,
    @provider_job_id,
    @provider_run_id,
    @provider_run_attempt,
    @provider_installation_id,
    @provider_repository_id,
    'pending',
    @command_sha256,
    @payload_json,
    @next_attempt_at,
    @updated_at,
    @updated_at
)
ON CONFLICT (command_kind, command_sha256) DO UPDATE SET
    state = CASE
        WHEN github_provider_outbox.state = 'processed' THEN github_provider_outbox.state
        ELSE 'pending'
    END,
    payload_json = EXCLUDED.payload_json,
    next_attempt_at = EXCLUDED.next_attempt_at,
    updated_at = EXCLUDED.updated_at;

-- name: MarkProviderOutboxProcessed :exec
UPDATE github_provider_outbox
SET state = 'processed',
    sandbox_execution_id = @sandbox_execution_id,
    sandbox_attempt_id = @sandbox_attempt_id,
    processed_at = @processed_at,
    failure_reason = '',
    updated_at = @processed_at
WHERE command_kind = @command_kind
  AND command_sha256 = @command_sha256;

-- name: MarkProviderOutboxFailed :exec
UPDATE github_provider_outbox
SET state = @state,
    failure_reason = @failure_reason,
    updated_at = @updated_at
WHERE command_kind = @command_kind
  AND command_sha256 = @command_sha256;

-- name: UpsertRunnerRegistration :exec
INSERT INTO github_runner_registrations (
    provider_job_id,
    demand_id,
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
    @demand_id,
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
    demand_id = COALESCE(EXCLUDED.demand_id, github_runner_registrations.demand_id),
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

-- name: TransferRunnerRegistrationToJob :execrows
WITH stale_target AS (
    DELETE FROM github_runner_registrations
    WHERE github_runner_registrations.provider_job_id = @to_provider_job_id
      AND github_runner_registrations.runner_name <> @runner_name
      AND github_runner_registrations.state IN ('cleaned', 'failed')
)
UPDATE github_runner_registrations
SET provider_job_id = @to_provider_job_id,
    demand_id = (
        SELECT demand_id
        FROM github_provider_demands
        WHERE github_provider_demands.provider_job_id = @to_provider_job_id
    ),
    state = 'sandbox_submitted',
    failure_reason = '',
    updated_at = @updated_at
WHERE github_runner_registrations.provider_job_id = @from_provider_job_id
  AND github_runner_registrations.runner_name = @runner_name;

-- name: SwapRunnerRegistrationJobs :execrows
WITH target AS (
    DELETE FROM github_runner_registrations
    WHERE github_runner_registrations.provider_job_id = @to_provider_job_id
      AND github_runner_registrations.runner_name <> @runner_name
      AND github_runner_registrations.state IN ('jit_created', 'sandbox_submitted')
    RETURNING *
), moved_source AS (
    UPDATE github_runner_registrations
    SET provider_job_id = @to_provider_job_id,
        demand_id = (
            SELECT demand_id
            FROM github_provider_demands
            WHERE github_provider_demands.provider_job_id = @to_provider_job_id
        ),
        state = 'sandbox_submitted',
        failure_reason = '',
        updated_at = @updated_at
    WHERE github_runner_registrations.provider_job_id = @from_provider_job_id
      AND github_runner_registrations.runner_name = @runner_name
    RETURNING *
)
INSERT INTO github_runner_registrations (
    provider_job_id,
    demand_id,
    provider_installation_id,
    provider_repository_id,
    runner_id,
    runner_name,
    runner_class,
    jit_config_sha256,
    sandbox_allocation_id,
    sandbox_execution_id,
    sandbox_attempt_id,
    state,
    failure_reason,
    created_at,
    updated_at
)
SELECT
    @from_provider_job_id,
    (
        SELECT demand_id
        FROM github_provider_demands
        WHERE github_provider_demands.provider_job_id = @from_provider_job_id
    ),
    target.provider_installation_id,
    target.provider_repository_id,
    target.runner_id,
    target.runner_name,
    target.runner_class,
    target.jit_config_sha256,
    target.sandbox_allocation_id,
    target.sandbox_execution_id,
    target.sandbox_attempt_id,
    target.state,
    '',
    target.created_at,
    @updated_at
FROM target
WHERE EXISTS (SELECT 1 FROM moved_source);

-- name: GetRunnerRegistrationForJob :one
SELECT provider_job_id, provider_installation_id, provider_repository_id, runner_id,
       runner_name, runner_class, sandbox_allocation_id, sandbox_execution_id,
       sandbox_attempt_id, state
FROM github_runner_registrations
WHERE provider_job_id = @provider_job_id;

-- name: GetRunnerRegistrationByRunnerName :one
SELECT provider_job_id, provider_installation_id, provider_repository_id, runner_id,
       runner_name, runner_class, sandbox_allocation_id, sandbox_execution_id,
       sandbox_attempt_id, state
FROM github_runner_registrations
WHERE runner_name = @runner_name;

-- name: CountActiveRunnerRegistrationsForRunnerClass :one
SELECT count(*)::bigint
FROM github_runner_registrations
WHERE github_runner_registrations.provider_repository_id = @provider_repository_id
  AND github_runner_registrations.runner_class = @runner_class
  AND github_runner_registrations.state IN ('jit_created', 'sandbox_submitted')
  AND NOT EXISTS (
      SELECT 1
      FROM github_workflow_jobs active_job
      WHERE active_job.provider_job_id = github_runner_registrations.provider_job_id
        AND active_job.status = 'completed'
  );

-- name: ListQueuedWorkflowJobsForRunnerSubmission :many
WITH candidates AS (
    SELECT
        j.provider_job_id,
        j.provider_installation_id,
        j.provider_repository_id,
        j.repository_full_name,
        j.provider_run_id,
        j.provider_run_attempt,
        j.job_name,
        j.head_sha,
        j.head_branch,
        j.workflow_name,
        j.status,
        j.conclusion,
        j.labels_json,
        j.started_at,
        j.completed_at,
        COALESCE(d.state, '')::text AS registration_state,
        d.updated_at AS registration_updated_at,
        COALESCE((
            SELECT label
            FROM jsonb_array_elements_text(j.labels_json) AS label
            WHERE label LIKE @runner_class_prefix || '%'
            ORDER BY label
            LIMIT 1
        ), '')::text AS runner_class
    FROM github_workflow_jobs j
    LEFT JOIN github_provider_demands d ON d.provider_job_id = j.provider_job_id
    WHERE j.status = 'queued'
      AND (
            d.provider_job_id IS NULL
         OR d.state IN ('demand_recorded', 'jit_failed', 'sandbox_failed')
      )
)
SELECT DISTINCT ON (provider_repository_id, runner_class)
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
    started_at,
    completed_at,
    registration_state,
    registration_updated_at
FROM candidates
WHERE runner_class <> ''
ORDER BY provider_repository_id ASC, runner_class ASC, provider_job_id ASC
LIMIT @limit_count;

-- name: InsertTerminalJobEvidence :one
INSERT INTO github_terminal_job_evidence (
    terminal_evidence_id,
    provider_job_id,
    provider_installation_id,
    provider_repository_id,
    provider_run_id,
    provider_run_attempt,
    sandbox_allocation_id,
    sandbox_execution_id,
    sandbox_attempt_id,
    runner_id,
    runner_name,
    job_shape_id,
    trust_class,
    status,
    conclusion,
    source,
    delivery_id,
    observed_at
) VALUES (
    @terminal_evidence_id,
    @provider_job_id,
    @provider_installation_id,
    @provider_repository_id,
    @provider_run_id,
    @provider_run_attempt,
    @sandbox_allocation_id,
    @sandbox_execution_id,
    @sandbox_attempt_id,
    @runner_id,
    @runner_name,
    @job_shape_id,
    @trust_class,
    @status,
    @conclusion,
    @source,
    @delivery_id,
    @observed_at
)
ON CONFLICT (provider_job_id) DO UPDATE SET
    provider_installation_id = EXCLUDED.provider_installation_id,
    provider_repository_id = EXCLUDED.provider_repository_id,
    status = EXCLUDED.status,
    conclusion = EXCLUDED.conclusion,
    sandbox_allocation_id = EXCLUDED.sandbox_allocation_id,
    sandbox_execution_id = EXCLUDED.sandbox_execution_id,
    sandbox_attempt_id = EXCLUDED.sandbox_attempt_id,
    runner_id = EXCLUDED.runner_id,
    runner_name = EXCLUDED.runner_name,
    job_shape_id = EXCLUDED.job_shape_id,
    trust_class = EXCLUDED.trust_class,
    source = EXCLUDED.source,
    delivery_id = EXCLUDED.delivery_id,
    observed_at = EXCLUDED.observed_at
RETURNING terminal_evidence_id;

-- name: UpsertGoldenSnapshotBarrier :exec
INSERT INTO github_golden_snapshot_barriers (
    barrier_id,
    terminal_evidence_id,
    provider_job_id,
    provider_run_id,
    provider_run_attempt,
    sandbox_execution_id,
    sandbox_attempt_id,
    job_shape_id,
    trust_class,
    state,
    failure_reason,
    requested_at,
    updated_at
) VALUES (
    @barrier_id,
    @terminal_evidence_id,
    @provider_job_id,
    @provider_run_id,
    @provider_run_attempt,
    @sandbox_execution_id,
    @sandbox_attempt_id,
    @job_shape_id,
    @trust_class,
    @state,
    @failure_reason,
    @requested_at,
    @requested_at
)
ON CONFLICT (provider_job_id) DO UPDATE SET
    terminal_evidence_id = EXCLUDED.terminal_evidence_id,
    provider_run_id = EXCLUDED.provider_run_id,
    provider_run_attempt = EXCLUDED.provider_run_attempt,
    sandbox_execution_id = EXCLUDED.sandbox_execution_id,
    sandbox_attempt_id = EXCLUDED.sandbox_attempt_id,
    job_shape_id = EXCLUDED.job_shape_id,
    trust_class = EXCLUDED.trust_class,
    state = EXCLUDED.state,
    failure_reason = EXCLUDED.failure_reason,
    requested_at = EXCLUDED.requested_at,
    updated_at = EXCLUDED.requested_at;
