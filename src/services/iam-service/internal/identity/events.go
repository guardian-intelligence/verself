package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

type iamEventProjectionRow struct {
	EventID                string    `ch:"event_id"`
	EventType              string    `ch:"event_type"`
	EventVersion           uint16    `ch:"event_version"`
	AggregateType          string    `ch:"aggregate_type"`
	AggregateID            string    `ch:"aggregate_id"`
	SignupIntentID         string    `ch:"signup_intent_id"`
	OrgID                  string    `ch:"org_id"`
	IdentityProviderOrgID  string    `ch:"identity_provider_org_id"`
	IdentityProviderUserID string    `ch:"identity_provider_user_id"`
	Step                   string    `ch:"step"`
	State                  string    `ch:"state"`
	Outcome                string    `ch:"outcome"`
	Retryable              uint8     `ch:"retryable"`
	Attempt                uint32    `ch:"attempt"`
	ErrorKind              string    `ch:"error_kind"`
	ErrorMessage           string    `ch:"error_message"`
	OccurredAt             time.Time `ch:"occurred_at"`
	Payload                string    `ch:"payload"`
	IdempotencyKeyHash     string    `ch:"idempotency_key_hash"`
	CorrelationID          string    `ch:"correlation_id"`
	RecordedAt             time.Time `ch:"recorded_at"`
	TraceID                string    `ch:"trace_id"`
}

func (s SQLStore) AppendIAMEvent(ctx context.Context, event IAMEvent) error {
	if s.CH == nil {
		return nil
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	if event.EventVersion == 0 {
		event.EventVersion = 1
	}
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("%w: marshal IAM event payload: %v", ErrStoreUnavailable, err)
	}
	if event.EventID == "" {
		event.EventID = iamEventID(event.EventType, event.AggregateID, event.OccurredAt, payload)
	}
	row := iamEventProjectionRow{
		EventID:                event.EventID,
		EventType:              event.EventType,
		EventVersion:           event.EventVersion,
		AggregateType:          event.AggregateType,
		AggregateID:            event.AggregateID,
		SignupIntentID:         event.SignupIntentID,
		OrgID:                  event.OrgID,
		IdentityProviderOrgID:  event.IdentityProviderOrgID,
		IdentityProviderUserID: event.IdentityProviderUserID,
		Step:                   event.Step,
		State:                  string(event.State),
		Outcome:                event.Outcome,
		Retryable:              boolByte(event.Retryable),
		Attempt:                event.Attempt,
		ErrorKind:              event.ErrorKind,
		ErrorMessage:           event.ErrorMessage,
		OccurredAt:             event.OccurredAt.UTC(),
		Payload:                string(payload),
		IdempotencyKeyHash:     event.IdempotencyKeyHash,
		CorrelationID:          event.CorrelationID,
		RecordedAt:             time.Now().UTC(),
		TraceID:                event.TraceID,
	}
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO verself.iam_events")
	if err != nil {
		return fmt.Errorf("%w: prepare IAM event ClickHouse batch: %v", ErrStoreUnavailable, err)
	}
	if err := batch.AppendStruct(&row); err != nil {
		return fmt.Errorf("%w: append IAM event ClickHouse row: %v", ErrStoreUnavailable, err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("%w: send IAM event ClickHouse row: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func iamEventID(eventType, aggregateID string, occurredAt time.Time, payload []byte) string {
	sum := sha256.Sum256(append([]byte(eventType+"\x00"+aggregateID+"\x00"+occurredAt.UTC().Format(time.RFC3339Nano)+"\x00"), payload...))
	return "iam_event_" + hex.EncodeToString(sum[:])
}

func boolByte(value bool) uint8 {
	if value {
		return 1
	}
	return 0
}
