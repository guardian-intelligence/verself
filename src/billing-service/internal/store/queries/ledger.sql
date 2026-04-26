-- name: InsertLedgerAccount :exec
INSERT INTO billing_ledger_accounts (account_key, account_id, ledger, code, flags, account_kind, description)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (account_key) DO NOTHING;

-- name: GetLedgerAccount :one
SELECT account_key, account_id::bytea AS account_id, ledger, code, flags
FROM billing_ledger_accounts
WHERE account_key = $1;

-- name: ListOperatorLedgerAccounts :many
SELECT account_key, account_id::bytea AS account_id
FROM billing_ledger_accounts
WHERE account_kind = 'operator'
ORDER BY account_key;

-- name: InsertLedgerCommand :exec
INSERT INTO billing_ledger_commands (
    command_id, operation, aggregate_type, aggregate_id, org_id, product_id, idempotency_key, payload, state
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'pending')
ON CONFLICT (idempotency_key) DO NOTHING;

-- name: GetLedgerCommandByIdempotencyKey :one
SELECT command_id, payload
FROM billing_ledger_commands
WHERE idempotency_key = $1;

-- name: GetLedgerCommand :one
SELECT command_id, operation, aggregate_type, aggregate_id, org_id, product_id, payload, attempts
FROM billing_ledger_commands
WHERE command_id = $1;

-- name: LeaseLedgerCommand :one
UPDATE billing_ledger_commands
SET state = 'in_progress',
    attempts = attempts + 1,
    last_attempt_at = now(),
    lease_expires_at = now() + interval '30 seconds',
    leased_by = 'billing-service',
    last_attempt_id = $2
WHERE command_id = $1
  AND state IN ('pending', 'retryable_failed', 'in_progress')
  AND (state <> 'in_progress' OR lease_expires_at IS NULL OR lease_expires_at < now())
RETURNING command_id, operation, aggregate_type, aggregate_id, org_id, product_id, payload, attempts;

-- name: MarkLedgerCommandPosted :exec
UPDATE billing_ledger_commands
SET state = 'posted',
    posted_at = now(),
    last_error = '',
    lease_expires_at = NULL,
    leased_by = ''
WHERE command_id = $1;

-- name: MarkLedgerCommandFailed :exec
UPDATE billing_ledger_commands
SET state = $2,
    next_attempt_at = $3::timestamptz,
    last_error = $4,
    lease_expires_at = NULL,
    leased_by = '',
    dead_lettered_at = COALESCE($5::timestamptz, dead_lettered_at),
    dead_letter_reason = COALESCE(NULLIF($6, ''), dead_letter_reason)
WHERE command_id = $1;

-- name: ListPendingLedgerCommands :many
SELECT command_id, state
FROM billing_ledger_commands
WHERE (
    state IN ('pending', 'retryable_failed')
    AND next_attempt_at <= now()
  )
  OR (
    state = 'in_progress'
    AND (lease_expires_at IS NULL OR lease_expires_at < now())
  )
  OR (
    state = 'posted'
    AND (
      (operation = 'grant_deposit' AND EXISTS (
        SELECT 1 FROM credit_grants g
        WHERE g.grant_id = aggregate_id AND g.ledger_posting_state <> 'posted'
      ))
      OR (operation = 'settle_window' AND EXISTS (
        SELECT 1 FROM billing_windows w
        WHERE w.window_id = aggregate_id AND w.state = 'settling'
      ))
    )
  )
ORDER BY next_attempt_at, created_at, command_id
LIMIT $1;

-- name: ListPendingGrantDeposits :many
SELECT grant_id
FROM credit_grants
WHERE org_id = $1
  AND closed_at IS NULL
  AND ledger_posting_state IN ('pending', 'retryable_failed')
  AND ($2 = '' OR COALESCE(scope_product_id, $2) = $2 OR scope_type = 'account')
ORDER BY starts_at, grant_id;

-- name: GetGrantLedgerRowForUpdate :one
SELECT g.grant_id,
       g.org_id,
       g.scope_type,
       COALESCE(g.scope_product_id, '') AS scope_product_id,
       COALESCE(g.scope_bucket_id, '') AS scope_bucket_id,
       COALESCE(g.scope_sku_id, '') AS scope_sku_id,
       g.amount,
       g.source,
       g.source_reference_id,
       g.account_id::bytea AS account_id,
       g.deposit_transfer_id::bytea AS deposit_transfer_id,
       g.starts_at,
       COALESCE(g.scope_product_id, '') AS product_id
FROM credit_grants g
WHERE g.grant_id = $1
FOR UPDATE;

-- name: MarkGrantLedgerPostingInProgress :exec
UPDATE credit_grants
SET ledger_posting_state = 'in_progress',
    ledger_last_error = ''
WHERE grant_id = $1 AND ledger_posting_state IN ('pending', 'retryable_failed', 'in_progress');

-- name: MarkGrantLedgerPostingPosted :one
UPDATE credit_grants
SET ledger_posting_state = 'posted',
    ledger_posted_at = now(),
    ledger_last_error = ''
WHERE grant_id = $1 AND ledger_posting_state <> 'posted'
RETURNING org_id, COALESCE(scope_product_id, '') AS product_id, source, amount;

-- name: MarkGrantLedgerPostingFailed :one
UPDATE credit_grants
SET ledger_posting_state = 'retryable_failed',
    ledger_last_error = $2
WHERE grant_id = $1
RETURNING org_id, COALESCE(scope_product_id, '') AS product_id;

-- name: ListPostedGrantSnapshotsForReconcile :many
SELECT grant_id, org_id, COALESCE(scope_product_id, '') AS product_id, amount, account_id::bytea AS account_id
FROM credit_grants
WHERE closed_at IS NULL
  AND ledger_posting_state = 'posted'
ORDER BY starts_at, grant_id
LIMIT $1;

-- name: ListSettledGrantUsage :many
SELECT l.grant_id, SUM(l.amount_posted)::bigint AS amount_posted
FROM billing_window_ledger_legs l
JOIN billing_windows w ON w.window_id = l.window_id
WHERE w.state = 'settled'
  AND l.state = 'posted'
  AND l.grant_id = ANY($1::text[])
GROUP BY l.grant_id
ORDER BY l.grant_id;

-- name: InsertLedgerDriftEvent :exec
INSERT INTO billing_ledger_drift_events (
    drift_id, drift_kind, severity, aggregate_type, aggregate_id, pg_snapshot, tigerbeetle_snapshot
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (drift_id) DO NOTHING;
