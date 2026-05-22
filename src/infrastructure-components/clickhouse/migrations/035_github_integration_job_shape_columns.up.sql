ALTER TABLE verself.github_integration_events
    ADD COLUMN IF NOT EXISTS job_shape_id String DEFAULT '' CODEC(ZSTD(3)) AFTER runner_class,
    ADD COLUMN IF NOT EXISTS trust_class LowCardinality(String) DEFAULT '' AFTER job_shape_id;
