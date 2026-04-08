package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	auth "github.com/forge-metal/auth-middleware"
	"github.com/forge-metal/mailbox-service/internal/mailstore"
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
