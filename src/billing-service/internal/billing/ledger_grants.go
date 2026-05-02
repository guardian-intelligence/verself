package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/verself/billing-service/internal/billing/ledger"
	"github.com/verself/billing-service/internal/store"
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
	grantIDs, err := c.queries.ListPendingGrantDeposits(ctx, store.ListPendingGrantDepositsParams{OrgID: orgIDText(orgID), Column2: productID})
	if err != nil {
		return 0, fmt.Errorf("query pending grant deposits: %w", err)
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
	err := c.WithTx(ctx, "billing.ledger.grant_deposit.create_command", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		grant, err := c.loadGrantLedgerRowTx(ctx, q, grantID)
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
		if err := q.MarkGrantLedgerPostingInProgress(ctx, store.MarkGrantLedgerPostingInProgressParams{GrantID: grant.GrantID}); err != nil {
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

func (c *Client) loadGrantLedgerRowTx(ctx context.Context, q *store.Queries, grantID string) (grantLedgerRow, error) {
	stored, err := q.GetGrantLedgerRowForUpdate(ctx, store.GetGrantLedgerRowForUpdateParams{GrantID: grantID})
	if err != nil {
		return grantLedgerRow{}, fmt.Errorf("load grant ledger row %s: %w", grantID, err)
	}
	orgID, err := parseOrgID(stored.OrgID)
	if err != nil {
		return grantLedgerRow{}, err
	}
	accountID, err := ledger.IDFromBytes(stored.AccountID)
	if err != nil {
		return grantLedgerRow{}, fmt.Errorf("parse grant account id %s: %w", grantID, err)
	}
	depositID, err := ledger.IDFromBytes(stored.DepositTransferID)
	if err != nil {
		return grantLedgerRow{}, fmt.Errorf("parse grant deposit transfer id %s: %w", grantID, err)
	}
	row := grantLedgerRow{
		GrantID:           stored.GrantID,
		OrgID:             orgID,
		ProductID:         stored.ProductID,
		ScopeType:         stored.ScopeType,
		ScopeProductID:    stored.ScopeProductID,
		ScopeBucketID:     stored.ScopeBucketID,
		ScopeSKUID:        stored.ScopeSkuID,
		Amount:            checkedUint64FromInt64(stored.Amount, "stored grant amount"),
		Source:            stored.Source,
		SourceReferenceID: stored.SourceReferenceID,
		AccountID:         accountID,
		DepositID:         depositID,
		StartsAt:          stored.StartsAt.Time.UTC(),
	}
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
		row, err := q.MarkGrantLedgerPostingPosted(ctx, store.MarkGrantLedgerPostingPostedParams{GrantID: grantID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("mark grant ledger posting posted: %w", err)
		}
		orgID, err := parseOrgID(row.OrgID)
		if err != nil {
			return err
		}
		return appendEvent(ctx, tx, q, eventFact{
			EventType:     "grant_ledger_posted",
			AggregateType: "credit_grant",
			AggregateID:   grantID,
			OrgID:         orgID,
			ProductID:     row.ProductID,
			OccurredAt:    time.Now().UTC(),
			Payload:       map[string]any{"grant_id": grantID, "source": row.Source, "amount": row.Amount},
		})
	})
}

func (c *Client) markGrantLedgerPostingFailed(ctx context.Context, grantID string, cause error) error {
	return c.WithTx(ctx, "billing.ledger.grant_deposit.failed", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		row, err := q.MarkGrantLedgerPostingFailed(ctx, store.MarkGrantLedgerPostingFailedParams{GrantID: grantID, LedgerLastError: cause.Error()})
		if err != nil {
			return fmt.Errorf("mark grant ledger posting failed: %w", err)
		}
		orgID, err := parseOrgID(row.OrgID)
		if err != nil {
			return err
		}
		return appendEvent(ctx, tx, q, eventFact{
			EventType:     "grant_ledger_failed",
			AggregateType: "credit_grant",
			AggregateID:   grantID,
			OrgID:         orgID,
			ProductID:     row.ProductID,
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
