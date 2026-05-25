DROP INDEX IF EXISTS idx_runner_allocations_active_job;

ALTER TABLE runner_allocations
    RENAME COLUMN requested_for_provider_job_id TO origin_provider_job_id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_runner_allocations_active_origin_job
    ON runner_allocations (provider, origin_provider_job_id)
    WHERE origin_provider_job_id <> 0
      AND state IN ('pending', 'jit_creating', 'jit_created', 'vm_submitted', 'runner_config_fetched');
