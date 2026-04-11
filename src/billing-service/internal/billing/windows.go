package billing

import (
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
	PlanID               string            `json:"plan_id"`
	UnitRates            map[string]uint64 `json:"unit_rates"`
	RateBuckets          map[string]string `json:"rate_buckets"`
	ComponentCostPerUnit map[string]uint64 `json:"component_cost_per_unit"`
	BucketCostPerUnit    map[string]uint64 `json:"bucket_cost_per_unit"`
	CostPerUnit          uint64            `json:"cost_per_unit"`
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
	UnitRates     map[string]uint64
	RateBuckets   map[string]string
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
	if err := c.ensureOrgNotSuspended(ctx, req.OrgID); err != nil {
		return WindowReservation{}, err
	}

	config, err := c.loadPlanConfig(ctx, req.OrgID, req.ProductID)
	if err != nil {
		return WindowReservation{}, err
	}
	if config.BillingMode != "prepaid" {
		return WindowReservation{}, ErrUnsupportedBilling
	}

	rateContext, err := computeRateBreakdown(req.ProductID, req.Allocation, config.UnitRates, config.RateBuckets)
	if err != nil {
		return WindowReservation{}, err
	}
	rateContext.PlanID = config.PlanID
	quantity, chargeUnits, bucketChargeUnits, err := c.pickReservationQuantityByFunding(ctx, req.OrgID, req.ProductID, config.ReservePolicy, rateContext.BucketCostPerUnit)
	if err != nil {
		return WindowReservation{}, err
	}

	windowID := ulidString()
	windowSeq, err := c.nextWindowSeq(ctx, req.ProductID, req.SourceType, req.SourceRef)
	if err != nil {
		return WindowReservation{}, err
	}
	windowStart := c.clock().UTC()
	expiresAt, renewBy := windowTiming(windowStart, config.ReservePolicy, c.cfg.PendingTimeoutSecs, quantity)

	legs, err := c.reserveGrantFunding(ctx, windowID, req.OrgID, req.ProductID, bucketChargeUnits)
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
		UnitRates:           cloneUint64Map(rateContext.UnitRates),
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
	bucketBilledUnits, billedChargeUnits, err := bucketChargeUnitsForQuantity(window, billableQuantity)
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

	settlements, err := settleFundingLegsByBucket(window.FundingLegs, bucketBilledUnits)
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
		WindowID:                window.WindowID,
		OrgID:                   strconv.FormatUint(uint64(window.OrgID), 10),
		ActorID:                 window.ActorID,
		ProductID:               window.ProductID,
		SourceType:              window.SourceType,
		SourceRef:               window.SourceRef,
		WindowSeq:               window.WindowSeq,
		ReservationShape:        string(window.ReservationShape),
		StartedAt:               startedAt,
		EndedAt:                 endedAt,
		ReservedQuantity:        uint64(window.ReservedQuantity),
		ActualQuantity:          uint64(window.ActualQuantity),
		BillableQuantity:        uint64(window.BillableQuantity),
		WriteoffQuantity:        uint64(window.WriteoffQuantity),
		PricingPhase:            string(window.PricingPhase),
		Dimensions:              cloneFloat64Map(window.Allocation),
		ComponentQuantities:     componentQuantitiesForQuantity(window.Allocation, window.BillableQuantity),
		ComponentChargeUnits:    map[string]uint64{},
		BucketChargeUnits:       map[string]uint64{},
		ChargeUnits:             window.BilledChargeUnits,
		WriteoffChargeUnits:     window.WriteoffChargeUnits,
		BucketFreeTierUnits:     map[string]uint64{},
		BucketSubscriptionUnits: map[string]uint64{},
		BucketPurchaseUnits:     map[string]uint64{},
		BucketPromoUnits:        map[string]uint64{},
		BucketRefundUnits:       map[string]uint64{},
		BucketReceivableUnits:   map[string]uint64{},
		PlanID:                  window.PlanID,
		CostPerUnit:             rateContext.CostPerUnit,
		RecordedAt:              time.Now().UTC(),
	}
	componentCharges, bucketCharges, err := componentAndBucketChargeUnitsForQuantity(window, window.BillableQuantity)
	if err != nil {
		return MeteringRow{}, err
	}
	row.ComponentChargeUnits = componentCharges
	row.BucketChargeUnits = bucketCharges
	settlements, err := settleFundingLegsByBucket(window.FundingLegs, bucketCharges)
	if err != nil {
		return MeteringRow{}, err
	}
	for _, settlement := range settlements {
		leg := settlement.Leg
		amount := settlement.PostAmount
		if amount == 0 {
			continue
		}
		switch leg.Source {
		case SourceFreeTier:
			row.FreeTierUnits += amount
			if err := addMapUint64(row.BucketFreeTierUnits, leg.ChargeBucketID, amount); err != nil {
				return MeteringRow{}, err
			}
		case SourceSubscription:
			row.SubscriptionUnits += amount
			if err := addMapUint64(row.BucketSubscriptionUnits, leg.ChargeBucketID, amount); err != nil {
				return MeteringRow{}, err
			}
		case SourcePurchase:
			row.PurchaseUnits += amount
			if err := addMapUint64(row.BucketPurchaseUnits, leg.ChargeBucketID, amount); err != nil {
				return MeteringRow{}, err
			}
		case SourcePromo:
			row.PromoUnits += amount
			if err := addMapUint64(row.BucketPromoUnits, leg.ChargeBucketID, amount); err != nil {
				return MeteringRow{}, err
			}
		case SourceRefund:
			row.RefundUnits += amount
			if err := addMapUint64(row.BucketRefundUnits, leg.ChargeBucketID, amount); err != nil {
				return MeteringRow{}, err
			}
		}
	}
	return row, nil
}

func (c *Client) loadPlanConfig(ctx context.Context, _ OrgID, productID string) (productPlanConfig, error) {
	var (
		planID            string
		billingMode       string
		unitRatesJSON     []byte
		rateBucketsJSON   []byte
		reservePolicyJSON []byte
	)

	err := c.pg.QueryRowContext(ctx, `
		SELECT p.plan_id, p.billing_mode, p.unit_rates::text, p.rate_buckets::text, pr.reserve_policy::text
		FROM plans p
		JOIN products pr ON pr.product_id = p.product_id
		WHERE p.product_id = $1 AND p.is_default AND p.active
		LIMIT 1
	`, productID).Scan(&planID, &billingMode, &unitRatesJSON, &rateBucketsJSON, &reservePolicyJSON)
	if err == sql.ErrNoRows {
		return productPlanConfig{}, ErrNoDefaultPlan
	}
	if err != nil {
		return productPlanConfig{}, fmt.Errorf("load default plan: %w", err)
	}

	unitRates, err := decodeUint64Map(unitRatesJSON)
	if err != nil {
		return productPlanConfig{}, err
	}
	rateBuckets, err := decodeStringMap(rateBucketsJSON)
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
		UnitRates:     unitRates,
		RateBuckets:   rateBuckets,
		ReservePolicy: reservePolicy,
	}, nil
}

func (c *Client) pickReservationQuantityByFunding(
	ctx context.Context,
	orgID OrgID,
	productID string,
	policy ReservePolicy,
	bucketCostPerUnit map[string]uint64,
) (uint32, uint64, map[string]uint64, error) {
	grants, err := c.listScopedGrantBalancesForFunding(ctx, orgID, productID)
	if err != nil {
		return 0, 0, nil, err
	}
	return pickBucketReservationQuantity(productID, policy, bucketCostPerUnit, grants)
}

func pickBucketReservationQuantity(
	productID string,
	policy ReservePolicy,
	bucketCostPerUnit map[string]uint64,
	grants []scopedGrantBalance,
) (uint32, uint64, map[string]uint64, error) {
	if policy.TargetQuantity == 0 {
		return 0, 0, nil, fmt.Errorf("reserve policy target_quantity is required")
	}
	minQuantity := policy.MinQuantity
	if minQuantity == 0 {
		minQuantity = policy.TargetQuantity
	}

	if !policy.AllowPartialReserve {
		return fundedReservationQuantity(productID, policy.TargetQuantity, bucketCostPerUnit, grants)
	}

	var bestQuantity uint32
	low := minQuantity
	high := policy.TargetQuantity
	for low <= high {
		mid := low + (high-low)/2
		_, _, _, err := fundedReservationQuantity(productID, mid, bucketCostPerUnit, grants)
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
			return 0, 0, nil, err
		}
	}
	if bestQuantity < minQuantity {
		return 0, 0, nil, ErrInsufficientBalance
	}
	return fundedReservationQuantity(productID, bestQuantity, bucketCostPerUnit, grants)
}

func fundedReservationQuantity(productID string, quantity uint32, bucketCostPerUnit map[string]uint64, grants []scopedGrantBalance) (uint32, uint64, map[string]uint64, error) {
	var totalChargeUnits uint64
	bucketChargeUnits := make(map[string]uint64, len(bucketCostPerUnit))
	for _, bucketID := range sortedUint64MapKeys(bucketCostPerUnit) {
		chargeUnits, err := safeMulUint64(uint64(quantity), bucketCostPerUnit[bucketID])
		if err != nil {
			return 0, 0, nil, err
		}
		if chargeUnits == 0 {
			continue
		}
		bucketChargeUnits[bucketID] = chargeUnits
		totalChargeUnits, err = safeAddUint64(totalChargeUnits, chargeUnits)
		if err != nil {
			return 0, 0, nil, err
		}
	}
	if _, err := planGrantFunding(productID, bucketChargeUnits, grants); err != nil {
		return 0, 0, nil, err
	}
	return quantity, totalChargeUnits, bucketChargeUnits, nil
}

func (c *Client) reserveGrantFunding(
	ctx context.Context,
	windowID string,
	orgID OrgID,
	productID string,
	bucketChargeUnits map[string]uint64,
) ([]WindowFundingLeg, error) {
	grants, err := c.listScopedGrantBalancesForFunding(ctx, orgID, productID)
	if err != nil {
		return nil, err
	}
	planned, err := planGrantFunding(productID, bucketChargeUnits, grants)
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
			Amount:              plannedLeg.AmountUnits,
			Source:              plannedLeg.Source,
			GrantScopeType:      plannedLeg.GrantScopeType,
			GrantScopeProductID: plannedLeg.GrantScopeProductID,
			GrantScopeBucketID:  plannedLeg.GrantScopeBucketID,
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

func settleFundingLegsByBucket(
	legs []WindowFundingLeg,
	billedByBucket map[string]uint64,
) ([]fundingLegSettlement, error) {
	remaining := cloneUint64Map(billedByBucket)
	if remaining == nil {
		remaining = map[string]uint64{}
	}
	actions := make([]fundingLegSettlement, 0, len(legs))
	for _, leg := range legs {
		postAmount := minUint64(leg.Amount, remaining[leg.ChargeBucketID])
		if postAmount > 0 {
			remaining[leg.ChargeBucketID] -= postAmount
		}
		actions = append(actions, fundingLegSettlement{
			Leg:        leg,
			PostAmount: postAmount,
			Void:       postAmount == 0,
		})
	}
	for _, bucketID := range sortedUint64MapKeys(remaining) {
		if remaining[bucketID] != 0 {
			return nil, fmt.Errorf("bucket %s has %d unfunded billed charge units", bucketID, remaining[bucketID])
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
	if err := json.Unmarshal(usageSummaryJSON, &row.UsageSummary); err != nil {
		return persistedWindow{}, fmt.Errorf("decode usage summary: %w", err)
	}
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
		UnitRates:           cloneUint64Map(w.RateContext.UnitRates),
		CostPerUnit:         w.RateContext.CostPerUnit,
		WindowStart:         w.WindowStart.UTC(),
		ActivatedAt:         cloneTimePtr(w.ActivatedAt),
		ExpiresAt:           w.ExpiresAt.UTC(),
		RenewBy:             cloneTimePtr(w.RenewBy),
	}
}

func (c *Client) nextWindowSeq(ctx context.Context, productID, sourceType, sourceRef string) (uint32, error) {
	var next sql.NullInt64
	err := c.pg.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(window_seq) + 1, 0)
		FROM billing_windows
		WHERE product_id = $1 AND source_type = $2 AND source_ref = $3
	`, productID, sourceType, sourceRef).Scan(&next)
	if err != nil {
		return 0, fmt.Errorf("load next window seq: %w", err)
	}
	return uint32(next.Int64), nil
}

func (c *Client) ensureOrgNotSuspended(ctx context.Context, orgID OrgID) error {
	var suspended bool
	if err := c.pg.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM subscriptions WHERE org_id = $1 AND status = 'suspended'
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
	productID string,
	allocation map[string]float64,
	unitRates map[string]uint64,
	rateBuckets map[string]string,
) (windowRateContext, error) {
	breakdown := windowRateContext{
		UnitRates:            cloneUint64Map(unitRates),
		RateBuckets:          cloneStringMap(rateBuckets),
		ComponentCostPerUnit: make(map[string]uint64, len(allocation)),
		BucketCostPerUnit:    map[string]uint64{},
	}
	for _, dimension := range sortedFloat64MapKeys(allocation) {
		quantity := allocation[dimension]
		if quantity < 0 {
			return windowRateContext{}, fmt.Errorf("allocation %s must be non-negative", dimension)
		}
		rate, ok := unitRates[dimension]
		if !ok {
			return windowRateContext{}, fmt.Errorf("%w: %s", ErrDimensionMismatch, dimension)
		}
		rawComponentCost := quantity * float64(rate)
		rounded := math.Round(rawComponentCost)
		if math.Abs(rawComponentCost-rounded) > 1e-9 {
			return windowRateContext{}, fmt.Errorf("non-integral component cost_per_unit %s %.9f", dimension, rawComponentCost)
		}
		if rounded < 0 || rounded > float64(^uint64(0)) {
			return windowRateContext{}, fmt.Errorf("component cost_per_unit %s overflows uint64", dimension)
		}
		componentCost := uint64(rounded)
		breakdown.ComponentCostPerUnit[dimension] = componentCost
		bucketID := bucketForDimension(productID, dimension, rateBuckets)
		if err := addMapUint64(breakdown.BucketCostPerUnit, bucketID, componentCost); err != nil {
			return windowRateContext{}, fmt.Errorf("add bucket cost %s: %w", bucketID, err)
		}
		var err error
		breakdown.CostPerUnit, err = safeAddUint64(breakdown.CostPerUnit, componentCost)
		if err != nil {
			return windowRateContext{}, err
		}
	}
	return breakdown, nil
}

func bucketChargeUnitsForQuantity(window persistedWindow, quantity uint32) (map[string]uint64, uint64, error) {
	_, bucketCharges, err := componentAndBucketChargeUnitsForQuantity(window, quantity)
	if err != nil {
		return nil, 0, err
	}
	var total uint64
	for _, bucketID := range sortedUint64MapKeys(bucketCharges) {
		total, err = safeAddUint64(total, bucketCharges[bucketID])
		if err != nil {
			return nil, 0, err
		}
	}
	return bucketCharges, total, nil
}

func componentAndBucketChargeUnitsForQuantity(window persistedWindow, quantity uint32) (map[string]uint64, map[string]uint64, error) {
	rateContext, err := completeRateContext(window)
	if err != nil {
		return nil, nil, err
	}
	componentCharges := make(map[string]uint64, len(rateContext.ComponentCostPerUnit))
	for _, componentID := range sortedUint64MapKeys(rateContext.ComponentCostPerUnit) {
		chargeUnits, err := safeMulUint64(uint64(quantity), rateContext.ComponentCostPerUnit[componentID])
		if err != nil {
			return nil, nil, err
		}
		if chargeUnits != 0 {
			componentCharges[componentID] = chargeUnits
		}
	}
	bucketCharges := make(map[string]uint64, len(rateContext.BucketCostPerUnit))
	for _, bucketID := range sortedUint64MapKeys(rateContext.BucketCostPerUnit) {
		chargeUnits, err := safeMulUint64(uint64(quantity), rateContext.BucketCostPerUnit[bucketID])
		if err != nil {
			return nil, nil, err
		}
		if chargeUnits != 0 {
			bucketCharges[bucketID] = chargeUnits
		}
	}
	return componentCharges, bucketCharges, nil
}

func completeRateContext(window persistedWindow) (windowRateContext, error) {
	rateContext := window.RateContext
	if rateContext.PlanID == "" {
		rateContext.PlanID = window.PlanID
	}
	if len(rateContext.ComponentCostPerUnit) > 0 && len(rateContext.BucketCostPerUnit) > 0 {
		return rateContext, nil
	}
	if len(rateContext.UnitRates) > 0 {
		breakdown, err := computeRateBreakdown(window.ProductID, window.Allocation, rateContext.UnitRates, rateContext.RateBuckets)
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
	rateContext.ComponentCostPerUnit = map[string]uint64{}
	rateContext.BucketCostPerUnit = map[string]uint64{window.ProductID: rateContext.CostPerUnit}
	return rateContext, nil
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

func bucketForDimension(productID string, dimension string, rateBuckets map[string]string) string {
	if bucketID := rateBuckets[dimension]; bucketID != "" {
		return bucketID
	}
	return productID
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
