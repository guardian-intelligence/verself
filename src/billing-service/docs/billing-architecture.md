# Billing Architecture

Usage-based billing with subscriptions, prepaid credits, and enterprise contracts. Three data stores, each authoritative for a different concern: TigerBeetle for financial state, PostgreSQL for commercial metadata plus first-class billing windows, ClickHouse for raw usage evidence and invoice-grade metering projections.

Reference architectures: Metronome's rate card + contract override model for pricing structure; Stripe's immutable prices and integer-only financial arithmetic for correctness guarantees; TigerBeetle's two-phase transfers for crash-safe fund reservation.

## Design invariants

1. **Customer never overpays.** The customer is never charged for more than they consumed. Void is the default failure mode — crashes, infrastructure outages, and incomplete work always resolve in the customer's favor. The operator eats the compute cost.
2. **Tax is Stripe's problem.** All spend caps, committed minimums, adjustment calculations, and MFN comparisons operate on pre-tax amounts. The billing service computes `pretax_total`; Stripe computes tax, collects it, and handles remittance and filing. Tax amounts on internal invoices are read-back fields from Stripe — the billing service does not compute, reconcile, or ledger tax in TigerBeetle. Same rationale as delegating email delivery to Resend: regulatory compliance surfaces with constantly changing rules are not worth self-hosting.
3. **Downgrades are not punitive, but not advantageous.** Unused prepaid credits from paid subscriptions (`source = 'subscription'` or `source = 'purchase'`) are carried forward as a refund grant on plan change. Free-tier grants (`source = 'free_tier'`) are forfeited. The carry-forward is capped at the prorated value of the remaining subscription term against the plan's bucketed entitlement map: `min(unused_credits, prorated_entitlement_value)`. This prevents gaming while ensuring light users aren't penalized for switching.
4. **Refund promises are capped, never a blank check.** Satisfaction guarantees refund the subscription fee, not unbounded overage consumption incurred during the guarantee window. SLA credits are capped at a configurable percentage of the affected period's charges. No refund path produces an unlimited liability for the operator.
5. **Financial state is append-only.** Corrections produce new transfers (reversals, adjustments), never mutations of existing transfers. TigerBeetle accounts and transfers are immutable by design. PostgreSQL invoices transition through `draft → finalized → paid → void` but never backward.

## Data stores and ownership

| Store | Authoritative for | Access pattern |
|-------|-------------------|----------------|
| TigerBeetle | Account balances, transfer history, spend caps | Synchronous at Reserve/Settle/Void time. All financial mutations go through TB. |
| PostgreSQL | Products, plans, contracts, orgs, subscriptions, credit grants, adjustments, invoices, billing windows, projection state | Read at Reserve time for rate resolution. Write for commercial lifecycle events and metered entitlement state transitions. |
| ClickHouse | Raw usage evidence and invoice-grade metering projections | Append product-specific usage events directly when useful; project settled billing windows asynchronously for invoice generation, dashboards, and reconciliation. |

Consistency between the three stores is verified by periodic reconciliation (eight named checks, described below). Billing truth is decided by TigerBeetle + PostgreSQL window state; ClickHouse is a derived read model that can be retried or rebuilt if projection falls behind.

## PostgreSQL schema

### products

Defines what is metered. One row per billable capability.

| Column | Type | Purpose |
|--------|------|---------|
| product_id | TEXT PK | Stable identifier (e.g. `sandbox-standard`, `sandbox-premium-nvme`) |
| display_name | TEXT | Human-readable name for invoices |
| meter_unit | TEXT | Unit of measurement (e.g. `seconds`, `requests`) |
| billing_model | TEXT | `metered`, `licensed`, or `one_time` |
| reserve_policy | JSONB | Metered products only. Defines the entitlement slice shape: time window vs unit tranche, target size, minimum size, partial-reserve behavior, and bounded operator-paid grace. |

The reserve policy is part of the product definition rather than the caller. This keeps liability policy close to the SKU being sold. A sandbox SKU may reserve 300-second slices with a 30-second minimum; an inference SKU may reserve 25,000-au token tranches; a network egress SKU may reserve byte tranches. The billing primitives stay the same.

### plans

Tier definition and list pricing. Each plan references a product and defines the default rates for that tier. Plans serve as the rate card — the single source of truth for list pricing.

| Column | Type | Purpose |
|--------|------|---------|
| plan_id | TEXT PK | Stable identifier (e.g. `sandbox-free`, `sandbox-pro`) |
| product_id | TEXT FK | Which product this plan prices |
| display_name | TEXT | Human-readable tier name |
| billing_mode | TEXT | `prepaid` or `postpaid`. Determines the Reserve funding path (see below). |
| included_credit_buckets | JSONB | Prepaid only. Monthly entitlement map from `bucket_id` to ledger units deposited as grants at period start. Empty for postpaid plans. |
| unit_rates | JSONB | Rate card: `{"vcpu": 100, "memory_gb": 50}` — atomic units per second per dimension. For prepaid, applies to the included allocation. For postpaid, applies to all usage from the first unit. |
| rate_buckets | JSONB | Dimension-to-bucket routing map. Each usage dimension/component drains a named credit bucket, allowing one plan to fund multiple entitlement lanes. |
| overage_unit_rates | JSONB | Prepaid only. Rates applied when the bucketed entitlements are exhausted. Empty object = hard stop (usage blocked until next period or upgrade). Non-empty object = overage pricing subject to spend cap. Ignored for postpaid plans. |
| quotas | JSONB | Concurrent limits, resource caps |
| is_default | BOOLEAN | One default plan per product (unique partial index) |
| tier | TEXT | `free`, `hobby`, `pro`, `enterprise` |

**Billing mode semantics:**

| Mode | Funding | On exhaustion | Invoice covers |
|------|---------|---------------|----------------|
| `prepaid` | Grants deposited from `included_credit_buckets` at period start, one grant per bucket | Hard stop (`overage_unit_rates = {}`) or overage pricing (spend-capped) | Overage charges only — prepaid portion already paid via subscription fee |
| `postpaid` | TigerBeetle receivable account (no balance constraint) | N/A — never exhausts | Total usage at `unit_rates` (after contract override resolution) |

Prepaid maps to self-serve tiers: the recurring subscription fee purchases bucketed credit entitlements, usage draws them down by bucket, and exhaustion is either a hard stop or a spend-capped overage. Postpaid maps to enterprise and platform tiers: usage accrues unbounded against a receivable (an Asset account in double-entry terms — "money owed to you"), and the invoice at period end is the collection event.

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
| `platform` | unlimited | unlimited | The operator's own org. Postpaid plan. Reserve always succeeds (receivable account, no balance constraint). Usage settles into `AcctRevenue` like any customer. Cost accounting separation via platform showback adjustment at invoice time. |

Automated trust tier transitions: `new` → `established` requires 3+ `payment_succeeded` billing events and zero `dispute_opened` events. `established` → `new` on any dispute or subscription suspension. `enterprise` and `platform` are never modified by automation.

### subscriptions

Binds an org to a plan for a product. One active subscription per (org, product) pair, enforced by partial unique index.

**Billing period:** anniversary-based. Each subscription's billing period is a 30-day rolling window anchored to the subscription's creation date (or the most recent plan change). On signup, the customer receives their full bucketed entitlement allocation immediately — no prorated first period. Period-scoped resources (grants, receivables, spend caps) are keyed to `current_period_start`.

The invoice cron runs daily and processes subscriptions where `current_period_end <= now`, rather than running for all customers on a fixed calendar date. This spreads invoice generation load evenly across days.

> **Future:** calendar-month billing will be supported as an option for enterprise customers who prefer it (contract-negotiated). Anniversary billing remains the default. The subscription's `current_period_start` / `current_period_end` already accommodates both models — the difference is only in how the next period's bounds are computed at rollover.

Subscriptions are modeled locally in PostgreSQL and are created or updated by Stripe Checkout and Stripe webhooks. Stripe collects the recurring plan payment; Stripe Customer Portal manages payment methods, invoices, and cancellation; the billing service records the local subscription row and mints the period grants. TigerBeetle remains the balance and transfer ledger for the credit grants those subscriptions fund.

| Column | Type | Purpose |
|--------|------|---------|
| subscription_id | BIGINT PK (IDENTITY) | |
| org_id | TEXT FK | |
| plan_id | TEXT FK | |
| product_id | TEXT FK | |
| cadence | `monthly` or `annual` | Billing cycle length. `monthly` = 30-day periods from subscription start. `annual` = subscription fee charged yearly, credit deposits and invoicing still on 30-day periods. |
| current_period_start | TIMESTAMPTZ | Anchor for all period-scoped resources (grants, receivables, spend caps) |
| current_period_end | TIMESTAMPTZ | `current_period_start + 30 days`. Invoice cron processes subscriptions where this is in the past. |
| status | ENUM | `active`, `past_due`, `suspended`, `cancelled`, `trialing` |
| overage_cap_units | BIGINT | Per-subscription spend cap override (nullable) |
| prorated_from_plan_id | TEXT | Nullable. Set when this subscription was created via a mid-period plan change. References the previous plan for audit trail. |

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
| refund_window_days | INT | Nullable. Satisfaction guarantee: number of days from `starting_at` during which the customer can request a full subscription fee refund. Null = no guarantee. |
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

Prepaid balance accounts. Used only by prepaid plans — postpaid plans use receivable accounts instead (see TigerBeetle account structure). Each grant maps 1:1 to a TigerBeetle account via the ULID half-swap (described below). Grants are consumed in waterfall order: earliest-expiring first, then ULID order within the same expiry.

| Column | Type | Purpose |
|--------|------|---------|
| grant_id | TEXT PK | Application-generated ULID. Decoupled from PG sequence state — survives database recreation. |
| org_id | TEXT FK | |
| product_id | TEXT FK | |
| amount | BIGINT | Initial balance in atomic units |
| source | TEXT | `free_tier`, `subscription`, `purchase`, `promo`, `refund` |
| contract_id | TEXT | Links commit-funded grants to their contract (nullable) |
| expires_at | TIMESTAMPTZ | Null = never expires |
| closed_at | TIMESTAMPTZ | Set when grant is fully consumed or expired. Append-only state transition. |

Grant lifecycle:
1. **Deposit**: PG INSERT + TB CreateAccount + TB pending transfer + TB post. Two-phase commit with deterministic transfer IDs for idempotency across cron and webhook paths.
2. **Consume**: Reserve creates pending debits against grant accounts in waterfall order. Settle posts actual spend, voids remainder.
3. **Expire**: Sweep job finds `expires_at <= now AND closed_at IS NULL`. Balancing debit drains remaining balance. PG `closed_at` set.

### billing_windows

Authoritative operational state for metered entitlement slices. Each row is one bounded billing window with immutable rate context and funding context. Callers keep only references to these rows; they do not own the financial truth themselves.

| Column | Type | Purpose |
|--------|------|---------|
| window_id | TEXT PK | Stable identifier for exactly-once settlement and projection |
| org_id | TEXT FK | |
| actor_id | TEXT | Principal that opened the window |
| product_id | TEXT FK | |
| source_type | TEXT | Product-specific source class (`job`, `inference_stream`, `request_batch`, `network_flow`) |
| source_ref | TEXT | Product-specific source identifier |
| window_seq | BIGINT | Monotonic sequence within a source |
| state | TEXT | `reserving`, `reserved`, `settled`, `voided`, `denied`, `failed` |
| reservation_shape | TEXT | `time` or `units` |
| reserved_quantity | BIGINT | Entitled quantity for this slice (seconds, tokens, requests, bytes, etc.) |
| actual_quantity | BIGINT | Actual consumed quantity once settled |
| billable_quantity | BIGINT | Quantity charged to the customer |
| writeoff_quantity | BIGINT | Quantity intentionally eaten by the operator |
| reserved_charge_units | BIGINT | Maximum spend reserved for this slice |
| billed_charge_units | BIGINT | Final customer charge in atomic units |
| writeoff_charge_units | BIGINT | Operator-paid spend in atomic units |
| pricing_phase | TEXT | `free_tier`, `included`, `overage`, `metered`, etc. |
| rate_context | JSONB | Resolved plan/contract rate snapshot used for settlement and invoice explainability |
| usage_summary | JSONB | Product-specific usage aggregate for the finalized window |
| expires_at | TIMESTAMPTZ | Pending transfer expiry / reservation validity bound |
| renew_by | TIMESTAMPTZ | When the caller should acquire the next slice. Nullable for discrete windows. |
| settled_at | TIMESTAMPTZ | |
| metering_projected_at | TIMESTAMPTZ | Null until ClickHouse projection succeeds |
| last_projection_error | TEXT | Last projection failure detail (nullable) |

This table is the boundary between request-path financial correctness and downstream analytics. A settled row is already billable truth even if ClickHouse is temporarily unavailable.

### product_entitlements

Tier-gated product access. Controls which products each tier can use.

| Column | Type | Purpose |
|--------|------|---------|
| product_id | TEXT | |
| tier | TEXT | `free`, `hobby`, `pro`, `enterprise`, `platform` |
| PK | (product_id, tier) | |

At request time, the calling service checks: does the org's current plan tier include access to this product? Premium SKUs (high-CPU, NVMe, ECC) are gated to `enterprise` and `platform` tiers.

### invoices

Generated monthly from projected ClickHouse metering aggregation.

| Column | Type | Purpose |
|--------|------|---------|
| invoice_id | TEXT PK | |
| org_id | TEXT FK | |
| contract_id | TEXT | Nullable. Links enterprise invoices to contract terms. |
| billing_period_start | TIMESTAMPTZ | |
| billing_period_end | TIMESTAMPTZ | |
| subtotal | BIGINT | Sum of usage + trueup line items (atomic units) |
| adjustments_total | BIGINT | Sum of adjustment line items (negative for credits/discounts, positive for surcharges). Default 0. |
| committed_minimum | BIGINT | From contract. Nullable. |
| pretax_total | BIGINT | `max(subtotal, committed_minimum) + adjustments_total` (floored at 0). All spend caps and adjustment calculations operate on this pre-tax value (design invariant 2). |
| currency | TEXT | ISO 4217 code (e.g. `USD`). Matches platform billing currency. Present on every invoice for forward-compatible multi-currency support. |
| tax_total | BIGINT | Read-back from Stripe Tax at finalization time. The billing service does not compute this value. 0 for zero-due invoices (platform showback, fully credited). |
| tax_jurisdiction | TEXT | Read-back from Stripe Tax (e.g. `US-NY`). Nullable. Null when `tax_total = 0`. |
| total_due | BIGINT | `pretax_total + tax_total` |
| due_date | TIMESTAMPTZ | Period end + `payment_terms_days` from contract (or 0 for self-serve) |
| status | TEXT | `draft`, `finalized`, `paid`, `void` |
| stripe_invoice_id | TEXT | Set after Stripe invoice creation. Null when `total_due = 0`. |

### invoice_line_items

Detailed breakdown for each invoice. Usage line items have positive amounts; adjustment line items have negative amounts. One usage line item per (product, pricing_phase) combination. Adjustment line items reference the `adjustments` row that generated them.

| Column | Type | Purpose |
|--------|------|---------|
| invoice_id | TEXT FK | |
| product_id | TEXT | |
| description | TEXT | e.g. "Sandbox Compute (Standard) — 47,800 min" or "Platform showback (100%)" |
| quantity | BIGINT | Total metered units for usage lines. Null for adjustment lines. |
| unit | TEXT | `seconds`, `requests`, `units`. Null for adjustment lines. |
| rate | BIGINT | Effective rate in atomic units (after contract override). Null for adjustment lines. |
| amount | BIGINT | Positive for charges, negative for adjustments |
| pricing_phase | TEXT | Usage: `free_tier`, `included`, `overage` (prepaid), `metered` (postpaid), `committed_minimum_trueup`. Adjustment: the `adjustment_type` value from the triggering adjustment. |
| adjustment_id | TEXT | FK to `adjustments`. Null for usage line items. Traces the line item back to the rule that generated it. |

### adjustments

General-purpose invoice adjustment rules. Adjustments modify invoice totals at generation time — distinct from contract_overrides, which modify per-unit rates at Reserve time. A single table with scope fields determines which invoices each rule applies to.

| Column | Type | Purpose |
|--------|------|---------|
| adjustment_id | TEXT PK | ULID |
| adjustment_type | TEXT | `percentage_discount`, `fixed_credit`, `platform_showback`, `sla_credit`, `volume_rebate` |
| description | TEXT | Human-readable, appears on invoice line item |
| contract_id | TEXT | Nullable. Non-null = scoped to a contract. |
| org_id | TEXT | Nullable. Non-null = scoped to an org (manual one-offs). |
| invoice_id | TEXT | Nullable. Non-null = pinned to a specific invoice (retroactive corrections). |
| tier | TEXT | Nullable. Non-null = applies to all orgs on this plan tier. |
| product_id | TEXT | Nullable. Null = applies to entire invoice subtotal. Non-null = applies only to that product's charges. |
| percentage | NUMERIC(6,4) | Nullable. e.g. `-0.1500` = 15% credit, `-1.0000` = 100% showback. Mutually exclusive with `fixed_amount` (CHECK constraint). |
| fixed_amount | BIGINT | Nullable. Fixed credit in atomic units. Mutually exclusive with `percentage`. |
| starting_at | TIMESTAMPTZ | Null = immediate / contract start |
| ending_at | TIMESTAMPTZ | Null = indefinite / contract end |

Scope resolution: an adjustment matches an invoice when the invoice's org, contract, tier, and billing period overlap the adjustment's scope fields and temporal bounds. Null scope fields are wildcards — a `contract_id = NULL, tier = NULL` adjustment with `percentage = -0.5` is a global 50% discount for all orgs.

**Adjustment precedence (MFN — Most Favored Nation):**

When multiple adjustments match the same invoice (e.g., a contract-specific 30% discount and a global 50% promotion):

1. Adjustments from the **same contract** stack (they're part of a single negotiated deal)
2. Adjustments from **different sources** (contract vs. global vs. manual) compete — the customer gets the better deal
3. Within competing adjustments of the same type: the larger discount (more negative) wins
4. `fixed_amount` and `percentage` adjustments of different types always stack (a $500 fixed credit and a 10% discount both apply)

This is a system-level invariant: an enterprise customer is never worse off for having a contract than they would be on the public pricing with active promotions. The contract's value is additive to public offers, never restrictive.

### refunds

Reversal of a payment already collected. Distinct from credits (forward-looking prepaid balance) and adjustments (invoice modifications before or at generation time). A refund moves cash backward — the customer paid, and you return some or all of that payment.

| Column | Type | Purpose |
|--------|------|---------|
| refund_id | TEXT PK | ULID |
| org_id | TEXT FK | |
| invoice_id | TEXT FK | Which invoice's payment is being reversed |
| refund_type | TEXT | `monetary` (cash back via Stripe) or `account_credit` (new prepaid grant) |
| amount | BIGINT | Pre-tax refund amount in atomic units. Capped — never exceeds the referenced invoice's `pretax_total` (design invariant 4). Stripe computes and reverses the proportional tax automatically. |
| reason | TEXT | `satisfaction_guarantee`, `sla_breach`, `billing_error`, `goodwill` |
| grant_id | TEXT | FK to `credit_grants`. Non-null when `refund_type = 'account_credit'`. The created grant has `source = 'refund'`. |
| stripe_refund_id | TEXT | Non-null when `refund_type = 'monetary'`. Set after Stripe Refund API call succeeds. |
| status | TEXT | `pending`, `completed`, `failed` |
| created_at | TIMESTAMPTZ | |

**Refund type semantics:**

| Type | TigerBeetle flow | Effect |
|------|-------------------|--------|
| `monetary` | DR AcctRevenue → CR AcctRefundPayable (linked), then DR AcctRefundPayable → CR AcctStripeHolding on Stripe confirmation | Cash returned to customer's payment method. Revenue reversed. TB transfers are for the pre-tax amount only. Stripe handles the tax portion of the refund automatically — the billing service does not track tax reversal in TB. |
| `account_credit` | DR AcctRevenue → CR new Grant account (`source = 'refund'`) | Customer receives prepaid balance for future usage. Revenue reversed, new liability created. For corrections on already-paid invoices, a Stripe Credit Note is also created (Stripe handles the tax adjustment). |

**Satisfaction guarantee refunds:**

When a customer invokes a satisfaction guarantee (within `contract.refund_window_days` of `contract.starting_at`):

1. Verify eligibility: `now < contract.starting_at + refund_window_days`
2. Refund amount = the subscription fee for the current period (the fixed payment, not usage). Overage charges are not refunded — the guarantee covers the subscription commitment, not unbounded consumption (design invariant 4).
3. Cancel the subscription (`status = 'cancelled'`)
4. Expire remaining grants (balancing debit in TB)
5. Create `refunds` row with `reason = 'satisfaction_guarantee'`, `refund_type = 'monetary'`
6. Execute TB transfers and Stripe refund

The net Revenue contribution from a guarantee invocation is negative (subscription fee refunded exceeds the settled usage value). This is the cost of the guarantee — it surfaces in operator reporting as customer acquisition cost.

**Invoice generation (daily cron — processes subscriptions where `current_period_end <= now`):**

1. Aggregate projected ClickHouse metering rows for the billing period, grouped by `(product_id, plan_id, pricing_phase)` for invoice totals and by component/bucket maps for customer-facing statement detail. These rows are derived from settled `billing_windows`, not from raw usage events directly.
2. Create invoice + usage line items in PostgreSQL
3. Commitment true-up: for contracts with `committed_monthly`, compare `subtotal` against the commitment. If usage < commitment, insert a `committed_minimum_trueup` line item for the difference and create a `KindCommitmentTrueUp` TigerBeetle transfer.
4. Evaluate adjustments: load all matching adjustment rules for this org/contract/tier/period. Apply MFN precedence. For each surviving adjustment, insert an adjustment line item (negative `amount`). Sum into `adjustments_total`.
5. Compute `pretax_total = max(subtotal, committed_minimum) + adjustments_total`, floored at 0. All spend caps and MFN comparisons operate on this value.
6. For each adjustment line item, create a TigerBeetle transfer: DR AcctRevenue, CR the destination account for that adjustment type (see TigerBeetle transfer types).
7. Stripe handoff: when `pretax_total > 0`, create Stripe invoice with line items and `automatic_tax: {enabled: true}`. Stripe computes tax (including per-line-item tax codes, customer exemptions, and jurisdiction rules). Read back `tax_total` and `tax_jurisdiction` from Stripe's response and record on the internal invoice. No TigerBeetle transfers for tax — Stripe owns the tax liability, collection, and remittance.
8. Compute `total_due = pretax_total + tax_total`.
9. Finalize Stripe invoice when `total_due > 0`. Invoices with `pretax_total = 0` (platform showback, fully credited) are finalized in PostgreSQL without a Stripe counterpart — no payment means no tax obligation.

**Period rollover:** after invoice generation, the cron advances the subscription: `current_period_start = current_period_end`, `current_period_end = current_period_start + 30 days`. For prepaid plans, the rollover also deposits the next period's credit grant. This keeps the anniversary anchor stable across periods.

**Per-tier invoice rules:**

Prepaid plans:
- Free tier orgs: no invoice generated (usage covered by auto-deposited grants, hard stop on exhaustion). Period rollover still occurs (new grant deposited).
- Self-serve orgs with overage (hobby/pro): invoice for overage charges only. Stripe invoice charged on receipt.

Postpaid plans:
- Enterprise orgs: invoice for total metered usage at contract rates. Stripe invoice with `due_date` set per contract payment terms. Adjustments applied per contract rules.
- Platform org: invoice for total metered usage at list rates. Platform showback adjustment (`percentage = -1.0000`) negates the subtotal. `total_due = 0`, no Stripe invoice. Dogfoods the entire invoice generation, adjustment evaluation, and TigerBeetle transfer pipeline on every billing cycle.

## TigerBeetle account structure

All financial state lives in TigerBeetle. Integer-only arithmetic — Stripe uses integers for the same reason: floating-point addition is not associative, and distributed aggregation of floats produces non-deterministic results across replicas. TigerBeetle uses uint128; the billing service bridges to uint64 for the current scale.

### Account types

Accounts are classified by their double-entry accounting type. This determines which TigerBeetle balance constraint flag is set and whether debits or credits increase the balance.

| Type | Code | Acct. type | ID scheme | Purpose | TB constraint flag |
|------|------|------------|-----------|---------|-------------------|
| Grant | 9 | Liability | `GrantAccountID(grantULID)` via half-swap | Prepaid balance (deferred revenue). Debits decrease (service delivered), credits increase (customer prepaid). | `debits_must_not_exceed_credits` — cannot consume more than was prepaid |
| Receivable | 14 | Asset | `ReceivableAccountID(orgID, productID, periodStart)` via FNV-1a | Postpaid usage accrual. Debits increase (customer owes more), credits decrease (payment received). One per (org, product, billing period). | `credits_must_not_exceed_debits` — cannot overpay beyond what's owed |
| Spend cap | 11 | — | `SpendCapAccountID(orgID, productID, periodStart)` via FNV-1a | Period-scoped overage limit. Prepaid plans only. | `debits_must_not_exceed_credits` |
| Revenue | 3 | Income | `OperatorAccountID(3)` | Destination for all settled charges (prepaid and postpaid). Credits increase. | — |
| Discounts | 15 | Income (contra) | `OperatorAccountID(15)` | Contra-revenue: tracks revenue reductions from adjustments (enterprise discounts, promotions, volume rebates). Debits increase. Enables gross vs. net revenue reporting. | — |
| Free tier pool | 4 | Equity | `OperatorAccountID(4)` | Source for free tier deposits | — |
| Stripe holding | 5 | Asset | `OperatorAccountID(5)` | Stripe payment holding | `credits_must_not_exceed_debits` |
| Promo pool | 6 | Equity | `OperatorAccountID(6)` | Source for promotional grants | — |
| Free tier expense | 7 | Expense | `OperatorAccountID(7)` | Sink for expired free tier grants | — |
| Expired credits | 8 | Income | `OperatorAccountID(8)` | Breakage: revenue recognized from unused prepaid credits that expired | — |
| Quota sink | 12 | — | `OperatorAccountID(12)` | Sink for spend-cap probe debits | — |
| Platform expense | 13 | Expense | `OperatorAccountID(13)` | Destination for platform showback adjustment transfers at invoice time | — |
| SLA expense | 16 | Expense | `OperatorAccountID(16)` | Destination for SLA credit adjustment transfers (operational cost of SLA breaches) | — |
| Refund payable | 18 | Liability | `OperatorAccountID(18)` | Monetary refunds owed to customers, pending Stripe payout. Cleared when Stripe confirms the refund. | `debits_must_not_exceed_credits` |

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
| Reservation | 1 | Prepaid: pending debit from grant |
| Settlement | 2 | Post actual spend from pending reservation |
| Void | 3 | Release unspent portion of reservation |
| Postpaid reservation | 17 | Postpaid: pending debit from receivable → AcctRevenue. No balance constraint. |
| Postpaid settlement | 18 | Post actual spend from postpaid reservation |
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
| Postpaid reservation | 17 | Postpaid: pending debit from receivable → AcctRevenue. No balance constraint. |
| Postpaid settlement | 18 | Post actual spend from postpaid reservation |
| Adjustment | 19 | Posted at invoice time: DR AcctRevenue → CR destination (AcctDiscounts, AcctPlatformExpense, or AcctSLAExpense depending on adjustment_type) |
| Receivable payment | 20 | Payment received: DR StripeHolding → CR Receivable (clears postpaid debt) |
| Proration refund | 21 | Mid-period plan change: transfer unused prepaid credits to a new refund grant (DR old Grant → CR new Grant with `source = 'refund'`) |
| Monetary refund | 24 | Revenue reversal: DR AcctRevenue → CR AcctRefundPayable. Linked pair with refund payout. |
| Refund payout | 25 | Refund disbursement: DR AcctRefundPayable → CR AcctStripeHolding. Posted on Stripe refund confirmation. |
| Refund credit | 26 | Revenue reversal via account credit: DR AcctRevenue → CR new Grant (`source = 'refund'`). No Stripe interaction. |

### Transfer ID schemes

Deterministic IDs enable safe retries across process crashes. All schemes pack a source identifier into the high u64 for TigerBeetle LSM locality, with discriminating fields in the low u64.

| Transfer type | High u64 | Low u64 packing |
|---------------|----------|-----------------|
| Prepaid reservation/settlement/void | job_id | `[0:4]=seq, [4]=grantIdx, [5]=kind` |
| Postpaid reservation/settlement/void | job_id | `[0:4]=seq, [5]=kind` (no grantIdx — single receivable leg) |
| Subscription deposit | subscription_id | `[0:4]=year*12+month, [5]=kind` |
| Stripe/task deposit | task_id | `[5]=kind` |
| Dispute debit | task_id | `[4]=grantIdx, [5]=KindDisputeDebit` |
| Spend cap ops | job_id | `[0:4]=seq, [5]=kind, [6:8]=AcctSpendCapCode` |
| Credit expiry | — | Full ULID half-swap (bijective, one per grant) |
| Adjustment | invoice_id hash | `[5]=KindAdjustment, [0:4]=adjustmentIdx` (one per adjustment line item per invoice, deterministic) |
| Receivable payment | invoice_id hash | `[5]=KindReceivablePayment` (one per invoice payment) |
| Proration refund | subscription_id | `[5]=KindProrationRefund, [0:4]=grantIdx` (one per grant carried forward) |
| Monetary refund / payout | refund_id hash | `[5]=KindMonetaryRefund` or `KindRefundPayout` (linked pair, one per refund) |
| Refund credit | refund_id hash | `[5]=KindRefundCredit` (one per account credit refund) |

All pending transfers have a 3600-second timeout. After timeout, TigerBeetle auto-voids and returns funds to the source account.

## ClickHouse usage evidence and metering projection

ClickHouse serves two roles:

1. **Raw usage evidence** for products whose consumption is naturally high-volume or bursty (for example, token streaming, request batches, or network I/O samples). These rows are product-specific telemetry, useful for debugging, analytics, and invoice explainability.
2. **Invoice-grade metering projection** derived from settled billing windows. One metering row represents one finalized billing window with immutable rate and funding context.

The second role is the financial read model. Invoice generation, billing dashboards, and customer-facing usage summaries read the projection, not the raw evidence stream.

### Schema: `forge_metal.metering`

```sql
CREATE TABLE forge_metal.metering (
    window_id                 String,
    org_id                    LowCardinality(String),
    actor_id                  String DEFAULT '',
    product_id                LowCardinality(String),
    source_type               LowCardinality(String),
    source_ref                String,
    window_seq                UInt32,
    reservation_shape         LowCardinality(String),
    started_at                DateTime64(6),
    ended_at                  DateTime64(6),
    reserved_quantity         UInt64,
    actual_quantity           UInt64,
    billable_quantity         UInt64,
    writeoff_quantity         UInt64,
    pricing_phase             LowCardinality(String),
    dimensions                Map(LowCardinality(String), Float64),
    component_quantities      Map(LowCardinality(String), Float64),
    component_charge_units    Map(LowCardinality(String), UInt64),
    bucket_charge_units       Map(LowCardinality(String), UInt64),
    charge_units              UInt64,
    writeoff_charge_units     UInt64,
    free_tier_units           UInt64,
    subscription_units        UInt64,
    purchase_units            UInt64,
    promo_units               UInt64,
    refund_units              UInt64,
    receivable_units          UInt64,
    bucket_free_tier_units    Map(LowCardinality(String), UInt64),
    bucket_subscription_units Map(LowCardinality(String), UInt64),
    bucket_purchase_units     Map(LowCardinality(String), UInt64),
    bucket_promo_units        Map(LowCardinality(String), UInt64),
    bucket_refund_units       Map(LowCardinality(String), UInt64),
    bucket_receivable_units   Map(LowCardinality(String), UInt64),
    plan_id                   LowCardinality(String) DEFAULT '',
    cost_per_unit             UInt64 DEFAULT 0,
    recorded_at               DateTime64(6) DEFAULT now64(6),
    trace_id                  String DEFAULT ''
)
ENGINE = MergeTree()
ORDER BY (org_id, product_id, started_at, source_ref, window_seq, window_id)
```

`window_id` is the stable identity of the projected billing window and is the basis for idempotent projection retries. `charge_units` is the billed cost in atomic units for this finalized window. The `*_units` columns break down which funding source covered each portion: grant-source columns (`free_tier_units`, `subscription_units`, etc.) for prepaid plans, `receivable_units` for postpaid plans. Bucket-level maps (`bucket_subscription_units`, `bucket_purchase_units`, etc.) preserve entitlement drawdown by product bucket, while `component_charge_units` and `component_quantities` preserve invoice-style SKU detail. For any given row, either the grant-source columns or `receivable_units` is nonzero, never both — determined by the plan's `billing_mode`. `plan_id` and `cost_per_unit` record the scalar reserve-time rate context; per-component rates live in the billing-window `rate_context` and are projected into statement line items through the billing service.

The metering row is a projection of authoritative billing-window state, not the write-time source of truth. A settled window is first made durable in PostgreSQL, then projected into ClickHouse. The first implementation can keep projection state inline on the billing window row (`metering_projected_at`, `last_projection_error`, retry counter). If multiple independent projections appear later, that inline marker can be split into a dedicated projection outbox table without changing the billing model.

This separation keeps ClickHouse off the charging-critical path:

- If ClickHouse is healthy, the projection usually lands immediately.
- If ClickHouse is unavailable, the projector retries until the settled window is reflected.
- If the projector crashes after inserting but before marking success, retries must be idempotent on billing window identity.

## Billing window lifecycle

The core billing loop for metered products is not "renew"; it is a window state machine. A billing window is a bounded entitlement slice for a specific `(org, product, source_type, source_ref, window_seq)` with immutable rate context and funding context. The slice may be time-based (sandbox runtime), unit-based (tokens, requests, bytes), or another product-specific shape chosen by reserve policy.

### Reserve

```
1. Verify org not suspended
2. Load active subscription plan (with contract override resolution)
3. Load default plan (fallback)
4. Check concurrent limit (min of trust tier policy, plan quota)
5. Load the product's reserve policy:
   - time window or unit tranche
   - target size
   - minimum reserveable size
   - whether partial reserve is allowed
   - whether bounded operator-paid grace is allowed
6. Create a PostgreSQL `billing_window` row in state `reserving`
7. Compute the target reservation amount from the product's usage model and resolved rate context
8. Branch on plan.billing_mode:

   PREPAID:
   9a. Load unexpired grant balances from PG, then lookup available amounts from TB
   10a. Select pricing phase: free_tier → included → overage (waterfall)
   11a. If the full target amount is unavailable:
       - if partial reserve is allowed and the remaining balance covers at least the minimum size, shrink the window
       - otherwise deny the window and leave the workload unstarted
   12a. If overage + spend cap: ensure spend-cap account, add linked probe+void to batch
   13a. Create linked TigerBeetle pending transfers for each grant in waterfall order
   14a. Update the billing window row to `reserved` with grant legs, reserved amount, rate snapshot, and expiry metadata

   POSTPAID:
   9b. Ensure receivable account exists for (org, product, current period)
   10b. Create pending transfer: debit receivable → credit AcctRevenue (KindPostpaidReservation)
   11b. Update the billing window row to `reserved` with receivable leg, reserved amount, rate snapshot, and expiry metadata
```

**Prepaid:** the linked batch is atomic — if the spend-cap probe fails (`ExceedsCredits`) or any grant has insufficient balance, the entire batch fails. This is TigerBeetle's linked transfer semantics. If `overage_unit_rates` is empty and grants are exhausted, Reserve returns `ErrInsufficientBalance` unless partial reserve is permitted and can produce a smaller valid window.

**Postpaid:** the receivable account has no `debits_must_not_exceed_credits` flag (it's an Asset, not a Liability), so the pending transfer always succeeds. There is no spend cap — postpaid plans accrue unbounded usage. The invoice at period end is the collection event.

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

This is evaluated per-dimension, identically for prepaid and postpaid. A single reservation can have different dimensions resolved by different overrides — e.g., `vcpu` at a promotional 90% discount while `memory_gb` uses the plan's list rate.

### Settle

```
1. Load the reserved billing window from PostgreSQL
2. Compute actual usage for the window from the product's usage model
3. Compute billed usage and operator-paid writeoff:
   - billed usage is bounded by the reserved entitlement for prepaid windows
   - bounded grace may intentionally let `actual > billed`
   - writeoff is explicit state, not an accounting accident
4. Branch on billing_mode:

   PREPAID:
   5a. For each grant leg: post billed spend, void remainder
   6a. If spend cap applied: post spend-cap debit for the billed amount

   POSTPAID:
   5b. Post billed spend on receivable leg, void remainder (KindPostpaidSettlement)

5. Update the billing window row to `settled` with actual usage, billed usage, writeoff amount, and settlement timestamps
6. Project the settled window into ClickHouse metering asynchronously
```

### Void

Cancel all pending legs (grant legs for prepaid, receivable leg for postpaid). Used when a workload fails before producing billable usage or when a reservation must be unwound after a persistence failure. The customer is never charged for crashes — void is the default failure mode.

### Continue long-running work

For long-running workloads, the caller performs two explicit commands:

1. `SettleWindow(current)`
2. `ReserveWindow(next)`

This replaces `Renew` as a billing primitive. `Renew` is only caller orchestration over two first-class transitions:

- finalize economic truth for the old window
- acquire entitlement for the next window

That split matters because partial progress is real. The old window may be fully settled even if the next window cannot be reserved. Making the two transitions explicit prevents duplicate settlement, keeps retries idempotent, and generalizes cleanly beyond sandbox runtime to inference streams, request firehoses, and network I/O billing.

## Reconciliation

Eight named consistency checks run periodically across all three stores. Each check has a severity (`alert` or `warn`) and emits a structured reconciliation finding for the OTel pipeline and operator dashboard.

| Check | Severity | Validates |
|-------|----------|-----------|
| `grant_account_catalog_consistency` | alert | Every active PG grant has a TB account |
| `no_orphan_grant_accounts` | alert | Every TB grant account found has a PG catalog row |
| `expired_grants_swept` | warn | Every expired grant has a credit expiry transfer in TB |
| `licensed_charge_exactly_once` | alert | Each completed licensed charge task has exactly one Revenue transfer |
| `metering_vs_transfers` | alert | Settled billing-window charges projected to CH are consistent with TB posted debits per org (CH > TB = projection bug or duplicate metering) |
| `receivable_vs_invoiced` | alert | For postpaid orgs: TB receivable `debits_posted` for the period matches the invoice `subtotal`. Drift = metering or invoice generation bug. |
| `trust_tier_monotonicity` | warn | No org auto-promoted to enterprise |
| `refunds_vs_stripe` | alert | Every completed PG refund has a matching Stripe refund (monetary) or TB grant (account credit) |

Failure policy: fail immediately on infrastructure errors. Do not advance any watermark on failure. Reconciliation failures are loud — they produce alert-level findings that surface in the OTel pipeline.

## Dispute handling

**Prepaid orgs** — on `dispute_opened`:
1. Load all open grants for the org, sorted: disputed payment's grant first, then by `expires_at`
2. Balancing debit each grant (clamped to available) into StripeHolding up to the dispute amount
3. If total debited < dispute amount: suspend the org (prevent further usage)
4. Log `dispute_opened` billing event with shortfall details

**Postpaid orgs** — on `dispute_opened`:
1. The disputed payment was against an invoice that cleared a receivable. Re-open the receivable: DR StripeHolding (Asset ↓, Stripe claws back funds), CR Receivable (Asset ↑, debt re-opens)
2. Suspend the org
3. Log `dispute_opened` billing event

Suspension sets `subscriptions.status = 'suspended'` for all active subscriptions. Future `Reserve` calls return `ErrOrgSuspended`.

## Plan changes (upgrades / downgrades)

Mid-period plan changes follow from design invariants 1 and 3: the customer never overpays, downgrades are not punitive but not advantageous. Projected metering rows already record `plan_id`, `cost_per_unit`, component charges, and bucket charges at Reserve time, so historical usage is always priced correctly regardless of plan changes. The problem is narrower than in most billing systems — it is about managing prepaid credit balances across the transition, not about re-rating usage.

### Upgrade (e.g. Free → Pro)

```
1. Close old subscription: SET current_period_end = NOW(), status = 'cancelled'
2. Forfeit free-tier grants: free_tier grants are not carried forward (invariant 3 — the customer
   didn't pay for them). Close with balancing debit in TB.
3. Create new subscription:
   - plan_id = new plan
   - current_period_start = NOW()
   - current_period_end = original period end
   - prorated_from_plan_id = old plan
4. Prorate credit deposit: `included_credit_buckets × (remaining_days / total_days)` at the bucket level
   - New grants with source = 'subscription', one per bucket, `expires_at = original period end`
5. Prorate subscription fee: charge via Stripe for (remaining_days / total_days) of the new plan fee
```

### Downgrade (e.g. Pro → Hobby)

```
1. Close old subscription: SET current_period_end = NOW(), status = 'cancelled'
2. Calculate carry-forward for paid grants (source = 'subscription' or 'purchase'):
   - unused_credits = sum of available balances across eligible grants
   - prorated_cap = entitlement_value_from `included_credit_buckets` × (remaining_days / total_days)
   - carry_forward = min(unused_credits, prorated_cap)
3. If carry_forward > 0: create refund grant
   - source = 'refund', amount = carry_forward, expires_at = original period end
   - TB: KindProrationRefund transfer from old grant(s) to new grant
4. Close remaining balance on old grants (balancing debit in TB)
5. Create new subscription:
   - plan_id = new plan
   - current_period_start = NOW()
   - current_period_end = original period end
   - prorated_from_plan_id = old plan
6. Prorate credit deposit: new plan's `included_credit_buckets × (remaining_days / total_days)` at the bucket level
7. Prorate subscription fee: Stripe charge for remaining portion of new plan fee.
   If old plan fee > new plan fee, issue prorated refund for the difference.
```

The `min(unused_credits, prorated_cap)` bound in step 2 is the key anti-gaming mechanism. A customer who consumed their entire Pro allocation (50,000 au) on day 1 and downgrades on day 2 gets `min(0, prorated) = 0` carry-forward. A light user who consumed 5,000 au of 50,000 au and downgrades on day 15 gets `min(45,000, 25,000) = 25,000` — the prorated value of what they paid, not the face value of unused credits.

### Postpaid plan changes

Simpler — no prepaid credits to manage. Close the old subscription, create a new one. The receivable for the current period captures usage through the switch point (projected metering rows already record the old contract rates). A new receivable picks up usage from the switch point forward. The month-end invoice produces separate line items per plan — the ClickHouse aggregation groups by `plan_id`.

## Stripe integration

Stripe is the payment collection and tax compliance layer, not the billing engine. The billing service computes `pretax_total`; Stripe handles everything downstream: tax computation, tax collection, tax remittance/filing, refund tax math, and credit notes.

| Operation | Stripe API | Ownership boundary |
|-----------|------------|-------------------|
| Store payment method | Customer + SetupIntent | — |
| Collect self-serve credit purchase | Checkout Session | Billing service creates the session; Stripe collects payment; `payment_intent.succeeded` deposits prepaid credits idempotently |
| Collect self-serve payment | Invoice API (created at invoice generation time) | Billing service provides pre-tax line items |
| Collect enterprise payment | Invoice API with `due_date` from contract terms | Billing service provides pre-tax line items |
| Confirm invoice payment | Webhook: `invoice.paid` | Future invoice subsystem records payment, posts TB transfers |
| Compute tax | Invoice API with `automatic_tax: {enabled: true}` | **Stripe owns.** Computes per-line-item tax from product tax codes, customer address, exemption status, and jurisdiction rules. Billing service reads back `tax_total` and `tax_jurisdiction`. |
| Collect and remit tax | Stripe Tax auto-filing (where registered) | **Stripe owns.** Billing service does not track tax liability in TigerBeetle. |
| Process refund | Refund API (`stripe.refunds.create`) | **Stripe owns tax math.** Billing service passes pre-tax refund amount. Stripe computes proportional tax reversal automatically. |
| Issue credit note | Credit Note API | **Stripe owns.** Used for post-finalization corrections (billing errors, SLA credits). References the Stripe invoice. |
| Customer tax exemption | Customer API (`tax_exempt` field) | Billing service sets `none`, `exempt`, or `reverse` on the Stripe Customer. Stripe Tax acts on it. |
| Handle disputes | Webhook: `charge.dispute.created` | Future dispute subsystem handles TB accounting |

Stripe Subscriptions are used only to collect recurring plan payments and maintain the customer subscription lifecycle. Metered usage, bucket consumption, and invoice-grade settlement stay in the billing service, PostgreSQL, and TigerBeetle. That keeps Stripe out of the request-path financial ledger while still using it for payment collection, customer portal flows, and cancellation.

**Why Stripe-first for tax:** Tax compliance requires maintaining jurisdiction-specific rates for ~13,000+ US tax districts alone, plus EU VAT, digital services taxes, reverse charge rules, nexus determination, and filing deadlines. This is the same class of problem as email deliverability (delegated to Resend) — regulatory compliance surfaces with constantly changing rules that are not worth self-hosting. Stripe Tax costs 0.5% per transaction.

## API surface

All endpoints use the Huma v2 framework with OpenAPI 3.1 spec generation.

### Enforcement (called by services before/after work)

| Endpoint | Purpose |
|----------|---------|
| `POST /internal/billing/v1/reserve` | Reserve credits for a billing window. Blocks on insufficient balance or spend cap. |
| `POST /internal/billing/v1/settle` | Finalize a reserved billing window: post billed spend, void remainder, persist settled window state, and queue ClickHouse projection. |
| `POST /internal/billing/v1/void` | Cancel reservation. Customer not charged. |

### Query

| Endpoint | Purpose |
|----------|---------|
| `GET /internal/billing/v1/orgs/{org_id}/balance` | Prepaid: grant balances (available/pending). Postpaid: current-period receivable accrual. |
| `GET /internal/billing/v1/orgs/{org_id}/products/{product_id}/balance` | Per-product balance breakdown |
| `GET /internal/billing/v1/orgs/{org_id}/subscriptions` | Active subscriptions (includes `billing_mode` from plan) |
| `GET /internal/billing/v1/orgs/{org_id}/grants` | Credit grants (prepaid orgs, filterable by product, active status) |
| `GET /internal/billing/v1/orgs/{org_id}/adjustments` | Active adjustments for this org (contract-scoped, org-scoped, and matching global/tier rules) |
| `GET /internal/billing/v1/orgs/{org_id}/refunds` | Refund history for this org (filterable by status, reason) |

### Operations (cron / admin)

| Endpoint | Purpose |
|----------|---------|
| `POST /internal/billing/v1/ops/deposit-credits` | Period credit deposits for active prepaid subscriptions (no-op for postpaid) |
| `POST /internal/billing/v1/ops/expire-credits` | Sweep expired grants (prepaid only) |
| `POST /internal/billing/v1/ops/reconcile` | Run eight-check consistency verification |
| `POST /internal/billing/v1/ops/trust-tier-evaluate` | Automated promotion/demotion |
| `POST /internal/billing/v1/ops/process-refund` | Execute a refund (monetary or account credit). Creates TB transfers, Stripe refund if monetary. |
| `POST /internal/billing/v1/ops/change-plan` | Mid-period plan change. Handles credit carry-forward, prorated deposits, subscription lifecycle. |

## Case studies

These demonstrate how `contract_overrides` (rate card, at Reserve time), `adjustments` (invoice modifications, at invoice time), `billing_mode` (prepaid vs. postpaid), plan changes, refunds, and tax compose to handle diverse commercial arrangements without special-purpose billing code.

### Postpaid: Acme Corp — enterprise contract with overrides + onboarding adjustment

Acme signs a 12-month contract on a postpaid plan (`billing_mode = 'postpaid'`). They want a blanket 30% discount on standard sandboxes, a fixed vCPU rate for premium NVMe, $2,000/month minimum spend, net-45 payment terms, and a 15% onboarding discount for the first 90 days. All usage accrues against receivable accounts.

**contracts row:**

| contract_id | org_id | starting_at | ending_at | committed_monthly | payment_terms_days | status |
|---|---|---|---|---|---|---|
| `acme-2026` | `org_acme` | 2026-04-01 | 2027-04-01 | 2000000 | 45 | active |

**contract_overrides rows** (rate card — applied at Reserve time):

| contract_id | product_id | dimension | override_type | multiplier | overwrite_rate | starting_at | ending_at |
|---|---|---|---|---|---|---|---|
| `acme-2026` | `sandbox-standard` | `all` | multiplier | 0.7000 | — | — | — |
| `acme-2026` | `sandbox-premium-nvme` | `vcpu` | overwrite | — | 45 | — | — |
| `acme-2026` | `sandbox-premium-nvme` | `memory_gb` | multiplier | 0.7000 | — | — | — |

**adjustments row** (invoice adjustment — applied at invoice time):

| adjustment_id | adjustment_type | contract_id | percentage | product_id | starting_at | ending_at | description |
|---|---|---|---|---|---|---|---|
| `adj-acme-onboard` | `percentage_discount` | `acme-2026` | -0.1500 | — | 2026-04-01 | 2026-07-01 | Onboarding discount (15%) |

These operate at different levels: the `contract_overrides` reduce Acme's per-second rates (projected metering rows already reflect the discounted rates). The `adjustment` reduces the invoice total by an additional 15% (metering projection unchanged, discount visible only on the invoice). They stack because the rate card changes the cost of each unit, while the adjustment discounts the aggregate.

**Rate resolution at Reserve time for a sandbox-premium-nvme job with allocation `{vcpu: 2.0, memory_gb: 8.0}`:**

- `vcpu`: specific dimension override exists → overwrite → rate = 45 (fixed, ignores plan list rate)
- `memory_gb`: specific dimension override exists → multiplier 0.7 → rate = plan.unit_rates.memory_gb × 0.7

If the operator later raises the list price of `memory_gb` on the sandbox-premium-nvme plan, Acme's memory rate adjusts automatically (multiplier tracks the rate card). The vCPU rate stays at 45 (overwrite is insulated).

**Invoice at month end during onboarding period (usage > commitment):**

```
Invoice #FM-2026-05-0042
Billing period: April 1–30, 2026
Account: Acme Corp | Contract: acme-2026

  Sandbox Standard (metered)
    62,400 sec × 70 au/sec (30% off list 100)               4,368,000 au

  Sandbox Premium NVMe (metered)
    vCPU: 12,200 sec × 45 au/sec (fixed)                      549,000 au
    memory_gb: 12,200 sec × 35 au/sec (30% off list 50)       427,000 au

  Usage subtotal                                             5,344,000 au
  Committed minimum                                          2,000,000 au

  Onboarding discount (15%)                                   -801,600 au

  ───────────────────────────────────────────────────────────────────────
  Adjustments total                                           -801,600 au
  Pre-tax total                                              4,542,400 au
  NY State + City Sales Tax (8.875%)                           403,138 au
  ───────────────────────────────────────────────────────────────────────
  Total due                                                  4,945,538 au

  Payment terms: Net-45
  Due date: June 14, 2026
```

A month where Acme uses less than their commitment:

```
  Usage subtotal                                             1,200,000 au
  Committed minimum                                          2,000,000 au
  Committed minimum true-up                                    800,000 au
  Onboarding discount (15%)                                   -300,000 au
  ───────────────────────────────────────────────────────────────────────
  Pre-tax total                                              1,700,000 au
  NY State + City Sales Tax (8.875%)                           150,875 au
  ───────────────────────────────────────────────────────────────────────
  Total due                                                  1,850,875 au
```

The true-up generates a `KindCommitmentTrueUp` TigerBeetle transfer for 800,000 au into `AcctRevenue`. The onboarding discount generates a `KindAdjustment` transfer: DR AcctRevenue, CR AcctDiscounts for 300,000 au. After month 3, the onboarding adjustment's `ending_at` is reached and it stops matching. No cleanup needed.

### Promotional: 90% off vCPU for first 30 days on Pro tier

The operator wants to incentivize Pro signups with a launch promotion: 90% off vCPU costs for the first 30 days, all products. Memory and storage at full rate. This is a rate card change (per-unit pricing), so it uses a contract with overrides — not an adjustment.

**On plan upgrade to Pro, the billing service creates:**

**contracts row:**

| contract_id | org_id | starting_at | ending_at | committed_monthly | payment_terms_days | status |
|---|---|---|---|---|---|---|
| `promo-vcpu90-{org_id}` | `{org_id}` | {signup_time} | {signup_time + 30d} | — | 0 | active |

**contract_overrides rows:**

| contract_id | product_id | dimension | override_type | multiplier | overwrite_rate | starting_at | ending_at |
|---|---|---|---|---|---|---|---|
| `promo-vcpu90-{org_id}` | `sandbox-standard` | `vcpu` | multiplier | 0.1000 | — | — | — |
| `promo-vcpu90-{org_id}` | `sandbox-premium-nvme` | `vcpu` | multiplier | 0.1000 | — | — | — |

No `committed_monthly`, no `payment_terms_days` override. The contract exists purely to carry the override rows. The `ending_at` on the contract (signup + 30 days) causes the contract to expire naturally. After expiry, Reserve falls through to the plan's list rate with no special-case code.

The promotion ends with zero cleanup. No cron job to remove discounts, no migration, no feature flags. The contract's temporal bounds do the work.

Note: this could alternatively be modeled as an adjustment (`percentage_discount` of `-0.9000` scoped to `product_id = sandbox-*`, `dimension = vcpu`). The difference: as a contract_override, the discount is visible in projected metering rows (per-second rates reflect the discount). As an adjustment, projected metering rows show full-rate and the discount appears only on the invoice. The contract_override approach is preferred for promotional pricing because it produces more accurate metering data.

### Prepaid: free tier with a 500-unit bucket entitlement, then hard stop

The operator defines a `free` plan tier (`billing_mode = 'prepaid'`) with `included_credit_buckets = {"sandbox": 500}` per month and no overage path (`overage_unit_rates = {}`). This is the default experience for new signups.

No contract, overrides, or adjustments needed. The plan's bucketed entitlement map deposits a grant each billing period. When the grant is exhausted, `Reserve` returns `ErrInsufficientBalance`. The frontend shows "free tier limit reached — upgrade to continue." No invoice is generated — usage is fully covered by the prepaid grant.

### Global promotion with MFN: CEO announces 50% off for everyone

The operator creates a global adjustment — no contract, no org, no tier restriction:

| adjustment_id | adjustment_type | contract_id | org_id | tier | percentage | starting_at | ending_at | description |
|---|---|---|---|---|---|---|---|---|
| `adj-summer-2026` | `percentage_discount` | — | — | — | -0.5000 | 2026-06-01 | 2026-09-01 | Summer 2026 promotion (50%) |

At invoice time for a self-serve Pro customer: the 50% global discount applies. Their invoice shows usage at list rates with a -50% adjustment.

At invoice time for Acme (who has a contract-scoped 15% onboarding discount): MFN precedence kicks in. Both the global 50% and the contract 15% are `percentage_discount` type from different sources (global vs. contract). The customer gets the better deal — the 50% global discount wins. Acme is never worse off for having a contract.

After September 1, the global adjustment's `ending_at` is reached. Self-serve customers return to list pricing. Acme returns to their 15% contract discount (if still within the onboarding window) or list pricing.

### Platform: operator dogfooding the full billing path

The operator's own org has `trust_tier = 'platform'`. It uses a postpaid plan (`billing_mode = 'postpaid'`) and goes through the same billing abstractions as any enterprise customer: subscription, Reserve/Settle/Void via receivable accounts, invoice generation and adjustment evaluation. The only difference is a platform showback adjustment that zeros the invoice.

**Setup (seeded at platform bootstrap):**
- Subscription to a postpaid plan at list rates
- Platform contract with a showback adjustment (see below)
- No Stripe customer (no payment to collect on $0 invoices)

**adjustments row:**

| adjustment_id | adjustment_type | contract_id | percentage | product_id | description |
|---|---|---|---|---|---|
| `adj-platform-showback` | `platform_showback` | `contract-platform` | -1.0000 | — | Platform showback (100%) |

**At Reserve time:** receivable account is debited. Rate resolution follows the same cascade as any org — plan rates, contract overrides if any. The receivable has no balance constraint (Asset account), so the reservation always succeeds.

**At Settle time:** actual spend is posted on the receivable. The billing window is marked `settled` with `pricing_phase = 'metered'`, `receivable_units = charge_units`, and then projected into ClickHouse. Same postpaid path as any enterprise org.

**At invoice generation time:** the platform org is not skipped. The invoice cron:
1. Aggregates projected ClickHouse metering rows for the period (same query as any postpaid org)
2. Creates `metered` usage line items at list rates
3. Evaluates adjustments — the `platform_showback` rule matches, producing an adjustment line item with `amount = -subtotal`
4. Computes `adjustments_total = -subtotal`, `pretax_total = 0`
5. Creates a `KindAdjustment` transfer: DR AcctRevenue, CR AcctPlatformExpense for the subtotal amount
6. Skips Stripe invoice creation (`pretax_total = 0` → no tax computation, `total_due = 0`)

**Sample platform invoice:**

```
Invoice #FM-2026-05-0001
Billing period: April 1–30, 2026
Account: Platform Org (trust_tier: platform)

  Sandbox Standard (metered)
    186,400 sec × 100 au/sec                                18,640,000 au

  Sandbox Premium NVMe (metered)
    vCPU: 42,000 sec × 60 au/sec                              2,520,000 au
    memory_gb: 42,000 sec × 50 au/sec                         2,100,000 au

  Usage subtotal                                             23,260,000 au
  Platform showback (100%)                                  -23,260,000 au
  ───────────────────────────────────────────────────────────────────────────
  Total due                                                           0 au
```

This exercises every component of the billing pipeline on every cycle: metering aggregation, rate resolution, line item generation, adjustment evaluation, MFN precedence, TigerBeetle transfer creation, and invoice finalization. The platform invoice is a real artifact — queryable, auditable, and structurally identical to customer invoices. The operator sees exactly what their CI compute would cost at list rates.

If billing infrastructure is down (TigerBeetle unavailable), the Forgejo webhook adapter flips a runtime configuration flag to route CI directly to the vm-orchestrator gRPC API, bypassing sandbox-rental-service and billing entirely. Those runs intentionally create no billing windows and no customer charge; the operator eats the cost by policy. When billing recovers, the flag is flipped back. This is an explicit operator subsidy path, not a reconciliation failure.

### Mid-month upgrade: Free → Pro on day 15

DevShop is on the free plan (`billing_mode = 'prepaid'`, 500 au in the sandbox bucket, no overage). On April 15 they upgrade to Pro (`billing_mode = 'prepaid'`, 50,000 au of bucketed entitlement, $49/mo subscription, overage at 120 au/sec).

**State at upgrade time:**
- Free grant: 500 au deposited April 1, 300 au consumed, 200 au remaining
- Period: April 1–30 (30 days). 15 days remaining.

**Billing service executes:**

1. Close free subscription (`status = 'cancelled'`, `current_period_end = April 15`)
2. Forfeit free grant: balancing debit of 200 au in TB. Free-tier grants are not carried forward (invariant 3 — the customer didn't pay for them).
3. Create Pro subscription: `current_period_start = April 15`, `current_period_end = April 30`, `prorated_from_plan_id = 'sandbox-free'`
4. Prorated credit deposit: `50,000 × (15/30) = 25,000 au`. New grant: `source = 'subscription'`, `expires_at = April 30`
5. Prorated subscription charge: `$49 × (15/30) = $24.50` via Stripe

**Invoice at month end:**

```
Invoice #FM-2026-05-0123
Billing period: April 1–30, 2026
Account: DevShop LLC

  Sandbox Standard — Free tier (Apr 1–15)
    Covered by free tier grant (300 au consumed)                       0 au

  Sandbox Standard — Pro tier (Apr 15–30)
    22,100 sec × 100 au/sec                                    2,210,000 au
    Covered by prorated subscription grant                    -2,210,000 au

  ─────────────────────────────────────────────────────────────────────────
  Overage charges                                                      0 au
  Pro subscription (prorated, 15/30 days)                        245,000 au
  Pre-tax total                                                  245,000 au
  CA Sales Tax (7.25%)                                            17,763 au
  ─────────────────────────────────────────────────────────────────────────
  Total due                                                      262,763 au
```

Projected metering rows for the free-tier period have `plan_id = 'sandbox-free'`, `pricing_phase = 'free_tier'`. Projected metering rows for the Pro period have `plan_id = 'sandbox-pro'`, `pricing_phase = 'included'`. The ClickHouse aggregation groups by `plan_id`, producing separate line items — no re-rating needed.

### Mid-month downgrade: Pro → Hobby on day 20

DevShop is on Pro ($49/mo, 50,000 au included). On April 20 they downgrade to Hobby ($9/mo, 10,000 au included). They consumed 35,000 au in 20 days.

**Carry-forward calculation (invariant 3):**

```
unused_credits    = 50,000 - 35,000 = 15,000 au
remaining_days    = 10/30
prorated_cap      = 50,000 × (10/30) = 16,667 au
carry_forward     = min(15,000, 16,667) = 15,000 au
```

The prorated cap exceeds unused credits here, so the customer gets the full 15,000 au. This is the "not punitive" case — they consumed less than the prorated value of their remaining subscription.

**Anti-gaming scenario (same plan, heavy user):** DevShop consumed 50,000 au in 5 days (full allocation) and downgrades on day 5:

```
unused_credits    = 50,000 - 50,000 = 0 au
carry_forward     = min(0, anything) = 0 au
```

No carry-forward. The customer consumed their full allocation and has nothing to carry. This is the "not advantageous" case.

**Billing service executes:**

1. Close Pro subscription
2. Create refund grant: 15,000 au, `source = 'refund'`, `expires_at = April 30`
3. TB: `KindProrationRefund` transfer from old Pro grant → new refund grant
4. Close remaining Pro grant balance (balancing debit)
5. Create Hobby subscription: `prorated_from_plan_id = 'sandbox-pro'`
6. Prorated Hobby credit deposit: `10,000 × (10/30) = 3,333 au`
7. Prorated subscription fee: Hobby $9 × (10/30) = $3.00. Prorated Pro refund: $49 × (10/30) = $16.33. Net refund to customer: $13.33.

**Invoice at month end:**

```
Invoice #FM-2026-05-0234
Billing period: April 1–30, 2026
Account: DevShop LLC

  Sandbox Standard — Pro tier (Apr 1–20)
    35,000 au consumed, covered by subscription grant                  0 au

  Sandbox Standard — Hobby tier (Apr 20–30)
    8,200 sec × 100 au/sec                                       820,000 au
    Covered by prorated subscription grant (3,333 au)            -333,300 au
    Covered by carry-forward refund grant (4,867 au)             -486,700 au

  ─────────────────────────────────────────────────────────────────────────
  Overage charges                                                      0 au
  Hobby subscription (prorated, 10/30 days)                       30,000 au
  Pre-tax total                                                   30,000 au
  CA Sales Tax (7.25%)                                             2,175 au
  ─────────────────────────────────────────────────────────────────────────
  Total due                                                       32,175 au
```

The refund grant from the carry-forward is consumed in waterfall order (earliest-expiring first). Since both the Hobby grant and refund grant expire at period end, ULID order breaks the tie. The customer sees a single invoice covering both plan periods.

### Satisfaction guarantee: customer cancels on day 22

DevShop signed up for Pro ($49/mo, 50,000 au) on April 1. Their contract has `refund_window_days = 30`. On April 22 (day 22, within the 30-day window) they invoke the satisfaction guarantee.

**State at invocation:**
- Subscription fee paid: $49.00 (490,000 au at asset scale 4)
- Grant deposited: 50,000 au
- Usage consumed: 32,000 au (across 15 sandbox sessions)
- Grant remaining: 18,000 au

**Step 1: Eligibility check**

```
contract.starting_at   = 2026-04-01
now                    = 2026-04-22
days_elapsed           = 21
refund_window_days     = 30
21 < 30 → eligible
```

**Step 2: Compute refund amount**

Refund = subscription fee = 490,000 au. The consumed 32,000 au of compute is not charged — the guarantee refunds the subscription fee regardless of usage (invariant 1 — the customer never overpays). However, the guarantee explicitly does not cover overage charges (invariant 4). If DevShop had consumed 60,000 au (10,000 au of overage), the refund would still be 490,000 au (subscription fee only); the overage charges stand.

**Step 3: Execute**

1. Cancel subscription: `status = 'cancelled'`, `current_period_end = NOW()`
2. Close remaining grant: balancing debit of 18,000 au in TB
3. Create refund:
   ```sql
   INSERT INTO refunds (refund_id, org_id, invoice_id, refund_type, amount, reason)
   VALUES ('ref-devshop-sat', 'org_devshop', NULL, 'monetary', 490000, 'satisfaction_guarantee')
   ```
4. TB transfers (linked):
   - DR AcctRevenue → CR AcctRefundPayable: 490,000 au (`KindMonetaryRefund`)
   - DR AcctRefundPayable → CR AcctStripeHolding: 490,000 au (`KindRefundPayout`)
5. Stripe: `stripe.refunds.create({payment_intent: '...', amount: 4900})`

**Revenue accounting:**

The 32,000 au of settled usage was posted to AcctRevenue during Reserve/Settle. The refund debits 490,000 au from AcctRevenue. Net Revenue contribution from this customer:

```
32,000 au (settled usage) - 490,000 au (refund) = -458,000 au
```

This negative contribution is correct and intentional — it represents the cost of the satisfaction guarantee. The grant consumption (32,000 au) and the refund (490,000 au) are separate transfers with separate transfer types. The `metering_vs_transfers` reconciliation check still passes: CH metering shows 32,000 au, TB grant debits_posted shows 32,000 au. The refund is against AcctRevenue, not the grant.

### Satisfaction guarantee with overage: cap in action

Same scenario, but DevShop consumed 65,000 au — exceeding their 50,000 au grant by 15,000 au of overage at 120 au/sec.

**Refund amount = subscription fee only (490,000 au).** The 15,000 au of overage charges are not refunded. The guarantee caps at the subscription fee — the customer accepted overage pricing when they exceeded their allocation (invariant 4).

**Invoice for the partial month (before guarantee invocation):**

```
  Overage: 15,000 au × 120 au/sec                             1,800,000 au
  Satisfaction guarantee refund                                 -490,000 au
  ─────────────────────────────────────────────────────────────────────────
  Pre-tax total                                                1,310,000 au
```

The customer pays for the overage minus the refunded subscription fee. The guarantee is valuable but bounded — it protects the customer's initial commitment, not unlimited consumption.
