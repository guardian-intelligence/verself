DROP INDEX IF EXISTS idx_github_runner_registrations_runner_name;

-- Runner names are provider assignment handles, not permanent job identity. A
-- GitHub runner can be rebound to the job GitHub actually selected, then the
-- original queued job needs a replacement runner with the same deterministic
-- name. Only live registrations may reserve a runner name.
CREATE UNIQUE INDEX IF NOT EXISTS idx_github_runner_registrations_runner_name
    ON github_runner_registrations (runner_name)
    WHERE state IN ('jit_created', 'sandbox_submitted');
