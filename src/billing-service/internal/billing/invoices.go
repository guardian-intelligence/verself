package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"strconv"
	"time"
)

const (
	invoiceIssuerForgeMetal                     = "forge-metal"
	invoiceFinalizationPolicyVersion            = "v1"
	automaticNoConsentAdjustmentCapUnits uint64 = 99 * 100_000
)

type invoiceArtifact struct {
	InvoiceID       string
	CycleID         string
	OrgID           OrgID
	ProductID       string
	InvoiceNumber   string
	Status          string
	PaymentStatus   string
	PeriodStart     time.Time
	PeriodEnd       time.Time
	IssuedAt        time.Time
	Currency        string
	TotalDueUnits   uint64
	SubtotalUnits   uint64
	TaxUnits        uint64
	AdjustmentUnits uint64
	SnapshotJSON    []byte
	RenderedHTML    string
	ContentHash     string
	LineItems       []invoiceLineItemArtifact
	Adjustments     []invoiceAdjustmentArtifact
}

type invoiceLineItemArtifact struct {
	LineItemID      string
	LineType        string
	ProductID       string
	BucketID        string
	SKUID           string
	Description     string
	Quantity        float64
	QuantityUnit    string
	UnitRate        uint64
	ChargeUnits     uint64
	FreeTierUnits   uint64
	ContractUnits   uint64
	PurchaseUnits   uint64
	PromoUnits      uint64
	RefundUnits     uint64
	ReceivableUnits uint64
	AdjustmentUnits uint64
	Metadata        []byte
}

type invoiceAdjustmentArtifact struct {
	AdjustmentID           string
	InvoiceID              string
	InvoiceFinalizationID  string
	OrgID                  OrgID
	ProductID              string
	WindowID               string
	SKUID                  string
	AmountUnits            uint64
	AdjustmentType         string
	AdjustmentSource       string
	ReasonCode             string
	PolicyVersion          string
	CustomerVisible        bool
	Recoverable            bool
	AffectsCustomerBalance bool
	Metadata               []byte
}

func (c *Client) FinalizeDueBillingCycles(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := c.pg.QueryContext(ctx, `
		SELECT cycle_id
		FROM billing_cycles
		WHERE status = 'closed_for_usage'
		  AND finalization_due_at <= now()
		ORDER BY finalization_due_at, cycle_id
		LIMIT $1
	`, limit)
	if err != nil {
		return 0, fmt.Errorf("query due invoice finalizations: %w", err)
	}
	defer rows.Close()

	var cycleIDs []string
	for rows.Next() {
		var cycleID string
		if err := rows.Scan(&cycleID); err != nil {
			return 0, fmt.Errorf("scan due invoice finalization: %w", err)
		}
		cycleIDs = append(cycleIDs, cycleID)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate due invoice finalizations: %w", err)
	}

	finalized := 0
	for _, cycleID := range cycleIDs {
		didFinalize, err := c.FinalizeBillingCycle(ctx, cycleID)
		if err != nil {
			return finalized, err
		}
		if didFinalize {
			finalized++
		}
	}
	return finalized, nil
}

func (c *Client) FinalizeBillingCycle(ctx context.Context, cycleID string) (bool, error) {
	if cycleID == "" {
		return false, fmt.Errorf("cycle_id is required")
	}
	issuedAt := c.clock().UTC()
	tx, err := c.pg.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin invoice finalization: %w", err)
	}
	defer tx.Rollback()

	cycle, found, err := loadBillingCycleTx(ctx, tx, cycleID)
	if err != nil {
		return false, err
	}
	if !found || cycle.Status == "invoiced" {
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("commit skipped invoice finalization: %w", err)
		}
		return false, nil
	}
	if cycle.Status != "closed_for_usage" {
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("commit non-due invoice finalization: %w", err)
		}
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE billing_cycles
		SET status = 'invoice_finalizing',
		    updated_at = now()
		WHERE cycle_id = $1
		  AND status = 'closed_for_usage'
	`, cycle.CycleID); err != nil {
		return false, fmt.Errorf("mark billing cycle %s invoice finalizing: %w", cycle.CycleID, err)
	}

	if reason, blocked, err := pendingWindowBlockReason(ctx, tx, cycle.CycleID); err != nil {
		return false, err
	} else if blocked {
		if err := blockInvoiceFinalizationTx(ctx, tx, cycle, reason, issuedAt); err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("commit blocked invoice finalization: %w", err)
		}
		return true, nil
	}

	if existing, found, err := loadCycleInvoiceTx(ctx, tx, cycle.CycleID); err != nil {
		return false, err
	} else if found {
		if _, err := tx.ExecContext(ctx, `
			UPDATE billing_cycles
			SET status = 'invoiced',
			    invoice_id = $2,
			    updated_at = now()
			WHERE cycle_id = $1
		`, cycle.CycleID, existing.InvoiceID); err != nil {
			return false, fmt.Errorf("attach existing invoice %s to cycle %s: %w", existing.InvoiceID, cycle.CycleID, err)
		}
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("commit existing invoice finalization: %w", err)
		}
		return false, nil
	}

	windows, err := c.invoiceSettledWindowsTx(ctx, tx, cycle.CycleID)
	if err != nil {
		return false, err
	}
	invoiceID := deterministicTextID("billing-invoice", "cycle", cycle.CycleID)
	adjustments, adjustmentTotal, err := buildNoConsentAdjustmentArtifacts(cycle, invoiceID, windows)
	if err != nil {
		return false, err
	}
	if adjustmentTotal > automaticNoConsentAdjustmentCapUnits {
		if err := blockInvoiceFinalizationTx(ctx, tx, cycle, "no_consent_adjustment_cap_exceeded", issuedAt); err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("commit no-consent adjustment cap block: %w", err)
		}
		return true, nil
	}
	invoiceNumber, err := allocateInvoiceNumberTx(ctx, tx, invoiceIssuerForgeMetal, issuedAt)
	if err != nil {
		return false, err
	}
	invoice, err := buildInvoiceArtifact(cycle, windows, invoiceID, invoiceNumber, issuedAt, adjustments, adjustmentTotal)
	if err != nil {
		return false, err
	}
	if err := insertInvoiceArtifactTx(ctx, tx, invoice); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE billing_cycles
		SET status = 'invoiced',
		    invoice_id = $2,
		    block_reason = '',
		    updated_at = now()
		WHERE cycle_id = $1
	`, cycle.CycleID, invoice.InvoiceID); err != nil {
		return false, fmt.Errorf("mark billing cycle %s invoiced: %w", cycle.CycleID, err)
	}
	if err := insertBillingEventTx(ctx, tx, invoiceIssuedEvent(invoice, issuedAt)); err != nil {
		return false, err
	}
	for _, adjustment := range invoice.Adjustments {
		if err := insertBillingEventTx(ctx, tx, invoiceAdjustmentCreatedEvent(adjustment, issuedAt)); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit invoice finalization: %w", err)
	}
	return true, nil
}

func pendingWindowBlockReason(ctx context.Context, tx *sql.Tx, cycleID string) (string, bool, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*)
		FROM billing_windows
		WHERE cycle_id = $1
		  AND state IN ('reserving', 'reserved')
	`, cycleID).Scan(&count); err != nil {
		return "", false, fmt.Errorf("count pending windows for invoice finalization %s: %w", cycleID, err)
	}
	if count == 0 {
		return "", false, nil
	}
	return "billing_windows_pending", true, nil
}

func blockInvoiceFinalizationTx(ctx context.Context, tx *sql.Tx, cycle BillingCycle, reason string, occurredAt time.Time) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE billing_cycles
		SET status = 'blocked',
		    block_reason = $2,
		    updated_at = now()
		WHERE cycle_id = $1
		  AND status = 'invoice_finalizing'
	`, cycle.CycleID, reason)
	if err != nil {
		return fmt.Errorf("block invoice finalization for cycle %s: %w", cycle.CycleID, err)
	}
	return insertBillingEventTx(ctx, tx, invoiceFinalizationBlockedEvent(cycle, reason, occurredAt))
}

func loadCycleInvoiceTx(ctx context.Context, tx *sql.Tx, cycleID string) (invoiceArtifact, bool, error) {
	var invoice invoiceArtifact
	var orgIDText string
	var snapshotJSON string
	var totalDueUnits int64
	var subtotalUnits int64
	var taxUnits int64
	var adjustmentUnits int64
	err := tx.QueryRowContext(ctx, `
		SELECT invoice_id, cycle_id, org_id, product_id, invoice_number, status, payment_status,
		       period_start, period_end, COALESCE(issued_at, created_at), currency,
		       total_due_units, subtotal_units, tax_units, adjustment_units,
		       invoice_snapshot_json::text, rendered_html, content_hash
		FROM billing_invoices
		WHERE cycle_id = $1
		  AND invoice_kind = 'cycle'
		  AND status <> 'voided'
	`, cycleID).Scan(
		&invoice.InvoiceID,
		&invoice.CycleID,
		&orgIDText,
		&invoice.ProductID,
		&invoice.InvoiceNumber,
		&invoice.Status,
		&invoice.PaymentStatus,
		&invoice.PeriodStart,
		&invoice.PeriodEnd,
		&invoice.IssuedAt,
		&invoice.Currency,
		&totalDueUnits,
		&subtotalUnits,
		&taxUnits,
		&adjustmentUnits,
		&snapshotJSON,
		&invoice.RenderedHTML,
		&invoice.ContentHash,
	)
	if err == sql.ErrNoRows {
		return invoiceArtifact{}, false, nil
	}
	if err != nil {
		return invoiceArtifact{}, false, fmt.Errorf("load invoice for cycle %s: %w", cycleID, err)
	}
	parsedOrgID, err := strconv.ParseUint(orgIDText, 10, 64)
	if err != nil {
		return invoiceArtifact{}, false, fmt.Errorf("parse invoice org_id %q: %w", orgIDText, err)
	}
	invoice.OrgID = OrgID(parsedOrgID)
	invoice.TotalDueUnits = uint64(totalDueUnits)
	invoice.SubtotalUnits = uint64(subtotalUnits)
	invoice.TaxUnits = uint64(taxUnits)
	invoice.AdjustmentUnits = uint64(adjustmentUnits)
	invoice.SnapshotJSON = []byte(snapshotJSON)
	return invoice, true, nil
}

func (c *Client) invoiceSettledWindowsTx(ctx context.Context, tx *sql.Tx, cycleID string) ([]persistedWindow, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT window_id
		FROM billing_windows
		WHERE cycle_id = $1
		  AND state = 'settled'
		ORDER BY window_start, window_seq, window_id
	`, cycleID)
	if err != nil {
		return nil, fmt.Errorf("query settled windows for invoice %s: %w", cycleID, err)
	}
	defer rows.Close()

	var windowIDs []string
	for rows.Next() {
		var windowID string
		if err := rows.Scan(&windowID); err != nil {
			return nil, fmt.Errorf("scan settled invoice window: %w", err)
		}
		windowIDs = append(windowIDs, windowID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate settled invoice windows: %w", err)
	}
	windows := make([]persistedWindow, 0, len(windowIDs))
	for _, windowID := range windowIDs {
		window, err := c.loadPersistedWindowFrom(ctx, tx, windowID)
		if err != nil {
			return nil, fmt.Errorf("load invoice window %s: %w", windowID, err)
		}
		windows = append(windows, window)
	}
	return windows, nil
}

func allocateInvoiceNumberTx(ctx context.Context, tx *sql.Tx, issuerID string, issuedAt time.Time) (string, error) {
	year := issuedAt.UTC().Year()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO invoice_number_allocators (issuer_id, invoice_year, next_number)
		VALUES ($1, $2, 1)
		ON CONFLICT (issuer_id, invoice_year) DO NOTHING
	`, issuerID, year); err != nil {
		return "", fmt.Errorf("ensure invoice number allocator: %w", err)
	}
	var nextNumber int64
	if err := tx.QueryRowContext(ctx, `
		SELECT next_number
		FROM invoice_number_allocators
		WHERE issuer_id = $1
		  AND invoice_year = $2
		FOR UPDATE
	`, issuerID, year).Scan(&nextNumber); err != nil {
		return "", fmt.Errorf("lock invoice number allocator: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE invoice_number_allocators
		SET next_number = next_number + 1,
		    updated_at = now()
		WHERE issuer_id = $1
		  AND invoice_year = $2
	`, issuerID, year); err != nil {
		return "", fmt.Errorf("advance invoice number allocator: %w", err)
	}
	return fmt.Sprintf("FM-%d-%06d", year, nextNumber), nil
}

func buildInvoiceArtifact(cycle BillingCycle, windows []persistedWindow, invoiceID string, invoiceNumber string, issuedAt time.Time, adjustments []invoiceAdjustmentArtifact, adjustmentTotal uint64) (invoiceArtifact, error) {
	period := statementPeriod{Start: cycle.StartsAt, End: cycle.EndsAt, Source: "billing_cycle"}
	statement, err := buildStatement(cycle.OrgID, cycle.ProductID, period, nil, windows, issuedAt)
	if err != nil {
		return invoiceArtifact{}, err
	}
	invoice := invoiceArtifact{
		InvoiceID:       invoiceID,
		CycleID:         cycle.CycleID,
		OrgID:           cycle.OrgID,
		ProductID:       cycle.ProductID,
		InvoiceNumber:   invoiceNumber,
		Status:          "issued",
		PaymentStatus:   "n_a",
		PeriodStart:     cycle.StartsAt.UTC(),
		PeriodEnd:       cycle.EndsAt.UTC(),
		IssuedAt:        issuedAt.UTC(),
		Currency:        statement.Currency,
		TotalDueUnits:   statement.Totals.TotalDueUnits,
		SubtotalUnits:   statement.Totals.ChargeUnits,
		TaxUnits:        0,
		AdjustmentUnits: adjustmentTotal,
		LineItems:       make([]invoiceLineItemArtifact, 0, len(statement.LineItems)),
		Adjustments:     adjustments,
	}
	for index, item := range statement.LineItems {
		line, err := invoiceLineItemFromStatementItem(invoice.InvoiceID, index, item)
		if err != nil {
			return invoiceArtifact{}, err
		}
		invoice.LineItems = append(invoice.LineItems, line)
	}
	adjustmentSnapshots, err := invoiceAdjustmentSnapshots(adjustments)
	if err != nil {
		return invoiceArtifact{}, err
	}
	snapshot, err := json.Marshal(map[string]any{
		"invoice_id":     invoice.InvoiceID,
		"invoice_number": invoice.InvoiceNumber,
		"cycle_id":       invoice.CycleID,
		"org_id":         strconv.FormatUint(uint64(invoice.OrgID), 10),
		"product_id":     invoice.ProductID,
		"period_start":   invoice.PeriodStart.Format(time.RFC3339Nano),
		"period_end":     invoice.PeriodEnd.Format(time.RFC3339Nano),
		"issued_at":      invoice.IssuedAt.Format(time.RFC3339Nano),
		"currency":       invoice.Currency,
		"totals":         statement.Totals,
		"line_items":     statement.LineItems,
		"adjustments":    adjustmentSnapshots,
	})
	if err != nil {
		return invoiceArtifact{}, fmt.Errorf("marshal invoice snapshot: %w", err)
	}
	canonicalSnapshot, contentHash, err := canonicalBillingEventPayload(snapshot)
	if err != nil {
		return invoiceArtifact{}, fmt.Errorf("canonicalize invoice snapshot: %w", err)
	}
	invoice.SnapshotJSON = canonicalSnapshot
	invoice.ContentHash = contentHash
	invoice.RenderedHTML = renderInvoiceHTML(invoice)
	return invoice, nil
}

func invoiceAdjustmentSnapshots(adjustments []invoiceAdjustmentArtifact) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(adjustments))
	for _, adjustment := range adjustments {
		metadata := any(map[string]any{})
		if len(adjustment.Metadata) > 0 {
			if err := json.Unmarshal(adjustment.Metadata, &metadata); err != nil {
				return nil, fmt.Errorf("decode invoice adjustment metadata %s: %w", adjustment.AdjustmentID, err)
			}
		}
		out = append(out, map[string]any{
			"adjustment_id":            adjustment.AdjustmentID,
			"invoice_id":               adjustment.InvoiceID,
			"invoice_finalization_id":  adjustment.InvoiceFinalizationID,
			"org_id":                   strconv.FormatUint(uint64(adjustment.OrgID), 10),
			"product_id":               adjustment.ProductID,
			"window_id":                adjustment.WindowID,
			"sku_id":                   adjustment.SKUID,
			"amount_units":             adjustment.AmountUnits,
			"adjustment_type":          adjustment.AdjustmentType,
			"adjustment_source":        adjustment.AdjustmentSource,
			"reason_code":              adjustment.ReasonCode,
			"policy_version":           adjustment.PolicyVersion,
			"customer_visible":         adjustment.CustomerVisible,
			"recoverable":              adjustment.Recoverable,
			"affects_customer_balance": adjustment.AffectsCustomerBalance,
			"metadata":                 metadata,
		})
	}
	return out, nil
}

func buildNoConsentAdjustmentArtifacts(cycle BillingCycle, invoiceID string, windows []persistedWindow) ([]invoiceAdjustmentArtifact, uint64, error) {
	finalizationID := deterministicTextID("invoice-finalization", cycle.CycleID)
	var total uint64
	adjustments := make([]invoiceAdjustmentArtifact, 0)
	for _, window := range windows {
		if window.WriteoffChargeUnits == 0 {
			continue
		}
		componentWriteoffs, _, componentTotal, err := componentAndBucketChargeUnitsForQuantity(window, window.WriteoffQuantity)
		if err != nil {
			return nil, 0, fmt.Errorf("compute no-consent adjustment components for window %s: %w", window.WindowID, err)
		}
		if componentTotal != window.WriteoffChargeUnits {
			return nil, 0, fmt.Errorf("writeoff charge mismatch for window %s: components=%d window=%d", window.WindowID, componentTotal, window.WriteoffChargeUnits)
		}
		reasonCode := noConsentAdjustmentReasonCode(window)
		for _, skuID := range sortedUint64MapKeys(componentWriteoffs) {
			amount := componentWriteoffs[skuID]
			if amount == 0 {
				continue
			}
			metadata, err := json.Marshal(map[string]string{
				"cycle_id":              cycle.CycleID,
				"window_id":             window.WindowID,
				"pricing_contract_id":   window.PricingContractID,
				"pricing_phase_id":      window.PricingPhaseID,
				"pricing_plan_id":       window.PlanID,
				"writeoff_quantity":     strconv.FormatUint(uint64(window.WriteoffQuantity), 10),
				"writeoff_charge_units": strconv.FormatUint(window.WriteoffChargeUnits, 10),
				"writeoff_reason":       window.WriteoffReason,
			})
			if err != nil {
				return nil, 0, fmt.Errorf("marshal no-consent adjustment metadata for window %s: %w", window.WindowID, err)
			}
			adjustment := invoiceAdjustmentArtifact{
				AdjustmentID:           deterministicTextID("invoice-adjustment", finalizationID, strconv.FormatUint(uint64(cycle.OrgID), 10), cycle.ProductID, window.WindowID, skuID, reasonCode, invoiceFinalizationPolicyVersion),
				InvoiceID:              invoiceID,
				InvoiceFinalizationID:  finalizationID,
				OrgID:                  cycle.OrgID,
				ProductID:              cycle.ProductID,
				WindowID:               window.WindowID,
				SKUID:                  skuID,
				AmountUnits:            amount,
				AdjustmentType:         "credit",
				AdjustmentSource:       "system_policy",
				ReasonCode:             reasonCode,
				PolicyVersion:          invoiceFinalizationPolicyVersion,
				CustomerVisible:        false,
				Recoverable:            false,
				AffectsCustomerBalance: false,
				Metadata:               metadata,
			}
			total, err = safeAddUint64(total, amount)
			if err != nil {
				return nil, 0, fmt.Errorf("add no-consent adjustment amount: %w", err)
			}
			adjustments = append(adjustments, adjustment)
		}
	}
	return adjustments, total, nil
}

func noConsentAdjustmentReasonCode(window persistedWindow) string {
	if window.PricingContractID == "" {
		return "free_tier_overage_absorbed"
	}
	return "paid_hard_cap_overage_absorbed"
}

func invoiceLineItemFromStatementItem(invoiceID string, index int, item StatementLineItem) (invoiceLineItemArtifact, error) {
	metadata, err := json.Marshal(map[string]string{
		"plan_id":       item.PlanID,
		"pricing_phase": item.PricingPhase,
	})
	if err != nil {
		return invoiceLineItemArtifact{}, fmt.Errorf("marshal invoice line metadata: %w", err)
	}
	return invoiceLineItemArtifact{
		LineItemID:      deterministicTextID("invoice-line-item", invoiceID, strconv.Itoa(index), item.ProductID, item.BucketID, item.SKUID, item.PricingPhase, strconv.FormatUint(item.UnitRate, 10)),
		LineType:        "usage",
		ProductID:       item.ProductID,
		BucketID:        item.BucketID,
		SKUID:           item.SKUID,
		Description:     item.BucketDisplayName + " - " + item.SKUDisplayName,
		Quantity:        item.Quantity,
		QuantityUnit:    item.QuantityUnit,
		UnitRate:        item.UnitRate,
		ChargeUnits:     item.ChargeUnits,
		FreeTierUnits:   item.FreeTierUnits,
		ContractUnits:   item.ContractUnits,
		PurchaseUnits:   item.PurchaseUnits,
		PromoUnits:      item.PromoUnits,
		RefundUnits:     item.RefundUnits,
		ReceivableUnits: item.ReceivableUnits,
		AdjustmentUnits: 0,
		Metadata:        metadata,
	}, nil
}

func renderInvoiceHTML(invoice invoiceArtifact) string {
	return "<article><h1>Invoice " + html.EscapeString(invoice.InvoiceNumber) + "</h1><p>Total due units: " + strconv.FormatUint(invoice.TotalDueUnits, 10) + "</p></article>"
}

func insertInvoiceArtifactTx(ctx context.Context, tx *sql.Tx, invoice invoiceArtifact) error {
	totalDueUnits, err := uint64ToInt64(invoice.TotalDueUnits)
	if err != nil {
		return err
	}
	subtotalUnits, err := uint64ToInt64(invoice.SubtotalUnits)
	if err != nil {
		return err
	}
	taxUnits, err := uint64ToInt64(invoice.TaxUnits)
	if err != nil {
		return err
	}
	adjustmentUnits, err := uint64ToInt64(invoice.AdjustmentUnits)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO billing_invoices (
			invoice_id, cycle_id, org_id, product_id, invoice_number, invoice_kind,
			status, payment_status, period_start, period_end, issued_at, due_at, currency,
			total_due_units, subtotal_units, tax_units, adjustment_units,
			invoice_snapshot_json, rendered_html, content_hash
		)
		VALUES ($1, $2, $3, $4, $5, 'cycle',
		        $6, $7, $8, $9, $10, $10, $11,
		        $12, $13, $14, $15,
		        $16::jsonb, $17, $18)
	`, invoice.InvoiceID, invoice.CycleID, strconv.FormatUint(uint64(invoice.OrgID), 10), invoice.ProductID, invoice.InvoiceNumber,
		invoice.Status, invoice.PaymentStatus, invoice.PeriodStart, invoice.PeriodEnd, invoice.IssuedAt, invoice.Currency,
		totalDueUnits, subtotalUnits, taxUnits, adjustmentUnits,
		string(invoice.SnapshotJSON), invoice.RenderedHTML, invoice.ContentHash)
	if err != nil {
		return fmt.Errorf("insert invoice %s: %w", invoice.InvoiceID, err)
	}
	for _, line := range invoice.LineItems {
		if err := insertInvoiceLineItemTx(ctx, tx, invoice.InvoiceID, line); err != nil {
			return err
		}
	}
	for _, adjustment := range invoice.Adjustments {
		if err := insertInvoiceAdjustmentTx(ctx, tx, adjustment); err != nil {
			return err
		}
	}
	return nil
}

func insertInvoiceAdjustmentTx(ctx context.Context, tx *sql.Tx, adjustment invoiceAdjustmentArtifact) error {
	amountUnits, err := uint64ToInt64(adjustment.AmountUnits)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO invoice_adjustments (
			adjustment_id, invoice_id, invoice_finalization_id, org_id, product_id,
			window_id, sku_id, amount_units, adjustment_type, adjustment_source,
			reason_code, policy_version, customer_visible, recoverable,
			affects_customer_balance, metadata
		)
		VALUES ($1, $2, $3, $4, $5,
		        $6, $7, $8, $9, $10,
		        $11, $12, $13, $14,
		        $15, $16::jsonb)
		ON CONFLICT (adjustment_id) DO NOTHING
	`, adjustment.AdjustmentID, adjustment.InvoiceID, adjustment.InvoiceFinalizationID,
		strconv.FormatUint(uint64(adjustment.OrgID), 10), adjustment.ProductID,
		adjustment.WindowID, adjustment.SKUID, amountUnits, adjustment.AdjustmentType, adjustment.AdjustmentSource,
		adjustment.ReasonCode, adjustment.PolicyVersion, adjustment.CustomerVisible, adjustment.Recoverable,
		adjustment.AffectsCustomerBalance, string(adjustment.Metadata))
	if err != nil {
		return fmt.Errorf("insert invoice adjustment %s: %w", adjustment.AdjustmentID, err)
	}
	return nil
}

func insertInvoiceLineItemTx(ctx context.Context, tx *sql.Tx, invoiceID string, line invoiceLineItemArtifact) error {
	unitRate, err := uint64ToInt64(line.UnitRate)
	if err != nil {
		return err
	}
	chargeUnits, err := uint64ToInt64(line.ChargeUnits)
	if err != nil {
		return err
	}
	freeTierUnits, err := uint64ToInt64(line.FreeTierUnits)
	if err != nil {
		return err
	}
	contractUnits, err := uint64ToInt64(line.ContractUnits)
	if err != nil {
		return err
	}
	purchaseUnits, err := uint64ToInt64(line.PurchaseUnits)
	if err != nil {
		return err
	}
	promoUnits, err := uint64ToInt64(line.PromoUnits)
	if err != nil {
		return err
	}
	refundUnits, err := uint64ToInt64(line.RefundUnits)
	if err != nil {
		return err
	}
	receivableUnits, err := uint64ToInt64(line.ReceivableUnits)
	if err != nil {
		return err
	}
	adjustmentUnits, err := uint64ToInt64(line.AdjustmentUnits)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO invoice_line_items (
			line_item_id, invoice_id, line_type, product_id, bucket_id, sku_id,
			description, quantity, quantity_unit, unit_rate, charge_units,
			free_tier_units, contract_units, purchase_units, promo_units, refund_units,
			receivable_units, adjustment_units, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6,
		        $7, $8::numeric, $9, $10, $11,
		        $12, $13, $14, $15, $16,
		        $17, $18, $19::jsonb)
	`, line.LineItemID, invoiceID, line.LineType, line.ProductID, line.BucketID, line.SKUID,
		line.Description, strconv.FormatFloat(line.Quantity, 'f', -1, 64), line.QuantityUnit, unitRate, chargeUnits,
		freeTierUnits, contractUnits, purchaseUnits, promoUnits, refundUnits,
		receivableUnits, adjustmentUnits, string(line.Metadata))
	if err != nil {
		return fmt.Errorf("insert invoice line item %s: %w", line.LineItemID, err)
	}
	return nil
}

func uint64ToInt64(value uint64) (int64, error) {
	if value > math.MaxInt64 {
		return 0, fmt.Errorf("uint64 value %d overflows int64", value)
	}
	return int64(value), nil
}
