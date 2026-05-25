ALTER TABLE runner_allocations
    ADD COLUMN IF NOT EXISTS primary_problem_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS primary_problem_code TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS primary_problem_status INTEGER NOT NULL DEFAULT 0 CHECK (primary_problem_status BETWEEN 0 AND 599),
    ADD COLUMN IF NOT EXISTS primary_problem_title TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS primary_problem_detail TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS problem_count INTEGER NOT NULL DEFAULT 0 CHECK (problem_count >= 0);

CREATE TABLE IF NOT EXISTS runner_allocation_problems (
    allocation_id      UUID        NOT NULL REFERENCES runner_allocations(allocation_id) ON DELETE CASCADE,
    problem_seq        INTEGER     NOT NULL CHECK (problem_seq > 0),
    phase              TEXT        NOT NULL CHECK (phase <> ''),
    problem_type       TEXT        NOT NULL CHECK (problem_type <> ''),
    problem_code       TEXT        NOT NULL CHECK (problem_code <> ''),
    title              TEXT        NOT NULL CHECK (title <> ''),
    detail             TEXT        NOT NULL DEFAULT '',
    status             INTEGER     NOT NULL DEFAULT 0 CHECK (status BETWEEN 0 AND 599),
    retryable          BOOLEAN     NOT NULL DEFAULT false,
    pointer            TEXT        NOT NULL DEFAULT '',
    observed_at        TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (allocation_id, problem_seq)
);

CREATE INDEX IF NOT EXISTS idx_runner_allocation_problems_allocation
    ON runner_allocation_problems (allocation_id, observed_at);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'runner_allocations'
          AND column_name = 'failure_reason'
    ) THEN
        INSERT INTO runner_allocation_problems (
            allocation_id,
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
            allocation_id,
            1,
            'legacy',
            'urn:verself:problem:runner-allocation:failed',
            'runner_allocation.failed',
            'Runner allocation failed',
            failure_reason,
            0,
            false,
            '',
            updated_at
        FROM runner_allocations
        WHERE failure_reason <> ''
        ON CONFLICT (allocation_id, problem_seq) DO NOTHING;

        UPDATE runner_allocations
        SET
            primary_problem_type = 'urn:verself:problem:runner-allocation:failed',
            primary_problem_code = 'runner_allocation.failed',
            primary_problem_status = 0,
            primary_problem_title = 'Runner allocation failed',
            primary_problem_detail = failure_reason,
            problem_count = 1
        WHERE failure_reason <> ''
          AND problem_count = 0;

        ALTER TABLE runner_allocations DROP COLUMN failure_reason;
    END IF;
END $$;

ALTER TABLE IF EXISTS public.runner_allocation_problems OWNER TO sandbox_rental;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO sandbox_rental;
