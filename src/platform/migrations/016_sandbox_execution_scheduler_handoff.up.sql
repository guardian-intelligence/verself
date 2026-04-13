-- Projection columns used to correlate API, runner, schedule, and VM-session
-- work through the shared sandbox execution state machine.

ALTER TABLE forge_metal.job_events
    ADD COLUMN IF NOT EXISTS source_kind LowCardinality(String) DEFAULT '' AFTER kind;

ALTER TABLE forge_metal.job_events
    ADD COLUMN IF NOT EXISTS workload_kind LowCardinality(String) DEFAULT '' AFTER source_kind;

ALTER TABLE forge_metal.job_events
    ADD COLUMN IF NOT EXISTS external_provider LowCardinality(String) DEFAULT '' AFTER workload_kind;

ALTER TABLE forge_metal.job_events
    ADD COLUMN IF NOT EXISTS external_task_id String DEFAULT '' CODEC(ZSTD(3)) AFTER external_provider;
