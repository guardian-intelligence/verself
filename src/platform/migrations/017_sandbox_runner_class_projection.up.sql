-- Product runner class used to correlate CI labels, direct execution requests,
-- billing allocation, and VM lifecycle evidence.

ALTER TABLE forge_metal.job_events
    ADD COLUMN IF NOT EXISTS runner_class LowCardinality(String) DEFAULT '' AFTER workload_kind;
