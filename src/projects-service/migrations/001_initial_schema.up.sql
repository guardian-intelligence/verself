CREATE TABLE projects (
    project_id UUID PRIMARY KEY,
    org_id BIGINT NOT NULL CHECK (org_id > 0),
    slug TEXT NOT NULL CHECK (slug ~ '^[a-z0-9]([a-z0-9-]{0,78}[a-z0-9])?$'),
    display_name TEXT NOT NULL CHECK (display_name <> ''),
    description TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK (state IN ('active', 'archived')),
    version BIGINT NOT NULL CHECK (version >= 1),
    created_by TEXT NOT NULL CHECK (created_by <> ''),
    updated_by TEXT NOT NULL CHECK (updated_by <> ''),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    archived_at TIMESTAMPTZ,
    UNIQUE (project_id, org_id),
    UNIQUE (org_id, slug)
);

CREATE INDEX projects_org_state_idx ON projects (org_id, state, created_at DESC, project_id DESC);

CREATE TABLE project_environments (
    environment_id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects (project_id) ON DELETE CASCADE,
    org_id BIGINT NOT NULL CHECK (org_id > 0),
    slug TEXT NOT NULL CHECK (slug ~ '^[a-z0-9]([a-z0-9-]{0,78}[a-z0-9])?$'),
    display_name TEXT NOT NULL CHECK (display_name <> ''),
    kind TEXT NOT NULL CHECK (kind IN ('production', 'preview', 'development', 'custom')),
    state TEXT NOT NULL CHECK (state IN ('active', 'archived')),
    protection_policy JSONB NOT NULL DEFAULT '{}'::jsonb,
    version BIGINT NOT NULL CHECK (version >= 1),
    created_by TEXT NOT NULL CHECK (created_by <> ''),
    updated_by TEXT NOT NULL CHECK (updated_by <> ''),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    archived_at TIMESTAMPTZ,
    UNIQUE (environment_id, project_id, org_id),
    UNIQUE (project_id, slug),
    FOREIGN KEY (project_id, org_id) REFERENCES projects (project_id, org_id) ON DELETE CASCADE
);

CREATE INDEX project_environments_project_state_idx ON project_environments (project_id, state, kind, slug);
CREATE INDEX project_environments_org_idx ON project_environments (org_id, project_id);

CREATE TABLE project_idempotency_records (
    org_id BIGINT NOT NULL CHECK (org_id > 0),
    operation TEXT NOT NULL CHECK (operation <> ''),
    key_hash TEXT NOT NULL CHECK (key_hash ~ '^[a-f0-9]{64}$'),
    request_hash TEXT NOT NULL CHECK (request_hash ~ '^[a-f0-9]{64}$'),
    result_kind TEXT NOT NULL CHECK (result_kind IN ('project', 'environment')),
    result_project_id UUID NOT NULL REFERENCES projects (project_id) ON DELETE CASCADE,
    result_environment_id UUID REFERENCES project_environments (environment_id) ON DELETE CASCADE,
    result_payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK (
        (result_kind = 'project' AND result_environment_id IS NULL)
        OR (result_kind = 'environment' AND result_environment_id IS NOT NULL)
    ),
    PRIMARY KEY (org_id, operation, key_hash),
    FOREIGN KEY (result_project_id, org_id) REFERENCES projects (project_id, org_id) ON DELETE CASCADE,
    FOREIGN KEY (result_environment_id, result_project_id, org_id) REFERENCES project_environments (environment_id, project_id, org_id) ON DELETE CASCADE
);

CREATE TABLE project_events (
    event_id UUID PRIMARY KEY,
    org_id BIGINT NOT NULL CHECK (org_id > 0),
    project_id UUID NOT NULL REFERENCES projects (project_id) ON DELETE CASCADE,
    environment_id UUID,
    event_type TEXT NOT NULL CHECK (event_type <> ''),
    actor_id TEXT NOT NULL CHECK (actor_id <> ''),
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    trace_id TEXT NOT NULL DEFAULT '',
    traceparent TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (project_id, org_id) REFERENCES projects (project_id, org_id) ON DELETE CASCADE,
    FOREIGN KEY (environment_id, project_id, org_id) REFERENCES project_environments (environment_id, project_id, org_id) ON DELETE CASCADE
);

CREATE INDEX project_events_org_created_idx ON project_events (org_id, created_at DESC, event_id DESC);
CREATE INDEX project_events_project_created_idx ON project_events (project_id, created_at DESC, event_id DESC);
