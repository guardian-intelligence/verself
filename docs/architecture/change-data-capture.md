# Change Data Capture

The platform adopts [PeerDB] as its Postgres→ClickHouse CDC plane. PeerDB
replaces the application-level dual-write pattern currently in use and
described in [`docs/system-context.md`](../system-context.md).

CDC is one of three async-infrastructure planes being added alongside
[durable execution](durable-execution.md) and the
[service event bus](event-bus.md). This document covers CDC only.

[PeerDB]: https://docs.peerdb.io/

## What PeerDB is

PeerDB is a purpose-built Postgres-to-data-warehouse replication engine.
It connects directly to a Postgres logical replication slot and streams
WAL changes to a destination — in our case, ClickHouse. It was
[acquired by ClickHouse Inc. in 2024][cha] and is the upstream of
ClickPipes, the managed CDC path in ClickHouse Cloud. Licensed AGPL-3.0.

The PeerDB stack itself consists of:

- **Flow API** — control plane (Go).
- **Flow Workers** — the replication workers, horizontally scalable (Go).
- **Nexus** — a Postgres-wire query layer (Rust) for ad-hoc source/sink
  querying. Optional; not on the CDC hot path.
- **Catalog** — a Postgres database for PeerDB's own configuration and
  mirror state.
- **Temporal** — PeerDB uses Temporal for workflow orchestration of
  mirror lifecycles (snapshot, initial sync, ongoing CDC). This is the
  hard dependency that makes durable execution the first brick.

[cha]: https://clickhouse.com/blog/clickhouse-welcomes-peerdb-adding-the-fastest-postgres-cdc-to-the-fastest-olap-database

## What PeerDB replaces

The dual-write pattern — services writing to PostgreSQL and ClickHouse in
the same request path — is retired per-service as PeerDB mirrors are
validated. The first cutover target is `billing-service → ClickHouse`:
it is the most instrumented dual-write path, and `Reconcile()` already
provides a correctness backstop that lets us compare CDC-derived rows to
the ground truth.

Reconciliation is **not** retired. `billing-service`'s `Reconcile()` and
analogous patterns remain as the integrity check; PeerDB is a faster,
cleaner transport, not a correctness guarantee. The same applies to any
other service that grows a reconciler.

## What PeerDB does not replace

- It is not an event bus. Domain events for other services to consume go
  through JetStream, not through a PeerDB mirror.
- It is not a Temporal replacement. PeerDB uses Temporal; it does not
  expose workflow primitives to callers.
- It is not a general ETL tool. Its scope is Postgres logical replication
  → ClickHouse. Other sink types exist upstream but are out of scope for
  the repo.

## Why not Debezium + Kafka

PeerDB's own positioning:
*"PeerDB removes the need to deploy and manage a complex, multi-component
CDC stack like Debezium and Kafka."* The operational delta is substantial:
a single Go process stack (Flow API + workers) reading the replication
slot and writing to ClickHouse, versus a Kafka cluster + Kafka Connect
workers + Debezium connectors + schema registry. For a repo whose
destination is already ClickHouse and whose source is already Postgres,
the direct path is the boring path.

## SPIFFE posture

PeerDB does not speak SPIFFE natively. It is wrapped by the
`spiffe-helper` pattern already in production use for ClickHouse.

Identity shape:

```
spiffe://<td>/svc/peerdb-flow-api
spiffe://<td>/svc/peerdb-flow-worker
spiffe://<td>/svc/peerdb-nexus   (only if Nexus is deployed)
```

Credential endpoints:

| Endpoint | Auth |
| --- | --- |
| PeerDB catalog Postgres | Local Unix socket peer auth via `pg_ident.conf` |
| Source Postgres (logical replication slot) | Local Unix socket peer auth; the replication role is granted `REPLICATION` on the target DB |
| ClickHouse sink | Existing SPIFFE-wrapped ClickHouse client path |
| Temporal (workflow orchestration) | SPIFFE X.509 client cert via `spiffe-helper` |

No shared bearer tokens. No static passwords in PeerDB configuration for
repo-owned databases. The only password that exists in PeerDB config is
for the source-side replication user *when* the source is outside the
peer-auth boundary (e.g. a future customer Postgres); that stays sealed
in OpenBao and fetched via JWT-SVID at process start.

## Observability

`make observe WHAT=peerdb` surfaces:

- Flow API and Flow Worker systemd state; per-role SVID TTL.
- Per-mirror replication slot lag (bytes and time).
- Rows replicated per mirror over time.
- ClickHouse sink write rate and per-table lag.
- Temporal workflow state for mirror lifecycles (snapshot, CDC, paused,
  failed).

Grafana receives one dashboard under
`src/platform/ansible/roles/grafana/dashboards/peerdb.json`.

## Proof artifact

PeerDB ships with a synthetic mirror between a seed Postgres table
(`peerdb_proof.source`) and a ClickHouse test table
(`peerdb_proof.sink`). The proof workflow inserts a row tagged with a
fresh trace ID and asserts via ClickHouse query that:

- The row appears in the sink within the target latency budget.
- Slot lag at quiesce is bounded.
- The mirror's Temporal workflow carries `spiffe.peer_id` on every
  internal hop.

The brick is not laid until the query returns green.

## Cutover order

1. `billing-service → ClickHouse` — the first dual-write removal.
   Reconcile validates CDC correctness against the existing path for at
   least one reconciliation cycle before the dual-write is deleted.
2. Additional dual-writers follow, one service at a time, each gated on
   its own reconciliation or synthetic validation.

No service retires its dual-write in a single commit. Every cutover is
three commits: add the PeerDB mirror, run both paths and compare, delete
the dual-write.

## Known unknowns

The implementing agent must answer these before the brick is considered
laid:

1. PeerDB's enterprise Helm chart is public, but bare-metal / systemd
   packaging will need to be derived. Whether the Go binaries from that
   chart can be run directly under systemd with `spiffe-helper` sidecars
   needs live validation.
2. Logical replication slot sizing on the single-node platform Postgres —
   both the max concurrent slots and the `wal_keep_size` required to
   tolerate a PeerDB worker being down for a realistic outage window.
3. Whether PeerDB's ClickHouse sink can be pointed at the existing
   SPIFFE-authenticated ClickHouse path without code changes.
4. Publication strategy — one publication for all mirrored tables, or
   one per service schema. Per-service gives tighter SPIRE selector
   granularity but costs more replication slots.

## Source notes

- Workload identity contract: [`workload-identity.md`](workload-identity.md).
- Related planes:
  [`durable-execution.md`](durable-execution.md),
  [`event-bus.md`](event-bus.md).
- System context on the dual-write pattern and its planned retirement:
  [`../system-context.md`](../system-context.md).
- Billing dual-write and reconciliation pattern:
  [`../../src/billing-service/docs/billing-architecture.md`](../../src/billing-service/docs/billing-architecture.md).
- PeerDB architecture: <https://docs.peerdb.io/architecture>.
- PeerDB Enterprise Helm charts: <https://github.com/PeerDB-io/peerdb-enterprise>.
- ClickHouse × PeerDB acquisition announcement:
  <https://clickhouse.com/blog/clickhouse-welcomes-peerdb-adding-the-fastest-postgres-cdc-to-the-fastest-olap-database>.
