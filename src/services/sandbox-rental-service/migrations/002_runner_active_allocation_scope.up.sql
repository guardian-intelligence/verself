DROP INDEX IF EXISTS idx_runner_allocations_active_job;

CREATE UNIQUE INDEX idx_runner_allocations_active_job
    ON runner_allocations (provider, requested_for_provider_job_id)
    WHERE requested_for_provider_job_id <> 0
      AND state IN ('pending', 'jit_creating', 'jit_created', 'vm_submitted', 'runner_config_fetched');
