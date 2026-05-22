# VM Execution Control Plane

sandbox-rental-service owns customer semantics: organization policy, IAM,
GitHub integration state, recurring canary schedules, billing windows,
execution state, logs, and public API DTOs. vm-orchestrator owns only host
facts: VM leases, execs inside leases, ZFS lifecycle, TAP slots, Firecracker,
vm-bridge control, and guest telemetry.

Code pointers:

- `internal/jobs/` - River workers, execution attempts, runner provider demand
  records, runner allocations, durable cache planning, and reconciliation.
- `internal/api/` - secured Huma routes for GitHub installations, execution
  history/logs, recurring schedules, and billing views.
- `migrations/` - PostgreSQL tables for executions, attempts, billing windows,
  logs, provider-neutral runner demand/allocation state, and schedule dispatch
  lineage.
- `../../vm-orchestrator/proto/v1/` - host lease/exec gRPC API. This is V1 of
  the rewritten orchestrator contract; the old Run API is gone.
- `../../smithy/models/verself/sandbox.smithy` - public and internal service
  contracts.
- `../openapi/` - generated OpenAPI contracts.

State model:

- `executions` are customer-visible workload rows created by a runner provider
  path or by a recurring schedule dispatch.
- `execution_attempts` are durable River/reconciliation units. Attempts store
  host-assigned `lease_id` and `exec_id` only after the host returns them.
- `execution_billing_windows` are control-plane billing records. The host never
  receives billing, org, customer, attempt, or quota vocabulary.
- `runner_provider_repositories` binds provider repository IDs to Verself
  org/source repository ownership.
- `runner_jobs` are provider demand facts from GitHub webhooks.
- `runner_allocations` are Verself capacity records for runner VMs.
- `runner_job_bindings` are the authoritative job-to-runner assignment records.
- Golden artifact state belongs beside executions: cache declarations, stable
  job shape rows, scoped immutable durable generation rows, golden VM manifests,
  current pointers, and durable operations.
- `execution_schedules` and `execution_schedule_dispatches` are Temporal-backed
  recurring canary state.

Runner flow:

1. Provider events record or refresh `runner_jobs` demand. GitHub uses the
   `workflow_job` webhook.
2. Allocation logic creates a `runner_allocations` row, obtains the provider
   bootstrap material, and internally submits an execution attempt for the
   selected runner class.
3. The execution worker reserves billing, acquires a vm-orchestrator lease,
   starts the workload payload, streams logs, and settles billing.

Durable volume flow:

1. Provider demand persists the GitHub job identity before any host mutation:
   organization, provider, repository, workflow, job name, runner labels,
   matrix key, run ID, run attempt, head SHA, and branch.
2. sandbox-rental derives a stable job shape and durable scope from persisted
   provider state. The guest never supplies tenant, repository, branch, or
   trust identity.
3. sandbox-rental resolves the current readable generation for the scope and
   inserts a `durable_operation` row with the observed source generation.
   No database row claims a new generation exists before ZFS has sealed it.
4. sandbox-rental computes the exact durable generation set and looks up a
   compatible golden VM manifest for that set, job shape, trust class, and
   Firecracker ABI.
5. The lease request includes a static filesystem mount plan and, on hit, the
   expected golden VM manifest. The workspace mount is required; cache mounts
   are optional. A durable hit clones the current generation; a miss creates an
   empty ext4 zvol. A golden VM hit restores Firecracker after the disk graph is
   prepared; a miss cold boots from the same durable sources.
6. The runner work directory is the normal GitHub Actions `_work` tree under
   `GITHUB_WORKSPACE` semantics, so customer YAML continues to use ordinary
   checkout and build steps.
7. After the runner exits successfully in a promotable context, sandbox-rental
   asks vm-orchestrator to checkpoint the running VM before filesystem seal.
   The checkpoint returns Firecracker artifact refs and zvol checkpoint refs for
   the root, workspace, and declared durable mounts.
8. sandbox-rental asks vm-orchestrator to seal each mounted writable volume.
   vm-orchestrator unmounts guest bind mounts, flushes block devices, clones
   the checkpointed or freshly snapshotted zvol into the durable generation
   namespace, and returns only service-level results.
9. sandbox-rental records immutable durable generations and the golden VM
   manifest, then promotes current pointers by compare-and-swap against the
   source generation set observed before host mutation. A lost CAS leaves the
   candidate retained and reapable, not failed.
10. A GitHub workflow run promotes branch goldens only after the run's job set
   is observed complete and every required job is successful or skipped. Failed
   or canceled runs leave current pointers at the last green artifact.

Recurring schedule flow:

1. `POST /api/v1/execution-schedules` persists the org-owned schedule config
   and creates or updates the Temporal schedule.
2. Each fire records an `execution_schedule_dispatches` row and internally
   submits an execution through the same worker pipeline used by GitHub jobs.
3. Execution history stays in `executions`, `execution_attempts`, and
   `execution_logs`; the schedule record tracks dispatch lineage and
   pause/resume state.

Reconciliation:

- Reconciliation repairs stale reserved or launching attempts by voiding
  unsettled windows, releasing any recorded lease ID, and terminalizing the
  attempt.
- Runner reconciliation reclaims stale allocations, expired bootstrap configs,
  and orphaned job bindings without granting product services direct host
  privilege.

Single-node VM concurrency budget:

- `SANDBOX_EXECUTION_MAX_WORKERS=4` is the current default for the single-node
  bare-metal profile. Treat that worker count as the VM admission limit until
  admission control distinguishes runner classes and requested memory.
- Do not raise the global default without class-specific admission, smoke runs,
  and tail-latency evidence. The long-term design should scale by adding more
  bare-metal VM hosts, not by overcommitting one node blindly.

Expected evidence surface:

- PostgreSQL shows each attempt moving through
  `queued -> reserved -> launching -> running -> finalizing -> succeeded`.
- ClickHouse `verself.job_events` has a terminal row per execution.
- ClickHouse `verself.vm_lease_evidence` has `lease_ready`,
  `exec_started`, and `lease_cleanup` rows for each host lease.
- ClickHouse durable/golden events show durable hit/miss, golden VM hit/miss,
  checkpoint result, manifest publish, and pointer promotion for the same run.
- OTel traces include sandbox-rental worker spans plus vm-orchestrator
  lease/exec spans for the same execution.
- [`VM Acquisition KPIs`](../../../../docs/architecture/vm-acquisition-kpis.md)
  defines the end-to-end runner acquisition timestamp contract, cache hit
  dimensions, and boot substep breakdown.
