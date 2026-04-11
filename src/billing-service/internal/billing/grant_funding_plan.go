package billing

import "fmt"

type GrantScopeType string

const (
	GrantScopeBucket  GrantScopeType = "bucket"
	GrantScopeProduct GrantScopeType = "product"
	GrantScopeAccount GrantScopeType = "account"
)

type scopedGrantBalance struct {
	GrantID        GrantID
	Source         GrantSourceType
	ScopeType      GrantScopeType
	ScopeProductID string
	ScopeBucketID  string
	AvailableUnits uint64
}

type plannedGrantFundingLeg struct {
	GrantID           GrantID
	Source            GrantSourceType
	AmountUnits       uint64
	ChargeProductID   string
	ChargeBucketID    string
	GrantScopeType    GrantScopeType
	GrantScopeProduct string
	GrantScopeBucket  string
}

// planGrantFunding applies grant-scope precedence; callers still own grant waterfall ordering.
func planGrantFunding(productID string, bucketChargeUnits map[string]uint64, grants []scopedGrantBalance) ([]plannedGrantFundingLeg, error) {
	if productID == "" {
		return nil, fmt.Errorf("product_id is required")
	}

	remainingByBucket := cloneUint64Map(bucketChargeUnits)
	grantRemaining := make([]uint64, len(grants))
	for i, grant := range grants {
		if err := validateGrantForFunding(grant); err != nil {
			return nil, err
		}
		grantRemaining[i] = grant.AvailableUnits
	}

	legs := make([]plannedGrantFundingLeg, 0, len(grants))
	for _, scope := range []GrantScopeType{GrantScopeBucket, GrantScopeProduct, GrantScopeAccount} {
		for _, bucketID := range sortedUint64MapKeys(remainingByBucket) {
			for i, grant := range grants {
				if remainingByBucket[bucketID] == 0 {
					break
				}
				if grantRemaining[i] == 0 || !grantCanFundCharge(grant, scope, productID, bucketID) {
					continue
				}
				amount := minUint64(grantRemaining[i], remainingByBucket[bucketID])
				grantRemaining[i] -= amount
				remainingByBucket[bucketID] -= amount
				legs = append(legs, plannedGrantFundingLeg{
					GrantID:           grant.GrantID,
					Source:            grant.Source,
					AmountUnits:       amount,
					ChargeProductID:   productID,
					ChargeBucketID:    bucketID,
					GrantScopeType:    grant.ScopeType,
					GrantScopeProduct: grant.ScopeProductID,
					GrantScopeBucket:  grant.ScopeBucketID,
				})
			}
		}
	}

	for _, bucketID := range sortedUint64MapKeys(remainingByBucket) {
		if remainingByBucket[bucketID] != 0 {
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
	switch grant.ScopeType {
	case GrantScopeBucket:
		if grant.ScopeProductID == "" {
			return fmt.Errorf("bucket-scoped grant product_id is required")
		}
		if grant.ScopeBucketID == "" {
			return fmt.Errorf("bucket-scoped grant bucket_id is required")
		}
	case GrantScopeProduct:
		if grant.ScopeProductID == "" {
			return fmt.Errorf("product-scoped grant product_id is required")
		}
		if grant.ScopeBucketID != "" {
			return fmt.Errorf("product-scoped grant bucket_id must be empty")
		}
	case GrantScopeAccount:
		if grant.ScopeProductID != "" || grant.ScopeBucketID != "" {
			return fmt.Errorf("account-scoped grant product_id and bucket_id must be empty")
		}
	default:
		return fmt.Errorf("unknown grant scope %q", grant.ScopeType)
	}
	return nil
}

func grantCanFundCharge(grant scopedGrantBalance, scope GrantScopeType, productID string, bucketID string) bool {
	if grant.ScopeType != scope {
		return false
	}
	switch grant.ScopeType {
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
