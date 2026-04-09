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
		"urn:zitadel:iam:user:resourceowner:id": "org-456",
		"urn:zitadel:iam:org:project:roles": map[string]any{
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
		expectedRoles := []string{"admin", "editor", "viewer"}
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
				OrganizationID:   "org-456",
				OrganizationName: "billing",
				Role:             "admin",
			},
			{
				OrganizationID:   "org-456",
				OrganizationName: "billing",
				ProjectID:        "999",
				Role:             "editor",
			},
			{
				OrganizationID:   "org-456",
				OrganizationName: "billing",
				Role:             "viewer",
			},
			{
				OrganizationID:   "org-456",
				OrganizationName: "billing",
				ProjectID:        "999",
				Role:             "viewer",
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
	if diff := cmp.Compare(a.OrganizationName, b.OrganizationName); diff != 0 {
		return diff
	}
	if diff := cmp.Compare(a.ProjectID, b.ProjectID); diff != 0 {
		return diff
	}
	return cmp.Compare(a.Role, b.Role)
}

func TestMiddlewareWithSplitJWKSURL(t *testing.T) {
	t.Parallel()

	// JWKS server: serves keys only (no OIDC discovery endpoint).
	provider := newTestProvider(t)
	defer provider.Close()

	// The issuer URL is a synthetic value that doesn't serve anything.
	// ProviderConfig validates the iss claim against it without fetching discovery.
	fakeIssuer := "https://auth.example.com"

	token := provider.signToken(t, jwt.MapClaims{
		"iss":   fakeIssuer,
		"sub":   "svc-account-1",
		"aud":   []string{"sandbox-project"},
		"exp":   time.Now().Add(time.Hour).Unix(),
		"email": "svc@example.com",
	})

	handler := Middleware(Config{
		IssuerURL: fakeIssuer,
		JWKSURL:   provider.URL + "/keys",
		Audience:  "sandbox-project",
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := FromContext(r.Context())
		if identity == nil {
			t.Fatal("expected identity in context")
		}
		if identity.Subject != "svc-account-1" {
			t.Fatalf("unexpected subject: %q", identity.Subject)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMiddlewareWithSplitJWKSURLUsesIssuerHostHeader(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t)
	provider.expectedJWKSHost = "auth.example.com"
	defer provider.Close()

	token := provider.signToken(t, jwt.MapClaims{
		"iss": "https://auth.example.com",
		"sub": "svc-account-1",
		"aud": []string{"sandbox-project"},
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	handler := Middleware(Config{
		IssuerURL: "https://auth.example.com",
		JWKSURL:   provider.URL + "/keys",
		Audience:  "sandbox-project",
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMiddlewareWithSplitJWKSURLRejectsWrongIssuer(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t)
	defer provider.Close()

	// Token has a different issuer than what middleware expects.
	token := provider.signToken(t, jwt.MapClaims{
		"iss": "https://wrong-issuer.example.com",
		"sub": "user-1",
		"aud": []string{"sandbox-project"},
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	handler := Middleware(Config{
		IssuerURL: "https://auth.example.com",
		JWKSURL:   provider.URL + "/keys",
		Audience:  "sandbox-project",
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

type testProvider struct {
	*httptest.Server
	privateKey       *rsa.PrivateKey
	keyID            string
	expectedJWKSHost string
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
		if provider.expectedJWKSHost != "" && r.Host != provider.expectedJWKSHost {
			http.Error(w, "unexpected host "+r.Host, http.StatusBadRequest)
			return
		}
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
