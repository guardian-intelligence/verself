package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	auth "github.com/forge-metal/auth-middleware"
	"github.com/forge-metal/mailbox-service/internal/mailstore"
)

type mailEmailPathInput struct {
	EmailID string `path:"email_id"`
}

type mailMoveInput struct {
	EmailID string `path:"email_id"`
	Body struct {
		MailboxID string `json:"mailbox_id" required:"true"`
	}
}

type mailMutationOutput struct {
	Body struct {
		Status  string `json:"status"`
		EmailID string `json:"email_id"`
	}
}

type mailBodyOutput struct {
	Body struct {
		AccountID string `json:"account_id"`
		EmailID   string `json:"email_id"`
		TextBody  string `json:"text_body"`
		HTMLBody  string `json:"html_body"`
		FetchedAt string `json:"fetched_at"`
	}
}

type mailAccountOutput struct {
	Body struct {
		AccountID        string `json:"account_id"`
		EmailAddress     string `json:"email_address"`
		DisplayName      string `json:"display_name"`
		DefaultMailboxID string `json:"default_mailbox_id,omitempty"`
	}
}

type mailSyncStatusOutput struct {
	Body struct {
		Status any `json:"status"`
	}
}

func registerMailRoutes(api huma.API, svc provider) {
	huma.Register(api, huma.Operation{
		OperationID: "mail-mark-read",
		Method:      http.MethodPost,
		Path:        "/api/v1/mail/emails/{email_id}/read",
		Summary:     "Mark an email as read",
	}, markRead(svc, true))

	huma.Register(api, huma.Operation{
		OperationID: "mail-mark-unread",
		Method:      http.MethodPost,
		Path:        "/api/v1/mail/emails/{email_id}/unread",
		Summary:     "Mark an email as unread",
	}, markRead(svc, false))

	huma.Register(api, huma.Operation{
		OperationID: "mail-flag",
		Method:      http.MethodPost,
		Path:        "/api/v1/mail/emails/{email_id}/flag",
		Summary:     "Flag an email",
	}, flagEmail(svc, true))

	huma.Register(api, huma.Operation{
		OperationID: "mail-unflag",
		Method:      http.MethodPost,
		Path:        "/api/v1/mail/emails/{email_id}/unflag",
		Summary:     "Unflag an email",
	}, flagEmail(svc, false))

	huma.Register(api, huma.Operation{
		OperationID: "mail-move",
		Method:      http.MethodPost,
		Path:        "/api/v1/mail/emails/{email_id}/move",
		Summary:     "Move an email to another mailbox",
	}, moveEmail(svc))

	huma.Register(api, huma.Operation{
		OperationID: "mail-trash",
		Method:      http.MethodPost,
		Path:        "/api/v1/mail/emails/{email_id}/trash",
		Summary:     "Move an email to trash",
	}, trashEmail(svc))

	huma.Register(api, huma.Operation{
		OperationID: "mail-body",
		Method:      http.MethodGet,
		Path:        "/api/v1/mail/emails/{email_id}/body",
		Summary:     "Fetch and cache an email body",
	}, fetchBody(svc))

	huma.Register(api, huma.Operation{
		OperationID: "mail-account",
		Method:      http.MethodGet,
		Path:        "/api/v1/mail/account",
		Summary:     "Get the authenticated user's bound mailbox account",
	}, accountInfo(svc))

	huma.Register(api, huma.Operation{
		OperationID: "mail-sync-status",
		Method:      http.MethodGet,
		Path:        "/api/v1/mail/sync/status",
		Summary:     "Mailbox sync status",
	}, syncStatus(svc))
}

func markRead(svc provider, seen bool) func(context.Context, *mailEmailPathInput) (*mailMutationOutput, error) {
	return func(ctx context.Context, input *mailEmailPathInput) (*mailMutationOutput, error) {
		accountID, err := boundAccountID(ctx, svc)
		if err != nil {
			return nil, err
		}
		if err := svc.SetEmailSeen(ctx, accountID, input.EmailID, seen); err != nil {
			return nil, toHumaError("set read state", err)
		}
		out := &mailMutationOutput{}
		out.Body.Status = "ok"
		out.Body.EmailID = input.EmailID
		return out, nil
	}
}

func flagEmail(svc provider, flagged bool) func(context.Context, *mailEmailPathInput) (*mailMutationOutput, error) {
	return func(ctx context.Context, input *mailEmailPathInput) (*mailMutationOutput, error) {
		accountID, err := boundAccountID(ctx, svc)
		if err != nil {
			return nil, err
		}
		if err := svc.SetEmailFlagged(ctx, accountID, input.EmailID, flagged); err != nil {
			return nil, toHumaError("set flag state", err)
		}
		out := &mailMutationOutput{}
		out.Body.Status = "ok"
		out.Body.EmailID = input.EmailID
		return out, nil
	}
}

func moveEmail(svc provider) func(context.Context, *mailMoveInput) (*mailMutationOutput, error) {
	return func(ctx context.Context, input *mailMoveInput) (*mailMutationOutput, error) {
		accountID, err := boundAccountID(ctx, svc)
		if err != nil {
			return nil, err
		}
		if err := svc.MoveEmail(ctx, accountID, input.EmailID, input.Body.MailboxID); err != nil {
			return nil, toHumaError("move email", err)
		}
		out := &mailMutationOutput{}
		out.Body.Status = "ok"
		out.Body.EmailID = input.EmailID
		return out, nil
	}
}

func trashEmail(svc provider) func(context.Context, *mailEmailPathInput) (*mailMutationOutput, error) {
	return func(ctx context.Context, input *mailEmailPathInput) (*mailMutationOutput, error) {
		accountID, err := boundAccountID(ctx, svc)
		if err != nil {
			return nil, err
		}
		if err := svc.TrashEmail(ctx, accountID, input.EmailID); err != nil {
			return nil, toHumaError("trash email", err)
		}
		out := &mailMutationOutput{}
		out.Body.Status = "ok"
		out.Body.EmailID = input.EmailID
		return out, nil
	}
}

func fetchBody(svc provider) func(context.Context, *mailEmailPathInput) (*mailBodyOutput, error) {
	return func(ctx context.Context, input *mailEmailPathInput) (*mailBodyOutput, error) {
		accountID, err := boundAccountID(ctx, svc)
		if err != nil {
			return nil, err
		}
		body, err := svc.FetchEmailBody(ctx, accountID, input.EmailID)
		if err != nil {
			return nil, toHumaError("fetch email body", err)
		}
		out := &mailBodyOutput{}
		out.Body.AccountID = body.AccountID
		out.Body.EmailID = body.EmailID
		out.Body.TextBody = body.TextBody
		out.Body.HTMLBody = body.HTMLBody
		out.Body.FetchedAt = body.FetchedAt.UTC().Format(time.RFC3339)
		return out, nil
	}
}

func accountInfo(svc provider) func(context.Context, *mailboxServiceEmptyInput) (*mailAccountOutput, error) {
	return func(ctx context.Context, _ *mailboxServiceEmptyInput) (*mailAccountOutput, error) {
		identity := auth.FromContext(ctx)
		if identity == nil {
			return nil, huma.Error401Unauthorized("missing identity")
		}
		account, err := svc.GetBoundAccount(ctx, identity.Subject)
		if err != nil {
			return nil, toHumaError("get bound account", err)
		}
		mailboxes, err := svc.ListMailboxes(ctx, account.AccountID)
		if err != nil {
			return nil, toHumaError("list mailboxes", err)
		}
		out := &mailAccountOutput{}
		out.Body.AccountID = account.AccountID
		out.Body.EmailAddress = account.EmailAddress
		out.Body.DisplayName = account.DisplayName
		out.Body.DefaultMailboxID = defaultMailboxID(mailboxes)
		return out, nil
	}
}

func defaultMailboxID(mailboxes []mailstore.Mailbox) string {
	if len(mailboxes) == 0 {
		return ""
	}
	for _, mailbox := range mailboxes {
		if mailbox.Role == "inbox" {
			return mailbox.ID
		}
	}
	return mailboxes[0].ID
}

func syncStatus(svc provider) func(context.Context, *mailboxServiceEmptyInput) (*mailSyncStatusOutput, error) {
	return func(context.Context, *mailboxServiceEmptyInput) (*mailSyncStatusOutput, error) {
		out := &mailSyncStatusOutput{}
		out.Body.Status = svc.Status()
		return out, nil
	}
}

func toHumaError(message string, err error) error {
	switch err {
	case nil:
		return nil
	case mailstore.ErrNotFound:
		return huma.Error404NotFound(message)
	default:
		return huma.Error500InternalServerError(message, err)
	}
}
