package api

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/email-service/internal/contractapi"
	"github.com/verself/email-service/internal/mailstore"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

func registerMailRoutes(api huma.API, svc provider, authorizer runtimeiam.OperationAuthorizer) {
	op, policy := mailboxContract(contractapi.MailMarkRead.Descriptor, "Mark an email as read")
	registerMailRoute(api, authorizer, op, policy, markRead(svc, true))
	op, policy = mailboxContract(contractapi.MailMarkUnread.Descriptor, "Mark an email as unread")
	registerMailRoute(api, authorizer, op, policy, markRead(svc, false))
	op, policy = mailboxContract(contractapi.MailFlag.Descriptor, "Flag an email")
	registerMailRoute(api, authorizer, op, policy, flagEmail(svc, true))
	op, policy = mailboxContract(contractapi.MailUnflag.Descriptor, "Unflag an email")
	registerMailRoute(api, authorizer, op, policy, flagEmail(svc, false))
	op, policy = mailboxContract(contractapi.MailMove.Descriptor, "Move an email to another mailbox")
	registerMailRoute(api, authorizer, op, policy, moveEmail(svc))
	op, policy = mailboxContract(contractapi.MailTrash.Descriptor, "Move an email to trash")
	registerMailRoute(api, authorizer, op, policy, trashEmail(svc))
	op, policy = mailboxContract(contractapi.MailBody.Descriptor, "Fetch and cache an email body")
	registerMailRoute(api, authorizer, op, policy, fetchBody(svc))
	op, policy = mailboxContract(contractapi.MailAccount.Descriptor, "Get the authenticated user's bound mailbox account")
	registerMailRoute(api, authorizer, op, policy, accountInfo(svc))
	op, policy = mailboxContract(contractapi.MailSyncStatus.Descriptor, "Mailbox sync status")
	registerMailRoute(api, authorizer, op, policy, syncStatus(svc))
}

func markRead(svc provider, seen bool) func(context.Context, *contractapi.MailEmailIdempotentInput) (*contractapi.MailMutationOutput, error) {
	return func(ctx context.Context, input *contractapi.MailEmailIdempotentInput) (*contractapi.MailMutationOutput, error) {
		accountID, err := boundAccountID(ctx, svc)
		if err != nil {
			return nil, err
		}
		if err := svc.SetEmailSeen(ctx, accountID, string(input.EmailID), seen); err != nil {
			return nil, toHumaError("set read state", err)
		}
		return mailMutation(input.EmailID), nil
	}
}

func flagEmail(svc provider, flagged bool) func(context.Context, *contractapi.MailEmailIdempotentInput) (*contractapi.MailMutationOutput, error) {
	return func(ctx context.Context, input *contractapi.MailEmailIdempotentInput) (*contractapi.MailMutationOutput, error) {
		accountID, err := boundAccountID(ctx, svc)
		if err != nil {
			return nil, err
		}
		if err := svc.SetEmailFlagged(ctx, accountID, string(input.EmailID), flagged); err != nil {
			return nil, toHumaError("set flag state", err)
		}
		return mailMutation(input.EmailID), nil
	}
}

func moveEmail(svc provider) func(context.Context, *contractapi.MailMoveInput) (*contractapi.MailMutationOutput, error) {
	return func(ctx context.Context, input *contractapi.MailMoveInput) (*contractapi.MailMutationOutput, error) {
		accountID, err := boundAccountID(ctx, svc)
		if err != nil {
			return nil, err
		}
		if err := svc.MoveEmail(ctx, accountID, string(input.EmailID), string(input.Body.MailboxID)); err != nil {
			return nil, toHumaError("move email", err)
		}
		return mailMutation(input.EmailID), nil
	}
}

func trashEmail(svc provider) func(context.Context, *contractapi.MailEmailIdempotentInput) (*contractapi.MailMutationOutput, error) {
	return func(ctx context.Context, input *contractapi.MailEmailIdempotentInput) (*contractapi.MailMutationOutput, error) {
		accountID, err := boundAccountID(ctx, svc)
		if err != nil {
			return nil, err
		}
		if err := svc.TrashEmail(ctx, accountID, string(input.EmailID)); err != nil {
			return nil, toHumaError("trash email", err)
		}
		return mailMutation(input.EmailID), nil
	}
}

func fetchBody(svc provider) func(context.Context, *contractapi.MailEmailPathInput) (*contractapi.MailBodyOutput, error) {
	return func(ctx context.Context, input *contractapi.MailEmailPathInput) (*contractapi.MailBodyOutput, error) {
		accountID, err := boundAccountID(ctx, svc)
		if err != nil {
			return nil, err
		}
		body, err := svc.FetchEmailBody(ctx, accountID, string(input.EmailID))
		if err != nil {
			return nil, toHumaError("fetch email body", err)
		}
		return &contractapi.MailBodyOutput{Body: contractapi.MailBodyResult{
			AccountID: contractapi.AccountID(body.AccountID),
			EmailID:   contractapi.EmailID(body.EmailID),
			TextBody:  contractapi.MailBodyText(body.TextBody),
			HtmlBody:  contractapi.MailBodyHTML(body.HTMLBody),
			FetchedAt: body.FetchedAt.UTC().Format(time.RFC3339),
		}}, nil
	}
}

func accountInfo(svc provider) func(context.Context, *contractapi.EmptyInput) (*contractapi.MailAccountOutput, error) {
	return func(ctx context.Context, _ *contractapi.EmptyInput) (*contractapi.MailAccountOutput, error) {
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
		return &contractapi.MailAccountOutput{Body: contractapi.MailboxAccountView{
			AccountID:        contractapi.AccountID(account.AccountID),
			EmailAddress:     contractapi.EmailAddress(account.EmailAddress),
			DisplayName:      contractapi.MailboxDisplayName(account.DisplayName),
			DefaultMailboxID: defaultMailboxID(mailboxes),
		}}, nil
	}
}

func defaultMailboxID(mailboxes []mailstore.Mailbox) *contractapi.MailboxID {
	if len(mailboxes) == 0 {
		return nil
	}
	for _, mailbox := range mailboxes {
		if mailbox.Role == "inbox" {
			return mailboxIDPtr(mailbox.ID)
		}
	}
	return mailboxIDPtr(mailboxes[0].ID)
}

func mailboxIDPtr(value string) *contractapi.MailboxID {
	if value == "" {
		return nil
	}
	out := contractapi.MailboxID(value)
	return &out
}

func mailMutation(emailID contractapi.EmailID) *contractapi.MailMutationOutput {
	return &contractapi.MailMutationOutput{
		Body: contractapi.MailMutationResult{
			EmailID: emailID,
			Status:  "ok",
		},
	}
}

func syncStatus(svc provider) func(context.Context, *contractapi.EmptyInput) (*contractapi.MailSyncStatusOutput, error) {
	return func(context.Context, *contractapi.EmptyInput) (*contractapi.MailSyncStatusOutput, error) {
		return &contractapi.MailSyncStatusOutput{Body: contractapi.MailSyncStatusOutputBody{Status: serviceStatus(svc.Status())}}, nil
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
