package billing

import (
	"errors"
	"testing"
)

func TestPlanGrantFundingUsesBucketBeforeAccountCredit(t *testing.T) {
	t.Parallel()

	bucketGrant := testScopedGrant("bucket-storage", SourceSubscription, GrantScopeBucket, "sandbox", "storage", 120)
	accountGrant := testScopedGrant("account-purchase", SourcePurchase, GrantScopeAccount, "", "", 100)

	legs, err := planGrantFunding("sandbox", map[string]uint64{"storage": 100}, []scopedGrantBalance{
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

	computeGrant := testScopedGrant("bucket-compute", SourceSubscription, GrantScopeBucket, "sandbox", "compute", 75)
	storageGrant := testScopedGrant("bucket-storage", SourceSubscription, GrantScopeBucket, "sandbox", "storage", 75)
	accountGrant := testScopedGrant("account-purchase", SourcePurchase, GrantScopeAccount, "", "", 50)

	legs, err := planGrantFunding("sandbox", map[string]uint64{
		"compute": 100,
		"storage": 100,
	}, []scopedGrantBalance{
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

	accountGrant := testScopedGrant("account-purchase", SourcePurchase, GrantScopeAccount, "", "", 500)

	_, err := planGrantFunding("sandbox", map[string]uint64{
		"compute": 600,
	}, []scopedGrantBalance{
		accountGrant,
		testScopedGrant("bucket-premium-disk", SourceFreeTier, GrantScopeBucket, "sandbox", "premium_disk", 100),
		testScopedGrant("bucket-regular-disk", SourceFreeTier, GrantScopeBucket, "sandbox", "regular_disk", 100),
	})
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("planGrantFunding error = %v, want %v", err, ErrInsufficientBalance)
	}
}

func TestPlanGrantFundingDoesNotCrossFundPremiumAndRegularDiskBuckets(t *testing.T) {
	t.Parallel()

	premiumGrant := testScopedGrant("bucket-premium-disk", SourceFreeTier, GrantScopeBucket, "sandbox", "premium_disk", 100)
	regularGrant := testScopedGrant("bucket-regular-disk", SourceFreeTier, GrantScopeBucket, "sandbox", "regular_disk", 100)

	legs, err := planGrantFunding("sandbox", map[string]uint64{
		"premium_disk": 60,
		"regular_disk": 40,
	}, []scopedGrantBalance{
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

	_, err := planGrantFunding("sandbox", map[string]uint64{
		"compute": 100,
		"storage": 100,
	}, []scopedGrantBalance{
		testScopedGrant("bucket-compute", SourceSubscription, GrantScopeBucket, "sandbox", "compute", 75),
		testScopedGrant("bucket-storage", SourceSubscription, GrantScopeBucket, "sandbox", "storage", 75),
		testScopedGrant("account-purchase", SourcePurchase, GrantScopeAccount, "", "", 40),
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

			purchaseGrant := testScopedGrant(tt.name+"-purchase", SourcePurchase, tt.scope, tt.grantProductID, tt.grantBucketID, 100)
			freeTierGrant := testScopedGrant(tt.name+"-free-tier", SourceFreeTier, tt.scope, tt.grantProductID, tt.grantBucketID, 100)

			legs, err := planGrantFunding("sandbox", map[string]uint64{tt.chargeBucketID: 150}, []scopedGrantBalance{
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

	productGrant := testScopedGrant("product-promo", SourcePromo, GrantScopeProduct, "sandbox", "", 40)
	accountGrant := testScopedGrant("account-purchase", SourcePurchase, GrantScopeAccount, "", "", 60)

	legs, err := planGrantFunding("sandbox", map[string]uint64{"storage": 100}, []scopedGrantBalance{
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

func testScopedGrant(id string, source GrantSourceType, scope GrantScopeType, productID string, bucketID string, availableUnits uint64) scopedGrantBalance {
	return scopedGrantBalance{
		GrantID:        stripeGrantID(42, scope, productID, bucketID, id),
		Source:         source,
		ScopeType:      scope,
		ScopeProductID: productID,
		ScopeBucketID:  bucketID,
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
