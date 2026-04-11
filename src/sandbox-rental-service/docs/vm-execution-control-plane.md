# VM Execution Control Plane

sandbox-rental-service owns the customer-facing execution state machine, billing
reserve/settle/void workflow, repo metadata import, and API policy checks.
vm-orchestrator owns only privileged Firecracker lifecycle and telemetry over its
Unix-socket gRPC API.

## Execution Kinds

The execution table currently accepts `direct` jobs only. A direct job carries a
submitted `run_command`; sandbox-rental-service reserves billing, asks
vm-orchestrator to run the command in a fresh microVM, streams logs into
PostgreSQL/ClickHouse, then settles or voids the billing window based on the
terminal outcome.

Repo import is metadata and clone-access validation. It does not enqueue a
bootstrap job, parse a repo-owned CI manifest, or create repo-scoped golden
images.

## Resource Accounting

Billing windows are keyed by `attempt_id` and use the same `execution_attempt`
source type for reserve, settle, and void. vm-orchestrator reports VM-level
duration, ZFS written bytes, log byte counts, and guest telemetry; host-level
network I/O accounting remains a vm-orchestrator responsibility rather than
guest-agent authority.

## State Machine

1. `queued`: execution row and first attempt are durable.
2. `reserved`: billing reserve succeeded and `execution_billing_windows` has a
   reserved window.
3. `launching`: sandbox-rental-service has assigned an orchestrator job id.
4. `running`: vm-orchestrator accepted the direct job and the attempt started.
5. `finalizing`: workload completed and billing settlement is in progress.
6. Terminal: `succeeded`, `failed`, or `canceled`.

The reconciler repairs interrupted `reserved` and `finalizing` attempts by
voiding or settling their billing windows before moving the execution terminal.
