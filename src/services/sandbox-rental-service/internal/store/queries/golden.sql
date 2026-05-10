-- name: UpsertJobShape :one
INSERT INTO job_shapes (
    job_shape_id, org_id, provider, provider_repository_id, workflow_identity,
    called_workflow_identity, job_identity, matrix_key, runner_class,
    platform_image_id, durable_manifest_hash, checkout_policy_hash, created_at
) VALUES (
    sqlc.arg(job_shape_id), sqlc.arg(org_id), sqlc.arg(provider),
    sqlc.arg(provider_repository_id), sqlc.arg(workflow_identity),
    sqlc.arg(called_workflow_identity), sqlc.arg(job_identity),
    sqlc.arg(matrix_key), sqlc.arg(runner_class), sqlc.arg(platform_image_id),
    sqlc.arg(durable_manifest_hash), sqlc.arg(checkout_policy_hash),
    sqlc.arg(created_at)
)
ON CONFLICT (
    org_id, provider, provider_repository_id, workflow_identity,
    called_workflow_identity, job_identity, matrix_key, runner_class,
    platform_image_id, durable_manifest_hash, checkout_policy_hash
) DO UPDATE SET created_at = job_shapes.created_at
RETURNING job_shape_id;

-- name: UpsertGoldenScope :one
INSERT INTO golden_scopes (
    golden_scope_id, org_id, provider, provider_repository_id, scope_kind,
    scope_ref, job_shape_id, trust_class, created_at
) VALUES (
    sqlc.arg(golden_scope_id), sqlc.arg(org_id), sqlc.arg(provider),
    sqlc.arg(provider_repository_id), sqlc.arg(scope_kind),
    sqlc.arg(scope_ref), sqlc.arg(job_shape_id), sqlc.arg(trust_class),
    sqlc.arg(created_at)
)
ON CONFLICT (
    org_id, provider, provider_repository_id, scope_kind, scope_ref,
    job_shape_id, trust_class
) DO UPDATE SET created_at = golden_scopes.created_at
RETURNING golden_scope_id;

-- name: GetCurrentGoldenGeneration :one
SELECT
    g.golden_generation_id,
    g.zfs_snapshot_ref
FROM golden_current_pointer p
JOIN golden_generations g ON g.golden_generation_id = p.current_generation_id
WHERE p.golden_scope_id = sqlc.arg(golden_scope_id)
  AND g.state = 'committed';

-- name: InsertWorkspaceOperation :one
INSERT INTO workspace_operations (
    operation_id, execution_id, attempt_id, allocation_id, golden_scope_id,
    job_shape_id, source_generation_id, source_snapshot_ref,
    candidate_generation_id, mount_name, mount_path, trust_class,
    taint_policy, final_state, requested_at
) VALUES (
    sqlc.arg(operation_id), sqlc.arg(execution_id), sqlc.arg(attempt_id),
    sqlc.narg(allocation_id), sqlc.arg(golden_scope_id),
    sqlc.arg(job_shape_id), sqlc.narg(source_generation_id),
    sqlc.arg(source_snapshot_ref), sqlc.arg(candidate_generation_id),
    sqlc.arg(mount_name), sqlc.arg(mount_path), sqlc.arg(trust_class),
    sqlc.arg(taint_policy), 'requested', sqlc.arg(requested_at)
)
ON CONFLICT (attempt_id) DO UPDATE SET
    requested_at = workspace_operations.requested_at
RETURNING operation_id, golden_scope_id, job_shape_id, source_generation_id,
    source_snapshot_ref, candidate_generation_id, mount_name, mount_path,
    trust_class, taint_policy, final_state;

-- name: MarkWorkspaceOperationMounted :exec
UPDATE workspace_operations
SET host_accepted_at = COALESCE(host_accepted_at, sqlc.arg(now)),
    mounted_at = COALESCE(mounted_at, sqlc.arg(now)),
    final_state = CASE WHEN final_state = 'requested' THEN 'mounted' ELSE final_state END
WHERE operation_id = sqlc.arg(operation_id);

-- name: MarkWorkspaceOperationFailed :exec
UPDATE workspace_operations
SET final_state = 'failed',
    failure_reason = sqlc.arg(failure_reason),
    result_recorded_at = sqlc.arg(now)
WHERE operation_id = sqlc.arg(operation_id);

-- name: InsertGoldenGeneration :one
INSERT INTO golden_generations (
    golden_generation_id, golden_scope_id, operation_id, source_generation_id,
    head_sha, tree_hash, provider_run_id, provider_job_id, result, taint_class,
    promotion_eligible, state, zfs_snapshot_ref, used_bytes, written_bytes,
    sealed_at, committed_at, last_used_at
) VALUES (
    sqlc.arg(golden_generation_id), sqlc.arg(golden_scope_id),
    sqlc.arg(operation_id), sqlc.narg(source_generation_id),
    sqlc.arg(head_sha), sqlc.arg(tree_hash), sqlc.arg(provider_run_id),
    sqlc.arg(provider_job_id), sqlc.arg(result), sqlc.arg(taint_class),
    sqlc.arg(promotion_eligible), 'committed', sqlc.arg(zfs_snapshot_ref),
    sqlc.arg(used_bytes), sqlc.arg(written_bytes), sqlc.arg(sealed_at),
    sqlc.arg(committed_at), sqlc.arg(committed_at)
)
ON CONFLICT (operation_id) DO UPDATE SET
    last_used_at = EXCLUDED.last_used_at
RETURNING golden_generation_id, golden_scope_id, operation_id,
    source_generation_id, zfs_snapshot_ref, state;

-- name: InsertDurableComponent :exec
INSERT INTO durable_components (
    durable_component_id, golden_generation_id, component_kind, component_key,
    guest_mount_path, host_dataset_ref, sealed_snapshot_ref, filesystem_kind,
    size_bytes_logical, size_bytes_referenced, scrub_policy_hash, created_at
) VALUES (
    sqlc.arg(durable_component_id), sqlc.arg(golden_generation_id),
    sqlc.arg(component_kind), sqlc.arg(component_key), sqlc.arg(guest_mount_path),
    sqlc.arg(host_dataset_ref), sqlc.arg(sealed_snapshot_ref),
    sqlc.arg(filesystem_kind), sqlc.arg(size_bytes_logical),
    sqlc.arg(size_bytes_referenced), sqlc.arg(scrub_policy_hash),
    sqlc.arg(created_at)
)
ON CONFLICT (golden_generation_id, component_kind, component_key) DO NOTHING;

-- name: PromoteGoldenGenerationCAS :execrows
UPDATE golden_current_pointer
SET current_generation_id = sqlc.arg(new_generation_id),
    promoted_by_operation_id = sqlc.arg(operation_id),
    promoted_at = sqlc.arg(promoted_at)
WHERE golden_scope_id = sqlc.arg(golden_scope_id)
  AND current_generation_id IS NOT DISTINCT FROM sqlc.narg(expected_generation_id)::uuid;

-- name: ListGoldenPromotionCandidatesForRun :many
SELECT
    g.golden_generation_id,
    g.golden_scope_id,
    g.operation_id,
    g.source_generation_id
FROM golden_generations g
JOIN golden_scopes s ON s.golden_scope_id = g.golden_scope_id
WHERE g.provider_run_id = sqlc.arg(provider_run_id)
  AND g.head_sha = sqlc.arg(head_sha)
  AND s.org_id = sqlc.arg(org_id)
  AND s.provider = sqlc.arg(provider)
  AND s.provider_repository_id = sqlc.arg(provider_repository_id)
  AND g.result = 'success'
  AND g.state = 'committed'
  AND g.promotion_eligible
ORDER BY g.committed_at, g.golden_generation_id;

-- name: InsertGoldenCurrentPointer :execrows
INSERT INTO golden_current_pointer (
    golden_scope_id, current_generation_id, promoted_by_operation_id, promoted_at
) VALUES (
    sqlc.arg(golden_scope_id), sqlc.arg(current_generation_id),
    sqlc.arg(operation_id), sqlc.arg(promoted_at)
)
ON CONFLICT (golden_scope_id) DO NOTHING;

-- name: MarkWorkspaceOperationResultRecorded :exec
UPDATE workspace_operations
SET final_state = sqlc.arg(final_state),
    sealed_at = sqlc.arg(sealed_at),
    result_recorded_at = sqlc.arg(recorded_at)
WHERE operation_id = sqlc.arg(operation_id);

-- name: InsertGoldenEvent :exec
INSERT INTO golden_events (
    event_id, operation_id, golden_scope_id, golden_generation_id,
    execution_id, attempt_id, event_name, result, reason, observed_at
) VALUES (
    sqlc.arg(event_id), sqlc.narg(operation_id), sqlc.narg(golden_scope_id),
    sqlc.narg(golden_generation_id), sqlc.narg(execution_id),
    sqlc.narg(attempt_id), sqlc.arg(event_name), sqlc.arg(result),
    sqlc.arg(reason), sqlc.arg(observed_at)
);
