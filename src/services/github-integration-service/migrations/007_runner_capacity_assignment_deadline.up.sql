ALTER TABLE github_runner_instances
    ADD COLUMN IF NOT EXISTS assignment_deadline_at TIMESTAMPTZ;

UPDATE github_runner_instances
SET assignment_deadline_at = CASE
        WHEN state IN ('jit_created', 'sandbox_submitted') THEN updated_at + interval '2 minutes'
        ELSE updated_at
    END
WHERE assignment_deadline_at IS NULL;

ALTER TABLE github_runner_instances
    ALTER COLUMN assignment_deadline_at SET NOT NULL,
    ALTER COLUMN assignment_deadline_at SET DEFAULT now();

DROP INDEX IF EXISTS idx_github_runner_instances_capacity;
CREATE INDEX idx_github_runner_instances_capacity
    ON github_runner_instances (provider_repository_id, runner_class, state, assignment_deadline_at, updated_at);
