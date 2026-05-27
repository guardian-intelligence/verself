package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
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
	if contentType := rec.Header().Get("Content-Type"); contentType != "application/problem+json" {
		t.Fatalf("expected problem content type, got %q", contentType)
	}
	problem := decodeProblem(t, rec)
	if problem.Type != "https://verself.sh/docs/reference/iam/errors#auth-unauthenticated" {
		t.Fatalf("unexpected problem type: %q", problem.Type)
	}
	if problem.Code != "auth.unauthenticated" {
		t.Fatalf("unexpected problem code: %q", problem.Code)
	}
	if problem.Detail != "missing bearer token" {
		t.Fatalf("unexpected problem detail: %q", problem.Detail)
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
		"org_id":                                "org_00000000000000000000000000",
		"urn:zitadel:iam:org:id":                "42",
		"urn:zitadel:iam:user:resourceowner:id": "42",
		"amr":                                   []string{"pwd", "mfa"},
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
		if identity.OrgID != "org_00000000000000000000000000" {
			t.Fatalf("unexpected org id: %q", identity.OrgID)
		}
		if identity.Email != "alice@example.com" {
			t.Fatalf("unexpected email: %q", identity.Email)
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

func TestMiddlewareIgnoresZitadelOrganizationClaims(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t)
	defer provider.Close()

	token := provider.signToken(t, jwt.MapClaims{
		"iss":                                   provider.URL,
		"sub":                                   "user-123",
		"aud":                                   []string{"identity-project"},
		"exp":                                   time.Now().Add(time.Hour).Unix(),
		"urn:zitadel:iam:user:resourceowner:id": "42",
	})

	handler := Middleware(Config{
		IssuerURL: provider.URL,
		Audience:  "identity-project",
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := FromContext(r.Context())
		if identity == nil {
			t.Fatal("expected identity in context")
		}
		if identity.OrgID != "" {
			t.Fatalf("provider org leaked into authz context: %#v", identity)
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

func TestMiddlewareUsesOnlyPublicOrgClaim(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t)
	defer provider.Close()

	token := provider.signToken(t, jwt.MapClaims{
		"iss":                    provider.URL,
		"sub":                    "user-123",
		"aud":                    []string{"billing-project"},
		"exp":                    time.Now().Add(time.Hour).Unix(),
		"org_id":                 "org_00000000000000000000000000",
		"urn:zitadel:iam:org:id": "42",
	})

	handler := Middleware(Config{
		IssuerURL: provider.URL,
		Audience:  "billing-project",
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := FromContext(r.Context())
		if identity == nil {
			t.Fatal("expected identity in context")
		}
		if identity.OrgID != "org_00000000000000000000000000" {
			t.Fatalf("public org claim must be preferred: %q", identity.OrgID)
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

func TestMiddlewareRejectsInvalidOrgIDClaim(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t)
	defer provider.Close()

	token := provider.signToken(t, jwt.MapClaims{
		"iss":    provider.URL,
		"sub":    "user-123",
		"aud":    []string{"billing-project"},
		"exp":    time.Now().Add(time.Hour).Unix(),
		"org_id": "42",
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
	problem := decodeProblem(t, rec)
	if problem.Detail != "invalid token claims" {
		t.Fatalf("unexpected problem detail: %q", problem.Detail)
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
	problem := decodeProblem(t, rec)
	if problem.Code != "auth.unauthenticated" {
		t.Fatalf("unexpected problem code: %q", problem.Code)
	}
	if problem.Detail != "invalid bearer token" {
		t.Fatalf("unexpected problem detail: %q", problem.Detail)
	}
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) problemDetails {
	t.Helper()

	var problem problemDetails
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem body: %v body=%q", err, rec.Body.String())
	}
	return problem
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
