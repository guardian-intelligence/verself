# `internal/billing` Package Specification — Revision 3

Package `billing` is the financial core of forge-metal. It owns org financial
provisioning, product pricing lookup, credit grant lifecycle, quota enforcement, Stripe cash
collection integration, TigerBeetle balance enforcement, reconciliation, and the asynchronous
worker contracts that connect those systems.

The package is product-agnostic. It does not know what a "sandbox" or "storefront" is. It
knows about products, plans, subscriptions, per-product grants, metering dimensions, funding
sources, and dunning state.

This document is normative and self-contained.

Four systems participate in every non-trivial billing flow:

| System | Authoritative for | Not authoritative for |
|--------|-------------------|-----------------------|
| Stripe | Cash collection, subscription invoice lifecycle, dispute notifications, Checkout sessions | Metering truth, internal balance enforcement, per-product credit attribution |
| TigerBeetle | Aggregate posted/pending balances, idempotent money movement, immutable transfer log | Per-product eligibility, grant expiration policy, task retry state |
| PostgreSQL | Product catalog, plan policy, per-product grant attribution, subscription state, task queue, retry/DLQ state | Aggregate account balance |
| ClickHouse | Append-only metering history, rolling-window quota queries, reconciliation evidence | Online balance enforcement |

The deliberate cross-system rule is:

1. Stripe decides whether external money was collected.
2. TigerBeetle decides whether forge-metal can afford to provide service right now.
3. PostgreSQL decides whether a given product is allowed to spend that aggregate balance.
4. ClickHouse records what actually happened.

The TigerBeetle account model and deterministic ID derivation scheme from
`docs/architecture/application-stack-rfc.md` remain in force. This revision does not introduce
an alternate account ID scheme.

This document makes six material design decisions that were previously ambiguous:

- TigerBeetle remains aggregate per org, product-unaware. Product-specific eligibility is
  enforced by PostgreSQL under a mandatory per-org advisory lock.
- Overage is a second rate card, not a post-facto inference from aggregate balance.
  The pricing phase is selected at reservation boundaries and recorded explicitly.
- Overage spending is capped per subscription via `overage_cap_units`. The org admin controls
  the ceiling; `Reserve` enforces it at reservation boundaries using ClickHouse actuals.
- Metering rows record `actor_id` (the Zitadel user who triggered the usage). Per-member
  spending policy is deferred; the column is recorded now to avoid losing attribution history.
- Stripe usage-record synchronization is removed from the design. The current Stripe surface
  has moved toward billing meters, and forge-metal does not require Stripe to be the metering
  source of truth.
- Annual cancellation with refund is modeled as credit-note issuance against the finalized
  Stripe invoice, not as `DELETE /v1/subscriptions/{id}` with `prorate=true` alone.

Verified upstream contracts as of 2026-04-04:

| Dependency | Verified release | Notes |
|------------|------------------|-------|
| `github.com/tigerbeetle/tigerbeetle-go` | `v0.16.78` | No `0.17.x` or `0.18.x` release line is published on the Go module proxy. |
| `github.com/stripe/stripe-go/v85` | `v85.0.0` | `v85.1.0-*` prereleases exist; no stable `v86` exists. |
| `pgregory.net/rapid` | `v1.2.0` | `StateMachine` interface unchanged; `StateMachineActions` additionally accepts `func(rapid.TB)` methods. |

---

## Table of Contents

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

Every mutating billing operation that reads or writes `credit_grants`, subscription status, or
product eligibility for a specific org must execute under a PostgreSQL transaction that first
acquires `pg_advisory_xact_lock(org_id::bigint)`. This is mandatory. Zitadel org IDs are
positive snowflake-style integers that fit in signed 63 bits, so `TEXT -> BIGINT` conversion is
lossless for forge-metal orgs.

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

`billing_model` is a product-level invariant in this revision. A product does not mix
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

The previous scalar `overage_rate` is removed. A single integer is not expressive enough for
multi-dimensional pricing such as `{vcpu_second, gib_second}`.

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

TigerBeetle tracks aggregate balances per org account. PostgreSQL tracks the product-specific
grants that make those balances spendable. The central rule in Revision 3 is:

> TigerBeetle answers "can this org afford this in aggregate right now?" PostgreSQL answers
> "is this product allowed to spend that aggregate balance, and if so from which grant set?"

This revision keeps aggregate TigerBeetle accounts and rejects per-product TigerBeetle
account proliferation. The concurrency hazard is resolved with the per-org advisory lock.

Grant rows therefore need one additional field: which TigerBeetle account the grant funds.

```sql
CREATE TYPE grant_account AS ENUM ('free_tier', 'credit');

CREATE TABLE credit_grants (
    grant_id            BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    org_id              TEXT NOT NULL REFERENCES orgs(org_id),
    product_id          TEXT NOT NULL REFERENCES products(product_id),
    account_type        grant_account NOT NULL,

    -- Amounts (ledger units)
    amount              BIGINT NOT NULL CHECK (amount > 0),
    consumed            BIGINT NOT NULL DEFAULT 0 CHECK (consumed >= 0),
    expired             BIGINT NOT NULL DEFAULT 0 CHECK (expired >= 0),
    remaining           BIGINT GENERATED ALWAYS AS (amount - consumed - expired) STORED,

    -- Source attribution
    source              TEXT NOT NULL,
    stripe_reference_id TEXT,
    subscription_id     BIGINT REFERENCES subscriptions(subscription_id),

    -- Lifecycle
    period_start        TIMESTAMPTZ,
    period_end          TIMESTAMPTZ,
    expires_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_credit_grants_active
    ON credit_grants (org_id, product_id, expires_at)
    WHERE remaining > 0;

CREATE UNIQUE INDEX idx_credit_grants_subscription_period
    ON credit_grants (subscription_id, period_start, account_type)
    WHERE subscription_id IS NOT NULL;
```

Required source/account combinations:

| `source` | `account_type` | Meaning |
|----------|----------------|---------|
| `subscription` | `credit` | Included credits on a paid metered subscription |
| `purchase` | `credit` | Product-specific prepaid top-up |
| `promo` | `credit` | Promotional credit funded by `PromoPool` |
| `refund` | `credit` | Refund credit or manual make-good tied to an invoice |
| `free_tier` | `free_tier` | Monthly free-plan allowance funded by `FreeTierPool` |

They stay in sync via three mechanisms:

1. **Grant creation**: the worker posts the TigerBeetle transfer and inserts the
   `credit_grants` row as one logical operation. If it crashes between them, reconciliation
   detects the drift.
2. **Grant consumption**: `Settle`, `ExpireCredits`, `HandleDispute`, and any other
   grant-mutating operation run under the per-org advisory lock. Consumption order is:
   product match, then eligibility set, then earliest `expires_at`, then smallest `grant_id`.
3. **Expiration sweeper**: a daily cron finds expired grants and drains them from the
   corresponding TigerBeetle account using a clamped transfer.

Eligibility sets:

- free-tier leg: `source='free_tier'`
- included metered leg: `source='subscription'`
- overage or prepaid leg: `source IN ('purchase', 'promo', 'refund')`

Expiration policies:

| Source | Default expiration | Rationale |
|--------|-------------------|-----------|
| `subscription` | Period end + 30 days grace | Subscription credits do not roll forever |
| `purchase` | 12 months from purchase | Breakage accounting and customer expectation |
| `promo` | 90 days | Promotional urgency |
| `free_tier` | End of month | Monthly allowance |
| `refund` | Same as original grant | Refund credits inherit the original expiry policy |

**Decision: aggregate TigerBeetle accounts plus PostgreSQL advisory locks**

The three candidate designs were:

- per-product TigerBeetle accounts
- aggregate TigerBeetle accounts with per-org advisory locking
- accepting the race

This specification chooses aggregate TigerBeetle accounts plus per-org advisory locking.

Reasons:

- It preserves the RFC account model and deterministic ID layout unchanged.
- It keeps TigerBeetle account count stable at `2 * orgs + operator_accounts`.
- It serializes exactly the critical section that was previously unsafe: product grant
  eligibility lookup plus TigerBeetle mutation plus grant-row mutation.
- The latency cost is negligible on a single-node deployment because the protected critical
  section is PostgreSQL plus localhost TigerBeetle.

Rejected alternatives:

- Per-product TigerBeetle accounts would fit only by consuming reserved bits in the existing
  ID layout and would materially change `GetOrgBalance` semantics and operator ergonomics.
- Accepting the race would make the core financial invariant dependent on "rare in practice".

`grant_id` remains a PostgreSQL `BIGINT GENERATED ALWAYS AS IDENTITY`. That is sufficient for
`CreditExpiryID(grant_id)`: the high 64 bits remain globally increasing, which is what
TigerBeetle's LSM ordering cares about.

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
| `stripe_subscription_credit_deposit` | Stripe invoice ID | `org_id`, `product_id`, `subscription_id`, `stripe_invoice_id`, `amount_ledger_units`, `period_start`, `period_end`, `expires_at`, `account_type` |
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
    grant_id        BIGINT,
    task_id         BIGINT,
    payload         JSONB NOT NULL DEFAULT '{}',
    stripe_event_id TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_billing_events_stripe
    ON billing_events (stripe_event_id)
    WHERE stripe_event_id IS NOT NULL;

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
    exit_reason          LowCardinality(String)               CODEC(ZSTD(3)),
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

### 1.11 Jobs (product-specific, unchanged in ownership)

The `jobs` table stays product-specific. The billing package never owns product execution
state directly. Each product type may keep its own control-plane tables:

- `jobs` for sandbox
- `api_batches` or equivalent for an API product
- `orders` for storefront

The billing package only requires a mapping from that product-specific state to
`source_ref`, `window_seq`, `dimensions`, `charge_units`, `started_at`, and `ended_at`.

---

## 2. Package API Surface

Import path: `github.com/forge-metal/forge-metal/internal/billing`

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

### 2.2 ID Types

Reproduced from the RFC with additions for dispute debits and credit expiry.

```go
type OrgID  uint64
type JobID  int64
type TaskID int64

type AccountID  struct{ raw types.Uint128 }
type TransferID struct{ raw types.Uint128 }

type OrgAcctType uint16
const (
    AcctFreeTier     OrgAcctType = 1
    AcctCredit       OrgAcctType = 2
)

type OperatorAcctType uint16
const (
    AcctRevenue         OperatorAcctType = 3
    AcctFreeTierPool    OperatorAcctType = 4
    AcctStripeHolding   OperatorAcctType = 5
    AcctPromoPool       OperatorAcctType = 6
    AcctFreeTierExpense OperatorAcctType = 7
    AcctExpiredCredits  OperatorAcctType = 8  // NEW: breakage (deferred revenue never recognized)
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
    KindDisputeDebit        XferKind = 8  // NEW: chargeback debit
    KindCreditExpiry        XferKind = 9  // NEW: expired credit sweep
)

type Leg uint8
const (
    LegFreeTier Leg = 0
    LegCredit   Leg = 1
)

type PricingPhase string
const (
    PricingPhaseFreeTier PricingPhase = "free_tier"
    PricingPhaseIncluded PricingPhase = "included"
    PricingPhaseOverage  PricingPhase = "overage"
    PricingPhaseLicensed PricingPhase = "licensed"
)

type GrantAccount string
const (
    GrantAccountFreeTier GrantAccount = "free_tier"
    GrantAccountCredit   GrantAccount = "credit"
)
```

ID derivation functions are defined in the RFC (§ "Go type system"): `OrgAccountID`,
`OperatorAccountID`, `VMTransferID`, `SubscriptionPeriodID`, `StripeDepositID`, and
`CreditExpiryID`.

`SubscriptionPeriodID(subscriptionID, periodStartUTC, kind)` uses the same transfer-ID
layout as the RFC: `source_id = subscription_id` in the high 64 bits, `seq = year*12+month`
from `periodStart.UTC()`, `kind = KindFreeTierReset` or `KindSubscriptionDeposit`, `leg = 0`.

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
// EnsureOrg provisions an org across PostgreSQL and TigerBeetle.
//
// Postconditions:
//   - PostgreSQL orgs table has a row for orgID (INSERT ... ON CONFLICT DO NOTHING)
//   - TigerBeetle has two accounts: FreeTier and Credit
//   - Both accounts have History: true for GetAccountTransfers support
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
    AggregateFreeTier uint64
    AggregateCredit   uint64
}

// GetProductBalance reads product-specific grant state from PostgreSQL and the
// aggregate backing balances from TigerBeetle.
//
// Reserve does not depend on this helper; it re-runs the authoritative checks
// under the per-org advisory lock.
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
    FreeTierTransferID TransferID
    CreditTransferID   TransferID
    FreeTierAmount     uint64
    CreditAmount       uint64
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
// Under pg_advisory_xact_lock(org_id):
//   1. Read the active subscription for ProductID, if any.
//   2. Determine the pricing phase:
//      a. free_tier if active free-tier grants remain for ProductID
//      b. included if active subscription grants remain for ProductID
//      c. overage if the active plan has non-empty overage_unit_rates
//      d. overage with the default plan if there is no active subscription but
//         ProductID has a default plan and eligible prepaid grants remain
//   3. Resolve the rate card for that phase, applying org_pricing_overrides if present.
//   4. Compute costPerSec = sum(allocation[dim] × resolvedRateCard[dim]).
//   5. Compute windowCost = costPerSec × ReservationWindowSecs.
//   6. If the pricing phase is overage and subscription.overage_cap_units is non-NULL,
//      query current-period overage consumption from ClickHouse:
//        SELECT sum(charge_units) FROM forge_metal.metering
//        WHERE org_id = :org_id AND product_id = :product_id
//          AND pricing_phase = 'overage'
//          AND started_at >= :current_period_start
//      If consumed + windowCost > overage_cap_units, return ErrOverageCeilingExceeded.
//   7. Reserve windowCost in TigerBeetle:
//      - free-tier account first with balancing_debit
//      - credit account second for any remainder
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
//   - 1-2 pending TigerBeetle transfers reserving funds for one window
//   - Reservation populated with the chosen PricingPhase, PlanID, and ActorID
//
// Error conditions:
//   - ErrOrgSuspended: subscription status is 'suspended'
//   - ErrNoActiveSubscription: no active subscription and no default pay-as-you-go plan
//   - ErrInsufficientBalance: rate card resolved but aggregate backing balance insufficient
//   - ErrOverageCeilingExceeded: overage consumption would exceed subscription.overage_cap_units
//   - ErrDimensionMismatch: allocation key not found in unit_rates
//   - TigerBeetle unavailable: transport error
//   - ClickHouse unavailable during overage ceiling check: fail closed (503)
//
// Idempotency: Transfer IDs are deterministic from (JobID, WindowSeq, Leg, Kind).
func (c *Client) Reserve(ctx context.Context, req ReserveRequest) (Reservation, error)
```

The previous wording treated PostgreSQL grant state as advisory while TigerBeetle
held the real balance. That was the race. Revision 3 makes PostgreSQL grant state part of the
authoritative admission decision by serializing it with the org advisory lock.

#### Renew, Settle, Void

```go
func (c *Client) Renew(ctx context.Context, reservation *Reservation, actualSeconds uint32) error
func (c *Client) Settle(ctx context.Context, reservation *Reservation, actualSeconds uint32) error
func (c *Client) Void(ctx context.Context, reservation *Reservation) error
```

Normative semantics:

- `Renew`: settle the current window, mutate grant consumption under the org advisory lock,
  then reserve the next window using current grant state. `Renew` may therefore change
  `reservation.PricingPhase`.
- `Settle`: post the pending TigerBeetle transfers for the actual cost, update
  `credit_grants.consumed` for the exact set of grants eligible for `reservation.PricingPhase`,
  and write the `forge_metal.metering` row (including `actor_id` from the reservation).
- `Void`: cancel pending transfers without mutating grant consumption.

### 2.6 Credit Management

#### DepositCredits

```go
type CreditGrant struct {
    OrgID          OrgID
    ProductID      string
    AccountType    GrantAccount
    Amount         uint64      // ledger units
    Source         string      // "subscription", "purchase", "promo", "free_tier", "refund"
    StripeReferenceID string   // Stripe payment intent or invoice ID
    SubscriptionID *int64      // non-nil for subscription-sourced grants
    PeriodStart    *time.Time  // billing period this grant covers
    PeriodEnd      *time.Time
    ExpiresAt      *time.Time  // nil = never expires
}

// DepositCredits creates a credit grant and posts the corresponding TigerBeetle transfer.
//
// Postconditions:
//   - TigerBeetle transfer: debit StripeHolding → credit org/Credit
//     (for purchase/subscription/refund sources)
//     OR debit FreeTierPool → credit org/FreeTier (for free_tier source)
//     OR debit PromoPool → credit org/Credit (for promo source)
//   - credit_grants row inserted in PostgreSQL with account_type populated
//   - billing_events row logged
//
// The TigerBeetle transfer and PostgreSQL insert are NOT atomic. TigerBeetle is
// the source of truth for balance; PostgreSQL is the source of truth for
// attribution and expiration. Reconciliation detects discrepancies.
//
// Error conditions:
//   - TigerBeetle unavailable: returns error (no grant row created)
//   - PostgreSQL unavailable: TigerBeetle transfer may have succeeded but
//     grant row is missing. Reconciliation cron detects and alerts.
//
// Idempotency:
//   - purchase / promo / refund: transfer ID derived from *taskID
//   - subscription / free_tier: transfer ID derived from
//     SubscriptionPeriodID(*grant.SubscriptionID, *grant.PeriodStart, kind)
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
// For each grant where expires_at <= now() AND remaining > 0:
//   1. Post TigerBeetle transfer:
//      - free_tier grants: debit org/FreeTier → credit operator/FreeTierExpense (clamped)
//      - credit grants: debit org/Credit → credit operator/ExpiredCredits (clamped)
//   2. Update grant: expired = remaining
//   3. Log billing event
//
// Error conditions:
//   - PostgreSQL unavailable: returns error immediately
//   - Individual TigerBeetle failures: accumulated in Errors, processing continues
//
// Idempotency: Transfer ID derived from (grant_id, KindCreditExpiry). Safe to re-run.
func (c *Client) ExpireCredits(ctx context.Context) (ExpireResult, error)
```

### 2.7 Payments

#### HandleDispute

```go
// HandleDispute processes a chargeback by debiting the org's credit account.
//
// Postconditions:
//   - TigerBeetle transfer: debit org/Credit → credit StripeHolding
//     (balancing_debit to clamp if balance insufficient)
//   - If org's credit balance insufficient to cover dispute: all org
//     subscriptions suspended via SuspendOrg
//   - billing_events row logged with event_type='dispute_opened'
//   - credit_grants updated: consumed increased on the original grant
//     that funded the disputed payment (matched via stripe_reference_id)
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
//   3. Optional void of remaining grants in PostgreSQL/TigerBeetle
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
//      - free plans: DepositCredits(taskID=nil, source='free_tier', account_type='free_tier')
//      - paid metered plans: DepositCredits(taskID=nil, source='subscription', account_type='credit')
//   3. Use balancing_credit only for free-tier grants
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

Revision 3 reconciliation compares:

- ClickHouse metering rows
- TigerBeetle posted transfers
- PostgreSQL grant remaining totals

It does not call Stripe usage-record endpoints. Stripe is not the metering source of truth.

#### DetectBalanceDrift

```go
func (c *Client) DetectBalanceDrift(ctx context.Context) (DriftResult, error)
```

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

- `tb.NewClient(clusterID types.Uint128, addresses []string) (Client, error)` remains the
  constructor signature; the older `concurrencyMax` parameter is gone.
- `types.AccountFlags` and `types.TransferFlags` still expose the field names used in this
  specification and still serialize through `.ToUint16()`.
- `types.Transfer.Amount` is still `types.Uint128`, not `uint64`.
- `types.TransferExists` is `46` and `types.TransferExceedsCredits` is `54`.

### 3.1 Client Construction

Port 3320, cluster ID 0, thread-safe single instance.

```go
client, err := tb.NewClient(types.ToUint128(0), []string{"127.0.0.1:3320"})
```

One process-scoped client is shared across goroutines. The billing package never creates one
client per request.

### 3.2 Account Construction

#### Org accounts

Two accounts per org: FreeTier (code 1) and Credit (code 2). Both have
`DebitsMustNotExceedCredits: true` and `History: true`.

#### Operator accounts

Six operator accounts. Addition:

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

New transfer type for the `ExpireCredits` sweeper:

```go
expiryTransfer := types.Transfer{
    ID:              CreditExpiryID(grantID).raw,  // new derivation function
    DebitAccountID:  OrgAccountID(orgID, AcctCredit).raw,
    CreditAccountID: OperatorAccountID(AcctExpiredCredits).raw,
    Amount:          types.ToUint128(remainingAmount),
    Ledger:          1,
    Code:            uint16(KindCreditExpiry), // 9
    Flags: types.TransferFlags{
        BalancingDebit: true,  // clamp to available balance
    }.ToUint16(),
    UserData64: uint64(orgID),
}
```

`BalancingDebit` is critical: between the PostgreSQL query (which reads `remaining > 0`)
and the TigerBeetle transfer, the org may have consumed some credits. The clamped amount
from `LookupTransfers` is used to update `credit_grants.expired`.

**ID derivation for credit expiry:**

```go
func CreditExpiryID(grantID int64) TransferID {
    var id [16]byte
    id[5] = uint8(KindCreditExpiry)
    binary.LittleEndian.PutUint64(id[8:16], uint64(grantID))
    return TransferID{tb.BytesToUint128(id)}
}
```

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

**Pending reservation (free-tier leg with balancing_debit):**

```go
freeTierTransfer := types.Transfer{
    ID:              VMTransferID(jobID, windowSeq, LegFreeTier, KindReservation).raw,
    DebitAccountID:  OrgAccountID(orgID, AcctFreeTier).raw,
    CreditAccountID: OperatorAccountID(AcctFreeTierExpense).raw,
    Amount:          types.ToUint128(windowCost),
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
transfers, err := client.LookupTransfers([]types.Uint128{freeTierTransfer.ID})
clampedAmount := uint128ToUint64(transfers[0].Amount) // actual, not requested
remainder := windowCost - clampedAmount
```

TigerBeetle does not return the clamped amount in `CreateTransfers` results — `LookupTransfers`
is the only way to read it.

**Credit leg (if remainder > 0):**

```go
creditTransfer := types.Transfer{
    ID:              VMTransferID(jobID, windowSeq, LegCredit, KindReservation).raw,
    DebitAccountID:  OrgAccountID(orgID, AcctCredit).raw,
    CreditAccountID: OperatorAccountID(AcctRevenue).raw,
    Amount:          types.ToUint128(remainder),
    Ledger: 1, Code: uint16(KindReservation),
    Flags:  types.TransferFlags{Pending: true}.ToUint16(),
    UserData64: uint64(orgID), UserData32: windowSeq,
    Timeout: cfg.PendingTimeoutSecs,
}
```

If this fails with `TransferExceedsCredits`: void the free-tier transfer, return
`ErrInsufficientBalance`.

**Settlement (post pending):**

```go
types.Transfer{
    ID:        VMTransferID(jobID, windowSeq, leg, KindSettlement).raw,
    PendingID: VMTransferID(jobID, windowSeq, leg, KindReservation).raw,
    Amount:    types.ToUint128(actualCost), // partial post releases excess
    Flags:     types.TransferFlags{PostPendingTransfer: true}.ToUint16(),
}
```

**Void (cancel pending):**

```go
types.Transfer{
    ID:        VMTransferID(jobID, windowSeq, leg, KindVoid).raw,
    PendingID: VMTransferID(jobID, windowSeq, leg, KindReservation).raw,
    Flags:     types.TransferFlags{VoidPendingTransfer: true}.ToUint16(),
}
```

**Linked transfers:** `Linked` flag chains transfers atomically. Last in chain must NOT
have `Linked` set. The two-phase billing flow does NOT use linked transfers — free-tier
and credit legs are independent (balancing_debit requires readback; legs must be
individually post-able/voidable).

**Periodic subscription deposit:**

```go
types.Transfer{
    ID:              SubscriptionPeriodID(subscriptionID, periodStart, KindFreeTierReset).raw,
    DebitAccountID:  OperatorAccountID(AcctFreeTierPool).raw,
    CreditAccountID: OrgAccountID(orgID, AcctFreeTier).raw,
    Amount:          types.ToUint128(includedCredits),
    Ledger: 1, Code: uint16(KindFreeTierReset),
    Flags:  types.TransferFlags{BalancingCredit: true}.ToUint16(),
    UserData64: uint64(orgID),
}
```

`BalancingCredit` clamps to available headroom — prevents accumulation.

Paid metered subscriptions use the same helper with `KindSubscriptionDeposit` and
`DebitAccountID = OperatorAccountID(AcctStripeHolding)`, `CreditAccountID = OrgAccountID(orgID, AcctCredit)`.

**ID derivation for subscription-period deposits:**

```go
func SubscriptionPeriodID(subscriptionID int64, periodStart time.Time, kind XferKind) TransferID {
    t := periodStart.UTC()
    var id [16]byte
    binary.LittleEndian.PutUint32(id[0:4], uint32(t.Year())*12+uint32(t.Month()))
    id[5] = uint8(kind)
    binary.LittleEndian.PutUint64(id[8:16], uint64(subscriptionID))
    return TransferID{tb.BytesToUint128(id)}
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
is decided in PostgreSQL under the org advisory lock and recorded in the `Reservation` and the
ClickHouse metering row. TigerBeetle only sees the resulting reservation amount.

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

- `stripe.NewClient(apiKey)` remains the recommended initialization path in `stripe-go/v85`.
- `webhook.ConstructEvent(payload []byte, header string, secret string)` still delegates to
  `webhook.DefaultTolerance`, which is `300 * time.Second`.
- The 3DS field path for Checkout in `v85.0.0` remains
  `CheckoutSessionParams.PaymentMethodOptions.Card.RequestThreeDSecure`.
- `v85.0.0` is the current stable SDK release. `v85.1.0-*` prereleases exist; no stable `v86`
  module is published.

### 4.1 Client Initialization

The billing package uses the modern `stripe.Client` API:

```go
sc := stripe.NewClient(apiKey)
```

The package does not use the deprecated `client.API` pattern.

### 4.2 Webhook Verification

Webhook verification is a thin wrapper over:

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

- the current `stripe-go/v85` generated surface no longer exposes the old usage-record helper
  APIs
- current Stripe billing documentation has moved toward billing meters and meter events
- forge-metal already has its own metering source of truth in ClickHouse plus its own balance
  enforcement in TigerBeetle

If forge-metal later decides to mirror usage into Stripe Billing Meters, that is a separate
design and not an implementation detail of this package revision.

### 4.8 Test Inputs

The test-card references in this specification are pinned to current Stripe docs:

- `4000000000003220`: 3DS Required, succeeds after authentication
- `4000003720000278`: bypasses pending balance; it is not the canonical 3DS-required card

The package test harness must treat `4000003720000278` as a balance-availability test card,
not as a 3DS trigger.

---

## 5. Testing Specification

### 5.1 Layer 1: Property-Based Invariant Tests

Use `pgregory.net/rapid` `v1.2.0` against a real TigerBeetle instance and a real PostgreSQL
database.

The `rapid.StateMachine` interface itself is unchanged from `v1.1.0`:

```go
type StateMachine interface {
    Check(*rapid.T)
}
```

`rapid.StateMachineActions` in `v1.2.0` additionally accepts action methods of type
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

- TigerBeetle never reports negative available balance on either org account
- `remaining = amount - consumed - expired` for every `credit_grants` row
- no row has `consumed + expired > amount`
- sum of active `credit_grants.remaining` for `account_type='credit'` approximates the
  TigerBeetle credit balance for that org, allowing only pending-transfer tolerance
- sum of active `credit_grants.remaining` for `account_type='free_tier'` approximates the
  TigerBeetle free-tier balance for that org, allowing only pending-transfer tolerance
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

- `grant_balance_consistency` (alert): product-grant totals match TigerBeetle aggregate balances
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
7. verify TigerBeetle and PostgreSQL balances moved as expected

The canary is not allowed to depend on Stripe being available.

### 5.5 Layer 5: Simulation Harness (Design Only)

The simulation harness is not a substitute for real TigerBeetle or Stripe tests. It exists to
exercise failure choreography.

Required fault catalog:

| Fault | Expected behavior |
|-------|-------------------|
| PostgreSQL down during `DepositCredits` | TigerBeetle transfer may succeed, grant row missing, reconciliation detects |
| Expiry sweeper crashes mid-batch | idempotent replay via `CreditExpiryID(grantID)` |
| Concurrent `Reserve` and `ExpireCredits` on same org | serialized by advisory lock; no cross-product overspend |
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

    // Stripe webhook signing secret. Required. From STRIPE_WEBHOOK_SECRET env var.
    StripeWebhookSecret string

    // PostgreSQL connection string. Required. From BILLING_PG_DSN env var.
    PgDSN string

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

- `BILLING_PG_DSN`
- `STOREFRONT_PG_DSN`
- `STRIPE_SECRET_KEY`
- `STRIPE_WEBHOOK_SECRET`

### 6.3 Hot-Reloadable vs. Restart-Required

| Value | Hot-reloadable | Reason |
|-------|:-:|--------|
| `ReservationWindowSecs` | No | Active reservations use window from creation time |
| `PendingTimeoutSecs` | No | Baked into TigerBeetle transfers at creation |
| `StripeSecretKey` | No | Stripe client is immutable |
| `StripeWebhookSecret` | No | Key rotation during processing → false rejections |
| `PgDSN` | No | Connection pool created once |
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
- **DepositCredits** (grant row insert after TB transfer): TB transfer may succeed
  without corresponding grant row. Reconciliation detects.
- **Reserve**: if the advisory-lock transaction cannot start, fail closed.

### 7.3 Stripe Failures

Key policies:

- **CreateCheckoutSession/CreateSubscription**: return error to caller; no task row created.
- **CancelSubscription**: never mutate local state before the Stripe credit note or
  subscription API call succeeds.
- **Webhook verification**: reject bad signatures with 400; return 500 on persistence failure.

### 7.4 ClickHouse Failures

Key policies:

- **CheckQuotas**: fail closed.
- **Metering write after settle**: TigerBeetle remains authoritative; reconciliation backfills
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
  rows are updated idempotently (SET expired = remaining WHERE remaining > 0).

#### Concurrent Reserve and ExpireCredits on same org

- **Resolution:** both execute under `pg_advisory_xact_lock(org_id)`, so they do not observe
  concurrent PostgreSQL grant state for the same org. TigerBeetle clamping remains the second
  line of defense if a lockless caller is introduced by bug.

#### Subscription renewal deposit when org has been suspended

- **Policy:** Deposit the credits anyway. The credits belong to the org (they paid for
  them). Suspension prevents *consumption*, not *receipt*. When suspension is lifted,
  the credits are available.

#### Duplicate `invoice.paid` delivery

- **Resolution:** `tasks.idempotency_key` on the invoice ID blocks duplicate task creation.
  If a duplicate task somehow exists, the TigerBeetle transfer ID still deduplicates it.

#### Stripe account configured to leave subscriptions `past_due`

- **Resolution:** unsupported for this revision. The billing doctor must fail configuration
  validation if the Stripe account terminal action diverges from "cancel the subscription".

#### Credit note created, local cancellation update fails

- **Resolution:** the task remains retryable by invoice-based idempotency. Reconciliation
  compares Stripe invoice state and local subscription state and raises `reconciliation_alert`.

---

## 8. Integration Points

### 8.1 VM Orchestrator (in-process Go calls)

The orchestrator imports `internal/billing` directly. The billing lifecycle for a metered
product:

```go
// 1. Check quotas
result, err := billing.CheckQuotas(ctx, orgID, "sandbox", map[string]float64{
    "vcpu":           float64(req.VCPUs),
    "gib":            float64(req.MemMiB) / 1024.0,
    "concurrent_vms": currentConcurrentVMs,
})
if !result.Allowed {
    return nil, status.Error(codes.ResourceExhausted, "quota exceeded")
}

// 2. Reserve
reservation, err := billing.Reserve(ctx, billing.ReserveRequest{
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

The orchestrator does not know about plan rates. It passes resource dimensions; the
billing package resolves rates from PostgreSQL.

### 8.2 Stripe Webhook Handler

Next.js route → webhook verification → PostgreSQL task row → Go worker.

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

### 8.3 Cron Jobs

| Subcommand | Schedule | Action |
|------------|----------|--------|
| `forge-metal billing deposit-credits` | Monthly, 1st at 00:05 UTC | `DepositSubscriptionCredits()` |
| `forge-metal billing expire-credits` | Daily at 02:00 UTC | `ExpireCredits()` |
| `forge-metal billing reconcile` | Hourly | `Reconcile()` |
| `forge-metal billing trust-tier-evaluate` | Daily at 03:00 UTC | `EvaluateTrustTiers()` |
| `forge-metal billing canary` | Every 5 minutes | Reserve → sleep → settle → verify |
| `forge-metal billing dlq` | Manual | List/retry/acknowledge dead tasks |

### 8.4 Next.js API Routes

Thin Go HTTP API on localhost for balance and usage queries. Required read endpoints:

```
GET /internal/billing/v1/orgs/:org_id/balance
GET /internal/billing/v1/orgs/:org_id/products/:product_id/balance
GET /internal/billing/v1/orgs/:org_id/subscriptions
GET /internal/billing/v1/orgs/:org_id/grants?product_id=sandbox&active=true
GET /internal/billing/v1/orgs/:org_id/usage?product_id=sandbox&since=<ts>&limit=<n>
```

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

## Appendix B: Remaining Manual-Review Items

- The shared billing schema still lives in the PostgreSQL database named `sandbox` because that
  is current infrastructure truth. A future cutover may want to rename the database to
  `billing`, but that is outside this document-only task.
- The current Ansible billing credentials publish both `BILLING_PG_DSN` and `STOREFRONT_PG_DSN`.
  The storefront application integration should be reviewed when `internal/billing` is
  implemented so the intended database ownership is explicit in code.
- **Per-member spending policy** is deferred. `actor_id` is recorded in metering rows so
  historical attribution is available when the feature is designed. The mechanism (tags,
  role-based budgets, per-user caps, Zitadel metadata-driven groups) is intentionally left
  open. The org-level `overage_cap_units` ceiling is sufficient for v1.
