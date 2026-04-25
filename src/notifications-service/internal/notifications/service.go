package notifications

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Publisher interface {
	PublishDomainEvent(ctx context.Context, event DomainEvent) error
}

type readyPublisher interface {
	Ready() error
}

type Service struct {
	PG        *pgxpool.Pool
	CH        driver.Conn
	Publisher Publisher
	Runtime   *Runtime
	Now       func() time.Time
}

type LedgerRow struct {
	RecordedAt         time.Time `ch:"recorded_at"`
	OccurredAt         time.Time `ch:"occurred_at"`
	SchemaVersion      string    `ch:"schema_version"`
	LedgerEventID      uuid.UUID `ch:"ledger_event_id"`
	EventType          string    `ch:"event_type"`
	OrgID              string    `ch:"org_id"`
	RecipientSubjectID string    `ch:"recipient_subject_id"`
	NotificationID     uuid.UUID `ch:"notification_id"`
	RecipientSequence  uint64    `ch:"recipient_sequence"`
	EventSource        string    `ch:"event_source"`
	SourceSubject      string    `ch:"source_subject"`
	SourceEventID      uuid.UUID `ch:"source_event_id"`
	Kind               string    `ch:"kind"`
	Priority           string    `ch:"priority"`
	Status             string    `ch:"status"`
	Reason             string    `ch:"reason"`
	ContentSHA256      string    `ch:"content_sha256"`
	TraceID            string    `ch:"trace_id"`
	SpanID             string    `ch:"span_id"`
	Traceparent        string    `ch:"traceparent"`
}

func (s *Service) SetRuntime(runtime *Runtime) {
	s.Runtime = runtime
}

func (s *Service) Ready(ctx context.Context) error {
	if s.PG == nil {
		return ErrStoreUnavailable
	}
	var one int
	if err := s.PG.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if s.CH == nil {
		return fmt.Errorf("%w: clickhouse unavailable", ErrStoreUnavailable)
	}
	var chOne uint8
	if err := s.CH.QueryRow(ctx, "SELECT 1").Scan(&chOne); err != nil {
		return fmt.Errorf("%w: clickhouse readiness: %v", ErrStoreUnavailable, err)
	}
	if publisher, ok := s.Publisher.(readyPublisher); ok {
		if err := publisher.Ready(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Summary(ctx context.Context, principal Principal) (Summary, error) {
	ctx, span := tracer.Start(ctx, "notifications.summary")
	defer span.End()
	if err := ValidatePrincipal(principal); err != nil {
		return Summary{}, err
	}
	span.SetAttributes(attribute.String("verself.org_id", principal.OrgID), attribute.String("verself.subject_id", principal.Subject))
	if err := s.ensureInboxState(ctx, s.PG, principal.OrgID, principal.Subject); err != nil {
		return Summary{}, err
	}
	preferences, err := s.preferences(ctx, s.PG, principal.OrgID, principal.Subject)
	if err != nil {
		return Summary{}, err
	}
	var (
		nextSequence   int64
		readUpTo       int64
		unreadCount    int64
		latestSequence int64
	)
	err = s.PG.QueryRow(ctx, `
SELECT state.next_sequence,
       state.read_up_to_sequence,
       GREATEST(state.next_sequence - 1, 0),
       COUNT(n.notification_id)
FROM notification_inbox_state state
LEFT JOIN user_notifications n
  ON n.org_id = state.org_id
 AND n.recipient_subject_id = state.recipient_subject_id
 AND n.recipient_sequence > state.read_up_to_sequence
 AND n.read_at IS NULL
 AND n.dismissed_at IS NULL
WHERE state.org_id = $1 AND state.recipient_subject_id = $2
GROUP BY state.next_sequence, state.read_up_to_sequence`,
		principal.OrgID, principal.Subject).Scan(&nextSequence, &readUpTo, &latestSequence, &unreadCount)
	if err != nil {
		return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	latest, err := s.latestNotification(ctx, principal.OrgID, principal.Subject)
	if err != nil {
		return Summary{}, err
	}
	if unreadCount > RingBufferSize {
		unreadCount = RingBufferSize
	}
	return Summary{
		OrgID:              principal.OrgID,
		SubjectID:          principal.Subject,
		UnreadCount:        uint16(unreadCount),
		LatestSequence:     latestSequence,
		ReadUpToSequence:   readUpTo,
		Preferences:        preferences,
		LatestNotification: latest,
	}, nil
}

func (s *Service) List(ctx context.Context, principal Principal, input ListRequest) (ListResult, error) {
	ctx, span := tracer.Start(ctx, "notifications.list")
	defer span.End()
	if err := ValidatePrincipal(principal); err != nil {
		return ListResult{}, err
	}
	input, err := NormalizeListRequest(input)
	if err != nil {
		return ListResult{}, err
	}
	summary, err := s.Summary(ctx, principal)
	if err != nil {
		return ListResult{}, err
	}
	rows, err := s.PG.Query(ctx, `
SELECT notification_id, org_id, recipient_subject_id, recipient_sequence, kind, priority, title, body,
       action_url, resource_kind, resource_id, created_at, expires_at, read_at, dismissed_at
FROM user_notifications
WHERE org_id = $1 AND recipient_subject_id = $2 AND dismissed_at IS NULL
ORDER BY recipient_sequence DESC
LIMIT $3`, principal.OrgID, principal.Subject, input.Limit)
	if err != nil {
		return ListResult{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rows.Close()
	notifications := make([]Notification, 0, input.Limit)
	for rows.Next() {
		notification, err := scanNotification(rows)
		if err != nil {
			return ListResult{}, err
		}
		notifications = append(notifications, notification)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	span.SetAttributes(attribute.Int("notification.list_count", len(notifications)))
	return ListResult{Summary: summary, Notifications: notifications}, nil
}

func (s *Service) PutPreferences(ctx context.Context, principal Principal, input PutPreferencesRequest) (Summary, error) {
	ctx, span := tracer.Start(ctx, "notifications.preferences.put")
	defer span.End()
	if err := ValidatePrincipal(principal); err != nil {
		return Summary{}, err
	}
	input, err := NormalizePutPreferences(input)
	if err != nil {
		return Summary{}, err
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	if err := s.ensureInboxState(ctx, tx, principal.OrgID, principal.Subject); err != nil {
		return Summary{}, err
	}
	old, err := s.preferences(ctx, tx, principal.OrgID, principal.Subject)
	if err != nil {
		return Summary{}, err
	}
	if old.Version != input.Version {
		return Summary{}, ErrConflict
	}
	now := s.now()
	nextVersion := old.Version + 1
	tag, err := tx.Exec(ctx, `
INSERT INTO notification_preferences (org_id, subject_id, version, enabled, updated_at, updated_by)
VALUES ($1, $2, $3, $4, $5, $2)
ON CONFLICT (org_id, subject_id) DO UPDATE
SET version = notification_preferences.version + 1,
    enabled = EXCLUDED.enabled,
    updated_at = EXCLUDED.updated_at,
    updated_by = EXCLUDED.updated_by
WHERE notification_preferences.version = $6`,
		principal.OrgID, principal.Subject, nextVersion, input.Enabled, now, input.Version)
	if err != nil {
		return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if tag.RowsAffected() != 1 {
		return Summary{}, ErrConflict
	}
	if err := s.enqueueProjectionTx(ctx, tx, projectionInput{
		EventType:          LedgerPreferencesUpdated,
		OrgID:              principal.OrgID,
		RecipientSubjectID: principal.Subject,
		Status:             boolStatus(input.Enabled),
		OccurredAt:         now,
		Traceparent:        traceparentFromContext(ctx),
	}); err != nil {
		return Summary{}, err
	}
	if s.Runtime != nil {
		if err := s.Runtime.EnqueueProjectionPendingTx(ctx, tx, traceparentFromContext(ctx)); err != nil {
			return Summary{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	span.SetAttributes(attribute.String("notification.preferences_enabled", boolStatus(input.Enabled)))
	return s.Summary(ctx, principal)
}

func (s *Service) MarkRead(ctx context.Context, principal Principal, input MarkReadRequest) (Summary, error) {
	ctx, span := tracer.Start(ctx, "notifications.read_cursor.advance")
	defer span.End()
	if err := ValidatePrincipal(principal); err != nil {
		return Summary{}, err
	}
	input, err := NormalizeMarkRead(input)
	if err != nil {
		return Summary{}, err
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	if err := s.ensureInboxState(ctx, tx, principal.OrgID, principal.Subject); err != nil {
		return Summary{}, err
	}
	now := s.now()
	traceparent := traceparentFromContext(ctx)
	advancedTo, advanced, err := s.advanceReadCursorTx(
		ctx,
		tx,
		principal.OrgID,
		principal.Subject,
		input.ReadUpToSequence,
		now,
		traceparent,
	)
	if err != nil {
		return Summary{}, err
	}
	if !advanced {
		if err := tx.Commit(ctx); err != nil {
			return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
		}
		span.SetAttributes(attribute.Int64("notification.read_up_to_sequence", input.ReadUpToSequence))
		return s.Summary(ctx, principal)
	}
	if s.Runtime != nil {
		if err := s.Runtime.EnqueueProjectionPendingTx(ctx, tx, traceparent); err != nil {
			return Summary{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	span.SetAttributes(attribute.Int64("notification.read_up_to_sequence", advancedTo))
	return s.Summary(ctx, principal)
}

func (s *Service) MarkNotificationRead(ctx context.Context, principal Principal, input ReadNotificationRequest) (Summary, error) {
	ctx, span := tracer.Start(ctx, "notifications.inbox.read")
	defer span.End()
	if err := ValidatePrincipal(principal); err != nil {
		return Summary{}, err
	}
	if input.NotificationID == uuid.Nil {
		return Summary{}, fmt.Errorf("%w: notification_id is required", ErrInvalidInput)
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	if err := s.ensureInboxState(ctx, tx, principal.OrgID, principal.Subject); err != nil {
		return Summary{}, err
	}
	now := s.now()
	traceparent := traceparentFromContext(ctx)
	var projected projectionInput
	err = tx.QueryRow(ctx, `
UPDATE user_notifications
SET read_at = $4
WHERE notification_id = $1
  AND org_id = $2
  AND recipient_subject_id = $3
  AND dismissed_at IS NULL
  AND read_at IS NULL
  AND recipient_sequence > (
      SELECT read_up_to_sequence
      FROM notification_inbox_state
      WHERE org_id = $2 AND recipient_subject_id = $3
  )
RETURNING org_id, recipient_subject_id, notification_id::text, recipient_sequence, event_source, event_id::text, kind, priority, content_sha256`,
		input.NotificationID, principal.OrgID, principal.Subject, now).Scan(
		&projected.OrgID,
		&projected.RecipientSubjectID,
		&projected.NotificationIDText,
		&projected.RecipientSequence,
		&projected.EventSource,
		&projected.EventIDText,
		&projected.Kind,
		&projected.Priority,
		&projected.ContentSHA256,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if exists, err := s.notificationExistsTx(ctx, tx, principal.OrgID, principal.Subject, input.NotificationID); err != nil {
			return Summary{}, err
		} else if !exists {
			return Summary{}, ErrNotFound
		}
		if err := tx.Commit(ctx); err != nil {
			return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
		}
		span.SetAttributes(attribute.String("notification.id", input.NotificationID.String()))
		return s.Summary(ctx, principal)
	}
	if err != nil {
		return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	projected.EventType = LedgerInboxRead
	projected.Status = "read"
	projected.OccurredAt = now
	projected.Traceparent = traceparent
	if err := s.enqueueProjectionTx(ctx, tx, projected); err != nil {
		return Summary{}, err
	}
	if err := s.touchInboxStateTx(ctx, tx, principal.OrgID, principal.Subject, now); err != nil {
		return Summary{}, err
	}
	if s.Runtime != nil {
		if err := s.Runtime.EnqueueProjectionPendingTx(ctx, tx, traceparent); err != nil {
			return Summary{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	span.SetAttributes(attribute.String("notification.id", input.NotificationID.String()))
	return s.Summary(ctx, principal)
}

func (s *Service) Dismiss(ctx context.Context, principal Principal, input DismissRequest) (Summary, error) {
	ctx, span := tracer.Start(ctx, "notifications.inbox.dismiss")
	defer span.End()
	if err := ValidatePrincipal(principal); err != nil {
		return Summary{}, err
	}
	if input.NotificationID == uuid.Nil {
		return Summary{}, fmt.Errorf("%w: notification_id is required", ErrInvalidInput)
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	if err := s.ensureInboxState(ctx, tx, principal.OrgID, principal.Subject); err != nil {
		return Summary{}, err
	}
	now := s.now()
	traceparent := traceparentFromContext(ctx)
	var projected projectionInput
	err = tx.QueryRow(ctx, `
UPDATE user_notifications
SET dismissed_at = COALESCE(dismissed_at, $4)
WHERE notification_id = $1 AND org_id = $2 AND recipient_subject_id = $3
RETURNING org_id, recipient_subject_id, notification_id::text, recipient_sequence, event_source, event_id::text, kind, priority, content_sha256`,
		input.NotificationID, principal.OrgID, principal.Subject, now).Scan(
		&projected.OrgID,
		&projected.RecipientSubjectID,
		&projected.NotificationIDText,
		&projected.RecipientSequence,
		&projected.EventSource,
		&projected.EventIDText,
		&projected.Kind,
		&projected.Priority,
		&projected.ContentSHA256,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Summary{}, ErrNotFound
	}
	if err != nil {
		return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	projected.EventType = LedgerInboxDismissed
	projected.Status = "dismissed"
	projected.OccurredAt = now
	projected.Traceparent = traceparent
	if err := s.enqueueProjectionTx(ctx, tx, projected); err != nil {
		return Summary{}, err
	}
	if err := s.touchInboxStateTx(ctx, tx, principal.OrgID, principal.Subject, now); err != nil {
		return Summary{}, err
	}
	if s.Runtime != nil {
		if err := s.Runtime.EnqueueProjectionPendingTx(ctx, tx, traceparent); err != nil {
			return Summary{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	span.SetAttributes(attribute.String("notification.id", input.NotificationID.String()))
	return s.Summary(ctx, principal)
}

func (s *Service) DismissAll(ctx context.Context, principal Principal) (Summary, error) {
	ctx, span := tracer.Start(ctx, "notifications.inbox.clear")
	defer span.End()
	if err := ValidatePrincipal(principal); err != nil {
		return Summary{}, err
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	if err := s.ensureInboxState(ctx, tx, principal.OrgID, principal.Subject); err != nil {
		return Summary{}, err
	}
	now := s.now()
	traceparent := traceparentFromContext(ctx)
	rows, err := tx.Query(ctx, `
UPDATE user_notifications
SET dismissed_at = COALESCE(dismissed_at, $3)
WHERE org_id = $1 AND recipient_subject_id = $2 AND dismissed_at IS NULL
RETURNING org_id, recipient_subject_id, notification_id::text, recipient_sequence, event_source, event_id::text, kind, priority, content_sha256`,
		principal.OrgID, principal.Subject, now)
	if err != nil {
		return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rows.Close()
	projections := make([]projectionInput, 0, 16)
	for rows.Next() {
		projected, err := scanProjection(rows)
		if err != nil {
			return Summary{}, err
		}
		projected.EventType = LedgerInboxDismissed
		projected.Status = "dismissed"
		projected.OccurredAt = now
		projected.Traceparent = traceparent
		projections = append(projections, projected)
	}
	if err := rows.Err(); err != nil {
		return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	rows.Close()
	for _, projected := range projections {
		if err := s.enqueueProjectionTx(ctx, tx, projected); err != nil {
			return Summary{}, err
		}
	}
	dismissed := len(projections)
	if dismissed > 0 {
		if err := s.touchInboxStateTx(ctx, tx, principal.OrgID, principal.Subject, now); err != nil {
			return Summary{}, err
		}
	}
	if s.Runtime != nil && dismissed > 0 {
		if err := s.Runtime.EnqueueProjectionPendingTx(ctx, tx, traceparent); err != nil {
			return Summary{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	span.SetAttributes(
		attribute.Int("notification.dismissed_count", dismissed),
	)
	return s.Summary(ctx, principal)
}

func (s *Service) PublishSyntheticTest(ctx context.Context, principal Principal, input TestRequest) (Accepted, error) {
	ctx, span := tracer.Start(ctx, "notifications.synthetic.publish")
	defer span.End()
	if s.Publisher == nil {
		return Accepted{}, fmt.Errorf("%w: nats publisher is unavailable", ErrStoreUnavailable)
	}
	event, err := NewSyntheticEvent(principal, input, s.now(), traceparentFromContext(ctx))
	if err != nil {
		return Accepted{}, err
	}
	span.SetAttributes(
		attribute.String("verself.org_id", principal.OrgID),
		attribute.String("verself.subject_id", principal.Subject),
		attribute.String("notification.event_id", event.EventID.String()),
	)
	if err := s.Publisher.PublishDomainEvent(ctx, event); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Accepted{}, err
	}
	return Accepted{EventID: event.EventID, Traceparent: event.Traceparent}, nil
}

func (s *Service) AcceptEvent(ctx context.Context, event DomainEvent) (bool, error) {
	ctx, span := tracer.Start(ctx, "notifications.event.persist")
	defer span.End()
	event, err := NormalizeDomainEvent(event)
	if err != nil {
		return false, err
	}
	span.SetAttributes(
		attribute.String("notification.event_source", event.EventSource),
		attribute.String("notification.event_id", event.EventID.String()),
		attribute.String("notification.subject", event.Subject),
		attribute.String("verself.org_id", event.OrgID),
		attribute.String("verself.subject_id", event.RecipientSubjectID),
	)
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return false, fmt.Errorf("%w: marshal event payload: %v", ErrStoreUnavailable, err)
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	now := s.now()
	contentHash := ContentSHA256(event)
	tag, err := tx.Exec(ctx, `
INSERT INTO notification_events (
    event_source, event_id, subject, org_id, actor_subject_id, recipient_subject_id,
    dedupe_key, kind, priority, title, body, action_url, resource_kind, resource_id,
    content_sha256, payload, traceparent, occurred_at, received_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
ON CONFLICT DO NOTHING`,
		event.EventSource,
		event.EventID,
		event.Subject,
		event.OrgID,
		event.ActorSubjectID,
		event.RecipientSubjectID,
		event.DedupeKey,
		event.Kind,
		event.Priority,
		event.Title,
		event.Body,
		event.ActionURL,
		event.ResourceKind,
		event.ResourceID,
		contentHash,
		payload,
		event.Traceparent,
		event.OccurredAt,
		now,
	)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if tag.RowsAffected() == 0 {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
		}
		return false, nil
	}
	if err := s.enqueueProjectionTx(ctx, tx, projectionInput{
		EventType:          LedgerEventReceived,
		OrgID:              event.OrgID,
		RecipientSubjectID: event.RecipientSubjectID,
		EventSource:        event.EventSource,
		EventIDText:        event.EventID.String(),
		SourceSubject:      event.Subject,
		Kind:               event.Kind,
		Priority:           event.Priority,
		ContentSHA256:      contentHash,
		Status:             "received",
		OccurredAt:         event.OccurredAt,
		Traceparent:        event.Traceparent,
	}); err != nil {
		return false, err
	}
	if s.Runtime != nil {
		if err := s.Runtime.EnqueueEventFanoutTx(ctx, tx, event.EventSource, event.EventID.String(), event.Traceparent); err != nil {
			return false, err
		}
		if err := s.Runtime.EnqueueProjectionPendingTx(ctx, tx, event.Traceparent); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return true, nil
}

func (s *Service) ProcessEvent(ctx context.Context, eventSource string, eventID string) error {
	ctx, span := tracer.Start(ctx, "notifications.event.fanout")
	defer span.End()
	eventUUID, err := uuid.Parse(strings.TrimSpace(eventID))
	if err != nil {
		return fmt.Errorf("%w: event_id is invalid", ErrInvalidInput)
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	event, err := s.loadEventForUpdate(ctx, tx, strings.TrimSpace(eventSource), eventUUID)
	if errors.Is(err, ErrDuplicate) {
		span.SetAttributes(attribute.Bool("notification.duplicate", true))
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	span.SetAttributes(
		attribute.String("notification.event_source", event.EventSource),
		attribute.String("notification.event_id", event.EventID.String()),
		attribute.String("verself.org_id", event.OrgID),
		attribute.String("verself.subject_id", event.RecipientSubjectID),
	)
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}
	preferences, err := s.preferences(ctx, tx, event.OrgID, event.RecipientSubjectID)
	if err != nil {
		return err
	}
	now := s.now()
	if !preferences.Enabled {
		if _, err := tx.Exec(ctx, `
UPDATE notification_events
SET suppressed_at = COALESCE(suppressed_at, $3),
    suppression_reason = 'preferences_disabled'
WHERE event_source = $1 AND event_id = $2 AND processed_at IS NULL`,
			event.EventSource, event.EventID, now); err != nil {
			return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
		}
		if err := s.enqueueProjectionTx(ctx, tx, projectionInput{
			EventType:          LedgerDeliverySuppressed,
			OrgID:              event.OrgID,
			RecipientSubjectID: event.RecipientSubjectID,
			EventSource:        event.EventSource,
			EventIDText:        event.EventID.String(),
			SourceSubject:      event.Subject,
			Kind:               event.Kind,
			Priority:           event.Priority,
			ContentSHA256:      ContentSHA256(event),
			Status:             "suppressed",
			Reason:             "preferences_disabled",
			OccurredAt:         now,
			Traceparent:        event.Traceparent,
		}); err != nil {
			return err
		}
		if s.Runtime != nil {
			if err := s.Runtime.EnqueueProjectionPendingTx(ctx, tx, event.Traceparent); err != nil {
				return err
			}
		}
		return tx.Commit(ctx)
	}
	if err := s.ensureInboxState(ctx, tx, event.OrgID, event.RecipientSubjectID); err != nil {
		return err
	}
	var sequence int64
	err = tx.QueryRow(ctx, `
UPDATE notification_inbox_state
SET next_sequence = next_sequence + 1,
    updated_at = $3
WHERE org_id = $1 AND recipient_subject_id = $2
RETURNING next_sequence - 1`,
		event.OrgID, event.RecipientSubjectID, now).Scan(&sequence)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	notificationID := uuid.New()
	contentHash := ContentSHA256(event)
	_, err = tx.Exec(ctx, `
INSERT INTO user_notifications (
    notification_id, org_id, recipient_subject_id, recipient_sequence, dedupe_key,
    event_source, event_id, kind, priority, title, body, action_url, resource_kind,
    resource_id, content_sha256, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		notificationID,
		event.OrgID,
		event.RecipientSubjectID,
		sequence,
		event.DedupeKey,
		event.EventSource,
		event.EventID,
		event.Kind,
		event.Priority,
		event.Title,
		event.Body,
		event.ActionURL,
		event.ResourceKind,
		event.ResourceID,
		contentHash,
		now,
	)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if err := s.enqueueProjectionTx(ctx, tx, projectionInput{
		EventType:          LedgerInboxCreated,
		OrgID:              event.OrgID,
		RecipientSubjectID: event.RecipientSubjectID,
		NotificationIDText: notificationID.String(),
		RecipientSequence:  sequence,
		EventSource:        event.EventSource,
		EventIDText:        event.EventID.String(),
		SourceSubject:      event.Subject,
		Kind:               event.Kind,
		Priority:           event.Priority,
		ContentSHA256:      contentHash,
		Status:             "created",
		OccurredAt:         now,
		Traceparent:        event.Traceparent,
	}); err != nil {
		return err
	}
	pruned, err := s.pruneInbox(ctx, tx, event.OrgID, event.RecipientSubjectID, sequence, now, event.Traceparent)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE notification_events
SET processed_at = COALESCE(processed_at, $3)
WHERE event_source = $1 AND event_id = $2`,
		event.EventSource, event.EventID, now); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if s.Runtime != nil {
		if err := s.Runtime.EnqueueProjectionPendingTx(ctx, tx, event.Traceparent); err != nil {
			return err
		}
	}
	span.SetAttributes(
		attribute.String("notification.id", notificationID.String()),
		attribute.Int64("notification.recipient_sequence", sequence),
		attribute.Int("notification.pruned_count", pruned),
	)
	return tx.Commit(ctx)
}

func (s *Service) ProjectPendingLedger(ctx context.Context, limit int) error {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if s.CH == nil {
		return fmt.Errorf("%w: clickhouse unavailable", ErrStoreUnavailable)
	}
	ctx, span := tracer.Start(ctx, "notifications.clickhouse.project_pending")
	defer span.End()
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	rows, err := tx.Query(ctx, `
SELECT ledger_event_id::text, event_type, org_id, recipient_subject_id,
       COALESCE(notification_id::text, ''), event_source, COALESCE(event_id::text, ''),
       recipient_sequence, source_subject, kind, priority, content_sha256, status,
       reason, traceparent, occurred_at
FROM notification_projection_queue
WHERE projected_at IS NULL AND next_attempt_at <= now()
ORDER BY occurred_at, ledger_event_id
LIMIT $1
FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rows.Close()
	claimed := make([]LedgerRow, 0, limit)
	for rows.Next() {
		var (
			ledgerID          string
			notificationID    string
			eventID           string
			recipientSequence int64
			row               LedgerRow
		)
		if err := rows.Scan(
			&ledgerID,
			&row.EventType,
			&row.OrgID,
			&row.RecipientSubjectID,
			&notificationID,
			&row.EventSource,
			&eventID,
			&recipientSequence,
			&row.SourceSubject,
			&row.Kind,
			&row.Priority,
			&row.ContentSHA256,
			&row.Status,
			&row.Reason,
			&row.Traceparent,
			&row.OccurredAt,
		); err != nil {
			return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
		}
		if recipientSequence < 0 {
			return fmt.Errorf("%w: recipient_sequence is negative", ErrStoreUnavailable)
		}
		row.LedgerEventID = mustUUID(ledgerID)
		row.NotificationID = mustUUID(notificationID)
		row.SourceEventID = mustUUID(eventID)
		row.RecipientSequence = uint64(recipientSequence)
		row.RecordedAt = s.now()
		row.SchemaVersion = SchemaVersion
		if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
			row.TraceID = sc.TraceID().String()
			row.SpanID = sc.SpanID().String()
		}
		claimed = append(claimed, row)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if len(claimed) == 0 {
		return tx.Commit(ctx)
	}
	for _, row := range claimed {
		if projected, err := s.ledgerEventProjected(ctx, row.LedgerEventID); err != nil {
			return err
		} else if !projected {
			if err := s.insertLedgerClickHouse(ctx, row); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `
UPDATE notification_projection_queue
SET projected_at = COALESCE(projected_at, now()),
    attempts = attempts + 1,
    last_error = ''
WHERE ledger_event_id = $1`, row.LedgerEventID); err != nil {
			return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
		}
	}
	span.SetAttributes(attribute.Int("notification.projected_count", len(claimed)))
	return tx.Commit(ctx)
}

func (s *Service) Reconcile(ctx context.Context, limit int) error {
	ctx, span := tracer.Start(ctx, "notifications.reconcile")
	defer span.End()
	span.SetAttributes(attribute.Int("notification.limit", limit))
	return s.ProjectPendingLedger(ctx, limit)
}

func (s *Service) loadEventForUpdate(ctx context.Context, q queryer, eventSource string, eventID uuid.UUID) (DomainEvent, error) {
	var (
		event      DomainEvent
		payload    []byte
		processed  pgtype.Timestamptz
		suppressed pgtype.Timestamptz
	)
	err := q.QueryRow(ctx, `
SELECT event_source, event_id, subject, org_id, actor_subject_id, recipient_subject_id,
       dedupe_key, kind, priority, title, body, action_url, resource_kind, resource_id,
       payload, traceparent, occurred_at, processed_at, suppressed_at
FROM notification_events
WHERE event_source = $1 AND event_id = $2
FOR UPDATE`,
		eventSource, eventID).Scan(
		&event.EventSource,
		&event.EventID,
		&event.Subject,
		&event.OrgID,
		&event.ActorSubjectID,
		&event.RecipientSubjectID,
		&event.DedupeKey,
		&event.Kind,
		&event.Priority,
		&event.Title,
		&event.Body,
		&event.ActionURL,
		&event.ResourceKind,
		&event.ResourceID,
		&payload,
		&event.Traceparent,
		&event.OccurredAt,
		&processed,
		&suppressed,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DomainEvent{}, ErrNotFound
	}
	if err != nil {
		return DomainEvent{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if processed.Valid || suppressed.Valid {
		return DomainEvent{}, ErrDuplicate
	}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &event.Payload)
	}
	return event, nil
}

func (s *Service) ensureInboxState(ctx context.Context, q queryer, orgID string, subjectID string) error {
	now := s.now()
	_, err := q.Exec(ctx, `
INSERT INTO notification_inbox_state (org_id, recipient_subject_id, next_sequence, read_up_to_sequence, created_at, updated_at)
VALUES ($1, $2, 1, 0, $3, $3)
ON CONFLICT (org_id, recipient_subject_id) DO NOTHING`, orgID, subjectID, now)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s *Service) advanceReadCursorTx(ctx context.Context, tx pgx.Tx, orgID string, subjectID string, readUpTo int64, now time.Time, traceparent string) (int64, bool, error) {
	var advancedTo int64
	// Clamp to the latest assigned sequence so a client cannot pre-read future notifications.
	err := tx.QueryRow(ctx, `
UPDATE notification_inbox_state
SET read_up_to_sequence = LEAST($3, next_sequence - 1),
    updated_at = $4
WHERE org_id = $1
  AND recipient_subject_id = $2
  AND read_up_to_sequence < LEAST($3, next_sequence - 1)
RETURNING read_up_to_sequence`,
		orgID, subjectID, readUpTo, now).Scan(&advancedTo)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	projected := projectionInput{
		EventType:          LedgerReadCursorAdvanced,
		OrgID:              orgID,
		RecipientSubjectID: subjectID,
		RecipientSequence:  advancedTo,
		Status:             "read",
		OccurredAt:         now,
		Traceparent:        traceparent,
	}
	err = tx.QueryRow(ctx, `
SELECT notification_id::text, event_source, event_id::text, kind, priority, content_sha256
FROM user_notifications
WHERE org_id = $1 AND recipient_subject_id = $2 AND recipient_sequence <= $3
ORDER BY recipient_sequence DESC
LIMIT 1`,
		orgID, subjectID, advancedTo).Scan(
		&projected.NotificationIDText,
		&projected.EventSource,
		&projected.EventIDText,
		&projected.Kind,
		&projected.Priority,
		&projected.ContentSHA256,
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if err := s.enqueueProjectionTx(ctx, tx, projected); err != nil {
		return 0, false, err
	}
	return advancedTo, true, nil
}

func (s *Service) touchInboxStateTx(ctx context.Context, tx pgx.Tx, orgID string, subjectID string, now time.Time) error {
	tag, err := tx.Exec(ctx, `
UPDATE notification_inbox_state
SET updated_at = $3
WHERE org_id = $1 AND recipient_subject_id = $2`, orgID, subjectID, now)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) notificationExistsTx(ctx context.Context, tx pgx.Tx, orgID string, subjectID string, notificationID uuid.UUID) (bool, error) {
	var one int
	err := tx.QueryRow(ctx, `
SELECT 1
FROM user_notifications
WHERE org_id = $1 AND recipient_subject_id = $2 AND notification_id = $3`,
		orgID, subjectID, notificationID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return true, nil
}

func (s *Service) preferences(ctx context.Context, q queryer, orgID string, subjectID string) (Preferences, error) {
	var prefs Preferences
	err := q.QueryRow(ctx, `
SELECT version, enabled, updated_at, updated_by
FROM notification_preferences
WHERE org_id = $1 AND subject_id = $2`, orgID, subjectID).Scan(
		&prefs.Version,
		&prefs.Enabled,
		&prefs.UpdatedAt,
		&prefs.UpdatedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		now := s.now()
		return Preferences{Version: 0, Enabled: true, UpdatedAt: now, UpdatedBy: ""}, nil
	}
	if err != nil {
		return Preferences{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	prefs.UpdatedAt = prefs.UpdatedAt.UTC()
	return prefs, nil
}

func (s *Service) latestNotification(ctx context.Context, orgID string, subjectID string) (*Notification, error) {
	rows, err := s.PG.Query(ctx, `
SELECT notification_id, org_id, recipient_subject_id, recipient_sequence, kind, priority, title, body,
       action_url, resource_kind, resource_id, created_at, expires_at, read_at, dismissed_at
FROM user_notifications
WHERE org_id = $1 AND recipient_subject_id = $2
ORDER BY recipient_sequence DESC
LIMIT 1`, orgID, subjectID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
		}
		return nil, nil
	}
	notification, err := scanNotification(rows)
	if err != nil {
		return nil, err
	}
	return &notification, nil
}

func (s *Service) pruneInbox(ctx context.Context, tx pgx.Tx, orgID string, subjectID string, sequence int64, now time.Time, traceparent string) (int, error) {
	cutoff := sequence - RingBufferSize
	if cutoff <= 0 {
		return 0, nil
	}
	rows, err := tx.Query(ctx, `
DELETE FROM user_notifications
WHERE org_id = $1 AND recipient_subject_id = $2 AND recipient_sequence <= $3
RETURNING notification_id::text, recipient_sequence, event_source, event_id::text, kind, priority, content_sha256`,
		orgID, subjectID, cutoff)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		input := projectionInput{
			EventType:          LedgerInboxPruned,
			OrgID:              orgID,
			RecipientSubjectID: subjectID,
			Status:             "pruned",
			Reason:             "ring_buffer_limit",
			OccurredAt:         now,
			Traceparent:        traceparent,
		}
		if err := rows.Scan(
			&input.NotificationIDText,
			&input.RecipientSequence,
			&input.EventSource,
			&input.EventIDText,
			&input.Kind,
			&input.Priority,
			&input.ContentSHA256,
		); err != nil {
			return count, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
		}
		if err := s.enqueueProjectionTx(ctx, tx, input); err != nil {
			return count, err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return count, nil
}

type projectionInput struct {
	EventType          string
	OrgID              string
	RecipientSubjectID string
	NotificationIDText string
	RecipientSequence  int64
	EventSource        string
	EventIDText        string
	SourceSubject      string
	Kind               string
	Priority           string
	ContentSHA256      string
	Status             string
	Reason             string
	OccurredAt         time.Time
	Traceparent        string
}

func (s *Service) enqueueProjectionTx(ctx context.Context, tx pgx.Tx, input projectionInput) error {
	if input.OccurredAt.IsZero() {
		input.OccurredAt = s.now()
	}
	ledgerID := deterministicLedgerID(input)
	var notificationID any
	if id := mustUUID(input.NotificationIDText); id != uuid.Nil {
		notificationID = id
	}
	var eventID any
	if id := mustUUID(input.EventIDText); id != uuid.Nil {
		eventID = id
	}
	_, err := tx.Exec(ctx, `
INSERT INTO notification_projection_queue (
    ledger_event_id, event_type, org_id, recipient_subject_id, notification_id,
    event_source, event_id, recipient_sequence, source_subject, kind, priority,
    content_sha256, status, reason, traceparent, occurred_at, next_attempt_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $16)
ON CONFLICT (ledger_event_id) DO NOTHING`,
		ledgerID,
		input.EventType,
		input.OrgID,
		input.RecipientSubjectID,
		notificationID,
		input.EventSource,
		eventID,
		input.RecipientSequence,
		input.SourceSubject,
		input.Kind,
		input.Priority,
		input.ContentSHA256,
		input.Status,
		input.Reason,
		input.Traceparent,
		input.OccurredAt,
	)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func deterministicLedgerID(input projectionInput) uuid.UUID {
	seed := strings.Join([]string{
		input.EventType,
		input.OrgID,
		input.RecipientSubjectID,
		input.NotificationIDText,
		strconv.FormatInt(input.RecipientSequence, 10),
		input.EventSource,
		input.EventIDText,
		input.Status,
		input.Reason,
	}, "|")
	sum := sha256.Sum256([]byte(seed))
	return uuid.NewHash(sha256.New(), uuid.Nil, []byte(hex.EncodeToString(sum[:])), 5)
}

func (s *Service) ledgerEventProjected(ctx context.Context, eventID uuid.UUID) (bool, error) {
	var found uint8
	err := s.CH.QueryRow(ctx, `
SELECT 1
FROM verself.notification_events
WHERE ledger_event_id = $1
LIMIT 1`, eventID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%w: check notification ledger projection: %v", ErrStoreUnavailable, err)
	}
	return true, nil
}

func (s *Service) insertLedgerClickHouse(ctx context.Context, row LedgerRow) error {
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO verself.notification_events")
	if err != nil {
		return fmt.Errorf("%w: prepare notification ledger insert: %v", ErrStoreUnavailable, err)
	}
	if err := batch.AppendStruct(&row); err != nil {
		return fmt.Errorf("%w: append notification ledger event: %v", ErrStoreUnavailable, err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("%w: send notification ledger event: %v", ErrStoreUnavailable, err)
	}
	return nil
}

type queryer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func scanProjection(rows pgx.Rows) (projectionInput, error) {
	var input projectionInput
	if err := rows.Scan(
		&input.OrgID,
		&input.RecipientSubjectID,
		&input.NotificationIDText,
		&input.RecipientSequence,
		&input.EventSource,
		&input.EventIDText,
		&input.Kind,
		&input.Priority,
		&input.ContentSHA256,
	); err != nil {
		return projectionInput{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return input, nil
}

func scanNotification(rows pgx.Rows) (Notification, error) {
	var (
		notification Notification
		expiresAt    pgtype.Timestamptz
		readAt       pgtype.Timestamptz
		dismissedAt  pgtype.Timestamptz
	)
	if err := rows.Scan(
		&notification.NotificationID,
		&notification.OrgID,
		&notification.RecipientSubjectID,
		&notification.RecipientSequence,
		&notification.Kind,
		&notification.Priority,
		&notification.Title,
		&notification.Body,
		&notification.ActionURL,
		&notification.ResourceKind,
		&notification.ResourceID,
		&notification.CreatedAt,
		&expiresAt,
		&readAt,
		&dismissedAt,
	); err != nil {
		return Notification{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	notification.CreatedAt = notification.CreatedAt.UTC()
	if expiresAt.Valid {
		t := expiresAt.Time.UTC()
		notification.ExpiresAt = &t
	}
	if readAt.Valid {
		t := readAt.Time.UTC()
		notification.ReadAt = &t
	}
	if dismissedAt.Valid {
		t := dismissedAt.Time.UTC()
		notification.DismissedAt = &t
	}
	return notification, nil
}

func mustUUID(value string) uuid.UUID {
	id, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil
	}
	return id
}

func traceparentFromContext(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.HasTraceID() {
		return ""
	}
	flags := "00"
	if sc.TraceFlags().IsSampled() {
		flags = "01"
	}
	return fmt.Sprintf("00-%s-%s-%s", sc.TraceID().String(), sc.SpanID().String(), flags)
}

func boolStatus(value bool) string {
	if value {
		return "enabled"
	}
	return "disabled"
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}
