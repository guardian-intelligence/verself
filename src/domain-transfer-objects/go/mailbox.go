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
