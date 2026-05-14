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

	"github.com/verself/iam-service/internal/identity"
)

func TestZitadelActionAppendsCredentialClaims(t *testing.T) {
	store := actionStore{
		result: identity.ResolveAPICredentialClaimsResult{
			CredentialID:       "credential-1",
			ServiceAccountID:   "service-account-1",
			OrgID:              "org_01J8QJ4P1R7S9W2X5M6N8P0Q2",
			DisplayName:        "deploy bot key",
			ServiceAccountName: "deploy bot",
			AuthMethod:         identity.APICredentialAuthMethodPrivateKeyJWT,
			Fingerprint:        "sha256:abcdef",
			OwnerID:            "owner-1",
			OwnerDisplay:       "owner@example.test",
		},
	}
	svc := &identity.Service{Store: store}
	payload := []byte(`{"user":{"id":"subject-1"}}`)
	req := httptest.NewRequest(http.MethodPost, "/internal/zitadel/actions/product-token-claims", bytes.NewReader(payload))
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
	if claims["verself:credential_id"] != "credential-1" || claims["org_id"] != "org_01J8QJ4P1R7S9W2X5M6N8P0Q2" {
		t.Fatalf("missing identity claims: %#v", claims)
	}
	if claims["verself:credential_name"] != "deploy bot key" ||
		claims["verself:credential_fingerprint"] != "sha256:abcdef" ||
		claims["verself:credential_owner_id"] != "owner-1" ||
		claims["verself:credential_owner_display"] != "owner@example.test" ||
		claims["verself:credential_auth_method"] != "private_key_jwt" ||
		claims["verself:principal_type"] != "service_account" ||
		claims["verself:service_account_id"] != "service-account-1" ||
		claims["verself:service_account_name"] != "deploy bot" {
		t.Fatalf("missing credential audit claims: %#v", claims)
	}
	if _, ok := claims["permissions"]; ok {
		t.Fatalf("permissions claim should not be minted by the identity provider action: %#v", claims)
	}
}

func TestZitadelActionRejectsInvalidSignature(t *testing.T) {
	svc := &identity.Service{Store: actionStore{}}
	req := httptest.NewRequest(http.MethodPost, "/internal/zitadel/actions/product-token-claims", bytes.NewReader([]byte(`{"user":{"id":"subject-1"}}`)))
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

func TestZitadelActionAppendsPublicOrgClaimFromPreAccessTokenPayload(t *testing.T) {
	store := actionStore{
		profile: identity.OrganizationProfile{
			OrgID:                 "org_01J8QJ4P1R7S9W2X5M6N8P0Q2",
			IdentityProviderOrgID: "provider-org-1",
			State:                 identity.OrganizationProfileStateActive,
		},
		err: identity.ErrAPICredentialMissing,
	}
	svc := &identity.Service{Store: store}
	payload := []byte(`{"function":"preaccesstoken","user":{"id":"subject-1"},"org":{"id":"provider-org-1"}}`)
	req := httptest.NewRequest(http.MethodPost, "/internal/zitadel/actions/product-token-claims", bytes.NewReader(payload))
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
	if claims["org_id"] != "org_01J8QJ4P1R7S9W2X5M6N8P0Q2" {
		t.Fatalf("org_id claim = %#v", claims["org_id"])
	}
}

type actionStore struct {
	staticIdentityStore
	result  identity.ResolveAPICredentialClaimsResult
	profile identity.OrganizationProfile
	err     error
}

func (s actionStore) ResolveAPICredentialClaims(context.Context, string, time.Time) (identity.ResolveAPICredentialClaimsResult, error) {
	if s.err != nil {
		return identity.ResolveAPICredentialClaimsResult{}, s.err
	}
	return s.result, nil
}

func (s actionStore) ResolveOrganizationProfile(context.Context, identity.ResolveOrganizationRequest) (identity.OrganizationProfile, error) {
	if s.profile.OrgID == "" {
		return identity.OrganizationProfile{}, identity.ErrOrganizationMissing
	}
	return s.profile, nil
}
