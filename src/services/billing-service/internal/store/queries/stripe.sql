-- name: GetProductDisplayName :one
SELECT display_name
FROM products
WHERE product_id = sqlc.arg(product_id);

-- name: GetOrgBillingEmail :one
SELECT billing_email
FROM orgs
WHERE org_id = sqlc.arg(org_id);

-- name: UpsertStripeCustomerBinding :exec
INSERT INTO provider_bindings (
    binding_id, aggregate_type, aggregate_id, provider, provider_object_type,
    provider_object_id, provider_customer_id, sync_state
) VALUES (
    sqlc.arg(binding_id), 'customer', sqlc.arg(org_id), 'stripe', 'customer',
    sqlc.arg(customer_id), sqlc.arg(customer_id), 'synced'
)
ON CONFLICT (provider, provider_object_type, provider_object_id) DO UPDATE
SET aggregate_id = EXCLUDED.aggregate_id,
    provider_customer_id = EXCLUDED.provider_customer_id,
    sync_state = 'synced';

-- name: InsertStripeCustomerBindingIfMissing :exec
INSERT INTO provider_bindings (
    binding_id, aggregate_type, aggregate_id, provider, provider_object_type,
    provider_object_id, provider_customer_id, sync_state
) VALUES (
    sqlc.arg(binding_id), 'customer', sqlc.arg(org_id), 'stripe', 'customer',
    sqlc.arg(customer_id), sqlc.arg(customer_id), 'synced'
)
ON CONFLICT (provider, provider_object_type, provider_object_id) DO NOTHING;

-- name: LookupStripeCustomer :one
SELECT provider_object_id
FROM provider_bindings
WHERE aggregate_type = 'customer'
  AND aggregate_id = sqlc.arg(org_id)
  AND provider = 'stripe'
  AND provider_object_type = 'customer'
ORDER BY created_at DESC
LIMIT 1;

-- name: GetDefaultStripePaymentMethod :one
SELECT provider_payment_method_id
FROM payment_methods
WHERE org_id = sqlc.arg(org_id)
  AND provider = 'stripe'
  AND status = 'active'
  AND is_default
ORDER BY updated_at DESC,
         payment_method_id DESC
LIMIT 1;

-- name: UpdateUpgradeInvoiceProviderDocument :exec
UPDATE billing_documents
SET stripe_invoice_id = NULLIF(sqlc.arg(provider_invoice_id), ''),
    stripe_hosted_invoice_url = sqlc.arg(hosted_url),
    stripe_invoice_pdf_url = sqlc.arg(pdf_url),
    status = sqlc.arg(status),
    payment_status = sqlc.arg(payment_status)
WHERE document_id = sqlc.arg(document_id);

-- name: UpdateUpgradeInvoiceProviderChange :exec
UPDATE contract_changes
SET provider_invoice_id = NULLIF(sqlc.arg(provider_invoice_id), ''),
    state = CASE WHEN sqlc.arg(payment_status)::text = 'paid' THEN state ELSE 'provider_pending' END
WHERE change_id = sqlc.arg(change_id);

-- name: InsertStripeProviderEvent :exec
INSERT INTO billing_provider_events (
    event_id, provider_event_id, provider, event_type, provider_object_type, provider_object_id,
    provider_customer_id, provider_invoice_id, provider_payment_intent_id, contract_id,
    change_id, finalization_id, document_id, org_id, product_id, provider_created_at,
    livemode, payload, state, idempotency_key
) VALUES (
    sqlc.arg(event_id), sqlc.arg(provider_event_id), 'stripe', sqlc.arg(event_type),
    sqlc.arg(provider_object_type), sqlc.arg(provider_object_id),
    NULLIF(sqlc.arg(provider_customer_id), ''), NULLIF(sqlc.arg(provider_invoice_id), ''),
    NULLIF(sqlc.arg(provider_payment_intent_id), ''), NULLIF(sqlc.arg(contract_id), ''),
    NULLIF(sqlc.arg(change_id), ''), NULLIF(sqlc.arg(finalization_id), ''),
    NULLIF(sqlc.arg(document_id), ''), NULLIF(sqlc.arg(org_id), ''),
    NULLIF(sqlc.arg(product_id), ''), sqlc.arg(provider_created_at), sqlc.arg(livemode),
    sqlc.arg(payload), 'received', sqlc.arg(event_id)
)
ON CONFLICT (provider, provider_event_id) DO UPDATE
SET payload = EXCLUDED.payload,
    product_id = COALESCE(billing_provider_events.product_id, EXCLUDED.product_id),
    state = CASE
        WHEN billing_provider_events.state IN ('applied','ignored','dead_letter') THEN billing_provider_events.state
        ELSE 'received'
    END,
    updated_at = now();

-- name: ListPendingStripeProviderEventIDs :many
SELECT event_id
FROM billing_provider_events
WHERE provider = 'stripe'
  AND state IN ('received','queued','failed')
  AND COALESCE(next_attempt_at, received_at) <= now()
ORDER BY received_at
LIMIT sqlc.arg(limit_count);

-- name: ClaimProviderEvent :one
UPDATE billing_provider_events
SET state = 'applying',
    attempts = attempts + 1,
    next_attempt_at = NULL,
    last_error = '',
    updated_at = now()
WHERE event_id = sqlc.arg(event_id)
  AND state IN ('received','queued','failed')
RETURNING event_type, payload;

-- name: ClearDefaultStripePaymentMethods :exec
UPDATE payment_methods
SET is_default = false
WHERE org_id = sqlc.arg(org_id)
  AND provider = 'stripe';

-- name: UpsertStripePaymentMethod :exec
INSERT INTO payment_methods (
    payment_method_id, org_id, provider, provider_customer_id, provider_payment_method_id,
    setup_intent_id, status, is_default, card_brand, card_last4, expires_month,
    expires_year, off_session_authorized_at
) VALUES (
    sqlc.arg(payment_method_id), sqlc.arg(org_id), 'stripe',
    sqlc.arg(provider_customer_id), sqlc.arg(provider_payment_method_id),
    sqlc.arg(setup_intent_id), 'active', true, sqlc.arg(card_brand),
    sqlc.arg(card_last4), sqlc.arg(expires_month), sqlc.arg(expires_year),
    sqlc.arg(off_session_authorized_at)
)
ON CONFLICT (provider, provider_payment_method_id) DO UPDATE
SET status = 'active',
    is_default = true,
    setup_intent_id = EXCLUDED.setup_intent_id,
    provider_customer_id = EXCLUDED.provider_customer_id,
    card_brand = EXCLUDED.card_brand,
    card_last4 = EXCLUDED.card_last4,
    expires_month = EXCLUDED.expires_month,
    expires_year = EXCLUDED.expires_year,
    off_session_authorized_at = EXCLUDED.off_session_authorized_at;

-- name: MarkInvoicePaymentFailedDocument :exec
UPDATE billing_documents
SET status = 'payment_failed',
    payment_status = 'failed',
    stripe_invoice_id = sqlc.arg(provider_invoice_id),
    stripe_hosted_invoice_url = sqlc.arg(hosted_url)
WHERE change_id = sqlc.arg(change_id);

-- name: MarkInvoicePaymentFailedFinalization :exec
UPDATE billing_finalizations
SET state = 'payment_failed',
    last_error = 'stripe invoice payment failed',
    updated_at = now()
WHERE finalization_id = sqlc.arg(finalization_id);

-- name: GetContractChangePayload :one
SELECT payload
FROM contract_changes
WHERE change_id = sqlc.arg(change_id);

-- name: MarkProviderEventFinal :one
UPDATE billing_provider_events
SET state = sqlc.arg(state),
    applied_at = now(),
    last_error = '',
    updated_at = now()
WHERE event_id = sqlc.arg(event_id)
  AND state = 'applying'
RETURNING provider_event_id,
          event_type,
          COALESCE(org_id, '') AS org_id,
          COALESCE(product_id, '') AS product_id,
          COALESCE(contract_id, '') AS contract_id,
          COALESCE(change_id, '') AS change_id,
          COALESCE(finalization_id, '') AS finalization_id,
          COALESCE(document_id, '') AS document_id;

-- name: FailProviderEvent :exec
UPDATE billing_provider_events
SET state = CASE WHEN attempts >= 25 THEN 'dead_letter' ELSE 'failed' END,
    last_error = sqlc.arg(last_error),
    next_attempt_at = now() + interval '30 seconds',
    updated_at = now()
WHERE event_id = sqlc.arg(event_id)
  AND state = 'applying';
