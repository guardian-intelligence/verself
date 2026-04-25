ALTER TABLE source_ci_runs
    ADD COLUMN IF NOT EXISTS actor_id TEXT NOT NULL DEFAULT 'system:migration' CHECK (actor_id <> '');

ALTER TABLE source_ci_runs
    ALTER COLUMN actor_id DROP DEFAULT;
