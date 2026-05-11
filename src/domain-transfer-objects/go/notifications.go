package dto

import (
	"encoding/json"
	"time"
)

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
	Enabled      bool      `json:"enabled"`
	WebEnabled   bool      `json:"web_enabled"`
	EmailEnabled bool      `json:"email_enabled"`
	PushEnabled  bool      `json:"push_enabled"`
	SMSEnabled   bool      `json:"sms_enabled"`
	Version      int32     `json:"version" minimum:"0" maximum:"2147483647"`
	UpdatedAt    time.Time `json:"updated_at"`
	UpdatedBy    string    `json:"updated_by"`
}

type Notification struct {
	NotificationID     string       `json:"notification_id" doc:"Notification UUID."`
	ResourceName       ResourceName `json:"resourceName" doc:"Globally unique Verself resource name for this notification."`
	OrgID              string       `json:"org_id"`
	RecipientSubjectID string       `json:"recipient_subject_id"`
	RecipientSequence  string       `json:"recipient_sequence" doc:"Per-recipient sequence, decimal-encoded for JavaScript safety."`
	Kind               string       `json:"kind"`
	Priority           string       `json:"priority" enum:"low,normal,high"`
	Title              string       `json:"title"`
	Body               string       `json:"body"`
	ActionURL          string       `json:"action_url,omitempty"`
	TargetResourceName ResourceName `json:"targetResourceName,omitempty" doc:"Globally unique Verself resource name for the related product resource."`
	CreatedAt          time.Time    `json:"created_at"`
	ExpiresAt          *time.Time   `json:"expires_at,omitempty"`
	ReadAt             *time.Time   `json:"read_at,omitempty"`
	DismissedAt        *time.Time   `json:"dismissed_at,omitempty"`
}

type NotificationPutPreferencesRequest struct {
	Version      int32 `json:"version" minimum:"0" maximum:"2147483647"`
	Enabled      bool  `json:"enabled"`
	WebEnabled   *bool `json:"web_enabled,omitempty"`
	EmailEnabled *bool `json:"email_enabled,omitempty"`
	PushEnabled  *bool `json:"push_enabled,omitempty"`
	SMSEnabled   *bool `json:"sms_enabled,omitempty"`
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

type NotificationWorkflowRecipient struct {
	SubjectID string `json:"subject_id,omitempty" maxLength:"200"`
	Email     string `json:"email,omitempty" maxLength:"320"`
}

type NotificationWorkflowTriggerRequest struct {
	OrgID              string                          `json:"org_id" maxLength:"200"`
	Recipients         []NotificationWorkflowRecipient `json:"recipients" minItems:"1" maxItems:"100"`
	Title              string                          `json:"title,omitempty" maxLength:"120"`
	Body               string                          `json:"body,omitempty" maxLength:"500"`
	ActionURL          string                          `json:"action_url,omitempty" maxLength:"500"`
	Priority           string                          `json:"priority,omitempty" enum:"low,normal,high"`
	TargetResourceName ResourceName                    `json:"targetResourceName,omitempty" maxLength:"512"`
	Data               json.RawMessage                 `json:"data,omitempty"`
	Traceparent        string                          `json:"traceparent,omitempty" maxLength:"200"`
}

type NotificationWorkflowTriggerResponse struct {
	WorkflowRunID            string    `json:"workflow_run_id"`
	WorkflowKey              string    `json:"workflow_key"`
	OrgID                    string    `json:"org_id"`
	AcceptedAt               time.Time `json:"accepted_at"`
	RecipientCount           uint16    `json:"recipient_count" minimum:"0" maximum:"100"`
	WebNotificationsAccepted uint16    `json:"web_notifications_accepted" minimum:"0" maximum:"100"`
	EmailDeliveriesQueued    uint16    `json:"email_deliveries_queued" minimum:"0" maximum:"100"`
	SuppressedCount          uint16    `json:"suppressed_count" minimum:"0" maximum:"200"`
	Traceparent              string    `json:"traceparent,omitempty"`
}
