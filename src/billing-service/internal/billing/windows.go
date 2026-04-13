package billing

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/tigerbeetle/tigerbeetle-go/pkg/types"
	"go.opentelemetry.io/otel/trace"
)

type windowRateContext struct {
	PlanID               string                    `json:"plan_id"`
	SKURates             map[string]uint64         `json:"sku_rates"`
	SKUBuckets           map[string]string         `json:"sku_buckets"`
	SKUDetails           map[string]skuRateContext `json:"sku_details"`
	BucketDisplayNames   map[string]string         `json:"bucket_display_names"`
	ComponentCostPerUnit map[string]uint64         `json:"component_cost_per_unit"`
	BucketCostPerUnit    map[string]uint64         `json:"bucket_cost_per_unit"`
	CostPerUnit          uint64                    `json:"cost_per_unit"`
}

type persistedWindow struct {
	WindowID            string
	OrgID               OrgID
	ActorID             string
	ProductID           string
	PlanID              string
	SourceType          string
	SourceRef           string
	WindowSeq           uint32
	State               string
	ReservationShape    ReservationShape
	ReservedQuantity    uint32
	ActualQuantity      uint32
	BillableQuantity    uint32
	WriteoffQuantity    uint32
	ReservedChargeUnits uint64
	BilledChargeUnits   uint64
	WriteoffChargeUnits uint64
	PricingPhase        PricingPhase
	Allocation          map[string]float64
	RateContext         windowRateContext
	UsageSummary        map[string]any
	FundingLegs         []WindowFundingLeg
	WindowStart         time.Time
	ActivatedAt         *time.Time
	ExpiresAt           time.Time
	RenewBy             *time.Time
	SettledAt           *time.Time
	MeteringProjectedAt *time.Time
	LastProjectionError string
}

type windowRowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type productPlanConfig struct {
	PlanID        string
	BillingMode   string
	SKURates      map[string]uint64
	SKUBuckets    map[string]string
	SKUs          map[string]SKUConfig
	Buckets       map[string]BucketConfig
	ReservePolicy ReservePolicy
}

type fundingLegSettlement struct {
	Leg        WindowFundingLeg
	PostAmount uint64
	Void       bool
}

func (c *Client) ReserveWindow(ctx context.Context, req ReserveRequest) (WindowReservation, error) {
	if err := ctx.Err(); err != nil {
		return WindowReservation{}, err
	}
	if req.ProductID == "" {
		return WindowReservation{}, fmt.Errorf("product_id is required")
	}
	if req.ActorID == "" {
		return WindowReservation{}, fmt.Errorf("actor_id is required")
	}
	if req.SourceType == "" || req.SourceRef == "" {
		return WindowReservation{}, fmt.Errorf("source_type and source_ref are required")
	}
	if existing, ok, err := c.loadWindowBySourceSeq(ctx, req.ProductID, req.SourceType, req.SourceRef, req.WindowSeq); err != nil {
		return WindowReservation{}, err
	} else if ok {
		if err := validateIdempotentReservation(existing, req); err != nil {
			return WindowReservation{}, err
		}
		if err := validateReusableReservation(existing); err != nil {
			return WindowReservation{}, err
		}
		return existing.reservation(), nil
	}
	if err := c.ensureOrgNotSuspended(ctx, req.OrgID); err != nil {
		return WindowReservation{}, err
	}
	if err := c.EnsureCurrentEntitlements(ctx, req.OrgID, req.ProductID); err != nil {
		return WindowReservation{}, err
	}

	config, err := c.loadPlanConfig(ctx, req.OrgID, req.ProductID)
	if err != nil {
		return WindowReservation{}, err
	}
	if config.BillingMode != "prepaid" {
		return WindowReservation{}, ErrUnsupportedBilling
	}

	rateContext, err := computeRateBreakdown(
		req.Allocation,
		config.SKURates,
		config.SKUBuckets,
		skuRateContextFromConfig(config.SKUs),
		bucketDisplayNamesFromConfig(config.Buckets),
	)
	if err != nil {
		return WindowReservation{}, err
	}
	rateContext.PlanID = config.PlanID
	plan, err := c.pickReservationQuantityByFunding(ctx, req.OrgID, req.ProductID, config.ReservePolicy, rateContext)
	if err != nil {
		return WindowReservation{}, err
	}
	quantity := plan.Quantity
	chargeUnits := plan.TotalChargeUnits

	windowID := ulidString()
	windowSeq := req.WindowSeq
	windowStart := c.clock().UTC()
	expiresAt, renewBy := windowTiming(windowStart, config.ReservePolicy, c.cfg.PendingTimeoutSecs, quantity)

	legs, err := c.reserveGrantFunding(ctx, windowID, req.OrgID, req.ProductID, plan.ComponentCharges, rateContext.SKUBuckets)
	if err != nil {
		return WindowReservation{}, err
	}

	fundingJSON, err := json.Marshal(legs)
	if err != nil {
		_ = c.voidReservedFunding(ctx, windowID, legs)
		return WindowReservation{}, fmt.Errorf("marshal funding legs: %w", err)
	}
	allocationJSON, err := json.Marshal(cloneFloat64Map(req.Allocation))
	if err != nil {
		_ = c.voidReservedFunding(ctx, windowID, legs)
		return WindowReservation{}, fmt.Errorf("marshal allocation: %w", err)
	}
	rateContextJSON, err := json.Marshal(rateContext)
	if err != nil {
		_ = c.voidReservedFunding(ctx, windowID, legs)
		return WindowReservation{}, fmt.Errorf("marshal rate context: %w", err)
	}

	_, err = c.pg.ExecContext(ctx, `
		INSERT INTO billing_windows (
			window_id, org_id, actor_id, product_id, plan_id, source_type, source_ref, window_seq,
			state, reservation_shape, reserved_quantity, reserved_charge_units, pricing_phase,
			allocation, rate_context, funding_legs, window_start, expires_at, renew_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8,
		        'reserved', $9, $10, $11, $12,
		        $13::jsonb, $14::jsonb, $15::jsonb, $16, $17, $18)
	`, windowID, strconv.FormatUint(uint64(req.OrgID), 10), req.ActorID, req.ProductID, config.PlanID, req.SourceType, req.SourceRef, windowSeq,
		string(config.ReservePolicy.Shape), quantity, chargeUnits, string(PricingPhaseIncluded),
		string(allocationJSON), string(rateContextJSON), string(fundingJSON), windowStart, expiresAt, renewBy)
	if err != nil {
		_ = c.voidReservedFunding(ctx, windowID, legs)
		if isUniqueViolation(err) {
			existing, ok, loadErr := c.loadWindowBySourceSeq(ctx, req.ProductID, req.SourceType, req.SourceRef, req.WindowSeq)
			if loadErr != nil {
				return WindowReservation{}, loadErr
			}
			if ok {
				if validateErr := validateIdempotentReservation(existing, req); validateErr != nil {
					return WindowReservation{}, validateErr
				}
				if reusableErr := validateReusableReservation(existing); reusableErr != nil {
					return WindowReservation{}, reusableErr
				}
				return existing.reservation(), nil
			}
		}
		return WindowReservation{}, fmt.Errorf("insert billing window: %w", err)
	}

	return WindowReservation{
		WindowID:            windowID,
		OrgID:               req.OrgID,
		ProductID:           req.ProductID,
		PlanID:              config.PlanID,
		ActorID:             req.ActorID,
		SourceType:          req.SourceType,
		SourceRef:           req.SourceRef,
		WindowSeq:           windowSeq,
		ReservationShape:    config.ReservePolicy.Shape,
		ReservedQuantity:    quantity,
		ReservedChargeUnits: chargeUnits,
		PricingPhase:        PricingPhaseIncluded,
		Allocation:          cloneFloat64Map(req.Allocation),
		SKURates:            cloneUint64Map(rateContext.SKURates),
		CostPerUnit:         rateContext.CostPerUnit,
		WindowStart:         windowStart,
		ActivatedAt:         nil,
		ExpiresAt:           expiresAt,
		RenewBy:             renewBy,
	}, nil
}

func (c *Client) ActivateWindow(ctx context.Context, windowID string, activatedAt time.Time) (WindowReservation, error) {
	if err := ctx.Err(); err != nil {
		return WindowReservation{}, err
	}
	if windowID == "" {
		return WindowReservation{}, fmt.Errorf("window_id is required")
	}
	if activatedAt.IsZero() {
		activatedAt = c.clock().UTC()
	} else {
		activatedAt = activatedAt.UTC()
	}

	window, err := c.loadPersistedWindow(ctx, windowID)
	if err != nil {
		return WindowReservation{}, err
	}
	switch window.State {
	case "settled":
		return WindowReservation{}, ErrWindowAlreadySettled
	case "voided":
		return WindowReservation{}, ErrWindowAlreadyVoided
	case "reserved":
	default:
		return WindowReservation{}, ErrWindowNotReserved
	}
	if window.ActivatedAt != nil {
		return window.reservation(), nil
	}

	if !window.ExpiresAt.IsZero() && activatedAt.After(window.ExpiresAt) {
		return WindowReservation{}, ErrPendingTransferExpired
	}
	renewBy := activatedRenewBy(window, activatedAt)
	_, err = c.pg.ExecContext(ctx, `
		UPDATE billing_windows
		SET window_start = $2,
		    activated_at = $2,
		    renew_by = $3
		WHERE window_id = $1 AND state = 'reserved' AND activated_at IS NULL
	`, windowID, activatedAt, renewBy)
	if err != nil {
		return WindowReservation{}, fmt.Errorf("activate billing window: %w", err)
	}

	activated, err := c.loadPersistedWindow(ctx, windowID)
	if err != nil {
		return WindowReservation{}, err
	}
	return activated.reservation(), nil
}

func (c *Client) SettleWindow(ctx context.Context, windowID string, actualQuantity uint32, usageSummary map[string]any) (SettleResult, error) {
	window, err := c.loadPersistedWindow(ctx, windowID)
	if err != nil {
		return SettleResult{}, err
	}
	switch window.State {
	case "settled":
		return SettleResult{
			WindowID:            window.WindowID,
			ActualQuantity:      window.ActualQuantity,
			BillableQuantity:    window.BillableQuantity,
			WriteoffQuantity:    window.WriteoffQuantity,
			BilledChargeUnits:   window.BilledChargeUnits,
			WriteoffChargeUnits: window.WriteoffChargeUnits,
			SettledAt:           derefTime(window.SettledAt),
		}, nil
	case "voided":
		return SettleResult{}, ErrWindowAlreadyVoided
	case "reserved":
	default:
		return SettleResult{}, ErrWindowNotReserved
	}
	if window.ReservationShape == ReservationShapeTime && window.ActivatedAt == nil {
		return SettleResult{}, ErrWindowNotActivated
	}

	billableQuantity := minUint32(actualQuantity, window.ReservedQuantity)
	writeoffQuantity := actualQuantity - billableQuantity
	componentBilledUnits, _, billedChargeUnits, err := componentAndBucketChargeUnitsForQuantity(window, billableQuantity)
	if err != nil {
		return SettleResult{}, fmt.Errorf("billed charge units: %w", err)
	}
	rateContext, err := completeRateContext(window)
	if err != nil {
		return SettleResult{}, fmt.Errorf("load rate context: %w", err)
	}
	writeoffChargeUnits, err := safeMulUint64(rateContext.CostPerUnit, uint64(writeoffQuantity))
	if err != nil {
		return SettleResult{}, fmt.Errorf("writeoff charge units: %w", err)
	}

	settlements, err := settleFundingLegs(window.FundingLegs, componentBilledUnits)
	if err != nil {
		return SettleResult{}, fmt.Errorf("settle funding allocation: %w", err)
	}
	transfers := make([]types.Transfer, 0, len(settlements))
	for idx, settlement := range settlements {
		if idx > math.MaxUint8 {
			return SettleResult{}, fmt.Errorf("funding leg %d exceeds tigerbeetle limit", idx)
		}
		if settlement.PostAmount > 0 {
			transfers = append(transfers, types.Transfer{
				ID:        WindowTransferID(window.WindowID, uint8(idx), KindSettlement).raw,
				PendingID: settlement.Leg.TransferID.raw,
				Amount:    types.ToUint128(settlement.PostAmount),
				Ledger:    1,
				Code:      uint16(KindReservation),
				Flags:     types.TransferFlags{PostPendingTransfer: true}.ToUint16(),
			})
			continue
		}
		transfers = append(transfers, types.Transfer{
			ID:        WindowTransferID(window.WindowID, uint8(idx), KindVoid).raw,
			PendingID: settlement.Leg.TransferID.raw,
			Ledger:    1,
			Code:      uint16(KindReservation),
			Flags:     types.TransferFlags{VoidPendingTransfer: true}.ToUint16(),
		})
	}
	linkTransfers(transfers)
	if err := c.createTransfers(transfers); err != nil {
		return SettleResult{}, fmt.Errorf("settle funding legs: %w", err)
	}

	if usageSummary == nil {
		usageSummary = map[string]any{}
	}
	usageSummaryJSON, err := json.Marshal(copyJSONMap(usageSummary))
	if err != nil {
		return SettleResult{}, fmt.Errorf("marshal usage summary: %w", err)
	}
	settledAt := c.clock().UTC()
	_, err = c.pg.ExecContext(ctx, `
		UPDATE billing_windows
		SET state = 'settled',
		    actual_quantity = $2,
		    billable_quantity = $3,
		    writeoff_quantity = $4,
		    billed_charge_units = $5,
		    writeoff_charge_units = $6,
		    usage_summary = $7::jsonb,
		    settled_at = $8,
		    metering_projected_at = NULL,
		    last_projection_error = ''
		WHERE window_id = $1 AND state = 'reserved'
	`, window.WindowID, actualQuantity, billableQuantity, writeoffQuantity, billedChargeUnits, writeoffChargeUnits, string(usageSummaryJSON), settledAt)
	if err != nil {
		return SettleResult{}, fmt.Errorf("update settled window: %w", err)
	}

	return SettleResult{
		WindowID:            window.WindowID,
		ActualQuantity:      actualQuantity,
		BillableQuantity:    billableQuantity,
		WriteoffQuantity:    writeoffQuantity,
		BilledChargeUnits:   billedChargeUnits,
		WriteoffChargeUnits: writeoffChargeUnits,
		SettledAt:           settledAt,
	}, nil
}

func (c *Client) VoidWindow(ctx context.Context, windowID string) error {
	window, err := c.loadPersistedWindow(ctx, windowID)
	if err != nil {
		return err
	}
	switch window.State {
	case "voided":
		return nil
	case "settled":
		return ErrWindowAlreadySettled
	case "reserved":
	default:
		return ErrWindowNotReserved
	}
	if err := c.voidReservedFunding(ctx, windowID, window.FundingLegs); err != nil {
		return err
	}
	_, err = c.pg.ExecContext(ctx, `
		UPDATE billing_windows
		SET state = 'voided', settled_at = $2
		WHERE window_id = $1
	`, windowID, c.clock().UTC())
	if err != nil {
		return fmt.Errorf("mark window voided: %w", err)
	}
	return nil
}

func (c *Client) ProjectPendingWindows(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	projected := 0
	for projected < limit {
		tx, err := c.pg.BeginTx(ctx, nil)
		if err != nil {
			return projected, fmt.Errorf("begin projection transaction: %w", err)
		}
		var windowID string
		err = tx.QueryRowContext(ctx, `
			SELECT window_id
			FROM billing_windows
			WHERE state = 'settled' AND metering_projected_at IS NULL
			ORDER BY (last_projection_error <> ''), created_at
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		`).Scan(&windowID)
		if err == sql.ErrNoRows {
			if commitErr := tx.Commit(); commitErr != nil {
				return projected, fmt.Errorf("commit empty projection transaction: %w", commitErr)
			}
			return projected, nil
		}
		if err != nil {
			_ = tx.Rollback()
			return projected, fmt.Errorf("claim pending projection: %w", err)
		}

		window, err := c.loadPersistedWindowFrom(ctx, tx, windowID)
		if err != nil {
			_ = tx.Rollback()
			return projected, err
		}
		row, err := buildMeteringRow(window)
		if err != nil {
			_ = tx.Rollback()
			return projected, err
		}
		if err := c.metering.InsertMeteringRow(ctx, row); err != nil {
			_, _ = tx.ExecContext(ctx, `
				UPDATE billing_windows
				SET last_projection_error = $2
				WHERE window_id = $1
			`, windowID, err.Error())
			if commitErr := tx.Commit(); commitErr != nil {
				return projected, fmt.Errorf("commit projection failure marker: %w", commitErr)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE billing_windows
			SET metering_projected_at = $2,
			    last_projection_error = ''
			WHERE window_id = $1
		`, windowID, c.clock().UTC()); err != nil {
			_ = tx.Rollback()
			return projected, fmt.Errorf("mark projected window: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return projected, fmt.Errorf("commit projected window: %w", err)
		}
		projected++
	}
	return projected, nil
}

func buildMeteringRow(window persistedWindow) (MeteringRow, error) {
	startedAt := window.WindowStart
	if window.ActivatedAt != nil {
		startedAt = window.ActivatedAt.UTC()
	}
	endedAt := startedAt
	switch window.ReservationShape {
	case ReservationShapeTime:
		endedAt = startedAt.Add(time.Duration(window.ActualQuantity) * time.Second)
	case ReservationShapeUnits:
		if window.SettledAt != nil {
			endedAt = *window.SettledAt
		}
	}
	rateContext, err := completeRateContext(window)
	if err != nil {
		return MeteringRow{}, err
	}

	row := MeteringRow{
		WindowID:                   window.WindowID,
		OrgID:                      strconv.FormatUint(uint64(window.OrgID), 10),
		ActorID:                    window.ActorID,
		ProductID:                  window.ProductID,
		SourceType:                 window.SourceType,
		SourceRef:                  window.SourceRef,
		WindowSeq:                  window.WindowSeq,
		ReservationShape:           string(window.ReservationShape),
		StartedAt:                  startedAt,
		EndedAt:                    endedAt,
		ReservedQuantity:           uint64(window.ReservedQuantity),
		ActualQuantity:             uint64(window.ActualQuantity),
		BillableQuantity:           uint64(window.BillableQuantity),
		WriteoffQuantity:           uint64(window.WriteoffQuantity),
		PricingPhase:               string(window.PricingPhase),
		Dimensions:                 cloneFloat64Map(window.Allocation),
		ComponentQuantities:        componentQuantitiesForQuantity(window.Allocation, window.BillableQuantity),
		ComponentChargeUnits:       map[string]uint64{},
		BucketChargeUnits:          map[string]uint64{},
		ChargeUnits:                window.BilledChargeUnits,
		WriteoffChargeUnits:        window.WriteoffChargeUnits,
		ComponentFreeTierUnits:     map[string]uint64{},
		ComponentSubscriptionUnits: map[string]uint64{},
		ComponentPurchaseUnits:     map[string]uint64{},
		ComponentPromoUnits:        map[string]uint64{},
		ComponentRefundUnits:       map[string]uint64{},
		ComponentReceivableUnits:   map[string]uint64{},
		UsageEvidence:              map[string]uint64{},
		PlanID:                     window.PlanID,
		CostPerUnit:                rateContext.CostPerUnit,
		RecordedAt:                 time.Now().UTC(),
	}
	usageEvidence, err := usageEvidenceFromSummary(window.UsageSummary)
	if err != nil {
		return MeteringRow{}, err
	}
	row.UsageEvidence = usageEvidence
	componentCharges, bucketCharges, _, err := componentAndBucketChargeUnitsForQuantity(window, window.BillableQuantity)
	if err != nil {
		return MeteringRow{}, err
	}
	row.ComponentChargeUnits = componentCharges
	row.BucketChargeUnits = bucketCharges
	settlements, err := settleFundingLegs(window.FundingLegs, componentCharges)
	if err != nil {
		return MeteringRow{}, err
	}
	for _, settlement := range settlements {
		leg := settlement.Leg
		amount := settlement.PostAmount
		if amount == 0 {
			continue
		}
		if leg.ChargeSKUID == "" {
			return MeteringRow{}, fmt.Errorf("funding leg %s missing charge_sku_id", leg.GrantID)
		}
		switch leg.Source {
		case SourceFreeTier:
			row.FreeTierUnits += amount
			if err := addMapUint64(row.ComponentFreeTierUnits, leg.ChargeSKUID, amount); err != nil {
				return MeteringRow{}, err
			}
		case SourceSubscription:
			row.SubscriptionUnits += amount
			if err := addMapUint64(row.ComponentSubscriptionUnits, leg.ChargeSKUID, amount); err != nil {
				return MeteringRow{}, err
			}
		case SourcePurchase:
			row.PurchaseUnits += amount
			if err := addMapUint64(row.ComponentPurchaseUnits, leg.ChargeSKUID, amount); err != nil {
				return MeteringRow{}, err
			}
		case SourcePromo:
			row.PromoUnits += amount
			if err := addMapUint64(row.ComponentPromoUnits, leg.ChargeSKUID, amount); err != nil {
				return MeteringRow{}, err
			}
		case SourceRefund:
			row.RefundUnits += amount
			if err := addMapUint64(row.ComponentRefundUnits, leg.ChargeSKUID, amount); err != nil {
				return MeteringRow{}, err
			}
		}
	}
	return row, nil
}

func (c *Client) loadPlanConfig(ctx context.Context, orgID OrgID, productID string) (productPlanConfig, error) {
	var (
		planID            string
		billingMode       string
		reservePolicyJSON []byte
	)

	err := c.pg.QueryRowContext(ctx, `
		WITH active_subscription AS (
			SELECT plan_id
			FROM subscription_contracts
			WHERE org_id = $1
			  AND product_id = $2
			  AND status NOT IN ('canceled', 'suspended')
			  AND entitlement_state IN ('active', 'grace')
			ORDER BY current_period_end DESC NULLS LAST, subscription_id DESC
			LIMIT 1
		)
		SELECT p.plan_id, p.billing_mode, pr.reserve_policy::text
		FROM plans p
		JOIN products pr ON pr.product_id = p.product_id
		LEFT JOIN active_subscription s ON s.plan_id = p.plan_id
		WHERE p.product_id = $2
		  AND p.active
		  AND (
			s.plan_id IS NOT NULL
			OR (NOT EXISTS (SELECT 1 FROM active_subscription) AND p.is_default)
		  )
		ORDER BY (s.plan_id IS NOT NULL) DESC
		LIMIT 1
	`, strconv.FormatUint(uint64(orgID), 10), productID).Scan(&planID, &billingMode, &reservePolicyJSON)
	if err == sql.ErrNoRows {
		return productPlanConfig{}, ErrNoDefaultPlan
	}
	if err != nil {
		return productPlanConfig{}, fmt.Errorf("load plan config: %w", err)
	}

	skus, buckets, err := c.loadPlanSKUConfig(ctx, planID)
	if err == sql.ErrNoRows {
		return productPlanConfig{}, fmt.Errorf("plan %s has no active sku rates", planID)
	}
	if err != nil {
		return productPlanConfig{}, err
	}
	var reservePolicy ReservePolicy
	if err := json.Unmarshal(reservePolicyJSON, &reservePolicy); err != nil {
		return productPlanConfig{}, fmt.Errorf("decode reserve policy: %w", err)
	}
	if reservePolicy.Shape == "" {
		return productPlanConfig{}, fmt.Errorf("reserve policy shape is required")
	}
	if reservePolicy.TargetQuantity == 0 {
		return productPlanConfig{}, fmt.Errorf("reserve policy target_quantity is required")
	}
	if reservePolicy.MinQuantity == 0 {
		reservePolicy.MinQuantity = reservePolicy.TargetQuantity
	}
	return productPlanConfig{
		PlanID:        planID,
		BillingMode:   billingMode,
		SKURates:      skuRatesFromSKUConfig(skus),
		SKUBuckets:    skuBucketsFromSKUConfig(skus),
		SKUs:          skus,
		Buckets:       buckets,
		ReservePolicy: reservePolicy,
	}, nil
}

// reservationPlan is the output of the reserve-quantity picker: the quantity
// the funder committed to, the total ledger-units that represent, and both the
// per-SKU and per-bucket breakdowns. reserveGrantFunding consumes ComponentCharges
// to build SKU-aware chargeLines; settlement consumes the same shape at
// billable-quantity time.
type reservationPlan struct {
	Quantity         uint32
	TotalChargeUnits uint64
	ComponentCharges map[string]uint64 // sku_id → charge units
	BucketCharges    map[string]uint64 // bucket_id → charge units
}

func (c *Client) pickReservationQuantityByFunding(
	ctx context.Context,
	orgID OrgID,
	productID string,
	policy ReservePolicy,
	rateContext windowRateContext,
) (reservationPlan, error) {
	grants, err := c.listScopedGrantBalancesForFunding(ctx, orgID, productID)
	if err != nil {
		return reservationPlan{}, err
	}
	return pickReservationQuantity(productID, policy, rateContext, grants)
}

func pickReservationQuantity(
	productID string,
	policy ReservePolicy,
	rateContext windowRateContext,
	grants []scopedGrantBalance,
) (reservationPlan, error) {
	if policy.TargetQuantity == 0 {
		return reservationPlan{}, fmt.Errorf("reserve policy target_quantity is required")
	}
	minQuantity := policy.MinQuantity
	if minQuantity == 0 {
		minQuantity = policy.TargetQuantity
	}

	if !policy.AllowPartialReserve {
		return fundedReservationPlan(productID, policy.TargetQuantity, rateContext, grants)
	}

	var bestQuantity uint32
	low := minQuantity
	high := policy.TargetQuantity
	for low <= high {
		mid := low + (high-low)/2
		_, err := fundedReservationPlan(productID, mid, rateContext, grants)
		switch {
		case err == nil:
			bestQuantity = mid
			low = mid + 1
		case errors.Is(err, ErrInsufficientBalance):
			if mid == 0 {
				high = 0
				continue
			}
			high = mid - 1
		default:
			return reservationPlan{}, err
		}
	}
	if bestQuantity < minQuantity {
		return reservationPlan{}, ErrInsufficientBalance
	}
	return fundedReservationPlan(productID, bestQuantity, rateContext, grants)
}

func fundedReservationPlan(productID string, quantity uint32, rateContext windowRateContext, grants []scopedGrantBalance) (reservationPlan, error) {
	componentCharges, bucketCharges, totalChargeUnits, err := chargeUnitsForQuantity(rateContext, quantity)
	if err != nil {
		return reservationPlan{}, err
	}
	lines, err := componentChargeLines(componentCharges, rateContext.SKUBuckets)
	if err != nil {
		return reservationPlan{}, err
	}
	if _, err := planGrantFunding(productID, lines, grants); err != nil {
		return reservationPlan{}, err
	}
	return reservationPlan{
		Quantity:         quantity,
		TotalChargeUnits: totalChargeUnits,
		ComponentCharges: componentCharges,
		BucketCharges:    bucketCharges,
	}, nil
}

// componentChargeLines lifts a sku→amount map into []chargeLine, resolving
// the bucket from rateContext.SKUBuckets. The reserve and settle paths use
// these lines so every resulting funding leg carries ChargeSKUID, which
// downstream (buildMeteringRow, statement.go) keys per-SKU drain attribution
// on.
func componentChargeLines(componentCharges map[string]uint64, skuBuckets map[string]string) ([]chargeLine, error) {
	if len(componentCharges) == 0 {
		return nil, nil
	}
	out := make([]chargeLine, 0, len(componentCharges))
	for _, skuID := range sortedUint64MapKeys(componentCharges) {
		amount := componentCharges[skuID]
		if amount == 0 {
			continue
		}
		bucketID, ok := skuBuckets[skuID]
		if !ok || bucketID == "" {
			return nil, fmt.Errorf("sku %s missing bucket mapping in rate context", skuID)
		}
		out = append(out, chargeLine{BucketID: bucketID, SKUID: skuID, AmountUnits: amount})
	}
	return out, nil
}

func (c *Client) reserveGrantFunding(
	ctx context.Context,
	windowID string,
	orgID OrgID,
	productID string,
	componentCharges map[string]uint64,
	skuBuckets map[string]string,
) ([]WindowFundingLeg, error) {
	grants, err := c.listScopedGrantBalancesForFunding(ctx, orgID, productID)
	if err != nil {
		return nil, err
	}
	lines, err := componentChargeLines(componentCharges, skuBuckets)
	if err != nil {
		return nil, err
	}
	planned, err := planGrantFunding(productID, lines, grants)
	if err != nil {
		return nil, err
	}

	legs := make([]WindowFundingLeg, 0, len(planned))
	transfers := make([]types.Transfer, 0, len(planned))
	for _, plannedLeg := range planned {
		if len(legs) > math.MaxUint8 {
			return nil, fmt.Errorf("funding legs exceed tigerbeetle transfer id limit")
		}
		transferID := WindowTransferID(windowID, uint8(len(legs)), KindReservation)
		legs = append(legs, WindowFundingLeg{
			GrantID:             plannedLeg.GrantID,
			TransferID:          transferID,
			ChargeProductID:     plannedLeg.ChargeProductID,
			ChargeBucketID:      plannedLeg.ChargeBucketID,
			ChargeSKUID:         plannedLeg.ChargeSKUID,
			Amount:              plannedLeg.AmountUnits,
			Source:              plannedLeg.Source,
			GrantScopeType:      plannedLeg.GrantScopeType,
			GrantScopeProductID: plannedLeg.GrantScopeProductID,
			GrantScopeBucketID:  plannedLeg.GrantScopeBucketID,
			GrantScopeSKUID:     plannedLeg.GrantScopeSKUID,
		})
		transfers = append(transfers, types.Transfer{
			ID:              transferID.raw,
			DebitAccountID:  GrantAccountID(plannedLeg.GrantID).raw,
			CreditAccountID: OperatorAccountID(AcctRevenue).raw,
			Amount:          types.ToUint128(plannedLeg.AmountUnits),
			Timeout:         c.cfg.PendingTimeoutSecs,
			Ledger:          1,
			Code:            uint16(KindReservation),
			Flags:           types.TransferFlags{Pending: true}.ToUint16(),
		})
	}
	linkTransfers(transfers)
	if err := c.createTransfers(transfers); err != nil {
		return nil, err
	}
	return legs, nil
}

// settleFundingLegs walks reserved legs in stored order and drains each up to
// its originating SKU's billable remainder. Legs whose ChargeSKUID exhausted
// before the leg became reachable are marked Void so the reserve-time
// TigerBeetle pending transfer is released without posting. The stored leg
// order is the funder's scope precedence (SKU → bucket → product → account)
// followed by source precedence; preserving it at settle time is how the
// invoice shows the same ordering the customer saw at reservation.
func settleFundingLegs(
	legs []WindowFundingLeg,
	billedBySKU map[string]uint64,
) ([]fundingLegSettlement, error) {
	remaining := cloneUint64Map(billedBySKU)
	if remaining == nil {
		remaining = map[string]uint64{}
	}
	actions := make([]fundingLegSettlement, 0, len(legs))
	for _, leg := range legs {
		if leg.ChargeSKUID == "" {
			return nil, fmt.Errorf("funding leg %s missing charge_sku_id", leg.GrantID)
		}
		postAmount := minUint64(leg.Amount, remaining[leg.ChargeSKUID])
		if postAmount > 0 {
			remaining[leg.ChargeSKUID] -= postAmount
		}
		actions = append(actions, fundingLegSettlement{
			Leg:        leg,
			PostAmount: postAmount,
			Void:       postAmount == 0,
		})
	}
	for _, skuID := range sortedUint64MapKeys(remaining) {
		if remaining[skuID] != 0 {
			return nil, fmt.Errorf("sku %s has %d unfunded billed charge units", skuID, remaining[skuID])
		}
	}
	return actions, nil
}

func (c *Client) voidReservedFunding(ctx context.Context, windowID string, legs []WindowFundingLeg) error {
	transfers := make([]types.Transfer, 0, len(legs))
	for idx, leg := range legs {
		if idx > math.MaxUint8 {
			return fmt.Errorf("funding leg %d exceeds tigerbeetle transfer id limit", idx)
		}
		transfers = append(transfers, types.Transfer{
			ID:        WindowTransferID(windowID, uint8(idx), KindVoid).raw,
			PendingID: leg.TransferID.raw,
			Ledger:    1,
			Code:      uint16(KindReservation),
			Flags:     types.TransferFlags{VoidPendingTransfer: true}.ToUint16(),
		})
	}
	linkTransfers(transfers)
	if err := c.createTransfers(transfers); err != nil {
		return fmt.Errorf("void reserved funding: %w", err)
	}
	return nil
}

func (c *Client) loadPersistedWindow(ctx context.Context, windowID string) (persistedWindow, error) {
	return c.loadPersistedWindowFrom(ctx, c.pg, windowID)
}

func (c *Client) loadPersistedWindowFrom(ctx context.Context, q windowRowQueryer, windowID string) (persistedWindow, error) {
	var (
		orgIDText            string
		reservationShapeText string
		pricingPhaseText     string
		allocationJSON       []byte
		rateContextJSON      []byte
		usageSummaryJSON     []byte
		fundingLegsJSON      []byte
		activatedAt          sql.NullTime
		renewBy              sql.NullTime
		settledAt            sql.NullTime
		projectedAt          sql.NullTime
	)

	var row persistedWindow
	err := q.QueryRowContext(ctx, `
		SELECT
			window_id, org_id, actor_id, product_id, plan_id, source_type, source_ref, window_seq,
			state, reservation_shape, reserved_quantity, actual_quantity, billable_quantity, writeoff_quantity,
			reserved_charge_units, billed_charge_units, writeoff_charge_units, pricing_phase,
			allocation::text, rate_context::text, usage_summary::text, funding_legs::text,
			window_start, activated_at, expires_at, renew_by, settled_at, metering_projected_at, last_projection_error
		FROM billing_windows
		WHERE window_id = $1
	`, windowID).Scan(
		&row.WindowID,
		&orgIDText,
		&row.ActorID,
		&row.ProductID,
		&row.PlanID,
		&row.SourceType,
		&row.SourceRef,
		&row.WindowSeq,
		&row.State,
		&reservationShapeText,
		&row.ReservedQuantity,
		&row.ActualQuantity,
		&row.BillableQuantity,
		&row.WriteoffQuantity,
		&row.ReservedChargeUnits,
		&row.BilledChargeUnits,
		&row.WriteoffChargeUnits,
		&pricingPhaseText,
		&allocationJSON,
		&rateContextJSON,
		&usageSummaryJSON,
		&fundingLegsJSON,
		&row.WindowStart,
		&activatedAt,
		&row.ExpiresAt,
		&renewBy,
		&settledAt,
		&projectedAt,
		&row.LastProjectionError,
	)
	if err == sql.ErrNoRows {
		return persistedWindow{}, ErrWindowNotFound
	}
	if err != nil {
		return persistedWindow{}, fmt.Errorf("load billing window: %w", err)
	}

	orgID, err := strconv.ParseUint(orgIDText, 10, 64)
	if err != nil {
		return persistedWindow{}, fmt.Errorf("parse window org id: %w", err)
	}
	row.OrgID = OrgID(orgID)
	row.ReservationShape = ReservationShape(reservationShapeText)
	row.PricingPhase = PricingPhase(pricingPhaseText)
	if err := json.Unmarshal(allocationJSON, &row.Allocation); err != nil {
		return persistedWindow{}, fmt.Errorf("decode allocation: %w", err)
	}
	if err := json.Unmarshal(rateContextJSON, &row.RateContext); err != nil {
		return persistedWindow{}, fmt.Errorf("decode rate context: %w", err)
	}
	usageSummary, err := decodeUsageSummary(usageSummaryJSON)
	if err != nil {
		return persistedWindow{}, fmt.Errorf("decode usage summary: %w", err)
	}
	row.UsageSummary = usageSummary
	if err := json.Unmarshal(fundingLegsJSON, &row.FundingLegs); err != nil {
		return persistedWindow{}, fmt.Errorf("decode funding legs: %w", err)
	}
	if activatedAt.Valid {
		value := activatedAt.Time.UTC()
		row.ActivatedAt = &value
	}
	if renewBy.Valid {
		value := renewBy.Time.UTC()
		row.RenewBy = &value
	}
	if settledAt.Valid {
		value := settledAt.Time.UTC()
		row.SettledAt = &value
	}
	if projectedAt.Valid {
		value := projectedAt.Time.UTC()
		row.MeteringProjectedAt = &value
	}
	return row, nil
}

func (w persistedWindow) reservation() WindowReservation {
	return WindowReservation{
		WindowID:            w.WindowID,
		OrgID:               w.OrgID,
		ProductID:           w.ProductID,
		PlanID:              w.PlanID,
		ActorID:             w.ActorID,
		SourceType:          w.SourceType,
		SourceRef:           w.SourceRef,
		WindowSeq:           w.WindowSeq,
		ReservationShape:    w.ReservationShape,
		ReservedQuantity:    w.ReservedQuantity,
		ReservedChargeUnits: w.ReservedChargeUnits,
		PricingPhase:        w.PricingPhase,
		Allocation:          cloneFloat64Map(w.Allocation),
		SKURates:            cloneUint64Map(w.RateContext.SKURates),
		CostPerUnit:         w.RateContext.CostPerUnit,
		WindowStart:         w.WindowStart.UTC(),
		ActivatedAt:         cloneTimePtr(w.ActivatedAt),
		ExpiresAt:           w.ExpiresAt.UTC(),
		RenewBy:             cloneTimePtr(w.RenewBy),
	}
}

func (c *Client) loadWindowBySourceSeq(ctx context.Context, productID, sourceType, sourceRef string, windowSeq uint32) (persistedWindow, bool, error) {
	var windowID string
	err := c.pg.QueryRowContext(ctx, `
		SELECT window_id
		FROM billing_windows
		WHERE product_id = $1
		  AND source_type = $2
		  AND source_ref = $3
		  AND window_seq = $4
	`, productID, sourceType, sourceRef, windowSeq).Scan(&windowID)
	if errors.Is(err, sql.ErrNoRows) {
		return persistedWindow{}, false, nil
	}
	if err != nil {
		return persistedWindow{}, false, fmt.Errorf("load billing window by source seq: %w", err)
	}
	window, err := c.loadPersistedWindow(ctx, windowID)
	if err != nil {
		return persistedWindow{}, false, err
	}
	return window, true, nil
}

func validateIdempotentReservation(existing persistedWindow, req ReserveRequest) error {
	if existing.OrgID != req.OrgID {
		return fmt.Errorf("idempotent billing reserve org mismatch for %s/%s/%s/%d", req.ProductID, req.SourceType, req.SourceRef, req.WindowSeq)
	}
	if existing.ActorID != req.ActorID {
		return fmt.Errorf("idempotent billing reserve actor mismatch for %s/%s/%s/%d", req.ProductID, req.SourceType, req.SourceRef, req.WindowSeq)
	}
	if !float64MapsEqual(existing.Allocation, req.Allocation) {
		return fmt.Errorf("idempotent billing reserve allocation mismatch for %s/%s/%s/%d", req.ProductID, req.SourceType, req.SourceRef, req.WindowSeq)
	}
	return nil
}

func validateReusableReservation(existing persistedWindow) error {
	switch existing.State {
	case "reserved":
		return nil
	case "settled":
		return ErrWindowAlreadySettled
	case "voided":
		return ErrWindowAlreadyVoided
	default:
		return fmt.Errorf("%w: %s", ErrWindowNotReserved, existing.State)
	}
}

func float64MapsEqual(a, b map[string]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for key, av := range a {
		if bv, ok := b[key]; !ok || bv != av {
			return false
		}
	}
	return true
}

func (c *Client) ensureOrgNotSuspended(ctx context.Context, orgID OrgID) error {
	var suspended bool
	if err := c.pg.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM subscription_contracts WHERE org_id = $1 AND status = 'suspended'
		)
	`, strconv.FormatUint(uint64(orgID), 10)).Scan(&suspended); err != nil {
		return fmt.Errorf("check org suspension: %w", err)
	}
	if suspended {
		return ErrOrgSuspended
	}
	return nil
}

func windowTiming(start time.Time, policy ReservePolicy, pendingTimeoutSecs uint32, quantity uint32) (time.Time, *time.Time) {
	expiresAt := start.Add(time.Duration(pendingTimeoutSecs) * time.Second)
	if policy.Shape != ReservationShapeTime {
		return expiresAt, nil
	}
	windowDuration := time.Duration(quantity) * time.Second
	renewAt := start.Add(windowDuration)
	if slack := time.Duration(policy.RenewSlackQuantity) * time.Second; slack > 0 {
		renewAt = renewAt.Add(-slack)
		if renewAt.Before(start) {
			renewAt = start
		}
	}
	return expiresAt, &renewAt
}

func activatedRenewBy(window persistedWindow, activatedAt time.Time) *time.Time {
	if window.ReservationShape != ReservationShapeTime {
		return nil
	}
	windowDuration := time.Duration(window.ReservedQuantity) * time.Second
	renewAt := activatedAt.Add(windowDuration)
	if window.RenewBy != nil {
		oldEnd := window.WindowStart.Add(windowDuration)
		if slack := oldEnd.Sub(*window.RenewBy); slack > 0 {
			renewAt = renewAt.Add(-slack)
			if renewAt.Before(activatedAt) {
				renewAt = activatedAt
			}
		}
	}
	return &renewAt
}

func computeRateBreakdown(
	allocation map[string]float64,
	skuRates map[string]uint64,
	skuBuckets map[string]string,
	skuDetails map[string]skuRateContext,
	bucketDisplayNames map[string]string,
) (windowRateContext, error) {
	breakdown := windowRateContext{
		SKURates:             cloneUint64Map(skuRates),
		SKUBuckets:           cloneStringMap(skuBuckets),
		SKUDetails:           cloneSKURateContextMap(skuDetails),
		BucketDisplayNames:   cloneStringMap(bucketDisplayNames),
		ComponentCostPerUnit: make(map[string]uint64, len(allocation)),
		BucketCostPerUnit:    map[string]uint64{},
	}
	for _, skuID := range sortedFloat64MapKeys(allocation) {
		quantity := allocation[skuID]
		if quantity < 0 {
			return windowRateContext{}, fmt.Errorf("allocation %s must be non-negative", skuID)
		}
		rate, ok := skuRates[skuID]
		if !ok {
			return windowRateContext{}, fmt.Errorf("%w: %s", ErrDimensionMismatch, skuID)
		}
		sku, ok := skuDetails[skuID]
		if !ok || sku.DisplayName == "" || sku.BucketID == "" || sku.BucketDisplayName == "" || sku.QuantityUnit == "" {
			return windowRateContext{}, fmt.Errorf("sku metadata missing for %s", skuID)
		}
		if sku.UnitRate != 0 && sku.UnitRate != rate {
			return windowRateContext{}, fmt.Errorf("sku metadata rate mismatch for %s", skuID)
		}
		rawComponentCost := quantity * float64(rate)
		rounded := math.Round(rawComponentCost)
		if math.Abs(rawComponentCost-rounded) > 1e-9 {
			return windowRateContext{}, fmt.Errorf("non-integral component cost_per_unit %s %.9f", skuID, rawComponentCost)
		}
		if rounded < 0 || rounded > float64(^uint64(0)) {
			return windowRateContext{}, fmt.Errorf("component cost_per_unit %s overflows uint64", skuID)
		}
		componentCost := uint64(rounded)
		breakdown.ComponentCostPerUnit[skuID] = componentCost
		if err := addMapUint64(breakdown.BucketCostPerUnit, sku.BucketID, componentCost); err != nil {
			return windowRateContext{}, fmt.Errorf("add bucket cost %s: %w", sku.BucketID, err)
		}
		var err error
		breakdown.CostPerUnit, err = safeAddUint64(breakdown.CostPerUnit, componentCost)
		if err != nil {
			return windowRateContext{}, err
		}
	}
	return breakdown, nil
}

// componentAndBucketChargeUnitsForQuantity returns the per-SKU and per-bucket
// charge-unit maps for the given billable quantity, plus the grand total.
// Callers use the per-SKU map as the settlement key (legs carry ChargeSKUID)
// and the per-bucket map strictly for analytics / dashboards that still
// aggregate at the bucket axis.
func componentAndBucketChargeUnitsForQuantity(window persistedWindow, quantity uint32) (map[string]uint64, map[string]uint64, uint64, error) {
	rateContext, err := completeRateContext(window)
	if err != nil {
		return nil, nil, 0, err
	}
	return chargeUnitsForQuantity(rateContext, quantity)
}

func chargeUnitsForQuantity(rateContext windowRateContext, quantity uint32) (map[string]uint64, map[string]uint64, uint64, error) {
	componentCharges := make(map[string]uint64, len(rateContext.ComponentCostPerUnit))
	for _, componentID := range sortedUint64MapKeys(rateContext.ComponentCostPerUnit) {
		chargeUnits, err := safeMulUint64(uint64(quantity), rateContext.ComponentCostPerUnit[componentID])
		if err != nil {
			return nil, nil, 0, err
		}
		if chargeUnits != 0 {
			componentCharges[componentID] = chargeUnits
		}
	}
	bucketCharges := make(map[string]uint64, len(rateContext.BucketCostPerUnit))
	var totalChargeUnits uint64
	for _, bucketID := range sortedUint64MapKeys(rateContext.BucketCostPerUnit) {
		chargeUnits, err := safeMulUint64(uint64(quantity), rateContext.BucketCostPerUnit[bucketID])
		if err != nil {
			return nil, nil, 0, err
		}
		if chargeUnits == 0 {
			continue
		}
		bucketCharges[bucketID] = chargeUnits
		totalChargeUnits, err = safeAddUint64(totalChargeUnits, chargeUnits)
		if err != nil {
			return nil, nil, 0, err
		}
	}
	return componentCharges, bucketCharges, totalChargeUnits, nil
}

func usageEvidenceFromSummary(summary map[string]any) (map[string]uint64, error) {
	if len(summary) == 0 {
		return map[string]uint64{}, nil
	}
	out := make(map[string]uint64, len(summary))
	for _, key := range sortedAnyMapKeys(summary) {
		value, ok, err := usageEvidenceUint64(summary[key])
		if err != nil {
			return nil, fmt.Errorf("usage evidence %s: %w", key, err)
		}
		if ok {
			out[key] = value
		}
	}
	return out, nil
}

func usageEvidenceUint64(value any) (uint64, bool, error) {
	switch v := value.(type) {
	case nil:
		return 0, false, nil
	case uint64:
		return v, true, nil
	case uint:
		return uint64(v), true, nil
	case int:
		if v < 0 {
			return 0, false, fmt.Errorf("must be non-negative")
		}
		return uint64(v), true, nil
	case int64:
		if v < 0 {
			return 0, false, fmt.Errorf("must be non-negative")
		}
		return uint64(v), true, nil
	case float64:
		if v < 0 || math.Trunc(v) != v || v > float64(^uint64(0)) {
			return 0, false, fmt.Errorf("must be an unsigned integer")
		}
		return uint64(v), true, nil
	case json.Number:
		raw := v.String()
		if parsed, err := strconv.ParseUint(raw, 10, 64); err == nil {
			return parsed, true, nil
		}
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
			if parsed < 0 {
				return 0, false, fmt.Errorf("must be non-negative")
			}
			return uint64(parsed), true, nil
		}
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return 0, false, err
		}
		if parsed < 0 || math.Trunc(parsed) != parsed || parsed > float64(^uint64(0)) {
			return 0, false, fmt.Errorf("must be an unsigned integer")
		}
		return uint64(parsed), true, nil
	default:
		return 0, false, fmt.Errorf("unsupported type %T", value)
	}
}

func decodeUsageSummary(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	// UseNumber avoids corrupting byte counters that exceed float64 integer precision.
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		return nil, err
	}
	if out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}

func cloneSKURateContextMap(in map[string]skuRateContext) map[string]skuRateContext {
	if len(in) == 0 {
		return map[string]skuRateContext{}
	}
	out := make(map[string]skuRateContext, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func completeRateContext(window persistedWindow) (windowRateContext, error) {
	rateContext := window.RateContext
	if rateContext.PlanID == "" {
		rateContext.PlanID = window.PlanID
	}
	if len(rateContext.ComponentCostPerUnit) > 0 && len(rateContext.BucketCostPerUnit) > 0 {
		return rateContext, nil
	}
	if len(rateContext.SKURates) > 0 {
		breakdown, err := computeRateBreakdown(
			window.Allocation,
			rateContext.SKURates,
			rateContext.SKUBuckets,
			rateContext.SKUDetails,
			rateContext.BucketDisplayNames,
		)
		if err != nil {
			return windowRateContext{}, err
		}
		breakdown.PlanID = rateContext.PlanID
		return breakdown, nil
	}
	if rateContext.CostPerUnit == 0 {
		rateContext.ComponentCostPerUnit = map[string]uint64{}
		rateContext.BucketCostPerUnit = map[string]uint64{}
		return rateContext, nil
	}
	return windowRateContext{}, fmt.Errorf("rate context for window %s has cost_per_unit without sku allocation", window.WindowID)
}

func componentQuantitiesForQuantity(allocation map[string]float64, quantity uint32) map[string]float64 {
	if len(allocation) == 0 {
		return nil
	}
	out := make(map[string]float64, len(allocation))
	for key, value := range allocation {
		out[key] = value * float64(quantity)
	}
	return out
}

func addMapUint64(out map[string]uint64, key string, amount uint64) error {
	if key == "" {
		return fmt.Errorf("map key is required")
	}
	if amount == 0 {
		return nil
	}
	sum, err := safeAddUint64(out[key], amount)
	if err != nil {
		return err
	}
	out[key] = sum
	return nil
}

func sortedUint64MapKeys(values map[string]uint64) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedFloat64MapKeys(values map[string]float64) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedAnyMapKeys(values map[string]any) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func linkTransfers(transfers []types.Transfer) {
	if len(transfers) < 2 {
		return
	}
	for idx := 0; idx < len(transfers)-1; idx++ {
		// TigerBeetle linked batches are atomic only when every transfer except the final one carries Linked.
		flags := transfers[idx].TransferFlags()
		flags.Linked = true
		transfers[idx].Flags = flags.ToUint16()
	}
}

func ulidString() string {
	return ulid.Make().String()
}

func minUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

func minUint32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

func safeMulUint64(a, b uint64) (uint64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	product := a * b
	if product/a != b {
		return 0, fmt.Errorf("uint64 overflow")
	}
	return product, nil
}

func derefTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func traceIDFromContext(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
		return sc.TraceID().String()
	}
	return ""
}
