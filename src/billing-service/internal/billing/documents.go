package billing

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

func (c *Client) ListDocuments(ctx context.Context, orgID OrgID, productID string) ([]DocumentRecord, error) {
	rows, err := c.pg.Query(ctx, `
		SELECT document_id, COALESCE(document_number, ''), document_kind, COALESCE(finalization_id, ''), product_id, COALESCE(cycle_id, ''),
		       status, payment_status, period_start, period_end, issued_at, currency, subtotal_units, adjustment_units, tax_units, total_due_units,
		       COALESCE(stripe_hosted_invoice_url, ''), COALESCE(stripe_invoice_pdf_url, ''), COALESCE(stripe_payment_intent_id, '')
		FROM billing_documents
		WHERE org_id = $1
		  AND ($2 = '' OR product_id = $2)
		  AND status <> 'voided'
		ORDER BY period_start DESC, issued_at DESC NULLS LAST, document_id DESC
	`, orgIDText(orgID), productID)
	if err != nil {
		return nil, fmt.Errorf("query billing documents: %w", err)
	}
	defer rows.Close()
	out := []DocumentRecord{}
	for rows.Next() {
		var record DocumentRecord
		var issuedAt pgtype.Timestamptz
		var subtotalUnits, taxUnits, totalDueUnits int64
		if err := rows.Scan(&record.DocumentID, &record.DocumentNumber, &record.DocumentKind, &record.FinalizationID, &record.ProductID, &record.CycleID, &record.Status, &record.PaymentStatus, &record.PeriodStart, &record.PeriodEnd, &issuedAt, &record.Currency, &subtotalUnits, &record.AdjustmentUnits, &taxUnits, &totalDueUnits, &record.StripeHostedInvoiceURL, &record.StripeInvoicePDFURL, &record.StripePaymentIntentID); err != nil {
			return nil, fmt.Errorf("scan billing document: %w", err)
		}
		record.IssuedAt = timePtr(issuedAt)
		record.PeriodStart = record.PeriodStart.UTC()
		record.PeriodEnd = record.PeriodEnd.UTC()
		record.SubtotalUnits = uint64(subtotalUnits)
		record.TaxUnits = uint64(taxUnits)
		record.TotalDueUnits = uint64(totalDueUnits)
		out = append(out, record)
	}
	return out, rows.Err()
}
