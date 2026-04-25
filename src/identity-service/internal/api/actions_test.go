package api

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/verself/identity-service/internal/identity"
)

func TestZitadelActionAppendsCredentialClaims(t *testing.T) {
	store := actionStore{
		staticIdentityStore: staticIdentityStore{capabilities: identity.DefaultMemberCapabilitiesDocument("42", "tester", time.Unix(1700000000, 0).UTC())},
		result: identity.ResolveAPICredentialClaimsResult{
			CredentialID: "credential-1",
			OrgID:        "42",
			DisplayName:  "deploy bot",
			AuthMethod:   identity.APICredentialAuthMethodPrivateKeyJWT,
			Fingerprint:  "sha256:abcdef",
			OwnerID:      "owner-1",
			OwnerDisplay: "owner@example.test",
			Permissions:  []string{"sandbox:logs:read"},
		},
	}
	svc := &identity.Service{Store: store}
	payload := []byte(`{"user":{"id":"subject-1"}}`)
	req := httptest.NewRequest(http.MethodPost, "/internal/zitadel/actions/api-credential-claims", bytes.NewReader(payload))
	req.Header.Set(zitadelActionSigningHeader, actionSignatureHeader(time.Now(), payload, "signing-key"))
	rec := httptest.NewRecorder()

	zitadelActionHandler(svc, "signing-key").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response zitadelActionResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	claims := map[string]any{}
	for _, claim := range response.AppendClaims {
		claims[claim.Key] = claim.Value
	}
	if claims["verself:credential_id"] != "credential-1" || claims["org_id"] != "42" {
		t.Fatalf("missing identity claims: %#v", claims)
	}
	if claims["verself:credential_name"] != "deploy bot" ||
		claims["verself:credential_fingerprint"] != "sha256:abcdef" ||
		claims["verself:credential_owner_id"] != "owner-1" ||
		claims["verself:credential_owner_display"] != "owner@example.test" ||
		claims["verself:credential_auth_method"] != "private_key_jwt" {
		t.Fatalf("missing credential audit claims: %#v", claims)
	}
	permissions, ok := claims["permissions"].([]any)
	if !ok || len(permissions) != 1 || permissions[0] != "sandbox:logs:read" {
		t.Fatalf("unexpected permissions claim: %#v", claims["permissions"])
	}
}

func TestZitadelActionRejectsInvalidSignature(t *testing.T) {
	svc := &identity.Service{Store: actionStore{}}
	req := httptest.NewRequest(http.MethodPost, "/internal/zitadel/actions/api-credential-claims", bytes.NewReader([]byte(`{"user":{"id":"subject-1"}}`)))
	req.Header.Set(zitadelActionSigningHeader, "t=1700000000,v1=deadbeef")
	rec := httptest.NewRecorder()

	zitadelActionHandler(svc, "signing-key").ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func actionSignatureHeader(ts time.Time, payload []byte, signingKey string) string {
	return fmt.Sprintf("t=%d,v1=%s", ts.Unix(), hex.EncodeToString(computeZitadelActionSignature(ts, payload, signingKey)))
}

type actionStore struct {
	staticIdentityStore
	result identity.ResolveAPICredentialClaimsResult
	err    error
}

func (s actionStore) ResolveAPICredentialClaims(context.Context, string, time.Time) (identity.ResolveAPICredentialClaimsResult, error) {
	if s.err != nil {
		return identity.ResolveAPICredentialClaimsResult{}, s.err
	}
	return s.result, nil
}
