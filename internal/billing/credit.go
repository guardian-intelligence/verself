package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

// DepositCredits inserts the PostgreSQL catalog row first (serialization point),
// then creates the TigerBeetle grant account and funds it.
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

	grantID := NewGrantID()
	grantIDStr := ulid.ULID(grantID).String()
	orgIDStr := strconv.FormatUint(uint64(grant.OrgID), 10)

	// Step 1: PG catalog row (serialization point).
	// For subscription/free_tier, the unique index on (subscription_id, period_start)
	// prevents duplicate grants. ON CONFLICT DO NOTHING means another writer won the race.
	var inserted bool
	err = c.pg.QueryRowContext(ctx, `
		INSERT INTO credit_grants (
			grant_id, org_id, product_id, amount, source,
			stripe_reference_id, subscription_id,
			period_start, period_end, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT DO NOTHING
		RETURNING true
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
	).Scan(&inserted)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("deposit credits: insert grant row: %w", err)
	}
	if !inserted {
		// Another writer won the race on the unique index — idempotent success.
		return nil
	}

	// Step 2: Create TigerBeetle grant account.
	if err := c.createGrantAccount(grantID, grant.OrgID, sourceType); err != nil {
		return fmt.Errorf("deposit credits: %w", err)
	}

	// Step 3: Fund the grant account.
	transferID := depositTransferID(sourceType, taskID, grant, xferKind)
	if err := c.createTransfers([]types.Transfer{{
		ID:              transferID.raw,
		DebitAccountID:  OperatorAccountID(fundingAccount).raw,
		CreditAccountID: GrantAccountID(grantID).raw,
		UserData64:      uint64(grant.OrgID),
		Code:            uint16(xferKind),
		Ledger:          1,
		Amount:          types.ToUint128(grant.Amount),
	}}); err != nil {
		return fmt.Errorf("deposit credits: fund grant: %w", err)
	}

	// Step 4: Log billing event.
	var taskIDVal *int64
	if taskID != nil {
		v := int64(*taskID)
		taskIDVal = &v
	}
	if _, err := c.pg.ExecContext(ctx, `
		INSERT INTO billing_events (org_id, event_type, subscription_id, grant_id, task_id, payload)
		VALUES ($1, 'credits_deposited', $2, $3, $4, $5::jsonb)
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

func (c *Client) expireSingleGrant(ctx context.Context, grantID GrantID, orgID OrgID, source GrantSourceType, now time.Time) (uint64, error) {
	// Determine sink account based on source type.
	var sinkAccount OperatorAcctType
	if source.IsFreeTier() {
		sinkAccount = AcctFreeTierExpense
	} else {
		sinkAccount = AcctExpiredCredits
	}

	// Step 1: BalancingDebit from grant account into sink.
	// BalancingDebit clamps the transfer amount to the available balance,
	// handling the case where concurrent settlements consumed some of the grant.
	transferID := CreditExpiryID(grantID)
	if err := c.createTransfers([]types.Transfer{{
		ID:              transferID.raw,
		DebitAccountID:  GrantAccountID(grantID).raw,
		CreditAccountID: OperatorAccountID(sinkAccount).raw,
		Amount:          types.ToUint128(maxUint64),
		Ledger:          1,
		Code:            uint16(KindCreditExpiry),
		Flags:           types.TransferFlags{BalancingDebit: true}.ToUint16(),
		UserData64:      uint64(orgID),
	}}); err != nil {
		return 0, fmt.Errorf("expiry transfer: %w", err)
	}

	// Read back the clamped amount to know how much was actually expired.
	transfer, err := c.lookupTransfer(transferID)
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

	// Step 3: Log billing event.
	if _, err := c.pg.ExecContext(ctx, `
		INSERT INTO billing_events (org_id, event_type, grant_id, payload)
		VALUES ($1, 'credits_expired', $2, $3::jsonb)
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
