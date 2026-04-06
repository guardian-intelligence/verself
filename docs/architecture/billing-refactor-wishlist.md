# Billing: Refactor Wishlist

Findings from a full audit of `src/billing/` implementation code and `src/billing-service/cmd/billing-service/`. Tests are being rewritten from scratch so test-only issues are excluded — this focuses on implementation changes that would make the next generation of tests more meaningful.

---

## Critical: Silent failure paths

The most pervasive issue. Dozens of `_, _ = c.pg.ExecContext(...)` calls discard errors on audit-critical operations.

| File | Line(s) | What's silently discarded |
|---|---|---|
| `webhook.go` | 91, 101, 193, 201, 212, 293, 350, 388 | Billing event inserts (audit trail) |
| `webhook.go` | 54-56, 274-276 | Customer/org correlation lookups |
| `quota.go` | 103 | Quota violation event logging |
| `suspend.go` | 31-34 | `RowsAffected()` check on suspension |
| `periodic.go` | 367, 389 | `RowsAffected()` on trust tier updates |
| `reconcile.go` | 114-123 | Reconciliation alert logging |
| `worker.go` | 269, 282, 291 | Task completion/failure state transitions |

**Recommendation**: Every PG exec that writes a billing event or updates state must return its error. Callers decide whether to fail the operation or log-and-continue — but the decision must be explicit. The worker task state transitions (`completeTask`, `failTask`) are especially dangerous: if `completeTask` fails, the task stays in `claimed` state and is never retried.

---

## Critical: Multi-step operations without atomicity

Several operations span PG + TigerBeetle without rollback capability.

### `DepositCredits` (credit.go:45-110)

Four steps: PG insert → TB account create → TB transfer → PG billing event. If step 2 or 3 fails after step 1 succeeds, the grant exists in PG but has no TB account or funding. Future `GetOrgBalance` will find the grant, try to lookup the TB account, and fail.

**Recommendation**: Wrap PG operations in a transaction. If TB operations fail, rollback the PG insert. Alternatively, insert with a `status = 'pending'` flag, promote to `active` only after TB succeeds.

### `expireSingleGrant` (credit.go:197-254)

Two steps: TB transfer (debit remaining balance) → PG update (`closed_at = now()`). If PG update fails, the grant appears active but has zero balance in TB. Subsequent reserves will try to debit it, get zero allocation, and waste a transfer slot.

**Recommendation**: Same pattern — PG transaction with rollback on TB failure. Or mark as `closing` in PG first, then do TB transfer, then mark as `closed`.

### `handleInvoicePaid` (webhook.go:147-263)

Simultaneously: updates subscription period, reverts `past_due` status, logs billing event, and enqueues a task — all in separate PG calls with no transaction. Partial failure leaves subscription in an inconsistent state.

**Recommendation**: Single PG transaction for all subscription state mutations. Task enqueue can be outside the transaction (ON CONFLICT idempotency handles replays).

---

## High: Unbounded queries

### Reconciliation checks (reconcile.go)

`checkGrantAccountCatalogConsistency` (line 140) and `checkNoOrphanGrantAccounts` (line 196) query all grants without LIMIT, then do N+1 TigerBeetle lookups. For an operator with 100k lifetime grants, this will:
- Load all rows into memory
- Issue 100k individual TB lookups (or one massive batch)
- Block the reconciliation endpoint for minutes

**Recommendation**: Paginate with cursor-based iteration (`WHERE grant_id > $last ORDER BY grant_id LIMIT 1000`). Batch TB lookups in chunks of 1000.

### `GetOrgBalance` and `GetProductBalance` (client.go, periodic.go)

Same pattern — no LIMIT on grant queries. Less critical since active grants per org are typically < 100, but a runaway deposit bug could create thousands.

**Recommendation**: Add a sanity-check LIMIT (e.g., 10,000) and return an error if hit. This catches data corruption early.

### `DepositSubscriptionCredits` (periodic.go:36-50)

Loads all active subscriptions with `included_credits > 0` into memory. At scale (10k subscriptions), this is fine. At 1M, it's not.

**Recommendation**: Cursor-based pagination. Process in batches of 100.

---

## High: Missing health and readiness endpoints

`src/billing-service/cmd/billing-service/main.go` has no `/healthz` or `/readyz`. Caddy and any future load balancer can't distinguish "server up, dependencies down" from "server healthy". The worker goroutine runs in the background with no health signal — if it panics, the server still serves HTTP 200.

**Recommendation**: Add:
- `GET /healthz` — returns 200 if process is alive (for liveness probes)
- `GET /readyz` — returns 200 only if PG, TB, and CH connections are healthy (for readiness probes)
- Worker health: expose a `LastClaimAt` timestamp; readyz fails if worker hasn't claimed in 5x the poll interval

---

## High: Webhook handler robustness

### Body size limit

`webhook.go:26` hard-codes 64KB (`1<<16`). Stripe invoice payloads with large metadata or many line items can exceed this. The signature verification will fail silently because the truncated body doesn't match the HMAC.

**Recommendation**: Increase to 256KB or make configurable. Log a specific error when body exceeds limit.

### No idempotency at the handler level

`handleCheckoutSessionCompleted` (webhook.go:71) does an `UPDATE ... WHERE stripe_customer_id IS NULL OR stripe_customer_id != $1`. If Stripe delivers the event twice concurrently, both handlers read the old state and both update — harmless here, but the pattern is fragile.

**Recommendation**: Add `ON CONFLICT` or `SELECT ... FOR UPDATE` for operations that must be exactly-once. The task enqueue already has this via `idempotency_key` — extend the pattern to synchronous webhook side-effects.

### Missing rate limiting

No rate limiting on the webhook endpoint. A misbehaving Stripe retry loop or replay attack could overwhelm PG.

**Recommendation**: Add a simple in-process rate limiter (e.g., `golang.org/x/time/rate`) on the webhook handler. 100 req/s is generous for webhooks.

---

## Medium: fmt.Sprintf for SQL construction

### ClickHouse queries (clickhouse.go)

Database name is interpolated via `fmt.Sprintf` in 5 locations:

```go
q.conn.QueryRow(ctx, fmt.Sprintf(`SELECT ... FROM %s.metering WHERE ...`, q.database), ...)
```

While `q.database` is set at construction time from a trusted source, this establishes a pattern that's one copy-paste away from injection.

**Recommendation**: Validate database name at construction time (`NewClickHouseMeteringWriter`, `NewClickHouseMeteringQuerier`, `NewClickHouseReconcileQuerier`) — reject anything that isn't `[a-z_]+`. Add a comment explaining why `fmt.Sprintf` is used (ClickHouse parameterized queries don't support database name binding).

### Stripe price column (stripe.go:124)

```go
priceColumn := "stripe_monthly_price_id"
// ...
query := fmt.Sprintf(`SELECT %s FROM plans WHERE ...`, priceColumn)
```

Safe because `priceColumn` is a compile-time constant derived from `cadence`, but the pattern invites extension.

**Recommendation**: Use a `switch` that returns the full query string instead of interpolating a column name.

---

## Medium: Exported symbols that should be private

| Symbol | File | Why private |
|---|---|---|
| `AcctGrantCode` | types.go | Only used by internal TB account creation |
| `VMTransferID`, `SubscriptionPeriodID`, `StripeDepositID`, `CreditExpiryID` | ids.go | Transfer ID builders are implementation details |
| `OperatorAccountID`, `GrantAccountID` | ids.go | Only `NewGrantID` needs to be public |
| `AcctRevenue`, `AcctFreeTierPool`, etc. | types.go | Operator account types are internal |
| `XferKind` and its constants | types.go | Transfer kinds are internal to TB |
| `BillingCadence`, `CadenceMonthly`, `CadenceAnnual` | stripe.go | Only used by server and tests |

**Recommendation**: Lowercase these. The billing server binary accesses them, but it should go through `Client` methods instead. The test rewrite is the right time to stop depending on internals.

---

## Medium: Worker loop issues

### No exponential backoff on infrastructure failure (worker.go:28-33)

When PG is down, the worker retries every 5 seconds forever. This generates connection storms during outages.

**Recommendation**: Exponential backoff with jitter, capped at 60s. Reset to base interval on successful claim.

### Task failure backoff overflow (worker.go:290)

```go
backoffSecs := 5.0 * math.Pow(2, float64(task.Attempts-1))
```

With `max_attempts = 20`, this computes `5 * 2^19 = 2,621,440 seconds` (~30 days). The task will never be retried in practice.

**Recommendation**: Cap backoff at 300s (5 minutes). Or use a fixed set of backoff durations.

### completeTask / failTask don't return errors (worker.go:268-298)

Both discard PG errors. If `completeTask` fails, the task stays in `claimed` status indefinitely (no retry, no dead-letter). If `failTask` fails, the task also stays `claimed`.

**Recommendation**: Return errors from both. If the state transition fails, the worker should log a structured error and continue (the task will eventually time out and be reclaimed by another worker cycle if you add a `claimed_at + timeout` check to `claimTask`).

---

## Medium: Missing input validation in billing server

### `src/billing-service/cmd/billing-service/main.go`

- `requireEnv` calls `os.Exit(1)` directly. No structured error, no cleanup. Connections opened before the missing env var will leak.
- No startup validation that PG, TB, and CH are actually reachable (only PG is pinged).
- `envUint64` calls `os.Exit(1)` on parse failure. Same issue.

**Recommendation**: Collect all env vars, validate all, open all connections, then fail if any are unhealthy — with a single structured error listing all problems.

### Request validation gaps

- `checkoutInput` uses Huma's `required` and `minimum` tags, but `subscriptionInput` doesn't validate `org_id > 0`
- No length limits on `product_id`, `plan_id`, `success_url`, `cancel_url` — these pass through to Stripe and PG
- `usageInput.Limit` has no Huma validation — the `ListUsageEvents` method silently clamps to [1, 1000]

**Recommendation**: Add Huma struct tags: `minimum:"1"` on org IDs, `maxLength:"255"` on string fields, `minimum:"1" maximum:"1000"` on limit.

---

## Low: Observability gaps

- **No structured logging**: Uses `log.Printf` (unstructured). Should use `slog` with JSON output for ClickHouse/HyperDX ingestion.
- **No request tracing**: No correlation ID propagated through webhook → task → worker → TB → CH. Debugging cross-system failures requires manual timestamp correlation.
- **No operation metrics**: No counters for reserves, settles, voids, deposits, disputes, expirations. No histograms for TB or CH latency. These would surface degradation before users notice.

**Recommendation**: Adopt `slog` + OpenTelemetry. Emit spans for each billing operation. This is medium-effort but high-payoff given the existing OTel infrastructure.

---

## Low: Hardcoded business rules

| Rule | Location | Value |
|---|---|---|
| Purchase expiry | webhook.go:132 | 12 months from payment |
| Subscription grace period | webhook.go:233 | `period_end + 30 days` |
| Annual credit drip | periodic.go:119 | `yearly_credits / 12` |
| Worker poll interval | src/billing-service/cmd/billing-service/main.go:110 | 5 seconds |
| Webhook body limit | webhook.go:26 | 64KB |
| Worker backoff base | worker.go:290 | 5 seconds |
| Reservation window default | config.go:9 | 300 seconds |
| Pending timeout default | config.go:10 | 3600 seconds |

**Recommendation**: Move business rules (expiry, grace period, drip formula) to `Config` or the product/plan catalog in PG. Operational knobs (poll interval, backoff, body limit) to `Config`. The defaults are fine — the issue is that changing them requires a code change and redeploy.

---

## Summary priority matrix

| Priority | Item | Effort |
|---|---|---|
| **Critical** | Fix silent failure paths (discarded errors) | Low — change `_, _` to error checks |
| **Critical** | Add PG transactions around multi-step operations | Medium — wrap in `tx.Begin()`/`tx.Commit()` |
| **High** | Add health/readiness endpoints | Low — two Huma operations |
| **High** | Paginate reconciliation queries | Medium — cursor-based iteration |
| **High** | Webhook body size + rate limiting | Low |
| **Medium** | Unexport internal symbols | Low — rename + fix callers |
| **Medium** | Fix worker backoff overflow + error returns | Low |
| **Medium** | Validate CH database name at construction | Low |
| **Medium** | Startup validation (all deps healthy) | Low-Medium |
| **Low** | Structured logging (slog) | Medium |
| **Low** | Move hardcoded business rules to config | Low-Medium |
