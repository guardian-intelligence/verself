package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/forge-metal/billing-service/internal/billing/ledger"
	"github.com/forge-metal/billing-service/internal/store"
)

const (
	defaultWindowMillis  uint32 = 5 * 60 * 1000
	defaultRenewBefore   uint32 = 30 * 1000
	pricingPhaseIncluded        = "included"
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
	if existing, ok, err := c.loadWindowBySource(ctx, req.ProductID, req.SourceType, req.SourceRef, req.WindowSeq); err != nil {
		return WindowReservation{}, err
	} else if ok {
		if existing.State == "voided" || existing.State == "settled" {
			return WindowReservation{}, fmt.Errorf("%w: existing window %s is %s", ErrWindowNotReserved, existing.WindowID, existing.State)
		}
		if existing.State == "reserving" || existing.State == "denied" || existing.State == "failed" {
			return WindowReservation{}, fmt.Errorf("%w: existing window %s is %s", ErrWindowNotReserved, existing.WindowID, existing.State)
		}
		return existing.reservation(), nil
	}
	if err := c.EnsureCurrentEntitlements(ctx, req.OrgID, req.ProductID); err != nil {
		return WindowReservation{}, err
	}

	var reserved persistedWindow
	var reserveCommandID string
	err := c.WithTx(ctx, "billing.window.reserve", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
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
		cycle, err := c.ensureOpenBillingCycleTx(ctx, tx, req.OrgID, req.ProductID, now)
		if err != nil {
			return err
		}
		pricing, err := c.loadPricingContextTx(ctx, tx, req.OrgID, req.ProductID, now)
		if err != nil {
			return err
		}
		quantity := defaultWindowMillis
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
			for _, skuID := range sortedKeys(componentCharges) {
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
		windowID := billingWindowID(req.ProductID, req.SourceType, req.SourceRef, req.WindowSeq)
		ledgerCorrelationID := ledger.NewID()
		reservePayload, err := c.reserveWindowLedgerPayloadTx(ctx, tx, legs, ledgerCorrelationID, now)
		if err != nil {
			return err
		}
		expiresAt := now.Add(time.Duration(quantity) * time.Millisecond)
		renewBy := expiresAt.Add(-time.Duration(defaultRenewBefore) * time.Millisecond)
		if !renewBy.After(now) {
			renewBy = expiresAt
		}
		billingJobID := ""
		if req.BillingJobID > 0 {
			billingJobID = strconv.FormatInt(req.BillingJobID, 10)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO billing_windows (
				window_id, cycle_id, org_id, actor_id, product_id, pricing_contract_id, pricing_phase_id, pricing_plan_id,
				source_type, source_ref, billing_job_id, window_seq, state, reservation_shape, reserved_quantity,
				reserved_charge_units, pricing_phase, allocation, rate_context, funding_legs, ledger_correlation_id, window_start, expires_at, renew_by
			) VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),$9,$10,NULLIF($11,''),$12,'reserving','time',$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
		`, windowID, cycle.CycleID, orgIDText(req.OrgID), req.ActorID, req.ProductID, pricing.ContractID, pricing.PhaseID, pricing.PlanID, req.SourceType, req.SourceRef, billingJobID, int64(req.WindowSeq), int64(quantity), int64(chargeUnits), pricingPhaseIncluded, allocationJSON, rateJSON, fundingJSON, ledgerCorrelationID.Bytes(), now, expiresAt, renewBy)
		if err != nil {
			return fmt.Errorf("insert billing window: %w", err)
		}
		if err := c.insertWindowLedgerLegsTx(ctx, tx, windowID, legs); err != nil {
			return err
		}
		reserveCommandID, _, err = c.createLedgerCommandTx(ctx, tx, "reserve_window", "billing_window", windowID, req.OrgID, req.ProductID, "reserve_window:"+windowID, reservePayload)
		if err != nil {
			return err
		}
		reserved = persistedWindow{WindowID: windowID, CycleID: cycle.CycleID, OrgID: req.OrgID, ActorID: req.ActorID, ProductID: req.ProductID, PricingContractID: pricing.ContractID, PricingPhaseID: pricing.PhaseID, PricingPlanID: pricing.PlanID, SourceType: req.SourceType, SourceRef: req.SourceRef, WindowSeq: req.WindowSeq, State: "reserved", ReservationShape: "time", ReservedQuantity: quantity, ReservedChargeUnits: chargeUnits, PricingPhase: pricingPhaseIncluded, Allocation: cloneFloatMap(req.Allocation), RateContext: pricing, FundingLegs: legs, WindowStart: now, ExpiresAt: expiresAt, RenewBy: &renewBy}
		return appendEvent(ctx, tx, q, eventFact{EventType: "billing_window_reserve_requested", AggregateType: "billing_window", AggregateID: windowID, OrgID: req.OrgID, ProductID: req.ProductID, OccurredAt: now, Payload: map[string]any{"window_id": windowID, "cycle_id": cycle.CycleID, "pricing_plan_id": pricing.PlanID, "pricing_phase_id": pricing.PhaseID, "pricing_contract_id": pricing.ContractID, "source_type": req.SourceType, "source_ref": req.SourceRef, "window_seq": req.WindowSeq, "charge_units": chargeUnits, "component_charge_units": componentCharges, "bucket_charge_units": bucketCharges, "ledger_command_id": reserveCommandID}})
	})
	if err != nil {
		return WindowReservation{}, err
	}
	if err := c.dispatchLedgerCommand(ctx, reserveCommandID); err != nil {
		_ = c.markWindowReservationFailed(ctx, reserved.WindowID, err)
		return WindowReservation{}, err
	}
	if err := c.markWindowReservationPosted(ctx, reserved); err != nil {
		return WindowReservation{}, err
	}
	return reserved.reservation(), nil
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
	renewBy := activatedAt.Add(time.Duration(window.ReservedQuantity-defaultRenewBefore) * time.Millisecond)
	if !renewBy.After(activatedAt) {
		renewBy = activatedAt.Add(time.Duration(window.ReservedQuantity) * time.Millisecond)
	}
	err = c.WithTx(ctx, "billing.window.activate", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		ct, err := tx.Exec(ctx, `
			UPDATE billing_windows
			SET state = 'active', window_start = $2, activated_at = $2, expires_at = $3, renew_by = $4
			WHERE window_id = $1 AND state = 'reserved' AND activated_at IS NULL
		`, windowID, activatedAt, activatedAt.Add(time.Duration(window.ReservedQuantity)*time.Millisecond), renewBy)
		if err != nil {
			return fmt.Errorf("activate billing window: %w", err)
		}
		if ct.RowsAffected() == 0 {
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
	componentBilled, _, costPerUnit, err := computeWindowCharges(window.Allocation, window.RateContext.SKURates, window.RateContext.SKUBuckets, billable)
	if err != nil {
		return SettleResult{}, err
	}
	billedUnits := sumUint64Map(componentBilled)
	writeoffUnits := uint64(writeoff) * costPerUnit
	settledAt := time.Now().UTC()
	usageJSON, err := json.Marshal(usageSummary)
	if err != nil {
		return SettleResult{}, fmt.Errorf("marshal usage summary: %w", err)
	}
	settledFundingLegs := settleFundingLegs(window.FundingLegs, componentBilled)
	fundingJSON, err := fundingLegsJSON(settledFundingLegs)
	if err != nil {
		return SettleResult{}, err
	}
	var settleCommandID string
	err = c.WithTx(ctx, "billing.window.settle.prepare", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		ct, err := tx.Exec(ctx, `
			UPDATE billing_windows
			SET state = 'settling', actual_quantity = $2, billable_quantity = $3, writeoff_quantity = $4,
			    billed_charge_units = $5, writeoff_charge_units = $6, writeoff_reason = $7,
			    usage_summary = $8, funding_legs = $9, settled_at = $10
			WHERE window_id = $1 AND state IN ('reserved','active')
		`, windowID, int64(actualQuantity), int64(billable), int64(writeoff), int64(billedUnits), int64(writeoffUnits), writeoffReason(writeoff, window), usageJSON, fundingJSON, settledAt)
		if err != nil {
			return fmt.Errorf("prepare billing window settlement: %w", err)
		}
		if ct.RowsAffected() == 0 {
			return nil
		}
		settlePayload, err := c.settleWindowLedgerPayloadTx(ctx, tx, windowID, settledFundingLegs, settledAt)
		if err != nil {
			return err
		}
		settleCommandID, _, err = c.createLedgerCommandTx(ctx, tx, "settle_window", "billing_window", windowID, window.OrgID, window.ProductID, "settle_window:"+windowID, settlePayload)
		if err != nil {
			return err
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "billing_window_settle_requested", AggregateType: "billing_window", AggregateID: windowID, OrgID: window.OrgID, ProductID: window.ProductID, OccurredAt: settledAt, Payload: map[string]any{"window_id": windowID, "cycle_id": window.CycleID, "actual_quantity": actualQuantity, "billable_quantity": billable, "billed_charge_units": billedUnits, "ledger_command_id": settleCommandID}})
	})
	if err != nil {
		return SettleResult{}, err
	}
	if err := c.dispatchLedgerCommand(ctx, settleCommandID); err != nil {
		return SettleResult{}, err
	}
	if err := c.markWindowSettlementPosted(ctx, windowID); err != nil {
		return SettleResult{}, err
	}
	settled, err := c.loadWindow(ctx, windowID)
	if err != nil {
		return SettleResult{}, err
	}
	if c.runtime == nil {
		if err := c.projectMeteringForWindow(ctx, settled); err != nil {
			c.logger.WarnContext(ctx, "billing metering projection failed", "window_id", windowID, "error", err)
			_, _ = c.pg.Exec(ctx, `UPDATE billing_windows SET last_projection_error = $2 WHERE window_id = $1`, windowID, err.Error())
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
	rows, err := c.pg.Query(ctx, `
		SELECT window_id
		FROM billing_windows
		WHERE state = 'settled'
		  AND metering_projected_at IS NULL
		ORDER BY settled_at, window_id
		LIMIT $1
	`, limit)
	if err != nil {
		return 0, fmt.Errorf("query pending metering windows: %w", err)
	}
	defer rows.Close()
	windowIDs := []string{}
	for rows.Next() {
		var windowID string
		if err := rows.Scan(&windowID); err != nil {
			return 0, fmt.Errorf("scan pending metering window: %w", err)
		}
		windowIDs = append(windowIDs, windowID)
	}
	if err := rows.Err(); err != nil {
		return 0, err
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
	var state string
	var projectedAt pgtype.Timestamptz
	err := c.pg.QueryRow(ctx, `SELECT state, metering_projected_at FROM billing_windows WHERE window_id = $1`, windowID).Scan(&state, &projectedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrWindowNotFound
	}
	if err != nil {
		return false, fmt.Errorf("load metering projection state: %w", err)
	}
	if state != "settled" || projectedAt.Valid {
		return false, nil
	}
	settled, err := c.loadWindow(ctx, windowID)
	if err != nil {
		return false, err
	}
	if err := c.projectMeteringForWindow(ctx, settled); err != nil {
		c.logger.WarnContext(ctx, "billing metering projection failed", "window_id", windowID, "error", err)
		_, _ = c.pg.Exec(ctx, `UPDATE billing_windows SET last_projection_error = $2 WHERE window_id = $1`, windowID, err.Error())
		return false, err
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
	var voidCommandID string
	if err := c.WithTx(ctx, "billing.window.void.prepare", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		ct, err := tx.Exec(ctx, `UPDATE billing_windows SET state = 'voiding' WHERE window_id = $1 AND state IN ('reserved','active')`, windowID)
		if err != nil {
			return fmt.Errorf("prepare billing window void: %w", err)
		}
		if ct.RowsAffected() == 0 {
			return nil
		}
		payload, err := c.voidWindowLedgerPayloadTx(ctx, tx, windowID, now)
		if err != nil {
			return err
		}
		voidCommandID, _, err = c.createLedgerCommandTx(ctx, tx, "void_window", "billing_window", windowID, window.OrgID, window.ProductID, "void_window:"+windowID, payload)
		if err != nil {
			return err
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "billing_window_void_requested", AggregateType: "billing_window", AggregateID: windowID, OrgID: window.OrgID, ProductID: window.ProductID, OccurredAt: now, Payload: map[string]any{"window_id": windowID, "cycle_id": window.CycleID, "ledger_command_id": voidCommandID}})
	}); err != nil {
		return err
	}
	if err := c.dispatchLedgerCommand(ctx, voidCommandID); err != nil {
		return err
	}
	return c.markWindowVoidPosted(ctx, windowID)
}

func (c *Client) reserveWindowLedgerPayloadTx(ctx context.Context, tx pgx.Tx, legs []fundingLeg, correlationID ledger.ID, businessTime time.Time) (ledger.CommandPayload, error) {
	if correlationID.IsZero() {
		return ledger.CommandPayload{}, fmt.Errorf("%w: reservation correlation id is required", ledger.ErrInvalidCommand)
	}
	operators, err := c.operatorLedgerAccountsTx(ctx, tx)
	if err != nil {
		return ledger.CommandPayload{}, err
	}
	revenueID, ok := operators["operator_revenue"]
	if !ok {
		return ledger.CommandPayload{}, fmt.Errorf("operator revenue ledger account is not bootstrapped")
	}
	timeoutSeconds := uint32(c.cfg.PendingTimeout / time.Second)
	if timeoutSeconds == 0 {
		timeoutSeconds = 1
	}
	transfers := make([]ledger.TransferPayload, 0, len(legs))
	for _, leg := range legs {
		if leg.GrantID == "" {
			continue
		}
		accountID, err := ledger.ParseID(leg.GrantAccountID)
		if err != nil {
			return ledger.CommandPayload{}, fmt.Errorf("parse reservation grant account id for grant %s: %w", leg.GrantID, err)
		}
		reservationID, err := ledger.ParseID(leg.ReservationID)
		if err != nil {
			return ledger.CommandPayload{}, fmt.Errorf("parse reservation transfer id for grant %s: %w", leg.GrantID, err)
		}
		transfers = append(transfers, ledger.PendingReservationTransfer(reservationID, accountID, revenueID, leg.Amount, correlationID, unixMillis(businessTime), timeoutSeconds))
	}
	ledger.LinkTransfers(transfers)
	return ledger.CommandPayload{Transfers: transfers}, nil
}

func (c *Client) settleWindowLedgerPayloadTx(ctx context.Context, tx pgx.Tx, windowID string, settledLegs []fundingLeg, businessTime time.Time) (ledger.CommandPayload, error) {
	var correlationRaw []byte
	if err := tx.QueryRow(ctx, `SELECT ledger_correlation_id FROM billing_windows WHERE window_id = $1`, windowID).Scan(&correlationRaw); err != nil {
		return ledger.CommandPayload{}, fmt.Errorf("load window ledger correlation id: %w", err)
	}
	correlationID, err := ledger.IDFromBytes(correlationRaw)
	if err != nil {
		return ledger.CommandPayload{}, fmt.Errorf("parse window ledger correlation id: %w", err)
	}
	postedByReservation := map[string]uint64{}
	for _, leg := range settledLegs {
		if leg.ReservationID != "" {
			postedByReservation[leg.ReservationID] += leg.Amount
		}
	}
	rows, err := tx.Query(ctx, `
		SELECT leg_seq, COALESCE(grant_id, ''), reservation_transfer_id, amount_reserved
		FROM billing_window_ledger_legs
		WHERE window_id = $1 AND state = 'pending'
		ORDER BY leg_seq
		FOR UPDATE
	`, windowID)
	if err != nil {
		return ledger.CommandPayload{}, fmt.Errorf("query pending window ledger legs: %w", err)
	}
	type pendingLeg struct {
		legSeq         int
		grantID        string
		reservationRaw []byte
		reservedAmount int64
	}
	pending := []pendingLeg{}
	for rows.Next() {
		var leg pendingLeg
		if err := rows.Scan(&leg.legSeq, &leg.grantID, &leg.reservationRaw, &leg.reservedAmount); err != nil {
			return ledger.CommandPayload{}, fmt.Errorf("scan pending window ledger leg: %w", err)
		}
		pending = append(pending, leg)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ledger.CommandPayload{}, err
	}
	rows.Close()
	transfers := []ledger.TransferPayload{}
	for _, leg := range pending {
		if leg.grantID == "" {
			_, err := tx.Exec(ctx, `
				UPDATE billing_window_ledger_legs
				SET amount_posted = amount_reserved, amount_voided = 0
				WHERE window_id = $1 AND leg_seq = $2
			`, windowID, leg.legSeq)
			if err != nil {
				return ledger.CommandPayload{}, fmt.Errorf("mark receivable window leg settled: %w", err)
			}
			continue
		}
		reservationID, err := ledger.IDFromBytes(leg.reservationRaw)
		if err != nil {
			return ledger.CommandPayload{}, fmt.Errorf("parse pending transfer for window leg %d: %w", leg.legSeq, err)
		}
		postedAmount := postedByReservation[reservationID.String()]
		if postedAmount > uint64(leg.reservedAmount) {
			return ledger.CommandPayload{}, fmt.Errorf("window leg %d posted amount %d exceeds reserved amount %d", leg.legSeq, postedAmount, leg.reservedAmount)
		}
		if postedAmount > 0 {
			settlementID := ledger.NewID()
			transfers = append(transfers, ledger.PostPendingTransfer(settlementID, reservationID, postedAmount, correlationID, unixMillis(businessTime)))
			_, err = tx.Exec(ctx, `
				UPDATE billing_window_ledger_legs
				SET settlement_transfer_id = $3, amount_posted = $4, amount_voided = $5
				WHERE window_id = $1 AND leg_seq = $2
			`, windowID, leg.legSeq, settlementID.Bytes(), int64(postedAmount), leg.reservedAmount-int64(postedAmount))
			if err != nil {
				return ledger.CommandPayload{}, fmt.Errorf("store settlement transfer for window leg %d: %w", leg.legSeq, err)
			}
			continue
		}
		voidID := ledger.NewID()
		transfers = append(transfers, ledger.VoidPendingTransfer(voidID, reservationID, correlationID, unixMillis(businessTime)))
		_, err = tx.Exec(ctx, `
			UPDATE billing_window_ledger_legs
			SET void_transfer_id = $3, amount_posted = 0, amount_voided = amount_reserved
			WHERE window_id = $1 AND leg_seq = $2
		`, windowID, leg.legSeq, voidID.Bytes())
		if err != nil {
			return ledger.CommandPayload{}, fmt.Errorf("store void transfer for window leg %d: %w", leg.legSeq, err)
		}
	}
	ledger.LinkTransfers(transfers)
	return ledger.CommandPayload{Transfers: transfers}, nil
}

func (c *Client) voidWindowLedgerPayloadTx(ctx context.Context, tx pgx.Tx, windowID string, businessTime time.Time) (ledger.CommandPayload, error) {
	var correlationRaw []byte
	if err := tx.QueryRow(ctx, `SELECT ledger_correlation_id FROM billing_windows WHERE window_id = $1`, windowID).Scan(&correlationRaw); err != nil {
		return ledger.CommandPayload{}, fmt.Errorf("load window ledger correlation id: %w", err)
	}
	correlationID, err := ledger.IDFromBytes(correlationRaw)
	if err != nil {
		return ledger.CommandPayload{}, fmt.Errorf("parse window ledger correlation id: %w", err)
	}
	rows, err := tx.Query(ctx, `
		SELECT leg_seq, COALESCE(grant_id, ''), reservation_transfer_id
		FROM billing_window_ledger_legs
		WHERE window_id = $1 AND state = 'pending'
		ORDER BY leg_seq
		FOR UPDATE
	`, windowID)
	if err != nil {
		return ledger.CommandPayload{}, fmt.Errorf("query pending window ledger legs for void: %w", err)
	}
	type voidLeg struct {
		legSeq         int
		grantID        string
		reservationRaw []byte
	}
	pending := []voidLeg{}
	for rows.Next() {
		var leg voidLeg
		if err := rows.Scan(&leg.legSeq, &leg.grantID, &leg.reservationRaw); err != nil {
			return ledger.CommandPayload{}, fmt.Errorf("scan pending window ledger leg for void: %w", err)
		}
		pending = append(pending, leg)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ledger.CommandPayload{}, err
	}
	rows.Close()
	transfers := []ledger.TransferPayload{}
	for _, leg := range pending {
		if leg.grantID == "" {
			_, err := tx.Exec(ctx, `
				UPDATE billing_window_ledger_legs
				SET amount_posted = 0, amount_voided = amount_reserved
				WHERE window_id = $1 AND leg_seq = $2
			`, windowID, leg.legSeq)
			if err != nil {
				return ledger.CommandPayload{}, fmt.Errorf("mark receivable window leg voided: %w", err)
			}
			continue
		}
		reservationID, err := ledger.IDFromBytes(leg.reservationRaw)
		if err != nil {
			return ledger.CommandPayload{}, fmt.Errorf("parse pending transfer for void window leg %d: %w", leg.legSeq, err)
		}
		voidID := ledger.NewID()
		transfers = append(transfers, ledger.VoidPendingTransfer(voidID, reservationID, correlationID, unixMillis(businessTime)))
		_, err = tx.Exec(ctx, `
			UPDATE billing_window_ledger_legs
			SET void_transfer_id = $3, amount_posted = 0, amount_voided = amount_reserved
			WHERE window_id = $1 AND leg_seq = $2
		`, windowID, leg.legSeq, voidID.Bytes())
		if err != nil {
			return ledger.CommandPayload{}, fmt.Errorf("store void transfer for window leg %d: %w", leg.legSeq, err)
		}
	}
	ledger.LinkTransfers(transfers)
	return ledger.CommandPayload{Transfers: transfers}, nil
}

func (c *Client) insertWindowLedgerLegsTx(ctx context.Context, tx pgx.Tx, windowID string, legs []fundingLeg) error {
	for i, leg := range legs {
		state := "pending"
		if leg.GrantID != "" {
			state = "pending_tb"
		}
		var accountID any
		var reservationID any
		if leg.GrantAccountID != "" {
			parsed, err := ledger.ParseID(leg.GrantAccountID)
			if err != nil {
				return fmt.Errorf("parse grant account id for ledger leg %d: %w", i, err)
			}
			accountID = parsed.Bytes()
		}
		if leg.ReservationID != "" {
			parsed, err := ledger.ParseID(leg.ReservationID)
			if err != nil {
				return fmt.Errorf("parse reservation id for ledger leg %d: %w", i, err)
			}
			reservationID = parsed.Bytes()
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO billing_window_ledger_legs (
				window_id, leg_seq, grant_id, grant_account_id, reservation_transfer_id,
				component_sku_id, component_bucket_id, source, scope_type, scope_product_id, scope_bucket_id, scope_sku_id,
				plan_id, amount_reserved, state
			) VALUES ($1,$2,NULLIF($3,''),$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
			ON CONFLICT (window_id, leg_seq) DO NOTHING
		`, windowID, i, leg.GrantID, accountID, reservationID, leg.ComponentSKUID, leg.ComponentBucketID, leg.Source, leg.ScopeType, leg.ScopeProductID, leg.ScopeBucketID, leg.ScopeSKUID, leg.PlanID, int64(leg.Amount), state)
		if err != nil {
			return fmt.Errorf("insert window ledger leg %s[%d]: %w", windowID, i, err)
		}
	}
	return nil
}

func (c *Client) markWindowReservationPosted(ctx context.Context, window persistedWindow) error {
	return c.WithTx(ctx, "billing.window.reserve.posted", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		ct, err := tx.Exec(ctx, `
			UPDATE billing_windows
			SET state = 'reserved'
			WHERE window_id = $1 AND state = 'reserving'
		`, window.WindowID)
		if err != nil {
			return fmt.Errorf("mark reserved window posted: %w", err)
		}
		if ct.RowsAffected() == 0 {
			return nil
		}
		_, err = tx.Exec(ctx, `
			UPDATE billing_window_ledger_legs
			SET state = 'pending'
			WHERE window_id = $1 AND state = 'pending_tb'
		`, window.WindowID)
		if err != nil {
			return fmt.Errorf("mark reserved window ledger legs pending: %w", err)
		}
		componentCharges, bucketCharges, _, _ := computeWindowCharges(window.Allocation, window.RateContext.SKURates, window.RateContext.SKUBuckets, window.ReservedQuantity)
		return appendEvent(ctx, tx, q, eventFact{
			EventType:     "billing_window_reserved",
			AggregateType: "billing_window",
			AggregateID:   window.WindowID,
			OrgID:         window.OrgID,
			ProductID:     window.ProductID,
			OccurredAt:    window.WindowStart,
			Payload: map[string]any{
				"window_id":                 window.WindowID,
				"cycle_id":                  window.CycleID,
				"pricing_plan_id":           window.PricingPlanID,
				"pricing_phase_id":          window.PricingPhaseID,
				"pricing_contract_id":       window.PricingContractID,
				"source_type":               window.SourceType,
				"source_ref":                window.SourceRef,
				"window_seq":                window.WindowSeq,
				"charge_units":              window.ReservedChargeUnits,
				"component_charge_units":    componentCharges,
				"bucket_charge_units":       bucketCharges,
				"ledger_reservation_posted": true,
			},
		})
	})
}

func (c *Client) markWindowReservationFailed(ctx context.Context, windowID string, cause error) error {
	state := "failed"
	if errors.Is(cause, ledger.ErrInsufficientBalance) {
		state = "denied"
	}
	return c.WithTx(ctx, "billing.window.reserve.failed", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		var orgText, productID string
		err := tx.QueryRow(ctx, `
			UPDATE billing_windows
			SET state = $2, last_projection_error = $3
			WHERE window_id = $1 AND state = 'reserving'
			RETURNING org_id, product_id
		`, windowID, state, cause.Error()).Scan(&orgText, &productID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("mark reservation failed: %w", err)
		}
		orgID, err := parseOrgID(orgText)
		if err != nil {
			return err
		}
		return appendEvent(ctx, tx, q, eventFact{
			EventType:     "billing_window_reserve_failed",
			AggregateType: "billing_window",
			AggregateID:   windowID,
			OrgID:         orgID,
			ProductID:     productID,
			OccurredAt:    time.Now().UTC(),
			Payload:       map[string]any{"window_id": windowID, "state": state, "error": cause.Error()},
		})
	})
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
		ct, err := tx.Exec(ctx, `UPDATE billing_windows SET state = 'settled' WHERE window_id = $1 AND state = 'settling'`, windowID)
		if err != nil {
			return fmt.Errorf("settle billing window: %w", err)
		}
		if ct.RowsAffected() == 0 {
			return nil
		}
		_, err = tx.Exec(ctx, `
			UPDATE billing_window_ledger_legs
			SET state = CASE WHEN amount_posted > 0 THEN 'posted' ELSE 'voided' END
			WHERE window_id = $1 AND state = 'pending'
		`, windowID)
		if err != nil {
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

func (c *Client) markWindowVoidPosted(ctx context.Context, windowID string) error {
	window, err := c.loadWindow(ctx, windowID)
	if err != nil {
		return err
	}
	if window.State == "voided" {
		return nil
	}
	if window.State != "voiding" {
		return nil
	}
	now := time.Now().UTC()
	return c.WithTx(ctx, "billing.window.void.posted", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		ct, err := tx.Exec(ctx, `UPDATE billing_windows SET state = 'voided' WHERE window_id = $1 AND state = 'voiding'`, windowID)
		if err != nil {
			return fmt.Errorf("void billing window: %w", err)
		}
		if ct.RowsAffected() == 0 {
			return nil
		}
		_, err = tx.Exec(ctx, `
			UPDATE billing_window_ledger_legs
			SET state = 'voided'
			WHERE window_id = $1 AND state = 'pending'
		`, windowID)
		if err != nil {
			return fmt.Errorf("void billing window ledger legs: %w", err)
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "billing_window_voided", AggregateType: "billing_window", AggregateID: windowID, OrgID: window.OrgID, ProductID: window.ProductID, OccurredAt: now, Payload: map[string]any{"window_id": windowID, "cycle_id": window.CycleID, "pricing_plan_id": window.PricingPlanID, "pricing_phase_id": window.PricingPhaseID, "pricing_contract_id": window.PricingContractID, "ledger_void_posted": true}})
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

func (c *Client) loadWindowBySource(ctx context.Context, productID, sourceType, sourceRef string, seq uint32) (persistedWindow, bool, error) {
	windowID := ""
	err := c.pg.QueryRow(ctx, `SELECT window_id FROM billing_windows WHERE product_id = $1 AND source_type = $2 AND source_ref = $3 AND window_seq = $4`, productID, sourceType, sourceRef, int64(seq)).Scan(&windowID)
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
	var w persistedWindow
	var orgIDTextValue string
	var windowSeq, reservedQuantity, actualQuantity, billableQuantity, writeoffQuantity, reservedUnits, billedUnits, writeoffUnits int64
	var allocationBytes, rateBytes, usageBytes, fundingBytes []byte
	var pricingContractID, pricingPhaseID, pricingPlanID pgtype.Text
	var activatedAt, renewBy, settledAt pgtype.Timestamptz
	err := c.pg.QueryRow(ctx, `
		SELECT window_id, cycle_id, org_id, actor_id, product_id, COALESCE(pricing_contract_id,''), COALESCE(pricing_phase_id,''), COALESCE(pricing_plan_id,''),
		       source_type, source_ref, window_seq, state, reservation_shape, reserved_quantity, actual_quantity, billable_quantity, writeoff_quantity,
		       reserved_charge_units, billed_charge_units, writeoff_charge_units, pricing_phase, allocation, rate_context, usage_summary, funding_legs,
		       window_start, activated_at, expires_at, renew_by, settled_at, created_at
		FROM billing_windows
		WHERE window_id = $1
	`, windowID).Scan(&w.WindowID, &w.CycleID, &orgIDTextValue, &w.ActorID, &w.ProductID, &pricingContractID, &pricingPhaseID, &pricingPlanID, &w.SourceType, &w.SourceRef, &windowSeq, &w.State, &w.ReservationShape, &reservedQuantity, &actualQuantity, &billableQuantity, &writeoffQuantity, &reservedUnits, &billedUnits, &writeoffUnits, &w.PricingPhase, &allocationBytes, &rateBytes, &usageBytes, &fundingBytes, &w.WindowStart, &activatedAt, &w.ExpiresAt, &renewBy, &settledAt, &w.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return persistedWindow{}, ErrWindowNotFound
	}
	if err != nil {
		return persistedWindow{}, fmt.Errorf("load billing window %s: %w", windowID, err)
	}
	parsedOrg, err := strconv.ParseUint(orgIDTextValue, 10, 64)
	if err != nil {
		return persistedWindow{}, fmt.Errorf("parse window org_id %q: %w", orgIDTextValue, err)
	}
	w.OrgID = OrgID(parsedOrg)
	w.PricingContractID = pricingContractID.String
	w.PricingPhaseID = pricingPhaseID.String
	w.PricingPlanID = pricingPlanID.String
	w.WindowSeq = uint32(windowSeq)
	w.ReservedQuantity = uint32(reservedQuantity)
	w.ActualQuantity = uint32(actualQuantity)
	w.BillableQuantity = uint32(billableQuantity)
	w.WriteoffQuantity = uint32(writeoffQuantity)
	w.ReservedChargeUnits = uint64(reservedUnits)
	w.BilledChargeUnits = uint64(billedUnits)
	w.WriteoffChargeUnits = uint64(writeoffUnits)
	_ = json.Unmarshal(allocationBytes, &w.Allocation)
	_ = json.Unmarshal(rateBytes, &w.RateContext)
	_ = json.Unmarshal(usageBytes, &w.UsageSummary)
	_ = json.Unmarshal(fundingBytes, &w.FundingLegs)
	w.ActivatedAt = timePtr(activatedAt)
	w.RenewBy = timePtr(renewBy)
	w.SettledAt = timePtr(settledAt)
	return w, nil
}

func (c *Client) lockOrgProductTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, orgIDText(orgID)+":"+productID)
	if err != nil {
		return fmt.Errorf("lock org product billing: %w", err)
	}
	return nil
}

func (c *Client) orgBillingStateTx(ctx context.Context, tx pgx.Tx, orgID OrgID) (state string, overagePolicy string, err error) {
	err = tx.QueryRow(ctx, `SELECT state, overage_policy FROM orgs WHERE org_id = $1`, orgIDText(orgID)).Scan(&state, &overagePolicy)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = tx.Exec(ctx, `INSERT INTO orgs (org_id, display_name, trust_tier) VALUES ($1, $2, 'new') ON CONFLICT DO NOTHING`, orgIDText(orgID), "Org "+orgIDText(orgID))
		if err != nil {
			return "", "", fmt.Errorf("bootstrap org: %w", err)
		}
		return "active", "block", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("load org billing state: %w", err)
	}
	return state, overagePolicy, nil
}

func (c *Client) loadPricingContextTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID string, now time.Time) (pricingContext, error) {
	var out pricingContext
	err := tx.QueryRow(ctx, `
		WITH active_phase AS (
			SELECT cp.contract_id, cp.phase_id, cp.plan_id, c.overage_policy
			FROM contract_phases cp
			JOIN contracts c ON c.contract_id = cp.contract_id
			WHERE cp.org_id = $1 AND cp.product_id = $2
			  AND cp.state IN ('active','grace')
			  AND cp.effective_start <= $3
			  AND (cp.effective_end IS NULL OR cp.effective_end > $3)
			  AND c.state IN ('active','past_due','cancel_scheduled')
			ORDER BY cp.effective_start DESC, cp.phase_id DESC
			LIMIT 1
		), chosen AS (
			SELECT COALESCE((SELECT plan_id FROM active_phase), (SELECT plan_id FROM plans WHERE product_id = $2 AND active AND is_default ORDER BY plan_id LIMIT 1)) AS plan_id,
			       COALESCE((SELECT contract_id FROM active_phase), '') AS contract_id,
			       COALESCE((SELECT phase_id FROM active_phase), '') AS phase_id,
			       COALESCE((SELECT overage_policy FROM active_phase), 'block') AS overage_policy
		)
		SELECT p.plan_id, p.billing_mode, chosen.contract_id, chosen.phase_id, chosen.overage_policy, p.currency
		FROM chosen
		JOIN plans p ON p.plan_id = chosen.plan_id
	`, orgIDText(orgID), productID, now).Scan(&out.PlanID, &out.BillingMode, &out.ContractID, &out.PhaseID, &out.OveragePolicy, &out.Currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return pricingContext{}, fmt.Errorf("no active/default plan for product %s", productID)
	}
	if err != nil {
		return pricingContext{}, fmt.Errorf("load pricing context: %w", err)
	}
	out.SKURates = map[string]uint64{}
	out.SKUBuckets = map[string]string{}
	out.SKUDisplayNames = map[string]string{}
	out.SKUQuantityUnits = map[string]string{}
	out.BucketDisplayNames = map[string]string{}
	rows, err := tx.Query(ctx, `
		SELECT r.sku_id, r.unit_rate, s.bucket_id, s.display_name, s.quantity_unit, b.display_name
		FROM plan_sku_rates r
		JOIN skus s ON s.sku_id = r.sku_id
		JOIN credit_buckets b ON b.bucket_id = s.bucket_id
		WHERE r.plan_id = $1 AND r.active AND r.active_from <= $2 AND (r.active_until IS NULL OR r.active_until > $2)
		ORDER BY r.sku_id
	`, out.PlanID, now)
	if err != nil {
		return pricingContext{}, fmt.Errorf("load sku rates: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var skuID, bucketID, skuName, quantityUnit, bucketName string
		var rate int64
		if err := rows.Scan(&skuID, &rate, &bucketID, &skuName, &quantityUnit, &bucketName); err != nil {
			return pricingContext{}, fmt.Errorf("scan sku rate: %w", err)
		}
		out.SKURates[skuID] = uint64(rate)
		out.SKUBuckets[skuID] = bucketID
		out.SKUDisplayNames[skuID] = skuName
		out.SKUQuantityUnits[skuID] = quantityUnit
		out.BucketDisplayNames[bucketID] = bucketName
	}
	if err := rows.Err(); err != nil {
		return pricingContext{}, err
	}
	return out, nil
}

func (c *Client) fundReservationTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID string, componentCharges map[string]uint64, pricing pricingContext, allowPartial bool) ([]fundingLeg, error) {
	grants, err := c.grantBalancesTx(ctx, tx, orgID, productID)
	if err != nil {
		return nil, err
	}
	legs := []fundingLeg{}
	for _, skuID := range sortedKeys(componentCharges) {
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
			reservationID := ledger.NewID()
			legs = append(legs, fundingLeg{GrantID: grant.GrantID, GrantAccountID: grant.ledgerAccountID.String(), ReservationID: reservationID.String(), Amount: amount, Source: grant.Source, ScopeType: grant.ScopeType, ScopeProductID: grant.ScopeProductID, ScopeBucketID: grant.ScopeBucketID, ScopeSKUID: grant.ScopeSKUID, PlanID: grant.PlanID, ComponentSKUID: skuID, ComponentBucketID: bucketID})
		}
		if remaining > 0 && !allowPartial {
			continue
		}
	}
	return legs, nil
}

func (c *Client) grantBalancesTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID string) ([]GrantBalance, error) {
	now, err := c.BusinessNow(ctx, c.queries.WithTx(tx), orgID, productID)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT g.grant_id, g.scope_type, COALESCE(g.scope_product_id,''), COALESCE(g.scope_bucket_id,''), COALESCE(g.scope_sku_id,''),
		       g.amount, g.source, g.source_reference_id, COALESCE(g.entitlement_period_id,''), g.policy_version,
		       COALESCE(cp.plan_id,''), COALESCE(pl.tier,''), COALESCE(pl.display_name,''), g.starts_at, g.period_start, g.period_end, g.expires_at, g.account_id
		FROM credit_grants g
		LEFT JOIN entitlement_periods ep ON ep.period_id = g.entitlement_period_id
		LEFT JOIN contract_phases cp ON cp.phase_id = ep.phase_id
		LEFT JOIN plans pl ON pl.plan_id = cp.plan_id
		WHERE g.org_id = $1
		  AND g.closed_at IS NULL
		  AND ($2 = '' OR COALESCE(g.scope_product_id, $2) = $2 OR g.scope_type = 'account')
		  AND g.starts_at <= $3
		  AND (g.expires_at IS NULL OR g.expires_at > $3)
		ORDER BY CASE g.source WHEN 'free_tier' THEN 1 WHEN 'contract' THEN 2 WHEN 'promo' THEN 3 WHEN 'refund' THEN 4 WHEN 'purchase' THEN 5 ELSE 6 END, g.starts_at, g.grant_id
		FOR UPDATE OF g
	`, orgIDText(orgID), productID, now)
	if err != nil {
		return nil, fmt.Errorf("query grants for reservation: %w", err)
	}
	defer rows.Close()
	out := []GrantBalance{}
	for rows.Next() {
		var grant GrantBalance
		var amount int64
		var accountRaw []byte
		var periodStart, periodEnd, expiresAt pgtype.Timestamptz
		if err := rows.Scan(&grant.GrantID, &grant.ScopeType, &grant.ScopeProductID, &grant.ScopeBucketID, &grant.ScopeSKUID, &amount, &grant.Source, &grant.SourceReferenceID, &grant.EntitlementPeriodID, &grant.PolicyVersion, &grant.PlanID, &grant.PlanTier, &grant.PlanDisplayName, &grant.StartsAt, &periodStart, &periodEnd, &expiresAt, &accountRaw); err != nil {
			return nil, fmt.Errorf("scan grant for reservation: %w", err)
		}
		accountID, err := ledger.IDFromBytes(accountRaw)
		if err != nil {
			return nil, fmt.Errorf("parse grant account id %s: %w", grant.GrantID, err)
		}
		grant.OrgID = orgID
		grant.OriginalAmount = uint64(amount)
		grant.Amount = uint64(amount)
		grant.PeriodStart = timePtr(periodStart)
		grant.PeriodEnd = timePtr(periodEnd)
		grant.ExpiresAt = timePtr(expiresAt)
		grant.ledgerAccountID = accountID
		out = append(out, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := c.hydrateGrantLedgerBalances(ctx, out); err != nil {
		return nil, err
	}
	reserving, err := c.grantReservingUsageTx(ctx, tx, orgID)
	if err != nil {
		return nil, err
	}
	for i := range out {
		amount := reserving[out[i].GrantID]
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

func (c *Client) grantReservingUsageTx(ctx context.Context, tx pgx.Tx, orgID OrgID) (map[string]uint64, error) {
	rows, err := tx.Query(ctx, `
		SELECT l.grant_id, SUM(l.amount_reserved)
		FROM billing_windows w
		JOIN billing_window_ledger_legs l ON l.window_id = w.window_id
		WHERE w.org_id = $1
		  AND w.state = 'reserving'
		  AND l.grant_id IS NOT NULL
		GROUP BY l.grant_id
	`, orgIDText(orgID))
	if err != nil {
		return nil, fmt.Errorf("query reserving grant usage tx: %w", err)
	}
	defer rows.Close()
	out := map[string]uint64{}
	for rows.Next() {
		var grantID string
		var amount int64
		if err := rows.Scan(&grantID, &amount); err != nil {
			return nil, fmt.Errorf("scan reserving grant usage: %w", err)
		}
		if amount > 0 {
			out[grantID] = uint64(amount)
		}
	}
	return out, rows.Err()
}

func (c *Client) grantUsageTx(ctx context.Context, tx pgx.Tx, orgID OrgID) (map[string]uint64, map[string]uint64, error) {
	rows, err := tx.Query(ctx, `
		SELECT w.state, leg->>'grant_id', SUM((leg->>'amount')::bigint)
		FROM billing_windows w
		CROSS JOIN LATERAL jsonb_array_elements(w.funding_legs) leg
		WHERE w.org_id = $1
		  AND w.state IN ('reserved', 'active', 'settled')
		  AND COALESCE(leg->>'grant_id','') <> ''
		GROUP BY w.state, leg->>'grant_id'
	`, orgIDText(orgID))
	if err != nil {
		return nil, nil, fmt.Errorf("query grant usage tx: %w", err)
	}
	defer rows.Close()
	spent := map[string]uint64{}
	pending := map[string]uint64{}
	for rows.Next() {
		var state, grantID string
		var amount int64
		if err := rows.Scan(&state, &grantID, &amount); err != nil {
			return nil, nil, err
		}
		if state == "settled" {
			spent[grantID] += uint64(amount)
		} else {
			pending[grantID] += uint64(amount)
		}
	}
	return spent, pending, rows.Err()
}

func computeWindowCharges(allocation map[string]float64, rates map[string]uint64, skuBuckets map[string]string, quantity uint32) (map[string]uint64, map[string]uint64, uint64, error) {
	components := map[string]uint64{}
	buckets := map[string]uint64{}
	costPerUnit := uint64(0)
	for skuID, units := range allocation {
		if units < 0 || math.IsNaN(units) || math.IsInf(units, 0) {
			return nil, nil, 0, fmt.Errorf("invalid allocation for sku %s", skuID)
		}
		rate, ok := rates[skuID]
		if !ok {
			return nil, nil, 0, fmt.Errorf("no active rate for sku %s", skuID)
		}
		componentPerUnit := uint64(math.Ceil(units * float64(rate)))
		costPerUnit += componentPerUnit
		charge := uint64(quantity) * componentPerUnit
		components[skuID] = charge
		buckets[skuBuckets[skuID]] += charge
	}
	return components, buckets, costPerUnit, nil
}

func (c *Client) projectMeteringForWindow(ctx context.Context, w persistedWindow) error {
	if c.ch == nil || w.State != "settled" {
		return nil
	}
	componentQuantities := map[string]float64{}
	componentCharges := map[string]uint64{}
	bucketCharges := map[string]uint64{}
	for skuID, units := range w.Allocation {
		componentQuantities[skuID] = units * float64(w.BillableQuantity)
		rate := w.RateContext.SKURates[skuID]
		charge := uint64(math.Ceil(units*float64(rate))) * uint64(w.BillableQuantity)
		componentCharges[skuID] = charge
		bucketCharges[w.RateContext.SKUBuckets[skuID]] += charge
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
	if w.SettledAt != nil && w.ActualQuantity == 0 {
		endedAt = *w.SettledAt
	}
	row := meteringRow{WindowID: w.WindowID, OrgID: orgIDText(w.OrgID), ActorID: w.ActorID, ProductID: w.ProductID, SourceType: w.SourceType, SourceRef: w.SourceRef, WindowSeq: w.WindowSeq, ReservationShape: w.ReservationShape, StartedAt: w.WindowStart, EndedAt: endedAt, ReservedQuantity: uint64(w.ReservedQuantity), ActualQuantity: uint64(w.ActualQuantity), BillableQuantity: uint64(w.BillableQuantity), WriteoffQuantity: uint64(w.WriteoffQuantity), CycleID: w.CycleID, PricingContractID: w.PricingContractID, PricingPhaseID: w.PricingPhaseID, PricingPlanID: w.PricingPlanID, PricingPhase: w.PricingPhase, Dimensions: cloneFloatMap(w.Allocation), ComponentQuantities: componentQuantities, ComponentChargeUnits: componentCharges, BucketChargeUnits: bucketCharges, ChargeUnits: w.BilledChargeUnits, WriteoffChargeUnits: w.WriteoffChargeUnits, FreeTierUnits: bySource["free_tier"], ContractUnits: bySource["contract"], PurchaseUnits: bySource["purchase"], PromoUnits: bySource["promo"], RefundUnits: bySource["refund"], ReceivableUnits: bySource["receivable"], AdjustmentUnits: bySource["adjustment"], ComponentFreeTierUnits: componentBySource["free_tier"], ComponentContractUnits: componentBySource["contract"], ComponentPurchaseUnits: componentBySource["purchase"], ComponentPromoUnits: componentBySource["promo"], ComponentRefundUnits: componentBySource["refund"], ComponentReceivableUnits: componentBySource["receivable"], ComponentAdjustmentUnits: componentBySource["adjustment"], UsageEvidence: usageEvidence(w.UsageSummary), CostPerUnit: w.RateContext.CostPerUnit, RecordedAt: time.Now().UTC()}
	batch, err := c.ch.PrepareBatch(ctx, "INSERT INTO forge_metal.metering")
	if err != nil {
		return fmt.Errorf("prepare metering batch: %w", err)
	}
	if err := appendMeteringRow(batch, row); err != nil {
		return err
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send metering batch: %w", err)
	}
	_, err = c.pg.Exec(ctx, `UPDATE billing_windows SET metering_projected_at = $2, last_projection_error = '' WHERE window_id = $1`, w.WindowID, row.RecordedAt)
	return err
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

func settleFundingLegs(legs []fundingLeg, componentBilled map[string]uint64) []fundingLeg {
	if len(legs) == 0 || len(componentBilled) == 0 {
		return []fundingLeg{}
	}
	remainingBySKU := cloneUintMap(componentBilled)
	trimmed := make([]fundingLeg, 0, len(componentBilled))
	for _, leg := range legs {
		if leg.Amount == 0 || leg.ComponentSKUID == "" {
			continue
		}
		remaining := remainingBySKU[leg.ComponentSKUID]
		if remaining == 0 {
			continue
		}
		next := leg
		if next.Amount > remaining {
			next.Amount = remaining
		}
		trimmed = append(trimmed, next)
		remainingBySKU[leg.ComponentSKUID] = remaining - next.Amount
	}
	return trimmed
}

func minUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
