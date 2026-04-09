# Billing Architecture

Usage-based billing with subscriptions, prepaid credits, and enterprise contracts. Three data stores, each authoritative for a different concern: TigerBeetle for financial state, PostgreSQL for commercial metadata, ClickHouse for usage metering.

Reference architectures: Metronome's rate card + contract override model for pricing structure; Stripe's immutable prices and integer-only financial arithmetic for correctness guarantees; TigerBeetle's two-phase transfers for crash-safe fund reservation.

## Data stores and ownership

| Store | Authoritative for | Access pattern |
|-------|-------------------|----------------|
| TigerBeetle | Account balances, transfer history, spend caps | Synchronous at Reserve/Settle/Void time. All financial mutations go through TB. |
| PostgreSQL | Products, plans, contracts, orgs, subscriptions, credit grants, invoices | Read at Reserve time for rate resolution. Write for commercial lifecycle events. |
| ClickHouse | Per-window usage metering (charge units, grant source breakdown, rate applied) | Write at Settle time. Read for invoice generation and reconciliation. |

Consistency between the three stores is verified by periodic reconciliation (six named checks, described below).

## PostgreSQL schema

### products

Defines what is metered. One row per billable capability.

| Column | Type | Purpose |
|--------|------|---------|
| product_id | TEXT PK | Stable identifier (e.g. `sandbox-standard`, `sandbox-premium-nvme`) |
| display_name | TEXT | Human-readable name for invoices |
| meter_unit | TEXT | Unit of measurement (e.g. `seconds`, `requests`) |
| billing_model | TEXT | `metered`, `licensed`, or `one_time` |

### plans

Tier definition and list pricing. Each plan references a product and defines the default rates for that tier. Plans serve as the rate card — the single source of truth for list pricing.

| Column | Type | Purpose |
|--------|------|---------|
| plan_id | TEXT PK | Stable identifier (e.g. `sandbox-free`, `sandbox-pro`) |
| product_id | TEXT FK | Which product this plan prices |
| display_name | TEXT | Human-readable tier name |
| included_credits | BIGINT | Monthly credit allowance deposited at period start |
| unit_rates | JSONB | Rate card: `{"vcpu": 100, "memory_gb": 50}` — atomic units per second per dimension |
| overage_unit_rates | JSONB | Rates applied when included credits are exhausted |
| quotas | JSONB | Concurrent limits, resource caps |
| is_default | BOOLEAN | One default plan per product (unique partial index) |
| tier | TEXT | `free`, `hobby`, `pro`, `enterprise` |

Rate changes follow the Stripe/Metronome immutable-price pattern: append a new plan version with an `effective_at` timestamp rather than mutating `unit_rates` in place. Historical metering rows reference the `plan_id` that was active at Reserve time, preserving the audit trail required by SOX/GAAP. Stripe chose immutable prices because their billing architecture treats pricing as a stream of events — mutation would produce temporal ambiguity across invoices, webhooks, and integrations.

### orgs

Organizations (tenants). Each org has a trust tier that governs fraud caps and admission control.

| Column | Type | Purpose |
|--------|------|---------|
| org_id | TEXT PK | Zitadel organization ID (uint64 as text) |
| display_name | TEXT | |
| stripe_customer_id | TEXT UNIQUE | For payment collection |
| billing_email | TEXT | |
| trust_tier | TEXT | `new`, `established`, `enterprise`, `platform` |

Trust tier policy:

| Tier | Concurrent VMs | Spend cap (units/period) | Behavior |
|------|---------------|--------------------------|----------|
| `new` | 2 | 500 | Default for new signups |
| `established` | 20 | 50,000 | Auto-promoted after 3 successful billing months with no disputes |
| `enterprise` | unlimited | unlimited | Manually assigned. Never auto-promoted or auto-demoted. |
| `platform` | unlimited | unlimited | The operator's own org. Reserve always succeeds financially. Usage settles into `AcctPlatformExpense` (not `AcctRevenue`) for cost accounting separation. |

Automated trust tier transitions: `new` → `established` requires 3+ `payment_succeeded` billing events and zero `dispute_opened` events. `established` → `new` on any dispute or subscription suspension. `enterprise` and `platform` are never modified by automation.

### subscriptions

Binds an org to a plan for a product. One active subscription per (org, product) pair, enforced by partial unique index.

| Column | Type | Purpose |
|--------|------|---------|
| subscription_id | BIGINT PK (IDENTITY) | |
| org_id | TEXT FK | |
| plan_id | TEXT FK | |
| product_id | TEXT FK | |
| cadence | `monthly` or `annual` | Billing cycle length |
| current_period_start | TIMESTAMPTZ | |
| current_period_end | TIMESTAMPTZ | |
| status | ENUM | `active`, `past_due`, `suspended`, `cancelled`, `trialing` |
| overage_cap_units | BIGINT | Per-subscription spend cap override (nullable) |

### contracts

Per-org commercial arrangement. Absorbs and replaces `org_pricing_overrides`. A contract is purely commercial metadata — committed spend, payment terms, temporal bounds. All pricing behavior lives in `contract_overrides` (below). An org can have multiple contracts over time (renewals, amendments). Contracts also serve as the vehicle for promotional pricing (a promotion is a time-bounded contract auto-assigned at signup or plan change).

| Column | Type | Purpose |
|--------|------|---------|
| contract_id | TEXT PK | |
| org_id | TEXT FK | |
| starting_at | TIMESTAMPTZ | Contract effective date |
| ending_at | TIMESTAMPTZ | Null = indefinite |
| committed_monthly | BIGINT | Minimum monthly spend in atomic units. Null = no commitment. |
| payment_terms_days | INT | 0 = due on receipt, 30 = net-30, etc. |
| status | TEXT | `active`, `expired`, `terminated` |

### contract_overrides

Dimensional pricing overrides within a contract. Collapses enterprise discounts, promotional pricing, and per-resource adjustments into one table. Each row targets a specific (product, dimension) pair with either a multiplier or a fixed rate, optionally time-bounded.

| Column | Type | Purpose |
|--------|------|---------|
| contract_id | TEXT FK | |
| product_id | TEXT | Which product this override applies to |
| dimension | TEXT | `all` = entire unit_rates map. Otherwise a specific dimension key (e.g. `vcpu`, `memory_gb`, `storage_gb`). |
| override_type | TEXT | `multiplier` or `overwrite` |
| multiplier | NUMERIC(6,4) | e.g. 0.7000 = 30% discount. Applied to the plan's list rate for this dimension. Tracks rate card changes automatically. |
| overwrite_rate | BIGINT | Fixed rate in atomic units per second. Ignores the plan's rate card for this dimension. |
| starting_at | TIMESTAMPTZ | Null = contract start. Enables mid-contract promotional windows. |
| ending_at | TIMESTAMPTZ | Null = contract end. Time-bounded overrides expire automatically. |
| PK | (contract_id, product_id, dimension) | |

Override resolution precedence (evaluated at Reserve time):
1. Specific dimension override (`dimension = 'vcpu'`) beats broad override (`dimension = 'all'`)
2. `overwrite` beats `multiplier` for the same scope
3. A `multiplier` on `dimension = 'all'` applies to every dimension that lacks a specific override
4. Time bounds are checked: only overrides where `starting_at <= now < ending_at` (or nulls) are active
5. If no override matches, the plan's list rate applies

### credit_grants

Prepaid balance accounts. Each grant maps 1:1 to a TigerBeetle account via the ULID half-swap (described below). Grants are consumed in waterfall order: earliest-expiring first, then ULID order within the same expiry.

| Column | Type | Purpose |
|--------|------|---------|
| grant_id | TEXT PK | Application-generated ULID. Decoupled from PG sequence state — survives database recreation. |
| org_id | TEXT FK | |
| product_id | TEXT FK | |
| amount | BIGINT | Initial balance in atomic units |
| source | TEXT | `free_tier`, `subscription`, `purchase`, `promo`, `refund`, `platform` |
| contract_id | TEXT | Links commit-funded grants to their contract (nullable) |
| expires_at | TIMESTAMPTZ | Null = never expires |
| closed_at | TIMESTAMPTZ | Set when grant is fully consumed or expired. Append-only state transition. |

Grant lifecycle:
1. **Deposit**: PG INSERT + TB CreateAccount + TB pending transfer + TB post. Two-phase commit with deterministic transfer IDs for idempotency across cron and webhook paths.
2. **Consume**: Reserve creates pending debits against grant accounts in waterfall order. Settle posts actual spend, voids remainder.
3. **Expire**: Sweep job finds `expires_at <= now AND closed_at IS NULL`. Balancing debit drains remaining balance. PG `closed_at` set.

### product_entitlements

Tier-gated product access. Controls which products each tier can use.

| Column | Type | Purpose |
|--------|------|---------|
| product_id | TEXT | |
| tier | TEXT | `free`, `hobby`, `pro`, `enterprise`, `platform` |
| PK | (product_id, tier) | |

At request time, the calling service checks: does the org's current plan tier include access to this product? Premium SKUs (high-CPU, NVMe, ECC) are gated to `enterprise` and `platform` tiers.

### invoices

Generated monthly from ClickHouse metering aggregation.

| Column | Type | Purpose |
|--------|------|---------|
| invoice_id | TEXT PK | |
| org_id | TEXT FK | |
| contract_id | TEXT | Nullable. Links enterprise invoices to contract terms. |
| billing_period_start | TIMESTAMPTZ | |
| billing_period_end | TIMESTAMPTZ | |
| subtotal | BIGINT | Sum of line items (atomic units) |
| committed_minimum | BIGINT | From contract. Nullable. |
| total_due | BIGINT | `max(subtotal, committed_minimum)` |
| due_date | TIMESTAMPTZ | Period end + `payment_terms_days` from contract (or 0 for self-serve) |
| status | TEXT | `draft`, `finalized`, `paid`, `void` |
| stripe_invoice_id | TEXT | Set after Stripe invoice creation |

### invoice_line_items

Detailed breakdown for each invoice. One line item per (product, pricing_phase) combination.

| Column | Type | Purpose |
|--------|------|---------|
| invoice_id | TEXT FK | |
| product_id | TEXT | |
| description | TEXT | e.g. "Sandbox Compute (Standard) — 47,800 min" |
| quantity | BIGINT | Total metered units (seconds, requests) |
| unit | TEXT | `seconds`, `requests`, `units` |
| rate | BIGINT | Effective rate in atomic units (after contract override) |
| amount | BIGINT | Charged amount |
| pricing_phase | TEXT | `free_tier`, `included`, `overage`, `committed_minimum_trueup` |

Invoice generation (monthly cron):
1. Aggregate ClickHouse metering rows for the billing period, grouped by `(product_id, pricing_phase)`
2. Create invoice + line items in PostgreSQL
3. For enterprise contracts: compare `subtotal` against `committed_monthly`. If usage < commitment, insert a `committed_minimum_trueup` line item for the difference and create a TigerBeetle transfer (`KindCommitmentTrueUp`) to record the revenue.
4. Create Stripe invoice via Invoice API for payment collection. Stripe is the payment processor, not the billing engine.

Free tier orgs: no invoice generated (usage covered by auto-deposited grants).
Self-serve orgs (hobby/pro): Stripe invoice charged on receipt.
Enterprise orgs: Stripe invoice with `due_date` set per contract payment terms.
Platform org: no invoice, no Stripe interaction. Usage metered for cost accounting only.

## TigerBeetle account structure

All financial state lives in TigerBeetle. Integer-only arithmetic — Stripe uses integers for the same reason: floating-point addition is not associative, and distributed aggregation of floats produces non-deterministic results across replicas. TigerBeetle uses uint128; the billing service bridges to uint64 for the current scale.

### Account types

| Type | Code | ID scheme | Purpose |
|------|------|-----------|---------|
| Grant | 9 | `GrantAccountID(grantULID)` via half-swap | Per-grant prepaid balance |
| Spend cap | 11 | `SpendCapAccountID(orgID, productID, periodStart)` via FNV-1a | Period-scoped overage limit |
| Revenue | 3 | `OperatorAccountID(3)` | Destination for settled metered + licensed charges |
| Free tier pool | 4 | `OperatorAccountID(4)` | Source for free tier deposits |
| Stripe holding | 5 | `OperatorAccountID(5)` | Stripe payment holding |
| Promo pool | 6 | `OperatorAccountID(6)` | Source for promotional grants |
| Free tier expense | 7 | `OperatorAccountID(7)` | Sink for expired free tier grants |
| Expired credits | 8 | `OperatorAccountID(8)` | Sink for expired paid grants |
| Quota sink | 12 | `OperatorAccountID(12)` | Sink for spend-cap probe debits |
| Platform expense | 13 | `OperatorAccountID(13)` | Sink for platform org usage (cost accounting, not revenue) |

### ULID → TigerBeetle half-swap

Grant account IDs are derived from the grant's ULID via a bijective mapping that places the ULID's 48-bit timestamp in TigerBeetle's high u64 for LSM tree locality:

```
ULID bytes [0:8]  (timestamp + random head, big-endian) → TB bytes [8:16] (high u64, little-endian)
ULID bytes [8:16] (random tail, big-endian)              → TB bytes [0:8]  (low u64, little-endian)
```

Operator accounts use low 16 bits only (type code), with the high u64 as zero. These never collide with grant accounts because any ULID generated after Unix epoch has a nonzero high u64 after the half-swap.

### Transfer types

| Kind | Code | Purpose |
|------|------|---------|
| Reservation | 1 | Pending debit from grant → phase sink |
| Settlement | 2 | Post actual spend from pending reservation |
| Void | 3 | Release unspent portion of reservation |
| Free tier reset | 4 | Deposit from FreeTierPool |
| Stripe deposit | 5 | Deposit from StripeHolding (purchase) |
| Subscription deposit | 6 | Deposit from StripeHolding (subscription) |
| Promo credit | 7 | Deposit from PromoPool |
| Dispute debit | 8 | Drain grant on chargeback |
| Credit expiry | 9 | Balancing debit to drain expired grant |
| Deposit confirm | 10 | Post pending deposit |
| Expiry confirm | 11 | Post pending expiry |
| Spend cap check | 13 | Linked probe+void at reserve time |
| Spend cap debit | 14 | Posted permanent spend-cap consumption |
| Commitment trueup | 15 | Posted revenue for commitment floor shortfall |

### Transfer ID schemes

Deterministic IDs enable safe retries across process crashes. All schemes pack a source identifier into the high u64 for TigerBeetle LSM locality, with discriminating fields in the low u64.

| Transfer type | High u64 | Low u64 packing |
|---------------|----------|-----------------|
| VM reservation/settlement/void | job_id | `[0:4]=seq, [4]=grantIdx, [5]=kind` |
| Subscription deposit | subscription_id | `[0:4]=year*12+month, [5]=kind` |
| Stripe/task deposit | task_id | `[5]=kind` |
| Dispute debit | task_id | `[4]=grantIdx, [5]=KindDisputeDebit` |
| Spend cap ops | job_id | `[0:4]=seq, [5]=kind, [6:8]=AcctSpendCapCode` |
| Credit expiry | — | Full ULID half-swap (bijective, one per grant) |

All pending transfers have a 3600-second timeout. After timeout, TigerBeetle auto-voids and returns funds to the source account.

## ClickHouse metering

One row per billing window (default 300 seconds) per job. Written synchronously at Settle time. The metering row is the complete record of what happened, at what rate, funded by which grant sources.

### Schema: `forge_metal.metering`

```sql
CREATE TABLE forge_metal.metering (
    org_id             LowCardinality(String),
    actor_id           String DEFAULT '',
    product_id         LowCardinality(String),
    source_type        LowCardinality(String),
    source_ref         String,
    window_seq         UInt32,
    started_at         DateTime64(6),
    ended_at           DateTime64(6),
    billed_seconds     UInt32,
    pricing_phase      LowCardinality(String),
    dimensions         Map(LowCardinality(String), Float64),
    charge_units       UInt64,
    free_tier_units    UInt64,
    subscription_units UInt64,
    purchase_units     UInt64,
    promo_units        UInt64,
    refund_units       UInt64,
    plan_id            LowCardinality(String) DEFAULT '',
    cost_per_sec       UInt64 DEFAULT 0,
    recorded_at        DateTime64(6) DEFAULT now64(6),
    trace_id           String DEFAULT ''
)
ENGINE = MergeTree()
ORDER BY (org_id, product_id, started_at, source_ref, window_seq)
```

`charge_units` is the total cost in atomic units for this window. The `*_units` columns break down which grant source funded each portion. `plan_id` and `cost_per_sec` record the rate context, making invoice generation a pure ClickHouse aggregation without joining to PostgreSQL.

### Async metering writer

The production metering path uses a buffered writer (4096-row buffer, 256-row batches, 250ms flush interval) to amortize ClickHouse insert overhead. Non-blocking enqueue — drops on buffer full with a log message. Reconciliation repairs any missing ClickHouse writes by comparing against TigerBeetle transfer history.

## Reserve / Settle / Void lifecycle

The core billing loop. All metered products follow this flow.

### Reserve

```
1. Verify org not suspended
2. Load active subscription plan (with contract override resolution)
3. Load default plan (fallback)
4. Load unexpired grant balances from PG, then lookup available amounts from TB
5. Select pricing phase: free_tier → included → overage (waterfall)
6. Check concurrent limit (min of trust tier policy, plan quota)
7. Compute cost_per_sec from allocation dimensions × unit rates
8. If overage + spend cap: ensure spend-cap account, add linked probe+void to batch
9. Create linked TigerBeetle pending transfers for each grant in waterfall order
10. Return Reservation with grant legs (transfer IDs + amounts)
```

The linked batch is atomic: if the spend-cap probe fails (`ExceedsCredits`), all grant reservations are voided. If any grant has insufficient balance, the entire batch fails. This is TigerBeetle's linked transfer semantics — the financial equivalent of a database transaction.

### Rate resolution cascade

For each dimension in the allocation (e.g. `vcpu`, `memory_gb`):

```
1. Load plan unit_rates for this dimension → base_rate
2. Load active contract_overrides for (product, dimension, now):
   a. Specific dimension override exists?
      - overwrite → use overwrite_rate
      - multiplier → base_rate × multiplier
   b. No specific override, but 'all' override exists?
      - overwrite → use overwrite_rate
      - multiplier → base_rate × multiplier
   c. No override → use base_rate
```

This is evaluated per-dimension. A single reservation can have different dimensions resolved by different overrides — e.g., `vcpu` at a promotional 90% discount while `memory_gb` uses the plan's list rate.

### Settle

```
1. Compute actual_cost = cost_per_sec × actual_seconds
2. For each grant leg: post actual spend, void remainder
3. If spend cap applied: post spend-cap debit
4. Write metering row to ClickHouse
```

### Void

Cancel all pending grant legs. Used when a job fails before producing billable usage. The customer is never charged for crashes — void is the default failure mode.

### Renew

For long-running workloads (>300 seconds): settle the current window, then reserve the next from the latest grant state. This prevents a single reservation from holding credits for the full job duration.

## Reconciliation

Six named consistency checks run periodically across all three stores. Each check has a severity (`alert` or `warn`) and produces a `billing_events` row on failure.

| Check | Severity | Validates |
|-------|----------|-----------|
| `grant_account_catalog_consistency` | alert | Every active PG grant has a TB account |
| `no_orphan_grant_accounts` | alert | Every TB grant account found has a PG catalog row |
| `expired_grants_swept` | warn | Every expired grant has a credit expiry transfer in TB |
| `licensed_charge_exactly_once` | alert | Each completed licensed charge task has exactly one Revenue transfer |
| `metering_vs_transfers` | alert | CH `charge_units` totals ≤ TB `debits_posted` per org (CH > TB = data loss) |
| `trust_tier_monotonicity` | warn | No org auto-promoted to enterprise |

Failure policy: fail immediately on infrastructure errors. Do not advance any watermark on failure. Reconciliation failures are loud — they produce alert-level billing events that surface in the OTel pipeline.

## Dispute handling

On `dispute_opened`:
1. Load all open grants for the org, sorted: disputed payment's grant first, then by `expires_at`
2. Balancing debit each grant (clamped to available) into StripeHolding up to the dispute amount
3. If total debited < dispute amount: suspend the org (prevent further usage)
4. Log `dispute_opened` billing event with shortfall details

Suspension sets `subscriptions.status = 'suspended'` for all active subscriptions. Future `Reserve` calls return `ErrOrgSuspended`.

## Stripe integration

Stripe is the payment collection layer, not the billing engine. The billing service computes all charges internally; Stripe collects payment.

| Operation | Stripe API |
|-----------|------------|
| Store payment method | Customer + SetupIntent |
| Collect self-serve payment | Invoice API (created at invoice generation time) |
| Collect enterprise payment | Invoice API with `due_date` from contract terms |
| Confirm payment | Webhook: `invoice.paid` |
| Handle disputes | Webhook: `charge.dispute.created` |

Stripe Subscriptions are not used for metered billing. This avoids the 20-item limit, proration complexity, and the coupling between usage reporting and subscription state that motivated Stripe's own meters API redesign.

## API surface

All endpoints use the Huma v2 framework with OpenAPI 3.1 spec generation.

### Enforcement (called by services before/after work)

| Endpoint | Purpose |
|----------|---------|
| `POST /internal/billing/v1/check-quotas` | Advisory preflight. Returns allowed/denied with violation details. |
| `POST /internal/billing/v1/reserve` | Reserve credits for a billing window. Blocks on insufficient balance or spend cap. |
| `POST /internal/billing/v1/settle` | Post actual spend, void remainder, write metering row. |
| `POST /internal/billing/v1/void` | Cancel reservation. Customer not charged. |

### Query

| Endpoint | Purpose |
|----------|---------|
| `GET /internal/billing/v1/orgs/{org_id}/balance` | Free tier + credit available/pending |
| `GET /internal/billing/v1/orgs/{org_id}/products/{product_id}/balance` | Per-product balance breakdown |
| `GET /internal/billing/v1/orgs/{org_id}/subscriptions` | Active subscriptions |
| `GET /internal/billing/v1/orgs/{org_id}/grants` | Credit grants (filterable by product, active status) |

### Operations (cron / admin)

| Endpoint | Purpose |
|----------|---------|
| `POST /internal/billing/v1/ops/deposit-credits` | Period credit deposits for active subscriptions |
| `POST /internal/billing/v1/ops/expire-credits` | Sweep expired grants |
| `POST /internal/billing/v1/ops/reconcile` | Run six-check consistency verification |
| `POST /internal/billing/v1/ops/trust-tier-evaluate` | Automated promotion/demotion |

## Case studies

These demonstrate the contract_overrides table handling diverse commercial arrangements without any special-purpose billing code.

### Enterprise: Acme Corp — multi-product committed spend with mixed overrides

Acme signs a 12-month contract for sandbox compute. They want a blanket 30% discount on standard sandboxes, but they've negotiated a fixed vCPU rate for premium NVMe sandboxes (they run GPU workloads and want price certainty regardless of list price changes). They commit to $2,000/month minimum spend with net-45 payment terms.

**contracts row:**

| contract_id | org_id | starting_at | ending_at | committed_monthly | payment_terms_days | status |
|---|---|---|---|---|---|---|
| `acme-2026` | `org_acme` | 2026-04-01 | 2027-04-01 | 2000000 | 45 | active |

**contract_overrides rows:**

| contract_id | product_id | dimension | override_type | multiplier | overwrite_rate | starting_at | ending_at |
|---|---|---|---|---|---|---|---|
| `acme-2026` | `sandbox-standard` | `all` | multiplier | 0.7000 | — | — | — |
| `acme-2026` | `sandbox-premium-nvme` | `vcpu` | overwrite | — | 45 | — | — |
| `acme-2026` | `sandbox-premium-nvme` | `memory_gb` | multiplier | 0.7000 | — | — | — |

**Rate resolution at Reserve time for a sandbox-premium-nvme job with allocation `{vcpu: 2.0, memory_gb: 8.0}`:**

- `vcpu`: specific dimension override exists → overwrite → rate = 45 (fixed, ignores plan list rate)
- `memory_gb`: specific dimension override exists → multiplier 0.7 → rate = plan.unit_rates.memory_gb × 0.7

If the operator later raises the list price of `memory_gb` on the sandbox-premium-nvme plan, Acme's memory rate adjusts automatically (multiplier tracks the rate card). The vCPU rate stays at 45 (overwrite is insulated).

**Invoice at month end (usage < commitment):**

```
Invoice #FM-2026-05-0042
Billing period: April 1–30, 2026
Account: Acme Corp | Contract: acme-2026

  Sandbox Standard (overage)
    62,400 sec × 70 atomic/sec (30% off list 100)          4,368,000 au

  Sandbox Premium NVMe (overage)
    vCPU: 12,200 sec × 45 atomic/sec (fixed)                 549,000 au
    memory_gb: 12,200 sec × 35 atomic/sec (30% off list 50)  427,000 au

  Usage subtotal                                            5,344,000 au
  Committed minimum                                         2,000,000 au
  ─────────────────────────────────────────────────────────────────────
  Total due                                                 5,344,000 au
  (Usage exceeds commitment — no true-up needed)

  Payment terms: Net-45
  Due date: June 14, 2026
```

A month where Acme uses less than their commitment:

```
  Usage subtotal                                            1,200,000 au
  Committed minimum                                         2,000,000 au
  Committed minimum true-up                                   800,000 au
  ─────────────────────────────────────────────────────────────────────
  Total due                                                 2,000,000 au
```

The true-up generates a `KindCommitmentTrueUp` TigerBeetle transfer for 800,000 au into `AcctRevenue`.

### Promotional: 90% off vCPU for first 30 days on Pro tier

The operator wants to incentivize Pro signups with a launch promotion: 90% off vCPU costs for the first 30 days, all products. Memory and storage at full rate. The operator creates a plan tier called `pro` and configures the promotion as a contract auto-assigned when a customer upgrades to Pro.

**On plan upgrade to Pro, the billing service creates:**

**contracts row:**

| contract_id | org_id | starting_at | ending_at | committed_monthly | payment_terms_days | status |
|---|---|---|---|---|---|---|
| `promo-vcpu90-{org_id}` | `{org_id}` | {signup_time} | {signup_time + 30d} | — | 0 | active |

**contract_overrides row:**

| contract_id | product_id | dimension | override_type | multiplier | overwrite_rate | starting_at | ending_at |
|---|---|---|---|---|---|---|---|
| `promo-vcpu90-{org_id}` | `sandbox-standard` | `vcpu` | multiplier | 0.1000 | — | — | — |
| `promo-vcpu90-{org_id}` | `sandbox-premium-nvme` | `vcpu` | multiplier | 0.1000 | — | — | — |

No `committed_monthly`, no `payment_terms_days` override. The contract exists purely to carry the override rows. The `ending_at` on the contract (signup + 30 days) causes the contract to expire naturally. After expiry, Reserve falls through to the plan's list rate with no special-case code.

**Rate resolution during the promotional period for a sandbox-standard job with allocation `{vcpu: 2.0, memory_gb: 4.0}`:**

- `vcpu`: override match → multiplier 0.1 → rate = plan.unit_rates.vcpu × 0.1 (90% off)
- `memory_gb`: no override → plan list rate

**Rate resolution after the promotional period (day 31):**

- Contract status = `expired`
- No active overrides found
- Both dimensions resolve to plan list rate

The promotion ends with zero cleanup. No cron job to remove discounts, no migration, no feature flags. The contract's temporal bounds do the work.

### Promotional: free tier with 500 included credits, then hard stop

The operator defines a `free` plan tier with 500 included credits per month and no overage path. This is the default experience for new signups.

No contract or overrides needed. The plan's `included_credits = 500` deposits a grant each billing period. When the grant is exhausted, `Reserve` returns `ErrInsufficientBalance`. The frontend shows "free tier limit reached — upgrade to continue."

This case study demonstrates that contracts and overrides are only needed for non-default pricing. The plan + grant waterfall handles the standard self-serve tiers without any contract machinery.

### Platform: operator dogfooding CI through sandbox-rental-service

The operator's own org has `trust_tier = 'platform'`. A single `SourcePlatform` credit grant with amount = 2^53 atomic units and `expires_at = NULL` is seeded at platform setup. No contract, no overrides, no Stripe customer.

At Reserve time: the platform grant is selected by the normal waterfall (it never expires, so it sorts after time-bounded grants — but since it's the only grant, it's always first). The reservation succeeds. At Settle time: the metering row is written to ClickHouse with `pricing_phase = 'free_tier'` and `plan_id` referencing the plan. Usage settles into `AcctPlatformExpense`.

At invoice generation time: the platform org is skipped (no Stripe customer, no invoice). The metering data remains queryable for cost accounting — the operator can see exactly how much compute their CI consumes in ClickHouse, valued at list rates.

If billing infrastructure is down (TigerBeetle unavailable), the Forgejo webhook adapter flips a runtime configuration flag to route CI directly to the vm-orchestrator gRPC API, bypassing sandbox-rental-service and billing entirely. When billing recovers, the flag is flipped back. The metering gap is detectable by the `metering_vs_transfers` reconciliation check.
