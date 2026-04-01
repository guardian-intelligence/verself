-- Warm-path gating telemetry for clean guest shutdown and filesystem validation.

ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    event_kind                 LowCardinality(String)  CODEC(ZSTD(3));

ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    warm_filesystem_check_ns   Int64                   CODEC(Delta(8), ZSTD(3));

ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    warm_snapshot_promotion_ns Int64                   CODEC(Delta(8), ZSTD(3));

ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    warm_previous_destroy_ns   Int64                   CODEC(Delta(8), ZSTD(3));

ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    warm_filesystem_check_ok   UInt8                   CODEC(ZSTD(3));
