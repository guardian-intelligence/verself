# VM Execution Control Plane

sandbox-rental-service owns customer semantics: organization policy, IAM,
GitHub integration state, recurring canary schedules, billing windows,
execution state, logs, and public API DTOs. vm-orchestrator owns only host
facts: VM leases, execs inside leases, ZFS lifecycle, TAP slots, Firecracker,
vm-bridge control, and guest telemetry.

Code pointers:

- `internal/jobs/` - River workers, execution attempts, GitHub demand records,
  runner allocations, sticky-disk saveback state, and reconciliation.
- `internal/api/` - secured Huma routes for GitHub installations, execution
  history/logs, recurring schedules, and billing views.
- `migrations/` - PostgreSQL tables for executions, attempts, billing windows,
  logs, GitHub workflow jobs, runner allocations, sticky disks, and schedule
  dispatch lineage.
- `../../vm-orchestrator/proto/v1/` - host lease/exec gRPC API. This is V1 of
  the rewritten orchestrator contract; the old Run API is gone.
- `../../apiwire/sandbox.go` - shared wire DTOs.
- `../openapi/` - generated OpenAPI contracts.

State model:

- `executions` are customer-visible workload rows created by the GitHub runner
  path or by a recurring schedule dispatch.
- `execution_attempts` are durable River/reconciliation units. Attempts store
  host-assigned `lease_id` and `exec_id` only after the host returns them.
- `execution_billing_windows` are control-plane billing records. The host never
  receives billing, org, customer, attempt, or quota vocabulary.
- `github_workflow_jobs` are GitHub demand facts from webhooks and polling.
- `github_runner_allocations` are Forge Metal capacity records for runner VMs.
- `github_runner_job_bindings` are the authoritative job-to-runner assignment
  records.
- `github_sticky_disk_generations` and `execution_sticky_disk_mounts` track the
  Blacksmith-style sticky-disk restore/saveback lifecycle.
- `execution_schedules` and `execution_schedule_dispatches` are Temporal-backed
  recurring canary state.

GitHub runner flow:

1. GitHub webhooks record or refresh `github_workflow_jobs` demand.
2. Allocation logic creates a `github_runner_allocations` row, obtains the JIT
   runner config, and internally submits an execution attempt for the selected
   runner class.
3. The execution worker reserves billing, acquires a vm-orchestrator lease,
   starts the workload payload, streams logs, and settles billing.
4. Sticky-disk mounts are restored before the run and committed back to
   `github_sticky_disk_generations` when saveback succeeds.

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
- GitHub runner reconciliation reclaims stale allocations, expired JIT configs,
  and orphaned job bindings without granting product services direct host
  privilege.

Single-node VM concurrency budget:

- `SANDBOX_EXECUTION_MAX_WORKERS=4` is the current default for the single-node
  bare-metal profile. Treat that worker count as the VM admission limit until
  admission control distinguishes runner classes and requested memory.
- Do not raise the global default without class-specific admission, proof runs,
  and tail-latency evidence. The long-term design should scale by adding more
  bare-metal VM hosts, not by overcommitting one node blindly.

Expected proof surface:

- PostgreSQL shows each attempt moving through
  `queued -> reserved -> launching -> running -> finalizing -> succeeded`.
- ClickHouse `forge_metal.job_events` has a terminal row per execution.
- ClickHouse `forge_metal.vm_lease_evidence` has `lease_ready`,
  `exec_started`, and `lease_cleanup` rows for each host lease.
- OTel traces include sandbox-rental worker spans plus vm-orchestrator
  lease/exec spans for the same execution.
