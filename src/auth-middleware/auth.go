package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const verifierInitTimeout = 5 * time.Second

type contextKey struct{}

var identityKey contextKey

// Identity is attached to the request context after successful validation.
type Identity struct {
	Subject         string           // Zitadel user or service account ID.
	OrgID           string           // Organization/resource owner ID when present.
	Roles           []string         // Roles from the target service project claim.
	RoleAssignments []RoleAssignment // Structured target-project role assignments.
	Email           string           // Email, if present in the token.
	Raw             map[string]any   // All claims, for extensibility.
}

type RoleAssignment struct {
	Role             string
	OrganizationID   string
	OrganizationName string
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

			roleAssignments := extractRoleAssignments(rawClaims, cache.cfg.Audience)
			identity := &Identity{
				Subject:         idToken.Subject,
				OrgID:           extractOrgID(rawClaims),
				Roles:           rolesFromAssignments(roleAssignments),
				RoleAssignments: roleAssignments,
				Email:           stringClaim(rawClaims, "email"),
				Raw:             rawClaims,
			}
			trace.SpanFromContext(r.Context()).SetAttributes(
				attribute.String("auth.audience", cache.cfg.Audience),
				attribute.Int("auth.role_assignment_count", len(roleAssignments)),
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

func rolesFromAssignments(assignments []RoleAssignment) []string {
	roleSet := map[string]struct{}{}
	for _, assignment := range assignments {
		if assignment.Role != "" {
			roleSet[assignment.Role] = struct{}{}
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

func extractRoleAssignments(claims map[string]any, projectID string) []RoleAssignment {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	assignments := collectRoleAssignments(claims["urn:zitadel:iam:org:project:"+projectID+":roles"])

	if len(assignments) == 0 {
		return nil
	}

	sort.Slice(assignments, func(i, j int) bool {
		left := assignments[i]
		right := assignments[j]
		if left.OrganizationID != right.OrganizationID {
			return left.OrganizationID < right.OrganizationID
		}
		return left.Role < right.Role
	})
	return assignments
}

func collectRoleAssignments(value any) []RoleAssignment {
	roleMap, ok := value.(map[string]any)
	if !ok {
		return nil
	}

	assignments := make([]RoleAssignment, 0, len(roleMap))
	for role, organizationsValue := range roleMap {
		organizations, ok := organizationsValue.(map[string]any)
		if !ok {
			continue
		}
		for organizationID, organizationNameValue := range organizations {
			organizationName, _ := organizationNameValue.(string)
			assignments = append(assignments, RoleAssignment{
				Role:             role,
				OrganizationID:   organizationID,
				OrganizationName: organizationName,
			})
		}
	}
	return assignments
}
