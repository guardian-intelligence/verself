package billing

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/oklog/ulid/v2"
	"github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

// HandleDispute processes a chargeback by debiting the grant(s) funded by the
// disputed Stripe payment first.
//
// Postconditions:
//   - TigerBeetle transfer(s): debit the original disputed grant(s) first,
//     then other eligible grants in waterfall order if needed,
//     crediting StripeHolding
//   - If org's credit balance insufficient to cover dispute: all org
//     subscriptions suspended via SuspendOrg
//   - billing_events row logged with event_type='dispute_opened'
//   - credit_grants remains immutable; dispute attribution comes from
//     stripe_reference_id and TigerBeetle transfer history
//
// Idempotency: Transfer ID derived from taskID + KindDisputeDebit via DisputeDebitID.
func (c *Client) HandleDispute(ctx context.Context, orgID OrgID, taskID TaskID, stripePaymentIntentID string, disputeAmount uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if disputeAmount == 0 {
		return fmt.Errorf("handle dispute: amount must be positive")
	}

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	// Load all active grants for the org (across all products).
	// Grants matching the disputed payment's stripe_reference_id come first.
	grants, err := c.loadOrgGrantsForDispute(ctx, orgIDStr, stripePaymentIntentID)
	if err != nil {
		return fmt.Errorf("handle dispute: %w", err)
	}

	// Debit waterfall: each grant gets a BalancingDebit transfer from the
	// grant account into StripeHolding for up to the remaining dispute amount.
	var (
		totalDebited uint64
		grantIdx     uint8
	)
	for _, g := range grants {
		if totalDebited >= disputeAmount {
			break
		}
		if g.available == 0 {
			continue
		}

		requested := disputeAmount - totalDebited
		if requested > g.available {
			requested = g.available
		}

		transferID := DisputeDebitID(taskID, grantIdx)
		if err := c.createTransfers([]types.Transfer{{
			ID:              transferID.raw,
			DebitAccountID:  GrantAccountID(g.grantID).raw,
			CreditAccountID: OperatorAccountID(AcctStripeHolding).raw,
			Amount:          types.ToUint128(requested),
			Ledger:          1,
			Code:            uint16(KindDisputeDebit),
			Flags:           types.TransferFlags{BalancingDebit: true}.ToUint16(),
			UserData64:      uint64(orgID),
		}}); err != nil {
			return fmt.Errorf("handle dispute: debit grant %x: %w", g.grantID, err)
		}

		// Read back the clamped amount (concurrent settlements may have reduced available).
		transfer, err := c.lookupTransfer(transferID)
		if err != nil {
			return fmt.Errorf("handle dispute: lookup transfer %x: %w", g.grantID, err)
		}
		clamped, err := uint128ToUint64(transfer.Amount)
		if err != nil {
			return fmt.Errorf("handle dispute: transfer amount %x: %w", g.grantID, err)
		}

		totalDebited += clamped
		grantIdx++
	}

	// If the org couldn't cover the full dispute amount, suspend all subscriptions.
	if totalDebited < disputeAmount {
		if err := c.SuspendOrg(ctx, orgID, fmt.Sprintf("dispute debit shortfall: debited %d of %d", totalDebited, disputeAmount)); err != nil {
			return fmt.Errorf("handle dispute: %w", err)
		}
	}

	// Log billing event.
	if _, err := c.pg.ExecContext(ctx, `
		INSERT INTO billing_events (org_id, event_type, task_id, payload)
		VALUES ($1, 'dispute_opened', $2, $3::jsonb)
	`,
		orgIDStr,
		int64(taskID),
		mustJSON(map[string]interface{}{
			"dispute_amount_ledger_units": disputeAmount,
			"total_debited_ledger_units":  totalDebited,
			"grants_debited":              int(grantIdx),
			"org_suspended":               totalDebited < disputeAmount,
		}),
	); err != nil {
		return fmt.Errorf("handle dispute: log billing event: %w", err)
	}

	return nil
}

// disputeGrant holds the data needed to debit a grant during dispute processing.
type disputeGrant struct {
	grantID   GrantID
	source    GrantSourceType
	available uint64
}

// loadOrgGrantsForDispute loads all active grants for an org across all products.
// Grants whose stripe_reference_id matches the disputed payment intent are returned
// first (spec §2.7: "debit the original disputed grant(s) first"), followed by all
// other active grants in grant_id order.
func (c *Client) loadOrgGrantsForDispute(ctx context.Context, orgIDStr string, stripePaymentIntentID string) ([]disputeGrant, error) {
	rows, err := c.pg.QueryContext(ctx, `
		SELECT grant_id, source, stripe_reference_id
		FROM credit_grants
		WHERE org_id = $1
		  AND closed_at IS NULL
		ORDER BY grant_id ASC
	`, orgIDStr)
	if err != nil {
		return nil, fmt.Errorf("query org grants: %w", err)
	}
	defer rows.Close()

	type grantRow struct {
		grantID          GrantID
		source           GrantSourceType
		matchesDisputed  bool
	}

	var grantRows []grantRow
	var accountIDs []types.Uint128
	for rows.Next() {
		var (
			grantIDStr string
			sourceStr  string
			stripeRef  sql.NullString
		)
		if err := rows.Scan(&grantIDStr, &sourceStr, &stripeRef); err != nil {
			return nil, fmt.Errorf("scan grant row: %w", err)
		}

		parsedULID, err := ulid.ParseStrict(grantIDStr)
		if err != nil {
			return nil, fmt.Errorf("parse grant ULID %q: %w", grantIDStr, err)
		}

		sourceType, err := ParseGrantSourceType(sourceStr)
		if err != nil {
			return nil, fmt.Errorf("grant %s: %w", grantIDStr, err)
		}

		matches := stripeRef.Valid && stripePaymentIntentID != "" && stripeRef.String == stripePaymentIntentID

		grantID := GrantID(parsedULID)
		grantRows = append(grantRows, grantRow{grantID: grantID, source: sourceType, matchesDisputed: matches})
		accountIDs = append(accountIDs, GrantAccountID(grantID).raw)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate grant rows: %w", err)
	}
	if len(grantRows) == 0 {
		return nil, nil
	}

	accounts, err := c.tb.LookupAccounts(accountIDs)
	if err != nil {
		return nil, fmt.Errorf("lookup grant accounts: %w", err)
	}

	accountByID := make(map[types.Uint128]types.Account, len(accounts))
	for _, account := range accounts {
		accountByID[account.ID] = account
	}

	// Split into disputed-first and others.
	var disputed, others []disputeGrant
	for _, row := range grantRows {
		account, ok := accountByID[GrantAccountID(row.grantID).raw]
		if !ok {
			continue
		}

		available, err := availableFromAccount(account)
		if err != nil {
			return nil, fmt.Errorf("grant %x available: %w", row.grantID, err)
		}

		g := disputeGrant{
			grantID:   row.grantID,
			source:    row.source,
			available: available,
		}

		if row.matchesDisputed {
			disputed = append(disputed, g)
		} else {
			others = append(others, g)
		}
	}

	return append(disputed, others...), nil
}
