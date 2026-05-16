ALTER TABLE verself.durable_events
    ADD COLUMN IF NOT EXISTS pool_size_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)) AFTER written_bytes,
    ADD COLUMN IF NOT EXISTS pool_allocated_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)) AFTER pool_size_bytes,
    ADD COLUMN IF NOT EXISTS pool_free_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)) AFTER pool_allocated_bytes,
    ADD COLUMN IF NOT EXISTS org_quota_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)) AFTER pool_free_bytes,
    ADD COLUMN IF NOT EXISTS org_used_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)) AFTER org_quota_bytes,
    ADD COLUMN IF NOT EXISTS org_available_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)) AFTER org_used_bytes;
