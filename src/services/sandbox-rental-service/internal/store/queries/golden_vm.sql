-- name: InsertGoldenVMOperation :one
INSERT INTO golden_vm_operation (
    operation_id, execution_id, attempt_id, allocation_id, org_id, repository_id,
    provider, provider_repository_id, scope_kind, scope_ref, job_shape_id,
    trust_class, source_generation_set_hash, candidate_golden_vm_snapshot_id,
    provider_run_id, provider_run_attempt, provider_job_id, head_sha,
    requested_at, final_state
) VALUES (
    sqlc.arg(operation_id), sqlc.arg(execution_id), sqlc.arg(attempt_id), sqlc.narg(allocation_id),
    sqlc.arg(org_id), sqlc.arg(repository_id), sqlc.arg(provider), sqlc.arg(provider_repository_id),
    sqlc.arg(scope_kind), sqlc.arg(scope_ref), sqlc.arg(job_shape_id), sqlc.arg(trust_class),
    sqlc.arg(source_generation_set_hash), sqlc.arg(candidate_golden_vm_snapshot_id),
    sqlc.arg(provider_run_id), sqlc.arg(provider_run_attempt), sqlc.arg(provider_job_id), sqlc.arg(head_sha),
    sqlc.arg(requested_at), 'requested'
)
ON CONFLICT (operation_id) DO UPDATE SET requested_at = golden_vm_operation.requested_at
RETURNING
    operation_id,
    candidate_golden_vm_snapshot_id,
    source_generation_set_hash;

-- name: MarkGoldenVMOperationCheckpointStarted :exec
UPDATE golden_vm_operation
SET checkpoint_started_at = COALESCE(checkpoint_started_at, sqlc.arg(checkpoint_started_at))
WHERE operation_id = sqlc.arg(operation_id)
  AND final_state = 'requested';

-- name: MarkGoldenVMOperationCheckpointed :exec
UPDATE golden_vm_operation
SET checkpointed_at = COALESCE(checkpointed_at, sqlc.arg(checkpointed_at)),
    final_state = 'checkpointed'
WHERE operation_id = sqlc.arg(operation_id)
  AND final_state = 'requested';

-- name: MarkGoldenVMOperationCommitted :exec
UPDATE golden_vm_operation
SET result_recorded_at = COALESCE(result_recorded_at, sqlc.arg(recorded_at)),
    final_state = 'committed',
    failure_reason = ''
WHERE operation_id = sqlc.arg(operation_id)
  AND final_state IN ('requested', 'checkpointed');

-- name: MarkGoldenVMOperationSkipped :exec
UPDATE golden_vm_operation
SET result_recorded_at = COALESCE(result_recorded_at, sqlc.arg(recorded_at)),
    final_state = 'skipped',
    failure_reason = sqlc.arg(failure_reason)
WHERE operation_id = sqlc.arg(operation_id)
  AND final_state IN ('requested', 'checkpointed');

-- name: MarkGoldenVMOperationFailed :exec
UPDATE golden_vm_operation
SET result_recorded_at = COALESCE(result_recorded_at, sqlc.arg(recorded_at)),
    final_state = 'failed',
    failure_reason = sqlc.arg(failure_reason)
WHERE operation_id = sqlc.arg(operation_id)
  AND final_state IN ('requested', 'checkpointed');

-- name: MarkOpenGoldenVMOperationsFailedByAttempt :many
UPDATE golden_vm_operation
SET result_recorded_at = COALESCE(result_recorded_at, sqlc.arg(recorded_at)),
    final_state = 'failed',
    failure_reason = sqlc.arg(failure_reason)
WHERE attempt_id = sqlc.arg(attempt_id)
  AND final_state IN ('requested', 'checkpointed')
RETURNING
    operation_id,
    candidate_golden_vm_snapshot_id,
    job_shape_id,
    execution_id,
    attempt_id,
    org_id,
    repository_id,
    provider,
    provider_repository_id,
    provider_run_id,
    provider_run_attempt,
    provider_job_id,
    source_generation_set_hash;

-- name: GetCurrentGoldenVMActivation :one
SELECT
    vm.golden_vm_snapshot_id,
    vm.generation_set_hash,
    vm.root_snapshot_ref,
    vm.root_snapshot_guid,
    vm.snapshot_key,
    vm.vmstate_artifact_ref,
    vm.memory_artifact_ref,
    vm.state_bytes,
    vm.memory_bytes
FROM golden_vm_current_pointer p
JOIN golden_vm_snapshot vm
  ON vm.golden_vm_snapshot_id = p.current_golden_vm_snapshot_id
WHERE p.org_id = sqlc.arg(org_id)
  AND p.repository_id = sqlc.arg(repository_id)
  AND p.provider = sqlc.arg(provider)
  AND p.provider_repository_id = sqlc.arg(provider_repository_id)
  AND p.scope_kind = sqlc.arg(scope_kind)
  AND p.scope_ref = sqlc.arg(scope_ref)
  AND p.job_shape_id = sqlc.arg(job_shape_id)
  AND p.trust_class = sqlc.arg(trust_class)
  AND vm.generation_set_hash = sqlc.arg(generation_set_hash)
  AND vm.state = 'current';

-- name: TouchGoldenVMSnapshotLastUsed :exec
UPDATE golden_vm_snapshot
SET last_used_at = sqlc.arg(last_used_at)
WHERE golden_vm_snapshot_id = sqlc.arg(golden_vm_snapshot_id);

-- name: InsertGoldenVMSnapshot :one
INSERT INTO golden_vm_snapshot (
    golden_vm_snapshot_id, operation_id, org_id, repository_id, provider,
    provider_repository_id, scope_kind, scope_ref, job_shape_id, trust_class,
    generation_set_hash, root_snapshot_ref, root_snapshot_guid, snapshot_key,
    vmstate_artifact_ref, memory_artifact_ref, state_bytes, memory_bytes,
    drive_manifest_hash, mount_manifest_hash, firecracker_abi_hash, host_abi_hash,
    network_model_hash, vsock_model_hash, clock_model_hash, vmproto_version,
    after_restore_hook_version, before_snapshot_hook_version, warm_profile_hash,
    vcpus, memory_mib, provider_run_id, provider_run_attempt, provider_job_id,
    head_sha, tree_hash, state, created_at, last_used_at, expires_at
) VALUES (
    sqlc.arg(golden_vm_snapshot_id), sqlc.arg(operation_id), sqlc.arg(org_id),
    sqlc.arg(repository_id), sqlc.arg(provider), sqlc.arg(provider_repository_id),
    sqlc.arg(scope_kind), sqlc.arg(scope_ref), sqlc.arg(job_shape_id),
    sqlc.arg(trust_class), sqlc.arg(generation_set_hash), sqlc.arg(root_snapshot_ref),
    sqlc.arg(root_snapshot_guid), sqlc.arg(snapshot_key), sqlc.arg(vmstate_artifact_ref),
    sqlc.arg(memory_artifact_ref), sqlc.arg(state_bytes), sqlc.arg(memory_bytes),
    sqlc.arg(drive_manifest_hash), sqlc.arg(mount_manifest_hash), sqlc.arg(firecracker_abi_hash),
    sqlc.arg(host_abi_hash), sqlc.arg(network_model_hash), sqlc.arg(vsock_model_hash),
    sqlc.arg(clock_model_hash), sqlc.arg(vmproto_version), sqlc.arg(after_restore_hook_version),
    sqlc.arg(before_snapshot_hook_version), sqlc.arg(warm_profile_hash),
    sqlc.arg(vcpus), sqlc.arg(memory_mib), sqlc.arg(provider_run_id),
    sqlc.arg(provider_run_attempt), sqlc.arg(provider_job_id), sqlc.arg(head_sha),
    sqlc.arg(tree_hash), sqlc.arg(state), sqlc.arg(created_at), sqlc.arg(last_used_at),
    sqlc.narg(expires_at)
)
ON CONFLICT (golden_vm_snapshot_id) DO UPDATE SET last_used_at = EXCLUDED.last_used_at
RETURNING golden_vm_snapshot_id;

-- name: InsertGoldenVMSnapshotGeneration :exec
INSERT INTO golden_vm_snapshot_generation (
    golden_vm_snapshot_id, durable_scope_id, durable_generation_id, cache_name,
    zfs_snapshot_ref, zfs_snapshot_guid, drive_id, mount_path, bind_paths_json,
    fs_type, read_only, required, sort_order
) VALUES (
    sqlc.arg(golden_vm_snapshot_id), sqlc.arg(durable_scope_id), sqlc.arg(durable_generation_id),
    sqlc.arg(cache_name), sqlc.arg(zfs_snapshot_ref), sqlc.arg(zfs_snapshot_guid),
    sqlc.arg(drive_id), sqlc.arg(mount_path), sqlc.arg(bind_paths_json)::jsonb,
    sqlc.arg(fs_type), sqlc.arg(read_only), sqlc.arg(required), sqlc.arg(sort_order)
)
ON CONFLICT (golden_vm_snapshot_id, durable_scope_id) DO UPDATE SET
    durable_generation_id = EXCLUDED.durable_generation_id,
    zfs_snapshot_ref = EXCLUDED.zfs_snapshot_ref,
    zfs_snapshot_guid = EXCLUDED.zfs_snapshot_guid,
    drive_id = EXCLUDED.drive_id,
    mount_path = EXCLUDED.mount_path,
    bind_paths_json = EXCLUDED.bind_paths_json,
    fs_type = EXCLUDED.fs_type,
    read_only = EXCLUDED.read_only,
    required = EXCLUDED.required,
    sort_order = EXCLUDED.sort_order;

-- name: ListGoldenVMSnapshotCandidatesForRun :many
SELECT
    vm.golden_vm_snapshot_id,
    vm.operation_id,
    op.source_generation_set_hash,
    vm.org_id,
    vm.repository_id,
    vm.provider,
    vm.provider_repository_id,
    vm.scope_kind,
    vm.scope_ref,
    vm.job_shape_id,
    vm.trust_class,
    vm.generation_set_hash,
    vm.snapshot_key,
    vm.provider_run_id,
    vm.provider_run_attempt,
    vm.provider_job_id,
    vm.head_sha
FROM golden_vm_snapshot vm
JOIN golden_vm_operation op ON op.operation_id = vm.operation_id
WHERE vm.provider_run_id = sqlc.arg(provider_run_id)
  AND (sqlc.arg(provider_run_attempt)::bigint = 0 OR vm.provider_run_attempt = sqlc.arg(provider_run_attempt))
  AND vm.head_sha = sqlc.arg(head_sha)
  AND vm.state = 'candidate'
ORDER BY vm.created_at, vm.golden_vm_snapshot_id;

-- name: ListPrunableGoldenVMSnapshots :many
SELECT
    vm.golden_vm_snapshot_id,
    vm.operation_id,
    vm.org_id,
    vm.repository_id,
    vm.provider,
    vm.provider_repository_id,
    vm.provider_run_id,
    vm.provider_run_attempt,
    vm.provider_job_id,
    vm.job_shape_id,
    vm.generation_set_hash,
    op.source_generation_set_hash,
    vm.snapshot_key,
    vm.root_snapshot_ref,
    vm.root_snapshot_guid,
    vm.vmstate_artifact_ref,
    vm.memory_artifact_ref,
    vm.state_bytes,
    vm.memory_bytes,
    vm.created_at,
    vm.expires_at
FROM golden_vm_snapshot vm
JOIN golden_vm_operation op ON op.operation_id = vm.operation_id
WHERE vm.expires_at IS NOT NULL
  AND vm.expires_at <= sqlc.arg(now_at)
  AND vm.state IN ('candidate', 'retained', 'invalidated', 'prunable')
  AND NOT EXISTS (
      SELECT 1
      FROM golden_vm_current_pointer p
      WHERE p.current_golden_vm_snapshot_id = vm.golden_vm_snapshot_id
  )
ORDER BY vm.expires_at, vm.created_at, vm.golden_vm_snapshot_id
LIMIT sqlc.arg(limit_count);

-- name: MarkGoldenVMSnapshotPruning :execrows
UPDATE golden_vm_snapshot vm
SET state = 'prunable'
WHERE vm.golden_vm_snapshot_id = sqlc.arg(golden_vm_snapshot_id)
  AND vm.state IN ('candidate', 'retained', 'invalidated', 'prunable')
  AND NOT EXISTS (
      SELECT 1
      FROM golden_vm_current_pointer p
      WHERE p.current_golden_vm_snapshot_id = vm.golden_vm_snapshot_id
  );

-- name: MarkGoldenVMSnapshotPruned :exec
UPDATE golden_vm_snapshot
SET state = 'pruned',
    pruned_at = sqlc.arg(pruned_at)
WHERE golden_vm_snapshot_id = sqlc.arg(golden_vm_snapshot_id)
  AND state = 'prunable';

-- name: PromoteGoldenVMSnapshotCAS :one
WITH ensure_pointer AS (
    INSERT INTO golden_vm_current_pointer (
        org_id, repository_id, provider, provider_repository_id, scope_kind,
        scope_ref, job_shape_id, trust_class, current_golden_vm_snapshot_id,
        promoted_by_operation_id, promoted_at
    ) VALUES (
        sqlc.arg(org_id), sqlc.arg(repository_id), sqlc.arg(provider),
        sqlc.arg(provider_repository_id), sqlc.arg(scope_kind), sqlc.arg(scope_ref),
        sqlc.arg(job_shape_id), sqlc.arg(trust_class), NULL, NULL, sqlc.arg(promoted_at)
    )
    ON CONFLICT (org_id, repository_id, provider, provider_repository_id, scope_kind, scope_ref, job_shape_id, trust_class)
    DO NOTHING
),
prior AS (
    SELECT p.current_golden_vm_snapshot_id AS prior_snapshot_id
    FROM golden_vm_current_pointer p
    WHERE p.org_id = sqlc.arg(org_id)
      AND p.repository_id = sqlc.arg(repository_id)
      AND p.provider = sqlc.arg(provider)
      AND p.provider_repository_id = sqlc.arg(provider_repository_id)
      AND p.scope_kind = sqlc.arg(scope_kind)
      AND p.scope_ref = sqlc.arg(scope_ref)
      AND p.job_shape_id = sqlc.arg(job_shape_id)
      AND p.trust_class = sqlc.arg(trust_class)
    FOR UPDATE
),
promote_pointer AS (
    UPDATE golden_vm_current_pointer p
    SET current_golden_vm_snapshot_id = sqlc.arg(candidate_golden_vm_snapshot_id),
        promoted_by_operation_id = sqlc.arg(operation_id),
        promoted_at = sqlc.arg(promoted_at)
    FROM prior
    LEFT JOIN golden_vm_snapshot current_snapshot
      ON current_snapshot.golden_vm_snapshot_id = prior.prior_snapshot_id
    JOIN golden_vm_snapshot candidate
      ON candidate.golden_vm_snapshot_id = sqlc.arg(candidate_golden_vm_snapshot_id)
    WHERE p.org_id = sqlc.arg(org_id)
      AND p.repository_id = sqlc.arg(repository_id)
      AND p.provider = sqlc.arg(provider)
      AND p.provider_repository_id = sqlc.arg(provider_repository_id)
      AND p.scope_kind = sqlc.arg(scope_kind)
      AND p.scope_ref = sqlc.arg(scope_ref)
      AND p.job_shape_id = sqlc.arg(job_shape_id)
      AND p.trust_class = sqlc.arg(trust_class)
      AND candidate.state = 'candidate'
      AND (
          prior.prior_snapshot_id IS NULL
          OR current_snapshot.generation_set_hash = sqlc.arg(source_generation_set_hash)
      )
    RETURNING prior.prior_snapshot_id
),
promote_candidate AS (
    UPDATE golden_vm_snapshot candidate
    SET state = 'current',
        last_used_at = sqlc.arg(promoted_at),
        expires_at = NULL
    WHERE candidate.golden_vm_snapshot_id = sqlc.arg(candidate_golden_vm_snapshot_id)
      AND EXISTS (SELECT 1 FROM promote_pointer)
    RETURNING candidate.golden_vm_snapshot_id
),
retain_prior AS (
    UPDATE golden_vm_snapshot old
    SET state = 'retained',
        expires_at = sqlc.narg(expires_at)
    WHERE old.golden_vm_snapshot_id = (SELECT prior_snapshot_id FROM promote_pointer)
      AND old.golden_vm_snapshot_id IS NOT NULL
      AND old.golden_vm_snapshot_id <> sqlc.arg(candidate_golden_vm_snapshot_id)
      AND old.state = 'current'
)
SELECT
    EXISTS (SELECT 1 FROM promote_candidate)::boolean AS promoted,
    COALESCE((SELECT prior_snapshot_id FROM promote_pointer), '00000000-0000-0000-0000-000000000000'::uuid) AS prior_snapshot_id;
