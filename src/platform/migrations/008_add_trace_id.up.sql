-- Trace correlation is now defined directly in the base tables, but keep this
-- migration idempotent for environments that apply it after the table rewrite.
ALTER TABLE forge_metal.metering
    ADD COLUMN IF NOT EXISTS trace_id String DEFAULT '' CODEC(ZSTD(3));

ALTER TABLE forge_metal.job_events
    ADD COLUMN IF NOT EXISTS trace_id String DEFAULT '' CODEC(ZSTD(3));

ALTER TABLE forge_metal.job_events
    ADD COLUMN IF NOT EXISTS repo_id String DEFAULT '' CODEC(ZSTD(3));

ALTER TABLE forge_metal.job_events
    ADD COLUMN IF NOT EXISTS correlation_id String DEFAULT '' CODEC(ZSTD(3));

ALTER TABLE forge_metal.job_events
    ADD COLUMN IF NOT EXISTS verification_run_id String DEFAULT '' CODEC(ZSTD(3));
