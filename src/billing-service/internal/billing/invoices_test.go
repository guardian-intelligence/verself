package billing

import "testing"

func TestBuildNoConsentAdjustmentArtifactsUsesWriteoffEvidence(t *testing.T) {
	t.Parallel()

	cycle := BillingCycle{
		CycleID:   "cycle_01",
		OrgID:     42,
		ProductID: "sandbox",
	}
	window := persistedWindow{
		WindowID:            "window_01",
		ProductID:           "sandbox",
		PlanID:              "sandbox-free",
		WriteoffQuantity:    2,
		WriteoffChargeUnits: 300,
		WriteoffReason:      "settled_beyond_reserved_quantity",
		RateContext: windowRateContext{
			ComponentCostPerUnit: map[string]uint64{"sandbox_compute": 150},
			BucketCostPerUnit:    map[string]uint64{"compute": 150},
		},
	}

	adjustments, total, err := buildNoConsentAdjustmentArtifacts(cycle, "invoice_01", []persistedWindow{window})
	if err != nil {
		t.Fatalf("build adjustments: %v", err)
	}
	if total != 300 {
		t.Fatalf("total = %d, want 300", total)
	}
	if len(adjustments) != 1 {
		t.Fatalf("adjustment count = %d, want 1", len(adjustments))
	}
	adjustment := adjustments[0]
	if adjustment.ReasonCode != "free_tier_overage_absorbed" {
		t.Fatalf("reason = %q", adjustment.ReasonCode)
	}
	if adjustment.AdjustmentSource != "system_policy" || adjustment.AdjustmentType != "credit" {
		t.Fatalf("unexpected adjustment source/type: %+v", adjustment)
	}
	if adjustment.CustomerVisible || adjustment.Recoverable || adjustment.AffectsCustomerBalance {
		t.Fatalf("automatic no-consent adjustment must not affect customer balance: %+v", adjustment)
	}

	again, againTotal, err := buildNoConsentAdjustmentArtifacts(cycle, "invoice_01", []persistedWindow{window})
	if err != nil {
		t.Fatalf("build adjustments again: %v", err)
	}
	if againTotal != total || len(again) != 1 || again[0].AdjustmentID != adjustment.AdjustmentID {
		t.Fatalf("adjustment is not deterministic: first=%+v again=%+v", adjustment, again)
	}
}

func TestNoConsentAdjustmentReasonCodeDistinguishesPaidHardCap(t *testing.T) {
	t.Parallel()

	if got := noConsentAdjustmentReasonCode(persistedWindow{}); got != "free_tier_overage_absorbed" {
		t.Fatalf("free reason = %q", got)
	}
	if got := noConsentAdjustmentReasonCode(persistedWindow{PricingContractID: "contract_01"}); got != "paid_hard_cap_overage_absorbed" {
		t.Fatalf("paid hard-cap reason = %q", got)
	}
}

func TestInvoiceLineItemFromStatementItemIsDeterministic(t *testing.T) {
	t.Parallel()

	item := StatementLineItem{
		ProductID:         "sandbox",
		PlanID:            "sandbox-hobby",
		BucketID:          "compute",
		BucketDisplayName: "Compute",
		SKUID:             "sandbox_compute",
		SKUDisplayName:    "vCPU",
		QuantityUnit:      "vCPU-second",
		PricingPhase:      "metered",
		Quantity:          12,
		UnitRate:          34,
		ChargeUnits:       408,
		ContractUnits:     408,
	}

	first, err := invoiceLineItemFromStatementItem("invoice_01", 0, item)
	if err != nil {
		t.Fatalf("line item: %v", err)
	}
	second, err := invoiceLineItemFromStatementItem("invoice_01", 0, item)
	if err != nil {
		t.Fatalf("line item again: %v", err)
	}
	if first.LineItemID != second.LineItemID {
		t.Fatalf("line item id = %q, want deterministic %q", second.LineItemID, first.LineItemID)
	}
	if first.Description != "Compute - vCPU" {
		t.Fatalf("description = %q", first.Description)
	}
	if first.ContractUnits != 408 || first.ChargeUnits != 408 {
		t.Fatalf("unexpected units: %+v", first)
	}
}
