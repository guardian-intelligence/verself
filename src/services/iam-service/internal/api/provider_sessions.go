package api

import (
	"context"
	"strings"
)

type ProviderSessionRevoker interface {
	DeleteSession(ctx context.Context, sessionID string) error
}

func providerSessionIDFromClaims(claims map[string]any) string {
	return strings.TrimSpace(claimString(claims, "sid"))
}
