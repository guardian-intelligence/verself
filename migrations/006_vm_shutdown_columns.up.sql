ALTER TABLE forge_metal.ci_events
    ADD COLUMN IF NOT EXISTS vm_exit_wait_ns Int64 CODEC(Delta(8), ZSTD(3)),
    ADD COLUMN IF NOT EXISTS vm_exit_forced UInt8 CODEC(ZSTD(3));
