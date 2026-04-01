-- Built guest artifact footprint captured at rootfs build time.
--
-- These columns let us track changes to the generic Firecracker guest image
-- over time without scraping build logs or parsing JSON payloads.

ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    guest_rootfs_tree_bytes        UInt64           CODEC(T64, ZSTD(3));

ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    guest_rootfs_allocated_bytes   UInt64           CODEC(T64, ZSTD(3));

ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    guest_rootfs_filesystem_bytes  UInt64           CODEC(T64, ZSTD(3));

ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    guest_rootfs_used_bytes        UInt64           CODEC(T64, ZSTD(3));

ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    guest_kernel_bytes             UInt64           CODEC(T64, ZSTD(3));

ALTER TABLE ci_events ADD COLUMN IF NOT EXISTS
    guest_package_count            UInt32           CODEC(T64, ZSTD(3));
