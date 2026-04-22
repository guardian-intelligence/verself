package sync

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/forge-metal/mailbox-service/internal/jmap"
	"github.com/forge-metal/mailbox-service/internal/mailstore"
)

func (w *Worker) applyPendingChanges(ctx context.Context, client *jmap.Client, principal DiscoveryPrincipal, reason string) error {
	account, err := w.cfg.Store.LookupAccount(ctx, principal.AccountID)
	if err != nil {
		return w.fullSync(ctx, client, principal)
	}

	states, err := w.cfg.Store.GetSyncStates(ctx, principal.AccountID)
	if err != nil {
		return err
	}
	if states["Mailbox"] == "" || states["Email"] == "" || states["Thread"] == "" {
		return w.fullSync(ctx, client, principal)
	}

	if err := w.applyMailboxChanges(ctx, client, principal, account, states["Mailbox"]); err != nil {
		return fmt.Errorf("%s mailbox changes: %w", reason, err)
	}
	if err := w.applyEmailChanges(ctx, client, principal, account, states["Email"]); err != nil {
		return fmt.Errorf("%s email changes: %w", reason, err)
	}
	if err := w.applyThreadChanges(ctx, client, principal, account, states["Thread"]); err != nil {
		return fmt.Errorf("%s thread changes: %w", reason, err)
	}

	w.markSynced(time.Now().UTC())
	return nil
}

func (w *Worker) applyMailboxChanges(ctx context.Context, client *jmap.Client, principal DiscoveryPrincipal, account mailstore.Account, sinceState string) error {
	changes, err := client.MailboxChanges(ctx, account.JMAPAccountID, sinceState)
	if err != nil {
		if shouldResync(err) {
			return w.fullSync(ctx, client, principal)
		}
		return err
	}
	if changes.HasMoreChanges {
		return w.fullSync(ctx, client, principal)
	}
	if changes.NewState == sinceState && len(changes.Created) == 0 && len(changes.Updated) == 0 && len(changes.Destroyed) == 0 {
		return nil
	}

	full, err := mailboxGetAtState(ctx, client, account.JMAPAccountID, nil, changes.NewState)
	if err != nil {
		if shouldResyncStateRead(err) {
			w.cfg.Logger.InfoContext(ctx, "mailbox-service: mailbox state advanced during change application, falling back to full sync",
				"mailbox_account", principal.AccountID,
				"since_state", sinceState,
				"expected_state", changes.NewState,
				"error", err,
			)
			return w.fullSync(ctx, client, principal)
		}
		return err
	}
	now := time.Now().UTC()
	account = mailstore.AccountFromDiscovery(principal.AccountID, account.JMAPAccountID, principal.EmailAddress, principal.DisplayName, principal.PrincipalType, now)
	mailboxes := make([]mailstore.Mailbox, 0, len(full.List))
	for _, mailbox := range full.List {
		mailboxes = append(mailboxes, mailstore.MailboxFromJMAP(principal.AccountID, mailbox, now))
	}
	return w.cfg.Store.ReplaceMailboxes(ctx, account, mailboxes, full.State)
}

func (w *Worker) applyEmailChanges(ctx context.Context, client *jmap.Client, principal DiscoveryPrincipal, account mailstore.Account, sinceState string) error {
	changes, err := client.EmailChanges(ctx, account.JMAPAccountID, sinceState)
	if err != nil {
		if shouldResync(err) {
			return w.fullSync(ctx, client, principal)
		}
		return err
	}
	if changes.HasMoreChanges {
		return w.fullSync(ctx, client, principal)
	}
	if changes.NewState == sinceState && len(changes.Created) == 0 && len(changes.Updated) == 0 && len(changes.Destroyed) == 0 {
		return nil
	}

	changedIDs := uniqueStrings(append(append([]string{}, changes.Created...), changes.Updated...))
	oldThreadIDs, err := w.cfg.Store.GetThreadIDsForEmails(ctx, principal.AccountID, changes.Destroyed)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	var emails []mailstore.Email
	threadIDs := append([]string{}, oldThreadIDs...)
	for _, ids := range batches(changedIDs, emailBatchSize) {
		result, err := emailGetAtState(ctx, client, account.JMAPAccountID, ids, jmap.EmailGetOptions{}, changes.NewState)
		if err != nil {
			if shouldResyncStateRead(err) {
				w.cfg.Logger.InfoContext(ctx, "mailbox-service: email state advanced during change application, falling back to full sync",
					"mailbox_account", principal.AccountID,
					"since_state", sinceState,
					"expected_state", changes.NewState,
					"error", err,
				)
				return w.fullSync(ctx, client, principal)
			}
			return err
		}
		for _, email := range result.List {
			emails = append(emails, mailstore.EmailFromJMAP(principal.AccountID, email, now))
			if email.ThreadID != "" {
				threadIDs = append(threadIDs, email.ThreadID)
			}
		}
	}

	var threads []mailstore.Thread
	threadState := ""
	for _, ids := range batches(uniqueStrings(threadIDs), emailBatchSize) {
		result, err := client.ThreadGet(ctx, account.JMAPAccountID, ids)
		if err != nil {
			return err
		}
		threadState = result.State
		for _, thread := range result.List {
			threads = append(threads, mailstore.ThreadFromJMAP(principal.AccountID, thread, now))
		}
	}

	if err := w.cfg.Store.ApplyEmailChanges(ctx, principal.AccountID, emails, changes.Destroyed, threads, nil, changes.NewState, threadState); err != nil {
		return err
	}
	w.cfg.Logger.InfoContext(ctx, "mailbox-service: email changes applied",
		"mailbox_account", principal.AccountID,
		"state", changes.NewState,
		"upserted_emails", len(emails),
		"destroyed_emails", len(changes.Destroyed),
		"upserted_threads", len(threads),
	)
	return nil
}

func (w *Worker) applyThreadChanges(ctx context.Context, client *jmap.Client, principal DiscoveryPrincipal, account mailstore.Account, sinceState string) error {
	changes, err := client.ThreadChanges(ctx, account.JMAPAccountID, sinceState)
	if err != nil {
		if shouldResync(err) {
			return w.fullSync(ctx, client, principal)
		}
		return err
	}
	if changes.HasMoreChanges {
		return w.fullSync(ctx, client, principal)
	}
	if changes.NewState == sinceState && len(changes.Created) == 0 && len(changes.Updated) == 0 && len(changes.Destroyed) == 0 {
		return nil
	}

	now := time.Now().UTC()
	var threads []mailstore.Thread
	for _, ids := range batches(uniqueStrings(append(append([]string{}, changes.Created...), changes.Updated...)), emailBatchSize) {
		result, err := threadGetAtState(ctx, client, account.JMAPAccountID, ids, changes.NewState)
		if err != nil {
			if shouldResyncStateRead(err) {
				w.cfg.Logger.InfoContext(ctx, "mailbox-service: thread state advanced during change application, falling back to full sync",
					"mailbox_account", principal.AccountID,
					"since_state", sinceState,
					"expected_state", changes.NewState,
					"error", err,
				)
				return w.fullSync(ctx, client, principal)
			}
			return err
		}
		for _, thread := range result.List {
			threads = append(threads, mailstore.ThreadFromJMAP(principal.AccountID, thread, now))
		}
	}
	return w.cfg.Store.ApplyThreadChanges(ctx, principal.AccountID, threads, changes.Destroyed, changes.NewState)
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}

func batches(values []string, size int) [][]string {
	if len(values) == 0 {
		return nil
	}
	var out [][]string
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		out = append(out, values[start:end])
	}
	return out
}

func mapKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	slices.Sort(out)
	return out
}

const (
	stateReadTimeout = 3 * time.Second
	stateReadBackoff = 150 * time.Millisecond
)

type stateReadMismatchError struct {
	Expected string
	LastSeen string
}

func (e *stateReadMismatchError) Error() string {
	return fmt.Sprintf("timed out waiting for state %s, last seen %s", e.Expected, e.LastSeen)
}

func shouldResyncStateRead(err error) bool {
	var mismatch *stateReadMismatchError
	return errors.As(err, &mismatch)
}

func mailboxGetAtState(ctx context.Context, client *jmap.Client, accountID string, ids []string, expectedState string) (jmap.MailboxGetResult, error) {
	return waitForState(ctx, expectedState, func(callCtx context.Context) (jmap.MailboxGetResult, string, error) {
		result, err := client.MailboxGet(callCtx, accountID, ids)
		if err != nil {
			return jmap.MailboxGetResult{}, "", err
		}
		return result, result.State, nil
	})
}

func emailGetAtState(ctx context.Context, client *jmap.Client, accountID string, ids []string, opts jmap.EmailGetOptions, expectedState string) (jmap.EmailGetResult, error) {
	return waitForState(ctx, expectedState, func(callCtx context.Context) (jmap.EmailGetResult, string, error) {
		result, err := client.EmailGet(callCtx, accountID, ids, opts)
		if err != nil {
			return jmap.EmailGetResult{}, "", err
		}
		return result, result.State, nil
	})
}

func threadGetAtState(ctx context.Context, client *jmap.Client, accountID string, ids []string, expectedState string) (jmap.ThreadGetResult, error) {
	return waitForState(ctx, expectedState, func(callCtx context.Context) (jmap.ThreadGetResult, string, error) {
		result, err := client.ThreadGet(callCtx, accountID, ids)
		if err != nil {
			return jmap.ThreadGetResult{}, "", err
		}
		return result, result.State, nil
	})
}

func waitForState[T any](ctx context.Context, expectedState string, read func(context.Context) (T, string, error)) (T, error) {
	var zero T
	if expectedState == "" {
		result, _, err := read(ctx)
		return result, err
	}

	deadline := time.Now().Add(stateReadTimeout)
	for {
		result, state, err := read(ctx)
		if err != nil {
			return zero, err
		}
		if state == expectedState {
			return result, nil
		}
		if time.Now().After(deadline) {
			return zero, &stateReadMismatchError{
				Expected: expectedState,
				LastSeen: state,
			}
		}

		timer := time.NewTimer(stateReadBackoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, ctx.Err()
		case <-timer.C:
		}
	}
}
