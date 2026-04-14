package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/forge-metal/billing-service/internal/billing/ledger"
	"github.com/forge-metal/billing-service/internal/store"
)

type grantLedgerRow struct {
	GrantID           string
	OrgID             OrgID
	ProductID         string
	ScopeType         string
	ScopeProductID    string
	ScopeBucketID     string
	ScopeSKUID        string
	Amount            uint64
	Source            string
	SourceReferenceID string
	AccountID         ledger.ID
	DepositID         ledger.ID
	StartsAt          time.Time
}

func (c *Client) PostPendingGrantDeposits(ctx context.Context, orgID OrgID, productID string) (int, error) {
	if _, err := c.requireLedger(); err != nil {
		return 0, err
	}
	rows, err := c.pg.Query(ctx, `
		SELECT grant_id
		FROM credit_grants
		WHERE org_id = $1
		  AND closed_at IS NULL
		  AND ledger_posting_state IN ('pending','retryable_failed')
		  AND ($2 = '' OR COALESCE(scope_product_id, $2) = $2 OR scope_type = 'account')
		ORDER BY starts_at, grant_id
	`, orgIDText(orgID), productID)
	if err != nil {
		return 0, fmt.Errorf("query pending grant deposits: %w", err)
	}
	defer rows.Close()
	grantIDs := []string{}
	for rows.Next() {
		var grantID string
		if err := rows.Scan(&grantID); err != nil {
			return 0, fmt.Errorf("scan pending grant deposit: %w", err)
		}
		grantIDs = append(grantIDs, grantID)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	posted := 0
	for _, grantID := range grantIDs {
		if err := c.postGrantDeposit(ctx, grantID); err != nil {
			return posted, err
		}
		posted++
	}
	return posted, nil
}

func (c *Client) postGrantDeposit(ctx context.Context, grantID string) error {
	var commandID string
	err := c.WithTx(ctx, "billing.ledger.grant_deposit.create_command", func(ctx context.Context, tx pgx.Tx, _ *store.Queries) error {
		grant, err := c.loadGrantLedgerRowTx(ctx, tx, grantID)
		if err != nil {
			return err
		}
		if grant.AccountID.IsZero() || grant.DepositID.IsZero() {
			return fmt.Errorf("grant %s missing ledger account or deposit transfer id", grantID)
		}
		operators, err := c.operatorLedgerAccountsTx(ctx, tx)
		if err != nil {
			return err
		}
		payload, err := c.grantDepositPayload(operators, grant)
		if err != nil {
			return err
		}
		commandID, _, err = c.createLedgerCommandTx(ctx, tx, "grant_deposit", "credit_grant", grant.GrantID, grant.OrgID, grant.ProductID, "grant_deposit:"+grant.GrantID, payload)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE credit_grants
			SET ledger_posting_state = 'in_progress', ledger_last_error = ''
			WHERE grant_id = $1 AND ledger_posting_state IN ('pending','retryable_failed','in_progress')
		`, grant.GrantID)
		if err != nil {
			return fmt.Errorf("mark grant ledger posting in progress: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := c.dispatchLedgerCommand(ctx, commandID); err != nil {
		_ = c.markGrantLedgerPostingFailed(ctx, grantID, err)
		return err
	}
	return c.markGrantLedgerPostingPosted(ctx, grantID)
}

func (c *Client) loadGrantLedgerRowTx(ctx context.Context, tx pgx.Tx, grantID string) (grantLedgerRow, error) {
	var row grantLedgerRow
	var orgText string
	var accountRaw, depositRaw []byte
	var amount int64
	err := tx.QueryRow(ctx, `
		SELECT g.grant_id, g.org_id, g.scope_type, COALESCE(g.scope_product_id,''), COALESCE(g.scope_bucket_id,''), COALESCE(g.scope_sku_id,''),
		       g.amount, g.source, g.source_reference_id, g.account_id, g.deposit_transfer_id, g.starts_at,
		       COALESCE(g.scope_product_id, '')
		FROM credit_grants g
		WHERE g.grant_id = $1
		FOR UPDATE
	`, grantID).Scan(&row.GrantID, &orgText, &row.ScopeType, &row.ScopeProductID, &row.ScopeBucketID, &row.ScopeSKUID, &amount, &row.Source, &row.SourceReferenceID, &accountRaw, &depositRaw, &row.StartsAt, &row.ProductID)
	if err != nil {
		return grantLedgerRow{}, fmt.Errorf("load grant ledger row %s: %w", grantID, err)
	}
	orgID, err := parseOrgID(orgText)
	if err != nil {
		return grantLedgerRow{}, err
	}
	accountID, err := ledger.IDFromBytes(accountRaw)
	if err != nil {
		return grantLedgerRow{}, fmt.Errorf("parse grant account id %s: %w", grantID, err)
	}
	depositID, err := ledger.IDFromBytes(depositRaw)
	if err != nil {
		return grantLedgerRow{}, fmt.Errorf("parse grant deposit transfer id %s: %w", grantID, err)
	}
	row.OrgID = orgID
	row.Amount = uint64(amount)
	row.AccountID = accountID
	row.DepositID = depositID
	return row, nil
}

func (c *Client) grantDepositPayload(operators map[string]ledger.ID, grant grantLedgerRow) (ledger.CommandPayload, error) {
	account, err := ledger.CustomerGrantAccount(grant.AccountID, uint64(grant.OrgID), grant.Source)
	if err != nil {
		return ledger.CommandPayload{}, err
	}
	payload := ledger.CommandPayload{Accounts: []ledger.AccountPayload{account}}
	businessMS := unixMillis(grant.StartsAt)
	if grant.Source == "purchase" {
		externalID := operators["operator_stripe_external"]
		holdingID := operators["operator_stripe_holding"]
		stripeTransferID := ledger.NewID()
		payload.Transfers = []ledger.TransferPayload{
			ledger.StripePaymentInTransfer(stripeTransferID, externalID, holdingID, grant.Amount, grant.DepositID, businessMS),
			ledger.GrantDepositTransfer(grant.DepositID, holdingID, grant.AccountID, grant.Amount, grant.DepositID, businessMS),
		}
		ledger.LinkTransfers(payload.Transfers)
		return payload, nil
	}
	fundingKey, err := ledger.GrantFundingAccountKey(grant.Source)
	if err != nil {
		return ledger.CommandPayload{}, err
	}
	fundingID, ok := operators[fundingKey]
	if !ok {
		return ledger.CommandPayload{}, fmt.Errorf("operator funding account %s is not bootstrapped", fundingKey)
	}
	payload.Transfers = []ledger.TransferPayload{ledger.GrantDepositTransfer(grant.DepositID, fundingID, grant.AccountID, grant.Amount, grant.DepositID, businessMS)}
	return payload, nil
}

func (c *Client) markGrantLedgerPostingPosted(ctx context.Context, grantID string) error {
	return c.WithTx(ctx, "billing.ledger.grant_deposit.posted", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		var orgText, productID, source string
		var amount int64
		err := tx.QueryRow(ctx, `
			UPDATE credit_grants
			SET ledger_posting_state = 'posted', ledger_posted_at = now(), ledger_last_error = ''
			WHERE grant_id = $1 AND ledger_posting_state <> 'posted'
			RETURNING org_id, COALESCE(scope_product_id,''), source, amount
		`, grantID).Scan(&orgText, &productID, &source, &amount)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("mark grant ledger posting posted: %w", err)
		}
		orgID, err := parseOrgID(orgText)
		if err != nil {
			return err
		}
		return appendEvent(ctx, tx, q, eventFact{
			EventType:     "grant_ledger_posted",
			AggregateType: "credit_grant",
			AggregateID:   grantID,
			OrgID:         orgID,
			ProductID:     productID,
			OccurredAt:    time.Now().UTC(),
			Payload:       map[string]any{"grant_id": grantID, "source": source, "amount": amount},
		})
	})
}

func (c *Client) markGrantLedgerPostingFailed(ctx context.Context, grantID string, cause error) error {
	return c.WithTx(ctx, "billing.ledger.grant_deposit.failed", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		var orgText, productID string
		err := tx.QueryRow(ctx, `
			UPDATE credit_grants
			SET ledger_posting_state = 'retryable_failed', ledger_last_error = $2
			WHERE grant_id = $1
			RETURNING org_id, COALESCE(scope_product_id,'')
		`, grantID, cause.Error()).Scan(&orgText, &productID)
		if err != nil {
			return fmt.Errorf("mark grant ledger posting failed: %w", err)
		}
		orgID, err := parseOrgID(orgText)
		if err != nil {
			return err
		}
		return appendEvent(ctx, tx, q, eventFact{
			EventType:     "grant_ledger_failed",
			AggregateType: "credit_grant",
			AggregateID:   grantID,
			OrgID:         orgID,
			ProductID:     productID,
			OccurredAt:    time.Now().UTC(),
			Payload:       map[string]any{"grant_id": grantID, "error": cause.Error()},
		})
	})
}

func unixMillis(t time.Time) uint64 {
	if t.IsZero() {
		t = time.Now().UTC()
	}
	if t.Before(time.Unix(0, 0)) {
		return 0
	}
	return uint64(t.UTC().UnixNano() / int64(time.Millisecond))
}
