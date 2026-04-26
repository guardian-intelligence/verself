package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/attribute"

	"github.com/verself/billing-service/internal/billing/ledger"
	"github.com/verself/billing-service/internal/store"
)

const (
	defaultWindowMillis   uint32 = 5 * 60 * 1000
	defaultRenewBefore    uint32 = 30 * 1000
	minCustomWindowMillis uint32 = 30 * 1000
	minRenewBeforeMillis  uint32 = 5 * 1000
	pricingPhaseIncluded         = "included"
)

type pricingContext struct {
	PlanID             string            `json:"plan_id"`
	BillingMode        string            `json:"billing_mode"`
	ContractID         string            `json:"contract_id,omitempty"`
	PhaseID            string            `json:"phase_id,omitempty"`
	OveragePolicy      string            `json:"overage_policy"`
	Currency           string            `json:"currency"`
	SKURates           map[string]uint64 `json:"sku_rates"`
	SKUBuckets         map[string]string `json:"sku_buckets"`
	SKUBucketOrders    map[string]int    `json:"sku_bucket_orders,omitempty"`
	SKUDisplayNames    map[string]string `json:"sku_display_names"`
	SKUQuantityUnits   map[string]string `json:"sku_quantity_units"`
	BucketDisplayNames map[string]string `json:"bucket_display_names"`
	CostPerUnit        uint64            `json:"cost_per_unit"`
}

type persistedWindow struct {
	WindowID            string
	CycleID             string
	OrgID               OrgID
	ActorID             string
	ProductID           string
	PricingContractID   string
	PricingPhaseID      string
	PricingPlanID       string
	SourceType          string
	SourceRef           string
	SourceFingerprint   string
	WindowSeq           uint32
	State               string
	ReservationShape    string
	ReservedQuantity    uint32
	ActualQuantity      uint32
	BillableQuantity    uint32
	WriteoffQuantity    uint32
	ReservedChargeUnits uint64
	BilledChargeUnits   uint64
	WriteoffChargeUnits uint64
	PricingPhase        string
	Allocation          map[string]float64
	RateContext         pricingContext
	UsageSummary        map[string]any
	FundingLegs         []fundingLeg
	WindowStart         time.Time
	ActivatedAt         *time.Time
	ExpiresAt           time.Time
	RenewBy             *time.Time
	SettledAt           *time.Time
	CreatedAt           time.Time
}

type meteringRow struct {
	WindowID                 string             `ch:"window_id"`
	OrgID                    string             `ch:"org_id"`
	ActorID                  string             `ch:"actor_id"`
	ProductID                string             `ch:"product_id"`
	SourceType               string             `ch:"source_type"`
	SourceRef                string             `ch:"source_ref"`
	WindowSeq                uint32             `ch:"window_seq"`
	ReservationShape         string             `ch:"reservation_shape"`
	StartedAt                time.Time          `ch:"started_at"`
	EndedAt                  time.Time          `ch:"ended_at"`
	ReservedQuantity         uint64             `ch:"reserved_quantity"`
	ActualQuantity           uint64             `ch:"actual_quantity"`
	BillableQuantity         uint64             `ch:"billable_quantity"`
	WriteoffQuantity         uint64             `ch:"writeoff_quantity"`
	CycleID                  string             `ch:"cycle_id"`
	PricingContractID        string             `ch:"pricing_contract_id"`
	PricingPhaseID           string             `ch:"pricing_phase_id"`
	PricingPlanID            string             `ch:"pricing_plan_id"`
	PricingPhase             string             `ch:"pricing_phase"`
	Dimensions               map[string]float64 `ch:"dimensions"`
	ComponentQuantities      map[string]float64 `ch:"component_quantities"`
	ComponentChargeUnits     map[string]uint64  `ch:"component_charge_units"`
	BucketChargeUnits        map[string]uint64  `ch:"bucket_charge_units"`
	ChargeUnits              uint64             `ch:"charge_units"`
	WriteoffChargeUnits      uint64             `ch:"writeoff_charge_units"`
	FreeTierUnits            uint64             `ch:"free_tier_units"`
	ContractUnits            uint64             `ch:"contract_units"`
	PurchaseUnits            uint64             `ch:"purchase_units"`
	PromoUnits               uint64             `ch:"promo_units"`
	RefundUnits              uint64             `ch:"refund_units"`
	ReceivableUnits          uint64             `ch:"receivable_units"`
	AdjustmentUnits          uint64             `ch:"adjustment_units"`
	AdjustmentReason         string             `ch:"adjustment_reason"`
	ComponentFreeTierUnits   map[string]uint64  `ch:"component_free_tier_units"`
	ComponentContractUnits   map[string]uint64  `ch:"component_contract_units"`
	ComponentPurchaseUnits   map[string]uint64  `ch:"component_purchase_units"`
	ComponentPromoUnits      map[string]uint64  `ch:"component_promo_units"`
	ComponentRefundUnits     map[string]uint64  `ch:"component_refund_units"`
	ComponentReceivableUnits map[string]uint64  `ch:"component_receivable_units"`
	ComponentAdjustmentUnits map[string]uint64  `ch:"component_adjustment_units"`
	UsageEvidence            map[string]uint64  `ch:"usage_evidence"`
	CostPerUnit              uint64             `ch:"cost_per_unit"`
	RecordedAt               time.Time          `ch:"recorded_at"`
	TraceID                  string             `ch:"trace_id"`
}

func (c *Client) ReserveWindow(ctx context.Context, req ReserveRequest) (WindowReservation, error) {
	if req.OrgID == 0 || req.ProductID == "" || req.ActorID == "" || req.SourceType == "" || req.SourceRef == "" {
		return WindowReservation{}, fmt.Errorf("reserve requires org_id, product_id, actor_id, source_type, and source_ref")
	}
	if len(req.Allocation) == 0 {
		return WindowReservation{}, fmt.Errorf("reserve allocation is required")
	}
	shape, err := normalizeReservationShape(req.ReservationShape)
	if err != nil {
		return WindowReservation{}, err
	}
	quantity, err := reserveWindowQuantity(shape, req.ReservedQuantity)
	if err != nil {
		return WindowReservation{}, err
	}
	sourceFingerprint := reserveSourceFingerprint(req, shape, quantity)
	if existing, ok, err := c.loadWindowBySource(ctx, req.OrgID, req.ProductID, req.SourceType, req.SourceRef, req.WindowSeq); err != nil {
		return WindowReservation{}, err
	} else if ok {
		if existing.SourceFingerprint != sourceFingerprint {
			return WindowReservation{}, fmt.Errorf("%w: existing window %s has a different request fingerprint", ErrWindowSourceConflict, existing.WindowID)
		}
		switch existing.State {
		case "reserved", "active", "settling", "settled":
			return existing.reservation(), nil
		case "voided":
			return WindowReservation{}, fmt.Errorf("%w: existing window %s is voided", ErrWindowAlreadyVoided, existing.WindowID)
		default:
			return WindowReservation{}, fmt.Errorf("%w: existing window %s is %s", ErrWindowNotReserved, existing.WindowID, existing.State)
		}
	}
	if err := c.EnsureCurrentEntitlements(ctx, req.OrgID, req.ProductID); err != nil {
		return WindowReservation{}, err
	}

	var reserved persistedWindow
	err = c.WithTx(ctx, "billing.window.reserve", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		now, err := c.BusinessNow(ctx, q, req.OrgID, req.ProductID)
		if err != nil {
			return err
		}
		if err := c.lockOrgProductTx(ctx, tx, req.OrgID, req.ProductID); err != nil {
			return err
		}
		orgState, _, err := c.orgBillingStateTx(ctx, tx, req.OrgID)
		if err != nil {
			return err
		}
		if orgState == "suspended" || orgState == "closed" {
			return ErrOrgSuspended
		}
		cycle, err := c.ensureOpenBillingCycleTx(ctx, tx, q, req.OrgID, req.ProductID, now)
		if err != nil {
			return err
		}
		pricing, err := c.loadPricingContextTx(ctx, tx, req.OrgID, req.ProductID, now)
		if err != nil {
			return err
		}
		componentCharges, bucketCharges, costPerUnit, err := computeWindowCharges(req.Allocation, pricing.SKURates, pricing.SKUBuckets, quantity)
		if err != nil {
			return err
		}
		pricing.CostPerUnit = costPerUnit
		chargeUnits := sumUint64Map(componentCharges)
		legs, err := c.fundReservationTx(ctx, tx, req.OrgID, req.ProductID, componentCharges, pricing, false)
		if err != nil {
			return err
		}
		if sumFundingLegs(legs) < chargeUnits {
			if pricing.ContractID == "" || pricing.OveragePolicy != "bill_published_rate" {
				return ErrInsufficientBalance
			}
			for _, skuID := range componentChargeOrder(componentCharges, pricing) {
				funded := fundingLegsForComponent(legs, skuID)
				if funded >= componentCharges[skuID] {
					continue
				}
				missing := componentCharges[skuID] - funded
				if missing == 0 {
					continue
				}
				legs = append(legs, fundingLeg{Amount: missing, Source: "receivable", PlanID: pricing.PlanID, ComponentSKUID: skuID, ComponentBucketID: pricing.SKUBuckets[skuID]})
			}
		}
		allocationJSON, err := json.Marshal(req.Allocation)
		if err != nil {
			return fmt.Errorf("marshal allocation: %w", err)
		}
		rateJSON, err := json.Marshal(pricing)
		if err != nil {
			return fmt.Errorf("marshal rate context: %w", err)
		}
		fundingJSON, err := fundingLegsJSON(legs)
		if err != nil {
			return err
		}
		windowID := billingWindowID(req.OrgID, req.ProductID, req.SourceType, req.SourceRef, req.WindowSeq)
		ledgerCorrelationID := ledger.NewID()
		expiresAt, renewBy := reserveWindowTiming(shape, now, quantity)
		billingJobID := ""
		if req.BillingJobID > 0 {
			billingJobID = strconv.FormatInt(req.BillingJobID, 10)
		}
		_, commitSpan := tracer.Start(ctx, "billing.authorization.commit_pg")
		defer commitSpan.End()
		commitSpan.SetAttributes(attribute.String("billing.window_id", windowID), attribute.String("billing.org_id", orgIDText(req.OrgID)), attribute.String("billing.product_id", req.ProductID), attribute.String("billing.reservation_shape", shape), attribute.Int64("billing.reserved_quantity", int64(quantity)), attribute.String("billing.charge_units", strconv.FormatUint(chargeUnits, 10)))
		if err := q.InsertBillingWindow(ctx, store.InsertBillingWindowParams{
			WindowID:            windowID,
			CycleID:             cycle.CycleID,
			OrgID:               orgIDText(req.OrgID),
			ActorID:             req.ActorID,
			ProductID:           req.ProductID,
			PricingContractID:   pricing.ContractID,
			PricingPhaseID:      pricing.PhaseID,
			PricingPlanID:       pricing.PlanID,
			SourceType:          req.SourceType,
			SourceRef:           req.SourceRef,
			SourceFingerprint:   sourceFingerprint,
			BillingJobID:        billingJobID,
			WindowSeq:           int64(req.WindowSeq),
			ReservationShape:    shape,
			ReservedQuantity:    int64(quantity),
			ReservedChargeUnits: int64(chargeUnits),
			PricingPhase:        pricingPhaseIncluded,
			Allocation:          allocationJSON,
			RateContext:         rateJSON,
			FundingLegs:         fundingJSON,
			LedgerCorrelationID: ledgerCorrelationID.Bytes(),
			WindowStart:         timestamptz(now),
			ExpiresAt:           timestamptz(expiresAt),
			RenewBy:             timestamptz(renewBy),
		}); err != nil {
			return fmt.Errorf("insert billing window: %w", err)
		}
		if err := c.insertWindowLedgerLegsTx(ctx, tx, windowID, legs); err != nil {
			return err
		}
		reserved = persistedWindow{WindowID: windowID, CycleID: cycle.CycleID, OrgID: req.OrgID, ActorID: req.ActorID, ProductID: req.ProductID, PricingContractID: pricing.ContractID, PricingPhaseID: pricing.PhaseID, PricingPlanID: pricing.PlanID, SourceType: req.SourceType, SourceRef: req.SourceRef, SourceFingerprint: sourceFingerprint, WindowSeq: req.WindowSeq, State: "reserved", ReservationShape: shape, ReservedQuantity: quantity, ReservedChargeUnits: chargeUnits, PricingPhase: pricingPhaseIncluded, Allocation: cloneFloatMap(req.Allocation), RateContext: pricing, FundingLegs: legs, WindowStart: now, ExpiresAt: expiresAt, RenewBy: &renewBy}
		if err := appendEvent(ctx, tx, q, eventFact{EventType: "billing_window_reserve_requested", AggregateType: "billing_window", AggregateID: windowID, OrgID: req.OrgID, ProductID: req.ProductID, OccurredAt: now, Payload: map[string]any{"window_id": windowID, "cycle_id": cycle.CycleID, "pricing_plan_id": pricing.PlanID, "pricing_phase_id": pricing.PhaseID, "pricing_contract_id": pricing.ContractID, "source_type": req.SourceType, "source_ref": req.SourceRef, "source_fingerprint": sourceFingerprint, "window_seq": req.WindowSeq, "reservation_shape": shape, "reserved_quantity": quantity, "charge_units": chargeUnits, "component_charge_units": componentCharges, "bucket_charge_units": bucketCharges}}); err != nil {
			return err
		}
		return appendEvent(ctx, tx, q, eventFact{
			EventType:     "billing_window_reserved",
			AggregateType: "billing_window",
			AggregateID:   windowID,
			OrgID:         req.OrgID,
			ProductID:     req.ProductID,
			OccurredAt:    now,
			Payload: map[string]any{
				"window_id":               windowID,
				"cycle_id":                cycle.CycleID,
				"pricing_plan_id":         pricing.PlanID,
				"pricing_phase_id":        pricing.PhaseID,
				"pricing_contract_id":     pricing.ContractID,
				"source_type":             req.SourceType,
				"source_ref":              req.SourceRef,
				"source_fingerprint":      sourceFingerprint,
				"window_seq":              req.WindowSeq,
				"reservation_shape":       shape,
				"reserved_quantity":       quantity,
				"charge_units":            chargeUnits,
				"component_charge_units":  componentCharges,
				"bucket_charge_units":     bucketCharges,
				"authorization_committed": true,
			},
		})
	})
	if err != nil {
		return WindowReservation{}, err
	}
	return reserved.reservation(), nil
}

func normalizeReservationShape(shape string) (string, error) {
	switch strings.TrimSpace(shape) {
	case ReservationShapeTime:
		return ReservationShapeTime, nil
	case ReservationShapeCount:
		return ReservationShapeCount, nil
	case "":
		return "", fmt.Errorf("reserve reservation_shape is required")
	default:
		return "", fmt.Errorf("reserve reservation_shape must be %q or %q", ReservationShapeTime, ReservationShapeCount)
	}
}

func reserveWindowQuantity(shape string, requested uint32) (uint32, error) {
	switch shape {
	case ReservationShapeTime:
		if requested == 0 {
			return defaultWindowMillis, nil
		}
		if requested < minCustomWindowMillis {
			return 0, fmt.Errorf("reserve time reserved_quantity must be 0 or at least %d", minCustomWindowMillis)
		}
		return requested, nil
	case ReservationShapeCount:
		if requested == 0 {
			return 0, fmt.Errorf("reserve count reserved_quantity must be greater than 0")
		}
		return requested, nil
	default:
		return 0, fmt.Errorf("reserve reservation_shape must be %q or %q", ReservationShapeTime, ReservationShapeCount)
	}
}

func reserveSourceFingerprint(req ReserveRequest, shape string, quantity uint32) string {
	parts := []string{
		orgIDText(req.OrgID),
		req.ProductID,
		req.ActorID,
		req.SourceType,
		req.SourceRef,
		strconv.FormatUint(uint64(req.WindowSeq), 10),
		shape,
		strconv.FormatUint(uint64(quantity), 10),
		strconv.FormatUint(req.ConcurrentCount, 10),
		strconv.FormatInt(req.BillingJobID, 10),
	}
	skus := make([]string, 0, len(req.Allocation))
	for skuID := range req.Allocation {
		skus = append(skus, skuID)
	}
	sort.Strings(skus)
	for _, skuID := range skus {
		parts = append(parts, skuID, strconv.FormatFloat(req.Allocation[skuID], 'g', -1, 64))
	}
	return textID("winfp", parts...)
}

func reserveWindowTiming(shape string, now time.Time, quantity uint32) (time.Time, time.Time) {
	authorizationMillis := quantity
	if shape == ReservationShapeCount {
		authorizationMillis = defaultWindowMillis
	}
	expiresAt := now.Add(time.Duration(authorizationMillis) * time.Millisecond)
	renewBy := expiresAt.Add(-time.Duration(windowRenewBeforeMillis(authorizationMillis)) * time.Millisecond)
	if !renewBy.After(now) {
		renewBy = expiresAt
	}
	return expiresAt, renewBy
}

func windowRenewBeforeMillis(quantity uint32) uint32 {
	lead := quantity / 3
	if lead < minRenewBeforeMillis {
		lead = minRenewBeforeMillis
	}
	if lead > defaultRenewBefore {
		lead = defaultRenewBefore
	}
	if lead >= quantity {
		return 0
	}
	return lead
}

func (c *Client) ActivateWindow(ctx context.Context, windowID string, activatedAt time.Time) (WindowReservation, error) {
	if windowID == "" {
		return WindowReservation{}, ErrWindowNotFound
	}
	window, err := c.loadWindow(ctx, windowID)
	if err != nil {
		return WindowReservation{}, err
	}
	if window.State == "settled" {
		return WindowReservation{}, ErrWindowAlreadySettled
	}
	if window.State == "voided" {
		return WindowReservation{}, ErrWindowAlreadyVoided
	}
	if window.State != "reserved" && window.State != "active" {
		return WindowReservation{}, ErrWindowNotReserved
	}
	if window.ActivatedAt != nil {
		return window.reservation(), nil
	}
	if activatedAt.IsZero() {
		activatedAt = time.Now().UTC()
	} else {
		activatedAt = activatedAt.UTC()
	}
	if activatedAt.After(window.ExpiresAt) {
		return WindowReservation{}, fmt.Errorf("%w: reservation expired", ErrWindowNotReserved)
	}
	expiresAt, renewBy := reserveWindowTiming(window.ReservationShape, activatedAt, window.ReservedQuantity)
	if !renewBy.After(activatedAt) {
		renewBy = expiresAt
	}
	err = c.WithTx(ctx, "billing.window.activate", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		rowsAffected, err := q.ActivateBillingWindow(ctx, store.ActivateBillingWindowParams{
			WindowID:    windowID,
			ActivatedAt: timestamptz(activatedAt),
			ExpiresAt:   timestamptz(expiresAt),
			RenewBy:     timestamptz(renewBy),
		})
		if err != nil {
			return fmt.Errorf("activate billing window: %w", err)
		}
		if rowsAffected == 0 {
			return nil
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "billing_window_activated", AggregateType: "billing_window", AggregateID: windowID, OrgID: window.OrgID, ProductID: window.ProductID, OccurredAt: activatedAt, Payload: map[string]any{"window_id": windowID, "cycle_id": window.CycleID, "pricing_plan_id": window.PricingPlanID, "pricing_phase_id": window.PricingPhaseID, "pricing_contract_id": window.PricingContractID}})
	})
	if err != nil {
		return WindowReservation{}, err
	}
	return c.loadReservation(ctx, windowID)
}

func (c *Client) SettleWindow(ctx context.Context, windowID string, actualQuantity uint32, usageSummary map[string]any) (SettleResult, error) {
	window, err := c.loadWindow(ctx, windowID)
	if err != nil {
		return SettleResult{}, err
	}
	if window.State == "voided" {
		return SettleResult{}, ErrWindowAlreadyVoided
	}
	if window.State == "settled" {
		return window.settleResult(), nil
	}
	if window.State != "reserved" && window.State != "active" {
		return SettleResult{}, ErrWindowNotReserved
	}
	if window.State == "active" && window.ActivatedAt == nil {
		return SettleResult{}, ErrWindowNotActivated
	}
	if usageSummary == nil {
		usageSummary = map[string]any{}
	}
	billable := actualQuantity
	writeoff := uint32(0)
	if actualQuantity > window.ReservedQuantity {
		billable = window.ReservedQuantity
		writeoff = actualQuantity - window.ReservedQuantity
	}
	componentBilled, _, _, err := computeWindowCharges(window.Allocation, window.RateContext.SKURates, window.RateContext.SKUBuckets, billable)
	if err != nil {
		return SettleResult{}, err
	}
	billedUnits := sumUint64Map(componentBilled)
	componentWriteoff, _, _, err := computeWindowCharges(window.Allocation, window.RateContext.SKURates, window.RateContext.SKUBuckets, writeoff)
	if err != nil {
		return SettleResult{}, err
	}
	writeoffUnits := sumUint64Map(componentWriteoff)
	settledAt := time.Now().UTC()
	usageJSON, err := json.Marshal(usageSummary)
	if err != nil {
		return SettleResult{}, fmt.Errorf("marshal usage summary: %w", err)
	}
	var settleCommandID string
	err = c.WithTx(ctx, "billing.window.settle.prepare", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		currentState, err := c.lockWindowStateTx(ctx, tx, windowID)
		if err != nil {
			return err
		}
		if currentState != "reserved" && currentState != "active" {
			return nil
		}
		settlePayload, settledFundingLegs, err := c.settleWindowLedgerPayloadTx(ctx, tx, windowID, componentBilled, settledAt)
		if err != nil {
			return err
		}
		fundingJSON, err := fundingLegsJSON(settledFundingLegs)
		if err != nil {
			return err
		}
		rowsAffected, err := q.PrepareBillingWindowSettlement(ctx, store.PrepareBillingWindowSettlementParams{
			WindowID:            windowID,
			ActualQuantity:      int64(actualQuantity),
			BillableQuantity:    int64(billable),
			WriteoffQuantity:    int64(writeoff),
			BilledChargeUnits:   int64(billedUnits),
			WriteoffChargeUnits: int64(writeoffUnits),
			WriteoffReason:      writeoffReason(writeoff, window),
			UsageSummary:        usageJSON,
			FundingLegs:         fundingJSON,
			SettledAt:           timestamptz(settledAt),
		})
		if err != nil {
			return fmt.Errorf("prepare billing window settlement: %w", err)
		}
		if rowsAffected == 0 {
			return nil
		}
		if len(settlePayload.Transfers) > 0 {
			settleCommandID, _, err = c.createLedgerCommandTx(ctx, tx, "settle_window", "billing_window", windowID, window.OrgID, window.ProductID, "settle_window:"+windowID, settlePayload)
			if err != nil {
				return err
			}
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "billing_window_settle_requested", AggregateType: "billing_window", AggregateID: windowID, OrgID: window.OrgID, ProductID: window.ProductID, OccurredAt: settledAt, Payload: map[string]any{"window_id": windowID, "cycle_id": window.CycleID, "actual_quantity": actualQuantity, "billable_quantity": billable, "billed_charge_units": billedUnits, "ledger_command_id": settleCommandID}})
	})
	if err != nil {
		return SettleResult{}, err
	}
	if settleCommandID != "" {
		dispatchCtx, span := tracer.Start(ctx, "billing.ledger.settle.dispatch")
		span.SetAttributes(attribute.String("billing.window_id", windowID), attribute.String("billing.ledger_command_id", settleCommandID))
		if err := c.dispatchLedgerCommand(dispatchCtx, settleCommandID); err != nil {
			span.End()
			return SettleResult{}, err
		}
		span.End()
	}
	if err := c.markWindowSettlementPosted(ctx, windowID); err != nil {
		return SettleResult{}, err
	}
	settled, err := c.loadWindow(ctx, windowID)
	if err != nil {
		return SettleResult{}, err
	}
	if c.runtime == nil {
		if _, err := c.ProjectMeteringWindow(ctx, settled.WindowID); err != nil {
			c.logger.WarnContext(ctx, "billing metering projection failed", "window_id", windowID, "error", err)
			_ = c.queries.UpdateWindowProjectionError(ctx, store.UpdateWindowProjectionErrorParams{WindowID: windowID, ProjectionError: err.Error()})
		}
	}
	return settled.settleResult(), nil
}

func (c *Client) ProjectPendingMeteringWindows(ctx context.Context, limit int) (int, error) {
	if c.ch == nil {
		return 0, nil
	}
	if limit <= 0 {
		limit = 100
	}
	windowIDs, err := c.queries.ListPendingMeteringWindowIDs(ctx, store.ListPendingMeteringWindowIDsParams{LimitCount: int32(limit)})
	if err != nil {
		return 0, fmt.Errorf("query pending metering windows: %w", err)
	}
	projected := 0
	for _, pendingWindowID := range windowIDs {
		ok, err := c.ProjectMeteringWindow(ctx, pendingWindowID)
		if err != nil {
			return projected, err
		}
		if ok {
			projected++
		}
	}
	return projected, nil
}

func (c *Client) ProjectMeteringWindow(ctx context.Context, windowID string) (bool, error) {
	if c.ch == nil {
		return false, nil
	}
	tx, err := c.pg.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin metering projection tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := c.queries.WithTx(tx)
	// The direct per-window job and periodic pending scanner can overlap; the
	// advisory lock makes ClickHouse insertion idempotent per billing window.
	if err := q.LockMeteringProjectionWindow(ctx, store.LockMeteringProjectionWindowParams{LockKey: "metering:" + windowID}); err != nil {
		return false, fmt.Errorf("lock metering projection window: %w", err)
	}
	projection, err := q.GetMeteringProjectionStateForUpdate(ctx, store.GetMeteringProjectionStateForUpdateParams{WindowID: windowID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrWindowNotFound
	}
	if err != nil {
		return false, fmt.Errorf("load metering projection state: %w", err)
	}
	if projection.State != "settled" || projection.MeteringProjectedAt.Valid {
		return false, nil
	}
	settled, err := c.loadWindow(ctx, windowID)
	if err != nil {
		return false, err
	}
	recordedAt, err := c.projectMeteringForWindow(ctx, settled)
	if err != nil {
		c.logger.WarnContext(ctx, "billing metering projection failed", "window_id", windowID, "error", err)
		_ = q.UpdateWindowProjectionError(ctx, store.UpdateWindowProjectionErrorParams{WindowID: windowID, ProjectionError: err.Error()})
		_ = tx.Commit(ctx)
		return false, err
	}
	if err := q.MarkMeteringProjected(ctx, store.MarkMeteringProjectedParams{WindowID: windowID, ProjectedAt: timestamptz(recordedAt)}); err != nil {
		return false, fmt.Errorf("mark metering projected: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit metering projection: %w", err)
	}
	return true, nil
}

func (c *Client) VoidWindow(ctx context.Context, windowID string) error {
	window, err := c.loadWindow(ctx, windowID)
	if err != nil {
		return err
	}
	if window.State == "voided" {
		return nil
	}
	if window.State == "settled" {
		return ErrWindowAlreadySettled
	}
	if window.State != "reserved" && window.State != "active" {
		return ErrWindowNotReserved
	}
	now := time.Now().UTC()
	return c.WithTx(ctx, "billing.window.void", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		rowsAffected, err := q.VoidBillingWindow(ctx, store.VoidBillingWindowParams{WindowID: windowID})
		if err != nil {
			return fmt.Errorf("void billing window: %w", err)
		}
		if rowsAffected == 0 {
			return nil
		}
		if err := q.VoidPendingWindowLedgerLegs(ctx, store.VoidPendingWindowLedgerLegsParams{WindowID: windowID}); err != nil {
			return fmt.Errorf("void billing window ledger legs: %w", err)
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "billing_window_voided", AggregateType: "billing_window", AggregateID: windowID, OrgID: window.OrgID, ProductID: window.ProductID, OccurredAt: now, Payload: map[string]any{"window_id": windowID, "cycle_id": window.CycleID, "pricing_plan_id": window.PricingPlanID, "pricing_phase_id": window.PricingPhaseID, "pricing_contract_id": window.PricingContractID, "authorization_released": true}})
	})
}

func (c *Client) settleWindowLedgerPayloadTx(ctx context.Context, tx pgx.Tx, windowID string, componentBilled map[string]uint64, businessTime time.Time) (ledger.CommandPayload, []fundingLeg, error) {
	ctx, span := tracer.Start(ctx, "billing.settle.ledger_payload")
	defer span.End()
	span.SetAttributes(attribute.String("billing.window_id", windowID), attribute.Int("billing.component_count", len(componentBilled)))
	q := c.queries.WithTx(tx)
	correlationRaw, err := q.GetWindowLedgerCorrelationID(ctx, store.GetWindowLedgerCorrelationIDParams{WindowID: windowID})
	if err != nil {
		return ledger.CommandPayload{}, nil, fmt.Errorf("load window ledger correlation id: %w", err)
	}
	correlationID, err := ledger.IDFromBytes(correlationRaw)
	if err != nil {
		return ledger.CommandPayload{}, nil, fmt.Errorf("parse window ledger correlation id: %w", err)
	}
	operators, err := c.operatorLedgerAccountsTx(ctx, tx)
	if err != nil {
		return ledger.CommandPayload{}, nil, err
	}
	revenueID, ok := operators["operator_revenue"]
	if !ok {
		return ledger.CommandPayload{}, nil, fmt.Errorf("operator revenue ledger account is not bootstrapped")
	}
	pending, err := q.ListPendingWindowLedgerLegsForUpdate(ctx, store.ListPendingWindowLedgerLegsForUpdateParams{WindowID: windowID})
	if err != nil {
		return ledger.CommandPayload{}, nil, fmt.Errorf("query pending window ledger legs: %w", err)
	}
	transfers := []ledger.TransferPayload{}
	settledLegs := []fundingLeg{}
	remainingBySKU := cloneUintMap(componentBilled)
	for _, leg := range pending {
		if leg.AmountReserved < 0 {
			return ledger.CommandPayload{}, nil, fmt.Errorf("window leg %d has negative reserved amount %d", leg.LegSeq, leg.AmountReserved)
		}
		postedAmount := minUint64(remainingBySKU[leg.ComponentSkuID], uint64(leg.AmountReserved))
		remainingBySKU[leg.ComponentSkuID] -= postedAmount
		voidedAmount := uint64(leg.AmountReserved) - postedAmount
		var settlementRaw []byte
		settled := fundingLeg{
			GrantID:           leg.GrantID,
			Amount:            postedAmount,
			Source:            leg.Source,
			ScopeType:         leg.ScopeType,
			ScopeProductID:    leg.ScopeProductID,
			ScopeBucketID:     leg.ScopeBucketID,
			ScopeSKUID:        leg.ScopeSkuID,
			PlanID:            leg.PlanID,
			ComponentSKUID:    leg.ComponentSkuID,
			ComponentBucketID: leg.ComponentBucketID,
		}
		if leg.GrantID != "" {
			accountID, err := ledger.IDFromBytes(leg.GrantAccountID)
			if err != nil {
				return ledger.CommandPayload{}, nil, fmt.Errorf("parse grant account id for window leg %d: %w", leg.LegSeq, err)
			}
			settled.GrantAccountID = accountID.String()
			if postedAmount > 0 {
				settlementID, err := existingOrNewLedgerID(leg.SettlementTransferID)
				if err != nil {
					return ledger.CommandPayload{}, nil, fmt.Errorf("parse settlement transfer id for window leg %d: %w", leg.LegSeq, err)
				}
				settlementRaw = settlementID.Bytes()
				settled.SettlementID = settlementID.String()
				transfers = append(transfers, ledger.UsageSpendTransfer(settlementID, accountID, revenueID, postedAmount, correlationID, unixMillis(businessTime)))
			}
		}
		if postedAmount > 0 {
			settledLegs = append(settledLegs, settled)
		}
		if err := q.StoreWindowSettlementAmounts(ctx, store.StoreWindowSettlementAmountsParams{
			WindowID:             windowID,
			LegSeq:               leg.LegSeq,
			SettlementTransferID: settlementRaw,
			AmountPosted:         int64(postedAmount),
			AmountVoided:         int64(voidedAmount),
		}); err != nil {
			return ledger.CommandPayload{}, nil, fmt.Errorf("store settlement amounts for window leg %d: %w", leg.LegSeq, err)
		}
	}
	for skuID, remaining := range remainingBySKU {
		if remaining > 0 {
			return ledger.CommandPayload{}, nil, fmt.Errorf("settlement for sku %s exceeds authorized funding by %d units", skuID, remaining)
		}
	}
	ledger.LinkTransfers(transfers)
	span.SetAttributes(attribute.Int("billing.settlement_transfer_count", len(transfers)), attribute.Int("billing.settled_leg_count", len(settledLegs)))
	return ledger.CommandPayload{Transfers: transfers}, settledLegs, nil
}

func (c *Client) insertWindowLedgerLegsTx(ctx context.Context, tx pgx.Tx, windowID string, legs []fundingLeg) error {
	q := c.queries.WithTx(tx)
	for i, leg := range legs {
		var accountID []byte
		if leg.GrantAccountID != "" {
			parsed, err := ledger.ParseID(leg.GrantAccountID)
			if err != nil {
				return fmt.Errorf("parse grant account id for ledger leg %d: %w", i, err)
			}
			accountID = parsed.Bytes()
		}
		if err := q.InsertWindowLedgerLeg(ctx, store.InsertWindowLedgerLegParams{
			WindowID:          windowID,
			LegSeq:            int32(i),
			GrantID:           leg.GrantID,
			GrantAccountID:    accountID,
			ComponentSkuID:    leg.ComponentSKUID,
			ComponentBucketID: leg.ComponentBucketID,
			Source:            leg.Source,
			ScopeType:         leg.ScopeType,
			ScopeProductID:    leg.ScopeProductID,
			ScopeBucketID:     leg.ScopeBucketID,
			ScopeSkuID:        leg.ScopeSKUID,
			PlanID:            leg.PlanID,
			AmountReserved:    int64(leg.Amount),
		}); err != nil {
			return fmt.Errorf("insert window ledger leg %s[%d]: %w", windowID, i, err)
		}
	}
	return nil
}

func (c *Client) markWindowSettlementPosted(ctx context.Context, windowID string) error {
	window, err := c.loadWindow(ctx, windowID)
	if err != nil {
		return err
	}
	if window.State == "settled" {
		return nil
	}
	if window.State != "settling" {
		return nil
	}
	occurredAt := time.Now().UTC()
	if window.SettledAt != nil {
		occurredAt = window.SettledAt.UTC()
	}
	componentBilled, bucketBilled := chargeMapsFromFundingLegs(window.FundingLegs)
	return c.WithTx(ctx, "billing.window.settle.posted", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		rowsAffected, err := q.MarkWindowSettled(ctx, store.MarkWindowSettledParams{WindowID: windowID})
		if err != nil {
			return fmt.Errorf("settle billing window: %w", err)
		}
		if rowsAffected == 0 {
			return nil
		}
		if err := q.MarkPendingWindowLedgerLegsPostedOrVoided(ctx, store.MarkPendingWindowLedgerLegsPostedOrVoidedParams{WindowID: windowID}); err != nil {
			return fmt.Errorf("settle billing window ledger legs: %w", err)
		}
		if err := appendEvent(ctx, tx, q, eventFact{EventType: "billing_window_settled", AggregateType: "billing_window", AggregateID: windowID, OrgID: window.OrgID, ProductID: window.ProductID, OccurredAt: occurredAt, Payload: map[string]any{"window_id": windowID, "cycle_id": window.CycleID, "pricing_plan_id": window.PricingPlanID, "pricing_phase_id": window.PricingPhaseID, "pricing_contract_id": window.PricingContractID, "actual_quantity": window.ActualQuantity, "billable_quantity": window.BillableQuantity, "writeoff_quantity": window.WriteoffQuantity, "billed_charge_units": window.BilledChargeUnits, "writeoff_charge_units": window.WriteoffChargeUnits, "component_charge_units": componentBilled, "bucket_charge_units": bucketBilled, "ledger_settlement_posted": true}}); err != nil {
			return err
		}
		if c.runtime != nil {
			return c.runtime.EnqueueMeteringProjectWindowTx(ctx, tx, windowID)
		}
		return nil
	})
}

func (c *Client) loadReservation(ctx context.Context, windowID string) (WindowReservation, error) {
	window, err := c.loadWindow(ctx, windowID)
	if err != nil {
		return WindowReservation{}, err
	}
	return window.reservation(), nil
}

func (w persistedWindow) reservation() WindowReservation {
	return WindowReservation{WindowID: w.WindowID, OrgID: w.OrgID, ProductID: w.ProductID, PlanID: w.PricingPlanID, ActorID: w.ActorID, SourceType: w.SourceType, SourceRef: w.SourceRef, WindowSeq: w.WindowSeq, ReservationShape: w.ReservationShape, ReservedQuantity: w.ReservedQuantity, ReservedChargeUnits: w.ReservedChargeUnits, PricingPhase: w.PricingPhase, Allocation: cloneFloatMap(w.Allocation), SKURates: cloneUintMap(w.RateContext.SKURates), CostPerUnit: w.RateContext.CostPerUnit, WindowStart: w.WindowStart, ActivatedAt: w.ActivatedAt, ExpiresAt: w.ExpiresAt, RenewBy: w.RenewBy}
}

func (w persistedWindow) settleResult() SettleResult {
	settledAt := time.Time{}
	if w.SettledAt != nil {
		settledAt = *w.SettledAt
	}
	return SettleResult{WindowID: w.WindowID, ActualQuantity: w.ActualQuantity, BillableQuantity: w.BillableQuantity, WriteoffQuantity: w.WriteoffQuantity, BilledChargeUnits: w.BilledChargeUnits, WriteoffChargeUnits: w.WriteoffChargeUnits, SettledAt: settledAt}
}

func (c *Client) loadWindowBySource(ctx context.Context, orgID OrgID, productID, sourceType, sourceRef string, seq uint32) (persistedWindow, bool, error) {
	windowID, err := c.queries.GetWindowIDBySource(ctx, store.GetWindowIDBySourceParams{
		OrgID:      orgIDText(orgID),
		ProductID:  productID,
		SourceType: sourceType,
		SourceRef:  sourceRef,
		WindowSeq:  int64(seq),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return persistedWindow{}, false, nil
	}
	if err != nil {
		return persistedWindow{}, false, fmt.Errorf("load billing window by source: %w", err)
	}
	window, err := c.loadWindow(ctx, windowID)
	return window, err == nil, err
}

func (c *Client) loadWindow(ctx context.Context, windowID string) (persistedWindow, error) {
	row, err := c.queries.GetBillingWindow(ctx, store.GetBillingWindowParams{WindowID: windowID})
	if errors.Is(err, pgx.ErrNoRows) {
		return persistedWindow{}, ErrWindowNotFound
	}
	if err != nil {
		return persistedWindow{}, fmt.Errorf("load billing window %s: %w", windowID, err)
	}
	parsedOrg, err := strconv.ParseUint(row.OrgID, 10, 64)
	if err != nil {
		return persistedWindow{}, fmt.Errorf("parse window org_id %q: %w", row.OrgID, err)
	}
	w := persistedWindow{
		WindowID:            row.WindowID,
		CycleID:             row.CycleID,
		OrgID:               OrgID(parsedOrg),
		ActorID:             row.ActorID,
		ProductID:           row.ProductID,
		PricingContractID:   row.PricingContractID,
		PricingPhaseID:      row.PricingPhaseID,
		PricingPlanID:       row.PricingPlanID,
		SourceType:          row.SourceType,
		SourceRef:           row.SourceRef,
		SourceFingerprint:   row.SourceFingerprint,
		WindowSeq:           uint32(row.WindowSeq),
		State:               row.State,
		ReservationShape:    row.ReservationShape,
		ReservedQuantity:    uint32(row.ReservedQuantity),
		ActualQuantity:      uint32(row.ActualQuantity),
		BillableQuantity:    uint32(row.BillableQuantity),
		WriteoffQuantity:    uint32(row.WriteoffQuantity),
		ReservedChargeUnits: uint64(row.ReservedChargeUnits),
		BilledChargeUnits:   uint64(row.BilledChargeUnits),
		WriteoffChargeUnits: uint64(row.WriteoffChargeUnits),
		PricingPhase:        row.PricingPhase,
		WindowStart:         row.WindowStart.Time.UTC(),
		ActivatedAt:         timePtr(row.ActivatedAt),
		ExpiresAt:           row.ExpiresAt.Time.UTC(),
		RenewBy:             timePtr(row.RenewBy),
		SettledAt:           timePtr(row.SettledAt),
		CreatedAt:           row.CreatedAt.Time.UTC(),
	}
	_ = json.Unmarshal(row.Allocation, &w.Allocation)
	_ = json.Unmarshal(row.RateContext, &w.RateContext)
	_ = json.Unmarshal(row.UsageSummary, &w.UsageSummary)
	_ = json.Unmarshal(row.FundingLegs, &w.FundingLegs)
	return w, nil
}

func (c *Client) lockWindowStateTx(ctx context.Context, tx pgx.Tx, windowID string) (string, error) {
	state, err := c.queries.WithTx(tx).LockWindowState(ctx, store.LockWindowStateParams{WindowID: windowID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrWindowNotFound
	}
	if err != nil {
		return "", fmt.Errorf("lock billing window %s: %w", windowID, err)
	}
	return state, nil
}

func existingOrNewLedgerID(raw []byte) (ledger.ID, error) {
	id, err := ledger.IDFromBytes(raw)
	if err != nil {
		return ledger.ID{}, err
	}
	if id.IsZero() {
		return ledger.NewID(), nil
	}
	return id, nil
}

func (c *Client) lockOrgProductTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID string) error {
	if err := c.queries.WithTx(tx).LockOrgProductBilling(ctx, store.LockOrgProductBillingParams{LockKey: orgIDText(orgID) + ":" + productID}); err != nil {
		return fmt.Errorf("lock org product billing: %w", err)
	}
	return nil
}

func (c *Client) orgBillingStateTx(ctx context.Context, tx pgx.Tx, orgID OrgID) (state string, overagePolicy string, err error) {
	q := c.queries.WithTx(tx)
	row, err := q.GetOrgBillingState(ctx, store.GetOrgBillingStateParams{OrgID: orgIDText(orgID)})
	if errors.Is(err, pgx.ErrNoRows) {
		if err := q.InsertDefaultOrg(ctx, store.InsertDefaultOrgParams{OrgID: orgIDText(orgID), DisplayName: "Org " + orgIDText(orgID)}); err != nil {
			return "", "", fmt.Errorf("bootstrap org: %w", err)
		}
		return "active", "block", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("load org billing state: %w", err)
	}
	return row.State, row.OveragePolicy, nil
}

func (c *Client) loadPricingContextTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID string, now time.Time) (pricingContext, error) {
	q := c.queries.WithTx(tx)
	row, err := q.LoadPricingContext(ctx, store.LoadPricingContextParams{
		OrgID:     orgIDText(orgID),
		ProductID: productID,
		Now:       timestamptz(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return pricingContext{}, fmt.Errorf("no active/default plan for product %s", productID)
	}
	if err != nil {
		return pricingContext{}, fmt.Errorf("load pricing context: %w", err)
	}
	out := pricingContext{
		PlanID:        row.PlanID,
		BillingMode:   row.BillingMode,
		ContractID:    row.ContractID,
		PhaseID:       row.PhaseID,
		OveragePolicy: row.OveragePolicy,
		Currency:      row.Currency,
	}
	out.SKURates = map[string]uint64{}
	out.SKUBuckets = map[string]string{}
	out.SKUBucketOrders = map[string]int{}
	out.SKUDisplayNames = map[string]string{}
	out.SKUQuantityUnits = map[string]string{}
	out.BucketDisplayNames = map[string]string{}
	rows, err := q.ListActivePlanSKURates(ctx, store.ListActivePlanSKURatesParams{PlanID: out.PlanID, Now: timestamptz(now)})
	if err != nil {
		return pricingContext{}, fmt.Errorf("load sku rates: %w", err)
	}
	for _, sku := range rows {
		out.SKURates[sku.SkuID] = uint64(sku.UnitRate)
		out.SKUBuckets[sku.SkuID] = sku.BucketID
		out.SKUBucketOrders[sku.SkuID] = int(sku.SortOrder)
		out.SKUDisplayNames[sku.SkuID] = sku.SkuDisplayName
		out.SKUQuantityUnits[sku.SkuID] = sku.QuantityUnit
		out.BucketDisplayNames[sku.BucketID] = sku.BucketDisplayName
	}
	return out, nil
}

func (c *Client) fundReservationTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID string, componentCharges map[string]uint64, pricing pricingContext, allowPartial bool) ([]fundingLeg, error) {
	ctx, span := tracer.Start(ctx, "billing.authorization.compute")
	defer span.End()
	span.SetAttributes(attribute.String("billing.org_id", orgIDText(orgID)), attribute.String("billing.product_id", productID), attribute.Int("billing.component_count", len(componentCharges)))
	grants, err := c.grantBalancesTx(ctx, tx, orgID, productID)
	if err != nil {
		return nil, err
	}
	legs := []fundingLeg{}
	for _, skuID := range componentChargeOrder(componentCharges, pricing) {
		remaining := componentCharges[skuID]
		if remaining == 0 {
			continue
		}
		bucketID := pricing.SKUBuckets[skuID]
		for i := range grants {
			if remaining == 0 {
				break
			}
			grant := &grants[i]
			if grant.Available == 0 || !grantCoversSKU(*grant, productID, bucketID, skuID) {
				continue
			}
			amount := minUint64(remaining, grant.Available)
			grant.Available -= amount
			remaining -= amount
			legs = append(legs, fundingLeg{GrantID: grant.GrantID, GrantAccountID: grant.ledgerAccountID.String(), Amount: amount, Source: grant.Source, ScopeType: grant.ScopeType, ScopeProductID: grant.ScopeProductID, ScopeBucketID: grant.ScopeBucketID, ScopeSKUID: grant.ScopeSKUID, PlanID: grant.PlanID, ComponentSKUID: skuID, ComponentBucketID: bucketID})
		}
		if remaining > 0 && !allowPartial {
			continue
		}
	}
	span.SetAttributes(attribute.Int("billing.authorization_leg_count", len(legs)), attribute.String("billing.authorized_units", strconv.FormatUint(sumFundingLegs(legs), 10)))
	return legs, nil
}

func (c *Client) grantBalancesTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID string) ([]GrantBalance, error) {
	now, err := c.BusinessNow(ctx, c.queries.WithTx(tx), orgID, productID)
	if err != nil {
		return nil, err
	}
	rows, err := c.queries.WithTx(tx).ListGrantBalancesForReservation(ctx, store.ListGrantBalancesForReservationParams{
		OrgID:     orgIDText(orgID),
		ProductID: productID,
		Now:       timestamptz(now),
	})
	if err != nil {
		return nil, fmt.Errorf("query grants for reservation: %w", err)
	}
	out := []GrantBalance{}
	for _, row := range rows {
		accountID, err := ledger.IDFromBytes(row.AccountID)
		if err != nil {
			return nil, fmt.Errorf("parse grant account id %s: %w", row.GrantID, err)
		}
		out = append(out, GrantBalance{
			GrantID:             row.GrantID,
			OrgID:               orgID,
			ScopeType:           row.ScopeType,
			ScopeProductID:      row.ScopeProductID,
			ScopeBucketID:       row.ScopeBucketID,
			ScopeSKUID:          row.ScopeSkuID,
			OriginalAmount:      uint64(row.Amount),
			Amount:              uint64(row.Amount),
			Source:              row.Source,
			SourceReferenceID:   row.SourceReferenceID,
			EntitlementPeriodID: row.EntitlementPeriodID,
			PolicyVersion:       row.PolicyVersion,
			PlanID:              row.PlanID,
			PlanTier:            row.PlanTier,
			PlanDisplayName:     row.PlanDisplayName,
			StartsAt:            row.StartsAt.Time.UTC(),
			PeriodStart:         timePtr(row.PeriodStart),
			PeriodEnd:           timePtr(row.PeriodEnd),
			ExpiresAt:           timePtr(row.ExpiresAt),
			ledgerAccountID:     accountID,
		})
	}
	if err := c.hydrateGrantLedgerBalances(ctx, out); err != nil {
		return nil, err
	}
	authorized, err := c.grantAuthorizedUsageTx(ctx, tx, orgID)
	if err != nil {
		return nil, err
	}
	for i := range out {
		amount := authorized[out[i].GrantID]
		if amount == 0 {
			continue
		}
		out[i].Pending += amount
		if amount >= out[i].Available {
			out[i].Available = 0
			continue
		}
		out[i].Available -= amount
	}
	return grantsByFundingPriority(out), nil
}

func (c *Client) grantAuthorizedUsageTx(ctx context.Context, tx pgx.Tx, orgID OrgID) (map[string]uint64, error) {
	rows, err := c.queries.WithTx(tx).ListAuthorizedGrantUsage(ctx, store.ListAuthorizedGrantUsageParams{OrgID: orgIDText(orgID)})
	if err != nil {
		return nil, fmt.Errorf("query authorized grant usage tx: %w", err)
	}
	out := map[string]uint64{}
	for _, row := range rows {
		if row.Amount > 0 && row.GrantID.Valid {
			out[row.GrantID.String] = uint64(row.Amount)
		}
	}
	return out, nil
}

func computeWindowCharges(allocation map[string]float64, rates map[string]uint64, skuBuckets map[string]string, quantity uint32) (map[string]uint64, map[string]uint64, uint64, error) {
	components := map[string]uint64{}
	buckets := map[string]uint64{}
	costPerUnit := uint64(0)
	for skuID, units := range allocation {
		rate, ok := rates[skuID]
		if !ok {
			return nil, nil, 0, fmt.Errorf("no active rate for sku %s", skuID)
		}
		componentPerUnit, err := chargeUnitsForQuantity(skuID, units, rate, 1)
		if err != nil {
			return nil, nil, 0, err
		}
		nextCostPerUnit, err := addChargeUnits(costPerUnit, componentPerUnit, "cost_per_unit")
		if err != nil {
			return nil, nil, 0, err
		}
		charge, err := chargeUnitsForQuantity(skuID, units, rate, quantity)
		if err != nil {
			return nil, nil, 0, err
		}
		bucketID := skuBuckets[skuID]
		nextBucketCharge, err := addChargeUnits(buckets[bucketID], charge, "bucket "+bucketID)
		if err != nil {
			return nil, nil, 0, err
		}
		costPerUnit = nextCostPerUnit
		components[skuID] = charge
		buckets[bucketID] = nextBucketCharge
	}
	return components, buckets, costPerUnit, nil
}

func chargeUnitsForQuantity(skuID string, units float64, rate uint64, quantity uint32) (uint64, error) {
	if units < 0 || math.IsNaN(units) || math.IsInf(units, 0) {
		return 0, fmt.Errorf("invalid allocation for sku %s", skuID)
	}
	if units == 0 || rate == 0 || quantity == 0 {
		return 0, nil
	}
	charge := units * float64(quantity) * float64(rate)
	if math.IsNaN(charge) || math.IsInf(charge, 0) || charge >= float64(^uint64(0)) {
		return 0, fmt.Errorf("charge overflow for sku %s", skuID)
	}
	return uint64(math.Ceil(charge)), nil
}

func addChargeUnits(left, right uint64, label string) (uint64, error) {
	if right > ^uint64(0)-left {
		return 0, fmt.Errorf("charge overflow for %s", label)
	}
	return left + right, nil
}

func componentChargeOrder(componentCharges map[string]uint64, pricing pricingContext) []string {
	skus := make([]string, 0, len(componentCharges))
	for skuID := range componentCharges {
		skus = append(skus, skuID)
	}
	sort.Slice(skus, func(i, j int) bool {
		left, right := skus[i], skus[j]
		leftOrder, rightOrder := skuBucketOrder(pricing, left), skuBucketOrder(pricing, right)
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		leftBucket, rightBucket := pricing.SKUBuckets[left], pricing.SKUBuckets[right]
		if leftBucket != rightBucket {
			return leftBucket < rightBucket
		}
		return left < right
	})
	return skus
}

func skuBucketOrder(pricing pricingContext, skuID string) int {
	if pricing.SKUBucketOrders != nil {
		if order, ok := pricing.SKUBucketOrders[skuID]; ok {
			return order
		}
	}
	return int(^uint(0) >> 1)
}

func (c *Client) projectMeteringForWindow(ctx context.Context, w persistedWindow) (time.Time, error) {
	if c.ch == nil || w.State != "settled" {
		return time.Time{}, nil
	}
	ctx, span := tracer.Start(ctx, "billing.metering.project")
	defer span.End()
	span.SetAttributes(attribute.String("billing.window_id", w.WindowID), attribute.String("billing.org_id", orgIDText(w.OrgID)), attribute.String("billing.product_id", w.ProductID), attribute.Int64("billing.actual_quantity", int64(w.ActualQuantity)))
	row, err := meteringRowForWindow(w, time.Now().UTC())
	if err != nil {
		return time.Time{}, err
	}
	batch, err := c.ch.PrepareBatch(ctx, "INSERT INTO verself.metering")
	if err != nil {
		return time.Time{}, fmt.Errorf("prepare metering batch: %w", err)
	}
	if err := appendMeteringRow(batch, row); err != nil {
		return time.Time{}, err
	}
	if err := batch.Send(); err != nil {
		return time.Time{}, fmt.Errorf("send metering batch: %w", err)
	}
	return row.RecordedAt, nil
}

func meteringRowForWindow(w persistedWindow, recordedAt time.Time) (meteringRow, error) {
	componentQuantities := map[string]float64{}
	componentCharges := map[string]uint64{}
	bucketCharges := map[string]uint64{}
	for skuID, units := range w.Allocation {
		componentQuantities[skuID] = units * float64(w.BillableQuantity)
		rate, ok := w.RateContext.SKURates[skuID]
		if !ok {
			return meteringRow{}, fmt.Errorf("no active rate for sku %s", skuID)
		}
		charge, err := chargeUnitsForQuantity(skuID, units, rate, w.BillableQuantity)
		if err != nil {
			return meteringRow{}, err
		}
		componentCharges[skuID] = charge
		bucketID := w.RateContext.SKUBuckets[skuID]
		nextBucketCharge, err := addChargeUnits(bucketCharges[bucketID], charge, "bucket "+bucketID)
		if err != nil {
			return meteringRow{}, err
		}
		bucketCharges[bucketID] = nextBucketCharge
	}
	bySource := map[string]uint64{}
	componentBySource := map[string]map[string]uint64{
		"free_tier": {}, "contract": {}, "purchase": {}, "promo": {}, "refund": {}, "receivable": {}, "adjustment": {},
	}
	for _, leg := range w.FundingLegs {
		bySource[leg.Source] += leg.Amount
		if leg.ComponentSKUID != "" {
			if componentBySource[leg.Source] == nil {
				componentBySource[leg.Source] = map[string]uint64{}
			}
			componentBySource[leg.Source][leg.ComponentSKUID] += leg.Amount
		}
	}
	endedAt := w.WindowStart.Add(time.Duration(w.ActualQuantity) * time.Millisecond)
	if w.ReservationShape == ReservationShapeCount {
		endedAt = w.WindowStart
	}
	if w.SettledAt != nil && (w.ActualQuantity == 0 || w.ReservationShape == ReservationShapeCount) {
		endedAt = *w.SettledAt
	}
	return meteringRow{WindowID: w.WindowID, OrgID: orgIDText(w.OrgID), ActorID: w.ActorID, ProductID: w.ProductID, SourceType: w.SourceType, SourceRef: w.SourceRef, WindowSeq: w.WindowSeq, ReservationShape: w.ReservationShape, StartedAt: w.WindowStart, EndedAt: endedAt, ReservedQuantity: uint64(w.ReservedQuantity), ActualQuantity: uint64(w.ActualQuantity), BillableQuantity: uint64(w.BillableQuantity), WriteoffQuantity: uint64(w.WriteoffQuantity), CycleID: w.CycleID, PricingContractID: w.PricingContractID, PricingPhaseID: w.PricingPhaseID, PricingPlanID: w.PricingPlanID, PricingPhase: w.PricingPhase, Dimensions: cloneFloatMap(w.Allocation), ComponentQuantities: componentQuantities, ComponentChargeUnits: componentCharges, BucketChargeUnits: bucketCharges, ChargeUnits: w.BilledChargeUnits, WriteoffChargeUnits: w.WriteoffChargeUnits, FreeTierUnits: bySource["free_tier"], ContractUnits: bySource["contract"], PurchaseUnits: bySource["purchase"], PromoUnits: bySource["promo"], RefundUnits: bySource["refund"], ReceivableUnits: bySource["receivable"], AdjustmentUnits: bySource["adjustment"], ComponentFreeTierUnits: componentBySource["free_tier"], ComponentContractUnits: componentBySource["contract"], ComponentPurchaseUnits: componentBySource["purchase"], ComponentPromoUnits: componentBySource["promo"], ComponentRefundUnits: componentBySource["refund"], ComponentReceivableUnits: componentBySource["receivable"], ComponentAdjustmentUnits: componentBySource["adjustment"], UsageEvidence: usageEvidence(w.UsageSummary), CostPerUnit: w.RateContext.CostPerUnit, RecordedAt: recordedAt}, nil
}

func appendMeteringRow(batch driver.Batch, row meteringRow) error {
	if err := batch.AppendStruct(&row); err != nil {
		return fmt.Errorf("append metering row: %w", err)
	}
	return nil
}

func writeoffReason(writeoff uint32, w persistedWindow) string {
	if writeoff == 0 {
		return ""
	}
	if w.PricingContractID == "" {
		return "free_tier_overage_absorbed"
	}
	return "paid_hard_cap_overage_absorbed"
}

func usageEvidence(summary map[string]any) map[string]uint64 {
	out := map[string]uint64{}
	for key, value := range summary {
		switch typed := value.(type) {
		case uint64:
			out[key] = typed
		case uint32:
			out[key] = uint64(typed)
		case int:
			if typed >= 0 {
				out[key] = uint64(typed)
			}
		case int64:
			if typed >= 0 {
				out[key] = uint64(typed)
			}
		case float64:
			if typed >= 0 {
				out[key] = uint64(typed)
			}
		}
	}
	return out
}

func timePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	v := value.Time.UTC()
	return &v
}

func cloneFloatMap(in map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneUintMap(in map[string]uint64) map[string]uint64 {
	out := make(map[string]uint64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func sumUint64Map(in map[string]uint64) uint64 {
	var out uint64
	for _, value := range in {
		out += value
	}
	return out
}

func sumFundingLegs(legs []fundingLeg) uint64 {
	var out uint64
	for _, leg := range legs {
		out += leg.Amount
	}
	return out
}

func chargeMapsFromFundingLegs(legs []fundingLeg) (map[string]uint64, map[string]uint64) {
	components := map[string]uint64{}
	buckets := map[string]uint64{}
	for _, leg := range legs {
		if leg.Amount == 0 {
			continue
		}
		if leg.ComponentSKUID != "" {
			components[leg.ComponentSKUID] += leg.Amount
		}
		if leg.ComponentBucketID != "" {
			buckets[leg.ComponentBucketID] += leg.Amount
		}
	}
	return components, buckets
}

func fundingLegsForComponent(legs []fundingLeg, skuID string) uint64 {
	var out uint64
	for _, leg := range legs {
		if leg.ComponentSKUID == skuID {
			out += leg.Amount
		}
	}
	return out
}

func minUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
