package mailstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("mailstore: not found")

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Close() {
	if s == nil || s.pool == nil {
		return
	}
	s.pool.Close()
}

func (s *Store) Ready(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("mailstore pool is nil")
	}
	return s.pool.Ping(ctx)
}

func (s *Store) ResolveBinding(ctx context.Context, subject string) (string, error) {
	var accountID string
	err := s.pool.QueryRow(ctx,
		`SELECT account_id FROM mailbox_bindings WHERE subject = $1`,
		subject,
	).Scan(&accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return accountID, nil
}

func (s *Store) LookupAccount(ctx context.Context, accountID string) (Account, error) {
	var account Account
	err := s.pool.QueryRow(ctx, `
		SELECT account_id, jmap_account_id, email_address, display_name, principal_type, synced_at
		FROM mailbox_accounts
		WHERE account_id = $1
	`, accountID).Scan(
		&account.AccountID,
		&account.JMAPAccountID,
		&account.EmailAddress,
		&account.DisplayName,
		&account.PrincipalType,
		&account.SyncedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	return account, nil
}

func (s *Store) ListAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT account_id, jmap_account_id, email_address, display_name, principal_type, synced_at
		FROM mailbox_accounts
		ORDER BY account_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []Account
	for rows.Next() {
		var account Account
		if err := rows.Scan(
			&account.AccountID,
			&account.JMAPAccountID,
			&account.EmailAddress,
			&account.DisplayName,
			&account.PrincipalType,
			&account.SyncedAt,
		); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (s *Store) GetSyncStates(ctx context.Context, accountID string) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT entity_type, jmap_state
		FROM sync_state
		WHERE account_id = $1
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var entityType string
		var state string
		if err := rows.Scan(&entityType, &state); err != nil {
			return nil, err
		}
		out[entityType] = state
	}
	return out, rows.Err()
}

func (s *Store) GetMailboxIDByRole(ctx context.Context, accountID, role string) (string, error) {
	var mailboxID string
	err := s.pool.QueryRow(ctx, `
		SELECT id
		FROM mailboxes
		WHERE account_id = $1 AND role = $2
	`, accountID, role).Scan(&mailboxID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return mailboxID, nil
}

func (s *Store) ListMailboxes(ctx context.Context, accountID string) ([]Mailbox, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT account_id, id, name, parent_id, role, sort_order, total_emails, unread_emails, total_threads, unread_threads, synced_at
		FROM mailboxes
		WHERE account_id = $1
		ORDER BY sort_order, name, id
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mailboxes []Mailbox
	for rows.Next() {
		var mailbox Mailbox
		if err := rows.Scan(
			&mailbox.AccountID,
			&mailbox.ID,
			&mailbox.Name,
			&mailbox.ParentID,
			&mailbox.Role,
			&mailbox.SortOrder,
			&mailbox.TotalEmails,
			&mailbox.UnreadEmails,
			&mailbox.TotalThreads,
			&mailbox.UnreadThreads,
			&mailbox.SyncedAt,
		); err != nil {
			return nil, err
		}
		mailboxes = append(mailboxes, mailbox)
	}
	return mailboxes, rows.Err()
}

func (s *Store) GetEmail(ctx context.Context, accountID, emailID string) (Email, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT account_id, id, blob_id, thread_id, subject, from_name, from_email, to_list, cc_list, reply_to_list,
		       preview, keywords, has_attachment, size, received_at, sent_at, is_seen, is_flagged, is_answered,
		       is_draft, synced_at
		FROM emails
		WHERE account_id = $1 AND id = $2
	`, accountID, emailID)
	email, err := scanEmail(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Email{}, ErrNotFound
		}
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

	var rows pgx.Rows
	var err error
	if mailboxID != "" {
		rows, err = s.pool.Query(ctx, `
			SELECT e.account_id, e.id, e.blob_id, e.thread_id, e.subject, e.from_name, e.from_email, e.to_list,
			       e.cc_list, e.reply_to_list, e.preview, e.keywords, e.has_attachment, e.size, e.received_at,
			       e.sent_at, e.is_seen, e.is_flagged, e.is_answered, e.is_draft, e.synced_at
			FROM emails e
			INNER JOIN email_mailboxes em
			        ON em.account_id = e.account_id
			       AND em.email_id = e.id
			WHERE e.account_id = $1 AND em.mailbox_id = $2
			ORDER BY e.received_at DESC, e.id DESC
			LIMIT $3
		`, accountID, mailboxID, limit)
	} else {
		rows, err = s.pool.Query(ctx, `
			SELECT account_id, id, blob_id, thread_id, subject, from_name, from_email, to_list, cc_list, reply_to_list,
			       preview, keywords, has_attachment, size, received_at, sent_at, is_seen, is_flagged, is_answered,
			       is_draft, synced_at
			FROM emails
			WHERE account_id = $1
			ORDER BY received_at DESC, id DESC
			LIMIT $2
		`, accountID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var emails []Email
	for rows.Next() {
		email, err := scanEmail(rows)
		if err != nil {
			return nil, err
		}
		emails = append(emails, email)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(emails) == 0 {
		return nil, nil
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
	var snapshot EmailSnapshot
	var rawKeywords []byte
	err := s.pool.QueryRow(ctx, `
		SELECT account_id, id, thread_id, keywords
		FROM emails
		WHERE account_id = $1 AND id = $2
	`, accountID, emailID).Scan(
		&snapshot.AccountID,
		&snapshot.EmailID,
		&snapshot.ThreadID,
		&rawKeywords,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return EmailSnapshot{}, ErrNotFound
	}
	if err != nil {
		return EmailSnapshot{}, err
	}
	if len(rawKeywords) > 0 {
		if err := json.Unmarshal(rawKeywords, &snapshot.Keywords); err != nil {
			return EmailSnapshot{}, err
		}
	}
	if snapshot.Keywords == nil {
		snapshot.Keywords = map[string]bool{}
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
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT thread_id
		FROM emails
		WHERE account_id = $1 AND id = ANY($2)
	`, accountID, emailIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var threadIDs []string
	for rows.Next() {
		var threadID string
		if err := rows.Scan(&threadID); err != nil {
			return nil, err
		}
		threadIDs = append(threadIDs, threadID)
	}
	return threadIDs, rows.Err()
}

func (s *Store) GetEmailBody(ctx context.Context, accountID, emailID string) (EmailBody, bool, error) {
	var body EmailBody
	err := s.pool.QueryRow(ctx, `
		SELECT account_id, email_id, text_body, html_body, fetched_at
		FROM email_bodies
		WHERE account_id = $1 AND email_id = $2
	`, accountID, emailID).Scan(
		&body.AccountID,
		&body.EmailID,
		&body.TextBody,
		&body.HTMLBody,
		&body.FetchedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return EmailBody{}, false, nil
	}
	if err != nil {
		return EmailBody{}, false, err
	}
	return body, true, nil
}

func (s *Store) listEmailMailboxIDs(ctx context.Context, accountID, emailID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT mailbox_id
		FROM email_mailboxes
		WHERE account_id = $1 AND email_id = $2
		ORDER BY mailbox_id
	`, accountID, emailID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mailboxIDs []string
	for rows.Next() {
		var mailboxID string
		if err := rows.Scan(&mailboxID); err != nil {
			return nil, err
		}
		mailboxIDs = append(mailboxIDs, mailboxID)
	}
	return mailboxIDs, rows.Err()
}

func (s *Store) listMailboxIDsForEmails(ctx context.Context, accountID string, emailIDs []string) (map[string][]string, error) {
	out := make(map[string][]string, len(emailIDs))
	if len(emailIDs) == 0 {
		return out, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT email_id, mailbox_id
		FROM email_mailboxes
		WHERE account_id = $1 AND email_id = ANY($2)
		ORDER BY email_id, mailbox_id
	`, accountID, emailIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var emailID string
		var mailboxID string
		if err := rows.Scan(&emailID, &mailboxID); err != nil {
			return nil, err
		}
		out[emailID] = append(out[emailID], mailboxID)
	}
	return out, rows.Err()
}

func scanEmail(row pgx.Row) (Email, error) {
	var email Email
	var rawTo []byte
	var rawCc []byte
	var rawReplyTo []byte
	var rawKeywords []byte
	err := row.Scan(
		&email.AccountID,
		&email.ID,
		&email.BlobID,
		&email.ThreadID,
		&email.Subject,
		&email.FromName,
		&email.FromEmail,
		&rawTo,
		&rawCc,
		&rawReplyTo,
		&email.Preview,
		&rawKeywords,
		&email.HasAttachment,
		&email.Size,
		&email.ReceivedAt,
		&email.SentAt,
		&email.IsSeen,
		&email.IsFlagged,
		&email.IsAnswered,
		&email.IsDraft,
		&email.SyncedAt,
	)
	if err != nil {
		return Email{}, err
	}
	if err := unmarshalAddresses(rawTo, &email.ToList); err != nil {
		return Email{}, err
	}
	if err := unmarshalAddresses(rawCc, &email.CcList); err != nil {
		return Email{}, err
	}
	if err := unmarshalAddresses(rawReplyTo, &email.ReplyToList); err != nil {
		return Email{}, err
	}
	if len(rawKeywords) > 0 {
		if err := json.Unmarshal(rawKeywords, &email.Keywords); err != nil {
			return Email{}, err
		}
	}
	if email.Keywords == nil {
		email.Keywords = map[string]bool{}
	}
	return email, nil
}

func unmarshalAddresses(raw []byte, out any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func emailIDsFromEmails(emails []Email) []string {
	ids := make([]string, 0, len(emails))
	for _, email := range emails {
		ids = append(ids, email.ID)
	}
	return ids
}

func (s *Store) PatchEmailKeywords(ctx context.Context, accountID, emailID string, keywords map[string]bool) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE emails
			SET keywords = $3::jsonb,
			    is_seen = $4,
			    is_flagged = $5,
			    is_answered = $6,
			    is_draft = $7,
			    synced_at = $8
			WHERE account_id = $1 AND id = $2
		`,
			accountID,
			emailID,
			MustJSON(keywords),
			keywords["$seen"],
			keywords["$flagged"],
			keywords["$answered"],
			keywords["$draft"],
			time.Now().UTC(),
		)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (s *Store) ReplaceEmailMailboxes(ctx context.Context, accountID, emailID string, mailboxIDs []string) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE emails
			SET synced_at = $3
			WHERE account_id = $1 AND id = $2
		`, accountID, emailID, time.Now().UTC())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return replaceEmailMailboxesTx(ctx, tx, accountID, emailID, mailboxIDs)
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
	return s.withTx(ctx, func(tx pgx.Tx) error {
		if err := upsertAccountTx(ctx, tx, account); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM email_mailboxes WHERE account_id = $1`, account.AccountID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM email_bodies WHERE account_id = $1`, account.AccountID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM emails WHERE account_id = $1`, account.AccountID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM threads WHERE account_id = $1`, account.AccountID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM mailboxes WHERE account_id = $1`, account.AccountID); err != nil {
			return err
		}

		for _, mailbox := range mailboxes {
			if err := upsertMailboxTx(ctx, tx, mailbox); err != nil {
				return err
			}
		}
		for _, email := range emails {
			if err := upsertEmailTx(ctx, tx, email); err != nil {
				return err
			}
			if err := replaceEmailMailboxesTx(ctx, tx, email.AccountID, email.ID, email.MailboxIDs); err != nil {
				return err
			}
		}
		for _, thread := range threads {
			if err := upsertThreadTx(ctx, tx, thread); err != nil {
				return err
			}
		}
		if err := touchSyncStateTx(ctx, tx, account.AccountID, "Mailbox", mailboxState); err != nil {
			return err
		}
		if err := touchSyncStateTx(ctx, tx, account.AccountID, "Email", emailState); err != nil {
			return err
		}
		if err := touchSyncStateTx(ctx, tx, account.AccountID, "Thread", threadState); err != nil {
			return err
		}
		return nil
	})
}

func (s *Store) ReplaceMailboxes(ctx context.Context, account Account, mailboxes []Mailbox, mailboxState string) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		if err := upsertAccountTx(ctx, tx, account); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM mailboxes WHERE account_id = $1`, account.AccountID); err != nil {
			return err
		}
		for _, mailbox := range mailboxes {
			if err := upsertMailboxTx(ctx, tx, mailbox); err != nil {
				return err
			}
		}
		return touchSyncStateTx(ctx, tx, account.AccountID, "Mailbox", mailboxState)
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
	return s.withTx(ctx, func(tx pgx.Tx) error {
		if len(destroyedIDs) > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM email_mailboxes WHERE account_id = $1 AND email_id = ANY($2)`, accountID, destroyedIDs); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM email_bodies WHERE account_id = $1 AND email_id = ANY($2)`, accountID, destroyedIDs); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM emails WHERE account_id = $1 AND id = ANY($2)`, accountID, destroyedIDs); err != nil {
				return err
			}
		}
		for _, email := range emails {
			if err := upsertEmailTx(ctx, tx, email); err != nil {
				return err
			}
			if err := replaceEmailMailboxesTx(ctx, tx, email.AccountID, email.ID, email.MailboxIDs); err != nil {
				return err
			}
		}

		if len(destroyedThreadIDs) > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM threads WHERE account_id = $1 AND id = ANY($2)`, accountID, destroyedThreadIDs); err != nil {
				return err
			}
		}
		for _, thread := range threads {
			if err := upsertThreadTx(ctx, tx, thread); err != nil {
				return err
			}
		}

		if emailState != "" {
			if err := touchSyncStateTx(ctx, tx, accountID, "Email", emailState); err != nil {
				return err
			}
		}
		if threadState != "" {
			if err := touchSyncStateTx(ctx, tx, accountID, "Thread", threadState); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) ApplyThreadChanges(ctx context.Context, accountID string, threads []Thread, destroyedIDs []string, threadState string) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		if len(destroyedIDs) > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM threads WHERE account_id = $1 AND id = ANY($2)`, accountID, destroyedIDs); err != nil {
				return err
			}
		}
		for _, thread := range threads {
			if err := upsertThreadTx(ctx, tx, thread); err != nil {
				return err
			}
		}
		return touchSyncStateTx(ctx, tx, accountID, "Thread", threadState)
	})
}

func (s *Store) UpsertEmailBundle(ctx context.Context, accountID string, emails []Email, threads []Thread, emailState string) error {
	return s.ApplyEmailChanges(ctx, accountID, emails, nil, threads, nil, emailState, "")
}

func (s *Store) UpsertEmailBody(ctx context.Context, body EmailBody) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO email_bodies (account_id, email_id, text_body, html_body, fetched_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (account_id, email_id) DO UPDATE
		SET text_body = EXCLUDED.text_body,
		    html_body = EXCLUDED.html_body,
		    fetched_at = EXCLUDED.fetched_at
	`, body.AccountID, body.EmailID, body.TextBody, body.HTMLBody, body.FetchedAt.UTC())
	return err
}

func (s *Store) withTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func upsertAccountTx(ctx context.Context, tx pgx.Tx, account Account) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO mailbox_accounts (account_id, jmap_account_id, email_address, display_name, principal_type, synced_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (account_id) DO UPDATE
		SET jmap_account_id = EXCLUDED.jmap_account_id,
		    email_address = EXCLUDED.email_address,
		    display_name = EXCLUDED.display_name,
		    principal_type = EXCLUDED.principal_type,
		    synced_at = EXCLUDED.synced_at
	`, account.AccountID, account.JMAPAccountID, account.EmailAddress, account.DisplayName, account.PrincipalType, account.SyncedAt.UTC())
	return err
}

func upsertMailboxTx(ctx context.Context, tx pgx.Tx, mailbox Mailbox) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO mailboxes (
			account_id, id, name, parent_id, role, sort_order,
			total_emails, unread_emails, total_threads, unread_threads, synced_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (account_id, id) DO UPDATE
		SET name = EXCLUDED.name,
		    parent_id = EXCLUDED.parent_id,
		    role = EXCLUDED.role,
		    sort_order = EXCLUDED.sort_order,
		    total_emails = EXCLUDED.total_emails,
		    unread_emails = EXCLUDED.unread_emails,
		    total_threads = EXCLUDED.total_threads,
		    unread_threads = EXCLUDED.unread_threads,
		    synced_at = EXCLUDED.synced_at
	`, mailbox.AccountID, mailbox.ID, mailbox.Name, mailbox.ParentID, mailbox.Role, mailbox.SortOrder, mailbox.TotalEmails, mailbox.UnreadEmails, mailbox.TotalThreads, mailbox.UnreadThreads, mailbox.SyncedAt.UTC())
	return err
}

func upsertEmailTx(ctx context.Context, tx pgx.Tx, email Email) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO emails (
			account_id, id, blob_id, thread_id, subject, from_name, from_email,
			to_list, cc_list, reply_to_list, preview, keywords, has_attachment,
			size, received_at, sent_at, is_seen, is_flagged, is_answered, is_draft, synced_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10::jsonb, $11, $12::jsonb, $13, $14, $15, $16, $17, $18, $19, $20, $21)
		ON CONFLICT (account_id, id) DO UPDATE
		SET blob_id = EXCLUDED.blob_id,
		    thread_id = EXCLUDED.thread_id,
		    subject = EXCLUDED.subject,
		    from_name = EXCLUDED.from_name,
		    from_email = EXCLUDED.from_email,
		    to_list = EXCLUDED.to_list,
		    cc_list = EXCLUDED.cc_list,
		    reply_to_list = EXCLUDED.reply_to_list,
		    preview = EXCLUDED.preview,
		    keywords = EXCLUDED.keywords,
		    has_attachment = EXCLUDED.has_attachment,
		    size = EXCLUDED.size,
		    received_at = EXCLUDED.received_at,
		    sent_at = EXCLUDED.sent_at,
		    is_seen = EXCLUDED.is_seen,
		    is_flagged = EXCLUDED.is_flagged,
		    is_answered = EXCLUDED.is_answered,
		    is_draft = EXCLUDED.is_draft,
		    synced_at = EXCLUDED.synced_at
	`,
		email.AccountID,
		email.ID,
		email.BlobID,
		email.ThreadID,
		email.Subject,
		email.FromName,
		email.FromEmail,
		MustJSON(email.ToList),
		MustJSON(email.CcList),
		MustJSON(email.ReplyToList),
		email.Preview,
		MustJSON(email.Keywords),
		email.HasAttachment,
		email.Size,
		email.ReceivedAt.UTC(),
		email.SentAt.UTC(),
		email.IsSeen,
		email.IsFlagged,
		email.IsAnswered,
		email.IsDraft,
		email.SyncedAt.UTC(),
	)
	return err
}

func replaceEmailMailboxesTx(ctx context.Context, tx pgx.Tx, accountID, emailID string, mailboxIDs []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM email_mailboxes WHERE account_id = $1 AND email_id = $2`, accountID, emailID); err != nil {
		return err
	}
	for _, mailboxID := range mailboxIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO email_mailboxes (account_id, email_id, mailbox_id)
			VALUES ($1, $2, $3)
			ON CONFLICT (account_id, email_id, mailbox_id) DO NOTHING
		`, accountID, emailID, mailboxID); err != nil {
			return err
		}
	}
	return nil
}

func upsertThreadTx(ctx context.Context, tx pgx.Tx, thread Thread) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO threads (account_id, id, email_ids, synced_at)
		VALUES ($1, $2, $3::jsonb, $4)
		ON CONFLICT (account_id, id) DO UPDATE
		SET email_ids = EXCLUDED.email_ids,
		    synced_at = EXCLUDED.synced_at
	`, thread.AccountID, thread.ID, MustJSON(thread.EmailIDs), thread.SyncedAt.UTC())
	return err
}

func touchSyncStateTx(ctx context.Context, tx pgx.Tx, accountID, entityType, state string) error {
	if state == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO sync_state (account_id, entity_type, jmap_state, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (account_id, entity_type) DO UPDATE
		SET jmap_state = EXCLUDED.jmap_state,
		    updated_at = EXCLUDED.updated_at
	`, accountID, entityType, state, time.Now().UTC())
	return err
}
