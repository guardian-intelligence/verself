# src/billing

Billing service for forge-metal. Uses TigerBeetle as the financial ledger, PostgreSQL as the control plane, Stripe for payment collection, and ClickHouse for metering.

## TigerBeetle Orientation

TigerBeetle is an OLTP (Online Transaction Processing) database purpose-built for financial accounting. It stores two object types: **Accounts** (128 bytes each) and **Transfers** (128 bytes each). Both are append-only and immutable. There is no SQL, no schema definition, no tables. The entire API surface is 8 operations.

Official docs: <https://docs.tigerbeetle.com>

### Architecture Split

TigerBeetle is the **data plane** (hot path: balance tracking, transfer execution, constraint enforcement). PostgreSQL is the **control plane** (metadata: org names, grant records, subscription state, Stripe references). ClickHouse is the **metering plane** (usage tracking, quota aggregation, billing analytics).

Critical rule from TB docs: **initiating a transfer must never require a synchronous fetch from the control plane database.** Integer-to-string mappings for ledger codes, account types, and transfer kinds must be hardcoded or cached. This package hardcodes them as Go constants (see `types.go`).

Docs: <https://docs.tigerbeetle.com/coding/system-architecture>

### Data Model

**Ledger**: a uint32 on each account that partitions which accounts can transact. Only same-ledger accounts transfer directly. Cross-ledger requires linked transfers through liquidity accounts. This package uses ledger `1` for all USD-denominated accounts.

**Account codes** (uint16, the "why" of the account):

| Code | Constant | Type |
|------|----------|------|
| 3 | `AcctRevenue` | Operator income |
| 4 | `AcctFreeTierPool` | Free tier credit source |
| 5 | `AcctStripeHolding` | Stripe payment holding |
| 6 | `AcctPromoPool` | Promotional credit source |
| 7 | `AcctFreeTierExpense` | Free tier expense tracking |
| 8 | `AcctExpiredCredits` | Expired credit sink |
| 9 | `AcctGrantCode` | Per-org credit grant |

**Transfer kinds** (packed into transfer IDs, not the TB `code` field):

| Kind | Constant | Description |
|------|----------|-------------|
| 1 | `KindReservation` | Two-phase pending: reserve grant balance for a job |
| 2 | `KindSettlement` | Post-pending: confirm metered usage |
| 3 | `KindVoid` | Void-pending: cancel unused reservation |
| 4 | `KindFreeTierReset` | Periodic free tier replenishment |
| 5 | `KindStripeDeposit` | Credit from Stripe payment |
| 6 | `KindSubscriptionDeposit` | Credit from subscription period |
| 7 | `KindPromoCredit` | Promotional credit grant |
| 8 | `KindDisputeDebit` | Stripe dispute clawback |
| 9 | `KindCreditExpiry` | Pending expiry (two-phase) |
| 10 | `KindDepositConfirm` | Post-pending deposit |
| 11 | `KindExpiryConfirm` | Post-pending expiry |

### Four Balance Fields Per Account

TigerBeetle tracks four running totals, not a single "balance" number:

| Field | Meaning |
|-------|---------|
| `debits_pending` | Reserved outgoing (two-phase transfers not yet posted/voided) |
| `debits_posted` | Confirmed outgoing |
| `credits_pending` | Reserved incoming |
| `credits_posted` | Confirmed incoming |

**Credit balance** = `credits_posted - debits_posted`. This is what grant accounts use. Protected by `DebitsMustNotExceedCredits` (TB rejects any transfer where `debits_pending + debits_posted + amount > credits_posted`).

**Debit balance** = `debits_posted - credits_posted`. Used for asset/expense accounts. Protected by `CreditsMustNotExceedDebits`.

These two flags are **mutually exclusive**. Setting neither means no balance constraint (the account can go arbitrarily negative in either direction).

## Best Practices

### IDs and Idempotency

IDs are 128-bit. Their primary purpose is **idempotency**: if the process crashes after sending a transfer but before receiving the response, retrying with the same ID returns `exists` (success) instead of double-executing.

**Recommended ID format**: high 48 bits = millisecond timestamp, low 80 bits = random. Strictly increasing IDs optimize TigerBeetle's LSM (Log-Structured Merge-tree) -- the internal data structure that sorts records on disk. Random UUIDs cause write amplification and significantly degrade throughput.

This package uses ULID (Universally Unique Lexicographically Sortable Identifier) with a half-swap to place the timestamp in TB's high u64. See `ids.go:grantHalfSwap`. For non-ULID transfer IDs, the source entity ID goes in the high u64 for the same LSM benefit. See `VMTransferID`, `SubscriptionPeriodID`, `StripeDepositID`.

**Deterministic IDs** are preferred over random generation. If you can derive the ID from the business event (e.g., `(subscription_id, period_start, kind)` -> transfer ID), the transfer is idempotent by construction. This package uses deterministic IDs for all transfer types.

Docs: <https://docs.tigerbeetle.com/coding/data-modeling#id>

### ID Byte Maps

All IDs are 128-bit (`types.Uint128`), stored as two little-endian uint64s: bytes [0:8] are the **low u64**, bytes [8:16] are the **high u64**. TigerBeetle's LSM tree orders by the full 128-bit value, so placing a monotonic key in the high u64 keeps related records physically adjacent on disk.

Source: `ids.go`

#### Account IDs

**Operator accounts** (`OperatorAccountID`, `ids.go:96`): sentinel IDs with just the type enum in the low 2 bytes.

```
byte:  0  1  2  3  4  5  6  7 │ 8  9 10 11 12 13 14 15
       ├──┘                    │
       type                    │  0  0  0  0  0  0  0  0
       (LE u16)                │  high u64 = 0
```

Collision safety: any ULID-derived grant account ID has a nonzero high u64 (the ULID timestamp), so operator IDs (high u64 = 0) never collide with grant IDs.

**Grant accounts** (`GrantAccountID`, `ids.go:54`): ULID half-swap via `grantHalfSwap` (`ids.go:44`).

```
ULID (16 bytes, big-endian):
byte:  0  1  2  3  4  5  6  7 │ 8  9 10 11 12 13 14 15
       ├─────────────┘  ├──┘  │
       48-bit timestamp random │  80-bit random tail
       (ms since epoch) head   │

                    ┌── swap + endian flip ──┐
                    ▼                        ▼
Uint128 (16 bytes, little-endian):
byte:  0  1  2  3  4  5  6  7 │ 8  9 10 11 12 13 14 15
       ULID[8:16] as LE u64   │  ULID[0:8] as LE u64
       (random tail)           │  (timestamp + random head)
       low u64                 │  high u64 ← monotonic
```

The mapping is bijective -- `AccountID.GrantULID()` (`ids.go:82`) reverses it exactly.

#### Transfer IDs

**Non-ULID transfers** share a common layout (`VMTransferID` `ids.go:104`, `SubscriptionPeriodID` `ids.go:116`, `StripeDepositID` `ids.go:128`, `DisputeDebitID` `ids.go:137`):

```
byte:  0  1  2  3  4     5     6  7 │ 8  9 10 11 12 13 14 15
       ├─────────┘  │     │         │  ├────────────────────┘
       seq          idx   kind  pad │  source_id
       (LE u32)     (u8)  (u8)     │  (LE u64) ← monotonic
```

| Constructor | source_id (high u64) | seq (low [0:4]) | idx [4] | kind [5] |
|---|---|---|---|---|
| `VMTransferID` | `job_id` | `window_seq` | `grant_idx` | transfer kind |
| `SubscriptionPeriodID` | `subscription_id` | `year*12 + month` | 0 | transfer kind |
| `StripeDepositID` | `task_id` | 0 | 0 | transfer kind |
| `DisputeDebitID` | `task_id` | 0 | `grant_idx` | `KindDisputeDebit` (8) |

`TransferID.Parse()` (`ids.go:147`) extracts all four fields.

**ULID-derived transfer IDs** for credit expiry:

| Constructor | Derivation | Source |
|---|---|---|
| `CreditExpiryID` | Same half-swap as `GrantAccountID` (safe: account and transfer ID namespaces are separate in TB) | `ids.go:62` |
| `CreditExpiryPostID` | Half-swap then XOR byte [5] with `0xFF` (guarantees difference from `CreditExpiryID`) | `ids.go:71` |

#### Collision Safety

Account IDs and transfer IDs are **separate TB namespaces** -- the same numeric value can exist in both.

**Account ID domains** are structurally disjoint:
- Operator account IDs have high u64 = 0.
- Grant account IDs have high u64 > 0 (ULID timestamp). No overlap possible.

**ULID-derived transfer IDs** (`CreditExpiryID`, `CreditExpiryPostID`) have high u64 values in the ~50-bit timestamp range (e.g., ~1.7 trillion for current epoch). Non-ULID transfer IDs have high u64 = small sequential PG int64s (1, 2, 3...). Practical collision is impossible.

**Non-ULID transfer ID domains** share the same byte layout and rely on the `(kind, seq)` combination for discrimination. Most kinds are exclusive to one constructor:

| Kind | Value | Exclusive to |
|------|-------|-------------|
| `KindReservation` | 1 | `VMTransferID` |
| `KindSettlement` | 2 | `VMTransferID` |
| `KindVoid` | 3 | `VMTransferID` |
| `KindFreeTierReset` | 4 | `SubscriptionPeriodID` |
| `KindStripeDeposit` | 5 | `StripeDepositID` |
| `KindPromoCredit` | 7 | `StripeDepositID` |
| `KindDisputeDebit` | 8 | `DisputeDebitID` |

Two kinds are **shared** between `StripeDepositID` and `SubscriptionPeriodID`:

| Shared kind | `StripeDepositID` seq [0:4] | `SubscriptionPeriodID` seq [0:4] |
|---|---|---|
| `KindSubscriptionDeposit` (6) | 0 | `year*12 + month` (always >= 1) |
| `KindDepositConfirm` (10) | 0 | `year*12 + month` (always >= 1) |

Collision between these two constructors is prevented by the seq field: `StripeDepositID` always has seq=0, while `SubscriptionPeriodID` always has seq >= 1 (since `time.Month()` returns 1-12, the minimum is `0*12 + 1 = 1`). This invariant holds for any valid `time.Time`. Source: `credit.go:395-401` (`depositTransferID`).

**`TransferID.Parse()` caveat**: assumes the non-ULID byte layout. Calling it on `CreditExpiryID` or `CreditExpiryPostID` returns syntactically valid but meaningless values. The docstring warns about this (`ids.go:146`). Current callsites are correct; this is a footgun for future code.

### Amounts Are Integers

All amounts are unsigned 128-bit integers. Map the smallest useful currency unit to 1. This package uses **asset scale 0** (1 unit = 1 credit unit, not fractional currency). USD cents would be asset scale 2.

**Asset scale cannot be changed after accounts exist.** Changing it requires a new ledger number and migrating all accounts.

Docs: <https://docs.tigerbeetle.com/coding/data-modeling#fractional-amounts-and-asset-scale>

### Batch Everything

TB processes up to **8,189 events per request**. The Go client automatically batches concurrent goroutine calls into one network round-trip. Share one `tb.Client` across the entire service -- never create per-request clients.

One in-flight request per client session. Max 64 concurrent sessions by default. The client queues excess requests internally.

Docs: <https://docs.tigerbeetle.com/coding/requests>, <https://docs.tigerbeetle.com/reference/sessions>

### No Balance Lookups Before Transfers

**Anti-pattern**: look up account balance, check if sufficient, then create transfer. This is a TOCTOU race (Time-Of-Check to Time-Of-Use) -- the balance can change between lookup and transfer.

**Correct pattern**: set `DebitsMustNotExceedCredits` on the account and let TB atomically reject insufficient-balance transfers. This is what all grant accounts in this package do.

Docs: <https://docs.tigerbeetle.com/reference/operations/lookup_accounts>

### Two-Phase Transfers

The core pattern for reserving funds, then confirming or canceling.

**Phase 1 (Pending)**: `flags.Pending` reserves amount in `*_pending` fields. Posted fields unchanged. Optional `Timeout` in seconds -- if not resolved before expiry, auto-voids.

**Phase 2 (Post)**: `flags.PostPendingTransfer` with `PendingID` referencing the pending transfer. Creates a **new** transfer record (the original is immutable). Use `AmountMax` (2^128 - 1) to post the full pending amount without restating it.

**Phase 2 (Void)**: `flags.VoidPendingTransfer` with `PendingID`. Reverses the reservation entirely.

**Phase 2 (Expire)**: Automatic void after `Timeout` seconds. Cannot manually post/void an expired transfer.

**Key constraint**: balance limits are checked **pessimistically at pending time**. If posting the full amount would violate `DebitsMustNotExceedCredits`, the pending transfer itself fails immediately.

A pending transfer can only be resolved **once**. Attempting a second resolution returns `pending_transfer_already_posted` / `_voided` / `_expired`.

Docs: <https://docs.tigerbeetle.com/coding/two-phase-transfers>

### Linked Events

`flags.Linked` chains events so they succeed or fail **atomically**. The chain ends at the first event without the `Linked` flag. The last event in a batch must **not** have `Linked` set (returns `linked_event_chain_open`).

Multiple independent chains can coexist in one batch. After execution, the link association is **not persisted** -- encode relationships in `user_data` fields or deterministic IDs.

Error reporting: the first failure in a chain gets the specific error code. All other chain members get `linked_event_failed`.

Docs: <https://docs.tigerbeetle.com/coding/linked-events>

### `user_data` Fields

Three indexed fields on accounts and transfers for joining back to the control plane:

| Field | Size | Convention in this package |
|-------|------|---------------------------|
| `user_data_128` | 16 bytes | Not currently used (reserved for entity pointers) |
| `user_data_64` | 8 bytes | `OrgID` on grant accounts |
| `user_data_32` | 4 bytes | `GrantSourceType` on grant accounts |

Zero means "no filter" in queries. Only non-zero values are usable as query predicates.

Docs: <https://docs.tigerbeetle.com/coding/data-modeling#user_data>

### Account History

Set `flags.History` at account creation to retain balance snapshots after every transfer. Required for `get_account_balances`. All operator and grant accounts in this package enable it.

Cannot be added after creation (accounts are immutable).

### Closing Accounts

`flags.Closed` on an account rejects all transfers except voiding pending ones. Can be set at creation, or via `closing_debit`/`closing_credit` transfer flags (which require `flags.Pending` so the action is reversible by voiding).

Docs: <https://docs.tigerbeetle.com/coding/recipes/close-account>

### Reliable Transaction Submission

The **client** (not the API server) should generate transfer IDs. Persist the ID locally before submission. On crash/retry, resubmit with the same ID. TB returns `ok` (first time) or `exists` (already done) -- both are success.

This package implements this via deterministic ID construction: the ID is derived from business event fields, so any retry naturally uses the same ID.

Docs: <https://docs.tigerbeetle.com/coding/reliable-transaction-submission>

### Time

TigerBeetle assigns all timestamps via its cluster clock. Clients must set `Timestamp: 0`. Timestamps are nanoseconds since Unix epoch, strictly monotonic, totally ordered, unique.

`Transfer.Timeout` is an interval in **seconds** (not an absolute timestamp), measured from the cluster-assigned arrival time.

Exception: `flags.Imported` lets you provide timestamps for backfilling historical data. Must be past, strictly increasing, unique at nanosecond resolution.

Docs: <https://docs.tigerbeetle.com/coding/time>

## Recipes Reference

These are composite patterns built from TB's primitives. Each is documented with worked examples in the official docs.

| Pattern | Use case | Docs |
|---------|----------|------|
| Two-phase transfers | Reserve then confirm/cancel | <https://docs.tigerbeetle.com/coding/two-phase-transfers> |
| Linked events | Atomic multi-transfer operations | <https://docs.tigerbeetle.com/coding/linked-events> |
| Currency exchange | Cross-ledger via liquidity accounts | <https://docs.tigerbeetle.com/coding/recipes/currency-exchange> |
| Balance-conditional transfers | "Transfer X only if balance >= Y" | <https://docs.tigerbeetle.com/coding/recipes/balance-conditional-transfers> |
| Balance bounds | Keep balance between upper and lower limits | <https://docs.tigerbeetle.com/coding/recipes/balance-bounds> |
| Closing accounts | Zero balance + forbid further transfers | <https://docs.tigerbeetle.com/coding/recipes/close-account> |
| Correcting transfers | Reverse/adjust immutable transfers | <https://docs.tigerbeetle.com/coding/recipes/correcting-transfers> |
| Rate limiting | Leaky bucket via pending transfer timeouts | <https://docs.tigerbeetle.com/coding/recipes/rate-limiting> |
| Multi-debit/credit | Compound journal entries via control accounts | <https://docs.tigerbeetle.com/coding/recipes/multi-debit-credit-transfers> |

## Error Handling

TB create operations return results only for **failures**. Success = empty result slice. The result struct has `.Index` (position in batch) and `.Result` (error code).

**Treat `exists` as success.** When the ID and all fields match an existing record, TB returns `exists` -- this is the idempotency guarantee. `exists_with_different_*` means the ID collided but fields differ, which is a bug.

**Transient errors** (may succeed on retry with different cluster state): `debit_account_not_found`, `credit_account_not_found`, `pending_transfer_not_found`, `exceeds_credits`, `exceeds_debits`, `debit_account_already_closed`, `credit_account_already_closed`.

See `client.go:createAccounts` for this package's error handling pattern.

Docs: <https://docs.tigerbeetle.com/reference/operations/create_accounts>, <https://docs.tigerbeetle.com/reference/operations/create_transfers>

## Query Operations

| Operation | Input | Max results | Notes |
|-----------|-------|-------------|-------|
| `lookup_accounts` | Up to 8,189 IDs | 8,189 | Missing IDs silently omitted |
| `lookup_transfers` | Up to 8,189 IDs | 8,189 | Missing IDs silently omitted |
| `get_account_transfers` | 1 `AccountFilter` | 8,189 | Filter by account + user_data + code + time range |
| `get_account_balances` | 1 `AccountFilter` | 8,189 | Requires `flags.History` on account |
| `query_accounts` | 1 `QueryFilter` | 8,189 | AND-intersection of user_data + ledger + code + time range |
| `query_transfers` | 1 `QueryFilter` | 8,189 | Same filter semantics as query_accounts |

Paginate via `timestamp_min`/`timestamp_max` using the last result's timestamp. Set `flags.Reversed` for newest-first ordering.

Docs: <https://docs.tigerbeetle.com/reference/account-filter>, <https://docs.tigerbeetle.com/reference/query-filter>

## Performance

TigerBeetle uses a single core per replica. It does not utilize additional CPU cores. Throughput comes from batching, not parallelism.

| Resource | Minimum | Recommended |
|----------|---------|-------------|
| CPU | 1 core for TB + 1 for OS | 2+ total |
| RAM | 6 GiB | 16-32 GiB (static allocation, more = better caching) |
| Storage | Any | Local NVMe |
| ECC RAM | Required for production | Non-negotiable for financial data |

**Throughput levers**:
- Use time-ordered IDs (this package does via ULID half-swap)
- Batch aggressively (Go client auto-batches concurrent calls)
- Share one client across the service (this package does via `Client.tb`)
- Prefer single-phase when two-phase isn't needed (2x fewer writes)
- Minimize concurrent client sessions

**Published numbers** (independent benchmarks):
- SoftwareMill benchmark: ~42,000 TPS on M1 Max, 2.8x faster than best-case PostgreSQL batched, 6.6x vs explicit locking
- Mojaloop integration: 2,000 TPS vs 78 TPS with MySQL (26x improvement)

Docs: <https://docs.tigerbeetle.com/operating/hardware>

## External Validation

### Jepsen Report (June 2025)

Kyle Kingsbury tested TB 0.16.11-0.16.30. Verdict: "consistent with TigerBeetle's claims of Strong Serializability" and "exceptional resilience to disk faults." Found 2 safety issues (both fixed in 0.16.26+), 2 client crashes, 5 server crashes. Recommended version: 0.16.43+.

Strong Serializability (strict serializability) means: all operations appear to execute atomically in some total order consistent with real-time ordering. If operation A completes before operation B starts, B observes A's effects.

- Jepsen report: <https://jepsen.io/analyses/tigerbeetle-0.16.11>
- TB response: <https://tigerbeetle.com/blog/2025-06-06-fuzzer-blind-spots-meet-jepsen/>

### Production Deployments

| Entity | Use case |
|--------|----------|
| Mojaloop / Gates Foundation | National payments infrastructure across 20 countries (Rwanda NDPS 2.0) |
| Senapt | Clean energy transaction processing |
| Interledger / Rafiki | Open-source payments platform backend |
| Unnamed $2B European fintech | Signed mid-2025 (CEO statement, unverified) |

### Engineering References

| Title | URL |
|-------|-----|
| Official docs | <https://docs.tigerbeetle.com> |
| Go client reference | <https://docs.tigerbeetle.com/coding/clients/go> |
| Design philosophy | <https://tigerbeetle.com/blog/2024-07-23-rediscovering-transaction-processing-from-history-and-first-principles/> |
| Trillion transaction scale test | <https://tigerbeetle.com/blog/2026-03-19-a-trillion-transactions/> |
| Deterministic simulation testing | <https://tigerbeetle.com/blog/2025-02-13-a-descent-into-the-vortex/> |
| SoftwareMill benchmark vs PostgreSQL | <https://softwaremill.com/tigerbeetle-vs-postgresql-performance-benchmark-setup-local-tests/> |
| Amplify Partners investor thesis | <https://www.amplifypartners.com/blog-posts/why-tigerbeetle-is-the-most-interesting-database-in-the-world> |
