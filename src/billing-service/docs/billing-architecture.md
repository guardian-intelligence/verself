# Billing Architecture

This document describes the target billing architecture for usage-based billing, prepaid credits, self-serve paid plans, and bespoke enterprise contracts.

The core thesis is:

```text
PostgreSQL says what is true and what is due.
River wakes workers to advance what is due.
Workers apply idempotent state transitions.
TigerBeetle records operational financial truth.
ClickHouse proves and presents the projection.
Verself owns billing periods, plan policy, finalization, billing documents, and consent.
Stripe is a payment rail and hosted payment-method provider, not the billing domain model.
```

PostgreSQL owns billing domain state and scheduling facts: catalog rows, contracts, contract changes, contract phases, entitlement lines, entitlement periods, credit-grant metadata, billing-cycle rows, billing-window metadata, finalizations, billing documents, invoice adjustments, provider bindings, provider events, ledger command rows, billing event rows, event-delivery queue rows, projection-delivery queue rows, and reconciliation cursors. TigerBeetle owns accepted balance-changing account and transfer facts. River owns durable asynchronous execution of billing work derived from PostgreSQL state. A River job is a wakeup, retry, concurrency, and observability handle; it is not entitlement or ledger truth. If a River job is late, duplicated, retried, canceled, or reconstructed by reconciliation, deterministic PostgreSQL identifiers and persisted TigerBeetle IDs still converge to the same contract, phase, cycle, entitlement period, grant, ledger command, finalization, document, adjustment, and billing event facts.

Stripe is a payment and hosted billing provider. The target architecture does not use Stripe Subscriptions as the self-serve contract state machine. Verself owns cadence, cycle rollover, contract changes, plan phases, entitlement materialization, finalization, billing document issue, overage consent, and dunning policy. Stripe is consulted when a card is vaulted, a hosted payment-method management surface is needed, a Verself invoice document is sent to Stripe for collection, a payment/refund/dispute event arrives, or Stripe Tax is enabled.

Reference points in this repo:

- `src/platform/docs/identity-and-iam.md` for org/auth ownership boundaries.
- `src/sandbox-rental-service/docs/vm-execution-control-plane.md` for the reserve/settle split used by sandbox jobs and the existing River control-plane pattern.
- `src/apiwire/docs/wire-contracts.md` for wire-shape and generated-client conventions.

Provider reference points:

- Stripe subscription updates support prorations and can invoice prorations immediately with `proration_behavior = always_invoice`: <https://docs.stripe.com/billing/subscriptions/prorations> and <https://docs.stripe.com/api/subscriptions/update>.
- Paddle subscription updates require an explicit `proration_billing_mode` when replacing subscription items: <https://developer.paddle.com/build/subscriptions/replace-products-prices-upgrade-downgrade>.
- Recurly and Chargebee expose proration and timing as subscription-change policy knobs rather than one universal behavior: <https://docs.recurly.com/recurly-subscriptions/docs/change-subscription> and <https://www.chargebee.com/docs/billing/2.0/subscriptions/proration>.

Verself follows the industry-standard price-side shape: immediate upgrades can charge the prorated positive price delta now; downgrades default to the next renewal unless explicitly overridden. Verself must additionally define entitlement-side proration because Stripe, Paddle, Recurly, and Chargebee do not model our credit-bucket grant semantics.

## Non-negotiable invariants

- Every commercial entitlement must be derivable from PostgreSQL state.
- Every queued worker must be idempotent over a deterministic domain identity.
- Every worker transition must re-read PostgreSQL truth and use compare-and-swap, row locks, or equivalent state/version checks before side effects.
- River jobs may run late, early, duplicated, or after a retry. Workers must inspect state before doing work and exit cleanly when work is no longer due.
- Request-path reservation must not depend on a scheduled job having run on time.
- A missed, delayed, duplicated, or canceled renewal worker is platform
  uncertainty, not customer policy evidence. Access revocation, write freeze,
  VM termination, or tombstoning may follow only from an explicit persisted
  business denial such as insufficient balance, suspended org, expired contract,
  or payment-policy state.
- Request-path self-healing may create deterministic current-period entitlement rows and grants from already-authorized PostgreSQL state.
- Request-path self-healing must not call Stripe, infer payment facts, or depend on ClickHouse.
- Spendable balances, posted consumption, top-up deposits, receivable accrual, receivable clearing, expiry sweeps, and financial corrections must be represented in TigerBeetle. Request-path admission holds are PostgreSQL authorization windows. PostgreSQL rows describe the domain operation; TigerBeetle accounts and transfers are the posted-balance authority.
- PostgreSQL domain identifiers remain deterministic text identifiers. TigerBeetle account IDs, transfer IDs, and correlation IDs are separate 128-bit values generated with TigerBeetle's time-based ID scheme, persisted before dispatch, and reused on retry.
- A PostgreSQL row must not become customer-spendable or terminally settled until the corresponding TigerBeetle command has been acknowledged or reconciled as already posted.
- Billing cycles define backend billing periods only. Contract phases define commercial policy intervals. Do not add a parallel `plan_bindings` table unless `contract_phases` is being renamed.
- Usage lines must be computed from `billing_windows` and their captured `rate_context`/funding legs, not from live plan rates.
- Cycle rollover must not wait on Stripe, document rendering, email delivery, or payment collection. New usage must have a successor cycle even when finalization is delayed or blocked.
- Provider webhooks must be durably recorded before being applied.
- ClickHouse projection may lag and must never be required for authorization, document issuance, or ledger correctness.
- Stripe never owns cadence, billing cycles, contract shape, SKU rates, grant scope, entitlement precedence, billing-window funding, document numbering, or metering.
- The free tier is universal and independent from paid contracts. Paid contract creation, upgrade, downgrade, cancellation, or payment failure must not close or decrement free-tier grants.
- Premium usage must not drain non-premium bucket grants, and non-premium usage must not drain premium bucket grants. Cross-bucket funding must be represented by product-level or account-level grants.
- Paid plan changes must be path-independent for a given effective time: a customer must not receive more total paid entitlement by stepping through intermediate self-serve plans than by moving directly from the current plan to the target plan. Immediate upgrades grant only the remaining positive entitlement delta between the target plan and the current plan; already-issued current-cycle paid grants remain available until their own period end instead of being replaced by a full prorated target-plan grant.
- A payment method on file is not overage consent. Free-tier orgs and paid orgs that enabled hard caps must not receive receivable funding legs for usage beyond authorized grants and prepaid balances.
- If usage without overage consent leaks through reservation or settlement, finalization must apply a deterministic automatic invoice adjustment before any customer charge is finalized. Automatic no-consent adjustments are capped at USD $0.99 per org per finalization run; exceeding that cap blocks finalization and forces operator review instead of billing the customer.
- Billing documents are immutable after issue. Corrections are explicit adjustment invoices or credit-note documents linked to the original artifact.
- Verself's stored document snapshot/rendered body is the canonical customer billing artifact. Stripe invoice PDFs and hosted invoice pages are provider/payment artifacts that must reconcile to Verself document totals but do not become billing truth.

## System roles

| System | Role |
|---|---|
| PostgreSQL | Source of truth for catalog tables, contracts, contract changes, phases, entitlement lines, entitlement periods, credit-grant metadata, billing cycles, billing-window metadata, finalizations, billing documents, invoice adjustments, payment methods, provider bindings, provider events, ledger command state, billing event rows, event-delivery queue rows, projection-delivery queue rows, schedule-defining timestamps, and reconciliation cursors. |
| River | Durable queue and scheduler runtime for provider-event application, contract-change execution, phase-boundary advancement, entitlement-period materialization, ledger command dispatch, ledger reconciliation, cycle rollover, finalization, Stripe invoice collection, document email delivery, retries, and periodic repair. |
| TigerBeetle | Operational financial ledger for credit balances, top-up deposits, recurring allowance deposits, receivables, settlements, refunds, expiry sweeps, corrections, showback transfers, and spend-cap enforcement. TigerBeetle is not the customer billing document artifact, not the request-path authorization engine, and not a substitute for PostgreSQL domain state. |
| ClickHouse | Append-only usage evidence plus billing event, metering, document, adjustment, and provider-event projections used for document preview, statements, dashboards, verification, and reconciliation. |
| Stripe | SetupIntents, PaymentMethods, Customer Portal, one-off invoice collection, payment intents, refunds, disputes, optional Stripe Tax, and hosted payment artifacts. Stripe Subscriptions are not part of the target domain model. |
| Mailbox service | Transactional delivery of Verself document emails from the stored billing document artifact. Stripe invoice emailing is disabled in the target Verself canonical-document path. |

## Design commitments and reversible choices

The load-bearing commitments are:

- Verself owns cadence, contract shape, phases, entitlements, billing documents, document numbering, overage consent, and finalization policy. Stripe Subscriptions must not become the self-serve contract state machine.
- PostgreSQL owns every schedule-defining timestamp and every durable state-machine row. A River job may be missing, duplicated, canceled, delayed, or reconstructed without changing billing truth.
- Cycle rollover and finalization are separate transition paths. Rollover opens the successor cycle before Stripe collection, document email, or payment completion so valid usage is not blocked by a slow external rail.
- Verself's issued billing document is canonical. Stripe invoice PDFs, hosted invoice pages, and payment intents are provider artifacts that must reconcile back to the Verself row.
- TigerBeetle account and transfer IDs are operational ledger identifiers only. They must not replace `contract_id`, `phase_id`, `cycle_id`, `period_id`, `grant_id`, `window_id`, `finalization_id`, `document_id`, `adjustment_id`, or `event_id` in public APIs, ClickHouse facts, or PostgreSQL domain relationships.
- Ledger command rows are durable side-effect state, not immutable billing facts. Immutable material facts live in `billing_events`; TigerBeetle transfers live in TigerBeetle; command rows bridge the two without becoming a third ledger.
- A payment method on file is not overage consent. Free-tier and hard-cap customers must not receive customer receivables for leaked no-consent usage.
- Enterprise agreements and self-serve Stripe-backed agreements use the same contract, phase, change, entitlement, cycle, finalization, document, and adjustment tables. Enterprise is a contract kind, phase kind, recurrence policy, collection method, and provider-binding choice, not a second billing engine.
- Self-serve catalog upgrades must be anti-arbitrage and path-independent at the same effective timestamp: charge the prorated positive price delta, preserve already-issued current-cycle paid grants until their own expiry, and issue only the prorated positive entitlement delta for the target phase.
- ClickHouse is evidence/read-model infrastructure. It must not perform billing transitions, authorize usage, issue documents, or decide ledger correctness.

The implementation choices that may vary without changing the target architecture are:

- Exact River job names, queue names, and job granularity, as long as jobs remain deterministic over domain identity.
- Whether a boundary uses bounded repair scanners, transactionally enqueues every one-row River job with its domain row, or does both for belt-and-suspenders recovery.
- Whether payment retry execution is delegated to Stripe automatic collection or owned by Verself dunning through `billing.payment.retry`, as long as Verself document payment state remains canonical.
- Dormant zero-usage zero-total cycle notification policy. The accounting behavior is fixed: every closed cycle has a finalization record and a billing document or internal statement artifact; customer email is suppressed only when policy says the cycle is dormant and not useful to the customer.
- ClickHouse projection table layout, as long as PostgreSQL remains authoritative for domain state, TigerBeetle remains authoritative for balances, and projections remain idempotent by deterministic identifiers.
- Exact numeric TigerBeetle account and transfer codes, as long as code meanings are stable after first production use and are stored in code/docs/registry together.

## Scheduling and queuing model

Scheduling and queuing are first-class billing verbs.

Scheduling is the act of declaring that work is due at or after a domain time. Billing schedules are encoded in PostgreSQL rows, not hidden inside River. Examples include `requested_effective_at`, `actual_effective_at`, `effective_start`, `effective_end`, `period_start`, `period_end`, `cycle.starts_at`, `cycle.ends_at`, `finalization_due_at`, `grace_until`, `next_materialize_at`, and `billing_event_delivery_queue.next_attempt_at`.

Queuing is the act of inserting a durable River job to execute one bounded transition derived from PostgreSQL state. River gives us retry, backoff, concurrency limits, delayed execution, periodic scans, OpenTelemetry spans, and transactional enqueueing. It does not replace the domain state machine.

This section describes the billing-service runtime. Product services that own
resources, such as sandbox-rental-service VMs or source-code-hosting-service git
object storage, may use their own River runtime or Temporal workflow to wake
renewable metering leases. The invariant is the same: the wakeup is an execution
handle, not policy truth. The product cursor and billing windows carry the
state; missed wakeups are repaired or marked `system_overdue`, not interpreted
as customer fault.

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
4. Write any immutable `billing_events` facts and corresponding
   `billing_event_delivery_queue` rows in the same transaction as the
   authoritative state change.
5. Enqueue follow-up River jobs in the same transaction when the transition creates new due work.
6. Write non-event ClickHouse projection delivery rows in the same transaction
   as the authoritative state change when a read model, such as metering, must
   be projected from mutable domain rows rather than from a `billing_events`
   fact.
7. Exit without side effects when the row is already terminal, superseded, not yet due, or already applied.

Reconciliation reconstructs missing River work from PostgreSQL state. If a job is missing but a row is due, reconciliation enqueues another deterministic job. If a job is duplicated, the worker sees already-applied state and exits.

Target billing job kinds:

- `billing.provider_event.apply`: apply one durable provider event.
- `billing.contract_change.apply`: execute one contract change when due.
- `billing.phase_boundary.advance`: activate, close, supersede, or void phases at due boundaries.
- `billing.entitlement_period.ensure`: materialize one deterministic entitlement period and its grant.
- `billing.entitlement_reconcile.org`: repair current and next entitlement periods for one org.
- `billing.cycle.rollover`: close a cycle for usage, open the successor cycle, and enqueue finalization for the closed cycle.
- `billing.finalization.run`: compute and advance one finalization step for a cycle, contract change, or correction subject; enforce overage consent, apply automatic adjustments, enforce the adjustment cap, allocate the document number, and issue or block the document.
- `billing.document.stripe_collect`: create/finalize the Stripe invoice for a Verself invoice document that needs Stripe collection.
- `billing.document.email`: send the stored Verself document email through mailbox-service.
- `billing.payment.retry`: run payment retry policy only when Verself owns dunning instead of delegating automatic collection to Stripe.
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
still converge through a PostgreSQL delivery queue plus periodic scanner; the
target shape is to enqueue one-row jobs in the same transaction as the domain
row.

Job uniqueness must be derived from domain identity, not random worker identity:

- Provider event jobs key by `(provider, provider_event_id)`.
- Contract change jobs key by `change_id`.
- Phase boundary jobs key by `(phase_id, boundary_kind, boundary_at)`.
- Entitlement period jobs key by `period_id` or the deterministic period source tuple.
- Cycle rollover jobs key by `(cycle_id, ends_at)`.
- Finalization jobs key by `finalization_id`.
- Stripe collection jobs key by `document_id`.
- Document email jobs key by `document_id`.
- Ledger command jobs key by `command_id`.
- Ledger reconciliation jobs key by `(reconcile_scope, org_id, product_id, reconcile_at_bucket)`.
- Grant expiry jobs key by `(grant_id, expires_at)`.
- Event delivery projection jobs key by `event_id`.
- Metering projection jobs key by `window_id`.

Billing-service must run its own River runtime against the billing PostgreSQL database. The sandbox-rental-service River runtime is a useful pattern, not a shared worker pool for billing. Billing workers need to enqueue jobs transactionally with billing domain rows, so their River tables and client belong in the billing database boundary. Ledger dispatch uses a dedicated billing ledger queue, not the generic ClickHouse delivery queue, because delayed financial posting and delayed analytic projection have different alerting and retry tolerances.

Billing correctness must not require River Pro-only workflow, sequence, durable-periodic-job, or dead-letter features. Domain tables carry the state machine, due timestamps, retry counters, and dead-letter status; River job rows are execution handles. Use River's transactional enqueueing, delayed execution, unique jobs, retries, and telemetry where available, and use bounded PostgreSQL repair scanners for any scheduling durability that must survive missing job rows.

River docs to keep near this design:

- River: <https://riverqueue.com/>

## TigerBeetle ledger model

TigerBeetle is the operational financial ledger for credit-unit balances. PostgreSQL is still the domain source of truth: it defines which org, product, contract, phase, cycle, grant, window, finalization, document, and adjustment exists and what state machine transition is allowed. TigerBeetle answers a narrower but load-bearing question: what balance exists, what amount is pending, and what immutable debit/credit movement has been accepted by the ledger.

TigerBeetle is not the legal billing document artifact, not a general-purpose customer-support database, and not a replacement for PostgreSQL constraints. It is also not a GAAP general ledger. The account names below deliberately track Verself operational credit flows: issued allowance, purchased credits, pending usage locks, settled usage value, customer receivables, absorbed no-consent usage, and internal showback. Finance exports can map those flows into GAAP accounts later, but the billing hot path must not wait for that mapping.

The supported TigerBeetle ledger for Verself credit units is ledger `1`, denominated in Verself ledger units. The USD scale is `100_000` ledger units per cent. Ledgers represent asset classes or materially different partitions; do not use one ledger per org. Tenant, product, grant source, and business-time identity belong in PostgreSQL and TigerBeetle `user_data_*` fields.

### Ledger identity

PostgreSQL domain IDs are deterministic text IDs. Examples: `grant_id`, `window_id`, `cycle_id`, `contract_id`, `phase_id`, `finalization_id`, `document_id`, and `event_id`. They remain the IDs exposed in APIs, OpenAPI specs, ClickHouse projections, support queries, and billing events.

TigerBeetle object IDs are separate 128-bit IDs:

- `account_id`: the TigerBeetle account ID for an operator account, grant account, or receivable account.
- `tb_transfer_id`: the TigerBeetle transfer ID for a deposit, reservation, settlement, void, receivable, payment-clearing, expiry, correction, or showback movement.
- `ledger_correlation_id`: a query correlation value stored in TigerBeetle `user_data_128` and in PostgreSQL. It may point at a window, grant, finalization, provider event, or ledger command, but it is not the domain ID.

TigerBeetle IDs must use the client `id()`/`types.ID()` time-based scheme and must be persisted before dispatch. Retries reuse the persisted ID list exactly. Do not derive TigerBeetle account or transfer IDs from SHA-256, FNV, ULID byte swaps, leg indexes, transfer kinds, or text domain IDs. Business idempotency comes from PostgreSQL unique keys and persisted command payloads; TigerBeetle idempotency comes from replaying the same TigerBeetle IDs.

`user_data_*` conventions:

- Grant accounts use `user_data_64 = org_id` and `user_data_32 = grant_source_code`.
- Customer receivable accounts use `user_data_64 = org_id`, `user_data_32 = receivable_scope_code`, and `user_data_128 = cycle, document, finalization, or org/product receivable correlation ID depending on account granularity.
- Reservation, settlement, and void transfers use `user_data_128 = window ledger_correlation_id`, `user_data_64 = business_clock_ms`, and `code = transfer reason`.
- Deposit, expiry, refund, and grant-correction transfers use `user_data_128 = deposit_transfer_id` or another persisted grant correlation ID.
- Finalization, no-consent adjustment, receivable-clearing, and payment-clearing transfers use `user_data_128 = finalization or document ledger_correlation_id`.

TigerBeetle preview query APIs may be used by operator tools and reconciliation, but the hot path should use IDs already stored in PostgreSQL and `LookupAccounts`/`LookupTransfers`. PostgreSQL remains the index for text domain IDs.

### Account taxonomy

Account codes are stable protocol values once production data exists. The exact numeric assignments can change before the first production cut, but the target account set is:

| Code name | Cardinality | Balance nature | Required flags | Purpose |
|---|---:|---|---|---|
| `operator_stripe_external` | singleton | external source/sink | `history` | Represents Stripe/the outside payment rail for operational balancing. It is not spendable customer balance. |
| `operator_stripe_holding` | singleton | credit-positive clearing | `debits_must_not_exceed_credits`, `history` | Credits posted payment events, then funds purchased credit deposits or clears receivables. It should not run negative. |
| `operator_contract_allowance_clearing` | singleton or per product | debit-positive allowance source | `credits_must_not_exceed_debits`, `history` | Funds recurring paid and enterprise contract allowance grants without tying entitlement continuity to Stripe collection timing. This is operational allowance funding, not a GAAP revenue account. |
| `operator_free_tier_expense` | singleton or per product | debit-positive expense | `credits_must_not_exceed_debits`, `history` | Funds universal free-tier grants. |
| `operator_promo_expense` | singleton or per campaign | debit-positive expense | `credits_must_not_exceed_debits`, `history` | Funds promo and goodwill grants that create spendable balance. |
| `operator_refund_expense` | singleton or refund-policy scoped | debit-positive expense | `credits_must_not_exceed_debits`, `history` | Funds restored or goodwill refund credits that create spendable customer balance. Cash refunds that remove previously purchased balance are separate correction/refund flows. |
| `operator_writeoff_expense` | singleton or per product | debit-positive expense | `credits_must_not_exceed_debits`, `history` | Records absorbed no-consent usage and other non-recoverable policy writeoffs for showback. |
| `operator_expired_credits` | singleton or per product | credit-positive sink | `history` | Receives unused customer grant balance swept at expiry. |
| `operator_revenue` | singleton or per product | credit-positive settlement | `debits_must_not_exceed_credits`, `history` | Receives posted usage value, recognized invoice value, and revenue-classified clearing movements. Do not treat this raw operational account as GAAP revenue without invoice classification. |
| `operator_receivable` | singleton or per product | credit-positive clearing | `debits_must_not_exceed_credits`, `history` | Counterparty for explicitly consented postpaid usage while the customer receivable is awaiting document issue or payment clearing. |
| `operator_revoked_contract_allowance` | singleton or per product | credit-positive sink | `debits_must_not_exceed_credits`, `history` | Receives unused recurring contract allowance swept after terminal non-payment, fraud, cancellation reversal, or operator correction. |
| `customer_grant` | one per `credit_grants` row | credit-positive spendable balance | `debits_must_not_exceed_credits`, `history` | Holds one scoped grant. Posted availability is `credits_posted - debits_posted`; request-path authorization subtracts active PostgreSQL window legs. |
| `customer_receivable` | per org/product/cycle or per document | debit-positive receivable | `credits_must_not_exceed_debits`, `history` | Accrues explicitly consented postpaid overage or document receivables and is cleared by payment or adjustment. |

Operator singleton account IDs are generated once and stored in `billing_ledger_accounts`. Customer grant and receivable account IDs are TigerBeetle time-based IDs generated when PostgreSQL creates the corresponding domain row. On boot and during reconciliation, billing verifies that registered operator accounts exist with the expected ledger, code, flags, and user data.

Dogfood/internal usage should not introduce a separate grant source. Internal contracts use the same `contract` grant source and `operator_contract_allowance_clearing` funding path as customer contracts; finalization nets the internal document to zero through explicit adjustments. This keeps internal traffic on the customer path without adding a shadow accounting model.

### Transfer taxonomy

Transfer codes are stable protocol values once production data exists. Target transfer meanings:

| Code name | Debit account | Credit account | Pending? | Purpose |
|---|---|---|---:|---|
| `stripe_payment_in` | `operator_stripe_external` | `operator_stripe_holding` | no | Records a successful Stripe payment in ledger units. |
| `grant_deposit` | source funding account | `customer_grant` | no | Creates spendable credit. The debit account identifies whether the grant came from free tier, contract allowance, top-up payment, promo, or refund policy. |
| `usage_spend` | `customer_grant` | `operator_revenue` | no | Posts final settled grant-backed usage. Admission holds are PostgreSQL authorization windows, not TigerBeetle pending transfers. |
| `document_receivable_accrue` | `customer_receivable` | `operator_receivable` | no | Accrues collectible document amounts that are not already represented by posted receivable reservation legs. |
| `receivable_revenue_recognition` | `operator_receivable` | `operator_revenue` | no | Reclassifies issued or collected receivable value into the operational revenue account when invoice policy says the amount is no longer merely pending collection. |
| `grant_expiry_sweep` | `customer_grant` | `operator_expired_credits` | no | Removes unused grant balance at `expires_at`. |
| `contract_allowance_revoke` | `customer_grant` | `operator_revoked_contract_allowance` | no | Removes unearned, unused contract allowance after terminal non-payment, fraud, or correction. |
| `refund_balance_remove` | `customer_grant` | refund/correction sink | no | Removes refundable unspent purchased balance; provider cash movement is tracked through Stripe/payment state. |
| `receivable_clear_payment` | `operator_stripe_holding` | `customer_receivable` | no | Clears a customer receivable after provider payment succeeds. |
| `no_consent_adjustment_showback` | `operator_writeoff_expense` | `operator_revenue` | no | Records absorbed leaked usage for marketing/policy showback without creating customer debt. The paired operational revenue credit preserves gross usage value while the expense debit identifies the operator-funded adjustment. |
| `ledger_correction` | correction-specific | correction-specific | no or pending | Corrects an erroneous prior movement using the same `user_data_128` correlation as the original operation. |

Settlement transfer IDs are persisted on `billing_window_ledger_legs.settlement_transfer_id`. The transfer ID never encodes leg order, operation kind, or source. Leg order is stored in PostgreSQL `billing_window_ledger_legs.leg_seq`, and operation meaning is stored in the TigerBeetle `code` plus PostgreSQL command metadata.

### Ledger commands

Every TigerBeetle side effect is represented by a durable PostgreSQL ledger command before dispatch. The command stores operation, aggregate identity, command state, exact TigerBeetle account/transfer IDs, expected batch payload, retry metadata, lease metadata, and terminal error/DLQ metadata. Command rows are operational state, not immutable billing facts; successful domain transitions still emit `billing_events`.

Ledger command lifecycle:

```text
pending -> in_progress -> posted
pending -> in_progress -> retryable_failed
retryable_failed -> in_progress -> posted
retryable_failed -> in_progress -> dead_letter
dead_letter -> pending (operator requeue, generation incremented)
```

`posted` means TigerBeetle accepted the command payload or reconciliation proved the expected accounts/transfers already exist with matching fields. It does not by itself prove the domain aggregate finished its follow-up transition; the command dispatcher must also complete the aggregate-specific transition in PostgreSQL. That second step is idempotent and repairable from `billing_ledger_commands(state = 'posted')` plus the aggregate row state.

Dispatch rules:

- Grant deposits dispatch synchronously before a grant becomes spendable. Request-path reservations are PostgreSQL authorization commits and do not create TigerBeetle commands.
- Settlement commands may be retried by River, but `billing_windows` must remain in non-terminal `settling` until TigerBeetle acknowledges the posted usage-spend transfers.
- Background commands such as expiry sweeps, receivable clearing, document showback, and corrections dispatch through River and are repaired by reconciliation.
- A command in `in_progress` whose lease expires is eligible for re-lease and replay with the same persisted payload. A process crash after lease acquisition must not require operator intervention.
- A command in `posted` whose aggregate row is still non-terminal is eligible for aggregate completion without replaying TigerBeetle. Examples: a posted `grant_deposit` with `credit_grants.ledger_posting_state != 'posted'` or a posted `settle_window` with `billing_windows.state = 'settling'`.
- A command retry must submit the exact same TigerBeetle IDs and linked-chain layout. Changing amount, account, code, flags, or ordering requires a new command generation and an explicit correction plan.
- Linked transfer chains must close inside a single TigerBeetle request. Large sweeps split into independently atomic commands before hitting TigerBeetle's batch limit.

### Balance reads

Customer balance reads load grant metadata from PostgreSQL and balances from TigerBeetle:

1. PostgreSQL selects open, active, in-scope `credit_grants` whose `ledger_posting_state = 'posted'` and whose time window contains the business clock.
2. Billing batches `LookupAccounts(account_id...)` for those grants.
3. For each `customer_grant` account, posted availability is `credits_posted - debits_posted`; TigerBeetle `debits_pending` must normally be zero.
4. Pending amount is the sum of PostgreSQL `billing_window_ledger_legs` attached to `billing_windows.state IN ('reserved', 'active', 'settling')`.
5. Customer-facing spent-by-source and spent-by-SKU is derived from settled `billing_window_ledger_legs` and metering projection, not from raw `debits_posted`, because expiry sweeps, refunds, and corrections also debit grant accounts.
6. The entitlements view groups the same grant metadata and TB balances into account/product/bucket/SKU slots. It must not synthesize a top-line balance that can be spent across incompatible scopes.

If TigerBeetle is unavailable, a balance read may return a dependency error or a clearly marked stale projection. It must not silently fall back to `credit_grants.amount - billing_windows.funding_legs`, because that would create a second financial truth path.

### Top-up flow

Purchased top-up credit is prepaid balance, not postpaid overage consent.

1. Stripe Checkout, PaymentIntent, or another provider collection path records the intended purchase with provider metadata containing `org_id`, `product_id`, purchase amount, and idempotency key.
2. The provider webhook is persisted in `billing_provider_events` before any balance mutation.
3. On `payment_intent.succeeded` or equivalent confirmed payment, PostgreSQL creates a deterministic `credit_grants(source = 'purchase')` row in `ledger_posting_state = 'pending'`, with `account_id`, `deposit_transfer_id`, and a durable `billing_ledger_commands(operation = 'grant_deposit')` envelope.
4. PostgreSQL emits `grant_issued` when the durable grant metadata row exists. This is a domain fact, not spendable-balance evidence.
5. The ledger command creates the customer grant account and linked transfers for `stripe_payment_in` and `grant_deposit`, unless the payment-in transfer was already posted by an earlier command for the same provider object.
6. Only after TigerBeetle acknowledges the command does PostgreSQL mark the grant `ledger_posting_state = 'posted'`, emit `grant_ledger_posted`, and make the balance visible to reserve and balance reads.
7. Duplicate provider events converge on the same purchase grant and same persisted TigerBeetle IDs.

A payment method on file does not participate in this flow unless the customer explicitly initiates a purchase or invoice-document collection. A free-tier customer may store a card without authorizing top-up purchases or metered overage invoices.

### Grant funding flow

Every spendable grant is a TigerBeetle `customer_grant` account credited by exactly one issuance command generation. The funding account depends on source:

- `free_tier`: debit `operator_free_tier_expense`, credit `customer_grant`.
- `contract`: debit `operator_contract_allowance_clearing`, credit `customer_grant`.
- `purchase`: debit `operator_stripe_holding`, credit `customer_grant`.
- `promo`: debit `operator_promo_expense`, credit `customer_grant`.
- `refund` or manually restored balance: debit `operator_refund_expense` or the policy-specific source account, credit `customer_grant`, and link back to the document/refund/correction artifact.

Free-tier and contract entitlement materialization may be triggered by org provisioning, reserve self-healing, or River recurrence work. All paths converge on the same deterministic PostgreSQL period/grant rows and the same persisted TigerBeetle command IDs.

### Reservation, settlement, and void flow

Reservation is the admission-control path:

1. PostgreSQL locks `(org_id, product_id)`.
2. Reserve self-heals current-period free-tier and already-active/grace contract grants from PostgreSQL facts.
3. PostgreSQL loads posted grant metadata and TigerBeetle balances, subtracts active PostgreSQL authorizations, then builds a strict waterfall: scope tightness first, source priority second, paid tier priority third, oldest grant first.
4. PostgreSQL inserts `billing_windows(state = 'reserved')`, `billing_window_ledger_legs(state = 'pending')`, and reserve events in one transaction.
5. If posted grant balances minus active PostgreSQL authorizations cannot fund the window, the caller receives no-capacity and must not start work.

Settlement is the completion path:

1. The caller reports billable usage evidence. For sandbox time windows, billable quantity is milliseconds of guest workload runtime, not VM launch/setup latency.
2. PostgreSQL computes billable charge units from the captured `rate_context`, trims the already-reserved legs in stored waterfall order, records `actual_quantity`, `billable_quantity`, `funding_legs`, `settled_at`, and no-consent writeoff evidence, and then moves the window to `settling`.
3. PostgreSQL creates direct usage-spend transfers for grant-backed posted legs and records released authorization amounts as `amount_voided`.
4. PostgreSQL stays in `settling` until TigerBeetle acknowledges the usage-spend command.
5. After acknowledgement, PostgreSQL marks legs `posted` or `voided`, marks the window `settled`, emits `billing_window_settled`, and enqueues metering projection.

Void is the safe-failure path:

1. PostgreSQL marks each pending leg `voided`, records `amount_voided = amount_reserved`, marks the window `voided`, and emits `billing_window_voided`.
2. No TigerBeetle command is created because no TigerBeetle transfer exists before settlement.

### Renewable metering leases

Long-lived metered products use renewable metering leases, not unbounded billing
windows and not a global cron sweep as the primary source of usage truth.

A renewable metering lease is a product-owned cursor that repeatedly opens short
billing windows for one resource. Examples:

- A running VM leases vCPU, memory, root disk, and durable disk capacity for the
  next `N` seconds.
- A repository leases git object storage bytes for the next `N` seconds.
- A long inference stream leases the next bounded token, duration, or compute
  segment.

The product service owns the resource cursor because it owns the resource state,
policy surface, and enforcement behavior. The billing service owns only the
authorization and settlement windows. A typical cursor contains:

- `resource_id`
- `org_id`
- `product_id`
- `meter_state` (`active`, `renewal_due`, `renewal_retrying`, `billing_denied`,
  `system_overdue`, `closed`)
- `current_window_id`
- `window_seq`
- `billed_through`
- `reserved_through`
- `current_allocation` (SKU quantities, such as GiB or vCPU count)
- `last_measurement_at`
- `last_denial_code`

Creation reserves and activates the first bounded window before the resource
becomes billable. The resource owner schedules a durable wakeup at
`reserved_through - renewal_buffer`. Renewal is one idempotent product command:

1. Lock the resource cursor.
2. Measure or load the current allocation.
3. Settle the current billing window through boundary `T`.
4. Reserve and activate the next billing window `[T, T + N)`.
5. Persist `current_window_id`, `window_seq`, `billed_through`,
   `reserved_through`, and the allocation used for the next window.
6. Schedule the next wakeup at `reserved_through - renewal_buffer`.

The successor boundary `T + N` must not cross a billing-cycle boundary. If a
resource would run across `cycle.ends_at`, the renewal command closes the current
window at the cycle boundary, opens the successor in the next cycle, and keeps
rate context and document attribution unambiguous.

If the allocation changes before the scheduled boundary, the resource owner may
run the same command early: settle the current interval through the mutation
time, reserve the next interval using the new allocation, and advance the cursor.
Storage mutations, repository deletion, git GC compaction, VM resize, VM stop,
and inference stream completion all use this early-close path.

Reserve-next-before-settle-current is not the default because it can double-hold
prepaid capacity during the overlap and create false no-capacity failures.
Resource owners should settle the old window and reserve the successor under the
same product cursor lock. If overlap is deliberately chosen for a low-latency
handoff, the product must treat overlap denial as a retryable platform condition
until the old window is settled or voided.

A missed wakeup does not imply nonpayment. Absence of renewal is platform
uncertainty. Only an explicit reserve denial from billing, after the owner
re-read policy and attempted renewal, may move the resource to `billing_denied`.
Products may alert, retry, or enter `system_overdue` when `now > renew_by` or
`now > reserved_through`, but they must not revoke access, freeze writes,
terminate VMs, or tombstone storage from that fact alone.

Enforcement belongs to the product service and must key off persisted policy
state, not queue state:

- `billing_denied` can stop new work, freeze writes after grace, or gracefully
  terminate a long-running VM according to product policy.
- `system_overdue` means the platform missed or cannot prove the renewal path;
  it is operator/actionable debt and should keep customer-visible access intact
  unless a separate safety limit is reached.
- `closed` settles or voids the final window and prevents further renewal.

Periodic repair remains useful, but only as a safety net: find cursors past
`renew_by`, `reserved_through`, or billing-cycle boundaries and enqueue the same
idempotent renewal command. Repair workers must converge on the cursor and
window sequence; they must not create an alternate metering path.

### Receivables and overage consent

Receivable-backed funding is allowed only when the customer has explicitly accepted the relevant overage model. A vaulted card is not enough.

For paid orgs with `overage_policy = 'bill_published_rate'`, reserve may add receivable legs after all eligible grants and prepaid balances are exhausted. Those legs are PostgreSQL authorization rows until settlement. Finalization then renders the receivable into customer-facing document lines, receivable ledger commands, and collection jobs.

Recurring base charges, upgrade price deltas, taxes, and other document amounts that are not grant-backed usage are represented by PostgreSQL document lines first. When they create a collectible amount due, finalization creates or updates the relevant `customer_receivable` ledger account and posts a receivable-accrual command. Payment success clears that receivable through `operator_stripe_holding`. If collection happens before issue in a hosted payment flow, the provider event is still recorded first, and finalization reconciles the already-collected provider amount to the issued Verself document.

For free-tier orgs and paid hard-cap orgs, reserve must not create receivable legs. If usage leaks through because of a race, stale reservation, retry, or bug, settlement records `writeoff_quantity` and `writeoff_charge_units` on the window but does not debit a customer receivable. Finalization converts that evidence into deterministic system-policy `invoice_adjustments` within the USD $0.99 cap. The optional TigerBeetle showback movement is `no_consent_adjustment_showback`: debit `operator_writeoff_expense`, credit `operator_revenue`, tagged to the finalization. That movement records gross usage value and operator-funded expense for analytics; it does not create spendable balance or customer debt.

If a receivable was created under consent and a later correction proves the customer did not authorize it, finalization must reverse or correct the receivable with a `ledger_correction` transfer correlated to the original window/document. Do not hide the correction by mutating the original transfer or silently dropping the document line.

### Showback and internal usage

Showback is an operator accounting projection over the same customer-facing machinery. It is not a separate usage path.

- Internal/dogfood usage uses internal contracts, contract grants, and the same reservation and settlement flow as customer usage. The billing document nets to zero through explicit adjustments rather than bypassing the billing service.
- Free-tier usage drains `operator_free_tier_expense`-funded grants and reports consumed value by product, bucket, SKU, org, and period.
- No-consent leaked usage creates `invoice_adjustments` with `cost_center`, `expense_category`, `recoverable = false`, and `affects_customer_balance = false`; optional TigerBeetle showback transfers mirror the adjustment total.
- Promo campaigns create promo grants from `operator_promo_expense`, not ad hoc balance edits.
- Expired credits sweep into `operator_expired_credits`, giving operators unused-allowance reporting without subtracting from a future customer balance.

The showback read model is built from PostgreSQL invoice adjustments, settled window legs, billing events, TigerBeetle reconciliation facts, and ClickHouse projections. Operators can answer “how much did free tier cost this month?” by grouping `invoice_adjustments`, free-tier grant settlement legs, and `operator_free_tier_expense` movements by product/bucket/SKU. Customers should not see internal showback clearing accounts.

### Reconciliation and trust mode

Billing reconciliation compares PostgreSQL ledger metadata against TigerBeetle state:

1. Operator account integrity: each registered operator account exists with expected ledger, code, flags, and `user_data`.
2. Grant account parity: every posted grant has a TigerBeetle account; every customer-grant account known to PostgreSQL maps to exactly one grant row.
3. Grant balance parity: posted grant deposits, posted settlement legs, expiry sweeps, refunds, and corrections reconcile to TigerBeetle account balances.
4. Window leg parity: every settled grant-backed `billing_window_ledger_legs` row has the expected TigerBeetle settlement transfer ID, and active `pending` legs exist only as PostgreSQL authorization holds.
5. Command drain health: no ledger command remains due, leased, or retryable beyond policy without alerting or DLQ transition.
6. Receivable parity: customer receivable balances reconcile to unfinalized/finalized receivable document lines, payments, adjustments, and corrections.
7. Accounting identity: all included account movements on a ledger net to zero under the configured account balance convention.

Drift writes a durable PostgreSQL drift row and a `billing_events` fact that projects to ClickHouse. Severe drift, such as missing operator accounts or an accounting identity violation, trips a billing ledger write guard until an operator resolves or explicitly waives the condition.

The single-node deployment can run TigerBeetle as one replica for development and early dogfooding, but that is not production-grade financial HA. Production financial trust requires independent TigerBeetle replicas according to the platform's multi-node topology. The service should expose a `ledger_trust_mode` in startup logs and verification output so operators can distinguish `development_single_replica` from `production_replicated`.

## Core billing model

The billing model is SKU-driven for usage, contract-driven for recurring entitlements, cycle-driven for backend billing periods, finalization-driven for accounting closure, and document-driven for customer-facing payment artifacts.

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
  -> TigerBeetle usage-spend transfers
  -> billing_finalizations
  -> billing_documents
  -> billing_document_line_items
  -> invoice_adjustments
  -> billing_events
  -> billing_event_delivery_queue
```

Provider ingress is modeled as:

```text
provider webhook/API event
  -> billing_provider_events
  -> River billing.provider_event.apply
  -> provider-neutral payment, document, finalization, contract, phase, dispute, or adjustment mutation
  -> billing_events
  -> billing_event_delivery_queue
```

A `contract` is the commercial agreement with an org. A `contract_phase` is the time-bounded version of that agreement: Hobby for a cycle, Pro after an immediate upgrade, or a bespoke enterprise package for a signed term. A `contract_entitlement_line` is the recurring promise inside the phase: which source funds which scope, how much, and on what recurrence. An `entitlement_period` materializes one recurrence window. A `credit_grant` is the PostgreSQL metadata row for a spendable TigerBeetle-backed balance issued from that period. The grant is not spendable until its ledger deposit command is posted.

A `billing_cycle` is the backend bookkeeping interval for an `(org, product)` billing timeline. Customer-facing copy should call this a billing period. A cycle has no financial meaning by itself. It only names the half-open interval `[starts_at, ends_at)` that finalization uses to select settled windows, recurring charges, entitlement periods, adjustments, payments, disputes, and contract phase overlaps. A cycle can contain multiple contract phases, and one contract phase can span multiple cycles.

A `billing_finalization` is the mutable accounting workflow for turning one accounting subject into an issued or blocked document. The dominant subject is a closed billing cycle; immediate self-serve activation charges, immediate upgrade delta charges, manual corrections, and credit-note flows can also use finalization with a non-cycle subject. Finalization decides whether the subject creates a collectible invoice, a customer-visible zero-dollar statement, an internal statement, a credit note, or a blocked operator state. It is the place where consent is verified, no-consent adjustments are capped, Stripe tax previews are reconciled, and immutable document creation becomes allowed.

A `billing_document` is the immutable issued artifact. The document can be a collectible invoice, a zero-dollar customer statement, an internal statement, a credit note, or an adjustment invoice. The document content is immutable after issue even though payment status and provider attachment fields may change later.

There is no separate `plan_bindings` concept. Contract phases are the plan/policy intervals. The funder uses the contract phase active at reservation time to capture rate and entitlement context into `billing_windows` and `billing_window_ledger_legs`; document generation uses that captured context, not a live phase lookup, so retroactive plan edits cannot rewrite history.

The supported entitlement scopes form a tightest-to-widest funnel: `sku` -> `bucket` -> `product` -> `account`. Entitlements are non-overlapping within a layer:

- `sku`: one specific SKU within one product bucket
- `bucket`: one product bucket, fed by any of its SKUs
- `product`: any bucket for one product
- `account`: any product bucket in the org

The `bucket` layer is the SKU-lane layer. If premium NVMe and non-premium disk need separate allowance behavior, they must be separate buckets and the corresponding SKUs must map to the correct bucket. Product-level or account-level grants are the only supported way to fund multiple buckets.

Free tier is not a contract and not a plan. It is a universal scheduled entitlement policy that grants monthly `source = 'free_tier'` balances to every org regardless of which paid contract the org has. Upgrading from free usage to any paid contract must not remove the current month's free-tier grants; the reserve waterfall consumes matching free-tier grants before recurring contract grants or purchased credit grants.

Free tier is also not implicit postpaid consent. A free-tier org may keep a payment method on file for future paid activation or credit purchases without authorizing metered overage invoices. If a free-tier org exhausts its free-tier grants and any explicit purchased or promo balances, admission must stop. If a race, stale read, retry, delayed settlement, or worker bug permits usage beyond that point, the excess is absorbed by the operator through an automatic invoice adjustment during finalization; it is not converted into debt, a rollover deduction, or a future customer balance.

The default invariant is one active commercial contract per org/product unless stacking groups are explicitly modeled as first-class contract groups. Multiple visible phases may exist during transitions, but only non-overlapping active/grace phase intervals for the same org, product, scope type, and scope target may fund reservations. Free-tier policies are outside that commercial contract constraint.

## PostgreSQL catalog and state

This section describes the billing schema. Recurring customer agreements are modeled as provider-neutral contracts; Stripe Subscriptions, `subscription_contracts`, `subscription` source values, and `/subscriptions` API names are not part of the implementation surface. PostgreSQL stores deterministic domain rows and ledger command state. TigerBeetle stores balance-changing account and transfer facts. Do not collapse those two identities into one primary key scheme.

### `tbid` domain

PostgreSQL columns that store TigerBeetle IDs should use a domain equivalent to:

```sql
CREATE DOMAIN tbid AS BYTEA CHECK (octet_length(VALUE) = 16);
```

Use `tbid` for TigerBeetle account IDs, transfer IDs, and ledger correlation IDs. Do not use it for `grant_id`, `window_id`, `contract_id`, `cycle_id`, `finalization_id`, `document_id`, `command_id`, or `event_id`.

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

Named entitlement lanes. These are the buckets free-tier and recurring contract lines fund, and document previews group by.

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

A plan is a reusable packaging template, not an active customer agreement. Self-serve flows use plan price IDs only to map document line items to optional Stripe Prices. Enterprise contracts may reference a plan for display and rate-card inheritance, but bespoke enterprise terms must be represented by contract phases and entitlement lines.

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

This table is the product/SKU rate card used for reservation cost calculations and document line items. Rate context is copied into each `billing_windows.rate_context` so documents do not depend on mutable catalog rows.

### `entitlement_policies`

Reusable free-tier, contract, promo, refund, and purchased-credit policy definitions.

Key fields:

- `policy_id`
- `product_id`
- `source` (`free_tier`, `contract`, `purchase`, `promo`, `refund`)
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

Optional external provider identity for a contract, payment method, billing document, payment intent, or other provider-backed object.

Key fields:

- `binding_id`
- `aggregate_type` (`contract`, `payment_method`, `document`, `finalization`, `payment_intent`, `customer`)
- `aggregate_id`
- `contract_id`
- `provider` (`stripe`, `manual`)
- `provider_object_type` (`customer`, `payment_method`, `invoice`, `payment_intent`, `manual_contract`, or provider-specific object type)
- `provider_object_id`
- `provider_customer_id`
- `sync_state` (`none`, `pending`, `synced`, `error`)
- `metadata`

A Stripe self-serve contract usually has a customer-level binding and Stripe invoice/payment-intent references on document/payment rows. It must not have a Stripe subscription binding in the target architecture. An enterprise contract may have no binding, a `manual_contract` binding, or a CRM/ERP binding without changing entitlement issuance.

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
- `finalization_id`
- `document_id`
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

There must be a unique key on `(provider, provider_event_id)`. Webhook ingress writes this row before applying the event and transactionally enqueues `billing.provider_event.apply`. Duplicate provider deliveries converge on the same row and same River job identity. Out-of-order events are not applied by arrival order; the worker translates each event into a provider-neutral mutation and lets the payment/document/finalization/contract/phase state machines decide whether it is still relevant.

This table is the primary fault-injection seam for Stripe. Tests exercise the provider-event boundary with delayed, duplicated, missing, failed, terminal, malformed, and out-of-order events, then verify PostgreSQL state plus `verself.billing_events` projection.

### `billing_payment_disputes`

Provider-neutral dispute and chargeback state linked to the document/payment/contract it can affect.

Key fields:

- `dispute_id`
- `provider`
- `provider_dispute_id`
- `provider_charge_id`
- `provider_payment_intent_id`
- `provider_invoice_id`
- `document_id`
- `finalization_id`
- `contract_id`
- `phase_id`
- `org_id`
- `product_id`
- `state` (`opened`, `won`, `lost`, `operator_resolved`)
- `provider_status`
- `amount_units`
- `fee_units`
- `currency`
- `opened_event_id`
- `closed_event_id`
- `opened_at`
- `closed_at`
- `action_taken` (`none`, `access_suspended`, `allowance_revoked`, `manual_review`)
- `metadata`
- `created_at`
- `updated_at`

Disputes are not modeled as ordinary refunds because provider dispute creation can remove funds immediately while the final outcome can still become won or lost. The dispute row is the durable state machine that links Stripe `charge.dispute.created` and `charge.dispute.closed` events to Verself contract access, document payment state, allowance revocation, chargeback-loss accounting, and user/operator notifications.

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

For immediate paid upgrades, the change row stores the proration basis instead of recomputing it from mutable plan state later. `price_delta_units` is the positive prorated recurring-charge delta in Verself ledger units before tax. `entitlement_delta_mode = 'positive_delta'` means the first target-phase entitlement period issues only `max(target_line_amount - current_line_amount, 0) * proration_fraction`, while already-issued current-cycle grants from the prior paid phase remain spendable until their own `expires_at`. This prevents a customer from receiving more entitlement by walking through intermediate plans at the same effective timestamp.

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

For self-serve catalog phases, lines normally use `recurrence_anchor_kind = 'billing_cycle'`, meaning the Verself billing cycle defines the entitlement window. Enterprise phases normally use `calendar_month_day` and a timezone so the contract can renew on a fixed calendar day regardless of signup anniversary. `anniversary` anchors are available for non-cycle contract terms that renew from service start.

Lines are copied from plan policies for catalog-plan phases and authored directly for bespoke phases. This keeps upgrades/downgrades and enterprise amendments on the same state machine.

Catalog-plan lines carry the full target-plan entitlement amount. They are not rewritten to a prorated amount when an upgrade happens mid-cycle. The first target-phase entitlement period can be computed as an upgrade delta by the associated `contract_changes` row; subsequent periods materialize the full line amount.

Recurring entitlement scheduling is not a timer hidden in River. The recurrence config and cursor live in PostgreSQL. River jobs are generated from that state and can be reconstructed by reconciliation.

### `billing_cycles`

Bookkeeping intervals for backend billing periods. Keep `billing_cycles` as the code/table name; expose this concept to customers as a billing period.

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
- `status` (`open`, `closing`, `closed_for_usage`, `finalized`, `voided`)
- `closed_reason` (`scheduled_period_end`, `free_to_paid_activation`, `cadence_change`, `enterprise_amendment`, `payment_dispute`, `contract_revocation`, `operator_correction`)
- `finalization_due_at`
- `active_finalization_id`
- `successor_cycle_id`
- `closed_by_event_id`
- `closed_for_usage_at`
- `finalized_at`
- `created_at`
- `updated_at`

Required invariants:

- Unique `(org_id, product_id, anchor_at, cycle_seq)`.
- At most one `open` or `closing` cycle per `(org_id, product_id)`.
- No overlapping non-voided cycle intervals per `(org_id, product_id)`, preferably enforced with a PostgreSQL exclusion constraint over `tstzrange(starts_at, ends_at, '[)')`.
- A cycle has no financial meaning on its own. It is a named interval used by finalization, document generation, and reporting.
- Free-tier orgs have cycles. They are not absent from cycle accounting just because no paid contract exists.
- A cycle may be finalized into a customer-visible invoice, customer-visible statement, internal statement, credit-note flow, or blocked finalization; the finalization row owns that state, not the cycle.

The only normal path that creates a successor cycle is `openNextCycle(predecessor)`. Scheduled rollover and immediate boundary changes both call the same transition. Rollover closes the predecessor for usage and opens the successor before any Stripe call, document email, or payment collection.

### `billing_finalizations`

Mutable accounting workflow for issuing or blocking the document for one accounting subject.

Key fields:

- `finalization_id`
- `subject_type` (`cycle`, `contract_change`, `manual_adjustment`, `credit_note`, `provider_correction`)
- `subject_id`
- `cycle_id` (required for cycle subjects; nullable or attribution-only for non-cycle subjects)
- `contract_change_id`
- `org_id`
- `product_id`
- `reason` (`scheduled_period_end`, `free_to_paid_activation`, `immediate_upgrade_delta`, `contract_change_collection`, `cadence_change`, `enterprise_amendment`, `payment_dispute`, `contract_revocation`, `manual_adjustment`, `credit_note`, `provider_correction`, `operator_correction`)
- `trigger_event_id`
- `state` (`pending`, `collecting_facts`, `awaiting_ledger_commands`, `awaiting_provider_preview`, `ready_to_issue`, `blocked`, `issued`, `collection_pending`, `paid`, `payment_failed`, `closed`, `voided`)
- `document_id`
- `document_kind` (`invoice`, `statement`, `internal_statement`, `credit_note`, `adjustment_invoice`)
- `customer_visible`
- `notification_policy` (`none`, `in_app`, `email`, `email_and_in_app`)
- `has_usage`
- `has_financial_activity`
- `input_fingerprint`
- `subtotal_units`
- `adjustment_units`
- `tax_units`
- `total_due_units`
- `no_consent_adjustment_units`
- `blocked_reason`
- `blocked_at`
- `state_version`
- `started_at`
- `issued_at`
- `closed_at`
- `created_at`
- `updated_at`

The finalization row is the mutable state machine. It is not an outbox and not an immutable fact stream. Every externally meaningful finalization transition writes `billing_events` rows in the same PostgreSQL transaction; `billing_event_delivery_queue` projects those facts. Any TigerBeetle movement created by finalization is represented by `billing_ledger_commands`. Stripe collection remains a River/provider command keyed by the issued document identity and Stripe idempotency key. Do not add a finalization-specific outbox table.

For `subject_type = 'cycle'`, `cycle_id` is required and the subject interval is the closed cycle. For `subject_type = 'contract_change'`, `contract_change_id` is required and `cycle_id` may point at the cycle whose commercial context the change belongs to, but the finalization must not close or finalize that cycle. This is how immediate activation invoices and immediate upgrade-delta invoices share the same document, Stripe preview, tax, numbering, and payment logic without pretending a mid-cycle upgrade is a cycle finalization.

`has_usage` is true when the period contains settled customer usage windows. `has_financial_activity` is true when the period contains usage, recurring charges, entitlement grants that should be disclosed, top-up purchases, payments, refunds, disputes, adjustments, receivable activity, or document corrections. Paid contract overlap makes a period financially active even when metered usage is zero.

Customer visibility is deterministic:

- Free periods are customer-visible only when they contain settled usage or customer-visible financial activity such as purchases, refunds, disputes, or adjustments. Free-tier grant issuance by itself does not make an empty free period customer-visible.
- Paid periods are customer-visible when a paid contract phase overlaps the cycle, even if usage is zero, because the contract/entitlement/payment history is billing activity.
- Contract-change finalizations are customer-visible when they collect payment, change customer-visible entitlements, issue a credit note, or require customer consent. Purely internal operator corrections can issue `internal_statement` documents.
- Internal/dogfood periods can issue `internal_statement` documents and suppress customer notification while still preserving the same accounting trail.
- Blocked finalizations are operator-visible and must not become customer debt.

### `entitlement_periods`

Durable period-level projection from free-tier policies or contract entitlement lines. Period rows are the bridge between scheduled entitlement truth and grant issuance.

Key fields:

- `period_id`
- `org_id`
- `product_id`
- `cycle_id` (required for cycle documents; nullable or attribution-only for non-cycle documents)
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
- `account_id`
- `deposit_transfer_id`
- `ledger_posting_state` (`pending`, `in_progress`, `posted`, `retryable_failed`, `dead_letter`, `failed`)
- `ledger_posted_at`
- `ledger_last_error`

Source-funded grants are deterministic over `org_id`, `source`, scope, and `source_reference_id` so retries converge on the same PostgreSQL row without making Stripe the only reference namespace. The TigerBeetle account ID and deposit transfer ID are not deterministic hashes; they are time-based TigerBeetle IDs allocated once, persisted on or through the grant's ledger command, and reused on retry.

Free-tier and contract grants carry `entitlement_period_id`, `policy_version`, `period_start`, `period_end`, `starts_at`, and `expires_at`. A paid phase transition must distinguish unearned future grants from already-earned current-cycle carryforward. Terminal or superseding phase events close future-period, voided, fraudulent, or otherwise unearned grant rows for the affected phase, but a normal immediate upgrade leaves already-issued current-cycle grants open until their own expiry and issues only a target-phase delta grant for the rest of the cycle.

Only `ledger_posting_state = 'posted'` grants may fund reservations or appear as available balance. Pending, retryable-failed, or dead-lettered grants may remain visible to operators, but customer-facing entitlement views must label or exclude them rather than treating `amount` as available. If a deposit command fails after the PostgreSQL grant row exists, reconciliation either completes the same command with the persisted TigerBeetle IDs or moves the grant to an operator-visible failure state; it must not mint a second grant for the same source reference.

Grant consumption is strict and step-function-shaped: scope tightness first, source priority second, recurring-contract tier priority third. Settlement trims the already-reserved funding legs in that order; it must never scale every source proportionally. The implementation lives in `internal/billing/grants.go` and is intentionally different from the entitlements view's display order, which is account-to-SKU because the customer is asking "what coverage do I have" rather than "what drains first":

```go
GrantScopeFundingOrder  = []GrantScopeType{ GrantScopeSKU, GrantScopeBucket, GrantScopeProduct, GrantScopeAccount }
GrantSourceFundingOrder = []GrantSourceType{ SourceFreeTier, SourceContract, SourcePromo, SourceRefund, SourcePurchase, SourceReceivable }
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
- `source_fingerprint`
- `window_seq`
- `state` (`reserved`, `active`, `settling`, `settled`, `voided`)
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
- `ledger_correlation_id`
- `window_start`
- `expires_at`
- `settled_at` (allowed while `state IN ('settling', 'settled')`; a `settling` row has computed settlement facts but not terminal TigerBeetle acknowledgement)
- `metering_projected_at`
- `last_projection_error`

`billing_windows` are request-path authorization locks, not queued jobs. A `reserved` window means PostgreSQL committed the admission hold after loading posted TigerBeetle grant balances and subtracting active PostgreSQL authorizations. A `settling` window has durable settlement math and a posted-or-pending ledger command, but it is not terminal and must not be projected as settled customer usage. A `settled` window means TigerBeetle accepted the final usage-spend command or reconciliation proved the transfer exists. A `voided` window is PostgreSQL-only authorization release and creates no TigerBeetle movement.

Sandbox time windows use AWS-Lambda-style millisecond quantities. `reserved_quantity`, `actual_quantity`, `billable_quantity`, and `writeoff_quantity` are milliseconds for `reservation_shape = 'time'`; the SKU quantity unit makes the resource dimension explicit (`vCPU-ms`, `GiB-ms`). VM launch and environment setup time are not billable. The billed duration comes from the billable guest `run` phase duration when available and falls back to host-side billable phase start/end evidence only when the guest duration is absent.

For renewable metering leases, the billing window is a bounded lease interval,
not the resource lifecycle. The product cursor, such as a VM lease cursor or
repository storage cursor, records `current_window_id`, `window_seq`,
`billed_through`, `reserved_through`, and allocation. Billing windows remain
cycle-bounded accounting facts with captured rate context; a resource that lives
for weeks produces many short settled windows instead of one long reservation.

Reserve accepts a caller-provided `window_millis` quantity. `window_millis = 0`
uses the billing-service default window; nonzero values choose the reserved
millisecond quantity for authorization, `reserved_quantity`, expiry, returned
reservation, events, and projection. This keeps VM execution admission policy
separate from storage or routine sweep cadence. Renewable products should choose
window lengths that cap platform liability and keep policy enforcement fresh;
they should not use month-scale windows to approximate storage-hours or VM
uptime.

`source_fingerprint` is the idempotency key for source-addressed replay. It is
derived from org, product, source type, source ref, sequence, quantity, and
allocation. Repeating the same source request returns the existing
`reserved`/`active`/`settling`/`settled` window. Reusing the same source with a
different quantity or allocation is a conflict because it would otherwise blur
already-settled usage.

For renewable metering leases, `source_ref` identifies the product resource and
`window_seq` identifies the lease interval. Allocation changes, cycle boundaries,
and final close events advance the sequence; they do not mutate a prior sequence
with a different allocation.

Window charge math rounds only after all usage dimensions are multiplied:
`ceil(allocation * quantity * unit_rate)`. Rounding `allocation * unit_rate`
before multiplying by milliseconds overstates tiny fractional GiB quantities and
is not allowed for measured at-rest bytes.

`funding_legs` may be retained as a denormalized snapshot for API and ClickHouse projection, but it is not the authoritative financial leg table. `billing_window_ledger_legs` owns per-leg settlement transfer IDs, source attribution, component SKU attribution, authorization amounts, posted amounts, released amounts, and leg state.

`writeoff_quantity` and `writeoff_charge_units` are settlement evidence, not a customer credit. They capture usage that was admitted but cannot be billed because it exceeded the reserved quantity or the org's overage-consent policy. Finalization turns that evidence into deterministic `invoice_adjustments` rows when the window would otherwise create unauthorized receivable units. Optional TigerBeetle showback transfers mirror those adjustments for operator reporting without creating customer debt.

### `billing_window_ledger_legs`

Normalized authorization, settlement, release, and source-attribution legs for a billing window.

Key fields:

- `window_id`
- `leg_seq`
- `org_id`
- `product_id`
- `cycle_id`
- `source` (`free_tier`, `contract`, `purchase`, `promo`, `refund`, `receivable`)
- `grant_id`
- `customer_receivable_account_id`
- `grant_account_id`
- `settlement_transfer_id`
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
- `state` (`pending`, `posted`, `voided`)
- `created_at`
- `updated_at`

The primary key is `(window_id, leg_seq)`. Leg order is the exact funding waterfall order used for settlement trimming and document attribution. `settlement_transfer_id` is a TigerBeetle transfer ID, not a domain ID. `source = 'receivable'` leaves `grant_id` empty; grant-backed sources require `grant_id` and `grant_account_id`. There is no `internal` source because dogfood usage is funded by internal contracts using the normal `contract` source and then netted by invoice adjustment.

This table is the indexed source for explaining how a window drained balances. TigerBeetle is still the balance authority, but PostgreSQL stores the domain reason for each transfer so documents, support tools, and ClickHouse projections do not need to depend on preview TigerBeetle query APIs.

### `billing_ledger_accounts`

Registry for operator accounts and other non-grant TigerBeetle accounts that must exist before ledger commands can dispatch.

Key fields:

- `account_key`
- `account_id`
- `ledger`
- `code`
- `flags`
- `account_kind`
- `description`
- `metadata`
- `created_at`
- `updated_at`

Operator accounts are bootstrapped idempotently and verified by reconciliation. A mismatch in account code, ledger, flags, or `user_data` is severe drift; billing must refuse new ledger writes until the operator resolves it. Customer grant accounts do not live only in this registry; they are referenced from `credit_grants`.

### `billing_ledger_commands`

Durable PostgreSQL command state for TigerBeetle side effects.

Key fields:

- `command_id`
- `operation` (`grant_deposit`, `settle_window`, `document_receivable_accrue`, `expire_grant`, `revoke_contract_allowance`, `refund_balance_remove`, `receivable_clear_payment`, `receivable_revenue_recognition`, `adjustment_showback`, `ledger_correction`)
- `aggregate_type`
- `aggregate_id`
- `org_id`
- `product_id`
- `idempotency_key`
- `generation`
- `state` (`pending`, `in_progress`, `posted`, `retryable_failed`, `dead_letter`)
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
- `command_id`
- `account_id`
- `tb_transfer_id`
- `expected`
- `observed`
- `state` (`open`, `acknowledged`, `resolved`, `waived`)
- `detected_at`
- `resolved_at`
- `operator_note`

Critical drift emits a `billing_events` fact and trips the ledger write guard. Examples include missing operator accounts, account-flag mismatch, grant account missing for a posted grant, a posted PostgreSQL leg without a TigerBeetle transfer, or a ledger accounting identity violation.

### `billing_documents`

Immutable issued billing artifacts.

Key fields:

- `document_id`
- `document_number`
- `document_kind` (`invoice`, `statement`, `internal_statement`, `credit_note`, `adjustment_invoice`)
- `finalization_id`
- `subject_type`
- `subject_id`
- `org_id`
- `product_id`
- `cycle_id`
- `status` (`issued`, `voided`)
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
- `document_snapshot_json`
- `rendered_html`
- `content_hash`
- `stripe_invoice_id`
- `stripe_hosted_invoice_url`
- `stripe_invoice_pdf_url`
- `stripe_payment_intent_id`
- `resend_message_id`
- `voided_by_document_id`
- `created_at`
- `updated_at`

Document generation builds gross usage lines, recurring charge lines, tax lines from configured tax policy, funding splits, and adjustment candidates from PostgreSQL domain rows, normalized ledger legs, and TigerBeetle-backed command state. Finalization is the state-machine boundary that proves every customer-chargeable receivable unit is backed by explicit consent. If a tax provider can change the customer amount due, tax calculation is part of finalization and must complete before the Verself document is issued.

A Verself document is immutable after issue. Corrections create a new adjustment invoice or credit-note document linked through `voided_by_document_id` or an explicit credit-note relation. The original remains queryable for audit.

`document_snapshot_json` is the canonical rendering input. `rendered_html` is the exact body emailed to the customer, rendered as PDF, or shown in the Verself console. `content_hash` lets operators prove what was issued without recomputing from mutable catalog or policy tables. Verself-rendered HTML/PDF is the canonical customer artifact; Stripe-hosted pages and PDFs are provider/payment artifacts that must reconcile to the same total but do not replace the Verself document.

Finalization must:

1. Re-read the org/product billing posture from PostgreSQL.
2. Recompute candidate receivable units from settled windows, captured rate context, recurring charges, grant funding, and explicit purchases.
3. Verify whether the org authorized postpaid overage for the product and period.
4. Apply deterministic automatic credit adjustments for no-consent receivable units when the adjustment total is within the cap.
5. Block finalization when no-consent automatic adjustments would exceed the cap.
6. Resolve tax and convert ledger units to Stripe invoice cents with an explicit rounding/residual policy when Stripe collection is needed.
7. Set `customer_visible`, `document_kind`, and `notification_policy` from deterministic period activity.
8. Allocate a document number only when the document artifact is ready to issue.
9. Insert the immutable document artifact and line items.
10. Emit billing event facts for created adjustments, issued documents, finalized documents, or blocked finalizations.
11. Enqueue Stripe collection and document email jobs when applicable.

If Stripe Tax is enabled, the Stripe draft/tax verification step happens while the Verself finalization is still in `awaiting_provider_preview`. The local document must not move to `issued` until tax units and the provider-facing cent total have been reconciled into `document_snapshot_json`.

### `billing_document_line_items`

Immutable line items belonging to a `billing_documents` artifact.

Key fields:

- `line_item_id`
- `document_id`
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

Document-scoped credits or debits that affect amount due without creating spendable customer balance.

Key fields:

- `adjustment_id`
- `document_id`
- `finalization_id`
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

Automatic no-consent adjustments use `adjustment_source = 'system_policy'`, `adjustment_type = 'credit'`, `customer_visible = false`, `recoverable = false`, and `affects_customer_balance = false`. They are deterministic over `(finalization_id, org_id, product_id, window_id, sku_id, reason_code, policy_version)` so finalization retries cannot double-credit the document.

The default automatic no-consent adjustment cap is USD $0.99 per org per finalization run. Because billing cycles are scoped to `(org, product)`, the normal case is one product; if statement-level finalization batches multiple products, the cap is shared across the batch. In the USD ledger scale, that is `99 * 100_000` ledger units. This cap is a circuit breaker, not overage consent. If the cap would be exceeded, finalization enters a blocked state, emits `billing_finalization_blocked`, blocks further no-consent execution for the affected org/product, and waits for operator resolution. Operator resolution may create an explicit manual adjustment or credit-note document, but must not create a customer receivable unless the customer grants overage consent.

Invoice adjustments do not create spendable customer balance. When an adjustment needs operator showback in TigerBeetle, finalization creates a separate ledger command such as `no_consent_adjustment_showback`, correlated to the finalization and adjustment row. That transfer records internal expense/clearing movement only; the adjustment remains the customer-facing and policy-enforcing artifact.

### `document_number_allocators`

Gapless customer-facing document number allocation.

Key fields:

- `issuer_id`
- `year`
- `prefix`
- `next_number`
- `updated_at`

The allocator row is locked with `SELECT ... FOR UPDATE` and incremented in the same transaction that inserts the issued document artifact. PostgreSQL sequences are not acceptable for gapless document numbers because sequence values can be lost on rollback. If any external side effect fails after number allocation, the document artifact remains present and transitions to `voided` or an associated finalization/payment failure state; it is not deleted.

The target number format is `VS-{year}-{seq}` unless the operator configures a different issuer prefix. Scope allocation by `(issuer_id, year)` avoids a global hot row and avoids leaking total document volume across years or issuers.

### `billing_document_previews`

Short-lived backend preview cache for current or closed-cycle document previews.

Key fields:

- `preview_id`
- `subject_type`
- `subject_id`
- `cycle_id`
- `finalization_id`
- `org_id`
- `product_id`
- `document_kind`
- `input_fingerprint`
- `state` (`built`, `provider_preview_pending`, `provider_preview_verified`, `failed`, `expired`)
- `verself_snapshot_json`
- `stripe_preview_response_json`
- `stripe_preview_expires_at`
- `subtotal_units`
- `adjustment_units`
- `tax_units`
- `total_due_units`
- `created_by_actor_id`
- `created_at`
- `expires_at`
- `last_error`

Preview rows are not document artifacts, not finalization facts, and not payment objects. They exist so current-period preview requests can be repeated without recomputing or re-calling Stripe when the inputs have not changed. The `input_fingerprint` covers cycle, windows, ledger legs, contract phases, adjustments, tax settings, customer tax identity, and provider-preview parameters. Any changed input creates a new preview.

Stripe preview responses must be treated as tax/total verification evidence only. Stripe preview invoices are not payable Verself documents, must not allocate Verself document numbers, and must not become the canonical PDF source.

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

Grant materialization writes the PostgreSQL grant row, ledger IDs, and deposit command before dispatch. `grant_issued` means the durable grant metadata exists; it must never be interpreted as available balance. `grant_ledger_posted` means TigerBeetle accepted the account and deposit transfer, PostgreSQL marked `ledger_posting_state = 'posted'`, and the grant can fund reservations if its scope and time window match. Contract creation, provider event ingestion, payment-method changes, contract changes, phase transitions, entitlement materialization, cycle rollover, billing-window reservation/settlement decisions, finalization transitions, invoice adjustments, Stripe collection updates, ledger command outcomes, and document email delivery also write billing events in the same transaction as their authoritative PostgreSQL state change.

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

ClickHouse is evidence/read-model infrastructure; PostgreSQL remains authoritative for billing domain state and TigerBeetle remains authoritative for balances and ledger movements.

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
- `billing_finalization_started`
- `billing_finalization_blocked`
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
- `billing_document_issued`
- `billing_statement_issued`
- `billing_document_voided`
- `billing_document_preview_created`
- `billing_document_preview_stripe_verified`
- `stripe_invoice_collection_started`
- `stripe_invoice_paid`
- `stripe_invoice_payment_failed`
- `payment_dispute_opened`
- `payment_dispute_won`
- `payment_dispute_lost`
- `contract_access_suspended`
- `contract_allowance_revoked`
- `billing_document_email_sent`

### Billing projection delivery

Non-event ClickHouse read models use the same transactional outbox discipline as
`billing_event_delivery_queue`, but their source identity is the domain row that
owns the projection. Billing metering is the first required non-event
projection:

- source kind: `billing_window`
- source id: `window_id`
- sink: `clickhouse.metering`
- generation: monotonic integer used when an operator intentionally reprojects
  a corrected row

The delivery row must be inserted in the same PostgreSQL transaction that makes
the source row projectable. For metering, that is the transition of
`billing_windows.state` to `settled` after ledger settlement posting is
acknowledged or reconciled. A marker such as `metering_projected_at` is useful as
a cached status field, but it is not the durable outbox and must not be the only
record that projection work is due.

Projection workers lease due rows, hydrate the current authoritative PostgreSQL
state, insert an idempotent ClickHouse row, and mark the delivery succeeded. If
the ClickHouse insert succeeds but the PostgreSQL success mark fails, replay is
allowed and expected; the ClickHouse table and all customer/operator queries must
deduplicate by the deterministic source identity. For `verself.metering`,
that identity is `window_id`.

Implementation may generalize the event queue into a typed projection delivery
table or add a sibling `billing_projection_delivery_queue`. The product invariant
is the same either way: no ClickHouse billing projection is driven only by an
in-memory River job, a marker timestamp, or a best-effort direct insert.

## State machines

There is not a separate state machine for enterprise contracts. The same state machines apply to every recurring paid agreement; only phase kind, recurrence anchor, collection method, and provider binding differ.

Each state transition has an execution source:

- API transition: a user or internal caller requests a contract, change, purchase, cancellation, or admin action.
- Provider event transition: Stripe or another provider reports setup-intent, payment, invoice, refund, dispute, or deletion state.
- Scheduled transition: a phase boundary, cycle boundary, recurrence boundary, grace deadline, or finalization due time becomes due.
- Reconciliation transition: a repair worker reconstructs missing deterministic rows or missing River jobs.
- Request-path transition: reserve performs bounded entitlement self-healing from already-authorized PostgreSQL state.
- Finalization transition: finalization verifies consent, applies deterministic invoice adjustments, and blocks customer charging when policy invariants are not met.

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

Canceling a scheduled period-end downgrade or cancellation is a local contract-change transition. If the user asks to start or change into the plan that is already the currently active paid phase, billing cancels matching scheduled period-end `contract_changes`, restores the contract from `cancel_scheduled` to `active` when applicable, clears cancellation boundary timestamps, and emits `contract_change_canceled` plus `contract_resume_applied`. It must not create a Stripe Checkout session, Stripe invoice, provider request, or provider event. A database uniqueness guard must reject more than one live scheduled period-end downgrade/cancellation for a contract, so the worst case is a failed local transaction rather than duplicate provider work.

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
closed_for_usage -> finalized
any non-terminal -> voided
```

`open` means new billing windows may attach to the cycle. `closed_for_usage` means no new windows can attach; finalization may still be pending. `finalized` means the active finalization reached a terminal issued, closed, paid, payment-failed, voided, or blocked state. A blocked finalization must not prevent `openNextCycle` from having created the successor cycle.

### Finalization lifecycle

```text
pending -> collecting_facts
collecting_facts -> awaiting_ledger_commands
awaiting_ledger_commands -> collecting_facts
collecting_facts -> awaiting_provider_preview
awaiting_provider_preview -> ready_to_issue
collecting_facts -> ready_to_issue
ready_to_issue -> issued
issued -> collection_pending
collection_pending -> paid
collection_pending -> payment_failed
payment_failed -> paid
issued -> closed
pending -> voided
collecting_facts -> blocked
awaiting_ledger_commands -> blocked
awaiting_provider_preview -> blocked
ready_to_issue -> blocked
blocked -> collecting_facts
```

`pending` means an accounting subject has a durable finalization row but no facts have been collected. `collecting_facts` recomputes the document candidate from PostgreSQL and TigerBeetle-backed ledger state. `awaiting_ledger_commands` means finalization-created TigerBeetle commands, such as no-consent showback or receivable accrual, must post before issuing a document. `awaiting_provider_preview` means Stripe tax/total preview is required before issue. `ready_to_issue` is the only state allowed to allocate a document number. `blocked` means a policy invariant failed and no customer charge may be finalized. A retry from `blocked` must recompute from the same or explicitly corrected facts and emit an operator-visible event.

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
pending -> in_progress -> posted
pending -> in_progress -> retryable_failed -> in_progress
retryable_failed -> dead_letter
posted -> expiring -> expired
posted -> closing -> closed
any non-posted -> failed
```

`pending` means the PostgreSQL grant row exists but must not fund reservations. `in_progress` means the deposit command is being dispatched. `posted` means the TigerBeetle grant account and deposit transfer exist and the grant can fund reservations if its scope and time window match. `closed` means PostgreSQL policy has closed the grant for future funding; it does not mutate historical TigerBeetle settlement transfers.

### Billing-window ledger lifecycle

```text
reserved -> active
reserved -> settling -> settled
active -> settling -> settled
reserved -> voided
active -> voided
settling --ledger command retryable_failed--> settling
```

`reserved` is a PostgreSQL authorization commit and is returned to callers only after active grant balances and active PostgreSQL authorization holds prove the workload can be admitted. `settled` is reached only after TigerBeetle accepts the final usage-spend transfers for grant-backed posted legs. `voided` is a PostgreSQL-only release of a reserved or active authorization. Ledger command retry failures keep the window in `settling`; they must not be returned to callers as terminal settlements.

### Billing-document lifecycle

```text
document status:
issued -> voided

payment_status:
n_a
pending -> paid
pending -> failed
failed -> paid
failed -> uncollectible
```

A document is immutable after `status = 'issued'`. `payment_status` and provider attachment fields may change, but document lines, totals, period boundaries, recipient snapshot, and rendered body must not. A blocked finalization is not a document lifecycle state because blocked finalizations are not customer debt and must not allocate a document number unless an operator deliberately issues a correction or explanatory internal statement.

### Dispute lifecycle

```text
none -> opened
opened -> won
opened -> lost
opened -> operator_resolved
```

Dispute state belongs to the payment/provider side of the issued document and the affected contract/change. On `charge.dispute.created`, billing records the provider event, marks the payment/document disputed, suspends or narrows the affected paid phase according to contract policy, stops future paid entitlement materialization, closes paid grants for future reservation selection when required, and opens a successor free or suspended cycle if paid access is revoked. On `charge.dispute.closed`, `won` restores or preserves payment state while `lost` records chargeback loss, revokes unused paid allowance through ledger commands, and keeps already-settled usage as auditable document/payment/correction history rather than mutating original windows.

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
pending -> in_progress -> posted
pending -> in_progress -> retryable_failed
retryable_failed -> in_progress -> posted
retryable_failed -> in_progress -> dead_letter
dead_letter -> pending (operator requeue, generation incremented)
```

Ledger command workers submit persisted TigerBeetle account/transfer specs. `posted` means TigerBeetle acknowledged the command or reconciliation proved every expected object exists with matching fields. `dead_letter` means retrying the same command would be unsafe or has exceeded policy; the corresponding domain rows must remain non-terminal or operator-blocked until resolved.

## Request-path reservation and self-healing

Reservation is an authorization lock, not a final charge.

1. Reserve validates org/product/actor/source input.
2. Reserve performs entitlement readiness self-healing from PostgreSQL-only facts.
3. Reserve loads the open billing cycle for the org/product.
4. Reserve loads active pricing and active/grace contract phases from PostgreSQL.
5. Reserve loads eligible posted grant metadata from PostgreSQL and balances from TigerBeetle.
6. Reserve subtracts active PostgreSQL authorizations for `reserved`, `active`, and `settling` windows from those posted balances.
7. Reserve chooses funding legs by the strict scope/source/tier/age waterfall.
8. Reserve inserts `billing_windows(state = 'reserved')`, normalized `billing_window_ledger_legs(state = 'pending')`, `source_fingerprint`, the resolved millisecond quantity, and `billing_window_reserved` evidence in one PostgreSQL transaction.
9. If posted grants minus active authorizations cannot fund the window and overage policy does not allow receivable funding, reserve returns no-capacity and the caller must not start work.
10. Settle computes actual usage, posts final spend through `billing_ledger_commands(operation = 'settle_window')`, releases any unbilled authorization remainder in PostgreSQL, and only then marks the window `settled`.
11. Metering projection is scheduled after settlement.

The request path never waits for River, Stripe, finalization, email delivery, or ClickHouse to prove current entitlements. It either creates missing deterministic current-period entitlement rows in the request transaction, or fails because the contract state/policy says the org is not entitled.

Renewable metering lease renewal is a product-service command around this same
reserve/settle API. The command locks the product cursor, settles the current
window through a boundary, reserves and activates the successor window, advances
the cursor, and schedules the next wakeup. The billing service does not infer
resource state from queue delays; the resource owner persists `billing_denied`
only after an explicit billing reserve denial and persists `system_overdue` for
late or failed renewal execution.

Self-healing rules:

- Free-tier current-period grants are always self-healable from org, policy, cycle, and calendar state.
- Contract grants are self-healable only when the contract and phase are already `active` or `grace` in PostgreSQL.
- Pending Stripe payment is not self-healable because the payment fact is external.
- If the product intentionally allows immediate access before payment finality, that must be represented by an explicit `grace` transition in PostgreSQL, not inferred during reserve.
- Reserve may close or ignore local rows only when their authoritative PostgreSQL phase/period state already proves they cannot fund current usage.
- Reserve must never call Stripe, scan provider APIs, render billing documents, send emails, or read ClickHouse.
- Reserve must never return a usable reservation before PostgreSQL commits the authorization window and normalized funding legs.
- Reserve must never fall back to PostgreSQL arithmetic when TigerBeetle balance lookup fails.
- Reserve must not create receivable funding legs for org/product postures that lack overage consent. Free-tier orgs and paid hard-cap orgs must deny admission once authorized grants and prepaid balances are exhausted.
- Settlement may record writeoff evidence for leaked no-consent usage, but that evidence must not become a customer receivable. Finalization is the only place where it becomes an automatic adjustment line.

This satisfies the user-facing guarantee: a customer must not lose a valid entitlement because a scheduled job was late. It also preserves payment correctness: a customer must not receive paid entitlements just because a provider event has not arrived.

## Recurring entitlement scheduling

Recurring grants are materialized from durable rows; they are not computed from provider state on the reservation hot path.

- Free-tier eligibility is universal: every org gets the configured free-tier policies. The billing-side org provisioning path must synchronously materialize the current billing cycle, current free-tier `entitlement_periods`, `credit_grants`, TigerBeetle grant deposit command, posted TigerBeetle balance, `grant_issued`, and `grant_ledger_posted` billing event facts before the org can submit billable usage. Reserve also self-heals the same deterministic current-period rows before funding.
- Contract eligibility is phase state plus entitlement-line recurrence. Only `active` and `grace` phases can materialize spendable contract periods. `scheduled` and `pending_payment` phases are planning state and must not fund reservations unless an explicit grace transition has been recorded.
- Self-serve `billing_cycle` lines materialize from Verself cycle boundaries, not Stripe subscription periods. Enterprise `calendar_month_day` lines materialize from the contract timezone and anchor day. `anniversary` anchors are available for non-cycle provider flows that renew from the service start date rather than the calendar.
- Period and grant identifiers are deterministic over org, source, scope, contract, phase, line, policy version, cycle id, and period boundaries. TigerBeetle IDs inside those rows are generated once and persisted, not recomputed from the deterministic domain ID. Retrying org provisioning, webhook handling, River jobs, reconciliation, or reserve self-healing must converge on the same PostgreSQL rows and the same TigerBeetle command IDs.
- River pre-materializes future due work, retries failed materialization, and repairs missed jobs. PostgreSQL rows remain the entitlement truth.

## Cycle rollover and finalization

Cycle rollover and finalization are separate transition paths.

`billing.cycle.rollover` runs at `billing_cycles.ends_at` or at an immediate commercial boundary such as free -> paid, cadence change, enterprise amendment, payment dispute, contract revocation, or operator correction. It must:

1. Lock the open predecessor cycle.
2. Mark it `closing` and then `closed_for_usage` so no new billing windows attach to it.
3. Apply due period-end `contract_changes` to determine successor policy.
4. Open the successor cycle with deterministic `(org_id, product_id, anchor_at, cycle_seq)` identity.
5. Attach or create successor contract phases as required.
6. Insert `billing_finalizations(state = 'pending', reason = ...)` for the closed predecessor.
7. Emit `billing_cycle_closed_for_usage`, `billing_cycle_opened`, and any reason-specific events such as `billing_period_split_for_cadence_change`.
8. Enqueue `billing.finalization.run` for the finalization row.
9. Enqueue `billing.cycle.rollover` for the successor.

It must not call Stripe, render document HTML, send email, or wait for payment collection. That prevents a slow or failed payment rail from causing a customer to lose a valid entitlement window.

`billing.finalization.run` advances one `billing_finalizations` row by one bounded step. Cycle finalizations run after the cycle is closed for usage. Contract-change, correction, and credit-note finalizations run when their subject reaches a ready-to-finalize state. The worker must:

1. Lock the finalization row, subject row, cycle when present, and any existing document for the finalization.
2. Recompute the document snapshot from settled windows, captured rate context, recurring charge policy, contract phases, entitlement periods, payments, disputes, and adjustments.
3. Verify that every settled window in the subject scope has posted or voided TigerBeetle ledger legs and no unresolved ledger command needed for the finalization scope.
4. Enforce overage consent and the USD $0.99 automatic no-consent adjustment cap from the PostgreSQL finalization set.
5. Create any required invoice-adjustment rows and optional TigerBeetle commands such as no-consent showback, receivable accrual, receivable clearing, or allowance revocation.
6. Move to `awaiting_ledger_commands` until those commands post.
7. Convert ledger units to Stripe cents only after the Verself total is final.
8. If Stripe Tax or provider total verification is required, move to `awaiting_provider_preview` and run the provider preview command before issue.
9. Set deterministic `document_kind`, `customer_visible`, `has_usage`, `has_financial_activity`, and `notification_policy`.
10. Allocate a gapless document number only from `ready_to_issue`.
11. Insert the immutable Verself document and line items.
12. Mark the finalization `issued`, `closed`, `collection_pending`, `paid`, `payment_failed`, or `blocked` according to document kind and collection state.
13. Mark the cycle `finalized` when a cycle finalization reaches a terminal state. Non-cycle finalizations must not mutate cycle status except through an explicit contract-change boundary.
14. Enqueue Stripe collection if `document_kind = 'invoice'`, `total_due_units > 0`, and payment collection is required.
15. Enqueue document email or notification delivery according to `notification_policy`.

Every closed cycle produces a finalization record. It produces a customer-visible invoice when a collectible amount is due, a customer-visible statement when the total is zero but customer-visible billing activity occurred, or an internal statement when the period must be accounted for but should not notify the customer. Dormant zero-usage zero-total free cycles suppress customer email and customer-facing notification by policy, but they are not absent from accounting history. Paid no-usage periods are customer-visible because the paid contract, recurring entitlement grant, payment, and renewal history are financial activity even when usage is zero.

Cycle catch-up is deterministic. If an org is inactive for several periods, reconciliation advances the cycle chain from the last open or due cycle to the current business time by repeating the same rollover/finalization function. Paid cycles still receive customer-visible invoices or statements even when usage is zero because recurring paid entitlement and collection facts are billing activity. Dormant free cycles may finalize as internal statements with suppressed customer delivery. Billing history and current-period UI must therefore read the finalization/document read model first and use raw usage only as supporting evidence; an absence of usage rows is not evidence that the customer has no billing activity.

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

Money is calculated in Verself ledger units and rounded to cents only when creating the Stripe collection artifact. Entitlement deltas are calculated per scope with deterministic integer rounding. Rounding must be applied once per applied change; multiple pending same-timestamp plan changes should coalesce to the final target before payment collection to avoid one-unit rounding arbitrage.

### Free tier to paid contract

Free tier remains independent. Creating a paid contract does not close or decrement free-tier grants.

Default flow:

1. API inserts a self-serve `contract` in `pending_activation` and a `contract_change(change_type = 'create', timing = 'immediate')`.
2. API records the intended paid plan in a pending `contract_phase` and copies plan-linked entitlement policies into `contract_entitlement_lines` for that phase.
3. Stripe collection for the paid activation is created from a `billing_finalizations(subject_type = 'contract_change')` row and its Verself invoice document; no Stripe Subscription is created.
4. Provider events are persisted in `billing_provider_events` and applied asynchronously.
5. Provider API success moves the change toward `awaiting_payment`; it does not activate paid entitlements and does not close the free cycle.
6. On `invoice.paid` or an explicitly accepted grace decision, the worker records `actual_effective_at`, calls the same cycle transition used by scheduled rollover, closes the current cycle for usage, opens a new cycle anchored at the activation moment, and enqueues finalization for the closed free cycle.
7. The worker sets the contract and phase entitlement state to `active` or `grace`.
8. Entitlement periods and credit grants are materialized for the new cycle after paid activation.
9. Free-tier grants continue independently and must not be closed by the paid activation.

Because free -> paid closes the free cycle for usage and opens a new paid cycle anchored at the paid activation time, the first paid Hobby cycle normally charges the full Hobby cycle price and issues the full Hobby contract grants for that new cycle. Free-tier grants from their own recurrence remain separate sources and continue to drain before contract grants.

The closed free cycle still goes through finalization. If it has settled usage, explicit adjustments, provider-visible customer activity, or other customer-visible financial activity, it issues a zero-total statement so the customer can see what changed before paid activation. If it has no customer-visible activity, it issues an internal statement and suppresses customer delivery. The new paid cycle notification is driven by `billing_cycle_opened` / `billing_period_started` domain events, not by a Stripe checkout redirect.

### Immediate paid upgrade

Default for Hobby -> Pro.

1. Insert `contract_changes(change_type = 'upgrade', timing = 'immediate')` with the current phase and target plan.
2. Compute and store the proration basis from the locked current cycle, current phase, target plan, and effective timestamp.
3. Keep the old phase active until payment succeeds or an explicitly accepted grace decision arrives.
4. Create the target Pro phase in `pending_payment` or `grace` according to policy.
5. Create a `billing_finalizations(subject_type = 'contract_change')` row and Verself invoice document for the positive prorated price delta when the business policy requires immediate collection.
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

Pending authorization windows that already selected old Hobby funding before the upgrade settle or void against their original funding legs. The upgrade path must not snapshot TigerBeetle balances and re-mint "remaining Hobby" into a replacement grant; keeping the old current-cycle grant open until expiry avoids double-spend and lost-capacity races around active authorization windows. New reservations after Pro activation can consume remaining Hobby carryforward plus the Pro delta grant, while their pricing context is captured from the active Pro phase.

Paid overages accrued before the upgrade remain attached to the old phase/rate context captured in their billing windows. They are not netted against the upgrade charge or erased by the Pro activation. If usage leaked without overage consent, finalization applies the automatic no-consent adjustment rules rather than charging the customer.

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

For an anniversary self-serve plan amended into an enterprise calendar-month contract, the successor may be a stub cycle from the amendment effective time to the next calendar boundary. Future cycles then follow normal calendar-month recurrence in the contract timezone. Billing windows must attach to exactly one cycle; any long-running window that crosses the cadence boundary is split at the boundary so rate context, entitlement context, and document line attribution remain unambiguous. Already settled windows stay on the predecessor cycle and are never re-priced by the cadence change.

## Stripe provider ingress and hardening

Stripe is one provider adapter for self-serve payment collection and payment-method management. Stripe Subscriptions are not part of the target architecture.

Target Stripe usage:

1. Vaulting: SetupIntent -> PaymentMethod -> `payment_methods` row linked to `provider_customer_id`.
2. Payment-method management: Customer Portal for card management, not a first-party card-vault UI.
3. Invoice collection: one-off Stripe invoices or payment intents created from an issued immutable Verself invoice document. The only exception is Stripe Tax pre-issue draft/preview verification, where a provider object is created from a Verself finalization candidate solely to compute and reconcile tax before the Verself document is issued.
4. Optional Stripe Tax: when enabled, Stripe tax computation is a pre-issue finalization input. The Verself document must not be issued until tax units are known and reconciled into the stored document snapshot.
5. Refunds/disputes: provider events update Verself payment state, dispute state, contract access state, and adjustment/refund records.

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
- `charge.dispute.updated`

Do not subscribe the billing endpoint to `customer.subscription.*` events in the target architecture.

Stripe collection flow for a finalizing or issued Verself invoice document:

1. Build a Verself finalization candidate in PostgreSQL from the subject facts: settled windows for cycle subjects, contract-change price deltas for immediate activation/upgrade subjects, plus recurring charges, adjustments, rounding policy, and overage-consent policy where applicable.
2. When Stripe Tax is disabled, issue the immutable Verself document before provider collection. When Stripe Tax is enabled, call Stripe preview/draft verification from the finalization candidate before issue, use it only to compute/verify tax, persist the reconciled tax units into `document_snapshot_json`, and then issue the immutable Verself document.
3. Create the Stripe draft invoice with `auto_advance = false` so Stripe cannot finalize the provider invoice before Verself verification completes, `collection_method = charge_automatically` only when the org has collection consent, and metadata containing `document_id` and `finalization_id`.
4. Add invoice items with deterministic idempotency keys per Verself document line and finalization generation. Do not reuse a Stripe idempotency key after changing request parameters.
5. Verify the Stripe draft or preview total matches the Verself document total after ledger-unit-to-cent rounding and tax policy.
6. After draft verification and Verself issue, explicitly finalize the Stripe invoice. If the product policy delegates payment retry to Stripe, configure the invoice so Stripe performs automatic collection/retry after finalization and report results through webhooks. If Verself owns dunning, keep provider automation disabled, explicitly finalize/pay through `billing.payment.retry`, and model retries as River-driven domain work.
7. Persist `stripe_invoice_id`, hosted invoice URL, invoice PDF URL, payment intent ID, and provider status on the Verself document row.
8. On payment success, persist the provider event and dispatch ledger commands for `stripe_payment_in` plus `receivable_clear_payment` for the invoice's outstanding receivable accounts.
9. Treat Stripe webhooks as payment-state inputs, not document truth.

When Stripe Tax is enabled, the draft or preview tax verification steps occur before the Verself document is marked `issued`; after issue, Stripe collection must not mutate the Verself document total.

The ledger unit to Stripe cent conversion must be explicit. Stripe invoice amounts are cent-denominated for USD. Verself ledger units are finer-grained, so finalization must apply one rounding/residual policy: carry forward residuals, write them off through an adjustment, or accumulate them in an org/product rounding bucket. Silent truncation is not allowed.

Direct top-up collection flow:

1. Create a provider checkout/payment-intent artifact for the exact top-up amount. Metadata must include `org_id`, `product_id`, intended ledger units, purchase idempotency key, and return-flow correlation.
2. Do not create spendable balance from checkout return alone. The balance appears only after durable payment success is recorded in `billing_provider_events`.
3. The provider-event worker creates or finds the deterministic `credit_grants(source = 'purchase')` row, emits `grant_issued`, persists TigerBeetle IDs, and dispatches the linked `stripe_payment_in` and `grant_deposit` command.
4. The provider event is not `applied` until the ledger command is posted or safely determined to have already posted with matching IDs.
5. `grant_ledger_posted` is emitted only after the grant is TigerBeetle-backed and spendable.

This gives the browser a repeatable polling story: checkout completion means payment is being applied; `grant_issued` means the domain grant is being made durable; `grant_ledger_posted` plus the TigerBeetle-backed grant balance means the account balance changed.

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
- Disable Stripe invoice emails. The target Verself path sends document emails through mailbox-service from the stored Verself document body.

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
- Create Preview Invoice API: <https://docs.stripe.com/api/invoices/create_preview>
- Create invoice API: <https://docs.stripe.com/api/invoices/create>
- Finalize invoice API: <https://docs.stripe.com/api/invoices/finalize>
- Invoice items API: <https://docs.stripe.com/api/invoiceitems>
- Dispute testing events: <https://docs.stripe.com/testing-use-cases>

Stripe never becomes the source of truth for cycles, contract phases, SKU pricing, entitlement scope, grant funding, billing documents, document numbering, or metering. It is strongest as a payment-method, hosted payment, optional tax, refund, dispute, and lifecycle-signal provider.

## Enterprise contracts

Enterprise contracts use the same tables and state machines.

- A bespoke enterprise agreement creates a `contracts` row with `contract_kind = 'enterprise'`.
- The active terms live in `contract_phases` with `phase_kind = 'bespoke'`.
- Recurring allowances live in `contract_entitlement_lines` with `recurrence_anchor_kind = 'calendar_month_day'`, an anchor day, and a contract timezone.
- Backend billing periods live in `billing_cycles` with `cadence_kind = 'calendar_monthly'`, `annual`, or `manual` depending on the contract.
- Amendments, renewals, upgrades, downgrades, and cancellations use `contract_changes`.
- Payment collection can be manual or provider-backed without changing entitlement generation.
- River schedules phase boundaries, cycle rollover, entitlement materialization, finalization, document delivery, and reconciliation exactly as it does for self-serve contracts.
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

## Document preview

Document preview is built from the same data finalization uses:

1. The current or closed `billing_cycles` row, or the `contract_changes` subject for immediate activation/upgrade previews.
2. Settled billing windows in PostgreSQL plus reserved-but-unsettled windows as clearly marked pending activity.
3. Normalized `billing_window_ledger_legs` for grant, receivable, source, and SKU attribution.
4. Projected metering rows in ClickHouse when available for display acceleration, with PostgreSQL plus TigerBeetle-backed ledger legs as truth.
5. SKU metadata and captured rate context from billing windows.
6. Contract, phase, and entitlement-period metadata from PostgreSQL.
7. Recurring charges, payment, refund, dispute, and adjustment facts from PostgreSQL.
8. Adjustment rules and entitlement/grant consumption.
9. Verself document snapshot rows once issued.

The preview shows:

- SKU line items first, using the SKU display name and quantity unit captured for the document.
- Bucket summaries next, using the bucket display name.
- Free-tier, contract, purchased, promo, and refund funding after that.
- Automatic system adjustments after funding in operator/finalization views. Customer-facing documents render them only when `customer_visible = true`, and never as spendable balance.
- Reserved but not yet finalized execution spend as a separate line.
- Remaining entitlement for the billing cycle and any purchased balance.

Blocked finalizations are not collectible invoice documents. They render operator-facing policy failure state and must not render as a customer amount due.

Current-period preview uses `billing_document_previews`. A preview row is keyed by `input_fingerprint`, expires quickly, and is discarded when any source fact changes. Stripe Create Preview Invoice may be called for tax and provider-total verification, but the Stripe preview is not payable, not editable into the Verself document, not a durable invoice, and not the PDF source. The canonical preview body is rendered from Verself's preview snapshot so the current-period UI and final issued document share one structure.

That keeps the preview structurally aligned with the final document rather than inventing a separate UI model.

## ClickHouse metering and document projection

ClickHouse stores the billing document and metering read models, not the transaction ledger.

All ClickHouse billing projections are at-least-once and must be backed by a
PostgreSQL delivery row written in the same transaction as the authoritative
domain transition. Billing events use `billing_event_delivery_queue`; metering
uses a non-event projection delivery row keyed by `window_id`.

Projection tables that use `ReplacingMergeTree` deduplicate during background
merges, so they can temporarily expose duplicate rows for the same deterministic
key. Operator verification queries that require immediate uniqueness must query
PostgreSQL domain truth, use ClickHouse `FINAL`, or explicitly group by
deterministic identifiers such as `event_id` or `window_id` and the relevant
version column. Production authorization, document issuance, ledger writes,
queue deletion, and provider-event application must not depend on ClickHouse
merge timing.

`verself.metering` must be idempotent by `window_id`. Its partition key must
be derived from a stable domain timestamp, such as `started_at`, not
`recorded_at`; otherwise a retry across a month boundary can strand duplicate
logical rows in different partitions. Customer-facing billing usage queries read
through a deduped view or aggregate by `window_id`.

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

The per-source drain maps are keyed by SKU id, not bucket id. The funder attributes every cent of every drain to the SKU that triggered it via the `ChargeSKUID` axis on funding legs. These maps preserve that attribution through to ClickHouse so the customer-facing document can show per-line drain splits without a secondary aggregation. Bucket-level drain splits are derivable by grouping `component_*_units` keys through each row's `rate_context.sku_buckets` mapping in analytics queries that still need the bucket axis.

Raw product usage is not billing truth. The billing read model is built from billing windows, normalized ledger legs, contract/phase context, finalization state, issued documents, payments, disputes, and adjustments. Product services may emit richer usage telemetry for debugging or capacity planning, but customer-facing billing history must derive from settled billing facts and the finalization/document state machine.

Document projections include `billing_cycle_opened`, `billing_cycle_closed_for_usage`, `billing_finalization_started`, `billing_document_issued`, `billing_statement_issued`, `invoice_adjustment_created`, `billing_finalization_blocked`, `billing_document_preview_created`, `billing_document_preview_stripe_verified`, `stripe_invoice_collection_started`, `stripe_invoice_paid`, `stripe_invoice_payment_failed`, and `billing_document_email_sent` events. These are evidence/read-model facts; PostgreSQL remains authoritative for the document artifact and finalization state machine.

Ledger projections include `grant_ledger_posted`, `ledger_command_posted`, `ledger_command_failed`, `ledger_drift_detected`, `billing_window_reserved`, `billing_window_settled`, `billing_window_voided`, `grant_expired`, `receivable_accrued`, `receivable_cleared`, and `no_consent_adjustment_showback_posted`. These projections prove that PostgreSQL domain rows and TigerBeetle side effects converged; they do not replace TigerBeetle account lookup for balance reads.

ClickHouse billing rows use contract projection names (`contract_units`, `pricing_contract_id`, `pricing_phase_id`, `pricing_plan_id`) rather than provider-specific subscription field names.

For sandbox jobs, trusted block storage evidence comes from the orchestrator's provisioned zvol size and is written as `rootfs_provisioned_bytes` in usage evidence. That gives the document preview a real storage signal instead of an inferred one.

ClickHouse docs to keep near this design:

- ReplacingMergeTree: <https://clickhouse.com/docs/engines/table-engines/mergetree-family/replacingmergetree>

## Fault injection and reconciliation

Fault injection targets provider-neutral seams rather than relying only on end-to-end Stripe payloads.

Provider-event fault cases:

- Duplicate provider event with identical `provider_event_id`.
- Delayed `invoice.paid` after local Stripe invoice creation.
- `invoice.payment_failed` followed by later `invoice.paid`.
- `setup_intent.succeeded` without overage consent.
- Refund or dispute event for a document already marked paid locally.
- Missing provider metadata resolved through local binding state.
- Unsupported provider event type recorded and ignored.
- Worker failure after provider event is recorded but before document/payment mutation.
- Worker failure after document/payment mutation but before event delivery projection.

Scheduler fault cases:

- River job missing for a due phase boundary.
- River job missing for cycle rollover.
- River job retried after the cycle successor has already opened.
- River job retried after the phase has already closed.
- Entitlement materialization job runs twice for the same period.
- Event delivery projection job fails after ClickHouse insert but before deleting the queue row.
- Metering projection job fails after ClickHouse insert but before marking the
  projection delivery row succeeded.
- Renewable metering lease wakeup misses `renew_by` without any explicit billing
  denial.
- Reserve runs before scheduled free-tier or active contract entitlement materialization.
- Finalization is delayed while the successor cycle remains open for valid usage.

Ledger fault cases:

- Grant deposit command posts in TigerBeetle but the process crashes before PostgreSQL marks the grant posted.
- Grant deposit command persists in PostgreSQL but TigerBeetle is unavailable before dispatch.
- Reservation transaction rolls back after selecting apparently eligible grants; no caller-visible reservation exists.
- Two concurrent reserve requests target the same org/product; the PostgreSQL org/product lock serializes authorization arithmetic.
- Settlement command posts only after a retry; the window must remain non-terminal until acknowledgement.
- Void after a workload launch failure is PostgreSQL-only and releases the authorization in the same transaction.
- Duplicate ledger command dispatch replays the same TigerBeetle IDs and receives idempotent exists/already-posted results.
- Operator account registry differs from TigerBeetle account flags, ledger, code, or `user_data`.
- Reconciliation finds a posted PostgreSQL leg without the expected TigerBeetle transfer.
- Reconciliation finds a TigerBeetle customer-grant account that PostgreSQL does not know about.
- TigerBeetle is unavailable during a customer balance read; the service must fail or return a marked stale projection, not recompute financial truth from PostgreSQL JSON.

Finalization fault cases:

- Free-tier org has a payment method on file but no paid contract or overage consent.
- Free-tier leaked overage is below the automatic-adjustment cap.
- Free-tier leaked overage exceeds the automatic-adjustment cap.
- Paid hard-cap org leaks usage after grants and prepaid balances are exhausted.
- Duplicate finalization job retries after automatic adjustments were created.
- Overage consent changes while finalization is in progress.
- Stripe draft invoice creation succeeds but invoice item creation fails.
- Stripe total diverges from the Verself document after rounding or tax.
- Document email delivery fails after issue and Stripe collection succeeds.

Expected behavior is convergence, not exactly-once execution. PostgreSQL state, deterministic identifiers, TigerBeetle idempotency, Stripe idempotency keys, document immutability, and billing event delivery idempotency must make repeated work safe.

## Edge-case appendix

This section records the target behavior for cases that are easy to mishandle when implementing the state machines.

### Grant issuance and spendability

**Provider payment succeeds, checkout returns, but the webhook is delayed.**

The browser may show “payment received and applying” based on the checkout return, but no spendable balance exists until the provider event is durably recorded, applied, the `credit_grants` row is created, `grant_issued` is emitted, the TigerBeetle `grant_deposit` command posts, and `grant_ledger_posted` is emitted. UI polling must key off provider-event/grant state rather than assuming the redirect proves balance.

**The grant row exists but TigerBeetle is unavailable before deposit dispatch.**

The grant remains `ledger_posting_state = 'pending'` or `retryable_failed` and cannot fund reservations. Balance reads either omit it from available balance or mark it as pending to operators. The system must not fall back to `credit_grants.amount - spent` arithmetic because that would create a second financial truth. Retry and reconciliation reuse the persisted account and transfer IDs.

**TigerBeetle posts the grant deposit, then the process crashes before PostgreSQL marks the grant posted.**

The ledger command can be replayed or reconciled by the same IDs. If TigerBeetle returns an idempotent already-exists/already-posted result with matching fields, PostgreSQL completes the aggregate transition, marks `ledger_posting_state = 'posted'`, emits `grant_ledger_posted`, and makes the grant eligible for reservation. `grant_issued` remains the earlier metadata fact and must not be retroactively treated as the spendable event.

**Two workers try to materialize the same entitlement period.**

The deterministic period/grant uniqueness constraints pick one PostgreSQL row. Only one command generation may be active for the grant. Duplicate workers either observe the existing posted command and exit or reuse the same pending command. They must not allocate a second TigerBeetle grant account for the same source reference.

### Reservation, settlement, and voiding

**Reservation transaction fails before `billing_windows.state = 'reserved'` commits.**

The original caller must not start work unless it receives the reserved response. No TigerBeetle transfer exists to repair. A retry with the same source identity either creates the authorization window or returns the already-committed reserved window.

**Reservation is rejected for insufficient balance after PostgreSQL selected eligible grants.**

PostgreSQL rolls back the authorization transaction and the caller must not launch work. This is the expected result when an earlier serialized reservation consumed the balance before the later request acquired the org/product lock.

**Work launches, launch fails, and the caller voids the window.**

The void path is PostgreSQL-only: pending legs move to `voided`, the window moves to `voided`, and no metering row or document usage line is emitted.

**Settlement command posts, then PostgreSQL fails before `billing_windows.state = 'settled'`.**

The window remains `settling`, which means settlement math is durable but terminal ledger acknowledgement has not been reflected into the aggregate. Projection and finalization must ignore the row as unsettled. Repair completes leg states, marks the window `settled`, emits `billing_window_settled`, and then projects metering.

**A settlement reports less usage than the reservation.**

Settlement posts only the billable amount in waterfall order and releases the unused authorization remainder. It never scales all sources proportionally. This preserves the hierarchy: tightest scope first, then free tier, contract, promo, refund, purchase, and finally consented receivable funding if allowed.

**A workload duration includes launch/setup time and guest runtime.**

Only the billable guest run phase is charged. Host launch latency, image setup, jailer setup, and warmup are usage evidence, not customer billable quantity. If guest runtime evidence is missing, the fallback must be an explicit host-side billable phase duration, not total wall-clock request duration.

**A renewable metering lease worker misses `renew_by`.**

The product cursor moves to `system_overdue` or remains retryable; customer
policy must not be revoked from the missed wakeup alone. Repair enqueues the same
idempotent renewal command, which either settles/reserves the missing interval or
records an explicit `billing_denied` outcome if billing rejects the successor
window for a business reason.

**A renewable metering lease renewal is explicitly denied by billing.**

The product owner records `billing_denied` with the denial code and applies
product policy from that persisted state. Storage should preserve reads and
exports while freezing writes only after policy grace. Long-running VMs should
move through graceful shutdown before termination. CI and short sandbox
executions may reject new admission immediately, because a new run has not yet
started.

**A storage object changes size during an open lease window.**

The storage owner closes the current interval early at the mutation or
measurement time, settles the old window using the prior allocation, reserves the
successor window with the new allocation, and advances the cursor. Git push,
branch deletion, repository deletion, LFS/object deletion, and git GC compaction
use this path. Idle storage is still billed by renewable lease wakeups or repair
at cycle boundaries; the absence of natural mutation events does not erase
storage-hour usage.

### Contract changes and recurring entitlements

**Free -> paid activation closes a free cycle that had real usage.**

The predecessor free cycle closes for usage and finalizes independently before the paid cycle begins. If settled usage or another customer-visible billing fact exists, finalization issues a zero-total statement instead of hiding the period. If the free period is dormant, finalization creates an internal statement and suppresses customer delivery. The paid cycle starts from its own anchor and never mutates or consumes the free grant.

**Free -> paid checkout is abandoned or payment fails.**

The contract-change finalization may be payment-failed or voided, but the free cycle remains open because activation never reached `invoice.paid` or an accepted grace decision. No paid grants materialize. Free-tier reservation and self-healing continue from the current free-cycle posture.

**A customer upgrades after consuming most of the lower-tier grant.**

Already-issued current-cycle paid grants remain open until their own expiry. The upgrade charges the positive prorated price delta and issues only the positive prorated entitlement delta between the target phase and the current phase for the remainder of the cycle. The customer cannot extract more entitlement by stepping through intermediate plans because each immediate upgrade is computed against the effective entitlement already promised at that timestamp.

**An immediate upgrade needs payment collection without closing the current cycle.**

The upgrade creates a `billing_finalizations(subject_type = 'contract_change')` row for the prorated price-delta document. That finalization may create a Stripe invoice or payment intent, but it must not mark the current cycle `closed_for_usage` or `finalized`. The cycle continues to accept post-upgrade windows after the contract change reaches `active` or `grace`; normal period-end finalization later includes the full cycle's usage and references the earlier contract-change document as payment/change history.

**A customer schedules a downgrade or cancellation, then resumes the current plan before period end.**

The scheduled `contract_changes` row transitions to canceled/reversed with actor and timestamp. The active phase remains the pricing and entitlement source until the cycle boundary. Starting the same plan while a downgrade is scheduled must resume/cancel the scheduled change rather than creating a new Stripe checkout or duplicate contract.

**A contract enters grace because payment collection is late.**

Entitlement continuity is controlled by contract/phase `entitlement_state`, not by Stripe subscription state. During explicit grace, contract grants may continue to materialize from `operator_contract_allowance_clearing`. If the account becomes terminally unpaid, unearned future grants are closed or voided, unused current-cycle contract allowance may be revoked through `contract_allowance_revoke` according to the contract policy, and already-consumed usage remains an invoice/receivable/correction problem rather than a hidden balance mutation.

**Enterprise contract recurrence is calendar anchored while self-serve is anniversary or cycle anchored.**

The recurrence policy lives on entitlement lines and phases. Both contract kinds materialize `entitlement_periods`, `credit_grants`, and `billing_cycles` through the same machinery; only the anchor calculation differs. Enterprise does not get a second subscription state machine.

**An enterprise amendment changes cadence in the middle of a running workload.**

The cycle boundary is still immediate at the amendment effective time when the contract says the cadence changes immediately. A workload window that crosses the boundary is split into predecessor and successor windows with separate rate context and ledger legs. If the product cannot split a specific long-running measurement safely, the amendment must be scheduled for period end or the workload must be checkpointed and restarted at the boundary. Do not attach one billing window to two cycles.

**A paid customer returns after several idle months.**

Due-work reconciliation advances every missed paid cycle and finalization because paid recurring entitlement and collection are financial activity even with no usage. Customer billing history shows those periods and their documents. A dormant free customer may have internal finalizations for the same gap, but the customer UI may suppress them unless usage or another customer-visible billing fact occurred.

**Business time is advanced or reset by an operator script.**

The business clock is an input to due-work discovery and period calculation, not a reason to mutate history. Already-opened cycles, periods, grants, windows, finalizations, and documents keep their timestamps. Reconciliation may open newly due periods after a forward jump. A backward reset must not create overlapping cycles or duplicate periods because deterministic domain identifiers and non-overlap constraints still apply.

### Overage consent and adjustments

**A free-tier org stores a payment method but does not opt into overage.**

The payment method only authorizes future provider collection flows chosen by the customer. It does not create receivable funding legs. Once grants and prepaid balances are exhausted, reservation denies new work. If admitted usage leaks through a race, settlement records writeoff evidence and finalization applies a no-consent automatic adjustment within the USD $0.99 cap or blocks finalization above the cap.

**A paid customer switches from bill-published-rate overage to hard cap mid-cycle.**

Reservation snapshots the overage policy used to admit work. Usage admitted before the change under explicit consent can settle into receivable legs. Usage admitted after hard cap must not create receivable legs. Finalization verifies the captured reservation policy and the current document policy; ambiguous or missing consent blocks charging rather than surprising the customer.

**No-consent leaked usage exceeds the automatic adjustment cap.**

Finalization must not issue a collectible invoice document for the leaked amount. It creates or updates a blocked finalization record, emits `billing_finalization_blocked`, prevents further no-consent execution for the affected org/product until operator resolution, and leaves explicit evidence for a manual adjustment, credit-note document, or engineering fix.

**A direct top-up is refunded after partial spend.**

Only unspent purchased balance can be removed from the customer grant account through `refund_balance_remove`. Any refund exceeding unspent balance is represented as a document/payment correction, goodwill adjustment, or operator-approved ledger correction. The system must not create a negative customer grant balance to make Stripe refund state “fit.”

### Document and provider boundaries

**Stripe invoice totals diverge from the Verself document because of rounding, tax, or provider line construction.**

The Verself finalization stays in `awaiting_provider_preview` or `blocked`; the document does not become issued and no document number is allocated. The residual must be represented as an explicit rounding/tax line or the provider draft must be corrected until totals reconcile. Stripe PDFs and hosted pages are allowed only after the Verself artifact is issued with a matching provider collection amount.

**A user previews a current-period billing document through Stripe.**

Stripe's preview API can be used only to verify tax and provider totals from a Verself finalization candidate. The preview response is stored as provider evidence on `billing_document_previews`, expires quickly, and cannot be promoted into a payable invoice, document number, PDF, or legal artifact. The canonical preview body is rendered from Verself snapshot data.

**Stripe reports `invoice.payment_failed` and later `invoice.paid`.**

Both provider events are immutable facts. The document payment state moves through failed/pending-retry to paid without changing the issued document artifact. Dunning and entitlement grace decisions read contract and document payment state, not Stripe subscription state.

**Stripe opens a dispute on a paid document.**

The provider event creates or updates `billing_payment_disputes`, marks the document/payment as disputed, and applies the contract's dispute policy. If policy revokes paid access immediately, rollover closes the paid cycle for usage with `closed_reason = 'payment_dispute'`, opens a free or suspended successor cycle, stops future paid entitlement materialization, and prevents unused disputed paid grants from funding new reservations. Already settled windows remain historical facts. If the dispute is won, payment state is restored or preserved. If it is lost, the system records chargeback loss, revokes unused paid allowance through ledger commands, and handles consumed usage through document/payment correction rather than mutating the original document.

**Provider metadata is missing but a binding exists locally.**

The provider event worker may resolve the event through `provider_bindings` only if the binding uniquely identifies the Verself aggregate and the event type is allowed for that binding. Otherwise it records the provider event as unsupported or needs-operator and performs no financial mutation.

### Projection, reconciliation, and operator repair

**ClickHouse contains duplicate projection rows after a retry.**

This is acceptable for at-least-once delivery. Evidence queries use `FINAL`,
deterministic source IDs, or aggregation by source ID depending on the table
engine. Billing events deduplicate by `event_id`; metering deduplicates by
`window_id`. Authorization, document issue, and balance reads never depend on
ClickHouse immediate exactness.

**A finalization row needs to notify Stripe, mailbox-service, and ClickHouse.**

Do not add a finalization-specific outbox table. The finalization row is mutable workflow state. Immutable facts are emitted into `billing_events`; sink-specific projection state lives in `billing_event_delivery_queue`; TigerBeetle side effects live in `billing_ledger_commands`; Stripe and mailbox-service work is scheduled as idempotent River work keyed by `document_id` or `finalization_id` and backed by provider binding rows when external IDs exist.

**A ledger command reaches `dead_letter`.**

The domain aggregate remains non-terminal or guarded; it is not silently treated as success. Operator repair must either requeue the same generation when the payload is still correct, create a new generation with an explicit correction reason, or close the aggregate through a documented failure transition. The repair itself emits billing events and is visible in ClickHouse.

**Reconciliation finds a TigerBeetle account or transfer PostgreSQL cannot explain.**

Critical drift opens a `billing_ledger_drift_events` row, emits `ledger_drift_detected`, and trips the ledger write guard for the affected scope or the whole service depending on severity. New financial writes remain blocked until the operator resolves, waives, or corrects the drift with an auditable ledger correction.

**A River job is missing, duplicated, or runs after the target row has changed.**

PostgreSQL timestamps and row state define due work. Periodic repair reconstructs missing jobs. Duplicate or stale jobs re-read the row, observe that the transition is not due or already applied, and exit without side effects.

## API naming target

The public/internal billing API is contract-, finalization-, and document-oriented:

- `/contracts` instead of `/subscriptions`
- `/contracts/{contract_id}/changes` for upgrade, downgrade, cancel, renew, and amend requests
- `/billing-cycles` for cycle inspection and operator repair
- `/billing-finalizations` and `/billing-finalizations/{finalization_id}` for operator-visible accounting closure state
- `/billing-documents` and `/billing-documents/{document_id}` for issued invoice, statement, internal-statement, credit-note, and adjustment-invoice artifacts
- `/billing-documents/preview` for current or closed-cycle previews
- `/billing-documents/{document_id}/adjustments` for manual/admin adjustments and credit-note flows
- `/payment-methods` for setup-intent-backed payment method management state
- `/provider-events` instead of `/subscription-provider-events`
- `/payment-disputes` for provider-neutral dispute state and operator repair
- `/ledger-commands` and `/ledger-drift` for operator-only inspection and repair; customer APIs should expose balances, documents, grants, and usage explanations, not raw TigerBeetle account internals
- `contract_id`, `phase_id`, `cycle_id`, `finalization_id`, `document_id`, and `change_id` in responses instead of a single mutable subscription plan field

Stripe-specific terms appear only at provider-adapter boundaries, provider binding rows, payment method rows, provider event rows, and Stripe UI flows such as Customer Portal and hosted payment collection.

## Production verification gates

Use PostgreSQL, ClickHouse traces, metering rows, finalization rows, document rows, provider-event rows, and billing events as the evidence point for the deployed path.

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
FROM verself.billing_events FINAL
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

The normal steady state has no stale `pending`, `in_progress`, or `retryable_failed` commands outside the retry policy. `dead_letter` is allowed only when an operator is actively resolving a documented incident.

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

Confirm provider events, change requests, phase transitions, and cycle transitions project into `verself.billing_events`.

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
FROM verself.billing_events FINAL
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
FROM verself.billing_events FINAL
WHERE event_type IN ('contract_change_applied', 'contract_phase_started', 'contract_phase_closed', 'grant_issued')
GROUP BY event_type, change_type, from_plan_id, target_plan_id, pricing_plan_id
ORDER BY event_type, pricing_plan_id, change_type
```

For a Hobby -> Pro upgrade, expect one applied upgrade change, one closed/superseded Hobby phase, one started Pro phase, carryforward Hobby grants still expiring at the current cycle end, and Pro `grant_issued` rows only for the positive prorated entitlement deltas in the current cycle.

9. **Reservation trace and ledger command present**

Confirm a sandbox job or billed workload produced a reservation trace in `billing-service`; query for the matching `window_id` in `default.otel_logs`, `billing_window_ledger_legs`, `billing_ledger_commands`, and the `verself.metering` row.

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
FROM verself.metering
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
FROM verself.metering
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
FROM verself.metering
WHERE product_id = 'sandbox'
ORDER BY recorded_at DESC
LIMIT 5
```

13. **Finalization subjects are explicit and terminal**

```sql
SELECT
  finalization_id,
  subject_type,
  subject_id,
  cycle_id,
  contract_change_id,
  state,
  document_id,
  document_kind,
  customer_visible,
  total_due_units,
  blocked_reason,
  updated_at
FROM billing_finalizations
ORDER BY updated_at DESC
LIMIT 20
```

14. **Billing document artifact is immutable and issued from stored snapshot**

```sql
SELECT
  document_id,
  document_number,
  document_kind,
  finalization_id,
  org_id,
  product_id,
  cycle_id,
  status,
  payment_status,
  total_due_units,
  content_hash,
  stripe_invoice_id,
  issued_at
FROM billing_documents
ORDER BY issued_at DESC
LIMIT 20
```

15. **No-consent adjustments enforce the cap**

Confirm free-tier and paid hard-cap orgs do not produce collectible receivable units without overage consent. Automatic no-consent adjustments must stay within the USD $0.99 per-org finalization cap; cap overflow must create a blocked finalization event instead of a customer charge.

```sql
SELECT
  finalization_id,
  org_id,
  sum(amount_units) FILTER (WHERE adjustment_source = 'system_policy') AS automatic_adjustment_units,
  max(reason_code) AS example_reason
FROM invoice_adjustments
WHERE reason_code IN ('free_tier_overage_absorbed', 'paid_hard_cap_overage_absorbed')
GROUP BY finalization_id, org_id
ORDER BY finalization_id DESC
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
FROM verself.billing_events FINAL
WHERE event_type IN ('invoice_adjustment_created', 'billing_finalization_blocked')
ORDER BY recorded_at DESC
LIMIT 20
```

16. **Stripe subscriptions are absent from target provider events**

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
