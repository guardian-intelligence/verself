package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/mailbox-service/internal/mailstore"
	auth "github.com/verself/service-runtime/auth"
)

func boundAccountID(ctx context.Context, svc provider) (string, error) {
	identity := auth.FromContext(ctx)
	if identity == nil {
		return "", huma.Error401Unauthorized("missing identity")
	}
	accountID, err := svc.ResolveBoundAccount(ctx, identity.Subject)
	if err == nil {
		return accountID, nil
	}
	if err == mailstore.ErrNotFound {
		return "", huma.Error403Forbidden("identity is not bound to a mailbox account")
	}
	return "", huma.Error500InternalServerError("resolve mailbox binding", err)
}
