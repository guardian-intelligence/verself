package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stripe/stripe-go/v85"

	"github.com/verself/billing-service/internal/billing/ledger"
	"github.com/verself/billing-service/internal/store"
)

type planEntitlementPolicy struct {
	PolicyID             string
	ProductID            string
	ScopeType            string
	ScopeProductID       string
	ScopeBucketID        string
	ScopeSKUID           string
	AmountUnits          uint64
	Cadence              string
	ProrationMode        string
	PolicyVersion        string
	RecurrenceAnchorKind string
}

type contractChangeQuote struct {
	ChangeID          string                     `json:"change_id"`
	ContractID        string                     `json:"contract_id"`
	OrgID             OrgID                      `json:"org_id"`
	ProductID         string                     `json:"product_id"`
	FromPlanID        string                     `json:"from_plan_id"`
	TargetPlanID      string                     `json:"target_plan_id"`
	FromPhaseID       string                     `json:"from_phase_id"`
	ToPhaseID         string                     `json:"to_phase_id"`
	CycleID           string                     `json:"cycle_id"`
	CycleStart        time.Time                  `json:"cycle_start"`
	CycleEnd          time.Time                  `json:"cycle_end"`
	EffectiveAt       time.Time                  `json:"effective_at"`
	RequestedAt       time.Time                  `json:"requested_at"`
	ProviderRequestID string                     `json:"provider_request_id"`
	PriceDeltaCents   uint64                     `json:"price_delta_cents"`
	PriceDeltaUnits   uint64                     `json:"price_delta_units"`
	Deltas            []contractEntitlementDelta `json:"deltas"`
	TargetPolicies    []planEntitlementPolicy    `json:"target_policies"`
}

type contractEntitlementDelta struct {
	Policy planEntitlementPolicy `json:"policy"`
	Amount uint64                `json:"amount"`
}

func (c *Client) CreateContract(ctx context.Context, orgID OrgID, planID string, cadence BillingCadence, successURL, cancelURL string) (string, error) {
	if planID == "" {
		return "", fmt.Errorf("plan_id is required")
	}
	if cadence == "" {
		cadence = CadenceMonthly
	}
	if cadence != CadenceMonthly {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedCadence, cadence)
	}
	plan, err := c.loadPlan(ctx, planID)
	if err != nil {
		return "", err
	}
	contractID := contractID(orgID, plan.ProductID)
	if existing, err := c.GetContract(ctx, orgID, contractID); err == nil {
		if existing.PlanID == planID && (existing.Status == "active" || existing.Status == "cancel_scheduled") {
			if _, err := c.resumeCurrentPlan(ctx, orgID, existing); err != nil {
				return "", err
			}
			return contractReturnURL(successURL, map[string]string{"contractAction": "resume", "targetPlanID": planID}), nil
		}
		result, err := c.CreateContractChange(ctx, orgID, contractID, ContractChangeRequest{TargetPlanID: planID, SuccessURL: successURL, CancelURL: cancelURL})
		if err != nil {
			return "", err
		}
		return result.URL, nil
	} else if !errors.Is(err, ErrContractNotFound) {
		return "", err
	}
	if !c.cfg.UseStripe || c.stripe == nil {
		now, err := c.BusinessNow(ctx, c.queries, orgID, plan.ProductID)
		if err != nil {
			return "", err
		}
		if err := c.activateCatalogContract(ctx, orgID, plan.ProductID, planID, contractID, phaseID(contractID, planID, now), now, now); err != nil {
			return "", err
		}
		return contractReturnURL(successURL, map[string]string{"contractAction": "start", "targetPlanID": planID}), nil
	}
	customerID, err := c.ensureStripeCustomer(ctx, orgID)
	if err != nil {
		return "", err
	}
	now, err := c.BusinessNow(ctx, c.queries, orgID, plan.ProductID)
	if err != nil {
		return "", err
	}
	phaseID := phaseID(contractID, planID, now)
	checkoutAttemptID := textID("stripe_setup_checkout", contractID, planID, time.Now().UTC().Format(time.RFC3339Nano))
	metadata := map[string]string{"org_id": orgIDText(orgID), "product_id": plan.ProductID, "plan_id": planID, "contract_id": contractID, "phase_id": phaseID, "cadence": string(cadence), "checkout_attempt_id": checkoutAttemptID}
	params := &stripe.CheckoutSessionCreateParams{Mode: stripe.String(string(stripe.CheckoutSessionModeSetup)), Customer: stripe.String(customerID), Currency: stripe.String(plan.Currency), SuccessURL: stripe.String(successURL), CancelURL: stripe.String(cancelURL), SetupIntentData: &stripe.CheckoutSessionCreateSetupIntentDataParams{Description: stripe.String(plan.DisplayName + " payment method"), Metadata: metadata}, Metadata: metadata}
	// Checkout setup sessions are single-use; key them by attempt, not business-effective phase.
	params.SetIdempotencyKey(checkoutAttemptID)
	session, err := c.stripe.V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("create stripe setup checkout: %w", err)
	}
	return session.URL, nil
}

func (c *Client) GetContract(ctx context.Context, orgID OrgID, contractID string) (ContractRecord, error) {
	row, err := c.queries.GetContract(ctx, store.GetContractParams{ContractID: contractID, OrgID: orgIDText(orgID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return ContractRecord{}, ErrContractNotFound
	}
	if err != nil {
		return ContractRecord{}, fmt.Errorf("load contract %s: %w", contractID, err)
	}
	record := ContractRecord{
		ContractID:       row.ContractID,
		ProductID:        row.ProductID,
		Status:           row.State,
		PaymentState:     row.PaymentState,
		EntitlementState: row.EntitlementState,
		StartsAt:         row.StartsAt.Time.UTC(),
		EndsAt:           timePtr(row.EndsAt),
	}
	if _, err := c.ApplyDueBillingWork(ctx, orgID, record.ProductID); err != nil {
		return ContractRecord{}, err
	}
	now, err := c.BusinessNow(ctx, c.queries, orgID, record.ProductID)
	if err != nil {
		return ContractRecord{}, err
	}
	phase, err := c.queries.GetCurrentContractPhase(ctx, store.GetCurrentContractPhaseParams{ContractID: contractID, Now: timestamptz(now)})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ContractRecord{}, fmt.Errorf("load contract phase %s: %w", contractID, err)
	}
	if err == nil {
		record.PhaseID = phase.PhaseID
		record.PlanID = phase.PlanID
		record.PhaseStart = timePtr(phase.EffectiveStart)
		record.PhaseEnd = timePtr(phase.EffectiveEnd)
	}
	record.CadenceKind = "anniversary_monthly"
	if err := c.loadPendingScheduledContractChange(ctx, orgID, &record); err != nil {
		return ContractRecord{}, err
	}
	return record, nil
}

func (c *Client) loadPendingScheduledContractChange(ctx context.Context, orgID OrgID, record *ContractRecord) error {
	if record == nil || record.ContractID == "" {
		return nil
	}
	row, err := c.queries.GetPendingScheduledContractChange(ctx, store.GetPendingScheduledContractChangeParams{
		ContractID: record.ContractID,
		OrgID:      orgIDText(orgID),
		ProductID:  record.ProductID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load pending scheduled contract change %s: %w", record.ContractID, err)
	}
	effectiveAt := row.RequestedEffectiveAt.Time.UTC()
	record.PendingChangeID = row.ChangeID
	record.PendingChangeType = row.ChangeType
	record.PendingChangeTargetPlanID = row.TargetPlanID
	record.PendingChangeEffectiveAt = &effectiveAt
	return nil
}

func (c *Client) CancelContract(ctx context.Context, orgID OrgID, contractID string) (ContractRecord, error) {
	record, err := c.GetContract(ctx, orgID, contractID)
	if err != nil {
		return ContractRecord{}, err
	}
	cycle, err := c.EnsureOpenBillingCycle(ctx, orgID, record.ProductID)
	if err != nil {
		return ContractRecord{}, err
	}
	now, err := c.BusinessNow(ctx, c.queries, orgID, record.ProductID)
	if err != nil {
		return ContractRecord{}, err
	}
	err = c.WithTx(ctx, "billing.contract.cancel", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		if err := q.ScheduleContractCancellation(ctx, store.ScheduleContractCancellationParams{ContractID: contractID, OrgID: orgIDText(orgID), CancelAt: timestamptz(cycle.EndsAt)}); err != nil {
			return fmt.Errorf("schedule contract cancellation: %w", err)
		}
		if err := q.ScheduleContractPhaseCancellation(ctx, store.ScheduleContractPhaseCancellationParams{ContractID: contractID, OrgID: orgIDText(orgID), CancelAt: timestamptz(cycle.EndsAt)}); err != nil {
			return fmt.Errorf("schedule phase cancellation: %w", err)
		}
		changeID := textID("contract_change", contractID, "cancel", cycle.EndsAt.Format(time.RFC3339Nano))
		if err := q.InsertCancellationContractChange(ctx, store.InsertCancellationContractChangeParams{
			ChangeID:             changeID,
			ContractID:           contractID,
			OrgID:                orgIDText(orgID),
			ProductID:            record.ProductID,
			RequestedEffectiveAt: timestamptz(cycle.EndsAt),
			RequestedAt:          timestamptz(now),
		}); err != nil {
			return fmt.Errorf("insert cancellation change: %w", err)
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "contract_cancel_scheduled", AggregateType: "contract", AggregateID: contractID, OrgID: orgID, ProductID: record.ProductID, OccurredAt: now, Payload: map[string]any{"contract_id": contractID, "cycle_id": cycle.CycleID, "cancel_at": cycle.EndsAt.Format(time.RFC3339Nano)}})
	})
	if err != nil {
		return ContractRecord{}, err
	}
	return c.GetContract(ctx, orgID, contractID)
}

func (c *Client) CreateContractChange(ctx context.Context, orgID OrgID, contractID string, req ContractChangeRequest) (ContractChangeResult, error) {
	if req.TargetPlanID == "" {
		return ContractChangeResult{}, fmt.Errorf("target_plan_id is required")
	}
	existing, err := c.GetContract(ctx, orgID, contractID)
	if err != nil {
		return ContractChangeResult{}, err
	}
	target, err := c.loadPlan(ctx, req.TargetPlanID)
	if err != nil {
		return ContractChangeResult{}, err
	}
	current, err := c.loadPlan(ctx, existing.PlanID)
	if err != nil {
		return ContractChangeResult{}, err
	}
	if target.ProductID != existing.ProductID || current.ProductID != existing.ProductID {
		return ContractChangeResult{}, fmt.Errorf("%w: plan product mismatch", ErrUnsupportedChange)
	}
	if target.PlanID == current.PlanID {
		resumed, err := c.resumeCurrentPlan(ctx, orgID, existing)
		if err != nil {
			return ContractChangeResult{}, err
		}
		if resumed {
			return ContractChangeResult{URL: contractReturnURL(req.SuccessURL, map[string]string{"contractAction": "resume", "targetPlanID": target.PlanID}), Status: "resumed"}, nil
		}
		return ContractChangeResult{URL: contractReturnURL(req.SuccessURL, map[string]string{"contractAction": "unchanged", "targetPlanID": target.PlanID}), Status: "unchanged"}, nil
	}
	if target.MonthlyAmountCents <= current.MonthlyAmountCents {
		change, effectiveAt, err := c.scheduleDowngrade(ctx, orgID, existing, target.PlanID)
		if err != nil {
			return ContractChangeResult{}, err
		}
		return ContractChangeResult{URL: contractReturnURL(req.SuccessURL, map[string]string{"contractAction": "downgrade", "contractEffectiveAt": effectiveAt.Format(time.RFC3339Nano), "targetPlanID": target.PlanID}), ChangeID: change, Status: "scheduled"}, nil
	}
	quote, err := c.prepareUpgradeQuote(ctx, orgID, existing, current, target)
	if err != nil {
		return ContractChangeResult{}, err
	}
	quote, err = c.insertPendingUpgrade(ctx, quote)
	if err != nil {
		return ContractChangeResult{}, err
	}
	status := "paid"
	url := req.SuccessURL
	providerInvoiceID := ""
	if c.cfg.UseStripe && c.stripe != nil {
		if c.runtime != nil {
			return ContractChangeResult{
				URL:             contractReturnURL(req.SuccessURL, map[string]string{"contractAction": "upgrade_requested", "contractEffectiveAt": quote.EffectiveAt.Format(time.RFC3339Nano), "targetPlanID": target.PlanID}),
				ChangeID:        quote.ChangeID,
				FinalizationID:  finalizationID("contract_change", quote.ChangeID),
				DocumentID:      documentID("contract_change", quote.ChangeID),
				Status:          "pending",
				PriceDeltaUnits: quote.PriceDeltaUnits,
			}, nil
		}
		stripeURL, stripeInvoiceID, paid, err := c.collectUpgradeInvoice(ctx, quote)
		if err != nil {
			return ContractChangeResult{}, err
		}
		providerInvoiceID = stripeInvoiceID
		if stripeURL != "" {
			url = stripeURL
		}
		if !paid {
			status = "pending"
		}
	}
	if status == "paid" {
		if err := c.applyPaidUpgrade(ctx, quote, providerInvoiceID); err != nil {
			return ContractChangeResult{}, err
		}
		url = contractReturnURL(req.SuccessURL, map[string]string{"contractAction": "upgrade", "contractEffectiveAt": quote.EffectiveAt.Format(time.RFC3339Nano), "targetPlanID": target.PlanID})
	}
	return ContractChangeResult{URL: url, ChangeID: quote.ChangeID, FinalizationID: finalizationID("contract_change", quote.ChangeID), DocumentID: documentID("contract_change", quote.ChangeID), Status: status, PriceDeltaUnits: quote.PriceDeltaUnits}, nil
}

func (c *Client) CollectContractUpgradePayment(ctx context.Context, changeID string) (bool, error) {
	status, err := c.queries.GetContractChangeStatus(ctx, store.GetContractChangeStatusParams{ChangeID: changeID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load contract change status %s: %w", changeID, err)
	}
	if status.ChangeType != "upgrade" {
		return false, fmt.Errorf("%w: contract change %s is %s", ErrUnsupportedChange, changeID, status.ChangeType)
	}
	switch status.State {
	case "applied", "canceled":
		return false, nil
	case "awaiting_payment", "provider_pending":
	default:
		return false, fmt.Errorf("%w: contract change %s has state %s", ErrUnsupportedChange, changeID, status.State)
	}
	quote, err := c.loadContractChangeQuote(ctx, changeID)
	if err != nil {
		return false, err
	}
	_, providerInvoiceID, paid, err := c.collectUpgradeInvoice(ctx, quote)
	if err != nil {
		return false, err
	}
	if !paid {
		return false, nil
	}
	return true, c.applyPaidUpgrade(ctx, quote, providerInvoiceID)
}

type scheduledChangeToCancel struct {
	ChangeID             string
	ChangeType           string
	TargetPlanID         string
	RequestedEffectiveAt time.Time
}

func (c *Client) resumeCurrentPlan(ctx context.Context, orgID OrgID, existing ContractRecord) (bool, error) {
	if existing.ContractID == "" || existing.PlanID == "" {
		return false, nil
	}
	now, err := c.BusinessNow(ctx, c.queries, orgID, existing.ProductID)
	if err != nil {
		return false, err
	}
	resumed := false
	err = c.WithTx(ctx, "billing.contract.resume_current_plan", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		rows, err := q.ListScheduledContractChangesForResume(ctx, store.ListScheduledContractChangesForResumeParams{
			ContractID: existing.ContractID,
			OrgID:      orgIDText(orgID),
			ProductID:  existing.ProductID,
		})
		if err != nil {
			return fmt.Errorf("query scheduled contract changes to resume: %w", err)
		}
		changes := []scheduledChangeToCancel{}
		for _, row := range rows {
			changes = append(changes, scheduledChangeToCancel{
				ChangeID:             row.ChangeID,
				ChangeType:           row.ChangeType,
				TargetPlanID:         row.TargetPlanID,
				RequestedEffectiveAt: row.RequestedEffectiveAt.Time.UTC(),
			})
		}
		if len(changes) == 0 {
			return nil
		}
		resumed = true
		if err := q.CancelScheduledContractChangesForResume(ctx, store.CancelScheduledContractChangesForResumeParams{
			ContractID: existing.ContractID,
			OrgID:      orgIDText(orgID),
			ProductID:  existing.ProductID,
		}); err != nil {
			return fmt.Errorf("cancel scheduled contract changes during resume: %w", err)
		}
		if err := q.RestoreContractDuringResume(ctx, store.RestoreContractDuringResumeParams{
			ContractID: existing.ContractID,
			OrgID:      orgIDText(orgID),
		}); err != nil {
			return fmt.Errorf("restore current contract during resume: %w", err)
		}
		for _, change := range changes {
			if change.ChangeType == "cancel" {
				if err := q.RestorePhaseBoundaryDuringCancelResume(ctx, store.RestorePhaseBoundaryDuringCancelResumeParams{
					ContractID:           existing.ContractID,
					OrgID:                orgIDText(orgID),
					ProductID:            existing.ProductID,
					RequestedEffectiveAt: timestamptz(change.RequestedEffectiveAt),
				}); err != nil {
					return fmt.Errorf("restore phase boundary during cancellation resume: %w", err)
				}
			}
			if err := appendEvent(ctx, tx, q, eventFact{EventType: "contract_change_canceled", AggregateType: "contract_change", AggregateID: change.ChangeID, OrgID: orgID, ProductID: existing.ProductID, OccurredAt: now, Payload: map[string]any{"contract_id": existing.ContractID, "change_id": change.ChangeID, "change_type": change.ChangeType, "target_plan_id": change.TargetPlanID, "pricing_plan_id": existing.PlanID, "requested_effective_at": change.RequestedEffectiveAt.Format(time.RFC3339Nano), "reason": "resume_current_plan"}}); err != nil {
				return err
			}
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "contract_resume_applied", AggregateType: "contract", AggregateID: existing.ContractID, OrgID: orgID, ProductID: existing.ProductID, OccurredAt: now, Payload: map[string]any{"contract_id": existing.ContractID, "pricing_plan_id": existing.PlanID, "canceled_changes": len(changes), "reason": "resume_current_plan"}})
	})
	if err != nil {
		return false, err
	}
	return resumed, nil
}

func (c *Client) scheduleDowngrade(ctx context.Context, orgID OrgID, existing ContractRecord, targetPlanID string) (string, time.Time, error) {
	cycle, err := c.EnsureOpenBillingCycle(ctx, orgID, existing.ProductID)
	if err != nil {
		return "", time.Time{}, err
	}
	now, err := c.BusinessNow(ctx, c.queries, orgID, existing.ProductID)
	if err != nil {
		return "", time.Time{}, err
	}
	changeID := changeID(existing.ContractID, targetPlanID, cycle.EndsAt)
	toPhaseID := phaseID(existing.ContractID, targetPlanID, cycle.EndsAt)
	err = c.WithTx(ctx, "billing.contract_change.schedule_downgrade", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		if err := q.InsertScheduledDowngrade(ctx, store.InsertScheduledDowngradeParams{
			ChangeID:             changeID,
			ContractID:           existing.ContractID,
			OrgID:                orgIDText(orgID),
			ProductID:            existing.ProductID,
			RequestedEffectiveAt: timestamptz(cycle.EndsAt),
			FromPhaseID:          existing.PhaseID,
			ToPhaseID:            pgTextValue(toPhaseID),
			TargetPlanID:         pgTextValue(targetPlanID),
			RequestedAt:          timestamptz(now),
		}); err != nil {
			return fmt.Errorf("insert scheduled downgrade: %w", err)
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "contract_change_scheduled", AggregateType: "contract_change", AggregateID: changeID, OrgID: orgID, ProductID: existing.ProductID, OccurredAt: now, Payload: map[string]any{"contract_id": existing.ContractID, "change_id": changeID, "cycle_id": cycle.CycleID, "from_phase_id": existing.PhaseID, "to_phase_id": toPhaseID, "target_plan_id": targetPlanID}})
	})
	return changeID, cycle.EndsAt, err
}

func contractReturnURL(rawURL string, params map[string]string) string {
	if rawURL == "" || len(params) == 0 {
		return rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	for key, value := range params {
		if value != "" {
			query.Set(key, value)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func (c *Client) prepareUpgradeQuote(ctx context.Context, orgID OrgID, existing ContractRecord, current PlanRecord, target PlanRecord) (contractChangeQuote, error) {
	cycle, err := c.EnsureOpenBillingCycle(ctx, orgID, existing.ProductID)
	if err != nil {
		return contractChangeQuote{}, err
	}
	now, err := c.BusinessNow(ctx, c.queries, orgID, existing.ProductID)
	if err != nil {
		return contractChangeQuote{}, err
	}
	if !cycle.EndsAt.After(now) {
		return contractChangeQuote{}, fmt.Errorf("%w: upgrade outside open cycle", ErrUnsupportedChange)
	}
	currentPolicies, err := c.planEntitlementPolicies(ctx, current.PlanID)
	if err != nil {
		return contractChangeQuote{}, err
	}
	targetPolicies, err := c.planEntitlementPolicies(ctx, target.PlanID)
	if err != nil {
		return contractChangeQuote{}, err
	}
	deltas := entitlementDeltas(currentPolicies, targetPolicies, now, cycle)
	remaining := cycle.EndsAt.Sub(now)
	period := cycle.EndsAt.Sub(cycle.StartsAt)
	priceDeltaCents := prorateCents(target.MonthlyAmountCents-current.MonthlyAmountCents, remaining, period)
	units, err := moneyUnitsFromCents(int64(priceDeltaCents))
	if err != nil {
		return contractChangeQuote{}, err
	}
	toPhaseID := phaseID(existing.ContractID, target.PlanID, now)
	changeID := changeID(existing.ContractID, target.PlanID, now)
	providerRequestID := textID("stripe_upgrade_request", changeID, time.Now().UTC().Format(time.RFC3339Nano))
	return contractChangeQuote{ChangeID: changeID, ContractID: existing.ContractID, OrgID: orgID, ProductID: existing.ProductID, FromPlanID: current.PlanID, TargetPlanID: target.PlanID, FromPhaseID: existing.PhaseID, ToPhaseID: toPhaseID, CycleID: cycle.CycleID, CycleStart: cycle.StartsAt, CycleEnd: cycle.EndsAt, EffectiveAt: now, RequestedAt: now, ProviderRequestID: providerRequestID, PriceDeltaCents: priceDeltaCents, PriceDeltaUnits: units, Deltas: deltas, TargetPolicies: targetPolicies}, nil
}

func (c *Client) insertPendingUpgrade(ctx context.Context, quote contractChangeQuote) (contractChangeQuote, error) {
	payload, err := json.Marshal(quote)
	if err != nil {
		return quote, err
	}
	finalizationIDValue := finalizationID("contract_change", quote.ChangeID)
	documentIDValue := documentID("contract_change", quote.ChangeID)
	err = c.WithTx(ctx, "billing.contract_change.request_upgrade", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		number, err := allocateDocumentNumberTx(ctx, q, quote.RequestedAt)
		if err != nil {
			return err
		}
		if err := q.InsertUpgradeContractChange(ctx, store.InsertUpgradeContractChangeParams{
			ChangeID:              quote.ChangeID,
			ContractID:            quote.ContractID,
			OrgID:                 orgIDText(quote.OrgID),
			ProductID:             quote.ProductID,
			RequestedEffectiveAt:  timestamptz(quote.EffectiveAt),
			FromPhaseID:           quote.FromPhaseID,
			ToPhaseID:             pgTextValue(quote.ToPhaseID),
			TargetPlanID:          pgTextValue(quote.TargetPlanID),
			ProviderRequestID:     pgTextValue(quote.ProviderRequestID),
			RequestedAt:           timestamptz(quote.RequestedAt),
			ProrationBasisCycleID: pgTextValue(quote.CycleID),
			PriceDeltaUnits:       int64(quote.PriceDeltaUnits),
			ProrationNumerator:    int64(quote.CycleEnd.Sub(quote.EffectiveAt)),
			ProrationDenominator:  int64(quote.CycleEnd.Sub(quote.CycleStart)),
			Payload:               payload,
		}); err != nil {
			return fmt.Errorf("insert upgrade change: %w", err)
		}
		snapshotHash := textID("document_snapshot", string(payload))
		if err := q.InsertUpgradeFinalization(ctx, store.InsertUpgradeFinalizationParams{
			FinalizationID: finalizationIDValue,
			ChangeID:       quote.ChangeID,
			CycleID:        pgTextValue(quote.CycleID),
			OrgID:          orgIDText(quote.OrgID),
			ProductID:      quote.ProductID,
			RequestedAt:    timestamptz(quote.RequestedAt),
			SnapshotHash:   pgTextValue(snapshotHash),
			Metadata:       payload,
		}); err != nil {
			return fmt.Errorf("insert upgrade finalization: %w", err)
		}
		if err := q.InsertUpgradeDocument(ctx, store.InsertUpgradeDocumentParams{
			DocumentID:           documentIDValue,
			DocumentNumber:       pgTextValue(number),
			FinalizationID:       pgTextValue(finalizationIDValue),
			OrgID:                orgIDText(quote.OrgID),
			ProductID:            quote.ProductID,
			CycleID:              pgTextValue(quote.CycleID),
			ChangeID:             pgTextValue(quote.ChangeID),
			PeriodStart:          timestamptz(quote.EffectiveAt),
			PeriodEnd:            timestamptz(quote.CycleEnd),
			IssuedAt:             timestamptz(quote.RequestedAt),
			SubtotalUnits:        int64(quote.PriceDeltaUnits),
			TotalDueUnits:        int64(quote.PriceDeltaUnits),
			DocumentSnapshotJson: payload,
			ContentHash:          snapshotHash,
		}); err != nil {
			return fmt.Errorf("insert upgrade document: %w", err)
		}
		if err := q.LinkUpgradeDocumentFinalization(ctx, store.LinkUpgradeDocumentFinalizationParams{
			FinalizationID: finalizationIDValue,
			DocumentID:     pgTextValue(documentIDValue),
		}); err != nil {
			return fmt.Errorf("link upgrade document: %w", err)
		}
		if err := appendEvent(ctx, tx, q, eventFact{EventType: "contract_change_requested", AggregateType: "contract_change", AggregateID: quote.ChangeID, OrgID: quote.OrgID, ProductID: quote.ProductID, OccurredAt: quote.RequestedAt, Payload: map[string]any{"contract_id": quote.ContractID, "change_id": quote.ChangeID, "cycle_id": quote.CycleID, "from_phase_id": quote.FromPhaseID, "to_phase_id": quote.ToPhaseID, "target_plan_id": quote.TargetPlanID, "finalization_id": finalizationIDValue, "document_id": documentIDValue, "document_kind": "invoice", "price_delta_units": quote.PriceDeltaUnits}}); err != nil {
			return err
		}
		if c.runtime != nil && c.cfg.UseStripe && c.stripe != nil {
			if err := c.runtime.EnqueueContractUpgradePaymentTx(ctx, tx, quote.ChangeID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return quote, err
	}
	if quote.ProviderRequestID, err = c.queries.GetContractChangeProviderRequestID(ctx, store.GetContractChangeProviderRequestIDParams{ChangeID: quote.ChangeID}); err != nil {
		return quote, fmt.Errorf("load upgrade provider request id: %w", err)
	}
	if quote.ProviderRequestID == "" {
		return quote, fmt.Errorf("upgrade change %s missing provider_request_id", quote.ChangeID)
	}
	return quote, nil
}

func (c *Client) applyPaidUpgrade(ctx context.Context, quote contractChangeQuote, providerInvoiceID string) error {
	finalizationIDValue := finalizationID("contract_change", quote.ChangeID)
	documentIDValue := documentID("contract_change", quote.ChangeID)
	if err := c.WithTx(ctx, "billing.contract_change.apply_upgrade", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		state, err := q.GetContractChangeStateForUpdate(ctx, store.GetContractChangeStateForUpdateParams{ChangeID: quote.ChangeID})
		if err != nil {
			return fmt.Errorf("lock upgrade change: %w", err)
		}
		if state == "applied" {
			return nil
		}
		if state != "awaiting_payment" && state != "provider_pending" {
			return fmt.Errorf("%w: upgrade change %s has state %s", ErrUnsupportedChange, quote.ChangeID, state)
		}
		if err := c.lockOrgProductTx(ctx, tx, quote.OrgID, quote.ProductID); err != nil {
			return err
		}
		closed, err := c.supersedeContractPhaseForUpgradeTx(ctx, tx, quote)
		if err != nil {
			return err
		}
		if closed {
			if err := appendEvent(ctx, tx, q, eventFact{EventType: "contract_phase_closed", AggregateType: "contract_phase", AggregateID: quote.FromPhaseID, OrgID: quote.OrgID, ProductID: quote.ProductID, OccurredAt: quote.EffectiveAt, Payload: map[string]any{"contract_id": quote.ContractID, "change_id": quote.ChangeID, "cycle_id": quote.CycleID, "pricing_phase_id": quote.FromPhaseID, "pricing_plan_id": quote.FromPlanID, "effective_at": quote.EffectiveAt.Format(time.RFC3339Nano), "reason": "upgrade"}}); err != nil {
				return err
			}
		}
		if err := c.insertContractPhaseTx(ctx, tx, quote.OrgID, quote.ProductID, quote.ContractID, quote.ToPhaseID, quote.TargetPlanID, "active", "paid", quote.EffectiveAt); err != nil {
			return err
		}
		if err := appendEvent(ctx, tx, q, eventFact{EventType: "contract_phase_started", AggregateType: "contract_phase", AggregateID: quote.ToPhaseID, OrgID: quote.OrgID, ProductID: quote.ProductID, OccurredAt: quote.EffectiveAt, Payload: map[string]any{"contract_id": quote.ContractID, "change_id": quote.ChangeID, "cycle_id": quote.CycleID, "pricing_phase_id": quote.ToPhaseID, "pricing_plan_id": quote.TargetPlanID, "effective_at": quote.EffectiveAt.Format(time.RFC3339Nano), "reason": "upgrade"}}); err != nil {
			return err
		}
		if err := q.LinkSupersededContractPhase(ctx, store.LinkSupersededContractPhaseParams{
			PhaseID:             quote.FromPhaseID,
			ContractID:          quote.ContractID,
			SupersededByPhaseID: pgTextValue(quote.ToPhaseID),
		}); err != nil {
			return fmt.Errorf("link superseded phase: %w", err)
		}
		if err := c.copyPlanEntitlementLinesTx(ctx, tx, quote.OrgID, quote.ProductID, quote.ContractID, quote.ToPhaseID, quote.TargetPolicies, quote.CycleEnd); err != nil {
			return err
		}
		for _, delta := range quote.Deltas {
			if err := c.insertUpgradeDeltaGrantTx(ctx, tx, quote, delta); err != nil {
				return err
			}
		}
		if err := q.UpdateUpgradedContract(ctx, store.UpdateUpgradedContractParams{
			ContractID:  quote.ContractID,
			OrgID:       orgIDText(quote.OrgID),
			DisplayName: quote.TargetPlanID,
		}); err != nil {
			return fmt.Errorf("update upgraded contract: %w", err)
		}
		if err := q.MarkUpgradeContractChangeApplied(ctx, store.MarkUpgradeContractChangeAppliedParams{
			ChangeID:          quote.ChangeID,
			EffectiveAt:       timestamptz(quote.EffectiveAt),
			ProviderInvoiceID: providerInvoiceID,
		}); err != nil {
			return fmt.Errorf("mark upgrade applied: %w", err)
		}
		if err := q.MarkUpgradeDocumentPaid(ctx, store.MarkUpgradeDocumentPaidParams{
			DocumentID:        documentIDValue,
			ProviderInvoiceID: providerInvoiceID,
			IssuedAt:          timestamptz(quote.EffectiveAt),
		}); err != nil {
			return fmt.Errorf("mark upgrade document paid: %w", err)
		}
		if err := q.MarkUpgradeFinalizationPaid(ctx, store.MarkUpgradeFinalizationPaidParams{
			FinalizationID: finalizationIDValue,
			CompletedAt:    timestamptz(quote.EffectiveAt),
			DocumentID:     pgTextValue(documentIDValue),
		}); err != nil {
			return fmt.Errorf("mark upgrade finalization paid: %w", err)
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "contract_change_applied", AggregateType: "contract_change", AggregateID: quote.ChangeID, OrgID: quote.OrgID, ProductID: quote.ProductID, OccurredAt: quote.EffectiveAt, Payload: map[string]any{"contract_id": quote.ContractID, "change_id": quote.ChangeID, "cycle_id": quote.CycleID, "from_phase_id": quote.FromPhaseID, "to_phase_id": quote.ToPhaseID, "pricing_plan_id": quote.TargetPlanID, "finalization_id": finalizationIDValue, "document_id": documentIDValue, "document_kind": "invoice", "provider_invoice_id": providerInvoiceID}})
	}); err != nil {
		return err
	}
	_, err := c.PostPendingGrantDeposits(ctx, quote.OrgID, quote.ProductID)
	return err
}

func (c *Client) supersedeContractPhaseForUpgradeTx(ctx context.Context, tx pgx.Tx, quote contractChangeQuote) (bool, error) {
	row, err := c.queries.WithTx(tx).SupersedeContractPhaseForUpgrade(ctx, store.SupersedeContractPhaseForUpgradeParams{
		PhaseID:     quote.FromPhaseID,
		ContractID:  quote.ContractID,
		EffectiveAt: timestamptz(quote.EffectiveAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("close old phase: %w", err)
	}
	if !row.EffectiveStart.Time.Before(quote.EffectiveAt) && row.EntitlementState != "voided" {
		return false, fmt.Errorf("zero-duration upgrade phase %s did not void entitlement state", quote.FromPhaseID)
	}
	return true, nil
}

type dueContractChange struct {
	ChangeID             string
	ContractID           string
	ChangeType           string
	FromPhaseID          string
	ToPhaseID            string
	TargetPlanID         string
	RequestedEffectiveAt time.Time
}

func (c *Client) applyDueContractChangesTx(ctx context.Context, tx pgx.Tx, q *store.Queries, orgID OrgID, productID string, cycle billingCycle, now time.Time) (uint64, error) {
	rows, err := q.ListDueContractChangesForUpdate(ctx, store.ListDueContractChangesForUpdateParams{
		OrgID:                orgIDText(orgID),
		ProductID:            productID,
		RequestedEffectiveAt: timestamptz(cycle.EndsAt),
	})
	if err != nil {
		return 0, fmt.Errorf("query due contract changes: %w", err)
	}
	changes := []dueContractChange{}
	for _, row := range rows {
		changes = append(changes, dueContractChange{
			ChangeID:             row.ChangeID,
			ContractID:           row.ContractID,
			ChangeType:           row.ChangeType,
			FromPhaseID:          row.FromPhaseID,
			ToPhaseID:            row.ToPhaseID,
			TargetPlanID:         row.TargetPlanID,
			RequestedEffectiveAt: row.RequestedEffectiveAt.Time.UTC(),
		})
	}
	var applied uint64
	for _, change := range changes {
		switch change.ChangeType {
		case "cancel":
			if err := c.applyScheduledCancelTx(ctx, tx, q, orgID, productID, cycle, now, change); err != nil {
				return applied, err
			}
		case "downgrade":
			if err := c.applyScheduledDowngradeTx(ctx, tx, q, orgID, productID, cycle, now, change); err != nil {
				return applied, err
			}
		default:
			return applied, fmt.Errorf("unsupported scheduled contract change %s type %s", change.ChangeID, change.ChangeType)
		}
		applied++
	}
	return applied, nil
}

func (c *Client) applyScheduledCancelTx(ctx context.Context, tx pgx.Tx, q *store.Queries, orgID OrgID, productID string, cycle billingCycle, now time.Time, change dueContractChange) error {
	if err := c.markContractChangeApplyingTx(ctx, tx, change.ChangeID); err != nil {
		return err
	}
	phaseRows, err := q.ListCancelableContractPhasesForUpdate(ctx, store.ListCancelableContractPhasesForUpdateParams{
		ContractID:           change.ContractID,
		OrgID:                orgIDText(orgID),
		ProductID:            productID,
		EffectiveStartBefore: timestamptz(cycle.EndsAt),
	})
	if err != nil {
		return fmt.Errorf("query cancellation phases: %w", err)
	}
	type phaseRef struct {
		PhaseID string
		PlanID  string
	}
	phases := []phaseRef{}
	for _, row := range phaseRows {
		phases = append(phases, phaseRef{PhaseID: row.PhaseID, PlanID: row.PlanID})
	}
	for _, phase := range phases {
		if err := c.closeContractPhaseTx(ctx, tx, q, orgID, productID, cycle, change, phase.PhaseID, phase.PlanID, "cancel"); err != nil {
			return err
		}
	}
	if err := q.CloseCanceledContract(ctx, store.CloseCanceledContractParams{
		ContractID: change.ContractID,
		OrgID:      orgIDText(orgID),
		EndedAt:    timestamptz(cycle.EndsAt),
	}); err != nil {
		return fmt.Errorf("close canceled contract: %w", err)
	}
	if err := c.markContractChangeAppliedTx(ctx, tx, change.ChangeID, cycle.EndsAt); err != nil {
		return err
	}
	return appendEvent(ctx, tx, q, eventFact{EventType: "contract_change_applied", AggregateType: "contract_change", AggregateID: change.ChangeID, OrgID: orgID, ProductID: productID, OccurredAt: cycle.EndsAt, Payload: map[string]any{"contract_id": change.ContractID, "change_id": change.ChangeID, "change_type": "cancel", "cycle_id": cycle.CycleID, "effective_at": cycle.EndsAt.Format(time.RFC3339Nano), "applied_at": now.UTC().Format(time.RFC3339Nano)}})
}

func (c *Client) applyScheduledDowngradeTx(ctx context.Context, tx pgx.Tx, q *store.Queries, orgID OrgID, productID string, cycle billingCycle, now time.Time, change dueContractChange) error {
	if change.TargetPlanID == "" {
		return fmt.Errorf("scheduled downgrade %s missing target_plan_id", change.ChangeID)
	}
	targetPolicies, err := c.planEntitlementPolicies(ctx, change.TargetPlanID)
	if err != nil {
		return err
	}
	if change.ToPhaseID == "" {
		change.ToPhaseID = phaseID(change.ContractID, change.TargetPlanID, cycle.EndsAt)
	}
	if change.FromPhaseID == "" {
		fromPhaseID, err := c.activePhaseAtTx(ctx, tx, change.ContractID, orgID, productID, cycle.EndsAt)
		if err != nil {
			return err
		}
		change.FromPhaseID = fromPhaseID
	}
	if err := c.markContractChangeApplyingTx(ctx, tx, change.ChangeID); err != nil {
		return err
	}
	fromPlanID, err := c.phasePlanIDTx(ctx, tx, change.FromPhaseID)
	if err != nil {
		return err
	}
	if err := c.closeContractPhaseTx(ctx, tx, q, orgID, productID, cycle, change, change.FromPhaseID, fromPlanID, "downgrade"); err != nil {
		return err
	}
	if err := c.insertContractPhaseTx(ctx, tx, orgID, productID, change.ContractID, change.ToPhaseID, change.TargetPlanID, "active", "paid", cycle.EndsAt); err != nil {
		return err
	}
	if err := c.copyPlanEntitlementLinesTx(ctx, tx, orgID, productID, change.ContractID, change.ToPhaseID, targetPolicies, cycle.EndsAt); err != nil {
		return err
	}
	if err := q.UpdateDowngradedContract(ctx, store.UpdateDowngradedContractParams{
		ContractID:  change.ContractID,
		OrgID:       orgIDText(orgID),
		DisplayName: change.TargetPlanID,
	}); err != nil {
		return fmt.Errorf("update downgraded contract: %w", err)
	}
	if err := c.markContractChangeAppliedTx(ctx, tx, change.ChangeID, cycle.EndsAt); err != nil {
		return err
	}
	if err := appendEvent(ctx, tx, q, eventFact{EventType: "contract_phase_started", AggregateType: "contract_phase", AggregateID: change.ToPhaseID, OrgID: orgID, ProductID: productID, OccurredAt: cycle.EndsAt, Payload: map[string]any{"contract_id": change.ContractID, "change_id": change.ChangeID, "cycle_id": cycle.CycleID, "pricing_phase_id": change.ToPhaseID, "pricing_plan_id": change.TargetPlanID, "effective_at": cycle.EndsAt.Format(time.RFC3339Nano), "reason": "downgrade"}}); err != nil {
		return err
	}
	return appendEvent(ctx, tx, q, eventFact{EventType: "contract_change_applied", AggregateType: "contract_change", AggregateID: change.ChangeID, OrgID: orgID, ProductID: productID, OccurredAt: cycle.EndsAt, Payload: map[string]any{"contract_id": change.ContractID, "change_id": change.ChangeID, "change_type": "downgrade", "cycle_id": cycle.CycleID, "from_phase_id": change.FromPhaseID, "to_phase_id": change.ToPhaseID, "pricing_plan_id": change.TargetPlanID, "effective_at": cycle.EndsAt.Format(time.RFC3339Nano), "applied_at": now.UTC().Format(time.RFC3339Nano)}})
}

func (c *Client) markContractChangeApplyingTx(ctx context.Context, tx pgx.Tx, changeID string) error {
	if err := c.queries.WithTx(tx).MarkContractChangeApplying(ctx, store.MarkContractChangeApplyingParams{ChangeID: changeID}); err != nil {
		return fmt.Errorf("mark contract change applying: %w", err)
	}
	return nil
}

func (c *Client) markContractChangeAppliedTx(ctx context.Context, tx pgx.Tx, changeID string, effectiveAt time.Time) error {
	if err := c.queries.WithTx(tx).MarkContractChangeApplied(ctx, store.MarkContractChangeAppliedParams{ChangeID: changeID, EffectiveAt: timestamptz(effectiveAt)}); err != nil {
		return fmt.Errorf("mark contract change applied: %w", err)
	}
	return nil
}

func (c *Client) closeContractPhaseTx(ctx context.Context, tx pgx.Tx, q *store.Queries, orgID OrgID, productID string, cycle billingCycle, change dueContractChange, phaseID string, planID string, reason string) error {
	if phaseID == "" {
		return nil
	}
	rowsAffected, err := q.CloseContractPhase(ctx, store.CloseContractPhaseParams{
		PhaseID:      phaseID,
		ContractID:   change.ContractID,
		EffectiveEnd: timestamptz(cycle.EndsAt),
	})
	if err != nil {
		return fmt.Errorf("close contract phase: %w", err)
	}
	if rowsAffected == 0 {
		return nil
	}
	return appendEvent(ctx, tx, q, eventFact{EventType: "contract_phase_closed", AggregateType: "contract_phase", AggregateID: phaseID, OrgID: orgID, ProductID: productID, OccurredAt: cycle.EndsAt, Payload: map[string]any{"contract_id": change.ContractID, "change_id": change.ChangeID, "cycle_id": cycle.CycleID, "pricing_phase_id": phaseID, "pricing_plan_id": planID, "effective_at": cycle.EndsAt.Format(time.RFC3339Nano), "reason": reason}})
}

func (c *Client) activePhaseAtTx(ctx context.Context, tx pgx.Tx, contractID string, orgID OrgID, productID string, effectiveAt time.Time) (string, error) {
	phaseID, err := c.queries.WithTx(tx).GetActivePhaseAtBoundary(ctx, store.GetActivePhaseAtBoundaryParams{
		ContractID:  contractID,
		OrgID:       orgIDText(orgID),
		ProductID:   productID,
		EffectiveAt: timestamptz(effectiveAt),
	})
	if err != nil {
		return "", fmt.Errorf("load active phase at boundary: %w", err)
	}
	return phaseID, nil
}

func (c *Client) phasePlanIDTx(ctx context.Context, tx pgx.Tx, phaseID string) (string, error) {
	if phaseID == "" {
		return "", nil
	}
	planID, err := c.queries.WithTx(tx).GetPhasePlanID(ctx, store.GetPhasePlanIDParams{PhaseID: phaseID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load phase plan: %w", err)
	}
	return planID, nil
}

func (c *Client) activateCatalogContract(ctx context.Context, orgID OrgID, productID, planID, contractID, phaseID string, effectiveAt time.Time, lineActiveFrom time.Time) error {
	policies, err := c.planEntitlementPolicies(ctx, planID)
	if err != nil {
		return err
	}
	var freeFinalizationID string
	err = c.WithTx(ctx, "billing.contract.activate", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		if err := c.lockOrgProductTx(ctx, tx, orgID, productID); err != nil {
			return err
		}
		alreadyActive, err := q.ContractPhaseAlreadyActive(ctx, store.ContractPhaseAlreadyActiveParams{PhaseID: phaseID, ContractID: contractID})
		if err != nil {
			return fmt.Errorf("check active contract phase: %w", err)
		}
		if !alreadyActive {
			_, finalizationID, err := c.splitCycleForContractActivationTx(ctx, tx, q, orgID, productID, effectiveAt)
			if err != nil {
				return err
			}
			freeFinalizationID = finalizationID
		}
		if err := c.insertContractTx(ctx, tx, orgID, productID, contractID, planID, effectiveAt); err != nil {
			return err
		}
		if err := c.insertContractPhaseTx(ctx, tx, orgID, productID, contractID, phaseID, planID, "active", "paid", effectiveAt); err != nil {
			return err
		}
		if err := c.copyPlanEntitlementLinesTx(ctx, tx, orgID, productID, contractID, phaseID, policies, lineActiveFrom); err != nil {
			return err
		}
		if alreadyActive {
			return nil
		}
		if err := appendEvent(ctx, tx, q, eventFact{EventType: "contract_activated", AggregateType: "contract", AggregateID: contractID, OrgID: orgID, ProductID: productID, OccurredAt: effectiveAt, Payload: map[string]any{"contract_id": contractID, "pricing_phase_id": phaseID, "pricing_plan_id": planID}}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if freeFinalizationID != "" && c.runtime == nil {
		_, err = c.FinalizeBillingFinalization(ctx, freeFinalizationID)
	}
	return err
}

func (c *Client) insertContractTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID, contractID, planID string, startsAt time.Time) error {
	if err := c.queries.WithTx(tx).UpsertContract(ctx, store.UpsertContractParams{
		ContractID:  contractID,
		OrgID:       orgIDText(orgID),
		ProductID:   productID,
		DisplayName: planID,
		StartsAt:    timestamptz(startsAt),
	}); err != nil {
		return fmt.Errorf("upsert contract: %w", err)
	}
	return nil
}

func (c *Client) insertContractPhaseTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID, contractID, phaseID, planID, state, paymentState string, effectiveAt time.Time) error {
	plan, err := c.loadPlan(ctx, planID)
	if err != nil {
		return err
	}
	units, err := moneyUnitsFromCents(int64(plan.MonthlyAmountCents))
	if err != nil {
		return err
	}
	if err := c.queries.WithTx(tx).UpsertContractPhase(ctx, store.UpsertContractPhaseParams{
		PhaseID:              phaseID,
		ContractID:           contractID,
		OrgID:                orgIDText(orgID),
		ProductID:            productID,
		PlanID:               pgTextValue(planID),
		State:                state,
		PaymentState:         paymentState,
		Currency:             plan.Currency,
		RecurringAmountUnits: int64(units),
		EffectiveStart:       timestamptz(effectiveAt),
	}); err != nil {
		return fmt.Errorf("insert contract phase: %w", err)
	}
	return nil
}

func (c *Client) copyPlanEntitlementLinesTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID, contractID, phaseID string, policies []planEntitlementPolicy, activeFrom time.Time) error {
	for _, policy := range policies {
		lineID := textID("contract_line", phaseID, policy.PolicyID)
		if err := c.queries.WithTx(tx).UpsertContractEntitlementLine(ctx, store.UpsertContractEntitlementLineParams{
			LineID:               lineID,
			PhaseID:              phaseID,
			ContractID:           contractID,
			OrgID:                orgIDText(orgID),
			ProductID:            productID,
			PolicyID:             pgTextValue(policy.PolicyID),
			ScopeType:            policy.ScopeType,
			ScopeProductID:       policy.ScopeProductID,
			ScopeBucketID:        policy.ScopeBucketID,
			ScopeSkuID:           policy.ScopeSKUID,
			AmountUnits:          int64(policy.AmountUnits),
			RecurrenceAnchorKind: cleanNonEmpty(policy.RecurrenceAnchorKind, "billing_cycle"),
			ProrationMode:        cleanNonEmpty(policy.ProrationMode, "none"),
			PolicyVersion:        policy.PolicyVersion,
			ActiveFrom:           timestamptz(activeFrom),
		}); err != nil {
			return fmt.Errorf("copy entitlement line %s: %w", policy.PolicyID, err)
		}
	}
	return nil
}

func (c *Client) insertUpgradeDeltaGrantTx(ctx context.Context, tx pgx.Tx, quote contractChangeQuote, delta contractEntitlementDelta) error {
	if delta.Amount == 0 {
		return nil
	}
	accountID := ledger.NewID()
	depositID := ledger.NewID()
	policy := delta.Policy
	lineID := textID("contract_line", quote.ToPhaseID, policy.PolicyID)
	sourceRef := "upgrade_delta:" + quote.ChangeID + ":" + policy.PolicyID
	periodID := textID("period", orgIDText(quote.OrgID), sourceRef)
	grantID := grantID(quote.OrgID, "contract", policy.ScopeType, policy.ScopeProductID, policy.ScopeBucketID, policy.ScopeSKUID, sourceRef)
	q := c.queries.WithTx(tx)
	if err := q.InsertUpgradeDeltaPeriod(ctx, store.InsertUpgradeDeltaPeriodParams{
		PeriodID:          periodID,
		OrgID:             orgIDText(quote.OrgID),
		ProductID:         quote.ProductID,
		CycleID:           pgTextValue(quote.CycleID),
		PolicyID:          pgTextValue(policy.PolicyID),
		ContractID:        pgTextValue(quote.ContractID),
		PhaseID:           pgTextValue(quote.ToPhaseID),
		LineID:            pgTextValue(lineID),
		ScopeType:         policy.ScopeType,
		ScopeProductID:    policy.ScopeProductID,
		ScopeBucketID:     policy.ScopeBucketID,
		ScopeSkuID:        policy.ScopeSKUID,
		AmountUnits:       int64(delta.Amount),
		PeriodStart:       timestamptz(quote.EffectiveAt),
		PeriodEnd:         timestamptz(quote.CycleEnd),
		PolicyVersion:     policy.PolicyVersion,
		SourceReferenceID: sourceRef,
		ChangeID:          pgTextValue(quote.ChangeID),
	}); err != nil {
		return fmt.Errorf("insert upgrade delta period: %w", err)
	}
	if err := q.InsertUpgradeDeltaGrant(ctx, store.InsertUpgradeDeltaGrantParams{
		GrantID:             grantID,
		OrgID:               orgIDText(quote.OrgID),
		ScopeType:           policy.ScopeType,
		ScopeProductID:      policy.ScopeProductID,
		ScopeBucketID:       policy.ScopeBucketID,
		ScopeSkuID:          policy.ScopeSKUID,
		Amount:              int64(delta.Amount),
		SourceReferenceID:   sourceRef,
		EntitlementPeriodID: pgTextValue(periodID),
		PolicyVersion:       policy.PolicyVersion,
		StartsAt:            timestamptz(quote.EffectiveAt),
		PeriodEnd:           timestamptz(quote.CycleEnd),
		AccountID:           accountID.Bytes(),
		DepositTransferID:   depositID.Bytes(),
	}); err != nil {
		return fmt.Errorf("insert upgrade delta grant: %w", err)
	}
	return appendEvent(ctx, tx, q, eventFact{EventType: "grant_issued", AggregateType: "credit_grant", AggregateID: grantID, OrgID: quote.OrgID, ProductID: quote.ProductID, OccurredAt: quote.EffectiveAt, Payload: map[string]any{"grant_id": grantID, "contract_id": quote.ContractID, "pricing_contract_id": quote.ContractID, "phase_id": quote.ToPhaseID, "pricing_phase_id": quote.ToPhaseID, "pricing_plan_id": quote.TargetPlanID, "change_id": quote.ChangeID, "cycle_id": quote.CycleID, "source": "contract", "amount": delta.Amount}})
}

func (c *Client) planEntitlementPolicies(ctx context.Context, planID string) ([]planEntitlementPolicy, error) {
	rows, err := c.queries.ListPlanEntitlementPolicies(ctx, store.ListPlanEntitlementPoliciesParams{PlanID: planID})
	if err != nil {
		return nil, fmt.Errorf("query plan entitlement policies: %w", err)
	}
	out := []planEntitlementPolicy{}
	for _, row := range rows {
		policy := planEntitlementPolicy{
			PolicyID:             row.PolicyID,
			ProductID:            row.ProductID,
			ScopeType:            row.ScopeType,
			ScopeProductID:       row.ScopeProductID,
			ScopeBucketID:        row.ScopeBucketID,
			ScopeSKUID:           row.ScopeSkuID,
			AmountUnits:          uint64(row.AmountUnits),
			Cadence:              row.Cadence,
			RecurrenceAnchorKind: row.AnchorKind,
			ProrationMode:        row.ProrationMode,
			PolicyVersion:        row.PolicyVersion,
		}
		if policy.RecurrenceAnchorKind == "calendar_month" {
			policy.RecurrenceAnchorKind = "billing_cycle"
		}
		out = append(out, policy)
	}
	return out, nil
}

func (c *Client) loadPlan(ctx context.Context, planID string) (PlanRecord, error) {
	row, err := c.queries.GetActivePlan(ctx, store.GetActivePlanParams{PlanID: planID})
	if errors.Is(err, pgx.ErrNoRows) {
		return PlanRecord{}, fmt.Errorf("plan %s not found", planID)
	}
	if err != nil {
		return PlanRecord{}, fmt.Errorf("load plan: %w", err)
	}
	return PlanRecord{
		PlanID:             row.PlanID,
		ProductID:          row.ProductID,
		DisplayName:        row.DisplayName,
		BillingMode:        row.BillingMode,
		Tier:               row.Tier,
		Currency:           row.Currency,
		MonthlyAmountCents: uint64(row.MonthlyAmountCents),
		AnnualAmountCents:  uint64(row.AnnualAmountCents),
		Active:             row.Active,
		IsDefault:          row.IsDefault,
	}, nil
}

func entitlementDeltas(current, target []planEntitlementPolicy, now time.Time, cycle billingCycle) []contractEntitlementDelta {
	currentByScope := map[string]uint64{}
	for _, policy := range current {
		currentByScope[entitlementPolicyScopeKey(policy)] = policy.AmountUnits
	}
	out := []contractEntitlementDelta{}
	for _, policy := range target {
		delta := uint64(0)
		if policy.AmountUnits > currentByScope[entitlementPolicyScopeKey(policy)] {
			delta = policy.AmountUnits - currentByScope[entitlementPolicyScopeKey(policy)]
		}
		if delta == 0 {
			continue
		}
		out = append(out, contractEntitlementDelta{Policy: policy, Amount: prorateUnits(delta, int64(cycle.EndsAt.Sub(now)), int64(cycle.EndsAt.Sub(cycle.StartsAt)))})
	}
	return out
}

func entitlementPolicyScopeKey(policy planEntitlementPolicy) string {
	return policy.ScopeType + ":" + policy.ScopeProductID + ":" + policy.ScopeBucketID + ":" + policy.ScopeSKUID
}

func prorateCents(cents uint64, remaining time.Duration, period time.Duration) uint64 {
	if cents == 0 || remaining <= 0 || period <= 0 {
		return 0
	}
	return uint64(math.Ceil(float64(cents) * float64(remaining) / float64(period)))
}

func allocateDocumentNumberTx(ctx context.Context, q *store.Queries, issuedAt time.Time) (string, error) {
	year := issuedAt.UTC().Year()
	if err := q.EnsureDocumentNumberAllocator(ctx, store.EnsureDocumentNumberAllocatorParams{DocumentYear: int32(year)}); err != nil {
		return "", fmt.Errorf("ensure document allocator: %w", err)
	}
	next, err := q.LockDocumentNumberAllocator(ctx, store.LockDocumentNumberAllocatorParams{DocumentYear: int32(year)})
	if err != nil {
		return "", fmt.Errorf("lock document allocator: %w", err)
	}
	if err := q.AdvanceDocumentNumberAllocator(ctx, store.AdvanceDocumentNumberAllocatorParams{DocumentYear: int32(year)}); err != nil {
		return "", fmt.Errorf("advance document allocator: %w", err)
	}
	return fmt.Sprintf("VS-%d-%06d", year, next), nil
}
