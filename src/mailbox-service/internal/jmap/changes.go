package jmap

import (
	"context"
)

type EmailGetOptions struct {
	FetchTextBodyValues bool
	FetchHTMLBodyValues bool
}

func (c *Client) MailboxGet(ctx context.Context, accountID string, ids []string) (MailboxGetResult, error) {
	args := map[string]any{
		"accountId": accountID,
	}
	if len(ids) > 0 {
		args["ids"] = ids
	}
	response, err := c.Call(ctx, NewRequest(MethodCall{
		Name:      "Mailbox/get",
		Arguments: args,
		CallID:    "mailboxes",
	}))
	if err != nil {
		return MailboxGetResult{}, err
	}
	return Decode[MailboxGetResult](response, "mailboxes", "Mailbox/get")
}

func (c *Client) EmailQuery(ctx context.Context, accountID string, position, limit int) (EmailQueryResult, error) {
	args := map[string]any{
		"accountId": accountID,
		"sort": []map[string]any{
			{"property": "receivedAt", "isAscending": false},
		},
		"position": position,
		"limit":    limit,
	}
	response, err := c.Call(ctx, NewRequest(MethodCall{
		Name:      "Email/query",
		Arguments: args,
		CallID:    "query",
	}))
	if err != nil {
		return EmailQueryResult{}, err
	}
	return Decode[EmailQueryResult](response, "query", "Email/query")
}

func (c *Client) EmailGet(ctx context.Context, accountID string, ids []string, opts EmailGetOptions) (EmailGetResult, error) {
	args := map[string]any{
		"accountId": accountID,
		"ids":       ids,
		"properties": []string{
			"id",
			"blobId",
			"threadId",
			"mailboxIds",
			"keywords",
			"receivedAt",
			"sentAt",
			"subject",
			"from",
			"to",
			"cc",
			"replyTo",
			"preview",
			"hasAttachment",
			"size",
		},
	}
	if opts.FetchTextBodyValues || opts.FetchHTMLBodyValues {
		args["properties"] = []string{
			"id",
			"blobId",
			"threadId",
			"mailboxIds",
			"keywords",
			"receivedAt",
			"sentAt",
			"subject",
			"from",
			"to",
			"cc",
			"replyTo",
			"preview",
			"hasAttachment",
			"size",
			"textBody",
			"htmlBody",
			"bodyValues",
		}
	}
	if opts.FetchTextBodyValues {
		args["fetchTextBodyValues"] = true
	}
	if opts.FetchHTMLBodyValues {
		args["fetchHTMLBodyValues"] = true
	}
	response, err := c.Call(ctx, NewRequest(MethodCall{
		Name:      "Email/get",
		Arguments: args,
		CallID:    "emails",
	}))
	if err != nil {
		return EmailGetResult{}, err
	}
	return Decode[EmailGetResult](response, "emails", "Email/get")
}

func (c *Client) ThreadGet(ctx context.Context, accountID string, ids []string) (ThreadGetResult, error) {
	args := map[string]any{
		"accountId": accountID,
		"ids":       ids,
	}
	response, err := c.Call(ctx, NewRequest(MethodCall{
		Name:      "Thread/get",
		Arguments: args,
		CallID:    "threads",
	}))
	if err != nil {
		return ThreadGetResult{}, err
	}
	return Decode[ThreadGetResult](response, "threads", "Thread/get")
}

func (c *Client) MailboxChanges(ctx context.Context, accountID, sinceState string) (ChangesResult, error) {
	response, err := c.Call(ctx, NewRequest(MethodCall{
		Name: "Mailbox/changes",
		Arguments: map[string]any{
			"sinceState": sinceState,
		},
		CallID: "mailbox_changes",
	}))
	if err != nil {
		return ChangesResult{}, err
	}
	return Decode[ChangesResult](response, "mailbox_changes", "Mailbox/changes")
}

func (c *Client) EmailChanges(ctx context.Context, accountID, sinceState string) (ChangesResult, error) {
	response, err := c.Call(ctx, NewRequest(MethodCall{
		Name: "Email/changes",
		Arguments: map[string]any{
			"sinceState": sinceState,
		},
		CallID: "email_changes",
	}))
	if err != nil {
		return ChangesResult{}, err
	}
	return Decode[ChangesResult](response, "email_changes", "Email/changes")
}

func (c *Client) ThreadChanges(ctx context.Context, accountID, sinceState string) (ChangesResult, error) {
	response, err := c.Call(ctx, NewRequest(MethodCall{
		Name: "Thread/changes",
		Arguments: map[string]any{
			"sinceState": sinceState,
		},
		CallID: "thread_changes",
	}))
	if err != nil {
		return ChangesResult{}, err
	}
	return Decode[ChangesResult](response, "thread_changes", "Thread/changes")
}

func (c *Client) EmailSet(ctx context.Context, accountID string, update map[string]map[string]any) (EmailSetResult, error) {
	response, err := c.Call(ctx, NewRequest(MethodCall{
		Name: "Email/set",
		Arguments: map[string]any{
			"accountId": accountID,
			"update":    update,
		},
		CallID: "email_set",
	}))
	if err != nil {
		return EmailSetResult{}, err
	}
	return Decode[EmailSetResult](response, "email_set", "Email/set")
}
