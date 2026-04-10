package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/tigerbeetle/tigerbeetle-go/pkg/types"
	"go.opentelemetry.io/otel/trace"
)

type windowRateContext struct {
	PlanID      string            `json:"plan_id"`
	UnitRates   map[string]uint64 `json:"unit_rates"`
	CostPerUnit uint64            `json:"cost_per_unit"`
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
	ReservePolicy ReservePolicy
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

	costPerUnit, err := computeCostPerUnit(req.Allocation, config.UnitRates)
	if err != nil {
		return WindowReservation{}, err
	}
	quantity, chargeUnits, err := c.pickReservationQuantity(ctx, req.OrgID, req.ProductID, config.ReservePolicy, costPerUnit)
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

	legs, err := c.reserveFunding(ctx, windowID, req.OrgID, req.ProductID, chargeUnits)
	if err != nil {
		return WindowReservation{}, err
	}

	rateContext := windowRateContext{
		PlanID:      config.PlanID,
		UnitRates:   cloneUint64Map(config.UnitRates),
		CostPerUnit: costPerUnit,
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
		UnitRates:           cloneUint64Map(config.UnitRates),
		CostPerUnit:         costPerUnit,
		WindowStart:         windowStart,
		ExpiresAt:           expiresAt,
		RenewBy:             renewBy,
	}, nil
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

	billableQuantity := minUint32(actualQuantity, window.ReservedQuantity)
	writeoffQuantity := actualQuantity - billableQuantity
	billedChargeUnits, err := safeMulUint64(window.RateContext.CostPerUnit, uint64(billableQuantity))
	if err != nil {
		return SettleResult{}, fmt.Errorf("billed charge units: %w", err)
	}
	writeoffChargeUnits, err := safeMulUint64(window.RateContext.CostPerUnit, uint64(writeoffQuantity))
	if err != nil {
		return SettleResult{}, fmt.Errorf("writeoff charge units: %w", err)
	}

	transfers := make([]types.Transfer, 0, len(window.FundingLegs))
	remaining := billedChargeUnits
	for idx, leg := range window.FundingLegs {
		if idx > math.MaxUint8 {
			return SettleResult{}, fmt.Errorf("funding leg %d exceeds tigerbeetle limit", idx)
		}
		postAmount := minUint64(leg.Amount, remaining)
		if postAmount > 0 {
			transfers = append(transfers, types.Transfer{
				ID:        WindowTransferID(window.WindowID, uint8(idx), KindSettlement).raw,
				PendingID: leg.TransferID.raw,
				Amount:    types.ToUint128(postAmount),
				Ledger:    1,
				Code:      uint16(KindReservation),
				Flags:     types.TransferFlags{PostPendingTransfer: true}.ToUint16(),
			})
			remaining -= postAmount
			continue
		}
		transfers = append(transfers, types.Transfer{
			ID:        WindowTransferID(window.WindowID, uint8(idx), KindVoid).raw,
			PendingID: leg.TransferID.raw,
			Ledger:    1,
			Code:      uint16(KindReservation),
			Flags:     types.TransferFlags{VoidPendingTransfer: true}.ToUint16(),
		})
	}
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
			ORDER BY created_at
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
	endedAt := window.WindowStart
	switch window.ReservationShape {
	case ReservationShapeTime:
		endedAt = window.WindowStart.Add(time.Duration(window.ActualQuantity) * time.Second)
	case ReservationShapeUnits:
		if window.SettledAt != nil {
			endedAt = *window.SettledAt
		}
	}

	row := MeteringRow{
		WindowID:            window.WindowID,
		OrgID:               strconv.FormatUint(uint64(window.OrgID), 10),
		ActorID:             window.ActorID,
		ProductID:           window.ProductID,
		SourceType:          window.SourceType,
		SourceRef:           window.SourceRef,
		WindowSeq:           window.WindowSeq,
		ReservationShape:    string(window.ReservationShape),
		StartedAt:           window.WindowStart,
		EndedAt:             endedAt,
		ReservedQuantity:    uint64(window.ReservedQuantity),
		ActualQuantity:      uint64(window.ActualQuantity),
		BillableQuantity:    uint64(window.BillableQuantity),
		WriteoffQuantity:    uint64(window.WriteoffQuantity),
		PricingPhase:        string(window.PricingPhase),
		Dimensions:          cloneFloat64Map(window.Allocation),
		ChargeUnits:         window.BilledChargeUnits,
		WriteoffChargeUnits: window.WriteoffChargeUnits,
		PlanID:              window.PlanID,
		CostPerUnit:         window.RateContext.CostPerUnit,
		RecordedAt:          time.Now().UTC(),
	}
	remaining := window.BilledChargeUnits
	for _, leg := range window.FundingLegs {
		amount := minUint64(leg.Amount, remaining)
		remaining -= amount
		switch leg.Source {
		case SourceFreeTier:
			row.FreeTierUnits += amount
		case SourceSubscription:
			row.SubscriptionUnits += amount
		case SourcePurchase:
			row.PurchaseUnits += amount
		case SourcePromo:
			row.PromoUnits += amount
		case SourceRefund:
			row.RefundUnits += amount
		}
	}
	return row, nil
}

func (c *Client) loadPlanConfig(ctx context.Context, orgID OrgID, productID string) (productPlanConfig, error) {
	var (
		planID            string
		billingMode       string
		unitRatesJSON     []byte
		reservePolicyJSON []byte
	)

	err := c.pg.QueryRowContext(ctx, `
		SELECT p.plan_id, p.billing_mode, p.unit_rates::text, pr.reserve_policy::text
		FROM plans p
		JOIN products pr ON pr.product_id = p.product_id
		WHERE p.product_id = $1 AND p.is_default AND p.active
		LIMIT 1
	`, productID).Scan(&planID, &billingMode, &unitRatesJSON, &reservePolicyJSON)
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
		ReservePolicy: reservePolicy,
	}, nil
}

func (c *Client) pickReservationQuantity(ctx context.Context, orgID OrgID, productID string, policy ReservePolicy, costPerUnit uint64) (uint32, uint64, error) {
	if costPerUnit == 0 {
		return policy.TargetQuantity, 0, nil
	}
	grants, err := c.ListGrantBalances(ctx, orgID, productID)
	if err != nil {
		return 0, 0, err
	}
	var available uint64
	for _, grant := range grants {
		available, err = safeAddUint64(available, grant.Available)
		if err != nil {
			return 0, 0, err
		}
	}
	targetChargeUnits, err := safeMulUint64(uint64(policy.TargetQuantity), costPerUnit)
	if err != nil {
		return 0, 0, err
	}
	minChargeUnits, err := safeMulUint64(uint64(policy.MinQuantity), costPerUnit)
	if err != nil {
		return 0, 0, err
	}
	if available >= targetChargeUnits {
		return policy.TargetQuantity, targetChargeUnits, nil
	}
	if !policy.AllowPartialReserve || available < minChargeUnits {
		return 0, 0, ErrInsufficientBalance
	}
	quantity := uint32(available / costPerUnit)
	if quantity < policy.MinQuantity {
		return 0, 0, ErrInsufficientBalance
	}
	if quantity > policy.TargetQuantity {
		quantity = policy.TargetQuantity
	}
	chargeUnits, err := safeMulUint64(uint64(quantity), costPerUnit)
	if err != nil {
		return 0, 0, err
	}
	return quantity, chargeUnits, nil
}

func (c *Client) reserveFunding(ctx context.Context, windowID string, orgID OrgID, productID string, chargeUnits uint64) ([]WindowFundingLeg, error) {
	grants, err := c.ListGrantBalances(ctx, orgID, productID)
	if err != nil {
		return nil, err
	}
	legs := make([]WindowFundingLeg, 0, len(grants))
	transfers := make([]types.Transfer, 0, len(grants))
	remaining := chargeUnits
	for idx, grant := range grants {
		if remaining == 0 {
			break
		}
		if grant.Available == 0 {
			continue
		}
		amount := minUint64(grant.Available, remaining)
		transferID := WindowTransferID(windowID, uint8(idx), KindReservation)
		legs = append(legs, WindowFundingLeg{
			GrantID:    grant.GrantID,
			TransferID: transferID,
			Amount:     amount,
			Source:     grant.Source,
		})
		transfers = append(transfers, types.Transfer{
			ID:              transferID.raw,
			DebitAccountID:  GrantAccountID(grant.GrantID).raw,
			CreditAccountID: OperatorAccountID(AcctRevenue).raw,
			Amount:          types.ToUint128(amount),
			Timeout:         c.cfg.PendingTimeoutSecs,
			Ledger:          1,
			Code:            uint16(KindReservation),
			Flags:           types.TransferFlags{Pending: true}.ToUint16(),
		})
		remaining -= amount
	}
	if remaining > 0 {
		return nil, ErrInsufficientBalance
	}
	if err := c.createTransfers(transfers); err != nil {
		return nil, err
	}
	return legs, nil
}

func (c *Client) voidReservedFunding(ctx context.Context, windowID string, legs []WindowFundingLeg) error {
	transfers := make([]types.Transfer, 0, len(legs))
	for idx, leg := range legs {
		transfers = append(transfers, types.Transfer{
			ID:        WindowTransferID(windowID, uint8(idx), KindVoid).raw,
			PendingID: leg.TransferID.raw,
			Ledger:    1,
			Code:      uint16(KindReservation),
			Flags:     types.TransferFlags{VoidPendingTransfer: true}.ToUint16(),
		})
	}
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
			window_start, expires_at, renew_by, settled_at, metering_projected_at, last_projection_error
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

func computeCostPerUnit(allocation map[string]float64, unitRates map[string]uint64) (uint64, error) {
	var total float64
	for dimension, quantity := range allocation {
		if quantity < 0 {
			return 0, fmt.Errorf("allocation %s must be non-negative", dimension)
		}
		rate, ok := unitRates[dimension]
		if !ok {
			return 0, fmt.Errorf("%w: %s", ErrDimensionMismatch, dimension)
		}
		total += quantity * float64(rate)
	}
	rounded := math.Round(total)
	if math.Abs(total-rounded) > 1e-9 {
		return 0, fmt.Errorf("non-integral cost_per_unit %.9f", total)
	}
	return uint64(rounded), nil
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
