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
	tb "github.com/tigerbeetle/tigerbeetle-go"
	"github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

type subscriptionPlan struct {
	planID           string
	status           string
	unitRates        map[string]uint64
	overageUnitRates map[string]uint64
}

type plan struct {
	planID    string
	unitRates map[string]uint64
}

type grantBalance struct {
	grantID   GrantID
	source    GrantSourceType
	expiresAt *time.Time
	available uint64
}

// Reserve initiates a billing reservation for a metered product.
func (c *Client) Reserve(ctx context.Context, req ReserveRequest) (Reservation, error) {
	return c.reserveWindow(ctx, req, 0, c.clock().UTC())
}

// Renew settles the current window, then reserves the next one from the latest grant state.
func (c *Client) Renew(ctx context.Context, reservation *Reservation, actualSeconds uint32) error {
	if err := c.Settle(ctx, reservation, actualSeconds); err != nil {
		return err
	}

	next, err := c.reserveWindow(ctx, ReserveRequest{
		JobID:      reservation.JobID,
		OrgID:      reservation.OrgID,
		ProductID:  reservation.ProductID,
		ActorID:    reservation.ActorID,
		Allocation: cloneFloat64Map(reservation.Allocation),
		SourceType: reservation.SourceType,
		SourceRef:  reservation.SourceRef,
	}, reservation.WindowSeq+1, reservation.WindowStart.Add(time.Duration(actualSeconds)*time.Second).UTC())
	if err != nil {
		return err
	}

	*reservation = next
	return nil
}

// Settle posts the spent portion of each pending grant leg and releases the rest.
func (c *Client) Settle(ctx context.Context, reservation *Reservation, actualSeconds uint32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if actualSeconds > reservation.WindowSecs {
		return fmt.Errorf("actual_seconds %d exceeds reserved window %d", actualSeconds, reservation.WindowSecs)
	}

	actualCost, err := safeMulUint64(reservation.CostPerSec, uint64(actualSeconds))
	if err != nil {
		return fmt.Errorf("actual cost: %w", err)
	}

	var reservedTotal uint64
	for _, leg := range reservation.GrantLegs {
		reservedTotal, err = safeAddUint64(reservedTotal, leg.Amount)
		if err != nil {
			return fmt.Errorf("reserved total: %w", err)
		}
	}
	if actualCost > reservedTotal {
		return fmt.Errorf("actual cost %d exceeds reserved total %d", actualCost, reservedTotal)
	}

	transfers := make([]types.Transfer, 0, len(reservation.GrantLegs))
	remainder := actualCost
	for i, leg := range reservation.GrantLegs {
		if i > math.MaxUint8 {
			return fmt.Errorf("grant leg index %d exceeds max supported tigerbeetle grant_idx", i)
		}

		settleAmount := minUint64(leg.Amount, remainder)
		if settleAmount > 0 {
			transfers = append(transfers, types.Transfer{
				ID:        VMTransferID(reservation.JobID, reservation.WindowSeq, uint8(i), KindSettlement).raw,
				PendingID: leg.TransferID.raw,
				Amount:    types.ToUint128(settleAmount),
				Flags:     types.TransferFlags{PostPendingTransfer: true}.ToUint16(),
			})
			remainder -= settleAmount
			continue
		}

		transfers = append(transfers, types.Transfer{
			ID:        VMTransferID(reservation.JobID, reservation.WindowSeq, uint8(i), KindVoid).raw,
			PendingID: leg.TransferID.raw,
			Flags:     types.TransferFlags{VoidPendingTransfer: true}.ToUint16(),
		})
	}

	if err := c.createTransfers(transfers); err != nil {
		return fmt.Errorf("settle reservation: %w", err)
	}

	// Overage cap: void the check-pending, post the real debit for actual cost.
	if reservation.CapCheckLeg != nil && reservation.PricingPhase == PricingPhaseOverage {
		capAcctID := OverageCapAccountID(reservation.OrgID, reservation.ProductID)
		sinkID := OperatorAccountID(AcctQuotaSink)

		var capTransfers []types.Transfer
		// Void the cap check pending transfer.
		capTransfers = append(capTransfers, types.Transfer{
			ID:        OverageCapTransferID(reservation.JobID, reservation.WindowSeq, KindVoid).raw,
			PendingID: reservation.CapCheckLeg.TransferID.raw,
			Flags:     types.TransferFlags{VoidPendingTransfer: true}.ToUint16(),
		})
		// Post the real overage debit for actual cost.
		if actualCost > 0 {
			capTransfers = append(capTransfers, types.Transfer{
				ID:              OverageCapTransferID(reservation.JobID, reservation.WindowSeq, KindOverageCapDebit).raw,
				DebitAccountID:  capAcctID.raw,
				CreditAccountID: sinkID.raw,
				Amount:          types.ToUint128(actualCost),
				Ledger:          1,
				Code:            uint16(KindOverageCapDebit),
			})
		}
		if err := c.createTransfers(capTransfers); err != nil {
			return fmt.Errorf("settle overage cap: %w", err)
		}
	}

	row := buildMeteringRow(reservation, actualSeconds, actualCost)
	if err := c.metering.InsertMeteringRow(ctx, row); err != nil {
		return fmt.Errorf("settle reservation: write metering row: %w", err)
	}

	return nil
}

// Void cancels each pending grant leg for a reservation.
func (c *Client) Void(ctx context.Context, reservation *Reservation) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	transfers := make([]types.Transfer, 0, len(reservation.GrantLegs))
	for i, leg := range reservation.GrantLegs {
		if i > math.MaxUint8 {
			return fmt.Errorf("grant leg index %d exceeds max supported tigerbeetle grant_idx", i)
		}
		transfers = append(transfers, types.Transfer{
			ID:        VMTransferID(reservation.JobID, reservation.WindowSeq, uint8(i), KindVoid).raw,
			PendingID: leg.TransferID.raw,
			Flags:     types.TransferFlags{VoidPendingTransfer: true}.ToUint16(),
		})
	}

	// Also void the cap check pending if present.
	if reservation.CapCheckLeg != nil {
		transfers = append(transfers, types.Transfer{
			ID:        OverageCapTransferID(reservation.JobID, reservation.WindowSeq, KindVoid).raw,
			PendingID: reservation.CapCheckLeg.TransferID.raw,
			Flags:     types.TransferFlags{VoidPendingTransfer: true}.ToUint16(),
		})
	}

	if err := c.createTransfers(transfers); err != nil {
		return fmt.Errorf("void reservation: %w", err)
	}
	return nil
}

func (c *Client) reserveWindow(ctx context.Context, req ReserveRequest, windowSeq uint32, windowStart time.Time) (Reservation, error) {
	if err := ctx.Err(); err != nil {
		return Reservation{}, err
	}
	if req.SourceType == "" {
		return Reservation{}, fmt.Errorf("billing: ReserveRequest.SourceType is required")
	}
	if req.SourceRef == "" {
		return Reservation{}, fmt.Errorf("billing: ReserveRequest.SourceRef is required")
	}
	if err := c.ensureOrgNotSuspended(ctx, req.OrgID); err != nil {
		return Reservation{}, err
	}

	now := c.clock().UTC()
	activePlan, err := c.loadActiveSubscriptionPlan(ctx, req.OrgID, req.ProductID)
	if err != nil {
		return Reservation{}, err
	}
	defaultPlan, err := c.loadDefaultPlan(ctx, req.OrgID, req.ProductID)
	if err != nil {
		return Reservation{}, err
	}
	grants, err := c.loadGrantBalances(ctx, req.OrgID, req.ProductID, now)
	if err != nil {
		return Reservation{}, err
	}

	phase, planID, unitRates, eligible, err := selectReservationPhase(activePlan, defaultPlan, grants)
	if err != nil {
		return Reservation{}, err
	}

	costPerSec, err := computeCostPerSecond(req.Allocation, unitRates)
	if err != nil {
		return Reservation{}, err
	}
	windowCost, err := safeMulUint64(costPerSec, uint64(c.cfg.ReservationWindowSecs))
	if err != nil {
		return Reservation{}, fmt.Errorf("window cost: %w", err)
	}

	// §2.5 step 8: overage ceiling enforcement via TigerBeetle balance-conditional.
	var capCheckLeg *GrantLeg
	if phase == PricingPhaseOverage {
		leg, err := c.enforceOverageCapTB(ctx, req.OrgID, req.ProductID, windowCost, req.JobID, windowSeq)
		if err != nil {
			return Reservation{}, err
		}
		capCheckLeg = leg
	}

	grantLegs, err := c.reserveGrantWaterfall(ctx, req.JobID, req.OrgID, windowSeq, phase, windowCost, eligible)
	if err != nil {
		return Reservation{}, err
	}

	return Reservation{
		JobID:        req.JobID,
		OrgID:        req.OrgID,
		ProductID:    req.ProductID,
		PlanID:       planID,
		ActorID:      req.ActorID,
		SourceType:   req.SourceType,
		SourceRef:    req.SourceRef,
		WindowSeq:    windowSeq,
		WindowSecs:   c.cfg.ReservationWindowSecs,
		WindowStart:  windowStart.UTC(),
		PricingPhase: phase,
		Allocation:   cloneFloat64Map(req.Allocation),
		UnitRates:    cloneUint64Map(unitRates),
		CostPerSec:   costPerSec,
		GrantLegs:    grantLegs,
		CapCheckLeg:  capCheckLeg,
	}, nil
}

// loadOverageCap reads the subscription's overage_cap_units and current_period_start.
// Returns (nil, _, nil) if the subscription has no cap or no active subscription exists.
func (c *Client) loadOverageCap(ctx context.Context, orgID OrgID, productID string) (*int64, time.Time, error) {
	var capUnits sql.NullInt64
	var periodStart time.Time

	err := c.pg.QueryRowContext(ctx, `
		SELECT overage_cap_units, current_period_start
		FROM subscriptions
		WHERE org_id = $1
		  AND product_id = $2
		  AND status IN ('active', 'past_due', 'trialing')
		ORDER BY subscription_id DESC
		LIMIT 1
	`, strconv.FormatUint(uint64(orgID), 10), productID).Scan(&capUnits, &periodStart)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("load overage cap: %w", err)
	}

	if !capUnits.Valid {
		return nil, time.Time{}, nil
	}
	v := capUnits.Int64
	return &v, periodStart.UTC(), nil
}

func (c *Client) ensureOrgNotSuspended(ctx context.Context, orgID OrgID) error {
	var suspended bool
	if err := c.pg.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM subscriptions
			WHERE org_id = $1
			  AND status = 'suspended'
		)
	`, strconv.FormatUint(uint64(orgID), 10)).Scan(&suspended); err != nil {
		return fmt.Errorf("check org suspension: %w", err)
	}
	if suspended {
		return ErrOrgSuspended
	}
	return nil
}

func (c *Client) loadActiveSubscriptionPlan(ctx context.Context, orgID OrgID, productID string) (*subscriptionPlan, error) {
	var (
		planID           string
		status           string
		unitRatesJSON    []byte
		overageRatesJSON []byte
	)

	err := c.pg.QueryRowContext(ctx, `
		SELECT
			s.plan_id,
			s.status,
			COALESCE(o.unit_rates, p.unit_rates)::text,
			p.overage_unit_rates::text
		FROM subscriptions s
		JOIN plans p ON p.plan_id = s.plan_id
		LEFT JOIN org_pricing_overrides o
		       ON o.org_id = s.org_id
		      AND o.plan_id = s.plan_id
		WHERE s.org_id = $1
		  AND s.product_id = $2
		  AND s.status IN ('active', 'past_due', 'trialing')
		ORDER BY s.subscription_id DESC
		LIMIT 1
	`, strconv.FormatUint(uint64(orgID), 10), productID).Scan(&planID, &status, &unitRatesJSON, &overageRatesJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load active subscription plan: %w", err)
	}

	unitRates, err := decodeRateCard(unitRatesJSON)
	if err != nil {
		return nil, fmt.Errorf("decode unit rates for %s: %w", planID, err)
	}
	overageUnitRates, err := decodeRateCard(overageRatesJSON)
	if err != nil {
		return nil, fmt.Errorf("decode overage unit rates for %s: %w", planID, err)
	}

	return &subscriptionPlan{
		planID:           planID,
		status:           status,
		unitRates:        unitRates,
		overageUnitRates: overageUnitRates,
	}, nil
}

func (c *Client) loadDefaultPlan(ctx context.Context, orgID OrgID, productID string) (*plan, error) {
	var (
		planID        string
		unitRatesJSON []byte
	)

	err := c.pg.QueryRowContext(ctx, `
		SELECT
			p.plan_id,
			COALESCE(o.unit_rates, p.unit_rates)::text
		FROM plans p
		LEFT JOIN org_pricing_overrides o
		       ON o.org_id = $1
		      AND o.plan_id = p.plan_id
		WHERE p.product_id = $2
		  AND p.is_default
		  AND p.active
		LIMIT 1
	`, strconv.FormatUint(uint64(orgID), 10), productID).Scan(&planID, &unitRatesJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load default plan: %w", err)
	}

	unitRates, err := decodeRateCard(unitRatesJSON)
	if err != nil {
		return nil, fmt.Errorf("decode default plan rates for %s: %w", planID, err)
	}

	return &plan{
		planID:    planID,
		unitRates: unitRates,
	}, nil
}

func (c *Client) loadGrantBalances(ctx context.Context, orgID OrgID, productID string, now time.Time) ([]grantBalance, error) {
	rows, err := c.pg.QueryContext(ctx, `
		SELECT grant_id, source, expires_at
		FROM credit_grants
		WHERE org_id = $1
		  AND product_id = $2
		  AND closed_at IS NULL
		  AND (expires_at IS NULL OR expires_at > $3)
		ORDER BY expires_at ASC NULLS LAST, grant_id ASC
	`, strconv.FormatUint(uint64(orgID), 10), productID, now)
	if err != nil {
		return nil, fmt.Errorf("query grant catalog: %w", err)
	}
	defer rows.Close()

	type grantRow struct {
		grantID   GrantID
		source    GrantSourceType
		expiresAt *time.Time
	}

	rowsForLookup := make([]grantRow, 0, 8)
	accountIDs := make([]types.Uint128, 0, 8)
	for rows.Next() {
		var (
			grantIDStr string
			source     string
			expiresAt  sql.NullTime
		)
		if err := rows.Scan(&grantIDStr, &source, &expiresAt); err != nil {
			return nil, fmt.Errorf("scan grant row: %w", err)
		}

		parsedULID, err := ulid.ParseStrict(grantIDStr)
		if err != nil {
			return nil, fmt.Errorf("parse grant ULID %q: %w", grantIDStr, err)
		}
		grantID := GrantID(parsedULID)

		sourceType, err := ParseGrantSourceType(source)
		if err != nil {
			return nil, fmt.Errorf("grant %x: %w", grantID, err)
		}

		var expiresAtPtr *time.Time
		if expiresAt.Valid {
			exp := expiresAt.Time.UTC()
			expiresAtPtr = &exp
		}

		rowsForLookup = append(rowsForLookup, grantRow{
			grantID:   grantID,
			source:    sourceType,
			expiresAt: expiresAtPtr,
		})
		accountIDs = append(accountIDs, GrantAccountID(grantID).raw)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate grant rows: %w", err)
	}
	if len(rowsForLookup) == 0 {
		return nil, nil
	}

	accounts, err := c.tb.LookupAccounts(accountIDs)
	if err != nil {
		return nil, fmt.Errorf("lookup grant accounts: %w", err)
	}
	if len(accounts) != len(rowsForLookup) {
		return nil, fmt.Errorf("lookup grant accounts: expected %d accounts, got %d", len(rowsForLookup), len(accounts))
	}

	accountByID := make(map[types.Uint128]types.Account, len(accounts))
	for _, account := range accounts {
		accountByID[account.ID] = account
	}

	grants := make([]grantBalance, 0, len(rowsForLookup))
	for _, row := range rowsForLookup {
		account, ok := accountByID[GrantAccountID(row.grantID).raw]
		if !ok {
			return nil, fmt.Errorf("lookup grant accounts: missing account for grant %x", row.grantID)
		}

		available, err := availableFromAccount(account)
		if err != nil {
			return nil, fmt.Errorf("grant %x available: %w", row.grantID, err)
		}

		grants = append(grants, grantBalance{
			grantID:   row.grantID,
			source:    row.source,
			expiresAt: row.expiresAt,
			available: available,
		})
	}

	return grants, nil
}

func selectReservationPhase(activePlan *subscriptionPlan, defaultPlan *plan, grants []grantBalance) (PricingPhase, string, map[string]uint64, []grantBalance, error) {
	freeTierGrants := filterGrantBalances(grants, func(grant grantBalance) bool {
		return grant.available > 0 && grant.source == SourceFreeTier
	})
	subscriptionGrants := filterGrantBalances(grants, func(grant grantBalance) bool {
		return grant.available > 0 && grant.source == SourceSubscription
	})
	prepaidGrants := filterGrantBalances(grants, func(grant grantBalance) bool {
		return grant.available > 0 && (grant.source == SourcePurchase || grant.source == SourcePromo || grant.source == SourceRefund)
	})

	switch {
	case len(freeTierGrants) > 0:
		selectedPlan := pickUnitRatePlan(activePlan, defaultPlan)
		if selectedPlan == nil {
			return "", "", nil, nil, ErrNoActiveSubscription
		}
		return PricingPhaseFreeTier, selectedPlan.planID, cloneUint64Map(selectedPlan.unitRates), freeTierGrants, nil
	case activePlan != nil && len(subscriptionGrants) > 0:
		return PricingPhaseIncluded, activePlan.planID, cloneUint64Map(activePlan.unitRates), subscriptionGrants, nil
	case len(prepaidGrants) > 0:
		switch {
		case activePlan != nil && len(activePlan.overageUnitRates) > 0:
			return PricingPhaseOverage, activePlan.planID, cloneUint64Map(activePlan.overageUnitRates), prepaidGrants, nil
		case defaultPlan != nil:
			return PricingPhaseOverage, defaultPlan.planID, cloneUint64Map(defaultPlan.unitRates), prepaidGrants, nil
		case activePlan == nil:
			return "", "", nil, nil, ErrNoActiveSubscription
		default:
			return "", "", nil, nil, ErrInsufficientBalance
		}
	default:
		if activePlan == nil && defaultPlan == nil {
			return "", "", nil, nil, ErrNoActiveSubscription
		}
		return "", "", nil, nil, ErrInsufficientBalance
	}
}

func (c *Client) reserveGrantWaterfall(ctx context.Context, jobID JobID, orgID OrgID, windowSeq uint32, phase PricingPhase, windowCost uint64, grants []grantBalance) ([]GrantLeg, error) {
	if windowCost == 0 {
		return nil, nil
	}

	sort.Slice(grants, func(i, j int) bool {
		left := grants[i]
		right := grants[j]
		switch {
		case left.expiresAt == nil && right.expiresAt != nil:
			return false
		case left.expiresAt != nil && right.expiresAt == nil:
			return true
		case left.expiresAt != nil && right.expiresAt != nil && !left.expiresAt.Equal(*right.expiresAt):
			return left.expiresAt.Before(*right.expiresAt)
		default:
			return bytes.Compare(left.grantID[:], right.grantID[:]) < 0
		}
	})

	phaseSinkAccount := phaseSinkAccountID(phase)
	remainder := windowCost
	grantLegs := make([]GrantLeg, 0, len(grants))

	for i, grant := range grants {
		if remainder == 0 {
			break
		}
		if i > math.MaxUint8 {
			return nil, fmt.Errorf("grant leg index %d exceeds max supported tigerbeetle grant_idx", i)
		}

		transferID := VMTransferID(jobID, windowSeq, uint8(i), KindReservation)
		if err := c.createTransfers([]types.Transfer{{
			ID:              transferID.raw,
			DebitAccountID:  GrantAccountID(grant.grantID).raw,
			CreditAccountID: phaseSinkAccount.raw,
			Amount:          types.ToUint128(remainder),
			Ledger:          1,
			Code:            uint16(KindReservation),
			Flags: types.TransferFlags{
				Pending:        true,
				BalancingDebit: true,
			}.ToUint16(),
			UserData64: uint64(orgID),
			UserData32: windowSeq,
			Timeout:    c.cfg.PendingTimeoutSecs,
		}}); err != nil {
			return nil, fmt.Errorf("create reservation leg %d: %w", i, err)
		}

		transfer, err := c.lookupTransfer(transferID)
		if err != nil {
			return nil, fmt.Errorf("lookup reservation leg %d: %w", i, err)
		}

		reservedAmount, err := uint128ToUint64(transfer.Amount)
		if err != nil {
			return nil, fmt.Errorf("reservation leg %d amount: %w", i, err)
		}
		if reservedAmount == 0 {
			continue
		}

		grantLegs = append(grantLegs, GrantLeg{
			GrantID:    grant.grantID,
			TransferID: transferID,
			Amount:     reservedAmount,
			Source:     grant.source,
		})
		remainder -= reservedAmount
	}

	if remainder > 0 {
		reservation := Reservation{
			JobID:     jobID,
			WindowSeq: windowSeq,
			GrantLegs: grantLegs,
		}
		if len(grantLegs) > 0 {
			if err := c.Void(ctx, &reservation); err != nil {
				return nil, fmt.Errorf("%w: void partial reservation: %v", ErrInsufficientBalance, err)
			}
		}
		return nil, ErrInsufficientBalance
	}

	return grantLegs, nil
}

func (c *Client) createTransfers(transfers []types.Transfer) error {
	if len(transfers) == 0 {
		return nil
	}

	results, err := c.tb.CreateTransfers(transfers)
	if err != nil {
		return fmt.Errorf("create transfers: %w", err)
	}

	for _, result := range results {
		switch result.Result {
		case types.TransferOK, types.TransferExists:
			continue
		case types.TransferExceedsCredits:
			return ErrInsufficientBalance
		case types.TransferLinkedEventFailed:
			return fmt.Errorf("transfer %d: linked event failed", result.Index)
		case types.TransferExistsWithDifferentFlags,
			types.TransferExistsWithDifferentPendingID,
			types.TransferExistsWithDifferentTimeout,
			types.TransferExistsWithDifferentDebitAccountID,
			types.TransferExistsWithDifferentCreditAccountID,
			types.TransferExistsWithDifferentAmount,
			types.TransferExistsWithDifferentUserData128,
			types.TransferExistsWithDifferentUserData64,
			types.TransferExistsWithDifferentUserData32,
			types.TransferExistsWithDifferentLedger,
			types.TransferExistsWithDifferentCode:
			return fmt.Errorf("transfer %d: %s", result.Index, result.Result)
		default:
			return fmt.Errorf("transfer %d: %s", result.Index, result.Result)
		}
	}

	return nil
}

// postPendingTransfer posts a pending transfer, handling the two-phase commit
// result codes. Returns nil on success or if the pending was already posted
// (idempotent). Returns ErrPendingTransferExpired if the timeout elapsed.
func (c *Client) postPendingTransfer(pendingID TransferID, postID TransferID) error {
	results, err := c.tb.CreateTransfers([]types.Transfer{{
		ID:        postID.raw,
		PendingID: pendingID.raw,
		Flags:     types.TransferFlags{PostPendingTransfer: true}.ToUint16(),
		Amount:    tb.AmountMax,
	}})
	if err != nil {
		return fmt.Errorf("post pending transfer: %w", err)
	}

	for _, result := range results {
		switch result.Result {
		case types.TransferOK, types.TransferExists:
			return nil
		case types.TransferPendingTransferAlreadyPosted:
			return nil
		case types.TransferPendingTransferExpired:
			return ErrPendingTransferExpired
		case types.TransferPendingTransferAlreadyVoided:
			return fmt.Errorf("post pending transfer: %w: voided", ErrPendingTransferExpired)
		default:
			return fmt.Errorf("post pending transfer: %s", result.Result)
		}
	}

	return nil
}

func (c *Client) lookupTransfer(id TransferID) (types.Transfer, error) {
	transfers, err := c.tb.LookupTransfers([]types.Uint128{id.raw})
	if err != nil {
		return types.Transfer{}, fmt.Errorf("lookup transfers: %w", err)
	}
	if len(transfers) != 1 {
		return types.Transfer{}, fmt.Errorf("lookup transfers: expected 1 transfer, got %d", len(transfers))
	}
	return transfers[0], nil
}

func phaseSinkAccountID(phase PricingPhase) AccountID {
	if phase == PricingPhaseFreeTier {
		return OperatorAccountID(AcctFreeTierExpense)
	}
	return OperatorAccountID(AcctRevenue)
}

func pickUnitRatePlan(activePlan *subscriptionPlan, defaultPlan *plan) *plan {
	switch {
	case activePlan != nil:
		return &plan{planID: activePlan.planID, unitRates: activePlan.unitRates}
	case defaultPlan != nil:
		return defaultPlan
	default:
		return nil
	}
}

func filterGrantBalances(grants []grantBalance, keep func(grantBalance) bool) []grantBalance {
	filtered := make([]grantBalance, 0, len(grants))
	for _, grant := range grants {
		if keep(grant) {
			filtered = append(filtered, grant)
		}
	}
	return filtered
}

func computeCostPerSecond(allocation map[string]float64, unitRates map[string]uint64) (uint64, error) {
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
		return 0, fmt.Errorf("non-integral cost_per_sec %.9f", total)
	}
	return uint64(rounded), nil
}

func decodeRateCard(raw []byte) (map[string]uint64, error) {
	if len(raw) == 0 {
		return map[string]uint64{}, nil
	}

	var card map[string]uint64
	if err := json.Unmarshal(raw, &card); err != nil {
		return nil, err
	}
	if card == nil {
		return map[string]uint64{}, nil
	}
	return card, nil
}

func cloneFloat64Map(in map[string]float64) map[string]float64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]float64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneUint64Map(in map[string]uint64) map[string]uint64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]uint64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func safeMulUint64(a, b uint64) (uint64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	if a > math.MaxUint64/b {
		return 0, fmt.Errorf("uint64 overflow")
	}
	return a * b, nil
}

func minUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

func buildMeteringRow(reservation *Reservation, actualSeconds uint32, actualCost uint64) MeteringRow {
	// Compute per-source unit breakdown from settled grant legs.
	var freeTier, subscription, purchase, promo, refund uint64
	remainder := actualCost
	for _, leg := range reservation.GrantLegs {
		settled := minUint64(leg.Amount, remainder)
		switch leg.Source {
		case SourceFreeTier:
			freeTier += settled
		case SourceSubscription:
			subscription += settled
		case SourcePurchase:
			purchase += settled
		case SourcePromo:
			promo += settled
		case SourceRefund:
			refund += settled
		}
		remainder -= settled
		if remainder == 0 {
			break
		}
	}

	return MeteringRow{
		OrgID:             strconv.FormatUint(uint64(reservation.OrgID), 10),
		ActorID:           reservation.ActorID,
		ProductID:         reservation.ProductID,
		SourceType:        reservation.SourceType,
		SourceRef:         reservation.SourceRef,
		WindowSeq:         reservation.WindowSeq,
		StartedAt:         reservation.WindowStart,
		EndedAt:           reservation.WindowStart.Add(time.Duration(actualSeconds) * time.Second),
		BilledSeconds:     actualSeconds,
		PricingPhase:      string(reservation.PricingPhase),
		Dimensions:        cloneFloat64Map(reservation.Allocation),
		ChargeUnits:       actualCost,
		FreeTierUnits:     freeTier,
		SubscriptionUnits: subscription,
		PurchaseUnits:     purchase,
		PromoUnits:        promo,
		RefundUnits:       refund,
		RecordedAt:        time.Now().UTC(),
	}
}
