-- Replace host filesystem-check warm gating telemetry with guest supervisor manifest gating.
ALTER TABLE forge_metal.ci_events
    ADD COLUMN IF NOT EXISTS warm_promotion_gate LowCardinality(String) CODEC(ZSTD(3)) AFTER warm_previous_destroy_ns,
    ADD COLUMN IF NOT EXISTS warm_host_fs_check_used UInt8 CODEC(ZSTD(3)) AFTER warm_promotion_gate,
    ADD COLUMN IF NOT EXISTS warm_guest_manifest_ok UInt8 CODEC(ZSTD(3)) AFTER warm_host_fs_check_used,
    DROP COLUMN IF EXISTS warm_filesystem_check_ns,
    DROP COLUMN IF EXISTS warm_filesystem_check_ok;
