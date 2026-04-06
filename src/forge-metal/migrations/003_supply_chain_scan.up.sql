-- Supply chain scan telemetry columns.
-- Recorded once per mirror-update scan (event_kind = 'supply-chain-scan').
ALTER TABLE forge_metal.ci_events
    ADD COLUMN IF NOT EXISTS supply_chain_scan_ns Int64 CODEC(Delta(8), ZSTD(3)),
    ADD COLUMN IF NOT EXISTS supply_chain_scan_ok UInt8 CODEC(ZSTD(3)),
    ADD COLUMN IF NOT EXISTS scan_age_findings UInt16 CODEC(T64, ZSTD(3)),
    ADD COLUMN IF NOT EXISTS scan_guarddog_findings UInt16 CODEC(T64, ZSTD(3)),
    ADD COLUMN IF NOT EXISTS scan_jsxray_findings UInt16 CODEC(T64, ZSTD(3)),
    ADD COLUMN IF NOT EXISTS scan_osv_findings UInt16 CODEC(T64, ZSTD(3)),
    ADD COLUMN IF NOT EXISTS scan_tarballs_count UInt32 CODEC(T64, ZSTD(3));
