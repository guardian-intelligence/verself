package secrets

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

func NewRawBearerToken(value string) (RawBearerToken, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, " \t\r\n") {
		return RawBearerToken{}, false
	}
	return RawBearerToken{value: value}, true
}

func RawBearerTokenFromAuthorization(header string) (RawBearerToken, bool) {
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return RawBearerToken{}, false
	}
	return NewRawBearerToken(token)
}

func ContextWithRawBearerToken(ctx context.Context, token RawBearerToken) context.Context {
	if token.value == "" {
		return ctx
	}
	return context.WithValue(ctx, rawBearerTokenContextKey{}, token)
}

func RawBearerTokenFromContext(ctx context.Context) (RawBearerToken, bool) {
	token, ok := ctx.Value(rawBearerTokenContextKey{}).(RawBearerToken)
	return token, ok && token.value != ""
}

func (t RawBearerToken) AuthorizationHeader() string {
	if t.value == "" {
		return ""
	}
	return "Bearer " + t.value
}

func (t RawBearerToken) ApplyAuthorization(req *http.Request) {
	if header := t.AuthorizationHeader(); header != "" {
		req.Header.Set("Authorization", header)
	}
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
