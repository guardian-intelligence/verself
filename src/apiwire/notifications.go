package apiwire

import "time"

type NotificationSummary struct {
	OrgID              string                  `json:"org_id" doc:"Active organization ID from the validated token."`
	SubjectID          string                  `json:"subject_id" doc:"Zitadel human subject ID."`
	UnreadCount        uint16                  `json:"unread_count" minimum:"0" maximum:"999"`
	LatestSequence     string                  `json:"latest_sequence" doc:"Latest notification sequence, decimal-encoded for JavaScript safety."`
	ReadUpToSequence   string                  `json:"read_up_to_sequence" doc:"Read cursor sequence, decimal-encoded for JavaScript safety."`
	Preferences        NotificationPreferences `json:"preferences"`
	LatestNotification *Notification           `json:"latest_notification,omitempty"`
}

type NotificationList struct {
	Summary       NotificationSummary `json:"summary"`
	Notifications []Notification      `json:"notifications"`
}

type NotificationPreferences struct {
	Enabled   bool      `json:"enabled"`
	Version   int32     `json:"version" minimum:"0" maximum:"2147483647"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by"`
}

type Notification struct {
	NotificationID     string     `json:"notification_id" doc:"Notification UUID."`
	OrgID              string     `json:"org_id"`
	RecipientSubjectID string     `json:"recipient_subject_id"`
	RecipientSequence  string     `json:"recipient_sequence" doc:"Per-recipient sequence, decimal-encoded for JavaScript safety."`
	Kind               string     `json:"kind"`
	Priority           string     `json:"priority" enum:"low,normal,high"`
	Title              string     `json:"title"`
	Body               string     `json:"body"`
	ActionURL          string     `json:"action_url,omitempty"`
	ResourceKind       string     `json:"resource_kind,omitempty"`
	ResourceID         string     `json:"resource_id,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	DismissedAt        *time.Time `json:"dismissed_at,omitempty"`
}

type NotificationPutPreferencesRequest struct {
	Version int32 `json:"version" minimum:"0" maximum:"2147483647"`
	Enabled bool  `json:"enabled"`
}

type NotificationMarkReadRequest struct {
	ReadUpToSequence string `json:"read_up_to_sequence" required:"true" pattern:"^[0-9]+$"`
}

type NotificationTestRequest struct {
	Title     string `json:"title,omitempty" maxLength:"120"`
	Body      string `json:"body,omitempty" maxLength:"500"`
	ActionURL string `json:"action_url,omitempty" maxLength:"500"`
}

type NotificationAccepted struct {
	EventID     string `json:"event_id"`
	Traceparent string `json:"traceparent,omitempty"`
}
