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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	notificationstore "github.com/verself/notifications-service/internal/store"
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

func (s *Service) q() *notificationstore.Queries {
	return notificationstore.New(s.PG)
}

func (s *Service) Ready(ctx context.Context) error {
	if s.PG == nil {
		return ErrStoreUnavailable
	}
	if _, err := s.q().Ping(ctx); err != nil {
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
	q := s.q()
	if err := s.ensureInboxState(ctx, q, principal.OrgID, principal.Subject); err != nil {
		return Summary{}, err
	}
	preferences, err := s.preferences(ctx, q, principal.OrgID, principal.Subject)
	if err != nil {
		return Summary{}, err
	}
	state, err := q.GetSummaryState(ctx, notificationstore.GetSummaryStateParams{
		OrgID:              principal.OrgID,
		RecipientSubjectID: principal.Subject,
	})
	if err != nil {
		return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	latest, err := s.latestNotification(ctx, q, principal.OrgID, principal.Subject)
	if err != nil {
		return Summary{}, err
	}
	unreadCount := state.UnreadCount
	if unreadCount > RingBufferSize {
		unreadCount = RingBufferSize
	}
	return Summary{
		OrgID:              principal.OrgID,
		SubjectID:          principal.Subject,
		UnreadCount:        uint16(unreadCount),
		LatestSequence:     state.LatestSequence,
		ReadUpToSequence:   state.ReadUpToSequence,
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
	rows, err := s.q().ListNotifications(ctx, notificationstore.ListNotificationsParams{
		OrgID:              principal.OrgID,
		RecipientSubjectID: principal.Subject,
		LimitCount:         int32FromInt(input.Limit, "notification list limit"),
	})
	if err != nil {
		return ListResult{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	notifications := make([]Notification, 0, input.Limit)
	for _, row := range rows {
		notification, err := notificationFromListRow(row)
		if err != nil {
			return ListResult{}, err
		}
		notifications = append(notifications, notification)
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
	q := notificationstore.New(tx)
	if err := s.ensureInboxState(ctx, q, principal.OrgID, principal.Subject); err != nil {
		return Summary{}, err
	}
	old, err := s.preferences(ctx, q, principal.OrgID, principal.Subject)
	if err != nil {
		return Summary{}, err
	}
	if old.Version != input.Version {
		return Summary{}, ErrConflict
	}
	now := s.now()
	nextVersion := old.Version + 1
	rowsAffected, err := q.UpsertPreferences(ctx, notificationstore.UpsertPreferencesParams{
		OrgID:           principal.OrgID,
		SubjectID:       principal.Subject,
		NextVersion:     nextVersion,
		Enabled:         input.Enabled,
		UpdatedAt:       timestamptz(now),
		ExpectedVersion: input.Version,
	})
	if err != nil {
		return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if rowsAffected != 1 {
		return Summary{}, ErrConflict
	}
	if err := s.enqueueProjectionTx(ctx, q, projectionInput{
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
	q := notificationstore.New(tx)
	if err := s.ensureInboxState(ctx, q, principal.OrgID, principal.Subject); err != nil {
		return Summary{}, err
	}
	now := s.now()
	traceparent := traceparentFromContext(ctx)
	advancedTo, advanced, err := s.advanceReadCursorTx(
		ctx,
		q,
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
	q := notificationstore.New(tx)
	if err := s.ensureInboxState(ctx, q, principal.OrgID, principal.Subject); err != nil {
		return Summary{}, err
	}
	now := s.now()
	traceparent := traceparentFromContext(ctx)
	row, err := q.MarkNotificationRead(ctx, notificationstore.MarkNotificationReadParams{
		ReadAt:             timestamptz(now),
		NotificationID:     input.NotificationID,
		OrgID:              principal.OrgID,
		RecipientSubjectID: principal.Subject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if exists, err := s.notificationExistsTx(ctx, q, principal.OrgID, principal.Subject, input.NotificationID); err != nil {
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
	projected := projectionFromMarkNotificationRead(row)
	projected.EventType = LedgerInboxRead
	projected.Status = "read"
	projected.OccurredAt = now
	projected.Traceparent = traceparent
	if err := s.enqueueProjectionTx(ctx, q, projected); err != nil {
		return Summary{}, err
	}
	if err := s.touchInboxStateTx(ctx, q, principal.OrgID, principal.Subject, now); err != nil {
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
	q := notificationstore.New(tx)
	if err := s.ensureInboxState(ctx, q, principal.OrgID, principal.Subject); err != nil {
		return Summary{}, err
	}
	now := s.now()
	traceparent := traceparentFromContext(ctx)
	row, err := q.DismissNotification(ctx, notificationstore.DismissNotificationParams{
		DismissedAt:        timestamptz(now),
		NotificationID:     input.NotificationID,
		OrgID:              principal.OrgID,
		RecipientSubjectID: principal.Subject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Summary{}, ErrNotFound
	}
	if err != nil {
		return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	projected := projectionFromDismissNotification(row)
	projected.EventType = LedgerInboxDismissed
	projected.Status = "dismissed"
	projected.OccurredAt = now
	projected.Traceparent = traceparent
	if err := s.enqueueProjectionTx(ctx, q, projected); err != nil {
		return Summary{}, err
	}
	if err := s.touchInboxStateTx(ctx, q, principal.OrgID, principal.Subject, now); err != nil {
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
	q := notificationstore.New(tx)
	if err := s.ensureInboxState(ctx, q, principal.OrgID, principal.Subject); err != nil {
		return Summary{}, err
	}
	now := s.now()
	traceparent := traceparentFromContext(ctx)
	rows, err := q.DismissAllNotifications(ctx, notificationstore.DismissAllNotificationsParams{
		DismissedAt:        timestamptz(now),
		OrgID:              principal.OrgID,
		RecipientSubjectID: principal.Subject,
	})
	if err != nil {
		return Summary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	projections := make([]projectionInput, 0, 16)
	for _, row := range rows {
		projected := projectionFromDismissAllNotifications(row)
		projected.EventType = LedgerInboxDismissed
		projected.Status = "dismissed"
		projected.OccurredAt = now
		projected.Traceparent = traceparent
		projections = append(projections, projected)
	}
	for _, projected := range projections {
		if err := s.enqueueProjectionTx(ctx, q, projected); err != nil {
			return Summary{}, err
		}
	}
	dismissed := len(projections)
	if dismissed > 0 {
		if err := s.touchInboxStateTx(ctx, q, principal.OrgID, principal.Subject, now); err != nil {
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
	q := notificationstore.New(tx)
	now := s.now()
	contentHash := ContentSHA256(event)
	rowsAffected, err := q.InsertEvent(ctx, notificationstore.InsertEventParams{
		EventSource:        event.EventSource,
		EventID:            event.EventID,
		Subject:            event.Subject,
		OrgID:              event.OrgID,
		ActorSubjectID:     event.ActorSubjectID,
		RecipientSubjectID: event.RecipientSubjectID,
		DedupeKey:          event.DedupeKey,
		Kind:               event.Kind,
		Priority:           event.Priority,
		Title:              event.Title,
		Body:               event.Body,
		ActionUrl:          event.ActionURL,
		ResourceKind:       event.ResourceKind,
		ResourceID:         event.ResourceID,
		ContentSha256:      contentHash,
		Payload:            payload,
		Traceparent:        event.Traceparent,
		OccurredAt:         timestamptz(event.OccurredAt),
		ReceivedAt:         timestamptz(now),
	})
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if rowsAffected == 0 {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
		}
		return false, nil
	}
	if err := s.enqueueProjectionTx(ctx, q, projectionInput{
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
	q := notificationstore.New(tx)
	event, err := s.loadEventForUpdate(ctx, q, strings.TrimSpace(eventSource), eventUUID)
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
	preferences, err := s.preferences(ctx, q, event.OrgID, event.RecipientSubjectID)
	if err != nil {
		return err
	}
	now := s.now()
	if !preferences.Enabled {
		if err := q.SuppressEvent(ctx, notificationstore.SuppressEventParams{
			SuppressedAt: timestamptz(now),
			EventSource:  event.EventSource,
			EventID:      event.EventID,
		}); err != nil {
			return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
		}
		if err := s.enqueueProjectionTx(ctx, q, projectionInput{
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
	if err := s.ensureInboxState(ctx, q, event.OrgID, event.RecipientSubjectID); err != nil {
		return err
	}
	sequence, err := q.NextInboxSequence(ctx, notificationstore.NextInboxSequenceParams{
		UpdatedAt:          timestamptz(now),
		OrgID:              event.OrgID,
		RecipientSubjectID: event.RecipientSubjectID,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	notificationID := uuid.New()
	contentHash := ContentSHA256(event)
	if err := q.InsertUserNotification(ctx, notificationstore.InsertUserNotificationParams{
		NotificationID:     notificationID,
		OrgID:              event.OrgID,
		RecipientSubjectID: event.RecipientSubjectID,
		RecipientSequence:  sequence,
		DedupeKey:          event.DedupeKey,
		EventSource:        event.EventSource,
		EventID:            event.EventID,
		Kind:               event.Kind,
		Priority:           event.Priority,
		Title:              event.Title,
		Body:               event.Body,
		ActionUrl:          event.ActionURL,
		ResourceKind:       event.ResourceKind,
		ResourceID:         event.ResourceID,
		ContentSha256:      contentHash,
		CreatedAt:          timestamptz(now),
	}); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if err := s.enqueueProjectionTx(ctx, q, projectionInput{
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
	pruned, err := s.pruneInbox(ctx, q, event.OrgID, event.RecipientSubjectID, sequence, now, event.Traceparent)
	if err != nil {
		return err
	}
	if err := q.MarkEventProcessed(ctx, notificationstore.MarkEventProcessedParams{
		ProcessedAt: timestamptz(now),
		EventSource: event.EventSource,
		EventID:     event.EventID,
	}); err != nil {
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
	q := notificationstore.New(tx)
	rows, err := q.ClaimProjectionQueue(ctx, notificationstore.ClaimProjectionQueueParams{LimitCount: int32(limit)})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	claimed := make([]LedgerRow, 0, limit)
	for _, claimedRow := range rows {
		if claimedRow.RecipientSequence < 0 {
			return fmt.Errorf("%w: recipient_sequence is negative", ErrStoreUnavailable)
		}
		occurredAt, err := requiredTime(claimedRow.OccurredAt)
		if err != nil {
			return err
		}
		row := LedgerRow{
			LedgerEventID:      mustUUID(claimedRow.LedgerEventID),
			EventType:          claimedRow.EventType,
			OrgID:              claimedRow.OrgID,
			RecipientSubjectID: claimedRow.RecipientSubjectID,
			NotificationID:     mustUUID(claimedRow.NotificationIDText),
			EventSource:        claimedRow.EventSource,
			SourceEventID:      mustUUID(claimedRow.EventIDText),
			RecipientSequence:  uint64(claimedRow.RecipientSequence),
			SourceSubject:      claimedRow.SourceSubject,
			Kind:               claimedRow.Kind,
			Priority:           claimedRow.Priority,
			ContentSHA256:      claimedRow.ContentSha256,
			Status:             claimedRow.Status,
			Reason:             claimedRow.Reason,
			Traceparent:        claimedRow.Traceparent,
			OccurredAt:         occurredAt,
		}
		row.RecordedAt = s.now()
		row.SchemaVersion = SchemaVersion
		if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
			row.TraceID = sc.TraceID().String()
			row.SpanID = sc.SpanID().String()
		}
		claimed = append(claimed, row)
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
		if err := q.MarkProjectionProjected(ctx, notificationstore.MarkProjectionProjectedParams{LedgerEventID: row.LedgerEventID}); err != nil {
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

func (s *Service) loadEventForUpdate(ctx context.Context, q *notificationstore.Queries, eventSource string, eventID uuid.UUID) (DomainEvent, error) {
	row, err := q.GetEventForUpdate(ctx, notificationstore.GetEventForUpdateParams{
		EventSource: eventSource,
		EventID:     eventID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return DomainEvent{}, ErrNotFound
	}
	if err != nil {
		return DomainEvent{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if row.ProcessedAt.Valid || row.SuppressedAt.Valid {
		return DomainEvent{}, ErrDuplicate
	}
	occurredAt, err := requiredTime(row.OccurredAt)
	if err != nil {
		return DomainEvent{}, err
	}
	event := DomainEvent{
		EventSource:        row.EventSource,
		EventID:            row.EventID,
		Subject:            row.Subject,
		OrgID:              row.OrgID,
		ActorSubjectID:     row.ActorSubjectID,
		RecipientSubjectID: row.RecipientSubjectID,
		DedupeKey:          row.DedupeKey,
		Kind:               row.Kind,
		Priority:           row.Priority,
		Title:              row.Title,
		Body:               row.Body,
		ActionURL:          row.ActionUrl,
		ResourceKind:       row.ResourceKind,
		ResourceID:         row.ResourceID,
		Traceparent:        row.Traceparent,
		OccurredAt:         occurredAt,
	}
	if len(row.Payload) > 0 {
		_ = json.Unmarshal(row.Payload, &event.Payload)
	}
	return event, nil
}

func (s *Service) ensureInboxState(ctx context.Context, q *notificationstore.Queries, orgID string, subjectID string) error {
	if err := q.EnsureInboxState(ctx, notificationstore.EnsureInboxStateParams{
		OrgID:              orgID,
		RecipientSubjectID: subjectID,
		Now:                timestamptz(s.now()),
	}); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s *Service) advanceReadCursorTx(ctx context.Context, q *notificationstore.Queries, orgID string, subjectID string, readUpTo int64, now time.Time, traceparent string) (int64, bool, error) {
	// Clamp to the latest assigned sequence so a client cannot pre-read future notifications.
	advancedTo, err := q.AdvanceReadCursor(ctx, notificationstore.AdvanceReadCursorParams{
		ReadUpToSequence:   readUpTo,
		UpdatedAt:          timestamptz(now),
		OrgID:              orgID,
		RecipientSubjectID: subjectID,
	})
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
	source, err := q.GetReadCursorProjectionSource(ctx, notificationstore.GetReadCursorProjectionSourceParams{
		OrgID:              orgID,
		RecipientSubjectID: subjectID,
		RecipientSequence:  advancedTo,
	})
	if err == nil {
		projected.NotificationIDText = source.NotificationID
		projected.EventSource = source.EventSource
		projected.EventIDText = source.EventID
		projected.Kind = source.Kind
		projected.Priority = source.Priority
		projected.ContentSHA256 = source.ContentSha256
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if err := s.enqueueProjectionTx(ctx, q, projected); err != nil {
		return 0, false, err
	}
	return advancedTo, true, nil
}

func (s *Service) touchInboxStateTx(ctx context.Context, q *notificationstore.Queries, orgID string, subjectID string, now time.Time) error {
	rowsAffected, err := q.TouchInboxState(ctx, notificationstore.TouchInboxStateParams{
		UpdatedAt:          timestamptz(now),
		OrgID:              orgID,
		RecipientSubjectID: subjectID,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if rowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) notificationExistsTx(ctx context.Context, q *notificationstore.Queries, orgID string, subjectID string, notificationID uuid.UUID) (bool, error) {
	exists, err := q.NotificationExists(ctx, notificationstore.NotificationExistsParams{
		OrgID:              orgID,
		RecipientSubjectID: subjectID,
		NotificationID:     notificationID,
	})
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return exists, nil
}

func (s *Service) preferences(ctx context.Context, q *notificationstore.Queries, orgID string, subjectID string) (Preferences, error) {
	row, err := q.GetPreferences(ctx, notificationstore.GetPreferencesParams{OrgID: orgID, SubjectID: subjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		now := s.now()
		return Preferences{Version: 0, Enabled: true, UpdatedAt: now, UpdatedBy: ""}, nil
	}
	if err != nil {
		return Preferences{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	updatedAt, err := requiredTime(row.UpdatedAt)
	if err != nil {
		return Preferences{}, err
	}
	return Preferences{Version: row.Version, Enabled: row.Enabled, UpdatedAt: updatedAt, UpdatedBy: row.UpdatedBy}, nil
}

func (s *Service) latestNotification(ctx context.Context, q *notificationstore.Queries, orgID string, subjectID string) (*Notification, error) {
	row, err := q.GetLatestNotification(ctx, notificationstore.GetLatestNotificationParams{
		OrgID:              orgID,
		RecipientSubjectID: subjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	notification, err := notificationFromLatestRow(row)
	if err != nil {
		return nil, err
	}
	return &notification, nil
}

func (s *Service) pruneInbox(ctx context.Context, q *notificationstore.Queries, orgID string, subjectID string, sequence int64, now time.Time, traceparent string) (int, error) {
	cutoff := sequence - RingBufferSize
	if cutoff <= 0 {
		return 0, nil
	}
	rows, err := q.PruneInbox(ctx, notificationstore.PruneInboxParams{
		OrgID:              orgID,
		RecipientSubjectID: subjectID,
		CutoffSequence:     cutoff,
	})
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	count := 0
	for _, row := range rows {
		input := projectionInput{
			EventType:          LedgerInboxPruned,
			OrgID:              orgID,
			RecipientSubjectID: subjectID,
			Status:             "pruned",
			Reason:             "ring_buffer_limit",
			OccurredAt:         now,
			Traceparent:        traceparent,
		}
		input.NotificationIDText = row.NotificationID
		input.RecipientSequence = row.RecipientSequence
		input.EventSource = row.EventSource
		input.EventIDText = row.EventID
		input.Kind = row.Kind
		input.Priority = row.Priority
		input.ContentSHA256 = row.ContentSha256
		if err := s.enqueueProjectionTx(ctx, q, input); err != nil {
			return count, err
		}
		count++
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

func (s *Service) enqueueProjectionTx(ctx context.Context, q *notificationstore.Queries, input projectionInput) error {
	if input.OccurredAt.IsZero() {
		input.OccurredAt = s.now()
	}
	ledgerID := deterministicLedgerID(input)
	if err := q.EnqueueProjection(ctx, notificationstore.EnqueueProjectionParams{
		LedgerEventID:      ledgerID,
		EventType:          input.EventType,
		OrgID:              input.OrgID,
		RecipientSubjectID: input.RecipientSubjectID,
		NotificationIDText: input.NotificationIDText,
		EventSource:        input.EventSource,
		EventIDText:        input.EventIDText,
		RecipientSequence:  input.RecipientSequence,
		SourceSubject:      input.SourceSubject,
		Kind:               input.Kind,
		Priority:           input.Priority,
		ContentSha256:      input.ContentSHA256,
		Status:             input.Status,
		Reason:             input.Reason,
		Traceparent:        input.Traceparent,
		OccurredAt:         timestamptz(input.OccurredAt),
	}); err != nil {
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

func projectionFromMarkNotificationRead(row notificationstore.MarkNotificationReadRow) projectionInput {
	return projectionInput{
		OrgID:              row.OrgID,
		RecipientSubjectID: row.RecipientSubjectID,
		NotificationIDText: row.NotificationID,
		RecipientSequence:  row.RecipientSequence,
		EventSource:        row.EventSource,
		EventIDText:        row.EventID,
		Kind:               row.Kind,
		Priority:           row.Priority,
		ContentSHA256:      row.ContentSha256,
	}
}

func projectionFromDismissNotification(row notificationstore.DismissNotificationRow) projectionInput {
	return projectionInput{
		OrgID:              row.OrgID,
		RecipientSubjectID: row.RecipientSubjectID,
		NotificationIDText: row.NotificationID,
		RecipientSequence:  row.RecipientSequence,
		EventSource:        row.EventSource,
		EventIDText:        row.EventID,
		Kind:               row.Kind,
		Priority:           row.Priority,
		ContentSHA256:      row.ContentSha256,
	}
}

func projectionFromDismissAllNotifications(row notificationstore.DismissAllNotificationsRow) projectionInput {
	return projectionInput{
		OrgID:              row.OrgID,
		RecipientSubjectID: row.RecipientSubjectID,
		NotificationIDText: row.NotificationID,
		RecipientSequence:  row.RecipientSequence,
		EventSource:        row.EventSource,
		EventIDText:        row.EventID,
		Kind:               row.Kind,
		Priority:           row.Priority,
		ContentSHA256:      row.ContentSha256,
	}
}

func notificationFromListRow(row notificationstore.ListNotificationsRow) (Notification, error) {
	return notificationFromFields(
		row.NotificationID,
		row.OrgID,
		row.RecipientSubjectID,
		row.RecipientSequence,
		row.Kind,
		row.Priority,
		row.Title,
		row.Body,
		row.ActionUrl,
		row.ResourceKind,
		row.ResourceID,
		row.CreatedAt,
		row.ExpiresAt,
		row.ReadAt,
		row.DismissedAt,
	)
}

func notificationFromLatestRow(row notificationstore.GetLatestNotificationRow) (Notification, error) {
	return notificationFromFields(
		row.NotificationID,
		row.OrgID,
		row.RecipientSubjectID,
		row.RecipientSequence,
		row.Kind,
		row.Priority,
		row.Title,
		row.Body,
		row.ActionUrl,
		row.ResourceKind,
		row.ResourceID,
		row.CreatedAt,
		row.ExpiresAt,
		row.ReadAt,
		row.DismissedAt,
	)
}

func notificationFromFields(
	notificationID uuid.UUID,
	orgID string,
	recipientSubjectID string,
	recipientSequence int64,
	kind string,
	priority string,
	title string,
	body string,
	actionURL string,
	resourceKind string,
	resourceID string,
	createdAt pgtype.Timestamptz,
	expiresAt pgtype.Timestamptz,
	readAt pgtype.Timestamptz,
	dismissedAt pgtype.Timestamptz,
) (Notification, error) {
	requiredCreatedAt, err := requiredTime(createdAt)
	if err != nil {
		return Notification{}, err
	}
	return Notification{
		NotificationID:     notificationID,
		OrgID:              orgID,
		RecipientSubjectID: recipientSubjectID,
		RecipientSequence:  recipientSequence,
		Kind:               kind,
		Priority:           priority,
		Title:              title,
		Body:               body,
		ActionURL:          actionURL,
		ResourceKind:       resourceKind,
		ResourceID:         resourceID,
		CreatedAt:          requiredCreatedAt,
		ExpiresAt:          optionalTime(expiresAt),
		ReadAt:             optionalTime(readAt),
		DismissedAt:        optionalTime(dismissedAt),
	}, nil
}

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time.UTC()
	return &t
}

func requiredTime(value pgtype.Timestamptz) (time.Time, error) {
	if !value.Valid {
		return time.Time{}, fmt.Errorf("%w: required timestamp was null", ErrStoreUnavailable)
	}
	return value.Time.UTC(), nil
}

func timestamptz(value time.Time) pgtype.Timestamptz {
	if value.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
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
