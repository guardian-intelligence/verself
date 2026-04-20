package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
)

type rawBearerTokenContextKey struct{}

// RawBearerToken keeps bearer material out of accidental string formatting.
type RawBearerToken struct {
	value string
}

func rawBearerTokenFromAuthorization(header string) (RawBearerToken, bool) {
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return RawBearerToken{}, false
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return RawBearerToken{}, false
	}
	return RawBearerToken{value: token}, true
}

// CaptureRawBearerToken stores the request bearer in context before the shared
// auth middleware parses it. The raw value is deliberately not copied into the
// authenticated identity, audit payloads, spans, or DTOs.
func CaptureRawBearerToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := rawBearerTokenFromAuthorization(r.Header.Get("Authorization"))
		if ok {
			r = r.WithContext(context.WithValue(r.Context(), rawBearerTokenContextKey{}, token))
		}
		next.ServeHTTP(w, r)
	})
}

func RawBearerTokenFromContext(ctx context.Context) (RawBearerToken, bool) {
	token, ok := ctx.Value(rawBearerTokenContextKey{}).(RawBearerToken)
	return token, ok && token.value != ""
}

func (t RawBearerToken) ApplyAuthorization(req *http.Request) {
	if t.value == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+t.value)
}

func (t RawBearerToken) SHA256() string {
	sum := sha256.Sum256([]byte(t.value))
	return hex.EncodeToString(sum[:])
}

func (t RawBearerToken) String() string {
	return "[redacted:jwt]"
}

func (t RawBearerToken) GoString() string {
	return "[redacted:jwt]"
}

func (t RawBearerToken) LogValue() slog.Value {
	return slog.StringValue("[redacted:jwt]")
}

func (t RawBearerToken) MarshalJSON() ([]byte, error) {
	return []byte(`"[redacted:jwt]"`), nil
}
