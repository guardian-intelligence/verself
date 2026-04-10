# VM Execution Control Plane

This document describes the VM execution architecture forge-metal is aiming to
build.

The central split is:

- `sandbox-rental-service` is the policy, identity, and billing layer.
- `vm-orchestrator` is the privileged Firecracker execution substrate.

`sandbox-rental-service` decides whether work may run, who it belongs to, how it
is billed, and how it is reconciled. `vm-orchestrator` boots VMs, manages ZFS
state, runs guest workloads, and reports execution results. It does not know
about org policy, prices, quotas, grants, promotions, or provider product
semantics.

## Product Model

The CI product contract is intentionally narrow in v1.

- Workflow YAML is the execution contract.
- The only supported runner label is `forge-metal`.
- The only supported runner profile slug is `forge-metal`.
- That profile maps to a provider-managed base image, resource shape, and
  warm-image policy.
- Users do not select raw Firecracker images or snapshots.
- A repo must be imported before it can run CI on forge-metal.
- A repo becomes runnable only after forge-metal prepares a repo-scoped golden
  from the default branch.

This is a Blacksmith-like contract, but with a Firecracker-specific warm-image
pipeline behind it.

## What Users Do

1. Connect or import a repo.
2. Update workflow jobs to use `runs-on: forge-metal`.
3. Wait for forge-metal to prepare the first default-branch golden.
4. Run future CI jobs against that active golden.

Default-branch pushes refresh the golden in the background. PRs, branches, and
manual CI runs continue using the currently active golden until the new
generation is ready.

## Core Principles

- One durable execution identity is created before any remote side effect.
- One concrete VM launch is represented by one attempt.
- Billing is based on reserved capacity times wall-clock duration, not sampled
  guest CPU usage.
- A Firecracker VM is operationally ephemeral. The durable unit is the attempt,
  not the VM.
- Webhooks are used for repo lifecycle events such as golden refresh, not as the
  execution seam for real Actions jobs.
- Forgejo remains the workflow engine. The official `forgejo-runner` inside the
  VM remains the runner for real Actions jobs.
- ClickHouse stores append-only execution summaries, mirrored logs, and billing
  evidence. PostgreSQL stores live control-plane state.

## Runtime Boundaries

| Module | Owns | Must not own |
|--------|------|---------------|
| `sandbox-rental-service` | authz, repo import state, compatibility checks, execution identity, attempt state machine, billing reserve/settle later renew, admission control, reconciliation, API surface, execution summaries | Firecracker internals, ZFS operations, TAP setup, jailer lifecycle |
| `vm-orchestrator` | Firecracker lifecycle, ZFS clone/snapshot/destroy, TAP networking, guest agent protocol, direct jobs, repo execution, golden warming, runner VM boot, guest event relay | billing, quotas, org policy, repo import rules, provider business logic |
| `billing-service` | plans, rates, grants, quota rules, reserve/settle/void later renew, financial reconciliation | VM lifecycle, repo state, provider integration |
| Forgejo | workflow graph, scheduling, secrets, runner matching, reruns, dispatch, native Actions status and step logs | VM lifecycle, billing, org quota enforcement |

## CI Model

There are two distinct CI concerns:

1. Repo readiness
   - import repo
   - scan workflows
   - determine compatibility with `runs-on: forge-metal`
   - prepare and activate a golden from the default branch

2. Job execution
   - a Forgejo Actions job is assigned to a runner VM
   - the runner VM uses the repo's active golden
   - `sandbox-rental-service` applies policy and billing around that VM launch

The first concern is repo-scoped and asynchronous. The second is job-scoped and
repeats for every execution.

## Repo Lifecycle

Repo lifecycle state models product readiness, not a single VM launch.

| State | Meaning |
|-------|---------|
| `importing` | repo record exists but metadata and workflow compatibility have not been resolved yet |
| `action_required` | repo is not compatible with forge-metal v1, usually because workflows do not use `runs-on: forge-metal` |
| `waiting_for_bootstrap` | repo is compatible but no bootstrap golden generation has started yet |
| `preparing` | a golden generation is currently being built from the default branch |
| `ready` | repo has an active ready golden and can serve CI |
| `degraded` | repo still has an active older golden, but the latest refresh failed |
| `failed` | repo has no active ready golden and the latest bootstrap or refresh failed |
| `archived` | repo is intentionally disabled from new forge-metal work |

### Repo State Transitions

| From | To | Trigger |
|------|----|---------|
| `importing` | `action_required` | compatibility scan found unsupported labels, no workflows, or other blockers |
| `importing` | `waiting_for_bootstrap` | compatibility scan succeeded |
| `action_required` | `waiting_for_bootstrap` | workflows were corrected and the repo rescanned cleanly |
| `waiting_for_bootstrap` | `preparing` | first golden generation was queued |
| `preparing` | `ready` | first generation became active |
| `preparing` | `failed` | bootstrap generation failed and no older active generation exists |
| `ready` | `preparing` | default-branch push or manual refresh queued a new generation |
| `ready` | `degraded` | refresh failed while an older active golden still exists |
| `degraded` | `preparing` | another refresh started |
| `degraded` | `ready` | a new generation became active |
| `failed` | `preparing` | a new bootstrap or refresh was queued |
| any non-terminal | `archived` | repo was disabled |

### Repo Routing Rules

- Only repos in `ready` or `degraded` may serve CI runs.
- `degraded` means the previous active golden is still usable.
- `failed` means there is no usable active golden.
- A failed refresh must never clear an existing active golden.

## Golden Generation Lifecycle

A golden generation represents one build of the active warm state for one repo
and one runner profile. In v1 the profile is always `forge-metal`, but the
schema keeps the profile slug so later profiles do not require a data model
rewrite.

| State | Meaning |
|-------|---------|
| `queued` | generation exists but its bootstrap execution has not started running yet |
| `building` | bootstrap execution is running |
| `sanitizing` | bootstrap execution finished and the filesystem is being sanitized before activation |
| `ready` | generation is complete and may serve future CI runs |
| `failed` | bootstrap execution or sanitization failed |
| `superseded` | a newer ready generation replaced this one as active |

### Generation State Transitions

| From | To | Trigger |
|------|----|---------|
| `queued` | `building` | bootstrap execution attempt entered `running` |
| `queued` | `failed` | bootstrap failed before meaningful build progress |
| `building` | `sanitizing` | bootstrap execution finished and snapshot finalization started |
| `building` | `failed` | bootstrap execution failed |
| `sanitizing` | `ready` | sanitization completed and repo activation pointer was updated |
| `sanitizing` | `failed` | sanitization or activation failed |
| `ready` | `superseded` | a newer generation became active |

### Generation Rules

- Only one generation per `(repo, runner_profile_slug)` may be active at a time.
- Activation is a pointer flip on the repo row, not an in-place mutation of an
  older generation.
- A generation is immutable with respect to its source revision. A new default
  branch SHA creates a new generation row.
- The active golden must be sanitized before activation. Raw post-run disk state
  is not promoted directly.

## Execution Model

Repo lifecycle is not enough by itself. Every VM-backed operation still needs a
durable execution model.

The execution model is shared across:

- direct sandbox launches
- repo-backed execution
- golden bootstrap and refresh work
- Forgejo runner VMs

### Execution

An execution is the stable unit of work visible to the service and, where
relevant, to providers or users.

Examples:

- one sandbox run from the website
- one golden bootstrap
- one Forgejo runner VM lease

### Attempt

An attempt is one concrete VM launch under an execution.

One attempt maps to:

- one service-owned `attempt_id`
- one billing reservation flow
- one `vm-orchestrator` job identity
- one ephemeral Firecracker VM lifecycle

Retries create new attempts under the same execution.

### Billing Window

A billing window is one durable reserve/settle or reserve/void slice attached to
an attempt.

In v1:

- reservations are 300-second windows
- long-running attempts renew into successive windows before the current window ends
- the schema persists every window transition durably on `execution_billing_windows`

## Attempt State Machine

The real state machine lives only on `execution_attempts`.

`executions.status` is a projection of the latest attempt, not a second
independent machine.

| State | Meaning |
|-------|---------|
| `queued` | attempt exists but billing has not reserved yet |
| `reserved` | billing reserved the first window |
| `launching` | `vm-orchestrator` accepted the launch request |
| `running` | the workload is clearly consuming VM resources |
| `finalizing` | terminal workload outcome is known but billing is not durably closed yet |
| `succeeded` | terminal success and billing is finalized |
| `failed` | terminal failure and billing is finalized or voided |
| `canceled` | terminal cancellation and billing is finalized or voided |
| `lost` | the service cannot currently prove the workload outcome and reconciliation is required |

### Allowed Transitions

| From | To | Trigger |
|------|----|---------|
| `queued` | `reserved` | `billing.Reserve` succeeded |
| `queued` | `failed` | reserve or admission failed |
| `queued` | `canceled` | request was canceled before reserve |
| `reserved` | `launching` | `vm-orchestrator` accepted the launch |
| `reserved` | `failed` | launch failed and reservation was voided |
| `reserved` | `canceled` | caller canceled and reservation was voided |
| `launching` | `running` | first positive execution signal arrived |
| `launching` | `finalizing` | workload terminated before a separate running signal was observed |
| `launching` | `canceled` | cancel completed and billing was finalized |
| `launching` | `lost` | service can no longer prove workload state |
| `running` | `finalizing` | terminal workload outcome was observed |
| `running` | `canceled` | cancel completed and billing was finalized |
| `running` | `lost` | service can no longer prove workload state |
| `finalizing` | `succeeded` | billing settle succeeded for a successful outcome |
| `finalizing` | `failed` | billing settle or void completed for a failed outcome |
| `finalizing` | `canceled` | billing finalize path completed for a canceled outcome |
| `finalizing` | `lost` | final outcome was interrupted before billing closure was persisted |
| `lost` | `running` | reconciler found the live workload again |
| `lost` | `finalizing` | reconciler found a terminal workload outcome with unresolved billing |
| `lost` | `succeeded` | reconciler proved success and finalized billing |
| `lost` | `failed` | reconciler proved failure and finalized or voided billing |
| `lost` | `canceled` | reconciler proved cancellation and finalized or voided billing |

### Transition Rules

- `sandbox-rental-service` creates `execution_id` before any remote side effect.
- `sandbox-rental-service` creates `attempt_id` before billing reserve.
- `attempt_id` is passed into `vm-orchestrator` as the job identity for
  service-owned launches.
- Terminal attempt state is written only after billing has a durable answer.
- A lost VM never implies a lost execution record.

### Guest Correlation

The service threads service-owned identity into the guest environment and guest
event path.

The important values are:

- `execution_id`
- `attempt_id`
- `orchestrator_job_id`
- `runner_name` when the workload is a runner VM

This is enough to correlate guest-originated events back to the correct
execution attempt. The provider's job or run identity is learned later, after
the runner has claimed work, and is attached back onto the same execution and
attempt records.

## Database Schema

The first-pass schema is intentionally compact.

### `repos`

`repos` is the durable product record for an imported repository.

| Column | Type | Notes |
|--------|------|-------|
| `repo_id` | `uuid primary key` | stable forge-metal repo identity |
| `org_id` | `bigint not null` | owning org |
| `provider` | `text not null` | initially `forgejo`, later `github` |
| `provider_repo_id` | `text not null` | provider-native repo identifier |
| `owner` | `text not null` | namespace or owner slug |
| `name` | `text not null` | repo name |
| `full_name` | `text not null` | denormalized `owner/name` |
| `clone_url` | `text not null` | canonical clone URL |
| `default_branch` | `text not null` | branch used for compatibility and golden refresh |
| `runner_profile_slug` | `text not null default 'forge-metal'` | future-proofing for later profiles |
| `state` | `text not null` | repo lifecycle state |
| `compatibility_status` | `text not null default ''` | summary of the latest compatibility decision |
| `compatibility_summary` | `jsonb not null default '{}'::jsonb` | actionable findings such as unsupported labels and workflow paths |
| `last_scanned_sha` | `text not null default ''` | default-branch SHA used for the latest scan |
| `active_golden_generation_id` | `uuid` | current active generation pointer, nullable until ready |
| `last_ready_sha` | `text not null default ''` | default-branch SHA baked into the active generation |
| `last_error` | `text not null default ''` | latest repo-level failure summary |
| `created_at` | `timestamptz not null` | audit |
| `updated_at` | `timestamptz not null` | audit |
| `archived_at` | `timestamptz` | nullable |

Recommended indexes:

- unique `(provider, provider_repo_id)`
- unique `(org_id, provider, full_name)`
- index `(org_id, state, updated_at desc)`

### `golden_generations`

`golden_generations` tracks bootstrap and refresh builds of repo-scoped warm
state.

| Column | Type | Notes |
|--------|------|-------|
| `golden_generation_id` | `uuid primary key` | stable generation identity |
| `repo_id` | `uuid not null references repos(repo_id)` | owning repo |
| `runner_profile_slug` | `text not null` | `forge-metal` in v1 |
| `source_ref` | `text not null` | usually `refs/heads/<default_branch>` |
| `source_sha` | `text not null` | exact default-branch commit used to build this generation |
| `state` | `text not null` | generation lifecycle state |
| `trigger_reason` | `text not null default ''` | `bootstrap`, `default_branch_push`, `manual_refresh` |
| `execution_id` | `uuid` | execution record for the bootstrap or refresh work |
| `attempt_id` | `uuid` | latest attempt for that execution |
| `orchestrator_job_id` | `text not null default ''` | substrate correlation for the active attempt |
| `snapshot_ref` | `text not null default ''` | substrate-facing golden identifier |
| `activated_at` | `timestamptz` | when the generation became active |
| `superseded_at` | `timestamptz` | when a newer generation replaced it |
| `failure_reason` | `text not null default ''` | machine-readable terminal failure classification |
| `failure_detail` | `text not null default ''` | short operator-facing explanation |
| `created_at` | `timestamptz not null` | audit |
| `updated_at` | `timestamptz not null` | audit |

Recommended indexes:

- index `(repo_id, runner_profile_slug, created_at desc)`
- index `(execution_id)`
- partial unique index on `(repo_id, runner_profile_slug)` where the generation is
  the active ready generation

### `executions`

`executions` is the stable control-plane record for one unit of work.

| Column | Type | Notes |
|--------|------|-------|
| `execution_id` | `uuid primary key` | stable identity |
| `org_id` | `bigint not null` | billing principal |
| `actor_id` | `text not null` | user or system actor |
| `product_id` | `text not null` | initially `sandbox` |
| `kind` | `text not null` | `direct`, `repo_exec`, `warm_golden`, `forgejo_runner` |
| `provider` | `text not null default ''` | `forgejo`, later others |
| `status` | `text not null` | projection of latest attempt |
| `idempotency_key` | `text` | nullable but unique when present |
| `repo_id` | `uuid` | nullable for non-repo work |
| `repo` | `text not null default ''` | logical repo key or `owner/name` |
| `repo_url` | `text not null default ''` | clone URL when relevant |
| `ref` | `text not null default ''` | target ref |
| `commit_sha` | `text not null default ''` | pinned SHA when known |
| `workflow_path` | `text not null default ''` | provider metadata |
| `workflow_job_name` | `text not null default ''` | provider metadata |
| `provider_run_id` | `text not null default ''` | external correlation |
| `provider_job_id` | `text not null default ''` | external correlation |
| `latest_attempt_id` | `uuid` | nullable until first attempt exists |
| `created_at` | `timestamptz not null` | audit |
| `updated_at` | `timestamptz not null` | audit |

### `execution_attempts`

`execution_attempts` owns the real lifecycle state machine.

| Column | Type | Notes |
|--------|------|-------|
| `attempt_id` | `uuid primary key` | service-owned attempt identity |
| `execution_id` | `uuid not null references executions(execution_id)` | parent execution |
| `attempt_seq` | `int not null` | retry order |
| `state` | `text not null` | attempt lifecycle state |
| `orchestrator_job_id` | `text not null default ''` | substrate job identity |
| `billing_job_id` | `bigint` | nullable until reserve succeeds |
| `runner_name` | `text not null default ''` | provider-visible runner name for runner VMs |
| `golden_snapshot` | `text not null default ''` | snapshot provenance |
| `failure_reason` | `text not null default ''` | machine-readable detail |
| `exit_code` | `int` | nullable until terminal outcome |
| `duration_ms` | `bigint` | nullable until completion |
| `zfs_written` | `bigint` | nullable until completion |
| `stdout_bytes` | `bigint` | nullable until completion |
| `stderr_bytes` | `bigint` | nullable until completion |
| `trace_id` | `text not null default ''` | observability correlation |
| `provider_claimed_at` | `timestamptz` | when the provider bound the runner to a job |
| `started_at` | `timestamptz` | when execution began |
| `completed_at` | `timestamptz` | terminal timestamp |
| `created_at` | `timestamptz not null` | audit |
| `updated_at` | `timestamptz not null` | audit |

### `execution_billing_windows`

`execution_billing_windows` is the durable bridge between attempts and financial
settlement.

| Column | Type | Notes |
|--------|------|-------|
| `attempt_id` | `uuid not null references execution_attempts(attempt_id)` | parent attempt |
| `window_seq` | `int not null` | reservation order |
| `reservation` | `jsonb not null` | exact billing reservation payload |
| `window_seconds` | `int not null` | reserved duration |
| `actual_seconds` | `int` | populated on settle |
| `pricing_phase` | `text not null default ''` | copied from billing |
| `state` | `text not null` | `reserved`, `settled`, `voided` |
| `created_at` | `timestamptz not null` | audit |
| `settled_at` | `timestamptz` | audit |

Primary key: `(attempt_id, window_seq)`.

### `execution_logs`

`execution_logs` stores ordered attempt log chunks.

| Column | Type | Notes |
|--------|------|-------|
| `attempt_id` | `uuid not null references execution_attempts(attempt_id)` | parent attempt |
| `seq` | `int not null` | per-attempt ordering |
| `stream` | `text not null` | `stdout`, `stderr`, `system` |
| `chunk` | `bytea not null` | raw bytes |
| `created_at` | `timestamptz not null` | audit |

For Forgejo runner VMs this table stores control-plane and mirrored runner logs,
not the canonical provider step-log presentation.

## Billing Model

Billing is based on reserved capacity multiplied by wall-clock duration.

For v1, the billable dimensions are:

- `vcpu`
- `gib`

The billing sequence is:

1. create execution and attempt
2. call `billing.Reserve`
3. persist `execution_billing_windows(window_seq=1, state='reserved')`
4. launch the VM
5. observe terminal outcome
6. call `billing.Settle` or `billing.Void`
7. persist the final attempt state and billing window state
8. write the ClickHouse summary row

For long-running attempts:

- `sandbox-rental-service` renews the current billing window before `reservation.renew_by`
- each successful renew settles the current window and persists the next reserved window
- if renew is denied, the workload is stopped after the already-settled window

## ClickHouse Model

ClickHouse remains append-only and execution-oriented.

It stores:

- execution summary rows
- billing metering rows written by `billing-service`
- mirrored control-plane and runner logs where appropriate

It does not replace PostgreSQL as the source of truth for live execution state.

For CI, Forgejo remains the user-facing system of record for workflow step logs
and final provider job state. ClickHouse stores an operator and analytics mirror.

Surgical note for later:

- if tenants need stronger control over provider log ownership and retention, the
  mirrored log backend may need to move out of shared ClickHouse into a separate
  multi-tenant log store

## Control Flows

### Repo Import And Bootstrap

```text
user or provider integration
  -> sandbox-rental-service imports repo
  -> service scans workflow files on default branch
  -> repo enters action_required or waiting_for_bootstrap
  -> service creates golden_generation + warm_golden execution
  -> billing Reserve
  -> vm-orchestrator builds sanitized repo golden
  -> billing Settle or Void
  -> service activates the generation on success
```

### Default-Branch Refresh

```text
default-branch push webhook
  -> sandbox-rental-service creates a new golden_generation
  -> service launches warm_golden execution
  -> old active generation remains live
  -> new generation activates only after ready
```

### CI Execution

```text
Forgejo scheduler
  -> runner VM is provisioned through sandbox-rental-service
  -> billing Reserve
  -> vm-orchestrator boots a VM from the repo's active golden
  -> official forgejo-runner inside the VM claims one job
  -> Forgejo receives native job status and step logs
  -> service observes attempt completion, settles billing, records summary, and tears down the VM
```

## Reconciliation

The architecture is only correct if the service can recover from process death.

Required reconciliation loops:

1. reserved but not launched
   - void stale reservations that never became real launches
2. launched but not finalized
   - settle or void exactly once after terminal workload outcome is known
3. lost orchestrator jobs
   - mark the attempt `lost` and run targeted recovery
4. repo refresh drift
   - keep old active generations live until a newer one is truly ready
5. stale runner rows
   - explicitly remove provider runner registrations when the current Forgejo
     version cannot do provider-side ephemeral cleanup

## Current v1 Constraints

These constraints are part of the design, not hidden implementation details.

- one supported runner profile slug: `forge-metal`
- one supported workflow label: `forge-metal`
- no warm VM pool yet
- no user-facing custom base images yet
- the label must stay honest and must not pretend the current guest is a
  broader compatibility target than it really is
- no multi-window billing yet
- a hard five-minute cap on CI attempts until renew exists
- current Forgejo runner lifecycle may require explicit stale-runner cleanup

## Summary

The architecture is:

- repo import and golden management live in `sandbox-rental-service`
- execution identity and billing live in `sandbox-rental-service`
- Firecracker, ZFS, and guest orchestration live in `vm-orchestrator`
- Forgejo remains the Actions workflow engine
- one repo has one active golden for one supported v1 profile
- every VM-backed launch is tracked as an execution attempt with durable billing
  windows and append-only summary evidence
