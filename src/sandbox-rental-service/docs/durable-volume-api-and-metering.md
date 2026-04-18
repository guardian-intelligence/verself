# Durable Volume API And Metering

sandbox-rental-service owns the customer-visible durable volume product surface.
vm-orchestrator owns privileged host execution and ZFS facts. Customer APIs,
browser UI, billing descriptions, IAM policies, and support tooling must expose
volumes and generations, not zvols, datasets, pool names, host paths, device
paths, or Firecracker/jailer arguments.

This document describes the target product/control-plane architecture. Some
paths are still proof-only.

## Customer API

The customer API is organization-scoped through the caller's Zitadel
organization context. Every handler must check operation permission before
reading sandbox-rental PostgreSQL or querying ClickHouse.

Target endpoints:

- `GET /api/v1/storage/summary`
- `GET /api/v1/volumes`
- `POST /api/v1/volumes`
- `GET /api/v1/volumes/{volume_id}`
- `GET /api/v1/volumes/{volume_id}/generations`
- `GET /api/v1/volumes/{volume_id}/usage`

Customer responses may include product fields such as `volume_id`,
`display_name`, `state`, `current_generation_id`, `retention_policy`,
`created_at`, `updated_at`, latest measured live bytes, latest measured retained
bytes, and billing/usage summaries. They must not include storage node IDs,
pool IDs, dataset refs, snapshot refs, zvol device paths, host mount paths, or
orchestrator implementation names.

ClickHouse may serve usage history through sandbox-rental, but it is not the
source of truth for volume ownership, current generation, IAM, or lifecycle
state. The customer usage API always applies `org_id` filtering inside
sandbox-rental; callers never query ClickHouse directly.

## Internal API

The current hidden `POST /api/v1/volumes/{volume_id}/meter-ticks` route is a
proof endpoint. It accepts measured bytes from the caller so billing and
projection can be verified before vm-orchestrator volume lifecycle work exists.
It is not the target customer API and must not be exposed as a way for customers
to submit billable measurements.

Target metering input comes from vm-orchestrator or an internal
service-authenticated collector that reads authoritative ZFS facts through
typed refs. sandbox-rental records product state, billing policy, idempotency,
and projection state; it does not receive host privileges or raw ZFS access.

## Metering Flow

The 60s durable volume sweep records storage at rest:

1. vm-orchestrator reads authoritative ZFS properties for the requested product
   refs and returns typed measurements.
2. sandbox-rental records a `volume_meter_ticks` row in PostgreSQL with the
   measurement, product identity, source identity, and billing state.
3. sandbox-rental computes live bytes as `max(used - usedbysnapshots, 0)` and
   retained bytes as `usedbysnapshots`.
4. sandbox-rental reserves a billing-service window with
   `source_type = volume_meter_tick`, `source_ref` derived from the tick, and
   `window_millis` equal to the sweep duration.
5. sandbox-rental settles the same `window_millis` quantity with allocation
   keyed by durable volume SKUs.
6. On success, sandbox-rental records the billing window ID and charge units,
   then inserts a transactional projection delivery row for
   `forge_metal.volume_meter_ticks`.
7. Billing-service independently projects the settled billing window into
   `forge_metal.metering` from its own billing projection delivery queue.

If billing denies the tick, sandbox-rental marks the tick `billing_failed`,
records the failure reason, and applies product policy such as `write_blocked`
or read-only retention. The unbilled measurement is still projected as product
evidence through the same sandbox-rental projection path so operators can see
what happened.

## Projection Outbox

ClickHouse projection is at-least-once. sandbox-rental must therefore use a
PostgreSQL transactional outbox for product usage projections. A marker column
such as `clickhouse_projected_at` is useful as cached status, but it is not the
durable outbox and must not be the only record that projection work is due.

Target delivery row shape:

- source kind: `volume_meter_tick`
- source id: `meter_tick_id`
- sink: `clickhouse.volume_meter_ticks`
- generation: monotonic integer used for intentional re-projection
- state: `pending`, `in_progress`, `retryable_failed`, or `dead_letter`
- attempts, lease fields, next attempt time, last error, and operator note

The delivery row is inserted in the same PostgreSQL transaction that makes the
tick projectable. A worker leases due rows, hydrates the current authoritative
PostgreSQL tick and volume state, inserts a deterministic ClickHouse row, and
marks delivery succeeded. If ClickHouse insert succeeds but the PostgreSQL
success mark fails, replay is allowed and expected.

`forge_metal.volume_meter_ticks` must be idempotent by `meter_tick_id`.
Partitioning must use a stable domain timestamp such as `window_start` or
`observed_at`, not `recorded_at`, so a retry across a month boundary cannot
place duplicate logical rows in different partitions. Customer and Grafana
queries read through a deduped view or group by `meter_tick_id` with latest
generation/recording semantics.

## ClickHouse Read Models

The product usage table is `forge_metal.volume_meter_ticks`. It stores raw
product evidence: measured bytes, billable live/retained bytes, billing state,
billing window ID, charge units when available, product dimensions, and trace
identity.

Billing truth for invoices and customer spend history comes from
billing-service settled windows and `forge_metal.metering`, not from raw product
usage alone. The two tables are intentionally complementary:

- `forge_metal.volume_meter_ticks`: product evidence and operational visibility.
- `forge_metal.metering`: settled billing read model derived from billing
  windows, pricing context, and ledger legs.

Grafana may read both tables. Customer usage APIs should use product evidence
for storage history and billing metering for settled spend, always filtered by
the caller's `org_id` inside sandbox-rental.

## Proof Status

The current proof implementation has `volumes`, `volume_generations`,
`volume_events`, and `volume_meter_ticks` tables plus the hidden meter tick
route. It proves GiB-ms reservation/settlement and ClickHouse projection, but it
does not yet implement the target projection outbox, authoritative
vm-orchestrator ZFS measurement path, generation API, usage API, or customer DTO
redaction of placement/storage internals.
