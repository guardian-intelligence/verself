ALTER TABLE forge_metal.metering
    ADD COLUMN IF NOT EXISTS window_id String CODEC(ZSTD(3));

ALTER TABLE forge_metal.metering
    ADD COLUMN IF NOT EXISTS reservation_shape LowCardinality(String) CODEC(ZSTD(3));

ALTER TABLE forge_metal.metering
    ADD COLUMN IF NOT EXISTS reserved_quantity UInt64 CODEC(T64, ZSTD(3));

ALTER TABLE forge_metal.metering
    ADD COLUMN IF NOT EXISTS actual_quantity UInt64 CODEC(T64, ZSTD(3));

ALTER TABLE forge_metal.metering
    ADD COLUMN IF NOT EXISTS billable_quantity UInt64 CODEC(T64, ZSTD(3));

ALTER TABLE forge_metal.metering
    ADD COLUMN IF NOT EXISTS writeoff_quantity UInt64 CODEC(T64, ZSTD(3));

ALTER TABLE forge_metal.metering
    ADD COLUMN IF NOT EXISTS writeoff_charge_units UInt64 CODEC(T64, ZSTD(3));

ALTER TABLE forge_metal.metering
    ADD COLUMN IF NOT EXISTS receivable_units UInt64 CODEC(T64, ZSTD(3));

ALTER TABLE forge_metal.metering
    ADD COLUMN IF NOT EXISTS plan_id LowCardinality(String) DEFAULT '' CODEC(ZSTD(3));

ALTER TABLE forge_metal.metering
    ADD COLUMN IF NOT EXISTS cost_per_unit UInt64 DEFAULT 0 CODEC(T64, ZSTD(3));
