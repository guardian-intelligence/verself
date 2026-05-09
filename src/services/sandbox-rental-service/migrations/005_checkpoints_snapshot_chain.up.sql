-- 005_checkpoints_snapshot_chain transitions Checkpoints to a single
-- persistent ZFS dataset per volume with a snapshot-chain for generations.
--
-- The legacy layout was one immutable image dataset per generation
-- (vspool/images/<source_ref>) with a single @ready snapshot, populated by
-- a `zfs send | zfs recv` of the writable clone. That made every save
-- O(used_bytes). The new layout is one persistent dataset per volume
-- (vspool/checkpoints/<volume_id>) with snapshots per generation, populated
-- by `zfs send -i <parent_snap> <clone_snap> | zfs recv <volume_dataset>`.
-- Save is O(delta_bytes); restore stays O(1) via `zfs clone`.
--
-- Pre-release product. There are no customer Checkpoints on prod, so this
-- migration truncates the per-volume tables instead of attempting an
-- in-place data rewrite. The retention sweeper destroys the legacy image
-- datasets before this migration applies; the bootstrap path recreates
-- vspool/checkpoints on the next vm-orchestrator startup via EnsureRoots.

BEGIN;

TRUNCATE TABLE checkpoint_lifecycle_events;
TRUNCATE TABLE execution_volume_mounts;
TRUNCATE TABLE volume_current_generation;
TRUNCATE TABLE volume_generations CASCADE;
TRUNCATE TABLE volumes CASCADE;

-- zfs_source_ref now stores the persistent volume dataset (shared across
-- generations of the same volume), not a per-generation immutable dataset
-- name. Multiple generations on one volume legitimately share the value.
ALTER TABLE volume_generations DROP CONSTRAINT IF EXISTS volume_generations_zfs_source_ref_key;

-- Snapshot ref is the full <dataset>@<gen_name> identity of one generation
-- and is the unique handle the retention sweeper, restore path, and
-- promotion CAS use to address a single point in the chain.
ALTER TABLE volume_generations ALTER COLUMN zfs_snapshot_ref SET NOT NULL;
ALTER TABLE volume_generations ADD CONSTRAINT volume_generations_zfs_snapshot_ref_key UNIQUE (zfs_snapshot_ref);

COMMIT;
