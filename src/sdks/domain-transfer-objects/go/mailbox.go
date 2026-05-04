package dto

import "time"

type MailboxHealth struct {
	Status string `json:"status"`
}

type MailboxMoveRequest struct {
	MailboxID string `json:"mailbox_id" required:"true"`
}

type MailboxMutation struct {
	Status  string `json:"status"`
	EmailID string `json:"email_id"`
}

type MailboxBody struct {
	AccountID string `json:"account_id"`
	EmailID   string `json:"email_id"`
	TextBody  string `json:"text_body"`
	HTMLBody  string `json:"html_body"`
	FetchedAt string `json:"fetched_at"`
}

type MailboxAccount struct {
	AccountID        string `json:"account_id"`
	EmailAddress     string `json:"email_address"`
	DisplayName      string `json:"display_name"`
	DefaultMailboxID string `json:"default_mailbox_id,omitempty"`
}

type MailboxServiceStatusResponse struct {
	Status MailboxServiceStatus `json:"status"`
}

type MailboxServiceStatus struct {
	StartedAt       time.Time        `json:"started_at"`
	StalwartBaseURL string           `json:"stalwart_base_url"`
	PublicBaseURL   string           `json:"public_base_url"`
	Forwarder       MailboxForwarder `json:"forwarder"`
	MailboxSync     MailboxSync      `json:"mailbox_sync"`
}

type MailboxForwarder struct {
	Enabled                 bool       `json:"enabled"`
	Running                 bool       `json:"running"`
	Mailbox                 string     `json:"mailbox"`
	ForwardTargetConfigured bool       `json:"forward_target_configured"`
	LastError               string     `json:"last_error,omitempty"`
	LastSyncAt              *time.Time `json:"last_sync_at,omitempty"`
	LastForwardedAt         *time.Time `json:"last_forwarded_at,omitempty"`
	LastForwardedEmailID    string     `json:"last_forwarded_email_id,omitempty"`
}

type MailboxSync struct {
	Running         bool                                `json:"running"`
	LastDiscoveryAt *time.Time                          `json:"last_discovery_at,omitempty"`
	LastError       string                              `json:"last_error,omitempty"`
	Accounts        map[string]MailboxSyncAccountStatus `json:"accounts"`
}

type MailboxSyncAccountStatus struct {
	AccountID       string     `json:"account_id"`
	Running         bool       `json:"running"`
	Connected       bool       `json:"connected"`
	LastSyncAt      *time.Time `json:"last_sync_at,omitempty"`
	LastEventAt     *time.Time `json:"last_event_at,omitempty"`
	LastConnectedAt *time.Time `json:"last_connected_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
}

type MailboxOperatorAccounts struct {
	Accounts []MailboxOperatorAccount `json:"accounts"`
}

type MailboxOperatorMailboxes struct {
	Mailboxes []MailboxOperatorMailbox `json:"mailboxes"`
}

type MailboxOperatorEmails struct {
	Emails []MailboxOperatorEmail `json:"emails"`
}

type MailboxOperatorAccount struct {
	AccountID     string `json:"account_id"`
	JMAPAccountID string `json:"jmap_account_id"`
	EmailAddress  string `json:"email_address"`
	DisplayName   string `json:"display_name"`
	PrincipalType string `json:"principal_type"`
	SyncedAt      string `json:"synced_at"`
}

type MailboxOperatorMailbox struct {
	AccountID     string `json:"account_id"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	ParentID      string `json:"parent_id"`
	Role          string `json:"role"`
	SortOrder     int    `json:"sort_order" minimum:"0" maximum:"9007199254740991"`
	TotalEmails   int    `json:"total_emails" minimum:"0" maximum:"9007199254740991"`
	UnreadEmails  int    `json:"unread_emails" minimum:"0" maximum:"9007199254740991"`
	TotalThreads  int    `json:"total_threads" minimum:"0" maximum:"9007199254740991"`
	UnreadThreads int    `json:"unread_threads" minimum:"0" maximum:"9007199254740991"`
	SyncedAt      string `json:"synced_at"`
}

type MailboxOperatorAddress struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type MailboxOperatorEmail struct {
	AccountID     string                   `json:"account_id"`
	EmailID       string                   `json:"email_id"`
	ThreadID      string                   `json:"thread_id"`
	MailboxIDs    []string                 `json:"mailbox_ids"`
	Keywords      map[string]bool          `json:"keywords"`
	Subject       string                   `json:"subject"`
	FromName      string                   `json:"from_name"`
	FromEmail     string                   `json:"from_email"`
	To            []MailboxOperatorAddress `json:"to"`
	Cc            []MailboxOperatorAddress `json:"cc"`
	ReplyTo       []MailboxOperatorAddress `json:"reply_to"`
	Preview       string                   `json:"preview"`
	HasAttachment bool                     `json:"has_attachment"`
	Size          int                      `json:"size" minimum:"0" maximum:"9007199254740991"`
	ReceivedAt    string                   `json:"received_at"`
	SentAt        string                   `json:"sent_at"`
	IsSeen        bool                     `json:"is_seen"`
	IsFlagged     bool                     `json:"is_flagged"`
	IsAnswered    bool                     `json:"is_answered"`
	IsDraft       bool                     `json:"is_draft"`
	SyncedAt      string                   `json:"synced_at"`
}

type MailboxOperatorEmailDetail struct {
	AccountID     string                   `json:"account_id"`
	EmailID       string                   `json:"email_id"`
	ThreadID      string                   `json:"thread_id"`
	MailboxIDs    []string                 `json:"mailbox_ids"`
	Keywords      map[string]bool          `json:"keywords"`
	Subject       string                   `json:"subject"`
	FromName      string                   `json:"from_name"`
	FromEmail     string                   `json:"from_email"`
	To            []MailboxOperatorAddress `json:"to"`
	Cc            []MailboxOperatorAddress `json:"cc"`
	ReplyTo       []MailboxOperatorAddress `json:"reply_to"`
	Preview       string                   `json:"preview"`
	HasAttachment bool                     `json:"has_attachment"`
	Size          int                      `json:"size" minimum:"0" maximum:"9007199254740991"`
	ReceivedAt    string                   `json:"received_at"`
	SentAt        string                   `json:"sent_at"`
	IsSeen        bool                     `json:"is_seen"`
	IsFlagged     bool                     `json:"is_flagged"`
	IsAnswered    bool                     `json:"is_answered"`
	IsDraft       bool                     `json:"is_draft"`
	SyncedAt      string                   `json:"synced_at"`
	TextBody      string                   `json:"text_body"`
	HTMLBody      string                   `json:"html_body"`
	FetchedAt     string                   `json:"fetched_at"`
}
