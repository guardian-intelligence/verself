package notifications

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
)

const (
	ServiceName    = "notifications-service"
	SchemaVersion  = "2026-04-24.v1"
	RingBufferSize = int64(999)

	DefaultPriority = "normal"
	PriorityLow     = "low"
	PriorityNormal  = "normal"
	PriorityHigh    = "high"

	EventsStreamName      = "DOMAIN_EVENTS"
	EventsConsumerDurable = "notifications-service"
	EventsSubjectPattern  = "events.>"

	LedgerEventReceived      = "notification.event.received"
	LedgerInboxCreated       = "notification.inbox.created"
	LedgerInboxPruned        = "notification.inbox.pruned"
	LedgerInboxDismissed     = "notification.inbox.dismissed"
	LedgerInboxRead          = "notification.inbox.read"
	LedgerReadCursorAdvanced = "notification.read_cursor_advanced"
	LedgerDeliveryQueued     = "notification.delivery.queued"
	LedgerDeliverySent       = "notification.delivery.sent"
	LedgerDeliveryFailed     = "notification.delivery.failed"
	LedgerDeliverySuppressed = "notification.delivery.suppressed"
	LedgerPreferencesUpdated = "notification.preferences.updated"
	defaultSyntheticSubject  = "events.notifications.test.requested"
	defaultSyntheticKind     = "test"
	defaultSyntheticTitle    = "Notification test"
	defaultSyntheticBody     = "Your realtime notification pipeline is working."
	defaultListLimit         = 50
	maxListLimit             = 500
	maxTitleRunes            = 240
	maxBodyRunes             = 4096
	maxActionURLBytes        = 8192

	zitadelGenericProjectRolesClaim = "urn:zitadel:iam:org:project:roles"
)

var (
	ErrInvalidInput     = errors.New("invalid input")
	ErrConflict         = errors.New("version conflict")
	ErrNotFound         = errors.New("not found")
	ErrStoreUnavailable = errors.New("notification store unavailable")
	ErrDuplicate        = errors.New("duplicate notification event")
)

var tracer = otel.Tracer("notifications-service/internal/notifications")

type Principal struct {
	Subject string
	OrgID   string
	Email   string
	Raw     map[string]any
}

type Preferences struct {
	Version      int32
	Enabled      bool
	WebEnabled   bool
	EmailEnabled bool
	PushEnabled  bool
	SMSEnabled   bool
	UpdatedAt    time.Time
	UpdatedBy    string
}

type Summary struct {
	OrgID              string
	SubjectID          string
	UnreadCount        uint16
	LatestSequence     int64
	ReadUpToSequence   int64
	Preferences        Preferences
	LatestNotification *Notification
}

type ListResult struct {
	Summary       Summary
	Notifications []Notification
	NextCursor    string
}

type Notification struct {
	NotificationID     uuid.UUID
	OrgID              string
	RecipientSubjectID string
	RecipientSequence  int64
	Kind               string
	Priority           string
	Title              string
	Body               string
	ActionURL          string
	ResourceKind       string
	ResourceID         string
	CreatedAt          time.Time
	ExpiresAt          *time.Time
	ReadAt             *time.Time
	DismissedAt        *time.Time
}

type PutPreferencesRequest struct {
	Version      int32
	Enabled      bool
	WebEnabled   *bool
	EmailEnabled *bool
	PushEnabled  *bool
	SMSEnabled   *bool
}

type MarkReadRequest struct {
	ReadUpToSequence int64
}

type ListRequest struct {
	Limit  int
	Cursor string
}

type DismissRequest struct {
	NotificationID uuid.UUID
}

type ReadNotificationRequest struct {
	NotificationID uuid.UUID
}

type TestRequest struct {
	Title     string
	Body      string
	ActionURL string
}

type Accepted struct {
	EventID     uuid.UUID
	Traceparent string
}

type WorkflowRecipient struct {
	SubjectID string
	Email     string
}

type WorkflowTriggerRequest struct {
	WorkflowKey    string
	OrgID          string
	TriggeredBy    string
	IdempotencyKey string
	Recipients     []WorkflowRecipient
	Title          string
	Body           string
	ActionURL      string
	Priority       string
	ResourceKind   string
	ResourceID     string
	Data           json.RawMessage
	Traceparent    string
}

type WorkflowTriggerResult struct {
	WorkflowRunID            uuid.UUID
	WorkflowKey              string
	OrgID                    string
	AcceptedAt               time.Time
	RecipientCount           uint16
	WebNotificationsAccepted uint16
	EmailDeliveriesQueued    uint16
	SuppressedCount          uint16
	Traceparent              string
}

type DomainEvent struct {
	EventID            uuid.UUID      `json:"event_id"`
	EventSource        string         `json:"event_source"`
	Subject            string         `json:"subject"`
	OrgID              string         `json:"org_id"`
	ActorSubjectID     string         `json:"actor_subject_id,omitempty"`
	RecipientSubjectID string         `json:"recipient_subject_id"`
	DedupeKey          string         `json:"dedupe_key"`
	Kind               string         `json:"kind"`
	Priority           string         `json:"priority"`
	Title              string         `json:"title"`
	Body               string         `json:"body"`
	ActionURL          string         `json:"action_url,omitempty"`
	ResourceKind       string         `json:"resource_kind,omitempty"`
	ResourceID         string         `json:"resource_id,omitempty"`
	OccurredAt         time.Time      `json:"occurred_at"`
	Payload            map[string]any `json:"payload,omitempty"`
	Traceparent        string         `json:"traceparent,omitempty"`
}

func ValidatePrincipal(principal Principal) error {
	if strings.TrimSpace(principal.Subject) == "" {
		return fmt.Errorf("%w: subject is required", ErrInvalidInput)
	}
	if strings.TrimSpace(principal.OrgID) == "" {
		return fmt.Errorf("%w: org_id is required", ErrInvalidInput)
	}
	if credentialID, _ := principal.Raw["verself:credential_id"].(string); strings.TrimSpace(credentialID) != "" {
		return fmt.Errorf("%w: api credentials cannot use human notification inboxes", ErrInvalidInput)
	}
	if !hasGenericProjectRolesClaim(principal.Raw) {
		return fmt.Errorf("%w: human token marker is required", ErrInvalidInput)
	}
	return nil
}

func hasGenericProjectRolesClaim(claims map[string]any) bool {
	// ZITADEL access tokens here omit email, so the generic roles claim is the current human-token discriminator.
	value, ok := claims[zitadelGenericProjectRolesClaim]
	if !ok {
		return false
	}
	roles, ok := value.(map[string]any)
	return ok && len(roles) > 0
}

func NormalizePutPreferences(input PutPreferencesRequest) (PutPreferencesRequest, error) {
	if input.Version < 0 {
		return input, fmt.Errorf("%w: version must be non-negative", ErrInvalidInput)
	}
	return input, nil
}

func NormalizeWorkflowTrigger(input WorkflowTriggerRequest) (WorkflowTriggerRequest, error) {
	input.WorkflowKey = strings.TrimSpace(input.WorkflowKey)
	input.OrgID = strings.TrimSpace(input.OrgID)
	input.TriggeredBy = strings.TrimSpace(input.TriggeredBy)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Title = normalizeText(input.Title)
	input.Body = normalizeText(input.Body)
	input.ActionURL = strings.TrimSpace(input.ActionURL)
	input.Priority = strings.TrimSpace(input.Priority)
	input.ResourceKind = strings.TrimSpace(input.ResourceKind)
	input.ResourceID = strings.TrimSpace(input.ResourceID)
	input.Traceparent = strings.TrimSpace(input.Traceparent)
	if err := validateWorkflowKey(input.WorkflowKey); err != nil {
		return input, err
	}
	if input.OrgID == "" {
		return input, fmt.Errorf("%w: org_id is required", ErrInvalidInput)
	}
	if input.TriggeredBy == "" {
		return input, fmt.Errorf("%w: triggered_by is required", ErrInvalidInput)
	}
	if input.IdempotencyKey == "" {
		return input, fmt.Errorf("%w: idempotency key is required", ErrInvalidInput)
	}
	if len(input.IdempotencyKey) > 128 {
		return input, fmt.Errorf("%w: idempotency key is too long", ErrInvalidInput)
	}
	switch input.Priority {
	case PriorityLow, PriorityNormal, PriorityHigh:
	case "":
		input.Priority = DefaultPriority
	default:
		return input, fmt.Errorf("%w: priority is invalid", ErrInvalidInput)
	}
	if input.Title == "" {
		input.Title = input.WorkflowKey
	}
	if input.Body == "" {
		return input, fmt.Errorf("%w: body is required", ErrInvalidInput)
	}
	if err := validateText("title", input.Title, maxTitleRunes); err != nil {
		return input, err
	}
	if err := validateText("body", input.Body, maxBodyRunes); err != nil {
		return input, err
	}
	if len(input.ActionURL) > maxActionURLBytes {
		return input, fmt.Errorf("%w: action_url is too long", ErrInvalidInput)
	}
	if len(input.Recipients) == 0 {
		return input, fmt.Errorf("%w: at least one recipient is required", ErrInvalidInput)
	}
	if len(input.Recipients) > WorkflowRecipientLimit {
		return input, fmt.Errorf("%w: at most %d recipients are supported", ErrInvalidInput, WorkflowRecipientLimit)
	}
	for idx := range input.Recipients {
		input.Recipients[idx].SubjectID = strings.TrimSpace(input.Recipients[idx].SubjectID)
		input.Recipients[idx].Email = strings.TrimSpace(input.Recipients[idx].Email)
		if input.Recipients[idx].SubjectID == "" && input.Recipients[idx].Email == "" {
			return input, fmt.Errorf("%w: recipient subject_id or email is required", ErrInvalidInput)
		}
	}
	if len(input.Data) == 0 {
		input.Data = json.RawMessage(`{}`)
	}
	if !json.Valid(input.Data) {
		return input, fmt.Errorf("%w: data must be valid JSON", ErrInvalidInput)
	}
	return input, nil
}

func NormalizeMarkRead(input MarkReadRequest) (MarkReadRequest, error) {
	if input.ReadUpToSequence < 0 {
		return input, fmt.Errorf("%w: read_up_to_sequence must be non-negative", ErrInvalidInput)
	}
	return input, nil
}

func NormalizeListRequest(input ListRequest) (ListRequest, error) {
	if input.Limit <= 0 {
		input.Limit = defaultListLimit
	}
	if input.Limit > maxListLimit {
		input.Limit = maxListLimit
	}
	input.Cursor = strings.TrimSpace(input.Cursor)
	return input, nil
}

func NormalizeTestRequest(input TestRequest) (TestRequest, error) {
	input.Title = normalizeText(input.Title)
	input.Body = normalizeText(input.Body)
	input.ActionURL = strings.TrimSpace(input.ActionURL)
	if input.Title == "" {
		input.Title = defaultSyntheticTitle
	}
	if input.Body == "" {
		input.Body = defaultSyntheticBody
	}
	if err := validateText("title", input.Title, maxTitleRunes); err != nil {
		return input, err
	}
	if err := validateText("body", input.Body, maxBodyRunes); err != nil {
		return input, err
	}
	if len(input.ActionURL) > maxActionURLBytes {
		return input, fmt.Errorf("%w: action_url is too long", ErrInvalidInput)
	}
	return input, nil
}

func NormalizeDomainEvent(event DomainEvent) (DomainEvent, error) {
	event.EventSource = strings.TrimSpace(event.EventSource)
	if event.EventSource == "" {
		event.EventSource = "nats"
	}
	event.Subject = strings.TrimSpace(event.Subject)
	event.OrgID = strings.TrimSpace(event.OrgID)
	event.ActorSubjectID = strings.TrimSpace(event.ActorSubjectID)
	event.RecipientSubjectID = strings.TrimSpace(event.RecipientSubjectID)
	event.DedupeKey = strings.TrimSpace(event.DedupeKey)
	event.Kind = strings.TrimSpace(event.Kind)
	event.Priority = strings.TrimSpace(event.Priority)
	event.Title = normalizeText(event.Title)
	event.Body = normalizeText(event.Body)
	event.ActionURL = strings.TrimSpace(event.ActionURL)
	event.ResourceKind = strings.TrimSpace(event.ResourceKind)
	event.ResourceID = strings.TrimSpace(event.ResourceID)
	event.Traceparent = strings.TrimSpace(event.Traceparent)
	if event.EventID == uuid.Nil {
		return event, fmt.Errorf("%w: event_id is required", ErrInvalidInput)
	}
	if event.Subject == "" {
		return event, fmt.Errorf("%w: subject is required", ErrInvalidInput)
	}
	if event.OrgID == "" {
		return event, fmt.Errorf("%w: org_id is required", ErrInvalidInput)
	}
	if event.RecipientSubjectID == "" {
		return event, fmt.Errorf("%w: recipient_subject_id is required", ErrInvalidInput)
	}
	if event.Kind == "" {
		return event, fmt.Errorf("%w: kind is required", ErrInvalidInput)
	}
	if event.DedupeKey == "" {
		event.DedupeKey = event.EventSource + ":" + event.EventID.String()
	}
	switch event.Priority {
	case PriorityLow, PriorityNormal, PriorityHigh:
	case "":
		event.Priority = DefaultPriority
	default:
		return event, fmt.Errorf("%w: priority is invalid", ErrInvalidInput)
	}
	if err := validateText("title", event.Title, maxTitleRunes); err != nil {
		return event, err
	}
	if err := validateText("body", event.Body, maxBodyRunes); err != nil {
		return event, err
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	} else {
		event.OccurredAt = event.OccurredAt.UTC()
	}
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}
	return event, nil
}

func NewSyntheticEvent(principal Principal, input TestRequest, now time.Time, traceparent string) (DomainEvent, error) {
	if err := ValidatePrincipal(principal); err != nil {
		return DomainEvent{}, err
	}
	input, err := NormalizeTestRequest(input)
	if err != nil {
		return DomainEvent{}, err
	}
	eventID := uuid.New()
	return NormalizeDomainEvent(DomainEvent{
		EventID:            eventID,
		EventSource:        ServiceName,
		Subject:            defaultSyntheticSubject,
		OrgID:              principal.OrgID,
		ActorSubjectID:     principal.Subject,
		RecipientSubjectID: principal.Subject,
		DedupeKey:          "notifications:test:" + eventID.String(),
		Kind:               defaultSyntheticKind,
		Priority:           PriorityNormal,
		Title:              input.Title,
		Body:               input.Body,
		ActionURL:          input.ActionURL,
		ResourceKind:       "notification_test",
		ResourceID:         eventID.String(),
		OccurredAt:         now.UTC(),
		Payload: map[string]any{
			"synthetic": true,
		},
		Traceparent: traceparent,
	})
}

func ContentSHA256(event DomainEvent) string {
	body, _ := json.Marshal(struct {
		Kind         string `json:"kind"`
		Title        string `json:"title"`
		Body         string `json:"body"`
		ActionURL    string `json:"action_url"`
		ResourceKind string `json:"resource_kind"`
		ResourceID   string `json:"resource_id"`
	}{
		Kind:         event.Kind,
		Title:        event.Title,
		Body:         event.Body,
		ActionURL:    event.ActionURL,
		ResourceKind: event.ResourceKind,
		ResourceID:   event.ResourceID,
	})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func WorkflowContentSHA256(input WorkflowTriggerRequest) string {
	body, _ := json.Marshal(struct {
		WorkflowKey  string          `json:"workflow_key"`
		Title        string          `json:"title"`
		Body         string          `json:"body"`
		ActionURL    string          `json:"action_url"`
		ResourceKind string          `json:"resource_kind"`
		ResourceID   string          `json:"resource_id"`
		Data         json.RawMessage `json:"data"`
	}{
		WorkflowKey:  input.WorkflowKey,
		Title:        input.Title,
		Body:         input.Body,
		ActionURL:    input.ActionURL,
		ResourceKind: input.ResourceKind,
		ResourceID:   input.ResourceID,
		Data:         input.Data,
	})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func validateWorkflowKey(value string) error {
	if value == "" {
		return fmt.Errorf("%w: workflow_key is required", ErrInvalidInput)
	}
	if len(value) > 128 {
		return fmt.Errorf("%w: workflow_key is too long", ErrInvalidInput)
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return fmt.Errorf("%w: workflow_key contains unsupported characters", ErrInvalidInput)
	}
	return nil
}

func normalizeText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func validateText(field, value string, maxRunes int) error {
	if value == "" {
		return fmt.Errorf("%w: %s is required", ErrInvalidInput, field)
	}
	runes := []rune(value)
	if len(runes) > maxRunes {
		return fmt.Errorf("%w: %s is too long", ErrInvalidInput, field)
	}
	for _, r := range runes {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: %s contains unsupported control text", ErrInvalidInput, field)
		}
	}
	return nil
}
