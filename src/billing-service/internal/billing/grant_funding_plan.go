package billing

import "fmt"

type GrantScopeType string

const (
	GrantScopeSKU     GrantScopeType = "sku"
	GrantScopeBucket  GrantScopeType = "bucket"
	GrantScopeProduct GrantScopeType = "product"
	GrantScopeAccount GrantScopeType = "account"
)

// GrantScopeFundingOrder is the precedence used by both the reserve-time funder
// and the entitlements view-model. Tightest scope first; the customer-facing
// "next to spend" position in any cell is the topmost grant after applying this
// order followed by GrantSourceFundingOrder.
var GrantScopeFundingOrder = []GrantScopeType{
	GrantScopeSKU,
	GrantScopeBucket,
	GrantScopeProduct,
	GrantScopeAccount,
}

// GrantSourceFundingOrder is the source priority applied inside each scope.
// Free-tier always burns first so paid balances last as long as possible.
var GrantSourceFundingOrder = []GrantSourceType{
	SourceFreeTier,
	SourceSubscription,
	SourcePurchase,
	SourcePromo,
	SourceRefund,
}

func ParseGrantScopeType(scope string) (GrantScopeType, error) {
	switch GrantScopeType(scope) {
	case GrantScopeSKU:
		return GrantScopeSKU, nil
	case GrantScopeBucket:
		return GrantScopeBucket, nil
	case GrantScopeProduct:
		return GrantScopeProduct, nil
	case GrantScopeAccount:
		return GrantScopeAccount, nil
	default:
		return "", fmt.Errorf("unknown grant scope %q", scope)
	}
}

func (t GrantScopeType) String() string {
	return string(t)
}

type scopedGrantBalance struct {
	GrantID        GrantID
	Source         GrantSourceType
	ScopeType      GrantScopeType
	ScopeProductID string
	ScopeBucketID  string
	ScopeSKUID     string
	AvailableUnits uint64
}

type plannedGrantFundingLeg struct {
	GrantID             GrantID
	Source              GrantSourceType
	AmountUnits         uint64
	ChargeProductID     string
	ChargeBucketID      string
	ChargeSKUID         string
	GrantScopeType      GrantScopeType
	GrantScopeProductID string
	GrantScopeBucketID  string
	GrantScopeSKUID     string
}

// chargeLine is one (sku, bucket) pair the funder must cover. The funder
// consumes scope-tightest grants first, so it needs the SKU id alongside the
// bucket id to honour SKU-scoped grants.
type chargeLine struct {
	BucketID    string
	SKUID       string
	AmountUnits uint64
}

// planGrantFunding applies grant-scope precedence and source priority inside each scope.
//
// charges is one entry per (sku, bucket) line the workload must pay for.
// Callers that have only bucket-level totals should pass SKUID="" — bucket and
// wider grants still match.
func planGrantFunding(productID string, charges []chargeLine, grants []scopedGrantBalance) ([]plannedGrantFundingLeg, error) {
	if productID == "" {
		return nil, fmt.Errorf("product_id is required")
	}
	for _, charge := range charges {
		if charge.BucketID == "" {
			return nil, fmt.Errorf("charge bucket_id is required")
		}
	}

	remaining := make([]uint64, len(charges))
	for i, charge := range charges {
		remaining[i] = charge.AmountUnits
	}

	grantRemaining := make([]uint64, len(grants))
	for i, grant := range grants {
		if err := validateGrantForFunding(grant); err != nil {
			return nil, err
		}
		grantRemaining[i] = grant.AvailableUnits
	}

	legs := make([]plannedGrantFundingLeg, 0, len(grants))
	for _, scope := range GrantScopeFundingOrder {
		for chargeIdx, charge := range charges {
			for _, source := range GrantSourceFundingOrder {
				for grantIdx, grant := range grants {
					if remaining[chargeIdx] == 0 {
						break
					}
					if grant.Source != source || grantRemaining[grantIdx] == 0 || !grantCanFundCharge(grant, scope, productID, charge.BucketID, charge.SKUID) {
						continue
					}
					amount := minUint64(grantRemaining[grantIdx], remaining[chargeIdx])
					grantRemaining[grantIdx] -= amount
					remaining[chargeIdx] -= amount
					legs = append(legs, plannedGrantFundingLeg{
						GrantID:             grant.GrantID,
						Source:              grant.Source,
						AmountUnits:         amount,
						ChargeProductID:     productID,
						ChargeBucketID:      charge.BucketID,
						ChargeSKUID:         charge.SKUID,
						GrantScopeType:      grant.ScopeType,
						GrantScopeProductID: grant.ScopeProductID,
						GrantScopeBucketID:  grant.ScopeBucketID,
						GrantScopeSKUID:     grant.ScopeSKUID,
					})
				}
			}
		}
	}

	for _, amount := range remaining {
		if amount != 0 {
			return nil, ErrInsufficientBalance
		}
	}
	return legs, nil
}

func validateGrantForFunding(grant scopedGrantBalance) error {
	if grant.GrantID == (GrantID{}) {
		return fmt.Errorf("grant_id is required")
	}
	if grant.Source == 0 {
		return fmt.Errorf("grant source is required")
	}
	return validateGrantScope(grant.ScopeType, grant.ScopeProductID, grant.ScopeBucketID, grant.ScopeSKUID)
}

func validateGrantScope(scopeType GrantScopeType, scopeProductID, scopeBucketID, scopeSKUID string) error {
	switch scopeType {
	case GrantScopeSKU:
		if scopeProductID == "" {
			return fmt.Errorf("sku-scoped grant product_id is required")
		}
		if scopeBucketID == "" {
			return fmt.Errorf("sku-scoped grant bucket_id is required")
		}
		if scopeSKUID == "" {
			return fmt.Errorf("sku-scoped grant sku_id is required")
		}
	case GrantScopeBucket:
		if scopeProductID == "" {
			return fmt.Errorf("bucket-scoped grant product_id is required")
		}
		if scopeBucketID == "" {
			return fmt.Errorf("bucket-scoped grant bucket_id is required")
		}
		if scopeSKUID != "" {
			return fmt.Errorf("bucket-scoped grant sku_id must be empty")
		}
	case GrantScopeProduct:
		if scopeProductID == "" {
			return fmt.Errorf("product-scoped grant product_id is required")
		}
		if scopeBucketID != "" {
			return fmt.Errorf("product-scoped grant bucket_id must be empty")
		}
		if scopeSKUID != "" {
			return fmt.Errorf("product-scoped grant sku_id must be empty")
		}
	case GrantScopeAccount:
		if scopeProductID != "" || scopeBucketID != "" || scopeSKUID != "" {
			return fmt.Errorf("account-scoped grant product_id, bucket_id, and sku_id must be empty")
		}
	default:
		return fmt.Errorf("unknown grant scope %q", scopeType)
	}
	return nil
}

func grantCanFundCharge(grant scopedGrantBalance, scope GrantScopeType, productID, bucketID, skuID string) bool {
	if grant.ScopeType != scope {
		return false
	}
	switch grant.ScopeType {
	case GrantScopeSKU:
		// SKU-scoped grants only fund a charge that names the same SKU. Bucket-only
		// charge lines (skuID == "") never reach a SKU-scoped grant — they fall
		// through to the next scope.
		return skuID != "" && grant.ScopeProductID == productID && grant.ScopeBucketID == bucketID && grant.ScopeSKUID == skuID
	case GrantScopeBucket:
		return grant.ScopeProductID == productID && grant.ScopeBucketID == bucketID
	case GrantScopeProduct:
		return grant.ScopeProductID == productID
	case GrantScopeAccount:
		return true
	default:
		return false
	}
}
