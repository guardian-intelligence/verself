package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/verself/billing-service/internal/store"
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
	rowsAffected, err := q.InsertCycleFinalization(ctx, store.InsertCycleFinalizationParams{
		FinalizationID: finalizationIDValue,
		SubjectID:      cycle.CycleID,
		OrgID:          orgIDText(orgID),
		ProductID:      productID,
		StartedAt:      timestamptz(now),
	})
	if err != nil {
		return "", fmt.Errorf("insert cycle finalization: %w", err)
	}
	if err := q.LinkCycleFinalization(ctx, store.LinkCycleFinalizationParams{CycleID: cycle.CycleID, ActiveFinalizationID: pgTextValue(finalizationIDValue)}); err != nil {
		return "", fmt.Errorf("link cycle finalization: %w", err)
	}
	if rowsAffected == 0 {
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
	ids, err := c.queries.ListPendingBillingFinalizations(ctx, store.ListPendingBillingFinalizationsParams{Limit: int32(limit)})
	if err != nil {
		return 0, fmt.Errorf("query pending finalizations: %w", err)
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
	completedAt, err := c.BusinessNow(ctx, c.queries, finalization.OrgID, finalization.ProductID)
	if err != nil {
		_ = c.failBillingFinalization(ctx, finalizationID, err)
		return true, nil
	}
	statement, err := c.statementForCycle(ctx, finalization.OrgID, finalization.ProductID, cycle, completedAt, false)
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
	if err := c.issueFinalizationDocument(ctx, finalization, cycle, statement, completedAt, hasUsage, hasFinancialActivity, customerVisible); err != nil {
		_ = c.failBillingFinalization(ctx, finalizationID, err)
		return true, nil
	}
	return true, nil
}

func (c *Client) claimBillingFinalization(ctx context.Context, finalizationID string) (billingFinalization, bool, error) {
	row, err := c.queries.ClaimBillingFinalization(ctx, store.ClaimBillingFinalizationParams{FinalizationID: finalizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return billingFinalization{}, false, nil
	}
	if err != nil {
		return billingFinalization{}, false, fmt.Errorf("claim finalization %s: %w", finalizationID, err)
	}
	parsed, err := parseOrgID(row.OrgID)
	if err != nil {
		return billingFinalization{}, false, err
	}
	out := billingFinalization{
		FinalizationID: row.FinalizationID,
		SubjectType:    row.SubjectType,
		SubjectID:      row.SubjectID,
		CycleID:        row.CycleID,
		OrgID:          parsed,
		ProductID:      row.ProductID,
		Reason:         row.Reason,
		DocumentKind:   row.DocumentKind,
		StartedAt:      row.StartedAt.Time.UTC(),
	}
	return out, true, nil
}

func (c *Client) failBillingFinalization(ctx context.Context, finalizationID string, cause error) error {
	return c.queries.FailBillingFinalization(ctx, store.FailBillingFinalizationParams{FinalizationID: finalizationID, LastError: cause.Error()})
}

func (c *Client) loadBillingCycle(ctx context.Context, cycleID string) (billingCycle, error) {
	row, err := c.queries.GetBillingCycle(ctx, store.GetBillingCycleParams{CycleID: cycleID})
	if errors.Is(err, pgx.ErrNoRows) {
		return billingCycle{}, fmt.Errorf("billing cycle %s not found", cycleID)
	}
	if err != nil {
		return billingCycle{}, fmt.Errorf("load billing cycle %s: %w", cycleID, err)
	}
	return billingCycle{
		CycleID:            row.CycleID,
		Currency:           row.Currency,
		PredecessorCycleID: row.PredecessorCycleID,
		AnchorAt:           row.AnchorAt.Time.UTC(),
		CycleSeq:           row.CycleSeq,
		CadenceKind:        row.CadenceKind,
		StartsAt:           row.StartsAt.Time.UTC(),
		EndsAt:             row.EndsAt.Time.UTC(),
	}, nil
}

func (c *Client) cycleHasPaidContractOverlap(ctx context.Context, orgID OrgID, productID string, cycle billingCycle) (bool, error) {
	exists, err := c.queries.CycleHasPaidContractOverlap(ctx, store.CycleHasPaidContractOverlapParams{
		OrgID:          orgIDText(orgID),
		ProductID:      productID,
		EffectiveEnd:   timestamptz(cycle.StartsAt),
		EffectiveStart: timestamptz(cycle.EndsAt),
	})
	if err != nil {
		return false, fmt.Errorf("check paid contract overlap: %w", err)
	}
	return exists, nil
}

func (c *Client) issueFinalizationDocument(ctx context.Context, finalization billingFinalization, cycle billingCycle, statement Statement, completedAt time.Time, hasUsage bool, hasFinancialActivity bool, customerVisible bool) error {
	snapshot, err := json.Marshal(statement)
	if err != nil {
		return fmt.Errorf("marshal document snapshot: %w", err)
	}
	completedAt = completedAt.UTC()
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
			documentNumber, err = allocateDocumentNumberTx(ctx, q, completedAt)
			if err != nil {
				return err
			}
			if err := q.InsertBillingDocument(ctx, store.InsertBillingDocumentParams{
				DocumentID:           documentIDValue,
				DocumentNumber:       pgTextValue(documentNumber),
				DocumentKind:         documentKind,
				FinalizationID:       pgTextValue(finalization.FinalizationID),
				OrgID:                orgIDText(finalization.OrgID),
				ProductID:            finalization.ProductID,
				CycleID:              pgTextValue(cycle.CycleID),
				Status:               documentStatus,
				PaymentStatus:        paymentStatus,
				PeriodStart:          timestamptz(cycle.StartsAt),
				PeriodEnd:            timestamptz(cycle.EndsAt),
				IssuedAt:             timestamptz(completedAt),
				Currency:             statement.Currency,
				SubtotalUnits:        int64(statement.Totals.ChargeUnits),
				TotalDueUnits:        int64(statement.Totals.TotalDueUnits),
				DocumentSnapshotJson: snapshot,
				RenderedHtml:         renderDocumentHTML(statement),
				ContentHash:          textID("document_snapshot", string(snapshot)),
			}); err != nil {
				return fmt.Errorf("insert billing document: %w", err)
			}
			if err := insertDocumentLineItemsTx(ctx, q, documentIDValue, statement); err != nil {
				return err
			}
		}
		if err := q.UpdateBillingFinalizationIssued(ctx, store.UpdateBillingFinalizationIssuedParams{
			FinalizationID:       finalization.FinalizationID,
			State:                finalizationState,
			Column3:              documentIDValue,
			DocumentKind:         documentKind,
			CustomerVisible:      customerVisible,
			NotificationPolicy:   notificationPolicy,
			HasUsage:             hasUsage,
			HasFinancialActivity: hasFinancialActivity,
			CompletedAt:          timestamptz(completedAt),
			SnapshotHash:         pgTextValue(textID("document_snapshot", string(snapshot))),
		}); err != nil {
			return fmt.Errorf("update finalization complete: %w", err)
		}
		if err := q.MarkBillingCycleFinalized(ctx, store.MarkBillingCycleFinalizedParams{
			CycleID:              cycle.CycleID,
			ActiveFinalizationID: pgTextValue(finalization.FinalizationID),
			FinalizedAt:          timestamptz(completedAt),
		}); err != nil {
			return fmt.Errorf("mark cycle finalized: %w", err)
		}
		if customerVisible {
			if err := appendEvent(ctx, tx, q, eventFact{EventType: "billing_document_issued", AggregateType: "billing_document", AggregateID: documentIDValue, OrgID: finalization.OrgID, ProductID: finalization.ProductID, OccurredAt: completedAt, Payload: map[string]any{"finalization_id": finalization.FinalizationID, "document_id": documentIDValue, "document_number": documentNumber, "document_kind": documentKind, "cycle_id": cycle.CycleID, "total_due_units": statement.Totals.TotalDueUnits, "payment_status": paymentStatus}}); err != nil {
				return err
			}
		}
		if err := appendEvent(ctx, tx, q, eventFact{EventType: "billing_finalization_closed", AggregateType: "billing_finalization", AggregateID: finalization.FinalizationID, OrgID: finalization.OrgID, ProductID: finalization.ProductID, OccurredAt: completedAt, Payload: map[string]any{"finalization_id": finalization.FinalizationID, "document_id": documentIDValue, "document_kind": documentKind, "cycle_id": cycle.CycleID, "state": finalizationState, "customer_visible": customerVisible, "has_usage": hasUsage, "has_financial_activity": hasFinancialActivity}}); err != nil {
			return err
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "billing_cycle_finalized", AggregateType: "billing_cycle", AggregateID: cycle.CycleID, OrgID: finalization.OrgID, ProductID: finalization.ProductID, OccurredAt: completedAt, Payload: map[string]any{"finalization_id": finalization.FinalizationID, "document_id": documentIDValue, "document_kind": documentKind, "cycle_id": cycle.CycleID, "starts_at": cycle.StartsAt.Format(time.RFC3339Nano), "ends_at": cycle.EndsAt.Format(time.RFC3339Nano)}})
	})
}

func insertDocumentLineItemsTx(ctx context.Context, q *store.Queries, documentID string, statement Statement) error {
	for _, line := range statement.LineItems {
		if err := q.InsertBillingDocumentLineItem(ctx, store.InsertBillingDocumentLineItemParams{
			LineItemID:      textID("document_line", documentID, line.ProductID, line.BucketID, line.SKUID, line.PricingPhase, fmt.Sprintf("%d", line.UnitRate)),
			DocumentID:      documentID,
			ProductID:       pgTextValue(line.ProductID),
			Column4:         line.BucketID,
			Column5:         line.SKUID,
			Description:     line.SKUDisplayName,
			Column7:         line.Quantity,
			QuantityUnit:    line.QuantityUnit,
			UnitRateUnits:   int64(line.UnitRate),
			ChargeUnits:     int64(line.ChargeUnits),
			FreeTierUnits:   int64(line.FreeTierUnits),
			ContractUnits:   int64(line.ContractUnits),
			PurchaseUnits:   int64(line.PurchaseUnits),
			PromoUnits:      int64(line.PromoUnits),
			RefundUnits:     int64(line.RefundUnits),
			ReceivableUnits: int64(line.ReceivableUnits),
		}); err != nil {
			return fmt.Errorf("insert document line item: %w", err)
		}
	}
	return nil
}

func renderDocumentHTML(statement Statement) string {
	return fmt.Sprintf("<h1>Verself billing %s to %s</h1><p>Total due units: %d</p>", statement.PeriodStart.Format(time.RFC3339Nano), statement.PeriodEnd.Format(time.RFC3339Nano), statement.Totals.TotalDueUnits)
}
