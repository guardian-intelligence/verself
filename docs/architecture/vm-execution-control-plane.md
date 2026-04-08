# VM Execution Control Plane

This document defines the target runtime boundary between `sandbox-rental-service`
and `vm-orchestrator` for all Firecracker-backed execution in forge-metal.

It covers four workload kinds:

1. direct sandbox commands
2. repo-backed execution (`repo_exec`)
3. default-branch golden warming (`warm_golden`)
4. Forgejo Actions jobs executed by an official `forgejo-runner` living inside a VM

The central design rule is:

- `sandbox-rental-service` is the policy, identity, and billing boundary.
- `vm-orchestrator` is the privileged execution substrate.

`vm-orchestrator` must not know about prices, grants, quotas, org policy, promotions,
or provider-specific business rules. `sandbox-rental-service` must not own ZFS,
Firecracker, TAP setup, jailer lifecycle, or any other privileged host concern.

This document is a working architecture draft as of 2026-04-08.

## Why This Exists

The current repo has the right low-level substrate but the wrong control-plane split:

- `vm-orchestrator` already exposes direct jobs, repo execution, golden warming,
  status polling, cancellation, log streaming, telemetry streaming, and capacity
  inspection.
- `sandbox-rental-service` currently models only `repo_url + run_command`, launches
  work in a background goroutine, and persists only a narrow `jobs` row.
- The existing Forgejo integration still registers a host runner on
  `ubuntu-latest:host` and shells out to the deprecated CLI CI path.

The goal is to invert that relationship:

- all policy decisions move up into `sandbox-rental-service`
- all privileged VM execution stays down in `vm-orchestrator`
- Forgejo remains the workflow engine for Actions
- the official runner, not a repo webhook, is the execution seam for real Actions jobs

## Design Goals

- One service boundary for entitlement, admission control, and durable execution identity.
- One privileged daemon for Firecracker, ZFS, TAP networking, jailer, and guest telemetry.
- Real Forgejo Actions semantics by running the official `forgejo-runner` inside VMs.
- A unified execution model for sandbox jobs, repo jobs, golden warming, and VM-backed CI.
- Crash recovery and reconciliation that do not depend on in-memory goroutines.
- Billing that is enforced by `sandbox-rental-service`, not leaked into `vm-orchestrator`.

## Non-Goals

- Putting billing logic into `vm-orchestrator`.
- Treating repo webhooks as the primary transport for real Actions jobs.
- Pretending the current Alpine guest is `ubuntu-latest`.
- Solving broad base-image compatibility before the runner control plane exists.
- Solving multi-window billing before the initial 5-minute cap is enforced in code.

## Proven Facts

These points are grounded in the current codebase and a live tracer bullet run
completed on 2026-04-08.

### 1. `vm-orchestrator` already has the right execution primitives

The client and server already support:

- direct VM jobs
- repo execution against an active repo golden
- default-branch golden warming
- async job creation plus status polling
- cancellation
- log streaming
- telemetry streaming
- capacity snapshots

This is the correct substrate boundary. The missing piece is the service control plane
above it, not a new privileged daemon.

### 2. `sandbox-rental-service` is still too small

Today it:

- reserves billing up front
- inserts a narrow `jobs` row
- starts a detached goroutine
- calls `Orchestrator.Run`
- settles or voids billing afterward

That is sufficient for a short direct command, but not for:

- repo-backed execution with durable state
- golden warming
- reconciliation after restart
- provider callbacks
- idempotency
- long-lived or provider-owned execution flows

### 3. The official Forgejo runner works inside a Firecracker VM

The tracer bullet playbook at
`src/platform/ansible/playbooks/forgejo-runner-vm-tracerbullet.yml` proved:

- an official `forgejo-runner` binary can be injected into the guest rootfs
- the guest can reach `https://git.anveio.com`
- the runner can register against the live Forgejo instance
- the runner daemon can stay healthy inside the VM
- teardown can delete the temporary runner row and destroy the tracer zvol cleanly

Important operational learnings from the tracer bullet:

- the runner writes `.runner` relative to its current working directory, so the guest
  process must run from `/home/runner` or another writable runner home
- the current deployed Forgejo is `14.0.3`
- the current deployed runner is `12.8.0`
- the runner CLI exposes `one-job`, `register --ephemeral`, and `create-runner-file`
- Forgejo `14.0.3` rejected ephemeral registration; the tracer bullet had to register
  a normal runner and delete its row during teardown
- `one-job --help` explicitly marks `--handle` as `Forgejo >= 15`

These points materially shape the first control-plane design.

### 4. A simple webhook is not the Actions execution seam

A webhook can tell us that a repo event happened. It cannot replace the real runner path
for:

- reruns
- `workflow_dispatch`
- scheduled workflows
- cancellation
- provider-issued job tokens
- native log and status transport
- runner label matching

Webhooks are still useful, but only for:

- default-branch golden warming
- idempotent provider-side side effects
- optional autoscaling hints

They are not the primary transport for real Actions execution.

## Runtime Module Boundaries

| Module | Owns | Must not own |
|--------|------|---------------|
| `vm-orchestrator` | Firecracker lifecycle, ZFS clone/snapshot/destroy, TAP networking, jailer setup, guest telemetry aggregation, repo golden state, direct/repo/warm execution primitives, capacity reporting | billing, org policy, auth, quotas, plan logic, promotions, provider API semantics |
| `sandbox-rental-service` | authz, entitlement checks, billing reserve/settle later renew, durable execution identity, idempotency, provider correlation, control-plane state machine, reconciliation, unified API, ClickHouse summary dual-write | privileged VM lifecycle internals, direct ZFS operations, Firecracker microVM orchestration, workflow step execution semantics |
| `billing-service` | product catalog, grants, quotas, spend caps, reserve/renew/settle/void, financial reconciliation | VM execution, provider orchestration, repo state |
| Forgejo | workflow graph, secret injection, scheduler, native Actions logs/status, runner matching, reruns, `workflow_dispatch`, cron | VM lifecycle, billing, host quota enforcement outside runner labels |

A useful way to state the split is:

- `vm-orchestrator` answers "can I boot and run this VM workload safely?"
- `sandbox-rental-service` answers "should this org be allowed to boot this VM workload, under which identity, and how should it be billed and reconciled?"

## Durable Identity Model

The service must attach a durable identity to every VM-backed execution before it talks
to `vm-orchestrator`.

The current `jobs.id` row is not enough. We need two layers.

### Execution

An `execution` is the stable, user-visible or provider-visible unit of work.

Examples:

- one direct sandbox launch from the website
- one repo-backed execution request
- one default-branch warm request
- one VM-backed Forgejo job request or runner lease

Suggested fields:

| Field | Purpose |
|-------|---------|
| `execution_id UUID` | stable primary key owned by `sandbox-rental-service` |
| `org_id BIGINT` | billing and quota principal |
| `actor_id TEXT` | human or system actor that caused the execution |
| `product_id TEXT` | initially `sandbox` |
| `kind TEXT` | `direct`, `repo_exec`, `warm_golden`, `forgejo_runner` |
| `provider TEXT` | `''`, `forgejo`, later others |
| `idempotency_key TEXT` | dedupe for provider-driven or retried requests |
| `repo TEXT` / `repo_url TEXT` | repo identity when relevant |
| `ref TEXT` / `commit_sha TEXT` | pinned revision where known |
| `workflow_path TEXT` / `workflow_job_name TEXT` | provider metadata for Actions jobs |
| `provider_run_id TEXT` / `provider_job_id TEXT` | external correlation where available |
| `status TEXT` | control-plane state, not just VM exit status |
| `latest_attempt_id UUID` | current or last attempt |
| `created_at`, `updated_at` | auditability |

### Attempt

An `attempt` is one concrete VM launch.

One attempt maps to exactly one `vm-orchestrator` job identity and exactly one
VM lifecycle. Retries create new attempts under the same execution.

Suggested fields:

| Field | Purpose |
|-------|---------|
| `attempt_id UUID` | per-launch identity |
| `execution_id UUID` | parent execution |
| `attempt_seq INT` | retry order |
| `orchestrator_job_id TEXT` | must be set by the service, not invented later |
| `billing_job_id BIGINT` | billing correlation |
| `state TEXT` | `queued`, `reserved`, `launching`, `running`, `settling`, `succeeded`, `failed`, `canceled`, `lost` |
| `runner_name TEXT` | unique provider-visible runner name when `kind='forgejo_runner'` |
| `golden_snapshot TEXT` | repo golden generation used, if any |
| `provider_claimed_at TIMESTAMPTZ` | when the provider bound the attempt to a job |
| `started_at`, `completed_at` | lifecycle timestamps |
| `exit_code INT` | terminal process result |
| `failure_reason TEXT` | control-plane or runtime failure |
| `duration_ms BIGINT` | wall time |
| `zfs_written BIGINT` | substrate cost signal |
| `stdout_bytes`, `stderr_bytes` | summary telemetry |
| `trace_id TEXT` | observability correlation |

### Billing Window

An attempt may span one or more billing windows.

Even before `renew` is implemented, the schema should leave room for it so the service
does not need another identity migration later.

Suggested fields:

| Field | Purpose |
|-------|---------|
| `attempt_id UUID` | parent attempt |
| `window_seq INT` | reserve order |
| `reservation JSONB` | exact billing reservation payload |
| `window_seconds INT` | reserved duration |
| `actual_seconds INT` | settled duration |
| `pricing_phase TEXT` | included, prepaid, overage, promo-backed, etc. |
| `state TEXT` | `reserved`, `settled`, `voided`, later `renewed` |
| `created_at`, `settled_at` | auditability |

### Identity Rules

These rules are important:

1. `sandbox-rental-service` creates `execution_id` before any remote side effect.
2. `sandbox-rental-service` creates `attempt_id` before billing reserve and before
   calling `vm-orchestrator`.
3. For service-owned callers, `attempt_id` must be passed into `vm-orchestrator` as
   the job identity. Production code must not rely on `vm-orchestrator` auto-generating
   job IDs.
4. A lost VM must never imply a lost execution record.
5. Provider retries and webhook duplicates must dedupe at the execution layer, not at
   the VM layer.

## Minimal Schema Recommendation

The schema does not need to be large. For the first real cut, four PostgreSQL tables are
enough:

1. `executions`
2. `execution_attempts`
3. `execution_billing_windows`
4. `execution_logs`

That is enough to support:

- direct sandbox jobs
- repo execution
- golden warming
- a VM-backed Forgejo runner supervisor
- billing reserve/settle now and renew later
- recovery after restart

No separate PostgreSQL event store is required for the first cut. ClickHouse remains the
append-only summary plane.

The important simplification is:

- `execution_attempts` owns the real state machine
- `executions` is mostly a stable identity record plus a denormalized view of the latest
  attempt state

That avoids building two independent state machines.

### Suggested Minimum Columns

The earlier sections listed a richer field set. The minimum viable implementation is smaller.

#### `executions`

| Column | Notes |
|--------|-------|
| `execution_id UUID PRIMARY KEY` | stable identity |
| `org_id BIGINT NOT NULL` | billing/quota principal |
| `actor_id TEXT NOT NULL` | user or system actor |
| `kind TEXT NOT NULL` | `direct`, `repo_exec`, `warm_golden`, `forgejo_runner` |
| `provider TEXT NOT NULL DEFAULT ''` | `forgejo` or empty |
| `product_id TEXT NOT NULL` | initially `sandbox` |
| `status TEXT NOT NULL` | projection of latest attempt |
| `idempotency_key TEXT` | nullable but unique when present |
| `repo TEXT NOT NULL DEFAULT ''` | logical repo key |
| `repo_url TEXT NOT NULL DEFAULT ''` | clone URL |
| `ref TEXT NOT NULL DEFAULT ''` | target ref |
| `commit_sha TEXT NOT NULL DEFAULT ''` | pinned SHA when known |
| `workflow_path TEXT NOT NULL DEFAULT ''` | provider metadata |
| `workflow_job_name TEXT NOT NULL DEFAULT ''` | provider metadata |
| `provider_run_id TEXT NOT NULL DEFAULT ''` | provider correlation |
| `provider_job_id TEXT NOT NULL DEFAULT ''` | provider correlation |
| `latest_attempt_id UUID` | nullable until first attempt exists |
| `created_at TIMESTAMPTZ NOT NULL` | audit |
| `updated_at TIMESTAMPTZ NOT NULL` | audit |

#### `execution_attempts`

| Column | Notes |
|--------|-------|
| `attempt_id UUID PRIMARY KEY` | service-owned attempt identity |
| `execution_id UUID NOT NULL REFERENCES executions` | parent |
| `attempt_seq INT NOT NULL` | retry order |
| `state TEXT NOT NULL` | control-plane state machine |
| `orchestrator_job_id TEXT NOT NULL DEFAULT ''` | must equal attempt identity for service-owned workloads |
| `billing_job_id BIGINT` | nullable until reserved |
| `runner_name TEXT NOT NULL DEFAULT ''` | used for Forgejo runner attempts |
| `golden_snapshot TEXT NOT NULL DEFAULT ''` | summary provenance |
| `failure_reason TEXT NOT NULL DEFAULT ''` | machine-readable failure reason |
| `exit_code INT` | nullable until terminal process outcome |
| `duration_ms BIGINT` | nullable until complete |
| `zfs_written BIGINT` | nullable until complete |
| `stdout_bytes BIGINT` | nullable until complete |
| `stderr_bytes BIGINT` | nullable until complete |
| `trace_id TEXT NOT NULL DEFAULT ''` | observability correlation |
| `provider_claimed_at TIMESTAMPTZ` | when provider job claim is known |
| `started_at TIMESTAMPTZ` | VM execution start |
| `completed_at TIMESTAMPTZ` | terminal timestamp |
| `created_at TIMESTAMPTZ NOT NULL` | audit |
| `updated_at TIMESTAMPTZ NOT NULL` | audit |

#### `execution_billing_windows`

| Column | Notes |
|--------|-------|
| `attempt_id UUID NOT NULL REFERENCES execution_attempts` | parent |
| `window_seq INT NOT NULL` | reservation order |
| `reservation JSONB NOT NULL` | exact billing payload |
| `window_seconds INT NOT NULL` | currently 300 |
| `actual_seconds INT` | populated on settle |
| `pricing_phase TEXT NOT NULL DEFAULT ''` | copied from billing reservation |
| `state TEXT NOT NULL` | `reserved`, `settled`, `voided` |
| `created_at TIMESTAMPTZ NOT NULL` | audit |
| `settled_at TIMESTAMPTZ` | audit |

Recommended PK: `(attempt_id, window_seq)`.

#### `execution_logs`

This can stay close to the current `job_logs` shape:

| Column | Notes |
|--------|-------|
| `attempt_id UUID NOT NULL REFERENCES execution_attempts` | parent attempt |
| `seq INT NOT NULL` | per-attempt ordering |
| `stream TEXT NOT NULL` | `stdout`, `stderr`, `system` |
| `chunk BYTEA NOT NULL` | raw bytes |
| `created_at TIMESTAMPTZ NOT NULL` | audit |

The same table can hold service-owned direct/repo logs. For `forgejo_runner`, this table
should only hold control-plane logs if needed, not the full Actions step log stream that
Forgejo already owns.

### What Not To Add Yet

Do not add these in the first cut unless a concrete implementation step forces it:

- a separate `execution_events` PostgreSQL table
- a separate `billing_reservations` table outside `execution_billing_windows`
- a separate `runner_registrations` table
- a generic workflow engine table set

The first cut should stay compact.

## Workload Kinds

| Kind | Trigger | Billing principal | Execution authority | Status/log authority |
|------|---------|-------------------|---------------------|----------------------|
| `direct` | website API or internal API | requesting org | `vm-orchestrator` direct job | `sandbox-rental-service` |
| `repo_exec` | website API or internal API | requesting org | `vm-orchestrator` repo exec | `sandbox-rental-service` |
| `warm_golden` | default-branch push or operator action | operator org initially | `vm-orchestrator` warm golden | `sandbox-rental-service` |
| `forgejo_runner` | runner supervisor | operator org initially | official `forgejo-runner` inside Firecracker VM | Forgejo for step logs and final job status; service for VM summary and billing |

Two clarifications matter:

- `warm_golden` is not a side effect hidden inside repo execution. It is a first-class
  workload kind with its own identity and billing.
- `forgejo_runner` is different from `repo_exec`. The service is provisioning a VM that
  hosts the official runner. Forgejo still owns workflow semantics.

## Control Flows

### Direct / Repo / Warm

These three flows are service-owned end to end.

```text
caller
  -> sandbox-rental-service
     -> create execution + attempt rows
     -> billing Reserve
     -> vm-orchestrator CreateJob / WarmGolden with attempt_id
     -> poll or stream status/logs
     -> billing Settle or Void
     -> persist final summary + logs + ClickHouse event
```

For these flows, `sandbox-rental-service` is the primary status authority.

### Forgejo Actions On VMs

The control flow is different:

```text
Forgejo scheduler
  -> VM-backed runner supervisor in sandbox-rental-service
     -> create execution + attempt rows
     -> billing Reserve
     -> vm-orchestrator boots a VM whose guest command starts forgejo-runner
     -> forgejo-runner claims one Actions job from Forgejo
     -> Forgejo receives logs and final job result natively
     -> sandbox-rental-service observes VM exit, settles billing, records summary,
        and tears the VM down
```

The important boundary is:

- Forgejo owns the workflow graph, step logs, status, secrets, reruns, and dispatch semantics.
- `sandbox-rental-service` owns admission, billing, durable identity, lifecycle tracking,
  and teardown.

## Attempt State Machine

The state machine is straightforward if it lives only on `execution_attempts`.

`executions.status` should be treated as a projection:

- copy the latest attempt state for cheap queries
- or collapse it into a smaller user-facing set like `queued`, `running`, `succeeded`,
  `failed`, `canceled`, `lost`

Do not invent a second independent state machine on `executions`.

### Attempt States

| State | Meaning |
|-------|---------|
| `queued` | the attempt row exists but no billing reservation has been recorded yet |
| `reserved` | billing has reserved funds/capacity for this attempt |
| `launching` | `vm-orchestrator` accepted the request and an `orchestrator_job_id` is persisted |
| `running` | the VM is alive or the workload has clearly begun execution |
| `finalizing` | the terminal VM outcome is known but billing finalization is not yet durably complete |
| `succeeded` | terminal success and billing has been finalized |
| `failed` | terminal failure and billing has been finalized or voided |
| `canceled` | terminal cancellation and billing has been finalized or voided |
| `lost` | the service cannot currently prove the VM's state and reconciliation is required |

The important operational distinction is between:

- execution outcome states: `succeeded`, `failed`, `canceled`
- recovery state: `lost`
- bookkeeping state: `finalizing`

`finalizing` exists because VM completion and billing completion are separate side effects.
Without it, a crash between those steps becomes ambiguous.

### Allowed Transitions

| From | To | Trigger |
|------|----|---------|
| `queued` | `reserved` | `billing.Reserve` succeeded and the first billing window row was inserted |
| `queued` | `failed` | admission denied or reserve failed with no outstanding reservation |
| `queued` | `canceled` | request canceled before reserve completed |
| `reserved` | `launching` | orchestrator accepted the create request and job identity was persisted |
| `reserved` | `canceled` | caller canceled before launch and reservation was voided |
| `reserved` | `failed` | launch failed and reservation was voided |
| `launching` | `running` | first positive execution signal arrived |
| `launching` | `finalizing` | workload terminated before a separate running signal was observed |
| `launching` | `canceled` | cancellation succeeded and billing was finalized |
| `launching` | `lost` | service restart or substrate inconsistency left outcome unknown |
| `running` | `finalizing` | terminal orchestrator or provider outcome observed |
| `running` | `canceled` | cancellation completed and billing was finalized |
| `running` | `lost` | service cannot determine whether the workload still exists |
| `finalizing` | `succeeded` | billing settle succeeded for exit code `0` |
| `finalizing` | `failed` | billing settle or void completed for non-zero exit or control-plane failure |
| `finalizing` | `canceled` | billing finalize path completed for cancellation |
| `finalizing` | `lost` | service crashed or reconciliation lost the finalize result |
| `lost` | `running` | reconciler found the live orchestrator job again |
| `lost` | `finalizing` | reconciler found a terminal workload outcome but billing was still unresolved |
| `lost` | `succeeded` | reconciler proved success and finalized billing |
| `lost` | `failed` | reconciler proved failure and finalized or voided billing |
| `lost` | `canceled` | reconciler proved cancellation and finalized or voided billing |

Anything else should be rejected as an invalid transition.

### Transition Side Effects

Each transition owns a small set of side effects.

#### `queued -> reserved`

- allocate `billing_job_id`
- call `billing.Reserve`
- insert `execution_billing_windows(window_seq=1, state='reserved')`
- persist `state='reserved'`

#### `reserved -> launching`

- construct the `vm-orchestrator` request
- set `orchestrator_job_id`
- pass the attempt identity through as the job identity
- persist `state='launching'`

#### `launching -> running`

The signal depends on workload kind:

- `direct` / `repo_exec`: first non-pending job status, first log chunk, or first telemetry frame
- `warm_golden`: entry into the run phase or any equivalent positive progress signal
- `forgejo_runner`: runner process is alive in the VM; provider job claim may still be null

Do not require provider claim before calling the attempt `running`. VM resource consumption
has already started by then.

#### `running -> finalizing`

- persist terminal VM outcome and summary metrics
- store `completed_at`
- decide whether the billing path is `settle` or `void`
- persist `state='finalizing'` before making the final billing call

#### `finalizing -> terminal`

- call `billing.Settle` or `billing.Void`
- update the relevant billing window row
- persist `succeeded`, `failed`, or `canceled`
- dual-write the final summary to ClickHouse

The terminal attempt state must only be written after billing has a durable answer.

### Failure Classification

The machine stays simpler if `failure_reason` carries detail and the number of states stays
small.

Examples:

- `quota_exceeded`
- `reserve_failed`
- `launch_failed`
- `orchestrator_not_found`
- `vm_exit_nonzero`
- `runner_registration_failed`
- `billing_settle_failed`
- `billing_void_failed`
- `canceled_by_user`
- `canceled_by_provider`

That is better than inventing one state per failure mode.

### Billing Window State Machine

Billing windows have a much smaller state machine:

| From | To | Trigger |
|------|----|---------|
| `reserved` | `settled` | `billing.Settle` succeeded |
| `reserved` | `voided` | `billing.Void` succeeded |

There is no reason to make this more elaborate until `renew` lands. When `renew` exists,
new rows get appended with `window_seq + 1`; existing rows do not change shape.

### Execution Status Projection

The execution row should not try to model billing detail. It is the query surface.

Suggested rule:

- if the latest attempt is `queued`, `reserved`, or `launching`, execution is `queued`
- if the latest attempt is `running` or `finalizing`, execution is `running`
- otherwise execution copies the latest terminal attempt state

That gives the website and operator tools a simple surface without erasing the finer-grained
attempt transitions.

### Cancellation Rules

Cancellation must be explicit because the side effects differ by state.

| Attempt state | Cancellation behavior |
|---------------|-----------------------|
| `queued` | mark `canceled`, no billing call needed |
| `reserved` | call `billing.Void`, then mark `canceled` |
| `launching` | call `vm-orchestrator.CancelJob`; if accepted, finalize billing and mark `canceled` |
| `running` | call `vm-orchestrator.CancelJob`; if the provider owns status, also mark `failure_reason='canceled_by_provider'` when appropriate |
| `finalizing` | reject duplicate cancel as a no-op |
| terminal | no-op |
| `lost` | record cancel intent and let reconciler finish the transition |

### Recovery Rules

The reconciler should operate on attempts, not executions.

#### Attempt in `reserved`

- if `orchestrator_job_id=''` and the row is stale, void billing and mark `failed`
- if the create call may have happened but the write did not, this is a bug in the calling
  transaction boundary; fix the write ordering rather than adding more states

#### Attempt in `launching` or `running`

- query `vm-orchestrator` by `orchestrator_job_id`
- if found, update to `running` or `finalizing` based on returned status
- if not found, move to `lost`

#### Attempt in `finalizing`

- read the last billing window
- if it is still `reserved`, retry `Settle` or `Void`
- once billing succeeds, transition to the correct terminal state

#### Attempt in `lost`

- try orchestrator lookup
- if provider-owned metadata exists, query provider correlation surfaces if needed
- if the workload is proven terminal, move through `finalizing` and into a terminal state
- otherwise leave it `lost` and surface it operationally

That is enough to make the machine implementable without turning it into a planning exercise.

## Why The Service Needs A Durable Attempt Identity

Without a service-owned attempt identity:

- billing cannot be reconciled safely after restarts
- the website cannot show accurate lifecycle state
- provider retries cannot be deduped
- logs and traces cannot be tied back to a stable record
- a lost `vm-orchestrator` in-memory record becomes an orphaned financial operation

The attempt record is the anchor that lets the service reconcile:

- PG state
- billing state
- `vm-orchestrator` job state
- provider-visible runner identity
- ClickHouse summary events
- any provider callback or outbox delivery

## Runner Supervisor Architecture

The VM-backed runner supervisor can begin life inside `sandbox-rental-service`.
It is an internal component, not a separate privileged daemon.

Its responsibilities are:

- decide whether a new runner VM should exist for a given label
- create the execution and attempt rows
- reserve billing for the attempt
- choose a unique runner name
- arrange runner credentials or secrets
- request a VM from `vm-orchestrator`
- watch for terminal state
- delete temporary runner registrations when required by provider limitations
- settle or void billing
- write summary telemetry and outbox events

It does not:

- interpret workflow YAML
- schedule steps
- stream step logs back to the provider itself
- manipulate ZFS or Firecracker directly

### First-Cut Contract For Runner VMs

For the first cut:

- use a dedicated label such as `forge-metal-vm:host`
- do not reuse `ubuntu-latest` while the guest remains Alpine and container support is unsettled
- cap job timeout to 5 minutes until billing `renew` exists
- treat CI spend as operator-org spend using the `platform` trust tier plan

### Provider Capability Constraints

The current live stack has real constraints that the architecture must acknowledge:

| Capability | Runner `12.8.0` | Forgejo `14.0.3` | Architectural consequence |
|------------|-----------------|------------------|---------------------------|
| `register --ephemeral` | yes | no | a short-lived VM cannot rely on automatic runner deletion today |
| `one-job --handle` | yes | no, help says Forgejo `>= 15` | the current server cannot target a specific queued attempt through this flag |
| `create-runner-file --secret` | yes, deprecated | supported by docs for modern Forgejo | may be useful for pre-registration experiments but should not become permanent architecture without another validation pass |

This creates a real decision point:

1. temporary Forgejo 14 path:
   - non-ephemeral registration
   - explicit row cleanup after teardown
   - likely coarse autoscaling or a standby VM approach
2. preferred longer-term path:
   - upgrade Forgejo before production runner cutover
   - validate ephemeral or handle-based single-job execution

The tracer bullet proved the runner seam. It did not eliminate the provider-version problem.

## Golden Warming

Default-branch warming remains a first-class control-plane operation.

Rules:

- default-branch `push` triggers `warm_golden`
- PRs and feature branches use `repo_exec` against the currently active golden
- warm state must never contain customer secrets
- repo golden generation remains an execution-substrate concern in `vm-orchestrator`
- the service records which golden generation was used or produced

Webhooks are appropriate here because warming is repo-event-driven and does not need to
pretend it is an Actions job.

## Observability

The target observability split is:

- PostgreSQL: authoritative live control-plane state
- ClickHouse: append-only execution summaries, logs where appropriate, reconciliation evidence
- Forgejo UI: native Actions step logs and job result for `forgejo_runner`
- OpenTelemetry: spans and metrics for service/orchestrator internals

For `forgejo_runner` workloads, the service should record summary facts only:

- attempt identity
- provider correlation IDs
- billed duration
- VM metrics
- terminal status
- runner name

It should not try to duplicate the entire Actions log stream that Forgejo already owns.

## Reconciliation And Recovery

This architecture is only correct if it can recover from process death.

Required reconciliation loops:

1. reserved-but-not-launched attempts:
   - if billing succeeded but `vm-orchestrator` was never called, void
2. launched-but-unsettled attempts:
   - if the VM terminated but billing is still reserved, settle or void exactly once
3. missing orchestrator jobs:
   - if PG says running but `vm-orchestrator` no longer knows the job, mark the attempt
     as `lost` and run a targeted recovery path
4. stale Forgejo runner rows:
   - delete by runner name for any terminated temporary VM-backed runner
5. provider callback/outbox rows:
   - retry until delivered or explicitly dead-lettered

The current in-memory job map in `vm-orchestrator` is acceptable as a substrate detail,
but it means `sandbox-rental-service` must be the durable source of truth.

## Security Constraints

- The guest label must describe reality. An Alpine guest without the expected runtime
  stack must not claim `ubuntu-latest`.
- Provider secrets, billing state, and auth context belong in the service layer, not
  in `vm-orchestrator`.
- Any runner registration token or shared secret injected into a VM must live only in
  a transient execution clone, never in a shared repo golden.
- Webhook ingress must be HMAC-validated and idempotent.
- Cross-org reads of execution metadata and logs must be enforced server-side.

## Recommended Near-Term Sequence

1. expand `sandbox-rental-service` PostgreSQL schema from `jobs` into `executions`,
   `execution_attempts`, `execution_billing_windows`, and renamed log tables
2. replace goroutine-only lifecycle handling with persisted state transitions
3. wire direct, repo, and warm workloads onto the new model first
4. build the runner supervisor on top of the same persisted attempt model
5. enforce the 5-minute cap in code before any VM-backed CI cutover
6. decide whether to tolerate Forgejo 14 temporary compromises or upgrade Forgejo before
   the production runner cutover
7. remove the host-runner path and deprecated CLI CI path only after the VM-backed path
   is proven under the new service-owned model

## Open Questions

These are the remaining architectural questions that still need targeted experiments
or code-reading before the design is final:

1. Should the first VM-backed runner cut keep one warm standby VM per label, or can we
   tolerate queue startup latency while using webhook hints or coarse polling?
2. Can `forgejo-runner one-job` plus a pre-created runner file replace the daemon flow
   cleanly on Forgejo `14.0.3`, or is a Forgejo upgrade the cleaner line in the sand?
3. How should the service learn which exact Forgejo job a temporary runner claimed on
   Forgejo `14.0.3`, where the newer handle-oriented path is unavailable?
4. Which minimal guest/runtime additions are necessary to support the first useful set
   of workflows without claiming general `ubuntu-latest` compatibility?
5. Which summary fields belong in the unified ClickHouse `job_events` row for
   `forgejo_runner`, given that Forgejo remains the source of truth for step-level logs?

This document should evolve alongside the control-plane implementation. The next edits
should happen after the schema refactor and after the first runner-supervisor experiment
against the new persisted attempt model.
