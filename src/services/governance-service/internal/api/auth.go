package api

import (
	"context"
	"strings"

	auth "github.com/verself/service-runtime/auth"
)

func requireIdentity(ctx context.Context) (*auth.Identity, error) {
	identity := auth.FromContext(ctx)
	if identity == nil || strings.TrimSpace(identity.Subject) == "" {
		return nil, unauthorized(ctx, "auth.unauthenticated", "missing authenticated identity")
	}
	if strings.TrimSpace(identity.OrgID) == "" {
		return nil, forbidden(ctx, "auth.permission_denied", "authenticated identity is missing organization scope")
	}
	return identity, nil
}
