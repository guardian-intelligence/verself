# Billing Architecture

Usage-based billing with subscriptions, prepaid credits, and enterprise contracts. Stripe collects money and manages payment methods. PostgreSQL owns the commercial catalog and billing windows. TigerBeetle owns balance correctness and transfer history. ClickHouse owns the invoice read model and usage evidence.

Reference points in this repo:

- `src/platform/docs/identity-and-iam.md` for org/auth ownership boundaries.
- `src/sandbox-rental-service/docs/vm-execution-control-plane.md` for the reserve/settle split used by sandbox jobs.
- `src/apiwire/docs/wire-contracts.md` for wire-shape and generated-client conventions.

## System roles

| System | Role |
|---|---|
| Stripe | Recurring payment collection, Customer Portal, invoices, tax calculation, payment methods, refunds, and subscription lifecycle events. |
| PostgreSQL | Catalog tables, subscription contracts, entitlement policies/periods, grants, billing windows, invoices, adjustments, the billing outbox, and the billing service's source of truth for commercial state. |
| TigerBeetle | Financial ledger for credit grants, receivables, reservations, settlements, voids, refunds, and spend-cap enforcement. |
| ClickHouse | Append-only usage evidence plus billing event and metering projections used for invoice preview, statements, dashboards, and reconciliation. |

## Core model

The current model is SKU-driven.

A product is something billable. A plan chooses which SKUs are active for that product and what each SKU costs. A SKU belongs to a credit bucket. Buckets are the entitlement lanes customers see and consume against. Examples:

- `Compute` bucket, SKU `AMD EPYC 4484PX @ 5.66GHz`, quantity unit `vCPU-second`
- `Memory` bucket, SKU `Standard Memory`, quantity unit `GiB-second`
- `Block Storage` bucket, SKU `Premium NVMe`, quantity unit `GiB-second`

Metered usage prices are attached to the plan/SKU pair, not to ad hoc JSON on the plan row. Stripe subscription price IDs stay on the plan row for checkout because they identify the provider's recurring invoice item, not the internal SKU rate card. That is the cutover from the legacy plan-local JSON pricing model.

The supported entitlement layers map to `account` bucket -> `product` bucket -> SKU bucket. Entitlements are non-overlapping within a layer:

- `account`: any product bucket in the org
- `product`: any bucket for one product
- `bucket`: one product bucket fed by one or more SKUs

The `bucket` layer is the SKU-lane layer. If premium NVMe and non-premium disk need separate allowance behavior, they must be separate buckets and the corresponding SKUs must map to the correct bucket. Premium usage must not drain non-premium bucket grants, and non-premium usage must not drain premium bucket grants. Product-level or account-level grants are the only supported way to fund multiple buckets.

Free tier is not a plan. It is an entitlement policy that grants monthly `source = 'free_tier'` balances to every eligible org regardless of which subscription plan the org has. Upgrading from free usage to a paid plan must not remove the current month's free-tier grants; the reserve waterfall consumes matching free-tier grants before paid subscription or purchased credit grants.

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

Named entitlement lanes. These are the buckets free-tier and subscriptions fund, and invoice previews group by.

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

Commercial subscription tiers.

Key fields:

- `plan_id`
- `product_id`
- `display_name`
- `billing_mode` (`prepaid` or `postpaid`)
- `quotas`
- `is_default`
- `tier`
- `stripe_price_id_monthly`
- `stripe_price_id_annual`

A plan no longer carries metered pricing or included-credit JSON. It is a tier shell, Stripe subscription checkout reference, rate-card rows, and linked entitlement policies. Free-tier entitlements live outside the plan model so paid subscribers keep their free-tier allowances.

### `plan_sku_rates`

Plan-specific list prices for active SKUs.

Key fields:

- `plan_id`
- `sku_id`
- `unit_rate`
- `active`

This table is the rate card.

### `entitlement_policies`

Reusable grant templates. The policy says which source funds the grant, which entitlement layer receives it, how much is granted, when the policy is active, how the period is anchored, and whether partial periods prorate.

Key fields:

- `policy_id`
- `source` (`free_tier`, `subscription`, `purchase`, `promo`, `refund`)
- `product_id`
- `scope_type`
- `scope_product_id`
- `scope_bucket_id`
- `amount_units`
- `cadence`
- `anchor_kind` (`calendar_month` or `subscription_period`)
- `proration_mode`
- `policy_version`
- `active_from`
- `active_until`

Free-tier policies are universal scheduled entitlements. They are reconciled for every org on calendar-month anchors and are also synchronously self-healed on org creation and reserve. Subscription policies are linked to plans and become grants when a provider event confirms or extends a subscription period.

### `plan_entitlements`

Many-to-many link table between commercial plans and subscription entitlement policies.

Key fields:

- `plan_id`
- `policy_id`

This avoids embedding entitlements inside plan JSON and makes policy versioning explicit.

### `subscription_contracts`

The org's binding to a plan.

Key fields:

- `subscription_id`
- `contract_id`
- `org_id`
- `product_id`
- `plan_id`
- `cadence`
- `status`
- `payment_state`
- `entitlement_state`
- `billing_anchor`
- `grace_until`
- `current_period_start`
- `current_period_end`
- `stripe_subscription_id`
- `stripe_checkout_session_id`

Subscription contracts are created and mutated by Stripe Checkout and Stripe webhooks, but the local row is authoritative for billing logic. The payment state machine is separate from the entitlement state machine so late Stripe payment events can coexist with immediate entitlement availability during an explicit grace window.

### `entitlement_periods`

Durable period-level projection from entitlement policies. Period rows are the bridge between scheduled policy truth and grant issuance.

Key fields:

- `period_id`
- `org_id`
- `product_id`
- `source`
- `policy_id`
- `contract_id`
- `scope_type`
- `scope_product_id`
- `scope_bucket_id`
- `amount_units`
- `period_start`
- `period_end`
- `policy_version`
- `payment_state`
- `entitlement_state`
- `source_reference_id`
- `created_reason`

The `source_reference_id` is deterministic and source-specific. Free-tier source references are policy/period scoped. Subscription source references additionally include the local contract ID, so two different subscription contracts cannot collapse into one grant.

### `credit_grants`

Prepaid balances with explicit scope.

Scope classes:

- `bucket` grant: only one product bucket
- `product` grant: any bucket for one product
- `account` grant: any product bucket in the org

Source-funded grants are deterministic over `org_id`, `source`, scope, and `source_reference_id` so retries are idempotent without making Stripe the only reference namespace. Free-tier and subscription grants carry `entitlement_period_id`, `policy_version`, `period_start`, `period_end`, `starts_at`, and `expires_at`. Terminal subscription events close the local grant rows so remaining units stop funding future reservations even though TigerBeetle retains the immutable financial history.

Grant consumption follows the entitlement hierarchy first, then source priority inside each hierarchy layer:

1. matching bucket-scoped grants
2. matching product-scoped grants
3. account-scoped grants

Inside each layer, `source = 'free_tier'` is consumed before subscription, purchase, promo, or refund credit. This preserves bucket isolation while making free-tier benefits the first matching balance consumed.

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

Grant issuance writes the PostgreSQL grant row and an outbox event in the same transaction. The service-local projector claims undelivered rows with `FOR UPDATE SKIP LOCKED`, writes to `forge_metal.billing_events`, then marks rows delivered. ClickHouse is proof/read-model infrastructure; PostgreSQL remains authoritative.

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
- `pricing_phase`
- `rate_context`
- `usage_summary`
- `expires_at`
- `settled_at`
- `metering_projected_at`
- `last_projection_error`

### `invoices` and `invoice_line_items`

These are generated from projected ClickHouse metering plus adjustment rules. They are not the request-path source of truth.

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
4. Adjustment rules and entitlement/grant consumption.

The preview should show:

- SKU line items first, using the SKU display name and quantity unit.
- Bucket summaries next, using the bucket display name.
- Promotions and entitlements after that.
- Reserved but not yet finalized execution spend as a separate line.
- Remaining entitlement for the billing period and any purchased balance.

That keeps the preview structurally aligned with the final invoice rather than inventing a separate UI model.

## ClickHouse metering projection

ClickHouse stores the invoice read model, not the transaction ledger.

The metering table currently contains row-level usage evidence and projected charge units, including:

- `component_quantities`
- `component_charge_units`
- `bucket_charge_units`
- `bucket_subscription_units`
- `bucket_purchase_units`
- `bucket_promo_units`
- `bucket_refund_units`
- `bucket_receivable_units`
- `usage_evidence`

For sandbox jobs, trusted block storage evidence comes from the orchestrator's provisioned zvol size and is written as `rootfs_provisioned_bytes` in usage evidence. That gives the invoice preview a real storage signal instead of an inferred one.

## Stripe usage in this model

Stripe is only the payment and subscription boundary.

- Checkout creates subscriptions and collects recurring payments.
- Customer Portal manages cards, invoices, and cancellation.
- Webhooks (`checkout.session.completed`, `invoice.paid`, `customer.subscription.updated`, `customer.subscription.deleted`) keep PostgreSQL in sync.
- Stripe never becomes the source of truth for SKU pricing or metering.

That keeps billing logic inside this repo and keeps Stripe where it is strongest: collecting money and handling tax.

## Production verification gates

Use ClickHouse traces and metering rows as the proof point for the deployed path.

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
  plan_id,
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
   - Confirm grant issuance facts are visible in ClickHouse after org creation, free-tier reconciliation, purchase, or subscription invoice processing.

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

5. **Bucket totals reconcile**
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

6. **Invoice preview matches projection**
   - Confirm the latest billing statement line items use SKU display names, bucket display names, and quantity units from the catalog, not legacy `description` fields.

## Related docs

- `src/sandbox-rental-service/docs/vm-execution-control-plane.md`
- `src/platform/docs/identity-and-iam.md`
- `src/apiwire/docs/wire-contracts.md`
