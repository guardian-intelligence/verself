package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/verself/billing-service/internal/billing/ledger"
	"github.com/verself/billing-service/internal/store"
)

func (c *Client) ReconcileLedger(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 1000
	}
	if _, err := c.requireLedger(); err != nil {
		return 0, err
	}
	if err := c.reconcileOperatorLedgerAccounts(ctx); err != nil {
		return 0, err
	}
	return c.reconcileGrantLedgerBalances(ctx, limit)
}

func (c *Client) reconcileOperatorLedgerAccounts(ctx context.Context) error {
	var ids []ledger.ID
	err := c.WithTx(ctx, "billing.ledger.reconcile.operator_accounts", func(ctx context.Context, tx pgx.Tx, _ *store.Queries) error {
		operators, err := c.operatorLedgerAccountsTx(ctx, tx)
		if err != nil {
			return err
		}
		for _, id := range operators {
			ids = append(ids, id)
		}
		return nil
	})
	if err != nil {
		return err
	}
	found, err := c.ledger.LookupAccountIDs(ctx, ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, ok := found[id]; !ok {
			return c.recordLedgerDrift(ctx, "operator_account_missing", "critical", "ledger_account", id.String(), map[string]any{"account_id": id.String()}, map[string]any{})
		}
	}
	return nil
}

func (c *Client) reconcileGrantLedgerBalances(ctx context.Context, limit int) (int, error) {
	rows, err := c.queries.ListPostedGrantSnapshotsForReconcile(ctx, store.ListPostedGrantSnapshotsForReconcileParams{Limit: int32(limit)})
	if err != nil {
		return 0, fmt.Errorf("query grants for ledger reconcile: %w", err)
	}
	type grantSnapshot struct {
		GrantID   string
		OrgText   string
		ProductID string
		Amount    uint64
		AccountID ledger.ID
	}
	grants := []grantSnapshot{}
	ids := []ledger.ID{}
	grantIDs := []string{}
	for _, row := range rows {
		id, err := ledger.IDFromBytes(row.AccountID)
		if err != nil {
			return 0, err
		}
		grant := grantSnapshot{
			GrantID:   row.GrantID,
			OrgText:   row.OrgID,
			ProductID: row.ProductID,
			Amount:    uint64(row.Amount),
			AccountID: id,
		}
		grants = append(grants, grant)
		ids = append(ids, id)
		grantIDs = append(grantIDs, grant.GrantID)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	balances, err := c.ledger.LookupBalances(ctx, ids)
	if err != nil {
		return 0, err
	}
	settledUsage, err := c.settledGrantUsage(ctx, grantIDs)
	if err != nil {
		return 0, err
	}
	drifts := 0
	for _, grant := range grants {
		balance, ok := balances[grant.AccountID]
		if !ok {
			drifts++
			if err := c.recordLedgerDrift(ctx, "grant_account_missing", "critical", "credit_grant", grant.GrantID, map[string]any{"grant_id": grant.GrantID, "account_id": grant.AccountID.String()}, map[string]any{}); err != nil {
				return drifts, err
			}
			continue
		}
		total := balance.Available + balance.Pending + balance.Spent
		if total != grant.Amount {
			drifts++
			if err := c.recordLedgerDrift(ctx, "grant_balance_mismatch", "warning", "credit_grant", grant.GrantID, map[string]any{"grant_id": grant.GrantID, "amount": grant.Amount}, map[string]any{"available": balance.Available, "pending": balance.Pending, "spent": balance.Spent, "total": total}); err != nil {
				return drifts, err
			}
		}
		if balance.Pending != 0 {
			drifts++
			if err := c.recordLedgerDrift(ctx, "grant_pending_balance_unexpected", "critical", "credit_grant", grant.GrantID, map[string]any{"grant_id": grant.GrantID}, map[string]any{"account_id": grant.AccountID.String(), "pending": balance.Pending}); err != nil {
				return drifts, err
			}
		}
		expectedSpent := settledUsage[grant.GrantID]
		if balance.Spent != expectedSpent {
			drifts++
			if err := c.recordLedgerDrift(ctx, "grant_spend_mismatch", "critical", "credit_grant", grant.GrantID, map[string]any{"grant_id": grant.GrantID, "settled_usage": expectedSpent}, map[string]any{"account_id": grant.AccountID.String(), "spent": balance.Spent}); err != nil {
				return drifts, err
			}
		}
	}
	return drifts, nil
}

func (c *Client) settledGrantUsage(ctx context.Context, grantIDs []string) (map[string]uint64, error) {
	out := map[string]uint64{}
	if len(grantIDs) == 0 {
		return out, nil
	}
	rows, err := c.queries.ListSettledGrantUsage(ctx, store.ListSettledGrantUsageParams{Column1: grantIDs})
	if err != nil {
		return nil, fmt.Errorf("query settled grant usage: %w", err)
	}
	for _, row := range rows {
		if row.GrantID.Valid && row.AmountPosted > 0 {
			out[row.GrantID.String] = uint64(row.AmountPosted)
		}
	}
	return out, nil
}

func (c *Client) recordLedgerDrift(ctx context.Context, kind, severity, aggregateType, aggregateID string, pgSnapshot map[string]any, tbSnapshot map[string]any) error {
	return c.WithTx(ctx, "billing.ledger.reconcile.drift", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		pgBytes, err := json.Marshal(pgSnapshot)
		if err != nil {
			return err
		}
		tbBytes, err := json.Marshal(tbSnapshot)
		if err != nil {
			return err
		}
		driftID := textID("ledger_drift", kind, aggregateType, aggregateID, time.Now().UTC().Format(time.RFC3339Nano))
		if err := q.InsertLedgerDriftEvent(ctx, store.InsertLedgerDriftEventParams{
			DriftID:             driftID,
			DriftKind:           kind,
			Severity:            severity,
			AggregateType:       aggregateType,
			AggregateID:         aggregateID,
			PgSnapshot:          pgBytes,
			TigerbeetleSnapshot: tbBytes,
		}); err != nil {
			return fmt.Errorf("insert ledger drift event: %w", err)
		}
		return appendEvent(ctx, tx, q, eventFact{
			EventType:     "ledger_drift_detected",
			AggregateType: aggregateType,
			AggregateID:   aggregateID,
			OccurredAt:    time.Now().UTC(),
			Payload:       map[string]any{"drift_id": driftID, "drift_kind": kind, "severity": severity, "pg": pgSnapshot, "tigerbeetle": tbSnapshot},
		})
	})
}
