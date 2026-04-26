package billing

import (
	"context"
	"fmt"

	"github.com/verself/billing-service/internal/store"
)

func (c *Client) ListDocuments(ctx context.Context, orgID OrgID, productID string) ([]DocumentRecord, error) {
	rows, err := c.queries.ListBillingDocuments(ctx, store.ListBillingDocumentsParams{OrgID: orgIDText(orgID), ProductID: productID})
	if err != nil {
		return nil, fmt.Errorf("query billing documents: %w", err)
	}
	out := []DocumentRecord{}
	for _, row := range rows {
		record := DocumentRecord{
			DocumentID:             row.DocumentID,
			DocumentNumber:         row.DocumentNumber,
			DocumentKind:           row.DocumentKind,
			FinalizationID:         row.FinalizationID,
			ProductID:              row.ProductID,
			CycleID:                row.CycleID,
			Status:                 row.Status,
			PaymentStatus:          row.PaymentStatus,
			IssuedAt:               timePtr(row.IssuedAt),
			Currency:               row.Currency,
			SubtotalUnits:          uint64(row.SubtotalUnits),
			AdjustmentUnits:        row.AdjustmentUnits,
			TaxUnits:               uint64(row.TaxUnits),
			TotalDueUnits:          uint64(row.TotalDueUnits),
			StripeHostedInvoiceURL: row.StripeHostedInvoiceUrl,
			StripeInvoicePDFURL:    row.StripeInvoicePdfUrl,
			StripePaymentIntentID:  row.StripePaymentIntentID,
		}
		if row.PeriodStart.Valid {
			record.PeriodStart = row.PeriodStart.Time.UTC()
		}
		if row.PeriodEnd.Valid {
			record.PeriodEnd = row.PeriodEnd.Time.UTC()
		}
		out = append(out, record)
	}
	return out, nil
}
