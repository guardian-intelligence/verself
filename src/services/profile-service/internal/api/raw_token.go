package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
)

type rawBearerTokenContextKey struct{}

type rawBearerToken struct {
	value string
}

func CaptureRawBearerToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token, ok := rawBearerTokenFromAuthorization(r.Header.Get("Authorization")); ok {
			r = r.WithContext(context.WithValue(r.Context(), rawBearerTokenContextKey{}, token))
		}
		next.ServeHTTP(w, r)
	})
}

func rawBearerTokenFromContext(ctx context.Context) (rawBearerToken, bool) {
	token, ok := ctx.Value(rawBearerTokenContextKey{}).(rawBearerToken)
	return token, ok && token.value != ""
}

func rawBearerTokenFromAuthorization(header string) (rawBearerToken, bool) {
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return rawBearerToken{}, false
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return rawBearerToken{}, false
	}
	return rawBearerToken{value: token}, true
}

func (t rawBearerToken) AuthorizationHeader() string {
	if t.value == "" {
		return ""
	}
	return "Bearer " + t.value
}

func (t rawBearerToken) String() string {
	return "[redacted:jwt]"
}

func (t rawBearerToken) GoString() string {
	return "[redacted:jwt]"
}

func (t rawBearerToken) LogValue() slog.Value {
	return slog.StringValue("[redacted:jwt]")
}
