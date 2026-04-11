package billing

import "fmt"

type grantScopeType string

const (
	grantScopeBucket  grantScopeType = "bucket"
	grantScopeProduct grantScopeType = "product"
	grantScopeAccount grantScopeType = "account"
)

type applicableGrantBalance struct {
	GrantID        GrantID
	Source         GrantSourceType
	ScopeType      grantScopeType
	ScopeProductID string
	ScopeBucketID  string
	Available      uint64
}

type plannedApplicableFundingLeg struct {
	GrantID           GrantID
	Source            GrantSourceType
	Amount            uint64
	ChargeProductID   string
	ChargeBucketID    string
	GrantScopeType    grantScopeType
	GrantScopeProduct string
	GrantScopeBucket  string
}

func planApplicableGrantFunding(productID string, bucketChargeUnits map[string]uint64, grants []applicableGrantBalance) ([]plannedApplicableFundingLeg, error) {
	if productID == "" {
		return nil, fmt.Errorf("product_id is required")
	}

	remainingByBucket := cloneUint64Map(bucketChargeUnits)
	grantRemaining := make([]uint64, len(grants))
	for i, grant := range grants {
		if err := validateApplicableGrant(grant); err != nil {
			return nil, err
		}
		grantRemaining[i] = grant.Available
	}

	legs := make([]plannedApplicableFundingLeg, 0, len(grants))
	for _, scope := range []grantScopeType{grantScopeBucket, grantScopeProduct, grantScopeAccount} {
		for _, bucketID := range sortedUint64MapKeys(remainingByBucket) {
			for i, grant := range grants {
				if remainingByBucket[bucketID] == 0 {
					break
				}
				if grantRemaining[i] == 0 || !grantAppliesToBucket(grant, scope, productID, bucketID) {
					continue
				}
				amount := minUint64(grantRemaining[i], remainingByBucket[bucketID])
				grantRemaining[i] -= amount
				remainingByBucket[bucketID] -= amount
				legs = append(legs, plannedApplicableFundingLeg{
					GrantID:           grant.GrantID,
					Source:            grant.Source,
					Amount:            amount,
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

func validateApplicableGrant(grant applicableGrantBalance) error {
	if grant.GrantID == (GrantID{}) {
		return fmt.Errorf("grant_id is required")
	}
	if grant.Source == 0 {
		return fmt.Errorf("grant source is required")
	}
	switch grant.ScopeType {
	case grantScopeBucket:
		if grant.ScopeProductID == "" {
			return fmt.Errorf("bucket-scoped grant product_id is required")
		}
		if grant.ScopeBucketID == "" {
			return fmt.Errorf("bucket-scoped grant bucket_id is required")
		}
	case grantScopeProduct:
		if grant.ScopeProductID == "" {
			return fmt.Errorf("product-scoped grant product_id is required")
		}
		if grant.ScopeBucketID != "" {
			return fmt.Errorf("product-scoped grant bucket_id must be empty")
		}
	case grantScopeAccount:
		if grant.ScopeProductID != "" || grant.ScopeBucketID != "" {
			return fmt.Errorf("account-scoped grant product_id and bucket_id must be empty")
		}
	default:
		return fmt.Errorf("unknown grant scope %q", grant.ScopeType)
	}
	return nil
}

func grantAppliesToBucket(grant applicableGrantBalance, scope grantScopeType, productID string, bucketID string) bool {
	if grant.ScopeType != scope {
		return false
	}
	switch grant.ScopeType {
	case grantScopeBucket:
		return grant.ScopeProductID == productID && grant.ScopeBucketID == bucketID
	case grantScopeProduct:
		return grant.ScopeProductID == productID
	case grantScopeAccount:
		return true
	default:
		return false
	}
}
