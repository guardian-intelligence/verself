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
