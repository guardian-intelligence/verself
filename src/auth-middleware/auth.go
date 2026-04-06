package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

const verifierInitTimeout = 5 * time.Second

type contextKey struct{}

var identityKey contextKey

// Identity is attached to the request context after successful validation.
type Identity struct {
	Subject string         // Zitadel user or service account ID.
	OrgID   string         // Organization/resource owner ID when present.
	Roles   []string       // Project-scoped roles.
	Email   string         // Email, if present in the token.
	Raw     map[string]any // All claims, for extensibility.
}

// Config for the middleware.
type Config struct {
	IssuerURL string // Expected issuer URL from the token's iss claim.
	Audience  string // Expected audience value from the token's aud claim.
	JWKSURL   string // Optional: fetch keys from this URL instead of OIDC discovery.
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

// Middleware returns HTTP middleware that validates Bearer tokens.
func Middleware(cfg Config) func(http.Handler) http.Handler {
	cache := &verifierCache{cfg: normalizeConfig(cfg)}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := bearerToken(r.Header.Get("Authorization"))
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "unauthorized", err.Error())
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
				writeJSONError(w, http.StatusServiceUnavailable, "auth_unavailable", "token verification unavailable")
				return
			}

			idToken, err := verifier.Verify(r.Context(), token)
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "unauthorized", "invalid bearer token")
				return
			}

			rawClaims := map[string]any{}
			if err := idToken.Claims(&rawClaims); err != nil {
				writeJSONError(w, http.StatusUnauthorized, "unauthorized", "invalid token claims")
				return
			}

			identity := &Identity{
				Subject: idToken.Subject,
				OrgID:   extractOrgID(rawClaims),
				Roles:   extractRoles(rawClaims),
				Email:   stringClaim(rawClaims, "email"),
				Raw:     rawClaims,
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey, identity)))
		})
	}
}

func normalizeConfig(cfg Config) Config {
	cfg.IssuerURL = strings.TrimSpace(cfg.IssuerURL)
	cfg.Audience = strings.TrimSpace(cfg.Audience)
	cfg.JWKSURL = strings.TrimSpace(cfg.JWKSURL)
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

	var provider *oidc.Provider
	if c.cfg.JWKSURL != "" {
		// Split-URL path: validate iss claim against IssuerURL, fetch keys
		// from JWKSURL. Used on single-node deployments where services
		// reach Zitadel via loopback instead of through Caddy.
		provCtx, err := c.cfg.jwksContext(initCtx)
		if err != nil {
			return nil, err
		}
		provider = (&oidc.ProviderConfig{
			IssuerURL: c.cfg.IssuerURL,
			JWKSURL:   c.cfg.JWKSURL,
		}).NewProvider(provCtx)
		log.Printf("auth: verifier initialized issuer=%s jwks=%s audience=%s",
			c.cfg.IssuerURL, c.cfg.JWKSURL, c.cfg.Audience)
	} else {
		var err error
		provider, err = oidc.NewProvider(initCtx, c.cfg.IssuerURL)
		if err != nil {
			return nil, err
		}
		log.Printf("auth: verifier initialized issuer=%s audience=%s",
			c.cfg.IssuerURL, c.cfg.Audience)
	}

	c.verifier = provider.Verifier(&oidc.Config{
		ClientID: c.cfg.Audience,
	})
	return c.verifier, nil
}

// jwksContext returns a context carrying an HTTP client that overrides the Host
// header on outgoing requests to match the issuer's hostname. Zitadel uses the
// Host header for instance routing and rejects requests whose Host doesn't match
// its configured ExternalDomain.
func (c Config) jwksContext(ctx context.Context) (context.Context, error) {
	issuer, err := url.Parse(c.IssuerURL)
	if err != nil {
		return nil, errors.New("auth: invalid issuer URL: " + err.Error())
	}

	client := &http.Client{
		Transport: &hostOverrideTransport{
			base: http.DefaultTransport,
			host: issuer.Host,
		},
	}
	return oidc.ClientContext(ctx, client), nil
}

// hostOverrideTransport injects a fixed Host header into every request. This
// lets us connect to Zitadel's loopback address while presenting the external
// domain it expects.
type hostOverrideTransport struct {
	base http.RoundTripper
	host string
}

func (t *hostOverrideTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Host = t.host
	return t.base.RoundTrip(req)
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

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   code,
		"message": message,
	})
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
	for _, key := range []string{
		"urn:zitadel:iam:user:resourceowner:id",
		"urn:zitadel:iam:org:id",
		"resource_owner",
		"org_id",
	} {
		if value := stringClaim(claims, key); value != "" {
			return value
		}
	}
	return ""
}

func extractRoles(claims map[string]any) []string {
	roleSet := map[string]struct{}{}

	for key, value := range claims {
		if key == "roles" || key == "role" {
			collectRoles(roleSet, value)
			continue
		}
		if key == "urn:zitadel:iam:org:project:roles" {
			collectRoles(roleSet, value)
			continue
		}
		if strings.HasPrefix(key, "urn:zitadel:iam:org:project:") && strings.HasSuffix(key, ":roles") {
			collectRoles(roleSet, value)
		}
	}

	if len(roleSet) == 0 {
		return nil
	}

	roles := make([]string, 0, len(roleSet))
	for role := range roleSet {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	return roles
}

func collectRoles(dst map[string]struct{}, value any) {
	switch typed := value.(type) {
	case string:
		if typed != "" {
			dst[typed] = struct{}{}
		}
	case []string:
		for _, role := range typed {
			if role != "" {
				dst[role] = struct{}{}
			}
		}
	case []any:
		for _, item := range typed {
			collectRoles(dst, item)
		}
	case map[string]any:
		for role := range typed {
			if role != "" {
				dst[role] = struct{}{}
			}
		}
	case map[string]string:
		for role := range typed {
			if role != "" {
				dst[role] = struct{}{}
			}
		}
	}
}
