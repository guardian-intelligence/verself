package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlatformRuntimeAuthAudienceSpecsIncludeBillingWebAudience(t *testing.T) {
	var found bool
	credentialPaths := map[string]string{}
	for _, spec := range platformRuntimeAuthAudienceSpecs() {
		if previous, ok := credentialPaths[spec.CredentialPath]; ok {
			t.Fatalf("duplicate runtime auth audience credential path %s for %s and %s", spec.CredentialPath, previous, spec.ComponentName)
		}
		credentialPaths[spec.CredentialPath] = spec.ComponentName
		if spec.ComponentName == "verself-web.billing" {
			found = true
			if spec.ProjectName != "billing" {
				t.Fatalf("web billing audience must target billing project, got %q", spec.ProjectName)
			}
			if spec.CredentialPath != "/etc/credstore/verself-web/billing-service-auth-audience" {
				t.Fatalf("unexpected web billing audience credential path %q", spec.CredentialPath)
			}
			if spec.Group != "verself-web" {
				t.Fatalf("unexpected web billing audience credential group %q", spec.Group)
			}
		}
	}
	if !found {
		t.Fatal("missing verself-web billing runtime auth audience")
	}
}

func TestPlatformZitadelUpsertAuthorizationCreatesAndUpdates(t *testing.T) {
	var authorizations []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		if got := r.Host; got != "auth.example.test" {
			t.Fatalf("unexpected host header %q", got)
		}
		switch r.URL.Path {
		case "/zitadel.authorization.v2.AuthorizationService/ListAuthorizations":
			writeJSON(t, w, map[string]any{
				"pagination":     map[string]any{"totalResult": len(authorizations)},
				"authorizations": authorizations,
			})
		case "/zitadel.authorization.v2.AuthorizationService/CreateAuthorization":
			var body struct {
				UserID         string   `json:"userId"`
				ProjectID      string   `json:"projectId"`
				OrganizationID string   `json:"organizationId"`
				RoleKeys       []string `json:"roleKeys"`
			}
			decodeJSON(t, r, &body)
			authorizations = append(authorizations, map[string]any{
				"id":           "authz-1",
				"user":         map[string]any{"id": body.UserID},
				"project":      map[string]any{"id": body.ProjectID},
				"organization": map[string]any{"id": body.OrganizationID},
				"roles":        rolePayload(body.RoleKeys),
				"state":        "STATE_ACTIVE",
			})
			writeJSON(t, w, map[string]any{})
		case "/zitadel.authorization.v2.AuthorizationService/UpdateAuthorization":
			var body struct {
				ID       string   `json:"id"`
				RoleKeys []string `json:"roleKeys"`
			}
			decodeJSON(t, r, &body)
			if body.ID != "authz-1" {
				t.Fatalf("unexpected authorization id %q", body.ID)
			}
			authorizations[0]["roles"] = rolePayload(body.RoleKeys)
			writeJSON(t, w, map[string]any{})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := platformZitadelClient{
		BaseURL:    server.URL,
		HostHeader: "auth.example.test",
		Token:      "test-token",
		Client:     server.Client(),
	}
	changed, err := client.UpsertAuthorization(context.Background(), "org-1", "project-1", "user-1", []string{"owner"})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("first upsert did not create authorization")
	}
	changed, err = client.UpsertAuthorization(context.Background(), "org-1", "project-1", "user-1", []string{"owner"})
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("second upsert changed matching authorization")
	}
	changed, err = client.UpsertAuthorization(context.Background(), "org-1", "project-1", "user-1", []string{"admin", "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("role update did not change authorization")
	}
	ok, roles, err := client.AuthorizationHasRoles(context.Background(), "org-1", "project-1", "user-1", []string{"owner", "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || strings.Join(roles, ",") != "admin,owner" {
		t.Fatalf("unexpected final roles ok=%t roles=%v", ok, roles)
	}
}

func rolePayload(keys []string) []map[string]string {
	roles := make([]map[string]string, 0, len(keys))
	for _, key := range keys {
		roles = append(roles, map[string]string{"key": key})
	}
	return roles
}

func decodeJSON(t *testing.T, r *http.Request, out any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}
