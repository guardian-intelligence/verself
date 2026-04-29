# Change Data Capture

The platform does not adopt a third-party CDC appliance as a committed
infrastructure plane.

This document records the replacement direction:

- **Current state:** some services still dual-write PostgreSQL and
  ClickHouse in the request path.
- **Near-term correction:** retire request-path dual writes by moving to
  service-owned transactional projection delivery queues.
- **Future shared CDC plane:** if a repo-wide WAL reader is still
  justified after the transactional outbox work lands, build a
  repo-owned Postgres logical-replication bridge into ClickHouse rather
  than introducing a separate vendor control plane.

CDC remains one of three async-infrastructure concerns alongside
[durable execution](durable-execution.md) and the
[domain event stream](domain-event-stream.md). The difference is that
the repo no longer commits to a specific external CDC product.

## What exists today

The current pattern is **application-level projection delivery**:

- PostgreSQL remains the source of truth for product and billing state.
- ClickHouse remains the smoke test, analytics, and operator-read-model
  store.
- Some services still write both in the same request path.
- The safer target shape, already reflected in billing, is a
  transactional queue row in PostgreSQL plus at-least-once delivery to
  ClickHouse by a worker. The service transaction commits authoritative
  state and the delivery record together; ClickHouse projection happens
  after.

This is not full WAL-based CDC, but it removes the main architectural
problem: request-path dependence on ClickHouse availability and latency.

## What the platform should do next

The next brick is not a shared CDC product. It is to make every service
that currently dual-writes adopt the same transactional projection
discipline used elsewhere in the repo:

1. PostgreSQL transaction writes authoritative state.
2. The same transaction writes immutable fact rows and projection-delivery
   rows.
3. A worker re-reads PostgreSQL truth, inserts deterministic ClickHouse
   rows, and marks delivery succeeded.
4. Reconciliation proves the projection against PostgreSQL ground truth.

`billing-service` is the reference shape for this today. The first
cutovers should continue to be per-service elimination of request-path
dual writes, not introduction of a new shared CDC subsystem.

## When a shared CDC plane becomes justified

A shared WAL-based CDC plane becomes worth building only when at least one
of these is true:

- multiple services need the same low-shape-loss Postgres change stream;
- projection workers are duplicating too much schema-tracking logic;
- replaying full service-owned projection queues is materially more
  expensive than tailing WAL once;
- ETL work benefits from raw row-change capture instead of
  service-authored immutable facts.

Until then, the transactional outbox path is simpler, more service-local,
and already matches the repo's correctness model.

## Target shared CDC shape

If the repo still needs a shared CDC plane after the projection-outbox
work lands, the preferred design is a **repo-owned logical replication
service** with a deliberately narrow scope:

- **Source:** PostgreSQL logical replication publications.
- **Transport:** `pgoutput`, one publication per service schema or
  another explicitly bounded ownership unit.
- **Sink:** ClickHouse only.
- **Output:** append-only raw change tables in ClickHouse plus
  service-owned downstream projections or materialized views.
- **Boundary:** one repo-owned Go service, deployed and observed like
  the rest of the platform.

This keeps the stack aligned with repo invariants:

- one self-hosted trust domain;
- SPIFFE on repo-owned service boundaries;
- ClickHouse as the only analytics sink we actually operate;
- no extra SQL/UI/control plane that platform services have to route
  around;
- no second product-specific catalog database.

## What this plane does not replace

- It is not the [domain event stream](domain-event-stream.md). Domain
  events are service-authored facts for other services to consume.
- It is not [durable execution](durable-execution.md). Multi-step
  orchestration stays on Temporal or River depending on the boundary.
- It does not replace reconciliation. WAL transport is not a correctness
  smoke test.
- It is not a general connector marketplace. The scope is Postgres
  logical replication into ClickHouse.

## Why not Kafka + Debezium today

Kafka + Debezium remains the most credible off-the-shelf reevaluation
point once the platform actually has a 3-node topology and real CDC
pressure.

It is not the default direction for the current single-node deployment
because:

- it adds multiple new distributed components to solve one narrow path;
- the repo only needs Postgres → ClickHouse, not a broad connector zoo;
- it does not improve the immediate problem, which is request-path
  dual-write elimination;
- a repo-owned service is easier to integrate with the current SPIFFE,
  governance, and observability contracts.

The 3-node evolution should re-evaluate this tradeoff with live data. If
the repo reaches the point where connector breadth, replay semantics, or
throughput justify Kafka + Debezium, that is the place to pay the
operational cost.

## SPIFFE posture

For the current outbox-based projection path:

- the owning service speaks SPIFFE on its repo-owned boundaries;
- PostgreSQL auth stays in the existing local peer-auth model where the
  service already owns the database boundary;
- ClickHouse projection uses the existing certificate-backed client path.

For a future shared CDC service:

- the CDC service itself is a repo-owned SPIFFE workload;
- PostgreSQL replication auth should stay repo-owned and local when the
  source database is on the same host;
- ClickHouse sink auth should stay on the existing certificate-backed
  path;
- no shared bearer tokens or unmanaged static credentials are added just
  to make CDC work.

## Observability

`aspect observe --what=cdc` should exist only when a shared CDC service
exists.

Until then, observability lives on the owning service's existing smoke test
surface:

- PostgreSQL authoritative rows;
- River or service worker state;
- ClickHouse projected rows;
- OTel traces spanning the authoritative write and projection delivery;
- service-specific reconciliation output.

The smoke-test artifact for any dual-write retirement remains the same:

- write one authoritative fact into PostgreSQL;
- observe the corresponding ClickHouse row under a fresh trace ID;
- prove replay/idempotency and reconciliation against source truth.

## Cutover order

1. Retire request-path dual writes service by service using transactional
   projection delivery.
2. Keep reconciliation as the correctness gate for each service cutover.
3. Re-evaluate whether a shared WAL-based CDC plane is still needed after
   those cutovers.
4. If yes, introduce one repo-owned logical replication service and
   cut over only the projections that benefit from raw CDC.

No service should change both its correctness model and its transport
model in one step.

## Known unknowns

The implementing agent for the future shared CDC service must answer:

1. Publication granularity: one publication per service schema, per
   database, or per projection family.
2. Replication slot budgeting and WAL retention on the single-node
   platform Postgres.
3. ClickHouse table shape for raw row changes: whether service-local
   projected tables should derive from raw CDC tables, service-authored
   immutable facts, or both.
4. Delete/update semantics, ordering guarantees, and replay windows for
   append-only ClickHouse projections.

## Source notes

- Workload identity contract: [`workload-identity.md`](workload-identity.md).
- Related planes:
  [`durable-execution.md`](durable-execution.md),
  [`domain-event-stream.md`](domain-event-stream.md).
- System context on the current dual-write pattern:
  [`../system-context.md`](../system-context.md).
- Billing's transactional projection and reconciliation shape:
  [`../../src/billing-service/docs/billing-architecture.md`](../../src/billing-service/docs/billing-architecture.md).
- PostgreSQL logical replication:
  <https://www.postgresql.org/docs/current/logical-replication.html>.
- Debezium architecture and operational model:
  <https://debezium.io/documentation/reference/stable/architecture.html>.
