# Billing Architecture

This document describes the target billing architecture for usage-based billing, prepaid credits, self-serve paid plans, and bespoke enterprise contracts.

The core thesis is:

```text
PostgreSQL says what is true and what is due.
River wakes workers to advance what is due.
Workers apply idempotent state transitions.
TigerBeetle records operational financial truth.
ClickHouse proves and presents the projection.
Forge Metal owns billing periods, plan policy, invoice artifacts, and consent.
Stripe is a payment rail and hosted payment-method provider, not the billing domain model.
```

PostgreSQL owns billing domain state and scheduling facts: catalog rows, contracts, contract changes, contract phases, entitlement lines, entitlement periods, credit-grant metadata, billing-cycle rows, billing-window metadata, invoices, invoice adjustments, provider bindings, provider events, ledger command rows, billing event rows, event-delivery queue rows, and reconciliation cursors. TigerBeetle owns accepted balance-changing account and transfer facts. River owns durable asynchronous execution of billing work derived from PostgreSQL state. A River job is a wakeup, retry, concurrency, and observability handle; it is not entitlement or ledger truth. If a River job is late, duplicated, retried, canceled, or reconstructed by reconciliation, deterministic PostgreSQL identifiers and persisted TigerBeetle IDs still converge to the same contract, phase, cycle, entitlement period, grant, ledger command, invoice, adjustment, and billing event facts.

Stripe is a payment and hosted billing provider. The target architecture does not use Stripe Subscriptions as the self-serve contract state machine. Forge Metal owns cadence, cycle rollover, contract changes, plan phases, entitlement materialization, invoice issue/finalization, overage consent, and dunning policy. Stripe is consulted when a card is vaulted, a hosted payment-method management surface is needed, an invoice is sent to Stripe for collection, a payment/refund/dispute event arrives, or Stripe Tax is later enabled.

Reference points in this repo:

- `src/platform/docs/identity-and-iam.md` for org/auth ownership boundaries.
- `src/sandbox-rental-service/docs/vm-execution-control-plane.md` for the reserve/settle split used by sandbox jobs and the existing River control-plane pattern.
- `src/apiwire/docs/wire-contracts.md` for wire-shape and generated-client conventions.

Provider reference points:

- Stripe subscription updates support prorations and can invoice prorations immediately with `proration_behavior = always_invoice`: <https://docs.stripe.com/billing/subscriptions/prorations> and <https://docs.stripe.com/api/subscriptions/update>.
- Paddle subscription updates require an explicit `proration_billing_mode` when replacing subscription items: <https://developer.paddle.com/build/subscriptions/replace-products-prices-upgrade-downgrade>.
- Recurly and Chargebee expose proration and timing as subscription-change policy knobs rather than one universal behavior: <https://docs.recurly.com/recurly-subscriptions/docs/change-subscription> and <https://www.chargebee.com/docs/billing/2.0/subscriptions/proration>.

Forge Metal follows the industry-standard price-side shape: immediate upgrades can charge the prorated positive price delta now; downgrades default to the next renewal unless explicitly overridden. Forge Metal must additionally define entitlement-side proration because Stripe, Paddle, Recurly, and Chargebee do not model our credit-bucket grant semantics.

## Non-negotiable invariants

- Every commercial entitlement must be derivable from PostgreSQL state.
- Every queued worker must be idempotent over a deterministic domain identity.
- Every worker transition must re-read PostgreSQL truth and use compare-and-swap, row locks, or equivalent state/version checks before side effects.
- River jobs may run late, early, duplicated, or after a retry. Workers must inspect state before doing work and exit cleanly when work is no longer due.
- Request-path reservation must not depend on a scheduled job having run on time.
- Request-path self-healing may create deterministic current-period entitlement rows and grants from already-authorized PostgreSQL state.
- Request-path self-healing must not call Stripe, infer payment facts, or depend on ClickHouse.
- Spendable balances, pending reservations, posted consumption, voids, top-up deposits, receivable accrual, receivable clearing, expiry sweeps, and financial corrections must be represented in TigerBeetle. PostgreSQL rows describe the domain operation; TigerBeetle accounts and transfers are the balance authority.
- PostgreSQL domain identifiers remain deterministic text identifiers. TigerBeetle account IDs, transfer IDs, and correlation IDs are separate 128-bit values generated with TigerBeetle's time-based ID scheme, persisted before dispatch, and reused on retry.
- A PostgreSQL row must not become customer-spendable or terminally settled until the corresponding TigerBeetle command has been acknowledged or reconciled as already posted.
- Billing cycles define invoice/finalization periods only. Contract phases define commercial policy intervals. Do not add a parallel `plan_bindings` table unless `contract_phases` is being renamed.
- Usage invoices must be computed from `billing_windows` and their captured `rate_context`/funding legs, not from live plan rates.
- Cycle rollover must not wait on Stripe, invoice rendering, email delivery, or payment collection. New usage must have a successor cycle even when invoice finalization is delayed or blocked.
- Provider webhooks must be durably recorded before being applied.
- ClickHouse projection may lag and must never be required for authorization, invoice issuance, or ledger correctness.
- Stripe never owns cadence, billing cycles, contract shape, SKU rates, grant scope, entitlement precedence, billing-window funding, invoice numbering, or metering.
- The free tier is universal and independent from paid contracts. Paid contract creation, upgrade, downgrade, cancellation, or payment failure must not close or decrement free-tier grants.
- Premium usage must not drain non-premium bucket grants, and non-premium usage must not drain premium bucket grants. Cross-bucket funding must be represented by product-level or account-level grants.
- Paid plan changes must be path-independent for a given effective time: a customer must not receive more total paid entitlement by stepping through intermediate self-serve plans than by moving directly from the current plan to the target plan. Immediate upgrades grant only the remaining positive entitlement delta between the target plan and the current plan; already-issued current-cycle paid grants remain available until their own period end instead of being replaced by a full prorated target-plan grant.
- A payment method on file is not overage consent. Free-tier orgs and paid orgs that enabled hard caps must not receive receivable funding legs for usage beyond authorized grants and prepaid balances.
- If usage without overage consent leaks through reservation or settlement, billing finalization must apply a deterministic automatic invoice adjustment before any customer charge is finalized. Automatic no-consent adjustments are capped at USD $0.99 per org per invoice finalization run; exceeding that cap blocks finalization and forces operator review instead of billing the customer.
- Invoices are immutable after issue. Corrections are explicit adjustment invoices or credit-note invoices linked to the original artifact.
- Forge Metal's stored invoice snapshot/rendered body is the canonical customer invoice artifact. Stripe invoice PDFs and hosted invoice pages are provider/payment artifacts that must reconcile to the Forge Metal invoice totals but do not become billing truth.

## System roles

| System | Role |
|---|---|
| PostgreSQL | Source of truth for catalog tables, contracts, contract changes, phases, entitlement lines, entitlement periods, credit-grant metadata, billing cycles, billing-window metadata, invoices, invoice adjustments, payment methods, provider bindings, provider events, ledger command state, billing event rows, event-delivery queue rows, schedule-defining timestamps, and reconciliation cursors. |
| River | Durable queue and scheduler runtime for provider-event application, contract-change execution, phase-boundary advancement, entitlement-period materialization, ledger command dispatch, ledger reconciliation, cycle rollover, invoice finalization, Stripe invoice collection, invoice email delivery, retries, and periodic repair. |
| TigerBeetle | Operational financial ledger for credit balances, top-up deposits, recurring allowance deposits, receivables, pending reservations, settlements, voids, refunds, expiry sweeps, corrections, showback transfers, and spend-cap enforcement. TigerBeetle is not the customer invoice artifact and not a substitute for PostgreSQL domain state. |
| ClickHouse | Append-only usage evidence plus billing event, metering, invoice, adjustment, and provider-event projections used for invoice preview, statements, dashboards, verification, and reconciliation. |
| Stripe | SetupIntents, PaymentMethods, Customer Portal, one-off invoice collection, payment intents, refunds, disputes, optional Stripe Tax, and hosted payment artifacts. Stripe Subscriptions are not part of the target domain model. |
| Mailbox service | Transactional delivery of Forge Metal invoice emails from the stored invoice artifact. Stripe invoice emailing is disabled in the target Forge Metal canonical-invoice path. |

## Design commitments and reversible choices

The load-bearing commitments are:

- Forge Metal owns cadence, contract shape, phases, entitlements, invoice artifacts, invoice numbering, overage consent, and finalization policy. Stripe Subscriptions must not become the self-serve contract state machine.
- PostgreSQL owns every schedule-defining timestamp and every durable state-machine row. A River job may be missing, duplicated, canceled, delayed, or reconstructed without changing billing truth.
- Cycle rollover and invoice finalization are separate transition paths. Rollover opens the successor cycle before Stripe collection, invoice email, or payment completion so valid usage is not blocked by a slow external rail.
- Forge Metal's issued invoice artifact is canonical. Stripe invoice PDFs, hosted invoice pages, and payment intents are provider artifacts that must reconcile back to the Forge Metal row.
- TigerBeetle account and transfer IDs are operational ledger identifiers only. They must not replace `contract_id`, `phase_id`, `cycle_id`, `period_id`, `grant_id`, `window_id`, `invoice_id`, `adjustment_id`, or `event_id` in public APIs, ClickHouse facts, or PostgreSQL domain relationships.
- Ledger command rows are durable side-effect state, not immutable billing facts. Immutable material facts live in `billing_events`; TigerBeetle transfers live in TigerBeetle; command rows bridge the two without becoming a third ledger.
- A payment method on file is not overage consent. Free-tier and hard-cap customers must not receive customer receivables for leaked no-consent usage.
- Enterprise agreements and self-serve Stripe-backed agreements use the same contract, phase, change, entitlement, cycle, invoice, and adjustment tables. Enterprise is a contract kind, phase kind, recurrence policy, collection method, and provider-binding choice, not a second billing engine.
- Self-serve catalog upgrades must be anti-arbitrage and path-independent at the same effective timestamp: charge the prorated positive price delta, preserve already-issued current-cycle paid grants until their own expiry, and issue only the prorated positive entitlement delta for the target phase.
- ClickHouse is proof/read-model infrastructure. It must not perform billing transitions, authorize usage, issue invoices, or decide ledger correctness.

The reversible implementation choices are:

- Exact River job names, queue names, and job granularity, as long as jobs remain deterministic over domain identity.
- Whether a first implementation relies on bounded repair scanners or transactionally enqueues every one-row River job with its domain row.
- Whether Stripe automatic collection is delegated to Stripe initially or replaced later by Forge Metal-owned dunning through `billing.payment.retry`.
- Whether dormant zero-usage zero-total cycles produce customer-facing invoice artifacts or only internal cycle records.
- ClickHouse projection table layout, as long as PostgreSQL remains authoritative for domain state, TigerBeetle remains authoritative for balances, and projections remain idempotent by deterministic identifiers.
- Exact numeric TigerBeetle account and transfer codes, as long as code meanings are stable after first production use and are stored in code/docs/registry together.

## Scheduling and queuing model

Scheduling and queuing are first-class billing verbs.

Scheduling is the act of declaring that work is due at or after a domain time. Billing schedules are encoded in PostgreSQL rows, not hidden inside River. Examples include `requested_effective_at`, `actual_effective_at`, `effective_start`, `effective_end`, `period_start`, `period_end`, `cycle.starts_at`, `cycle.ends_at`, `finalization_due_at`, `grace_until`, `next_materialize_at`, and `billing_event_delivery_queue.next_attempt_at`.

Queuing is the act of inserting a durable River job to execute one bounded transition derived from PostgreSQL state. River gives us retry, backoff, concurrency limits, delayed execution, periodic scans, OpenTelemetry spans, and transactional enqueueing. It does not replace the domain state machine.

A transition belongs in River when it has at least one of these properties:

- It is due at a future domain timestamp.
- It can be retried without making the caller wait.
- It performs an external side effect such as Stripe collection, mailbox delivery, or ClickHouse projection.
- It repairs or reconciles missing deterministic work from PostgreSQL state.
- It should be observable as a durable background transition with retry history.

A transition should stay in the request path or a single PostgreSQL transaction when it has one of these properties:

- It decides whether a customer request may start, continue, settle, or safely fail.
- It is required to preserve a financial lock or prevent a TigerBeetle reservation leak.
- It can be expressed as a PostgreSQL constraint, row lock, compare-and-swap update, deterministic upsert, or stored domain row without delayed execution.
- It would create a customer-visible outage if a queue worker were late.
- It would be incorrect if ClickHouse projection lagged.

Workers are thin executors. A worker must:

1. Load the authoritative PostgreSQL row by deterministic domain identity.
2. Verify the transition is still due and allowed.
3. Apply one bounded state-machine step with compare-and-swap semantics.
4. Write any immutable `billing_events` facts and corresponding `billing_event_delivery_queue` rows in the same transaction as the authoritative state change.
5. Enqueue follow-up River jobs in the same transaction when the transition creates new due work.
6. Exit without side effects when the row is already terminal, superseded, not yet due, or already applied.

Reconciliation reconstructs missing River work from PostgreSQL state. If a job is missing but a row is due, reconciliation enqueues another deterministic job. If a job is duplicated, the worker sees already-applied state and exits.

Target billing job kinds:

- `billing.provider_event.apply`: apply one durable provider event.
- `billing.contract_change.apply`: execute one contract change when due.
- `billing.phase_boundary.advance`: activate, close, supersede, or void phases at due boundaries.
- `billing.entitlement_period.ensure`: materialize one deterministic entitlement period and its grant.
- `billing.entitlement_reconcile.org`: repair current and next entitlement periods for one org.
- `billing.cycle.rollover`: close a cycle for usage, open the successor cycle, and enqueue finalization for the closed cycle.
- `billing.invoice.finalize`: compute the immutable invoice artifact, enforce overage consent, apply automatic adjustments, enforce the adjustment cap, allocate the invoice number, and mark the invoice as issued or blocked.
- `billing.invoice.stripe_collect`: create/finalize the Stripe invoice for a Forge Metal invoice that needs Stripe collection.
- `billing.invoice.email`: send the stored Forge Metal invoice email through mailbox-service.
- `billing.payment.retry`: run payment retry policy only when Forge Metal owns dunning instead of delegating automatic collection to Stripe.
- `billing.ledger.command_dispatch`: dispatch one durable TigerBeetle command, then mark the corresponding PostgreSQL rows posted, settled, voided, retryable, or dead-lettered.
- `billing.ledger.command_dispatch_pending`: repair stuck or missing ledger dispatch work.
- `billing.ledger.reconcile`: compare PostgreSQL ledger metadata to TigerBeetle accounts/transfers and emit drift facts.
- `billing.ledger.expire_grants`: sweep expired remaining grant balances to the expired-credit account.
- `billing.metering.project_window`: project one settled billing window into ClickHouse.
- `billing.event_delivery.project`: project one billing event fact into ClickHouse.
- `billing.event_delivery.project_pending`: repair stuck or missing event delivery projection jobs.

The first implemented River cut keeps bounded repair scanners as
`billing.metering.project_pending_windows`, `billing.event_delivery.project_pending`,
and `billing.entitlements.reconcile`, and adds the one-row
`billing.event_delivery.project` worker for precise event delivery. Producers
that cannot enqueue River transactionally with their current SQL transaction
still converge through the delivery queue plus periodic scanner; the target
shape is to enqueue one-row jobs in the same transaction as the domain row.

Job uniqueness must be derived from domain identity, not random worker identity:

- Provider event jobs key by `(provider, provider_event_id)`.
- Contract change jobs key by `change_id`.
- Phase boundary jobs key by `(phase_id, boundary_kind, boundary_at)`.
- Entitlement period jobs key by `period_id` or the deterministic period source tuple.
- Cycle rollover jobs key by `(cycle_id, ends_at)`.
- Invoice finalization jobs key by `invoice_finalization_id` or `cycle_id` when there is exactly one invoice per cycle.
- Stripe collection jobs key by `invoice_id`.
- Invoice email jobs key by `invoice_id`.
- Ledger command jobs key by `ledger_command_id`.
- Ledger reconciliation jobs key by `(reconcile_scope, org_id, product_id, reconcile_at_bucket)`.
- Grant expiry jobs key by `(grant_id, expires_at)`.
- Event delivery projection jobs key by `event_id`.
- Metering projection jobs key by `window_id`.

Billing-service must run its own River runtime against the billing PostgreSQL database. The sandbox-rental-service River runtime is a useful pattern, not a shared worker pool for billing. Billing workers need to enqueue jobs transactionally with billing domain rows, so their River tables and client belong in the billing database boundary. Ledger dispatch uses a dedicated billing ledger queue, not the generic ClickHouse delivery queue, because delayed financial posting and delayed analytic projection have different alerting and retry tolerances.

Billing correctness must not require River Pro-only workflow, sequence, durable-periodic-job, or dead-letter features. Domain tables carry the state machine, due timestamps, retry counters, and dead-letter status; River job rows are execution handles. Use River's transactional enqueueing, delayed execution, unique jobs, retries, and telemetry where available, and use bounded PostgreSQL repair scanners for any scheduling durability that must survive missing job rows.

River docs to keep near this design:

- River: <https://riverqueue.com/>

## TigerBeetle ledger model

TigerBeetle is the operational financial ledger for credit-unit balances. PostgreSQL is still the domain source of truth: it defines which org, product, contract, phase, cycle, grant, window, invoice, and adjustment exists and what state machine transition is allowed. TigerBeetle answers a narrower but load-bearing question: what balance exists, what amount is pending, and what immutable debit/credit movement has been accepted by the ledger.

TigerBeetle is not the legal invoice artifact, not a general-purpose customer-support database, and not a replacement for PostgreSQL constraints. It is also not a GAAP general ledger. The account names below deliberately track Forge Metal operational credit flows: issued allowance, purchased credits, pending usage locks, settled usage value, customer receivables, absorbed no-consent usage, and internal showback. Finance exports can map those flows into GAAP accounts later, but the billing hot path must not wait for that mapping.

The single supported TigerBeetle ledger for the initial product is ledger `1`, denominated in Forge Metal ledger units. The current USD scale is `100_000` ledger units per cent. Ledgers represent asset classes or materially different partitions; do not use one ledger per org in the initial design. Tenant, product, grant source, and business-time identity belong in PostgreSQL and TigerBeetle `user_data_*` fields.

### Ledger identity

PostgreSQL domain IDs are deterministic text IDs. Examples: `grant_id`, `window_id`, `cycle_id`, `contract_id`, `phase_id`, `invoice_id`, and `event_id`. They remain the IDs exposed in APIs, OpenAPI specs, ClickHouse projections, support queries, and billing events.

TigerBeetle object IDs are separate 128-bit IDs:

- `tb_account_id`: the TigerBeetle account ID for an operator account, grant account, or receivable account.
- `tb_transfer_id`: the TigerBeetle transfer ID for a deposit, reservation, settlement, void, receivable, payment-clearing, expiry, correction, or showback movement.
- `tb_correlation_id`: a query correlation value stored in TigerBeetle `user_data_128` and in PostgreSQL. It may point at a window, grant, invoice finalization, provider event, or ledger command, but it is not the domain ID.

TigerBeetle IDs must use the client `id()`/`types.ID()` time-based scheme and must be persisted before dispatch. Retries reuse the persisted ID list exactly. Do not derive TigerBeetle account or transfer IDs from SHA-256, FNV, ULID byte swaps, leg indexes, transfer kinds, or text domain IDs. Business idempotency comes from PostgreSQL unique keys and persisted command payloads; TigerBeetle idempotency comes from replaying the same TigerBeetle IDs.

`user_data_*` conventions:

- Grant accounts use `user_data_64 = org_id`, `user_data_32 = grant_source_code`, and `user_data_128 = grant tb_correlation_id`.
- Customer receivable accounts use `user_data_64 = org_id`, `user_data_32 = receivable_scope_code`, and `user_data_128 = cycle, invoice, or org/product receivable correlation ID depending on account granularity.
- Reservation, settlement, and void transfers use `user_data_128 = window tb_correlation_id`, `user_data_64 = business_clock_ms`, and `code = transfer reason`.
- Deposit, expiry, refund, and grant-correction transfers use `user_data_128 = grant tb_correlation_id`.
- Invoice finalization, no-consent adjustment, receivable-clearing, and payment-clearing transfers use `user_data_128 = invoice_finalization or invoice tb_correlation_id`.

TigerBeetle preview query APIs may be used by operator tools and reconciliation, but the hot path should use IDs already stored in PostgreSQL and `LookupAccounts`/`LookupTransfers`. PostgreSQL remains the index for text domain IDs.

### Account taxonomy

Account codes are stable protocol values once production data exists. The exact numeric assignments can change before the first production cut, but the target account set is:

| Code name | Cardinality | Balance nature | Required flags | Purpose |
|---|---:|---|---|---|
| `operator_stripe_external` | singleton | external source/sink | `history` | Represents Stripe/the outside payment rail for operational balancing. It is not spendable customer balance. |
| `operator_stripe_holding` | singleton | credit-positive clearing | `debits_must_not_exceed_credits`, `history` | Credits posted payment events, then funds purchased credit deposits or clears receivables. It should not run negative. |
| `operator_contract_allowance` | singleton or per product | debit-positive allowance source | `credits_must_not_exceed_debits`, `history` | Funds recurring paid contract allowance grants without tying entitlement continuity to Stripe collection timing. |
| `operator_free_tier_expense` | singleton or per product | debit-positive expense | `credits_must_not_exceed_debits`, `history` | Funds universal free-tier grants. |
| `operator_promo_expense` | singleton or per campaign | debit-positive expense | `credits_must_not_exceed_debits`, `history` | Funds promo and goodwill grants that create spendable balance. |
| `operator_internal_expense` | singleton or cost-center scoped | debit-positive expense | `credits_must_not_exceed_debits`, `history` | Funds dogfood/internal grants that should use the same product paths and net to zero on invoices. |
| `operator_writeoff_expense` | singleton or per product | debit-positive expense | `credits_must_not_exceed_debits`, `history` | Records absorbed no-consent usage and other non-recoverable policy writeoffs for showback. |
| `operator_adjustment_clearing` | singleton | credit-positive clearing | `history` | Counterparty for non-customer-balance invoice-adjustment showback movements. |
| `operator_expired_credits` | singleton or per product | credit-positive sink | `history` | Receives unused customer grant balance swept at expiry. |
| `operator_refund_clearing` | singleton | credit-positive clearing | `history` | Receives removed credit balance when an unspent top-up is refunded; provider cash movement is tracked through Stripe/payment state. |
| `operator_usage_settlement` | singleton or per product | credit-positive settlement | `debits_must_not_exceed_credits`, `history` | Receives posted usage value from grant-backed settlement. Do not treat this raw account as GAAP revenue without invoice classification. |
| `operator_billed_revenue` | singleton or per product | credit-positive settlement | `debits_must_not_exceed_credits`, `history` | Receives chargeable invoice and receivable accrual value for recurring charges, upgrade deltas, taxes, and consented overage. |
| `customer_grant` | one per `credit_grants` row | credit-positive spendable balance | `debits_must_not_exceed_credits`, `history` | Holds one scoped grant. Available balance is `credits_posted - debits_posted - debits_pending`. |
| `customer_receivable` | per org/product/cycle or per invoice | debit-positive receivable | `credits_must_not_exceed_debits`, `history` | Accrues explicitly consented postpaid overage or invoice receivables and is cleared by payment or adjustment. |

Operator singleton account IDs may be generated once and stored in `billing_ledger_account_registry`, or assigned fixed non-zero constants before production. Customer grant and receivable account IDs must be TigerBeetle time-based IDs generated when PostgreSQL creates the corresponding row. On boot and during reconciliation, billing verifies that registered operator accounts exist with the expected ledger, code, and flags.

### Transfer taxonomy

Transfer codes are stable protocol values once production data exists. Target transfer meanings:

| Code name | Debit account | Credit account | Pending? | Purpose |
|---|---|---|---:|---|
| `stripe_payment_in` | `operator_stripe_external` | `operator_stripe_holding` | no | Records a successful Stripe payment in ledger units. |
| `purchase_grant_deposit` | `operator_stripe_holding` | `customer_grant` | no | Creates spendable purchased/top-up credit after payment success. |
| `free_tier_grant_deposit` | `operator_free_tier_expense` | `customer_grant` | no | Creates spendable universal free-tier credit. |
| `contract_grant_deposit` | `operator_contract_allowance` | `customer_grant` | no | Creates spendable recurring paid contract allowance. |
| `promo_grant_deposit` | `operator_promo_expense` | `customer_grant` | no | Creates spendable promo/goodwill credit. |
| `internal_grant_deposit` | `operator_internal_expense` | `customer_grant` | no | Creates spendable dogfood/internal credit. |
| `reservation_grant` | `customer_grant` | `operator_usage_settlement` | yes | Locks grant-backed usage before work starts. |
| `reservation_receivable` | `customer_receivable` | `operator_billed_revenue` | yes | Locks explicitly consented postpaid overage before work starts. |
| `settlement_post` | references pending transfer | references pending transfer | no | Posts all or part of a pending reservation. |
| `settlement_void` | references pending transfer | references pending transfer | no | Releases all or part of a pending reservation. |
| `invoice_receivable_accrue` | `customer_receivable` | `operator_billed_revenue` | no | Accrues collectible invoice amounts that are not already represented by posted receivable reservation legs. |
| `grant_expiry_sweep` | `customer_grant` | `operator_expired_credits` | no | Removes unused grant balance at `expires_at`. |
| `refund_balance_remove` | `customer_grant` | `operator_refund_clearing` | no | Removes refundable unspent purchased balance. |
| `receivable_clear_payment` | `operator_stripe_holding` | `customer_receivable` | no | Clears a customer receivable after provider payment succeeds. |
| `no_consent_adjustment_showback` | `operator_writeoff_expense` | `operator_adjustment_clearing` | no | Records absorbed leaked usage for marketing/policy showback without creating customer debt. |
| `ledger_correction` | correction-specific | correction-specific | no or pending | Corrects an erroneous prior movement using the same `user_data_128` correlation as the original operation. |

Settlement transfers must use TigerBeetle `pending_id` to reference the reservation transfer. The transfer ID never encodes leg order, operation kind, or source. Leg order is stored in PostgreSQL `billing_window_ledger_legs.leg_seq`, and operation meaning is stored in the TigerBeetle `code` plus PostgreSQL command metadata.

### Ledger commands

Every TigerBeetle side effect is represented by a durable PostgreSQL ledger command before dispatch. The command stores operation, aggregate identity, command state, exact TigerBeetle account/transfer IDs, expected batch payload, retry metadata, lease metadata, and terminal error/DLQ metadata. Command rows are operational state, not immutable billing facts; successful domain transitions still emit `billing_events`.

Ledger command lifecycle:

```text
pending -> dispatching -> posted
pending -> dispatching -> retryable_failed -> pending
retryable_failed -> dispatching -> posted
retryable_failed -> dispatching -> dead_letter
posted -> reconciled
dead_letter -> pending (operator requeue, generation incremented)
```

Dispatch rules:

- Grant deposits and request-path reservations dispatch synchronously before returning customer-visible success.
- Settlement and void commands may be retried by River, but `billing_windows` must remain in a non-terminal `settling` or `voiding` state until TigerBeetle acknowledges the post/void transfers.
- Background commands such as expiry sweeps, receivable clearing, invoice showback, and corrections dispatch through River and are repaired by reconciliation.
- A command retry must submit the exact same TigerBeetle IDs and linked-chain layout. Changing amount, account, code, flags, or ordering requires a new command generation and an explicit correction plan.
- Linked transfer chains must close inside a single TigerBeetle request. Large sweeps split into independently atomic commands before hitting TigerBeetle's batch limit.

### Balance reads

Customer balance reads load grant metadata from PostgreSQL and balances from TigerBeetle:

1. PostgreSQL selects open, active, in-scope `credit_grants` whose `ledger_posting_state = 'posted'` and whose time window contains the business clock.
2. Billing batches `LookupAccounts(tb_account_id...)` for those grants.
3. For each `customer_grant` account, spendable availability is `credits_posted - debits_posted - debits_pending`.
4. Pending amount is `debits_pending`.
5. Customer-facing spent-by-source and spent-by-SKU is derived from settled `billing_window_ledger_legs` and metering projection, not from raw `debits_posted`, because expiry sweeps, refunds, and corrections also debit grant accounts.
6. The entitlements view groups the same grant metadata and TB balances into account/product/bucket/SKU slots. It must not synthesize a top-line balance that can be spent across incompatible scopes.

If TigerBeetle is unavailable, a balance read may return a dependency error or a clearly marked stale projection. It must not silently fall back to `credit_grants.amount - billing_windows.funding_legs`, because that would create a second financial truth path.

### Top-up flow

Purchased top-up credit is prepaid balance, not postpaid overage consent.

1. Stripe Checkout, PaymentIntent, or another provider collection path records the intended purchase with provider metadata containing `org_id`, `product_id`, purchase amount, and idempotency key.
2. The provider webhook is persisted in `billing_provider_events` before any balance mutation.
3. On `payment_intent.succeeded` or equivalent confirmed payment, PostgreSQL creates a deterministic `credit_grants(source = 'purchase')` row in `ledger_posting_state = 'pending'`, with a `tb_account_id`, `tb_correlation_id`, and deposit command IDs.
4. The ledger command creates the customer grant account and linked transfers for `stripe_payment_in` and `purchase_grant_deposit`, unless the payment-in transfer was already posted by an earlier command for the same provider object.
5. Only after TigerBeetle acknowledges the command does PostgreSQL mark the grant `ledger_posting_state = 'posted'`, emit `grant_issued`, and make the balance visible to reserve and balance reads.
6. Duplicate provider events converge on the same purchase grant and same persisted TigerBeetle IDs.

A payment method on file does not participate in this flow unless the customer explicitly initiates a purchase or invoice collection. A free-tier customer may store a card without authorizing top-up purchases or metered overage invoices.

### Grant funding flow

Every spendable grant is a TigerBeetle `customer_grant` account credited by exactly one issuance command generation. The funding account depends on source:

- `free_tier`: debit `operator_free_tier_expense`, credit `customer_grant`.
- `contract`: debit `operator_contract_allowance`, credit `customer_grant`.
- `purchase`: debit `operator_stripe_holding`, credit `customer_grant`.
- `promo`: debit `operator_promo_expense`, credit `customer_grant`.
- `refund` or manually restored balance: debit the policy-specific source account, credit `customer_grant`, and link back to the invoice/refund/correction artifact.
- `internal`: debit `operator_internal_expense`, credit `customer_grant`.

Free-tier and contract entitlement materialization may be triggered by org provisioning, reserve self-healing, or River recurrence work. All paths converge on the same deterministic PostgreSQL period/grant rows and the same persisted TigerBeetle command IDs.

### Reservation, settlement, and void flow

Reservation is the admission-control path:

1. PostgreSQL locks `(org_id, product_id)`.
2. Reserve self-heals current-period free-tier and already-active/grace contract grants from PostgreSQL facts.
3. PostgreSQL loads posted grant metadata and TigerBeetle balances, then builds a strict waterfall: scope tightness first, source priority second, paid tier priority third, oldest grant first.
4. PostgreSQL inserts `billing_windows(state = 'reserving')`, `billing_window_ledger_legs(state = 'pending_tb')`, and one ledger command containing linked pending transfers.
5. Billing dispatches the command synchronously to TigerBeetle.
6. If TigerBeetle accepts every pending transfer, PostgreSQL marks the window `reserved` and the legs `pending`.
7. If TigerBeetle rejects the command for insufficient credits, PostgreSQL marks the window `denied`; the caller must not start work.
8. If TigerBeetle is unavailable after the PostgreSQL command commit, the caller receives a retryable dependency error and the window remains `reserving` until command dispatch or reconciliation resolves it. A `reserving` window is not a valid reservation.

Settlement is the completion path:

1. The caller reports billable usage evidence. For sandbox time windows, billable quantity is milliseconds of guest workload runtime, not VM launch/setup latency.
2. PostgreSQL computes billable charge units from the captured `rate_context`, trims the already-reserved legs in stored waterfall order, and records no-consent writeoff evidence for any admitted but unauthorized excess.
3. PostgreSQL creates post/void command transfers referencing each pending reservation transfer by `pending_id`.
4. PostgreSQL stays in `settling` until TigerBeetle acknowledges the post/void command.
5. After acknowledgement, PostgreSQL marks legs `posted` or `voided`, marks the window `settled`, emits `billing_window_settled`, and enqueues metering projection.

Void is the safe-failure path:

1. PostgreSQL creates a void command for each still-pending reservation leg.
2. PostgreSQL stays in `voiding` until TigerBeetle acknowledges the voids or reconciliation proves they expired/voided.
3. After acknowledgement, PostgreSQL marks the window `voided`, emits `billing_window_voided`, and the grant balances become available again.

### Receivables and overage consent

Receivable-backed funding is allowed only when the customer has explicitly accepted the relevant overage model. A vaulted card is not enough.

For paid orgs with `overage_policy = 'bill_published_rate'`, reserve may add `reservation_receivable` legs after all eligible grants and prepaid balances are exhausted. Those legs debit a `customer_receivable` account and credit `operator_billed_revenue` as pending transfers. Settlement posts the actual billable amount and voids the unused remainder. Invoice finalization then renders the receivable into customer-facing invoice lines and collection jobs.

Recurring base charges, upgrade price deltas, taxes, and other invoice amounts that are not grant-backed usage are represented by PostgreSQL invoice lines first. When they create a collectible amount due, finalization creates or updates the relevant `customer_receivable` ledger account and posts a receivable-accrual command. Payment success clears that receivable through `operator_stripe_holding`. If collection happens before issue in a hosted payment flow, the provider event is still recorded first, and finalization reconciles the already-collected provider amount to the issued Forge Metal invoice.

For free-tier orgs and paid hard-cap orgs, reserve must not create receivable legs. If usage leaks through because of a race, stale reservation, retry, or bug, settlement records `writeoff_quantity` and `writeoff_charge_units` on the window but does not debit a customer receivable. Invoice finalization converts that evidence into deterministic system-policy `invoice_adjustments` within the USD $0.99 cap. The optional TigerBeetle showback movement is `no_consent_adjustment_showback`: debit `operator_writeoff_expense`, credit `operator_adjustment_clearing`, tagged to the invoice finalization. That movement records operator liability for analytics; it does not create spendable balance or customer debt.

If a receivable was created under consent and a later correction proves the customer did not authorize it, finalization must reverse or correct the receivable with a `ledger_correction` transfer correlated to the original window/invoice. Do not hide the correction by mutating the original transfer or silently dropping the invoice line.

### Showback and internal usage

Showback is an operator accounting projection over the same customer-facing machinery. It is not a separate usage path.

- Internal/dogfood usage receives grants from `operator_internal_expense` and drains through the same reservation and settlement flow as customer usage.
- Free-tier usage drains `operator_free_tier_expense`-funded grants and reports consumed value by product, bucket, SKU, org, and period.
- No-consent leaked usage creates `invoice_adjustments` with `cost_center`, `expense_category`, `recoverable = false`, and `affects_customer_balance = false`; optional TigerBeetle showback transfers mirror the adjustment total.
- Promo campaigns create promo grants from `operator_promo_expense`, not ad hoc balance edits.
- Expired credits sweep into `operator_expired_credits`, giving operators unused-allowance reporting without subtracting from a future customer balance.

The showback read model is built from PostgreSQL invoice adjustments, settled window legs, billing events, TigerBeetle reconciliation facts, and ClickHouse projections. Operators can answer “how much did free tier cost this month?” by grouping `invoice_adjustments`, free-tier grant settlement legs, and `operator_free_tier_expense` movements by product/bucket/SKU. Customers should not see internal showback clearing accounts.

### Reconciliation and trust mode

Billing reconciliation compares PostgreSQL ledger metadata against TigerBeetle state:

1. Operator account integrity: each registered operator account exists with expected ledger, code, flags, and `user_data`.
2. Grant account parity: every posted grant has a TigerBeetle account; every customer-grant account known to PostgreSQL maps to exactly one grant row.
3. Grant balance parity: posted grant deposits, pending reservation legs, posted settlement legs, expiry sweeps, refunds, and corrections reconcile to the TigerBeetle account balances.
4. Window leg parity: every `billing_window_ledger_legs` row in `pending`, `posted`, or `voided` has the expected TigerBeetle reservation/post/void transfer IDs.
5. Command drain health: no ledger command remains due, leased, or retryable beyond policy without alerting or DLQ transition.
6. Receivable parity: customer receivable balances reconcile to unfinalized/finalized receivable invoice lines, payments, adjustments, and corrections.
7. Accounting identity: all included account movements on a ledger net to zero under the configured account balance convention.

Drift writes a durable PostgreSQL drift row and a `billing_events` fact that projects to ClickHouse. Severe drift, such as missing operator accounts or an accounting identity violation, trips a billing ledger write guard until an operator resolves or explicitly waives the condition.

The single-node deployment can run TigerBeetle as one replica for development and early dogfooding, but that is not production-grade financial HA. Production financial trust requires independent TigerBeetle replicas according to the platform's multi-node topology. The service should expose a `ledger_trust_mode` in startup logs and verification output so operators can distinguish `development_single_replica` from `production_replicated`.

## Core billing model

The billing model is SKU-driven for usage, contract-driven for recurring entitlements, cycle-driven for invoice periods, and invoice-driven for customer-facing payment artifacts.

A product is something billable. A SKU is a billable usage component. A SKU belongs to a credit bucket. Buckets are the entitlement lanes customers see and consume against. Examples:

- `Compute` bucket, SKU `AMD EPYC 4484PX @ 5.66GHz`, quantity unit `vCPU-ms`
- `Memory` bucket, SKU `Standard Memory`, quantity unit `GiB-ms`
- `Block Storage` bucket, SKU `Premium NVMe`, quantity unit `GiB-ms`

Metered usage prices are attached to the plan/SKU pair, not to ad hoc JSON on the plan row. Provider price IDs on a plan are optional Stripe invoice-item references; they are not the source of truth for SKU pricing or metering.

Recurring paid entitlements are modeled as:

```text
contracts
  -> contract_changes
  -> contract_phases
  -> contract_entitlement_lines
  -> entitlement_periods
  -> credit_grants
  -> billing_ledger_commands
  -> TigerBeetle customer_grant account + deposit transfer
  -> billing_events
  -> billing_event_delivery_queue
```

Invoice period accounting is modeled as:

```text
billing_cycles
  -> billing_windows bounded by cycle interval
  -> billing_window_ledger_legs
  -> billing_ledger_commands
  -> TigerBeetle pending/post/void transfers
  -> billing_invoices
  -> invoice_line_items
  -> invoice_adjustments
  -> billing_events
  -> billing_event_delivery_queue
```

Provider ingress is modeled as:

```text
provider webhook/API event
  -> billing_provider_events
  -> River billing.provider_event.apply
  -> provider-neutral payment, invoice, contract, phase, or adjustment mutation
  -> billing_events
  -> billing_event_delivery_queue
```

A `contract` is the commercial agreement with an org. A `contract_phase` is the time-bounded version of that agreement: Hobby for a cycle, Pro after an immediate upgrade, or a bespoke enterprise package for a signed term. A `contract_entitlement_line` is the recurring promise inside the phase: which source funds which scope, how much, and on what recurrence. An `entitlement_period` materializes one recurrence window. A `credit_grant` is the PostgreSQL metadata row for a spendable TigerBeetle-backed balance issued from that period. The grant is not spendable until its ledger deposit command is posted.

A `billing_cycle` is the bookkeeping interval for an `(org, product)` billing timeline. A cycle has no financial meaning by itself. It only names the half-open interval `[starts_at, ends_at)` that invoice generation uses to select settled windows, recurring charges, entitlement periods, adjustments, and contract phase overlaps. A cycle can contain multiple contract phases, and one contract phase can span multiple cycles.

There is no separate `plan_bindings` concept. Contract phases are the plan/policy intervals. The funder uses the contract phase active at reservation time to capture rate and entitlement context into `billing_windows` and `billing_window_ledger_legs`; invoice generation uses that captured context, not a live phase lookup, so retroactive plan edits cannot rewrite history.

The supported entitlement scopes form a tightest-to-widest funnel: `sku` -> `bucket` -> `product` -> `account`. Entitlements are non-overlapping within a layer:

- `sku`: one specific SKU within one product bucket
- `bucket`: one product bucket, fed by any of its SKUs
- `product`: any bucket for one product
- `account`: any product bucket in the org

The `bucket` layer is the SKU-lane layer. If premium NVMe and non-premium disk need separate allowance behavior, they must be separate buckets and the corresponding SKUs must map to the correct bucket. Product-level or account-level grants are the only supported way to fund multiple buckets.

Free tier is not a contract and not a plan. It is a universal scheduled entitlement policy that grants monthly `source = 'free_tier'` balances to every org regardless of which paid contract the org has. Upgrading from free usage to any paid contract must not remove the current month's free-tier grants; the reserve waterfall consumes matching free-tier grants before recurring contract grants or purchased credit grants.

Free tier is also not implicit postpaid consent. A free-tier org may keep a payment method on file for future paid activation or credit purchases without authorizing metered overage invoices. If a free-tier org exhausts its free-tier grants and any explicit purchased or promo balances, admission must stop. If a race, stale read, retry, delayed settlement, or worker bug permits usage beyond that point, the excess is absorbed by the operator through an automatic invoice adjustment during finalization; it is not converted into debt, a rollover deduction, or a future customer balance.

The default invariant is one active commercial contract per org/product unless an explicit future model introduces stacking groups. Multiple visible phases may exist during transitions, but only non-overlapping active/grace phase intervals for the same org, product, scope type, and scope target may fund reservations. Free-tier policies are outside that commercial contract constraint.

## PostgreSQL catalog and state

This section describes the billing schema. Recurring customer agreements are modeled as provider-neutral contracts; Stripe Subscriptions, `subscription_contracts`, `subscription` source values, and `/subscriptions` API names are not part of the implementation surface. PostgreSQL stores deterministic domain rows and ledger command state. TigerBeetle stores balance-changing account and transfer facts. Do not collapse those two identities into one primary key scheme.

### `tbid` domain

PostgreSQL columns that store TigerBeetle IDs should use a domain equivalent to:

```sql
CREATE DOMAIN tbid AS BYTEA CHECK (octet_length(VALUE) = 16);
```

Use `tbid` for TigerBeetle account IDs, transfer IDs, command IDs when represented as TigerBeetle IDs, and `tb_correlation_id` fields. Do not use it for `grant_id`, `window_id`, `contract_id`, `cycle_id`, `invoice_id`, or `event_id`.

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

### `skus`

Billable usage components.

Key fields:

- `sku_id`
- `product_id`
- `bucket_id`
- `display_name`
- `quantity_unit`
- `active`

A SKU answers two questions: what line item name should the customer see, and which bucket should usage drain from.

### `plans`

Reusable commercial package templates.

Key fields:

- `plan_id`
- `display_name`
- `tier`
- `billing_mode`
- `monthly_amount_cents`
- `annual_amount_cents`
- `currency`
- `stripe_price_id_monthly`
- `stripe_price_id_annual`
- `active`
- `is_default`

A plan is a reusable packaging template, not an active customer agreement. Self-serve flows use plan price IDs only to map invoice line items to optional Stripe Prices. Enterprise contracts may reference a plan for display and rate-card inheritance, but bespoke enterprise terms must be represented by contract phases and entitlement lines.

A plan no longer carries included-credit JSON. Plan entitlements are linked through `plan_entitlements` and copied into `contract_entitlement_lines` when a catalog-plan phase is created.

### `plan_sku_rates`

One row per plan/SKU price.

Key fields:

- `plan_id`
- `sku_id`
- `unit_rate`
- `currency`
- `active_from`
- `active_until`

This table is the product/SKU rate card used for reservation cost calculations and invoice line items. Rate context is copied into each `billing_windows.rate_context` so invoices do not depend on mutable catalog rows.

### `entitlement_policies`

Reusable free-tier, contract, promo, refund, and purchased-credit policy definitions.

Key fields:

- `policy_id`
- `product_id`
- `source` (`free_tier`, `contract`, `purchase`, `promo`, `refund`, `internal`)
- `scope_type`
- `scope_product_id`
- `scope_bucket_id`
- `scope_sku_id`
- `amount_units`
- `cadence` (`monthly`, `annual`, `one_time`)
- `anchor_kind` (`billing_cycle`, `calendar_month`, `anniversary`, `calendar_month_day`)
- `proration_mode`
- `policy_version`
- `active_from`
- `active_until`

Free-tier policies are global product policies. Contract policies are copied into phase-local entitlement lines when a paid phase is created, so later catalog edits do not rewrite historical phase promises.

### `plan_entitlements`

Join table from a plan to entitlement policies.

Key fields:

- `plan_id`
- `policy_id`

This avoids embedding entitlements inside plan JSON and makes policy versioning explicit. Creating a Hobby or Pro phase copies the active linked policies into `contract_entitlement_lines`, preserving the exact policy version used by that customer phase.

### `contracts`

One row per commercial agreement with an org.

Key fields:

- `contract_id`
- `org_id`
- `product_id`
- `display_name`
- `contract_kind` (`self_serve`, `enterprise`, `internal`)
- `state` (`draft`, `pending_activation`, `active`, `past_due`, `suspended`, `cancel_scheduled`, `ended`, `voided`)
- `payment_state` (`not_required`, `pending`, `paid`, `failed`, `uncollectible`, `refunded`)
- `entitlement_state` (`scheduled`, `active`, `grace`, `closed`, `voided`)
- `currency`
- `overage_policy` (`block`, `bill_published_rate`, `block_after_balance`)
- `starts_at`
- `ends_at`
- `grace_until`
- `cancel_at`
- `closed_at`
- `state_version`

The contract row is provider-neutral. It must not carry a durable `plan_id`; plan identity is phase-scoped. This prevents historical Hobby grants from being relabeled as Pro when the customer upgrades the same commercial relationship.

The contract payment state machine is separate from the entitlement state machine. A late Stripe invoice, manual enterprise payment collection, or explicit grace window can leave a contract `payment_state = 'pending'` or `failed` while entitlement availability remains `grace` instead of immediately failing customer requests.

### `provider_bindings`

Optional external provider identity for a contract, payment method, invoice, payment intent, or other provider-backed object.

Key fields:

- `binding_id`
- `aggregate_type` (`contract`, `payment_method`, `invoice`, `payment_intent`, `customer`)
- `aggregate_id`
- `contract_id`
- `provider` (`stripe`, `manual`)
- `provider_object_type` (`customer`, `payment_method`, `invoice`, `payment_intent`, `manual_contract`, or provider-specific object type)
- `provider_object_id`
- `provider_customer_id`
- `sync_state` (`none`, `pending`, `synced`, `error`)
- `metadata`

A Stripe self-serve contract usually has a customer-level binding and invoice/payment-intent references on invoice/payment rows. It must not have a Stripe subscription binding in the target architecture. An enterprise contract may have no binding, a `manual_contract` binding, or a later CRM/ERP binding without changing entitlement issuance.

### `payment_methods`

Vaulted payment methods and customer payment consent.

Key fields:

- `payment_method_id`
- `org_id`
- `provider` (`stripe`)
- `provider_customer_id`
- `provider_payment_method_id`
- `setup_intent_id`
- `status` (`pending`, `active`, `detached`, `failed`)
- `is_default`
- `card_brand`
- `card_last4`
- `expires_month`
- `expires_year`
- `off_session_authorized_at`
- `created_at`
- `updated_at`

A vaulted card enables future payment collection only when paired with explicit customer consent for the specific charge model. It does not imply postpaid overage consent.

### `billing_provider_events`

Durable inbound event table for provider webhooks, provider API callbacks, and manually injected provider-test events.

Key fields:

- `provider_event_id`
- `provider`
- `event_type`
- `provider_object_type`
- `provider_object_id`
- `provider_customer_id`
- `provider_invoice_id`
- `provider_payment_intent_id`
- `contract_id`
- `invoice_id`
- `binding_id`
- `org_id`
- `received_at`
- `provider_created_at`
- `api_version`
- `livemode`
- `payload`
- `state` (`received`, `queued`, `applying`, `applied`, `ignored`, `failed`, `dead_letter`)
- `attempts`
- `next_attempt_at`
- `applied_at`
- `last_error`
- `idempotency_key`

There must be a unique key on `(provider, provider_event_id)`. Webhook ingress writes this row before applying the event and transactionally enqueues `billing.provider_event.apply`. Duplicate provider deliveries converge on the same row and same River job identity. Out-of-order events are not applied by arrival order; the worker translates each event into a provider-neutral mutation and lets the payment/invoice/contract/phase state machines decide whether it is still relevant.

This table is the primary fault-injection seam for Stripe. Tests exercise the provider-event boundary with delayed, duplicated, missing, failed, terminal, malformed, and out-of-order events, then verify PostgreSQL state plus `forge_metal.billing_events` projection.

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
- `state` (`requested`, `provider_pending`, `awaiting_payment`, `scheduled`, `applying`, `applied`, `failed`, `canceled`)
- `provider`
- `provider_request_id`
- `provider_invoice_id`
- `idempotency_key`
- `failure_reason`
- `requested_by`
- `requested_at`
- `next_attempt_at`
- `attempts`
- `state_version`
- `proration_basis_cycle_id`
- `price_delta_units`
- `entitlement_delta_mode` (`none`, `positive_delta`)
- `proration_numerator`
- `proration_denominator`

This table is the seam for fault injection and for unifying self-serve and enterprise behavior. Stripe-backed self-serve changes move through payment states when an activation, upgrade, renewal, or one-off charge requires successful collection. Enterprise amendments can often move from `requested` to `scheduled` or `applied` without provider state.

A change row is both a state-machine request and a schedule source. If `timing = 'period_end'`, the due time is the active billing cycle's `ends_at` or the explicitly recorded phase `effective_end`. If `timing = 'specific_time'`, the due time is `requested_effective_at`. River executes the change when due, but the worker re-checks cycle state, phase state, provider state, and payment/grace state before applying it.

For immediate paid upgrades, the change row stores the proration basis instead of recomputing it from mutable plan state later. `price_delta_units` is the positive prorated recurring-charge delta in Forge Metal ledger units before tax. `entitlement_delta_mode = 'positive_delta'` means the first target-phase entitlement period issues only `max(target_line_amount - current_line_amount, 0) * proration_fraction`, while already-issued current-cycle grants from the prior paid phase remain spendable until their own `expires_at`. This prevents a customer from receiving more entitlement by walking through intermediate plans at the same effective timestamp.

Do not model deferred downgrades or cancellations as nullable hint fields like `next_cycle_plan_id`. A scheduled commercial change needs idempotency, audit history, actor identity, cancellation/reversal state, and failure handling.

At most one live scheduled period-end downgrade/cancellation may exist per contract. Enforce this in PostgreSQL with a partial unique index over `contract_id` for `state = 'scheduled'`, `timing = 'period_end'`, and `change_type IN ('downgrade', 'cancel')`. Application code should cancel or replace the existing local change deliberately; it must not accidentally stack multiple future plan transitions or escape to Stripe to resolve ambiguity.

### `contract_phases`

Time-bounded commercial policy intervals for a contract.

Key fields:

- `phase_id`
- `contract_id`
- `org_id`
- `product_id`
- `plan_id`
- `provider_price_id`
- `phase_kind` (`catalog_plan`, `bespoke`, `internal`)
- `state` (`scheduled`, `pending_payment`, `active`, `grace`, `superseded`, `closed`, `voided`)
- `payment_state`
- `entitlement_state`
- `effective_start`
- `effective_end`
- `activated_at`
- `closed_at`
- `superseded_by_phase_id`
- `created_reason`
- `state_version`

A phase is the unit that upgrades and downgrades operate on. Hobby -> Pro does not mutate one plan field in place; it supersedes the Hobby phase and creates a Pro phase. Superseding a phase stops it from materializing new paid recurrence periods, but it does not require closing already-issued current-cycle grants; those grants are paid allowance and may remain spendable carryforward until their own `expires_at`. Pro -> Hobby at period end keeps the Pro phase active until the current billing cycle ends and records a scheduled `contract_changes` row for the successor phase. `phase_kind = 'catalog_plan'` requires a plan. `phase_kind = 'bespoke'` can omit `plan_id` and carry directly authored entitlement lines.

Phase activation and closure are scheduled transitions. River enqueues boundary jobs when phases are created or amended. Reconciliation scans for due phases and repairs missing jobs. Scheduled future phases can overlap in planning time only if their effective intervals and line scopes do not overlap when activated.

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
- `recurrence_anchor_kind` (`billing_cycle`, `anniversary`, `calendar_month_day`)
- `recurrence_anchor_day`
- `recurrence_timezone`
- `charge_timing` (`cycle_start`, `cycle_end`, `none`)
- `proration_mode`
- `policy_version`
- `active_from`
- `active_until`
- `last_materialized_period_start`
- `next_materialize_at`

For self-serve catalog phases, lines normally use `recurrence_anchor_kind = 'billing_cycle'`, meaning the Forge Metal billing cycle defines the entitlement window. Enterprise phases normally use `calendar_month_day` and a timezone so the contract can renew on a fixed calendar day regardless of signup anniversary. `anniversary` anchors are available for non-cycle contract terms that renew from service start.

Lines are copied from plan policies for catalog-plan phases and authored directly for bespoke phases. This keeps upgrades/downgrades and enterprise amendments on the same state machine.

Catalog-plan lines carry the full target-plan entitlement amount. They are not rewritten to a prorated amount when an upgrade happens mid-cycle. The first target-phase entitlement period can be computed as an upgrade delta by the associated `contract_changes` row; subsequent periods materialize the full line amount.

Recurring entitlement scheduling is not a timer hidden in River. The recurrence config and cursor live in PostgreSQL. River jobs are generated from that state and can be reconstructed by reconciliation.

### `billing_cycles`

Bookkeeping intervals for invoice/finalization periods.

Key fields:

- `cycle_id`
- `org_id`
- `product_id`
- `currency`
- `predecessor_cycle_id`
- `anchor_at`
- `cycle_seq`
- `cadence_kind` (`anniversary_monthly`, `calendar_monthly`, `annual`, `manual`)
- `starts_at`
- `ends_at`
- `status` (`open`, `closing`, `closed_for_usage`, `invoice_finalizing`, `invoiced`, `blocked`, `voided`)
- `finalization_due_at`
- `invoice_id`
- `blocked_reason`
- `closed_for_usage_at`
- `finalized_at`
- `created_at`
- `updated_at`

Required invariants:

- Unique `(org_id, product_id, anchor_at, cycle_seq)`.
- At most one `open` or `closing` cycle per `(org_id, product_id)`.
- No overlapping non-voided cycle intervals per `(org_id, product_id)`, preferably enforced with a PostgreSQL exclusion constraint over `tstzrange(starts_at, ends_at, '[)')`.
- A cycle has no financial meaning on its own. It is a named interval used by invoice generation and reporting.
- Free-tier orgs have cycles. They are not absent from cycle accounting just because no paid contract exists.

The only normal path that creates a successor cycle is `openNextCycle(predecessor)`. Scheduled rollover and immediate boundary changes both call the same transition. Rollover closes the predecessor for usage and opens the successor before any Stripe call, invoice email, or payment collection.

### `entitlement_periods`

Durable period-level projection from free-tier policies or contract entitlement lines. Period rows are the bridge between scheduled entitlement truth and grant issuance.

Key fields:

- `period_id`
- `org_id`
- `product_id`
- `cycle_id`
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
- `change_id`
- `calculation_kind` (`recurrence`, `activation`, `upgrade_delta`, `manual_adjustment`)
- `source_reference_id`
- `created_reason`

The `source_reference_id` is deterministic and source-specific. Free-tier references are policy/period scoped. Contract references include `contract_id`, `phase_id`, `line_id`, policy version, and period boundaries, so two phases under the same contract cannot collapse into one grant.

For `source = 'contract'`, `contract_id`, `phase_id`, and `line_id` are required. For `source = 'free_tier'`, those fields must be empty. This keeps universal free-tier recurrence independent from paid contract recurrence.

### `credit_grants`

PostgreSQL metadata for one spendable TigerBeetle-backed balance with explicit scope.

Scope classes, tightest to widest:

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
- `tb_account_id`
- `tb_correlation_id`
- `deposit_ledger_command_id`
- `deposit_transfer_id`
- `ledger_posting_state` (`pending`, `posting`, `posted`, `retryable_failed`, `dead_letter`, `failed`)
- `ledger_posted_at`
- `ledger_last_error`

Source-funded grants are deterministic over `org_id`, `source`, scope, and `source_reference_id` so retries converge on the same PostgreSQL row without making Stripe the only reference namespace. The TigerBeetle account ID and deposit transfer ID are not deterministic hashes; they are time-based TigerBeetle IDs allocated once, persisted on or through the grant's ledger command, and reused on retry.

Free-tier and contract grants carry `entitlement_period_id`, `policy_version`, `period_start`, `period_end`, `starts_at`, and `expires_at`. A paid phase transition must distinguish unearned future grants from already-earned current-cycle carryforward. Terminal or superseding phase events close future-period, voided, fraudulent, or otherwise unearned grant rows for the affected phase, but a normal immediate upgrade leaves already-issued current-cycle grants open until their own expiry and issues only a target-phase delta grant for the rest of the cycle.

Only `ledger_posting_state = 'posted'` grants may fund reservations or appear as available balance. Pending, retryable-failed, or dead-lettered grants may remain visible to operators, but customer-facing entitlement views must label or exclude them rather than treating `amount` as available. If a deposit command fails after the PostgreSQL grant row exists, reconciliation either completes the same command with the persisted TigerBeetle IDs or moves the grant to an operator-visible failure state; it must not mint a second grant for the same source reference.

Grant consumption is strict and step-function-shaped: scope tightness first, source priority second, recurring-contract tier priority third. Settlement trims the already-reserved funding legs in that order; it must never scale every source proportionally. The implementation lives in `internal/billing/grants.go` and is intentionally different from the entitlements view's display order, which is account-to-SKU because the customer is asking "what coverage do I have" rather than "what drains first":

```go
GrantScopeFundingOrder  = []GrantScopeType{ GrantScopeSKU, GrantScopeBucket, GrantScopeProduct, GrantScopeAccount }
GrantSourceFundingOrder = []GrantSourceType{ SourceFreeTier, SourceContract, SourcePromo, SourceRefund, SourceInternal, SourcePurchase, SourceReceivable }
GrantPlanFundingOrder   = []PlanTier{ Default, Hobby, Pro, Enterprise }
```

The domain source for any recurring paid agreement is `contract`. Stripe Hobby, Stripe Pro, and enterprise MSA credits share the recurring-contract source class, but concrete tiers still drain in ascending tier order within a scope so an upgrade cannot cause lower-tier already-earned entitlements to sit behind newer higher-tier entitlements. Account-level purchased balance drains after scoped free-tier, contract, promo, and refund grants. The funder never computes availability from `credit_grants.amount` alone; it asks TigerBeetle for balances on the eligible grant accounts and then records the chosen waterfall in `billing_window_ledger_legs`.

### `billing_windows`

The request-path financial state machine.

Key fields:

- `window_id`
- `cycle_id`
- `org_id`
- `product_id`
- `actor_id`
- `source_type`
- `source_ref`
- `window_seq`
- `state` (`reserving`, `reserved`, `active`, `settling`, `settled`, `voiding`, `voided`, `denied`, `failed`)
- `reservation_shape`
- `reserved_quantity`
- `actual_quantity`
- `billable_quantity`
- `writeoff_quantity`
- `reserved_charge_units`
- `billed_charge_units`
- `writeoff_charge_units`
- `writeoff_reason_code`
- `pricing_contract_id`
- `pricing_phase_id`
- `pricing_plan_id`
- `pricing_phase`
- `rate_context`
- `usage_summary`
- `funding_legs`
- `tb_correlation_id`
- `reservation_ledger_command_id`
- `settlement_ledger_command_id`
- `void_ledger_command_id`
- `window_start`
- `expires_at`
- `settled_at`
- `metering_projected_at`
- `last_projection_error`

`billing_windows` are request-path financial locks, not queued jobs. A `reserved` window means TigerBeetle accepted the pending reservation transfers. A `reserving` window is not a valid reservation and must not let a caller start work. A `settled` or `voided` window means TigerBeetle accepted the corresponding post/void command or reconciliation proved the pending transfers resolved. Settlement and projection can be retried asynchronously, but terminal window state must not lead TigerBeetle acknowledgement.

Sandbox time windows use AWS-Lambda-style millisecond quantities. `reserved_quantity`, `actual_quantity`, `billable_quantity`, and `writeoff_quantity` are milliseconds for `reservation_shape = 'time'`; the SKU quantity unit makes the resource dimension explicit (`vCPU-ms`, `GiB-ms`). VM launch and environment setup time are not billable. The billed duration comes from the billable guest `run` phase duration when available and falls back to host-side billable phase start/end evidence only when the guest duration is absent.

`funding_legs` may be retained as a denormalized snapshot for API and ClickHouse projection, but it is not the authoritative financial leg table. `billing_window_ledger_legs` owns per-leg transfer IDs, source attribution, component SKU attribution, and leg state.

`writeoff_quantity` and `writeoff_charge_units` are settlement evidence, not a customer credit. They capture usage that was admitted but cannot be billed because it exceeded the reserved quantity or the org's overage-consent policy. Invoice finalization turns that evidence into deterministic `invoice_adjustments` rows when the window would otherwise create unauthorized receivable units. Optional TigerBeetle showback transfers mirror those adjustments for operator reporting without creating customer debt.

### `billing_window_ledger_legs`

Normalized reservation, settlement, void, and source-attribution legs for a billing window.

Key fields:

- `window_id`
- `leg_seq`
- `org_id`
- `product_id`
- `cycle_id`
- `source` (`free_tier`, `contract`, `purchase`, `promo`, `refund`, `internal`, `receivable`)
- `grant_id`
- `customer_receivable_account_id`
- `grant_account_id`
- `reservation_transfer_id`
- `settlement_transfer_id`
- `void_transfer_id`
- `component_sku_id`
- `component_bucket_id`
- `scope_type`
- `scope_product_id`
- `scope_bucket_id`
- `scope_sku_id`
- `pricing_plan_id`
- `pricing_phase_id`
- `amount_reserved`
- `amount_posted`
- `amount_voided`
- `state` (`pending_tb`, `pending`, `posting`, `posted`, `voiding`, `voided`, `retryable_failed`, `expired`, `failed`)
- `ledger_command_id`
- `created_at`
- `updated_at`

The primary key is `(window_id, leg_seq)`. Leg order is the exact funding waterfall order used for settlement trimming and invoice attribution. The transfer IDs are TigerBeetle IDs, not domain IDs. `source = 'receivable'` uses `customer_receivable_account_id` and leaves `grant_id` empty; grant-backed sources require `grant_id` and `grant_account_id`.

This table is the indexed source for explaining how a window drained balances. TigerBeetle is still the balance authority, but PostgreSQL stores the domain reason for each transfer so invoices, support tools, and ClickHouse projections do not need to depend on preview TigerBeetle query APIs.

### `billing_ledger_account_registry`

Registry for operator accounts and other non-grant TigerBeetle accounts that must exist before ledger commands can dispatch.

Key fields:

- `account_key`
- `account_code`
- `account_role`
- `tb_account_id`
- `ledger`
- `flags`
- `user_data_128`
- `user_data_64`
- `user_data_32`
- `description`
- `metadata`
- `created_at`
- `updated_at`

Operator accounts are bootstrapped idempotently and verified by reconciliation. A mismatch in account code, ledger, flags, or `user_data` is severe drift; billing must refuse new ledger writes until the operator resolves it. Customer grant accounts do not live only in this registry; they are referenced from `credit_grants`.

### `billing_ledger_commands`

Durable PostgreSQL command state for TigerBeetle side effects.

Key fields:

- `ledger_command_id`
- `operation` (`grant_deposit`, `reserve_window`, `settle_window`, `void_window`, `invoice_receivable_accrue`, `expire_grant`, `refund_balance`, `receivable_clear`, `adjustment_showback`, `ledger_correction`)
- `aggregate_type`
- `aggregate_id`
- `org_id`
- `product_id`
- `idempotency_key`
- `state` (`pending`, `dispatching`, `posted`, `retryable_failed`, `dead_letter`, `reconciled`)
- `generation`
- `payload`
- `attempts`
- `next_attempt_at`
- `last_attempt_at`
- `lease_expires_at`
- `leased_by`
- `last_attempt_id`
- `posted_at`
- `last_error`
- `dead_lettered_at`
- `dead_letter_reason`
- `operator_note`
- `created_at`
- `updated_at`

The command payload stores the exact TigerBeetle account and transfer specs needed for retry. It is immutable within a generation. If a command must change amount, account, transfer ID, linked-chain shape, or operation semantics, create a new generation with an explicit reason and preserve the failed generation for audit.

Ledger command rows should follow the same lease/DLQ discipline as `billing_event_delivery_queue`, but they are not billing facts and should not be deleted on success. Keeping successful ledger commands gives reconciliation and support a durable bridge from domain row to TigerBeetle transfer IDs.

### `billing_ledger_drift_events`

Operator-facing reconciliation findings.

Key fields:

- `drift_id`
- `severity` (`info`, `warning`, `critical`)
- `drift_type`
- `aggregate_type`
- `aggregate_id`
- `org_id`
- `product_id`
- `ledger_command_id`
- `tb_account_id`
- `tb_transfer_id`
- `expected`
- `observed`
- `state` (`open`, `acknowledged`, `resolved`, `waived`)
- `detected_at`
- `resolved_at`
- `operator_note`

Critical drift emits a `billing_events` fact and trips the ledger write guard. Examples include missing operator accounts, account-flag mismatch, grant account missing for a posted grant, a posted PostgreSQL leg without a TigerBeetle transfer, or a ledger accounting identity violation.

### `billing_invoices`

Immutable customer invoice artifacts.

Key fields:

- `invoice_id`
- `invoice_number`
- `org_id`
- `product_id`
- `cycle_id`
- `status` (`draft`, `finalizing`, `issued`, `paid`, `payment_failed`, `blocked`, `voided`)
- `payment_status` (`n_a`, `pending`, `paid`, `failed`, `uncollectible`)
- `period_start`
- `period_end`
- `issued_at`
- `currency`
- `subtotal_units`
- `adjustment_units`
- `tax_units`
- `total_due_units`
- `recipient_email`
- `recipient_name`
- `invoice_snapshot_json`
- `rendered_html`
- `content_hash`
- `stripe_invoice_id`
- `stripe_hosted_invoice_url`
- `stripe_invoice_pdf_url`
- `stripe_payment_intent_id`
- `resend_message_id`
- `voided_by_invoice_id`
- `created_at`
- `updated_at`

Invoice generation builds gross usage lines, recurring charge lines, tax lines from configured tax policy, funding splits, and adjustment candidates from PostgreSQL domain rows, normalized ledger legs, and TigerBeetle-backed command state. Invoice finalization is the state-machine boundary that proves every customer-chargeable receivable unit is backed by explicit consent. If a tax provider can change the customer amount due, tax calculation is part of finalization and must complete before the Forge Metal invoice is issued.

A Forge Metal invoice is immutable after issue. Corrections create a new adjustment invoice or credit-note invoice linked through `voided_by_invoice_id` or an explicit credit-note relation. The original remains queryable for audit.

`invoice_snapshot_json` is the canonical rendering input. `rendered_html` is the exact body emailed to the customer or shown in the Forge Metal console. `content_hash` lets operators prove what was issued without recomputing from mutable catalog or policy tables.

Finalization must:

1. Re-read the org/product billing posture from PostgreSQL.
2. Recompute candidate receivable units from settled windows, captured rate context, recurring charges, grant funding, and explicit purchases.
3. Verify whether the org authorized postpaid overage for the product and period.
4. Apply deterministic automatic credit adjustments for no-consent receivable units when the adjustment total is within the cap.
5. Block finalization when no-consent automatic adjustments would exceed the cap.
6. Resolve tax and convert ledger units to Stripe invoice cents with an explicit rounding/residual policy when Stripe collection is needed.
7. Allocate an invoice number only when the invoice artifact is ready to issue.
8. Insert the immutable invoice artifact.
9. Emit billing event facts for created adjustments, issued invoices, finalized invoices, or blocked finalizations.
10. Enqueue Stripe collection and invoice email jobs when applicable.

If Stripe Tax is enabled, the Stripe draft/tax verification step happens while the Forge Metal invoice is still finalizing. The local invoice must not move to `issued` until tax units and the provider-facing cent total have been reconciled into `invoice_snapshot_json`.

### `invoice_line_items`

Immutable line items belonging to a `billing_invoices` artifact.

Key fields:

- `line_item_id`
- `invoice_id`
- `line_type` (`usage`, `recurring_charge`, `adjustment`, `tax`, `rounding`)
- `product_id`
- `bucket_id`
- `sku_id`
- `description`
- `quantity`
- `quantity_unit`
- `unit_rate_units`
- `charge_units`
- `free_tier_units`
- `contract_units`
- `purchase_units`
- `promo_units`
- `refund_units`
- `receivable_units`
- `adjustment_units`
- `source_window_id`
- `source_ledger_leg_ids`
- `source_phase_id`
- `source_entitlement_period_id`
- `metadata`

Line items are denormalized on purpose. They are the customer-facing artifact and must not need live catalog joins to be understood later.

### `invoice_adjustments`

Invoice-scoped credits or debits that affect amount due without creating spendable customer balance.

Key fields:

- `adjustment_id`
- `invoice_id`
- `invoice_finalization_id`
- `org_id`
- `product_id`
- `window_id`
- `bucket_id`
- `sku_id`
- `adjustment_type` (`credit`, `debit`)
- `adjustment_source` (`system_policy`, `manual_admin`, `sla`, `campaign`)
- `reason_code` (`free_tier_overage_absorbed`, `paid_hard_cap_overage_absorbed`, `operator_goodwill`, `policy_migration`, `rounding_residual`)
- `amount_units`
- `published_charge_units`
- `estimated_cost_units`
- `customer_visible`
- `recoverable`
- `affects_customer_balance`
- `cost_center`
- `expense_category`
- `policy_version`
- `created_at`

Automatic no-consent adjustments use `adjustment_source = 'system_policy'`, `adjustment_type = 'credit'`, `customer_visible = false`, `recoverable = false`, and `affects_customer_balance = false`. They are deterministic over `(invoice_finalization_id, org_id, product_id, window_id, sku_id, reason_code, policy_version)` so finalization retries cannot double-credit the invoice.

The default automatic no-consent adjustment cap is USD $0.99 per org per invoice finalization run. Because billing cycles are scoped to `(org, product)`, the normal case is one product; if a future statement-level finalization batches multiple products, the cap is shared across the batch. In the current USD ledger scale, that is `99 * 100_000` ledger units. This cap is a circuit breaker, not overage consent. If the cap would be exceeded, finalization enters a blocked state, emits `invoice_finalization_blocked`, blocks further no-consent execution for the affected org/product, and waits for operator resolution. Operator resolution may create an explicit manual adjustment or credit-note invoice, but must not create a customer receivable unless the customer grants overage consent.

Invoice adjustments do not create spendable customer balance. When an adjustment needs operator showback in TigerBeetle, finalization creates a separate ledger command such as `no_consent_adjustment_showback`, correlated to the invoice finalization and adjustment row. That transfer records internal expense/clearing movement only; the invoice adjustment remains the customer-facing and policy-enforcing artifact.

### `invoice_number_allocators`

Gapless customer-facing invoice number allocation.

Key fields:

- `issuer_id`
- `year`
- `prefix`
- `next_number`
- `updated_at`

The allocator row is locked with `SELECT ... FOR UPDATE` and incremented in the same transaction that inserts the issued invoice artifact. PostgreSQL sequences are not acceptable for gapless invoice numbers because sequence values can be lost on rollback. If any external side effect fails after number allocation, the invoice artifact remains present and transitions to `voided`, `blocked`, or `payment_failed`; it is not deleted.

The target number format is `FM-{year}-{seq}` unless the operator configures a different issuer prefix. Scope allocation by `(issuer_id, year)` to avoid a global hot row and avoid leaking total invoice volume across years or issuers.

### `billing_events`

Immutable PostgreSQL fact stream for material billing facts that must be projected to ClickHouse.

Key fields:

- `event_id`
- `event_type`
- `event_version`
- `aggregate_type`
- `aggregate_id`
- `org_id`
- `product_id`
- `occurred_at`
- `payload`
- `payload_hash`
- `correlation_id`
- `causation_event_id`
- `created_at`

The billing event row is append-only. Re-inserting the same `event_id` with the same canonical `payload_hash` is an idempotent no-op. Re-inserting the same `event_id` with a different hash is a data-integrity error because a supposedly immutable fact changed meaning.

Grant materialization writes the PostgreSQL grant row, ledger IDs, and deposit command before dispatch. The spendable `grant_issued`/`grant_ledger_posted` facts are emitted only in the transaction that marks the grant ledger posting `posted` after TigerBeetle acknowledgement or reconciliation. Contract creation, provider event ingestion, payment-method changes, contract changes, phase transitions, entitlement materialization, cycle rollover, billing-window reservation/settlement decisions, invoice finalization, invoice adjustments, Stripe collection updates, ledger command outcomes, and invoice email delivery also write billing events in the same transaction as their authoritative PostgreSQL state change.

Successful delivery does not mutate `billing_events`; delivery status is operational state and belongs outside the fact stream.

### `billing_event_delivery_queue`

Active-only delivery backlog and DLQ for billing facts that need sink-specific projection.

Key fields:

- `event_id`
- `sink`
- `generation`
- `state` (`pending`, `in_progress`, `retryable_failed`, `dead_letter`)
- `attempts`
- `next_attempt_at`
- `last_attempt_at`
- `lease_expires_at`
- `leased_by`
- `last_attempt_id`
- `delivery_error`
- `dead_lettered_at`
- `dead_letter_reason`
- `operator_note`
- `created_at`
- `updated_at`

A queue row is inserted in the same transaction as a new `billing_events` fact for every required sink. Delivery workers lease due rows, project to the sink, and delete the queue row on success. Repeated failure transitions the row to `dead_letter`, where it remains until an operator fixes the underlying cause and requeues it with an incremented `generation`.

River runs `billing.event_delivery.project` for one delivery row and `billing.event_delivery.project_pending` as a bounded repair scanner. ClickHouse delivery is at-least-once. If projection succeeds but the queue delete fails, the retry may replay the same fact; the ClickHouse projection must therefore be idempotent by `event_id`.

ClickHouse is proof/read-model infrastructure; PostgreSQL remains authoritative for billing domain state and TigerBeetle remains authoritative for balances and ledger movements.

Expected event types include:

- `payment_method_vaulted`
- `contract_created`
- `contract_change_requested`
- `contract_change_canceled`
- `contract_change_applied`
- `contract_resume_applied`
- `contract_phase_started`
- `contract_phase_closed`
- `provider_event_received`
- `provider_event_applied`
- `billing_cycle_opened`
- `billing_cycle_closed_for_usage`
- `grant_issued`
- `grant_ledger_posted`
- `grant_expired`
- `ledger_command_posted`
- `ledger_command_failed`
- `ledger_drift_detected`
- `billing_window_reserved`
- `billing_window_settled`
- `billing_window_voided`
- `receivable_accrued`
- `receivable_cleared`
- `no_consent_adjustment_showback_posted`
- `contract_catalog_reconciled`
- `billing_window_projected`
- `invoice_adjustment_created`
- `invoice_issued`
- `invoice_finalized`
- `invoice_finalization_blocked`
- `stripe_invoice_collection_started`
- `stripe_invoice_paid`
- `stripe_invoice_payment_failed`
- `invoice_email_sent`

## State machines

There is not a separate state machine for enterprise contracts. The same state machines apply to every recurring paid agreement; only phase kind, recurrence anchor, collection method, and provider binding differ.

Each state transition has an execution source:

- API transition: a user or internal caller requests a contract, change, purchase, cancellation, or admin action.
- Provider event transition: Stripe or another provider reports setup-intent, payment, invoice, refund, dispute, or deletion state.
- Scheduled transition: a phase boundary, cycle boundary, recurrence boundary, grace deadline, or invoice finalization time becomes due.
- Reconciliation transition: a repair worker reconstructs missing deterministic rows or missing River jobs.
- Request-path transition: reserve performs bounded entitlement self-healing from already-authorized PostgreSQL state.
- Finalization transition: invoice finalization verifies consent, applies deterministic invoice adjustments, and blocks customer charging when policy invariants are not met.

### Contract lifecycle

```text
draft -> pending_activation -> active
active -> past_due
past_due -> active
active -> suspended
suspended -> active
active -> cancel_scheduled
cancel_scheduled -> active
cancel_scheduled -> ended
active -> ended
any non-terminal -> voided
```

`pending_activation` means the agreement exists but must not yet issue active paid grants. For self-serve paid contracts, this usually means the first invoice has not been paid and no explicit grace decision exists. For enterprise, this may mean the agreement is signed but the service date has not arrived.

`past_due` and `suspended` are separate because late payment may preserve entitlements during grace, while suspension blocks or narrows entitlement availability according to policy.

### Change lifecycle

```text
requested -> provider_pending -> awaiting_payment -> applied
requested -> scheduled -> applied
requested -> applying -> applied
requested -> failed
provider_pending -> failed
awaiting_payment -> failed
scheduled -> canceled
any non-terminal -> canceled
```

Self-serve paid activations and immediate upgrades pass through `provider_pending` and `awaiting_payment` when a Stripe invoice is required before entitlement activation. Enterprise amendments can often go from `requested` to `scheduled`, `applying`, or `applied` without provider state.

Provider API success does not activate target paid entitlements by itself. For paid self-serve changes, provider API success moves the change toward `awaiting_payment` unless the business policy explicitly records a `grace` transition. `invoice.paid` or an accepted grace transition activates the new paid phase and materializes grants.

Canceling a scheduled period-end downgrade or cancellation is a local contract-change transition. If the user asks to start or change into the plan that is already the currently active paid phase, billing cancels matching scheduled period-end `contract_changes`, restores the contract from `cancel_scheduled` to `active` when applicable, clears cancellation boundary timestamps, and emits `contract_change_canceled` plus `contract_resume_applied`. It must not create a Stripe Checkout session, Stripe invoice, provider request, or provider event. A database uniqueness guard should reject more than one live scheduled period-end downgrade/cancellation for a contract, so the worst case is a failed local transaction rather than duplicate provider work.

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

### Billing-cycle lifecycle

```text
open -> closing -> closed_for_usage
closed_for_usage -> invoice_finalizing -> invoiced
closed_for_usage -> invoice_finalizing -> blocked
blocked -> invoice_finalizing -> invoiced
any non-terminal -> voided
```

`open` means new billing windows may attach to the cycle. `closed_for_usage` means no new windows can attach; finalization may still be pending. `blocked` means invoice finalization hit a policy invariant and must not charge the customer. A blocked finalization must not prevent `openNextCycle` from having created the successor cycle.

### Entitlement-period lifecycle

```text
scheduled -> active
scheduled -> grace
grace -> active
active -> closed
grace -> closed
any non-terminal -> voided
```

`scheduled` periods exist as durable plans but must not fund reservations. Only `active` and `grace` periods may issue spendable grants. Closing or voiding a period closes the associated local grant rows for future reservations without mutating historical TigerBeetle transfers. If a period issues a grant, the grant remains `ledger_posting_state = 'pending'` until the TigerBeetle deposit command posts.

### Credit-grant ledger lifecycle

```text
pending -> posting -> posted
pending -> posting -> retryable_failed -> posting
retryable_failed -> dead_letter
posted -> expiring -> expired
posted -> closing -> closed
any non-posted -> failed
```

`pending` means the PostgreSQL grant row exists but must not fund reservations. `posting` means the deposit command is being dispatched. `posted` means the TigerBeetle grant account and deposit transfer exist and the grant can fund reservations if its scope and time window match. `expired` means remaining balance was swept to `operator_expired_credits`. `closed` means PostgreSQL policy has closed the grant for future funding; it does not mutate historical TigerBeetle settlement transfers.

### Billing-window ledger lifecycle

```text
reserving -> reserved
reserving -> denied
reserving -> failed
reserved -> active
reserved -> settling -> settled
active -> settling -> settled
reserved -> voiding -> voided
active -> voiding -> voided
settling --ledger command retryable_failed--> settling
voiding --ledger command retryable_failed--> voiding
```

`reserved` is reached only after TigerBeetle accepts the pending reservation transfers. `settled` is reached only after TigerBeetle accepts the post/void transfers for every reservation leg. `voided` is reached only after TigerBeetle accepts void transfers or reconciliation proves the pending transfers have expired or were already voided. Ledger command retry failures keep the window in `settling` or `voiding`; they must not be returned to callers as usable reservations or terminal settlements.

### Invoice lifecycle

```text
draft -> finalizing -> issued
issued -> paid
issued -> payment_failed
payment_failed -> paid
issued -> voided
finalizing -> blocked
blocked -> finalizing -> issued
```

A `blocked` invoice must not be charged because a policy invariant failed, usually because no-consent automatic adjustments would exceed the cap or because the consent posture changed while finalization was in flight. Blocked finalization is an operator-facing state, not a customer debt state.

### Provider-event lifecycle

```text
received -> queued -> applying -> applied
received -> queued -> applying -> ignored
received -> queued -> applying -> failed -> queued
failed -> dead_letter
```

`ignored` is for duplicate, stale, or irrelevant provider events that were validly received but do not change domain state. `dead_letter` is for events that repeatedly fail and require operator intervention or a code/data fix.

### Billing event delivery lifecycle

```text
pending -> in_progress -> delivered (queue row deleted)
pending -> in_progress -> retryable_failed -> pending
retryable_failed -> in_progress -> dead_letter
dead_letter -> pending (operator requeue, generation incremented)
```

Projection failures are retried by River using deterministic `(event_id, sink, generation)` identity. Delivery to ClickHouse is not part of the authoritative transaction; the immutable `billing_events` row plus active `billing_event_delivery_queue` row is the durable bridge.

### Ledger command lifecycle

```text
pending -> dispatching -> posted
pending -> dispatching -> retryable_failed -> pending
retryable_failed -> dispatching -> posted
retryable_failed -> dispatching -> dead_letter
posted -> reconciled
dead_letter -> pending (operator requeue, generation incremented)
```

Ledger command workers submit persisted TigerBeetle account/transfer specs. `posted` means TigerBeetle acknowledged the command or reconciliation proved every expected object exists with matching fields. `dead_letter` means retrying the same command would be unsafe or has exceeded policy; the corresponding domain rows must remain non-terminal or operator-blocked until resolved.

## Request-path reservation and self-healing

Reservation is a financial lock, not a final charge.

1. Reserve validates org/product/actor/source input.
2. Reserve performs entitlement readiness self-healing from PostgreSQL-only facts.
3. Reserve loads the open billing cycle for the org/product.
4. Reserve loads active pricing and active/grace contract phases from PostgreSQL.
5. Reserve loads eligible posted grant metadata from PostgreSQL and balances from TigerBeetle.
6. Reserve chooses funding legs by the strict scope/source/tier/age waterfall.
7. Reserve inserts `billing_windows(state = 'reserving')`, normalized `billing_window_ledger_legs`, and a `billing_ledger_commands(operation = 'reserve_window')` row containing the exact TigerBeetle pending transfers.
8. Reserve synchronously dispatches the ledger command. The request path may wait on TigerBeetle here because this is the financial lock that decides whether the workload may start.
9. On TigerBeetle success, PostgreSQL marks the window `reserved` and returns admission to the caller.
10. On TigerBeetle insufficient-balance rejection, PostgreSQL marks the window `denied` and returns a no-capacity response.
11. On TigerBeetle unavailability, PostgreSQL leaves the window `reserving` and returns a retryable dependency error. A `reserving` window must not be treated as a valid reservation.
12. Settle computes actual usage, posts final spend, voids any remainder, and only then marks the window `settled`.
13. Metering projection is scheduled after settlement.

The request path never waits for River, Stripe, invoice finalization, email delivery, or ClickHouse to prove current entitlements. It either creates missing deterministic current-period entitlement rows in the request transaction, or fails because the contract state/policy says the org is not entitled.

Self-healing rules:

- Free-tier current-period grants are always self-healable from org, policy, cycle, and calendar state.
- Contract grants are self-healable only when the contract and phase are already `active` or `grace` in PostgreSQL.
- Pending Stripe payment is not self-healable because the payment fact is external.
- If the product intentionally allows immediate access before payment finality, that must be represented by an explicit `grace` transition in PostgreSQL, not inferred during reserve.
- Reserve may close or ignore local rows only when their authoritative PostgreSQL phase/period state already proves they cannot fund current usage.
- Reserve must never call Stripe, scan provider APIs, render invoices, send emails, or read ClickHouse.
- Reserve must never return a usable reservation before TigerBeetle accepts the pending transfer command.
- Reserve must never fall back to PostgreSQL arithmetic when TigerBeetle balance lookup fails.
- Reserve must not create receivable funding legs for org/product postures that lack overage consent. Free-tier orgs and paid hard-cap orgs must deny admission once authorized grants and prepaid balances are exhausted.
- Settlement may record writeoff evidence for leaked no-consent usage, but that evidence must not become a customer receivable. Invoice finalization is the only place where it becomes an automatic adjustment line.

This satisfies the user-facing guarantee: a customer must not lose a valid entitlement because a scheduled job was late. It also preserves payment correctness: a customer must not receive paid entitlements just because a provider event has not arrived.

## Recurring entitlement scheduling

Recurring grants are materialized from durable rows; they are not computed from provider state on the reservation hot path.

- Free-tier eligibility is universal: every org gets the configured free-tier policies. The billing-side org provisioning path must synchronously materialize the current billing cycle, current free-tier `entitlement_periods`, `credit_grants`, TigerBeetle grant deposit command, posted TigerBeetle balance, and `grant_issued` billing event facts before the org can submit billable usage. Reserve also self-heals the same deterministic current-period rows before funding.
- Contract eligibility is phase state plus entitlement-line recurrence. Only `active` and `grace` phases can materialize spendable contract periods. `scheduled` and `pending_payment` phases are planning state and must not fund reservations unless an explicit grace transition has been recorded.
- Self-serve `billing_cycle` lines materialize from Forge Metal cycle boundaries, not Stripe subscription periods. Enterprise `calendar_month_day` lines materialize from the contract timezone and anchor day. `anniversary` anchors are available for non-cycle provider flows that renew from the service start date rather than the calendar.
- Period and grant identifiers are deterministic over org, source, scope, contract, phase, line, policy version, cycle id, and period boundaries. TigerBeetle IDs inside those rows are generated once and persisted, not recomputed from the deterministic domain ID. Retrying org provisioning, webhook handling, River jobs, reconciliation, or reserve self-healing must converge on the same PostgreSQL rows and the same TigerBeetle command IDs.
- River pre-materializes future due work, retries failed materialization, and repairs missed jobs. PostgreSQL rows remain the entitlement truth.

## Cycle rollover and invoice finalization

Cycle rollover and invoice finalization are separate transition paths.

`billing.cycle.rollover` runs at `billing_cycles.ends_at` or at an immediate commercial boundary such as free -> paid or cadence change. It must:

1. Lock the open predecessor cycle.
2. Mark it `closing` and then `closed_for_usage` so no new billing windows attach to it.
3. Apply due period-end `contract_changes` to determine successor policy.
4. Open the successor cycle with deterministic `(org_id, product_id, anchor_at, cycle_seq)` identity.
5. Attach or create successor contract phases as required.
6. Enqueue `billing.invoice.finalize` for the closed predecessor.
7. Enqueue `billing.cycle.rollover` for the successor.

It must not call Stripe, render invoice HTML, send email, or wait for payment collection. That prevents a slow or failed payment rail from causing a customer to lose a valid entitlement window.

`billing.invoice.finalize` runs after a cycle is closed for usage. It must:

1. Lock the cycle and any existing invoice for the cycle.
2. Recompute the invoice snapshot from settled windows, captured rate context, recurring charge policy, contract phases, entitlement periods, and adjustments.
3. Verify that every settled window has posted or voided TigerBeetle ledger legs and no unresolved ledger command needed for the invoice scope.
4. Enforce overage consent and the USD $0.99 automatic no-consent adjustment cap from the PostgreSQL finalization set.
5. Create any required invoice-adjustment and optional no-consent showback ledger commands.
6. Convert ledger units to Stripe cents only after the Forge Metal total is final.
7. Allocate a gapless invoice number.
8. Insert the immutable Forge Metal invoice and invoice line items.
9. Mark the cycle `invoiced`, or mark it `blocked` with a reason.
10. Enqueue Stripe collection if `total_due_units > 0` and payment collection is required.
11. Enqueue invoice email delivery according to notification policy.

Zero-total cycles can still produce invoices when the operator wants a complete free-tier statement history. Dormant zero-usage zero-total cycles may be skipped by explicit policy, but the cycle row remains the source of truth that the period existed.

## Upgrade, downgrade, and cancellation semantics

### Catalog tier ordering

Self-serve catalog tiers must be monotonic for immediate upgrades. A target plan qualifies as an immediate upgrade only when:

- its recurring base price is greater than the current plan's recurring base price for the same product and cadence;
- for every paid entitlement scope shared with the current plan, the target amount is greater than or equal to the current amount;
- any newly introduced entitlement scope is additive;
- the change does not remove a product, bucket, SKU, priority lane, license class, or other commercial right during the current cycle.

If a catalog change is not monotonic, treat it as a period-end downgrade/replacement or an explicit enterprise-style amendment with a previewed contract change. Do not force the general upgrade path to handle negative entitlement deltas. Mid-cycle customer-visible entitlement reduction is a downgrade even when the target plan has a higher headline price.

Immediate upgrade calculations use the open billing cycle as the denominator:

```text
remaining_fraction = (cycle.ends_at - effective_at) / (cycle.ends_at - cycle.starts_at)
price_delta_units = max(target_recurring_price_units - current_recurring_price_units, 0) * remaining_fraction
entitlement_delta_units(scope) = max(target_line_units(scope) - current_line_units(scope), 0) * remaining_fraction
```

Money is calculated in Forge Metal ledger units and rounded to cents only when creating the Stripe collection artifact. Entitlement deltas are calculated per scope with deterministic integer rounding. Rounding must be applied once per applied change; multiple pending same-timestamp plan changes should coalesce to the final target before payment collection to avoid one-unit rounding arbitrage.

### Free tier to paid contract

Free tier remains independent. Creating a paid contract does not close or decrement free-tier grants.

Default flow:

1. API inserts a self-serve `contract` in `pending_activation` and a `contract_change(change_type = 'create', timing = 'immediate')`.
2. API records the intended paid plan in a pending `contract_phase` and copies plan-linked entitlement policies into `contract_entitlement_lines` for that phase.
3. The immediate boundary command calls the same cycle transition used by scheduled rollover: close the current cycle for usage, open a new cycle anchored at the change moment, and enqueue finalization for the closed cycle.
4. Stripe collection for the paid activation is created as a one-off invoice or hosted payment flow against the Forge Metal invoice artifact; no Stripe Subscription is created.
5. Provider events are persisted in `billing_provider_events` and applied asynchronously.
6. On `invoice.paid` or an explicitly accepted grace decision, the worker sets the contract and phase entitlement state to `active` or `grace`.
7. Entitlement periods and credit grants are materialized for the new cycle after paid activation.
8. Free-tier grants continue independently and must not be closed by the paid activation.

Because free -> paid closes the free cycle for usage and opens a new paid cycle anchored at the paid activation time, the first paid Hobby cycle normally charges the full Hobby cycle price and issues the full Hobby contract grants for that new cycle. Free-tier grants from their own recurrence remain separate sources and continue to drain before contract grants.

### Immediate paid upgrade

Default for Hobby -> Pro.

1. Insert `contract_changes(change_type = 'upgrade', timing = 'immediate')` with the current phase and target plan.
2. Compute and store the proration basis from the locked current cycle, current phase, target plan, and effective timestamp.
3. Keep the old phase active until payment succeeds or an explicitly accepted grace decision arrives.
4. Create the target Pro phase in `pending_payment` or `grace` according to policy.
5. Create a one-off Forge Metal invoice for the positive prorated price delta when the business policy requires immediate collection.
6. Provider API success moves the change to `awaiting_payment`; it does not activate new paid entitlements by itself.
7. On `invoice.paid` or accepted grace, set the old phase `effective_end = actual_effective_at` and `state = 'superseded'`.
8. Activate the new phase with `effective_start = actual_effective_at`. The target phase carries full Pro entitlement lines because those terms apply to subsequent full cycles.
9. Leave already-issued current-cycle grants from the superseded Hobby phase open until their own `expires_at`. They are paid allowance carryforward, not a mutable live phase lookup.
10. Materialize only upgrade-delta entitlement periods and grants for the target Pro phase for the remaining part of the current cycle.
11. From the next cycle onward, materialize normal full Pro entitlement periods and grants.

The billing cycle usually does not change for a paid mid-cycle upgrade. Proration is not a live invoice-time lookup; it is encoded by phase boundaries, change-linked upgrade-delta entitlement periods, carried-forward current-cycle grants, and captured rate context on subsequent billing windows.

Example:

```text
Current plan: Hobby at $5/month with 30,000,000 compute units.
Target plan: Pro at $20/month with 120,000,000 compute units.
Upgrade time: 25% through the paid Hobby cycle, so remaining_fraction = 75%.
Hobby usage so far: 90% of Hobby compute, so 27,000,000 used and 3,000,000 Hobby compute left.

Upgrade price delta: ($20 - $5) * 75% = $11.25 before tax.
Upgrade entitlement delta: (120,000,000 - 30,000,000) * 75% = 67,500,000 compute units.
Post-upgrade paid compute available: 3,000,000 Hobby carryforward + 67,500,000 Pro delta = 70,500,000 units.
```

The customer does not get a full prorated Pro grant of `120,000,000 * 75% = 90,000,000` units on top of heavy Hobby usage, because that creates step-through-tier arbitrage. The customer also does not lose unused Hobby allowance when they upgrade early, because that makes upgrades feel punitive when usage is low. The path-independent total paid entitlement for the cycle is the original Hobby grant plus the prorated positive delta from Hobby to Pro.

Pending reservations that already selected old Hobby funding before the upgrade settle or void against their original funding legs. The upgrade path must not snapshot TigerBeetle balances and re-mint "remaining Hobby" into a replacement grant; keeping the old current-cycle grant open until expiry avoids double-spend and lost-capacity races around pending reservations. New reservations after Pro activation can consume remaining Hobby carryforward plus the Pro delta grant, while their pricing context is captured from the active Pro phase.

Paid overages accrued before the upgrade remain attached to the old phase/rate context captured in their billing windows. They are not netted against the upgrade charge or erased by the Pro activation. If usage leaked without overage consent, invoice finalization applies the automatic no-consent adjustment rules rather than charging the customer.

Failure cases:

- If payment fails and no grace transition is accepted, the target phase remains `pending_payment` or the change moves to `failed`; no Pro delta grant materializes.
- If payment succeeds after a retry, use the stored proration basis from the accepted change, not a newly computed later timestamp, unless the customer explicitly accepts a new preview.
- If the customer requests another upgrade while the first change is pending, coalesce or cancel the pending change and create a new preview. Do not apply two same-effective-time prorations independently.
- If a plan change would reduce any current-cycle entitlement scope, schedule it for period end unless an explicit admin or enterprise amendment records the customer-visible consequence.

### Period-end downgrade

Default for Pro -> Hobby.

1. Insert `contract_changes(change_type = 'downgrade', timing = 'period_end')` with the target plan.
2. Keep the current Pro phase active until the current billing cycle ends.
3. Do not store the downgrade as `next_cycle_plan_id` on the cycle.
4. At `billing.cycle.rollover`, apply the scheduled change, close the Pro phase, create or activate the Hobby phase for the successor cycle, and open the successor cycle.
5. Issue Hobby grants for the new cycle after payment or grace rules allow.

If the River boundary job is late, reserve uses PostgreSQL state and self-healing rules. Reconciliation repairs the missing boundary job. Downgrades must not take away paid capacity before the period the customer already paid for ends.

If the customer resumes the current Pro plan before the period boundary, cancel the scheduled downgrade locally. The current Pro phase remains active, the scheduled Hobby phase is not activated, and no Stripe Checkout or invoice is created because no new payment method, payment consent, or charge is required.

Immediate downgrades are not a self-serve default. If an explicit admin or enterprise amendment allows an immediate downgrade, it must preview the customer-visible result, avoid negative entitlement grants, and represent any refund or account credit as an invoice adjustment or credit-note artifact rather than mutating prior grants.

### Cancellation

Default for paid -> free is period-end cancellation.

1. Insert `contract_changes(change_type = 'cancel', timing = 'period_end')`.
2. Mark the contract `cancel_scheduled` and preserve the active phase until the current billing cycle ends.
3. At cycle rollover, close the active paid phase, close remaining phase grants, mark the paid contract `ended`, and open a successor cycle with no active paid phase.
4. Free-tier grants continue independently and are reconciled on their calendar or billing-cycle schedule.

If the customer resumes the same paid plan before `cancel_at`, cancel the scheduled cancellation locally. The contract returns from `cancel_scheduled` to `active`, the active phase's period-end `effective_end` is cleared, and no Stripe Checkout or invoice is created.

Immediate cancellation is reserved for explicit admin actions, fraud, or payment terminality, and must record a closed reason.

### Cadence changes

Changing from anniversary monthly to calendar monthly, monthly to annual, or product-specific cadence to enterprise calendar cadence is an immediate cycle boundary unless the contract change explicitly schedules it for period end.

The transition closes the current cycle for usage, opens a successor cycle with the new `cadence_kind` and anchor, and enqueues finalization for the closed cycle. This keeps cadence changes on the same path as free -> paid and avoids special-cycle code.

## Stripe provider ingress and hardening

Stripe is one provider adapter for self-serve payment collection and payment-method management. Stripe Subscriptions are not part of the target architecture.

Target Stripe usage:

1. Vaulting: SetupIntent -> PaymentMethod -> `payment_methods` row linked to `provider_customer_id`.
2. Payment-method management: Customer Portal for card management, not a first-party card-vault UI.
3. Invoice collection: one-off Stripe invoices or payment intents created from an issued immutable Forge Metal invoice artifact. The only exception is Stripe Tax pre-issue draft verification, where a provider draft is created from a Forge Metal finalization candidate solely to compute and reconcile tax before the Forge Metal artifact is issued.
4. Optional Stripe Tax: when enabled, Stripe tax computation is a pre-issue finalization input. The Forge Metal invoice must not be issued until tax units are known and reconciled into the stored invoice snapshot.
5. Refunds/disputes: provider events update Forge Metal payment state and adjustment/refund records.

The Stripe webhook route must do the minimum synchronous work required to safely accept a provider event:

1. Receive the raw request body.
2. Verify the `Stripe-Signature` header against the endpoint secret.
3. Reject events from unsupported providers or unsupported event shapes.
4. Insert or update `billing_provider_events` under unique `(provider, provider_event_id)`.
5. Enqueue `billing.provider_event.apply` transactionally with the provider-event row.
6. Return `2xx` after durable persistence and enqueueing, not after downstream contract/grant/invoice mutation.

The provider-event worker translates Stripe objects into provider-neutral state changes. Supported Stripe signals include:

- `setup_intent.succeeded`
- `setup_intent.setup_failed`
- `invoice.finalized`
- `invoice.paid`
- `invoice.payment_failed`
- `payment_intent.succeeded`
- `payment_intent.payment_failed`
- `charge.refunded`
- `charge.dispute.created`
- `charge.dispute.closed`

Do not subscribe the billing endpoint to `customer.subscription.*` events in the target architecture.

Stripe collection flow for a finalizing or issued Forge Metal invoice:

1. Build a Forge Metal finalization candidate in PostgreSQL from settled windows, recurring charges, adjustments, rounding policy, and overage-consent policy.
2. When Stripe Tax is disabled, issue the immutable Forge Metal invoice before provider collection. When Stripe Tax is enabled, create a Stripe draft from the finalization candidate before issue, use it only to compute/verify tax, persist the reconciled tax units into `invoice_snapshot_json`, and then issue the immutable Forge Metal invoice.
3. Create the Stripe draft invoice with `auto_advance = false` so Stripe cannot finalize the provider invoice before Forge Metal verification completes, `collection_method = charge_automatically` only when the org has collection consent, and metadata containing `invoice_id`.
4. Add invoice items with deterministic idempotency keys per Forge Metal invoice line and finalization generation. Do not reuse a Stripe idempotency key after changing request parameters.
5. Verify the Stripe draft total matches the Forge Metal invoice total after ledger-unit-to-cent rounding and tax policy.
6. After draft verification and Forge Metal issue, explicitly finalize the Stripe invoice. If Stripe owns initial dunning, configure the invoice so Stripe performs automatic collection/retry after finalization and report results through webhooks. If Forge Metal later owns dunning, keep provider automation disabled, explicitly finalize/pay through `billing.payment.retry`, and model retries as River-driven domain work.
7. Persist `stripe_invoice_id`, hosted invoice URL, invoice PDF URL, payment intent ID, and provider status on the Forge Metal invoice row.
8. On payment success, persist the provider event and dispatch ledger commands for `stripe_payment_in` plus `receivable_clear_payment` for the invoice's outstanding receivable accounts.
9. Treat Stripe webhooks as payment-state inputs, not invoice truth.

When Stripe Tax is enabled, the draft creation and tax verification steps occur before the Forge Metal invoice is marked `issued`; after issue, Stripe collection must not mutate the Forge Metal invoice total.

The ledger unit to Stripe cent conversion must be explicit. Stripe invoice amounts are cent-denominated for USD. Forge Metal ledger units are finer-grained, so finalization must apply one rounding/residual policy: carry forward residuals, write them off through an adjustment, or accumulate them in an org/product rounding bucket. Silent truncation is not allowed.

Direct top-up collection flow:

1. Create a provider checkout/payment-intent artifact for the exact top-up amount. Metadata must include `org_id`, `product_id`, intended ledger units, purchase idempotency key, and return-flow correlation.
2. Do not create spendable balance from checkout return alone. The balance appears only after durable payment success is recorded in `billing_provider_events`.
3. The provider-event worker creates or finds the deterministic `credit_grants(source = 'purchase')` row, persists TigerBeetle IDs, and dispatches the linked `stripe_payment_in` and `purchase_grant_deposit` command.
4. The provider event is not `applied` until the ledger command is posted or safely determined to have already posted with matching IDs.
5. `grant_issued` is emitted only after the grant is TigerBeetle-backed and spendable.

This gives the browser a repeatable polling story: checkout completion means payment is being applied; `grant_issued` plus the TB-backed grant balance means the account balance changed.

Hardening requirements:

- Use HTTPS/TLS for live webhook endpoints.
- Verify raw-body signatures with Stripe's official library.
- Allowlist Stripe's published webhook source IPs at the edge while still requiring signature verification.
- Keep endpoint signing secrets out of process args and environment variables; load through systemd credentials or the repo's secret plane.
- Exempt the webhook route from CSRF middleware if a framework would otherwise apply it.
- Persist provider events before applying them.
- Handle duplicate deliveries using `(provider, provider_event_id)` idempotency.
- Handle automatic retries by making provider-event application idempotent and replayable.
- Use Stripe sandboxes to validate invoice creation, payment-method vaulting, duplicate events, delayed events, replay, payment failure, refunds, disputes, and tax behavior.
- Disable Stripe invoice emails. The target Forge Metal path sends invoice emails through mailbox-service from the stored Forge Metal invoice body.

Stripe docs to keep near this design:

- Webhooks: <https://docs.stripe.com/webhooks>
- Integration security guide: <https://docs.stripe.com/security/guide>
- Domains and IP addresses: <https://docs.stripe.com/ips>
- Process undelivered webhook events: <https://docs.stripe.com/webhooks/process-undelivered-events>
- Sandboxes: <https://docs.stripe.com/sandboxes>
- SetupIntents: <https://docs.stripe.com/payments/setup-intents>
- Invoice integration: <https://docs.stripe.com/invoicing/integration>
- Automatic invoice advancement: <https://docs.stripe.com/invoicing/integration/automatic-advancement-collection>
- Idempotent requests: <https://docs.stripe.com/api/idempotent_requests>
- Invoices API: <https://docs.stripe.com/api/invoices>
- Create invoice API: <https://docs.stripe.com/api/invoices/create>
- Finalize invoice API: <https://docs.stripe.com/api/invoices/finalize>
- Invoice items API: <https://docs.stripe.com/api/invoiceitems>

Stripe never becomes the source of truth for cycles, contract phases, SKU pricing, entitlement scope, grant funding, invoice artifacts, invoice numbering, or metering. It is strongest as a payment-method, hosted payment, optional tax, refund, dispute, and lifecycle-signal provider.

## Enterprise contracts

Enterprise contracts use the same tables and state machines.

- A bespoke enterprise agreement creates a `contracts` row with `contract_kind = 'enterprise'`.
- The active terms live in `contract_phases` with `phase_kind = 'bespoke'`.
- Recurring allowances live in `contract_entitlement_lines` with `recurrence_anchor_kind = 'calendar_month_day'`, an anchor day, and a contract timezone.
- Invoice cycles live in `billing_cycles` with `cadence_kind = 'calendar_monthly'`, `annual`, or `manual` depending on the contract.
- Amendments, renewals, upgrades, downgrades, and cancellations use `contract_changes`.
- Payment collection can be manual at first and later integrated through a provider binding without changing entitlement generation.
- River schedules phase boundaries, cycle rollover, entitlement materialization, invoice finalization, invoice delivery, and reconciliation exactly as it does for self-serve contracts.
- TigerBeetle account and transfer flows are the same as self-serve contracts: contract allowances fund `customer_grant` accounts, reservations post or void through window ledger legs, consented overage accrues to customer receivable accounts, and manual payment clears receivables through a provider or manual clearing command.

The design must not fork into a second enterprise billing schema. Enterprise is a contract kind, phase kind, recurrence configuration, cycle cadence, and optional provider binding, not a parallel billing engine.

## Entitlements view

`GET /internal/billing/v1/orgs/{org_id}/entitlements` returns the same posted grants the funder will consume, grouped into customer-facing slots by `(scope, product, bucket, sku)` and balanced from TigerBeetle account lookups. The view-model deliberately refuses to sum across slots because the moment a customer holds any credit narrower than account scope, a single top-line balance is dishonest about what the next reservation can actually spend.

The shape:

- A `universal` slot carries every `account`-scope source total.
- A `products[]` array carries one section per product.
- Each product section can carry a `product_slot` for product-scope grants.
- Each bucket section can carry a `bucket_slot` for bucket-scope grants and `sku_slots[]` for SKU-scope grants.
- Each slot surfaces source totals keyed by source and source identity. Non-contract sources collapse by source. Contract sources must preserve enough identity to distinguish Hobby, Pro, and enterprise grants when multiple phases are visible during a transition.
- Pending or failed ledger-posting grants are operator-visible but must not be included in spendable customer totals.

Within a bucket table, rows are sorted bucket-scope-first then SKU-scope, then by `GrantSourceFundingOrder`. Across the view, the cell-level next-to-spend position is a load-bearing claim about funder behavior. The contract is pinned by `entitlements_view_funding_test.go`: every cell projects back to a `scopedGrantBalance` and runs `planGrantFunding` against a representative charge, asserting the funder's first leg matches the cell's top entry. Any future change to `GrantScopeFundingOrder`, `GrantSourceFundingOrder`, or the view's sort logic must keep that test green or the customer-facing claim drifts from reality.

## Invoice preview

Invoice preview is built from the same data invoice finalization uses:

1. The current or closed `billing_cycles` row.
2. Settled billing windows in PostgreSQL.
3. Normalized `billing_window_ledger_legs` for grant, receivable, source, and SKU attribution.
4. Projected metering rows in ClickHouse when available for display acceleration, with PostgreSQL plus TigerBeetle-backed ledger legs as truth.
5. SKU metadata and captured rate context from billing windows.
6. Contract, phase, and entitlement-period metadata from PostgreSQL.
7. Adjustment rules and entitlement/grant consumption.
8. Forge Metal invoice snapshot rows once issued.

The preview shows:

- SKU line items first, using the SKU display name and quantity unit captured for the invoice.
- Bucket summaries next, using the bucket display name.
- Free-tier, contract, purchased, promo, and refund funding after that.
- Automatic system adjustments after funding in operator/finalization views. Customer-facing invoices render them only when `customer_visible = true`, and never as spendable balance.
- Reserved but not yet finalized execution spend as a separate line.
- Remaining entitlement for the billing cycle and any purchased balance.

Blocked finalizations are not collectible invoices. They render operator-facing policy failure state and must not render as a customer amount due.

That keeps the preview structurally aligned with the final invoice rather than inventing a separate UI model.

## ClickHouse metering and invoice projection

ClickHouse stores the invoice read model, not the transaction ledger.

Billing event projection is at-least-once. Projection tables that use `ReplacingMergeTree` deduplicate during background merges, so they can temporarily expose duplicate rows for the same deterministic key. Operator verification queries that require immediate uniqueness must query PostgreSQL domain truth, use ClickHouse `FINAL`, or explicitly group by deterministic identifiers such as `event_id` and the relevant version column. Production authorization, invoice issuance, ledger writes, queue deletion, and provider-event application must not depend on ClickHouse merge timing.

The target metering projection contains row-level usage evidence and projected charge units, including:

- `cycle_id`
- `pricing_contract_id`
- `pricing_phase_id`
- `pricing_plan_id`
- `pricing_phase`
- `component_quantities`
- `component_charge_units`
- `bucket_charge_units`
- `component_free_tier_units`
- `component_contract_units`
- `component_purchase_units`
- `component_promo_units`
- `component_refund_units`
- `component_receivable_units`
- `component_adjustment_units`
- `adjustment_units`
- `adjustment_reason`
- `usage_evidence`

The per-source drain maps are keyed by SKU id, not bucket id. The funder attributes every cent of every drain to the SKU that triggered it via the `ChargeSKUID` axis on funding legs. These maps preserve that attribution through to ClickHouse so the customer-facing invoice can show per-line drain splits without a secondary aggregation. Bucket-level drain splits are derivable by grouping `component_*_units` keys through each row's `rate_context.sku_buckets` mapping in analytics queries that still need the bucket axis.

Invoice projections include `billing_cycle_opened`, `billing_cycle_closed_for_usage`, `invoice_issued`, `invoice_adjustment_created`, `invoice_finalization_blocked`, `stripe_invoice_collection_started`, `stripe_invoice_paid`, `stripe_invoice_payment_failed`, and `invoice_email_sent` events. These are proof/read-model facts; PostgreSQL remains authoritative for the invoice artifact and state machine.

Ledger projections include `grant_ledger_posted`, `ledger_command_posted`, `ledger_command_failed`, `ledger_drift_detected`, `billing_window_reserved`, `billing_window_settled`, `billing_window_voided`, `grant_expired`, `receivable_accrued`, `receivable_cleared`, and `no_consent_adjustment_showback_posted`. These projections prove that PostgreSQL domain rows and TigerBeetle side effects converged; they do not replace TigerBeetle account lookup for balance reads.

ClickHouse billing rows use contract projection names (`contract_units`, `pricing_contract_id`, `pricing_phase_id`, `pricing_plan_id`) rather than provider-specific subscription field names.

For sandbox jobs, trusted block storage evidence comes from the orchestrator's provisioned zvol size and is written as `rootfs_provisioned_bytes` in usage evidence. That gives the invoice preview a real storage signal instead of an inferred one.

ClickHouse docs to keep near this design:

- ReplacingMergeTree: <https://clickhouse.com/docs/engines/table-engines/mergetree-family/replacingmergetree>

## Fault injection and reconciliation

Fault injection targets provider-neutral seams rather than relying only on end-to-end Stripe payloads.

Provider-event fault cases:

- Duplicate provider event with identical `provider_event_id`.
- Delayed `invoice.paid` after local Stripe invoice creation.
- `invoice.payment_failed` followed by later `invoice.paid`.
- `setup_intent.succeeded` without overage consent.
- Refund or dispute event for an invoice already marked paid locally.
- Missing provider metadata resolved through local binding state.
- Unsupported provider event type recorded and ignored.
- Worker failure after provider event is recorded but before invoice/payment mutation.
- Worker failure after invoice/payment mutation but before event delivery projection.

Scheduler fault cases:

- River job missing for a due phase boundary.
- River job missing for cycle rollover.
- River job retried after the cycle successor has already opened.
- River job retried after the phase has already closed.
- Entitlement materialization job runs twice for the same period.
- Event delivery projection job fails after ClickHouse insert but before deleting the queue row.
- Reserve runs before scheduled free-tier or active contract entitlement materialization.
- Invoice finalization is delayed while successor cycle remains open for valid usage.

Ledger fault cases:

- Grant deposit command posts in TigerBeetle but the process crashes before PostgreSQL marks the grant posted.
- Grant deposit command persists in PostgreSQL but TigerBeetle is unavailable before dispatch.
- Reservation command persists with `billing_windows.state = 'reserving'` and the process crashes before dispatch.
- Reservation command posts in TigerBeetle but PostgreSQL update to `reserved` fails.
- Reservation command is rejected for insufficient balance after PostgreSQL selected apparently eligible grants.
- Settlement command posts only after a retry; the window must remain non-terminal until acknowledgement.
- Void command is delayed after a workload launch failure; pending balance must be released by retry or TigerBeetle timeout.
- Duplicate ledger command dispatch replays the same TigerBeetle IDs and receives idempotent exists/already-posted results.
- Operator account registry differs from TigerBeetle account flags, ledger, code, or `user_data`.
- Reconciliation finds a posted PostgreSQL leg without the expected TigerBeetle transfer.
- Reconciliation finds a TigerBeetle customer-grant account that PostgreSQL does not know about.
- TigerBeetle is unavailable during a customer balance read; the service must fail or return a marked stale projection, not recompute financial truth from PostgreSQL JSON.

Invoice finalization fault cases:

- Free-tier org has a payment method on file but no paid contract or overage consent.
- Free-tier leaked overage is below the automatic-adjustment cap.
- Free-tier leaked overage exceeds the automatic-adjustment cap.
- Paid hard-cap org leaks usage after grants and prepaid balances are exhausted.
- Duplicate invoice finalization job retries after automatic adjustments were created.
- Overage consent changes while invoice finalization is in progress.
- Stripe draft invoice creation succeeds but invoice item creation fails.
- Stripe total diverges from the Forge Metal invoice after rounding or tax.
- Invoice email delivery fails after invoice issue and Stripe collection succeeds.

Expected behavior is convergence, not exactly-once execution. PostgreSQL state, deterministic identifiers, TigerBeetle idempotency, Stripe idempotency keys, invoice immutability, and billing event delivery idempotency must make repeated work safe.

## API naming target

The public/internal billing API is contract- and invoice-oriented:

- `/contracts` instead of `/subscriptions`
- `/contracts/{contract_id}/changes` for upgrade, downgrade, cancel, renew, and amend requests
- `/billing-cycles` for cycle inspection and operator repair
- `/invoices` and `/invoices/{invoice_id}` for issued invoice artifacts
- `/invoices/{invoice_id}/adjustments` for manual/admin adjustments and credit-note flows
- `/payment-methods` for setup-intent-backed payment method management state
- `/provider-events` instead of `/subscription-provider-events`
- `/ledger-commands` and `/ledger-drift` for operator-only inspection and repair; customer APIs should expose balances, invoices, grants, and usage explanations, not raw TigerBeetle account internals
- `contract_id`, `phase_id`, `cycle_id`, `invoice_id`, and `change_id` in responses instead of a single mutable subscription plan field

Stripe-specific terms appear only at provider-adapter boundaries, provider binding rows, payment method rows, provider event rows, and Stripe UI flows such as Customer Portal and hosted payment collection.

## Production verification gates

Use PostgreSQL, ClickHouse traces, metering rows, invoice rows, provider-event rows, and billing events as the proof point for the deployed path.

1. **River billing runtime present**

```sql
SELECT queue, kind, state, count(*)
FROM river_job
WHERE kind LIKE 'billing.%'
GROUP BY queue, kind, state
ORDER BY queue, kind, state
```

2. **Billing cycles are open and non-overlapping**

```sql
SELECT
  org_id,
  product_id,
  status,
  count(*)
FROM billing_cycles
WHERE status IN ('open', 'closing')
GROUP BY org_id, product_id, status
ORDER BY org_id, product_id, status
```

3. **Provider events durably ingested**

```sql
SELECT
  provider,
  provider_event_id,
  event_type,
  state,
  attempts,
  received_at,
  applied_at,
  last_error
FROM billing_provider_events
ORDER BY received_at DESC
LIMIT 20
```

4. **Grant events and ledger postings present**

Confirm grant issuance and ledger-posting facts are visible in ClickHouse after org creation, free-tier reconciliation, purchase, contract activation, phase change, enterprise amendment, or direct top-up.

```sql
SELECT
  event_id,
  event_type,
  event_version,
  aggregate_type,
  aggregate_id,
  org_id,
  product_id,
  payload,
  payload_hash,
  correlation_id,
  causation_event_id,
  recorded_at
FROM forge_metal.billing_events FINAL
WHERE event_type IN ('grant_issued', 'grant_ledger_posted')
ORDER BY recorded_at DESC
LIMIT 5
```

5. **Ledger commands are drained or intentionally blocked**

```sql
SELECT
  operation,
  state,
  count(*) AS commands,
  min(created_at) AS oldest_created_at,
  max(last_error) AS sample_error
FROM billing_ledger_commands
GROUP BY operation, state
ORDER BY operation, state
```

The normal steady state has no stale `pending`, `dispatching`, or `retryable_failed` commands outside the retry policy. `dead_letter` is allowed only when an operator is actively resolving a documented incident.

6. **Ledger drift is empty**

```sql
SELECT
  severity,
  drift_type,
  aggregate_type,
  aggregate_id,
  state,
  detected_at,
  observed
FROM billing_ledger_drift_events
WHERE state = 'open'
ORDER BY detected_at DESC
LIMIT 20
```

The expected result is zero open critical drift rows. Any open `critical` row should trip the ledger write guard and appear in ClickHouse as `ledger_drift_detected`.

7. **Contract and cycle events present**

Confirm provider events, change requests, phase transitions, and cycle transitions project into `forge_metal.billing_events`.

```sql
SELECT
  event_id,
  event_type,
  event_version,
  aggregate_type,
  aggregate_id,
  org_id,
  product_id,
  payload,
  payload_hash,
  correlation_id,
  causation_event_id,
  recorded_at
FROM forge_metal.billing_events FINAL
WHERE event_type IN (
  'contract_created',
  'contract_change_requested',
  'contract_change_applied',
  'contract_phase_started',
  'contract_phase_closed',
  'provider_event_received',
  'provider_event_applied',
  'billing_cycle_opened',
  'billing_cycle_closed_for_usage'
)
ORDER BY recorded_at DESC
LIMIT 20
```

8. **Plan-change proration facts are auditable**

Confirm immediate upgrades record their price and entitlement proration basis in PostgreSQL and project the applied change to ClickHouse.

```sql
SELECT
  change_id,
  change_type,
  state,
  timing,
  target_plan_id,
  from_phase_id,
  to_phase_id,
  proration_basis_cycle_id,
  price_delta_units,
  entitlement_delta_mode,
  proration_numerator,
  proration_denominator,
  actual_effective_at
FROM contract_changes
WHERE change_type = 'upgrade'
ORDER BY updated_at DESC
LIMIT 20
```

```sql
SELECT
  event_type,
  JSONExtractString(payload, 'change_type') AS change_type,
  JSONExtractString(payload, 'from_plan_id') AS from_plan_id,
  JSONExtractString(payload, 'target_plan_id') AS target_plan_id,
  pricing_plan_id,
  count() AS events
FROM forge_metal.billing_events FINAL
WHERE event_type IN ('contract_change_applied', 'contract_phase_started', 'contract_phase_closed', 'grant_issued')
GROUP BY event_type, change_type, from_plan_id, target_plan_id, pricing_plan_id
ORDER BY event_type, pricing_plan_id, change_type
```

For a Hobby -> Pro upgrade in the target implementation, expect one applied upgrade change, one closed/superseded Hobby phase, one started Pro phase, carryforward Hobby grants still expiring at the current cycle end, and Pro `grant_issued` rows only for the positive prorated entitlement deltas in the current cycle.

9. **Reservation trace and ledger command present**

Confirm a sandbox job or billed workload produced a reservation trace in `billing-service`; query for the matching `window_id` in `default.otel_logs`, `billing_window_ledger_legs`, `billing_ledger_commands`, and the `forge_metal.metering` row.

10. **SKU projection present**

Confirm the metering row contains SKU-level charge maps, bucket totals, cycle id, and usage evidence.

```sql
SELECT
  window_id,
  cycle_id,
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

11. **Storage evidence present**

Confirm `usage_evidence['rootfs_provisioned_bytes']` is non-zero for a sandbox execution that used a real zvol.

```sql
SELECT
  window_id,
  arrayElement(usage_evidence, 'rootfs_provisioned_bytes') AS rootfs_provisioned_bytes
FROM forge_metal.metering
WHERE product_id = 'sandbox'
ORDER BY recorded_at DESC
LIMIT 5
```

12. **Bucket totals reconcile**

Confirm component charges sum into bucket charges, and bucket charges sum into the row charge units.

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

13. **Invoice artifact is immutable and issued from stored snapshot**

```sql
SELECT
  invoice_id,
  invoice_number,
  org_id,
  product_id,
  cycle_id,
  status,
  payment_status,
  total_due_units,
  content_hash,
  stripe_invoice_id,
  issued_at
FROM billing_invoices
ORDER BY issued_at DESC
LIMIT 20
```

14. **No-consent adjustments enforce the cap**

Confirm free-tier and paid hard-cap orgs do not produce collectible receivable units without overage consent. Automatic no-consent adjustments must stay within the USD $0.99 per-org finalization cap; cap overflow must create a blocked finalization event instead of a customer charge.

```sql
SELECT
  invoice_finalization_id,
  org_id,
  sum(amount_units) FILTER (WHERE adjustment_source = 'system_policy') AS automatic_adjustment_units,
  max(reason_code) AS example_reason
FROM invoice_adjustments
WHERE reason_code IN ('free_tier_overage_absorbed', 'paid_hard_cap_overage_absorbed')
GROUP BY invoice_finalization_id, org_id
ORDER BY invoice_finalization_id DESC
LIMIT 20
```

```sql
SELECT
  event_id,
  event_type,
  event_version,
  aggregate_id,
  org_id,
  payload,
  payload_hash,
  recorded_at
FROM forge_metal.billing_events FINAL
WHERE event_type IN ('invoice_adjustment_created', 'invoice_finalization_blocked')
ORDER BY recorded_at DESC
LIMIT 20
```

15. **Stripe subscriptions are absent from target provider events**

```sql
SELECT event_type, count(*)
FROM billing_provider_events
WHERE provider = 'stripe'
GROUP BY event_type
ORDER BY event_type
```

The expected target event set contains setup-intent, invoice, payment-intent, charge, refund, and dispute events. It must not require `customer.subscription.*` events.

## Related docs

- `src/sandbox-rental-service/docs/vm-execution-control-plane.md`
- `src/platform/docs/identity-and-iam.md`
- `src/apiwire/docs/wire-contracts.md`
