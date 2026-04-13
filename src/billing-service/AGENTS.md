# billing-service

Credit-based recurring contract billing with entitlements — prepaid + metered hybrid. Stripe + TigerBeetle + PostgreSQL tri-store. Includes `tb-inspect/` debugger.

Architecture detail: `docs/billing-architecture.md`.

## Key use cases

- Monthly contracts granting entitlements: credits, access to digital goods, software licenses, priority lanes.
- Credits consumed via metering events published by services (token inference, vCPU / RAM / Disk / Network usage, build minutes, etc).

## Dual-write pattern

Services producing data for both real-time UX and long-term analytics use **application-level dual write**: the service writes to PostgreSQL (live sync via ElectricSQL → TanStack DB in the browser) and to ClickHouse (dashboards, metering, historical queries) in the same request path. Consistency between the two stores is verified by periodic reconciliation — same shape as billing's 6-check `Reconcile()`.

ClickHouse's `MaterializedPostgreSQL` engine was evaluated as a CDC alternative and **rejected** — experimental, with replication-slot coupling risks on a single node.

The 3-node evolution should introduce NATS JetStream or Kafka + Debezium for proper WAL-based CDC, replacing application-level dual write with streaming.

## Migrations

Live in `postgresql-migrations/`. Platform provisions the database + role; the service's Ansible role applies migrations on deploy. During pre-customer phase, prefer `billing-reset.yml` (exhaustive TigerBeetle + PG wipe) over crafting tricky migrations.

## Reset

```bash
ansible-playbook playbooks/billing-reset.yml
```

Wipes TigerBeetle and billing PostgreSQL state and restarts billing callers.
