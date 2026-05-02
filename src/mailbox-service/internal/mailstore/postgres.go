package mailstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	dbstore "github.com/verself/mailbox-service/internal/store"
)

var ErrNotFound = errors.New("mailstore: not found")

type Store struct {
	pool    *pgxpool.Pool
	queries *dbstore.Queries
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{
		pool:    pool,
		queries: dbstore.New(pool),
	}
}

func (s *Store) Close() {
	if s == nil || s.pool == nil {
		return
	}
	s.pool.Close()
}

func (s *Store) Ready(ctx context.Context) error {
	if s == nil || s.pool == nil || s.queries == nil {
		return fmt.Errorf("mailstore pool is nil")
	}
	_, err := s.queries.Ping(ctx)
	return err
}

func (s *Store) ResolveBinding(ctx context.Context, subject string) (string, error) {
	accountID, err := s.queries.ResolveBinding(ctx, dbstore.ResolveBindingParams{Subject: subject})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return accountID, nil
}

func (s *Store) LookupAccount(ctx context.Context, accountID string) (Account, error) {
	account, err := s.queries.LookupAccount(ctx, dbstore.LookupAccountParams{AccountID: accountID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	return accountFromStore(account), nil
}

func (s *Store) ListAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.queries.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	accounts := make([]Account, 0, len(rows))
	for _, row := range rows {
		accounts = append(accounts, accountFromStore(row))
	}
	return accounts, nil
}

func (s *Store) GetSyncStates(ctx context.Context, accountID string) (map[string]string, error) {
	rows, err := s.queries.ListSyncStates(ctx, dbstore.ListSyncStatesParams{AccountID: accountID})
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, row := range rows {
		out[row.EntityType] = row.JmapState
	}
	return out, nil
}

func (s *Store) GetMailboxIDByRole(ctx context.Context, accountID, role string) (string, error) {
	mailboxID, err := s.queries.GetMailboxIDByRole(ctx, dbstore.GetMailboxIDByRoleParams{
		AccountID: accountID,
		Role:      role,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return mailboxID, nil
}

func (s *Store) ListMailboxes(ctx context.Context, accountID string) ([]Mailbox, error) {
	rows, err := s.queries.ListMailboxes(ctx, dbstore.ListMailboxesParams{AccountID: accountID})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	mailboxes := make([]Mailbox, 0, len(rows))
	for _, row := range rows {
		mailboxes = append(mailboxes, mailboxFromStore(row))
	}
	return mailboxes, nil
}

func (s *Store) GetEmail(ctx context.Context, accountID, emailID string) (Email, error) {
	row, err := s.queries.GetEmail(ctx, dbstore.GetEmailParams{
		AccountID: accountID,
		EmailID:   emailID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Email{}, ErrNotFound
	}
	if err != nil {
		return Email{}, err
	}
	email, err := emailFromStore(row)
	if err != nil {
		return Email{}, err
	}
	email.MailboxIDs, err = s.listEmailMailboxIDs(ctx, accountID, emailID)
	if err != nil {
		return Email{}, err
	}
	return email, nil
}

func (s *Store) ListEmails(ctx context.Context, accountID, mailboxID string, limit int) ([]Email, error) {
	if limit <= 0 {
		limit = 10
	}
	limitCount, err := int32FromInt("email list limit", limit)
	if err != nil {
		return nil, err
	}

	var rows []dbstore.Email
	if mailboxID != "" {
		rows, err = s.queries.ListEmailsByMailbox(ctx, dbstore.ListEmailsByMailboxParams{
			AccountID:  accountID,
			MailboxID:  mailboxID,
			LimitCount: limitCount,
		})
	} else {
		rows, err = s.queries.ListEmailsByAccount(ctx, dbstore.ListEmailsByAccountParams{
			AccountID:  accountID,
			LimitCount: limitCount,
		})
	}
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	emails := make([]Email, 0, len(rows))
	for _, row := range rows {
		email, err := emailFromStore(row)
		if err != nil {
			return nil, err
		}
		emails = append(emails, email)
	}

	mailboxIDsByEmail, err := s.listMailboxIDsForEmails(ctx, accountID, emailIDsFromEmails(emails))
	if err != nil {
		return nil, err
	}
	for i := range emails {
		emails[i].MailboxIDs = mailboxIDsByEmail[emails[i].ID]
	}
	return emails, nil
}

func (s *Store) GetEmailSnapshot(ctx context.Context, accountID, emailID string) (EmailSnapshot, error) {
	row, err := s.queries.GetEmailSnapshot(ctx, dbstore.GetEmailSnapshotParams{
		AccountID: accountID,
		EmailID:   emailID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return EmailSnapshot{}, ErrNotFound
	}
	if err != nil {
		return EmailSnapshot{}, err
	}
	snapshot, err := emailSnapshotFromStore(row)
	if err != nil {
		return EmailSnapshot{}, err
	}

	mailboxIDs, err := s.listEmailMailboxIDs(ctx, accountID, emailID)
	if err != nil {
		return EmailSnapshot{}, err
	}
	snapshot.MailboxIDs = mailboxIDs
	return snapshot, nil
}

func (s *Store) GetThreadIDsForEmails(ctx context.Context, accountID string, emailIDs []string) ([]string, error) {
	if len(emailIDs) == 0 {
		return nil, nil
	}
	threadIDs, err := s.queries.ListThreadIDsForEmails(ctx, dbstore.ListThreadIDsForEmailsParams{
		AccountID: accountID,
		EmailIds:  emailIDs,
	})
	if err != nil {
		return nil, err
	}
	if len(threadIDs) == 0 {
		return nil, nil
	}
	return threadIDs, nil
}

func (s *Store) GetEmailBody(ctx context.Context, accountID, emailID string) (EmailBody, bool, error) {
	row, err := s.queries.GetEmailBody(ctx, dbstore.GetEmailBodyParams{
		AccountID: accountID,
		EmailID:   emailID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return EmailBody{}, false, nil
	}
	if err != nil {
		return EmailBody{}, false, err
	}
	return bodyFromStore(row), true, nil
}

func (s *Store) listEmailMailboxIDs(ctx context.Context, accountID, emailID string) ([]string, error) {
	mailboxIDs, err := s.queries.ListEmailMailboxIDs(ctx, dbstore.ListEmailMailboxIDsParams{
		AccountID: accountID,
		EmailID:   emailID,
	})
	if err != nil {
		return nil, err
	}
	if len(mailboxIDs) == 0 {
		return nil, nil
	}
	return mailboxIDs, nil
}

func (s *Store) listMailboxIDsForEmails(ctx context.Context, accountID string, emailIDs []string) (map[string][]string, error) {
	out := make(map[string][]string, len(emailIDs))
	if len(emailIDs) == 0 {
		return out, nil
	}

	rows, err := s.queries.ListMailboxIDsForEmails(ctx, dbstore.ListMailboxIDsForEmailsParams{
		AccountID: accountID,
		EmailIds:  emailIDs,
	})
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.EmailID] = append(out[row.EmailID], row.MailboxID)
	}
	return out, nil
}

func emailIDsFromEmails(emails []Email) []string {
	ids := make([]string, 0, len(emails))
	for _, email := range emails {
		ids = append(ids, email.ID)
	}
	return ids
}

func (s *Store) PatchEmailKeywords(ctx context.Context, accountID, emailID string, keywords map[string]bool) error {
	return s.withTx(ctx, func(q *dbstore.Queries) error {
		rowsAffected, err := q.PatchEmailKeywords(ctx, dbstore.PatchEmailKeywordsParams{
			AccountID:  accountID,
			EmailID:    emailID,
			Keywords:   MustJSON(keywords),
			IsSeen:     keywords["$seen"],
			IsFlagged:  keywords["$flagged"],
			IsAnswered: keywords["$answered"],
			IsDraft:    keywords["$draft"],
			SyncedAt:   pgTime(time.Now().UTC()),
		})
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (s *Store) ReplaceEmailMailboxes(ctx context.Context, accountID, emailID string, mailboxIDs []string) error {
	return s.withTx(ctx, func(q *dbstore.Queries) error {
		rowsAffected, err := q.TouchEmailSyncedAt(ctx, dbstore.TouchEmailSyncedAtParams{
			AccountID: accountID,
			EmailID:   emailID,
			SyncedAt:  pgTime(time.Now().UTC()),
		})
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return ErrNotFound
		}
		return replaceEmailMailboxes(ctx, q, accountID, emailID, mailboxIDs)
	})
}

func (s *Store) ReplaceBootstrap(
	ctx context.Context,
	account Account,
	mailboxes []Mailbox,
	emails []Email,
	threads []Thread,
	mailboxState string,
	emailState string,
	threadState string,
) error {
	return s.withTx(ctx, func(q *dbstore.Queries) error {
		if err := upsertAccount(ctx, q, account); err != nil {
			return err
		}
		if err := q.DeleteEmailMailboxesForAccount(ctx, dbstore.DeleteEmailMailboxesForAccountParams{AccountID: account.AccountID}); err != nil {
			return err
		}
		if err := q.DeleteEmailBodiesForAccount(ctx, dbstore.DeleteEmailBodiesForAccountParams{AccountID: account.AccountID}); err != nil {
			return err
		}
		if err := q.DeleteEmailsForAccount(ctx, dbstore.DeleteEmailsForAccountParams{AccountID: account.AccountID}); err != nil {
			return err
		}
		if err := q.DeleteThreadsForAccount(ctx, dbstore.DeleteThreadsForAccountParams{AccountID: account.AccountID}); err != nil {
			return err
		}
		if err := q.DeleteMailboxesForAccount(ctx, dbstore.DeleteMailboxesForAccountParams{AccountID: account.AccountID}); err != nil {
			return err
		}

		for _, mailbox := range mailboxes {
			if err := upsertMailbox(ctx, q, mailbox); err != nil {
				return err
			}
		}
		for _, email := range emails {
			if err := upsertEmail(ctx, q, email); err != nil {
				return err
			}
			if err := replaceEmailMailboxes(ctx, q, email.AccountID, email.ID, email.MailboxIDs); err != nil {
				return err
			}
		}
		for _, thread := range threads {
			if err := upsertThread(ctx, q, thread); err != nil {
				return err
			}
		}
		if err := touchSyncState(ctx, q, account.AccountID, "Mailbox", mailboxState); err != nil {
			return err
		}
		if err := touchSyncState(ctx, q, account.AccountID, "Email", emailState); err != nil {
			return err
		}
		if err := touchSyncState(ctx, q, account.AccountID, "Thread", threadState); err != nil {
			return err
		}
		return nil
	})
}

func (s *Store) ReplaceMailboxes(ctx context.Context, account Account, mailboxes []Mailbox, mailboxState string) error {
	return s.withTx(ctx, func(q *dbstore.Queries) error {
		if err := upsertAccount(ctx, q, account); err != nil {
			return err
		}
		if err := q.DeleteMailboxesForAccount(ctx, dbstore.DeleteMailboxesForAccountParams{AccountID: account.AccountID}); err != nil {
			return err
		}
		for _, mailbox := range mailboxes {
			if err := upsertMailbox(ctx, q, mailbox); err != nil {
				return err
			}
		}
		return touchSyncState(ctx, q, account.AccountID, "Mailbox", mailboxState)
	})
}

func (s *Store) ApplyEmailChanges(
	ctx context.Context,
	accountID string,
	emails []Email,
	destroyedIDs []string,
	threads []Thread,
	destroyedThreadIDs []string,
	emailState string,
	threadState string,
) error {
	return s.withTx(ctx, func(q *dbstore.Queries) error {
		if len(destroyedIDs) > 0 {
			if err := q.DeleteEmailMailboxesForEmails(ctx, dbstore.DeleteEmailMailboxesForEmailsParams{AccountID: accountID, EmailIds: destroyedIDs}); err != nil {
				return err
			}
			if err := q.DeleteEmailBodiesForEmails(ctx, dbstore.DeleteEmailBodiesForEmailsParams{AccountID: accountID, EmailIds: destroyedIDs}); err != nil {
				return err
			}
			if err := q.DeleteEmailsByIDs(ctx, dbstore.DeleteEmailsByIDsParams{AccountID: accountID, EmailIds: destroyedIDs}); err != nil {
				return err
			}
		}
		for _, email := range emails {
			if err := upsertEmail(ctx, q, email); err != nil {
				return err
			}
			if err := replaceEmailMailboxes(ctx, q, email.AccountID, email.ID, email.MailboxIDs); err != nil {
				return err
			}
		}

		if len(destroyedThreadIDs) > 0 {
			if err := q.DeleteThreadsByIDs(ctx, dbstore.DeleteThreadsByIDsParams{AccountID: accountID, ThreadIds: destroyedThreadIDs}); err != nil {
				return err
			}
		}
		for _, thread := range threads {
			if err := upsertThread(ctx, q, thread); err != nil {
				return err
			}
		}

		if emailState != "" {
			if err := touchSyncState(ctx, q, accountID, "Email", emailState); err != nil {
				return err
			}
		}
		if threadState != "" {
			if err := touchSyncState(ctx, q, accountID, "Thread", threadState); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) ApplyThreadChanges(ctx context.Context, accountID string, threads []Thread, destroyedIDs []string, threadState string) error {
	return s.withTx(ctx, func(q *dbstore.Queries) error {
		if len(destroyedIDs) > 0 {
			if err := q.DeleteThreadsByIDs(ctx, dbstore.DeleteThreadsByIDsParams{AccountID: accountID, ThreadIds: destroyedIDs}); err != nil {
				return err
			}
		}
		for _, thread := range threads {
			if err := upsertThread(ctx, q, thread); err != nil {
				return err
			}
		}
		return touchSyncState(ctx, q, accountID, "Thread", threadState)
	})
}

func (s *Store) UpsertEmailBundle(ctx context.Context, accountID string, emails []Email, threads []Thread, emailState string) error {
	return s.ApplyEmailChanges(ctx, accountID, emails, nil, threads, nil, emailState, "")
}

func (s *Store) UpsertEmailBody(ctx context.Context, body EmailBody) error {
	return s.queries.UpsertEmailBody(ctx, dbstore.UpsertEmailBodyParams{
		AccountID: body.AccountID,
		EmailID:   body.EmailID,
		TextBody:  body.TextBody,
		HtmlBody:  body.HTMLBody,
		FetchedAt: pgTime(body.FetchedAt),
	})
}

func (s *Store) withTx(ctx context.Context, fn func(*dbstore.Queries) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(s.queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func upsertAccount(ctx context.Context, q *dbstore.Queries, account Account) error {
	return q.UpsertAccount(ctx, dbstore.UpsertAccountParams{
		AccountID:     account.AccountID,
		JmapAccountID: account.JMAPAccountID,
		EmailAddress:  account.EmailAddress,
		DisplayName:   account.DisplayName,
		PrincipalType: account.PrincipalType,
		SyncedAt:      pgTime(account.SyncedAt),
	})
}

func upsertMailbox(ctx context.Context, q *dbstore.Queries, mailbox Mailbox) error {
	sortOrder, err := int32FromInt("mailbox sort_order", mailbox.SortOrder)
	if err != nil {
		return err
	}
	totalEmails, err := int32FromInt("mailbox total_emails", mailbox.TotalEmails)
	if err != nil {
		return err
	}
	unreadEmails, err := int32FromInt("mailbox unread_emails", mailbox.UnreadEmails)
	if err != nil {
		return err
	}
	totalThreads, err := int32FromInt("mailbox total_threads", mailbox.TotalThreads)
	if err != nil {
		return err
	}
	unreadThreads, err := int32FromInt("mailbox unread_threads", mailbox.UnreadThreads)
	if err != nil {
		return err
	}

	return q.UpsertMailbox(ctx, dbstore.UpsertMailboxParams{
		AccountID:     mailbox.AccountID,
		MailboxID:     mailbox.ID,
		Name:          mailbox.Name,
		ParentID:      mailbox.ParentID,
		Role:          mailbox.Role,
		SortOrder:     sortOrder,
		TotalEmails:   totalEmails,
		UnreadEmails:  unreadEmails,
		TotalThreads:  totalThreads,
		UnreadThreads: unreadThreads,
		SyncedAt:      pgTime(mailbox.SyncedAt),
	})
}

func upsertEmail(ctx context.Context, q *dbstore.Queries, email Email) error {
	size, err := int32FromInt("email size", email.Size)
	if err != nil {
		return err
	}

	return q.UpsertEmail(ctx, dbstore.UpsertEmailParams{
		AccountID:     email.AccountID,
		EmailID:       email.ID,
		BlobID:        email.BlobID,
		ThreadID:      email.ThreadID,
		Subject:       email.Subject,
		FromName:      email.FromName,
		FromEmail:     email.FromEmail,
		ToList:        MustJSON(email.ToList),
		CcList:        MustJSON(email.CcList),
		ReplyToList:   MustJSON(email.ReplyToList),
		Preview:       email.Preview,
		Keywords:      MustJSON(email.Keywords),
		HasAttachment: email.HasAttachment,
		Size:          size,
		ReceivedAt:    pgTime(email.ReceivedAt),
		SentAt:        pgTime(email.SentAt),
		IsSeen:        email.IsSeen,
		IsFlagged:     email.IsFlagged,
		IsAnswered:    email.IsAnswered,
		IsDraft:       email.IsDraft,
		SyncedAt:      pgTime(email.SyncedAt),
	})
}

func replaceEmailMailboxes(ctx context.Context, q *dbstore.Queries, accountID, emailID string, mailboxIDs []string) error {
	if err := q.DeleteEmailMailboxesForEmail(ctx, dbstore.DeleteEmailMailboxesForEmailParams{
		AccountID: accountID,
		EmailID:   emailID,
	}); err != nil {
		return err
	}
	for _, mailboxID := range mailboxIDs {
		if err := q.UpsertEmailMailbox(ctx, dbstore.UpsertEmailMailboxParams{
			AccountID: accountID,
			EmailID:   emailID,
			MailboxID: mailboxID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func upsertThread(ctx context.Context, q *dbstore.Queries, thread Thread) error {
	return q.UpsertThread(ctx, dbstore.UpsertThreadParams{
		AccountID: thread.AccountID,
		ThreadID:  thread.ID,
		EmailIds:  MustJSON(thread.EmailIDs),
		SyncedAt:  pgTime(thread.SyncedAt),
	})
}

func touchSyncState(ctx context.Context, q *dbstore.Queries, accountID, entityType, state string) error {
	if state == "" {
		return nil
	}
	return q.TouchSyncState(ctx, dbstore.TouchSyncStateParams{
		AccountID:  accountID,
		EntityType: entityType,
		JmapState:  state,
		UpdatedAt:  pgTime(time.Now().UTC()),
	})
}

func accountFromStore(row dbstore.MailboxAccount) Account {
	return Account{
		AccountID:     row.AccountID,
		JMAPAccountID: row.JmapAccountID,
		EmailAddress:  row.EmailAddress,
		DisplayName:   row.DisplayName,
		PrincipalType: row.PrincipalType,
		SyncedAt:      timeFromPG(row.SyncedAt),
	}
}

func mailboxFromStore(row dbstore.Mailbox) Mailbox {
	return Mailbox{
		AccountID:     row.AccountID,
		ID:            row.ID,
		Name:          row.Name,
		ParentID:      row.ParentID,
		Role:          row.Role,
		SortOrder:     int(row.SortOrder),
		TotalEmails:   int(row.TotalEmails),
		UnreadEmails:  int(row.UnreadEmails),
		TotalThreads:  int(row.TotalThreads),
		UnreadThreads: int(row.UnreadThreads),
		SyncedAt:      timeFromPG(row.SyncedAt),
	}
}

func emailFromStore(row dbstore.Email) (Email, error) {
	email := Email{
		AccountID:     row.AccountID,
		ID:            row.ID,
		BlobID:        row.BlobID,
		ThreadID:      row.ThreadID,
		Subject:       row.Subject,
		FromName:      row.FromName,
		FromEmail:     row.FromEmail,
		Preview:       row.Preview,
		HasAttachment: row.HasAttachment,
		Size:          int(row.Size),
		ReceivedAt:    timeFromPG(row.ReceivedAt),
		SentAt:        timeFromPG(row.SentAt),
		IsSeen:        row.IsSeen,
		IsFlagged:     row.IsFlagged,
		IsAnswered:    row.IsAnswered,
		IsDraft:       row.IsDraft,
		SyncedAt:      timeFromPG(row.SyncedAt),
	}
	if err := unmarshalAddresses(row.ToList, &email.ToList); err != nil {
		return Email{}, err
	}
	if err := unmarshalAddresses(row.CcList, &email.CcList); err != nil {
		return Email{}, err
	}
	if err := unmarshalAddresses(row.ReplyToList, &email.ReplyToList); err != nil {
		return Email{}, err
	}
	if len(row.Keywords) > 0 {
		if err := json.Unmarshal(row.Keywords, &email.Keywords); err != nil {
			return Email{}, err
		}
	}
	if email.Keywords == nil {
		email.Keywords = map[string]bool{}
	}
	return email, nil
}

func emailSnapshotFromStore(row dbstore.GetEmailSnapshotRow) (EmailSnapshot, error) {
	snapshot := EmailSnapshot{
		AccountID: row.AccountID,
		EmailID:   row.ID,
		ThreadID:  row.ThreadID,
	}
	if len(row.Keywords) > 0 {
		if err := json.Unmarshal(row.Keywords, &snapshot.Keywords); err != nil {
			return EmailSnapshot{}, err
		}
	}
	if snapshot.Keywords == nil {
		snapshot.Keywords = map[string]bool{}
	}
	return snapshot, nil
}

func bodyFromStore(row dbstore.EmailBody) EmailBody {
	return EmailBody{
		AccountID: row.AccountID,
		EmailID:   row.EmailID,
		TextBody:  row.TextBody,
		HTMLBody:  row.HtmlBody,
		FetchedAt: timeFromPG(row.FetchedAt),
	}
}

func unmarshalAddresses(raw []byte, out any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func pgTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func timeFromPG(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func int32FromInt(name string, value int) (int32, error) {
	const (
		minInt32 = -1 << 31
		maxInt32 = 1<<31 - 1
	)
	if value < minInt32 || value > maxInt32 {
		return 0, fmt.Errorf("%s out of int32 range: %d", name, value)
	}
	return int32(value), nil
}
