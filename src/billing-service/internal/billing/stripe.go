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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stripe/stripe-go/v85"
	"github.com/stripe/stripe-go/v85/webhook"

	"github.com/verself/billing-service/internal/store"
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
	productName, err := c.queries.GetProductDisplayName(ctx, store.GetProductDisplayNameParams{ProductID: productID})
	if err != nil {
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
	billingEmail, err := c.queries.GetOrgBillingEmail(ctx, store.GetOrgBillingEmailParams{OrgID: orgIDText(orgID)})
	if errors.Is(err, pgx.ErrNoRows) {
		billingEmail = ""
	} else if err != nil {
		return "", fmt.Errorf("load org billing email: %w", err)
	}
	params := &stripe.CustomerCreateParams{Metadata: map[string]string{"org_id": orgIDText(orgID)}}
	if billingEmail != "" {
		params.Email = stripe.String(billingEmail)
	}
	params.SetIdempotencyKey(textID("stripe_customer_request", orgIDText(orgID), billingEmail))
	customer, err := c.stripe.V1Customers.Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("create stripe customer: %w", err)
	}
	if err := c.queries.UpsertStripeCustomerBinding(ctx, store.UpsertStripeCustomerBindingParams{
		BindingID:  textID("provider_binding", "stripe", "customer", customer.ID),
		OrgID:      orgIDText(orgID),
		CustomerID: customer.ID,
	}); err != nil {
		return "", fmt.Errorf("persist stripe customer binding: %w", err)
	}
	return customer.ID, nil
}

func (c *Client) lookupStripeCustomer(ctx context.Context, orgID OrgID) (string, error) {
	customerID, err := c.queries.LookupStripeCustomer(ctx, store.LookupStripeCustomerParams{OrgID: orgIDText(orgID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoStripeCustomer
	}
	if err != nil {
		return "", fmt.Errorf("lookup stripe customer: %w", err)
	}
	return customerID, nil
}

func (c *Client) defaultStripePaymentMethod(ctx context.Context, orgID OrgID) (string, error) {
	paymentMethodID, err := c.queries.GetDefaultStripePaymentMethod(ctx, store.GetDefaultStripePaymentMethodParams{OrgID: orgIDText(orgID)})
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
	createParams := &stripe.InvoiceCreateParams{AutoAdvance: stripe.Bool(false), CollectionMethod: stripe.String(string(stripe.InvoiceCollectionMethodChargeAutomatically)), Currency: stripe.String("usd"), Customer: stripe.String(customerID), DefaultPaymentMethod: stripe.String(paymentMethodID), Description: stripe.String("Verself plan upgrade"), Metadata: metadata, PendingInvoiceItemsBehavior: stripe.String("exclude")}
	createParams.SetIdempotencyKey("verself:upgrade:" + stripeID + ":invoice")
	invoice, err := c.stripe.V1Invoices.Create(ctx, createParams)
	if err != nil {
		return "", "", false, fmt.Errorf("create stripe upgrade invoice: %w", err)
	}
	itemParams := &stripe.InvoiceItemCreateParams{Amount: stripe.Int64(checkedInt64FromUint64(quote.PriceDeltaCents, "stripe price delta cents")), Currency: stripe.String("usd"), Customer: stripe.String(customerID), Description: stripe.String("Prorated upgrade from " + quote.FromPlanID + " to " + quote.TargetPlanID), Invoice: stripe.String(invoice.ID), Metadata: metadata, Period: &stripe.InvoiceItemCreatePeriodParams{Start: stripe.Int64(quote.EffectiveAt.Unix()), End: stripe.Int64(quote.CycleEnd.Unix())}}
	itemParams.SetIdempotencyKey("verself:upgrade:" + stripeID + ":item")
	if _, err := c.stripe.V1InvoiceItems.Create(ctx, itemParams); err != nil {
		return "", invoice.ID, false, fmt.Errorf("create stripe upgrade invoice item: %w", err)
	}
	finalizeParams := &stripe.InvoiceFinalizeInvoiceParams{AutoAdvance: stripe.Bool(false)}
	finalizeParams.SetIdempotencyKey("verself:upgrade:" + stripeID + ":finalize")
	finalized, err := c.stripe.V1Invoices.FinalizeInvoice(ctx, invoice.ID, finalizeParams)
	if err != nil {
		return "", invoice.ID, false, fmt.Errorf("finalize stripe upgrade invoice: %w", err)
	}
	payParams := &stripe.InvoicePayParams{PaymentMethod: stripe.String(paymentMethodID), OffSession: stripe.Bool(false)}
	payParams.SetIdempotencyKey("verself:upgrade:" + stripeID + ":pay")
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
	if err := c.queries.UpdateUpgradeInvoiceProviderDocument(ctx, store.UpdateUpgradeInvoiceProviderDocumentParams{
		DocumentID:        documentID("contract_change", quote.ChangeID),
		ProviderInvoiceID: providerInvoiceID,
		HostedUrl:         pgTextValue(hostedURL),
		PdfUrl:            pgTextValue(pdfURL),
		Status:            status,
		PaymentStatus:     paymentStatus,
	}); err != nil {
		return fmt.Errorf("update upgrade document provider state: %w", err)
	}
	return c.queries.UpdateUpgradeInvoiceProviderChange(ctx, store.UpdateUpgradeInvoiceProviderChangeParams{
		ChangeID:          quote.ChangeID,
		ProviderInvoiceID: providerInvoiceID,
		PaymentStatus:     paymentStatus,
	})
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
		if err := q.InsertStripeProviderEvent(ctx, store.InsertStripeProviderEventParams{
			EventID:                 eventID,
			ProviderEventID:         event.ID,
			EventType:               string(event.Type),
			ProviderObjectType:      pgTextValue(stripeProviderObjectType(string(event.Type))),
			ProviderObjectID:        pgTextValue(objectID),
			ProviderCustomerID:      metadata["customer_id"],
			ProviderInvoiceID:       metadata["provider_invoice_id"],
			ProviderPaymentIntentID: metadata["provider_payment_intent_id"],
			ContractID:              metadata["contract_id"],
			ChangeID:                metadata["change_id"],
			FinalizationID:          metadata["finalization_id"],
			DocumentID:              metadata["document_id"],
			OrgID:                   metadata["org_id"],
			ProductID:               metadata["product_id"],
			ProviderCreatedAt:       timestamptz(occurred),
			Livemode:                event.Livemode,
			Payload:                 payload,
		}); err != nil {
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
	ids, err := c.queries.ListPendingStripeProviderEventIDs(ctx, store.ListPendingStripeProviderEventIDsParams{LimitCount: checkedInt32FromInt(limit, "pending stripe provider events limit")})
	if err != nil {
		return 0, fmt.Errorf("query pending provider events: %w", err)
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
	return applied, nil
}

func (c *Client) ApplyProviderEvent(ctx context.Context, eventID string) (bool, error) {
	claimed, err := c.queries.ClaimProviderEvent(ctx, store.ClaimProviderEventParams{EventID: eventID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim provider event: %w", err)
	}
	var event stripe.Event
	if err := json.Unmarshal(claimed.Payload, &event); err != nil {
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
		if err := q.ClearDefaultStripePaymentMethods(ctx, store.ClearDefaultStripePaymentMethodsParams{OrgID: orgIDText(orgID)}); err != nil {
			return fmt.Errorf("clear default payment methods: %w", err)
		}
		if err := q.UpsertStripePaymentMethod(ctx, store.UpsertStripePaymentMethodParams{
			PaymentMethodID:         textID("payment_method", "stripe", paymentMethodID),
			OrgID:                   orgIDText(orgID),
			ProviderCustomerID:      customerID,
			ProviderPaymentMethodID: paymentMethodID,
			SetupIntentID:           pgTextValue(setup.ID),
			CardBrand:               brand,
			CardLast4:               last4,
			ExpiresMonth:            nullableInt4(month),
			ExpiresYear:             nullableInt4(year),
			OffSessionAuthorizedAt:  timestamptz(appliedAt),
		}); err != nil {
			return fmt.Errorf("upsert payment method: %w", err)
		}
		if err := q.InsertStripeCustomerBindingIfMissing(ctx, store.InsertStripeCustomerBindingIfMissingParams{
			BindingID:  textID("provider_binding", "stripe", "customer", customerID),
			OrgID:      orgIDText(orgID),
			CustomerID: customerID,
		}); err != nil {
			return fmt.Errorf("persist stripe customer binding: %w", err)
		}
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
	if err := c.queries.MarkInvoicePaymentFailedDocument(ctx, store.MarkInvoicePaymentFailedDocumentParams{
		ChangeID:          pgTextValue(changeIDValue),
		ProviderInvoiceID: pgTextValue(invoice.ID),
		HostedUrl:         pgTextValue(invoice.HostedInvoiceURL),
	}); err != nil {
		return err
	}
	return c.queries.MarkInvoicePaymentFailedFinalization(ctx, store.MarkInvoicePaymentFailedFinalizationParams{FinalizationID: finalizationID("contract_change", changeIDValue)})
}

func (c *Client) loadContractChangeQuote(ctx context.Context, changeIDValue string) (contractChangeQuote, error) {
	payload, err := c.queries.GetContractChangePayload(ctx, store.GetContractChangePayloadParams{ChangeID: changeIDValue})
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
	row, err := c.queries.MarkProviderEventFinal(ctx, store.MarkProviderEventFinalParams{EventID: eventID, State: state})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("mark provider event final: %w", err)
	}
	orgID, _ := parseOrgID(row.OrgID)
	if orgID == 0 {
		return nil
	}
	return c.WithTx(ctx, "billing.provider_event.final_event", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		return appendEvent(ctx, tx, q, eventFact{EventType: "provider_event_" + state, AggregateType: "provider_event", AggregateID: eventID, OrgID: orgID, ProductID: row.ProductID, OccurredAt: time.Now().UTC(), Payload: map[string]any{"provider_event_id": eventID, "provider_stripe_event_id": row.ProviderEventID, "provider_event_type": row.EventType, "contract_id": row.ContractID, "change_id": row.ChangeID, "finalization_id": row.FinalizationID, "document_id": row.DocumentID}})
	})
}

func (c *Client) failProviderEvent(ctx context.Context, eventID string, cause error) error {
	return c.queries.FailProviderEvent(ctx, store.FailProviderEventParams{EventID: eventID, LastError: cause.Error()})
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

func nullableInt4(value int) pgtype.Int4 {
	if value == 0 {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: checkedInt32FromInt(value, "nullable int4"), Valid: true}
}
