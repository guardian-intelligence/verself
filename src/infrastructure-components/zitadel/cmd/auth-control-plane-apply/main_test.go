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
	endpoint := productTokenClaimsEndpoint(config{
		iamServiceDomain: "iam.api.verself.sh",
		claimsActionPath: "/internal/zitadel/actions/product-token-claims",
	})
	if endpoint != "https://iam.api.verself.sh/internal/zitadel/actions/product-token-claims" {
		t.Fatalf("endpoint = %v", endpoint)
	}
	body := productTokenClaimsTargetBody("target", endpoint)
	if body["name"] != "target" {
		t.Fatalf("name = %v", body["name"])
	}
	if body["endpoint"] != "https://iam.api.verself.sh/internal/zitadel/actions/product-token-claims" {
		t.Fatalf("endpoint = %v", body["endpoint"])
	}
	if body["timeout"] != "1s" {
		t.Fatalf("timeout = %v", body["timeout"])
	}
}

func TestConfigValidateRequiresIAMServiceDomain(t *testing.T) {
	cfg := config{
		zitadelBaseURL:   "http://127.0.0.1:8085",
		zitadelHost:      "verself.sh",
		adminPAT:         "token",
		verselfDomain:    "verself.sh",
		productProjectID: "verself-api",
		projectName:      "verself-api",
		browserAppName:   "verself-web",
		cliAppName:       "verself-cli",
		claimsTargetName: "verself-product-token-claims",
		claimsActionPath: "/internal/zitadel/actions/product-token-claims",
		openBaoAddr:      "https://127.0.0.1:8200",
		openBaoToken:     "token",
		publicStateDir:   t.TempDir(),
	}
	if err := cfg.validate(); err == nil {
		t.Fatalf("validate succeeded without IAM service domain")
	}
	cfg.iamServiceDomain = "iam.api.verself.sh"
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestEnsureProjectCreatesConfiguredID(t *testing.T) {
	var gotRequests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequests = append(gotRequests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "POST /zitadel.project.v2.ProjectService/GetProject":
			if r.Header.Get("Connect-Protocol-Version") != "1" {
				t.Fatalf("missing connect header")
			}
			assertJSONBody(t, r, map[string]any{"projectId": "verself-api"})
			http.Error(w, `{"code":"not_found"}`, http.StatusNotFound)
		case "GET /management/v1/orgs/me":
			writeJSON(t, w, map[string]any{"org": map[string]any{"id": "org-1"}})
		case "POST /zitadel.project.v2.ProjectService/CreateProject":
			if r.Header.Get("Connect-Protocol-Version") != "1" {
				t.Fatalf("missing connect header")
			}
			assertJSONBody(t, r, map[string]any{
				"organizationId":        "org-1",
				"projectId":             "verself-api",
				"name":                  "verself-api",
				"projectRoleAssertion":  false,
				"authorizationRequired": false,
				"projectAccessRequired": false,
			})
			writeJSON(t, w, map[string]any{"projectId": "verself-api"})
		case "POST /zitadel.project.v2.ProjectService/UpdateProject":
			if r.Header.Get("Connect-Protocol-Version") != "1" {
				t.Fatalf("missing connect header")
			}
			assertJSONBody(t, r, map[string]any{
				"projectId":             "verself-api",
				"name":                  "verself-api",
				"projectRoleAssertion":  false,
				"authorizationRequired": false,
				"projectAccessRequired": false,
			})
			writeJSON(t, w, map[string]any{})
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := zitadelClient{baseURL: server.URL, token: "token", client: server.Client()}
	project, err := client.EnsureProject(context.Background(), "verself-api", "verself-api")
	if err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	if project.ID != "verself-api" {
		t.Fatalf("project ID = %q", project.ID)
	}
	wantRequests := []string{
		"POST /zitadel.project.v2.ProjectService/GetProject",
		"GET /management/v1/orgs/me",
		"POST /zitadel.project.v2.ProjectService/CreateProject",
		"POST /zitadel.project.v2.ProjectService/UpdateProject",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
}

func TestPublishPublicAuthIdentifiers(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "verself")
	if err := os.WriteFile(manifestPath, []byte(`{"auth":{"issuer_url":"https://verself.sh"},"public_apis":{"iam":{"base_url":"https://iam.api.verself.sh"}}}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "zitadel-github-idp-id"), []byte("stale\n"), 0o644); err != nil {
		t.Fatalf("write stale idp id: %v", err)
	}
	if err := publishPublicAuthIdentifiers(publicAuthIdentifiers{
		StateDir:            dir,
		ProductProjectID:    "verself-api",
		BrowserOIDCAppID:    "browser-app",
		BrowserOIDCClientID: "browser-client",
		CLIOIDCAppID:        "cli-app",
		CLIOIDCClientID:     "cli-client",
		DiscoveryManifest:   manifestPath,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	assertFile(t, filepath.Join(dir, "auth-audience"), "verself-api\n")
	assertFile(t, filepath.Join(dir, "oidc-cli-client-id"), "cli-client\n")
	var manifest map[string]any
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	auth := manifest["auth"].(map[string]any)
	if auth["cli_client_id"] != "cli-client" || auth["product_api_audience"] != "verself-api" {
		t.Fatalf("manifest auth = %#v", auth)
	}
	publicAPIs := manifest["public_apis"].(map[string]any)
	if _, ok := publicAPIs["iam"]; !ok {
		t.Fatalf("manifest public apis = %#v", publicAPIs)
	}
	if _, err := os.Stat(filepath.Join(dir, "zitadel-github-idp-id")); !os.IsNotExist(err) {
		t.Fatalf("github idp stale file err = %v, want not exist", err)
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

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
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
