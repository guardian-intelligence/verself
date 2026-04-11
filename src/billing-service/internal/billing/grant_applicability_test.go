package billing

import (
	"errors"
	"testing"
)

func TestPlanApplicableGrantFundingUsesBucketBeforeAccountCredit(t *testing.T) {
	t.Parallel()

	bucketGrant := testApplicableGrant("bucket-storage", SourceSubscription, grantScopeBucket, "sandbox", "storage", 120)
	accountGrant := testApplicableGrant("account-purchase", SourcePurchase, grantScopeAccount, "", "", 100)

	legs, err := planApplicableGrantFunding("sandbox", map[string]uint64{"storage": 100}, []applicableGrantBalance{
		bucketGrant,
		accountGrant,
	})
	if err != nil {
		t.Fatalf("planApplicableGrantFunding: %v", err)
	}

	assertEqual(t, len(legs), 1, "leg count")
	assertEqual(t, legs[0].GrantID, bucketGrant.GrantID, "funding grant")
	assertEqual(t, legs[0].Amount, uint64(100), "funding amount")
	assertEqual(t, legs[0].ChargeBucketID, "storage", "charge bucket")
	assertEqual(t, legs[0].GrantScopeType, grantScopeBucket, "grant scope")
}

func TestPlanApplicableGrantFundingUsesAccountCreditForSummedDeficits(t *testing.T) {
	t.Parallel()

	computeGrant := testApplicableGrant("bucket-compute", SourceSubscription, grantScopeBucket, "sandbox", "compute", 75)
	storageGrant := testApplicableGrant("bucket-storage", SourceSubscription, grantScopeBucket, "sandbox", "storage", 75)
	accountGrant := testApplicableGrant("account-purchase", SourcePurchase, grantScopeAccount, "", "", 50)

	legs, err := planApplicableGrantFunding("sandbox", map[string]uint64{
		"compute": 100,
		"storage": 100,
	}, []applicableGrantBalance{
		computeGrant,
		storageGrant,
		accountGrant,
	})
	if err != nil {
		t.Fatalf("planApplicableGrantFunding: %v", err)
	}

	assertEqual(t, len(legs), 4, "leg count")
	assertFundingLeg(t, legs[0], computeGrant, "compute", grantScopeBucket, 75)
	assertFundingLeg(t, legs[1], storageGrant, "storage", grantScopeBucket, 75)
	assertFundingLeg(t, legs[2], accountGrant, "compute", grantScopeAccount, 25)
	assertFundingLeg(t, legs[3], accountGrant, "storage", grantScopeAccount, 25)
}

func TestPlanApplicableGrantFundingDoesNotDoubleCountAccountCreditAcrossBuckets(t *testing.T) {
	t.Parallel()

	_, err := planApplicableGrantFunding("sandbox", map[string]uint64{
		"compute": 100,
		"storage": 100,
	}, []applicableGrantBalance{
		testApplicableGrant("bucket-compute", SourceSubscription, grantScopeBucket, "sandbox", "compute", 75),
		testApplicableGrant("bucket-storage", SourceSubscription, grantScopeBucket, "sandbox", "storage", 75),
		testApplicableGrant("account-purchase", SourcePurchase, grantScopeAccount, "", "", 40),
	})
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("planApplicableGrantFunding error = %v, want %v", err, ErrInsufficientBalance)
	}
}

func TestPlanApplicableGrantFundingUsesProductBeforeAccountCredit(t *testing.T) {
	t.Parallel()

	productGrant := testApplicableGrant("product-promo", SourcePromo, grantScopeProduct, "sandbox", "", 40)
	accountGrant := testApplicableGrant("account-purchase", SourcePurchase, grantScopeAccount, "", "", 60)

	legs, err := planApplicableGrantFunding("sandbox", map[string]uint64{"storage": 100}, []applicableGrantBalance{
		productGrant,
		accountGrant,
	})
	if err != nil {
		t.Fatalf("planApplicableGrantFunding: %v", err)
	}

	assertEqual(t, len(legs), 2, "leg count")
	assertFundingLeg(t, legs[0], productGrant, "storage", grantScopeProduct, 40)
	assertFundingLeg(t, legs[1], accountGrant, "storage", grantScopeAccount, 60)
}

func testApplicableGrant(id string, source GrantSourceType, scope grantScopeType, productID string, bucketID string, available uint64) applicableGrantBalance {
	return applicableGrantBalance{
		GrantID:        stripeGrantID(42, productID, bucketID, id),
		Source:         source,
		ScopeType:      scope,
		ScopeProductID: productID,
		ScopeBucketID:  bucketID,
		Available:      available,
	}
}

func assertFundingLeg(t *testing.T, leg plannedApplicableFundingLeg, grant applicableGrantBalance, chargeBucketID string, scope grantScopeType, amount uint64) {
	t.Helper()
	assertEqual(t, leg.GrantID, grant.GrantID, "grant id")
	assertEqual(t, leg.ChargeProductID, "sandbox", "charge product")
	assertEqual(t, leg.ChargeBucketID, chargeBucketID, "charge bucket")
	assertEqual(t, leg.GrantScopeType, scope, "grant scope")
	assertEqual(t, leg.Amount, amount, "amount")
}
