package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stripe/stripe-go/v85"
	"github.com/stripe/stripe-go/v85/webhook"

	"github.com/forge-metal/billing-service/internal/store"
)

func (c *Client) CreateCheckoutSession(ctx context.Context, orgID OrgID, productID string, params CheckoutParams) (string, error) {
	if params.AmountCents <= 0 {
		return "", fmt.Errorf("amount_cents must be positive")
	}
	if !c.cfg.UseStripe || c.stripe == nil {
		units, err := moneyUnitsFromCents(params.AmountCents)
		if err != nil {
			return "", err
		}
		_, err = c.DepositCredits(ctx, GrantBalance{OrgID: orgID, ScopeType: "account", Source: "purchase", SourceReferenceID: textID("offline_purchase", orgIDText(orgID), productID, strconv.FormatInt(params.AmountCents, 10), time.Now().UTC().Format(time.RFC3339Nano)), Amount: units, StartsAt: time.Now().UTC()})
		return params.SuccessURL, err
	}
	var productName string
	if err := c.pg.QueryRow(ctx, `SELECT display_name FROM products WHERE product_id = $1`, productID).Scan(&productName); err != nil {
		return "", fmt.Errorf("load checkout product: %w", err)
	}
	customerID, _ := c.lookupStripeCustomer(ctx, orgID)
	metadata := map[string]string{"org_id": orgIDText(orgID), "product_id": productID, "ledger_units": strconv.FormatInt(params.AmountCents*int64(ledgerUnitsPerCent), 10)}
	checkoutParams := &stripe.CheckoutSessionCreateParams{Mode: stripe.String(string(stripe.CheckoutSessionModePayment)), SuccessURL: stripe.String(params.SuccessURL), CancelURL: stripe.String(params.CancelURL), LineItems: []*stripe.CheckoutSessionCreateLineItemParams{{Quantity: stripe.Int64(1), PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{Currency: stripe.String("usd"), UnitAmount: stripe.Int64(params.AmountCents), ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{Name: stripe.String(productName + " Credits")}}}}, PaymentIntentData: &stripe.CheckoutSessionCreatePaymentIntentDataParams{Metadata: metadata}, Metadata: metadata}
	if customerID != "" {
		checkoutParams.Customer = stripe.String(customerID)
	} else {
		checkoutParams.CustomerCreation = stripe.String("always")
	}
	session, err := c.stripe.V1CheckoutSessions.Create(ctx, checkoutParams)
	if err != nil {
		return "", fmt.Errorf("create stripe checkout session: %w", err)
	}
	return session.URL, nil
}

func (c *Client) CreatePortalSession(ctx context.Context, orgID OrgID, returnURL string) (string, error) {
	customerID, err := c.lookupStripeCustomer(ctx, orgID)
	if err != nil || customerID == "" {
		return "", ErrNoStripeCustomer
	}
	if c.stripe == nil {
		return "", ErrNoStripeCustomer
	}
	session, err := c.stripe.V1BillingPortalSessions.Create(ctx, &stripe.BillingPortalSessionCreateParams{Customer: stripe.String(customerID), ReturnURL: stripe.String(returnURL)})
	if err != nil {
		return "", fmt.Errorf("create stripe portal session: %w", err)
	}
	return session.URL, nil
}

func (c *Client) ensureStripeCustomer(ctx context.Context, orgID OrgID) (string, error) {
	if customerID, err := c.lookupStripeCustomer(ctx, orgID); err == nil && customerID != "" {
		return customerID, nil
	}
	if c.stripe == nil {
		return "", ErrNoStripeCustomer
	}
	var billingEmail string
	_ = c.pg.QueryRow(ctx, `SELECT billing_email FROM orgs WHERE org_id = $1`, orgIDText(orgID)).Scan(&billingEmail)
	params := &stripe.CustomerCreateParams{Metadata: map[string]string{"org_id": orgIDText(orgID)}}
	if billingEmail != "" {
		params.Email = stripe.String(billingEmail)
	}
	params.SetIdempotencyKey(textID("stripe_customer_request", orgIDText(orgID), billingEmail))
	customer, err := c.stripe.V1Customers.Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("create stripe customer: %w", err)
	}
	_, err = c.pg.Exec(ctx, `
		INSERT INTO provider_bindings (binding_id, aggregate_type, aggregate_id, provider, provider_object_type, provider_object_id, provider_customer_id, sync_state)
		VALUES ($1,'customer',$2,'stripe','customer',$3,$3,'synced')
		ON CONFLICT (provider, provider_object_type, provider_object_id) DO UPDATE SET aggregate_id = EXCLUDED.aggregate_id, provider_customer_id = EXCLUDED.provider_customer_id, sync_state = 'synced'
	`, textID("provider_binding", "stripe", "customer", customer.ID), orgIDText(orgID), customer.ID)
	if err != nil {
		return "", fmt.Errorf("persist stripe customer binding: %w", err)
	}
	return customer.ID, nil
}

func (c *Client) lookupStripeCustomer(ctx context.Context, orgID OrgID) (string, error) {
	var customerID string
	err := c.pg.QueryRow(ctx, `
		SELECT provider_object_id
		FROM provider_bindings
		WHERE aggregate_type = 'customer' AND aggregate_id = $1 AND provider = 'stripe' AND provider_object_type = 'customer'
		ORDER BY created_at DESC
		LIMIT 1
	`, orgIDText(orgID)).Scan(&customerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoStripeCustomer
	}
	if err != nil {
		return "", fmt.Errorf("lookup stripe customer: %w", err)
	}
	return customerID, nil
}

func (c *Client) defaultStripePaymentMethod(ctx context.Context, orgID OrgID) (string, error) {
	var paymentMethodID string
	err := c.pg.QueryRow(ctx, `
		SELECT provider_payment_method_id
		FROM payment_methods
		WHERE org_id = $1 AND provider = 'stripe' AND status = 'active' AND is_default
		ORDER BY updated_at DESC, payment_method_id DESC
		LIMIT 1
	`, orgIDText(orgID)).Scan(&paymentMethodID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoStripeCustomer
	}
	if err != nil {
		return "", fmt.Errorf("lookup default stripe payment method: %w", err)
	}
	return paymentMethodID, nil
}

func (c *Client) collectUpgradeInvoice(ctx context.Context, quote contractChangeQuote) (hostedURL string, providerInvoiceID string, paid bool, err error) {
	customerID, err := c.ensureStripeCustomer(ctx, quote.OrgID)
	if err != nil {
		return "", "", false, err
	}
	paymentMethodID, err := c.defaultStripePaymentMethod(ctx, quote.OrgID)
	if err != nil {
		return "", "", false, err
	}
	metadata := map[string]string{"org_id": orgIDText(quote.OrgID), "product_id": quote.ProductID, "contract_id": quote.ContractID, "change_id": quote.ChangeID, "finalization_id": finalizationID("contract_change", quote.ChangeID), "document_id": documentID("contract_change", quote.ChangeID), "document_kind": "invoice", "from_plan_id": quote.FromPlanID, "target_plan_id": quote.TargetPlanID}
	if quote.ProviderRequestID != "" {
		metadata["provider_request_id"] = quote.ProviderRequestID
	}
	stripeID := cleanNonEmpty(quote.ProviderRequestID, quote.ChangeID)
	createParams := &stripe.InvoiceCreateParams{AutoAdvance: stripe.Bool(false), CollectionMethod: stripe.String(string(stripe.InvoiceCollectionMethodChargeAutomatically)), Currency: stripe.String("usd"), Customer: stripe.String(customerID), DefaultPaymentMethod: stripe.String(paymentMethodID), Description: stripe.String("Forge Metal plan upgrade"), Metadata: metadata, PendingInvoiceItemsBehavior: stripe.String("exclude")}
	createParams.SetIdempotencyKey("forge-metal:upgrade:" + stripeID + ":invoice")
	invoice, err := c.stripe.V1Invoices.Create(ctx, createParams)
	if err != nil {
		return "", "", false, fmt.Errorf("create stripe upgrade invoice: %w", err)
	}
	itemParams := &stripe.InvoiceItemCreateParams{Amount: stripe.Int64(int64(quote.PriceDeltaCents)), Currency: stripe.String("usd"), Customer: stripe.String(customerID), Description: stripe.String("Prorated upgrade from " + quote.FromPlanID + " to " + quote.TargetPlanID), Invoice: stripe.String(invoice.ID), Metadata: metadata, Period: &stripe.InvoiceItemCreatePeriodParams{Start: stripe.Int64(quote.EffectiveAt.Unix()), End: stripe.Int64(quote.CycleEnd.Unix())}}
	itemParams.SetIdempotencyKey("forge-metal:upgrade:" + stripeID + ":item")
	if _, err := c.stripe.V1InvoiceItems.Create(ctx, itemParams); err != nil {
		return "", invoice.ID, false, fmt.Errorf("create stripe upgrade invoice item: %w", err)
	}
	finalizeParams := &stripe.InvoiceFinalizeInvoiceParams{AutoAdvance: stripe.Bool(false)}
	finalizeParams.SetIdempotencyKey("forge-metal:upgrade:" + stripeID + ":finalize")
	finalized, err := c.stripe.V1Invoices.FinalizeInvoice(ctx, invoice.ID, finalizeParams)
	if err != nil {
		return "", invoice.ID, false, fmt.Errorf("finalize stripe upgrade invoice: %w", err)
	}
	payParams := &stripe.InvoicePayParams{PaymentMethod: stripe.String(paymentMethodID), OffSession: stripe.Bool(false)}
	payParams.SetIdempotencyKey("forge-metal:upgrade:" + stripeID + ":pay")
	paidInvoice, err := c.stripe.V1Invoices.Pay(ctx, finalized.ID, payParams)
	if err != nil {
		_ = c.updateUpgradeInvoiceProvider(ctx, quote, finalized.ID, finalized.HostedInvoiceURL, finalized.InvoicePDF, "issued", "pending")
		return finalized.HostedInvoiceURL, finalized.ID, false, fmt.Errorf("pay stripe upgrade invoice: %w", err)
	}
	paymentStatus := "pending"
	dbStatus := "issued"
	if paidInvoice.Status == stripe.InvoiceStatusPaid {
		paymentStatus = "paid"
		dbStatus = "paid"
	}
	if err := c.updateUpgradeInvoiceProvider(ctx, quote, paidInvoice.ID, paidInvoice.HostedInvoiceURL, paidInvoice.InvoicePDF, dbStatus, paymentStatus); err != nil {
		return paidInvoice.HostedInvoiceURL, paidInvoice.ID, false, err
	}
	return paidInvoice.HostedInvoiceURL, paidInvoice.ID, paymentStatus == "paid", nil
}

func (c *Client) updateUpgradeInvoiceProvider(ctx context.Context, quote contractChangeQuote, providerInvoiceID, hostedURL, pdfURL, status, paymentStatus string) error {
	_, err := c.pg.Exec(ctx, `UPDATE billing_documents SET stripe_invoice_id = NULLIF($2,''), stripe_hosted_invoice_url = $3, stripe_invoice_pdf_url = $4, status = $5, payment_status = $6 WHERE document_id = $1`, documentID("contract_change", quote.ChangeID), providerInvoiceID, hostedURL, pdfURL, status, paymentStatus)
	if err != nil {
		return fmt.Errorf("update upgrade document provider state: %w", err)
	}
	_, err = c.pg.Exec(ctx, `UPDATE contract_changes SET provider_invoice_id = NULLIF($2,''), state = CASE WHEN $3 = 'paid' THEN state ELSE 'provider_pending' END WHERE change_id = $1`, quote.ChangeID, providerInvoiceID, paymentStatus)
	return err
}

func (c *Client) HandleStripeWebhook(ctx context.Context, payload []byte, signatureHeader string, webhookSecret string) error {
	event, err := webhook.ConstructEvent(payload, signatureHeader, webhookSecret)
	if err != nil {
		return fmt.Errorf("construct stripe webhook event: %w", err)
	}
	eventID, err := c.recordStripeProviderEvent(ctx, event, payload)
	if err != nil {
		return err
	}
	if c.runtime != nil {
		return nil
	}
	_, err = c.ApplyProviderEvent(ctx, eventID)
	return err
}

func (c *Client) recordStripeProviderEvent(ctx context.Context, event stripe.Event, payload []byte) (string, error) {
	metadata := stripeEventMetadata(event)
	eventID := textID("provider_event", "stripe", event.ID)
	objectID := stripeEventObjectID(event)
	occurred := time.Unix(event.Created, 0).UTC()
	if event.Created == 0 {
		occurred = time.Now().UTC()
	}
	err := c.WithTx(ctx, "billing.provider_event.record", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO billing_provider_events (event_id, provider_event_id, provider, event_type, provider_object_type, provider_object_id, provider_customer_id, provider_invoice_id, provider_payment_intent_id, contract_id, change_id, finalization_id, document_id, org_id, product_id, provider_created_at, livemode, payload, state, idempotency_key)
			VALUES ($1,$2,'stripe',$3,$4,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),NULLIF($13,''),NULLIF($14,''),$15,$16,$17,'received',$1)
			ON CONFLICT (provider, provider_event_id) DO UPDATE
			SET payload = EXCLUDED.payload,
			    product_id = COALESCE(billing_provider_events.product_id, EXCLUDED.product_id),
			    state = CASE WHEN billing_provider_events.state IN ('applied','ignored','dead_letter') THEN billing_provider_events.state ELSE 'received' END,
			    updated_at = now()
		`, eventID, event.ID, string(event.Type), stripeProviderObjectType(string(event.Type)), objectID, metadata["customer_id"], metadata["provider_invoice_id"], metadata["provider_payment_intent_id"], metadata["contract_id"], metadata["change_id"], metadata["finalization_id"], metadata["document_id"], metadata["org_id"], metadata["product_id"], occurred, event.Livemode, payload)
		if err != nil {
			return fmt.Errorf("insert stripe provider event: %w", err)
		}
		orgID, _ := parseOrgID(metadata["org_id"])
		if orgID == 0 {
			return nil
		}
		if err := appendEvent(ctx, tx, q, eventFact{EventType: "provider_event_received", AggregateType: "provider_event", AggregateID: eventID, OrgID: orgID, ProductID: metadata["product_id"], OccurredAt: occurred, Payload: map[string]any{"provider_event_id": event.ID, "provider_event_type": string(event.Type), "provider_event_id_internal": eventID, "contract_id": metadata["contract_id"], "change_id": metadata["change_id"], "finalization_id": metadata["finalization_id"], "document_id": metadata["document_id"], "document_kind": metadata["document_kind"]}}); err != nil {
			return err
		}
		if c.runtime != nil {
			return c.runtime.EnqueueProviderEventApplyTx(ctx, tx, eventID)
		}
		return nil
	})
	return eventID, err
}

func (c *Client) ApplyPendingProviderEvents(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := c.pg.Query(ctx, `SELECT event_id FROM billing_provider_events WHERE provider = 'stripe' AND state IN ('received','queued','failed') AND COALESCE(next_attempt_at, received_at) <= now() ORDER BY received_at LIMIT $1`, limit)
	if err != nil {
		return 0, fmt.Errorf("query pending provider events: %w", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	applied := 0
	for _, id := range ids {
		ok, err := c.ApplyProviderEvent(ctx, id)
		if err != nil {
			return applied, err
		}
		if ok {
			applied++
		}
	}
	return applied, rows.Err()
}

func (c *Client) ApplyProviderEvent(ctx context.Context, eventID string) (bool, error) {
	var payload []byte
	var eventType string
	err := c.pg.QueryRow(ctx, `
		UPDATE billing_provider_events SET state = 'applying', attempts = attempts + 1, next_attempt_at = NULL, last_error = '', updated_at = now()
		WHERE event_id = $1 AND state IN ('received','queued','failed')
		RETURNING event_type, payload
	`, eventID).Scan(&eventType, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim provider event: %w", err)
	}
	var event stripe.Event
	if err := json.Unmarshal(payload, &event); err != nil {
		_ = c.failProviderEvent(ctx, eventID, err)
		return true, nil
	}
	var applyErr error
	switch string(event.Type) {
	case "setup_intent.succeeded":
		applyErr = c.handleSetupIntentSucceeded(ctx, event)
	case "payment_intent.succeeded":
		applyErr = c.handlePaymentIntentSucceeded(ctx, event)
	case "invoice.paid":
		applyErr = c.handleInvoicePaid(ctx, event)
	case "invoice.payment_failed":
		applyErr = c.handleInvoicePaymentFailed(ctx, event)
	case "checkout.session.completed":
		applyErr = c.handleCheckoutSessionCompleted(ctx, event)
	default:
		return true, c.markProviderEventFinal(ctx, eventID, "ignored")
	}
	if applyErr != nil {
		_ = c.failProviderEvent(ctx, eventID, applyErr)
		return true, nil
	}
	return true, c.markProviderEventFinal(ctx, eventID, "applied")
}

func (c *Client) handleSetupIntentSucceeded(ctx context.Context, event stripe.Event) error {
	var setup stripe.SetupIntent
	if err := json.Unmarshal(event.Data.Raw, &setup); err != nil {
		return fmt.Errorf("decode setup intent: %w", err)
	}
	return c.applySucceededSetupIntent(ctx, &setup)
}

func (c *Client) handleCheckoutSessionCompleted(ctx context.Context, event stripe.Event) error {
	var session stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
		return fmt.Errorf("decode checkout session: %w", err)
	}
	if session.Mode != stripe.CheckoutSessionModeSetup {
		return nil
	}
	if session.SetupIntent == nil || session.SetupIntent.ID == "" {
		return fmt.Errorf("checkout session %s has no setup intent", session.ID)
	}
	setup := session.SetupIntent
	if setup.Status != stripe.SetupIntentStatusSucceeded || setup.PaymentMethod == nil || setup.PaymentMethod.ID == "" {
		if c.stripe == nil {
			return fmt.Errorf("checkout session %s setup intent %s is not expanded", session.ID, setup.ID)
		}
		params := &stripe.SetupIntentRetrieveParams{}
		params.AddExpand("payment_method")
		var err error
		setup, err = c.stripe.V1SetupIntents.Retrieve(ctx, setup.ID, params)
		if err != nil {
			return fmt.Errorf("retrieve checkout setup intent %s: %w", setup.ID, err)
		}
	}
	if setup.Metadata == nil {
		setup.Metadata = map[string]string{}
	}
	for key, value := range session.Metadata {
		if setup.Metadata[key] == "" {
			setup.Metadata[key] = value
		}
	}
	if setup.Customer == nil {
		setup.Customer = session.Customer
	}
	return c.applySucceededSetupIntent(ctx, setup)
}

func (c *Client) applySucceededSetupIntent(ctx context.Context, setup *stripe.SetupIntent) error {
	if setup == nil {
		return fmt.Errorf("setup intent is required")
	}
	if setup.Status != stripe.SetupIntentStatusSucceeded {
		return fmt.Errorf("setup intent %s is %s", setup.ID, setup.Status)
	}
	metadata := setup.Metadata
	orgID, err := parseOrgID(metadata["org_id"])
	if err != nil {
		return err
	}
	productID, planID, contractID, phaseIDValue := metadata["product_id"], metadata["plan_id"], metadata["contract_id"], metadata["phase_id"]
	if setup.PaymentMethod == nil || setup.PaymentMethod.ID == "" {
		return fmt.Errorf("setup intent %s has no payment method", setup.ID)
	}
	customerID := ""
	if setup.Customer != nil {
		customerID = setup.Customer.ID
	}
	paymentMethodID := setup.PaymentMethod.ID
	brand, last4 := "", ""
	month, year := 0, 0
	if setup.PaymentMethod.Card != nil {
		brand = string(setup.PaymentMethod.Card.Brand)
		last4 = setup.PaymentMethod.Card.Last4
		month = int(setup.PaymentMethod.Card.ExpMonth)
		year = int(setup.PaymentMethod.Card.ExpYear)
	}
	appliedAt := time.Now().UTC()
	businessNow := appliedAt
	if productID != "" {
		businessNow, err = c.BusinessNow(ctx, c.queries, orgID, productID)
		if err != nil {
			return err
		}
	}
	err = c.WithTx(ctx, "billing.stripe.setup_intent.apply", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		_, _ = tx.Exec(ctx, `UPDATE payment_methods SET is_default = false WHERE org_id = $1 AND provider = 'stripe'`, orgIDText(orgID))
		_, err := tx.Exec(ctx, `
			INSERT INTO payment_methods (payment_method_id, org_id, provider, provider_customer_id, provider_payment_method_id, setup_intent_id, status, is_default, card_brand, card_last4, expires_month, expires_year, off_session_authorized_at)
			VALUES ($1,$2,'stripe',$3,$4,$5,'active',true,$6,$7,$8,$9,$10)
			ON CONFLICT (provider, provider_payment_method_id) DO UPDATE SET status = 'active', is_default = true, setup_intent_id = EXCLUDED.setup_intent_id, provider_customer_id = EXCLUDED.provider_customer_id, card_brand = EXCLUDED.card_brand, card_last4 = EXCLUDED.card_last4, expires_month = EXCLUDED.expires_month, expires_year = EXCLUDED.expires_year, off_session_authorized_at = EXCLUDED.off_session_authorized_at
		`, textID("payment_method", "stripe", paymentMethodID), orgIDText(orgID), customerID, paymentMethodID, setup.ID, brand, last4, nullableInt(month), nullableInt(year), appliedAt)
		if err != nil {
			return fmt.Errorf("upsert payment method: %w", err)
		}
		_, _ = tx.Exec(ctx, `INSERT INTO provider_bindings (binding_id, aggregate_type, aggregate_id, provider, provider_object_type, provider_object_id, provider_customer_id, sync_state) VALUES ($1,'customer',$2,'stripe','customer',$3,$3,'synced') ON CONFLICT (provider, provider_object_type, provider_object_id) DO NOTHING`, textID("provider_binding", "stripe", "customer", customerID), orgIDText(orgID), customerID)
		return appendEvent(ctx, tx, q, eventFact{EventType: "payment_method_activated", AggregateType: "payment_method", AggregateID: paymentMethodID, OrgID: orgID, ProductID: productID, OccurredAt: appliedAt, Payload: map[string]any{"provider": "stripe", "payment_method_id": paymentMethodID, "contract_id": contractID}})
	})
	if err != nil {
		return err
	}
	if planID != "" && contractID != "" {
		if phaseIDValue == "" {
			phaseIDValue = phaseID(contractID, planID, businessNow)
		}
		if err := c.activateCatalogContract(ctx, orgID, productID, planID, contractID, phaseIDValue, businessNow, businessNow); err != nil {
			return err
		}
		return c.EnsureCurrentEntitlements(ctx, orgID, productID)
	}
	return nil
}

func (c *Client) handlePaymentIntentSucceeded(ctx context.Context, event stripe.Event) error {
	var intent stripe.PaymentIntent
	if err := json.Unmarshal(event.Data.Raw, &intent); err != nil {
		return fmt.Errorf("decode payment intent: %w", err)
	}
	orgID, err := parseOrgID(intent.Metadata["org_id"])
	if err != nil || orgID == 0 {
		return nil
	}
	productID := intent.Metadata["product_id"]
	ledgerUnits, _ := strconv.ParseUint(intent.Metadata["ledger_units"], 10, 64)
	if productID == "" || ledgerUnits == 0 {
		return nil
	}
	startsAt, err := c.BusinessNow(ctx, c.queries, orgID, productID)
	if err != nil {
		return err
	}
	_, err = c.DepositCredits(ctx, GrantBalance{OrgID: orgID, ScopeType: "account", Source: "purchase", SourceReferenceID: "stripe_payment_intent:" + intent.ID, Amount: ledgerUnits, StartsAt: startsAt})
	return err
}

func (c *Client) handleInvoicePaid(ctx context.Context, event stripe.Event) error {
	var invoice stripe.Invoice
	if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
		return fmt.Errorf("decode invoice paid: %w", err)
	}
	changeIDValue := invoice.Metadata["change_id"]
	if changeIDValue == "" {
		return nil
	}
	quote, err := c.loadContractChangeQuote(ctx, changeIDValue)
	if err != nil {
		return err
	}
	return c.applyPaidUpgrade(ctx, quote, invoice.ID)
}

func (c *Client) handleInvoicePaymentFailed(ctx context.Context, event stripe.Event) error {
	var invoice stripe.Invoice
	if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
		return fmt.Errorf("decode invoice payment failed: %w", err)
	}
	changeIDValue := invoice.Metadata["change_id"]
	if changeIDValue == "" {
		return nil
	}
	_, err := c.pg.Exec(ctx, `UPDATE billing_documents SET status = 'payment_failed', payment_status = 'failed', stripe_invoice_id = $2, stripe_hosted_invoice_url = $3 WHERE change_id = $1`, changeIDValue, invoice.ID, invoice.HostedInvoiceURL)
	if err != nil {
		return err
	}
	_, err = c.pg.Exec(ctx, `UPDATE billing_finalizations SET state = 'payment_failed', last_error = 'stripe invoice payment failed', updated_at = now() WHERE finalization_id = $1`, finalizationID("contract_change", changeIDValue))
	return err
}

func (c *Client) loadContractChangeQuote(ctx context.Context, changeIDValue string) (contractChangeQuote, error) {
	var payload []byte
	err := c.pg.QueryRow(ctx, `SELECT payload FROM contract_changes WHERE change_id = $1`, changeIDValue).Scan(&payload)
	if err != nil {
		return contractChangeQuote{}, fmt.Errorf("load contract change payload: %w", err)
	}
	var quote contractChangeQuote
	if err := json.Unmarshal(payload, &quote); err != nil {
		return contractChangeQuote{}, fmt.Errorf("decode contract change payload: %w", err)
	}
	return quote, nil
}

func (c *Client) markProviderEventFinal(ctx context.Context, eventID, state string) error {
	var providerEventID, eventType, orgIDTextValue, productID, contractID, changeIDValue, finalizationIDValue, documentIDValue string
	err := c.pg.QueryRow(ctx, `
		UPDATE billing_provider_events SET state = $2, applied_at = now(), last_error = '', updated_at = now()
		WHERE event_id = $1 AND state = 'applying'
		RETURNING provider_event_id, event_type, COALESCE(org_id,''), COALESCE(product_id,''), COALESCE(contract_id,''), COALESCE(change_id,''), COALESCE(finalization_id,''), COALESCE(document_id,'')
	`, eventID, state).Scan(&providerEventID, &eventType, &orgIDTextValue, &productID, &contractID, &changeIDValue, &finalizationIDValue, &documentIDValue)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("mark provider event final: %w", err)
	}
	orgID, _ := parseOrgID(orgIDTextValue)
	if orgID == 0 {
		return nil
	}
	return c.WithTx(ctx, "billing.provider_event.final_event", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		return appendEvent(ctx, tx, q, eventFact{EventType: "provider_event_" + state, AggregateType: "provider_event", AggregateID: eventID, OrgID: orgID, ProductID: productID, OccurredAt: time.Now().UTC(), Payload: map[string]any{"provider_event_id": eventID, "provider_stripe_event_id": providerEventID, "provider_event_type": eventType, "contract_id": contractID, "change_id": changeIDValue, "finalization_id": finalizationIDValue, "document_id": documentIDValue}})
	})
}

func (c *Client) failProviderEvent(ctx context.Context, eventID string, cause error) error {
	_, err := c.pg.Exec(ctx, `UPDATE billing_provider_events SET state = CASE WHEN attempts >= 25 THEN 'dead_letter' ELSE 'failed' END, last_error = $2, next_attempt_at = now() + interval '30 seconds', updated_at = now() WHERE event_id = $1 AND state = 'applying'`, eventID, cause.Error())
	return err
}

func stripeEventMetadata(event stripe.Event) map[string]string {
	object := map[string]any{}
	_ = json.Unmarshal(event.Data.Raw, &object)
	metadata := stringMap(object["metadata"])
	metadata["customer_id"] = stringValue(object["customer"])
	metadata["provider_invoice_id"] = ""
	metadata["provider_payment_intent_id"] = ""
	switch {
	case strings.HasPrefix(string(event.Type), "invoice."):
		metadata["provider_invoice_id"] = stringValue(object["id"])
		metadata["provider_payment_intent_id"] = stringValue(object["payment_intent"])
	case strings.HasPrefix(string(event.Type), "payment_intent."):
		metadata["provider_payment_intent_id"] = stringValue(object["id"])
	}
	return metadata
}

func stripeEventObjectID(event stripe.Event) string {
	object := map[string]any{}
	_ = json.Unmarshal(event.Data.Raw, &object)
	return stringValue(object["id"])
}

func stripeProviderObjectType(eventType string) string {
	parts := strings.Split(eventType, ".")
	if len(parts) <= 1 {
		return eventType
	}
	return strings.Join(parts[:len(parts)-1], ".")
}

func stringMap(value any) map[string]string {
	out := map[string]string{}
	if m, ok := value.(map[string]any); ok {
		for key, raw := range m {
			out[key] = stringValue(raw)
		}
	}
	return out
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case map[string]any:
		return stringValue(typed["id"])
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func parseOrgID(value string) (OrgID, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse org_id %q: %w", value, err)
	}
	return OrgID(parsed), nil
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}
