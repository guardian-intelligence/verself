-- Host/guest vmproto runtime telemetry for the vsock control-plane cutover.
--
-- Durations remain nanoseconds to match the wide-event table's existing timing
-- convention. Log counters use the same T64 encoding as other byte metrics.

ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    boot_to_ready_ns    Int64            CODEC(Delta(8), ZSTD(3));

ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    service_start_ns    Int64            CODEC(Delta(8), ZSTD(3));

ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    stdout_bytes        UInt64           CODEC(T64, ZSTD(3));

ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    stderr_bytes        UInt64           CODEC(T64, ZSTD(3));

ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    dropped_log_bytes   UInt64           CODEC(T64, ZSTD(3));
