CREATE TABLE profile_subjects (
    subject_id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL DEFAULT '',
    email_cache TEXT NOT NULL DEFAULT '',
    given_name_cache TEXT NOT NULL DEFAULT '',
    family_name_cache TEXT NOT NULL DEFAULT '',
    display_name_cache TEXT NOT NULL DEFAULT '',
    identity_version INTEGER NOT NULL DEFAULT 0,
    identity_synced_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    tombstoned_at TIMESTAMPTZ,
    tombstone_request_id TEXT NOT NULL DEFAULT '',
    tombstoned_by TEXT NOT NULL DEFAULT '',
    CHECK (length(btrim(subject_id)) > 0),
    CHECK (identity_version >= 0)
);

CREATE TABLE profile_preferences (
    subject_id TEXT PRIMARY KEY REFERENCES profile_subjects(subject_id) ON DELETE CASCADE,
    version INTEGER NOT NULL DEFAULT 1,
    locale TEXT NOT NULL,
    timezone TEXT NOT NULL,
    time_display TEXT NOT NULL CHECK (time_display IN ('utc', 'local')),
    theme TEXT NOT NULL CHECK (theme IN ('system', 'light', 'dark')),
    default_surface TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL,
    updated_by TEXT NOT NULL,
    CHECK (version >= 0)
);

CREATE TABLE profile_data_rights_requests (
    request_id TEXT PRIMARY KEY,
    request_type TEXT NOT NULL CHECK (request_type IN ('org_export', 'subject_export', 'subject_erasure')),
    org_id TEXT NOT NULL DEFAULT '',
    subject_id TEXT NOT NULL DEFAULT '',
    requested_at TIMESTAMPTZ NOT NULL,
    requested_by TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('completed', 'partial', 'failed')),
    manifest JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK (length(btrim(request_id)) > 0)
);

CREATE TABLE profile_domain_event_outbox (
    event_id UUID PRIMARY KEY,
    aggregate_subject_id TEXT NOT NULL,
    aggregate_version INTEGER NOT NULL,
    subject TEXT NOT NULL,
    payload JSONB NOT NULL,
    traceparent TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    CHECK (aggregate_version >= 0)
);

CREATE INDEX profile_subjects_org_id_idx ON profile_subjects (org_id, subject_id);
CREATE INDEX profile_data_rights_requests_subject_idx ON profile_data_rights_requests (subject_id, request_type);
CREATE INDEX profile_domain_event_outbox_pending_idx ON profile_domain_event_outbox (created_at) WHERE published_at IS NULL;
