package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOIDCConfigBodies(t *testing.T) {
	browser := browserOIDCConfigBody([]string{"https://verself.sh/api/v1/auth/callback"}, []string{"https://verself.sh"}, "https://verself.sh")
	if got := browser["authMethodType"]; got != "OIDC_AUTH_METHOD_TYPE_POST" {
		t.Fatalf("browser auth method = %v", got)
	}
	if got := browser["appType"]; got != "OIDC_APP_TYPE_WEB" {
		t.Fatalf("browser app type = %v", got)
	}
	wantLogin := map[string]any{"loginV2": map[string]any{"baseUri": "https://verself.sh"}}
	if !reflect.DeepEqual(browser["loginVersion"], wantLogin) {
		t.Fatalf("browser login version = %#v", browser["loginVersion"])
	}
	native := nativeOIDCConfigBody("https://verself.sh")
	if got := native["authMethodType"]; got != "OIDC_AUTH_METHOD_TYPE_NONE" {
		t.Fatalf("native auth method = %v", got)
	}
	if got := native["appType"]; got != "OIDC_APP_TYPE_NATIVE" {
		t.Fatalf("native app type = %v", got)
	}
	if !reflect.DeepEqual(native["loginVersion"], wantLogin) {
		t.Fatalf("native login version = %#v", native["loginVersion"])
	}
}

func TestProductTokenClaimsTargetBody(t *testing.T) {
	body := productTokenClaimsTargetBody("target", "https://verself.sh/internal/zitadel/actions/product-token-claims")
	if body["name"] != "target" {
		t.Fatalf("name = %v", body["name"])
	}
	if body["endpoint"] != "https://verself.sh/internal/zitadel/actions/product-token-claims" {
		t.Fatalf("endpoint = %v", body["endpoint"])
	}
	if body["timeout"] != "1s" {
		t.Fatalf("timeout = %v", body["timeout"])
	}
}

func TestProductTokenClaimsEndpoint(t *testing.T) {
	got := productTokenClaimsEndpoint(config{
		claimsEndpointHost: "iam.api.gamma.verself.sh",
		claimsActionPath:   "/internal/zitadel/actions/product-token-claims",
	})
	want := "https://iam.api.gamma.verself.sh/internal/zitadel/actions/product-token-claims"
	if got != want {
		t.Fatalf("endpoint = %q, want %q", got, want)
	}
}

func TestReadOptionalSecret(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	if value, ok, err := readOptionalSecret(missing); err != nil || ok || value != "" {
		t.Fatalf("missing optional secret = value:%q ok:%v err:%v", value, ok, err)
	}
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if value, ok, err := readOptionalSecret(empty); err != nil || ok || value != "" {
		t.Fatalf("empty optional secret = value:%q ok:%v err:%v", value, ok, err)
	}
	present := filepath.Join(dir, "present")
	if err := os.WriteFile(present, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if value, ok, err := readOptionalSecret(present); err != nil || !ok || value != "secret" {
		t.Fatalf("present optional secret = value:%q ok:%v err:%v", value, ok, err)
	}
}

func TestSiteDomainFromTrustDomain(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "spiffe prefix", input: "spiffe.gamma.verself.sh\n", want: "gamma.verself.sh"},
		{name: "plain domain", input: "prod.verself.sh", want: "prod.verself.sh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := siteDomainFromTrustDomain(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("domain = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPasswordPolicyBodies(t *testing.T) {
	complexity := desiredPasswordComplexityPolicyBody()
	if complexity["minLength"] != 8 {
		t.Fatalf("minLength = %v", complexity["minLength"])
	}
	for _, key := range []string{"hasUppercase", "hasLowercase", "hasNumber", "hasSymbol"} {
		if complexity[key] != false {
			t.Fatalf("%s = %v", key, complexity[key])
		}
	}
	age := desiredPasswordAgePolicyBody()
	if age["maxAgeDays"] != 0 || age["expireWarnDays"] != 0 {
		t.Fatalf("age body = %#v", age)
	}
	lockout := desiredLockoutPolicyBody()
	if lockout["maxPasswordAttempts"] != 10 || lockout["maxOtpAttempts"] != 10 {
		t.Fatalf("lockout body = %#v", lockout)
	}
}

func TestOIDCConfigConverged(t *testing.T) {
	var browserLogin oidcLoginVersion
	browserLogin.LoginV2.BaseURI = "https://gamma.verself.sh"
	browser := oidcAppConfig{
		RedirectURIs:             []string{"https://gamma.verself.sh/api/v1/auth/callback"},
		ResponseTypes:            []string{"OIDC_RESPONSE_TYPE_CODE"},
		GrantTypes:               []string{"OIDC_GRANT_TYPE_AUTHORIZATION_CODE", "OIDC_GRANT_TYPE_REFRESH_TOKEN", "OIDC_GRANT_TYPE_TOKEN_EXCHANGE"},
		AuthMethodType:           "OIDC_AUTH_METHOD_TYPE_POST",
		PostLogoutRedirectURIs:   []string{"https://gamma.verself.sh"},
		AccessTokenType:          "OIDC_TOKEN_TYPE_JWT",
		IDTokenUserinfoAssertion: true,
		LoginVersion:             browserLogin,
	}
	if !browserOIDCConfigConverged(browser, []string{"https://gamma.verself.sh/api/v1/auth/callback"}, []string{"https://gamma.verself.sh"}, "https://gamma.verself.sh") {
		t.Fatal("browser OIDC config should be converged")
	}

	var nativeLogin oidcLoginVersion
	nativeLogin.LoginV2.BaseURI = "https://gamma.verself.sh"
	native := oidcAppConfig{
		ResponseTypes:            []string{"OIDC_RESPONSE_TYPE_CODE"},
		GrantTypes:               []string{"OIDC_GRANT_TYPE_DEVICE_CODE", "OIDC_GRANT_TYPE_REFRESH_TOKEN"},
		AppType:                  "OIDC_APP_TYPE_NATIVE",
		AuthMethodType:           "OIDC_AUTH_METHOD_TYPE_NONE",
		AccessTokenType:          "OIDC_TOKEN_TYPE_JWT",
		IDTokenUserinfoAssertion: true,
		LoginVersion:             nativeLogin,
	}
	if !nativeOIDCConfigConverged(native, "https://gamma.verself.sh") {
		t.Fatal("native OIDC config should be converged")
	}
}

func TestEnsureProjectRoleSkipsExistingRole(t *testing.T) {
	var gotRequests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequests = append(gotRequests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "POST /management/v1/projects/project/roles/_search":
			writeJSON(t, w, map[string]any{"result": []map[string]any{{"key": "member"}}})
		case "POST /management/v1/projects/project/roles":
			t.Fatal("existing role should not be recreated")
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := zitadelClient{baseURL: server.URL, token: "token", client: server.Client()}
	if err := client.EnsureProjectRole(context.Background(), "project", "member", "Member"); err != nil {
		t.Fatalf("EnsureProjectRole: %v", err)
	}
	wantRequests := []string{"POST /management/v1/projects/project/roles/_search"}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
}

func TestEnsurePasswordPoliciesUpdatesDrift(t *testing.T) {
	var gotRequests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequests = append(gotRequests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET /admin/v1/policies/password/complexity":
			writeJSON(t, w, map[string]any{"policy": map[string]any{"details": map[string]any{"sequence": "12"}, "minLength": "8", "hasUppercase": true, "hasLowercase": true, "hasNumber": true, "hasSymbol": true, "isDefault": true}})
		case "PUT /admin/v1/policies/password/complexity":
			assertJSONBody(t, r, desiredPasswordComplexityPolicyBody())
			writeJSON(t, w, map[string]any{"details": map[string]any{}})
		case "GET /admin/v1/policies/password/age":
			writeJSON(t, w, map[string]any{"policy": map[string]any{"details": map[string]any{"sequence": "13"}, "isDefault": true}})
		case "GET /admin/v1/policies/lockout":
			writeJSON(t, w, map[string]any{"policy": map[string]any{"details": map[string]any{"sequence": "21"}, "maxPasswordAttempts": "3", "maxOtpAttempts": "3", "isDefault": true}})
		case "PUT /admin/v1/policies/password/lockout":
			assertJSONBody(t, r, desiredLockoutPolicyBody())
			writeJSON(t, w, map[string]any{"details": map[string]any{}})
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := zitadelClient{baseURL: server.URL, token: "token", client: server.Client()}
	if err := client.EnsurePasswordPolicies(context.Background()); err != nil {
		t.Fatalf("EnsurePasswordPolicies: %v", err)
	}
	wantRequests := []string{
		"GET /admin/v1/policies/password/complexity",
		"PUT /admin/v1/policies/password/complexity",
		"GET /admin/v1/policies/password/age",
		"GET /admin/v1/policies/lockout",
		"PUT /admin/v1/policies/password/lockout",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
}

func TestPolicyIntUnmarshal(t *testing.T) {
	for _, tt := range []struct {
		name string
		raw  string
		want policyInt
	}{
		{name: "proto json string", raw: `"15"`, want: 15},
		{name: "plain json number", raw: `15`, want: 15},
		{name: "empty string", raw: `""`, want: 0},
		{name: "null", raw: `null`, want: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var got policyInt
			if err := got.UnmarshalJSON([]byte(tt.raw)); err != nil {
				t.Fatalf("UnmarshalJSON: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func assertJSONBody(t *testing.T, r *http.Request, want map[string]any) {
	t.Helper()
	var got map[string]any
	if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("encode wanted body: %v", err)
	}
	var normalizedWant map[string]any
	if err := json.Unmarshal(wantJSON, &normalizedWant); err != nil {
		t.Fatalf("normalize wanted body: %v", err)
	}
	if !reflect.DeepEqual(got, normalizedWant) {
		t.Fatalf("body = %#v, want %#v", got, normalizedWant)
	}
}
