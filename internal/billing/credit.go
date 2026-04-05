package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

// DepositCredits uses a two-phase TigerBeetle transfer to coordinate PG and TB
// state. The pending transfer locks funds in the source pool; if the PG write
// fails or the process crashes, the pending transfer expires and funds return
// automatically. On retry, deterministic transfer IDs make every step idempotent.
//
// Flow:
//  1. PG INSERT grant row (serialization, returns grantID even on conflict)
//  2. TB CreateAccount (idempotent)
//  3. TB CreateTransfer PENDING (funds locked, not yet available to grant)
//  4. TB PostPendingTransfer (funds now in grant — atomicity point)
//  5. PG INSERT billing event
//
// For subscription/free_tier sources, idempotency is via the unique index on
// (subscription_id, period_start). For purchase/promo/refund sources,
// idempotency is via the task's idempotency_key plus deterministic transfer IDs.
func (c *Client) DepositCredits(ctx context.Context, taskID *TaskID, grant CreditGrant) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	sourceType, err := ParseGrantSourceType(grant.Source)
	if err != nil {
		return fmt.Errorf("deposit credits: %w", err)
	}

	if err := validateDepositPreconditions(sourceType, taskID, grant); err != nil {
		return err
	}

	fundingAccount, xferKind := depositFundingSource(sourceType)
	orgIDStr := strconv.FormatUint(uint64(grant.OrgID), 10)

	// Step 1: PG catalog row (serialization point).
	// Always returns a grantID — either from the new insert or the existing row.
	// This ensures retries use the same TB account as the original call.
	grantID := NewGrantID()
	grantIDStr := ulid.ULID(grantID).String()
	var returnedGrantIDStr string
	err = c.pg.QueryRowContext(ctx, `
		WITH inserted AS (
			INSERT INTO credit_grants (
				grant_id, org_id, product_id, amount, source,
				stripe_reference_id, subscription_id,
				period_start, period_end, expires_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT DO NOTHING
			RETURNING grant_id
		)
		SELECT grant_id FROM inserted
		UNION ALL
		SELECT grant_id FROM credit_grants
		WHERE subscription_id IS NOT DISTINCT FROM $7
		  AND period_start IS NOT DISTINCT FROM $8
		  AND org_id = $2
		  AND product_id = $3
		  AND source = $5
		LIMIT 1
	`,
		grantIDStr,
		orgIDStr,
		grant.ProductID,
		int64(grant.Amount),
		grant.Source,
		nullString(grant.StripeReferenceID),
		grant.SubscriptionID,
		grant.PeriodStart,
		grant.PeriodEnd,
		grant.ExpiresAt,
	).Scan(&returnedGrantIDStr)
	if err != nil {
		return fmt.Errorf("deposit credits: upsert grant row: %w", err)
	}

	// Use the returned grant ID (may differ from generated one on retry).
	if returnedGrantIDStr != grantIDStr {
		parsedULID, err := ulid.ParseStrict(returnedGrantIDStr)
		if err != nil {
			return fmt.Errorf("deposit credits: parse existing grant ULID %q: %w", returnedGrantIDStr, err)
		}
		grantID = GrantID(parsedULID)
		grantIDStr = returnedGrantIDStr
	}

	// Step 2: TB grant account (idempotent via AccountExists).
	if err := c.createGrantAccount(grantID, grant.OrgID, sourceType); err != nil {
		return fmt.Errorf("deposit credits: %w", err)
	}

	// Step 3: TB pending transfer — funds locked in source, not yet in grant.
	// If the process crashes after this step, the transfer expires after the
	// timeout and funds return to the source pool automatically.
	pendingID := depositTransferID(sourceType, taskID, grant, xferKind)
	if err := c.createTransfers([]types.Transfer{{
		ID:              pendingID.raw,
		DebitAccountID:  OperatorAccountID(fundingAccount).raw,
		CreditAccountID: GrantAccountID(grantID).raw,
		UserData64:      uint64(grant.OrgID),
		Code:            uint16(xferKind),
		Ledger:          1,
		Amount:          types.ToUint128(grant.Amount),
		Flags:           types.TransferFlags{Pending: true}.ToUint16(),
		Timeout:         3600, // 1 hour safety net for crash recovery
	}}); err != nil {
		return fmt.Errorf("deposit credits: pending transfer: %w", err)
	}

	// Step 4: Post the pending transfer — atomicity point.
	// After this succeeds, the grant is funded in TB regardless of
	// subsequent PG failures. Deterministic post ID ensures idempotency.
	postID := depositTransferID(sourceType, taskID, grant, KindDepositConfirm)
	if err := c.postPendingTransfer(pendingID, postID); err != nil {
		return fmt.Errorf("deposit credits: %w", err)
	}

	// Step 5: Billing event (audit trail). If this fails, the grant is still
	// funded correctly — the event can be reconstructed from TB transfer history.
	var taskIDVal *int64
	if taskID != nil {
		v := int64(*taskID)
		taskIDVal = &v
	}
	if _, err := c.pg.ExecContext(ctx, `
		INSERT INTO billing_events (org_id, event_type, subscription_id, grant_id, task_id, payload)
		VALUES ($1, 'credits_deposited', $2, $3, $4, $5::jsonb)
		ON CONFLICT DO NOTHING
	`,
		orgIDStr,
		grant.SubscriptionID,
		grantIDStr,
		taskIDVal,
		mustJSON(map[string]interface{}{"amount": grant.Amount, "source": grant.Source, "product_id": grant.ProductID}),
	); err != nil {
		return fmt.Errorf("deposit credits: log billing event: %w", err)
	}

	return nil
}

// ExpireCredits sweeps expired credit grants. For each grant where
// expires_at <= now() AND closed_at IS NULL, it drains the remaining balance
// via a BalancingDebit into ExpiredCredits (paid) or FreeTierExpense (free-tier),
// then sets closed_at in PostgreSQL.
//
// Partial-failure tolerant: accumulates errors and continues processing.
func (c *Client) ExpireCredits(ctx context.Context) (ExpireResult, error) {
	if err := ctx.Err(); err != nil {
		return ExpireResult{}, err
	}

	now := c.clock().UTC()
	rows, err := c.pg.QueryContext(ctx, `
		SELECT grant_id, org_id, source
		FROM credit_grants
		WHERE expires_at <= $1
		  AND closed_at IS NULL
		ORDER BY expires_at ASC, grant_id ASC
	`, now)
	if err != nil {
		return ExpireResult{}, fmt.Errorf("expire credits: query expired grants: %w", err)
	}
	defer rows.Close()

	type expiredGrant struct {
		grantID GrantID
		orgID   OrgID
		source  GrantSourceType
	}

	var grants []expiredGrant
	for rows.Next() {
		var (
			grantIDStr string
			orgIDStr   string
			sourceStr  string
		)
		if err := rows.Scan(&grantIDStr, &orgIDStr, &sourceStr); err != nil {
			return ExpireResult{}, fmt.Errorf("expire credits: scan grant row: %w", err)
		}

		parsedULID, err := ulid.ParseStrict(grantIDStr)
		if err != nil {
			return ExpireResult{}, fmt.Errorf("expire credits: parse grant ULID %q: %w", grantIDStr, err)
		}

		orgIDVal, err := strconv.ParseUint(orgIDStr, 10, 64)
		if err != nil {
			return ExpireResult{}, fmt.Errorf("expire credits: parse org_id %q: %w", orgIDStr, err)
		}

		sourceType, err := ParseGrantSourceType(sourceStr)
		if err != nil {
			return ExpireResult{}, fmt.Errorf("expire credits: grant %s: %w", grantIDStr, err)
		}

		grants = append(grants, expiredGrant{
			grantID: GrantID(parsedULID),
			orgID:   OrgID(orgIDVal),
			source:  sourceType,
		})
	}
	if err := rows.Err(); err != nil {
		return ExpireResult{}, fmt.Errorf("expire credits: iterate rows: %w", err)
	}

	result := ExpireResult{GrantsChecked: len(grants)}

	for _, g := range grants {
		expiredUnits, err := c.expireSingleGrant(ctx, g.grantID, g.orgID, g.source, now)
		if err != nil {
			result.GrantsFailed++
			result.Errors = append(result.Errors, fmt.Errorf("grant %x: %w", g.grantID, err))
			continue
		}
		result.GrantsExpired++
		result.UnitsExpired += expiredUnits
	}

	return result, nil
}

// expireSingleGrant uses a two-phase transfer to drain remaining balance from a
// grant. The pending BalancingDebit locks the remaining balance; PG is updated
// to closed; then the drain is posted. If PG update fails, the pending drain
// expires and the balance is restored — the next sweep retries cleanly.
func (c *Client) expireSingleGrant(ctx context.Context, grantID GrantID, orgID OrgID, source GrantSourceType, now time.Time) (uint64, error) {
	var sinkAccount OperatorAcctType
	if source.IsFreeTier() {
		sinkAccount = AcctFreeTierExpense
	} else {
		sinkAccount = AcctExpiredCredits
	}

	// Step 1: Pending BalancingDebit — locks remaining grant balance.
	// BalancingDebit clamps to available; Pending means funds are in
	// debits_pending, not yet posted. Timeout provides crash recovery.
	pendingID := CreditExpiryID(grantID)
	if err := c.createTransfers([]types.Transfer{{
		ID:              pendingID.raw,
		DebitAccountID:  GrantAccountID(grantID).raw,
		CreditAccountID: OperatorAccountID(sinkAccount).raw,
		Amount:          types.ToUint128(maxUint64),
		Ledger:          1,
		Code:            uint16(KindCreditExpiry),
		Flags:           types.TransferFlags{BalancingDebit: true, Pending: true}.ToUint16(),
		UserData64:      uint64(orgID),
		Timeout:         3600,
	}}); err != nil {
		return 0, fmt.Errorf("expiry pending transfer: %w", err)
	}

	// Read back the clamped amount to know how much will be expired.
	transfer, err := c.lookupTransfer(pendingID)
	if err != nil {
		return 0, fmt.Errorf("lookup expiry transfer: %w", err)
	}
	expiredAmount, err := uint128ToUint64(transfer.Amount)
	if err != nil {
		return 0, fmt.Errorf("expiry transfer amount: %w", err)
	}

	// Step 2: Close the grant in PostgreSQL.
	orgIDStr := strconv.FormatUint(uint64(orgID), 10)
	grantIDStr := ulid.ULID(grantID).String()
	if _, err := c.pg.ExecContext(ctx, `
		UPDATE credit_grants SET closed_at = $1 WHERE grant_id = $2 AND closed_at IS NULL
	`, now, grantIDStr); err != nil {
		return 0, fmt.Errorf("close grant row: %w", err)
	}

	// Step 3: Post the drain — atomicity point.
	// After this, the expired balance is permanently moved to the sink.
	postID := CreditExpiryPostID(grantID)
	if err := c.postPendingTransfer(pendingID, postID); err != nil {
		return 0, fmt.Errorf("expiry confirm: %w", err)
	}

	// Step 4: Billing event (audit trail).
	if _, err := c.pg.ExecContext(ctx, `
		INSERT INTO billing_events (org_id, event_type, grant_id, payload)
		VALUES ($1, 'credits_expired', $2, $3::jsonb)
		ON CONFLICT DO NOTHING
	`,
		orgIDStr,
		grantIDStr,
		mustJSON(map[string]interface{}{"expired_units": expiredAmount, "source": source.String()}),
	); err != nil {
		return 0, fmt.Errorf("log billing event: %w", err)
	}

	return expiredAmount, nil
}

// RecordLicensedCharge recognizes a recurring licensed invoice in TigerBeetle.
// It posts a StripeHolding → Revenue transfer and logs a billing event.
// No credit_grants row is created.
func (c *Client) RecordLicensedCharge(ctx context.Context, taskID TaskID, charge LicensedCharge) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	orgIDStr := strconv.FormatUint(uint64(charge.OrgID), 10)

	// TigerBeetle transfer: StripeHolding → Revenue.
	transferID := StripeDepositID(taskID, KindSubscriptionDeposit)
	if err := c.createTransfers([]types.Transfer{{
		ID:              transferID.raw,
		DebitAccountID:  OperatorAccountID(AcctStripeHolding).raw,
		CreditAccountID: OperatorAccountID(AcctRevenue).raw,
		Amount:          types.ToUint128(charge.Amount),
		Ledger:          1,
		Code:            uint16(KindSubscriptionDeposit),
		UserData64:      uint64(charge.OrgID),
	}}); err != nil {
		return fmt.Errorf("record licensed charge: transfer: %w", err)
	}

	// Log billing event.
	if _, err := c.pg.ExecContext(ctx, `
		INSERT INTO billing_events (org_id, event_type, subscription_id, task_id, payload)
		VALUES ($1, 'licensed_charge_recorded', $2, $3, $4::jsonb)
	`,
		orgIDStr,
		charge.SubscriptionID,
		int64(taskID),
		mustJSON(map[string]interface{}{
			"amount":            charge.Amount,
			"product_id":        charge.ProductID,
			"stripe_invoice_id": charge.StripeInvoiceID,
			"period_start":      charge.PeriodStart.UTC().Format(time.RFC3339),
			"period_end":        charge.PeriodEnd.UTC().Format(time.RFC3339),
		}),
	); err != nil {
		return fmt.Errorf("record licensed charge: log billing event: %w", err)
	}

	return nil
}

// validateDepositPreconditions checks that the required fields are set for
// the given source type.
func validateDepositPreconditions(source GrantSourceType, taskID *TaskID, grant CreditGrant) error {
	switch source {
	case SourceSubscription, SourceFreeTier:
		if grant.SubscriptionID == nil {
			return fmt.Errorf("deposit credits: subscription_id required for %s source", source)
		}
		if grant.PeriodStart == nil {
			return fmt.Errorf("deposit credits: period_start required for %s source", source)
		}
	case SourcePurchase, SourcePromo, SourceRefund:
		if taskID == nil {
			return fmt.Errorf("deposit credits: task_id required for %s source", source)
		}
	}
	if grant.Amount == 0 {
		return fmt.Errorf("deposit credits: amount must be positive")
	}
	return nil
}

// depositFundingSource returns the operator account to debit and the transfer
// kind for a given grant source.
func depositFundingSource(source GrantSourceType) (OperatorAcctType, XferKind) {
	switch source {
	case SourceFreeTier:
		return AcctFreeTierPool, KindFreeTierReset
	case SourceSubscription:
		return AcctStripeHolding, KindSubscriptionDeposit
	case SourcePurchase:
		return AcctStripeHolding, KindStripeDeposit
	case SourcePromo:
		return AcctPromoPool, KindPromoCredit
	case SourceRefund:
		return AcctStripeHolding, KindStripeDeposit
	default:
		return AcctStripeHolding, KindStripeDeposit
	}
}

// depositTransferID constructs the deterministic transfer ID for a deposit.
func depositTransferID(source GrantSourceType, taskID *TaskID, grant CreditGrant, kind XferKind) TransferID {
	switch source {
	case SourceSubscription, SourceFreeTier:
		return SubscriptionPeriodID(SubscriptionID(*grant.SubscriptionID), *grant.PeriodStart, kind)
	default:
		return StripeDepositID(*taskID, kind)
	}
}

// maxUint64 is used as the requested amount for BalancingDebit transfers,
// which clamp to the available balance.
const maxUint64 = ^uint64(0)

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("billing: mustJSON: %v", err))
	}
	return string(b)
}
