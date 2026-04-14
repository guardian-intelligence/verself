package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/forge-metal/billing-service/internal/store"
)

type billingFinalization struct {
	FinalizationID string
	SubjectType    string
	SubjectID      string
	CycleID        string
	OrgID          OrgID
	ProductID      string
	Reason         string
	DocumentKind   string
	StartedAt      time.Time
}

func (c *Client) ensureCycleFinalizationTx(ctx context.Context, tx pgx.Tx, q *store.Queries, orgID OrgID, productID string, cycle billingCycle, now time.Time) (string, error) {
	finalizationIDValue := finalizationID("cycle", cycle.CycleID)
	tag, err := tx.Exec(ctx, `
		INSERT INTO billing_finalizations (finalization_id, subject_type, subject_id, cycle_id, org_id, product_id, reason, document_kind, state, started_at, idempotency_key)
		VALUES ($1,'cycle',$2,$2,$3,$4,'scheduled_period_end','statement','pending',$5,$1)
		ON CONFLICT (subject_type, subject_id, idempotency_key) DO NOTHING
	`, finalizationIDValue, cycle.CycleID, orgIDText(orgID), productID, now)
	if err != nil {
		return "", fmt.Errorf("insert cycle finalization: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE billing_cycles SET active_finalization_id = $2 WHERE cycle_id = $1 AND active_finalization_id IS NULL`, cycle.CycleID, finalizationIDValue)
	if err != nil {
		return "", fmt.Errorf("link cycle finalization: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return finalizationIDValue, nil
	}
	if err := appendEvent(ctx, tx, q, eventFact{
		EventType:     "billing_finalization_started",
		AggregateType: "billing_finalization",
		AggregateID:   finalizationIDValue,
		OrgID:         orgID,
		ProductID:     productID,
		OccurredAt:    now,
		Payload: map[string]any{
			"finalization_id": finalizationIDValue,
			"subject_type":    "cycle",
			"subject_id":      cycle.CycleID,
			"cycle_id":        cycle.CycleID,
			"reason":          "scheduled_period_end",
			"document_kind":   "statement",
		},
	}); err != nil {
		return "", err
	}
	if c.runtime != nil {
		if err := c.runtime.EnqueueFinalizationRunTx(ctx, tx, finalizationIDValue); err != nil {
			return "", err
		}
	}
	return finalizationIDValue, nil
}

func (c *Client) FinalizePendingBillingFinalizations(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := c.pg.Query(ctx, `
		SELECT finalization_id
		FROM billing_finalizations
		WHERE state IN ('pending', 'failed')
		  AND subject_type = 'cycle'
		ORDER BY started_at, finalization_id
		LIMIT $1
	`, limit)
	if err != nil {
		return 0, fmt.Errorf("query pending finalizations: %w", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, fmt.Errorf("scan pending finalization: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	finalized := 0
	for _, id := range ids {
		ok, err := c.FinalizeBillingFinalization(ctx, id)
		if err != nil {
			return finalized, err
		}
		if ok {
			finalized++
		}
	}
	return finalized, nil
}

func (c *Client) FinalizeBillingFinalization(ctx context.Context, finalizationID string) (bool, error) {
	finalization, ok, err := c.claimBillingFinalization(ctx, finalizationID)
	if err != nil || !ok {
		return ok, err
	}
	cycle, err := c.loadBillingCycle(ctx, finalization.CycleID)
	if err != nil {
		_ = c.failBillingFinalization(ctx, finalizationID, err)
		return true, nil
	}
	statement, err := c.statementForCycle(ctx, finalization.OrgID, finalization.ProductID, cycle, time.Now().UTC(), false)
	if err != nil {
		_ = c.failBillingFinalization(ctx, finalizationID, err)
		return true, nil
	}
	hasUsage := statement.Totals.ChargeUnits > 0 || statement.Totals.ReservedUnits > 0
	hasPaidContract, err := c.cycleHasPaidContractOverlap(ctx, finalization.OrgID, finalization.ProductID, cycle)
	if err != nil {
		_ = c.failBillingFinalization(ctx, finalizationID, err)
		return true, nil
	}
	hasFinancialActivity := hasUsage || hasPaidContract || statement.Totals.TotalDueUnits > 0 || statement.Totals.PurchaseUnits > 0 || statement.Totals.PromoUnits > 0 || statement.Totals.RefundUnits > 0 || statement.Totals.ReceivableUnits > 0
	customerVisible := hasUsage || hasPaidContract || statement.Totals.TotalDueUnits > 0 || statement.Totals.ReceivableUnits > 0
	if err := c.issueFinalizationDocument(ctx, finalization, cycle, statement, hasUsage, hasFinancialActivity, customerVisible); err != nil {
		_ = c.failBillingFinalization(ctx, finalizationID, err)
		return true, nil
	}
	return true, nil
}

func (c *Client) claimBillingFinalization(ctx context.Context, finalizationID string) (billingFinalization, bool, error) {
	var out billingFinalization
	var orgIDTextValue string
	err := c.pg.QueryRow(ctx, `
		UPDATE billing_finalizations
		SET state = 'collecting_facts',
		    attempts = attempts + 1,
		    last_error = '',
		    updated_at = now()
		WHERE finalization_id = $1
		  AND state IN ('pending', 'failed')
		RETURNING finalization_id, subject_type, subject_id, COALESCE(cycle_id,''), org_id, product_id, reason, document_kind, started_at
	`, finalizationID).Scan(&out.FinalizationID, &out.SubjectType, &out.SubjectID, &out.CycleID, &orgIDTextValue, &out.ProductID, &out.Reason, &out.DocumentKind, &out.StartedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return billingFinalization{}, false, nil
	}
	if err != nil {
		return billingFinalization{}, false, fmt.Errorf("claim finalization %s: %w", finalizationID, err)
	}
	parsed, err := parseOrgID(orgIDTextValue)
	if err != nil {
		return billingFinalization{}, false, err
	}
	out.OrgID = parsed
	return out, true, nil
}

func (c *Client) failBillingFinalization(ctx context.Context, finalizationID string, cause error) error {
	_, err := c.pg.Exec(ctx, `
		UPDATE billing_finalizations
		SET state = CASE WHEN attempts >= 25 THEN 'blocked' ELSE 'failed' END,
		    last_error = $2,
		    blocked_reason = CASE WHEN attempts >= 25 THEN $2 ELSE blocked_reason END,
		    updated_at = now()
		WHERE finalization_id = $1
		  AND state = 'collecting_facts'
	`, finalizationID, cause.Error())
	return err
}

func (c *Client) loadBillingCycle(ctx context.Context, cycleID string) (billingCycle, error) {
	cycle, err := c.scanBillingCycle(c.pg.QueryRow(ctx, `
		SELECT cycle_id, currency, COALESCE(predecessor_cycle_id, ''), anchor_at, cycle_seq, cadence_kind, starts_at, ends_at
		FROM billing_cycles
		WHERE cycle_id = $1
	`, cycleID))
	if errors.Is(err, pgx.ErrNoRows) {
		return billingCycle{}, fmt.Errorf("billing cycle %s not found", cycleID)
	}
	if err != nil {
		return billingCycle{}, fmt.Errorf("load billing cycle %s: %w", cycleID, err)
	}
	return cycle, nil
}

func (c *Client) cycleHasPaidContractOverlap(ctx context.Context, orgID OrgID, productID string, cycle billingCycle) (bool, error) {
	var exists bool
	err := c.pg.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM contract_phases cp
			JOIN contracts c ON c.contract_id = cp.contract_id
			WHERE cp.org_id = $1
			  AND cp.product_id = $2
			  AND cp.payment_state = 'paid'
			  AND c.payment_state = 'paid'
			  AND cp.effective_start < $4
			  AND COALESCE(cp.effective_end, $4) > $3
			  AND cp.state IN ('active', 'grace', 'closed', 'superseded')
		)
	`, orgIDText(orgID), productID, cycle.StartsAt, cycle.EndsAt).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check paid contract overlap: %w", err)
	}
	return exists, nil
}

func (c *Client) issueFinalizationDocument(ctx context.Context, finalization billingFinalization, cycle billingCycle, statement Statement, hasUsage bool, hasFinancialActivity bool, customerVisible bool) error {
	snapshot, err := json.Marshal(statement)
	if err != nil {
		return fmt.Errorf("marshal document snapshot: %w", err)
	}
	now := time.Now().UTC()
	documentKind := "statement"
	documentStatus := "issued"
	paymentStatus := "n_a"
	finalizationState := "closed"
	notificationPolicy := "send_if_activity"
	documentIDValue := ""
	documentNumber := ""
	if statement.Totals.TotalDueUnits > 0 {
		documentKind = "invoice"
		paymentStatus = "pending"
		finalizationState = "collection_pending"
	}
	if !customerVisible {
		documentKind = "internal_statement"
		notificationPolicy = "never"
	}
	return c.WithTx(ctx, "billing.finalization.issue_document", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		if customerVisible {
			documentIDValue = documentID(finalization.SubjectType, finalization.SubjectID)
			var err error
			documentNumber, err = allocateDocumentNumberTx(ctx, tx, now)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO billing_documents (document_id, document_number, document_kind, finalization_id, org_id, product_id, cycle_id, status, payment_status, period_start, period_end, issued_at, currency, subtotal_units, total_due_units, document_snapshot_json, rendered_html, content_hash)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
				ON CONFLICT (document_id) DO NOTHING
			`, documentIDValue, documentNumber, documentKind, finalization.FinalizationID, orgIDText(finalization.OrgID), finalization.ProductID, cycle.CycleID, documentStatus, paymentStatus, cycle.StartsAt, cycle.EndsAt, now, statement.Currency, int64(statement.Totals.ChargeUnits), int64(statement.Totals.TotalDueUnits), snapshot, renderDocumentHTML(statement), textID("document_snapshot", string(snapshot)))
			if err != nil {
				return fmt.Errorf("insert billing document: %w", err)
			}
			if err := insertDocumentLineItemsTx(ctx, tx, documentIDValue, statement); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `
			UPDATE billing_finalizations
			SET state = $2,
			    document_id = NULLIF($3, ''),
			    document_kind = $4,
			    customer_visible = $5,
			    notification_policy = $6,
			    has_usage = $7,
			    has_financial_activity = $8,
			    completed_at = $9,
			    snapshot_hash = $10,
			    updated_at = now()
			WHERE finalization_id = $1
		`, finalization.FinalizationID, finalizationState, documentIDValue, documentKind, customerVisible, notificationPolicy, hasUsage, hasFinancialActivity, now, textID("document_snapshot", string(snapshot)))
		if err != nil {
			return fmt.Errorf("update finalization complete: %w", err)
		}
		_, err = tx.Exec(ctx, `
			UPDATE billing_cycles
			SET status = 'finalized',
			    active_finalization_id = $2,
			    finalized_at = $3,
			    updated_at = now()
			WHERE cycle_id = $1
			  AND status IN ('closed_for_usage', 'finalized')
		`, cycle.CycleID, finalization.FinalizationID, now)
		if err != nil {
			return fmt.Errorf("mark cycle finalized: %w", err)
		}
		if customerVisible {
			if err := appendEvent(ctx, tx, q, eventFact{EventType: "billing_document_issued", AggregateType: "billing_document", AggregateID: documentIDValue, OrgID: finalization.OrgID, ProductID: finalization.ProductID, OccurredAt: now, Payload: map[string]any{"finalization_id": finalization.FinalizationID, "document_id": documentIDValue, "document_number": documentNumber, "document_kind": documentKind, "cycle_id": cycle.CycleID, "total_due_units": statement.Totals.TotalDueUnits, "payment_status": paymentStatus}}); err != nil {
				return err
			}
		}
		if err := appendEvent(ctx, tx, q, eventFact{EventType: "billing_finalization_closed", AggregateType: "billing_finalization", AggregateID: finalization.FinalizationID, OrgID: finalization.OrgID, ProductID: finalization.ProductID, OccurredAt: now, Payload: map[string]any{"finalization_id": finalization.FinalizationID, "document_id": documentIDValue, "document_kind": documentKind, "cycle_id": cycle.CycleID, "state": finalizationState, "customer_visible": customerVisible, "has_usage": hasUsage, "has_financial_activity": hasFinancialActivity}}); err != nil {
			return err
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "billing_cycle_finalized", AggregateType: "billing_cycle", AggregateID: cycle.CycleID, OrgID: finalization.OrgID, ProductID: finalization.ProductID, OccurredAt: now, Payload: map[string]any{"finalization_id": finalization.FinalizationID, "document_id": documentIDValue, "document_kind": documentKind, "cycle_id": cycle.CycleID, "starts_at": cycle.StartsAt.Format(time.RFC3339Nano), "ends_at": cycle.EndsAt.Format(time.RFC3339Nano)}})
	})
}

func insertDocumentLineItemsTx(ctx context.Context, tx pgx.Tx, documentID string, statement Statement) error {
	for _, line := range statement.LineItems {
		_, err := tx.Exec(ctx, `
			INSERT INTO billing_document_line_items (line_item_id, document_id, line_type, product_id, bucket_id, sku_id, description, quantity, quantity_unit, unit_rate_units, charge_units, free_tier_units, contract_units, purchase_units, promo_units, refund_units, receivable_units)
			VALUES ($1,$2,'usage',$3,NULLIF($4,''),NULLIF($5,''),$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
			ON CONFLICT (line_item_id) DO NOTHING
		`, textID("document_line", documentID, line.ProductID, line.BucketID, line.SKUID, line.PricingPhase, fmt.Sprintf("%d", line.UnitRate)), documentID, line.ProductID, line.BucketID, line.SKUID, line.SKUDisplayName, line.Quantity, line.QuantityUnit, int64(line.UnitRate), int64(line.ChargeUnits), int64(line.FreeTierUnits), int64(line.ContractUnits), int64(line.PurchaseUnits), int64(line.PromoUnits), int64(line.RefundUnits), int64(line.ReceivableUnits))
		if err != nil {
			return fmt.Errorf("insert document line item: %w", err)
		}
	}
	return nil
}

func renderDocumentHTML(statement Statement) string {
	return fmt.Sprintf("<h1>Forge Metal billing %s to %s</h1><p>Total due units: %d</p>", statement.PeriodStart.Format(time.RFC3339Nano), statement.PeriodEnd.Format(time.RFC3339Nano), statement.Totals.TotalDueUnits)
}
