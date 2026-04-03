# RFC: Application Layer Tech Stack

Two revenue-generating applications on the forge-metal platform, sharing auth and billing infrastructure. Both deployed on the same bare-metal box alongside existing CI and observability services.

## Applications

**Sandbox** — agent execution environments billed per second of wall-clock compute. Thin product layer over existing Firecracker infrastructure. Users get API keys, launch sandboxed VMs, pay for vCPU-seconds and GiB-seconds.

**Storefront** — bare metal server resale. Inventory sourced from upstream providers (Latitude.sh initially), marked up, sold as managed boxes with provisioning automation. Standard ecommerce: catalog, cart, checkout, order lifecycle.

Both are Next.js applications. Both authenticate against the same identity provider. Both settle payments through Stripe.

## Component Decisions

### Identity: Zitadel

Single Go binary. Multi-tenant organizations are first-class (not bolted on like Keycloak 25+ Organizations). ~200 MB RAM vs Keycloak's 1.5-2 GB JVM footprint.

Each customer org maps to a Zitadel organization. Both applications use OIDC with Zitadel as the IdP. Org membership and roles are managed in Zitadel; application-specific authorization (e.g., sandbox quotas, storefront purchase history) stays in each app's database.

Deployment: Nix closure, systemd unit, Caddy route at `auth.<domain>`. PostgreSQL database `zitadel`.

### Database: PostgreSQL (single instance, multiple databases)

```
PostgreSQL
├── zitadel       # Zitadel IAM state
├── sandbox       # sandbox app (API keys, job metadata, org quotas, pricing config)
└── storefront    # storefront app (inventory, orders, provisioning state)
```

One PostgreSQL instance. Databases are isolated — no cross-database queries, no shared schemas. The tenant linkage between apps is the Zitadel organization ID stored as a column in each app's tables.

Application state lives here. Financial state does not — that's TigerBeetle. Pricing configuration (per-org rates, plan tier) lives in PostgreSQL; balance enforcement lives in TigerBeetle.

### Financial Ledger: TigerBeetle

Purpose-built double-entry accounting database. Single statically-linked Zig binary. Handles balance enforcement, idempotent transfers, and audit trail without application-level locking.

#### Why a separate ledger

PostgreSQL can do double-entry accounting with serializable transactions and CHECK constraints. The argument for TigerBeetle is that the invariants are enforced at the storage engine level, not the application level. A bug in Go code cannot create money, overdraw an account, or produce an unbalanced ledger — the database physically rejects it. For financial data, moving the correctness guarantee out of application code and into the storage layer eliminates an entire class of bugs.

The trade-off is operational: one more service. But TigerBeetle has no external dependencies (no PostgreSQL, no ZooKeeper, no configuration files), so the marginal operational cost is low.

#### Deployment model

TigerBeetle's deployment is two commands:

```bash
# One-time: create the data file
tigerbeetle format --cluster=0 --replica=0 --replica-count=1 /var/lib/tigerbeetle/data.tigerbeetle

# Run
tigerbeetle start --addresses=127.0.0.1:3000 /var/lib/tigerbeetle/data.tigerbeetle
```

The data file is a single pre-allocated file that grows elastically. Internally it's divided into 512 KiB blocks, all immutable and 128-bit checksummed. There are no configuration files — cluster topology is baked in at format time.

The systemd unit requires `AmbientCapabilities=CAP_IPC_LOCK` and `LimitMEMLOCK=infinity` because TigerBeetle locks its entire working set into physical memory (prevents the kernel from swapping pages to disk). Same pattern as ClickHouse's large-page setup.

Default port: 3000.

#### Single-node caveats

TigerBeetle is designed for 6-replica clusters across 3 geographic sites. A 1-replica deployment works but has specific implications:

- **No replication.** Durability depends on ZFS snapshots and `zfs send` backups of the data file. This is acceptable for the current single-node platform.
- **Replica count is immutable.** Changing from 1 to 3 replicas requires reformatting the data file and replaying state. Plan for this if multi-node is on the roadmap.
- **Upgrades are simple.** Replace the binary, restart via systemd (~5 seconds of downtime). Data files are forward-compatible — new binaries auto-migrate the format.
- **No online schema changes.** Account types and flags are set at creation time. Changing an account's flags requires creating a new account and migrating balances via transfers.

#### Resource requirements

TigerBeetle's binary is ~50 MB, but the runtime footprint is much larger. It locks its grid cache into physical memory and uses io_uring for disk I/O.

| Resource | Minimum | This deployment |
|----------|---------|-----------------|
| Memory | 6 GiB (docs) | 1-2 GiB (tuned via `--cache-grid-size` for single-replica, low-volume) |
| CPU | 1 core dedicated | Shared, low contention at current scale |
| Disk | NVMe local (required for production) | NVMe already present on Latitude.sh box |
| Filesystem | ext4 or XFS | ext4 (ZFS zvol backing is fine — TigerBeetle sees a block device) |

Budget 1-2 GiB, not 50 MB.

#### Account model

Every billing concept maps to TigerBeetle accounts and transfers. No special-cased application logic for discounts, credits, or free tier — they're all transfers between accounts.

```
Per org:
├── {org}/free-tier       # monthly allowance, reset on the 1st
├── {org}/credit          # prepaid balance (Stripe purchases + subscription deposits)

Operator:
├── operator/revenue         # realized revenue from posted usage
├── operator/free-tier-pool  # funds monthly free tier grants
├── operator/stripe-holding  # Stripe payments land here before crediting org
└── operator/promo-pool      # funds promotional credits
```

Three ways for an org to have balance: free tier (automatic monthly), prepaid credits (Stripe ACH/card), and subscription deposits (recurring Stripe). All three fund the same draw-down chain. From TigerBeetle's perspective, every VM launch is the same operation: "does this org have enough balance to run?"

#### Account flags

TigerBeetle accounts have flags that constrain what transfers are permitted. These are set at account creation and cannot be changed.

| Account | Flag | Effect |
|---------|------|--------|
| `{org}/free-tier` | `debits_must_not_exceed_credits` | Monthly allowance — usage reservations fail when allowance exhausted. |
| `{org}/credit` | `debits_must_not_exceed_credits` | Prepaid balance — cannot go negative. Funds come from Stripe purchases or subscription deposits. |
| `operator/revenue` | `credits_must_not_exceed_debits` | Revenue only flows in. Prevents accidental reversal without an explicit refund transfer. |
| `operator/free-tier-pool` | `debits_must_not_exceed_credits` | Pool has a finite budget. Free tier grants debit this account — fails if pool exhausted. |
| `operator/stripe-holding` | (none) | Contra account. Intentionally unflagged — allows negative balances. Stripe payments credit it; org deposits debit it. A negative balance represents settled Stripe funds that have been deposited into org accounts (correct double-entry: the debit to stripe-holding and credit to {org}/credit are balanced). |
| `operator/promo-pool` | `debits_must_not_exceed_credits` | Promotional credits draw from a funded pool. |

#### Transfer flags

| Flag | When used |
|------|-----------|
| `linked` | Chain multiple transfers to succeed or fail atomically. Used when a VM reservation should drain free-tier first, then fall back to credits. |
| `balancing_debit` | Auto-clamp the debit amount to available balance. Used for the free-tier leg of a reservation: "debit up to X, but don't exceed what's left." |
| `balancing_credit` | Auto-clamp the credit amount to the account's limit. Used for free-tier monthly reset: "credit up to the monthly cap, don't stack unused balance." |
| `pending` | Reserve funds at VM launch. Moves amount into `debits_pending` / `credits_pending` — reserved but not spent. |
| `post_pending_transfer` | Settle a reservation at VM exit. Can specify a lower amount than the original pending — the difference is released. |
| `void_pending_transfer` | Cancel a reservation entirely. Used when a VM fails to boot or is cancelled before execution. |

#### Two-phase billing (VM lifecycle)

The core billing mechanism is two-phase transfers tied to the VM lifecycle, not periodic aggregation. This gives sub-minute lockout for exhausted accounts without per-second TigerBeetle traffic.

```
1. User requests VM launch (vcpus=2, mem_mib=512)
2. Orchestrator estimates cost for RESERVATION_WINDOW (e.g., 300 seconds):
     reservation = (vcpus × vcpu_rate + mem_gib × mem_rate) × 300
3. Orchestrator reserves funds (two round trips, not one):
     a. Post PENDING transfer: debit {org}/free-tier, credit operator/revenue
        (balancing_debit — clamps to available free-tier balance)
     b. Read back transfer 3a's result to learn the clamped amount
     c. Compute remainder = reservation - clamped_amount
     d. If remainder > 0: post PENDING transfer: debit {org}/credit,
        credit operator/revenue, amount = remainder
     e. If 3d fails (insufficient credit balance): void 3a, reject launch
     → If both succeed: funds reserved across two pending transfers, VM boots
4. VM runs
5. Every RESERVATION_WINDOW (300s) while VM is alive:
     a. POST current pending transfers (both 3a and 3d) for actual seconds used,
        splitting proportionally between the free-tier and credit portions
     b. Create NEW pending transfers for the next window (same two-step flow)
     c. If new reservation fails → signal VM to shut down (out of funds)
6. VM exits:
     a. POST final pending transfers for actual seconds consumed
     b. Excess reservation released automatically (partial post)
```

This is a two-round-trip operation per reservation: `balancing_debit` clamps the free-tier transfer to available balance, but TigerBeetle does not auto-route the remainder to another account. The orchestrator must read back the clamped amount from the first transfer's result before computing the second transfer's amount. The two pending transfers are independent (not linked) so they can be individually posted/voided during settlement.

The reservation window is tunable: 60 seconds for aggressive enforcement, 300 seconds for less TigerBeetle traffic. A user who exhausts their balance is locked out within one reservation window.

#### Idempotency

Every transfer has a 128-bit ID. Posting the same ID twice is a no-op — TigerBeetle returns success without double-processing. Transfer IDs are derived deterministically from `(org_id, job_id, window_sequence)`, making retries after orchestrator crashes inherently safe.

#### Go client

```go
import tb "github.com/tigerbeetle/tigerbeetle-go"

client, err := tb.NewClient(tb.ToUint128(0), []string{"127.0.0.1:3000"})
defer client.Close()
```

Thread-safe, single instance shared across goroutines. The client batches operations automatically. Requires Go >= 1.21, Linux >= 5.6 in production.

#### Security model

TigerBeetle has no authentication or authorization. No passwords, no TLS, no per-client permissions. Any process that can open a TCP connection to port 3000 has full read-write access. This is a deliberate design choice — TigerBeetle is meant to sit behind your application layer.

For this deployment: TigerBeetle listens on `127.0.0.1:3000`. Only local processes (the VM orchestrator, the reconciliation cron, the Stripe webhook worker) can connect. Network binding is the access control. If multi-service access with different permission levels is needed later, the documented pattern is a gateway service that authenticates callers and proxies permitted operations.

#### Observability

TigerBeetle does not emit OpenTelemetry natively. It uses StatsD (DogStatsD format) via an experimental flag:

```
tigerbeetle start --experimental --statsd=127.0.0.1:8125 ...
```

This emits `tb.*` metrics: request latency by operation type, grid cache hits/misses, compaction timing. The OTel Collector's StatsD receiver on `:8125` bridges these into the existing ClickHouse metrics pipeline.

Host-level CPU/RAM/disk metrics come from the OTel Collector's `hostmetricsreceiver` (already deployed). TigerBeetle's memory usage is a flat line (static allocation, locked at startup). Disk I/O shows periodic sequential write bursts (10ms batching window). No north/south traffic — all clients are local.

#### Backup strategy

Single-replica means no replication-based durability. Two complementary backup mechanisms:

1. **ZFS snapshots** of the data file directory. Crash-consistent because TigerBeetle's data file is designed for crash recovery (checksummed, hash-chained, immutable blocks). Schedule via the existing ZFS snapshot automation.
2. **Logical dump** — a small Go program that queries all account balances via the client library and writes them to a JSON file. This provides a human-readable reconstruction path if the data file format changes across a major version upgrade. Run daily via systemd timer.

### Metering: ClickHouse (already deployed)

Raw usage events are already flowing from smelter into ClickHouse as wide events. Sandbox metering extends this — each VM execution produces a row with resource allocation, wall-clock duration, and billing metadata.

ClickHouse is the metering source of truth for audit and analytics. It retains full-resolution events permanently. TigerBeetle handles real-time balance enforcement via two-phase transfers; ClickHouse provides the long-term record for reconciliation, usage dashboards, and dispute resolution.

New table: `forge_metal.sandbox_metering`

```sql
CREATE TABLE forge_metal.sandbox_metering (
    org_id          LowCardinality(String)  CODEC(ZSTD(3)),
    job_id          UUID,
    started_at      DateTime64(6)           CODEC(DoubleDelta, ZSTD(3)),
    ended_at        DateTime64(6)           CODEC(DoubleDelta, ZSTD(3)),
    wall_clock_us   Int64                   CODEC(Delta(8), ZSTD(3)),
    billed_seconds  UInt32                  CODEC(Delta(4), ZSTD(3)),
    cpu_us          Int64                   CODEC(Delta(8), ZSTD(3)),
    mem_peak_kb     Int64                   CODEC(T64, ZSTD(3)),
    vcpus           UInt8                   CODEC(ZSTD(3)),
    mem_mib         UInt16                  CODEC(T64, ZSTD(3)),
    charge_units    UInt64                  CODEC(T64, ZSTD(3)),
    exit_reason     LowCardinality(String)  CODEC(ZSTD(3))
) ENGINE = MergeTree()
ORDER BY (org_id, started_at)
```

Billing is per-second with per-invocation rounding: `billed_seconds = ceil(wall_clock_us / 1_000_000)`. Charges are computed as `(vcpus × vcpu_rate + mem_gib × mem_rate) × billed_seconds`, matching the E2B/Modal model of separate vCPU and memory dimensions.

`charge_units` records the total ledger units charged via TigerBeetle for this job. `exit_reason` distinguishes normal completion, timeout, and balance exhaustion (`funds_exhausted`).

No `billed` / `billed_at` mutation columns. ClickHouse MergeTree is append-only — `ALTER TABLE UPDATE` triggers asynchronous mutations that rewrite entire data parts. The row is written once at VM exit with the final billing outcome already resolved by the orchestrator.

### Billing Architecture

Billing has two paths: a real-time path that enforces balance limits during VM execution, and a reconciliation path that syncs financial state to Stripe and detects discrepancies.

#### Real-time path (VM orchestrator)

The VM orchestrator owns the billing hot path. Every VM launch, renewal, and exit produces TigerBeetle transfers directly — no intermediate queue or aggregation step.

```
VM launch request
    ↓
Orchestrator: estimate cost for reservation window
    ↓
TigerBeetle: pending transfer (free-tier + credit, linked)
    ↓ success                    ↓ failure
VM boots                    Reject: insufficient balance
    ↓
Every RESERVATION_WINDOW:
    Post current + reserve next
    ↓ reservation fails
    Signal VM shutdown (funds_exhausted)
    ↓
VM exits
    ↓
Post final pending transfer (actual usage)
    ↓
Write metering row to ClickHouse (append-only, once)
```

Rating logic (vCPU rate, memory rate, per-second pricing) lives in the orchestrator. Pricing changes are code deploys — acceptable for a single-operator platform.

#### Reconciliation path (hourly cron)

A Go binary runs hourly via systemd timer. Its job is not to compute charges (the orchestrator already did that) but to verify consistency and sync to Stripe.

1. **Query ClickHouse** for all unreconciled metering rows using a high-water mark, not a sliding time window. The cron persists the last-reconciled `started_at` timestamp in PostgreSQL and queries forward from it.
   ```sql
   SELECT org_id, job_id, charge_units, billed_seconds, started_at
   FROM sandbox_metering
   WHERE started_at > :last_reconciled_at
     AND started_at <= now() - INTERVAL 5 MINUTE  -- grace period for late writes
   ORDER BY started_at
   ```
   After successful reconciliation, advance the watermark to the maximum `started_at` in the result set. This eliminates the gap where a VM spanning an hour boundary could be missed by a fixed `now() - INTERVAL 1 HOUR` window — every row is reconciled exactly once regardless of when it was written.

2. **Query TigerBeetle** for posted transfers in the same range (via `get_account_transfers` with timestamp filter).

3. **Compare.** Flag discrepancies — a metering row without a corresponding posted transfer, or vice versa. Alert on mismatch.

4. **Push usage records to Stripe.** Stripe Usage Records API with `action: 'set'` and the window timestamp. Idempotent — setting the same timestamp twice overwrites rather than accumulates.

5. **Log reconciliation result to ClickHouse** as a wide event for observability.

#### Crash safety

The real-time path is crash-safe because TigerBeetle transfers are idempotent by ID. If the orchestrator crashes mid-reservation-renewal:

| Failure point | Recovery |
|--------------|----------|
| After pending transfer, before VM boot | Pending transfer exists. Orchestrator restart voids it (VM never ran). |
| During VM execution, before renewal | Firecracker VM continues running unaware of orchestrator state. On restart, the orchestrator discovers orphaned VMs by scanning Firecracker process state, computes actual wall-clock usage from VM start time to now, and posts the pending transfer(s) for the actual amount. If the orchestrator stays down longer than the pending transfer timeout, TigerBeetle voids the pending transfers and the org's funds are released — the operator absorbs the cost of the unmetered compute. Pending transfer timeout must be set longer than the maximum expected orchestrator restart time. |
| After VM exit, before ClickHouse write | TigerBeetle has the financial record. Reconciliation cron detects missing metering row and alerts. ClickHouse row can be backfilled from TigerBeetle transfer data. |

The reconciliation cron is not on the critical path — if it fails, billing enforcement continues via the orchestrator. Stripe sync is delayed but catches up on the next successful run.

### Payments: Stripe (external)

Accepted external dependency. Three funding sources feed into TigerBeetle accounts:

#### Prepaid credits (one-time purchase)

Customer buys credits via Stripe Checkout (card or ACH). No subscription, no commitment.

```
Stripe Checkout session completes
  → payment_intent.succeeded webhook
  → PostgreSQL task row
  → Worker posts TigerBeetle transfer:
      debit:  operator/stripe-holding
      credit: {org}/credit
      amount: purchased_amount_in_ledger_units
      user_data_128: stripe_payment_intent_id
```

#### Monthly plan (subscription with included credits)

Customer subscribes to a plan ($X/month or $X×10/year). Each billing period, included credits are deposited into their account.

```
Stripe invoice.paid webhook
  → PostgreSQL task row
  → Worker posts TigerBeetle transfer:
      debit:  operator/stripe-holding
      credit: {org}/credit
      amount: plan_included_credits
      user_data_128: stripe_invoice_id
```

Annual billing is identical — Stripe handles the billing cadence, TigerBeetle just sees credit deposits. Overage beyond included credits draws from any existing prepaid balance. If both are exhausted, VMs stop launching.

#### Free tier (monthly reset, no Stripe)

Every org gets a monthly allowance of compute. No credit card required. Resets on the 1st via systemd timer.

```
Monthly cron (1st of month):
  For each org:
    Transfer {
      debit:  operator/free-tier-pool
      credit: {org}/free-tier
      amount: MONTHLY_ALLOWANCE
      flags:  balancing_credit  ← caps at allowance, doesn't stack
    }
```

`balancing_credit` prevents accumulation — if 200 of 1000 units remain, the reset brings the account to 1000, not 1200.

#### Storefront payments

Separate from sandbox billing. Standard Stripe Checkout sessions for server purchases, Stripe Billing for monthly recurring. Webhook-driven provisioning: `payment_intent.succeeded` → PostgreSQL task row → provisioning worker. No TigerBeetle involvement — storefront orders are simple purchase transactions, not metered usage.

#### Customer-facing usage

Usage visibility is a Next.js page querying ClickHouse directly, not Stripe's dashboard. Current balance is read from TigerBeetle. Stripe is the payment rail, not the usage display.

### Task Queue: PostgreSQL SKIP LOCKED

No message broker. Async work (provisioning after payment, webhook processing, Stripe sync retries) uses PostgreSQL as a task queue:

```sql
SELECT * FROM tasks
WHERE status = 'pending' AND scheduled_at <= now()
ORDER BY scheduled_at
FOR UPDATE SKIP LOCKED
LIMIT 1
```

This handles the concurrency patterns both apps need without adding infrastructure. When fan-out or multi-node coordination becomes necessary, NATS JetStream is the upgrade path — single Go binary, ~50 MB, built-in persistence. Kafka is categorically excluded for single-node deployments.

### Messaging: Deferred

No NATS, no Kafka, no Redis pub/sub. Current architecture has no fan-out pattern (multiple consumers of the same event). All async flows are point-to-point task execution. Revisit when adding a second node or when a genuine pub/sub need emerges.

## What Is Not In This Stack

| Excluded | Reason |
|----------|--------|
| Kafka | Designed for distributed clusters. ~1 GB+ RAM, ZooKeeper/KRaft overhead. No single-node justification. |
| NATS JetStream | Good technology, no current need. PostgreSQL SKIP LOCKED covers task queue patterns. First candidate when multi-node or fan-out arrives. |
| Lago | AGPL license (copyleft, incompatible with distribution). Overlaps with TigerBeetle on credit management. Ruby/Rails + Redis + Sidekiq adds ~1 GB RAM for billing UI we don't need. |
| OpenMeter | Requires Kafka. Overlaps with existing ClickHouse metering pipeline. |
| Keycloak | 1.5-2 GB JVM footprint. Multi-tenant orgs bolted on in v25, not native. Zitadel covers the same OIDC/SAML surface in ~200 MB. |
| Separate metering service | ClickHouse already ingests microsecond-resolution events from smelter. Adding a metering service creates a redundant event store. |

## Resource Budget

Estimated memory footprint of new components alongside existing services:

| Component | RAM | Status |
|-----------|-----|--------|
| Caddy | ~50 MB | existing |
| ClickHouse | ~1 GB | existing |
| HyperDX + MongoDB | ~1.5 GB | existing |
| OTel Collector | ~200 MB | existing |
| Forgejo | ~300 MB | existing |
| Firecracker VMs (per job) | ~256 MB each | existing |
| **Zitadel** | **~200 MB** | **new** |
| **PostgreSQL** | **~500 MB** | **new** |
| **TigerBeetle** | **~1-2 GB** | **new** |
| **Sandbox app (Next.js)** | **~200 MB** | **new** |
| **Storefront app (Next.js)** | **~200 MB** | **new** |

New components add ~2.1-3.1 GB. Total platform footprint ~6-7 GB, well within a 64 GB bare-metal box. The TigerBeetle figure reflects the actual runtime working set (grid cache locked into memory), not the binary size.

## Deployment Model

All new components follow the existing pattern: Nix closure for binaries, Ansible role for configuration, systemd for lifecycle, Caddy for TLS termination.

```
Nix closure additions:
├── zitadel          # single binary
├── postgresql       # server + client
├── tigerbeetle      # single binary
└── (Next.js apps built separately, deployed as Node processes)

Ansible roles (new):
├── postgresql/      # instance, databases, users, pg_hba
├── zitadel/         # config, systemd, initial org bootstrap
├── tigerbeetle/     # data file format (idempotent), systemd, CAP_IPC_LOCK
├── sandbox_app/     # Next.js process, env, systemd
└── storefront_app/  # Next.js process, env, systemd

Caddy routes (additions):
├── auth.<domain>    → Zitadel :8080
├── sandbox.<domain> → Sandbox app :3001
└── store.<domain>   → Storefront app :3002
```

## Implementation Order

1. **PostgreSQL role** — dependency for everything else
2. **Zitadel role** — auth must exist before apps can authenticate
3. **TigerBeetle role** — stand up in isolation with Ansible role, systemd unit, OTel Collector StatsD receiver
4. **TigerBeetle stress test** — Go harness that creates the account model, hammers transfers at increasing batch sizes, validates two-phase reservation flow, measures TPS ceiling on Latitude.sh hardware
5. **Storefront app** — simpler CRUD, proves auth + Stripe Checkout end-to-end
6. **Sandbox metering table** — extend ClickHouse schema
7. **Sandbox app + orchestrator billing** — two-phase transfers wired into VM lifecycle
8. **Reconciliation cron** — hourly ClickHouse ↔ TigerBeetle consistency check + Stripe usage sync