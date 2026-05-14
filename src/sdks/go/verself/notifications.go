package verself

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	notificationscore "github.com/verself/verself-go/internal/transport/notifications"
)

type NotificationPriority string

const (
	NotificationPriorityLow    NotificationPriority = "low"
	NotificationPriorityNormal NotificationPriority = "normal"
	NotificationPriorityHigh   NotificationPriority = "high"
)

type NotificationPreferences struct {
	Enabled      bool      `json:"enabled"`
	WebEnabled   bool      `json:"web_enabled"`
	EmailEnabled bool      `json:"email_enabled"`
	PushEnabled  bool      `json:"push_enabled"`
	SMSEnabled   bool      `json:"sms_enabled"`
	Version      int32     `json:"version"`
	UpdatedAt    time.Time `json:"updated_at"`
	UpdatedBy    string    `json:"updated_by"`
}

type Notification struct {
	NotificationID     string               `json:"notification_id"`
	ResourceName       string               `json:"resourceName"`
	OrgID              string               `json:"org_id"`
	RecipientSubjectID string               `json:"recipient_subject_id"`
	RecipientSequence  int64                `json:"recipient_sequence"`
	Kind               string               `json:"kind"`
	Priority           NotificationPriority `json:"priority"`
	Title              string               `json:"title"`
	Body               string               `json:"body"`
	ActionURL          string               `json:"action_url,omitempty"`
	TargetResourceName string               `json:"targetResourceName,omitempty"`
	CreatedAt          time.Time            `json:"created_at"`
	ExpiresAt          *time.Time           `json:"expires_at,omitempty"`
	ReadAt             *time.Time           `json:"read_at,omitempty"`
	DismissedAt        *time.Time           `json:"dismissed_at,omitempty"`
}

type NotificationSummary struct {
	OrgID              string                  `json:"org_id"`
	SubjectID          string                  `json:"subject_id"`
	UnreadCount        int32                   `json:"unread_count"`
	LatestSequence     int64                   `json:"latest_sequence"`
	ReadUpToSequence   int64                   `json:"read_up_to_sequence"`
	Preferences        NotificationPreferences `json:"preferences"`
	LatestNotification *Notification           `json:"latest_notification,omitempty"`
}

type NotificationList struct {
	Summary       NotificationSummary `json:"summary"`
	Notifications []Notification      `json:"notifications"`
	NextCursor    string              `json:"next_cursor,omitempty"`
}

type NotificationAccepted struct {
	EventID     string `json:"event_id"`
	Traceparent string `json:"traceparent,omitempty"`
}

type NotificationsMutationOptions struct {
	IdempotencyKey string
}

type ListNotificationsOptions struct {
	Limit  int64
	Cursor string
}

type PutNotificationPreferencesInput struct {
	Version        int32
	Enabled        bool
	WebEnabled     *bool
	EmailEnabled   *bool
	PushEnabled    *bool
	SMSEnabled     *bool
	IdempotencyKey string
}

type MarkNotificationsReadInput struct {
	ReadUpToSequence int64
	IdempotencyKey   string
}

type NotificationIDMutationInput struct {
	NotificationID string
	IdempotencyKey string
}

type PublishTestNotificationInput struct {
	Title          string
	Body           string
	ActionURL      string
	IdempotencyKey string
}

type NotificationsClient struct {
	client *notificationscore.Client
}

func (c *NotificationsClient) List(ctx context.Context, options ListNotificationsOptions) (NotificationList, error) {
	if c == nil || c.client == nil {
		return NotificationList{}, fmt.Errorf("verself sdk: notifications client is not initialized")
	}
	request := notificationscore.ListNotificationsRequest{}
	if options.Limit > 0 {
		limit := notificationscore.NotificationPageSize(options.Limit)
		request.Limit = &limit
	}
	if strings.TrimSpace(options.Cursor) != "" {
		cursor := notificationscore.PageToken(strings.TrimSpace(options.Cursor))
		request.Cursor = &cursor
	}
	response, err := c.client.ListNotifications(ctx, request)
	if err != nil {
		return NotificationList{}, err
	}
	if response.Result == nil {
		return NotificationList{}, notificationsAPIError("list notifications", response.StatusCode, response.Problem, response.Body)
	}
	return notificationListFromGenerated(*response.Result)
}

func (c *NotificationsClient) Summary(ctx context.Context) (NotificationSummary, error) {
	if c == nil || c.client == nil {
		return NotificationSummary{}, fmt.Errorf("verself sdk: notifications client is not initialized")
	}
	response, err := c.client.GetNotificationSummary(ctx, notificationscore.GetNotificationSummaryRequest{})
	if err != nil {
		return NotificationSummary{}, err
	}
	if response.Result == nil {
		return NotificationSummary{}, notificationsAPIError("get notification summary", response.StatusCode, response.Problem, response.Body)
	}
	return notificationSummaryFromGenerated(response.Result)
}

func (c *NotificationsClient) PutPreferences(ctx context.Context, input PutNotificationPreferencesInput) (NotificationSummary, error) {
	if c == nil || c.client == nil {
		return NotificationSummary{}, fmt.Errorf("verself sdk: notifications client is not initialized")
	}
	if input.Version < 0 {
		return NotificationSummary{}, fmt.Errorf("verself sdk: notification preferences version must be non-negative")
	}
	key, err := mutationKey("notification-preferences", input.IdempotencyKey)
	if err != nil {
		return NotificationSummary{}, err
	}
	body := notificationscore.PutNotificationPreferencesInputBody{
		Enabled:      input.Enabled,
		Version:      notificationscore.PreferenceVersion(input.Version),
		WebEnabled:   input.WebEnabled,
		EmailEnabled: input.EmailEnabled,
		PushEnabled:  input.PushEnabled,
		SmsEnabled:   input.SMSEnabled,
	}
	response, err := c.client.PutNotificationPreferences(ctx, notificationscore.PutNotificationPreferencesRequest{
		IdempotencyKey: notificationscore.IdempotencyKey(key),
		Body:           body,
	})
	if err != nil {
		return NotificationSummary{}, err
	}
	if response.Result == nil {
		return NotificationSummary{}, notificationsAPIError("put notification preferences", response.StatusCode, response.Problem, response.Body)
	}
	return notificationSummaryFromGenerated(response.Result)
}

func (c *NotificationsClient) MarkRead(ctx context.Context, input MarkNotificationsReadInput) (NotificationSummary, error) {
	if c == nil || c.client == nil {
		return NotificationSummary{}, fmt.Errorf("verself sdk: notifications client is not initialized")
	}
	if input.ReadUpToSequence < 0 {
		return NotificationSummary{}, fmt.Errorf("verself sdk: notification read cursor must be non-negative")
	}
	key, err := mutationKey("notification-read-cursor", input.IdempotencyKey)
	if err != nil {
		return NotificationSummary{}, err
	}
	response, err := c.client.AdvanceNotificationReadCursor(ctx, notificationscore.AdvanceNotificationReadCursorRequest{
		IdempotencyKey: notificationscore.IdempotencyKey(key),
		Body: notificationscore.AdvanceNotificationReadCursorInputBody{
			ReadUpToSequence: notificationscore.DecimalInt64(strconv.FormatInt(input.ReadUpToSequence, 10)),
		},
	})
	if err != nil {
		return NotificationSummary{}, err
	}
	if response.Result == nil {
		return NotificationSummary{}, notificationsAPIError("mark notifications read", response.StatusCode, response.Problem, response.Body)
	}
	return notificationSummaryFromGenerated(response.Result)
}

func (c *NotificationsClient) MarkNotificationRead(ctx context.Context, input NotificationIDMutationInput) (NotificationSummary, error) {
	if c == nil || c.client == nil {
		return NotificationSummary{}, fmt.Errorf("verself sdk: notifications client is not initialized")
	}
	id, err := parseUUIDInput(input.NotificationID, "notification id")
	if err != nil {
		return NotificationSummary{}, err
	}
	key, err := mutationKey("notification-read", input.IdempotencyKey)
	if err != nil {
		return NotificationSummary{}, err
	}
	response, err := c.client.MarkNotificationRead(ctx, notificationscore.MarkNotificationReadRequest{
		NotificationID: notificationscore.NotificationId(id.String()),
		IdempotencyKey: notificationscore.IdempotencyKey(key),
	})
	if err != nil {
		return NotificationSummary{}, err
	}
	if response.Result == nil {
		return NotificationSummary{}, notificationsAPIError("mark notification read", response.StatusCode, response.Problem, response.Body)
	}
	return notificationSummaryFromGenerated(response.Result)
}

func (c *NotificationsClient) Dismiss(ctx context.Context, input NotificationIDMutationInput) (NotificationSummary, error) {
	if c == nil || c.client == nil {
		return NotificationSummary{}, fmt.Errorf("verself sdk: notifications client is not initialized")
	}
	id, err := parseUUIDInput(input.NotificationID, "notification id")
	if err != nil {
		return NotificationSummary{}, err
	}
	key, err := mutationKey("notification-dismiss", input.IdempotencyKey)
	if err != nil {
		return NotificationSummary{}, err
	}
	response, err := c.client.DismissNotification(ctx, notificationscore.DismissNotificationRequest{
		NotificationID: notificationscore.NotificationId(id.String()),
		IdempotencyKey: notificationscore.IdempotencyKey(key),
	})
	if err != nil {
		return NotificationSummary{}, err
	}
	if response.Result == nil {
		return NotificationSummary{}, notificationsAPIError("dismiss notification", response.StatusCode, response.Problem, response.Body)
	}
	return notificationSummaryFromGenerated(response.Result)
}

func (c *NotificationsClient) Clear(ctx context.Context, options NotificationsMutationOptions) (NotificationSummary, error) {
	if c == nil || c.client == nil {
		return NotificationSummary{}, fmt.Errorf("verself sdk: notifications client is not initialized")
	}
	key, err := mutationKey("notification-clear", options.IdempotencyKey)
	if err != nil {
		return NotificationSummary{}, err
	}
	response, err := c.client.ClearNotifications(ctx, notificationscore.ClearNotificationsRequest{IdempotencyKey: notificationscore.IdempotencyKey(key)})
	if err != nil {
		return NotificationSummary{}, err
	}
	if response.Result == nil {
		return NotificationSummary{}, notificationsAPIError("clear notifications", response.StatusCode, response.Problem, response.Body)
	}
	return notificationSummaryFromGenerated(response.Result)
}

func (c *NotificationsClient) PublishTest(ctx context.Context, input PublishTestNotificationInput) (NotificationAccepted, error) {
	if c == nil || c.client == nil {
		return NotificationAccepted{}, fmt.Errorf("verself sdk: notifications client is not initialized")
	}
	key, err := mutationKey("notification-test", input.IdempotencyKey)
	if err != nil {
		return NotificationAccepted{}, err
	}
	body := notificationscore.PublishTestNotificationInputBody{
		Title:     trimPointer(input.Title),
		Body:      trimPointer(input.Body),
		ActionURL: trimPointer(input.ActionURL),
	}
	response, err := c.client.PublishTestNotification(ctx, notificationscore.PublishTestNotificationRequest{
		IdempotencyKey: notificationscore.IdempotencyKey(key),
		Body:           body,
	})
	if err != nil {
		return NotificationAccepted{}, err
	}
	if response.Result == nil {
		return NotificationAccepted{}, notificationsAPIError("publish test notification", response.StatusCode, response.Problem, response.Body)
	}
	return notificationAcceptedFromGenerated(*response.Result), nil
}

func notificationListFromGenerated(input notificationscore.ListNotificationsOutputBody) (NotificationList, error) {
	summary, err := notificationSummaryFromGenerated(&input.Summary)
	if err != nil {
		return NotificationList{}, err
	}
	out := NotificationList{Summary: summary}
	if input.NextCursor != nil {
		out.NextCursor = string(*input.NextCursor)
	}
	out.Notifications = make([]Notification, 0, len(input.Notifications))
	for _, generated := range input.Notifications {
		notification, err := notificationFromGenerated(generated)
		if err != nil {
			return NotificationList{}, err
		}
		out.Notifications = append(out.Notifications, notification)
	}
	return out, nil
}

func notificationSummaryFromGenerated(input *notificationscore.NotificationSummary) (NotificationSummary, error) {
	if input == nil {
		return NotificationSummary{}, fmt.Errorf("verself sdk: notification summary is missing")
	}
	latestSequence, err := parseDecimalInt64(input.LatestSequence, "latest sequence")
	if err != nil {
		return NotificationSummary{}, err
	}
	readUpToSequence, err := parseDecimalInt64(input.ReadUpToSequence, "read cursor")
	if err != nil {
		return NotificationSummary{}, err
	}
	unreadCount, err := int32FromGenerated(input.UnreadCount, "notification unread count")
	if err != nil {
		return NotificationSummary{}, err
	}
	preferencesVersion, err := int32FromGenerated(input.Preferences.Version, "notification preferences version")
	if err != nil {
		return NotificationSummary{}, err
	}
	preferencesUpdatedAt, err := parseGeneratedTime(input.Preferences.UpdatedAt, "notification preferences updated_at")
	if err != nil {
		return NotificationSummary{}, err
	}
	out := NotificationSummary{
		OrgID:            input.OrgID,
		SubjectID:        input.SubjectID,
		UnreadCount:      unreadCount,
		LatestSequence:   latestSequence,
		ReadUpToSequence: readUpToSequence,
		Preferences: NotificationPreferences{
			Enabled:      input.Preferences.Enabled,
			WebEnabled:   input.Preferences.WebEnabled,
			EmailEnabled: input.Preferences.EmailEnabled,
			PushEnabled:  input.Preferences.PushEnabled,
			SMSEnabled:   input.Preferences.SmsEnabled,
			Version:      preferencesVersion,
			UpdatedAt:    preferencesUpdatedAt,
			UpdatedBy:    optionalGeneratedString(input.Preferences.UpdatedBy),
		},
	}
	if input.LatestNotification != nil {
		latest, err := notificationFromGenerated(*input.LatestNotification)
		if err != nil {
			return NotificationSummary{}, err
		}
		out.LatestNotification = &latest
	}
	return out, nil
}

func optionalGeneratedString[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func notificationFromGenerated(input notificationscore.NotificationRecord) (Notification, error) {
	sequence, err := parseDecimalInt64(input.RecipientSequence, "notification sequence")
	if err != nil {
		return Notification{}, err
	}
	createdAt, err := parseGeneratedTime(input.CreatedAt, "notification created_at")
	if err != nil {
		return Notification{}, err
	}
	expiresAt, err := parseGeneratedOptionalTime(input.ExpiresAt, "notification expires_at")
	if err != nil {
		return Notification{}, err
	}
	readAt, err := parseGeneratedOptionalTime(input.ReadAt, "notification read_at")
	if err != nil {
		return Notification{}, err
	}
	dismissedAt, err := parseGeneratedOptionalTime(input.DismissedAt, "notification dismissed_at")
	if err != nil {
		return Notification{}, err
	}
	return Notification{
		NotificationID:     input.NotificationID,
		ResourceName:       input.ResourceName,
		OrgID:              input.OrgID,
		RecipientSubjectID: input.RecipientSubjectID,
		RecipientSequence:  sequence,
		Kind:               input.Kind,
		Priority:           NotificationPriority(input.Priority),
		Title:              input.Title,
		Body:               input.Body,
		ActionURL:          stringFromPointer(input.ActionURL),
		TargetResourceName: stringFromPointer(input.TargetResourceName),
		CreatedAt:          createdAt,
		ExpiresAt:          expiresAt,
		ReadAt:             readAt,
		DismissedAt:        dismissedAt,
	}, nil
}

func notificationAcceptedFromGenerated(input notificationscore.NotificationAccepted) NotificationAccepted {
	return NotificationAccepted{
		EventID:     input.EventID,
		Traceparent: stringFromPointer(input.Traceparent),
	}
}

func parseDecimalInt64(input string, field string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(input), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("verself sdk: invalid %s: %w", field, err)
	}
	return value, nil
}

func int32FromGenerated(input int64, field string) (int32, error) {
	const (
		minInt32 = -1 << 31
		maxInt32 = 1<<31 - 1
	)
	if input < minInt32 || input > maxInt32 {
		return 0, fmt.Errorf("verself sdk: %s is outside int32 range", field)
	}
	return int32(input), nil
}

func notificationsAPIError(operation string, statusCode int, model *notificationscore.ErrorModel, body []byte) error {
	var title *string
	var detail *string
	if model != nil {
		title = model.Title
		detail = model.Detail
	}
	return apiErrorFields("Notifications API", operation, statusCode, title, detail, body)
}
