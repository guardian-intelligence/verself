-- name: UpsertJobShape :one
INSERT INTO job_shape (
    job_shape_id, repository_id, provider, workflow_identity, called_workflow_identity,
    job_identity, matrix_key, runner_class, guest_arch, platform_image_id,
    kernel_image_id, runner_toolchain_image_id, cache_spec_hash, created_at
) VALUES (
    sqlc.arg(job_shape_id), sqlc.arg(repository_id), sqlc.arg(provider),
    sqlc.arg(workflow_identity), sqlc.arg(called_workflow_identity),
    sqlc.arg(job_identity), sqlc.arg(matrix_key), sqlc.arg(runner_class),
    sqlc.arg(guest_arch), sqlc.arg(platform_image_id), sqlc.arg(kernel_image_id),
    sqlc.arg(runner_toolchain_image_id), sqlc.arg(cache_spec_hash),
    sqlc.arg(created_at)
)
ON CONFLICT (
    repository_id, provider, workflow_identity, called_workflow_identity,
    job_identity, matrix_key, runner_class, guest_arch, platform_image_id,
    kernel_image_id, runner_toolchain_image_id, cache_spec_hash
) DO UPDATE SET created_at = job_shape.created_at
RETURNING job_shape_id;

-- name: UpsertDurableScope :one
INSERT INTO durable_scope (
    durable_scope_id, org_id, repository_id, provider, provider_repository_id, scope_kind,
    scope_ref, job_shape_id, cache_name, trust_class, created_at
) VALUES (
    sqlc.arg(durable_scope_id), sqlc.arg(org_id), sqlc.arg(repository_id), sqlc.arg(provider),
    sqlc.arg(provider_repository_id), sqlc.arg(scope_kind), sqlc.arg(scope_ref),
    sqlc.arg(job_shape_id), sqlc.arg(cache_name), sqlc.arg(trust_class), sqlc.arg(created_at)
)
ON CONFLICT (org_id, repository_id, provider, provider_repository_id, scope_kind, scope_ref, job_shape_id, cache_name, trust_class)
DO UPDATE SET created_at = durable_scope.created_at
RETURNING durable_scope_id;

-- name: EnsureDurableCurrentPointer :exec
INSERT INTO durable_current_pointer (durable_scope_id, current_generation_id, promoted_at)
VALUES (sqlc.arg(durable_scope_id), NULL, sqlc.arg(promoted_at))
ON CONFLICT (durable_scope_id) DO NOTHING;

-- name: GetCurrentDurableGeneration :one
SELECT
    g.durable_generation_id,
    g.zfs_snapshot_ref,
    g.zfs_snapshot_guid,
    g.head_sha,
    g.tree_hash
FROM durable_current_pointer p
JOIN durable_generation g ON g.durable_generation_id = p.current_generation_id
WHERE p.durable_scope_id = sqlc.arg(durable_scope_id)
  AND g.state IN ('committed', 'retained');

-- name: InsertDurableOperation :one
INSERT INTO durable_operation (
    operation_id, execution_id, attempt_id, allocation_id, durable_scope_id,
    source_generation_id, source_snapshot_ref, source_skip_reason, candidate_generation_id,
    mount_name, internal_mount_path, bind_paths_json, trust_class,
    promotion_eligible, required, sort_order, requested_at, final_state
) VALUES (
    sqlc.arg(operation_id), sqlc.arg(execution_id), sqlc.arg(attempt_id), sqlc.narg(allocation_id),
    sqlc.arg(durable_scope_id), sqlc.narg(source_generation_id), sqlc.arg(source_snapshot_ref),
    sqlc.arg(source_skip_reason), sqlc.arg(candidate_generation_id), sqlc.arg(mount_name), sqlc.arg(internal_mount_path),
    sqlc.arg(bind_paths_json)::jsonb, sqlc.arg(trust_class),
    sqlc.arg(promotion_eligible), sqlc.arg(required), sqlc.arg(sort_order),
    sqlc.arg(requested_at), 'requested'
)
ON CONFLICT (operation_id) DO UPDATE SET requested_at = durable_operation.requested_at
RETURNING operation_id, durable_scope_id, source_generation_id, source_snapshot_ref, source_skip_reason,
          candidate_generation_id, mount_name, internal_mount_path, bind_paths_json,
          trust_class, promotion_eligible, required, sort_order;

-- name: TouchDurableGenerationLastUsed :exec
UPDATE durable_generation
SET last_used_at = sqlc.arg(last_used_at)
WHERE durable_generation_id = sqlc.arg(durable_generation_id);

-- name: MarkDurableOperationMounted :exec
UPDATE durable_operation
SET host_accepted_at = COALESCE(host_accepted_at, sqlc.arg(mounted_at)),
    mounted_at = COALESCE(mounted_at, sqlc.arg(mounted_at)),
    final_state = 'mounted'
WHERE operation_id = sqlc.arg(operation_id)
  AND final_state = 'requested';

-- name: MarkDurableOperationSkipped :exec
UPDATE durable_operation
SET host_accepted_at = COALESCE(host_accepted_at, sqlc.arg(recorded_at)),
    final_state = 'skipped',
    failure_reason = sqlc.arg(failure_reason),
    result_recorded_at = COALESCE(result_recorded_at, sqlc.arg(recorded_at))
WHERE operation_id = sqlc.arg(operation_id)
  AND final_state IN ('requested', 'mounted');

-- name: MarkDurableOperationSealStarted :exec
UPDATE durable_operation
SET seal_started_at = COALESCE(seal_started_at, sqlc.arg(seal_started_at))
WHERE operation_id = sqlc.arg(operation_id)
  AND final_state IN ('requested', 'mounted');

-- name: MarkDurableOperationFailed :exec
UPDATE durable_operation
SET final_state = 'failed',
    failure_reason = sqlc.arg(failure_reason),
    result_recorded_at = COALESCE(result_recorded_at, sqlc.arg(recorded_at))
WHERE operation_id = sqlc.arg(operation_id)
  AND final_state IN ('requested', 'mounted');

-- name: MarkOpenDurableOperationsFailedByAttempt :many
UPDATE durable_operation
SET final_state = 'failed',
    failure_reason = sqlc.arg(failure_reason),
    result_recorded_at = COALESCE(result_recorded_at, sqlc.arg(recorded_at))
WHERE attempt_id = sqlc.arg(attempt_id)
  AND final_state IN ('requested', 'mounted')
RETURNING
    operation_id,
    durable_scope_id,
    execution_id,
    attempt_id,
    (
        SELECT s.cache_name
        FROM durable_scope s
        WHERE s.durable_scope_id = durable_operation.durable_scope_id
    ) AS cache_name;

-- name: InsertDurableGeneration :one
INSERT INTO durable_generation (
    durable_generation_id, durable_scope_id, operation_id, source_generation_id,
    head_sha, tree_hash, provider_run_id, provider_run_attempt, provider_job_id,
    result, promotion_eligible, state, zfs_snapshot_ref, zfs_snapshot_guid, used_bytes, written_bytes,
    sealed_at, committed_at, last_used_at, expires_at
) VALUES (
    sqlc.arg(durable_generation_id), sqlc.arg(durable_scope_id), sqlc.arg(operation_id),
    sqlc.narg(source_generation_id), sqlc.arg(head_sha), sqlc.arg(tree_hash),
    sqlc.arg(provider_run_id), sqlc.arg(provider_run_attempt), sqlc.arg(provider_job_id),
    sqlc.arg(result), sqlc.arg(promotion_eligible), sqlc.arg(state), sqlc.arg(zfs_snapshot_ref),
    sqlc.arg(zfs_snapshot_guid), sqlc.arg(used_bytes), sqlc.arg(written_bytes), sqlc.arg(sealed_at),
    sqlc.arg(committed_at), sqlc.arg(last_used_at), sqlc.narg(expires_at)
)
ON CONFLICT (operation_id) DO UPDATE SET last_used_at = EXCLUDED.last_used_at
RETURNING durable_generation_id;

-- name: MarkDurableOperationResultRecorded :exec
UPDATE durable_operation
SET final_state = sqlc.arg(final_state),
    sealed_at = COALESCE(sealed_at, sqlc.arg(sealed_at)),
    result_recorded_at = COALESCE(result_recorded_at, sqlc.arg(recorded_at)),
    failure_reason = sqlc.arg(failure_reason)
WHERE operation_id = sqlc.arg(operation_id)
  AND final_state IN ('requested', 'mounted');

-- name: PromoteDurableGenerationCAS :execrows
UPDATE durable_current_pointer
SET current_generation_id = sqlc.arg(candidate_generation_id),
    promoted_by_operation_id = sqlc.arg(operation_id),
    promoted_at = sqlc.arg(promoted_at)
WHERE durable_scope_id = sqlc.arg(durable_scope_id)
  AND (current_generation_id IS NOT DISTINCT FROM sqlc.narg(source_generation_id));

-- name: RetainDurableGeneration :exec
UPDATE durable_generation
SET state = 'retained',
    last_used_at = sqlc.arg(retained_at),
    expires_at = sqlc.arg(expires_at)
WHERE durable_generation_id = sqlc.arg(durable_generation_id)
  AND durable_generation.durable_scope_id = sqlc.arg(durable_scope_id)
  AND state = 'committed'
  AND NOT EXISTS (
      SELECT 1
      FROM durable_current_pointer
      WHERE durable_current_pointer.durable_scope_id = sqlc.arg(durable_scope_id)
        AND durable_current_pointer.current_generation_id = durable_generation.durable_generation_id
  );

-- name: ListPrunableDurableGenerations :many
SELECT
    g.durable_generation_id,
    g.durable_scope_id,
    g.operation_id,
    g.zfs_snapshot_ref,
    g.used_bytes,
    g.written_bytes,
    g.provider_run_id,
    g.provider_run_attempt,
    g.provider_job_id,
    s.org_id,
    s.provider,
    s.provider_repository_id,
    s.cache_name,
    op.execution_id,
    op.attempt_id
FROM durable_generation g
JOIN durable_scope s ON s.durable_scope_id = g.durable_scope_id
JOIN durable_operation op ON op.operation_id = g.operation_id
WHERE g.expires_at <= sqlc.arg(now_at)
  AND g.state IN ('committed', 'retained', 'prunable')
  AND NOT EXISTS (
      SELECT 1
      FROM durable_current_pointer p
      WHERE p.durable_scope_id = g.durable_scope_id
        AND p.current_generation_id = g.durable_generation_id
  )
  AND NOT EXISTS (
      SELECT 1
      FROM durable_operation open_op
      WHERE open_op.source_generation_id = g.durable_generation_id
        AND open_op.final_state IN ('requested', 'mounted')
  )
  AND NOT EXISTS (
      SELECT 1
      FROM durable_generation child
      WHERE child.source_generation_id = g.durable_generation_id
        AND child.state <> 'pruned'
  )
  AND NOT EXISTS (
      SELECT 1
      FROM golden_vm_snapshot_generation vm_gen
      JOIN golden_vm_snapshot vm ON vm.golden_vm_snapshot_id = vm_gen.golden_vm_snapshot_id
      WHERE vm_gen.durable_generation_id = g.durable_generation_id
        AND vm.state IN ('candidate', 'current', 'retained')
  )
ORDER BY g.expires_at, g.committed_at, g.durable_generation_id
LIMIT sqlc.arg(limit_count);

-- name: ListEvictableDurableGenerations :many
SELECT
    g.durable_generation_id,
    g.durable_scope_id,
    g.operation_id,
    g.zfs_snapshot_ref,
    g.used_bytes,
    g.written_bytes,
    g.provider_run_id,
    g.provider_run_attempt,
    g.provider_job_id,
    s.org_id,
    s.provider,
    s.provider_repository_id,
    s.cache_name,
    op.execution_id,
    op.attempt_id
FROM durable_generation g
JOIN durable_scope s ON s.durable_scope_id = g.durable_scope_id
JOIN durable_operation op ON op.operation_id = g.operation_id
WHERE g.state IN ('committed', 'retained', 'prunable')
  AND NOT EXISTS (
      SELECT 1
      FROM durable_current_pointer p
      WHERE p.durable_scope_id = g.durable_scope_id
        AND p.current_generation_id = g.durable_generation_id
  )
  AND NOT EXISTS (
      SELECT 1
      FROM durable_operation open_op
      WHERE open_op.source_generation_id = g.durable_generation_id
        AND open_op.final_state IN ('requested', 'mounted')
  )
  AND NOT EXISTS (
      SELECT 1
      FROM durable_generation child
      WHERE child.source_generation_id = g.durable_generation_id
        AND child.state <> 'pruned'
  )
  AND NOT EXISTS (
      SELECT 1
      FROM golden_vm_snapshot_generation vm_gen
      JOIN golden_vm_snapshot vm ON vm.golden_vm_snapshot_id = vm_gen.golden_vm_snapshot_id
      WHERE vm_gen.durable_generation_id = g.durable_generation_id
        AND vm.state IN ('candidate', 'current', 'retained')
  )
ORDER BY g.last_used_at, g.committed_at, g.durable_generation_id
LIMIT sqlc.arg(limit_count);

-- name: ListDurableCaches :many
SELECT
    s.durable_scope_id,
    s.org_id,
    s.provider,
    s.provider_repository_id,
    COALESCE(r.repository_full_name, '') AS repository_full_name,
    s.scope_ref,
    s.cache_name,
    s.created_at,
    cp.current_generation_id,
    COUNT(g.durable_generation_id)::bigint AS generation_count,
    COALESCE(SUM(g.used_bytes), 0)::bigint AS used_bytes,
    COALESCE(SUM(g.written_bytes), 0)::bigint AS written_bytes,
    COALESCE(MAX(g.last_used_at), s.created_at) AS last_used_at
FROM durable_scope s
LEFT JOIN runner_provider_repositories r
    ON r.provider = s.provider
   AND r.provider_repository_id = s.provider_repository_id
LEFT JOIN durable_current_pointer cp
    ON cp.durable_scope_id = s.durable_scope_id
LEFT JOIN durable_generation g
    ON g.durable_scope_id = s.durable_scope_id
   AND g.state <> 'pruned'
WHERE s.org_id = sqlc.arg(org_id)
GROUP BY
    s.durable_scope_id,
    s.org_id,
    s.provider,
    s.provider_repository_id,
    r.repository_full_name,
    s.scope_ref,
    s.cache_name,
    s.created_at,
    cp.current_generation_id
ORDER BY COALESCE(MAX(g.last_used_at), s.created_at) DESC, s.created_at DESC, s.durable_scope_id
LIMIT sqlc.arg(limit_count);

-- name: GetDurableCache :one
SELECT durable_scope_id
FROM durable_scope
WHERE org_id = sqlc.arg(org_id)
  AND durable_scope_id = sqlc.arg(durable_scope_id);

-- name: ListDurableCacheGenerations :many
SELECT
    g.durable_generation_id,
    g.durable_scope_id,
    g.operation_id,
    g.source_generation_id,
    g.head_sha,
    g.tree_hash,
    g.provider_run_id,
    g.provider_run_attempt,
    g.provider_job_id,
    g.result,
    g.promotion_eligible,
    g.state,
    g.zfs_snapshot_ref,
    g.used_bytes,
    g.written_bytes,
    g.sealed_at,
    g.committed_at,
    g.last_used_at,
    g.expires_at,
    s.org_id,
    s.provider,
    s.provider_repository_id,
    COALESCE(r.repository_full_name, '') AS repository_full_name,
    s.scope_ref,
    s.cache_name,
    op.bind_paths_json,
    op.execution_id,
    op.attempt_id,
    cp.current_generation_id IS NOT DISTINCT FROM g.durable_generation_id AS is_current
FROM durable_generation g
JOIN durable_scope s ON s.durable_scope_id = g.durable_scope_id
JOIN durable_operation op ON op.operation_id = g.operation_id
LEFT JOIN runner_provider_repositories r
    ON r.provider = s.provider
   AND r.provider_repository_id = s.provider_repository_id
LEFT JOIN durable_current_pointer cp
    ON cp.durable_scope_id = g.durable_scope_id
WHERE s.org_id = sqlc.arg(org_id)
  AND g.durable_scope_id = sqlc.arg(durable_scope_id)
  AND g.state <> 'pruned'
ORDER BY g.last_used_at DESC, g.committed_at DESC, g.durable_generation_id
LIMIT sqlc.arg(limit_count);

-- name: GetDurableCacheGenerationForUserPrune :one
SELECT
    g.durable_generation_id,
    g.durable_scope_id,
    g.operation_id,
    g.source_generation_id,
    g.head_sha,
    g.tree_hash,
    g.result,
    g.zfs_snapshot_ref,
    g.used_bytes,
    g.written_bytes,
    g.provider_run_id,
    g.provider_run_attempt,
    g.provider_job_id,
    g.state,
    g.sealed_at,
    g.committed_at,
    g.last_used_at,
    g.expires_at,
    s.org_id,
    s.provider,
    s.provider_repository_id,
    COALESCE(r.repository_full_name, '') AS repository_full_name,
    s.scope_ref,
    s.cache_name,
    op.execution_id,
    op.attempt_id,
    op.bind_paths_json,
    cp.current_generation_id IS NOT DISTINCT FROM g.durable_generation_id AS is_current
FROM durable_generation g
JOIN durable_scope s ON s.durable_scope_id = g.durable_scope_id
JOIN durable_operation op ON op.operation_id = g.operation_id
LEFT JOIN runner_provider_repositories r
    ON r.provider = s.provider
   AND r.provider_repository_id = s.provider_repository_id
LEFT JOIN durable_current_pointer cp
    ON cp.durable_scope_id = g.durable_scope_id
WHERE s.org_id = sqlc.arg(org_id)
  AND g.durable_generation_id = sqlc.arg(durable_generation_id)
  AND g.state <> 'pruned';

-- name: ListDurableCacheGenerationsForPathPrune :many
SELECT
    g.durable_generation_id,
    g.durable_scope_id,
    g.operation_id,
    g.source_generation_id,
    g.head_sha,
    g.tree_hash,
    g.result,
    g.zfs_snapshot_ref,
    g.used_bytes,
    g.written_bytes,
    g.provider_run_id,
    g.provider_run_attempt,
    g.provider_job_id,
    g.state,
    g.sealed_at,
    g.committed_at,
    g.last_used_at,
    g.expires_at,
    s.org_id,
    s.provider,
    s.provider_repository_id,
    COALESCE(r.repository_full_name, '') AS repository_full_name,
    s.scope_ref,
    s.cache_name,
    op.execution_id,
    op.attempt_id,
    op.bind_paths_json,
    cp.current_generation_id IS NOT DISTINCT FROM g.durable_generation_id AS is_current
FROM durable_generation g
JOIN durable_scope s ON s.durable_scope_id = g.durable_scope_id
JOIN durable_operation op ON op.operation_id = g.operation_id
LEFT JOIN runner_provider_repositories r
    ON r.provider = s.provider
   AND r.provider_repository_id = s.provider_repository_id
LEFT JOIN durable_current_pointer cp
    ON cp.durable_scope_id = g.durable_scope_id
WHERE s.org_id = sqlc.arg(org_id)
  AND g.durable_scope_id = sqlc.arg(durable_scope_id)
  AND g.state <> 'pruned'
  AND op.bind_paths_json ? sqlc.arg(bind_path)::text
ORDER BY g.last_used_at DESC, g.committed_at DESC, g.durable_generation_id;

-- name: MarkDurableGenerationUserPruning :execrows
UPDATE durable_generation AS target
SET state = 'prunable',
    last_used_at = sqlc.arg(pruning_at)
FROM durable_scope s
WHERE target.durable_generation_id = sqlc.arg(durable_generation_id)
  AND target.durable_scope_id = sqlc.arg(durable_scope_id)
  AND s.durable_scope_id = target.durable_scope_id
  AND s.org_id = sqlc.arg(org_id)
  AND target.state IN ('committed', 'retained', 'prunable')
  AND NOT EXISTS (
      SELECT 1
      FROM durable_operation open_op
      WHERE open_op.source_generation_id = target.durable_generation_id
        AND open_op.final_state IN ('requested', 'mounted')
  )
  AND NOT EXISTS (
      SELECT 1
      FROM durable_generation child
      WHERE child.source_generation_id = target.durable_generation_id
        AND child.state <> 'pruned'
  );

-- name: ClearDurableCurrentPointerForGeneration :exec
UPDATE durable_current_pointer
SET current_generation_id = NULL,
    promoted_by_operation_id = NULL,
    promoted_at = sqlc.arg(cleared_at)
WHERE durable_scope_id = sqlc.arg(durable_scope_id)
  AND current_generation_id = sqlc.arg(durable_generation_id);

-- name: MarkDurableGenerationPruning :execrows
UPDATE durable_generation AS target
SET state = 'prunable',
    last_used_at = sqlc.arg(pruning_at)
WHERE target.durable_generation_id = sqlc.arg(durable_generation_id)
  AND target.durable_scope_id = sqlc.arg(durable_scope_id)
  AND state IN ('committed', 'retained', 'prunable')
  AND NOT EXISTS (
      SELECT 1
      FROM durable_current_pointer p
      WHERE p.durable_scope_id = target.durable_scope_id
        AND p.current_generation_id = target.durable_generation_id
  )
  AND NOT EXISTS (
      SELECT 1
      FROM durable_operation open_op
      WHERE open_op.source_generation_id = target.durable_generation_id
        AND open_op.final_state IN ('requested', 'mounted')
  )
  AND NOT EXISTS (
      SELECT 1
      FROM durable_generation child
      WHERE child.source_generation_id = target.durable_generation_id
        AND child.state <> 'pruned'
  );

-- name: MarkDurableGenerationPruned :exec
UPDATE durable_generation
SET state = 'pruned',
    last_used_at = sqlc.arg(pruned_at)
WHERE durable_generation_id = sqlc.arg(durable_generation_id)
  AND durable_generation.durable_scope_id = sqlc.arg(durable_scope_id)
  AND state = 'prunable';

-- name: ListDurablePromotionCandidatesForRun :many
SELECT
    g.durable_generation_id,
    g.durable_scope_id,
    g.operation_id,
    g.source_generation_id,
    g.zfs_snapshot_ref,
    g.used_bytes,
    g.written_bytes,
    s.cache_name,
    op.execution_id,
    op.attempt_id,
    g.provider_job_id
FROM durable_generation g
JOIN durable_scope s ON s.durable_scope_id = g.durable_scope_id
JOIN durable_operation op ON op.operation_id = g.operation_id
WHERE g.provider_run_id = sqlc.arg(provider_run_id)
  AND (sqlc.arg(provider_run_attempt)::bigint = 0 OR g.provider_run_attempt = sqlc.arg(provider_run_attempt))
  AND g.head_sha = sqlc.arg(head_sha)
  AND g.promotion_eligible
  AND g.state = 'committed'
ORDER BY g.committed_at, g.durable_generation_id;

-- name: ListGoldenVMDurableOperations :many
SELECT
    op.operation_id,
    op.durable_scope_id,
    op.source_generation_id,
    op.source_snapshot_ref,
    op.source_skip_reason,
    op.candidate_generation_id,
    op.mount_name,
    op.internal_mount_path,
    op.bind_paths_json,
    op.trust_class,
    op.promotion_eligible,
    op.required,
    op.sort_order,
    op.final_state,
    scope.cache_name
FROM durable_operation op
JOIN durable_scope scope ON scope.durable_scope_id = op.durable_scope_id
WHERE op.attempt_id = sqlc.arg(attempt_id)
ORDER BY op.sort_order, scope.cache_name, op.operation_id;

-- name: ListTerminalAttemptsWithOpenDurableOperations :many
SELECT DISTINCT
    a.execution_id,
    a.attempt_id,
    a.state AS attempt_state,
    a.failure_reason AS attempt_failure_reason
FROM durable_operation op
JOIN execution_attempts a ON a.attempt_id = op.attempt_id
WHERE op.final_state IN ('requested', 'mounted')
  AND a.state IN ('succeeded', 'failed', 'canceled', 'lost')
  AND NOT EXISTS (
      SELECT 1
      FROM golden_vm_operation g
      WHERE g.attempt_id = a.attempt_id
        AND g.state IN ('create_queued', 'creating', 'created', 'publishing', 'published', 'promoting')
  );

-- name: GetDurableRunRepository :one
SELECT
    p.org_id,
    provider_installation_id,
    gi.provider_repository_id,
    gi.repository_full_name
FROM github_workflow_invocations gi
JOIN runner_provider_repositories p ON p.provider = 'github'
    AND p.provider_repository_id = gi.provider_repository_id
    AND p.active
WHERE gi.provider_repository_id = sqlc.arg(provider_repository_id)
  AND gi.provider_run_id = sqlc.arg(provider_run_id)
ORDER BY gi.provider_run_attempt DESC
LIMIT 1;
