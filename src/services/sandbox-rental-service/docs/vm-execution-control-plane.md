# VM Execution Control Plane

sandbox-rental-service owns customer semantics: organization policy, IAM,
GitHub integration state, recurring canary schedules, billing windows,
execution state, logs, and public API DTOs. vm-orchestrator owns only host
facts: VM leases, execs inside leases, ZFS lifecycle, TAP slots, Firecracker,
vm-bridge control, and guest telemetry.

Code pointers:

- `internal/jobs/` - River workers, execution attempts, runner provider demand
  records, runner allocations, checkout cache state, and reconciliation.
- `internal/api/` - secured Huma routes for GitHub installations, execution
  history/logs, recurring schedules, and billing views.
- `migrations/` - PostgreSQL tables for executions, attempts, billing windows,
  logs, provider-neutral runner demand/allocation state, and schedule dispatch
  lineage.
- `checkpoint-event-contract.md` - Checkpoint lifecycle event names, state
  vocabulary, and future FSM transition contract.
- `../../vm-orchestrator/proto/v1/` - host lease/exec gRPC API. This is V1 of
  the rewritten orchestrator contract; the old Run API is gone.
- `../../dto/sandbox.go` - shared wire DTOs.
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
- `runner_jobs` are provider demand facts from GitHub webhooks or Forgejo action
  job sync.
- `runner_allocations` are Verself capacity records for runner VMs.
- `runner_job_bindings` are the authoritative job-to-runner assignment records.
- Checkpoint control-plane state belongs beside executions: stable volume rows,
  immutable generation rows, trust/scope current pointers, and per-attempt
  mount/saveback rows.
- `execution_schedules` and `execution_schedule_dispatches` are Temporal-backed
  recurring canary state.

Runner flow:

1. Provider events record or refresh `runner_jobs` demand. GitHub uses the
   `workflow_job` webhook. Forgejo registers a per-repository webhook and syncs
   queued jobs from the Forgejo v15 Actions runner jobs API.
2. Allocation logic creates a `runner_allocations` row, obtains the provider
   bootstrap material, and internally submits an execution attempt for the
   selected runner class.
3. The execution worker reserves billing, acquires a vm-orchestrator lease,
   starts the workload payload, streams logs, and settles billing.

Checkpoint flow:

1. The runner boots with a bounded set of reserved Checkpoint drive slots.
   `guardian-intelligence/verself-checkpoint@v0` runs as a normal action step after the GitHub runner
   has evaluated workflow expressions, then sends concrete `key` and `path`
   values to sandbox-rental through the guest-visible internal API.
2. sandbox-rental authenticates the attempt-scoped token and derives
   organization, repository, provider installation, run, job, branch, PR, and
   trust context from persisted runner state. The guest never supplies tenant
   or repository identity.
3. sandbox-rental records lifecycle events using the contract in
   `checkpoint-event-contract.md`; the same transition vocabulary is the input
   to the future finite state machine.
4. sandbox-rental resolves a scoped volume for `(org, repository, scope,
   component_kind, key_hash)`, chooses the readable current generation, and
   persists an attempt-scoped mount row. First use creates an empty mount plan
   and records a miss.
5. sandbox-rental asks vm-orchestrator to create or clone the writable ext4
   zvol, bind it to a reserved runner drive slot, trigger guest discovery, and
   mount it at the requested path through vm-bridge. vm-orchestrator returns
   service results only; host paths, dataset names, and device paths stay
   behind the privileged boundary.
6. The action post step marks requested mounts for saveback with
   attempt-scoped credentials. During finalization, sandbox-rental asks
   vm-orchestrator to snapshot requested writable mounts, records immutable
   generations, and promotes current pointers only by observed-state
   compare-and-swap.
7. Protected/default branch runs may promote protected current pointers.
   Same-repository branch/PR runs may promote branch pointers. Fork PR runs
   write only to PR-scoped short-retention namespaces.

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
- OTel traces include sandbox-rental worker spans plus vm-orchestrator
  lease/exec spans for the same execution.
