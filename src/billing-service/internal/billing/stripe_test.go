package billing

import (
	"context"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v85"
)

func TestStripeCheckoutSessionStateUsesTypedSDKObject(t *testing.T) {
	t.Parallel()

	event := stripe.Event{Data: &stripe.EventData{Raw: []byte(`{
		"id": "cs_test",
		"mode": "subscription",
		"customer": "cus_test",
		"subscription": "sub_test",
		"metadata": {
			"org_id": "42",
			"product_id": "sandbox",
			"plan_id": "sandbox-pro",
			"cadence": "monthly"
		}
	}`)}}

	session, err := decodeStripeEventObject[stripe.CheckoutSession](event, "checkout.session.completed")
	if err != nil {
		t.Fatalf("decode checkout session: %v", err)
	}
	state := stripeSubscriptionStateFromCheckoutSession(session).withDefaults()

	assertEqual(t, state.OrgIDText, "42", "org id")
	assertEqual(t, state.ProductID, "sandbox", "product id")
	assertEqual(t, state.PlanID, "sandbox-pro", "plan id")
	assertEqual(t, state.Cadence, "monthly", "cadence")
	assertEqual(t, state.Status, "active", "status")
	assertEqual(t, state.StripeSubscriptionID, "sub_test", "stripe subscription id")
	assertEqual(t, state.StripeCheckoutSessionID, "cs_test", "stripe checkout session id")
	assertEqual(t, state.StripeCustomerID, "cus_test", "stripe customer id")
}

func TestStripeInvoiceStateUsesParentSubscriptionSnapshot(t *testing.T) {
	t.Parallel()

	event := stripe.Event{Data: &stripe.EventData{Raw: []byte(`{
		"id": "in_test",
		"customer": "cus_test",
		"period_start": 1711929600,
		"period_end": 1714521600,
		"parent": {
			"type": "subscription_details",
			"subscription_details": {
				"subscription": "sub_test",
				"metadata": {
					"org_id": "42",
					"product_id": "sandbox",
					"plan_id": "sandbox-pro",
					"cadence": "monthly"
				}
			}
		}
	}`)}}

	invoice, err := decodeStripeEventObject[stripe.Invoice](event, "invoice.paid")
	if err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	state, err := (*Client)(nil).subscriptionStateFromInvoice(context.Background(), invoice, "active")
	if err != nil {
		t.Fatalf("subscriptionStateFromInvoice: %v", err)
	}

	assertEqual(t, state.OrgIDText, "42", "org id")
	assertEqual(t, state.ProductID, "sandbox", "product id")
	assertEqual(t, state.PlanID, "sandbox-pro", "plan id")
	assertEqual(t, state.Cadence, "monthly", "cadence")
	assertEqual(t, state.Status, "active", "status")
	assertEqual(t, state.StripeSubscriptionID, "sub_test", "stripe subscription id")
	assertEqual(t, state.StripeCustomerID, "cus_test", "stripe customer id")
	assertTimePresent(t, state.CurrentPeriodStart, "current period start")
	assertTimePresent(t, state.CurrentPeriodEnd, "current period end")
}

func TestStripeInvoiceStatePrefersLineItemServicePeriod(t *testing.T) {
	t.Parallel()

	event := stripe.Event{Data: &stripe.EventData{Raw: []byte(`{
		"id": "in_test",
		"customer": "cus_test",
		"period_start": 1776028570,
		"period_end": 1776028570,
		"parent": {
			"type": "subscription_details",
			"subscription_details": {
				"subscription": "sub_test",
				"metadata": {
					"org_id": "42",
					"product_id": "sandbox",
					"plan_id": "sandbox-hobby",
					"cadence": "monthly"
				}
			}
		},
		"lines": {
			"object": "list",
			"data": [{
				"id": "il_test",
				"period": {
					"start": 1776028570,
					"end": 1778620570
				}
			}]
		}
	}`)}}

	invoice, err := decodeStripeEventObject[stripe.Invoice](event, "invoice.paid")
	if err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	state, err := (*Client)(nil).subscriptionStateFromInvoice(context.Background(), invoice, "active")
	if err != nil {
		t.Fatalf("subscriptionStateFromInvoice: %v", err)
	}

	assertEqual(t, state.PlanID, "sandbox-hobby", "plan id")
	assertTimeEqual(t, state.CurrentPeriodStart, time.Unix(1776028570, 0).UTC(), "current period start")
	assertTimeEqual(t, state.CurrentPeriodEnd, time.Unix(1778620570, 0).UTC(), "current period end")
}

func TestStripeSubscriptionStateUsesItemPeriod(t *testing.T) {
	t.Parallel()

	event := stripe.Event{Data: &stripe.EventData{Raw: []byte(`{
		"id": "sub_test",
		"customer": "cus_test",
		"status": "past_due",
		"metadata": {
			"org_id": "42",
			"product_id": "sandbox",
			"plan_id": "sandbox-pro",
			"cadence": "monthly"
		},
		"items": {
			"object": "list",
			"data": [{
				"id": "si_test",
				"current_period_start": 1711929600,
				"current_period_end": 1714521600
			}]
		}
	}`)}}

	subscription, err := decodeStripeEventObject[stripe.Subscription](event, "customer.subscription.updated")
	if err != nil {
		t.Fatalf("decode subscription: %v", err)
	}
	state := stripeSubscriptionStateFromSubscription(subscription, string(subscription.Status)).withDefaults()

	assertEqual(t, state.Status, "past_due", "status")
	assertEqual(t, state.StripeSubscriptionID, "sub_test", "stripe subscription id")
	assertEqual(t, state.StripeCustomerID, "cus_test", "stripe customer id")
	assertTimePresent(t, state.CurrentPeriodStart, "current period start")
	assertTimePresent(t, state.CurrentPeriodEnd, "current period end")
}

func TestClosedSubscriptionPaymentStatePreservesLastKnownState(t *testing.T) {
	t.Parallel()

	state := mergeStripeSubscriptionState(
		stripeSubscriptionState{Status: "canceled"},
		stripeSubscriptionState{PaymentState: PaymentPaid},
	)

	assertEqual(t, closedSubscriptionPaymentState(state), PaymentPaid, "closed subscription payment state")
	assertEqual(t, closedSubscriptionPaymentState(stripeSubscriptionState{}), PaymentPending, "unknown closed subscription payment state")
}

func TestSourceReferenceGrantIDIsDeterministicAndScoped(t *testing.T) {
	t.Parallel()

	first := sourceReferenceGrantID(42, SourceSubscription, GrantScopeBucket, "sandbox", "compute", "", "in_test")
	second := sourceReferenceGrantID(42, SourceSubscription, GrantScopeBucket, "sandbox", "compute", "", "in_test")
	differentBucket := sourceReferenceGrantID(42, SourceSubscription, GrantScopeBucket, "sandbox", "ram", "", "in_test")
	differentSource := sourceReferenceGrantID(42, SourceFreeTier, GrantScopeBucket, "sandbox", "compute", "", "in_test")

	assertEqual(t, first.String(), second.String(), "same source reference grant id")
	if first == differentBucket {
		t.Fatal("source reference grant id must be scoped by bucket")
	}
	if first == differentSource {
		t.Fatal("source reference grant id must be scoped by source")
	}
	if _, err := ParseGrantID(first.String()); err != nil {
		t.Fatalf("source reference grant id should round-trip through the public grant id format: %v", err)
	}
}

func assertTimePresent(t *testing.T, value *time.Time, label string) {
	t.Helper()
	if value == nil || value.IsZero() {
		t.Fatalf("%s is not present", label)
	}
}

func assertTimeEqual(t *testing.T, value *time.Time, expected time.Time, label string) {
	t.Helper()
	if value == nil {
		t.Fatalf("%s is not present", label)
	}
	if !value.Equal(expected) {
		t.Fatalf("%s: got %s, want %s", label, value.Format(time.RFC3339Nano), expected.Format(time.RFC3339Nano))
	}
}
