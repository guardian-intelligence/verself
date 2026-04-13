package billing

import (
	"testing"

	"github.com/stripe/stripe-go/v85"
)

func TestStripeSetupIntentDecodeUsesProviderNeutralMetadata(t *testing.T) {
	t.Parallel()

	event := stripe.Event{Data: &stripe.EventData{Raw: []byte(`{
		"id": "seti_test",
		"customer": "cus_test",
		"status": "succeeded",
		"metadata": {
			"org_id": "42",
			"product_id": "sandbox",
			"plan_id": "sandbox-pro",
			"contract_id": "contract_test",
			"phase_id": "phase_test",
			"cadence": "monthly"
		},
		"payment_method": {
			"id": "pm_test",
			"type": "card",
			"card": {
				"brand": "visa",
				"last4": "4242",
				"exp_month": 12,
				"exp_year": 2030
			}
		}
	}`)}}

	intent, err := decodeStripeEventObject[stripe.SetupIntent](event, "setup_intent.succeeded")
	if err != nil {
		t.Fatalf("decode setup intent: %v", err)
	}

	assertEqual(t, intent.Metadata["org_id"], "42", "org id")
	assertEqual(t, intent.Metadata["contract_id"], "contract_test", "contract id")
	if intent.PaymentMethod == nil {
		t.Fatal("payment method not decoded")
	}
	assertEqual(t, intent.PaymentMethod.ID, "pm_test", "payment method id")
	if intent.PaymentMethod.Card == nil {
		t.Fatal("payment method card not decoded")
	}
	assertEqual(t, string(intent.PaymentMethod.Card.Brand), "visa", "card brand")
	assertEqual(t, intent.PaymentMethod.Card.Last4, "4242", "card last4")
}

func TestSourceReferenceGrantIDIsDeterministicAndScoped(t *testing.T) {
	t.Parallel()

	first := sourceReferenceGrantID(42, SourceContract, GrantScopeBucket, "sandbox", "compute", "", "in_test")
	second := sourceReferenceGrantID(42, SourceContract, GrantScopeBucket, "sandbox", "compute", "", "in_test")
	differentBucket := sourceReferenceGrantID(42, SourceContract, GrantScopeBucket, "sandbox", "ram", "", "in_test")
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
