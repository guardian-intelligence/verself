package api

import (
	"net/http"

	"github.com/verself/secrets-service/internal/secrets"
)

// CaptureRawBearerToken stores the request bearer in context before the shared
// auth middleware parses it. The raw value is deliberately not copied into the
// authenticated identity, audit payloads, spans, or DTOs.
func CaptureRawBearerToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := secrets.RawBearerTokenFromAuthorization(r.Header.Get("Authorization"))
		if ok {
			r = r.WithContext(secrets.ContextWithRawBearerToken(r.Context(), token))
		}
		next.ServeHTTP(w, r)
	})
}
