package billing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const ledgerUnitsPerCent uint64 = 100_000

func orgIDText(orgID OrgID) string {
	return strconv.FormatUint(uint64(orgID), 10)
}

func textID(kind string, parts ...string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(kind))
	for _, part := range parts {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(part))
	}
	sum := h.Sum(nil)
	return kind + "_" + hex.EncodeToString(sum[:16])
}

func monthStartUTC(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func nextMonth(t time.Time) time.Time {
	return monthStartUTC(t).AddDate(0, 1, 0)
}

func cycleID(orgID OrgID, productID string, startsAt time.Time) string {
	return textID("cycle", orgIDText(orgID), productID, startsAt.UTC().Format(time.RFC3339Nano))
}

func freeTierPeriodID(orgID OrgID, policyID string, start, end time.Time) string {
	return textID("period", orgIDText(orgID), "free_tier", policyID, start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano))
}

func contractPeriodID(orgID OrgID, lineID string, cycleID string, start, end time.Time) string {
	return textID("period", orgIDText(orgID), "contract", lineID, cycleID, start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano))
}

func grantID(orgID OrgID, source, scopeType, productID, bucketID, skuID, sourceRef string) string {
	return textID("grant", orgIDText(orgID), source, scopeType, productID, bucketID, skuID, sourceRef)
}

func billingWindowID(productID, sourceType, sourceRef string, seq uint32) string {
	return textID("win", productID, sourceType, sourceRef, strconv.FormatUint(uint64(seq), 10))
}

func contractID(orgID OrgID, productID string) string {
	return textID("contract", orgIDText(orgID), productID)
}

func phaseID(contractID string, planID string, effectiveAt time.Time) string {
	return textID("phase", contractID, planID, effectiveAt.UTC().Format(time.RFC3339Nano))
}

func changeID(contractID, targetPlanID string, effectiveAt time.Time) string {
	return textID("change", contractID, targetPlanID, effectiveAt.UTC().Format(time.RFC3339Nano))
}

func finalizationID(subjectType, subjectID string) string {
	return textID("finalization", subjectType, subjectID)
}

func documentID(subjectType, subjectID string) string {
	return textID("document", subjectType, subjectID)
}

func eventID(eventType, aggregateID string, occurredAt time.Time, payloadHash string) string {
	return textID("evt", eventType, aggregateID, occurredAt.UTC().Format(time.RFC3339Nano), payloadHash)
}

func cleanNonEmpty(parts ...string) string {
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			return strings.TrimSpace(part)
		}
	}
	return ""
}

func moneyUnitsFromCents(cents int64) (uint64, error) {
	if cents < 0 {
		return 0, fmt.Errorf("cents must be non-negative")
	}
	return uint64(cents) * ledgerUnitsPerCent, nil
}
