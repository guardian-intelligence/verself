package api

import (
	"context"
	"strings"

	auth "github.com/forge-metal/auth-middleware"
)

func requireIdentity(ctx context.Context) (*auth.Identity, error) {
	identity := auth.FromContext(ctx)
	if identity == nil || strings.TrimSpace(identity.Subject) == "" {
		return nil, unauthorized(ctx, "missing-identity", "missing authenticated identity")
	}
	if strings.TrimSpace(identity.OrgID) == "" {
		return nil, forbidden(ctx, "missing-org", "authenticated identity is missing organization scope")
	}
	return identity, nil
}
