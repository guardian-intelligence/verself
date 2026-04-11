package billing

import (
	"context"
	"database/sql"
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

func (c *Client) CreateSubscription(ctx context.Context, orgID OrgID, planID string, cadence BillingCadence, successURL, cancelURL string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if cadence == "" {
		cadence = CadenceMonthly
	}
	priceColumn := "stripe_price_id_monthly"
	if cadence == CadenceAnnual {
		priceColumn = "stripe_price_id_annual"
	}

	var productID, stripePriceID string
	// priceColumn is a hardcoded column name (not user input), safe to concatenate.
	if err := c.pg.QueryRowContext(ctx,
		`SELECT product_id, `+priceColumn+` FROM plans WHERE plan_id = $1 AND active`, planID,
	).Scan(&productID, &stripePriceID); err != nil {
		return "", fmt.Errorf("read plan for subscription: %w", err)
	}
	if stripePriceID == "" {
		return "", fmt.Errorf("plan %s has no stripe price for cadence %s", planID, cadence)
	}

	orgIDText := strconv.FormatUint(uint64(orgID), 10)
	var stripeCustomerID sql.NullString
	_ = c.pg.QueryRowContext(ctx, `
		SELECT stripe_customer_id FROM orgs WHERE org_id = $1
	`, orgIDText).Scan(&stripeCustomerID)

	sessionParams := &stripe.CheckoutSessionCreateParams{
		Mode:             stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		CustomerCreation: stripe.String("always"),
		SuccessURL:       stripe.String(successURL),
		CancelURL:        stripe.String(cancelURL),
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{{
			Price:    stripe.String(stripePriceID),
			Quantity: stripe.Int64(1),
		}},
		SubscriptionData: &stripe.CheckoutSessionCreateSubscriptionDataParams{
			Metadata: map[string]string{
				"org_id":     orgIDText,
				"plan_id":    planID,
				"product_id": productID,
				"cadence":    string(cadence),
			},
		},
	}
	if stripeCustomerID.Valid && stripeCustomerID.String != "" {
		sessionParams.Customer = stripe.String(stripeCustomerID.String)
		sessionParams.CustomerCreation = nil
	}
	sessionParams.AddMetadata("org_id", orgIDText)
	sessionParams.AddMetadata("plan_id", planID)
	sessionParams.AddMetadata("product_id", productID)
	sessionParams.AddMetadata("cadence", string(cadence))

	session, err := c.stripe.V1CheckoutSessions.Create(ctx, sessionParams)
	if err != nil {
		return "", fmt.Errorf("create subscription checkout session: %w", err)
	}
	return session.URL, nil
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

	switch event.Type {
	case "checkout.session.completed":
		return c.handleCheckoutSessionCompleted(ctx, event)
	case "payment_intent.succeeded":
		return c.handlePaymentIntentSucceeded(ctx, event)
	case "invoice.paid":
		return c.handleInvoicePaid(ctx, event)
	case "customer.subscription.updated":
		return c.handleSubscriptionUpdated(ctx, event)
	case "customer.subscription.deleted":
		return c.handleSubscriptionDeleted(ctx, event)
	default:
		return nil
	}
}

func (c *Client) handleCheckoutSessionCompleted(ctx context.Context, event stripe.Event) error {
	customerID := event.GetObjectValue("customer")
	orgIDText := event.GetObjectValue("metadata", "org_id")
	if customerID == "" || orgIDText == "" {
		return nil
	}
	_, err := c.pg.ExecContext(ctx, `
		UPDATE orgs
		SET stripe_customer_id = $1,
		    updated_at = now()
		WHERE org_id = $2
	`, customerID, orgIDText)
	if err != nil {
		return fmt.Errorf("checkout.session.completed: update customer: %w", err)
	}

	mode := event.GetObjectValue("mode")
	if mode == "subscription" {
		stripeSubID := event.GetObjectValue("subscription")
		planID := event.GetObjectValue("metadata", "plan_id")
		productID := event.GetObjectValue("metadata", "product_id")
		cadence := event.GetObjectValue("metadata", "cadence")
		if stripeSubID == "" || planID == "" || productID == "" {
			return nil
		}
		if cadence == "" {
			cadence = "monthly"
		}

		// Idempotent insert — a duplicate webhook delivery must not create a second row.
		// No unique index on stripe_subscription_id, so use a subquery guard.
		_, err := c.pg.ExecContext(ctx, `
			INSERT INTO subscriptions (org_id, product_id, plan_id, cadence, status, stripe_subscription_id, stripe_checkout_session_id)
			SELECT $1, $2, $3, $4, 'active', $5, $6
			WHERE NOT EXISTS (
				SELECT 1 FROM subscriptions WHERE stripe_subscription_id = $5
			)
		`, orgIDText, productID, planID, cadence, stripeSubID, event.GetObjectValue("id"))
		if err != nil {
			return fmt.Errorf("checkout.session.completed: insert subscription: %w", err)
		}
	}

	return nil
}

func (c *Client) handlePaymentIntentSucceeded(ctx context.Context, event stripe.Event) error {
	paymentIntentID := event.GetObjectValue("id")
	orgIDText := event.GetObjectValue("metadata", "org_id")
	productID := event.GetObjectValue("metadata", "product_id")
	ledgerUnitsText := event.GetObjectValue("metadata", "ledger_units")
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
	expiresAt := c.clock().UTC().AddDate(1, 0, 0)
	_, err = c.DepositCredits(ctx, CreditGrant{
		OrgID:             OrgID(orgID),
		ProductID:         productID,
		Amount:            ledgerUnits,
		Source:            "purchase",
		StripeReferenceID: paymentIntentID,
		ExpiresAt:         &expiresAt,
	})
	if err != nil {
		return fmt.Errorf("payment_intent.succeeded: deposit credits: %w", err)
	}
	return nil
}

func (c *Client) handleInvoicePaid(ctx context.Context, event stripe.Event) error {
	invoiceID := event.GetObjectValue("id")
	stripeSubID := event.GetObjectValue("subscription")
	if invoiceID == "" || stripeSubID == "" {
		return nil
	}

	var orgIDText, productID, planID string
	err := c.pg.QueryRowContext(ctx, `
		SELECT org_id, product_id, plan_id FROM subscriptions WHERE stripe_subscription_id = $1
	`, stripeSubID).Scan(&orgIDText, &productID, &planID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("invoice.paid: lookup subscription: %w", err)
	}

	var includedCredits sql.NullInt64
	if err := c.pg.QueryRowContext(ctx, `
		SELECT included_credits FROM plans WHERE plan_id = $1
	`, planID).Scan(&includedCredits); err != nil {
		return fmt.Errorf("invoice.paid: lookup plan credits: %w", err)
	}
	if !includedCredits.Valid || includedCredits.Int64 == 0 {
		return nil
	}

	orgID, err := strconv.ParseUint(orgIDText, 10, 64)
	if err != nil {
		return fmt.Errorf("invoice.paid: parse org id: %w", err)
	}

	expiresAt := c.clock().UTC().AddDate(1, 0, 0)
	_, err = c.DepositCredits(ctx, CreditGrant{
		OrgID:             OrgID(orgID),
		ProductID:         productID,
		Amount:            uint64(includedCredits.Int64),
		Source:            "subscription",
		StripeReferenceID: invoiceID,
		ExpiresAt:         &expiresAt,
	})
	if err != nil {
		return fmt.Errorf("invoice.paid: deposit credits: %w", err)
	}
	return nil
}

func (c *Client) handleSubscriptionUpdated(ctx context.Context, event stripe.Event) error {
	stripeSubID := event.GetObjectValue("id")
	status := event.GetObjectValue("status")
	if stripeSubID == "" {
		return nil
	}

	var periodStart, periodEnd sql.NullTime
	if startStr := event.GetObjectValue("current_period_start"); startStr != "" {
		if ts, err := strconv.ParseInt(startStr, 10, 64); err == nil {
			periodStart = sql.NullTime{Time: time.Unix(ts, 0).UTC(), Valid: true}
		}
	}
	if endStr := event.GetObjectValue("current_period_end"); endStr != "" {
		if ts, err := strconv.ParseInt(endStr, 10, 64); err == nil {
			periodEnd = sql.NullTime{Time: time.Unix(ts, 0).UTC(), Valid: true}
		}
	}

	_, err := c.pg.ExecContext(ctx, `
		UPDATE subscriptions
		SET status = $1,
		    current_period_start = $2,
		    current_period_end = $3,
		    updated_at = now()
		WHERE stripe_subscription_id = $4
	`, status, periodStart, periodEnd, stripeSubID)
	if err != nil {
		return fmt.Errorf("customer.subscription.updated: update subscription: %w", err)
	}
	return nil
}

func (c *Client) handleSubscriptionDeleted(ctx context.Context, event stripe.Event) error {
	stripeSubID := event.GetObjectValue("id")
	if stripeSubID == "" {
		return nil
	}

	_, err := c.pg.ExecContext(ctx, `
		UPDATE subscriptions SET status = 'canceled', updated_at = now() WHERE stripe_subscription_id = $1
	`, stripeSubID)
	if err != nil {
		return fmt.Errorf("customer.subscription.deleted: update subscription: %w", err)
	}
	return nil
}
