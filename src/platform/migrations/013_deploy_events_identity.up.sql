-- Deploy identity correlation baseline for Ansible callbacks.
-- Existing rows read as empty strings because the new columns default to ''.

ALTER TABLE deploy_events
    ADD COLUMN IF NOT EXISTS trace_id String DEFAULT '' CODEC(ZSTD(3)) AFTER deploy_id,
    ADD COLUMN IF NOT EXISTS deploy_run_key String DEFAULT '' CODEC(ZSTD(3)) AFTER trace_id;
