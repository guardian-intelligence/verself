package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/domain-transfer-objects"
	"github.com/verself/mailbox-service/internal/mailstore"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

type mailEmailPathInput struct {
	EmailID string `path:"email_id"`
}

type mailMoveInput struct {
	EmailID string `path:"email_id"`
	Body    dto.MailboxMoveRequest
}

type mailMutationOutput struct {
	Body dto.MailboxMutation
}

type mailBodyOutput struct {
	Body dto.MailboxBody
}

type mailAccountOutput struct {
	Body dto.MailboxAccount
}

type mailSyncStatusOutput struct {
	Body dto.MailboxServiceStatusResponse
}

func registerMailRoutes(api huma.API, svc provider, authorizer runtimeiam.OperationAuthorizer) {
	registerMailRoute(api, authorizer, huma.Operation{
		OperationID: "mail-mark-read",
		Method:      http.MethodPost,
		Path:        "/api/v1/mail/emails/{email_id}/read",
		Summary:     "Mark an email as read",
	}, mailWritePolicy("mail_mark_read", "mailbox_email", "mark_read"), markRead(svc, true))

	registerMailRoute(api, authorizer, huma.Operation{
		OperationID: "mail-mark-unread",
		Method:      http.MethodPost,
		Path:        "/api/v1/mail/emails/{email_id}/unread",
		Summary:     "Mark an email as unread",
	}, mailWritePolicy("mail_mark_unread", "mailbox_email", "mark_unread"), markRead(svc, false))

	registerMailRoute(api, authorizer, huma.Operation{
		OperationID: "mail-flag",
		Method:      http.MethodPost,
		Path:        "/api/v1/mail/emails/{email_id}/flag",
		Summary:     "Flag an email",
	}, mailWritePolicy("mail_flag", "mailbox_email", "flag"), flagEmail(svc, true))

	registerMailRoute(api, authorizer, huma.Operation{
		OperationID: "mail-unflag",
		Method:      http.MethodPost,
		Path:        "/api/v1/mail/emails/{email_id}/unflag",
		Summary:     "Unflag an email",
	}, mailWritePolicy("mail_unflag", "mailbox_email", "unflag"), flagEmail(svc, false))

	registerMailRoute(api, authorizer, huma.Operation{
		OperationID: "mail-move",
		Method:      http.MethodPost,
		Path:        "/api/v1/mail/emails/{email_id}/move",
		Summary:     "Move an email to another mailbox",
	}, mailWritePolicy("mail_move", "mailbox_email", "move"), moveEmail(svc))

	registerMailRoute(api, authorizer, huma.Operation{
		OperationID: "mail-trash",
		Method:      http.MethodPost,
		Path:        "/api/v1/mail/emails/{email_id}/trash",
		Summary:     "Move an email to trash",
	}, mailWritePolicy("mail_trash", "mailbox_email", "trash"), trashEmail(svc))

	registerMailRoute(api, authorizer, huma.Operation{
		OperationID: "mail-body",
		Method:      http.MethodGet,
		Path:        "/api/v1/mail/emails/{email_id}/body",
		Summary:     "Fetch and cache an email body",
	}, mailReadPolicy("mail_body", "mailbox_email", "read"), fetchBody(svc))

	registerMailRoute(api, authorizer, huma.Operation{
		OperationID: "mail-account",
		Method:      http.MethodGet,
		Path:        "/api/v1/mail/account",
		Summary:     "Get the authenticated user's bound mailbox account",
	}, runtimeiam.OperationPolicy{
		Permission:     runtimeiam.Permission("mailbox:account:read"),
		Resource:       "mailbox_account",
		Action:         runtimeiam.ActionRead,
		OrgScope:       runtimeiam.OrgScopeTokenSubject,
		RateLimitClass: "read",
		AuditEvent:     "mailbox.mail_account",
	}, accountInfo(svc))

	registerMailRoute(api, authorizer, huma.Operation{
		OperationID: "mail-sync-status",
		Method:      http.MethodGet,
		Path:        "/api/v1/mail/sync/status",
		Summary:     "Mailbox sync status",
	}, runtimeiam.OperationPolicy{
		Permission:     runtimeiam.Permission("mailbox:sync_status:read"),
		Resource:       "mailbox_sync_status",
		Action:         runtimeiam.ActionRead,
		OrgScope:       runtimeiam.OrgScopeTokenSubject,
		RateLimitClass: "read",
		AuditEvent:     "mailbox.mail_sync_status",
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
		out.Body = dto.MailboxMutation{Status: "ok", EmailID: input.EmailID}
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
		out.Body = dto.MailboxMutation{Status: "ok", EmailID: input.EmailID}
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
		out.Body = dto.MailboxMutation{Status: "ok", EmailID: input.EmailID}
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
		out.Body = dto.MailboxMutation{Status: "ok", EmailID: input.EmailID}
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
		out.Body = dto.MailboxBody{
			AccountID: body.AccountID,
			EmailID:   body.EmailID,
			TextBody:  body.TextBody,
			HTMLBody:  body.HTMLBody,
			FetchedAt: body.FetchedAt.UTC().Format(time.RFC3339),
		}
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
		out.Body = dto.MailboxAccount{
			AccountID:        account.AccountID,
			EmailAddress:     account.EmailAddress,
			DisplayName:      account.DisplayName,
			DefaultMailboxID: defaultMailboxID(mailboxes),
		}
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
		out.Body.Status = serviceStatus(svc.Status())
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
