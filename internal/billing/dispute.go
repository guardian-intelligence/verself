package billing

import (
	"context"
	"fmt"
	"strconv"

	"github.com/oklog/ulid/v2"
	"github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

// HandleDispute processes a chargeback by debiting the grant(s) funded by the
// disputed Stripe payment first, then other eligible grants in reverse
// waterfall order if needed, crediting StripeHolding.
//
// If the org's total credit balance is insufficient to cover the dispute amount,
// all org subscriptions are suspended via SuspendOrg.
//
// Idempotency: Transfer IDs are derived from (taskID, grantIdx, KindDisputeDebit).
// TigerBeetle returns TransferExists on replay.
func (c *Client) HandleDispute(ctx context.Context, orgID OrgID, taskID TaskID, stripePaymentIntentID string, disputeAmount uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	// Step 1: Load all open grants for this org (across all products).
	// Disputes are org-level, not product-scoped.
	// Grants funded by the disputed payment are placed first per spec §2.7:
	// "debit the original disputed grant(s) first, then other eligible
	// grants in reverse waterfall order if needed".
	grants, err := c.loadOrgGrantsForDispute(ctx, orgID, stripePaymentIntentID)
	if err != nil {
		return fmt.Errorf("handle dispute: %w", err)
	}

	// Step 2: Debit grants via BalancingDebit into StripeHolding.
	// Each transfer is clamped to the grant's available balance.
	var totalDebited uint64
	var grantIdx uint8

	for i := range grants {
		if totalDebited >= disputeAmount {
			break
		}
		if grants[i].available == 0 {
			continue
		}

		remaining := disputeAmount - totalDebited
		requestAmount := remaining
		if grants[i].available < requestAmount {
			requestAmount = grants[i].available
		}

		transferID := DisputeDebitID(taskID, grantIdx)
		if err := c.createTransfers([]types.Transfer{{
			ID:              transferID.raw,
			DebitAccountID:  GrantAccountID(grants[i].grantID).raw,
			CreditAccountID: OperatorAccountID(AcctStripeHolding).raw,
			Amount:          types.ToUint128(requestAmount),
			Ledger:          1,
			Code:            uint16(KindDisputeDebit),
			Flags:           types.TransferFlags{BalancingDebit: true}.ToUint16(),
			UserData64:      uint64(orgID),
		}}); err != nil {
			return fmt.Errorf("handle dispute: debit grant %x: %w", grants[i].grantID, err)
		}

		// Read back the clamped amount.
		transfer, err := c.lookupTransfer(transferID)
		if err != nil {
			return fmt.Errorf("handle dispute: lookup transfer for grant %x: %w", grants[i].grantID, err)
		}
		debited, err := uint128ToUint64(transfer.Amount)
		if err != nil {
			return fmt.Errorf("handle dispute: transfer amount: %w", err)
		}

		totalDebited += debited
		grantIdx++
	}

	// Step 3: If we couldn't cover the full dispute, suspend the org.
	suspended := totalDebited < disputeAmount
	if suspended {
		if err := c.SuspendOrg(ctx, orgID, fmt.Sprintf("dispute shortfall: debited %d of %d", totalDebited, disputeAmount)); err != nil {
			return fmt.Errorf("handle dispute: %w", err)
		}
	}

	// Step 4: Log billing event.
	if _, err := c.pg.ExecContext(ctx, `
		INSERT INTO billing_events (org_id, event_type, task_id, payload)
		VALUES ($1, 'dispute_opened', $2, $3::jsonb)
	`,
		orgIDStr,
		int64(taskID),
		mustJSON(map[string]interface{}{
			"dispute_amount":           disputeAmount,
			"total_debited":            totalDebited,
			"grants_debited":           int(grantIdx),
			"org_suspended":            suspended,
			"stripe_payment_intent_id": stripePaymentIntentID,
		}),
	); err != nil {
		return fmt.Errorf("handle dispute: log billing event: %w", err)
	}

	return nil
}

// loadOrgGrantsForDispute loads all open grants for an org across all products,
// with grants funded by the disputed payment (matching stripe_reference_id)
// sorted first. Remaining grants follow in expiry ASC order.
func (c *Client) loadOrgGrantsForDispute(ctx context.Context, orgID OrgID, stripePaymentIntentID string) ([]orgGrant, error) {
	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	// Grants matching the disputed payment come first (disputed = TRUE sorts
	// before FALSE). Within each group, soonest-expiring first.
	rows, err := c.pg.QueryContext(ctx, `
		SELECT grant_id, source
		FROM credit_grants
		WHERE org_id = $1
		  AND closed_at IS NULL
		ORDER BY (stripe_reference_id = $2) DESC,
		         expires_at ASC NULLS LAST,
		         grant_id ASC
	`, orgIDStr, stripePaymentIntentID)
	if err != nil {
		return nil, fmt.Errorf("query org grants: %w", err)
	}
	defer rows.Close()

	type grantRow struct {
		grantID GrantID
		source  GrantSourceType
	}

	var grantRows []grantRow
	var accountIDs []types.Uint128
	for rows.Next() {
		var (
			grantIDStr string
			sourceStr  string
		)
		if err := rows.Scan(&grantIDStr, &sourceStr); err != nil {
			return nil, fmt.Errorf("scan org grant: %w", err)
		}

		parsedULID, err := ulid.ParseStrict(grantIDStr)
		if err != nil {
			return nil, fmt.Errorf("parse grant ULID %q: %w", grantIDStr, err)
		}

		sourceType, err := ParseGrantSourceType(sourceStr)
		if err != nil {
			return nil, fmt.Errorf("grant %s: %w", grantIDStr, err)
		}

		grantID := GrantID(parsedULID)
		grantRows = append(grantRows, grantRow{grantID: grantID, source: sourceType})
		accountIDs = append(accountIDs, GrantAccountID(grantID).raw)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate org grants: %w", err)
	}
	if len(grantRows) == 0 {
		return nil, nil
	}

	accounts, err := c.tb.LookupAccounts(accountIDs)
	if err != nil {
		return nil, fmt.Errorf("lookup org grant accounts: %w", err)
	}

	accountByID := make(map[types.Uint128]types.Account, len(accounts))
	for _, acct := range accounts {
		accountByID[acct.ID] = acct
	}

	result := make([]orgGrant, 0, len(grantRows))
	for _, row := range grantRows {
		acct, ok := accountByID[GrantAccountID(row.grantID).raw]
		if !ok {
			return nil, fmt.Errorf("missing TB account for grant %x", row.grantID)
		}

		available, err := availableFromAccount(acct)
		if err != nil {
			return nil, fmt.Errorf("grant %x available: %w", row.grantID, err)
		}

		result = append(result, orgGrant{
			grantID:   row.grantID,
			source:    row.source,
			available: available,
		})
	}

	return result, nil
}

type orgGrant struct {
	grantID   GrantID
	source    GrantSourceType
	available uint64
}
