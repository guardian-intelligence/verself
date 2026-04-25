package mailstore

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/verself/mailbox-service/internal/jmap"
)

var unixEpoch = time.Unix(0, 0).UTC()

type Account struct {
	AccountID     string    `json:"account_id"`
	JMAPAccountID string    `json:"jmap_account_id"`
	EmailAddress  string    `json:"email_address"`
	DisplayName   string    `json:"display_name"`
	PrincipalType string    `json:"principal_type"`
	SyncedAt      time.Time `json:"synced_at"`
}

type Mailbox struct {
	AccountID     string    `json:"account_id"`
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	ParentID      string    `json:"parent_id"`
	Role          string    `json:"role"`
	SortOrder     int       `json:"sort_order"`
	TotalEmails   int       `json:"total_emails"`
	UnreadEmails  int       `json:"unread_emails"`
	TotalThreads  int       `json:"total_threads"`
	UnreadThreads int       `json:"unread_threads"`
	SyncedAt      time.Time `json:"synced_at"`
}

type Email struct {
	AccountID     string          `json:"account_id"`
	ID            string          `json:"id"`
	BlobID        string          `json:"blob_id"`
	ThreadID      string          `json:"thread_id"`
	MailboxIDs    []string        `json:"mailbox_ids"`
	Keywords      map[string]bool `json:"keywords"`
	Subject       string          `json:"subject"`
	FromName      string          `json:"from_name"`
	FromEmail     string          `json:"from_email"`
	ToList        []jmap.Address  `json:"to_list"`
	CcList        []jmap.Address  `json:"cc_list"`
	ReplyToList   []jmap.Address  `json:"reply_to_list"`
	Preview       string          `json:"preview"`
	HasAttachment bool            `json:"has_attachment"`
	Size          int             `json:"size"`
	ReceivedAt    time.Time       `json:"received_at"`
	SentAt        time.Time       `json:"sent_at"`
	IsSeen        bool            `json:"is_seen"`
	IsFlagged     bool            `json:"is_flagged"`
	IsAnswered    bool            `json:"is_answered"`
	IsDraft       bool            `json:"is_draft"`
	SyncedAt      time.Time       `json:"synced_at"`
}

type EmailBody struct {
	AccountID string    `json:"account_id"`
	EmailID   string    `json:"email_id"`
	TextBody  string    `json:"text_body"`
	HTMLBody  string    `json:"html_body"`
	FetchedAt time.Time `json:"fetched_at"`
}

type Thread struct {
	AccountID string    `json:"account_id"`
	ID        string    `json:"id"`
	EmailIDs  []string  `json:"email_ids"`
	SyncedAt  time.Time `json:"synced_at"`
}

type EmailSnapshot struct {
	AccountID  string
	EmailID    string
	ThreadID   string
	Keywords   map[string]bool
	MailboxIDs []string
}

func AccountFromDiscovery(accountID, jmapAccountID, emailAddress, displayName, principalType string, now time.Time) Account {
	return Account{
		AccountID:     strings.TrimSpace(accountID),
		JMAPAccountID: strings.TrimSpace(jmapAccountID),
		EmailAddress:  strings.TrimSpace(emailAddress),
		DisplayName:   strings.TrimSpace(displayName),
		PrincipalType: strings.TrimSpace(principalType),
		SyncedAt:      now.UTC(),
	}
}

func MailboxFromJMAP(accountID string, mailbox jmap.Mailbox, now time.Time) Mailbox {
	return Mailbox{
		AccountID:     accountID,
		ID:            mailbox.ID,
		Name:          mailbox.Name,
		ParentID:      mailbox.ParentID,
		Role:          mailbox.Role,
		SortOrder:     mailbox.SortOrder,
		TotalEmails:   mailbox.TotalEmails,
		UnreadEmails:  mailbox.UnreadEmails,
		TotalThreads:  mailbox.TotalThreads,
		UnreadThreads: mailbox.UnreadThreads,
		SyncedAt:      now.UTC(),
	}
}

func EmailFromJMAP(accountID string, email jmap.Email, now time.Time) Email {
	fromName := ""
	fromEmail := ""
	if len(email.From) > 0 {
		fromName = email.From[0].Name
		fromEmail = email.From[0].Email
	}

	return Email{
		AccountID:     accountID,
		ID:            email.ID,
		BlobID:        email.BlobID,
		ThreadID:      email.ThreadID,
		MailboxIDs:    mailboxIDs(email.MailboxIDs),
		Keywords:      cloneKeywords(email.Keywords),
		Subject:       email.Subject,
		FromName:      fromName,
		FromEmail:     fromEmail,
		ToList:        cloneAddresses(email.To),
		CcList:        cloneAddresses(email.Cc),
		ReplyToList:   cloneAddresses(email.ReplyTo),
		Preview:       email.Preview,
		HasAttachment: email.HasAttachment,
		Size:          email.Size,
		ReceivedAt:    parseTimestamp(email.ReceivedAt),
		SentAt:        parseTimestamp(email.SentAt),
		IsSeen:        email.Keywords["$seen"],
		IsFlagged:     email.Keywords["$flagged"],
		IsAnswered:    email.Keywords["$answered"],
		IsDraft:       email.Keywords["$draft"],
		SyncedAt:      now.UTC(),
	}
}

func EmailBodyFromJMAP(accountID string, email jmap.Email, now time.Time) EmailBody {
	return EmailBody{
		AccountID: accountID,
		EmailID:   email.ID,
		TextBody:  extractBody(email.TextBody, email.BodyValues),
		HTMLBody:  extractBody(email.HTMLBody, email.BodyValues),
		FetchedAt: now.UTC(),
	}
}

func ThreadFromJMAP(accountID string, thread jmap.Thread, now time.Time) Thread {
	return Thread{
		AccountID: accountID,
		ID:        thread.ID,
		EmailIDs:  append([]string(nil), thread.EmailIDs...),
		SyncedAt:  now.UTC(),
	}
}

func parseTimestamp(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return unixEpoch
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return unixEpoch
	}
	return parsed.UTC()
}

func mailboxIDs(ids map[string]bool) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	for id, enabled := range ids {
		if enabled {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

func cloneAddresses(list []jmap.Address) []jmap.Address {
	if len(list) == 0 {
		return nil
	}
	out := make([]jmap.Address, len(list))
	copy(out, list)
	return out
}

func cloneKeywords(keywords map[string]bool) map[string]bool {
	if len(keywords) == 0 {
		return map[string]bool{}
	}
	out := make(map[string]bool, len(keywords))
	for key, value := range keywords {
		out[key] = value
	}
	return out
}

func extractBody(parts []jmap.BodyPart, values map[string]jmap.BodyValue) string {
	var lines []string
	for _, part := range parts {
		text := strings.TrimSpace(values[part.PartID].Value)
		if text != "" {
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, "\n\n")
}

func MustJSON(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}
