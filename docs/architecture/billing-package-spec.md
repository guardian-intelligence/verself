# Billing Domain And Service Specification

Package `billing` is the financial core of forge-metal. It owns org financial
provisioning, product pricing lookup, credit grant lifecycle, quota enforcement, Stripe cash
collection integration, TigerBeetle balance enforcement, and reconciliation.

`billing-service` is the transport and runtime boundary around that package. It owns HTTP,
OpenAPI, generated-client distribution, auth, webhook ingress, worker execution, and
operational endpoints.

The package is product-agnostic. It does not know what a "sandbox" or "storefront" is. It
knows about products, plans, subscriptions, per-product grants, metering dimensions, funding
sources, and dunning state.

This document is normative and self-contained.

Three modules participate in the target billing architecture:

| Module | Owns | Must not own |
|--------|------|---------------|
| `github.com/forge-metal/billing` | domain operations, invariants, pricing resolution, grant lifecycle, quota checks, TigerBeetle/PostgreSQL/ClickHouse coordination, Stripe API calls needed to create checkout/subscription sessions | `net/http`, Huma schemas, webhook signature verification, background worker loops, service DTOs, generated clients |
| `github.com/forge-metal/billing-service` | Huma route registration, OpenAPI document, auth, Stripe webhook ingress, task worker runtime, health/readiness, operational commands | duplicated billing rules, handwritten service clients |
| generated billing client | typed remote access to `billing-service` for other services | handwritten endpoint paths, duplicated request/response structs |

Four systems participate in every non-trivial billing flow:

| System | Authoritative for | Not authoritative for |
|--------|-------------------|-----------------------|
| Stripe | Cash collection, subscription invoice lifecycle, dispute notifications, Checkout sessions | Metering truth, internal balance enforcement, per-product credit attribution |
| TigerBeetle | Per-grant posted/pending balances, idempotent money movement, immutable transfer log | Product eligibility policy, task retry state |
| PostgreSQL | Product catalog, immutable grant catalog, subscription state, task queue, retry/DLQ state | Grant balance |
| ClickHouse | Append-only metering history, rolling-window quota queries, reconciliation evidence | Online balance enforcement |

The deliberate cross-system rule is:

1. Stripe decides whether external money was collected.
2. TigerBeetle decides whether forge-metal can afford to provide service right now, one grant at a time.
3. PostgreSQL decides which grants are eligible to be spent for a given product.
4. ClickHouse records what actually happened.

The TigerBeetle account model and deterministic ID derivation scheme from
`docs/architecture/application-stack-rfc.md` are normative for this package.

This document makes six material design decisions:

- Each credit grant gets its own TigerBeetle account with
  `DebitsMustNotExceedCredits`. TigerBeetle is the single source of financial
  truth for balance. PostgreSQL is an immutable grant catalog. Financial
  operations do not require PostgreSQL-side serialization.
- Overage is a second rate card, not a post-facto inference from aggregate balance.
  The pricing phase is selected at reservation boundaries and recorded explicitly.
- Overage spending is capped per subscription via `overage_cap_units`. The org admin controls
  the ceiling; `Reserve` enforces it at reservation boundaries using ClickHouse actuals.
- Metering rows record `actor_id` (the Zitadel user who triggered the usage). Per-member
  spending policy is deferred; the column is recorded now to avoid losing attribution history.
- Stripe usage-record synchronization is not part of this design. Stripe billing meters are
  sufficient for Stripe-facing lifecycle needs, and forge-metal does not require Stripe to be
  the metering source of truth.
- Annual cancellation with refund is modeled as credit-note issuance against the finalized
  Stripe invoice, not as `DELETE /v1/subscriptions/{id}` with `prorate=true` alone.

Verified upstream contracts as of 2026-04-04:

| Dependency | Verified release | Notes |
|------------|------------------|-------|
| `github.com/tigerbeetle/tigerbeetle-go` | `v0.16.78` | No `0.17.x` or `0.18.x` release line is published on the Go module proxy. |
| `github.com/stripe/stripe-go/v85` | `v85.0.0` | `v85.1.0-*` prereleases exist; no stable `v86` exists. |
| `pgregory.net/rapid` | `v1.2.0` | `StateMachineActions` accepts `func(rapid.TB)` methods. |
| `github.com/oklog/ulid/v2` | `v2.1.0` | ULID generation with monotonic entropy source. |

---

## 0. Runtime Module Boundaries

The target deployment topology is:

```text
                                      public HTTPS
                                           |
                                           v
                                        Caddy
                                           |
                  +------------------------+------------------------+
                  |                                                 |
                  v                                                 v
   POST /webhooks/stripe                                internal billing HTTP API
     Stripe-signed only                             /internal/billing/v1/... + /openapi
                  |                                                 |
                  +------------------------+------------------------+
                                           |
                                           v
                                  billing-service
                        (Huma contract, auth, webhook ingress,
                         task worker, ops endpoints, OpenAPI)
                                           |
                                           v
                                      billing lib
                         (domain rules, ledger logic, pricing,
                          quotas, reconciliation primitives)
                      +----------------+----------------+----------------+
                      |                |                |                |
                      v                v                v                v
                 PostgreSQL       TigerBeetle      ClickHouse         Stripe
```

The sanctioned call paths are:

1. Browser apps and internal frontend backends call `billing-service`.
2. Other forge-metal services call `billing-service` through the generated billing client.
3. Stripe calls only the webhook ingress owned by `billing-service`.
4. Only code running inside `billing-service` may directly compose `billing` with transport
   adapters. Cross-service imports of `billing` are not part of the public integration model.

This split is deliberate:

- `billing` is the semantic source of truth for money movement and entitlement decisions.
- `billing-service` is the contract source of truth for cross-service integration.
- The generated billing client is the only approved way for a remote service to talk to
  billing over HTTP.

## Table of Contents

0. [Runtime Module Boundaries](#0-runtime-module-boundaries)
1. [PostgreSQL And ClickHouse Schema](#1-postgresql-and-clickhouse-schema)
2. [Package API Surface](#2-package-api-surface)
3. [TigerBeetle Integration Details](#3-tigerbeetle-integration-details)
4. [Stripe Integration Details](#4-stripe-integration-details)
5. [Testing Specification](#5-testing-specification)
6. [Configuration](#6-configuration)
7. [Error Handling and Failure Modes](#7-error-handling-and-failure-modes)
8. [Integration Points](#8-integration-points)

---

## 1. PostgreSQL And ClickHouse Schema

The billing package owns these PostgreSQL tables in the existing `sandbox` database and one
generic ClickHouse metering table in the `forge_metal` database. The database name `sandbox`
is infrastructure truth today; this specification does not introduce a third PostgreSQL
database. The billing package is shared across products even though the backing PostgreSQL
database retains its historical name.

Mutating billing operations may use PostgreSQL transactions for catalog writes, but the
financial invariant no longer depends on PostgreSQL locking. Grant balance is enforced
directly by TigerBeetle on each grant account.

### 1.1 Products

A product is anything you charge for. Each product defines its metering unit and billing model.

```sql
CREATE TABLE products (
    product_id    TEXT PRIMARY KEY,
    display_name  TEXT NOT NULL,
    meter_unit    TEXT NOT NULL,
    billing_model TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

| Column | Constraints | Purpose |
|--------|-------------|---------|
| `product_id` | PK | Stable identifier: `'sandbox'`, `'api'`, `'storefront-hosting'` |
| `meter_unit` | NOT NULL | Primary display unit: `'vcpu_second'`, `'token'`, `'request'`, `'gb_month'`, `'unit'` |
| `billing_model` | NOT NULL | `'metered'`, `'licensed'`, `'one_time'` |

`billing_model` is a product-level invariant. A product does not mix
metered, licensed, and one-time plans under the same `product_id`.

**Billing models**

| Model | Mechanism | TigerBeetle involvement | Example |
|-------|-----------|:-:|---------|
| `metered` | Pay per unit consumed. Credits are reserved and settled in real time. | Yes — `Reserve`/`Renew`/`Settle`/`Void` | Sandbox VMs, token-based APIs |
| `licensed` | Recurring invoice, no usage-based depletion. Access is gated by subscription status. | Yes — Stripe-funded transfer to Revenue on `invoice.paid` | "Pro plan: $20/month for unlimited X" |
| `one_time` | Single purchase, no recurring. Standard Stripe Checkout. | No | Server purchase |

The billing package dispatches on `billing_model`:

- `metered`: online balance enforcement in TigerBeetle plus product-specific grant accounting
- `licensed`: webhook-driven recognition of the recurring invoice in TigerBeetle; no runtime
  balance gating
- `one_time`: Stripe Checkout metadata and webhook correlation only

### 1.2 Plans

A plan is a pricing tier within a product. It defines what the customer gets and what it costs.

```sql
CREATE TABLE plans (
    plan_id                 TEXT PRIMARY KEY,
    product_id              TEXT NOT NULL REFERENCES products(product_id),
    display_name            TEXT NOT NULL,

    -- Stripe Price IDs
    stripe_monthly_price_id TEXT,
    stripe_annual_price_id  TEXT,

    -- Display prices (informational — Stripe is the billing source of truth)
    monthly_price_cents     INTEGER,
    annual_price_cents      INTEGER,

    -- Credit allocation per billing period. Applies only to billing_model='metered'.
    included_credits        BIGINT NOT NULL DEFAULT 0,

    -- First and second rate cards.
    unit_rates              JSONB NOT NULL DEFAULT '{}',
    overage_unit_rates      JSONB NOT NULL DEFAULT '{}',

    -- Quota policies (rate limits, not balance limits)
    quotas                  JSONB NOT NULL DEFAULT '{}',

    -- Cancellation policy
    cancellation_policy     JSONB NOT NULL DEFAULT '{"annual_refund_mode": "credit_note", "void_remaining_credits": false}',

    -- Default pay-as-you-go plan when an org has prepaid grants but no active subscription.
    is_default              BOOLEAN NOT NULL DEFAULT false,

    sort_order              INTEGER NOT NULL DEFAULT 0,
    active                  BOOLEAN NOT NULL DEFAULT true,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_default_plan_per_product
    ON plans (product_id)
    WHERE is_default;
```

`included_credits` is the number of ledger units deposited at the start of each billable
period for a metered subscription. For annual subscriptions, the annual Stripe invoice is
collected up front, but the credit deposit schedule is monthly drip.

`unit_rates` and `overage_unit_rates` are rate cards, not balances:

- `unit_rates` is the rate card applied while the product still has active included credits.
- `overage_unit_rates` is the rate card applied after included credits for that product are
  exhausted.
- Empty `overage_unit_rates` means hard cap: once included credits are exhausted, new
  reservations fail with `ErrInsufficientBalance` unless the org also has eligible prepaid
  grants for that product and the product's default plan is explicitly configured.

`overage_rate` is intentionally not part of this schema. A single integer is not expressive enough for
multi-dimensional pricing such as `{vcpu_second, gib_second}`.

**Future refinement: rate card versioning.** `unit_rates` and `overage_unit_rates` live
directly on `plans` in the initial implementation. When pricing iteration requires auditable
mid-period rate changes, introduce a `plan_rate_versions` table keyed on `(plan_id,
effective_at)` and record the `version_id` on each metering row. Resolution becomes "latest
version where `effective_at <= now()`." The migration is a column move, not a redesign. Until
then, price decreases can be applied immediately (each `Reserve` reads the current row); price
increases should coincide with period boundaries.

**`unit_rates` JSONB**: Per-product metering dimensions. The billing package computes
`sum(allocation[dimension] × rate_card[dimension])`. It does not interpret the semantic
meaning of the keys.

```jsonc
// Sandbox plan
{"vcpu_second": 325, "gib_second": 40}

// API plan
{"token": 7}

// Licensed plan
{}
```

**`quotas` JSONB**: Multi-cadence rate limits. Each quota has a dimension, a limit, and
a window. These are policy limits enforced before any TigerBeetle reservation is attempted.

```json
{
  "limits": [
    {"dimension": "token",          "window": "month",   "limit": 1000000},
    {"dimension": "token",          "window": "4h",      "limit": 50000},
    {"dimension": "concurrent_vms", "window": "instant", "limit": 10},
    {"dimension": "spend_units",    "window": "hour",    "limit": 5000000}
  ]
}
```

Window values:

- `month`: billing-anchor aligned
- `week`: rolling seven days
- `4h`: rolling four hours
- `hour`: rolling one hour
- `instant`: point-in-time, product-supplied, not read from ClickHouse

For rolling windows the generic query shape is:

```sql
SELECT sum(mapGet(dimensions, 'token'))
FROM forge_metal.metering
WHERE org_id = :org_id
  AND product_id = :product_id
  AND started_at >= now() - INTERVAL 4 HOUR
```

### 1.3 Orgs

Identity and Stripe correlation. Billing state lives in `subscriptions`, not here.

```sql
CREATE TABLE orgs (
    org_id              TEXT PRIMARY KEY,
    display_name        TEXT NOT NULL,
    stripe_customer_id  TEXT UNIQUE,
    billing_email       TEXT,
    trust_tier          TEXT NOT NULL DEFAULT 'new',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

`trust_tier` values:

| Tier | Quota behavior | Promotion / demotion rule |
|------|---------------|----------------------------|
| `new` | Conservative ceilings | Default for every org and automatic demotion target |
| `established` | Standard ceilings | Automatic promotion when the daily evaluation finds either 3 successful subscription billings across distinct months with no dispute, or at least `$100` equivalent of paid usage with no dispute |
| `enterprise` | Explicit operator override | Manual only |

Promotion query:

```sql
WITH paid_usage AS (
    SELECT org_id, sum(charge_units - free_tier_units) AS paid_units
    FROM forge_metal.metering
    WHERE started_at >= now() - INTERVAL 180 DAY
    GROUP BY org_id
),
successful_periods AS (
    SELECT org_id, count(DISTINCT date_trunc('month', created_at)) AS paid_months
    FROM billing_events
    WHERE event_type = 'payment_succeeded'
    GROUP BY org_id
),
has_dispute AS (
    SELECT DISTINCT org_id
    FROM billing_events
    WHERE event_type = 'dispute_opened'
)
SELECT o.org_id
FROM orgs o
LEFT JOIN paid_usage pu ON pu.org_id = o.org_id
LEFT JOIN successful_periods sp ON sp.org_id = o.org_id
LEFT JOIN has_dispute hd ON hd.org_id = o.org_id
WHERE o.trust_tier = 'new'
  AND hd.org_id IS NULL
  AND (
        COALESCE(sp.paid_months, 0) >= 3
        OR COALESCE(pu.paid_units, 0) >= 1000000000
      );
```

Demotion rules:

- any `charge.dispute.created` event demotes the org to `new`
- any subscription reaching `status='suspended'` for dunning demotes the org to `new`
- `enterprise` is never set or cleared by the cron

### 1.4 Subscriptions

Tracks which org is on which plan and the state of the billing relationship.

```sql
CREATE TYPE subscription_status AS ENUM (
    'active', 'past_due', 'suspended', 'cancelled', 'trialing'
);

CREATE TYPE billing_cadence AS ENUM ('monthly', 'annual');

CREATE TABLE subscriptions (
    subscription_id         BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    org_id                  TEXT NOT NULL REFERENCES orgs(org_id),
    plan_id                 TEXT NOT NULL REFERENCES plans(plan_id),
    product_id              TEXT NOT NULL REFERENCES products(product_id),

    -- Stripe correlation
    stripe_subscription_id  TEXT UNIQUE,
    stripe_item_id          TEXT,

    -- Billing cadence and timing
    cadence                 billing_cadence NOT NULL DEFAULT 'monthly',
    billing_anchor_day      SMALLINT NOT NULL DEFAULT 1,
    current_period_start    TIMESTAMPTZ,
    current_period_end      TIMESTAMPTZ,

    -- Lifecycle state
    status                  subscription_status NOT NULL DEFAULT 'active',
    status_changed_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    past_due_since          TIMESTAMPTZ,

    -- Overage ceiling (ledger units per billing period, org-admin-configurable)
    -- NULL = no org-specific ceiling (plan overage behavior applies without limit)
    -- 0    = org has self-disabled overage
    -- >0   = cap overage consumption at this many ledger units per period
    overage_cap_units       BIGINT CHECK (overage_cap_units >= 0),

    -- Cancellation
    cancel_at_period_end    BOOLEAN NOT NULL DEFAULT false,
    cancelled_at            TIMESTAMPTZ,
    cancellation_reason     TEXT,

    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_one_active_sub_per_product
    ON subscriptions (org_id, product_id)
    WHERE status IN ('active', 'past_due', 'trialing');
```

`product_id` is denormalized from `plans.product_id` because "what is this org's active
subscription for product X?" is on the hot path for every billing decision.

**Dunning state machine**

```
                 ┌──────── invoice.paid ────────┐
                 ▼                               │
trialing ──→ active ──→ past_due ──→ suspended ──┼─→ cancelled
               ▲                 │               │
               └──── invoice.paid┘               │
                                operator action ─┘
```

forge-metal requires the following Stripe Dashboard configuration:

- **Billing > Revenue recovery > Retries**: Smart Retries enabled
- retry policy: `8` attempts within `2 weeks` unless an operator intentionally narrows it
- terminal action after the retry window: **Cancel the subscription**

With that account-level setting:

- every failed attempt emits `invoice.payment_failed`; `attempt_count` increments on each
  failure/update
- eventual success emits `invoice.paid`
- exhausted retries emit `customer.subscription.deleted` because the terminal action is pinned
  to cancellation

If the Stripe account is configured to leave subscriptions `past_due` or mark them `unpaid`
instead, this specification is no longer correct without amendment.

Annual billing uses the same schema with `cadence='annual'`. The annual Stripe invoice is
collected up front, but metered credit deposits are still monthly drip. This limits operator
exposure if the annual charge is disputed.

**Cancellation semantics**

| Type | Behavior |
|------|----------|
| Graceful (`cancel_at_period_end = true`) | Service continues until period end. No new credit deposits after period end. No refund. |
| Immediate | `status = 'cancelled'` now. Refunds for an already-paid annual invoice are issued via `POST /v1/credit_notes`; `DELETE /v1/subscriptions/{id}` with `prorate=true` alone is not the accounting source of truth for the refund. Remaining credits are optionally voided. |

### 1.5 Credit Grants

Each `credit_grants` row is an immutable catalog entry for one TigerBeetle grant account. The
central rule is:

> TigerBeetle answers "how much remains on this grant right now?" PostgreSQL answers
> "which grants are eligible to fund this product, and in what order?"

There is no aggregate per-org balance account. TigerBeetle is the single source of financial
truth. PostgreSQL no longer stores mutable consumption counters.

```sql
CREATE TABLE credit_grants (
    grant_id            TEXT PRIMARY KEY,  -- ULID, application-generated
    org_id              TEXT NOT NULL REFERENCES orgs(org_id),
    product_id          TEXT NOT NULL REFERENCES products(product_id),

    -- Original funded amount (ledger units)
    amount              BIGINT NOT NULL CHECK (amount > 0),

    -- Source attribution
    source              TEXT NOT NULL,
    stripe_reference_id TEXT,
    subscription_id     BIGINT REFERENCES subscriptions(subscription_id),  -- nullable; only set for subscription/free_tier grants

    -- Lifecycle
    period_start        TIMESTAMPTZ,
    period_end          TIMESTAMPTZ,
    expires_at          TIMESTAMPTZ,
    closed_at           TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_credit_grants_active
    ON credit_grants (org_id, product_id, expires_at)
    WHERE closed_at IS NULL;

CREATE UNIQUE INDEX idx_credit_grants_subscription_period
    ON credit_grants (subscription_id, period_start)
    WHERE subscription_id IS NOT NULL;
```

Required source meanings:

| `source` | Meaning |
|----------|---------|
| `subscription` | Included credits on a paid metered subscription |
| `purchase` | Product-specific prepaid top-up |
| `promo` | Promotional credit funded by `PromoPool` |
| `refund` | Refund credit or manual make-good tied to an invoice |
| `free_tier` | Monthly free-plan allowance funded by `FreeTierPool` |

The billing system keeps grant state aligned by construction:

1. **Grant creation**: generate `grant_id` (ULID), insert the immutable `credit_grants`
   row in PostgreSQL (serialization point), then create the TigerBeetle grant account and
   fund it. PostgreSQL is written first, TigerBeetle last.
2. **Grant consumption**: `Reserve`, `Settle`, `Renew`, `HandleDispute`, and
   `ExpireCredits` operate directly on the grant account that the row names via `grant_id`.
3. **Grant closure**: expiry or dispute workflows drain the grant account, then set
   `closed_at` when the account is no longer active.

Eligibility sets:

- free-tier phase: `source='free_tier'`
- included phase: `source='subscription'`
- overage/prepaid phase: `source IN ('purchase', 'promo', 'refund')`

Expiration policies:

| Source | Default expiration | Rationale |
|--------|-------------------|-----------|
| `subscription` | Period end + 30 days grace | Subscription credits do not roll forever |
| `purchase` | 12 months from purchase | Breakage accounting and customer expectation |
| `promo` | 90 days | Promotional urgency |
| `free_tier` | End of month | Monthly allowance |
| `refund` | Same as original grant | Refund credits inherit the original expiry policy |

One TigerBeetle account per grant:

- `DebitsMustNotExceedCredits` enforces each grant's ceiling directly in TigerBeetle.
- No PostgreSQL-side serialization is required on the financial hot path; TigerBeetle
  serializes concurrent debits atomically.
- `consumed` is derived from `debits_posted` on the grant account.
- `remaining` is derived from `credits_posted - debits_posted - debits_pending`.
- Reconciliation no longer needs to repair grant drift between PostgreSQL counters and
  TigerBeetle. The grant account itself is the balance.

`grant_id` is an application-generated ULID (128-bit, time-ordered). The ULID is mapped to a
TigerBeetle `Uint128` by swapping its big-endian halves into the little-endian layout:
`GrantAccountID(grantID)` places `BE.Uint64(ulid[0:8])` (timestamp + random head) in the
high u64 and `BE.Uint64(ulid[8:16])` (random tail) in the low u64. `CreditExpiryID(grantID)`
uses the same half-swap in the transfer ID namespace. This places the ULID's 48-bit timestamp
in the high u64 where TigerBeetle's LSM tree benefits from monotonic ordering. Because the ID
is application-generated, not database-generated, the grant's TigerBeetle identity is
decoupled from PostgreSQL's internal sequence state.

### 1.6 Org Pricing Overrides

Per-org pricing overrides for enterprise customers.

```sql
CREATE TABLE org_pricing_overrides (
    org_id       TEXT NOT NULL REFERENCES orgs(org_id),
    plan_id      TEXT NOT NULL REFERENCES plans(plan_id),
    unit_rates   JSONB NOT NULL,
    quotas       JSONB,
    notes        TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, plan_id)
);
```

The orchestrator reads pricing as:
`COALESCE(org_pricing_overrides.unit_rates, plans.unit_rates)`.

### 1.7 Tasks (with Retry/DLQ)

Async work queue with retry semantics and dead-letter visibility.

```sql
CREATE TYPE task_status AS ENUM ('pending', 'claimed', 'completed', 'retrying', 'dead');

CREATE TABLE tasks (
    task_id         BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    task_type       TEXT NOT NULL,
    payload         JSONB NOT NULL DEFAULT '{}',
    status          task_status NOT NULL DEFAULT 'pending',

    -- Idempotency
    idempotency_key TEXT UNIQUE,

    -- Retry state
    attempts        INTEGER NOT NULL DEFAULT 0,
    max_attempts    INTEGER NOT NULL DEFAULT 5,
    last_error      TEXT,
    next_retry_at   TIMESTAMPTZ,

    -- Timing
    scheduled_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    dead_at         TIMESTAMPTZ,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_tasks_claimable
    ON tasks (scheduled_at)
    WHERE status IN ('pending', 'retrying')
      AND (next_retry_at IS NULL OR next_retry_at <= now());

CREATE INDEX idx_tasks_dead
    ON tasks (dead_at)
    WHERE status = 'dead';
```

`idempotency_key` accepts:

- Stripe payment intent IDs (`pi_...`)
- Stripe invoice IDs (`in_...`)
- Stripe dispute IDs (`dp_...`)
- application-generated compound keys such as `trust_tier_evaluate:2026-04-04`

Retry backoff is exponential: `5s, 10s, 20s, 40s, 80s`.

Smart Retries and task retries solve different problems:

- Smart Retries: Stripe retries payment collection from the customer
- task retries: forge-metal retries its own side effects after Stripe has already emitted an event

**Worker claim query**

```sql
UPDATE tasks
SET status = 'claimed', claimed_at = now(), attempts = attempts + 1
WHERE task_id = (
    SELECT task_id
    FROM tasks
    WHERE status IN ('pending', 'retrying')
      AND (next_retry_at IS NULL OR next_retry_at <= now())
    ORDER BY scheduled_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING *;
```

**On failure**

```sql
UPDATE tasks
SET status = 'retrying',
    last_error = :error,
    next_retry_at = now() + make_interval(secs => power(2, attempts) * 5)
WHERE task_id = :task_id AND attempts < max_attempts;

UPDATE tasks
SET status = 'dead', last_error = :error, dead_at = now()
WHERE task_id = :task_id AND attempts >= max_attempts;
```

**DLQ operations**

```sql
UPDATE tasks
SET status = 'retrying', next_retry_at = now(),
    max_attempts = max_attempts + 3, dead_at = NULL
WHERE task_id = :task_id AND status = 'dead';

UPDATE tasks
SET status = 'completed', completed_at = now(),
    last_error = last_error || E'\n[resolved] ' || :resolution_note
WHERE task_id = :task_id AND status = 'dead';
```

Normative task types and payload contracts:

| `task_type` | `idempotency_key` | Payload keys |
|-------------|-------------------|--------------|
| `stripe_purchase_deposit` | Stripe PaymentIntent ID | `org_id`, `product_id`, `stripe_payment_intent_id`, `amount_ledger_units`, `expires_at` |
| `stripe_subscription_credit_deposit` | Stripe invoice ID | `org_id`, `product_id`, `subscription_id`, `stripe_invoice_id`, `amount_ledger_units`, `period_start`, `period_end`, `expires_at`, `source` |
| `stripe_licensed_charge` | Stripe invoice ID | `org_id`, `product_id`, `subscription_id`, `stripe_invoice_id`, `amount_ledger_units`, `period_start`, `period_end` |
| `stripe_dispute_debit` | Stripe dispute ID | `org_id`, `stripe_dispute_id`, `stripe_payment_intent_id`, `amount_ledger_units` |
| `trust_tier_evaluate` | `trust_tier_evaluate:<date>` | `as_of_date` |

### 1.8 Billing Events

Audit log of billing decisions. This is not the financial ledger; TigerBeetle is.

```sql
CREATE TABLE billing_events (
    event_id        BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    org_id          TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    subscription_id BIGINT,
    grant_id        TEXT,
    task_id         BIGINT,
    payload         JSONB NOT NULL DEFAULT '{}',
    stripe_event_id TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_billing_events_stripe
    ON billing_events (stripe_event_id)
    WHERE stripe_event_id IS NOT NULL;

CREATE UNIQUE INDEX idx_billing_events_credits_deposited_grant
    ON billing_events (grant_id)
    WHERE event_type = 'credits_deposited' AND grant_id IS NOT NULL;

CREATE INDEX idx_billing_events_org
    ON billing_events (org_id, created_at);
```

`event_type` values:

- `subscription_created`
- `subscription_cancelled`
- `payment_succeeded`
- `payment_failed`
- `credits_deposited`
- `credits_expired`
- `dispute_opened`
- `dispute_won`
- `dispute_lost`
- `plan_changed`
- `org_suspended`
- `quota_exceeded`
- `overage_ceiling_hit`
- `licensed_charge_recorded`
- `trust_tier_promoted`
- `trust_tier_demoted`
- `reconciliation_alert`

### 1.9 Billing Cursors

Periodic jobs need durable watermarks. The billing package owns one generic cursor table.

```sql
CREATE TABLE billing_cursors (
    cursor_name   TEXT PRIMARY KEY,
    cursor_ts     TIMESTAMPTZ,
    cursor_bigint BIGINT,
    cursor_json   JSONB NOT NULL DEFAULT '{}',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Named cursors used by this specification:

| `cursor_name` | Meaning |
|---------------|---------|
| `metering_reconcile_high_watermark` | Highest metering timestamp fully reconciled |
| `trust_tier_daily_eval` | Most recent date for which trust-tier evaluation completed |
| `credit_expiry_sweep` | Most recent successful expiry sweep |

### 1.10 ClickHouse Metering

There is one generic append-only table: `forge_metal.metering`.

```sql
CREATE TABLE forge_metal.metering (
    org_id               LowCardinality(String)               CODEC(ZSTD(3)),
    actor_id             String DEFAULT ''                    CODEC(ZSTD(3)),
    product_id           LowCardinality(String)               CODEC(ZSTD(3)),
    source_type          LowCardinality(String)               CODEC(ZSTD(3)),
    source_ref           String                               CODEC(ZSTD(3)),
    window_seq           UInt32                               CODEC(Delta(4), ZSTD(3)),
    started_at           DateTime64(6)                        CODEC(DoubleDelta, ZSTD(3)),
    ended_at             DateTime64(6)                        CODEC(DoubleDelta, ZSTD(3)),
    billed_seconds       UInt32                               CODEC(Delta(4), ZSTD(3)),
    pricing_phase        LowCardinality(String)               CODEC(ZSTD(3)),
    dimensions           Map(LowCardinality(String), Float64) CODEC(ZSTD(3)),
    charge_units         UInt64                               CODEC(T64, ZSTD(3)),
    free_tier_units      UInt64                               CODEC(T64, ZSTD(3)),
    subscription_units   UInt64                               CODEC(T64, ZSTD(3)),
    purchase_units       UInt64                               CODEC(T64, ZSTD(3)),
    promo_units          UInt64                               CODEC(T64, ZSTD(3)),
    refund_units         UInt64                               CODEC(T64, ZSTD(3)),
    recorded_at          DateTime64(6) DEFAULT now64(6)      CODEC(DoubleDelta, ZSTD(3))
) ENGINE = MergeTree()
ORDER BY (org_id, product_id, started_at, source_ref, window_seq);
```

Required invariants:

- `charge_units = free_tier_units + subscription_units + purchase_units + promo_units + refund_units`
- `started_at <= ended_at`
- rows are immutable after insert
- `pricing_phase` is one of `free_tier`, `included`, `overage`, `licensed`
- `actor_id` is the Zitadel `sub` claim of the user who initiated the usage. Empty string
  for system-initiated actions (cron deposits, licensed period records). Not in the primary
  key — it is attribution metadata, not a partitioning dimension.

`source_type` identifies the product-level unit being metered:

- `job` for Firecracker VM executions
- `request_batch` for API request windows
- `licensed_period` for licensed products

`source_ref` is opaque to the billing package. For sandbox it is the decimal string form of
`job_id`.

### 1.11 Jobs (product-specific)

The `jobs` table stays product-specific. The billing package never owns product execution
state directly. Each product type may keep its own control-plane tables:

- `jobs` for sandbox
- `api_batches` or equivalent for an API product
- `orders` for storefront

The billing package only requires a mapping from that product-specific state to
`source_ref`, `window_seq`, `dimensions`, `charge_units`, `started_at`, and `ended_at`.

---

## 2. Package API Surface

Import path: `github.com/forge-metal/billing`

### 2.1 Dependencies

```go
import (
    "context"
    "database/sql"
    "time"

    tb "github.com/tigerbeetle/tigerbeetle-go"
    tberrors "github.com/tigerbeetle/tigerbeetle-go/pkg/errors"
    "github.com/tigerbeetle/tigerbeetle-go/pkg/types"
    "github.com/stripe/stripe-go/v85"
    "github.com/stripe/stripe-go/v85/webhook"
)
```

Normative versions:

- TigerBeetle Go SDK: `v0.16.78`
- Stripe Go SDK: `v85.0.0`
- rapid: `v1.2.0`
- ulid: `v2.1.0`

### 2.2 ID Types

Reproduced from the RFC.

```go
type OrgID  uint64
type JobID  int64
type TaskID int64
type GrantID [16]byte  // ULID, 128-bit, time-ordered

type AccountID  struct{ raw types.Uint128 }
type TransferID struct{ raw types.Uint128 }

type OperatorAcctType uint16
const (
    AcctRevenue         OperatorAcctType = 3
    AcctFreeTierPool    OperatorAcctType = 4
    AcctStripeHolding   OperatorAcctType = 5
    AcctPromoPool       OperatorAcctType = 6
    AcctFreeTierExpense OperatorAcctType = 7
    AcctExpiredCredits  OperatorAcctType = 8
)

type XferKind uint8
const (
    KindReservation         XferKind = 1
    KindSettlement          XferKind = 2
    KindVoid                XferKind = 3
    KindFreeTierReset       XferKind = 4
    KindStripeDeposit       XferKind = 5
    KindSubscriptionDeposit XferKind = 6
    KindPromoCredit         XferKind = 7
    KindDisputeDebit        XferKind = 8
    KindCreditExpiry        XferKind = 9
)

type PricingPhase string
const (
    PricingPhaseFreeTier PricingPhase = "free_tier"
    PricingPhaseIncluded PricingPhase = "included"
    PricingPhaseOverage  PricingPhase = "overage"
    PricingPhaseLicensed PricingPhase = "licensed"
)

type GrantSourceType uint8
const (
    SourceFreeTier     GrantSourceType = 1
    SourceSubscription GrantSourceType = 2
    SourcePurchase     GrantSourceType = 3
    SourcePromo        GrantSourceType = 4
    SourceRefund       GrantSourceType = 5
)
```

ULID generation uses `github.com/oklog/ulid/v2` with a monotonic entropy source:

```go
import "github.com/oklog/ulid/v2"

func NewGrantID() GrantID {
    return GrantID(ulid.Make())
}
```

Grant-related ID derivation swaps the ULID's big-endian halves into TigerBeetle's
little-endian `Uint128` layout, placing the timestamp in the high u64 where the LSM tree
benefits from it:

- `GrantAccountID(grantID)`: ULID bytes 0:8 (timestamp + random head) become the high u64;
  bytes 8:16 (random tail) become the low u64. The numeric value differs from the ULID's
  big-endian interpretation, but the mapping is bijective and the reverse function recovers
  the original ULID.
- `CreditExpiryID(grantID)`: same half-swap into a `TransferID`. Account IDs and transfer
  IDs are separate TigerBeetle namespaces, so the same numeric value in both is safe. The
  transfer's `code` field stores `KindCreditExpiry` for type discrimination.

Other ID derivation functions use the packed transfer-ID layout defined in the RFC:
`VMTransferID`, `SubscriptionPeriodID`, `StripeDepositID`. These use `BIGINT` source IDs
(job_id, subscription_id, task_id) in the high 64 bits.

`SubscriptionPeriodID(subscriptionID, periodStartUTC, kind)` uses `source_id = subscription_id`
in the high 64 bits, `seq = year*12+month` from `periodStart.UTC()`,
`kind = KindFreeTierReset` or `KindSubscriptionDeposit`, `grant_idx = 0`.

Task-driven transfer kinds (`stripe_deposit`, `promo_credit`, `dispute_debit`) use the
`StripeDepositID(taskID, kind)` pattern. Periodic subscription grants do not.

### 2.3 Client

```go
type Client struct {
    tb     tb.Client
    pg     *sql.DB
    stripe *stripe.Client
    cfg    Config
    clock  func() time.Time
}

// NewClient constructs a billing Client.
//
// On construction:
//   - Creates operator accounts in TigerBeetle (idempotent):
//     Revenue, FreeTierPool, StripeHolding, PromoPool,
//     FreeTierExpense, ExpiredCredits (6 accounts)
//   - Validates config
//
// Preconditions:
//   - tb is a connected TigerBeetle client
//   - pg is an open *sql.DB pointed at the sandbox database
//   - stripe is initialized with a valid API key
//   - cfg passes validation
//
// Idempotency: Safe to call multiple times.
func NewClient(tb tb.Client, pg *sql.DB, sc *stripe.Client, cfg Config) (*Client, error)
```

### 2.4 Org Lifecycle

#### EnsureOrg

```go
// EnsureOrg provisions an org in PostgreSQL.
//
// Postconditions:
//   - PostgreSQL orgs table has a row for orgID (INSERT ... ON CONFLICT DO NOTHING)
//
// Idempotency: Fully idempotent.
func (c *Client) EnsureOrg(ctx context.Context, orgID OrgID, displayName string) error
```

#### GetOrgBalance

```go
type Balance struct {
    FreeTierAvailable  uint64
    FreeTierPending    uint64
    CreditAvailable    uint64
    CreditPending      uint64
    TotalAvailable     uint64
}

// GetOrgBalance reads the current balance from TigerBeetle.
//
// Flow:
//   1. SELECT grant_id, source FROM credit_grants
//      WHERE org_id = $1 AND closed_at IS NULL
//   2. Batch LookupAccounts on GrantAccountID(grant_id)
//   3. Sum available/pending for free-tier grants vs. all other grants
//
// Read-only, always safe.
func (c *Client) GetOrgBalance(ctx context.Context, orgID OrgID) (Balance, error)
```

#### GetProductBalance

```go
type ProductBalance struct {
    ProductID         string
    FreeTierRemaining uint64
    IncludedRemaining uint64
    PrepaidRemaining  uint64
}

// GetProductBalance reads product-specific grant state from PostgreSQL and the
// corresponding TigerBeetle grant accounts.
//
// Reserve does not depend on this helper; it re-runs the authoritative grant
// eligibility checks when it builds the waterfall.
func (c *Client) GetProductBalance(ctx context.Context, orgID OrgID, productID string) (ProductBalance, error)
```

#### SuspendOrg / SuspendSubscription

```go
// SuspendOrg suspends ALL active subscriptions for an org.
// Used for dispute-driven suspension where all access should be revoked.
//
// Sets subscriptions.status = 'suspended' for all active/past_due subscriptions.
// Future Reserve calls for any product return ErrOrgSuspended.
func (c *Client) SuspendOrg(ctx context.Context, orgID OrgID, reason string) error

// SuspendSubscription suspends a single subscription.
// Used for product-specific suspension (e.g., quota abuse on one product).
func (c *Client) SuspendSubscription(ctx context.Context, subscriptionID int64, reason string) error
```

### 2.5 Metered Billing (Product-Agnostic)

#### Reservation Type

```go
type Reservation struct {
    JobID       JobID
    OrgID       OrgID
    ProductID   string
    PlanID      string
    ActorID     string
    WindowSeq   uint32
    WindowSecs  uint32
    WindowStart time.Time
    PricingPhase PricingPhase

    // Resource allocation (product-specific dimensions)
    Allocation  map[string]float64  // {"vcpu": 2, "gib": 0.5}

    // Resolved rates (from plan or org override)
    UnitRates   map[string]uint64   // {"vcpu_second": 325, "gib_second": 40}

    // Per-second cost in ledger units
    CostPerSec  uint64

    // Pending transfer state
    GrantLegs []GrantLeg
}
```

#### CheckQuotas

```go
type QuotaResult struct {
    Allowed    bool
    Violations []QuotaViolation
}

type QuotaViolation struct {
    Dimension string
    Window    string
    Limit     uint64
    Current   uint64
}

// CheckQuotas verifies that an org's usage is within plan-defined quota limits.
//
// Checks all quota windows for the product by querying ClickHouse metering tables.
// Quota checks gate access; balance checks gate payment. Both must pass.
// Rolling windows come from ClickHouse. `instant` windows are supplied by the
// caller in the `usage` map and are compared in-process.
//
// Preconditions:
//   - org has an active subscription for productID, or is using prepaid credits
//
// Error conditions:
//   - ClickHouse unavailable: returns error (fail closed — no quota check = no access)
//   - PostgreSQL unavailable: returns error (can't read plan quotas)
//
// Idempotency: Read-only, always safe.
func (c *Client) CheckQuotas(ctx context.Context, orgID OrgID, productID string, usage map[string]float64) (QuotaResult, error)
```

The orchestrator calls `CheckQuotas` before `Reserve`:

```
1. CheckQuotas → any window exceeded? → 429 Too Many Requests
2. Reserve → sufficient balance? → 402 Payment Required
3. Boot VM
```

#### Reserve

```go
type ReserveRequest struct {
    JobID      JobID
    OrgID      OrgID
    ProductID  string
    ActorID    string              // Zitadel sub claim; written to metering row
    Allocation map[string]float64  // {"vcpu": 2, "gib": 0.5}
}

// Reserve initiates a billing reservation for a metered product.
//
// Flow:
//   1. Read the active subscription for ProductID, if any.
//   2. SELECT grant_id, source, expires_at FROM credit_grants
//      WHERE org_id = :org AND product_id = :product AND closed_at IS NULL
//      ORDER BY eligibility priority, expires_at ASC, grant_id ASC
//   3. Batch LookupAccounts on those grant IDs.
//   4. Determine the pricing phase:
//      a. free_tier if sum(available free-tier grants) > 0
//      b. included if sum(available subscription grants) > 0
//      c. overage if the active plan has non-empty overage_unit_rates
//      d. else ErrInsufficientBalance
//   5. Resolve the rate card for that phase, applying org_pricing_overrides if present.
//   6. Compute costPerSec = sum(allocation[dim] × resolvedRateCard[dim]).
//   7. Compute windowCost = costPerSec × ReservationWindowSecs.
//   8. If the pricing phase is overage and subscription.overage_cap_units is non-NULL,
//      query current-period overage consumption from ClickHouse:
//        SELECT sum(charge_units) FROM forge_metal.metering
//        WHERE org_id = :org_id AND product_id = :product_id
//          AND pricing_phase = 'overage'
//          AND started_at >= :current_period_start
//      If currentOverageUnits + windowCost > overage_cap_units, return ErrOverageCeilingExceeded.
//   9. Reserve windowCost in TigerBeetle by walking the eligible grant waterfall:
//      for i, grant := range eligibleGrants {
//          submit pending balancing_debit using
//          VMTransferID(jobID, windowSeq, i, KindReservation)
//          lookup transfer, read clamped amount, decrement remainder
//          append GrantLeg{GrantID, TransferID, Amount}
//          break if remainder == 0
//      }
//  10. If remainder > 0, void all pending legs and return ErrInsufficientBalance.
//
// Rate selection is per reservation window. The billing package does not split a
// single window across included and overage phases. If included credits exhaust
// during the current window, the new pricing phase applies on the next boundary.
//
// Preconditions:
//   - orgID provisioned via EnsureOrg
//   - org not suspended
//   - Allocation keys must have corresponding entries in the resolved rate card
//
// Postconditions on success:
//   - 1..N pending TigerBeetle transfers reserving funds for one window
//   - Reservation populated with the chosen PricingPhase, PlanID, ActorID, and GrantLegs
//
// Error conditions:
//   - ErrOrgSuspended: subscription status is 'suspended'
//   - ErrNoActiveSubscription: no active subscription and no default pay-as-you-go plan
//   - ErrInsufficientBalance: rate card resolved but the eligible grant waterfall cannot cover the window
//   - ErrOverageCeilingExceeded: overage consumption would exceed subscription.overage_cap_units
//   - ErrDimensionMismatch: allocation key not found in unit_rates
//   - TigerBeetle unavailable: transport error
//   - ClickHouse unavailable during overage ceiling check: fail closed (503)
//
// Idempotency: Transfer IDs are deterministic from (JobID, WindowSeq, grant_idx, Kind).
func (c *Client) Reserve(ctx context.Context, req ReserveRequest) (Reservation, error)
```

#### Renew, Settle, Void

```go
func (c *Client) Renew(ctx context.Context, reservation *Reservation, actualSeconds uint32) error
func (c *Client) Settle(ctx context.Context, reservation *Reservation, actualSeconds uint32) error
func (c *Client) Void(ctx context.Context, reservation *Reservation) error
```

Normative semantics:

- `Renew`: settle the current window, re-read the active grant catalog, then reserve the next
  window. `Renew` may therefore change `reservation.PricingPhase`.
- `Settle`: post each pending grant leg for the actual cost and write the
  `forge_metal.metering` row (including `actor_id` from the reservation). No PostgreSQL
  consumption counter is updated.
- `Void`: cancel each pending grant leg.

### 2.6 Credit Management

#### DepositCredits

```go
type CreditGrant struct {
    OrgID          OrgID
    ProductID      string
    Amount         uint64      // ledger units
    Source         string      // "subscription", "purchase", "promo", "free_tier", "refund"
    StripeReferenceID string   // Stripe payment intent or invoice ID
    SubscriptionID *int64      // non-nil for subscription-sourced grants
    PeriodStart    *time.Time  // billing period this grant covers
    PeriodEnd      *time.Time
    ExpiresAt      *time.Time  // nil = never expires
}

// DepositCredits inserts the PostgreSQL catalog row first (serialization point),
// then creates the TigerBeetle grant account and funds it.
//
// Ordering rationale (Write Last, Read First): PostgreSQL is the system of
// reference; TigerBeetle is the system of record. Writing PostgreSQL first
// ensures that a crash between the two systems leaves a catalog row with no
// funded account (detectable, retryable) rather than a funded account with no
// catalog row (orphan money).
//
// Flow:
//   1. Generate grant_id = NewGrantID() (ULID)
//   2. INSERT INTO credit_grants (...) the immutable catalog row.
//      For subscription/free_tier sources, the unique index on
//      (subscription_id, period_start) serves as the race arbiter:
//      ON CONFLICT DO NOTHING. If no row is returned, another writer won
//      the race — return early with no error.
//   3. Create TigerBeetle account for GrantAccountID(grantID) with
//      DebitsMustNotExceedCredits and History
//   4. Post funding transfer from the source operator account to
//      GrantAccountID(grantID)
//   5. Log billing event
//
// Error conditions:
//   - PostgreSQL unavailable: returns error (no grant row created, no TB mutation)
//   - TigerBeetle unavailable after PG insert: grant row exists but account
//     is unfunded. Same grant_id on retry produces the same TigerBeetle
//     account ID (deterministic from ULID). Reconciliation detects and alerts.
//
// Idempotency:
//   - subscription / free_tier: PostgreSQL unique index on
//     (subscription_id, period_start) prevents duplicate grants.
//     TigerBeetle transfer ID derived from
//     SubscriptionPeriodID(*grant.SubscriptionID, *grant.PeriodStart, kind).
//   - purchase / promo / refund: task idempotency_key prevents duplicate
//     tasks. TigerBeetle transfer ID derived from *taskID.
//     Grant ULID is deterministic per task execution (generated once,
//     persisted in the catalog row on first attempt, reused on retry).
//
// Preconditions:
//   - taskID must be non-nil for purchase, promo, and refund sources
//   - SubscriptionID and PeriodStart must be non-nil for subscription and free_tier sources
func (c *Client) DepositCredits(ctx context.Context, taskID *TaskID, grant CreditGrant) error
```

#### RecordLicensedCharge

```go
type LicensedCharge struct {
    OrgID           OrgID
    ProductID       string
    SubscriptionID  int64
    StripeInvoiceID string
    Amount          uint64
    PeriodStart     time.Time
    PeriodEnd       time.Time
}

// RecordLicensedCharge recognizes a recurring licensed invoice in TigerBeetle.
//
// Postconditions:
//   - TigerBeetle transfer: debit StripeHolding → credit Revenue
//   - billing_events row logged with event_type='licensed_charge_recorded'
//   - no credit_grants row is created
//
// Idempotency: Transfer ID derived from taskID with KindSubscriptionDeposit.
func (c *Client) RecordLicensedCharge(ctx context.Context, taskID TaskID, charge LicensedCharge) error
```

#### ExpireCredits

```go
type ExpireResult struct {
    GrantsChecked  int
    GrantsExpired  int
    GrantsFailed   int
    UnitsExpired   uint64
    Errors         []error
}

// ExpireCredits sweeps expired credit grants.
//
// For each grant where expires_at <= now() AND closed_at IS NULL:
//   1. BalancingDebit from GrantAccountID(grantID) into:
//      - FreeTierExpense for free_tier grants
//      - ExpiredCredits for every other grant source
//   2. Close the TigerBeetle grant account
//   3. UPDATE credit_grants SET closed_at = now()
//   4. Log billing event
//
// Error conditions:
//   - PostgreSQL unavailable: returns error immediately
//   - Individual TigerBeetle failures: accumulated in Errors, processing continues
//
// Idempotency: Transfer ID = CreditExpiryID(grantID) = the grant's ULID. One expiry
// transfer per grant. TigerBeetle returns TransferExists on replay. Safe to re-run.
func (c *Client) ExpireCredits(ctx context.Context) (ExpireResult, error)
```

### 2.7 Payments

#### HandleDispute

```go
// HandleDispute processes a chargeback by debiting the grant(s) funded by the
// disputed Stripe payment first.
//
// Postconditions:
//   - TigerBeetle transfer(s): debit the original disputed grant(s) first,
//     then other eligible grants in reverse waterfall order if needed,
//     crediting StripeHolding
//   - If org's credit balance insufficient to cover dispute: all org
//     subscriptions suspended via SuspendOrg
//   - billing_events row logged with event_type='dispute_opened'
//   - credit_grants remains immutable; dispute attribution comes from
//     stripe_reference_id and TigerBeetle transfer history
//
// Idempotency: Transfer ID derived from taskID + KindDisputeDebit.
func (c *Client) HandleDispute(ctx context.Context, orgID OrgID, taskID TaskID, disputeAmount uint64) error
```

#### VerifyWebhook

Thin wrapper around `webhook.ConstructEvent(payload, signature, secret)`.

```go
func VerifyWebhook(payload []byte, signature string, secret string) (stripe.Event, error)
```

#### CancelSubscription

```go
type CancelSubscriptionRequest struct {
    SubscriptionID        int64
    Immediate             bool
    RefundAnnualProration bool
    VoidRemainingCredits  bool
}

// CancelSubscription performs operator-initiated cancellation.
//
// Immediate annual cancellation with refund uses:
//   1. POST /v1/credit_notes against the finalized annual invoice
//   2. DELETE /v1/subscriptions/{id} with prorate=false
//   3. Optional drain and closure of remaining active grant accounts
//
// Graceful cancellation uses:
//   1. POST /v1/subscriptions/{id} with cancel_at_period_end=true
//
// The cancellation path never relies on Stripe proration alone as the refund
// ledger artifact.
func (c *Client) CancelSubscription(ctx context.Context, req CancelSubscriptionRequest) error
```

### 2.8 Periodic Operations

#### DepositSubscriptionCredits

```go
type DepositResult struct {
    SubscriptionsProcessed int
    CreditsDeposited       int
    CreditsSkipped         int   // already deposited for this period
    CreditsFailed          int
    Errors                 []error
}

// DepositSubscriptionCredits deposits included credits for all active subscriptions
// whose current period has started.
//
// For each active subscription with included_credits > 0:
//   1. Check if a credit_grants row already exists for this (subscription_id, period_start)
//   2. If not:
//      - free plans: DepositCredits(taskID=nil, source='free_tier')
//      - paid metered plans: DepositCredits(taskID=nil, source='subscription')
//   3. Each deposit creates a fresh TigerBeetle grant account for that period
//
// This runs monthly for both monthly and annual subscriptions (monthly drip).
// Annual subscriptions receive 1/12th of yearly credits each month.
//
// Idempotency: Grant existence check prevents double-deposit per period.
// TigerBeetle transfer IDs derived from
// SubscriptionPeriodID(subscription_id, period_start.UTC(), kind).
func (c *Client) DepositSubscriptionCredits(ctx context.Context) (DepositResult, error)
```

#### Reconcile

```go
func (c *Client) Reconcile(ctx context.Context, ch ClickHouseQuerier) (ReconcileResult, error)
```

Reconciliation compares:

- every active PostgreSQL grant row against an open TigerBeetle grant account
- TigerBeetle settled transfers against ClickHouse metering rows
- TigerBeetle grant accounts against PostgreSQL catalog rows to detect orphans

It does not call Stripe usage-record endpoints. Stripe is not the metering source of truth.
There is no separate grant-drift detector because PostgreSQL no longer stores mutable grant
balances.

#### EvaluateTrustTiers

```go
type TrustTierResult struct {
    OrgPromoted int
    OrgDemoted  int
    Errors      []error
}

// EvaluateTrustTiers runs the deterministic trust-tier promotion query and
// writes any resulting org mutations plus billing_events rows.
func (c *Client) EvaluateTrustTiers(ctx context.Context) (TrustTierResult, error)
```

### 2.9 Checkout

```go
// CreateCheckoutSession creates a Stripe Checkout session for one-time credit purchase.
//
// The product_name is used as the line item description (e.g., "Sandbox Credits",
// "API Credits"). The billing package reads the product's display_name from PostgreSQL.
//
// Postconditions:
//   - POST /v1/checkout/sessions via stripe.Client.V1CheckoutSessions.Create
//   - mode='payment'
//   - customer_creation='always'
//   - payment_method_options.card.request_three_d_secure='any'
//   - Metadata includes org_id and product_id for webhook correlation
//
// Returns the Checkout session URL for redirect.
func (c *Client) CreateCheckoutSession(ctx context.Context, orgID OrgID, productID string, params CheckoutParams) (string, error)

type CheckoutParams struct {
    AmountCents int64
    SuccessURL  string
    CancelURL   string
}

// CreateSubscription creates a Stripe Checkout session for subscription signup.
//
// Reads stripe_monthly_price_id or stripe_annual_price_id from the plans table
// based on the requested cadence.
// Uses POST /v1/checkout/sessions with mode='subscription'.
//
// Returns the Checkout session URL.
func (c *Client) CreateSubscription(ctx context.Context, orgID OrgID, planID string, cadence billing_cadence) (string, error)
```

---

## 3. TigerBeetle Integration Details

This section documents the exact SDK usage patterns for `github.com/tigerbeetle/tigerbeetle-go`
at `v0.16.78`.

Validated upstream facts:

- `tb.NewClient(clusterID types.Uint128, addresses []string) (Client, error)` is the
  constructor signature.
- `types.AccountFlags` and `types.TransferFlags` expose the field names used in this
  specification and serialize through `.ToUint16()`.
- `types.Transfer.Amount` is `types.Uint128`.
- `types.TransferExists` is `46` and `types.TransferExceedsCredits` is `54`.

### 3.1 Client Construction

Port 3320, cluster ID 0, thread-safe single instance.

```go
client, err := tb.NewClient(types.ToUint128(0), []string{"127.0.0.1:3320"})
```

One process-scoped client is shared across goroutines. The billing package never creates one
client per request.

### 3.2 Account Construction

#### Grant accounts

Each `credit_grants.grant_id` (ULID) gets exactly one TigerBeetle account. The account ID
is the ULID interpreted as `Uint128` — no packing, no type prefix:

```go
types.Account{
    ID:         GrantAccountID(grantID).raw,  // ULID half-swap into Uint128
    UserData64: uint64(orgID),
    UserData32: uint32(sourceType), // 1=free_tier, 2=subscription, 3=purchase, 4=promo, 5=refund
    Ledger:     1,
    Code:       uint16(9),  // AcctGrant — type discrimination via code, not via ID bits
    Flags: types.AccountFlags{
        DebitsMustNotExceedCredits: true,
        History:                    true,
    }.ToUint16(),
}
```

Grant accounts (ULID-based) and operator accounts (small sentinel IDs with high bits = 0)
occupy non-overlapping ranges of the `Uint128` space. After the half-swap, the high u64
contains `BE.Uint64(ulid[0:8])` — the ULID timestamp occupies the top 48 bits of this value,
which is always > 0 for any ULID generated after Unix epoch. Operator account IDs always
have zero in the high u64. The non-overlap is structural, not probabilistic.

This account is the grant's balance. `debits_posted` is consumed amount. `available` is
`credits_posted - debits_posted - debits_pending`. `closed_at` in PostgreSQL is set only
after the account has been drained and closed.

#### Operator accounts

Operator accounts include `ExpiredCredits`, which receives unused paid grant balances during
expiry sweeps:

```go
{
    ID:     OperatorAccountID(AcctExpiredCredits).raw,
    Ledger: 1,
    Code:   uint16(AcctExpiredCredits), // 8
    Flags:  types.AccountFlags{
        History: true,
    }.ToUint16(),
    // Unflagged — breakage accumulator. Credit-normal: expired credits credit this account.
    // No balance constraints because it's a sink.
},
```

**ExpiredCredits** is a breakage account — deferred revenue that was never recognized.
Credits that expire are neither revenue (they weren't consumed) nor expense (they weren't
free tier). Accountants care about this distinction.

### 3.3 Transfer Construction — Credit Expiry

The expiry sweeper drains one grant account at a time:

```go
expiryTransfer := types.Transfer{
    ID:              CreditExpiryID(grantID).raw,
    DebitAccountID:  GrantAccountID(grantID).raw,
    CreditAccountID: sinkAccountID, // ExpiredCredits or FreeTierExpense
    Amount:          types.ToUint128(requestedAmount),
    Ledger:          1,
    Code:            uint16(KindCreditExpiry),
    Flags: types.TransferFlags{
        BalancingDebit: true,
    }.ToUint16(),
    UserData64: uint64(orgID),
}
```

`BalancingDebit` is critical: between the PostgreSQL query (`closed_at IS NULL`) and the
TigerBeetle transfer, a concurrent settlement may have consumed some of the grant. The
clamped amount from `LookupTransfers` is the authoritative expired amount.

**ID derivation for credit expiry:**

```go
func GrantAccountID(grant GrantID) AccountID {
    return AccountID{types.Uint128{
        binary.BigEndian.Uint64(grant[8:16]),  // low u64 ← ULID random tail
        binary.BigEndian.Uint64(grant[0:8]),   // high u64 ← ULID timestamp + random head
    }}
}

func CreditExpiryID(grant GrantID) TransferID {
    return TransferID{types.Uint128{
        binary.BigEndian.Uint64(grant[8:16]),
        binary.BigEndian.Uint64(grant[0:8]),
    }}
}
```

Both functions swap the ULID's big-endian halves into TigerBeetle's little-endian Uint128,
placing the timestamp in the high u64 for LSM ordering. Account IDs and transfer IDs are
separate TigerBeetle namespaces, so the same numeric value in both is safe. There is exactly
one grant account and at most one expiry transfer per grant.

### 3.4 Account Creation Error Handling

```go
results, err := client.CreateAccounts(accounts)
if err != nil {
    return fmt.Errorf("create accounts: %w", err) // transport error
}
for _, r := range results {
    switch {
    case r.Result == types.AccountExists:
        continue // idempotent
    case r.Result >= types.AccountExistsWithDifferentFlags &&
         r.Result <= types.AccountExistsWithDifferentUserData128:
        return fmt.Errorf("account %d: %w: %s", r.Index, ErrAccountConflict, r.Result)
    default:
        return fmt.Errorf("account %d: creation failed: %s", r.Index, r.Result)
    }
}
```

`AccountEventResult.Index` is zero-based position in the batch. Empty results = all
succeeded. `AccountExists` (value 21) is idempotent confirmation.

### 3.5 Transfer Construction

Reservations are sequential per-grant waterfalls. Each pending transfer is one leg:

```go
reservationLeg := types.Transfer{
    ID:              VMTransferID(jobID, windowSeq, grantIdx, KindReservation).raw,
    DebitAccountID:  GrantAccountID(grantID).raw,
    CreditAccountID: phaseSinkAccountID, // FreeTierExpense or Revenue
    Amount:          types.ToUint128(remainder),
    Ledger:          1,
    Code:            uint16(KindReservation),
    Flags:           types.TransferFlags{Pending: true, BalancingDebit: true}.ToUint16(),
    UserData64:      uint64(orgID),
    UserData32:      windowSeq,
    Timeout:         cfg.PendingTimeoutSecs,
}
```

**Reading back the clamped amount** (two-round-trip pattern):

```go
transfers, err := client.LookupTransfers([]types.Uint128{reservationLeg.ID})
clampedAmount := uint128ToUint64(transfers[0].Amount) // actual, not requested
remainder -= clampedAmount
```

TigerBeetle does not return the clamped amount in `CreateTransfers` results — `LookupTransfers`
is the only way to read it.

The orchestrator repeats that loop for each eligible grant in order. If `remainder > 0`
after the final grant, it voids every pending leg and returns `ErrInsufficientBalance`.

**Settlement (post pending):**

```go
types.Transfer{
    ID:        VMTransferID(jobID, windowSeq, grantIdx, KindSettlement).raw,
    PendingID: VMTransferID(jobID, windowSeq, grantIdx, KindReservation).raw,
    Amount:    types.ToUint128(actualCost), // partial post releases excess
    Flags:     types.TransferFlags{PostPendingTransfer: true}.ToUint16(),
}
```

**Void (cancel pending):**

```go
types.Transfer{
    ID:        VMTransferID(jobID, windowSeq, grantIdx, KindVoid).raw,
    PendingID: VMTransferID(jobID, windowSeq, grantIdx, KindReservation).raw,
    Flags:     types.TransferFlags{VoidPendingTransfer: true}.ToUint16(),
}
```

**Linked transfers:** `Linked` flag chains transfers atomically. Last in chain must NOT
have `Linked` set. The grant-waterfall flow does NOT use linked transfers — each leg
requires readback and must remain individually post-able/voidable.

**Periodic subscription deposit:**

```go
types.Transfer{
    ID:              SubscriptionPeriodID(subscriptionID, periodStart, KindFreeTierReset).raw,
    DebitAccountID:  OperatorAccountID(AcctFreeTierPool).raw,
    CreditAccountID: GrantAccountID(grantID).raw,
    Amount:          types.ToUint128(includedCredits),
    Ledger: 1, Code: uint16(KindFreeTierReset),
    UserData64: uint64(orgID),
}
```

Paid metered subscriptions use the same helper with `KindSubscriptionDeposit` and
`DebitAccountID = OperatorAccountID(AcctStripeHolding)`, `CreditAccountID = GrantAccountID(grantID)`.

**ID derivation for subscription-period deposits:**

```go
func SubscriptionPeriodID(subscriptionID int64, periodStart time.Time, kind XferKind) TransferID {
    t := periodStart.UTC()
    var id [16]byte
    binary.LittleEndian.PutUint32(id[0:4], uint32(t.Year())*12+uint32(t.Month()))
    id[5] = uint8(kind)
    binary.LittleEndian.PutUint64(id[8:16], uint64(subscriptionID))
    return TransferID{types.BytesToUint128(id)}
}
```

**Licensed recurring charge (`invoice.paid` on a licensed plan):**

```go
types.Transfer{
    ID:              StripeDepositID(taskID, KindSubscriptionDeposit).raw,
    DebitAccountID:  OperatorAccountID(AcctStripeHolding).raw,
    CreditAccountID: OperatorAccountID(AcctRevenue).raw,
    Amount:          types.ToUint128(amountUnits),
    Ledger:          1,
    Code:            uint16(KindSubscriptionDeposit),
    UserData64:      uint64(orgID),
}
```

Licensed products do not create `credit_grants`. The recurring Stripe invoice is recognized
directly as revenue in TigerBeetle.

TigerBeetle is intentionally unaware of pricing-phase selection. `included` versus `overage`
is decided in PostgreSQL from the eligible grant set and recorded in the `Reservation` and the
ClickHouse metering row. TigerBeetle only sees the resulting reservation legs.

### 3.6 Transfer Error Handling

```go
results, err := client.CreateTransfers(transfers)
if err != nil {
    return fmt.Errorf("create transfers: %w", err) // transport error
}
for _, r := range results {
    switch r.Result {
    case types.TransferExists:
        continue // idempotent
    case types.TransferExceedsCredits:
        return ErrInsufficientBalance // debit account balance insufficient
    case types.TransferLinkedEventFailed:
        return fmt.Errorf("transfer %d: linked event failed", r.Index)
    default:
        return fmt.Errorf("transfer %d: %s", r.Index, r.Result)
    }
}
```

| Condition | `err` | `results` |
|-----------|-------|-----------|
| Insufficient balance | `nil` | `TransferExceedsCredits` (54) |
| TigerBeetle down | Non-nil (`tberrors.Err*`) | N/A |
| ID collision (same fields) | `nil` | `TransferExists` (46) |
| ID collision (different fields) | `nil` | `TransferExistsWithDifferent*` |

### 3.7 Batch Size Limits

Max per `CreateAccounts`/`CreateTransfers` call: **8191** (1 MiB message / 128 bytes).
Exceeding returns `tberrors.ErrMaximumBatchSizeExceeded`. Query result limit: **8189**.
Pagination via `TimestampMin`/`TimestampMax`.

### 3.8 Uint128 Utilities

```go
func uint128ToUint64(v types.Uint128) uint64 {
    b := v.Bytes()
    for i := 8; i < 16; i++ {
        if b[i] != 0 { panic("uint128 overflow") }
    }
    return binary.LittleEndian.Uint64(b[0:8])
}
```

Construction: `types.ToUint128(val)` for single uint64; `types.BytesToUint128(buf)` for
packed 16-byte arrays. Comparison: direct `==` works. Ordering: convert via `.BigInt()`.

---

## 4. Stripe Integration Details

Validated upstream facts:

- `stripe.NewClient(apiKey)` is the recommended initialization path in `stripe-go/v85`.
- `webhook.ConstructEvent(payload []byte, header string, secret string)` delegates to
  `webhook.DefaultTolerance`, which is `300 * time.Second`.
- The 3DS field path for Checkout in `v85.0.0` is
  `CheckoutSessionParams.PaymentMethodOptions.Card.RequestThreeDSecure`.
- `v85.0.0` is the stable SDK release. `v85.1.0-*` prereleases exist; no stable `v86`
  module is published.

### 4.1 Client Initialization

The billing runtime uses the modern `stripe.Client` API:

```go
sc := stripe.NewClient(apiKey)
```

The package does not use the deprecated `client.API` pattern.

### 4.2 Webhook Verification

Webhook verification in `billing-service` is a thin wrapper over:

```go
event, err := webhook.ConstructEvent(payload, signatureHeader, secret)
```

Verification rules:

- reject signatures older than `webhook.DefaultTolerance`
- reject API-version mismatch
- return `500` to Stripe on verification or persistence failure so Stripe retries delivery

### 4.3 Checkout Sessions

Credit top-ups use `POST /v1/checkout/sessions` in `mode=payment`.
Subscription signup uses `POST /v1/checkout/sessions` in `mode=subscription`.

The normative 3DS field path in `stripe-go/v85` is:

```go
params := &stripe.CheckoutSessionParams{
    Mode: stripe.String(string(stripe.CheckoutSessionModePayment)),
    PaymentMethodOptions: &stripe.CheckoutSessionPaymentMethodOptionsParams{
        Card: &stripe.CheckoutSessionPaymentMethodOptionsCardParams{
            RequestThreeDSecure: stripe.String(
                string(stripe.CheckoutSessionPaymentMethodOptionsCardRequestThreeDSecureAny),
            ),
        },
    },
}
```

Required metadata on every Checkout Session created by the billing package:

- `org_id`
- `product_id`
- `plan_id` for subscription sessions

### 4.4 Smart Retries

Smart Retries is a Stripe-account setting configured in the Dashboard, not an API parameter.

Required account configuration for this specification:

- Smart Retries enabled
- retry policy `8` attempts within `2 weeks`
- terminal action after retry exhaustion: **Cancel the subscription**

Observed webhook lifecycle under that configuration:

- `invoice.payment_failed` fires on each failed attempt and retry update
- `invoice.paid` fires if a later retry succeeds
- `customer.subscription.deleted` fires when the configured retry window ends and the terminal
  action cancels the subscription

Additional Stripe facts that matter to the worker:

- `invoice.payment_failed` is not a single initial-failure event; it is the event used for
  retry attempt updates
- `attempt_count` on the invoice records how many attempts have been made so far
- `next_payment_attempt` is present on the invoice; if Stripe Automations is enabled, the
  next attempt timestamp moves from `invoice.payment_failed` to `invoice.updated`
- the retry window is configurable to `1 week`, `2 weeks`, `3 weeks`, `1 month`, or `2 months`
  and Stripe recommends `8` tries within `2 weeks`

### 4.5 Annual Cancellation With Refund

Annual cancellation with refund has two distinct concerns:

- stop future service
- create the correct accounting artifact in Stripe for the unused paid period

The correct refund artifact for an already-paid annual invoice is a credit note:

- `POST /v1/credit_notes`
- required parameter: `invoice`
- optional refund path: `refund_amount` or linked `refunds[]`

This is the object that explicitly ties the refund or customer-balance credit back to the
finalized invoice.

`DELETE /v1/subscriptions/{id}` with `prorate=true` and `invoice_now=true` is not sufficient on
its own for the refund path in this system. Stripe documents that immediate cancellation with
proration generates proration invoice items and can create customer credit. That is appropriate
for internal Stripe billing math, but the refund artifact forge-metal relies on is the credit
note against the finalized annual invoice.

Therefore:

- graceful cancellation: `POST /v1/subscriptions/{id}` with `cancel_at_period_end=true`
- immediate cancellation without refund: `DELETE /v1/subscriptions/{id}` with `prorate=false`
- immediate annual cancellation with refund: create credit note first, then delete the
  subscription without Stripe-side proration

### 4.6 Webhook Event Processing

#### `checkout.session.completed`

Purpose: customer correlation.

Required effect:

- upsert `orgs.stripe_customer_id`
- log a `billing_events` row if this is the first correlation

No TigerBeetle mutation occurs here.

#### `payment_intent.succeeded`

Purpose: one-time prepaid credit purchase.

Required effect:

1. enqueue `stripe_purchase_deposit` with `idempotency_key = payment_intent.id`
2. worker calls `DepositCredits`

Required payload fields:

- `org_id` from Checkout Session metadata
- `product_id` from Checkout Session metadata
- `stripe_payment_intent_id`
- `amount_ledger_units`
- `expires_at`

#### `invoice.paid`

Purpose: recurring invoice success.

Required branch by `products.billing_model`:

- `metered`: update `subscriptions.current_period_*`, enqueue `stripe_subscription_credit_deposit`,
  and materialize the current-period grant using the same uniqueness rules as
  `DepositSubscriptionCredits`
- `licensed`: enqueue `stripe_licensed_charge` and call `RecordLicensedCharge`

This branch point is explicit. A licensed recurring invoice does not create a grant. The
periodic `DepositSubscriptionCredits` job is the catch-up path for missed metered deposits and
the monthly-drip path for annual subscriptions; it must produce the same TigerBeetle transfer
ID as the webhook-driven path for the same `(subscription_id, period_start, kind)`.

#### `invoice.payment_failed`

Purpose: dunning status update and retry-attempt visibility.

Required effect:

```sql
UPDATE subscriptions
SET status = 'past_due',
    past_due_since = COALESCE(past_due_since, now()),
    status_changed_at = now()
WHERE stripe_subscription_id = :stripe_sub_id
  AND status IN ('active', 'trialing');
```

No TigerBeetle mutation occurs here.

#### `charge.dispute.created`

Purpose: debit disputed funds and tighten access.

Required effect:

1. enqueue `stripe_dispute_debit` with `idempotency_key = dispute.id`
2. worker calls `HandleDispute`
3. if the clamped debit is smaller than the dispute amount, suspend the org

#### `customer.subscription.deleted`

Purpose: terminal subscription cancellation.

Required effect:

```sql
UPDATE subscriptions
SET status = 'cancelled',
    cancelled_at = now(),
    status_changed_at = now()
WHERE stripe_subscription_id = :stripe_sub_id;
```

If the deleted subscription was the only active entitlement for a product, future `Reserve`
calls for that product fail unless prepaid grants and a default plan exist.

### 4.7 Stripe Endpoints Explicitly Not Used

This specification does not call:

- `/v1/subscription_items/{id}/usage_records`

Reason:

- the `stripe-go/v85` generated surface does not expose a usage-record helper API
- Stripe billing documentation uses billing meters and meter events
- forge-metal already has its own metering source of truth in ClickHouse plus its own balance
  enforcement in TigerBeetle

If forge-metal later decides to mirror usage into Stripe Billing Meters, that is a separate
design and not an implementation detail of this package.

### 4.8 Test Inputs

The test-card references in this specification are pinned to Stripe docs:

- `4000000000003220`: 3DS Required, succeeds after authentication
- `4000003720000278`: bypasses pending balance; it is not the canonical 3DS-required card

The package test harness must treat `4000003720000278` as a balance-availability test card,
not as a 3DS trigger.

---

## 5. Testing Specification

### 5.1 Layer 1: Property-Based Invariant Tests

Use `pgregory.net/rapid` `v1.2.0` against a real TigerBeetle instance and a real PostgreSQL
database.

The `rapid.StateMachine` interface is:

```go
type StateMachine interface {
    Check(*rapid.T)
}
```

`rapid.StateMachineActions` in `v1.2.0` accepts action methods of type
`func(rapid.TB)`, but the billing package test harness should continue using explicit
`func(*rapid.T)` methods for clarity.

Required operations in the state machine:

- `OpEnsureOrg`
- `OpDepositCredits`
- `OpReserve`
- `OpRenew`
- `OpSettle`
- `OpVoid`
- `OpExpireCredits`
- `OpHandleDispute`
- `OpEvaluateTrustTiers`

Required invariants in `Check`:

- for each active grant, TigerBeetle available equals
  `credits_posted - debits_posted - debits_pending`
- no TigerBeetle grant account has a negative available balance
- every active PostgreSQL grant row has exactly one TigerBeetle grant account with
  `code=9`, `user_data_64=org_id`, and `user_data_32=source_type`
- `AcctExpiredCredits` only receives transfers with `Code == KindCreditExpiry`
- any `Reservation` that settles must consume grant rows from the eligibility set implied by
  `reservation.PricingPhase`
- no concurrent action sequence can cause a product to consume a grant row belonging to a
  different `product_id`
- if `overage_cap_units` is set and the current-period overage consumption has reached the cap,
  `Reserve` returns `ErrOverageCeilingExceeded` (not `ErrInsufficientBalance`)

Required generator biases:

- `expires_at`: already expired, expires within one second, expires within one hour, nil
- allocations: zero dimensions, exact boundary values, one-unit-over values
- dispute amounts: zero, exact grant amount, slightly above available balance
- subscription mix: free-tier plans, paid metered plans, licensed plans, no-subscription with
  default pay-as-you-go plan

### 5.2 Layer 2: Stripe Test Mode Integration Tests

Run against Stripe test mode with the Stripe CLI forwarding webhooks.

Required scenarios:

- Checkout top-up: `checkout.session.completed` then `payment_intent.succeeded`
- Metered subscription signup: `invoice.paid` leading to `DepositSubscriptionCredits`
- Licensed subscription signup: `invoice.paid` leading to `RecordLicensedCharge`
- Smart Retries recovery: repeated `invoice.payment_failed` updates followed by `invoice.paid`
- Smart Retries exhaustion: repeated `invoice.payment_failed` updates followed by
  `customer.subscription.deleted`
- Annual cancellation with refund: credit note issuance plus local subscription cancellation
- Dispute handling: `charge.dispute.created` leading to a clamped debit and possible suspension

Required test inputs:

- 3DS-required card: `4000000000003220`
- non-3DS balance-path card: `4000003720000278`

Test Clocks are required for subscription cadence tests and annual monthly-drip tests.

This layer verifies the Stripe surface, not financial invariants. The financial invariants are
owned by Layer 1 and Layer 3.

### 5.3 Layer 3: Reconciliation as a Test Suite

`Reconcile` is both an operational function and a named verification suite.

Required named checks:

- `grant_account_catalog_consistency` (alert): every active PostgreSQL grant row has an open TigerBeetle grant account
- `no_orphan_grant_accounts` (alert): every TigerBeetle grant account has a PostgreSQL catalog row
- `expired_grants_swept` (warn): no expired active grants without an expiry transfer
- `licensed_charge_exactly_once` (alert): each licensed invoice produces exactly one revenue
  transfer
- `metering_vs_transfers` (alert): metering totals align with settled TigerBeetle transfers
- `trust_tier_monotonicity` (warn): no automatic promotion directly to `enterprise`

### 5.4 Layer 4: Synthetic Canary

One canary org runs continuously.

Required cycle:

1. EnsureOrg
2. seed a small prepaid grant
3. Reserve one short metered window
4. sleep
5. Settle
6. verify a `forge_metal.metering` row exists
7. verify the funded grant account balance decreased exactly as expected

The canary is not allowed to depend on Stripe being available.

### 5.5 Layer 5: Simulation Harness (Design Only)

The simulation harness is not a substitute for real TigerBeetle or Stripe tests. It exists to
exercise failure choreography.

Required fault catalog:

| Fault | Expected behavior |
|-------|-------------------|
| PostgreSQL down during `DepositCredits` | No grant row inserted, no TigerBeetle mutation. Retryable. |
| TigerBeetle down after PG insert in `DepositCredits` | Grant row exists, account unfunded. Same ULID on retry. Reconciliation detects. |
| Cron and webhook race on subscription deposit | PostgreSQL unique index `(subscription_id, period_start)` is the arbiter. Loser returns early, no orphan TB accounts. |
| Expiry sweeper crashes mid-batch | idempotent replay via `CreditExpiryID(grantID)` |
| Concurrent `Reserve` and `ExpireCredits` on same grant | TigerBeetle serializes the debits; no grant can go negative |
| Smart Retries emits repeated `invoice.payment_failed` for one invoice | subscription stays `past_due`, no duplicate grant deposits |
| Stripe sends `invoice.paid` twice | task idempotency plus TigerBeetle transfer idempotency prevent duplicate mutation |
| Licensed invoice task replays after partial failure | exactly one `StripeHolding -> Revenue` transfer |
| ClickHouse down during overage ceiling check in `Reserve` | fail closed (503); no VM boots; `overage_ceiling_hit` event not logged |

---

## 6. Configuration

### 6.1 Config Struct

```go
type Config struct {
    // Reservation window duration in seconds.
    // Range: [60, 600]. Default: 300.
    ReservationWindowSecs uint32

    // Pending transfer timeout in seconds.
    // Must be > ReservationWindowSecs. Range: [600, 7200]. Default: 3600.
    PendingTimeoutSecs uint32

    // Stripe API key. Required. From STRIPE_SECRET_KEY env var.
    StripeSecretKey string

    // TigerBeetle cluster addresses. Default: ["127.0.0.1:3320"].
    TigerBeetleAddresses []string

    // TigerBeetle cluster ID. Default: 0.
    TigerBeetleClusterID uint64
}
```

Removed global pricing knobs:

- `VCPURate`
- `MemRate`
- `FreeTierMonthlyAllowance`

Rates and included credits live in PostgreSQL plan rows, not process config.

**Consequence for rate changes:** Changing a plan's `unit_rates` in PostgreSQL takes
effect on the *next* reservation. Active reservations use the rates resolved at reserve
time (stored in the `Reservation` struct). No restart required. No billing inconsistency.

### 6.2 Loading

Configuration is loaded from the deployed credential file:

- `/etc/credstore/billing/secrets.env`

Required environment variables:

- `STRIPE_SECRET_KEY`

`billing-service` owns the transport/runtime credentials that were removed from the
domain package:

- `BILLING_PG_DSN`
- `STOREFRONT_PG_DSN`
- `STRIPE_WEBHOOK_SECRET`

### 6.3 Hot-Reloadable vs. Restart-Required

| Value | Hot-reloadable | Reason |
|-------|:-:|--------|
| `ReservationWindowSecs` | No | Active reservations use window from creation time |
| `PendingTimeoutSecs` | No | Baked into TigerBeetle transfers at creation |
| `StripeSecretKey` | No | Stripe client is immutable |
| `TigerBeetleAddresses` | No | TigerBeetle client created once |
| **Plan unit_rates** | **Yes** | Stored in PostgreSQL, read per-request |
| **Plan overage_unit_rates** | **Yes** | Stored in PostgreSQL, read at reservation boundaries |
| **Subscription overage_cap_units** | **Yes** | Stored in PostgreSQL, read at reservation boundaries |
| **Plan quotas** | **Yes** | Stored in PostgreSQL, read per-request |
| **Plan included_credits** | **Yes** | Takes effect on next deposit cycle |

The operational config (connection strings, keys) is restart-required. The billing policy
(rates, quotas, credits) is hot-reloadable via PostgreSQL updates.

Graceful shutdown contract:

- stop claiming new tasks
- finish or requeue any claimed task before exit
- do not create new TigerBeetle reservations after SIGTERM
- close Stripe, PostgreSQL, and TigerBeetle clients

### 6.4 Ledger Unit Scale

Unchanged: 10^7 per USD. `$1.00 = 10,000,000 ledger units`.

---

## 7. Error Handling and Failure Modes

### 7.1 TigerBeetle Failures

Key policies:

- **Reserve**: fail closed (503). No VMs boot. Includes overage ceiling check failure.
- **Renew/Settle**: retry with backoff. Pending transfer timeout is the backstop.
- **DepositSubscriptionCredits**: continue processing remaining subscriptions. Accumulate errors.
- **RecordLicensedCharge**: leave the task retryable; do not mutate local state first.
- **Reconcile**: fail immediately. Do not advance watermark.

### 7.2 PostgreSQL Failures

Key policies:

- **Webhook processing**: return 500, Stripe retries.
- **EnsureOrg**: return error, next request retries.
- **DepositCredits** (PG insert is the first step): if the insert succeeds but
  TigerBeetle is unavailable, the grant row exists with no funded account.
  Same grant ULID on retry produces the same TigerBeetle account ID.
  Reconciliation detects unfunded grants and alerts.
- **Reserve**: if PostgreSQL cannot supply the eligible grant catalog, fail closed.

### 7.3 Stripe Failures

Key policies:

- **CreateCheckoutSession/CreateSubscription**: return error to caller; no task row created.
- **CancelSubscription**: never mutate local state before the Stripe credit note or
  subscription API call succeeds.
- **Webhook verification**: reject bad signatures with 400; return 500 on persistence failure.

### 7.4 ClickHouse Failures

Key policies:

- **CheckQuotas**: fail closed.
- **Metering write after settle**: TigerBeetle is authoritative; reconciliation backfills
  or alerts.
- **Reconcile**: fail immediately. Do not advance the watermark.

### 7.5 New Failure Scenarios

#### Quota check fails (ClickHouse down)

- **Policy:** Fail closed. If quota cannot be verified, access is denied (429).
- **Rationale:** Fail-open on quotas allows unlimited usage during ClickHouse outages.
  For a metered platform, this is unacceptable.

#### Credit expiry sweeper crashes mid-batch

- **Recovery:** Re-run. `CreditExpiryID(grantID)` produces the same transfer ID.
  TigerBeetle returns `TransferExists` for already-processed grants. PostgreSQL grant
  rows are updated idempotently (`SET closed_at = now()` where still open).

#### Concurrent Reserve and ExpireCredits on same grant

- **Resolution:** both debit the same TigerBeetle grant account using balancing semantics, so
  the grant cannot go negative. PostgreSQL only records closure after the account is drained.

#### Subscription renewal deposit when org has been suspended

- **Policy:** Deposit the credits anyway. The credits belong to the org (they paid for
  them). Suspension prevents *consumption*, not *receipt*. When suspension is lifted,
  the credits are available.

#### Cron and webhook race on subscription credit deposit

- **Resolution:** both paths call `DepositCredits`, which inserts the PostgreSQL catalog
  row first. The unique index on `(subscription_id, period_start)` is the serialization
  point: exactly one writer wins the insert, the other gets a no-op and returns early.
  No orphan TigerBeetle accounts are created because the losing path never reaches
  TigerBeetle.

#### Duplicate `invoice.paid` delivery

- **Resolution:** `tasks.idempotency_key` on the invoice ID blocks duplicate task creation.
  If a duplicate task somehow exists, the TigerBeetle transfer ID still deduplicates it.

#### Stripe account configured to leave subscriptions `past_due`

- **Resolution:** unsupported. The billing doctor must fail configuration
  validation if the Stripe account terminal action diverges from "cancel the subscription".

#### Credit note created, local cancellation update fails

- **Resolution:** the task remains retryable by invoice-based idempotency. Reconciliation
  compares Stripe invoice state and local subscription state and raises `reconciliation_alert`.

---

## 8. Integration Points

### 8.1 In-Process Domain Calls

Direct imports of `billing` are reserved for code running inside the billing runtime boundary
itself. In practice that means `billing-service` handlers, webhook dispatch, worker actions,
and command entrypoints that are part of the billing deployment unit.

The in-process metered billing lifecycle is:

```go
// 1. Check quotas
result, err := billingClient.CheckQuotas(ctx, orgID, "sandbox", map[string]float64{
    "vcpu":           float64(req.VCPUs),
    "gib":            float64(req.MemMiB) / 1024.0,
    "concurrent_vms": currentConcurrentVMs,
})
if !result.Allowed {
    return nil, status.Error(codes.ResourceExhausted, "quota exceeded")
}

// 2. Reserve
reservation, err := billingClient.Reserve(ctx, billing.ReserveRequest{
    JobID:      jobID,
    OrgID:      orgID,
    ProductID:  "sandbox",
    ActorID:    actorID, // from Zitadel JWT sub claim
    Allocation: map[string]float64{"vcpu": 2, "gib": 0.5},
})
if errors.Is(err, billing.ErrInsufficientBalance) {
    return nil, status.Error(codes.FailedPrecondition, "insufficient balance")
}
if errors.Is(err, billing.ErrOverageCeilingExceeded) {
    return nil, status.Error(codes.FailedPrecondition, "overage spending cap reached")
}

// 3. Boot VM, periodic Renew, Settle on exit
```

The caller does not know about plan rates. It passes resource dimensions; the billing package
resolves rates from PostgreSQL.

Cross-service consumers must not import `billing` directly. A separate service that needs
billing behavior calls `billing-service` through the generated billing client described in
section 8.2.

### 8.2 Generated Billing Client For Remote Services

Remote forge-metal services integrate with billing through the OpenAPI contract exported by
`billing-service`.

The OpenAPI document is the source artifact for:

- generated Go client types and methods
- CI drift detection between handlers and consumers
- service-to-service contract review

Normative rules:

1. No forge-metal service may ship a handwritten HTTP client for billing.
2. The generated client is produced from the checked-in `billing-service` OpenAPI document,
   not scraped from a running authenticated server.
3. `sandbox-rental-service` and future services use the generated client for metered
   reservation lifecycle endpoints.
4. The billing package's Go types are not the public transport schema for remote services.

### 8.3 Stripe Webhook Handler

`billing-service` webhook route → verification → PostgreSQL task row → Go worker.

Normative task dispatch:

| `task_type` | Billing API |
|-------------|-------------|
| `stripe_purchase_deposit` | `DepositCredits` |
| `stripe_subscription_credit_deposit` | `DepositCredits` |
| `stripe_licensed_charge` | `RecordLicensedCharge` |
| `stripe_dispute_debit` | `HandleDispute` |
| `trust_tier_evaluate` | `EvaluateTrustTiers` |

**Retry/DLQ lifecycle:** on worker error, the task's `status` transitions to `retrying`
(with exponential backoff) or `dead` (after `max_attempts`). Dead tasks are visible via
the `idx_tasks_dead` index. A `forge-metal billing dlq` subcommand lists, retries, or
acknowledges dead tasks.

### 8.4 Cron Jobs

| Subcommand | Schedule | Action |
|------------|----------|--------|
| `forge-metal billing deposit-credits` | Monthly, 1st at 00:05 UTC | `DepositSubscriptionCredits()` |
| `forge-metal billing expire-credits` | Daily at 02:00 UTC | `ExpireCredits()` |
| `forge-metal billing reconcile` | Hourly | `Reconcile()` |
| `forge-metal billing trust-tier-evaluate` | Daily at 03:00 UTC | `EvaluateTrustTiers()` |
| `forge-metal billing canary` | Every 5 minutes | Reserve → sleep → settle → verify |
| `forge-metal billing dlq` | Manual | List/retry/acknowledge dead tasks |

### 8.5 Billing Service HTTP Contract

`billing-service` is the sole HTTP surface for billing. Its Huma definition is the canonical
cross-service contract and must produce the checked-in OpenAPI artifact used for generated
clients.

The contract has four endpoint classes:

1. Read endpoints for product and account state
2. Service-to-service metered lifecycle endpoints
3. Browser-initiated Stripe session creation endpoints
4. Operational endpoints and webhook ingress

Required read endpoints:

```
GET /internal/billing/v1/orgs/:org_id/balance
GET /internal/billing/v1/orgs/:org_id/products/:product_id/balance
GET /internal/billing/v1/orgs/:org_id/subscriptions
GET /internal/billing/v1/orgs/:org_id/grants?product_id=sandbox&active=true
GET /internal/billing/v1/orgs/:org_id/usage?product_id=sandbox&since=<ts>&limit=<n>
```

Required metered lifecycle endpoints for remote service consumers:

```
POST /internal/billing/v1/check-quotas
POST /internal/billing/v1/reserve
POST /internal/billing/v1/settle
POST /internal/billing/v1/void
```

Required browser/session endpoints:

```
POST /internal/billing/v1/checkout
POST /internal/billing/v1/subscribe
```

Required operational/runtime endpoints:

```
POST /webhooks/stripe
POST /internal/billing/v1/ops/deposit-credits
POST /internal/billing/v1/ops/expire-credits
POST /internal/billing/v1/ops/reconcile
POST /internal/billing/v1/ops/trust-tier-evaluate
GET  /healthz
GET  /readyz
GET  /openapi
```

Transport ownership rules:

- Request/response DTOs for these endpoints live with `billing-service`, not in `billing`.
- Webhook signature verification lives with `billing-service`, not in `billing`.
- Task claiming, dispatch, retry, and DLQ runtime live with `billing-service`, not in
  `billing`.
- The OpenAPI artifact is versioned with the `billing-service` source tree.

---

## Appendix A: RFC Amendment Checklist

| # | Topic | RFC change required |
|---|-------|---------------------|
| 1 | TigerBeetle port | Use port `3320` everywhere |
| 2 | Account flags | Add `History` to every account |
| 3 | Operator accounts | Add `ExpiredCredits` account type `8` |
| 4 | Transfer kinds | Add `KindDisputeDebit=8` and `KindCreditExpiry=9` |
| 5 | Balance field types | Document `types.Uint128` for amounts and balances |
| 6 | Stripe SDK | Pin to `stripe-go/v85` and `stripe.NewClient` |
| 7 | Batch limits | Use `8191` create limit and `8189` query limit |
| 8 | PostgreSQL schema | Replace sandbox/storefront-specific billing tables with `products`, `plans`, `subscriptions`, `credit_grants`, `tasks`, `billing_events`, `billing_cursors` |
| 9 | Trust tiers | Replace `plan_tier` with `trust_tier` plus subscription status |
| 10 | Overage | Replace scalar `overage_rate` with `overage_unit_rates` |
| 11 | Free tier | Model free-tier allowance as subscription-period deposits, not one global monthly reset |
| 12 | Task queue | Add retry semantics and DLQ |
| 13 | Metering | Replace `sandbox_metering` with generic `forge_metal.metering` |
| 14 | Metering actor | Add `actor_id` to `forge_metal.metering` for per-user attribution |
| 15 | Overage ceiling | Add `overage_cap_units` to `subscriptions`; enforce in `Reserve` |
| 16 | Stripe metering | Remove `/usage_records` dependency from reconciliation |
| 17 | Annual refunds | Use Stripe credit notes as the refund artifact |
| 18 | Grant IDs | ULID `grant_id` (application-generated), endian-aware half-swap for TigerBeetle account and expiry transfer IDs, account type via `code` field not ID bits |
| 19 | DepositCredits ordering | PostgreSQL insert first (serialization point), TigerBeetle last (Write Last, Read First) |
| 20 | ULID dependency | Add `github.com/oklog/ulid/v2` `v2.1.0` |

## Appendix B: Transport Cutover Plan

The refactor is a full cutover. Do not preserve parallel handwritten and generated client
paths.

### Phase 1: Contract Freeze

Goal: define the service boundary before moving code.

Required changes:

- add the metered lifecycle endpoints (`reserve`, `settle`, `void`) to the Huma contract
- define `healthz`, `readyz`, and OpenAPI export as first-class `billing-service` runtime
  surfaces
- define transport DTOs in `billing-service`
- check in the generated OpenAPI artifact

Exit criteria:

- the OpenAPI document fully describes every route remote consumers are allowed to call
- no remote consumer depends on undocumented billing endpoints

### Phase 2: Transport Extraction

Goal: move runtime concerns out of `billing`.

Move out of `github.com/forge-metal/billing`:

- Stripe webhook HTTP handler
- webhook request verification and HTTP status mapping
- worker loop, claim/complete/fail runtime, and task dispatch orchestration
- API serialization row types and query DTOs

Move into `github.com/forge-metal/billing-service`:

- Huma handlers and request/response models
- webhook ingress package
- worker/runtime package
- health/readiness probes

Exit criteria:

- `src/billing` contains no `net/http` handler entrypoints
- `src/billing` contains no background worker loop
- `src/billing` contains no service-shaped DTO package

### Phase 3: Consumer Cutover

Goal: make generated-client usage mandatory.

Required changes:

- generate the billing client from the checked-in OpenAPI document
- replace `sandbox-rental-service`'s handwritten billing client with the generated client
- remove handwritten URL/path construction from consumers

Exit criteria:

- `sandbox-rental-service` imports the generated billing client
- no repo path contains a handwritten billing HTTP client

### Phase 4: Domain Cleanup

Goal: leave `billing` as a domain library only.

Delete from `src/billing` after extraction:

- `webhook.go`
- `worker.go`
- `server.go`

Possible follow-on reshaping:

- split `billing-service` internals into `transport`, `webhooks`, `worker`, and `ops`
  packages for ownership clarity
- collapse any billing types that exist only to satisfy transport JSON

Exit criteria:

- the only public API exported by `billing` is domain/application behavior
- every remote integration path flows through `billing-service` and its generated client

### Phase 5: Verification Bar

The cutover is complete only when all of the following are true:

- `billing-service` builds and serves the checked-in OpenAPI contract
- generated client code is regenerated in CI and shows no drift
- `sandbox-rental-service` exercises `reserve`, `settle`, and `void` through the generated
  client against a live deployment
- Stripe webhooks still flow through `billing-service` and complete the task/worker path
- ClickHouse logs show successful live traces for the new service-to-service path with no
  billing auth or transport errors

## Appendix C: Remaining Manual-Review Items

- The shared billing schema lives in the PostgreSQL database named `sandbox`.
- The Ansible billing credentials publish both `BILLING_PG_DSN` and `STOREFRONT_PG_DSN`.
  The storefront application integration should be reviewed when the billing package
  is implemented so the intended database ownership is explicit in code.
- **Per-member spending policy** is deferred. `actor_id` is recorded in metering rows so
  historical attribution is available when the feature is designed. The mechanism (tags,
  role-based budgets, per-user caps, Zitadel metadata-driven groups) is intentionally left
  open. The org-level `overage_cap_units` ceiling is sufficient for v1.
