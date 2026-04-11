package zitadel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/forge-metal/identity-service/internal/identity"
)

func TestCreateAuthorizationRequestShape(t *testing.T) {
	var gotHost string
	var gotConnect string
	var gotAuth string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		gotConnect = r.Header.Get("Connect-Protocol-Version")
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/zitadel.authorization.v2.AuthorizationService/ListAuthorizations" &&
			r.URL.Path != "/zitadel.authorization.v2.AuthorizationService/CreateAuthorization" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Path == "/zitadel.authorization.v2.AuthorizationService/ListAuthorizations" {
			_ = json.NewEncoder(w).Encode(map[string]any{"authorizations": []any{}})
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer server.Close()

	client, err := New(Config{
		BaseURL:    server.URL,
		HostHeader: "auth.example.com",
		AdminToken: "admin-token",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	err = client.createAuthorization(context.Background(), "42", "project-1", "user-1", []string{identity.RoleOrgMember})
	if err != nil {
		t.Fatalf("create authorization: %v", err)
	}

	if gotHost != "auth.example.com" {
		t.Fatalf("host = %q", gotHost)
	}
	if gotConnect != "1" {
		t.Fatalf("connect protocol = %q", gotConnect)
	}
	if gotAuth != "Bearer admin-token" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if gotBody["userId"] != "user-1" || gotBody["projectId"] != "project-1" || gotBody["organizationId"] != "42" {
		t.Fatalf("unexpected body %#v", gotBody)
	}
}

func TestInviteMemberUsesSendCode(t *testing.T) {
	var createUserBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/users/new":
			if err := json.NewDecoder(r.Body).Decode(&createUserBody); err != nil {
				t.Fatalf("decode user body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "user-1"})
		case "/zitadel.authorization.v2.AuthorizationService/ListAuthorizations":
			_ = json.NewEncoder(w).Encode(map[string]any{"authorizations": []any{}})
		case "/zitadel.authorization.v2.AuthorizationService/CreateAuthorization":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, AdminToken: "admin-token"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = client.InviteMember(context.Background(), "42", "project-1", identity.InviteMemberRequest{
		Email:    "new@example.com",
		RoleKeys: []string{identity.RoleOrgMember},
	})
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	human, _ := createUserBody["human"].(map[string]any)
	email, _ := human["email"].(map[string]any)
	if _, ok := email["sendCode"].(map[string]any); !ok {
		t.Fatalf("expected email.sendCode object in %#v", createUserBody)
	}
}

func TestMemberRolesUsesFilteredAuthorizationQuery(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zitadel.authorization.v2.AuthorizationService/ListAuthorizations" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"pagination": map[string]any{"totalResult": "2", "appliedLimit": "2"},
			"authorizations": []map[string]any{
				{
					"id":           "assignment-1",
					"state":        "STATE_ACTIVE",
					"user":         map[string]any{"id": "user-1"},
					"project":      map[string]any{"id": "project-1"},
					"organization": map[string]any{"id": "42"},
					"roles":        []map[string]any{{"key": identity.RoleOrgAdmin}},
				},
				{
					"id":           "assignment-2",
					"state":        "STATE_INACTIVE",
					"user":         map[string]any{"id": "user-1"},
					"project":      map[string]any{"id": "project-1"},
					"organization": map[string]any{"id": "42"},
					"roles":        []map[string]any{{"key": identity.RoleOrgMember}},
				},
			},
		})
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, AdminToken: "admin-token"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	roles, err := client.MemberRoles(context.Background(), "42", "project-1", "user-1")
	if err != nil {
		t.Fatalf("member roles: %v", err)
	}
	if len(roles) != 1 || roles[0] != identity.RoleOrgAdmin {
		t.Fatalf("unexpected roles %#v", roles)
	}
	filters, ok := gotBody["filters"].([]any)
	if !ok || len(filters) != 3 {
		t.Fatalf("expected three authorization filters, got %#v", gotBody["filters"])
	}
	if got := filters[0].(map[string]any)["inUserIds"].(map[string]any)["ids"].([]any)[0]; got != "user-1" {
		t.Fatalf("unexpected user filter %#v", gotBody)
	}
	if got := filters[1].(map[string]any)["projectId"].(map[string]any)["id"]; got != "project-1" {
		t.Fatalf("unexpected project filter %#v", gotBody)
	}
	if got := filters[2].(map[string]any)["organizationId"].(map[string]any)["id"]; got != "42" {
		t.Fatalf("unexpected organization filter %#v", gotBody)
	}
}
