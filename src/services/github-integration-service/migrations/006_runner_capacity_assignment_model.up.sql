CREATE TABLE IF NOT EXISTS github_runner_instances (
    runner_name              TEXT        PRIMARY KEY CHECK (runner_name <> ''),
    origin_provider_job_id   BIGINT      NOT NULL REFERENCES github_workflow_jobs(provider_job_id) ON DELETE CASCADE,
    origin_demand_id         UUID        REFERENCES github_provider_demands(demand_id) ON DELETE SET NULL,
    org_id                   TEXT        NOT NULL DEFAULT '',
    installation_binding_id  UUID,
    repository_binding_id    UUID,
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id   BIGINT      NOT NULL DEFAULT 0,
    runner_id                BIGINT      NOT NULL DEFAULT 0,
    runner_class             TEXT        NOT NULL DEFAULT '',
    jit_config_sha256        TEXT        NOT NULL DEFAULT '',
    sandbox_allocation_id    UUID,
    sandbox_execution_id     UUID,
    sandbox_attempt_id       UUID,
    assignment_deadline_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    state                    TEXT        NOT NULL CHECK (state <> ''),
    failure_reason           TEXT        NOT NULL DEFAULT '',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_github_runner_instances_runner_id
    ON github_runner_instances (provider_installation_id, provider_repository_id, runner_id)
    WHERE runner_id <> 0;
CREATE INDEX IF NOT EXISTS idx_github_runner_instances_origin
    ON github_runner_instances (origin_provider_job_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_github_runner_instances_capacity
    ON github_runner_instances (provider_repository_id, runner_class, state, assignment_deadline_at, updated_at);

CREATE TABLE IF NOT EXISTS github_job_assignments (
    provider_job_id          BIGINT      PRIMARY KEY REFERENCES github_workflow_jobs(provider_job_id) ON DELETE CASCADE,
    runner_name              TEXT        NOT NULL CHECK (runner_name <> ''),
    runner_id                BIGINT      NOT NULL DEFAULT 0,
    observed_from            TEXT        NOT NULL CHECK (observed_from <> ''),
    delivery_id              TEXT        NOT NULL DEFAULT '',
    observed_at              TIMESTAMPTZ NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_github_job_assignments_runner_name
    ON github_job_assignments (runner_name);
CREATE INDEX IF NOT EXISTS idx_github_job_assignments_runner_id
    ON github_job_assignments (runner_id)
    WHERE runner_id <> 0;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'github_provider_demands'
          AND column_name = 'runner_name'
    ) THEN
        EXECUTE $sql$
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
                sandbox_allocation_id,
                sandbox_execution_id,
                sandbox_attempt_id,
                assignment_deadline_at,
                state,
                failure_reason,
                created_at,
                updated_at
            )
            SELECT
                runner_name,
                provider_job_id,
                demand_id,
                org_id,
                installation_binding_id,
                repository_binding_id,
                provider_installation_id,
                provider_repository_id,
                runner_id,
                runner_class,
                jit_config_sha256,
                sandbox_allocation_id,
                sandbox_execution_id,
                sandbox_attempt_id,
                updated_at + interval '2 minutes',
                CASE
                    WHEN state IN ('sandbox_submitted', 'capacity_live') THEN 'sandbox_submitted'
                    WHEN state IN ('assigned', 'completed') THEN state
                    WHEN state IN ('jit_failed', 'sandbox_failed', 'capacity_failed', 'failed') THEN 'failed'
                    ELSE 'jit_created'
                END,
                failure_reason,
                created_at,
                updated_at
            FROM github_provider_demands
            WHERE runner_name <> ''
            ON CONFLICT (runner_name) DO NOTHING
        $sql$;
    END IF;
END $$;

UPDATE github_provider_demands
SET state = CASE
        WHEN state IN ('sandbox_submitted', 'capacity_live') THEN 'capacity_requested'
        WHEN state IN ('runner_registered', 'runner_assigned') THEN 'assigned'
        ELSE state
    END;

ALTER TABLE github_provider_demands DROP COLUMN IF EXISTS runner_name;
ALTER TABLE github_provider_demands DROP COLUMN IF EXISTS runner_id;
ALTER TABLE github_provider_demands DROP COLUMN IF EXISTS jit_config_sha256;
ALTER TABLE github_provider_demands DROP COLUMN IF EXISTS sandbox_allocation_id;
ALTER TABLE github_provider_demands DROP COLUMN IF EXISTS sandbox_execution_id;
ALTER TABLE github_provider_demands DROP COLUMN IF EXISTS sandbox_attempt_id;

DROP INDEX IF EXISTS idx_github_runner_registrations_runner_name;
DROP TABLE IF EXISTS github_runner_registrations;

ALTER TABLE IF EXISTS public.github_job_assignments OWNER TO github_integration_service;
ALTER TABLE IF EXISTS public.github_runner_instances OWNER TO github_integration_service;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO github_integration_service;
