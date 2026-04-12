package billing

import (
	"errors"
	"testing"
)

// bucketCharges builds bucket-only charge lines (no SKU id) for tests that
// only care about bucket-and-wider grant matching.
func bucketCharges(charges map[string]uint64) []chargeLine {
	out := make([]chargeLine, 0, len(charges))
	for _, bucketID := range sortedUint64MapKeys(charges) {
		out = append(out, chargeLine{BucketID: bucketID, AmountUnits: charges[bucketID]})
	}
	return out
}

func TestPlanGrantFundingUsesBucketBeforeAccountCredit(t *testing.T) {
	t.Parallel()

	bucketGrant := testScopedGrant("bucket-storage", SourceSubscription, GrantScopeBucket, "sandbox", "storage", "", 120)
	accountGrant := testScopedGrant("account-purchase", SourcePurchase, GrantScopeAccount, "", "", "", 100)

	legs, err := planGrantFunding("sandbox", bucketCharges(map[string]uint64{"storage": 100}), []scopedGrantBalance{
		bucketGrant,
		accountGrant,
	})
	if err != nil {
		t.Fatalf("planGrantFunding: %v", err)
	}

	assertEqual(t, len(legs), 1, "leg count")
	assertEqual(t, legs[0].GrantID, bucketGrant.GrantID, "funding grant")
	assertEqual(t, legs[0].AmountUnits, uint64(100), "funding amount")
	assertEqual(t, legs[0].ChargeBucketID, "storage", "charge bucket")
	assertEqual(t, legs[0].GrantScopeType, GrantScopeBucket, "grant scope")
}

func TestPlanGrantFundingUsesAccountCreditForSummedDeficits(t *testing.T) {
	t.Parallel()

	computeGrant := testScopedGrant("bucket-compute", SourceSubscription, GrantScopeBucket, "sandbox", "compute", "", 75)
	storageGrant := testScopedGrant("bucket-storage", SourceSubscription, GrantScopeBucket, "sandbox", "storage", "", 75)
	accountGrant := testScopedGrant("account-purchase", SourcePurchase, GrantScopeAccount, "", "", "", 50)

	legs, err := planGrantFunding("sandbox", bucketCharges(map[string]uint64{
		"compute": 100,
		"storage": 100,
	}), []scopedGrantBalance{
		computeGrant,
		storageGrant,
		accountGrant,
	})
	if err != nil {
		t.Fatalf("planGrantFunding: %v", err)
	}

	assertEqual(t, len(legs), 4, "leg count")
	assertFundingLeg(t, legs[0], computeGrant, "compute", GrantScopeBucket, 75)
	assertFundingLeg(t, legs[1], storageGrant, "storage", GrantScopeBucket, 75)
	assertFundingLeg(t, legs[2], accountGrant, "compute", GrantScopeAccount, 25)
	assertFundingLeg(t, legs[3], accountGrant, "storage", GrantScopeAccount, 25)
}

func TestPlanGrantFundingRejectsUnrelatedBucketGrants(t *testing.T) {
	t.Parallel()

	accountGrant := testScopedGrant("account-purchase", SourcePurchase, GrantScopeAccount, "", "", "", 500)

	_, err := planGrantFunding("sandbox", bucketCharges(map[string]uint64{
		"compute": 600,
	}), []scopedGrantBalance{
		accountGrant,
		testScopedGrant("bucket-premium-disk", SourceFreeTier, GrantScopeBucket, "sandbox", "premium_disk", "", 100),
		testScopedGrant("bucket-regular-disk", SourceFreeTier, GrantScopeBucket, "sandbox", "regular_disk", "", 100),
	})
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("planGrantFunding error = %v, want %v", err, ErrInsufficientBalance)
	}
}

func TestPlanGrantFundingDoesNotCrossFundPremiumAndRegularDiskBuckets(t *testing.T) {
	t.Parallel()

	premiumGrant := testScopedGrant("bucket-premium-disk", SourceFreeTier, GrantScopeBucket, "sandbox", "premium_disk", "", 100)
	regularGrant := testScopedGrant("bucket-regular-disk", SourceFreeTier, GrantScopeBucket, "sandbox", "regular_disk", "", 100)

	legs, err := planGrantFunding("sandbox", bucketCharges(map[string]uint64{
		"premium_disk": 60,
		"regular_disk": 40,
	}), []scopedGrantBalance{
		premiumGrant,
		regularGrant,
	})
	if err != nil {
		t.Fatalf("planGrantFunding: %v", err)
	}

	assertEqual(t, len(legs), 2, "leg count")
	assertFundingLeg(t, legs[0], premiumGrant, "premium_disk", GrantScopeBucket, 60)
	assertFundingLeg(t, legs[1], regularGrant, "regular_disk", GrantScopeBucket, 40)
}

func TestPlanGrantFundingDoesNotDoubleCountAccountCreditAcrossBuckets(t *testing.T) {
	t.Parallel()

	_, err := planGrantFunding("sandbox", bucketCharges(map[string]uint64{
		"compute": 100,
		"storage": 100,
	}), []scopedGrantBalance{
		testScopedGrant("bucket-compute", SourceSubscription, GrantScopeBucket, "sandbox", "compute", "", 75),
		testScopedGrant("bucket-storage", SourceSubscription, GrantScopeBucket, "sandbox", "storage", "", 75),
		testScopedGrant("account-purchase", SourcePurchase, GrantScopeAccount, "", "", "", 40),
	})
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("planGrantFunding error = %v, want %v", err, ErrInsufficientBalance)
	}
}

func TestPlanGrantFundingUsesFreeTierBeforePaidCreditWithinEachScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		scope            GrantScopeType
		grantProductID   string
		grantBucketID    string
		chargeBucketID   string
		expectedLegScope GrantScopeType
	}{
		{
			name:             "bucket",
			scope:            GrantScopeBucket,
			grantProductID:   "sandbox",
			grantBucketID:    "storage",
			chargeBucketID:   "storage",
			expectedLegScope: GrantScopeBucket,
		},
		{
			name:             "product",
			scope:            GrantScopeProduct,
			grantProductID:   "sandbox",
			chargeBucketID:   "storage",
			expectedLegScope: GrantScopeProduct,
		},
		{
			name:             "account",
			scope:            GrantScopeAccount,
			chargeBucketID:   "storage",
			expectedLegScope: GrantScopeAccount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			purchaseGrant := testScopedGrant(tt.name+"-purchase", SourcePurchase, tt.scope, tt.grantProductID, tt.grantBucketID, "", 100)
			freeTierGrant := testScopedGrant(tt.name+"-free-tier", SourceFreeTier, tt.scope, tt.grantProductID, tt.grantBucketID, "", 100)

			legs, err := planGrantFunding("sandbox", bucketCharges(map[string]uint64{tt.chargeBucketID: 150}), []scopedGrantBalance{
				purchaseGrant,
				freeTierGrant,
			})
			if err != nil {
				t.Fatalf("planGrantFunding: %v", err)
			}

			assertEqual(t, len(legs), 2, "leg count")
			assertFundingLeg(t, legs[0], freeTierGrant, tt.chargeBucketID, tt.expectedLegScope, 100)
			assertFundingLeg(t, legs[1], purchaseGrant, tt.chargeBucketID, tt.expectedLegScope, 50)
		})
	}
}

func TestPlanGrantFundingUsesProductBeforeAccountCredit(t *testing.T) {
	t.Parallel()

	productGrant := testScopedGrant("product-promo", SourcePromo, GrantScopeProduct, "sandbox", "", "", 40)
	accountGrant := testScopedGrant("account-purchase", SourcePurchase, GrantScopeAccount, "", "", "", 60)

	legs, err := planGrantFunding("sandbox", bucketCharges(map[string]uint64{"storage": 100}), []scopedGrantBalance{
		productGrant,
		accountGrant,
	})
	if err != nil {
		t.Fatalf("planGrantFunding: %v", err)
	}

	assertEqual(t, len(legs), 2, "leg count")
	assertFundingLeg(t, legs[0], productGrant, "storage", GrantScopeProduct, 40)
	assertFundingLeg(t, legs[1], accountGrant, "storage", GrantScopeAccount, 60)
}

func TestPlanGrantFundingUsesSKUBeforeBucketGrantWhenChargeNamesSKU(t *testing.T) {
	t.Parallel()

	skuGrant := testScopedGrant("sku-premium-nvme", SourceSubscription, GrantScopeSKU, "sandbox", "block_storage", "premium_nvme", 60)
	bucketGrant := testScopedGrant("bucket-block-storage", SourceSubscription, GrantScopeBucket, "sandbox", "block_storage", "", 100)

	legs, err := planGrantFunding("sandbox", []chargeLine{
		{BucketID: "block_storage", SKUID: "premium_nvme", AmountUnits: 80},
	}, []scopedGrantBalance{bucketGrant, skuGrant})
	if err != nil {
		t.Fatalf("planGrantFunding: %v", err)
	}

	assertEqual(t, len(legs), 2, "leg count")
	assertEqual(t, legs[0].GrantID, skuGrant.GrantID, "first leg drains the SKU grant")
	assertEqual(t, legs[0].AmountUnits, uint64(60), "sku amount")
	assertEqual(t, legs[0].GrantScopeType, GrantScopeSKU, "first leg scope")
	assertEqual(t, legs[1].GrantID, bucketGrant.GrantID, "second leg falls through to bucket")
	assertEqual(t, legs[1].AmountUnits, uint64(20), "bucket residual")
}

func TestPlanGrantFundingSKUGrantSkippedForBucketOnlyChargeLine(t *testing.T) {
	t.Parallel()

	// A bucket-only charge line (no SKU named) cannot drain a SKU-scoped grant —
	// the funder needs the SKU id to know the workload owns that SKU.
	skuGrant := testScopedGrant("sku-premium-nvme", SourceSubscription, GrantScopeSKU, "sandbox", "block_storage", "premium_nvme", 100)

	_, err := planGrantFunding("sandbox", bucketCharges(map[string]uint64{"block_storage": 50}), []scopedGrantBalance{skuGrant})
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("planGrantFunding error = %v, want %v", err, ErrInsufficientBalance)
	}
}

func testScopedGrant(id string, source GrantSourceType, scope GrantScopeType, productID, bucketID, skuID string, availableUnits uint64) scopedGrantBalance {
	return scopedGrantBalance{
		GrantID:        sourceReferenceGrantID(42, source, scope, productID, bucketID, skuID, id),
		Source:         source,
		ScopeType:      scope,
		ScopeProductID: productID,
		ScopeBucketID:  bucketID,
		ScopeSKUID:     skuID,
		AvailableUnits: availableUnits,
	}
}

func assertFundingLeg(t *testing.T, leg plannedGrantFundingLeg, grant scopedGrantBalance, chargeBucketID string, scope GrantScopeType, amountUnits uint64) {
	t.Helper()
	assertEqual(t, leg.GrantID, grant.GrantID, "grant id")
	assertEqual(t, leg.ChargeProductID, "sandbox", "charge product")
	assertEqual(t, leg.ChargeBucketID, chargeBucketID, "charge bucket")
	assertEqual(t, leg.GrantScopeType, scope, "grant scope")
	assertEqual(t, leg.AmountUnits, amountUnits, "amount")
}
