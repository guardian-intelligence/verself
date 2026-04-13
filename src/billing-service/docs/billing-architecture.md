# Billing Architecture

This document describes the target billing architecture for usage-based billing, prepaid credits, self-serve Stripe plans, and bespoke enterprise contracts. PostgreSQL owns the commercial catalog, contracts, entitlement scheduling, grants, billing windows, and the durable outbox. TigerBeetle owns balance correctness and transfer history. ClickHouse owns the invoice read model, usage evidence, and projected billing facts.

Stripe is a payment and hosted billing provider. It is not the subscription domain model. A Stripe subscription is one provider-backed way to create and collect against a Forge Metal contract; an enterprise agreement is another contract form with manually authored terms and calendar-based recurrence. Both must flow through the same contract, phase, entitlement-period, and credit-grant machinery.

Reference points in this repo:

- `src/platform/docs/identity-and-iam.md` for org/auth ownership boundaries.
- `src/sandbox-rental-service/docs/vm-execution-control-plane.md` for the reserve/settle split used by sandbox jobs.
- `src/apiwire/docs/wire-contracts.md` for wire-shape and generated-client conventions.

## System roles

| System | Role |
|---|---|
| Stripe | Hosted Checkout, Customer Portal, payment methods, invoices, tax calculation, refunds, recurring payment collection, and subscription lifecycle signals for self-serve contracts. |
| PostgreSQL | Source of truth for catalog tables, contracts, provider bindings, contract changes, phases, recurring entitlement lines, entitlement periods, credit grants, billing windows, invoices, adjustments, and the billing outbox. |
| TigerBeetle | Financial ledger for credit grants, receivables, reservations, settlements, voids, refunds, and spend-cap enforcement. |
| ClickHouse | Append-only usage evidence plus billing event and metering projections used for invoice preview, statements, dashboards, and reconciliation. |

## Core model

The billing model is SKU-driven for usage and contract-driven for recurring entitlements.

A product is something billable. A SKU is a billable usage component. A SKU belongs to a credit bucket. Buckets are the entitlement lanes customers see and consume against. Examples:

- `Compute` bucket, SKU `AMD EPYC 4484PX @ 5.66GHz`, quantity unit `vCPU-second`
- `Memory` bucket, SKU `Standard Memory`, quantity unit `GiB-second`
- `Block Storage` bucket, SKU `Premium NVMe`, quantity unit `GiB-second`

Metered usage prices are attached to the plan/SKU pair, not to ad hoc JSON on the plan row. Provider price IDs on a plan are checkout references for prepackaged self-serve contracts; they are not the source of truth for SKU pricing or metering.

Recurring entitlements are modeled as:

```text
contracts
  -> contract_provider_bindings
  -> contract_changes
  -> contract_phases
  -> contract_entitlement_lines
  -> entitlement_periods
  -> credit_grants
  -> billing_outbox_events
```

A `contract` is the commercial agreement with an org. A `contract_phase` is the time-bounded version of that agreement: Hobby this month, Pro after an immediate upgrade, or a bespoke enterprise package for a signed term. A `contract_entitlement_line` is the recurring promise inside the phase: which source funds which scope, how much, and on what recurrence. An `entitlement_period` materializes one recurrence window. A `credit_grant` is the spendable TigerBeetle-backed balance issued from that period.

Stripe subscriptions and enterprise agreements therefore differ only at the provider and recurrence edges:

- Stripe self-serve phases use catalog plans, Stripe provider bindings, Stripe period anchors, and provider events to confirm payment and period boundaries.
- Enterprise phases use bespoke entitlement lines, optional manual provider bindings, calendar-day anchors, and internal reconciliation to create period boundaries.

The supported entitlement scopes form a tightest-to-widest funnel: `sku` -> `bucket` -> `product` -> `account`. Entitlements are non-overlapping within a layer:

- `sku`: one specific SKU within one product bucket
- `bucket`: one product bucket, fed by any of its SKUs
- `product`: any bucket for one product
- `account`: any product bucket in the org

The `bucket` layer is the SKU-lane layer. If premium NVMe and non-premium disk need separate allowance behavior, they must be separate buckets and the corresponding SKUs must map to the correct bucket. Premium usage must not drain non-premium bucket grants, and non-premium usage must not drain premium bucket grants. Product-level or account-level grants are the only supported way to fund multiple buckets.

Free tier is not a contract and not a plan. It is a universal scheduled entitlement policy that grants monthly `source = 'free_tier'` balances to every org regardless of which paid contract the org has. Upgrading from free usage to any paid contract must not remove the current month's free-tier grants; the reserve waterfall consumes matching free-tier grants before recurring contract grants or purchased credit grants.

## PostgreSQL catalog and state

### `products`

One row per billable product.

Key fields:

- `product_id`
- `display_name`
- `meter_unit`
- `billing_model`
- `reserve_policy`

The product owns the reserve policy because the liability shape is product-specific, not caller-specific.

### `credit_buckets`

Named entitlement lanes. These are the buckets free-tier and recurring contract lines fund, and invoice previews group by.

Key fields:

- `bucket_id`
- `display_name`
- `sort_order`

### `skus`

The billable units within a product.

Key fields:

- `sku_id`
- `product_id`
- `bucket_id`
- `display_name`
- `quantity_unit`
- `active`

A SKU answers two questions: what line item name should the customer see, and which bucket should usage drain from.

### `plans`

Prepackaged commercial tiers.

Key fields:

- `plan_id`
- `product_id`
- `display_name`
- `billing_mode` (`prepaid` or `postpaid`)
- `is_default`
- `tier`
- `active`
- `currency`
- `monthly_amount_cents`
- `annual_amount_cents`
- `stripe_price_id_monthly`
- `stripe_price_id_annual`

A plan is a reusable packaging template, not an active customer agreement. Self-serve Stripe flows use plan price IDs to create provider subscriptions and to request provider subscription-item changes. Enterprise contracts may reference a plan for display and rate-card inheritance, but bespoke enterprise terms must be represented by contract phases and entitlement lines.

A plan no longer carries included-credit JSON. Plan entitlements are linked through `plan_entitlements` and copied into contract entitlement lines when a catalog-plan phase is created.

### `plan_sku_rates`

Plan-specific list prices for active SKUs.

Key fields:

- `plan_id`
- `sku_id`
- `unit_rate`
- `active`

This table is the product/SKU rate card used for reservation cost calculations and invoice line items.

### `entitlement_policies`

Reusable grant templates for catalog plans and universal scheduled grants.

Key fields:

- `policy_id`
- `source` (`free_tier`, `contract`, `purchase`, `promo`, `refund`)
- `product_id`
- `scope_type`
- `scope_product_id`
- `scope_bucket_id`
- `scope_sku_id`
- `amount_units`
- `cadence`
- `anchor_kind` (`calendar_month`, `provider_period`, `anniversary`, or `calendar_month_day`)
- `proration_mode`
- `policy_version`
- `active_from`
- `active_until`

Free-tier policies remain policy-driven because they apply universally to orgs and do not belong to customer contracts. Catalog-plan contract lines are derived from plan-linked policies when a phase is created. Bespoke enterprise lines can be authored directly without first creating reusable plan policies.

### `plan_entitlements`

Many-to-many link table between catalog plans and entitlement policies.

Key fields:

- `plan_id`
- `policy_id`

This avoids embedding entitlements inside plan JSON and makes policy versioning explicit. Creating a Hobby or Pro phase copies the active linked policies into `contract_entitlement_lines`, preserving the exact policy version used by that customer phase.

### `contracts`

One row per commercial agreement with an org.

Key fields:

- `contract_id`
- `org_id`
- `display_name`
- `contract_kind` (`self_serve`, `enterprise`, `internal`)
- `state` (`draft`, `pending_activation`, `active`, `past_due`, `suspended`, `cancel_scheduled`, `ended`, `voided`)
- `payment_state` (`not_required`, `pending`, `paid`, `failed`, `uncollectible`, `refunded`)
- `entitlement_state` (`scheduled`, `active`, `grace`, `closed`, `voided`)
- `currency`
- `starts_at`
- `ends_at`
- `current_period_start`
- `current_period_end`
- `grace_until`
- `cancel_at_period_end`
- `cancel_at`
- `closed_at`

The contract row is provider-neutral. It should not carry a durable `plan_id`; plan identity is phase-scoped. This prevents historical Hobby grants from being relabeled as Pro when the customer upgrades the same underlying Stripe subscription.

The contract payment state machine is separate from the entitlement state machine. A late Stripe invoice, manual enterprise payment collection, or explicit grace window can leave a contract `payment_state = 'pending'` or `failed` while entitlement availability remains `grace` instead of immediately failing customer requests.

### `contract_provider_bindings`

Optional external provider identity for a contract.

Key fields:

- `binding_id`
- `contract_id`
- `provider` (`stripe`, `manual`)
- `provider_object_type` (`subscription`, `invoice`, `manual_contract`, or provider-specific object type)
- `provider_object_id`
- `provider_customer_id`
- `sync_state` (`none`, `pending`, `synced`, `error`)
- `metadata`

A Stripe self-serve contract has a binding like `provider = 'stripe'`, `provider_object_type = 'subscription'`, and `provider_object_id = 'sub_...'`. An enterprise contract may have no binding, a `manual` binding, or a later CRM/ERP binding without changing entitlement issuance.

### `contract_changes`

Durable transition requests for creation, upgrades, downgrades, cancellation, renewal, and amendments.

Key fields:

- `change_id`
- `contract_id`
- `org_id`
- `change_type` (`create`, `upgrade`, `downgrade`, `cancel`, `renew`, `amend`)
- `timing` (`immediate`, `period_end`, `specific_time`)
- `requested_effective_at`
- `actual_effective_at`
- `from_phase_id`
- `to_phase_id`
- `target_plan_id`
- `state` (`requested`, `provider_pending`, `awaiting_payment`, `scheduled`, `applied`, `failed`, `canceled`)
- `provider`
- `provider_request_id`
- `provider_invoice_id`
- `idempotency_key`
- `failure_reason`
- `requested_by`

This table is the seam for fault injection and for unifying self-serve and enterprise behavior. Stripe changes move through provider-pending and payment states as webhooks arrive. Enterprise amendments can move directly from `requested` to `scheduled` or `applied` after internal approval.

### `contract_phases`

Time-bounded versions of a contract.

Key fields:

- `phase_id`
- `contract_id`
- `org_id`
- `product_id`
- `plan_id`
- `provider_price_id`
- `provider_subscription_item_id`
- `phase_kind` (`catalog_plan`, `bespoke`)
- `state` (`scheduled`, `pending_payment`, `active`, `grace`, `superseded`, `closed`, `voided`)
- `payment_state`
- `entitlement_state`
- `effective_start`
- `effective_end`
- `superseded_by_phase_id`
- `created_reason`

A phase is the unit that upgrades and downgrades operate on. Hobby -> Pro does not mutate one plan field in place; it closes or supersedes the Hobby phase and creates a Pro phase. Pro -> Hobby at period end keeps the Pro phase active until its end and creates a scheduled Hobby phase that activates at the next period boundary. `phase_kind = 'catalog_plan'` requires a plan and, for Stripe-backed phases, the provider price and subscription item identity needed to update the existing Stripe subscription. `phase_kind = 'bespoke'` can omit `plan_id` and carry directly authored entitlement lines.

There must be no overlapping active entitlement lines for the same contract, product, scope type, and scope target. Scheduled future phases can overlap in planning time only if their effective intervals and line scopes do not overlap when activated.

### `contract_entitlement_lines`

Recurring entitlement promises inside a contract phase.

Key fields:

- `line_id`
- `phase_id`
- `contract_id`
- `org_id`
- `product_id`
- `source` (`contract`)
- `scope_type`
- `scope_product_id`
- `scope_bucket_id`
- `scope_sku_id`
- `amount_units`
- `recurrence_interval` (`month`, `year`)
- `recurrence_anchor_kind` (`provider_period`, `anniversary`, `calendar_month_day`)
- `recurrence_anchor_day`
- `recurrence_timezone`
- `proration_mode`
- `policy_version`
- `active_from`
- `active_until`

For Stripe self-serve phases, lines normally use `recurrence_anchor_kind = 'provider_period'`, meaning the Stripe invoice or subscription period defines the entitlement window. For enterprise phases, lines normally use `calendar_month_day` and a timezone so the contract can renew on a fixed calendar day regardless of signup anniversary.

Lines are copied from plan policies for catalog-plan phases and authored directly for bespoke phases. This keeps upgrades/downgrades and enterprise amendments on the same state machine.

### `entitlement_periods`

Durable period-level projection from free-tier policies or contract entitlement lines. Period rows are the bridge between scheduled entitlement truth and grant issuance.

Key fields:

- `period_id`
- `org_id`
- `product_id`
- `source`
- `policy_id`
- `contract_id`
- `phase_id`
- `line_id`
- `scope_type`
- `scope_product_id`
- `scope_bucket_id`
- `scope_sku_id`
- `amount_units`
- `period_start`
- `period_end`
- `policy_version`
- `payment_state`
- `entitlement_state`
- `provider_invoice_id`
- `provider_event_id`
- `source_reference_id`
- `created_reason`

The `source_reference_id` is deterministic and source-specific. Free-tier references are policy/period scoped. Contract references include `contract_id`, `phase_id`, `line_id`, policy version, and period boundaries, so two phases under the same contract cannot collapse into one grant.

For `source = 'contract'`, `contract_id`, `phase_id`, and `line_id` are required. For `source = 'free_tier'`, those fields must be empty. This keeps universal free-tier recurrence independent from paid contract recurrence.

### `credit_grants`

Prepaid balances with explicit scope.

Scope classes (tightest to widest):

- `sku` grant: one SKU within one product bucket
- `bucket` grant: only one product bucket, fed by any of its SKUs
- `product` grant: any bucket for one product
- `account` grant: any product bucket in the org

Key fields:

- `grant_id`
- `org_id`
- `scope_type`
- `scope_product_id`
- `scope_bucket_id`
- `scope_sku_id`
- `amount`
- `source`
- `source_reference_id`
- `entitlement_period_id`
- `policy_version`
- `starts_at`
- `period_start`
- `period_end`
- `expires_at`
- `closed_at`
- `closed_reason`

Source-funded grants are deterministic over `org_id`, `source`, scope, and `source_reference_id` so retries are idempotent without making Stripe the only reference namespace. Free-tier and contract grants carry `entitlement_period_id`, `policy_version`, `period_start`, `period_end`, `starts_at`, and `expires_at`. Terminal or superseding phase events close the local grant rows for the affected phase so remaining units stop funding future reservations even though TigerBeetle retains the immutable financial history.

Grant consumption is two-level precedence: scope tightness first, source priority second. The constants live in `internal/billing/grant_funding_plan.go` and are also baked into the entitlements view's row sort:

```go
GrantScopeFundingOrder  = []GrantScopeType{ GrantScopeSKU, GrantScopeBucket, GrantScopeProduct, GrantScopeAccount }
GrantSourceFundingOrder = []GrantSourceType{ SourceFreeTier, SourceContract, SourcePurchase, SourcePromo, SourceRefund }
```

The legacy `subscription` source name should be treated as an implementation detail to migrate away from; the target domain source for any recurring paid agreement is `contract`. Stripe Hobby, Stripe Pro, and enterprise MSA credits all drain at the same priority because they are all recurring contract entitlements.

`planGrantFunding` walks the outer scope loop tightest-first, and inside each (scope, source) step it drains grants in expires-asc order. SKU-scoped grants only fund charges that name the SKU; bucket-only charge lines fall through them. Bucket isolation is preserved at every layer: a `premium_nvme` SKU grant can never drain into a `compute` charge, and a `premium_disk` bucket grant can never drain into a `regular_disk` charge. `source = 'free_tier'` always burns first inside a given scope, so paid balances last as long as possible.

### `billing_outbox_events`

Durable PostgreSQL outbox for facts that must reach ClickHouse.

Key fields:

- `event_id`
- `event_type`
- `aggregate_type`
- `aggregate_id`
- `org_id`
- `product_id`
- `occurred_at`
- `payload`
- `delivered_at`
- `delivery_error`
- `attempts`

Grant issuance writes the PostgreSQL grant row and an outbox event in the same transaction. Contract creation, provider event ingestion, contract changes, phase transitions, and catalog reconciliation also write outbox events in the same transaction as their authoritative PostgreSQL state change. The service-local projector claims undelivered rows with `FOR UPDATE SKIP LOCKED`, writes to `forge_metal.billing_events`, then marks rows delivered. ClickHouse is proof/read-model infrastructure; PostgreSQL remains authoritative.

Expected event types include:

- `contract_created`
- `contract_change_requested`
- `contract_change_applied`
- `contract_phase_started`
- `contract_phase_closed`
- `contract_provider_event`
- `grant_issued`
- `catalog_plan_reconciled`

### `billing_windows`

The request-path financial state machine.

Key fields:

- `window_id`
- `org_id`
- `product_id`
- `source_type`
- `source_ref`
- `window_seq`
- `state` (`reserving`, `reserved`, `settled`, `voided`, `denied`, `failed`)
- `reservation_shape`
- `reserved_quantity`
- `actual_quantity`
- `billable_quantity`
- `reserved_charge_units`
- `billed_charge_units`
- `writeoff_charge_units`
- `pricing_contract_id`
- `pricing_phase_id`
- `pricing_plan_id`
- `pricing_phase`
- `rate_context`
- `usage_summary`
- `expires_at`
- `settled_at`
- `metering_projected_at`
- `last_projection_error`

### `invoices` and `invoice_line_items`

These are generated from projected ClickHouse metering plus adjustment rules. They are not the request-path source of truth.

## Contract state machines

There is not a separate state machine for enterprise contracts. The same state machines apply to every recurring paid agreement; only the provider binding and recurrence anchor differ.

### Contract lifecycle

```text
draft -> pending_activation -> active
active -> past_due
past_due -> active
active -> suspended
suspended -> active
active -> cancel_scheduled
cancel_scheduled -> ended
active -> ended
any non-terminal -> voided
```

`pending_activation` means the agreement exists but should not yet issue active paid grants. For Stripe, this usually means Checkout or a provider update exists but payment has not been confirmed. For enterprise, this may mean the agreement is signed but the service date has not arrived.

`past_due` and `suspended` are separate because late payment may preserve entitlements during grace, while suspension should block or narrow entitlement availability according to policy.

### Phase lifecycle

```text
scheduled -> pending_payment
scheduled -> active
pending_payment -> active
pending_payment -> grace
grace -> active
grace -> closed
active -> superseded
active -> closed
any non-terminal -> voided
```

A phase can be scheduled before it is effective, pending payment before its grants activate, active while it funds reservations, grace while payment is late but service remains available, superseded by an upgrade, closed at a period boundary, or voided when it should be treated as if it never became effective.

### Change lifecycle

```text
requested -> provider_pending -> awaiting_payment -> applied
requested -> scheduled -> applied
requested -> failed
provider_pending -> failed
awaiting_payment -> failed
scheduled -> canceled
```

Stripe upgrades usually pass through `provider_pending` and `awaiting_payment`. Enterprise amendments can often go from `requested` to `scheduled` or `applied` without provider state.

## Recurring entitlement scheduling

Recurring grants are materialized from durable rows; they are not computed from provider state on the reservation hot path.

- Free-tier eligibility is universal: every org gets the configured free-tier policies. The billing-side org provisioning path must synchronously materialize the current free-tier `entitlement_periods`, `credit_grants`, and `grant_issued` outbox facts before the org can submit billable usage. Reserve also self-heals the same deterministic current-period rows before funding, so a delayed reconciler cannot create a customer-visible entitlement miss.
- Contract eligibility is phase state plus entitlement-line recurrence. Only `active` and `grace` phases can materialize spendable contract periods. `scheduled` and `pending_payment` phases are planning state and must not fund reservations unless an explicit grace transition has been recorded.
- Stripe `provider_period` lines materialize from provider period boundaries carried by Checkout, invoice, and subscription events. Enterprise `calendar_month_day` lines materialize from the contract timezone and anchor day. `anniversary` anchors are available for non-Stripe provider flows that should renew from the service start date rather than the calendar.
- Period and grant identifiers are deterministic over org, source, scope, contract, phase, line, policy version, and period boundaries. Retrying org provisioning, webhook handling, reconciliation, or reserve self-healing must converge on the same rows.
- River can execute recurring reconciliation, provider retries, and outbox projection. PostgreSQL rows remain the entitlement truth. A request must not depend on a background job having run on time; it either creates the missing deterministic current-period entitlement rows in the request transaction or fails because the contract state/policy says the org is not entitled.

## Upgrade, downgrade, and cancellation semantics

### Free tier to paid contract

Free tier remains independent. Creating a paid contract does not close or decrement free-tier grants.

Flow:

1. Create a self-serve `contract` in `pending_activation`.
2. Create a Stripe `contract_provider_binding` after Checkout returns a subscription ID.
3. Create a catalog-plan `contract_phase` for the selected plan in `pending_payment` or `scheduled`.
4. Copy the plan's linked entitlement policies into `contract_entitlement_lines` for that phase.
5. On `invoice.paid` or an explicitly accepted provider event, set the contract and phase entitlement state to `active`.
6. Materialize `entitlement_periods` for the provider period.
7. Issue `credit_grants` and outbox `grant_issued` events.

### Immediate upgrade

Default for Hobby -> Pro.

1. Insert `contract_changes(change_type = 'upgrade', timing = 'immediate')` with the current phase and target plan.
2. Ask the provider adapter to update the existing provider subscription item to the target plan's provider price.
3. Keep the old phase active until payment or an explicitly accepted grace decision arrives.
4. On provider success, set the old phase `effective_end = actual_effective_at` and `state = 'superseded'`.
5. Create the new phase with `effective_start = actual_effective_at` and `effective_end = current_period_end` if the provider period is known.
6. Copy target plan policies into new phase entitlement lines.
7. Materialize prorated entitlement periods for the remaining provider period.
8. Issue new grants and close only the old phase's future-funding grants. Do not close free-tier grants and do not mutate old TigerBeetle history.

### Period-end downgrade

Default for Pro -> Hobby.

1. Insert `contract_changes(change_type = 'downgrade', timing = 'period_end')`.
2. Ask the provider adapter to schedule the downgrade at the provider period boundary, or store an internal scheduled change for non-provider contracts.
3. Keep the current Pro phase active until `current_period_end`.
4. Create a scheduled Hobby phase with `effective_start = current_period_end`.
5. At the boundary, close the Pro phase and activate the Hobby phase after payment or grace rules allow.
6. Issue Hobby grants for the new period.

This matches user expectations: downgrades should not take away paid capacity before the period they already paid for ends.

### Cancellation

Default for paid -> free is period-end cancellation.

1. Insert `contract_changes(change_type = 'cancel', timing = 'period_end')`.
2. Mark the contract `cancel_scheduled` and preserve the active phase until its period end.
3. At the boundary, close the active phase, close remaining phase grants, and mark the contract `ended`.
4. Free-tier grants continue independently and are reconciled on their calendar schedule.

Immediate cancellation is reserved for explicit admin actions, fraud, or payment terminality, and must record a closed reason.

## Entitlements view

`GET /internal/billing/v1/orgs/{org_id}/entitlements` returns the same grants the funder will consume, grouped into customer-facing slots by `(scope, product, bucket, sku)`. The view-model deliberately refuses to sum across slots because the moment a customer holds any credit narrower than account scope, a single top-line balance is dishonest about what the next reservation can actually spend.

The shape:

- A `universal` slot carries every `account`-scope source total.
- A `products[]` array carries one section per product.
- Each product section can carry a `product_slot` for product-scope grants.
- Each bucket section can carry a `bucket_slot` for bucket-scope grants and `sku_slots[]` for SKU-scope grants.
- Each slot surfaces source totals keyed by source and source identity. Non-contract sources collapse by source. Contract sources should preserve enough identity to distinguish Hobby, Pro, and enterprise grants when multiple phases are visible during a transition.

Within a bucket table, rows are sorted bucket-scope-first then SKU-scope, then by `GrantSourceFundingOrder`. Across the view, the cell-level next-to-spend position is a load-bearing claim about funder behavior. The contract is pinned by `entitlements_view_funding_test.go`: every cell projects back to a `scopedGrantBalance` and runs `planGrantFunding` against a representative charge, asserting the funder's first leg matches the cell's top entry. Any future change to `GrantScopeFundingOrder`, `GrantSourceFundingOrder`, or the view's sort logic must keep that test green or the customer-facing claim drifts from reality.

## Reservation and finalization invariant

Reservation is a financial lock, not a final charge.

1. Reserve creates the `billing_windows` row.
2. TigerBeetle receives the pending reservation transfers.
3. PostgreSQL stores the immutable rate context and funding context.
4. Settle computes actual usage, posts the final spend, and voids any remainder.
5. Void releases the hold and leaves no customer charge.

That invariant is the same for time-based sandbox execution, token/request metering, and storage evidence. The request path never mutates settled truth.

## Invoice preview

Invoice preview is built from the same data the month-end invoice uses:

1. Settled billing windows in PostgreSQL.
2. Projected metering rows in ClickHouse.
3. SKU metadata from the catalog tables.
4. Contract, phase, and entitlement-period metadata from PostgreSQL.
5. Adjustment rules and entitlement/grant consumption.

The preview should show:

- SKU line items first, using the SKU display name and quantity unit.
- Bucket summaries next, using the bucket display name.
- Free-tier, contract, purchased, promo, and refund funding after that.
- Reserved but not yet finalized execution spend as a separate line.
- Remaining entitlement for the billing period and any purchased balance.

That keeps the preview structurally aligned with the final invoice rather than inventing a separate UI model.

## ClickHouse metering projection

ClickHouse stores the invoice read model, not the transaction ledger.

The target metering projection contains row-level usage evidence and projected charge units, including:

- `pricing_contract_id`
- `pricing_phase_id`
- `pricing_plan_id`
- `pricing_phase`
- `component_quantities`
- `component_charge_units`
- `bucket_charge_units`
- `bucket_free_tier_units`
- `bucket_contract_units`
- `bucket_purchase_units`
- `bucket_promo_units`
- `bucket_refund_units`
- `bucket_receivable_units`
- `usage_evidence`

The current implementation may still use `subscription` field names in the ClickHouse row schema. The target model should migrate those projection names to `contract` while preserving backwards-compatible reads during rollout.

For sandbox jobs, trusted block storage evidence comes from the orchestrator's provisioned zvol size and is written as `rootfs_provisioned_bytes` in usage evidence. That gives the invoice preview a real storage signal instead of an inferred one.

## Stripe usage in this model

Stripe is one provider adapter for self-serve contracts.

- Checkout creates a provider subscription and collects the initial payment method.
- Customer Portal manages cards, invoices, and cancellation where appropriate.
- Provider events such as `checkout.session.completed`, `invoice.paid`, `invoice.payment_failed`, `customer.subscription.updated`, and `customer.subscription.deleted` are translated into `contract_provider_event` facts and contract/change/phase mutations.
- Stripe test clocks and sandbox environments should be used to validate provider-period recurrence, mid-cycle upgrades, downgrades, renewals, cancellation, and payment failures.
- Stripe never becomes the source of truth for contract phases, SKU pricing, entitlement scope, grant funding, or metering.

That keeps billing logic inside this repo and keeps Stripe where it is strongest: collecting money, handling tax, and emitting provider lifecycle signals.

## Enterprise usage in this model

Enterprise contracts use the same tables and state machines.

- A bespoke enterprise agreement creates a `contracts` row with `contract_kind = 'enterprise'`.
- The active terms live in `contract_phases` with `phase_kind = 'bespoke'`.
- Recurring allowances live in `contract_entitlement_lines` with `recurrence_anchor_kind = 'calendar_month_day'`, an anchor day, and a contract timezone.
- Amendments, renewals, upgrades, downgrades, and cancellations use `contract_changes`.
- Payment collection can be manual at first and later integrated through a provider binding without changing entitlement generation.

The design must not fork into a second enterprise billing schema. Enterprise is a contract kind and recurrence configuration, not a parallel billing engine.

## Production verification gates

Use ClickHouse traces, metering rows, and billing events as the proof point for the deployed path.

1. **Reservation trace present**
   - Confirm a sandbox job or billed workload produced a reservation trace in `billing-service`.
   - Query for the matching `window_id` in `default.otel_logs` and the `forge_metal.metering` row.

2. **SKU projection present**
   - Confirm the metering row contains SKU-level charge maps, bucket totals, and usage evidence.
   - Example query:

```sql
SELECT
  window_id,
  product_id,
  pricing_contract_id,
  pricing_phase_id,
  pricing_plan_id,
  pricing_phase,
  mapKeys(component_charge_units) AS sku_ids,
  component_quantities,
  component_charge_units,
  bucket_charge_units,
  usage_evidence
FROM forge_metal.metering
WHERE product_id = 'sandbox'
ORDER BY recorded_at DESC
LIMIT 5
```

3. **Storage evidence present**
   - Confirm `usage_evidence['rootfs_provisioned_bytes']` is non-zero for a sandbox execution that used a real zvol.

```sql
SELECT
  window_id,
  arrayElement(usage_evidence, 'rootfs_provisioned_bytes') AS rootfs_provisioned_bytes
FROM forge_metal.metering
WHERE product_id = 'sandbox'
ORDER BY recorded_at DESC
LIMIT 5
```

4. **Grant events present**
   - Confirm grant issuance facts are visible in ClickHouse after org creation, free-tier reconciliation, purchase, contract activation, phase change, or enterprise amendment.

```sql
SELECT
  event_id,
  event_type,
  aggregate_type,
  aggregate_id,
  org_id,
  product_id,
  payload,
  recorded_at
FROM forge_metal.billing_events
WHERE event_type = 'grant_issued'
ORDER BY recorded_at DESC
LIMIT 5
```

5. **Contract events present**
   - Confirm provider events, change requests, and phase transitions project into `forge_metal.billing_events`.

```sql
SELECT
  event_id,
  event_type,
  aggregate_type,
  aggregate_id,
  org_id,
  product_id,
  payload,
  recorded_at
FROM forge_metal.billing_events
WHERE event_type IN (
  'contract_created',
  'contract_change_requested',
  'contract_change_applied',
  'contract_phase_started',
  'contract_phase_closed',
  'contract_provider_event'
)
ORDER BY recorded_at DESC
LIMIT 20
```

6. **Bucket totals reconcile**
   - Confirm component charges sum into bucket charges, and bucket charges sum into the row charge units.

```sql
SELECT
  window_id,
  component_charge_units,
  bucket_charge_units,
  charge_units
FROM forge_metal.metering
WHERE product_id = 'sandbox'
ORDER BY recorded_at DESC
LIMIT 5
```

7. **Invoice preview matches projection**
   - Confirm the latest billing statement line items use SKU display names, bucket display names, quantity units, and contract/phase labels from the source-of-truth catalog and contract tables, not legacy description fields.

## Related docs

- `src/sandbox-rental-service/docs/vm-execution-control-plane.md`
- `src/platform/docs/identity-and-iam.md`
- `src/apiwire/docs/wire-contracts.md`
