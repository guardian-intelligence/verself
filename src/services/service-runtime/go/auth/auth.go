package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	verifierInitTimeout = 5 * time.Second
	authProblemDocsURL  = "https://verself.sh/docs/reference/iam/errors#"
)

type contextKey struct{}

var identityKey contextKey

// Identity is attached to the request context after successful validation.
type Identity struct {
	Subject string         // Zitadel user or service account ID.
	OrgID   string         // Explicitly selected organization ID for the target service token.
	Email   string         // Email, if present in the token.
	Raw     map[string]any // All claims, for extensibility.
}

// Config for the middleware.
type Config struct {
	IssuerURL string // Expected issuer URL from the token's iss claim.
	Audience  string // Expected audience value from the token's aud claim.
}

type verifierCache struct {
	cfg Config

	mu       sync.RWMutex
	verifier *oidc.IDTokenVerifier
}

// FromContext extracts the validated identity. Returns nil if unauthenticated.
func FromContext(ctx context.Context) *Identity {
	identity, _ := ctx.Value(identityKey).(*Identity)
	return identity
}

// WithIdentity is for in-process harnesses that need to exercise service
// authorization without standing up an OIDC issuer.
func WithIdentity(ctx context.Context, identity *Identity) context.Context {
	return context.WithValue(ctx, identityKey, identity)
}

// Middleware returns HTTP middleware that validates Bearer tokens.
func Middleware(cfg Config) func(http.Handler) http.Handler {
	cache := &verifierCache{cfg: normalizeConfig(cfg)}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := bearerToken(r.Header.Get("Authorization"))
			if err != nil {
				writeProblem(w, r, http.StatusUnauthorized, "auth.unauthenticated", err.Error())
				return
			}

			verifier, err := cache.get(r.Context())
			if err != nil {
				log.Printf(
					"auth: verifier init failed issuer=%s audience=%s err=%v",
					cache.cfg.IssuerURL,
					cache.cfg.Audience,
					err,
				)
				writeProblem(w, r, http.StatusServiceUnavailable, "service.unavailable", "token verification unavailable")
				return
			}

			idToken, err := verifier.Verify(r.Context(), token)
			if err != nil {
				writeProblem(w, r, http.StatusUnauthorized, "auth.unauthenticated", "invalid bearer token")
				return
			}

			rawClaims := map[string]any{}
			if err := idToken.Claims(&rawClaims); err != nil {
				writeProblem(w, r, http.StatusUnauthorized, "auth.unauthenticated", "invalid token claims")
				return
			}

			orgID := extractOrgID(rawClaims)
			identity := &Identity{
				Subject: idToken.Subject,
				OrgID:   orgID,
				Email:   stringClaim(rawClaims, "email"),
				Raw:     rawClaims,
			}
			trace.SpanFromContext(r.Context()).SetAttributes(
				attribute.String("auth.audience", cache.cfg.Audience),
				attribute.String("auth.selected_org_id", orgID),
			)

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey, identity)))
		})
	}
}

func normalizeConfig(cfg Config) Config {
	cfg.IssuerURL = strings.TrimSpace(cfg.IssuerURL)
	cfg.Audience = strings.TrimSpace(cfg.Audience)
	return cfg
}

func (c *verifierCache) get(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	if err := c.cfg.validate(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	if c.verifier != nil {
		defer c.mu.RUnlock()
		return c.verifier, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.verifier != nil {
		return c.verifier, nil
	}

	initCtx, cancel := context.WithTimeout(ctx, verifierInitTimeout)
	defer cancel()

	provider, err := oidc.NewProvider(initCtx, c.cfg.IssuerURL)
	if err != nil {
		return nil, err
	}
	log.Printf("auth: verifier initialized issuer=%s audience=%s",
		c.cfg.IssuerURL, c.cfg.Audience)

	c.verifier = provider.Verifier(&oidc.Config{
		ClientID: c.cfg.Audience,
	})
	return c.verifier, nil
}

func (c Config) validate() error {
	switch {
	case c.IssuerURL == "":
		return errors.New("issuer URL is required")
	case c.Audience == "":
		return errors.New("audience is required")
	default:
		return nil
	}
}

func bearerToken(header string) (string, error) {
	if header == "" {
		return "", errors.New("missing bearer token")
	}

	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", errors.New("malformed authorization header")
	}

	return parts[1], nil
}

type problemDetails struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Status      int    `json:"status"`
	Detail      string `json:"detail"`
	Code        string `json:"code"`
	Instance    string `json:"instance,omitempty"`
	Traceparent string `json:"traceparent,omitempty"`
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)

	problem := problemDetails{
		Type:   problemType(code),
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
		Code:   code,
	}
	if spanContext := trace.SpanContextFromContext(r.Context()); spanContext.HasTraceID() {
		problem.Instance = "urn:verself:trace:" + spanContext.TraceID().String()
		if spanContext.HasSpanID() {
			problem.Traceparent = "00-" + spanContext.TraceID().String() + "-" + spanContext.SpanID().String() + "-" + spanContext.TraceFlags().String()
		}
	}

	_ = json.NewEncoder(w).Encode(problem)
}

func problemType(code string) string {
	code = strings.TrimSpace(code)
	switch {
	case strings.HasPrefix(code, "auth."):
		return authProblemDocsURL + strings.NewReplacer(".", "-", "_", "-").Replace(code)
	case strings.HasPrefix(code, "service."):
		return "urn:verself:problem:service:" + strings.ReplaceAll(strings.TrimPrefix(code, "service."), ".", "_")
	default:
		return "urn:verself:problem:" + strings.ReplaceAll(strings.ReplaceAll(code, "-", "_"), ".", "_")
	}
}

func stringClaim(claims map[string]any, key string) string {
	value, ok := claims[key]
	if !ok {
		return ""
	}

	text, _ := value.(string)
	return text
}

func extractOrgID(claims map[string]any) string {
	if value := stringClaim(claims, "org_id"); value != "" {
		return value
	}
	if value := stringClaim(claims, "urn:zitadel:iam:org:id"); value != "" {
		return value
	}
	for _, key := range []string{"urn:zitadel:iam:user:resourceowner:id", "resource_owner"} {
		if value := stringClaim(claims, key); value != "" {
			return value
		}
	}
	return ""
}
