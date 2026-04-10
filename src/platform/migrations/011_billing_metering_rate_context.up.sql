ALTER TABLE forge_metal.metering
    ADD COLUMN IF NOT EXISTS receivable_units UInt64 CODEC(T64, ZSTD(3));

ALTER TABLE forge_metal.metering
    ADD COLUMN IF NOT EXISTS plan_id LowCardinality(String) DEFAULT '' CODEC(ZSTD(3));

ALTER TABLE forge_metal.metering
    ADD COLUMN IF NOT EXISTS cost_per_sec UInt64 DEFAULT 0 CODEC(T64, ZSTD(3));
