# VM Acquisition KPIs

The acquisition lifecycle starts when GitHub accepts work that can produce a
runner job and ends when the customer payload is running inside a lease. The
post-workload seal path is tracked in the same document because cache quality
for the next acquisition depends on it.

Every timestamp is stored in UTC. Spans carry monotonic duration within one
process; cross-system deltas use absolute timestamps and must include the
source clock in the field name. Derived KPI tables are projections over
PostgreSQL state, ClickHouse OTel rows, and ClickHouse evidence tables.

## Correlation Keys

Every row that participates in this lifecycle should carry as many of these
keys as are available at that point:

| Key | Source | Purpose |
| --- | --- | --- |
| `github_delivery_id` | GitHub webhook header | Provider ingress correlation. |
| `provider_run_id` | GitHub workflow run | End-to-end run grouping. |
| `provider_run_attempt` | GitHub workflow run attempt | Retry isolation. |
| `provider_job_id` | GitHub workflow job | Unit of VM acquisition demand. |
| `repository_full_name` | GitHub webhook/API | Human RCA lookup. |
| `provider_repository_id` | GitHub repository ID | Stable provider repository key. |
| `org_id` | `runner_provider_repositories` | Tenant boundary after demand ingestion. |
| `job_shape_id` | `job_shape` | Matrix, runner class, toolchain, and cache shape. |
| `matrix_key` | `job_shape` | Matrix split dimension. |
| `runner_class` | `job_shape` and `runner_allocations` | Capacity and image selection. |
| `allocation_id` | `runner_allocations` | Provider runner capacity record. |
| `execution_id` | `executions` | Product execution record. |
| `attempt_id` | `execution_attempts` | Retry and billing unit. |
| `lease_id` | vm-orchestrator | Host VM lifecycle unit. |
| `exec_id` | vm-orchestrator | Guest process lifecycle unit. |
| `trace_id` and `span_id` | OTel | Span/log/evidence joins. |

## Lifecycle Points

Every point records `started_at`, `completed_at`, `duration_ms`, `result`,
`reason`, and the correlation keys available at that boundary. Points that
already have durable state should use that state as the canonical timestamp;
points that only exist as spans should remain queryable from
`default.otel_traces` until promoted into a first-class evidence table.

| Point | Canonical Evidence | Primary KPI | Required Attributes |
| --- | --- | --- | --- |
| GitHub accepts push/work | GitHub `workflow_run.created_at`, `workflow_job.created_at`, or future `push` webhook row | `github_accept_to_webhook_ms` | provider, repository, head SHA, run ID, job ID. |
| Webhook reaches Verself edge | `default.http_access_logs` for `/api/v1/github/webhooks` | `edge_receive_to_handler_ms` | delivery ID, event name, path, status. |
| Webhook handler starts | `github.webhook.receive` span and `verself.github_integration_events` | `webhook_handler_ms` | delivery ID, action, status, run ID, job ID. |
| Demand persisted | `runner_jobs`, `verself.github_integration_events` | `webhook_to_demand_persist_ms` | delivery ID, job status, labels, runner identity fields. |
| Allocation row inserted | `runner_allocations.created_at`, `github.runner.allocate` | `demand_to_allocation_ms` | allocation ID, deadlines, runner class, repository ownership decision. |
| GitHub runner capacity requested | `github.runner.allocate`, `github.api.request` | `allocation_to_jit_ms` | API endpoint, status, runner group, provider runner ID. |
| JIT config persisted | `runner_bootstrap_configs.created_at` | `jit_to_bootstrap_persist_ms` | allocation ID, bootstrap kind, expiry. |
| Execution submitted | `executions`, `execution_attempts`, `sandbox-rental.execution.submit` | `allocation_to_execution_submit_ms` | execution ID, attempt ID, external task ID. |
| Execution worker starts | `river.work/execution.advance`, `sandbox-rental.execution.run` | `execution_queue_ms` | execution ID, attempt ID, attempt state. |
| Billing reserved | billing client span and `execution_billing_windows` | `billing_reserve_ms` | billing job ID, window ID, result. |
| Durable declarations resolved | `durable.declaration.resolve` span/event | `durable_declaration_resolve_ms` | job shape, cache spec hash, cache names. |
| Durable source selected | `verself.durable_events`, `durable.cache.select` | `zfs_generation_hit_rate` | cache name, result `hit` or `miss`, source generation, snapshot ref. |
| Durable mount plan persisted | `durable_operation.requested_at`, `execution_filesystem_mounts` | `durable_prepare_ms` | operation ID, scope ID, mount name, required flag. |
| Org runtime ensured | `sandbox.org_runtime.ensure`, `rpc.EnsureOrgRuntime`, `vmorchestrator.org_runtime.ensure` | `org_runtime_ensure_ms` | org ID, quota bytes, image refs, namespace dataset, image cache hit/miss. |
| Lease accepted | `rpc.AcquireLease`, client span, `execution_attempts.lease_id` | `lease_accept_ms` | lease ID, attempt ID, mount count, resource shape. |
| Lease ready | `verself.vm_lease_evidence` `lease_ready`, `GetLease` polling spans | `lease_accept_to_ready_ms` | lease ID, activation mode, filesystem result count, golden VM snapshot ID on hit. |
| Exec started | `rpc.StartExec`, `verself.vm_lease_evidence` `exec_started` | `ready_to_exec_started_ms` | lease ID, exec ID, command class. |
| Runner bootstrap fetched | `runner.bootstrap.consume`, `runner_bootstrap_configs.consumed_at` | `exec_started_to_bootstrap_fetch_ms` | allocation ID, execution ID, attempt ID. |
| GitHub assigns job | `runner_job_bindings`, `github.job.bind` | `bootstrap_to_assignment_ms` | provider runner ID/name, job ID, allocation ID. |
| First workflow step starts | GitHub job steps API | `assignment_to_first_step_ms` | step name, job ID, runner name. |
| Verself checkout completes | checkout action annotation, `github.checkout.*` spans | `checkout_ready_ms` | bundle cache hit, bytes, git duration. |

## Async Lease Boot Breakdown

`vmorchestrator.lease.boot` is a rollup. It must never be the only evidence
for boot overhead. Every substep below should appear as a span or evidence row
with `lease_id`, result, and duration. Snapshot hit paths and cold-boot paths
share the same parent.

| Substep | Current / Required Span | KPI |
| --- | --- | --- |
| Org runtime require-ready | `vmorchestrator.org_runtime.require_ready_check`, `vmorchestrator.org_runtime.require_ready` | `org_runtime_require_ready_ms` |
| Storage namespace assertion | `vmorchestrator.zfs.namespace.assert_ready` | `zfs_namespace_assert_ms` |
| Root substrate clone | `vmorchestrator.zfs.root_clone`, `vmorchestrator.zfs.root.prepare_substrate_clone_from_snapshot` | `root_clone_ms` |
| Root resize | required span around `ResizeLeaseRootExt4` | `root_resize_ms` |
| Mount source select | `durable.cache.select`, host mount attributes | `mount_source_hit_rate` |
| Mount clone/create | required span around `PrepareMountFromSnapshot`, `PrepareMount`, `PrepareEmptyMount` | `mount_prepare_ms` |
| Root zvol device appears | `vmorchestrator.zvol.wait_device` | `root_device_wait_ms` |
| Mount zvol device appears | `vmorchestrator.zvol.mount_wait_device` | `mount_device_wait_ms` |
| Jail setup | `vmorchestrator.jail.setup` | `jail_setup_ms` |
| TAP/network acquire | `vmorchestrator.network.acquire`, `vmorchestrator.network.tap_create`, `vmorchestrator.network.tap_up`, `vmorchestrator.network.setup` | `network_setup_ms` |
| Jailer process starts | `vmorchestrator.jailer.start` | `jailer_start_ms` |
| Firecracker API socket ready | `vmorchestrator.firecracker.api_socket_wait` | `fc_api_socket_wait_ms` |
| Golden VM key built | required span around golden VM compatibility key construction | `golden_vm_key_build_ms` |
| Golden VM manifest lookup | required span around golden VM manifest lookup | `golden_vm_lookup_ms`, `golden_vm_hit_rate` |
| Golden VM artifact staged | `vmorchestrator.firecracker.snapshot_stage` | `golden_vm_stage_ms` |
| Golden VM snapshot loaded | `vmorchestrator.firecracker.snapshot_load` | `golden_vm_load_ms` |
| Golden VM snapshot resumed | `vmorchestrator.firecracker.snapshot_resume` | `golden_vm_resume_ms` |
| Guest after-restore | `vmorchestrator.guest.after_restore` | `guest_after_restore_ms` |
| Cold Firecracker configure | `vmorchestrator.firecracker.configure_all` plus per-step `vmorchestrator.firecracker.configure` | `fc_configure_ms` |
| Cold Firecracker start | `vmorchestrator.firecracker.instance_start` | `fc_instance_start_ms` |
| Guest control socket appears | `vmorchestrator.guest.control_socket_wait` | `guest_control_socket_wait_ms` |
| Guest control connect | `vmorchestrator.guest.control_connect`, `vmorchestrator.guest.vsock_proxy_*` | `guest_control_connect_ms` |
| Guest hello | `vmorchestrator.guest.hello`, `verself.vm_lease_evidence` `telemetry_hello` | `guest_hello_ms` |
| Lease init | `vmorchestrator.guest.lease_init` | `lease_init_ms` |
| Guest network apply | `guest.lease_init.apply_network_ms` on `vmorchestrator.guest.lease_init` | `guest_network_apply_ms` |
| Guest filesystem mount | `guest.lease_init.mount_filesystems_ms` on `vmorchestrator.guest.lease_init` | `guest_mount_ms` |
| Guest time sync | `guest.lease_init.set_wall_clock_ms` on `vmorchestrator.guest.lease_init` | `guest_time_sync_ms` |
| Guest local control start | `guest.lease_init.start_local_control_ms` on `vmorchestrator.guest.lease_init` | `guest_local_control_start_ms` |

The warm target for `lease_accept_to_ready_ms` is as close to zero as the
golden VM restore path permits. Cold boot remains tracked as a separate
fallback path and should not justify increasing `lease_accept_ms` or provider
allocation deadlines.

## Latency Budgets

Budgets are performance assertions. Timeout changes require evidence that the
correct boundary is too narrow after the slow substep has been identified.

| KPI | Budget | Interpretation |
| --- | --- | --- |
| `lease_accept_ms` | p99 <= 250 ms | Host accepted durable lease state. No ZFS clone, Firecracker startup, guest readiness, or provider polling belongs here. |
| `lease_accept_to_ready_ms` on golden VM hit | p50 <= 250 ms, p99 <= 1 s | Restore and after-restore hooks should trend toward zero. Seconds-grade values require substep attribution. |
| `lease_accept_to_ready_ms` on cold boot | tracked separately | Cold boot is fallback evidence, not a reason to widen lease acceptance. |
| `ready_to_exec_started_ms` | p99 <= 250 ms | Guest control is ready; dispatch should be protocol overhead only. |
| `bootstrap_to_assignment_ms` | p99 <= provider assignment deadline | Slow values usually point to runner registration, GitHub assignment, or webhook delivery. |

## Cache Hit Dimensions

Cache reporting must distinguish cache identity from cache result.

| Cache | Hit Evidence | Miss Evidence | Identity Dimensions |
| --- | --- | --- | --- |
| Durable ZFS generation | `verself.durable_events` `durable.cache.select` with `result='hit'` and `source_generation_id` | `durable.cache.select` with `result='miss'` | org, provider, repository, scope kind/ref, job shape, cache name, trust class. |
| Workspace checkout bundle | `github.checkout.bundle_cache_hit=true` | `github.checkout.bundle_cache_hit=false` | repository, base/head SHA, bundle key. |
| Firecracker golden VM snapshot | `firecracker.activation_mode='snapshot_restore'`, golden VM snapshot ID, and snapshot key | `golden.vm.lookup` with `result='miss'` or `firecracker.snapshot_cache_miss=true` | org, provider, repository, scope kind/ref, job shape, trust class, exact durable generation set, Firecracker runtime ABI, hook profile. |
| Platform image materialization | required image materialization event | required miss/copy event | org, image digest, image tier, substrate/toolchain ref. |
| ZFS device wait | device wait span duration | wait timeout/error | dataset, device path, mount name. |

## Post-Workload Cache Path

The next acquisition depends on terminal-state evidence after the workload
exits. These points are tracked even though they are outside the pre-exec
acquisition path.

| Point | Canonical Evidence | Primary KPI |
| --- | --- | --- |
| Exec result received | `rpc.WaitExec`, `vmorchestrator.guest.exec_*` spans | `exec_result_latency_ms` |
| Golden VM before-snapshot hook | `golden.vm.before_snapshot`, `vmorchestrator.guest.before_golden_snapshot` | `golden_vm_before_snapshot_ms` |
| Golden VM checkpoint created | `golden.vm.checkpoint`, `vmorchestrator.firecracker.snapshot_create` | `golden_vm_checkpoint_ms` |
| Golden VM manifest published | `golden.vm.publish` | `golden_vm_publish_ms` |
| Durable seal starts | `durable.cache.seal`, `durable_operation.seal_started_at` | `seal_queue_ms` |
| Guest filesystem sealed | `vmorchestrator.guest.filesystem_seal` | `guest_filesystem_seal_ms` |
| Block flushed | `vmorchestrator.block.flush` | `block_flush_ms` |
| ZFS generation committed | `durable.cache.commit`, `durable_operation.result_recorded_at` | `zfs_commit_ms` |
| Golden promotion evaluated | `durable.workflow_run.promote`, `golden.run.promote` | `promotion_decision_ms` |
| Current pointer promoted | `durable.cache.promote`, `golden.vm.promote`, `durable_current_pointer.promoted_at`, `golden_vm_current_pointer.promoted_at` | `promotion_commit_ms` |
| VM cleanup complete | `verself.vm_lease_evidence` `lease_cleanup`, `runner.cleanup` | `cleanup_ms` |

## Dashboard Cuts

Dashboards should expose p50, p90, p99, max, error count, and hit rate for:

- provider, repository, workflow, job, matrix key, and runner class;
- host, Firecracker version, kernel image, substrate image, and toolchain image;
- activation mode: `snapshot_restore`, `cold_boot`, or disabled snapshots;
- durable cache name and trust class;
- cache hit/miss result for ZFS generations, checkout bundles, and golden VM
  snapshots;
- allocation result and failure reason;
- lease failure reason and vm-orchestrator status message.

## Current Instrumentation Gaps

1. Exact GitHub push acceptance is not durable in Verself. `workflow_run` and
   `workflow_job` timestamps are usable provider proxies. Exact push ingress
   requires accepting and persisting the GitHub `push` or `workflow_run` event
   with provider timestamp and Verself receive timestamp.
2. Golden VM snapshot key construction and manifest lookup are attributes
   today, not timed substeps. They need spans with hit/miss, artifact size, and
   generation-set hash.
3. Runner process readiness is inferred from bootstrap fetch and GitHub
   assignment. The guest should emit runner process start, registration attempt,
   and registration success/failure as product-visible evidence.
4. Cross-system deltas depend on GitHub, host, and guest clocks. Dashboards
   should show the source of each timestamp and keep host monotonic durations
   separate from provider-to-host wall-clock deltas.
5. The current ClickHouse evidence is split across OTel traces, OTel logs,
   `verself.vm_lease_evidence`, `verself.durable_events`, and GitHub API
   polling. A dedicated `verself.vm_acquisition_events` projection should
   materialize the lifecycle points above once the event names stabilize.

## Query Anchors

Recent trace spans for one provider job:

```sql
SELECT
  Timestamp,
  ServiceName,
  SpanName,
  round(Duration / 1000000, 1) AS duration_ms,
  StatusCode,
  StatusMessage,
  SpanAttributes['lease.id'] AS lease_id,
  SpanAttributes['firecracker.activation_mode'] AS activation_mode,
  SpanAttributes['firecracker.snapshot_cache_miss'] AS snapshot_cache_miss,
  SpanAttributes['golden_vm.snapshot_id'] AS golden_vm_snapshot_id,
  SpanAttributes['golden_vm.generation_set_hash'] AS generation_set_hash
FROM default.otel_traces
WHERE Timestamp >= now() - INTERVAL 1 HOUR
  AND ServiceName IN ('sandbox-rental-service', 'vm-orchestrator')
  AND (
    SpanName LIKE '%AcquireLease%'
    OR SpanName = 'vmorchestrator.lease.boot'
    OR SpanName LIKE 'vmorchestrator.%'
    OR SpanName LIKE 'github.%'
    OR SpanName LIKE 'runner.%'
    OR SpanName LIKE 'durable.%'
  )
ORDER BY Timestamp;
```

Durable cache hit/miss and commit evidence:

```sql
SELECT
  observed_at,
  provider_run_id,
  provider_job_id,
  execution_id,
  attempt_id,
  cache_name,
  event_name,
  result,
  reason,
  source_generation_id,
  candidate_generation_id,
  zfs_snapshot_ref,
  used_bytes,
  written_bytes
FROM verself.durable_events
WHERE provider_run_id = {provider_run_id:UInt64}
ORDER BY observed_at;
```

Golden VM hit/miss and checkpoint evidence:

```sql
SELECT
  observed_at,
  provider_run_id,
  provider_job_id,
  execution_id,
  attempt_id,
  event_name,
  result,
  reason,
  golden_vm_snapshot_id,
  generation_set_hash,
  snapshot_key,
  activation_mode,
  state_bytes,
  memory_bytes
FROM verself.golden_vm_events
WHERE provider_run_id = {provider_run_id:UInt64}
ORDER BY observed_at;
```

VM evidence for one lease:

```sql
SELECT
  evidence_time,
  lease_id,
  exec_id,
  evidence_type,
  diagnostic_kind,
  reason_code,
  trace_id,
  span_id
FROM verself.vm_lease_evidence
WHERE lease_id = {lease_id:String}
ORDER BY evidence_time;
```

Provider and product state for one GitHub run:

```sql
SELECT
  r.provider_run_id,
  r.provider_job_id,
  r.status,
  r.conclusion,
  r.runner_id,
  r.runner_name,
  b.allocation_id,
  e.execution_id,
  e.state AS execution_state,
  a.attempt_id,
  a.state AS attempt_state,
  a.lease_id,
  a.exec_id,
  a.failure_reason
FROM runner_jobs r
LEFT JOIN runner_job_bindings b ON b.provider_job_id = r.provider_job_id
LEFT JOIN executions e ON e.external_task_id = r.provider_job_id::text
LEFT JOIN execution_attempts a ON a.execution_id = e.execution_id
WHERE r.provider_run_id = $1
ORDER BY r.provider_job_id, a.updated_at DESC;
```
