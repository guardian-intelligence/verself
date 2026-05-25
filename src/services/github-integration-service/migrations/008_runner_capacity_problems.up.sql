ALTER TABLE github_provider_demands
    ADD COLUMN IF NOT EXISTS primary_problem_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS primary_problem_code TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS primary_problem_status INTEGER NOT NULL DEFAULT 0 CHECK (primary_problem_status BETWEEN 0 AND 599),
    ADD COLUMN IF NOT EXISTS primary_problem_title TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS primary_problem_detail TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS problem_count INTEGER NOT NULL DEFAULT 0 CHECK (problem_count >= 0);

CREATE TABLE IF NOT EXISTS github_provider_demand_problems (
    provider_job_id          BIGINT      NOT NULL REFERENCES github_provider_demands(provider_job_id) ON DELETE CASCADE,
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
    PRIMARY KEY (provider_job_id, problem_seq)
);

CREATE INDEX IF NOT EXISTS idx_github_provider_demand_problems_demand
    ON github_provider_demand_problems (provider_job_id, observed_at);

ALTER TABLE github_runner_instances
    ADD COLUMN IF NOT EXISTS primary_problem_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS primary_problem_code TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS primary_problem_status INTEGER NOT NULL DEFAULT 0 CHECK (primary_problem_status BETWEEN 0 AND 599),
    ADD COLUMN IF NOT EXISTS primary_problem_title TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS primary_problem_detail TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS problem_count INTEGER NOT NULL DEFAULT 0 CHECK (problem_count >= 0);

CREATE TABLE IF NOT EXISTS github_runner_instance_problems (
    runner_name              TEXT        NOT NULL REFERENCES github_runner_instances(runner_name) ON DELETE CASCADE,
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
    PRIMARY KEY (runner_name, problem_seq)
);

CREATE INDEX IF NOT EXISTS idx_github_runner_instance_problems_runner
    ON github_runner_instance_problems (runner_name, observed_at);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'github_provider_demands'
          AND column_name = 'failure_reason'
    ) THEN
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
            provider_job_id,
            1,
            'legacy',
            'urn:verself:problem:github-runner:capacity_failed',
            'github_runner.capacity_failed',
            'GitHub runner capacity failed',
            failure_reason,
            0,
            false,
            '',
            updated_at
        FROM github_provider_demands
        WHERE failure_reason <> ''
        ON CONFLICT (provider_job_id, problem_seq) DO NOTHING;

        UPDATE github_provider_demands
        SET
            primary_problem_type = 'urn:verself:problem:github-runner:capacity_failed',
            primary_problem_code = 'github_runner.capacity_failed',
            primary_problem_status = 0,
            primary_problem_title = 'GitHub runner capacity failed',
            primary_problem_detail = failure_reason,
            problem_count = 1
        WHERE failure_reason <> ''
          AND problem_count = 0;

        ALTER TABLE github_provider_demands DROP COLUMN failure_reason;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'github_runner_instances'
          AND column_name = 'failure_reason'
    ) THEN
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
            runner_name,
            1,
            'legacy',
            'urn:verself:problem:github-runner:capacity_failed',
            'github_runner.capacity_failed',
            'GitHub runner capacity failed',
            failure_reason,
            0,
            false,
            '',
            updated_at
        FROM github_runner_instances
        WHERE failure_reason <> ''
        ON CONFLICT (runner_name, problem_seq) DO NOTHING;

        UPDATE github_runner_instances
        SET
            primary_problem_type = 'urn:verself:problem:github-runner:capacity_failed',
            primary_problem_code = 'github_runner.capacity_failed',
            primary_problem_status = 0,
            primary_problem_title = 'GitHub runner capacity failed',
            primary_problem_detail = failure_reason,
            problem_count = 1
        WHERE failure_reason <> ''
          AND problem_count = 0;

        ALTER TABLE github_runner_instances DROP COLUMN failure_reason;
    END IF;
END $$;

ALTER TABLE IF EXISTS public.github_provider_demand_problems OWNER TO github_integration_service;
ALTER TABLE IF EXISTS public.github_runner_instance_problems OWNER TO github_integration_service;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO github_integration_service;
