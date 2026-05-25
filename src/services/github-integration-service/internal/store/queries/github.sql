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
    'accepted',
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
    primary_problem_type = CASE
        WHEN github_webhook_deliveries.state = 'rejected' THEN ''
        ELSE github_webhook_deliveries.primary_problem_type
    END,
    primary_problem_code = CASE
        WHEN github_webhook_deliveries.state = 'rejected' THEN ''
        ELSE github_webhook_deliveries.primary_problem_code
    END,
    primary_problem_status = CASE
        WHEN github_webhook_deliveries.state = 'rejected' THEN 0
        ELSE github_webhook_deliveries.primary_problem_status
    END,
    primary_problem_title = CASE
        WHEN github_webhook_deliveries.state = 'rejected' THEN ''
        ELSE github_webhook_deliveries.primary_problem_title
    END,
    primary_problem_detail = CASE
        WHEN github_webhook_deliveries.state = 'rejected' THEN ''
        ELSE github_webhook_deliveries.primary_problem_detail
    END,
    problem_count = CASE
        WHEN github_webhook_deliveries.state = 'rejected' THEN 0
        ELSE github_webhook_deliveries.problem_count
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

-- name: MarkDeliveryRejected :one
INSERT INTO github_webhook_deliveries (
    delivery_id,
    event_name,
    action,
    state,
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
    @payload_sha256,
    @payload_json,
    @received_at,
    @received_at,
    @received_at
)
ON CONFLICT (delivery_id) DO UPDATE SET
    state = 'rejected',
    updated_at = EXCLUDED.updated_at
WHERE github_webhook_deliveries.state = 'rejected'
  AND github_webhook_deliveries.payload_sha256 = EXCLUDED.payload_sha256
RETURNING delivery_id;

-- name: AppendWebhookDeliveryProblem :exec
WITH next_problem AS (
    SELECT COALESCE(MAX(problem_seq), 0) + 1 AS problem_seq
    FROM github_webhook_delivery_problems
    WHERE delivery_id = @delivery_id
),
inserted AS (
    INSERT INTO github_webhook_delivery_problems (
        delivery_id,
        problem_seq,
        phase,
        problem_type,
        problem_code,
        title,
        detail,
        status,
        retryable,
        pointer,
        observed_at
    )
    SELECT
        @delivery_id,
        next_problem.problem_seq,
        @phase,
        @problem_type,
        @problem_code,
        @title,
        @detail,
        @status,
        @retryable,
        @pointer,
        @observed_at
    FROM next_problem
    RETURNING delivery_id
)
UPDATE github_webhook_deliveries
SET
    primary_problem_type = CASE WHEN problem_count = 0 THEN @problem_type ELSE primary_problem_type END,
    primary_problem_code = CASE WHEN problem_count = 0 THEN @problem_code ELSE primary_problem_code END,
    primary_problem_status = CASE WHEN problem_count = 0 THEN @status ELSE primary_problem_status END,
    primary_problem_title = CASE WHEN problem_count = 0 THEN @title ELSE primary_problem_title END,
    primary_problem_detail = CASE WHEN problem_count = 0 THEN @detail ELSE primary_problem_detail END,
    problem_count = problem_count + (SELECT COUNT(*) FROM inserted),
    updated_at = @observed_at
WHERE github_webhook_deliveries.delivery_id = @delivery_id;

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
    WHERE state IN ('accepted', 'retryable')
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
    processed_at = @processed_at,
    processing_started_at = NULL,
    updated_at = @processed_at
WHERE delivery_id = @delivery_id
  AND state = 'processing';

-- name: MarkDeliveryIgnored :exec
UPDATE github_webhook_deliveries
SET state = 'ignored',
    processed_at = @processed_at,
    processing_started_at = NULL,
    updated_at = @processed_at
WHERE delivery_id = @delivery_id
  AND state = 'processing';

-- name: MarkDeliveryRetryable :exec
UPDATE github_webhook_deliveries
SET state = 'retryable',
    next_attempt_at = @next_attempt_at,
    processing_started_at = NULL,
    updated_at = @updated_at
WHERE delivery_id = @delivery_id
  AND state = 'processing';

-- name: MarkDeliveryFailed :exec
UPDATE github_webhook_deliveries
SET state = 'failed',
    processed_at = @failed_at,
    processing_started_at = NULL,
    updated_at = @failed_at
WHERE delivery_id = @delivery_id
  AND state = 'processing';

-- name: ListStaleProcessingDeliveries :many
SELECT delivery_id, event_name, action, provider_installation_id,
       provider_repository_id, repository_full_name, provider_run_id,
       provider_run_attempt, provider_job_id, attempt_count, processing_started_at
FROM github_webhook_deliveries
WHERE state = 'processing'
  AND processing_started_at IS NOT NULL
  AND processing_started_at <= @stale_before
ORDER BY processing_started_at ASC
LIMIT @limit_count;

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
WITH upserted_repository AS (
    INSERT INTO github_repositories (
        provider_repository_id,
        owner_login,
        repository_name,
        repository_full_name,
        state,
        last_event_delivery_id,
        updated_at
    ) VALUES (
        @provider_repository_id,
        @owner_login,
        @repository_name,
        @repository_full_name,
        @state,
        @last_event_delivery_id,
        @updated_at
    )
    ON CONFLICT (provider_repository_id) DO UPDATE SET
        owner_login = COALESCE(NULLIF(EXCLUDED.owner_login, ''), github_repositories.owner_login),
        repository_name = COALESCE(NULLIF(EXCLUDED.repository_name, ''), github_repositories.repository_name),
        repository_full_name = COALESCE(NULLIF(EXCLUDED.repository_full_name, ''), github_repositories.repository_full_name),
        state = EXCLUDED.state,
        last_event_delivery_id = COALESCE(NULLIF(EXCLUDED.last_event_delivery_id, ''), github_repositories.last_event_delivery_id),
        updated_at = EXCLUDED.updated_at
    RETURNING provider_repository_id
)
INSERT INTO github_installation_repositories (
    provider_installation_id,
    provider_repository_id,
    state,
    last_event_delivery_id,
    updated_at
)
SELECT
    sqlc.arg(provider_installation_id)::bigint,
    provider_repository_id,
    'selected',
    @last_event_delivery_id,
    @updated_at
FROM upserted_repository
WHERE sqlc.arg(provider_installation_id)::bigint > 0
ON CONFLICT (provider_installation_id, provider_repository_id) DO UPDATE SET
    state = EXCLUDED.state,
    last_event_delivery_id = COALESCE(NULLIF(EXCLUDED.last_event_delivery_id, ''), github_installation_repositories.last_event_delivery_id),
    updated_at = EXCLUDED.updated_at;

-- name: LockIdempotencyKey :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(lock_key), 0::bigint));

-- name: GetIdempotencyRecord :one
SELECT org_id, actor_id, operation, key_hash, request_hash, result_payload, created_at
FROM github_idempotency_records
WHERE org_id = @org_id
  AND actor_id = @actor_id
  AND operation = @operation
  AND key_hash = @key_hash;

-- name: InsertIdempotencyRecord :exec
INSERT INTO github_idempotency_records (
    org_id,
    actor_id,
    operation,
    key_hash,
    request_hash,
    result_payload,
    created_at
) VALUES (
    @org_id,
    @actor_id,
    @operation,
    @key_hash,
    @request_hash,
    @result_payload,
    @created_at
);

-- name: CreateSetupSession :one
INSERT INTO github_setup_sessions (
    setup_session_id,
    org_id,
    actor_id,
    state_hash,
    session_state,
    installation_url,
    callback_url,
    expires_at,
    created_at,
    updated_at
) VALUES (
    @setup_session_id,
    @org_id,
    @actor_id,
    @state_hash,
    'pending_setup',
    @installation_url,
    @callback_url,
    @expires_at,
    @created_at,
    @created_at
)
RETURNING setup_session_id, org_id, actor_id, session_state, installation_url,
          callback_url, provider_installation_id, github_user_authorization_id,
          installation_binding_id, expires_at, completed_at, created_at, updated_at;

-- name: GetSetupSession :one
SELECT setup_session_id, org_id, actor_id, state_hash, session_state, installation_url,
       callback_url, provider_installation_id, github_user_authorization_id,
       installation_binding_id, expires_at, completed_at, created_at, updated_at
FROM github_setup_sessions
WHERE setup_session_id = @setup_session_id
  AND org_id = @org_id;

-- name: CompleteSetupSession :one
UPDATE github_setup_sessions
SET session_state = 'completed',
    provider_installation_id = @provider_installation_id,
    github_user_authorization_id = @github_user_authorization_id,
    installation_binding_id = @installation_binding_id,
    completed_at = @completed_at,
    updated_at = @completed_at
WHERE setup_session_id = @setup_session_id
  AND org_id = @org_id
  AND state_hash = @state_hash
  AND session_state IN ('pending_setup', 'awaiting_user_authorization')
  AND expires_at > @completed_at
RETURNING setup_session_id, org_id, actor_id, session_state, installation_url,
          callback_url, provider_installation_id, github_user_authorization_id,
          installation_binding_id, expires_at, completed_at, created_at, updated_at;

-- name: MarkSetupSessionAwaitingAuthorization :one
UPDATE github_setup_sessions
SET session_state = 'awaiting_user_authorization',
    failure_reason = @failure_reason,
    updated_at = @updated_at
WHERE setup_session_id = @setup_session_id
  AND org_id = @org_id
  AND session_state = 'pending_setup'
RETURNING setup_session_id, org_id, actor_id, session_state, installation_url,
          callback_url, provider_installation_id, github_user_authorization_id,
          installation_binding_id, expires_at, completed_at, created_at, updated_at;

-- name: CreateOAuthSession :one
INSERT INTO github_oauth_sessions (
    oauth_session_id,
    org_id,
    actor_id,
    state_hash,
    session_state,
    authorization_url,
    callback_url,
    expires_at,
    created_at,
    updated_at
) VALUES (
    @oauth_session_id,
    @org_id,
    @actor_id,
    @state_hash,
    'pending',
    @authorization_url,
    @callback_url,
    @expires_at,
    @created_at,
    @created_at
)
RETURNING oauth_session_id, org_id, actor_id, session_state, authorization_url,
          callback_url, github_user_authorization_id, expires_at, completed_at,
          created_at, updated_at;

-- name: CompleteOAuthSession :one
UPDATE github_oauth_sessions
SET session_state = 'completed',
    github_user_authorization_id = @github_user_authorization_id,
    completed_at = @completed_at,
    updated_at = @completed_at
WHERE oauth_session_id = @oauth_session_id
  AND org_id = @org_id
  AND state_hash = @state_hash
  AND session_state = 'pending'
  AND expires_at > @completed_at
RETURNING oauth_session_id, org_id, actor_id, session_state, authorization_url,
          callback_url, github_user_authorization_id, expires_at, completed_at,
          created_at, updated_at;

-- name: GetOAuthSessionForCompletion :one
SELECT oauth_session_id, org_id, actor_id, state_hash, session_state, authorization_url,
       callback_url, github_user_authorization_id, expires_at, completed_at,
       created_at, updated_at
FROM github_oauth_sessions
WHERE oauth_session_id = @oauth_session_id
  AND org_id = @org_id;

-- name: UpsertGithubAccount :exec
INSERT INTO github_accounts (
    provider_account_id,
    login,
    account_type,
    avatar_url,
    html_url,
    state,
    last_event_delivery_id,
    observed_from_api_at,
    updated_at
) VALUES (
    @provider_account_id,
    @login,
    @account_type,
    @avatar_url,
    @html_url,
    @state,
    @last_event_delivery_id,
    @observed_from_api_at,
    @updated_at
)
ON CONFLICT (provider_account_id) DO UPDATE SET
    login = COALESCE(NULLIF(EXCLUDED.login, ''), github_accounts.login),
    account_type = COALESCE(NULLIF(EXCLUDED.account_type, ''), github_accounts.account_type),
    avatar_url = COALESCE(NULLIF(EXCLUDED.avatar_url, ''), github_accounts.avatar_url),
    html_url = COALESCE(NULLIF(EXCLUDED.html_url, ''), github_accounts.html_url),
    state = EXCLUDED.state,
    last_event_delivery_id = COALESCE(NULLIF(EXCLUDED.last_event_delivery_id, ''), github_accounts.last_event_delivery_id),
    observed_from_api_at = COALESCE(EXCLUDED.observed_from_api_at, github_accounts.observed_from_api_at),
    updated_at = EXCLUDED.updated_at;

-- name: UpsertInstallationDetails :exec
INSERT INTO github_installations (
    provider_installation_id,
    provider_account_id,
    account_login,
    account_type,
    app_slug,
    target_type,
    repository_selection,
    configuration_url,
    permissions_json,
    state,
    last_event_delivery_id,
    observed_from_api_at,
    updated_at
) VALUES (
    @provider_installation_id,
    @provider_account_id,
    @account_login,
    @account_type,
    @app_slug,
    @target_type,
    @repository_selection,
    @configuration_url,
    @permissions_json,
    @state,
    @last_event_delivery_id,
    @observed_from_api_at,
    @updated_at
)
ON CONFLICT (provider_installation_id) DO UPDATE SET
    provider_account_id = CASE WHEN EXCLUDED.provider_account_id > 0 THEN EXCLUDED.provider_account_id ELSE github_installations.provider_account_id END,
    account_login = COALESCE(NULLIF(EXCLUDED.account_login, ''), github_installations.account_login),
    account_type = COALESCE(NULLIF(EXCLUDED.account_type, ''), github_installations.account_type),
    app_slug = COALESCE(NULLIF(EXCLUDED.app_slug, ''), github_installations.app_slug),
    target_type = COALESCE(NULLIF(EXCLUDED.target_type, ''), github_installations.target_type),
    repository_selection = COALESCE(NULLIF(EXCLUDED.repository_selection, ''), github_installations.repository_selection),
    configuration_url = COALESCE(NULLIF(EXCLUDED.configuration_url, ''), github_installations.configuration_url),
    permissions_json = EXCLUDED.permissions_json,
    state = EXCLUDED.state,
    last_event_delivery_id = COALESCE(NULLIF(EXCLUDED.last_event_delivery_id, ''), github_installations.last_event_delivery_id),
    observed_from_api_at = COALESCE(EXCLUDED.observed_from_api_at, github_installations.observed_from_api_at),
    updated_at = EXCLUDED.updated_at;

-- name: UpsertUserAuthorization :one
INSERT INTO github_user_authorizations (
    github_user_authorization_id,
    org_id,
    actor_id,
    provider_user_id,
    github_login,
    scopes_json,
    credential_ref,
    state,
    authorized_at,
    last_verified_at,
    updated_at
) VALUES (
    @github_user_authorization_id,
    @org_id,
    @actor_id,
    @provider_user_id,
    @github_login,
    @scopes_json,
    @credential_ref,
    'active',
    @authorized_at,
    @authorized_at,
    @authorized_at
)
ON CONFLICT (org_id, actor_id, provider_user_id) WHERE state = 'active' DO UPDATE SET
    github_login = EXCLUDED.github_login,
    scopes_json = EXCLUDED.scopes_json,
    credential_ref = EXCLUDED.credential_ref,
    last_verified_at = EXCLUDED.last_verified_at,
    updated_at = EXCLUDED.updated_at
RETURNING github_user_authorization_id, org_id, actor_id, provider_user_id,
          github_login, scopes_json, credential_ref, state, authorized_at,
          last_verified_at, revoked_at, created_at, updated_at;

-- name: GetUserAuthorization :one
SELECT github_user_authorization_id, org_id, actor_id, provider_user_id,
       github_login, scopes_json, credential_ref, state, authorized_at,
       last_verified_at, revoked_at, created_at, updated_at
FROM github_user_authorizations
WHERE github_user_authorization_id = @github_user_authorization_id
  AND org_id = @org_id
  AND actor_id = @actor_id
  AND state = 'active';

-- name: RevokeUserAuthorizationsByGitHubUser :exec
UPDATE github_user_authorizations
SET state = 'revoked',
    revoked_at = @revoked_at,
    updated_at = @revoked_at
WHERE provider_user_id = @provider_user_id
  AND state = 'active';

-- name: UpsertInstallationBinding :one
INSERT INTO github_installation_bindings (
    installation_binding_id,
    org_id,
    provider_installation_id,
    provider_account_id,
    setup_session_id,
    connected_by_actor_id,
    state,
    connected_at,
    last_synced_at,
    updated_at
) VALUES (
    @installation_binding_id,
    @org_id,
    @provider_installation_id,
    @provider_account_id,
    @setup_session_id,
    @connected_by_actor_id,
    'active',
    @connected_at,
    @connected_at,
    @connected_at
)
ON CONFLICT (provider_installation_id) WHERE state = 'active' DO UPDATE SET
    org_id = CASE
        WHEN github_installation_bindings.org_id = EXCLUDED.org_id THEN github_installation_bindings.org_id
        ELSE github_installation_bindings.org_id
    END,
    provider_account_id = EXCLUDED.provider_account_id,
    setup_session_id = COALESCE(EXCLUDED.setup_session_id, github_installation_bindings.setup_session_id),
    connected_by_actor_id = CASE
        WHEN github_installation_bindings.org_id = EXCLUDED.org_id THEN EXCLUDED.connected_by_actor_id
        ELSE github_installation_bindings.connected_by_actor_id
    END,
    last_synced_at = EXCLUDED.last_synced_at,
    updated_at = EXCLUDED.updated_at
WHERE github_installation_bindings.org_id = EXCLUDED.org_id
RETURNING installation_binding_id, org_id, provider_installation_id, provider_account_id,
          setup_session_id, connected_by_actor_id, state, connected_at,
          disconnected_at, last_synced_at, created_at, updated_at;

-- name: GetInstallationBinding :one
SELECT b.installation_binding_id, b.org_id, b.provider_installation_id, b.provider_account_id,
       b.setup_session_id, b.connected_by_actor_id, b.state, b.connected_at,
       b.disconnected_at, b.last_synced_at, b.created_at, b.updated_at,
       i.account_login, i.account_type, i.app_slug, i.repository_selection,
       i.configuration_url
FROM github_installation_bindings b
JOIN github_installations i ON i.provider_installation_id = b.provider_installation_id
WHERE b.installation_binding_id = @installation_binding_id
  AND b.org_id = @org_id;

-- name: ListInstallationBindings :many
SELECT b.installation_binding_id, b.org_id, b.provider_installation_id, b.provider_account_id,
       b.setup_session_id, b.connected_by_actor_id, b.state, b.connected_at,
       b.disconnected_at, b.last_synced_at, b.created_at, b.updated_at,
       i.account_login, i.account_type, i.app_slug, i.repository_selection,
       i.configuration_url
FROM github_installation_bindings b
JOIN github_installations i ON i.provider_installation_id = b.provider_installation_id
WHERE b.org_id = @org_id
  AND b.state = 'active'
ORDER BY b.connected_at DESC, b.installation_binding_id DESC
LIMIT @limit_count
OFFSET @offset_count;

-- name: DisconnectInstallationBinding :one
UPDATE github_installation_bindings
SET state = 'disconnected',
    disconnected_by_actor_id = @actor_id,
    disconnected_at = @disconnected_at,
    updated_at = @disconnected_at
WHERE installation_binding_id = @installation_binding_id
  AND org_id = @org_id
RETURNING installation_binding_id, org_id, provider_installation_id, provider_account_id,
          setup_session_id, connected_by_actor_id, state, connected_at,
          disconnected_at, last_synced_at, created_at, updated_at;

-- name: MarkRepositoryBindingsUnavailableForInstallation :exec
UPDATE github_repository_bindings
SET state = 'unavailable',
    updated_at = @updated_at
WHERE installation_binding_id = @installation_binding_id
  AND state = 'enabled';

-- name: MarkInstallationBindingsRevokedByProvider :exec
UPDATE github_installation_bindings
SET state = 'revoked',
    disconnected_at = @revoked_at,
    updated_at = @revoked_at
WHERE provider_installation_id = @provider_installation_id
  AND state = 'active';

-- name: MarkRepositoryBindingUnavailableByProvider :exec
UPDATE github_repository_bindings
SET state = 'unavailable',
    updated_at = @updated_at
WHERE provider_installation_id = @provider_installation_id
  AND provider_repository_id = @provider_repository_id
  AND state = 'enabled';

-- name: UpsertInstallationRepository :exec
INSERT INTO github_installation_repositories (
    provider_installation_id,
    provider_repository_id,
    state,
    observed_from_api_at,
    updated_at
) VALUES (
    @provider_installation_id,
    @provider_repository_id,
    @state,
    @observed_from_api_at,
    @updated_at
)
ON CONFLICT (provider_installation_id, provider_repository_id) DO UPDATE SET
    state = EXCLUDED.state,
    observed_from_api_at = COALESCE(EXCLUDED.observed_from_api_at, github_installation_repositories.observed_from_api_at),
    updated_at = EXCLUDED.updated_at;

-- name: UpsertRepositoryDetails :exec
INSERT INTO github_repositories (
    provider_repository_id,
    provider_account_id,
    owner_login,
    repository_name,
    repository_full_name,
    default_branch,
    private,
    archived,
    visibility,
    html_url,
    state,
    observed_from_api_at,
    updated_at
) VALUES (
    @provider_repository_id,
    @provider_account_id,
    @owner_login,
    @repository_name,
    @repository_full_name,
    @default_branch,
    @private,
    @archived,
    @visibility,
    @html_url,
    @state,
    @observed_from_api_at,
    @updated_at
)
ON CONFLICT (provider_repository_id) DO UPDATE SET
    provider_account_id = CASE WHEN EXCLUDED.provider_account_id > 0 THEN EXCLUDED.provider_account_id ELSE github_repositories.provider_account_id END,
    owner_login = COALESCE(NULLIF(EXCLUDED.owner_login, ''), github_repositories.owner_login),
    repository_name = COALESCE(NULLIF(EXCLUDED.repository_name, ''), github_repositories.repository_name),
    repository_full_name = COALESCE(NULLIF(EXCLUDED.repository_full_name, ''), github_repositories.repository_full_name),
    default_branch = COALESCE(NULLIF(EXCLUDED.default_branch, ''), github_repositories.default_branch),
    private = EXCLUDED.private,
    archived = EXCLUDED.archived,
    visibility = COALESCE(NULLIF(EXCLUDED.visibility, ''), github_repositories.visibility),
    html_url = COALESCE(NULLIF(EXCLUDED.html_url, ''), github_repositories.html_url),
    state = EXCLUDED.state,
    observed_from_api_at = COALESCE(EXCLUDED.observed_from_api_at, github_repositories.observed_from_api_at),
    updated_at = EXCLUDED.updated_at;

-- name: ListRepositoryCandidates :many
SELECT ir.provider_installation_id, r.provider_repository_id, r.owner_login,
       r.repository_name, r.repository_full_name, r.default_branch, r.private,
       r.observed_from_api_at, ir.state AS installation_repository_state,
       rb.repository_binding_id, COALESCE(rb.state, '')::text AS repository_binding_state,
       rb.org_id
FROM github_installation_repositories ir
JOIN github_repositories r ON r.provider_repository_id = ir.provider_repository_id
JOIN github_installation_bindings ib ON ib.provider_installation_id = ir.provider_installation_id
LEFT JOIN github_repository_bindings rb
  ON rb.installation_binding_id = ib.installation_binding_id
 AND rb.provider_repository_id = r.provider_repository_id
WHERE ib.installation_binding_id = @installation_binding_id
  AND ib.org_id = @org_id
ORDER BY r.repository_full_name ASC
LIMIT @limit_count
OFFSET @offset_count;

-- name: EnableRepositoryBinding :one
INSERT INTO github_repository_bindings (
    repository_binding_id,
    org_id,
    installation_binding_id,
    provider_installation_id,
    provider_repository_id,
    state,
    enabled_by_actor_id,
    enabled_at,
    updated_at
)
SELECT
    @repository_binding_id,
    ib.org_id,
    ib.installation_binding_id,
    ib.provider_installation_id,
    ir.provider_repository_id,
    'enabled',
    @actor_id,
    @enabled_at,
    @enabled_at
FROM github_installation_bindings ib
JOIN github_installation_repositories ir
  ON ir.provider_installation_id = ib.provider_installation_id
WHERE ib.installation_binding_id = @installation_binding_id
  AND ib.org_id = @org_id
  AND ib.state = 'active'
  AND ir.provider_repository_id = @provider_repository_id
  AND ir.state = 'selected'
ON CONFLICT (installation_binding_id, provider_repository_id) DO UPDATE SET
    state = 'enabled',
    enabled_by_actor_id = EXCLUDED.enabled_by_actor_id,
    enabled_at = EXCLUDED.enabled_at,
    disabled_by_actor_id = '',
    disabled_at = NULL,
    updated_at = EXCLUDED.updated_at
RETURNING repository_binding_id, org_id, installation_binding_id, provider_installation_id,
          provider_repository_id, state, enabled_by_actor_id, disabled_by_actor_id,
          enabled_at, disabled_at, created_at, updated_at;

-- name: DisableRepositoryBinding :one
UPDATE github_repository_bindings
SET state = 'disabled',
    disabled_by_actor_id = @actor_id,
    disabled_at = @disabled_at,
    updated_at = @disabled_at
WHERE repository_binding_id = @repository_binding_id
  AND org_id = @org_id
RETURNING repository_binding_id, org_id, installation_binding_id, provider_installation_id,
          provider_repository_id, state, enabled_by_actor_id, disabled_by_actor_id,
          enabled_at, disabled_at, created_at, updated_at;

-- name: GetRepositoryBinding :one
SELECT rb.repository_binding_id, rb.org_id, rb.installation_binding_id,
       rb.provider_installation_id, rb.provider_repository_id, rb.state,
       rb.enabled_by_actor_id, rb.disabled_by_actor_id, rb.enabled_at,
       rb.disabled_at, rb.created_at, rb.updated_at,
       r.owner_login, r.repository_name, r.repository_full_name,
       r.default_branch, r.private, r.observed_from_api_at
FROM github_repository_bindings rb
JOIN github_repositories r ON r.provider_repository_id = rb.provider_repository_id
WHERE rb.repository_binding_id = @repository_binding_id
  AND rb.org_id = @org_id;

-- name: LookupRuntimeBinding :one
SELECT ib.org_id, ib.installation_binding_id, rb.repository_binding_id
FROM github_installation_bindings ib
JOIN github_repository_bindings rb
  ON rb.installation_binding_id = ib.installation_binding_id
WHERE ib.provider_installation_id = @provider_installation_id
  AND ib.state = 'active'
  AND rb.provider_repository_id = @provider_repository_id
  AND rb.state = 'enabled';

-- name: InsertProviderReconciliation :exec
INSERT INTO github_provider_reconciliations (
    reconciliation_id,
    org_id,
    installation_binding_id,
    provider_installation_id,
    reason,
    state,
    started_at,
    created_at,
    updated_at
) VALUES (
    @reconciliation_id,
    @org_id,
    @installation_binding_id,
    @provider_installation_id,
    @reason,
    'processing',
    @started_at,
    @started_at,
    @started_at
);

-- name: CompleteProviderReconciliation :exec
UPDATE github_provider_reconciliations
SET state = @state,
    failure_reason = @failure_reason,
    completed_at = @completed_at,
    updated_at = @completed_at
WHERE reconciliation_id = @reconciliation_id;

-- name: UpsertWorkflowRun :exec
INSERT INTO github_workflow_runs (
    org_id,
    installation_binding_id,
    repository_binding_id,
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
    @org_id,
    @installation_binding_id,
    @repository_binding_id,
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
    org_id = COALESCE(NULLIF(EXCLUDED.org_id, ''), github_workflow_runs.org_id),
    installation_binding_id = COALESCE(EXCLUDED.installation_binding_id, github_workflow_runs.installation_binding_id),
    repository_binding_id = COALESCE(EXCLUDED.repository_binding_id, github_workflow_runs.repository_binding_id),
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
    org_id,
    installation_binding_id,
    repository_binding_id,
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
    @org_id,
    @installation_binding_id,
    @repository_binding_id,
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
    org_id = COALESCE(NULLIF(EXCLUDED.org_id, ''), github_workflow_jobs.org_id),
    installation_binding_id = COALESCE(EXCLUDED.installation_binding_id, github_workflow_jobs.installation_binding_id),
    repository_binding_id = COALESCE(EXCLUDED.repository_binding_id, github_workflow_jobs.repository_binding_id),
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
    org_id,
    installation_binding_id,
    repository_binding_id,
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
    @org_id,
    @installation_binding_id,
    @repository_binding_id,
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
    org_id = COALESCE(NULLIF(EXCLUDED.org_id, ''), github_job_shapes.org_id),
    installation_binding_id = COALESCE(EXCLUDED.installation_binding_id, github_job_shapes.installation_binding_id),
    repository_binding_id = COALESCE(EXCLUDED.repository_binding_id, github_job_shapes.repository_binding_id),
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
    org_id,
    installation_binding_id,
    repository_binding_id,
    provider_installation_id,
    provider_repository_id,
    repository_full_name,
    provider_run_id,
    provider_run_attempt,
    job_shape_id,
    trust_class,
    runner_class,
    state,
    last_delivery_id,
    created_at,
    updated_at
) VALUES (
    @demand_id,
    @provider_job_id,
    @org_id,
    @installation_binding_id,
    @repository_binding_id,
    @provider_installation_id,
    @provider_repository_id,
    @repository_full_name,
    @provider_run_id,
    @provider_run_attempt,
    @job_shape_id,
    @trust_class,
    @runner_class,
    'demand_recorded',
    @last_delivery_id,
    @updated_at,
    @updated_at
)
ON CONFLICT (provider_job_id) DO UPDATE SET
    org_id = COALESCE(NULLIF(EXCLUDED.org_id, ''), github_provider_demands.org_id),
    installation_binding_id = COALESCE(EXCLUDED.installation_binding_id, github_provider_demands.installation_binding_id),
    repository_binding_id = COALESCE(EXCLUDED.repository_binding_id, github_provider_demands.repository_binding_id),
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
RETURNING demand_id, provider_job_id, org_id, installation_binding_id, repository_binding_id,
          provider_installation_id, provider_repository_id, repository_full_name,
          provider_run_id, provider_run_attempt, runner_class, job_shape_id,
          trust_class, state;

-- name: ClaimProviderDemandForCapacity :one
UPDATE github_provider_demands
SET claimed_at = @claimed_at,
    updated_at = @claimed_at
WHERE github_provider_demands.provider_job_id = @provider_job_id
  AND github_provider_demands.state = 'demand_recorded'
  AND NOT EXISTS (
      SELECT 1
      FROM github_job_assignments assigned
      WHERE assigned.provider_job_id = github_provider_demands.provider_job_id
  )
  AND (
      SELECT count(*)
      FROM github_runner_instances active
      WHERE active.provider_repository_id = github_provider_demands.provider_repository_id
        AND active.runner_class = github_provider_demands.runner_class
        AND EXISTS (
            SELECT 1
            FROM github_provider_demands active_demand
            WHERE active_demand.demand_id = active.origin_demand_id
              AND active_demand.state NOT IN ('completed', 'capacity_failed', 'jit_failed', 'sandbox_failed')
        )
        AND (
            active.state = 'assigned'
            OR (
                active.state IN ('jit_created', 'sandbox_submitted')
                AND active.assignment_deadline_at > now()
            )
        )
        AND NOT EXISTS (
            SELECT 1
            FROM github_job_assignments active_assignment
            JOIN github_workflow_jobs active_job
              ON active_job.provider_job_id = active_assignment.provider_job_id
            WHERE active_assignment.runner_name = active.runner_name
              AND active_job.status = 'completed'
        )
  ) < @repository_runner_class_active_limit
RETURNING demand_id, provider_job_id, org_id, installation_binding_id, repository_binding_id,
          provider_installation_id, provider_repository_id,
          repository_full_name, provider_run_id, provider_run_attempt,
          runner_class, job_shape_id, trust_class, state;

-- name: MarkProviderDemandCapacityRequested :exec
UPDATE github_provider_demands
SET state = 'capacity_requested',
    updated_at = @updated_at
WHERE provider_job_id = @provider_job_id
  AND state IN ('demand_recorded', 'capacity_requested');

-- name: MarkProviderDemandAssigned :exec
UPDATE github_provider_demands
SET state = 'assigned',
    updated_at = @updated_at
WHERE provider_job_id = @provider_job_id
  AND state <> 'completed';

-- name: MarkProviderDemandTerminal :exec
UPDATE github_provider_demands
SET state = 'completed',
    updated_at = @updated_at
WHERE provider_job_id = @provider_job_id;

-- name: ResetProviderDemandAfterCapacityDisplaced :execrows
UPDATE github_provider_demands
SET state = 'demand_recorded',
    updated_at = @updated_at
WHERE provider_job_id = @provider_job_id
  AND state = 'capacity_requested';

-- name: MarkProviderDemandFailed :execrows
UPDATE github_provider_demands
SET state = @state,
    updated_at = @updated_at
WHERE provider_job_id = @provider_job_id
  AND state NOT IN ('assigned', 'completed');

-- name: AppendProviderDemandProblem :exec
WITH next_problem AS (
    SELECT COALESCE(MAX(problem_seq), 0) + 1 AS problem_seq
    FROM github_provider_demand_problems
    WHERE provider_job_id = @provider_job_id
),
inserted AS (
    INSERT INTO github_provider_demand_problems (
        provider_job_id,
        problem_seq,
        phase,
        problem_type,
        problem_code,
        title,
        detail,
        status,
        retryable,
        pointer,
        observed_at
    )
    SELECT
        @provider_job_id,
        next_problem.problem_seq,
        @phase,
        @problem_type,
        @problem_code,
        @title,
        @detail,
        @status,
        @retryable,
        @pointer,
        @observed_at
    FROM next_problem
    WHERE EXISTS (
        SELECT 1
        FROM github_provider_demands d
        WHERE d.provider_job_id = @provider_job_id
    )
    RETURNING provider_job_id
)
UPDATE github_provider_demands AS d
SET
    primary_problem_type = CASE WHEN problem_count = 0 THEN @problem_type ELSE primary_problem_type END,
    primary_problem_code = CASE WHEN problem_count = 0 THEN @problem_code ELSE primary_problem_code END,
    primary_problem_status = CASE WHEN problem_count = 0 THEN @status ELSE primary_problem_status END,
    primary_problem_title = CASE WHEN problem_count = 0 THEN @title ELSE primary_problem_title END,
    primary_problem_detail = CASE WHEN problem_count = 0 THEN @detail ELSE primary_problem_detail END,
    problem_count = problem_count + (SELECT COUNT(*) FROM inserted),
    updated_at = @observed_at
WHERE d.provider_job_id = @provider_job_id;

-- name: UpsertProviderOutboxCommand :exec
INSERT INTO github_provider_outbox (
    outbox_id,
    command_kind,
    org_id,
    installation_binding_id,
    repository_binding_id,
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
    @org_id,
    @installation_binding_id,
    @repository_binding_id,
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

-- name: UpsertProviderSurfaceCommand :one
INSERT INTO github_provider_surface_commands (
    surface_id,
    command_key,
    command_kind,
    org_id,
    installation_binding_id,
    repository_binding_id,
    provider_installation_id,
    provider_repository_id,
    repository_full_name,
    provider_run_id,
    provider_run_attempt,
    provider_job_id,
    runner_id,
    runner_name,
    runner_class,
    state,
    next_attempt_at,
    created_at,
    updated_at
) VALUES (
    @surface_id,
    @command_key,
    @command_kind,
    @org_id,
    @installation_binding_id,
    @repository_binding_id,
    @provider_installation_id,
    @provider_repository_id,
    @repository_full_name,
    @provider_run_id,
    @provider_run_attempt,
    @provider_job_id,
    @runner_id,
    @runner_name,
    @runner_class,
    'pending',
    @next_attempt_at,
    @updated_at,
    @updated_at
)
ON CONFLICT (command_key) DO UPDATE SET
    command_kind = EXCLUDED.command_kind,
    org_id = COALESCE(NULLIF(EXCLUDED.org_id, ''), github_provider_surface_commands.org_id),
    installation_binding_id = COALESCE(EXCLUDED.installation_binding_id, github_provider_surface_commands.installation_binding_id),
    repository_binding_id = COALESCE(EXCLUDED.repository_binding_id, github_provider_surface_commands.repository_binding_id),
    provider_installation_id = CASE WHEN EXCLUDED.provider_installation_id <> 0 THEN EXCLUDED.provider_installation_id ELSE github_provider_surface_commands.provider_installation_id END,
    provider_repository_id = CASE WHEN EXCLUDED.provider_repository_id <> 0 THEN EXCLUDED.provider_repository_id ELSE github_provider_surface_commands.provider_repository_id END,
    repository_full_name = COALESCE(NULLIF(EXCLUDED.repository_full_name, ''), github_provider_surface_commands.repository_full_name),
    provider_run_id = CASE WHEN EXCLUDED.provider_run_id <> 0 THEN EXCLUDED.provider_run_id ELSE github_provider_surface_commands.provider_run_id END,
    provider_run_attempt = CASE WHEN EXCLUDED.provider_run_attempt <> 0 THEN EXCLUDED.provider_run_attempt ELSE github_provider_surface_commands.provider_run_attempt END,
    provider_job_id = CASE WHEN EXCLUDED.provider_job_id <> 0 THEN EXCLUDED.provider_job_id ELSE github_provider_surface_commands.provider_job_id END,
    runner_id = CASE WHEN EXCLUDED.runner_id <> 0 THEN EXCLUDED.runner_id ELSE github_provider_surface_commands.runner_id END,
    runner_name = COALESCE(NULLIF(EXCLUDED.runner_name, ''), github_provider_surface_commands.runner_name),
    runner_class = COALESCE(NULLIF(EXCLUDED.runner_class, ''), github_provider_surface_commands.runner_class),
    state = CASE
        WHEN github_provider_surface_commands.state = 'succeeded' THEN github_provider_surface_commands.state
        ELSE 'pending'
    END,
    next_attempt_at = CASE
        WHEN github_provider_surface_commands.state = 'succeeded' THEN github_provider_surface_commands.next_attempt_at
        ELSE EXCLUDED.next_attempt_at
    END,
    updated_at = EXCLUDED.updated_at
RETURNING surface_id, command_key, command_kind, state;

-- name: AppendProviderSurfaceProblem :exec
WITH next_problem AS (
    SELECT COALESCE(MAX(problem_seq), 0) + 1 AS problem_seq
    FROM github_provider_surface_problems
    WHERE surface_id = @surface_id
),
inserted AS (
    INSERT INTO github_provider_surface_problems (
        surface_id,
        problem_seq,
        phase,
        problem_type,
        problem_code,
        title,
        detail,
        status,
        retryable,
        pointer,
        observed_at
    )
    SELECT
        @surface_id,
        next_problem.problem_seq,
        @phase,
        @problem_type,
        @problem_code,
        @title,
        @detail,
        @status,
        @retryable,
        @pointer,
        @observed_at
    FROM next_problem
    RETURNING surface_id
)
UPDATE github_provider_surface_commands AS c
SET
    primary_problem_type = CASE WHEN problem_count = 0 THEN @problem_type ELSE primary_problem_type END,
    primary_problem_code = CASE WHEN problem_count = 0 THEN @problem_code ELSE primary_problem_code END,
    primary_problem_status = CASE WHEN problem_count = 0 THEN @status ELSE primary_problem_status END,
    primary_problem_title = CASE WHEN problem_count = 0 THEN @title ELSE primary_problem_title END,
    primary_problem_detail = CASE WHEN problem_count = 0 THEN @detail ELSE primary_problem_detail END,
    problem_count = problem_count + (SELECT COUNT(*) FROM inserted),
    updated_at = @observed_at
WHERE c.surface_id = @surface_id;

-- name: LockReadyProviderSurfaceCommands :many
UPDATE github_provider_surface_commands AS c
SET
    state = 'running',
    attempt_count = c.attempt_count + 1,
    locked_at = @locked_at,
    updated_at = @locked_at
FROM (
    SELECT surface_id
    FROM github_provider_surface_commands
    WHERE state IN ('pending', 'retryable')
      AND (next_attempt_at IS NULL OR next_attempt_at <= @locked_at)
    ORDER BY created_at
    LIMIT @limit_count
    FOR UPDATE SKIP LOCKED
) AS ready
WHERE c.surface_id = ready.surface_id
RETURNING c.surface_id, c.command_key, c.command_kind, c.org_id,
          c.installation_binding_id, c.repository_binding_id,
          c.provider_installation_id, c.provider_repository_id,
          c.repository_full_name, c.provider_run_id, c.provider_run_attempt,
          c.provider_job_id, c.runner_id, c.runner_name, c.runner_class,
          c.primary_problem_code, c.primary_problem_detail, c.attempt_count;

-- name: ListStaleProviderSurfaceCommands :many
SELECT surface_id, command_key, command_kind, org_id, installation_binding_id,
       repository_binding_id, provider_installation_id, provider_repository_id,
       repository_full_name, provider_run_id, provider_run_attempt,
       provider_job_id, runner_id, runner_name, runner_class,
       primary_problem_code, primary_problem_detail, attempt_count, locked_at
FROM github_provider_surface_commands
WHERE state = 'running'
  AND locked_at IS NOT NULL
  AND locked_at <= @stale_before
ORDER BY locked_at ASC
LIMIT @limit_count;

-- name: MarkProviderSurfaceCommandSucceeded :exec
UPDATE github_provider_surface_commands
SET state = 'succeeded',
    locked_at = NULL,
    completed_at = @completed_at,
    updated_at = @completed_at
WHERE surface_id = @surface_id
  AND state = 'running';

-- name: MarkProviderSurfaceCommandRetryable :exec
UPDATE github_provider_surface_commands
SET state = 'retryable',
    locked_at = NULL,
    next_attempt_at = @next_attempt_at,
    updated_at = @updated_at
WHERE surface_id = @surface_id
  AND state = 'running';

-- name: MarkProviderSurfaceCommandFailed :exec
UPDATE github_provider_surface_commands
SET state = 'failed',
    locked_at = NULL,
    failed_at = @failed_at,
    updated_at = @failed_at
WHERE surface_id = @surface_id
  AND state = 'running';

-- name: UpsertRunnerInstance :exec
INSERT INTO github_runner_instances (
    runner_name,
    origin_provider_job_id,
    origin_demand_id,
    org_id,
    installation_binding_id,
    repository_binding_id,
    provider_installation_id,
    provider_repository_id,
    runner_id,
    runner_class,
    jit_config_sha256,
    assignment_deadline_at,
    state,
    updated_at
) VALUES (
    @runner_name,
    @origin_provider_job_id,
    @origin_demand_id,
    @org_id,
    @installation_binding_id,
    @repository_binding_id,
    @provider_installation_id,
    @provider_repository_id,
    @runner_id,
    @runner_class,
    @jit_config_sha256,
    @assignment_deadline_at,
    @state,
    @updated_at
)
ON CONFLICT (runner_name) DO UPDATE SET
    origin_provider_job_id = COALESCE(NULLIF(EXCLUDED.origin_provider_job_id, 0), github_runner_instances.origin_provider_job_id),
    origin_demand_id = COALESCE(EXCLUDED.origin_demand_id, github_runner_instances.origin_demand_id),
    org_id = COALESCE(NULLIF(EXCLUDED.org_id, ''), github_runner_instances.org_id),
    installation_binding_id = COALESCE(EXCLUDED.installation_binding_id, github_runner_instances.installation_binding_id),
    repository_binding_id = COALESCE(EXCLUDED.repository_binding_id, github_runner_instances.repository_binding_id),
    provider_installation_id = CASE WHEN EXCLUDED.provider_installation_id <> 0 THEN EXCLUDED.provider_installation_id ELSE github_runner_instances.provider_installation_id END,
    provider_repository_id = CASE WHEN EXCLUDED.provider_repository_id <> 0 THEN EXCLUDED.provider_repository_id ELSE github_runner_instances.provider_repository_id END,
    runner_id = EXCLUDED.runner_id,
    runner_class = EXCLUDED.runner_class,
    jit_config_sha256 = EXCLUDED.jit_config_sha256,
    assignment_deadline_at = EXCLUDED.assignment_deadline_at,
    state = EXCLUDED.state,
    updated_at = EXCLUDED.updated_at;

-- name: MarkRunnerInstanceSubmitted :exec
UPDATE github_runner_instances
SET sandbox_allocation_id = @sandbox_allocation_id,
    sandbox_execution_id = @sandbox_execution_id,
    sandbox_attempt_id = @sandbox_attempt_id,
    runner_id = @runner_id,
    runner_name = @runner_name,
    state = 'sandbox_submitted',
    assignment_deadline_at = @assignment_deadline_at,
    updated_at = @updated_at
WHERE github_runner_instances.runner_name = @runner_name;

-- name: MarkRunnerInstanceFailed :execrows
UPDATE github_runner_instances
SET state = 'failed',
    updated_at = @updated_at
WHERE github_runner_instances.runner_name = @runner_name
  AND state IN ('jit_created', 'sandbox_submitted')
  AND NOT EXISTS (
      SELECT 1
      FROM github_job_assignments assigned
      WHERE assigned.runner_name = github_runner_instances.runner_name
  );

-- name: AppendRunnerInstanceProblem :exec
WITH next_problem AS (
    SELECT COALESCE(MAX(problem_seq), 0) + 1 AS problem_seq
    FROM github_runner_instance_problems
    WHERE runner_name = @runner_name
),
inserted AS (
    INSERT INTO github_runner_instance_problems (
        runner_name,
        problem_seq,
        phase,
        problem_type,
        problem_code,
        title,
        detail,
        status,
        retryable,
        pointer,
        observed_at
    )
    SELECT
        @runner_name,
        next_problem.problem_seq,
        @phase,
        @problem_type,
        @problem_code,
        @title,
        @detail,
        @status,
        @retryable,
        @pointer,
        @observed_at
    FROM next_problem
    WHERE EXISTS (
        SELECT 1
        FROM github_runner_instances ri
        WHERE ri.runner_name = @runner_name
    )
    RETURNING runner_name
)
UPDATE github_runner_instances AS ri
SET
    primary_problem_type = CASE WHEN problem_count = 0 THEN @problem_type ELSE primary_problem_type END,
    primary_problem_code = CASE WHEN problem_count = 0 THEN @problem_code ELSE primary_problem_code END,
    primary_problem_status = CASE WHEN problem_count = 0 THEN @status ELSE primary_problem_status END,
    primary_problem_title = CASE WHEN problem_count = 0 THEN @title ELSE primary_problem_title END,
    primary_problem_detail = CASE WHEN problem_count = 0 THEN @detail ELSE primary_problem_detail END,
    problem_count = problem_count + (SELECT COUNT(*) FROM inserted),
    updated_at = @observed_at
WHERE ri.runner_name = @runner_name;

-- name: MarkRunnerInstanceCleaned :exec
UPDATE github_runner_instances
SET state = 'cleaned',
    updated_at = @updated_at
WHERE runner_name = @runner_name;

-- name: MarkRunnerInstanceAssigned :exec
UPDATE github_runner_instances
SET state = CASE WHEN state = 'cleaned' THEN state ELSE @state END,
    updated_at = @updated_at
WHERE runner_name = @runner_name;

-- name: UpsertJobAssignment :exec
WITH stale_runner_assignment AS (
    DELETE FROM github_job_assignments
    WHERE runner_name = @runner_name
      AND provider_job_id <> @provider_job_id
)
INSERT INTO github_job_assignments (
    provider_job_id,
    runner_name,
    runner_id,
    observed_from,
    delivery_id,
    observed_at,
    updated_at
) VALUES (
    @provider_job_id,
    @runner_name,
    @runner_id,
    @observed_from,
    @delivery_id,
    @observed_at,
    @updated_at
)
ON CONFLICT (provider_job_id) DO UPDATE SET
    runner_name = EXCLUDED.runner_name,
    runner_id = EXCLUDED.runner_id,
    observed_from = EXCLUDED.observed_from,
    delivery_id = EXCLUDED.delivery_id,
    observed_at = EXCLUDED.observed_at,
    updated_at = EXCLUDED.updated_at;

-- name: GetRunnerInstanceByRunnerName :one
SELECT runner_name, origin_provider_job_id, origin_demand_id,
       org_id, installation_binding_id, repository_binding_id,
       provider_installation_id, provider_repository_id, runner_id,
       runner_class, jit_config_sha256, sandbox_allocation_id,
       sandbox_execution_id, sandbox_attempt_id, state
FROM github_runner_instances
WHERE runner_name = @runner_name;

-- name: ListSubmittedRunnerInstancesForSandboxReconcile :many
SELECT
    ri.runner_name,
    ri.origin_provider_job_id,
    ri.org_id,
    ri.installation_binding_id,
    ri.repository_binding_id,
    ri.provider_installation_id,
    ri.provider_repository_id,
    COALESCE(d.repository_full_name, '')::text AS repository_full_name,
    COALESCE(d.provider_run_id, 0)::bigint AS provider_run_id,
    COALESCE(d.provider_run_attempt, 0)::bigint AS provider_run_attempt,
    ri.runner_id,
    ri.runner_class,
    ri.sandbox_allocation_id,
    ri.sandbox_execution_id,
    ri.sandbox_attempt_id,
    ri.assignment_deadline_at,
    ri.state,
    ri.updated_at
FROM github_runner_instances ri
LEFT JOIN github_provider_demands d ON d.provider_job_id = ri.origin_provider_job_id
WHERE (
        (ri.state IN ('jit_created', 'sandbox_submitted') AND ri.assignment_deadline_at <= now())
     OR (ri.state = 'sandbox_submitted' AND ri.sandbox_allocation_id IS NOT NULL)
      )
  AND NOT EXISTS (
      SELECT 1
      FROM github_job_assignments assigned
      WHERE assigned.runner_name = ri.runner_name
  )
ORDER BY ri.updated_at ASC
LIMIT @limit_count;

-- name: GetJobAssignmentContext :one
SELECT
    j.provider_job_id,
    COALESCE(NULLIF(d.org_id, ''), j.org_id, '')::text AS org_id,
    COALESCE(d.installation_binding_id, j.installation_binding_id) AS installation_binding_id,
    COALESCE(d.repository_binding_id, j.repository_binding_id) AS repository_binding_id,
    COALESCE(d.provider_installation_id, j.provider_installation_id)::bigint AS provider_installation_id,
    COALESCE(d.provider_repository_id, j.provider_repository_id)::bigint AS provider_repository_id,
    COALESCE(NULLIF(d.repository_full_name, ''), j.repository_full_name, '')::text AS repository_full_name,
    COALESCE(d.provider_run_id, j.provider_run_id)::bigint AS provider_run_id,
    COALESCE(d.provider_run_attempt, j.provider_run_attempt)::bigint AS provider_run_attempt,
    COALESCE(NULLIF(d.runner_class, ''), ri.runner_class, '')::text AS runner_class,
    COALESCE(d.job_shape_id, '')::text AS job_shape_id,
    COALESCE(d.trust_class, '')::text AS trust_class,
    COALESCE(d.state, '')::text AS demand_state,
    COALESCE(a.runner_name, '')::text AS runner_name,
    COALESCE(NULLIF(a.runner_id, 0), ri.runner_id, 0)::bigint AS runner_id,
    COALESCE(ri.origin_provider_job_id, 0)::bigint AS origin_provider_job_id,
    ri.sandbox_allocation_id,
    ri.sandbox_execution_id,
    ri.sandbox_attempt_id,
    COALESCE(ri.state, '')::text AS runner_state
FROM github_workflow_jobs j
LEFT JOIN github_provider_demands d ON d.provider_job_id = j.provider_job_id
LEFT JOIN github_job_assignments a ON a.provider_job_id = j.provider_job_id
LEFT JOIN github_runner_instances ri ON ri.runner_name = a.runner_name
WHERE j.provider_job_id = @provider_job_id;

-- name: CountActiveRunnerInstancesForRunnerClass :one
SELECT count(*)::bigint
FROM github_runner_instances active
WHERE active.provider_repository_id = @provider_repository_id
  AND active.runner_class = @runner_class
  AND EXISTS (
      SELECT 1
      FROM github_provider_demands active_demand
      WHERE active_demand.demand_id = active.origin_demand_id
        AND active_demand.state NOT IN ('completed', 'capacity_failed', 'jit_failed', 'sandbox_failed')
  )
  AND (
      active.state = 'assigned'
      OR (
          active.state IN ('jit_created', 'sandbox_submitted')
          AND active.assignment_deadline_at > now()
      )
  )
  AND NOT EXISTS (
      SELECT 1
      FROM github_job_assignments active_assignment
      JOIN github_workflow_jobs active_job
        ON active_job.provider_job_id = active_assignment.provider_job_id
      WHERE active_assignment.runner_name = active.runner_name
        AND active_job.status = 'completed'
  );

-- name: ListQueuedWorkflowJobsForRunnerSubmission :many
WITH candidates AS (
    SELECT
        j.provider_job_id,
        ib.org_id,
        ib.installation_binding_id,
        rb.repository_binding_id,
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
        COALESCE(d.state, '')::text AS demand_state,
        d.updated_at AS demand_updated_at,
        COALESCE((
            SELECT label
            FROM jsonb_array_elements_text(j.labels_json) AS label
            WHERE label LIKE @runner_class_prefix || '%'
            ORDER BY label
            LIMIT 1
        ), '')::text AS runner_class
    FROM github_workflow_jobs j
    JOIN github_installation_bindings ib
      ON ib.provider_installation_id = j.provider_installation_id
     AND ib.state = 'active'
    JOIN github_repository_bindings rb
      ON rb.installation_binding_id = ib.installation_binding_id
     AND rb.provider_repository_id = j.provider_repository_id
     AND rb.state = 'enabled'
    LEFT JOIN github_provider_demands d ON d.provider_job_id = j.provider_job_id
    WHERE j.status = 'queued'
      AND NOT EXISTS (
          SELECT 1
          FROM github_job_assignments assigned
          WHERE assigned.provider_job_id = j.provider_job_id
      )
      AND (
            d.provider_job_id IS NULL
         OR d.state = 'demand_recorded'
         OR (
                d.state = 'capacity_requested'
            AND NOT EXISTS (
                    SELECT 1
                    FROM github_runner_instances own_instance
                    WHERE own_instance.origin_provider_job_id = j.provider_job_id
                      AND own_instance.state IN ('jit_created', 'sandbox_submitted', 'assigned')
                )
            )
      )
)
SELECT DISTINCT ON (provider_repository_id, runner_class)
    provider_job_id,
    org_id,
    installation_binding_id,
    repository_binding_id,
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
    demand_state,
    demand_updated_at
FROM candidates
WHERE runner_class <> ''
ORDER BY provider_repository_id ASC, runner_class ASC, provider_job_id ASC
LIMIT @limit_count;

-- name: InsertTerminalJobEvidence :one
INSERT INTO github_terminal_job_evidence (
    terminal_evidence_id,
    provider_job_id,
    org_id,
    installation_binding_id,
    repository_binding_id,
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
    @org_id,
    @installation_binding_id,
    @repository_binding_id,
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
    org_id = COALESCE(NULLIF(EXCLUDED.org_id, ''), github_terminal_job_evidence.org_id),
    installation_binding_id = COALESCE(EXCLUDED.installation_binding_id, github_terminal_job_evidence.installation_binding_id),
    repository_binding_id = COALESCE(EXCLUDED.repository_binding_id, github_terminal_job_evidence.repository_binding_id),
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
    org_id,
    installation_binding_id,
    repository_binding_id,
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
    @org_id,
    @installation_binding_id,
    @repository_binding_id,
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
    org_id = COALESCE(NULLIF(EXCLUDED.org_id, ''), github_golden_snapshot_barriers.org_id),
    installation_binding_id = COALESCE(EXCLUDED.installation_binding_id, github_golden_snapshot_barriers.installation_binding_id),
    repository_binding_id = COALESCE(EXCLUDED.repository_binding_id, github_golden_snapshot_barriers.repository_binding_id),
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
