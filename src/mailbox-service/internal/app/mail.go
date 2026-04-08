package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/forge-metal/mailbox-service/internal/jmap"
	"github.com/forge-metal/mailbox-service/internal/mailstore"
)

func (s *Service) ResolveBoundAccount(ctx context.Context, subject string) (string, error) {
	if s.store == nil {
		return "", fmt.Errorf("mailstore is not configured")
	}
	return s.store.ResolveBinding(ctx, subject)
}

func (s *Service) SetEmailSeen(ctx context.Context, accountID, emailID string, seen bool) error {
	current, client, account, err := s.currentEmail(ctx, accountID, emailID)
	if err != nil {
		return err
	}
	keywords := cloneKeywords(current.Keywords)
	if seen {
		keywords["$seen"] = true
	} else {
		delete(keywords, "$seen")
	}
	_, err = client.EmailSet(ctx, account.JMAPAccountID, map[string]map[string]any{
		emailID: {"keywords": keywords},
	})
	if err != nil {
		return err
	}
	if err := s.store.PatchEmailKeywords(ctx, accountID, emailID, keywords); err != nil {
		return err
	}
	snapshot, err := s.store.GetEmailSnapshot(ctx, accountID, emailID)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "mailbox-service: email keywords patched",
		"mailbox_account", accountID,
		"email_id", emailID,
		"keywords", keywords,
		"snapshot_keywords", snapshot.Keywords,
	)
	return nil
}

func (s *Service) SetEmailFlagged(ctx context.Context, accountID, emailID string, flagged bool) error {
	current, client, account, err := s.currentEmail(ctx, accountID, emailID)
	if err != nil {
		return err
	}
	keywords := cloneKeywords(current.Keywords)
	if flagged {
		keywords["$flagged"] = true
	} else {
		delete(keywords, "$flagged")
	}
	_, err = client.EmailSet(ctx, account.JMAPAccountID, map[string]map[string]any{
		emailID: {"keywords": keywords},
	})
	if err != nil {
		return err
	}
	if err := s.store.PatchEmailKeywords(ctx, accountID, emailID, keywords); err != nil {
		return err
	}
	snapshot, err := s.store.GetEmailSnapshot(ctx, accountID, emailID)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "mailbox-service: email keywords patched",
		"mailbox_account", accountID,
		"email_id", emailID,
		"keywords", keywords,
		"snapshot_keywords", snapshot.Keywords,
	)
	return nil
}

func (s *Service) MoveEmail(ctx context.Context, accountID, emailID, mailboxID string) error {
	current, client, account, err := s.currentEmail(ctx, accountID, emailID)
	if err != nil {
		slog.ErrorContext(ctx, "mailbox-service: move email current state lookup failed",
			"mailbox_account", accountID,
			"email_id", emailID,
			"target_mailbox_id", mailboxID,
			"error", err,
		)
		return err
	}
	slog.InfoContext(ctx, "mailbox-service: move email requested",
		"mailbox_account", accountID,
		"email_id", emailID,
		"current_mailbox_ids", current.MailboxIDs,
		"target_mailbox_id", mailboxID,
	)
	mailboxIDs := map[string]bool{mailboxID: true}
	_, err = client.EmailSet(ctx, account.JMAPAccountID, map[string]map[string]any{
		emailID: {"mailboxIds": mailboxIDs},
	})
	if err != nil {
		slog.ErrorContext(ctx, "mailbox-service: move email upstream mutation failed",
			"mailbox_account", accountID,
			"email_id", emailID,
			"target_mailbox_id", mailboxID,
			"error", err,
		)
		return err
	}
	if err := s.store.ReplaceEmailMailboxes(ctx, accountID, emailID, []string{mailboxID}); err != nil {
		slog.ErrorContext(ctx, "mailbox-service: move email local mailbox patch failed",
			"mailbox_account", accountID,
			"email_id", emailID,
			"target_mailbox_id", mailboxID,
			"error", err,
		)
		return err
	}
	snapshot, err := s.store.GetEmailSnapshot(ctx, accountID, emailID)
	if err != nil {
		slog.ErrorContext(ctx, "mailbox-service: move email snapshot reload failed",
			"mailbox_account", accountID,
			"email_id", emailID,
			"target_mailbox_id", mailboxID,
			"error", err,
		)
		return err
	}
	slog.InfoContext(ctx, "mailbox-service: email mailboxes patched",
		"mailbox_account", accountID,
		"email_id", emailID,
		"mailbox_ids", snapshot.MailboxIDs,
	)
	return nil
}

func (s *Service) TrashEmail(ctx context.Context, accountID, emailID string) error {
	current, client, account, err := s.currentEmail(ctx, accountID, emailID)
	if err != nil {
		slog.ErrorContext(ctx, "mailbox-service: trash email current state lookup failed",
			"mailbox_account", accountID,
			"email_id", emailID,
			"error", err,
		)
		return err
	}
	trashMailboxID, err := s.store.GetMailboxIDByRole(ctx, accountID, "trash")
	if err != nil {
		slog.ErrorContext(ctx, "mailbox-service: trash mailbox lookup failed",
			"mailbox_account", accountID,
			"email_id", emailID,
			"error", err,
		)
		return err
	}
	mailboxIDs := mailboxIDMap(current.MailboxIDs)
	for mailboxID := range mailboxIDs {
		delete(mailboxIDs, mailboxID)
	}
	mailboxIDs[trashMailboxID] = true
	_, err = client.EmailSet(ctx, account.JMAPAccountID, map[string]map[string]any{
		emailID: {"mailboxIds": mailboxIDs},
	})
	if err != nil {
		slog.ErrorContext(ctx, "mailbox-service: trash email upstream mutation failed",
			"mailbox_account", accountID,
			"email_id", emailID,
			"trash_mailbox_id", trashMailboxID,
			"error", err,
		)
		return err
	}
	if err := s.store.ReplaceEmailMailboxes(ctx, accountID, emailID, []string{trashMailboxID}); err != nil {
		slog.ErrorContext(ctx, "mailbox-service: trash email local mailbox patch failed",
			"mailbox_account", accountID,
			"email_id", emailID,
			"trash_mailbox_id", trashMailboxID,
			"error", err,
		)
		return err
	}
	snapshot, err := s.store.GetEmailSnapshot(ctx, accountID, emailID)
	if err != nil {
		slog.ErrorContext(ctx, "mailbox-service: trash email snapshot reload failed",
			"mailbox_account", accountID,
			"email_id", emailID,
			"trash_mailbox_id", trashMailboxID,
			"error", err,
		)
		return err
	}
	slog.InfoContext(ctx, "mailbox-service: email mailboxes patched",
		"mailbox_account", accountID,
		"email_id", emailID,
		"mailbox_ids", snapshot.MailboxIDs,
	)
	return nil
}

func (s *Service) FetchEmailBody(ctx context.Context, accountID, emailID string) (mailstore.EmailBody, error) {
	if body, ok, err := s.store.GetEmailBody(ctx, accountID, emailID); err != nil {
		return mailstore.EmailBody{}, err
	} else if ok {
		return body, nil
	}

	_, client, account, err := s.currentEmail(ctx, accountID, emailID)
	if err != nil {
		return mailstore.EmailBody{}, err
	}
	return s.refreshEmail(ctx, accountID, client, account, emailID, "", true)
}

func (s *Service) currentEmail(ctx context.Context, accountID, emailID string) (mailstore.EmailSnapshot, *jmap.Client, mailstore.Account, error) {
	if s.syncManager == nil {
		return mailstore.EmailSnapshot{}, nil, mailstore.Account{}, fmt.Errorf("sync manager is not configured")
	}
	client, account, err := s.syncManager.AccountClient(ctx, accountID)
	if err != nil {
		return mailstore.EmailSnapshot{}, nil, mailstore.Account{}, err
	}

	snapshot, err := s.store.GetEmailSnapshot(ctx, accountID, emailID)
	if err == nil {
		return snapshot, client, account, nil
	}
	if err != mailstore.ErrNotFound {
		return mailstore.EmailSnapshot{}, nil, mailstore.Account{}, err
	}

	result, err := client.EmailGet(ctx, account.JMAPAccountID, []string{emailID}, jmap.EmailGetOptions{})
	if err != nil {
		return mailstore.EmailSnapshot{}, nil, mailstore.Account{}, err
	}
	if len(result.List) == 0 {
		return mailstore.EmailSnapshot{}, nil, mailstore.Account{}, mailstore.ErrNotFound
	}
	if _, err := s.refreshEmail(ctx, accountID, client, account, emailID, result.State, false); err != nil {
		return mailstore.EmailSnapshot{}, nil, mailstore.Account{}, err
	}
	snapshot, err = s.store.GetEmailSnapshot(ctx, accountID, emailID)
	if err != nil {
		return mailstore.EmailSnapshot{}, nil, mailstore.Account{}, err
	}
	return snapshot, client, account, nil
}

func (s *Service) refreshEmail(ctx context.Context, accountID string, client *jmap.Client, account mailstore.Account, emailID, emailState string, includeBody bool) (mailstore.EmailBody, error) {
	result, err := client.EmailGet(ctx, account.JMAPAccountID, []string{emailID}, jmap.EmailGetOptions{
		FetchTextBodyValues: includeBody,
		FetchHTMLBodyValues: includeBody,
	})
	if err != nil {
		return mailstore.EmailBody{}, err
	}
	if len(result.List) == 0 {
		return mailstore.EmailBody{}, mailstore.ErrNotFound
	}

	if emailState == "" {
		emailState = result.State
	}

	now := resultStateTime()
	email := mailstore.EmailFromJMAP(accountID, result.List[0], now)
	var threads []mailstore.Thread
	if result.List[0].ThreadID != "" {
		threadResult, err := client.ThreadGet(ctx, account.JMAPAccountID, []string{result.List[0].ThreadID})
		if err != nil {
			return mailstore.EmailBody{}, err
		}
		for _, thread := range threadResult.List {
			threads = append(threads, mailstore.ThreadFromJMAP(accountID, thread, now))
		}
	}

	if err := s.store.UpsertEmailBundle(ctx, accountID, []mailstore.Email{email}, threads, emailState); err != nil {
		return mailstore.EmailBody{}, err
	}

	if !includeBody {
		return mailstore.EmailBody{}, nil
	}
	body := mailstore.EmailBodyFromJMAP(accountID, result.List[0], now)
	if err := s.store.UpsertEmailBody(ctx, body); err != nil {
		return mailstore.EmailBody{}, err
	}
	return body, nil
}

func cloneKeywords(keywords map[string]bool) map[string]bool {
	out := make(map[string]bool, len(keywords))
	for key, value := range keywords {
		out[key] = value
	}
	return out
}

func mailboxIDMap(mailboxIDs []string) map[string]bool {
	out := make(map[string]bool, len(mailboxIDs))
	for _, mailboxID := range mailboxIDs {
		out[mailboxID] = true
	}
	return out
}

func resultStateTime() time.Time {
	return time.Now().UTC()
}
