package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/stripe/stripe-go/v85"
	"github.com/stripe/stripe-go/v85/webhook"
)

func (c *Client) CreateCheckoutSession(ctx context.Context, orgID OrgID, productID string, params CheckoutParams) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var displayName string
	if err := c.pg.QueryRowContext(ctx, `
		SELECT display_name FROM products WHERE product_id = $1
	`, productID).Scan(&displayName); err != nil {
		return "", fmt.Errorf("read product for checkout: %w", err)
	}

	orgIDText := strconv.FormatUint(uint64(orgID), 10)
	var stripeCustomerID sql.NullString
	_ = c.pg.QueryRowContext(ctx, `
		SELECT stripe_customer_id FROM orgs WHERE org_id = $1
	`, orgIDText).Scan(&stripeCustomerID)

	sessionParams := &stripe.CheckoutSessionCreateParams{
		Mode:             stripe.String(string(stripe.CheckoutSessionModePayment)),
		CustomerCreation: stripe.String("always"),
		SuccessURL:       stripe.String(params.SuccessURL),
		CancelURL:        stripe.String(params.CancelURL),
		PaymentMethodOptions: &stripe.CheckoutSessionCreatePaymentMethodOptionsParams{
			Card: &stripe.CheckoutSessionCreatePaymentMethodOptionsCardParams{
				RequestThreeDSecure: stripe.String(
					string(stripe.CheckoutSessionPaymentMethodOptionsCardRequestThreeDSecureAny),
				),
			},
		},
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{{
			Quantity: stripe.Int64(1),
			PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{
				Currency:   stripe.String("usd"),
				UnitAmount: stripe.Int64(params.AmountCents),
				ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{
					Name: stripe.String(displayName + " Credits"),
				},
			},
		}},
		PaymentIntentData: &stripe.CheckoutSessionCreatePaymentIntentDataParams{
			Metadata: map[string]string{
				"org_id":       orgIDText,
				"product_id":   productID,
				"ledger_units": strconv.FormatInt(params.AmountCents*100_000, 10),
			},
		},
	}
	if stripeCustomerID.Valid && stripeCustomerID.String != "" {
		sessionParams.Customer = stripe.String(stripeCustomerID.String)
		sessionParams.CustomerCreation = nil
	}
	sessionParams.AddMetadata("org_id", orgIDText)
	sessionParams.AddMetadata("product_id", productID)
	sessionParams.AddMetadata("ledger_units", strconv.FormatInt(params.AmountCents*100_000, 10))

	session, err := c.stripe.V1CheckoutSessions.Create(ctx, sessionParams)
	if err != nil {
		return "", fmt.Errorf("create checkout session: %w", err)
	}
	return session.URL, nil
}

func (c *Client) CreateContract(ctx context.Context, orgID OrgID, planID string, cadence BillingCadence, successURL, cancelURL string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if planID == "" {
		return "", fmt.Errorf("plan_id is required")
	}
	if cadence == "" {
		cadence = CadenceMonthly
	}
	if cadence != CadenceMonthly {
		return "", fmt.Errorf("%w: %q", ErrUnsupportedCadence, cadence)
	}

	var productID string
	var displayName string
	var currency string
	if err := c.pg.QueryRowContext(ctx, `
		SELECT p.product_id, p.display_name, p.currency
		FROM plans p
		WHERE p.plan_id = $1
		  AND p.active
	`, planID).Scan(&productID, &displayName, &currency); err != nil {
		return "", fmt.Errorf("read plan for contract: %w", err)
	}
	if currency == "" {
		currency = "usd"
	}

	contractID := deterministicTextID("self-serve-contract", strconv.FormatUint(uint64(orgID), 10), productID)
	phaseID := deterministicTextID("self-serve-contract-phase", contractID, planID)

	customerID, err := c.ensureStripeCustomer(ctx, orgID)
	if err != nil {
		return "", err
	}

	metadata := map[string]string{
		"org_id":      strconv.FormatUint(uint64(orgID), 10),
		"product_id":  productID,
		"plan_id":     planID,
		"contract_id": contractID,
		"phase_id":    phaseID,
		"cadence":     string(cadence),
	}
	sessionParams := &stripe.CheckoutSessionCreateParams{
		Mode:       stripe.String(string(stripe.CheckoutSessionModeSetup)),
		Customer:   stripe.String(customerID),
		Currency:   stripe.String(currency),
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
		SetupIntentData: &stripe.CheckoutSessionCreateSetupIntentDataParams{
			Description: stripe.String(displayName + " payment method"),
			Metadata:    metadata,
		},
		Metadata: metadata,
	}

	session, err := c.stripe.V1CheckoutSessions.Create(ctx, sessionParams)
	if err != nil {
		return "", fmt.Errorf("create contract payment-method checkout session: %w", err)
	}
	return session.URL, nil
}

func (c *Client) ensureStripeCustomer(ctx context.Context, orgID OrgID) (string, error) {
	orgIDText := strconv.FormatUint(uint64(orgID), 10)
	var stripeCustomerID sql.NullString
	var billingEmail sql.NullString
	if err := c.pg.QueryRowContext(ctx, `
		SELECT stripe_customer_id, billing_email FROM orgs WHERE org_id = $1
	`, orgIDText).Scan(&stripeCustomerID, &billingEmail); err != nil {
		return "", fmt.Errorf("lookup org stripe customer: %w", err)
	}
	if stripeCustomerID.Valid && stripeCustomerID.String != "" {
		return stripeCustomerID.String, nil
	}

	params := &stripe.CustomerCreateParams{
		Metadata: map[string]string{"org_id": orgIDText},
	}
	params.SetIdempotencyKey("forge-metal:stripe-customer:" + orgIDText)
	if billingEmail.Valid && billingEmail.String != "" {
		params.Email = stripe.String(billingEmail.String)
	}
	customer, err := c.stripe.V1Customers.Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("create stripe customer: %w", err)
	}
	if _, err := c.pg.ExecContext(ctx, `
		UPDATE orgs
		SET stripe_customer_id = $2,
		    updated_at = now()
		WHERE org_id = $1
	`, orgIDText, customer.ID); err != nil {
		return "", fmt.Errorf("persist stripe customer: %w", err)
	}
	return customer.ID, nil
}

func (c *Client) CreatePortalSession(ctx context.Context, orgID OrgID, returnURL string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	orgIDText := strconv.FormatUint(uint64(orgID), 10)
	var stripeCustomerID sql.NullString
	_ = c.pg.QueryRowContext(ctx, `
		SELECT stripe_customer_id FROM orgs WHERE org_id = $1
	`, orgIDText).Scan(&stripeCustomerID)
	if !stripeCustomerID.Valid || stripeCustomerID.String == "" {
		return "", ErrNoStripeCustomer
	}

	session, err := c.stripe.V1BillingPortalSessions.Create(ctx, &stripe.BillingPortalSessionCreateParams{
		Customer:  stripe.String(stripeCustomerID.String),
		ReturnURL: stripe.String(returnURL),
	})
	if err != nil {
		return "", fmt.Errorf("create portal session: %w", err)
	}
	return session.URL, nil
}

func (c *Client) HandleStripeWebhook(ctx context.Context, payload []byte, signatureHeader string, webhookSecret string) error {
	event, err := webhook.ConstructEvent(payload, signatureHeader, webhookSecret)
	if err != nil {
		return fmt.Errorf("construct stripe webhook event: %w", err)
	}

	providerEventID, err := c.recordStripeProviderEvent(ctx, event, payload)
	if err != nil {
		return err
	}

	var handleErr error
	finalState := "applied"
	switch event.Type {
	case "checkout.session.completed":
		handleErr = c.handleCheckoutSessionCompleted(ctx, providerEventID, event)
	case "setup_intent.succeeded":
		handleErr = c.handleSetupIntentSucceeded(ctx, providerEventID, event)
	case "payment_intent.succeeded":
		handleErr = c.handlePaymentIntentSucceeded(ctx, event)
	case "invoice.paid":
		handleErr = c.handleInvoicePaid(ctx, event)
	case "invoice.payment_failed":
		handleErr = c.handleInvoicePaymentFailed(ctx, event)
	default:
		finalState = "ignored"
	}
	if handleErr != nil {
		_ = c.markStripeProviderEvent(ctx, providerEventID, "failed", handleErr.Error())
		return handleErr
	}
	return c.markStripeProviderEvent(ctx, providerEventID, finalState, "")
}

func (c *Client) recordStripeProviderEvent(ctx context.Context, event stripe.Event, payload []byte) (string, error) {
	if event.ID == "" {
		return "", fmt.Errorf("stripe event id is required")
	}
	eventID := deterministicTextID("stripe-provider-event", event.ID)
	_, err := c.pg.ExecContext(ctx, `
		INSERT INTO billing_provider_events (
			event_id, provider, provider_event_id, event_type, state, livemode,
			provider_created_at, payload
		)
		VALUES ($1, 'stripe', $2, $3, 'received', $4, $5, $6::jsonb)
		ON CONFLICT (provider, provider_event_id) DO UPDATE
		SET event_type = EXCLUDED.event_type,
		    livemode = EXCLUDED.livemode,
		    provider_created_at = EXCLUDED.provider_created_at,
		    payload = EXCLUDED.payload,
		    updated_at = now()
	`, eventID, event.ID, string(event.Type), event.Livemode, time.Unix(event.Created, 0).UTC(), string(payload))
	if err != nil {
		return "", fmt.Errorf("record stripe provider event %s: %w", event.ID, err)
	}
	return eventID, nil
}

func (c *Client) markStripeProviderEvent(ctx context.Context, eventID string, state string, failure string) error {
	if eventID == "" {
		return nil
	}
	_, err := c.pg.ExecContext(ctx, `
		UPDATE billing_provider_events
		SET state = $2,
		    applied_at = CASE WHEN $2 IN ('applied', 'ignored') THEN now() ELSE applied_at END,
		    attempts = attempts + CASE WHEN $2 = 'failed' THEN 1 ELSE 0 END,
		    last_error = $3,
		    updated_at = now()
		WHERE event_id = $1
	`, eventID, state, failure)
	if err != nil {
		return fmt.Errorf("mark stripe provider event %s: %w", eventID, err)
	}
	return nil
}

func (c *Client) handleCheckoutSessionCompleted(ctx context.Context, providerEventID string, event stripe.Event) error {
	session, err := decodeStripeEventObject[stripe.CheckoutSession](event, "checkout.session.completed")
	if err != nil {
		return err
	}
	if err := c.annotateCheckoutSessionProviderEvent(ctx, providerEventID, session); err != nil {
		return err
	}

	customerID := stripeCustomerID(session.Customer)
	orgIDText := session.Metadata["org_id"]
	if customerID != "" && orgIDText != "" {
		_, err := c.pg.ExecContext(ctx, `
			UPDATE orgs
			SET stripe_customer_id = $1,
			    updated_at = now()
			WHERE org_id = $2
		`, customerID, orgIDText)
		if err != nil {
			return fmt.Errorf("checkout.session.completed: update customer: %w", err)
		}
	}
	if session.SetupIntent != nil && session.SetupIntent.ID != "" {
		intent, err := c.retrieveCheckoutSetupIntent(ctx, session)
		if err != nil {
			return err
		}
		return c.upsertPaymentMethodFromSetupIntent(ctx, intent)
	}
	return nil
}

func (c *Client) handleSetupIntentSucceeded(ctx context.Context, providerEventID string, event stripe.Event) error {
	intent, err := decodeStripeEventObject[stripe.SetupIntent](event, "setup_intent.succeeded")
	if err != nil {
		return err
	}
	if err := c.annotateSetupIntentProviderEvent(ctx, providerEventID, intent); err != nil {
		return err
	}
	return c.upsertPaymentMethodFromSetupIntent(ctx, intent)
}

func (c *Client) retrieveCheckoutSetupIntent(ctx context.Context, session stripe.CheckoutSession) (stripe.SetupIntent, error) {
	params := &stripe.SetupIntentRetrieveParams{}
	params.AddExpand("payment_method")
	intent, err := c.stripe.V1SetupIntents.Retrieve(ctx, session.SetupIntent.ID, params)
	if err != nil {
		return stripe.SetupIntent{}, fmt.Errorf("checkout.session.completed: retrieve setup intent: %w", err)
	}
	if intent == nil {
		return stripe.SetupIntent{}, fmt.Errorf("checkout.session.completed: missing setup intent %s", session.SetupIntent.ID)
	}
	if intent.Metadata == nil {
		intent.Metadata = map[string]string{}
	}
	for key, value := range session.Metadata {
		if intent.Metadata[key] == "" {
			intent.Metadata[key] = value
		}
	}
	return *intent, nil
}

func (c *Client) annotateCheckoutSessionProviderEvent(ctx context.Context, eventID string, session stripe.CheckoutSession) error {
	setupIntentID := ""
	if session.SetupIntent != nil {
		setupIntentID = session.SetupIntent.ID
	}
	normalized := map[string]string{
		"checkout_session_id": session.ID,
		"mode":                string(session.Mode),
		"payment_status":      string(session.PaymentStatus),
		"setup_intent_id":     setupIntentID,
	}
	return c.annotateStripeProviderEvent(ctx, eventID, providerEventAnnotation{
		OrgID:            session.Metadata["org_id"],
		ProductID:        session.Metadata["product_id"],
		ContractID:       session.Metadata["contract_id"],
		ProviderCustomer: stripeCustomerID(session.Customer),
		ObjectType:       "checkout.session",
		ObjectID:         session.ID,
		Normalized:       normalized,
	})
}

func (c *Client) annotateSetupIntentProviderEvent(ctx context.Context, eventID string, intent stripe.SetupIntent) error {
	paymentMethodID := ""
	if intent.PaymentMethod != nil {
		paymentMethodID = intent.PaymentMethod.ID
	}
	normalized := map[string]string{
		"setup_intent_id":   intent.ID,
		"payment_method_id": paymentMethodID,
		"status":            string(intent.Status),
	}
	return c.annotateStripeProviderEvent(ctx, eventID, providerEventAnnotation{
		OrgID:            intent.Metadata["org_id"],
		ProductID:        intent.Metadata["product_id"],
		ContractID:       intent.Metadata["contract_id"],
		ProviderCustomer: stripeCustomerID(intent.Customer),
		ObjectType:       "setup_intent",
		ObjectID:         intent.ID,
		Normalized:       normalized,
	})
}

type providerEventAnnotation struct {
	OrgID            string
	ProductID        string
	ContractID       string
	ProviderCustomer string
	ObjectType       string
	ObjectID         string
	Normalized       map[string]string
}

func (c *Client) annotateStripeProviderEvent(ctx context.Context, eventID string, annotation providerEventAnnotation) error {
	if eventID == "" {
		return nil
	}
	normalized, err := json.Marshal(annotation.Normalized)
	if err != nil {
		return fmt.Errorf("marshal stripe provider event annotation: %w", err)
	}
	_, err = c.pg.ExecContext(ctx, `
		UPDATE billing_provider_events
		SET org_id = $2,
		    product_id = $3,
		    contract_id = $4,
		    provider_customer_id = $5,
		    provider_object_type = $6,
		    provider_object_id = $7,
		    normalized_payload = $8::jsonb,
		    updated_at = now()
		WHERE event_id = $1
	`, eventID, annotation.OrgID, annotation.ProductID, annotation.ContractID,
		annotation.ProviderCustomer, annotation.ObjectType, annotation.ObjectID, string(normalized))
	if err != nil {
		return fmt.Errorf("annotate stripe provider event %s: %w", eventID, err)
	}
	return nil
}

func (c *Client) upsertPaymentMethodFromSetupIntent(ctx context.Context, intent stripe.SetupIntent) error {
	orgIDText := intent.Metadata["org_id"]
	if orgIDText == "" || intent.PaymentMethod == nil || intent.PaymentMethod.ID == "" {
		return nil
	}
	customerID := stripeCustomerID(intent.Customer)
	paymentMethodID := deterministicTextID("stripe-payment-method", intent.PaymentMethod.ID)
	brand := ""
	last4 := ""
	var expMonth any = nil
	var expYear any = nil
	if intent.PaymentMethod.Card != nil {
		brand = string(intent.PaymentMethod.Card.Brand)
		last4 = intent.PaymentMethod.Card.Last4
		if intent.PaymentMethod.Card.ExpMonth != 0 {
			expMonth = intent.PaymentMethod.Card.ExpMonth
		}
		if intent.PaymentMethod.Card.ExpYear != 0 {
			expYear = intent.PaymentMethod.Card.ExpYear
		}
	}
	tx, err := c.pg.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin stripe payment method upsert: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE payment_methods
		SET is_default = false,
		    updated_at = now()
		WHERE org_id = $1
		  AND is_default
	`, orgIDText); err != nil {
		return fmt.Errorf("clear prior default stripe payment method: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO payment_methods (
			payment_method_id, org_id, provider, provider_payment_method_id,
			provider_customer_id, status, is_default, card_brand, card_last4,
			card_exp_month, card_exp_year
		)
		VALUES ($1, $2, 'stripe', $3, $4, 'active', true, $5, $6, $7, $8)
		ON CONFLICT (provider, provider_payment_method_id) WHERE provider_payment_method_id <> '' DO UPDATE
		SET status = 'active',
		    is_default = true,
		    provider_customer_id = EXCLUDED.provider_customer_id,
		    card_brand = EXCLUDED.card_brand,
		    card_last4 = EXCLUDED.card_last4,
		    card_exp_month = EXCLUDED.card_exp_month,
		    card_exp_year = EXCLUDED.card_exp_year,
		    updated_at = now()
	`, paymentMethodID, orgIDText, intent.PaymentMethod.ID, customerID, brand, last4, expMonth, expYear)
	if err != nil {
		return fmt.Errorf("upsert stripe payment method: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit stripe payment method upsert: %w", err)
	}

	contractID := intent.Metadata["contract_id"]
	phaseID := intent.Metadata["phase_id"]
	planID := intent.Metadata["plan_id"]
	productID := intent.Metadata["product_id"]
	cadence := intent.Metadata["cadence"]
	if contractID == "" || phaseID == "" || planID == "" || productID == "" {
		return nil
	}
	rawOrgID, err := strconv.ParseUint(orgIDText, 10, 64)
	if err != nil {
		return fmt.Errorf("setup_intent.succeeded: parse org id: %w", err)
	}
	effectiveAt := setupIntentEffectiveAt(intent, c.clock().UTC())
	if err := c.activateCatalogContract(ctx, OrgID(rawOrgID), productID, planID, contractID, phaseID, cadence, effectiveAt, PaymentPaid); err != nil {
		return fmt.Errorf("activate contract from setup intent: %w", err)
	}
	return nil
}

func setupIntentEffectiveAt(intent stripe.SetupIntent, fallback time.Time) time.Time {
	if intent.Created > 0 {
		return time.Unix(intent.Created, 0).UTC()
	}
	return fallback.UTC()
}

func (c *Client) handlePaymentIntentSucceeded(ctx context.Context, event stripe.Event) error {
	intent, err := decodeStripeEventObject[stripe.PaymentIntent](event, "payment_intent.succeeded")
	if err != nil {
		return err
	}

	paymentIntentID := intent.ID
	orgIDText := intent.Metadata["org_id"]
	productID := intent.Metadata["product_id"]
	ledgerUnitsText := intent.Metadata["ledger_units"]
	if paymentIntentID == "" || orgIDText == "" || productID == "" || ledgerUnitsText == "" {
		return nil
	}

	orgID, err := strconv.ParseUint(orgIDText, 10, 64)
	if err != nil {
		return fmt.Errorf("payment_intent.succeeded: parse org id: %w", err)
	}
	ledgerUnits, err := strconv.ParseUint(ledgerUnitsText, 10, 64)
	if err != nil {
		return fmt.Errorf("payment_intent.succeeded: parse ledger units: %w", err)
	}
	// Prepaid account credit is modeled as a debit-card balance, not a coupon:
	// it must not appear to expire in the customer-facing UI.
	expiresAt := c.clock().UTC().AddDate(100, 0, 0)
	_, err = c.DepositCredits(ctx, CreditGrant{
		OrgID:             OrgID(orgID),
		ScopeType:         GrantScopeAccount,
		Amount:            ledgerUnits,
		Source:            "purchase",
		SourceReferenceID: "stripe:payment_intent:" + paymentIntentID,
		ExpiresAt:         &expiresAt,
	})
	if err != nil {
		return fmt.Errorf("payment_intent.succeeded: deposit credits: %w", err)
	}
	return nil
}

func (c *Client) handleInvoicePaid(ctx context.Context, event stripe.Event) error {
	invoice, err := decodeStripeEventObject[stripe.Invoice](event, "invoice.paid")
	if err != nil {
		return err
	}
	if invoice.ID == "" {
		return nil
	}
	_, err = c.pg.ExecContext(ctx, `
		UPDATE billing_invoices
		SET payment_status = 'paid',
		    status = CASE WHEN status = 'issued' THEN 'paid' ELSE status END,
		    updated_at = now()
		WHERE stripe_invoice_id = $1
	`, invoice.ID)
	if err != nil {
		return fmt.Errorf("invoice.paid: update billing invoice: %w", err)
	}
	return nil
}

func (c *Client) handleInvoicePaymentFailed(ctx context.Context, event stripe.Event) error {
	invoice, err := decodeStripeEventObject[stripe.Invoice](event, "invoice.payment_failed")
	if err != nil {
		return err
	}
	if invoice.ID == "" {
		return nil
	}
	_, err = c.pg.ExecContext(ctx, `
		UPDATE billing_invoices
		SET payment_status = 'failed',
		    status = CASE WHEN status = 'issued' THEN 'payment_failed' ELSE status END,
		    updated_at = now()
		WHERE stripe_invoice_id = $1
	`, invoice.ID)
	if err != nil {
		return fmt.Errorf("invoice.payment_failed: update billing invoice: %w", err)
	}
	return nil
}

func (c *Client) contractEntitlementPolicies(ctx context.Context, planID string, periodStart time.Time, periodEnd time.Time) ([]EntitlementPolicy, error) {
	rows, err := c.pg.QueryContext(ctx, `
		SELECT p.policy_id, p.source, p.product_id, p.scope_type, p.scope_product_id, p.scope_bucket_id, p.scope_sku_id,
		       p.amount_units, p.cadence, p.anchor_kind, p.proration_mode, p.policy_version, p.active_from, p.active_until
		FROM plan_entitlements pe
		JOIN entitlement_policies p ON p.policy_id = pe.policy_id
		WHERE pe.plan_id = $1
		  AND p.source = 'contract'
		  AND p.active_from < $2
		  AND (p.active_until IS NULL OR p.active_until > $3)
		ORDER BY p.policy_id
	`, planID, periodEnd.UTC(), periodStart.UTC())
	if err != nil {
		return nil, fmt.Errorf("lookup plan entitlement policies: %w", err)
	}
	defer rows.Close()

	var out []EntitlementPolicy
	for rows.Next() {
		var policy EntitlementPolicy
		var sourceText, scopeText string
		var amount int64
		var activeUntil sql.NullTime
		if err := rows.Scan(
			&policy.PolicyID,
			&sourceText,
			&policy.ProductID,
			&scopeText,
			&policy.ScopeProductID,
			&policy.ScopeBucketID,
			&policy.ScopeSKUID,
			&amount,
			&policy.Cadence,
			&policy.AnchorKind,
			&policy.ProrationMode,
			&policy.PolicyVersion,
			&policy.ActiveFrom,
			&activeUntil,
		); err != nil {
			return nil, fmt.Errorf("scan plan entitlement policy: %w", err)
		}
		source, err := ParseGrantSourceType(sourceText)
		if err != nil {
			return nil, err
		}
		scope, err := ParseGrantScopeType(scopeText)
		if err != nil {
			return nil, err
		}
		if amount < 0 {
			return nil, fmt.Errorf("policy %s has negative amount", policy.PolicyID)
		}
		policy.Source = source
		policy.ScopeType = scope
		policy.AmountUnits = uint64(amount)
		policy.ActiveFrom = policy.ActiveFrom.UTC()
		if activeUntil.Valid {
			value := activeUntil.Time.UTC()
			policy.ActiveUntil = &value
		}
		if err := validateGrantScope(policy.ScopeType, policy.ScopeProductID, policy.ScopeBucketID, policy.ScopeSKUID); err != nil {
			return nil, fmt.Errorf("policy %s: %w", policy.PolicyID, err)
		}
		out = append(out, policy)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plan entitlement policies: %w", err)
	}
	return out, nil
}

func decodeStripeEventObject[T any](event stripe.Event, eventType string) (T, error) {
	var out T
	if event.Data == nil || len(event.Data.Raw) == 0 {
		return out, fmt.Errorf("%s: missing Stripe event object", eventType)
	}
	if err := json.Unmarshal(event.Data.Raw, &out); err != nil {
		return out, fmt.Errorf("%s: decode Stripe event object: %w", eventType, err)
	}
	return out, nil
}

func stripeCustomerID(customer *stripe.Customer) string {
	if customer == nil {
		return ""
	}
	return customer.ID
}
