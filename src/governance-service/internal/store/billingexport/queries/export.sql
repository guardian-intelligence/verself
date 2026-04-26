-- name: ExportBillingOrgsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM orgs WHERE org_id = sqlc.arg(org_id)) t;

-- name: ExportBillingProductsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM products ORDER BY product_id) t;

-- name: ExportBillingCreditBucketsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM credit_buckets ORDER BY sort_order, bucket_id) t;

-- name: ExportBillingSKUsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM skus ORDER BY product_id, bucket_id, sku_id) t;

-- name: ExportBillingPlansJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM plans ORDER BY product_id, tier, plan_id) t;

-- name: ExportBillingPlanSKURatesJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM plan_sku_rates ORDER BY plan_id, sku_id, active_from) t;

-- name: ExportBillingEntitlementPoliciesJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM entitlement_policies ORDER BY product_id, source, policy_id) t;

-- name: ExportBillingPlanEntitlementsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM plan_entitlements ORDER BY plan_id, sort_order, policy_id) t;

-- name: ExportBillingContractsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM contracts WHERE org_id = sqlc.arg(org_id) ORDER BY created_at, contract_id) t;

-- name: ExportBillingContractChangesJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM contract_changes WHERE org_id = sqlc.arg(org_id) ORDER BY created_at, change_id) t;

-- name: ExportBillingContractPhasesJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM contract_phases WHERE org_id = sqlc.arg(org_id) ORDER BY created_at, phase_id) t;

-- name: ExportBillingContractEntitlementLinesJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM contract_entitlement_lines WHERE org_id = sqlc.arg(org_id) ORDER BY created_at, line_id) t;

-- name: ExportBillingCyclesJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM billing_cycles WHERE org_id = sqlc.arg(org_id) ORDER BY starts_at, cycle_id) t;

-- name: ExportBillingEntitlementPeriodsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM entitlement_periods WHERE org_id = sqlc.arg(org_id) ORDER BY period_start, period_id) t;

-- name: ExportBillingCreditGrantsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM credit_grants WHERE org_id = sqlc.arg(org_id) ORDER BY starts_at, grant_id) t;

-- name: ExportBillingWindowsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM billing_windows WHERE org_id = sqlc.arg(org_id) ORDER BY window_start, window_id) t;

-- name: ExportBillingWindowLedgerLegsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (
    SELECT l.*
    FROM billing_window_ledger_legs l
    JOIN billing_windows w ON w.window_id = l.window_id
    WHERE w.org_id = sqlc.arg(org_id)
    ORDER BY l.window_id,
             l.leg_seq
) t;

-- name: ExportBillingFinalizationsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM billing_finalizations WHERE org_id = sqlc.arg(org_id) ORDER BY created_at, finalization_id) t;

-- name: ExportBillingDocumentsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM billing_documents WHERE org_id = sqlc.arg(org_id) ORDER BY created_at, document_id) t;

-- name: ExportBillingDocumentLineItemsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (
    SELECT li.*
    FROM billing_document_line_items li
    JOIN billing_documents d ON d.document_id = li.document_id
    WHERE d.org_id = sqlc.arg(org_id)
    ORDER BY li.document_id,
             li.line_item_id
) t;

-- name: ExportBillingInvoiceAdjustmentsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM invoice_adjustments WHERE org_id = sqlc.arg(org_id) ORDER BY created_at, adjustment_id) t;

-- name: ExportBillingEventsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (SELECT * FROM billing_events WHERE org_id = sqlc.arg(org_id) ORDER BY occurred_at, event_id) t;

-- name: ListBillingInvoiceExportRows :many
SELECT document_id,
       COALESCE(document_number, '') AS document_number,
       document_kind,
       status,
       payment_status,
       period_start,
       period_end,
       issued_at,
       currency,
       subtotal_units,
       adjustment_units,
       tax_units,
       total_due_units,
       recipient_email,
       stripe_hosted_invoice_url,
       stripe_invoice_pdf_url
FROM billing_documents
WHERE org_id = sqlc.arg(org_id)
ORDER BY created_at,
         document_id;
