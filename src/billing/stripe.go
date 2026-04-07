package billing

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/stripe/stripe-go/v85"
)

// BillingCadence matches the PostgreSQL billing_cadence enum.
type BillingCadence string

const (
	CadenceMonthly BillingCadence = "monthly"
	CadenceAnnual  BillingCadence = "annual"
)

// CreateCheckoutSession creates a Stripe Checkout session for a one-time
// credit purchase. Returns the Checkout session URL for redirect.
//
// Spec §2.9: mode=payment, customer_creation=always,
// payment_method_options.card.request_three_d_secure=any,
// metadata includes org_id and product_id.
func (c *Client) CreateCheckoutSession(ctx context.Context, orgID OrgID, productID string, params CheckoutParams) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Read product display name for the line item description.
	var displayName string
	err := c.pg.QueryRowContext(ctx, `
		SELECT display_name FROM products WHERE product_id = $1
	`, productID).Scan(&displayName)
	if err != nil {
		return "", fmt.Errorf("create checkout session: read product: %w", err)
	}

	// Look up existing Stripe customer ID for this org if we have one.
	orgIDStr := strconv.FormatUint(uint64(orgID), 10)
	var stripeCustomerID sql.NullString
	_ = c.pg.QueryRowContext(ctx, `
		SELECT stripe_customer_id FROM orgs WHERE org_id = $1
	`, orgIDStr).Scan(&stripeCustomerID)

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
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{
			{
				Quantity: stripe.Int64(1),
				PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{
					Currency:   stripe.String("usd"),
					UnitAmount: stripe.Int64(params.AmountCents),
					ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{
						Name: stripe.String(displayName + " Credits"),
					},
				},
			},
		},
	}

	if stripeCustomerID.Valid {
		sessionParams.Customer = stripe.String(stripeCustomerID.String)
		sessionParams.CustomerCreation = nil // can't set both
	}

	sessionParams.AddMetadata("org_id", orgIDStr)
	sessionParams.AddMetadata("product_id", productID)

	// Propagate metadata to the payment intent so payment_intent.succeeded
	// webhooks carry org_id and product_id (Stripe does not inherit session
	// metadata onto the PI automatically).
	sessionParams.PaymentIntentData = &stripe.CheckoutSessionCreatePaymentIntentDataParams{
		Metadata: map[string]string{
			"org_id":     orgIDStr,
			"product_id": productID,
		},
	}

	session, err := c.stripe.V1CheckoutSessions.Create(ctx, sessionParams)
	if err != nil {
		return "", fmt.Errorf("create checkout session: stripe: %w", err)
	}

	return session.URL, nil
}

// CreateSubscription creates a Stripe Checkout session for subscription
// signup. Reads stripe_monthly_price_id or stripe_annual_price_id from the
// plans table based on the requested cadence.
//
// Returns the Checkout session URL.
func (c *Client) CreateSubscription(ctx context.Context, orgID OrgID, planID string, cadence BillingCadence, successURL, cancelURL string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Read the Stripe price ID for the requested cadence.
	var priceColumn string
	switch cadence {
	case CadenceMonthly:
		priceColumn = "stripe_monthly_price_id"
	case CadenceAnnual:
		priceColumn = "stripe_annual_price_id"
	default:
		return "", fmt.Errorf("create subscription: unsupported cadence %q", cadence)
	}

	var stripePriceID sql.NullString
	var productID string
	// priceColumn is a compile-time constant, not user input.
	err := c.pg.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT %s, product_id FROM plans WHERE plan_id = $1 AND active
	`, priceColumn), planID).Scan(&stripePriceID, &productID)
	if err != nil {
		return "", fmt.Errorf("create subscription: read plan: %w", err)
	}
	if !stripePriceID.Valid || stripePriceID.String == "" {
		return "", fmt.Errorf("%w: plan %s cadence %s", ErrNoPriceConfigured, planID, cadence)
	}

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)
	var stripeCustomerID sql.NullString
	_ = c.pg.QueryRowContext(ctx, `
		SELECT stripe_customer_id FROM orgs WHERE org_id = $1
	`, orgIDStr).Scan(&stripeCustomerID)

	sessionParams := &stripe.CheckoutSessionCreateParams{
		Mode:       stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{
			{
				Price:    stripe.String(stripePriceID.String),
				Quantity: stripe.Int64(1),
			},
		},
	}

	if stripeCustomerID.Valid {
		sessionParams.Customer = stripe.String(stripeCustomerID.String)
	} else {
		sessionParams.CustomerCreation = stripe.String("always")
	}

	sessionParams.AddMetadata("org_id", orgIDStr)
	sessionParams.AddMetadata("product_id", productID)
	sessionParams.AddMetadata("plan_id", planID)

	session, err := c.stripe.V1CheckoutSessions.Create(ctx, sessionParams)
	if err != nil {
		return "", fmt.Errorf("create subscription: stripe: %w", err)
	}

	return session.URL, nil
}
