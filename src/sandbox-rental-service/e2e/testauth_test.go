// Mock OIDC provider for e2e auth. Generates an RSA key pair and serves
// OIDC discovery + JWKS endpoints so auth-middleware can validate test JWTs.
// Replicates the testProvider pattern from auth-middleware/auth_test.go.
package e2e_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	auth "github.com/forge-metal/auth-middleware"
)

// testAuthProvider is a local OIDC mock: RSA key pair + JWKS endpoint.
// Replicates the testProvider pattern from auth_test.go (unexported, can't import).
type testAuthProvider struct {
	*httptest.Server
	privateKey *rsa.PrivateKey
	keyID      string
}

func newTestAuthProvider(t *testing.T) *testAuthProvider {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	provider := &testAuthProvider{
		privateKey: privateKey,
		keyID:      "e2e-test-key",
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

func (p *testAuthProvider) signToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = p.keyID

	signed, err := token.SignedString(p.privateKey)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func (p *testAuthProvider) authConfig(audience string) auth.Config {
	return auth.Config{
		IssuerURL: p.URL,
		Audience:  audience,
	}
}
