-- Trace correlation is now defined directly in the base tables, but keep this
-- migration idempotent for environments that apply it after the table rewrite.
ALTER TABLE forge_metal.metering
    ADD COLUMN IF NOT EXISTS trace_id String DEFAULT '' CODEC(ZSTD(3));

ALTER TABLE forge_metal.job_events
    ADD COLUMN IF NOT EXISTS trace_id String DEFAULT '' CODEC(ZSTD(3));

ALTER TABLE forge_metal.job_events
    ADD COLUMN IF NOT EXISTS repo_id String DEFAULT '' CODEC(ZSTD(3));

ALTER TABLE forge_metal.job_events
    ADD COLUMN IF NOT EXISTS golden_generation_id String DEFAULT '' CODEC(ZSTD(3));

ALTER TABLE forge_metal.job_events
    ADD COLUMN IF NOT EXISTS workflow_path String DEFAULT '' CODEC(ZSTD(3));

ALTER TABLE forge_metal.job_events
    ADD COLUMN IF NOT EXISTS workflow_job_name String DEFAULT '' CODEC(ZSTD(3));

ALTER TABLE forge_metal.job_events
    ADD COLUMN IF NOT EXISTS provider_run_id String DEFAULT '' CODEC(ZSTD(3));

ALTER TABLE forge_metal.job_events
    ADD COLUMN IF NOT EXISTS provider_job_id String DEFAULT '' CODEC(ZSTD(3));

ALTER TABLE forge_metal.job_events
    ADD COLUMN IF NOT EXISTS runner_name LowCardinality(String) DEFAULT '';

ALTER TABLE forge_metal.job_events
    ADD COLUMN IF NOT EXISTS correlation_id String DEFAULT '' CODEC(ZSTD(3));

ALTER TABLE forge_metal.job_events
    ADD COLUMN IF NOT EXISTS verification_run_id String DEFAULT '' CODEC(ZSTD(3));
