-- Firecracker VM metrics columns for ci_events.
-- Added for tracer bullet: zvol->VM->ClickHouse pipeline.
--
-- Compression codecs follow existing conventions:
--   Byte counters: T64 + ZSTD(3)
--   Durations (us): Delta(8) + ZSTD(3)
--   Small integers: ZSTD(3)
--   High-cardinality strings: ZSTD(3)

-- VM boot time from Firecracker process_startup_time_us metric.
ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    vm_boot_time_us      UInt64           CODEC(Delta(8), ZSTD(3));

-- Block device I/O from Firecracker metrics FIFO.
ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    block_read_bytes     UInt64           CODEC(T64, ZSTD(3));
ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    block_write_bytes    UInt64           CODEC(T64, ZSTD(3));
ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    block_read_count     UInt64           CODEC(T64, ZSTD(3));
ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    block_write_count    UInt64           CODEC(T64, ZSTD(3));

-- Network I/O from Firecracker metrics FIFO.
ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    net_rx_bytes         UInt64           CODEC(T64, ZSTD(3));
ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    net_tx_bytes         UInt64           CODEC(T64, ZSTD(3));

-- vCPU exit count (sum of IO + MMIO exits).
ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    vcpu_exit_count      UInt64           CODEC(T64, ZSTD(3));

-- VM exit code (distinct from per-phase exit codes).
ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    vm_exit_code         Int32            CODEC(ZSTD(3));

-- Full job config JSON for reproducibility.
ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    job_config_json      String           CODEC(ZSTD(3));
