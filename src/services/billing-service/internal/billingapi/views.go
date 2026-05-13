package billingapi

import (
	"fmt"
	"strconv"
	"time"

	"github.com/verself/billing-service/internal/billing"
	"github.com/verself/billing-service/internal/contractapi"
	"github.com/verself/billing-service/internal/internalcontractapi"
)

func entitlementsResponse(view billing.EntitlementsView) contractapi.BillingEntitlementsView {
	products := make(contractapi.BillingEntitlementProductSections, 0, len(view.Products))
	for _, product := range view.Products {
		buckets := make(contractapi.BillingEntitlementBucketSections, 0, len(product.Buckets))
		for _, bucket := range product.Buckets {
			skus := make(contractapi.BillingEntitlementSlots, 0, len(bucket.SKUSlots))
			for _, slot := range bucket.SKUSlots {
				skus = append(skus, entitlementSlot(slot))
			}
			buckets = append(buckets, contractapi.BillingEntitlementBucketSection{
				BucketID:    contractapi.BillingBucketID(bucket.BucketID),
				DisplayName: contractapi.DisplayName(bucket.DisplayName),
				BucketSlot:  entitlementSlotPtr(bucket.BucketSlot),
				SkuSlots:    skus,
			})
		}
		products = append(products, contractapi.BillingEntitlementProductSection{
			ProductID:   contractapi.ProductID(product.ProductID),
			DisplayName: contractapi.DisplayName(product.DisplayName),
			ProductSlot: entitlementSlotPtr(product.ProductSlot),
			Buckets:     buckets,
		})
	}
	return contractapi.BillingEntitlementsView{
		OrgID:     contractapi.OrgID(strconv.FormatUint(uint64(view.OrgID), 10)),
		Universal: entitlementSlot(view.Universal),
		Products:  products,
	}
}

func entitlementSlotPtr(slot *billing.EntitlementSlot) *contractapi.BillingEntitlementSlot {
	if slot == nil {
		return nil
	}
	out := entitlementSlot(*slot)
	return &out
}

func entitlementSlot(slot billing.EntitlementSlot) contractapi.BillingEntitlementSlot {
	sources := make(contractapi.BillingEntitlementSourceTotals, 0, len(slot.Sources))
	for _, source := range slot.Sources {
		sources = append(sources, contractapi.BillingEntitlementSourceTotal{
			Source:           contractapi.BillingSource(source.Source),
			PlanID:           contractapi.BillingScopeID(source.PlanID),
			Label:            contractapi.BillingLabel(source.Label),
			PeriodStartUnits: decimalUint64(source.PeriodStartUnits),
			AvailableUnits:   decimalUint64(source.AvailableUnits),
			PendingUnits:     decimalUint64(source.PendingUnits),
			InlineExpiresAt:  timeStringPtr(source.InlineExpiresAt),
		})
	}
	return contractapi.BillingEntitlementSlot{
		ScopeType:        contractapi.BillingScopeType(slot.ScopeType),
		ProductID:        contractapi.BillingScopeID(slot.ProductID),
		ProductDisplay:   contractapi.BillingDisplayName(slot.ProductDisplay),
		BucketID:         contractapi.BillingScopeID(slot.BucketID),
		BucketDisplay:    contractapi.BillingDisplayName(slot.BucketDisplay),
		SkuID:            contractapi.BillingSkuID(slot.SKUID),
		SkuDisplay:       contractapi.BillingDisplayName(slot.SKUDisplay),
		CoverageLabel:    contractapi.CoverageLabel(slot.CoverageLabel),
		PeriodStartUnits: decimalUint64(slot.PeriodStartUnits),
		SpentUnits:       decimalUint64(slot.SpentUnits),
		PendingUnits:     decimalUint64(slot.PendingUnits),
		AvailableUnits:   decimalUint64(slot.AvailableUnits),
		Sources:          sources,
	}
}

func grantResponse(grant billing.GrantBalance) contractapi.BillingGrant {
	return contractapi.BillingGrant{
		GrantID:             grant.GrantID,
		ScopeType:           contractapi.BillingScopeType(grant.ScopeType),
		ScopeProductID:      contractapi.BillingScopeID(grant.ScopeProductID),
		ScopeBucketID:       contractapi.BillingScopeID(grant.ScopeBucketID),
		ScopeSkuID:          contractapi.BillingSkuID(grant.ScopeSKUID),
		Source:              contractapi.BillingSource(grant.Source),
		SourceReferenceID:   contractapi.BillingSourceReferenceID(grant.SourceReferenceID),
		EntitlementPeriodID: contractapi.BillingEntitlementPeriodID(grant.EntitlementPeriodID),
		PolicyVersion:       contractapi.BillingPolicyVersion(grant.PolicyVersion),
		StartsAt:            timeString(grant.StartsAt),
		PeriodStart:         timeStringPtr(grant.PeriodStart),
		PeriodEnd:           timeStringPtr(grant.PeriodEnd),
		Available:           decimalUint64(grant.Available),
		Pending:             decimalUint64(grant.Pending),
		ExpiresAt:           timeStringPtr(grant.ExpiresAt),
	}
}

func documentResponse(installationID string, orgID billing.OrgID, document billing.DocumentRecord) contractapi.BillingDocument {
	return contractapi.BillingDocument{
		DocumentID:             contractapi.DocumentID(document.DocumentID),
		ResourceName:           contractapi.ResourceName(resourceNameBillingDocument(installationID, strconv.FormatUint(uint64(orgID), 10), document.DocumentID)),
		DocumentNumber:         contractapi.DocumentNumber(document.DocumentNumber),
		DocumentKind:           contractapi.DocumentKind(document.DocumentKind),
		FinalizationID:         contractapi.BillingFinalizationID(document.FinalizationID),
		ProductID:              contractapi.ProductID(document.ProductID),
		CycleID:                contractapi.CycleID(document.CycleID),
		Status:                 contractapi.BillingState(document.Status),
		PaymentStatus:          contractapi.PaymentState(document.PaymentStatus),
		PeriodStart:            timeString(document.PeriodStart),
		PeriodEnd:              timeString(document.PeriodEnd),
		IssuedAt:               timeStringPtr(document.IssuedAt),
		Currency:               contractapi.Currency(document.Currency),
		SubtotalUnits:          decimalUint64(document.SubtotalUnits),
		AdjustmentUnits:        decimalInt64(document.AdjustmentUnits),
		TaxUnits:               decimalUint64(document.TaxUnits),
		TotalDueUnits:          decimalUint64(document.TotalDueUnits),
		StripeHostedInvoiceURL: optionalURL(document.StripeHostedInvoiceURL),
		StripeInvoicePdfURL:    optionalURL(document.StripeInvoicePDFURL),
		StripePaymentIntentID:  optionalString(document.StripePaymentIntentID),
	}
}

func statementResponse(statement billing.Statement) contractapi.BillingStatement {
	items := make(contractapi.BillingStatementLineItems, 0, len(statement.LineItems))
	for _, line := range statement.LineItems {
		items = append(items, contractapi.BillingStatementLineItem{
			ProductID:         contractapi.ProductID(line.ProductID),
			PlanID:            contractapi.BillingScopeID(line.PlanID),
			BucketID:          contractapi.BillingBucketID(line.BucketID),
			BucketDisplayName: contractapi.BillingDisplayName(line.BucketDisplayName),
			SkuID:             contractapi.BillingSkuID(line.SKUID),
			SkuDisplayName:    contractapi.BillingDisplayName(line.SKUDisplayName),
			QuantityUnit:      contractapi.QuantityUnit(line.QuantityUnit),
			PricingPhase:      contractapi.PricingPhase(line.PricingPhase),
			Quantity:          line.Quantity,
			UnitRate:          decimalUint64(line.UnitRate),
			ChargeUnits:       decimalUint64(line.ChargeUnits),
			FreeTierUnits:     decimalUint64(line.FreeTierUnits),
			ContractUnits:     decimalUint64(line.ContractUnits),
			PurchaseUnits:     decimalUint64(line.PurchaseUnits),
			PromoUnits:        decimalUint64(line.PromoUnits),
			RefundUnits:       decimalUint64(line.RefundUnits),
			ReceivableUnits:   decimalUint64(line.ReceivableUnits),
			ReservedUnits:     decimalUint64(line.ReservedUnits),
		})
	}
	summaries := make(contractapi.BillingStatementGrantSummaries, 0, len(statement.GrantSummaries))
	for _, summary := range statement.GrantSummaries {
		summaries = append(summaries, contractapi.BillingStatementGrantSummary{
			ScopeType:      contractapi.BillingScopeType(summary.ScopeType),
			ScopeProductID: contractapi.BillingScopeID(summary.ScopeProductID),
			ScopeBucketID:  contractapi.BillingScopeID(summary.ScopeBucketID),
			Source:         contractapi.BillingSource(summary.Source),
			Available:      decimalUint64(summary.Available),
			Pending:        decimalUint64(summary.Pending),
		})
	}
	return contractapi.BillingStatement{
		OrgID:          contractapi.OrgID(strconv.FormatUint(uint64(statement.OrgID), 10)),
		ProductID:      contractapi.ProductID(statement.ProductID),
		PeriodStart:    timeString(statement.PeriodStart),
		PeriodEnd:      timeString(statement.PeriodEnd),
		PeriodSource:   statement.PeriodSource,
		GeneratedAt:    timeString(statement.GeneratedAt),
		Currency:       contractapi.Currency(statement.Currency),
		UnitLabel:      statement.UnitLabel,
		LineItems:      items,
		GrantSummaries: summaries,
		Totals: contractapi.BillingStatementTotals{
			ChargeUnits:     decimalUint64(statement.Totals.ChargeUnits),
			FreeTierUnits:   decimalUint64(statement.Totals.FreeTierUnits),
			ContractUnits:   decimalUint64(statement.Totals.ContractUnits),
			PurchaseUnits:   decimalUint64(statement.Totals.PurchaseUnits),
			PromoUnits:      decimalUint64(statement.Totals.PromoUnits),
			RefundUnits:     decimalUint64(statement.Totals.RefundUnits),
			ReceivableUnits: decimalUint64(statement.Totals.ReceivableUnits),
			ReservedUnits:   decimalUint64(statement.Totals.ReservedUnits),
			TotalDueUnits:   decimalUint64(statement.Totals.TotalDueUnits),
		},
	}
}

func contractResponse(contract billing.ContractRecord) contractapi.BillingContract {
	return contractapi.BillingContract{
		ContractID:                contractapi.ContractID(contract.ContractID),
		ProductID:                 contractapi.ProductID(contract.ProductID),
		PlanID:                    contractapi.PlanID(contract.PlanID),
		PhaseID:                   contract.PhaseID,
		CadenceKind:               contractapi.BillingCadence(contract.CadenceKind),
		Status:                    contractapi.BillingState(contract.Status),
		PaymentState:              contractapi.PaymentState(contract.PaymentState),
		EntitlementState:          contractapi.EntitlementState(contract.EntitlementState),
		PendingChangeID:           optionalTypedString[contractapi.BillingChangeID](contract.PendingChangeID),
		PendingChangeType:         optionalTypedString[contractapi.BillingState](contract.PendingChangeType),
		PendingChangeTargetPlanID: optionalTypedString[contractapi.PlanID](contract.PendingChangeTargetPlanID),
		PendingChangeEffectiveAt:  timeStringPtr(contract.PendingChangeEffectiveAt),
		StartsAt:                  timeString(contract.StartsAt),
		EndsAt:                    timeStringPtr(contract.EndsAt),
		PhaseStart:                timeStringPtr(contract.PhaseStart),
		PhaseEnd:                  timeStringPtr(contract.PhaseEnd),
	}
}

func publicPlanResponse(plan billing.PlanRecord) contractapi.BillingPlan {
	return contractapi.BillingPlan{
		PlanID:             contractapi.PlanID(plan.PlanID),
		ProductID:          contractapi.ProductID(plan.ProductID),
		DisplayName:        contractapi.DisplayName(plan.DisplayName),
		BillingMode:        contractapi.BillingMode(plan.BillingMode),
		Tier:               contractapi.BillingTier(plan.Tier),
		Currency:           contractapi.Currency(plan.Currency),
		MonthlyAmountCents: decimalUint64(plan.MonthlyAmountCents),
		AnnualAmountCents:  decimalUint64(plan.AnnualAmountCents),
		Active:             plan.Active,
		IsDefault:          plan.IsDefault,
	}
}

func reservationResponse(reservation billing.WindowReservation) internalcontractapi.BillingWindowReservation {
	return internalcontractapi.BillingWindowReservation{
		WindowID:            internalcontractapi.BillingWindowID(reservation.WindowID),
		OrgID:               internalcontractapi.OrgID(strconv.FormatUint(uint64(reservation.OrgID), 10)),
		ProductID:           internalcontractapi.ProductID(reservation.ProductID),
		PlanID:              internalcontractapi.PlanID(reservation.PlanID),
		ActorID:             internalcontractapi.ActorID(reservation.ActorID),
		SourceType:          internalcontractapi.BillingSourceType(reservation.SourceType),
		SourceRef:           internalcontractapi.BillingSourceRef(reservation.SourceRef),
		WindowSeq:           internalcontractapi.WindowSequence(reservation.WindowSeq),
		ReservationShape:    internalcontractapi.ReservationShape(reservation.ReservationShape),
		ReservedQuantity:    internalcontractapi.WindowQuantity(reservation.ReservedQuantity),
		ReservedChargeUnits: internalDecimalUint64(reservation.ReservedChargeUnits),
		PricingPhase:        internalcontractapi.PricingPhase(reservation.PricingPhase),
		Allocation:          internalcontractapi.BillingAllocation(reservation.Allocation),
		SkuRates:            internalSKURates(reservation.SKURates),
		CostPerUnit:         internalDecimalUint64(reservation.CostPerUnit),
		WindowStart:         timeString(reservation.WindowStart),
		ActivatedAt:         timeStringPtr(reservation.ActivatedAt),
		ExpiresAt:           timeString(reservation.ExpiresAt),
		RenewBy:             timeStringPtr(reservation.RenewBy),
	}
}

func settlementResponse(result billing.SettleResult) internalcontractapi.BillingSettleResult {
	return internalcontractapi.BillingSettleResult{
		WindowID:            internalcontractapi.BillingWindowID(result.WindowID),
		ActualQuantity:      internalcontractapi.WindowQuantity(result.ActualQuantity),
		BillableQuantity:    internalcontractapi.WindowQuantity(result.BillableQuantity),
		WriteoffQuantity:    internalcontractapi.WindowQuantity(result.WriteoffQuantity),
		BilledChargeUnits:   internalDecimalUint64(result.BilledChargeUnits),
		WriteoffChargeUnits: internalDecimalUint64(result.WriteoffChargeUnits),
		SettledAt:           timeString(result.SettledAt),
	}
}

func resourceNameBillingDocument(installationID string, orgID string, documentID string) string {
	return "urn:verself:" + installationID + ":billing-document:" + orgID + ":" + documentID
}

func decimalUint64(value uint64) contractapi.DecimalUint64 {
	return contractapi.DecimalUint64(strconv.FormatUint(value, 10))
}

func decimalInt64(value int64) contractapi.DecimalInt64 {
	return contractapi.DecimalInt64(strconv.FormatInt(value, 10))
}

func internalDecimalUint64(value uint64) internalcontractapi.DecimalUint64 {
	return internalcontractapi.DecimalUint64(strconv.FormatUint(value, 10))
}

func internalSKURates(input map[string]uint64) internalcontractapi.BillingSKURates {
	out := make(internalcontractapi.BillingSKURates, len(input))
	for sku, rate := range input {
		out[sku] = internalDecimalUint64(rate)
	}
	return out
}

func timeString(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func timeStringPtr(value *time.Time) *string {
	if value == nil {
		return nil
	}
	out := timeString(*value)
	return &out
}

func optionalURL(value string) *contractapi.URL {
	return optionalTypedString[contractapi.URL](value)
}

func optionalString(value string) *string {
	return optionalTypedString[string](value)
}

func optionalTypedString[T ~string](value string) *T {
	if value == "" {
		return nil
	}
	out := T(value)
	return &out
}

func billingOrgIDFromWire(id internalcontractapi.OrgID) (billing.OrgID, error) {
	parsed, err := strconv.ParseUint(string(id), 10, 64)
	if err != nil || parsed == 0 {
		return 0, badRequest("org_id must be positive")
	}
	return billing.OrgID(parsed), nil
}

func safeUint64(value internalcontractapi.SafeUint64, field string) (uint64, error) {
	if value < 0 {
		return 0, badRequest(fmt.Sprintf("%s must be non-negative", field))
	}
	return uint64(value), nil // #nosec G115 -- value is checked to be non-negative above.
}

func windowSequence(value internalcontractapi.WindowSequence, field string) (uint32, error) {
	if value < 0 || value > 2147483647 {
		return 0, badRequest(fmt.Sprintf("%s is outside uint32 bounds", field))
	}
	return uint32(value), nil // #nosec G115 -- value is checked against the Smithy range above.
}

func windowQuantity(value internalcontractapi.WindowQuantity, field string) (uint32, error) {
	if value < 0 || value > 4294967295 {
		return 0, badRequest(fmt.Sprintf("%s is outside uint32 bounds", field))
	}
	return uint32(value), nil // #nosec G115 -- value is checked against the uint32 range above.
}

func billingAllocation(input internalcontractapi.BillingAllocation) map[string]float64 {
	out := make(map[string]float64, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func billingUsageSummary(input *map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(*input))
	for key, value := range *input {
		out[key] = value
	}
	return out
}
