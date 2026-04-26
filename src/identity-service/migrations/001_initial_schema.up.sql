CREATE TABLE IF NOT EXISTS identity_organizations (
    org_id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    state TEXT NOT NULL DEFAULT 'active',
    version INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT NOT NULL,
    CHECK (org_id ~ '^[0-9]+$'),
    CHECK (length(btrim(display_name)) > 0),
    CHECK (slug ~ '^[a-z0-9]([a-z0-9-]{0,78}[a-z0-9])?$'),
    CHECK (NOT slug ~ '^org-[0-9]+$'),
    CHECK (state IN ('active')),
    CHECK (version > 0),
    CHECK (length(btrim(created_by)) > 0),
    CHECK (length(btrim(updated_by)) > 0)
);

CREATE TABLE IF NOT EXISTS identity_organization_slug_redirects (
    slug TEXT PRIMARY KEY,
    org_id TEXT NOT NULL REFERENCES identity_organizations (org_id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by TEXT NOT NULL,
    CHECK (slug ~ '^[a-z0-9]([a-z0-9-]{0,78}[a-z0-9])?$'),
    CHECK (length(btrim(created_by)) > 0)
);

CREATE INDEX IF NOT EXISTS identity_organization_slug_redirects_org_idx
    ON identity_organization_slug_redirects (org_id, created_at DESC);

CREATE TABLE IF NOT EXISTS identity_member_capabilities (
    org_id TEXT PRIMARY KEY,
    enabled_keys TEXT[] NOT NULL DEFAULT '{}',
    version INTEGER NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT NOT NULL,
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(btrim(updated_by)) > 0),
    CHECK (version >= 0)
);

CREATE TABLE IF NOT EXISTS identity_org_acl_state (
    org_id TEXT PRIMARY KEY,
    version INTEGER NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT NOT NULL,
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(btrim(updated_by)) > 0),
    CHECK (version > 0)
);

CREATE TABLE IF NOT EXISTS identity_command_results (
    command_id UUID PRIMARY KEY,
    org_id TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    operation_id TEXT NOT NULL,
    idempotency_key_hash TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    result TEXT NOT NULL,
    reason TEXT NOT NULL,
    aggregate_kind TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    aggregate_version INTEGER NOT NULL,
    target_user_id TEXT NOT NULL,
    requested_role_keys TEXT[] NOT NULL DEFAULT '{}',
    expected_role_keys TEXT[] NOT NULL DEFAULT '{}',
    actual_role_keys TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(btrim(actor_id)) > 0),
    CHECK (length(btrim(operation_id)) > 0),
    CHECK (length(btrim(idempotency_key_hash)) = 64),
    CHECK (length(btrim(request_hash)) = 64),
    CHECK (result IN ('accepted', 'rejected')),
    CHECK (length(btrim(reason)) > 0),
    CHECK (length(btrim(aggregate_kind)) > 0),
    CHECK (length(btrim(aggregate_id)) > 0),
    CHECK (aggregate_version > 0),
    CHECK (length(btrim(target_user_id)) > 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS identity_command_results_idempotency_idx
    ON identity_command_results (org_id, actor_id, operation_id, idempotency_key_hash);

CREATE TABLE IF NOT EXISTS identity_domain_event_outbox (
    event_id UUID PRIMARY KEY,
    command_id UUID NOT NULL REFERENCES identity_command_results (command_id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    org_id TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    operation_id TEXT NOT NULL,
    idempotency_key_hash TEXT NOT NULL,
    aggregate_kind TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    aggregate_version INTEGER NOT NULL,
    target_kind TEXT NOT NULL,
    target_id TEXT NOT NULL,
    result TEXT NOT NULL,
    reason TEXT NOT NULL,
    conflict_policy TEXT NOT NULL,
    expected_version INTEGER NOT NULL,
    actual_version INTEGER NOT NULL,
    expected_hash TEXT NOT NULL,
    actual_hash TEXT NOT NULL,
    requested_hash TEXT NOT NULL,
    changed_fields TEXT[] NOT NULL DEFAULT '{}',
    payload JSONB NOT NULL DEFAULT '{}',
    traceparent TEXT NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    projected_at TIMESTAMPTZ,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    CHECK (length(btrim(event_type)) > 0),
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(btrim(actor_id)) > 0),
    CHECK (length(btrim(operation_id)) > 0),
    CHECK (length(btrim(idempotency_key_hash)) = 64),
    CHECK (length(btrim(aggregate_kind)) > 0),
    CHECK (length(btrim(aggregate_id)) > 0),
    CHECK (aggregate_version > 0),
    CHECK (length(btrim(target_kind)) > 0),
    CHECK (length(btrim(target_id)) > 0),
    CHECK (result IN ('accepted', 'rejected')),
    CHECK (length(btrim(reason)) > 0),
    CHECK (length(btrim(conflict_policy)) > 0),
    CHECK (expected_version >= 0),
    CHECK (actual_version >= 0),
    CHECK (length(btrim(expected_hash)) = 64),
    CHECK (length(btrim(actual_hash)) = 64),
    CHECK (length(btrim(requested_hash)) = 64),
    CHECK (attempts >= 0)
);

CREATE INDEX IF NOT EXISTS identity_domain_event_outbox_pending_idx
    ON identity_domain_event_outbox (next_attempt_at, occurred_at, event_id)
    WHERE projected_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_api_credentials (
    credential_id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    client_id TEXT NOT NULL,
    display_name TEXT NOT NULL,
    auth_method TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    policy_version_at_issue INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    revoked_by TEXT,
    last_used_at TIMESTAMPTZ,
    CHECK (length(btrim(credential_id)) > 0),
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(btrim(subject_id)) > 0),
    CHECK (length(btrim(client_id)) > 0),
    CHECK (length(btrim(display_name)) > 0),
    CHECK (auth_method IN ('private_key_jwt', 'client_secret')),
    CHECK (length(btrim(created_by)) > 0),
    CHECK (policy_version_at_issue >= 0),
    CHECK (status IN ('active', 'revoked')),
    CHECK (expires_at IS NULL OR expires_at > created_at),
    CHECK (
        (status = 'active' AND revoked_at IS NULL AND revoked_by IS NULL)
        OR
        (status = 'revoked' AND revoked_at IS NOT NULL AND length(btrim(revoked_by)) > 0)
    )
);

CREATE INDEX IF NOT EXISTS identity_api_credentials_org_subject_idx
    ON identity_api_credentials (org_id, subject_id, status);

CREATE UNIQUE INDEX IF NOT EXISTS identity_api_credentials_active_subject_idx
    ON identity_api_credentials (subject_id)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS identity_api_credential_permissions (
    credential_id TEXT NOT NULL REFERENCES identity_api_credentials (credential_id) ON DELETE CASCADE,
    permission TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (credential_id, permission),
    CHECK (length(btrim(permission)) > 0)
);

CREATE TABLE IF NOT EXISTS identity_api_credential_secrets (
    secret_id TEXT PRIMARY KEY,
    credential_id TEXT NOT NULL REFERENCES identity_api_credentials (credential_id) ON DELETE CASCADE,
    auth_method TEXT NOT NULL,
    provider_key_id TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    secret_hash BYTEA NOT NULL UNIQUE,
    hash_algorithm TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by TEXT NOT NULL,
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    revoked_by TEXT,
    CHECK (length(btrim(secret_id)) > 0),
    CHECK (auth_method IN ('private_key_jwt', 'client_secret')),
    CHECK (length(btrim(provider_key_id)) > 0),
    CHECK (length(btrim(fingerprint)) > 0),
    CHECK (length(secret_hash) > 0),
    CHECK (length(btrim(hash_algorithm)) > 0),
    CHECK (length(btrim(created_by)) > 0),
    CHECK (expires_at IS NULL OR expires_at > created_at),
    CHECK (revoked_at IS NULL OR length(btrim(revoked_by)) > 0)
);

CREATE INDEX IF NOT EXISTS identity_api_credential_secrets_active_idx
    ON identity_api_credential_secrets (credential_id, expires_at)
    WHERE revoked_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS identity_api_credential_secrets_provider_key_idx
    ON identity_api_credential_secrets (auth_method, provider_key_id)
    WHERE revoked_at IS NULL;
