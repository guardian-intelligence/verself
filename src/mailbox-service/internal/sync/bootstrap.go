package sync

import (
	"context"
	"time"

	"github.com/forge-metal/mailbox-service/internal/jmap"
	"github.com/forge-metal/mailbox-service/internal/mailstore"
)

const emailBatchSize = 100

func (w *Worker) fullSync(ctx context.Context, client *jmap.Client, principal DiscoveryPrincipal) error {
	jmapAccountID, err := client.AccountID(ctx)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	account := mailstore.AccountFromDiscovery(
		principal.AccountID,
		jmapAccountID,
		principal.EmailAddress,
		principal.DisplayName,
		principal.PrincipalType,
		now,
	)

	mailboxResult, err := client.MailboxGet(ctx, jmapAccountID, nil)
	if err != nil {
		return err
	}
	mailboxes := make([]mailstore.Mailbox, 0, len(mailboxResult.List))
	for _, mailbox := range mailboxResult.List {
		mailboxes = append(mailboxes, mailstore.MailboxFromJMAP(principal.AccountID, mailbox, now))
	}

	var allIDs []string
	emailState := ""
	for position := 0; ; {
		queryResult, err := client.EmailQuery(ctx, jmapAccountID, position, emailBatchSize)
		if err != nil {
			return err
		}
		emailState = queryResult.QueryState
		if len(queryResult.IDs) == 0 {
			break
		}
		allIDs = append(allIDs, queryResult.IDs...)
		if len(queryResult.IDs) < emailBatchSize {
			break
		}
		position += len(queryResult.IDs)
	}

	var emails []mailstore.Email
	threadIDs := map[string]struct{}{}
	for _, ids := range batches(allIDs, emailBatchSize) {
		emailResult, err := client.EmailGet(ctx, jmapAccountID, ids, jmap.EmailGetOptions{})
		if err != nil {
			return err
		}
		if emailState == "" {
			emailState = emailResult.State
		}
		for _, email := range emailResult.List {
			emails = append(emails, mailstore.EmailFromJMAP(principal.AccountID, email, now))
			if email.ThreadID != "" {
				threadIDs[email.ThreadID] = struct{}{}
			}
		}
	}

	threadState := emailState
	var threads []mailstore.Thread
	threadIDList := mapKeys(threadIDs)
	for _, ids := range batches(threadIDList, emailBatchSize) {
		threadResult, err := client.ThreadGet(ctx, jmapAccountID, ids)
		if err != nil {
			return err
		}
		threadState = threadResult.State
		for _, thread := range threadResult.List {
			threads = append(threads, mailstore.ThreadFromJMAP(principal.AccountID, thread, now))
		}
	}

	if err := w.cfg.Store.ReplaceBootstrap(ctx, account, mailboxes, emails, threads, mailboxResult.State, emailState, threadState); err != nil {
		return err
	}
	w.markSynced(now)
	w.cfg.Logger.InfoContext(ctx, "mailbox-service: sync worker bootstrap completed",
		"mailboxes", len(mailboxes),
		"emails", len(emails),
		"threads", len(threads),
	)
	return nil
}
