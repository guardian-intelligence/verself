package auth

import (
	"cmp"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestMiddlewareRejectsMissingBearerToken(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t)
	defer provider.Close()

	handler := Middleware(Config{
		IssuerURL: provider.URL,
		Audience:  "billing-project",
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestMiddlewareAttachesIdentity(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t)
	defer provider.Close()

	token := provider.signToken(t, jwt.MapClaims{
		"iss":                                   provider.URL,
		"sub":                                   "user-123",
		"aud":                                   []string{"billing-project"},
		"exp":                                   time.Now().Add(time.Hour).Unix(),
		"email":                                 "alice@example.com",
		"urn:zitadel:iam:user:resourceowner:id": "home-org",
		"roles":                                 []string{"legacy-admin"},
		"urn:zitadel:iam:org:project:roles": map[string]any{
			"legacy-viewer": map[string]any{"org-456": "billing"},
		},
		"urn:zitadel:iam:org:project:billing-project:roles": map[string]any{
			"admin":  map[string]any{"org-456": "billing"},
			"viewer": map[string]any{"org-456": "billing"},
		},
		"urn:zitadel:iam:org:project:999:roles": map[string]any{
			"viewer": map[string]any{"org-456": "billing"},
			"editor": map[string]any{"org-456": "billing"},
		},
		"amr": []string{"pwd", "mfa"},
	})

	handler := Middleware(Config{
		IssuerURL: provider.URL,
		Audience:  "billing-project",
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := FromContext(r.Context())
		if identity == nil {
			t.Fatal("expected identity in context")
		}
		if identity.Subject != "user-123" {
			t.Fatalf("unexpected subject: %q", identity.Subject)
		}
		if identity.OrgID != "org-456" {
			t.Fatalf("unexpected org id: %q", identity.OrgID)
		}
		if identity.Email != "alice@example.com" {
			t.Fatalf("unexpected email: %q", identity.Email)
		}
		expectedRoles := []string{"admin", "viewer"}
		if len(identity.Roles) != len(expectedRoles) {
			t.Fatalf("unexpected roles length: got %v want %v", identity.Roles, expectedRoles)
		}
		for i, role := range expectedRoles {
			if identity.Roles[i] != role {
				t.Fatalf("unexpected roles: got %v want %v", identity.Roles, expectedRoles)
			}
		}
		expectedAssignments := []RoleAssignment{
			{
				OrganizationID: "org-456",
				Role:           "admin",
			},
			{
				OrganizationID: "org-456",
				Role:           "viewer",
			},
		}
		if len(identity.RoleAssignments) != len(expectedAssignments) {
			t.Fatalf("unexpected role assignments length: got %#v want %#v", identity.RoleAssignments, expectedAssignments)
		}
		actualAssignments := slices.Clone(identity.RoleAssignments)
		slices.SortFunc(actualAssignments, compareRoleAssignment)
		slices.SortFunc(expectedAssignments, compareRoleAssignment)
		for i, assignment := range expectedAssignments {
			if actualAssignments[i] != assignment {
				t.Fatalf("unexpected role assignments: got %#v want %#v", actualAssignments, expectedAssignments)
			}
		}
		if _, ok := identity.Raw["amr"]; !ok {
			t.Fatal("expected raw amr claim")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

func TestMiddlewareMissingTargetProjectRoleClaimAttachesNoRoles(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t)
	defer provider.Close()

	token := provider.signToken(t, jwt.MapClaims{
		"iss":                                   provider.URL,
		"sub":                                   "user-123",
		"aud":                                   []string{"identity-project"},
		"exp":                                   time.Now().Add(time.Hour).Unix(),
		"urn:zitadel:iam:user:resourceowner:id": "org-456",
		"urn:zitadel:iam:org:project:sandbox-project:roles": map[string]any{
			"admin": map[string]any{"org-456": "billing"},
		},
	})

	handler := Middleware(Config{
		IssuerURL: provider.URL,
		Audience:  "identity-project",
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := FromContext(r.Context())
		if identity == nil {
			t.Fatal("expected identity in context")
		}
		if len(identity.Roles) != 0 || len(identity.RoleAssignments) != 0 {
			t.Fatalf("target project without a role claim must not inherit other project roles: %#v", identity)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

func TestMiddlewareDoesNotSelectOrgFromMultiOrgRoleClaims(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t)
	defer provider.Close()

	token := provider.signToken(t, jwt.MapClaims{
		"iss": provider.URL,
		"sub": "user-123",
		"aud": []string{"billing-project"},
		"exp": time.Now().Add(time.Hour).Unix(),
		"urn:zitadel:iam:org:project:billing-project:roles": map[string]any{
			"admin": map[string]any{
				"org-456": "billing",
				"org-789": "billing-alt",
			},
		},
	})

	handler := Middleware(Config{
		IssuerURL: provider.URL,
		Audience:  "billing-project",
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := FromContext(r.Context())
		if identity == nil {
			t.Fatal("expected identity in context")
		}
		if identity.OrgID != "" {
			t.Fatalf("multi-org target role token must not select an org implicitly: %q", identity.OrgID)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

func TestMiddlewareSelectsExplicitOrgWithMultiOrgRoleClaims(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t)
	defer provider.Close()

	token := provider.signToken(t, jwt.MapClaims{
		"iss":                    provider.URL,
		"sub":                    "user-123",
		"aud":                    []string{"billing-project"},
		"exp":                    time.Now().Add(time.Hour).Unix(),
		"urn:zitadel:iam:org:id": "org-456",
		"urn:zitadel:iam:org:project:billing-project:roles": map[string]any{
			"admin": map[string]any{
				"org-456": "billing",
				"org-789": "billing-alt",
			},
		},
	})

	handler := Middleware(Config{
		IssuerURL: provider.URL,
		Audience:  "billing-project",
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := FromContext(r.Context())
		if identity == nil {
			t.Fatal("expected identity in context")
		}
		if identity.OrgID != "org-456" {
			t.Fatalf("explicit selected org must survive multi-org role claims: %q", identity.OrgID)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

func TestMiddlewareRejectsWrongAudience(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t)
	defer provider.Close()

	token := provider.signToken(t, jwt.MapClaims{
		"iss": provider.URL,
		"sub": "user-123",
		"aud": []string{"wrong-audience"},
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	handler := Middleware(Config{
		IssuerURL: provider.URL,
		Audience:  "billing-project",
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func compareRoleAssignment(a, b RoleAssignment) int {
	if diff := cmp.Compare(a.OrganizationID, b.OrganizationID); diff != 0 {
		return diff
	}
	return cmp.Compare(a.Role, b.Role)
}

type testProvider struct {
	*httptest.Server
	privateKey *rsa.PrivateKey
	keyID      string
}

func newTestProvider(t *testing.T) *testProvider {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	provider := &testProvider{
		privateKey: privateKey,
		keyID:      "test-key",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":   provider.URL,
			"jwks_uri": provider.URL + "/keys",
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{
				{
					"kty": "RSA",
					"use": "sig",
					"alg": "RS256",
					"kid": provider.keyID,
					"n":   base64.RawURLEncoding.EncodeToString(provider.privateKey.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(provider.privateKey.PublicKey.E)).Bytes()),
				},
			},
		})
	})

	provider.Server = httptest.NewServer(mux)
	return provider
}

func (p *testProvider) signToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = p.keyID

	signed, err := token.SignedString(p.privateKey)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}
