package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/forge-metal/mailbox-service/internal/jmap"
	"github.com/forge-metal/mailbox-service/internal/mailstore"
)

type operatorAccountPathInput struct {
	AccountID string `path:"account_id"`
}

type operatorEmailPathInput struct {
	AccountID string `path:"account_id"`
	EmailID   string `path:"email_id"`
}

type operatorEmailListInput struct {
	AccountID string `path:"account_id"`
	Limit     int    `query:"limit"`
	MailboxID string `query:"mailbox_id"`
}

type operatorAccountsOutput struct {
	Body struct {
		Accounts []operatorAccount `json:"accounts"`
	}
}

type operatorMailboxesOutput struct {
	Body struct {
		Mailboxes []operatorMailbox `json:"mailboxes"`
	}
}

type operatorEmailsOutput struct {
	Body struct {
		Emails []operatorEmail `json:"emails"`
	}
}

type operatorEmailOutput struct {
	Body operatorEmailDetail `json:"body"`
}

type operatorAccount struct {
	AccountID     string `json:"account_id"`
	JMAPAccountID string `json:"jmap_account_id"`
	EmailAddress  string `json:"email_address"`
	DisplayName   string `json:"display_name"`
	PrincipalType string `json:"principal_type"`
	SyncedAt      string `json:"synced_at"`
}

type operatorMailbox struct {
	AccountID     string `json:"account_id"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	ParentID      string `json:"parent_id"`
	Role          string `json:"role"`
	SortOrder     int    `json:"sort_order"`
	TotalEmails   int    `json:"total_emails"`
	UnreadEmails  int    `json:"unread_emails"`
	TotalThreads  int    `json:"total_threads"`
	UnreadThreads int    `json:"unread_threads"`
	SyncedAt      string `json:"synced_at"`
}

type operatorAddress struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type operatorEmail struct {
	AccountID     string            `json:"account_id"`
	EmailID       string            `json:"email_id"`
	ThreadID      string            `json:"thread_id"`
	MailboxIDs    []string          `json:"mailbox_ids"`
	Keywords      map[string]bool   `json:"keywords"`
	Subject       string            `json:"subject"`
	FromName      string            `json:"from_name"`
	FromEmail     string            `json:"from_email"`
	To            []operatorAddress `json:"to"`
	Cc            []operatorAddress `json:"cc"`
	ReplyTo       []operatorAddress `json:"reply_to"`
	Preview       string            `json:"preview"`
	HasAttachment bool              `json:"has_attachment"`
	Size          int               `json:"size"`
	ReceivedAt    string            `json:"received_at"`
	SentAt        string            `json:"sent_at"`
	IsSeen        bool              `json:"is_seen"`
	IsFlagged     bool              `json:"is_flagged"`
	IsAnswered    bool              `json:"is_answered"`
	IsDraft       bool              `json:"is_draft"`
	SyncedAt      string            `json:"synced_at"`
}

type operatorEmailDetail struct {
	AccountID     string            `json:"account_id"`
	EmailID       string            `json:"email_id"`
	ThreadID      string            `json:"thread_id"`
	MailboxIDs    []string          `json:"mailbox_ids"`
	Keywords      map[string]bool   `json:"keywords"`
	Subject       string            `json:"subject"`
	FromName      string            `json:"from_name"`
	FromEmail     string            `json:"from_email"`
	To            []operatorAddress `json:"to"`
	Cc            []operatorAddress `json:"cc"`
	ReplyTo       []operatorAddress `json:"reply_to"`
	Preview       string            `json:"preview"`
	HasAttachment bool              `json:"has_attachment"`
	Size          int               `json:"size"`
	ReceivedAt    string            `json:"received_at"`
	SentAt        string            `json:"sent_at"`
	IsSeen        bool              `json:"is_seen"`
	IsFlagged     bool              `json:"is_flagged"`
	IsAnswered    bool              `json:"is_answered"`
	IsDraft       bool              `json:"is_draft"`
	SyncedAt      string            `json:"synced_at"`
	TextBody      string            `json:"text_body"`
	HTMLBody      string            `json:"html_body"`
	FetchedAt     string            `json:"fetched_at"`
}

func registerOperatorRoutes(api huma.API, svc provider) {
	huma.Register(api, huma.Operation{
		OperationID: "operator-list-accounts",
		Method:      http.MethodGet,
		Path:        "/internal/mailbox/v1/accounts",
		Summary:     "List synced mailbox accounts for operator tooling",
	}, operatorListAccounts(svc))

	huma.Register(api, huma.Operation{
		OperationID: "operator-list-mailboxes",
		Method:      http.MethodGet,
		Path:        "/internal/mailbox/v1/accounts/{account_id}/mailboxes",
		Summary:     "List synced mailboxes for an account",
	}, operatorListMailboxes(svc))

	huma.Register(api, huma.Operation{
		OperationID: "operator-list-emails",
		Method:      http.MethodGet,
		Path:        "/internal/mailbox/v1/accounts/{account_id}/emails",
		Summary:     "List synced emails for an account",
	}, operatorListEmails(svc))

	huma.Register(api, huma.Operation{
		OperationID: "operator-get-email",
		Method:      http.MethodGet,
		Path:        "/internal/mailbox/v1/accounts/{account_id}/emails/{email_id}",
		Summary:     "Read an email body for an account",
	}, operatorGetEmail(svc))
}

func operatorListAccounts(svc provider) func(context.Context, *mailboxServiceEmptyInput) (*operatorAccountsOutput, error) {
	return func(ctx context.Context, _ *mailboxServiceEmptyInput) (*operatorAccountsOutput, error) {
		accounts, err := svc.ListAccounts(ctx)
		if err != nil {
			return nil, toHumaError("list accounts", err)
		}
		out := &operatorAccountsOutput{}
		out.Body.Accounts = make([]operatorAccount, 0, len(accounts))
		for _, account := range accounts {
			out.Body.Accounts = append(out.Body.Accounts, toOperatorAccount(account))
		}
		return out, nil
	}
}

func operatorListMailboxes(svc provider) func(context.Context, *operatorAccountPathInput) (*operatorMailboxesOutput, error) {
	return func(ctx context.Context, input *operatorAccountPathInput) (*operatorMailboxesOutput, error) {
		mailboxes, err := svc.ListMailboxes(ctx, input.AccountID)
		if err != nil {
			return nil, toHumaError("list mailboxes", err)
		}
		out := &operatorMailboxesOutput{}
		out.Body.Mailboxes = make([]operatorMailbox, 0, len(mailboxes))
		for _, mailbox := range mailboxes {
			out.Body.Mailboxes = append(out.Body.Mailboxes, toOperatorMailbox(mailbox))
		}
		return out, nil
	}
}

func operatorListEmails(svc provider) func(context.Context, *operatorEmailListInput) (*operatorEmailsOutput, error) {
	return func(ctx context.Context, input *operatorEmailListInput) (*operatorEmailsOutput, error) {
		limit := input.Limit
		if limit <= 0 {
			limit = 10
		}
		emails, err := svc.ListEmails(ctx, input.AccountID, input.MailboxID, limit)
		if err != nil {
			return nil, toHumaError("list emails", err)
		}
		out := &operatorEmailsOutput{}
		out.Body.Emails = make([]operatorEmail, 0, len(emails))
		for _, email := range emails {
			out.Body.Emails = append(out.Body.Emails, toOperatorEmail(email))
		}
		return out, nil
	}
}

func operatorGetEmail(svc provider) func(context.Context, *operatorEmailPathInput) (*operatorEmailOutput, error) {
	return func(ctx context.Context, input *operatorEmailPathInput) (*operatorEmailOutput, error) {
		email, err := svc.GetEmail(ctx, input.AccountID, input.EmailID)
		if err != nil {
			return nil, toHumaError("get email", err)
		}
		body, err := svc.FetchEmailBody(ctx, input.AccountID, input.EmailID)
		if err != nil {
			return nil, toHumaError("fetch email body", err)
		}
		out := &operatorEmailOutput{}
		out.Body = toOperatorEmailDetail(email, body)
		return out, nil
	}
}

func toOperatorAccount(account mailstore.Account) operatorAccount {
	return operatorAccount{
		AccountID:     account.AccountID,
		JMAPAccountID: account.JMAPAccountID,
		EmailAddress:  account.EmailAddress,
		DisplayName:   account.DisplayName,
		PrincipalType: account.PrincipalType,
		SyncedAt:      formatTime(account.SyncedAt),
	}
}

func toOperatorMailbox(mailbox mailstore.Mailbox) operatorMailbox {
	return operatorMailbox{
		AccountID:     mailbox.AccountID,
		ID:            mailbox.ID,
		Name:          mailbox.Name,
		ParentID:      mailbox.ParentID,
		Role:          mailbox.Role,
		SortOrder:     mailbox.SortOrder,
		TotalEmails:   mailbox.TotalEmails,
		UnreadEmails:  mailbox.UnreadEmails,
		TotalThreads:  mailbox.TotalThreads,
		UnreadThreads: mailbox.UnreadThreads,
		SyncedAt:      formatTime(mailbox.SyncedAt),
	}
}

func toOperatorEmail(email mailstore.Email) operatorEmail {
	return operatorEmail{
		AccountID:     email.AccountID,
		EmailID:       email.ID,
		ThreadID:      email.ThreadID,
		MailboxIDs:    append([]string(nil), email.MailboxIDs...),
		Keywords:      cloneKeywordMap(email.Keywords),
		Subject:       email.Subject,
		FromName:      email.FromName,
		FromEmail:     email.FromEmail,
		To:            toOperatorAddresses(email.ToList),
		Cc:            toOperatorAddresses(email.CcList),
		ReplyTo:       toOperatorAddresses(email.ReplyToList),
		Preview:       email.Preview,
		HasAttachment: email.HasAttachment,
		Size:          email.Size,
		ReceivedAt:    formatTime(email.ReceivedAt),
		SentAt:        formatTime(email.SentAt),
		IsSeen:        email.IsSeen,
		IsFlagged:     email.IsFlagged,
		IsAnswered:    email.IsAnswered,
		IsDraft:       email.IsDraft,
		SyncedAt:      formatTime(email.SyncedAt),
	}
}

func toOperatorEmailDetail(email mailstore.Email, body mailstore.EmailBody) operatorEmailDetail {
	return operatorEmailDetail{
		AccountID:     email.AccountID,
		EmailID:       email.ID,
		ThreadID:      email.ThreadID,
		MailboxIDs:    append([]string(nil), email.MailboxIDs...),
		Keywords:      cloneKeywordMap(email.Keywords),
		Subject:       email.Subject,
		FromName:      email.FromName,
		FromEmail:     email.FromEmail,
		To:            toOperatorAddresses(email.ToList),
		Cc:            toOperatorAddresses(email.CcList),
		ReplyTo:       toOperatorAddresses(email.ReplyToList),
		Preview:       email.Preview,
		HasAttachment: email.HasAttachment,
		Size:          email.Size,
		ReceivedAt:    formatTime(email.ReceivedAt),
		SentAt:        formatTime(email.SentAt),
		IsSeen:        email.IsSeen,
		IsFlagged:     email.IsFlagged,
		IsAnswered:    email.IsAnswered,
		IsDraft:       email.IsDraft,
		SyncedAt:      formatTime(email.SyncedAt),
		TextBody:      body.TextBody,
		HTMLBody:      body.HTMLBody,
		FetchedAt:     formatTime(body.FetchedAt),
	}
}

func toOperatorAddresses(addresses []jmap.Address) []operatorAddress {
	if len(addresses) == 0 {
		return nil
	}
	out := make([]operatorAddress, 0, len(addresses))
	for _, address := range addresses {
		out = append(out, operatorAddress{Name: address.Name, Email: address.Email})
	}
	return out
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func cloneKeywordMap(keywords map[string]bool) map[string]bool {
	if len(keywords) == 0 {
		return map[string]bool{}
	}
	out := make(map[string]bool, len(keywords))
	for key, value := range keywords {
		out[key] = value
	}
	return out
}
