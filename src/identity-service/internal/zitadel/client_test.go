package zitadel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/verself/identity-service/internal/identity"
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
	err = client.createAuthorization(context.Background(), "42", "project-1", "user-1", []string{identity.RoleMember})
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
		RoleKeys: []string{identity.RoleMember},
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

func TestCreateServiceAccountCredentialRequestShape(t *testing.T) {
	var createBody map[string]any
	var keyBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/users/new":
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "subject-1"})
		case "/v2/users/subject-1/keys":
			if err := json.NewDecoder(r.Body).Decode(&keyBody); err != nil {
				t.Fatalf("decode key body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"keyId": "key-1", "keyContent": "private-key"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, HostHeader: "auth.example.com", AdminToken: "admin-token"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	subjectID, material, err := client.CreateServiceAccountCredential(context.Background(), "42", identity.ServiceAccountCredentialInput{
		CredentialID: "credential-1",
		ClientID:     "client-1",
		DisplayName:  "Automation",
		AuthMethod:   identity.APICredentialAuthMethodPrivateKeyJWT,
	})
	if err != nil {
		t.Fatalf("create service account credential: %v", err)
	}
	if subjectID != "subject-1" {
		t.Fatalf("subject = %q", subjectID)
	}
	if material.AuthMethod != identity.APICredentialAuthMethodPrivateKeyJWT || material.KeyID != "key-1" || material.KeyContent != "private-key" {
		t.Fatalf("unexpected material %#v", material)
	}
	if material.TokenURL != "https://auth.example.com/oauth/v2/token" {
		t.Fatalf("token url = %q", material.TokenURL)
	}
	machine, _ := createBody["machine"].(map[string]any)
	if createBody["organizationId"] != "42" || createBody["username"] != "client-1" || machine["accessTokenType"] != "ACCESS_TOKEN_TYPE_JWT" {
		t.Fatalf("unexpected create body %#v", createBody)
	}
	if got := keyBody["expirationDate"]; got != zitadelMaxKeyExpiration.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected key body %#v", keyBody)
	}
}

func TestAddServiceAccountClientSecretRequestShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/users/subject-1/secret" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"clientSecret": "secret-1"})
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, AdminToken: "admin-token"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	material, err := client.AddServiceAccountCredential(context.Background(), identity.AddServiceAccountCredentialInput{
		SubjectID:  "subject-1",
		ClientID:   "client-1",
		AuthMethod: identity.APICredentialAuthMethodClientSecret,
	})
	if err != nil {
		t.Fatalf("add service account secret: %v", err)
	}
	if material.AuthMethod != identity.APICredentialAuthMethodClientSecret || material.ClientSecret != "secret-1" {
		t.Fatalf("unexpected material %#v", material)
	}
}

func TestRemoveServiceAccountCredentialAllowsAlreadyDeletedUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v2/users/subject-1/secret" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 5, "message": "User could not be found (COMMAND-test)"})
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, AdminToken: "admin-token"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	err = client.RemoveServiceAccountCredential(context.Background(), "subject-1", identity.APICredentialSecret{
		AuthMethod:    identity.APICredentialAuthMethodClientSecret,
		ProviderKeyID: "client-secret",
	})
	if err != nil {
		t.Fatalf("expected already-deleted user to be ignored, got %v", err)
	}
}

func TestDeactivateServiceAccountAllowsAlreadyDeletedUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v2/users/subject-1" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 9, "message": "Errors.User.NotExisting (COMMAND-test)"})
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, AdminToken: "admin-token"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if err := client.DeactivateServiceAccount(context.Background(), "subject-1"); err != nil {
		t.Fatalf("expected already-deleted user to be ignored, got %v", err)
	}
}
