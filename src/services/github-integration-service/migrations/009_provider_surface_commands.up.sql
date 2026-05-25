CREATE TABLE IF NOT EXISTS github_provider_surface_commands (
    surface_id               UUID        PRIMARY KEY,
    command_key              TEXT        NOT NULL UNIQUE CHECK (command_key <> ''),
    command_kind             TEXT        NOT NULL CHECK (command_kind <> ''),
    org_id                   TEXT        NOT NULL DEFAULT '',
    installation_binding_id  UUID,
    repository_binding_id    UUID,
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id   BIGINT      NOT NULL DEFAULT 0,
    repository_full_name     TEXT        NOT NULL DEFAULT '',
    provider_run_id          BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt     BIGINT      NOT NULL DEFAULT 0,
    provider_job_id          BIGINT      NOT NULL DEFAULT 0,
    runner_id                BIGINT      NOT NULL DEFAULT 0,
    runner_name              TEXT        NOT NULL DEFAULT '',
    runner_class             TEXT        NOT NULL DEFAULT '',
    state                    TEXT        NOT NULL CHECK (state <> ''),
    primary_problem_type     TEXT        NOT NULL DEFAULT '',
    primary_problem_code     TEXT        NOT NULL DEFAULT '',
    primary_problem_status   INTEGER     NOT NULL DEFAULT 0 CHECK (primary_problem_status BETWEEN 0 AND 599),
    primary_problem_title    TEXT        NOT NULL DEFAULT '',
    primary_problem_detail   TEXT        NOT NULL DEFAULT '',
    problem_count            INTEGER     NOT NULL DEFAULT 0 CHECK (problem_count >= 0),
    attempt_count            INTEGER     NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at          TIMESTAMPTZ,
    locked_at                TIMESTAMPTZ,
    completed_at             TIMESTAMPTZ,
    failed_at                TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_github_provider_surface_commands_ready
    ON github_provider_surface_commands (next_attempt_at NULLS FIRST, created_at)
    WHERE state IN ('pending', 'retryable');
CREATE INDEX IF NOT EXISTS idx_github_provider_surface_commands_running
    ON github_provider_surface_commands (locked_at, updated_at)
    WHERE state = 'running';
CREATE INDEX IF NOT EXISTS idx_github_provider_surface_commands_job
    ON github_provider_surface_commands (provider_job_id, updated_at)
    WHERE provider_job_id <> 0;

CREATE TABLE IF NOT EXISTS github_provider_surface_problems (
    surface_id               UUID        NOT NULL REFERENCES github_provider_surface_commands(surface_id) ON DELETE CASCADE,
    problem_seq              INTEGER     NOT NULL CHECK (problem_seq > 0),
    phase                    TEXT        NOT NULL CHECK (phase <> ''),
    problem_type             TEXT        NOT NULL CHECK (problem_type <> ''),
    problem_code             TEXT        NOT NULL CHECK (problem_code <> ''),
    title                    TEXT        NOT NULL CHECK (title <> ''),
    detail                   TEXT        NOT NULL DEFAULT '',
    status                   INTEGER     NOT NULL DEFAULT 0 CHECK (status BETWEEN 0 AND 599),
    retryable                BOOLEAN     NOT NULL DEFAULT false,
    pointer                  TEXT        NOT NULL DEFAULT '',
    observed_at              TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (surface_id, problem_seq)
);

CREATE INDEX IF NOT EXISTS idx_github_provider_surface_problems_surface
    ON github_provider_surface_problems (surface_id, observed_at);

ALTER TABLE IF EXISTS public.github_provider_surface_commands OWNER TO github_integration_service;
ALTER TABLE IF EXISTS public.github_provider_surface_problems OWNER TO github_integration_service;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO github_integration_service;
