-- name: InsertGoldenVMOperation :one
INSERT INTO golden_vm_operation (
    operation_id, execution_id, attempt_id, allocation_id, org_id, repository_id,
    provider, provider_repository_id, scope_kind, scope_ref, job_shape_id,
    trust_class, source_generation_set_hash, candidate_golden_vm_snapshot_id,
    promotion_eligible, provider_run_id, provider_run_attempt, provider_job_id,
    head_sha, requested_at, updated_at, state
) VALUES (
    sqlc.arg(operation_id), sqlc.arg(execution_id), sqlc.arg(attempt_id), sqlc.narg(allocation_id),
    sqlc.arg(org_id), sqlc.arg(repository_id), sqlc.arg(provider), sqlc.arg(provider_repository_id),
    sqlc.arg(scope_kind), sqlc.arg(scope_ref), sqlc.arg(job_shape_id), sqlc.arg(trust_class),
    sqlc.arg(source_generation_set_hash), sqlc.arg(candidate_golden_vm_snapshot_id),
    sqlc.arg(promotion_eligible), sqlc.arg(provider_run_id), sqlc.arg(provider_run_attempt),
    sqlc.arg(provider_job_id), sqlc.arg(head_sha), sqlc.arg(requested_at),
    sqlc.arg(requested_at), 'requested'
)
ON CONFLICT (operation_id) DO UPDATE SET requested_at = golden_vm_operation.requested_at
RETURNING
    operation_id,
    candidate_golden_vm_snapshot_id,
    source_generation_set_hash;

-- name: MarkGoldenVMOperationCreateQueued :execrows
UPDATE golden_vm_operation
SET state = 'create_queued',
    lease_id = sqlc.arg(lease_id),
    exec_id = sqlc.arg(exec_id),
    create_job_id = sqlc.arg(create_job_id),
    create_queued_at = COALESCE(create_queued_at, sqlc.arg(recorded_at)),
    updated_at = sqlc.arg(recorded_at)
WHERE operation_id = sqlc.arg(operation_id)
  AND state = 'requested';

-- name: MarkGoldenVMOperationCreating :execrows
UPDATE golden_vm_operation
SET state = 'creating',
    creating_started_at = COALESCE(creating_started_at, sqlc.arg(recorded_at)),
    updated_at = sqlc.arg(recorded_at)
WHERE operation_id = sqlc.arg(operation_id)
  AND state IN ('requested', 'create_queued', 'creating');

-- name: MarkGoldenVMOperationCreated :execrows
UPDATE golden_vm_operation
SET state = 'created',
    snapshot_key = sqlc.arg(snapshot_key),
    root_snapshot_ref = sqlc.arg(root_snapshot_ref),
    root_snapshot_guid = sqlc.arg(root_snapshot_guid),
    vmstate_artifact_ref = sqlc.arg(vmstate_artifact_ref),
    memory_artifact_ref = sqlc.arg(memory_artifact_ref),
    mount_snapshots_json = sqlc.arg(mount_snapshots_json)::jsonb,
    state_bytes = sqlc.arg(state_bytes),
    memory_bytes = sqlc.arg(memory_bytes),
    created_at = COALESCE(created_at, sqlc.arg(recorded_at)),
    updated_at = sqlc.arg(recorded_at),
    failure_reason = ''
WHERE operation_id = sqlc.arg(operation_id)
  AND state IN ('creating', 'created');

-- name: MarkGoldenVMOperationPublishing :execrows
UPDATE golden_vm_operation
SET state = 'publishing',
    publishing_started_at = COALESCE(publishing_started_at, sqlc.arg(recorded_at)),
    updated_at = sqlc.arg(recorded_at)
WHERE operation_id = sqlc.arg(operation_id)
  AND state IN ('created', 'publishing');

-- name: MarkGoldenVMOperationPublished :execrows
UPDATE golden_vm_operation
SET state = 'published',
    generation_set_hash = sqlc.arg(generation_set_hash),
    published_at = COALESCE(published_at, sqlc.arg(recorded_at)),
    updated_at = sqlc.arg(recorded_at)
WHERE operation_id = sqlc.arg(operation_id)
  AND state IN ('publishing', 'published');

-- name: MarkGoldenVMOperationPromoting :execrows
UPDATE golden_vm_operation
SET state = 'promoting',
    promoting_started_at = COALESCE(promoting_started_at, sqlc.arg(recorded_at)),
    updated_at = sqlc.arg(recorded_at)
WHERE operation_id = sqlc.arg(operation_id)
  AND state IN ('published', 'promoting');

-- name: MarkGoldenVMOperationCommitted :exec
UPDATE golden_vm_operation
SET result_recorded_at = COALESCE(result_recorded_at, sqlc.arg(recorded_at)),
    promoted_at = COALESCE(promoted_at, sqlc.arg(recorded_at)),
    state = 'committed',
    updated_at = sqlc.arg(recorded_at),
    failure_reason = ''
WHERE operation_id = sqlc.arg(operation_id)
  AND state IN ('published', 'promoting', 'committed');

-- name: MarkGoldenVMOperationSkipped :exec
UPDATE golden_vm_operation
SET result_recorded_at = COALESCE(result_recorded_at, sqlc.arg(recorded_at)),
    state = 'skipped',
    updated_at = sqlc.arg(recorded_at),
    failure_reason = sqlc.arg(failure_reason)
WHERE operation_id = sqlc.arg(operation_id)
  AND state IN ('requested', 'create_queued', 'creating', 'created', 'publishing', 'published', 'promoting');

-- name: MarkGoldenVMOperationFailed :exec
UPDATE golden_vm_operation
SET result_recorded_at = COALESCE(result_recorded_at, sqlc.arg(recorded_at)),
    state = 'failed',
    updated_at = sqlc.arg(recorded_at),
    failure_reason = sqlc.arg(failure_reason)
WHERE operation_id = sqlc.arg(operation_id)
  AND state IN ('requested', 'create_queued', 'creating', 'created', 'publishing', 'published', 'promoting');

-- name: MarkOpenGoldenVMOperationsFailedByAttempt :many
UPDATE golden_vm_operation
SET result_recorded_at = COALESCE(result_recorded_at, sqlc.arg(recorded_at)),
    state = 'failed',
    updated_at = sqlc.arg(recorded_at),
    failure_reason = sqlc.arg(failure_reason)
WHERE attempt_id = sqlc.arg(attempt_id)
  AND state = 'requested'
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

-- name: GetGoldenVMOperationForCreate :one
SELECT
    operation_id,
    execution_id,
    attempt_id,
    allocation_id,
    org_id,
    repository_id,
    provider,
    provider_repository_id,
    scope_kind,
    scope_ref,
    job_shape_id,
    trust_class,
    source_generation_set_hash,
    generation_set_hash,
    candidate_golden_vm_snapshot_id,
    promotion_eligible,
    provider_run_id,
    provider_run_attempt,
    provider_job_id,
    head_sha,
    lease_id,
    exec_id,
    create_job_id,
    snapshot_key,
    root_snapshot_ref,
    root_snapshot_guid,
    vmstate_artifact_ref,
    memory_artifact_ref,
    mount_snapshots_json,
    state_bytes,
    memory_bytes,
    requested_at,
    create_queued_at,
    creating_started_at,
    created_at,
    publishing_started_at,
    published_at,
    promoting_started_at,
    promoted_at,
    result_recorded_at,
    state,
    failure_reason,
    updated_at
FROM golden_vm_operation
WHERE operation_id = sqlc.arg(operation_id);

-- name: GetGoldenVMOperationForAttempt :one
SELECT
    operation_id,
    execution_id,
    attempt_id,
    allocation_id,
    org_id,
    repository_id,
    provider,
    provider_repository_id,
    scope_kind,
    scope_ref,
    job_shape_id,
    trust_class,
    source_generation_set_hash,
    generation_set_hash,
    candidate_golden_vm_snapshot_id,
    promotion_eligible,
    provider_run_id,
    provider_run_attempt,
    provider_job_id,
    head_sha,
    lease_id,
    exec_id,
    create_job_id,
    snapshot_key,
    root_snapshot_ref,
    root_snapshot_guid,
    vmstate_artifact_ref,
    memory_artifact_ref,
    mount_snapshots_json,
    state_bytes,
    memory_bytes,
    requested_at,
    create_queued_at,
    creating_started_at,
    created_at,
    publishing_started_at,
    published_at,
    promoting_started_at,
    promoted_at,
    result_recorded_at,
    state,
    failure_reason,
    updated_at
FROM golden_vm_operation
WHERE attempt_id = sqlc.arg(attempt_id)
  AND state IN ('requested', 'create_queued', 'creating', 'created', 'publishing', 'published', 'promoting')
ORDER BY requested_at DESC, operation_id DESC
LIMIT 1;

-- name: ListGoldenVMCreateOperationsForReconcile :many
SELECT operation_id
FROM golden_vm_operation
WHERE state IN ('create_queued', 'creating', 'created', 'publishing', 'published', 'promoting')
  AND updated_at < (now() - (sqlc.arg(stale_seconds) * interval '1 second'))
ORDER BY updated_at, operation_id
LIMIT sqlc.arg(limit_count);

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
  AND vm.firecracker_abi_hash = sqlc.arg(firecracker_abi_hash)
  AND vm.host_abi_hash = sqlc.arg(host_abi_hash)
  AND vm.network_model_hash = sqlc.arg(network_model_hash)
  AND vm.vsock_model_hash = sqlc.arg(vsock_model_hash)
  AND vm.clock_model_hash = sqlc.arg(clock_model_hash)
  AND vm.vmproto_version = sqlc.arg(vmproto_version)
  AND vm.after_restore_hook_version = sqlc.arg(after_restore_hook_version)
  AND vm.before_snapshot_hook_version = sqlc.arg(before_snapshot_hook_version)
  AND vm.state = 'current';

-- name: TouchGoldenVMSnapshotLastUsed :exec
UPDATE golden_vm_snapshot
SET last_used_at = sqlc.arg(last_used_at)
WHERE golden_vm_snapshot_id = sqlc.arg(golden_vm_snapshot_id);

-- name: InvalidateCurrentGoldenVMSnapshot :execrows
WITH target AS (
    SELECT vm.golden_vm_snapshot_id
    FROM golden_vm_snapshot vm
    WHERE vm.golden_vm_snapshot_id = sqlc.arg(golden_vm_snapshot_id)
      AND vm.state = 'current'
),
clear_pointer AS (
    UPDATE golden_vm_current_pointer p
    SET current_golden_vm_snapshot_id = NULL
    WHERE p.current_golden_vm_snapshot_id = sqlc.arg(golden_vm_snapshot_id)
      AND EXISTS (SELECT 1 FROM target)
    RETURNING 1
)
UPDATE golden_vm_snapshot vm
SET state = 'invalidated',
    expires_at = COALESCE(vm.expires_at, sqlc.arg(invalidated_at)),
    last_used_at = sqlc.arg(invalidated_at)
FROM target
WHERE vm.golden_vm_snapshot_id = target.golden_vm_snapshot_id
  AND (SELECT count(*) FROM clear_pointer) >= 0;

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

-- name: ListReapableGoldenVMSnapshots :many
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
    vm.state,
    vm.created_at,
    vm.expires_at
FROM golden_vm_snapshot vm
JOIN golden_vm_operation op ON op.operation_id = vm.operation_id
WHERE (
      vm.state = 'invalidated'
      OR (
          vm.state = 'reapable'
          AND (
              vm.expires_at IS NULL
              OR vm.expires_at <= sqlc.arg(now_at)
          )
      )
      OR (
          vm.expires_at IS NOT NULL
          AND vm.expires_at <= sqlc.arg(now_at)
          AND vm.state IN ('candidate', 'retained')
      )
  )
ORDER BY
    CASE WHEN vm.state IN ('invalidated', 'reapable') THEN 0 ELSE 1 END,
    COALESCE(vm.expires_at, vm.created_at),
    vm.created_at,
    vm.golden_vm_snapshot_id
LIMIT sqlc.arg(limit_count);

-- name: MarkGoldenVMSnapshotReaping :execrows
WITH target AS (
    SELECT vm.golden_vm_snapshot_id
    FROM golden_vm_snapshot vm
    WHERE vm.golden_vm_snapshot_id = sqlc.arg(golden_vm_snapshot_id)
      AND vm.state IN ('candidate', 'retained', 'invalidated', 'reapable')
),
clear_pointer AS (
    UPDATE golden_vm_current_pointer p
    SET current_golden_vm_snapshot_id = NULL
    WHERE p.current_golden_vm_snapshot_id = sqlc.arg(golden_vm_snapshot_id)
      AND EXISTS (SELECT 1 FROM target)
    RETURNING 1
)
UPDATE golden_vm_snapshot vm
SET state = 'reapable',
    expires_at = COALESCE(vm.expires_at, sqlc.arg(reaping_at))
FROM target
WHERE vm.golden_vm_snapshot_id = target.golden_vm_snapshot_id
  AND (SELECT count(*) FROM clear_pointer) >= 0;

-- name: ListGoldenVMSnapshotRetentionOrgs :many
SELECT DISTINCT org_id
FROM golden_vm_snapshot
WHERE state = 'retained'
ORDER BY org_id
LIMIT sqlc.arg(limit_count);

-- name: ListGoldenVMSnapshotRingOverflow :many
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
    vm.state,
    vm.created_at,
    vm.expires_at
FROM golden_vm_snapshot vm
JOIN golden_vm_operation op ON op.operation_id = vm.operation_id
WHERE vm.org_id = sqlc.arg(org_id)
  AND vm.state = 'retained'
ORDER BY vm.last_used_at DESC, vm.created_at DESC, vm.golden_vm_snapshot_id DESC
OFFSET sqlc.arg(retain_count)
LIMIT sqlc.arg(limit_count);

-- name: MarkGoldenVMSnapshotRingReaping :execrows
WITH target AS (
    SELECT vm.golden_vm_snapshot_id
    FROM golden_vm_snapshot vm
    WHERE vm.org_id = sqlc.arg(org_id)
      AND vm.golden_vm_snapshot_id = sqlc.arg(golden_vm_snapshot_id)
      AND vm.state = 'retained'
      AND (
          SELECT count(*)
          FROM golden_vm_snapshot newer
          WHERE newer.org_id = vm.org_id
            AND newer.state = 'retained'
            AND (newer.last_used_at, newer.created_at, newer.golden_vm_snapshot_id) >= (vm.last_used_at, vm.created_at, vm.golden_vm_snapshot_id)
      ) > sqlc.arg(retain_count)::bigint
)
UPDATE golden_vm_snapshot vm
SET state = 'reapable',
    expires_at = COALESCE(vm.expires_at, sqlc.arg(reaping_at))
FROM target
WHERE vm.golden_vm_snapshot_id = target.golden_vm_snapshot_id
  AND vm.state = 'retained';

-- name: MarkGoldenVMSnapshotReaped :exec
UPDATE golden_vm_snapshot
SET state = 'reaped',
    reaped_at = sqlc.arg(reaped_at)
WHERE golden_vm_snapshot_id = sqlc.arg(golden_vm_snapshot_id)
  AND state = 'reapable';

-- name: PromoteGoldenVMSnapshotCAS :one
WITH candidate AS (
    SELECT
        g.golden_vm_snapshot_id,
        g.firecracker_abi_hash,
        g.host_abi_hash,
        g.network_model_hash,
        g.vsock_model_hash,
        g.clock_model_hash,
        g.vmproto_version,
        g.after_restore_hook_version,
        g.before_snapshot_hook_version
    FROM golden_vm_snapshot g
    WHERE g.golden_vm_snapshot_id = sqlc.arg(candidate_golden_vm_snapshot_id)
      AND g.state = 'candidate'
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
promote_existing AS (
    UPDATE golden_vm_current_pointer p
    SET current_golden_vm_snapshot_id = sqlc.arg(candidate_golden_vm_snapshot_id),
        promoted_by_operation_id = sqlc.arg(operation_id),
        promoted_at = sqlc.arg(promoted_at)
    FROM prior
    JOIN candidate ON TRUE
    LEFT JOIN golden_vm_snapshot current_snapshot
      ON current_snapshot.golden_vm_snapshot_id = prior.prior_snapshot_id
    WHERE p.org_id = sqlc.arg(org_id)
      AND p.repository_id = sqlc.arg(repository_id)
      AND p.provider = sqlc.arg(provider)
      AND p.provider_repository_id = sqlc.arg(provider_repository_id)
      AND p.scope_kind = sqlc.arg(scope_kind)
      AND p.scope_ref = sqlc.arg(scope_ref)
      AND p.job_shape_id = sqlc.arg(job_shape_id)
      AND p.trust_class = sqlc.arg(trust_class)
      AND (
          prior.prior_snapshot_id IS NULL
          OR current_snapshot.golden_vm_snapshot_id IS NULL
          OR current_snapshot.state <> 'current'
          OR current_snapshot.generation_set_hash = sqlc.arg(source_generation_set_hash)
          OR current_snapshot.firecracker_abi_hash <> candidate.firecracker_abi_hash
          OR current_snapshot.host_abi_hash <> candidate.host_abi_hash
          OR current_snapshot.network_model_hash <> candidate.network_model_hash
          OR current_snapshot.vsock_model_hash <> candidate.vsock_model_hash
          OR current_snapshot.clock_model_hash <> candidate.clock_model_hash
          OR current_snapshot.vmproto_version <> candidate.vmproto_version
          OR current_snapshot.after_restore_hook_version <> candidate.after_restore_hook_version
          OR current_snapshot.before_snapshot_hook_version <> candidate.before_snapshot_hook_version
      )
    RETURNING prior.prior_snapshot_id
),
promote_missing AS (
    INSERT INTO golden_vm_current_pointer (
        org_id, repository_id, provider, provider_repository_id, scope_kind,
        scope_ref, job_shape_id, trust_class, current_golden_vm_snapshot_id,
        promoted_by_operation_id, promoted_at
    )
    SELECT
        sqlc.arg(org_id), sqlc.arg(repository_id), sqlc.arg(provider),
        sqlc.arg(provider_repository_id), sqlc.arg(scope_kind), sqlc.arg(scope_ref),
        sqlc.arg(job_shape_id), sqlc.arg(trust_class), sqlc.arg(candidate_golden_vm_snapshot_id),
        sqlc.arg(operation_id), sqlc.arg(promoted_at)
    FROM candidate
    WHERE NOT EXISTS (SELECT 1 FROM prior)
    ON CONFLICT (org_id, repository_id, provider, provider_repository_id, scope_kind, scope_ref, job_shape_id, trust_class)
    DO NOTHING
    RETURNING NULL::uuid AS prior_snapshot_id
),
promote_pointer AS (
    SELECT prior_snapshot_id FROM promote_existing
    UNION ALL
    SELECT prior_snapshot_id FROM promote_missing
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
