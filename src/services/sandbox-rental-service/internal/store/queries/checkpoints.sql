-- name: UpsertVolume :one
INSERT INTO volumes (
    volume_id, org_id, provider, provider_installation_id, provider_repository_id,
    repository_full_name, scope_kind, scope_ref, default_scope_ref, product_kind,
    component_kind, key_hash, key, display_name, retention_policy, state,
    created_at, updated_at, last_used_at
) VALUES (
    sqlc.arg(volume_id), sqlc.arg(org_id), sqlc.arg(provider), sqlc.arg(provider_installation_id),
    sqlc.arg(provider_repository_id), sqlc.arg(repository_full_name), sqlc.arg(scope_kind),
    sqlc.arg(scope_ref), sqlc.arg(default_scope_ref), sqlc.arg(product_kind),
    sqlc.arg(component_kind), sqlc.arg(key_hash), sqlc.arg(key), sqlc.arg(display_name),
    sqlc.arg(retention_policy), 'active', sqlc.arg(now), sqlc.arg(now), sqlc.arg(now)
)
ON CONFLICT (
    org_id, provider, provider_repository_id, scope_kind, scope_ref,
    product_kind, component_kind, key_hash
) DO UPDATE SET
    repository_full_name = COALESCE(NULLIF(EXCLUDED.repository_full_name, ''), volumes.repository_full_name),
    key = COALESCE(NULLIF(EXCLUDED.key, ''), volumes.key),
    display_name = COALESCE(NULLIF(EXCLUDED.display_name, ''), volumes.display_name),
    updated_at = EXCLUDED.updated_at,
    last_used_at = EXCLUDED.last_used_at
RETURNING (xmax = 0)::boolean AS created, volume_id, org_id, provider, provider_installation_id, provider_repository_id,
    repository_full_name, scope_kind, scope_ref, default_scope_ref, product_kind,
    component_kind, key_hash, key, display_name, retention_policy, state,
    created_at, updated_at, last_used_at;

-- name: GetCurrentVolumeGeneration :one
SELECT
    vg.volume_generation_id,
    vg.generation,
    vg.zfs_source_ref,
    vg.zfs_snapshot_ref
FROM volume_current_generation vcg
JOIN volume_generations vg ON vg.volume_generation_id = vcg.volume_generation_id
WHERE vcg.org_id = sqlc.arg(org_id)
  AND vcg.volume_id = sqlc.arg(volume_id)
  AND vcg.trust_class = sqlc.arg(trust_class);

-- name: InsertExecutionVolumeMount :one
INSERT INTO execution_volume_mounts (
    mount_id, execution_id, attempt_id, allocation_id, volume_id, mount_name,
    mount_path, mount_path_hash, key_hash, source_generation_id,
    source_generation, source_ref, mount_state, save_state, requested_at,
    created_at, updated_at
) VALUES (
    sqlc.arg(mount_id), sqlc.arg(execution_id), sqlc.arg(attempt_id),
    sqlc.narg(allocation_id), sqlc.arg(volume_id), sqlc.arg(mount_name),
    sqlc.arg(mount_path), sqlc.arg(mount_path_hash), sqlc.arg(key_hash),
    sqlc.narg(source_generation_id), sqlc.narg(source_generation),
    sqlc.arg(source_ref), 'requested', 'none', sqlc.arg(now), sqlc.arg(now), sqlc.arg(now)
)
ON CONFLICT (attempt_id, key_hash, mount_path_hash) DO UPDATE SET
    updated_at = EXCLUDED.updated_at
RETURNING mount_id, execution_id, attempt_id, allocation_id, volume_id, mount_name,
    mount_path, mount_path_hash, key_hash, source_generation_id, source_generation,
    source_ref, target_source_ref, guest_device_path, mount_state, save_requested,
    save_state, committed_generation_id, failure_reason, requested_at, mounted_at,
    completed_at, created_at, updated_at;

-- name: UpdateExecutionVolumeMountResolving :one
UPDATE execution_volume_mounts
SET mount_state = 'resolving', updated_at = sqlc.arg(now)
WHERE mount_id = sqlc.arg(mount_id)
RETURNING mount_state, save_state;

-- name: UpdateExecutionVolumeMountPreparing :one
UPDATE execution_volume_mounts
SET mount_state = 'preparing_host', updated_at = sqlc.arg(now)
WHERE mount_id = sqlc.arg(mount_id)
RETURNING mount_state, save_state;

-- name: UpdateExecutionVolumeMountAttached :one
UPDATE execution_volume_mounts
SET mount_state = 'attaching',
    guest_device_path = sqlc.arg(guest_device_path),
    updated_at = sqlc.arg(now)
WHERE mount_id = sqlc.arg(mount_id)
RETURNING mount_state, save_state;

-- name: MarkExecutionVolumeMountReady :one
UPDATE execution_volume_mounts
SET mount_state = 'mounted',
    mounted_at = sqlc.arg(now),
    updated_at = sqlc.arg(now)
WHERE mount_id = sqlc.arg(mount_id)
  AND execution_id = sqlc.arg(execution_id)
  AND attempt_id = sqlc.arg(attempt_id)
  AND mount_state = 'attaching'
RETURNING mount_id, execution_id, attempt_id, allocation_id, volume_id, mount_name,
    mount_path, mount_path_hash, key_hash, source_generation_id, source_generation,
    source_ref, target_source_ref, guest_device_path, mount_state, save_requested,
    save_state, committed_generation_id, failure_reason, requested_at, mounted_at,
    completed_at, created_at, updated_at;

-- name: MarkExecutionVolumeMountFailed :exec
UPDATE execution_volume_mounts
SET mount_state = 'mount_failed',
    failure_reason = sqlc.arg(failure_reason),
    completed_at = sqlc.arg(now),
    updated_at = sqlc.arg(now)
WHERE mount_id = sqlc.arg(mount_id);

-- name: RequestExecutionVolumeMountSave :one
UPDATE execution_volume_mounts
SET save_requested = true,
    save_state = 'requested',
    updated_at = sqlc.arg(now)
WHERE mount_id = sqlc.arg(mount_id)
  AND execution_id = sqlc.arg(execution_id)
  AND attempt_id = sqlc.arg(attempt_id)
  AND mount_state = 'mounted'
RETURNING mount_id, execution_id, attempt_id, allocation_id, volume_id, mount_name,
    mount_path, mount_path_hash, key_hash, source_generation_id, source_generation,
    source_ref, target_source_ref, guest_device_path, mount_state, save_requested,
    save_state, committed_generation_id, failure_reason, requested_at, mounted_at,
    completed_at, created_at, updated_at;

-- name: ListSaveRequestedExecutionVolumeMounts :many
SELECT
    m.mount_id, m.execution_id, m.attempt_id, m.allocation_id, m.volume_id,
    m.mount_name, m.mount_path, m.mount_path_hash, m.key_hash,
    m.source_generation_id, m.source_generation, m.source_ref,
    m.target_source_ref, m.guest_device_path, m.mount_state,
    m.save_requested, m.save_state, m.committed_generation_id,
    m.failure_reason, m.requested_at, m.mounted_at, m.completed_at,
    m.created_at, m.updated_at,
    v.org_id, v.provider, v.provider_installation_id, v.provider_repository_id,
    v.repository_full_name, v.scope_kind, v.scope_ref
FROM execution_volume_mounts m
JOIN volumes v ON v.volume_id = m.volume_id
WHERE m.attempt_id = sqlc.arg(attempt_id)
  AND m.save_requested
  AND m.mount_state = 'mounted'
  AND m.save_state = 'requested'
ORDER BY m.created_at, m.mount_id;

-- name: MarkExecutionVolumeMountFinalizing :one
UPDATE execution_volume_mounts
SET mount_state = 'finalizing', updated_at = sqlc.arg(now)
WHERE mount_id = sqlc.arg(mount_id)
RETURNING mount_state, save_state;

-- name: MarkExecutionVolumeMountCommitting :one
UPDATE execution_volume_mounts
SET save_state = 'committing',
    target_source_ref = sqlc.arg(target_source_ref),
    updated_at = sqlc.arg(now)
WHERE mount_id = sqlc.arg(mount_id)
RETURNING mount_state, save_state;

-- name: NextVolumeGeneration :one
SELECT COALESCE(MAX(generation), 0)::integer + 1 AS generation
FROM volume_generations
WHERE volume_id = sqlc.arg(volume_id);

-- name: LockVolumeForGeneration :exec
SELECT volume_id
FROM volumes
WHERE volume_id = sqlc.arg(volume_id)
FOR UPDATE;

-- name: InsertVolumeGeneration :one
INSERT INTO volume_generations (
    volume_generation_id, volume_id, org_id, generation, parent_generation_id,
    trust_class, zfs_source_ref, zfs_snapshot_ref, used_bytes, written_bytes,
    state, created_by_execution_id, created_by_attempt_id, created_at, last_used_at
) VALUES (
    sqlc.arg(volume_generation_id), sqlc.arg(volume_id), sqlc.arg(org_id),
    sqlc.arg(generation), sqlc.narg(parent_generation_id), sqlc.arg(trust_class),
    sqlc.arg(zfs_source_ref), sqlc.arg(zfs_snapshot_ref), sqlc.arg(used_bytes),
    sqlc.arg(written_bytes), 'committed', sqlc.arg(created_by_execution_id),
    sqlc.arg(created_by_attempt_id), sqlc.arg(now), sqlc.arg(now)
)
RETURNING volume_generation_id, volume_id, org_id, generation, parent_generation_id,
    trust_class, zfs_source_ref, zfs_snapshot_ref, used_bytes, written_bytes,
    state, created_by_execution_id, created_by_attempt_id, created_at, last_used_at,
    expires_at;

-- name: InsertPendingVolumeGeneration :one
INSERT INTO volume_generations (
    volume_generation_id, volume_id, org_id, generation, parent_generation_id,
    trust_class, zfs_source_ref, zfs_snapshot_ref, used_bytes, written_bytes,
    state, created_by_execution_id, created_by_attempt_id, created_at, last_used_at
) VALUES (
    sqlc.arg(volume_generation_id), sqlc.arg(volume_id), sqlc.arg(org_id),
    sqlc.arg(generation), sqlc.narg(parent_generation_id), sqlc.arg(trust_class),
    sqlc.arg(zfs_source_ref), sqlc.arg(zfs_snapshot_ref), 0, 0, 'committing',
    sqlc.arg(created_by_execution_id), sqlc.arg(created_by_attempt_id),
    sqlc.arg(now), sqlc.arg(now)
)
RETURNING volume_generation_id, volume_id, org_id, generation, parent_generation_id,
    trust_class, zfs_source_ref, zfs_snapshot_ref, used_bytes, written_bytes,
    state, created_by_execution_id, created_by_attempt_id, created_at, last_used_at,
    expires_at;

-- name: MarkVolumeGenerationCommitted :one
UPDATE volume_generations
SET state = 'committed',
    zfs_snapshot_ref = sqlc.arg(zfs_snapshot_ref),
    used_bytes = sqlc.arg(used_bytes),
    written_bytes = sqlc.arg(written_bytes),
    last_used_at = sqlc.arg(now)
WHERE volume_generation_id = sqlc.arg(volume_generation_id)
RETURNING volume_generation_id, volume_id, org_id, generation, parent_generation_id,
    trust_class, zfs_source_ref, zfs_snapshot_ref, used_bytes, written_bytes,
    state, created_by_execution_id, created_by_attempt_id, created_at, last_used_at,
    expires_at;

-- name: MarkVolumeGenerationFailed :exec
UPDATE volume_generations
SET state = 'failed',
    last_used_at = sqlc.arg(now)
WHERE volume_generation_id = sqlc.arg(volume_generation_id);

-- name: MarkExecutionVolumeMountCommitted :one
UPDATE execution_volume_mounts
SET save_state = 'committed',
    committed_generation_id = sqlc.arg(committed_generation_id),
    updated_at = sqlc.arg(now)
WHERE mount_id = sqlc.arg(mount_id)
RETURNING mount_state, save_state;

-- name: MarkExecutionVolumeMountCommitFailed :one
UPDATE execution_volume_mounts
SET save_state = 'failed',
    failure_reason = sqlc.arg(failure_reason),
    completed_at = sqlc.arg(now),
    updated_at = sqlc.arg(now)
WHERE mount_id = sqlc.arg(mount_id)
RETURNING mount_state, save_state;

-- name: InsertVolumeCurrentGeneration :execrows
INSERT INTO volume_current_generation (
    org_id, volume_id, trust_class, current_generation, volume_generation_id, updated_at
) VALUES (
    sqlc.arg(org_id), sqlc.arg(volume_id), sqlc.arg(trust_class),
    sqlc.arg(current_generation), sqlc.arg(volume_generation_id), sqlc.arg(now)
)
ON CONFLICT (org_id, volume_id, trust_class) DO NOTHING;

-- name: UpdateVolumeCurrentGenerationCAS :execrows
UPDATE volume_current_generation
SET current_generation = sqlc.arg(current_generation),
    volume_generation_id = sqlc.arg(volume_generation_id),
    updated_at = sqlc.arg(now)
WHERE org_id = sqlc.arg(org_id)
  AND volume_id = sqlc.arg(volume_id)
  AND trust_class = sqlc.arg(trust_class)
  AND current_generation = sqlc.arg(expected_generation);

-- name: MarkExecutionVolumeMountPromotion :one
UPDATE execution_volume_mounts
SET save_state = sqlc.arg(save_state),
    mount_state = 'finalized',
    completed_at = sqlc.arg(now),
    updated_at = sqlc.arg(now)
WHERE mount_id = sqlc.arg(mount_id)
RETURNING mount_state, save_state;

-- name: ListPrunableVolumeGenerationsForAttempt :many
SELECT
    m.mount_id,
    m.execution_id,
    m.attempt_id,
    m.mount_state,
    m.save_state,
    m.key_hash,
    m.mount_path_hash,
    v.provider,
    v.provider_installation_id,
    v.provider_repository_id,
    v.repository_full_name,
    v.scope_kind,
    v.scope_ref,
    vg.volume_generation_id,
    vg.volume_id,
    vg.org_id,
    vg.generation,
    vg.trust_class,
    vg.zfs_source_ref,
    vg.zfs_snapshot_ref
FROM execution_volume_mounts m
JOIN volumes v ON v.volume_id = m.volume_id
JOIN volume_generations vg ON vg.volume_id = m.volume_id
JOIN volume_current_generation vcg
  ON vcg.org_id = vg.org_id
 AND vcg.volume_id = vg.volume_id
 AND vcg.trust_class = vg.trust_class
WHERE m.attempt_id = sqlc.arg(attempt_id)
  AND vg.state = 'committed'
  AND vg.volume_generation_id <> vcg.volume_generation_id
ORDER BY vg.volume_id, vg.generation;

-- name: MarkVolumeGenerationPruned :exec
UPDATE volume_generations
SET state = 'pruned',
    last_used_at = sqlc.arg(now)
WHERE volume_generation_id = sqlc.arg(volume_generation_id)
  AND state = 'committed';

-- name: InsertCheckpointLifecycleEvent :exec
INSERT INTO checkpoint_lifecycle_events (
    event_id, event_name, observed_at, execution_id, attempt_id, mount_id,
    volume_id, source_generation_id, committed_generation_id, org_id, provider,
    provider_installation_id, provider_repository_id, provider_run_id,
    provider_job_id, repository_full_name, runner_class, scope_kind, scope_ref,
    trust_class, key_hash, mount_path_hash, from_mount_state, to_mount_state,
    from_save_state, to_save_state, operation, result, reason_code, used_bytes,
    written_bytes, duration_ms, trace_id, span_id
) VALUES (
    sqlc.arg(event_id), sqlc.arg(event_name), sqlc.arg(observed_at),
    sqlc.arg(execution_id), sqlc.arg(attempt_id), sqlc.narg(mount_id),
    sqlc.narg(volume_id), sqlc.narg(source_generation_id),
    sqlc.narg(committed_generation_id), sqlc.arg(org_id), sqlc.arg(provider),
    sqlc.arg(provider_installation_id), sqlc.arg(provider_repository_id),
    sqlc.arg(provider_run_id), sqlc.arg(provider_job_id),
    sqlc.arg(repository_full_name), sqlc.arg(runner_class), sqlc.arg(scope_kind),
    sqlc.arg(scope_ref), sqlc.arg(trust_class), sqlc.arg(key_hash),
    sqlc.arg(mount_path_hash), sqlc.arg(from_mount_state),
    sqlc.arg(to_mount_state), sqlc.arg(from_save_state), sqlc.arg(to_save_state),
    sqlc.arg(operation), sqlc.arg(result), sqlc.arg(reason_code),
    sqlc.arg(used_bytes), sqlc.arg(written_bytes), sqlc.arg(duration_ms),
    sqlc.arg(trace_id), sqlc.arg(span_id)
);
