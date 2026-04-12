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
		Mode:       stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
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

func (c *Client) CancelSubscription(ctx context.Context, orgID OrgID, subscriptionID int64) (SubscriptionRecord, error) {
	if err := ctx.Err(); err != nil {
		return SubscriptionRecord{}, err
	}
	if subscriptionID <= 0 {
		return SubscriptionRecord{}, fmt.Errorf("%w: subscription_id must be positive", ErrSubscriptionNotFound)
	}

	local, err := c.loadLocalStripeSubscriptionByID(ctx, orgID, subscriptionID)
	if err != nil {
		return SubscriptionRecord{}, err
	}
	if local.Record.Status == "canceled" || local.Record.EntitlementState == EntitlementClosed || local.Record.EntitlementState == EntitlementVoided {
		return local.Record, nil
	}
	if local.StripeSubscriptionID == "" {
		return SubscriptionRecord{}, fmt.Errorf("%w: subscription %d has no stripe subscription id", ErrSubscriptionNotFound, subscriptionID)
	}

	subscription, err := c.stripe.V1Subscriptions.Cancel(ctx, local.StripeSubscriptionID, &stripe.SubscriptionCancelParams{
		InvoiceNow: stripe.Bool(false),
		Prorate:    stripe.Bool(false),
	})
	if err != nil {
		return SubscriptionRecord{}, fmt.Errorf("cancel stripe subscription: %w", err)
	}

	state := mergeStripeSubscriptionState(
		stripeSubscriptionStateFromSubscription(*subscription, "canceled"),
		local.State(),
	).withDefaults()
	providerEvent, err := state.providerEvent("customer.subscription.deleted", closedSubscriptionPaymentState(state), EntitlementClosed)
	if err != nil {
		return SubscriptionRecord{}, fmt.Errorf("cancel subscription provider event: %w", err)
	}
	if err := c.ApplySubscriptionProviderEvent(ctx, providerEvent); err != nil {
		return SubscriptionRecord{}, fmt.Errorf("apply cancellation provider event: %w", err)
	}

	refreshed, err := c.loadLocalStripeSubscriptionByID(ctx, orgID, subscriptionID)
	if err != nil {
		return SubscriptionRecord{}, err
	}
	return refreshed.Record, nil
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
	case "invoice.payment_failed":
		return c.handleInvoicePaymentFailed(ctx, event)
	case "customer.subscription.updated":
		return c.handleSubscriptionUpdated(ctx, event)
	case "customer.subscription.deleted":
		return c.handleSubscriptionDeleted(ctx, event)
	default:
		return nil
	}
}

type stripeSubscriptionState struct {
	OrgIDText               string
	ProductID               string
	PlanID                  string
	Cadence                 string
	Status                  string
	PaymentState            EntitlementPaymentState
	StripeSubscriptionID    string
	StripeCheckoutSessionID string
	StripeCustomerID        string
	CurrentPeriodStart      *time.Time
	CurrentPeriodEnd        *time.Time
}

type localStripeSubscription struct {
	Record                  SubscriptionRecord
	StripeSubscriptionID    string
	StripeCheckoutSessionID string
}

func (s localStripeSubscription) State() stripeSubscriptionState {
	return stripeSubscriptionState{
		OrgIDText:               s.Record.OrgID,
		ProductID:               s.Record.ProductID,
		PlanID:                  s.Record.PlanID,
		Cadence:                 s.Record.Cadence,
		Status:                  s.Record.Status,
		PaymentState:            s.Record.PaymentState,
		StripeSubscriptionID:    s.StripeSubscriptionID,
		StripeCheckoutSessionID: s.StripeCheckoutSessionID,
		CurrentPeriodStart:      s.Record.CurrentPeriodStart,
		CurrentPeriodEnd:        s.Record.CurrentPeriodEnd,
	}
}

func (s stripeSubscriptionState) hasRequiredForgeMetadata() bool {
	return s.OrgIDText != "" && s.ProductID != "" && s.PlanID != "" && s.StripeSubscriptionID != ""
}

func (s stripeSubscriptionState) hasCurrentPeriod() bool {
	return subscriptionPeriodValid(s.CurrentPeriodStart, s.CurrentPeriodEnd)
}

func (s stripeSubscriptionState) withDefaults() stripeSubscriptionState {
	if s.Cadence == "" {
		s.Cadence = string(CadenceMonthly)
	}
	if s.Status == "" {
		s.Status = "active"
	}
	return s
}

func (s stripeSubscriptionState) providerEvent(eventType string, paymentState EntitlementPaymentState, entitlementState EntitlementState) (SubscriptionProviderEvent, error) {
	var orgID OrgID
	if s.OrgIDText != "" {
		parsed, err := strconv.ParseUint(s.OrgIDText, 10, 64)
		if err != nil {
			return SubscriptionProviderEvent{}, fmt.Errorf("parse provider event org id: %w", err)
		}
		orgID = OrgID(parsed)
	}
	return SubscriptionProviderEvent{
		Provider:                  "stripe",
		EventType:                 eventType,
		OrgID:                     orgID,
		ProductID:                 s.ProductID,
		PlanID:                    s.PlanID,
		Cadence:                   s.Cadence,
		Status:                    s.Status,
		ProviderSubscriptionID:    s.StripeSubscriptionID,
		ProviderCheckoutSessionID: s.StripeCheckoutSessionID,
		ProviderCustomerID:        s.StripeCustomerID,
		CurrentPeriodStart:        s.CurrentPeriodStart,
		CurrentPeriodEnd:          s.CurrentPeriodEnd,
		PaymentState:              paymentState,
		EntitlementState:          entitlementState,
	}, nil
}

func (c *Client) ApplySubscriptionProviderEvent(ctx context.Context, event SubscriptionProviderEvent) error {
	if event.Provider != "stripe" {
		return fmt.Errorf("unsupported subscription provider %q", event.Provider)
	}
	if event.OrgID == 0 {
		return fmt.Errorf("subscription provider event org id is required")
	}
	if err := validateSubscriptionProviderEvent(event); err != nil {
		return err
	}
	state := stripeSubscriptionState{
		OrgIDText:               strconv.FormatUint(uint64(event.OrgID), 10),
		ProductID:               event.ProductID,
		PlanID:                  event.PlanID,
		Cadence:                 event.Cadence,
		Status:                  event.Status,
		StripeSubscriptionID:    event.ProviderSubscriptionID,
		StripeCheckoutSessionID: event.ProviderCheckoutSessionID,
		StripeCustomerID:        event.ProviderCustomerID,
		CurrentPeriodStart:      event.CurrentPeriodStart,
		CurrentPeriodEnd:        event.CurrentPeriodEnd,
	}.withDefaults()
	if !state.hasRequiredForgeMetadata() {
		return fmt.Errorf("subscription provider event is missing required forge metadata")
	}
	if err := c.upsertStripeSubscription(ctx, state); err != nil {
		return err
	}
	if event.EntitlementState == EntitlementClosed || event.EntitlementState == EntitlementVoided {
		if err := c.closeSubscriptionEntitlements(ctx, state, event.PaymentState, event.EntitlementState); err != nil {
			return err
		}
		return c.recordSubscriptionProviderEvent(ctx, event, state)
	}
	if event.EntitlementState != "" {
		if err := c.ensureSubscriptionEntitlements(ctx, state, event.PaymentState, event.EntitlementState); err != nil {
			return err
		}
	}
	return c.recordSubscriptionProviderEvent(ctx, event, state)
}

func (c *Client) recordSubscriptionProviderEvent(ctx context.Context, event SubscriptionProviderEvent, state stripeSubscriptionState) error {
	outboxEvent := subscriptionProviderEventOutbox(event, state, c.clock().UTC())
	if err := insertOutboxEvent(ctx, c.pg, outboxEvent); err != nil {
		return fmt.Errorf("record subscription provider event: %w", err)
	}
	return nil
}

func validateSubscriptionProviderEvent(event SubscriptionProviderEvent) error {
	switch BillingCadence(event.Cadence) {
	case "", CadenceMonthly, CadenceAnnual:
	default:
		return fmt.Errorf("unsupported subscription cadence %q", event.Cadence)
	}
	switch event.PaymentState {
	case "", PaymentNotRequired, PaymentPending, PaymentPaid, PaymentFailed, PaymentUncollectible, PaymentRefunded:
	default:
		return fmt.Errorf("unsupported subscription payment state %q", event.PaymentState)
	}
	switch event.EntitlementState {
	case "", EntitlementScheduled, EntitlementActive, EntitlementGrace, EntitlementClosed, EntitlementVoided:
	default:
		return fmt.Errorf("unsupported subscription entitlement state %q", event.EntitlementState)
	}
	if event.EntitlementState != "" && event.PaymentState == "" {
		return fmt.Errorf("subscription provider event payment_state is required when entitlement_state is set")
	}
	return nil
}

func (c *Client) handleCheckoutSessionCompleted(ctx context.Context, event stripe.Event) error {
	session, err := decodeStripeEventObject[stripe.CheckoutSession](event, "checkout.session.completed")
	if err != nil {
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

	if session.Mode == stripe.CheckoutSessionModeSubscription {
		state := stripeSubscriptionStateFromCheckoutSession(session).withDefaults()
		if !state.hasRequiredForgeMetadata() {
			return fmt.Errorf("checkout.session.completed: missing forge subscription metadata for stripe subscription %q", state.StripeSubscriptionID)
		}
		providerEvent, err := state.providerEvent("checkout.session.completed", PaymentPending, "")
		if err != nil {
			return fmt.Errorf("checkout.session.completed: provider event: %w", err)
		}
		if err := c.ApplySubscriptionProviderEvent(ctx, providerEvent); err != nil {
			return fmt.Errorf("checkout.session.completed: apply provider event: %w", err)
		}
	}

	return nil
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
	expiresAt := c.clock().UTC().AddDate(1, 0, 0)
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

	state, err := c.subscriptionStateFromInvoice(ctx, invoice, "active")
	if err != nil {
		return fmt.Errorf("invoice.paid: resolve subscription: %w", err)
	}
	if state.StripeSubscriptionID == "" {
		return nil
	}
	if !state.hasRequiredForgeMetadata() {
		return fmt.Errorf("invoice.paid: missing forge subscription metadata for stripe subscription %q", state.StripeSubscriptionID)
	}
	providerEvent, err := state.providerEvent("invoice.paid", PaymentPaid, EntitlementActive)
	if err != nil {
		return fmt.Errorf("invoice.paid: provider event: %w", err)
	}
	if err := c.ApplySubscriptionProviderEvent(ctx, providerEvent); err != nil {
		return fmt.Errorf("invoice.paid: apply provider event: %w", err)
	}
	return nil
}

func (c *Client) handleInvoicePaymentFailed(ctx context.Context, event stripe.Event) error {
	invoice, err := decodeStripeEventObject[stripe.Invoice](event, "invoice.payment_failed")
	if err != nil {
		return err
	}

	state, err := c.subscriptionStateFromInvoice(ctx, invoice, "past_due")
	if err != nil {
		return fmt.Errorf("invoice.payment_failed: resolve subscription: %w", err)
	}
	if state.StripeSubscriptionID == "" {
		return nil
	}
	if state.hasRequiredForgeMetadata() {
		providerEvent, err := state.providerEvent("invoice.payment_failed", PaymentFailed, EntitlementGrace)
		if err != nil {
			return fmt.Errorf("invoice.payment_failed: provider event: %w", err)
		}
		if err := c.ApplySubscriptionProviderEvent(ctx, providerEvent); err != nil {
			return fmt.Errorf("invoice.payment_failed: apply provider event: %w", err)
		}
		return nil
	}
	rows, err := c.updateStripeSubscriptionStatus(ctx, state)
	if err != nil {
		return fmt.Errorf("invoice.payment_failed: update subscription: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("invoice.payment_failed: missing local subscription for stripe subscription %q", state.StripeSubscriptionID)
	}
	return nil
}

func (c *Client) handleSubscriptionUpdated(ctx context.Context, event stripe.Event) error {
	subscription, err := decodeStripeEventObject[stripe.Subscription](event, "customer.subscription.updated")
	if err != nil {
		return err
	}

	state := stripeSubscriptionStateFromSubscription(subscription, string(subscription.Status)).withDefaults()
	if state.StripeSubscriptionID == "" {
		return nil
	}
	if state.hasRequiredForgeMetadata() {
		entitlementState := EntitlementState("")
		if state.CurrentPeriodStart != nil && state.CurrentPeriodEnd != nil && state.Status != "canceled" {
			entitlementState = EntitlementGrace
		}
		providerEvent, err := state.providerEvent("customer.subscription.updated", PaymentPending, entitlementState)
		if err != nil {
			return fmt.Errorf("customer.subscription.updated: provider event: %w", err)
		}
		if err := c.ApplySubscriptionProviderEvent(ctx, providerEvent); err != nil {
			return fmt.Errorf("customer.subscription.updated: apply provider event: %w", err)
		}
		return nil
	}
	rows, err := c.updateStripeSubscriptionStatus(ctx, state)
	if err != nil {
		return fmt.Errorf("customer.subscription.updated: update subscription: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("customer.subscription.updated: missing local subscription for stripe subscription %q", state.StripeSubscriptionID)
	}
	return nil
}

func (c *Client) handleSubscriptionDeleted(ctx context.Context, event stripe.Event) error {
	subscription, err := decodeStripeEventObject[stripe.Subscription](event, "customer.subscription.deleted")
	if err != nil {
		return err
	}

	state := stripeSubscriptionStateFromSubscription(subscription, "canceled").withDefaults()
	if state.StripeSubscriptionID == "" {
		return nil
	}
	if state.hasRequiredForgeMetadata() {
		if local, found, err := c.loadLocalStripeSubscriptionState(ctx, state.StripeSubscriptionID); err != nil {
			return fmt.Errorf("customer.subscription.deleted: load local subscription: %w", err)
		} else if found {
			state = mergeStripeSubscriptionState(state, local).withDefaults()
		}
		providerEvent, err := state.providerEvent("customer.subscription.deleted", closedSubscriptionPaymentState(state), EntitlementClosed)
		if err != nil {
			return fmt.Errorf("customer.subscription.deleted: provider event: %w", err)
		}
		if err := c.ApplySubscriptionProviderEvent(ctx, providerEvent); err != nil {
			return fmt.Errorf("customer.subscription.deleted: apply provider event: %w", err)
		}
		return nil
	}
	rows, err := c.updateStripeSubscriptionStatus(ctx, state)
	if err != nil {
		return fmt.Errorf("customer.subscription.deleted: update subscription: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("customer.subscription.deleted: missing local subscription for stripe subscription %q", state.StripeSubscriptionID)
	}
	return nil
}

func (c *Client) upsertStripeSubscription(ctx context.Context, state stripeSubscriptionState) error {
	state = state.withDefaults()
	if !state.hasRequiredForgeMetadata() {
		return fmt.Errorf("stripe subscription state is missing required forge metadata")
	}

	tx, err := c.pg.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO subscription_contracts (
			contract_id,
			org_id,
			product_id,
			plan_id,
			cadence,
			status,
			payment_state,
			entitlement_state,
			billing_anchor,
			grace_until,
			current_period_start,
			current_period_end,
			stripe_subscription_id,
			stripe_checkout_session_id
		)
		VALUES ($1, $2, $3, $4, $5, $6,
		        'pending', 'grace', $7, $8,
		        $9, $10, $11, $12)
		ON CONFLICT (stripe_subscription_id) WHERE stripe_subscription_id <> '' DO UPDATE
		SET contract_id = COALESCE(NULLIF(EXCLUDED.contract_id, ''), subscription_contracts.contract_id),
		    org_id = EXCLUDED.org_id,
		    product_id = EXCLUDED.product_id,
		    plan_id = EXCLUDED.plan_id,
		    cadence = EXCLUDED.cadence,
		    status = EXCLUDED.status,
		    current_period_start = COALESCE(EXCLUDED.current_period_start, subscription_contracts.current_period_start),
		    current_period_end = COALESCE(EXCLUDED.current_period_end, subscription_contracts.current_period_end),
		    stripe_checkout_session_id = COALESCE(NULLIF(EXCLUDED.stripe_checkout_session_id, ''), subscription_contracts.stripe_checkout_session_id),
		    updated_at = now()
	`, stripeContractID(state.StripeSubscriptionID), state.OrgIDText, state.ProductID, state.PlanID, state.Cadence, state.Status,
		sqlTime(state.CurrentPeriodStart), sqlTime(graceUntil(state.CurrentPeriodEnd, c.cfg.SubscriptionGracePeriod)),
		sqlTime(state.CurrentPeriodStart), sqlTime(state.CurrentPeriodEnd), state.StripeSubscriptionID, state.StripeCheckoutSessionID)
	if err != nil {
		return err
	}

	if state.StripeCustomerID != "" {
		_, err = tx.ExecContext(ctx, `
			UPDATE orgs
			SET stripe_customer_id = $1,
			    updated_at = now()
			WHERE org_id = $2
		`, state.StripeCustomerID, state.OrgIDText)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (c *Client) updateStripeSubscriptionStatus(ctx context.Context, state stripeSubscriptionState) (int64, error) {
	if state.StripeSubscriptionID == "" {
		return 0, nil
	}
	result, err := c.pg.ExecContext(ctx, `
		UPDATE subscription_contracts
		SET status = COALESCE(NULLIF($1, ''), status),
		    current_period_start = COALESCE($2, current_period_start),
		    current_period_end = COALESCE($3, current_period_end),
		    updated_at = now()
		WHERE stripe_subscription_id = $4
	`, state.Status, sqlTime(state.CurrentPeriodStart), sqlTime(state.CurrentPeriodEnd), state.StripeSubscriptionID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (c *Client) subscriptionStateFromInvoice(ctx context.Context, invoice stripe.Invoice, status string) (stripeSubscriptionState, error) {
	metadata := invoiceSubscriptionMetadata(invoice)
	periodStart, periodEnd := invoiceSubscriptionPeriod(invoice)
	state := stripeSubscriptionState{
		OrgIDText:            metadata["org_id"],
		ProductID:            metadata["product_id"],
		PlanID:               metadata["plan_id"],
		Cadence:              metadata["cadence"],
		Status:               status,
		StripeSubscriptionID: invoiceSubscriptionID(invoice),
		StripeCustomerID:     stripeCustomerID(invoice.Customer),
		CurrentPeriodStart:   periodStart,
		CurrentPeriodEnd:     periodEnd,
	}.withDefaults()
	if state.StripeSubscriptionID == "" || (state.hasRequiredForgeMetadata() && state.hasCurrentPeriod()) || c == nil {
		return state, nil
	}

	local, found, err := c.loadLocalStripeSubscriptionState(ctx, state.StripeSubscriptionID)
	if err != nil {
		return stripeSubscriptionState{}, err
	}
	if found {
		state = mergeStripeSubscriptionState(state, local).withDefaults()
		if state.hasRequiredForgeMetadata() && state.hasCurrentPeriod() {
			return state, nil
		}
	}

	remote, err := c.retrieveStripeSubscriptionState(ctx, state.StripeSubscriptionID, status)
	if err != nil {
		return state, err
	}
	return mergeStripeSubscriptionState(state, remote).withDefaults(), nil
}

func stripeSubscriptionStateFromCheckoutSession(session stripe.CheckoutSession) stripeSubscriptionState {
	return stripeSubscriptionState{
		OrgIDText:               session.Metadata["org_id"],
		ProductID:               session.Metadata["product_id"],
		PlanID:                  session.Metadata["plan_id"],
		Cadence:                 session.Metadata["cadence"],
		Status:                  "active",
		StripeSubscriptionID:    stripeSubscriptionID(session.Subscription),
		StripeCheckoutSessionID: session.ID,
		StripeCustomerID:        stripeCustomerID(session.Customer),
	}
}

func stripeSubscriptionStateFromSubscription(subscription stripe.Subscription, status string) stripeSubscriptionState {
	if status == "" {
		status = string(subscription.Status)
	}
	periodStart, periodEnd := stripeSubscriptionCurrentPeriod(&subscription)
	return stripeSubscriptionState{
		OrgIDText:            subscription.Metadata["org_id"],
		ProductID:            subscription.Metadata["product_id"],
		PlanID:               subscription.Metadata["plan_id"],
		Cadence:              subscription.Metadata["cadence"],
		Status:               status,
		StripeSubscriptionID: subscription.ID,
		StripeCustomerID:     stripeCustomerID(subscription.Customer),
		CurrentPeriodStart:   periodStart,
		CurrentPeriodEnd:     periodEnd,
	}
}

func (c *Client) loadLocalStripeSubscriptionState(ctx context.Context, stripeSubscriptionID string) (stripeSubscriptionState, bool, error) {
	var state stripeSubscriptionState
	var start, end sql.NullTime
	var paymentState string
	err := c.pg.QueryRowContext(ctx, `
		SELECT org_id, product_id, plan_id, cadence, status, payment_state,
		       current_period_start, current_period_end, stripe_subscription_id, stripe_checkout_session_id
		FROM subscription_contracts
		WHERE stripe_subscription_id = $1
	`, stripeSubscriptionID).Scan(
		&state.OrgIDText,
		&state.ProductID,
		&state.PlanID,
		&state.Cadence,
		&state.Status,
		&paymentState,
		&start,
		&end,
		&state.StripeSubscriptionID,
		&state.StripeCheckoutSessionID,
	)
	if err == sql.ErrNoRows {
		return stripeSubscriptionState{}, false, nil
	}
	if err != nil {
		return stripeSubscriptionState{}, false, err
	}
	state.PaymentState = EntitlementPaymentState(paymentState)
	if start.Valid {
		value := start.Time.UTC()
		state.CurrentPeriodStart = &value
	}
	if end.Valid {
		value := end.Time.UTC()
		state.CurrentPeriodEnd = &value
	}
	return state, true, nil
}

func (c *Client) loadLocalStripeSubscriptionByID(ctx context.Context, orgID OrgID, subscriptionID int64) (localStripeSubscription, error) {
	var out localStripeSubscription
	var start, end sql.NullTime
	var paymentState string
	var entitlementState string
	err := c.pg.QueryRowContext(ctx, `
		SELECT subscription_id, contract_id, org_id, product_id, plan_id, cadence, status,
		       payment_state, entitlement_state, current_period_start, current_period_end,
		       stripe_subscription_id, stripe_checkout_session_id
		FROM subscription_contracts
		WHERE subscription_id = $1
		  AND org_id = $2
	`, subscriptionID, strconv.FormatUint(uint64(orgID), 10)).Scan(
		&out.Record.SubscriptionID,
		&out.Record.ContractID,
		&out.Record.OrgID,
		&out.Record.ProductID,
		&out.Record.PlanID,
		&out.Record.Cadence,
		&out.Record.Status,
		&paymentState,
		&entitlementState,
		&start,
		&end,
		&out.StripeSubscriptionID,
		&out.StripeCheckoutSessionID,
	)
	if err == sql.ErrNoRows {
		return localStripeSubscription{}, fmt.Errorf("%w: %d", ErrSubscriptionNotFound, subscriptionID)
	}
	if err != nil {
		return localStripeSubscription{}, fmt.Errorf("load subscription %d: %w", subscriptionID, err)
	}
	out.Record.PaymentState = EntitlementPaymentState(paymentState)
	out.Record.EntitlementState = EntitlementState(entitlementState)
	if start.Valid {
		value := start.Time.UTC()
		out.Record.CurrentPeriodStart = &value
	}
	if end.Valid {
		value := end.Time.UTC()
		out.Record.CurrentPeriodEnd = &value
	}
	return out, nil
}

func (c *Client) retrieveStripeSubscriptionState(ctx context.Context, stripeSubscriptionID, status string) (stripeSubscriptionState, error) {
	subscription, err := c.stripe.V1Subscriptions.Retrieve(ctx, stripeSubscriptionID, nil)
	if err != nil {
		return stripeSubscriptionState{}, err
	}
	return stripeSubscriptionStateFromSubscription(*subscription, status), nil
}

func mergeStripeSubscriptionState(primary, fallback stripeSubscriptionState) stripeSubscriptionState {
	out := primary
	if out.OrgIDText == "" {
		out.OrgIDText = fallback.OrgIDText
	}
	if out.ProductID == "" {
		out.ProductID = fallback.ProductID
	}
	if out.PlanID == "" {
		out.PlanID = fallback.PlanID
	}
	if out.Cadence == "" {
		out.Cadence = fallback.Cadence
	}
	if out.Status == "" {
		out.Status = fallback.Status
	}
	if out.PaymentState == "" {
		out.PaymentState = fallback.PaymentState
	}
	if out.StripeSubscriptionID == "" {
		out.StripeSubscriptionID = fallback.StripeSubscriptionID
	}
	if out.StripeCheckoutSessionID == "" {
		out.StripeCheckoutSessionID = fallback.StripeCheckoutSessionID
	}
	if out.StripeCustomerID == "" {
		out.StripeCustomerID = fallback.StripeCustomerID
	}
	if !subscriptionPeriodValid(out.CurrentPeriodStart, out.CurrentPeriodEnd) && subscriptionPeriodValid(fallback.CurrentPeriodStart, fallback.CurrentPeriodEnd) {
		out.CurrentPeriodStart = fallback.CurrentPeriodStart
		out.CurrentPeriodEnd = fallback.CurrentPeriodEnd
	} else if out.CurrentPeriodStart == nil {
		out.CurrentPeriodStart = fallback.CurrentPeriodStart
	} else if out.CurrentPeriodEnd == nil {
		out.CurrentPeriodEnd = fallback.CurrentPeriodEnd
	}
	return out
}

func closedSubscriptionPaymentState(state stripeSubscriptionState) EntitlementPaymentState {
	if state.PaymentState != "" {
		return state.PaymentState
	}
	return PaymentPending
}

func (c *Client) ensureSubscriptionEntitlements(ctx context.Context, state stripeSubscriptionState, paymentState EntitlementPaymentState, entitlementState EntitlementState) error {
	if !state.hasCurrentPeriod() {
		return nil
	}
	if err := c.updateStripeSubscriptionEntitlementState(ctx, state, paymentState, entitlementState); err != nil {
		return err
	}
	orgID, err := strconv.ParseUint(state.OrgIDText, 10, 64)
	if err != nil {
		return fmt.Errorf("parse org id: %w", err)
	}
	policies, err := c.subscriptionEntitlementPolicies(ctx, state.PlanID, *state.CurrentPeriodStart, *state.CurrentPeriodEnd)
	if err != nil {
		return err
	}
	contractID := stripeContractID(state.StripeSubscriptionID)
	for _, policy := range policies {
		period, ok := subscriptionEntitlementPeriod(OrgID(orgID), contractID, policy, *state.CurrentPeriodStart, *state.CurrentPeriodEnd, paymentState, entitlementState)
		if !ok {
			continue
		}
		if err := c.ensureEntitlementPeriod(ctx, period); err != nil {
			return err
		}
		periodStart := period.PeriodStart
		periodEnd := period.PeriodEnd
		if _, err := c.IssueCreditGrant(ctx, CreditGrant{
			OrgID:               period.OrgID,
			ScopeType:           period.ScopeType,
			ScopeProductID:      period.ScopeProductID,
			ScopeBucketID:       period.ScopeBucketID,
			ScopeSKUID:          period.ScopeSKUID,
			Amount:              period.AmountUnits,
			Source:              period.Source.String(),
			SourceReferenceID:   period.SourceReferenceID,
			EntitlementPeriodID: period.PeriodID,
			PolicyVersion:       period.PolicyVersion,
			StartsAt:            &periodStart,
			PeriodStart:         &periodStart,
			PeriodEnd:           &periodEnd,
			ExpiresAt:           &periodEnd,
		}); err != nil {
			return fmt.Errorf("issue subscription grant for policy %s: %w", policy.PolicyID, err)
		}
	}
	return nil
}

func (c *Client) updateStripeSubscriptionEntitlementState(ctx context.Context, state stripeSubscriptionState, paymentState EntitlementPaymentState, entitlementState EntitlementState) error {
	if state.StripeSubscriptionID == "" {
		return nil
	}
	_, err := c.pg.ExecContext(ctx, `
		UPDATE subscription_contracts
		SET payment_state = $2,
		    entitlement_state = $3,
		    current_period_start = COALESCE($4, current_period_start),
		    current_period_end = COALESCE($5, current_period_end),
		    grace_until = COALESCE($6, grace_until),
		    updated_at = now()
		WHERE stripe_subscription_id = $1
	`, state.StripeSubscriptionID, string(paymentState), string(entitlementState), sqlTime(state.CurrentPeriodStart), sqlTime(state.CurrentPeriodEnd), sqlTime(graceUntil(state.CurrentPeriodEnd, c.cfg.SubscriptionGracePeriod)))
	if err != nil {
		return fmt.Errorf("update subscription entitlement state: %w", err)
	}
	return nil
}

func (c *Client) closeSubscriptionEntitlements(ctx context.Context, state stripeSubscriptionState, paymentState EntitlementPaymentState, entitlementState EntitlementState) error {
	if state.StripeSubscriptionID == "" {
		return nil
	}
	contractID := stripeContractID(state.StripeSubscriptionID)
	if contractID == "" {
		return nil
	}
	tx, err := c.pg.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	closedAt := c.clock().UTC()
	_, err = tx.ExecContext(ctx, `
		UPDATE subscription_contracts
		SET status = COALESCE(NULLIF($2, ''), status),
		    payment_state = $3,
		    entitlement_state = $4,
		    current_period_start = COALESCE($5, current_period_start),
		    current_period_end = COALESCE($6, current_period_end),
		    grace_until = COALESCE($7, grace_until),
		    updated_at = now()
		WHERE stripe_subscription_id = $1
	`, state.StripeSubscriptionID, state.Status, string(paymentState), string(entitlementState), sqlTime(state.CurrentPeriodStart), sqlTime(state.CurrentPeriodEnd), sqlTime(graceUntil(state.CurrentPeriodEnd, c.cfg.SubscriptionGracePeriod)))
	if err != nil {
		return fmt.Errorf("close subscription contract: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE entitlement_periods
		SET payment_state = $2,
		    entitlement_state = $3,
		    updated_at = now()
		WHERE contract_id = $1
		  AND entitlement_state NOT IN ('closed', 'voided')
	`, contractID, string(paymentState), string(entitlementState))
	if err != nil {
		return fmt.Errorf("close entitlement periods: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE credit_grants
		SET closed_at = $2
		WHERE entitlement_period_id IN (
			SELECT period_id
			FROM entitlement_periods
			WHERE contract_id = $1
		)
		  AND closed_at IS NULL
	`, contractID, closedAt)
	if err != nil {
		return fmt.Errorf("close subscription grants: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit subscription close: %w", err)
	}
	return nil
}

func (c *Client) subscriptionEntitlementPolicies(ctx context.Context, planID string, periodStart time.Time, periodEnd time.Time) ([]EntitlementPolicy, error) {
	rows, err := c.pg.QueryContext(ctx, `
		SELECT p.policy_id, p.source, p.product_id, p.scope_type, p.scope_product_id, p.scope_bucket_id, p.scope_sku_id,
		       p.amount_units, p.cadence, p.anchor_kind, p.proration_mode, p.policy_version, p.active_from, p.active_until
		FROM plan_entitlements pe
		JOIN entitlement_policies p ON p.policy_id = pe.policy_id
		WHERE pe.plan_id = $1
		  AND p.source = 'subscription'
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func sqlTime(value *time.Time) sql.NullTime {
	if value == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: value.UTC(), Valid: true}
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

func invoiceSubscriptionMetadata(invoice stripe.Invoice) map[string]string {
	if invoice.Parent != nil && invoice.Parent.SubscriptionDetails != nil {
		if len(invoice.Parent.SubscriptionDetails.Metadata) != 0 {
			return invoice.Parent.SubscriptionDetails.Metadata
		}
		if invoice.Parent.SubscriptionDetails.Subscription != nil && len(invoice.Parent.SubscriptionDetails.Subscription.Metadata) != 0 {
			return invoice.Parent.SubscriptionDetails.Subscription.Metadata
		}
	}
	return invoice.Metadata
}

func invoiceSubscriptionID(invoice stripe.Invoice) string {
	if invoice.Parent == nil || invoice.Parent.SubscriptionDetails == nil {
		return ""
	}
	return stripeSubscriptionID(invoice.Parent.SubscriptionDetails.Subscription)
}

func invoiceSubscriptionPeriod(invoice stripe.Invoice) (*time.Time, *time.Time) {
	if invoice.Lines != nil {
		var selectedStart *time.Time
		var selectedEnd *time.Time
		for _, line := range invoice.Lines.Data {
			if line == nil || line.Period == nil {
				continue
			}
			start := unixTimePtr(line.Period.Start)
			end := unixTimePtr(line.Period.End)
			if !subscriptionPeriodValid(start, end) {
				continue
			}
			if selectedEnd == nil || end.After(*selectedEnd) {
				selectedStart = start
				selectedEnd = end
			}
		}
		if selectedEnd != nil {
			return selectedStart, selectedEnd
		}
	}

	// Invoice-level periods are usage collection windows; line item periods are the service period.
	start := unixTimePtr(invoice.PeriodStart)
	end := unixTimePtr(invoice.PeriodEnd)
	if subscriptionPeriodValid(start, end) {
		return start, end
	}
	return nil, nil
}

func stripeSubscriptionCurrentPeriod(subscription *stripe.Subscription) (*time.Time, *time.Time) {
	if subscription == nil || subscription.Items == nil {
		return nil, nil
	}
	for _, item := range subscription.Items.Data {
		if item == nil {
			continue
		}
		if item.CurrentPeriodStart == 0 && item.CurrentPeriodEnd == 0 {
			continue
		}
		return unixTimePtr(item.CurrentPeriodStart), unixTimePtr(item.CurrentPeriodEnd)
	}
	return nil, nil
}

func subscriptionPeriodValid(start *time.Time, end *time.Time) bool {
	return start != nil && end != nil && end.After(*start)
}

func stripeCustomerID(customer *stripe.Customer) string {
	if customer == nil {
		return ""
	}
	return customer.ID
}

func stripeSubscriptionID(subscription *stripe.Subscription) string {
	if subscription == nil {
		return ""
	}
	return subscription.ID
}

func unixTimePtr(ts int64) *time.Time {
	if ts == 0 {
		return nil
	}
	value := time.Unix(ts, 0).UTC()
	return &value
}
