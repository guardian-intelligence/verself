package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	verself "github.com/verself/verself-go"
)

func TestCLIErrorMessageIncludesAPIProblemMetadata(t *testing.T) {
	got := cliErrorMessage(&verself.APIError{
		Service:     "IAM API",
		Operation:   "verify signup",
		StatusCode:  http.StatusConflict,
		Detail:      "signup verification was already used",
		Code:        "iam.signup.verification.already_used",
		RequestID:   "req_test",
		Traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	})
	for _, want := range []string{
		"IAM API verify signup failed with HTTP 409: signup verification was already used",
		"code=iam.signup.verification.already_used",
		"requestId=req_test",
		"traceparent=00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("cli error message missing %q: %s", want, got)
		}
	}
}

func TestAuthCommandErrorLinksPublicDocsAnchor(t *testing.T) {
	got := authCommandError("auth.session_revoked", "device session is expired or revoked").Error()
	want := "https://verself.sh/docs/reference/iam/errors#auth-session-revoked"
	if !strings.Contains(got, want) {
		t.Fatalf("auth error missing docs link %q: %s", want, got)
	}
}

func TestBootstrapRendersEncryptedCompanySiteArtifacts(t *testing.T) {
	requireTool(t, "age-keygen")
	requireTool(t, "sops")

	xdgRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(xdgRoot, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(xdgRoot, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(xdgRoot, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(xdgRoot, "cache"))

	runCLI(t, nil,
		"company", "configure", "guardian",
		"--site", "prod",
		"--product-domain", "guardian.example",
		"--company-domain", "guardianintelligence.org",
		"--company-name", "Guardian Intelligence",
		"--owner-alias", "shovon",
		"--owner-name", "Shovon Hasan",
		"--cli-name", "guardian",
	)

	t.Setenv("LATITUDESH_AUTH_TOKEN", "lat_test_guardian")
	runCLI(t, nil, "company", "options", "add", "guardian", "latitude.api_token", "--from-env", "LATITUDESH_AUTH_TOKEN")
	runCLI(t, nil, "company", "options", "set", "guardian", "latitude.project_id", "--value", "proj_guardian")
	runCLI(t, nil, "company", "options", "set", "guardian", "latitude.region", "--value", "ASH")
	runCLI(t, nil, "company", "options", "set", "guardian", "latitude.plan", "--value", "f4-metal-medium")

	t.Setenv("STRIPE_SECRET_KEY", "sk_test_guardian")
	runCLI(t, nil, "company", "options", "add", "guardian", "stripe.secret_key", "--from-env", "STRIPE_SECRET_KEY")
	runCLI(t, nil, "company", "options", "set", "guardian", "stripe.publishable_key", "--value", "pk_test_guardian")

	repoRoot := t.TempDir()
	runCLI(t, nil, "bootstrap", "--company", "guardian", "--repo-root", repoRoot, "--json")

	var out bytes.Buffer
	runCLI(t, &out,
		"env", "get", rootSOPSKeyName,
		"--org", "guardianintelligence.org",
		"--project", "verself",
		"--environment", "bootstrap",
		"--reveal-secret",
	)
	identity := strings.TrimSpace(out.String())
	if !strings.HasPrefix(identity, "AGE-SECRET-KEY-") {
		t.Fatalf("env get returned non-Age identity: %q", identity)
	}

	assertFileContains(t, filepath.Join(repoRoot, ".verself", "bootstrap", "manifest.yaml"), []string{
		`owner_email: "shovon@guardianintelligence.org"`,
		`organization_name: "guardianintelligence.org"`,
		`recipient: "age1`,
		`decoupled_from_verself_sh: true`,
		`render_targets:`,
	})
	assertFileContains(t, filepath.Join(repoRoot, "src", "host", "sites", "prod", "vars.yml"), []string{
		`platform_owner_email: "shovon@guardianintelligence.org"`,
		`platform_organization_name: "guardianintelligence.org"`,
		`bootstrap_runtime_substrate: customer_latitude_bare_metal`,
	})
	assertFileContains(t, filepath.Join(repoRoot, "README.md"), []string{
		"bazelisk build //src/guardian-cli:guardian",
		"guardian env get VERSELF_SOPS_AGE_IDENTITY --org guardianintelligence.org --project verself --environment bootstrap",
		"aspect deploy --site=prod --sha=HEAD",
	})

	provisioningBag := filepath.Join(repoRoot, "src", "host", "sites", "prod", "secrets", "provisioning.sops.yml")
	raw := readFile(t, provisioningBag)
	if strings.Contains(raw, "lat_test_guardian") {
		t.Fatalf("encrypted provisioning bag contains plaintext Latitude token")
	}
	plain := decryptSOPS(t, repoRoot, provisioningBag, identity)
	for _, want := range []string{
		`latitude_api_token: lat_test_guardian`,
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("decrypted provisioning bag missing %q:\n%s", want, plain)
		}
	}

	hostBag := filepath.Join(repoRoot, "src", "host", "sites", "prod", "secrets", "host.sops.yml")
	hostPlain := decryptSOPS(t, repoRoot, hostBag, identity)
	for _, want := range []string{
		"zitadel_initial_admin_password:",
		"forgejo_initial_admin_password:",
	} {
		if !strings.Contains(hostPlain, want) {
			t.Fatalf("decrypted host bag missing %q:\n%s", want, hostPlain)
		}
	}

	externalBag := filepath.Join(repoRoot, "src", "host", "sites", "prod", "secrets", "external.sops.yml")
	externalRaw := readFile(t, externalBag)
	if strings.Contains(externalRaw, "sk_test_guardian") {
		t.Fatalf("encrypted external bag contains plaintext Stripe secret")
	}
	externalPlain := decryptSOPS(t, repoRoot, externalBag, identity)
	for _, want := range []string{
		`stripe_secret_key: sk_test_guardian`,
		`stripe_publishable_key: pk_test_guardian`,
		"billing_cookie_signing_key:",
		"stripe_webhook_secret:",
	} {
		if !strings.Contains(externalPlain, want) {
			t.Fatalf("decrypted external bag missing %q:\n%s", want, externalPlain)
		}
	}

	if found, path := treeContains(t, repoRoot, "AGE-SECRET-KEY-"); found {
		t.Fatalf("rendered repository contains private Age identity in %s", path)
	}
	if found, path := treeContains(t, repoRoot, "verself-cred://"); found {
		t.Fatalf("rendered repository contains local credential ref in %s", path)
	}

	overrideRepoRoot := t.TempDir()
	runCLI(t, nil,
		"bootstrap",
		"--company", "guardian",
		"--repo-root", overrideRepoRoot,
		"--set", "latitude.region=DFW",
	)
	assertFileContains(t, filepath.Join(overrideRepoRoot, "src", "host", "sites", "prod", "provisioning.tfvars.json.template"), []string{
		`"region": "DFW"`,
	})

	var companyJSON bytes.Buffer
	runCLI(t, &companyJSON, "company", "inspect", "guardian", "--json")
	if strings.Contains(companyJSON.String(), `"value": "DFW"`) {
		t.Fatalf("one-run bootstrap override persisted into company record:\n%s", companyJSON.String())
	}
}

func TestAuthSignupCommandsUsePublicIAMAPI(t *testing.T) {
	const (
		signupIntentID    = "signup_01J8QJ4P1R7S9W2X5M6N8P0Q2"
		verificationToken = "signup-verification-token-0000000001"
		orgID             = "org_01J8QJ4P1R7S9W2X5M6N8P0Q2"
		traceparent       = "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01"
	)
	startAuths := map[string]string{}
	startBodies := map[string]map[string]any{}
	startIdempotencyKeys := map[string]string{}
	startTraceparents := map[string]string{}
	var verifyKey string
	var verifyAuth string
	var verifyBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/signup-intents":
			startKey := r.Header.Get("Idempotency-Key")
			var startBody map[string]any
			if err := json.NewDecoder(r.Body).Decode(&startBody); err != nil {
				t.Fatal(err)
			}
			email, _ := startBody["email"].(string)
			startTraceparents[email] = r.Header.Get("Traceparent")
			startAuths[email] = r.Header.Get("Authorization")
			startIdempotencyKeys[email] = startKey
			startBodies[email] = startBody
			w.WriteHeader(http.StatusAccepted)
			if err := json.NewEncoder(w).Encode(map[string]any{
				"message":               "Check your email to continue.",
				"status":                "accepted",
				"verificationExpiresAt": "2026-05-25T00:00:00Z",
			}); err != nil {
				t.Fatal(err)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/signup-intents/"+signupIntentID+"/verification":
			verifyKey = r.Header.Get("Idempotency-Key")
			verifyAuth = r.Header.Get("Authorization")
			if err := json.NewDecoder(r.Body).Decode(&verifyBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"organization":{"orgId":"` + orgID + `","resourceName":"urn:verself:iam:organization:` + orgID + `","displayName":"Guardian Intelligence","slug":"guardian-intelligence","version":1},"loginUrl":"https://verself.sh/login"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("VERSELF_IAM_API_URL", server.URL)

	var defaultStartOut bytes.Buffer
	runCLI(t, &defaultStartOut,
		"auth", "signup",
		"--email", "my-email@email.com",
	)
	if startBodies["my-email@email.com"]["organizationDisplayName"] != "My Email" {
		t.Fatalf("default signup organization display name = %#v", startBodies["my-email@email.com"])
	}
	var defaultStarted signupStartOutput
	if err := json.Unmarshal(defaultStartOut.Bytes(), &defaultStarted); err != nil {
		t.Fatalf("decode default start output: %v\n%s", err, defaultStartOut.String())
	}
	if defaultStarted.Status != "accepted" || defaultStarted.VerificationExpiresAt != "2026-05-25T00:00:00Z" {
		t.Fatalf("unexpected default start output: %#v", defaultStarted)
	}

	var startOut bytes.Buffer
	runCLI(t, &startOut,
		"auth", "signup",
		"--email", "operator@example.test",
		"--org", "Guardian Intelligence",
		"--slug", "guardian-intelligence",
		"--given-name", "Operator",
		"--family-name", "Example",
		"--traceparent", traceparent,
	)
	if startAuths["operator@example.test"] != "" {
		t.Fatalf("start Authorization = %q", startAuths["operator@example.test"])
	}
	if startTraceparents["operator@example.test"] != traceparent {
		t.Fatalf("unexpected start traceparent=%q", startTraceparents["operator@example.test"])
	}
	if startIdempotencyKeys["operator@example.test"] == "" {
		t.Fatalf("start idempotency key was not derived")
	}
	startBody := startBodies["operator@example.test"]
	if startBody["email"] != "operator@example.test" ||
		startBody["organizationDisplayName"] != "Guardian Intelligence" ||
		startBody["organizationSlug"] != "guardian-intelligence" ||
		startBody["givenName"] != "Operator" ||
		startBody["familyName"] != "Example" {
		t.Fatalf("unexpected start body: %#v", startBody)
	}
	var started signupStartOutput
	if err := json.Unmarshal(startOut.Bytes(), &started); err != nil {
		t.Fatalf("decode start output: %v\n%s", err, startOut.String())
	}
	if started.Message != "Check your email to continue." ||
		started.Status != "accepted" ||
		started.VerificationExpiresAt != "2026-05-25T00:00:00Z" {
		t.Fatalf("unexpected start output: %#v", started)
	}

	verifyURL := "https://verself.sh/signup/verify?signup_intent_id=" + signupIntentID + "&verification_token=" + verificationToken
	var verifyOut bytes.Buffer
	runCLI(t, &verifyOut,
		"auth", "signup", "verify",
		"--url", verifyURL,
		"--org", "Guardian Intelligence",
		"--slug", "guardian-intelligence",
	)
	if verifyAuth != "" {
		t.Fatalf("verify Authorization = %q", verifyAuth)
	}
	if verifyKey == "" {
		t.Fatalf("verify idempotency key was not derived")
	}
	if verifyBody["verificationToken"] != verificationToken {
		t.Fatalf("unexpected verification token body: %#v", verifyBody)
	}
	if verifyBody["organizationDisplayName"] != "Guardian Intelligence" || verifyBody["organizationSlug"] != "guardian-intelligence" {
		t.Fatalf("unexpected verification organization body: %#v", verifyBody)
	}
	var verified signupVerifyOutput
	if err := json.Unmarshal(verifyOut.Bytes(), &verified); err != nil {
		t.Fatalf("decode verify output: %v\n%s", err, verifyOut.String())
	}
	if verified.Organization.OrgID != orgID ||
		verified.Organization.DisplayName != "Guardian Intelligence" ||
		verified.LoginURL != "https://verself.sh/login" ||
		!strings.Contains(verified.Message, "verself auth login") {
		t.Fatalf("unexpected verify output: %#v", verified)
	}
}

func TestProjectsCommandsUseSDKBackedAPI(t *testing.T) {
	var createIDKey string
	var createAuth string
	var updateBody map[string]any
	var archiveBody map[string]any
	var restoreBody map[string]any
	var environmentCreateKey string
	var environmentUpdateBody map[string]any
	var environmentArchiveBody map[string]any
	const projectID = "11111111-1111-1111-1111-111111111111"
	const createdProjectID = "22222222-2222-2222-2222-222222222222"
	const environmentID = "33333333-3333-3333-3333-333333333333"
	projectJSON := func(id, slug, displayName, description, state, version string) string {
		return `{"project_id":"` + id + `","org_id":"370200542594579812","slug":"` + slug + `","display_name":"` + displayName + `","description":"` + description + `","state":"` + state + `","version":"` + version + `","created_by":"user","updated_by":"user","created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:00:00Z"}`
	}
	environmentJSON := func(slug, displayName, kind, state, version string) string {
		return `{"environment_id":"` + environmentID + `","project_id":"` + projectID + `","org_id":"370200542594579812","slug":"` + slug + `","display_name":"` + displayName + `","kind":"` + kind + `","state":"` + state + `","version":"` + version + `","created_by":"user","updated_by":"user","created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:00:00Z"}`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects":
			if r.Header.Get("Authorization") != "Bearer tok_test" {
				t.Fatalf("list Authorization = %q", r.Header.Get("Authorization"))
			}
			if r.URL.Query().Get("state") != "active" {
				t.Fatalf("list query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"projects":[` + projectJSON(projectID, "api", "API", "", "active", "1") + `]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects":
			createIDKey = r.Header.Get("Idempotency-Key")
			createAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(projectJSON(createdProjectID, "web", "Web", "Console", "active", "1")))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/"+projectID:
			_, _ = w.Write([]byte(projectJSON(projectID, "api", "API", "", "active", "1")))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/projects/"+projectID:
			if r.Header.Get("Idempotency-Key") != "project:update" {
				t.Fatalf("update idempotency key = %q", r.Header.Get("Idempotency-Key"))
			}
			if err := json.NewDecoder(r.Body).Decode(&updateBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(projectJSON(projectID, "api-core", "API Core", "Core", "active", "2")))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/"+projectID+"/archive":
			if err := json.NewDecoder(r.Body).Decode(&archiveBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(projectJSON(projectID, "api-core", "API Core", "Core", "archived", "3")))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/"+projectID+"/restore":
			if err := json.NewDecoder(r.Body).Decode(&restoreBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(projectJSON(projectID, "api-core", "API Core", "Core", "active", "4")))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/"+projectID+"/environments":
			_, _ = w.Write([]byte(`{"environments":[` + environmentJSON("production", "Production", "production", "active", "1") + `]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/"+projectID+"/environments":
			environmentCreateKey = r.Header.Get("Idempotency-Key")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(environmentJSON("staging", "Staging", "custom", "active", "1")))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/projects/"+projectID+"/environments/"+environmentID:
			if err := json.NewDecoder(r.Body).Decode(&environmentUpdateBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(environmentJSON("staging", "Staging 2", "custom", "active", "2")))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/"+projectID+"/environments/"+environmentID+"/archive":
			if err := json.NewDecoder(r.Body).Decode(&environmentArchiveBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(environmentJSON("staging", "Staging 2", "custom", "archived", "3")))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("VERSELF_TOKEN", "tok_test")
	t.Setenv("VERSELF_PROJECTS_API_URL", server.URL)

	var listOut bytes.Buffer
	runCLI(t, &listOut, "projects", "list", "--state", "active")
	if !strings.Contains(listOut.String(), "api\t11111111-1111-1111-1111-111111111111\tAPI") {
		t.Fatalf("projects list output:\n%s", listOut.String())
	}

	var createOut bytes.Buffer
	runCLI(t, &createOut, "projects", "create", "Web", "--slug", "web", "--description", "Console", "--idempotency-key", "project:test")
	if createAuth != "Bearer tok_test" {
		t.Fatalf("create Authorization = %q", createAuth)
	}
	if createIDKey != "project:test" {
		t.Fatalf("create idempotency key = %q", createIDKey)
	}
	if !strings.Contains(createOut.String(), "web\t22222222-2222-2222-2222-222222222222\tWeb") {
		t.Fatalf("projects create output:\n%s", createOut.String())
	}

	var getOut bytes.Buffer
	runCLI(t, &getOut, "projects", "get", projectID)
	if !strings.Contains(getOut.String(), "api\t"+projectID+"\tAPI") {
		t.Fatalf("projects get output:\n%s", getOut.String())
	}

	var updateOut bytes.Buffer
	runCLI(t, &updateOut, "projects", "update", projectID, "--version", "1", "--slug", "api-core", "--display-name", "API Core", "--description", "Core", "--idempotency-key", "project:update")
	if updateBody["version"] != "1" || updateBody["slug"] != "api-core" || updateBody["display_name"] != "API Core" || updateBody["description"] != "Core" {
		t.Fatalf("unexpected update body: %#v", updateBody)
	}
	if !strings.Contains(updateOut.String(), "api-core\t"+projectID+"\tAPI Core") {
		t.Fatalf("projects update output:\n%s", updateOut.String())
	}

	runCLI(t, nil, "projects", "archive", projectID, "--version", "2", "--idempotency-key", "project:archive")
	if archiveBody["version"] != "2" {
		t.Fatalf("unexpected archive body: %#v", archiveBody)
	}
	runCLI(t, nil, "projects", "restore", projectID, "--version", "3", "--idempotency-key", "project:restore")
	if restoreBody["version"] != "3" {
		t.Fatalf("unexpected restore body: %#v", restoreBody)
	}

	var environmentsOut bytes.Buffer
	runCLI(t, &environmentsOut, "projects", "environments", "list", projectID)
	if !strings.Contains(environmentsOut.String(), "production\t"+environmentID+"\tproduction\tProduction") {
		t.Fatalf("projects environments list output:\n%s", environmentsOut.String())
	}
	var environmentCreateOut bytes.Buffer
	runCLI(t, &environmentCreateOut, "projects", "environments", "create", projectID, "Staging", "--slug", "staging", "--kind", "custom", "--idempotency-key", "project-environment:create")
	if environmentCreateKey != "project-environment:create" {
		t.Fatalf("environment create idempotency key = %q", environmentCreateKey)
	}
	if !strings.Contains(environmentCreateOut.String(), "staging\t"+environmentID+"\tcustom\tStaging") {
		t.Fatalf("projects environments create output:\n%s", environmentCreateOut.String())
	}
	runCLI(t, nil, "projects", "environments", "update", projectID, environmentID, "--version", "1", "--display-name", "Staging 2", "--policy", "deploy=manual", "--idempotency-key", "project-environment:update")
	if environmentUpdateBody["version"] != "1" || environmentUpdateBody["display_name"] != "Staging 2" {
		t.Fatalf("unexpected environment update body: %#v", environmentUpdateBody)
	}
	policy, ok := environmentUpdateBody["protection_policy"].(map[string]any)
	if !ok || policy["deploy"] != "manual" {
		t.Fatalf("unexpected environment update policy: %#v", environmentUpdateBody)
	}
	runCLI(t, nil, "projects", "environments", "archive", projectID, environmentID, "--version", "2", "--idempotency-key", "project-environment:archive")
	if environmentArchiveBody["version"] != "2" {
		t.Fatalf("unexpected environment archive body: %#v", environmentArchiveBody)
	}
}

func TestAuditCommandsUseSDKBackedAPI(t *testing.T) {
	const exportID = "11111111-1111-1111-1111-111111111111"
	const traceparent = "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01"
	var createKey string
	var createBody map[string]any
	exportJSON := `{"export_id":"` + exportID + `","resourceName":"urn:verself:governance:data_export:` + exportID + `","org_id":"370200542594579812","requested_by":"user_1","scopes":["api_activity"],"include_logs":true,"format":"tar.gz","state":"ready","artifact_sha256":"sha256","artifact_bytes":"10","download_url":"/api/v1/governance/exports/` + exportID + `/download","files":[{"path":"governance/ocsf_api_activities.jsonl","content_type":"application/jsonl","rows":"1","bytes":"10","sha256":"file_sha256"}],"created_at":"2026-05-07T00:00:00Z","updated_at":"2026-05-07T00:00:01Z","completed_at":"2026-05-07T00:00:01Z","expires_at":"2026-05-08T00:00:00Z"}`
	apiActivityJSON := `{"metadata_uid":"22222222-2222-2222-2222-222222222222","time":"2026-05-07T00:00:01Z","org_id":"370200542594579812","sequence":"1","ocsf_version":"1.5.0","category_uid":6,"category_name":"Application Activity","class_uid":6003,"class_name":"API Activity","type_uid":600301,"activity_id":1,"activity_name":"Create","action_id":1,"action":"Allowed","status_id":1,"status":"Success","status_code":"200","severity_id":1,"severity":"Informational","api_service":"governance-service","api_operation":"create-data-export","actor_type":"user","actor_uid":"user_1","credential_uid":"cred_1","primary_resource_type":"data_export","primary_resource_uid":"` + exportID + `","primary_resource_full_name":"urn:verself:governance:data_export:` + exportID + `","permission":"governance.exports.write","http_method":"POST","http_route":"/api/v1/governance/exports","http_response_code":201,"trace_uid":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","span_uid":"bbbbbbbbbbbbbbbb","ocsf_sha256":"hash","prev_hmac":"prev","row_hmac":"row"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok_governance" {
			t.Fatalf("%s %s Authorization = %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/governance/ocsf/api-activities":
			if r.Header.Get("Traceparent") != traceparent {
				t.Fatalf("api activities Traceparent = %q", r.Header.Get("Traceparent"))
			}
			if r.URL.Query().Get("limit") != "2" || r.URL.Query().Get("api_service") != "governance-service" {
				t.Fatalf("api activities query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"api_activities":[` + apiActivityJSON + `],"filters":{"api_service":"governance-service"},"limit":2}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/governance/exports":
			_, _ = w.Write([]byte(`{"exports":[` + exportJSON + `]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/governance/exports":
			createKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"export":` + exportJSON + `}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/governance/exports/"+exportID:
			_, _ = w.Write([]byte(`{"export":` + exportJSON + `}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/governance/exports/"+exportID+"/download":
			if !strings.Contains(r.Header.Get("Accept"), "application/gzip") {
				t.Fatalf("download Accept = %q", r.Header.Get("Accept"))
			}
			w.Header().Set("Content-Type", "application/gzip")
			w.Header().Set("Content-Disposition", `attachment; filename="governance-export.tar.gz"`)
			_, _ = w.Write([]byte("gzip-bytes"))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("VERSELF_TOKEN", "tok_governance")
	t.Setenv("VERSELF_GOVERNANCE_API_URL", server.URL)

	var activitiesOut bytes.Buffer
	runCLI(t, &activitiesOut, "audit", "api-activities", "--limit", "2", "--api-service", "governance-service", "--traceparent", traceparent)
	if !strings.Contains(activitiesOut.String(), "api_activity\t22222222-2222-2222-2222-222222222222\t2026-05-07T00:00:01Z\tSuccess\tgovernance-service\tcreate-data-export") {
		t.Fatalf("audit api-activities output:\n%s", activitiesOut.String())
	}

	var exportsOut bytes.Buffer
	runCLI(t, &exportsOut, "audit", "exports", "list")
	if !strings.Contains(exportsOut.String(), "ready\t"+exportID+"\tapi_activity") {
		t.Fatalf("audit exports list output:\n%s", exportsOut.String())
	}

	var createOut bytes.Buffer
	runCLI(t, &createOut, "audit", "exports", "create", "--scope", "api_activity", "--include-logs", "--idempotency-key", "governance-export:test")
	if createKey != "governance-export:test" || createBody["include_logs"] != true {
		t.Fatalf("unexpected export create: %q %#v", createKey, createBody)
	}
	if !strings.Contains(createOut.String(), "ready\t"+exportID+"\tapi_activity") {
		t.Fatalf("audit exports create output:\n%s", createOut.String())
	}

	artifactPath := filepath.Join(t.TempDir(), "export.tar.gz")
	var downloadOut bytes.Buffer
	runCLI(t, &downloadOut, "audit", "exports", "download", exportID, artifactPath, "--json")
	if got := readFile(t, artifactPath); got != "gzip-bytes" {
		t.Fatalf("downloaded artifact = %q", got)
	}
	if !strings.Contains(downloadOut.String(), `"file_name": "governance-export.tar.gz"`) {
		t.Fatalf("audit exports download output:\n%s", downloadOut.String())
	}
}

func TestNotificationsCommandsUseSDKBackedAPI(t *testing.T) {
	const notificationID = "11111111-1111-1111-1111-111111111111"
	notificationJSON := func(sequence string) string {
		return `{"notification_id":"` + notificationID + `","org_id":"370200542594579812","recipient_subject_id":"user_1","recipient_sequence":"` + sequence + `","kind":"test","priority":"normal","title":"Smoke","body":"Body","action_url":"https://verself.sh","resource_kind":"run","resource_id":"run_1","created_at":"2026-05-07T00:00:00Z"}`
	}
	summaryJSON := func(readUpTo, latest string) string {
		return `{"org_id":"370200542594579812","subject_id":"user_1","unread_count":1,"latest_sequence":"` + latest + `","read_up_to_sequence":"` + readUpTo + `","preferences":{"enabled":true,"web_enabled":true,"email_enabled":false,"push_enabled":false,"sms_enabled":false,"version":1,"updated_at":"2026-05-07T00:00:00Z","updated_by":"user_1"},"latest_notification":` + notificationJSON(latest) + `}`
	}
	var listTraceparent string
	var preferencesKey string
	var preferencesBody map[string]any
	var readCursorKey string
	var readCursorBody map[string]any
	var readKey string
	var dismissKey string
	var clearKey string
	var testKey string
	var testBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer tok_notifications" {
			t.Fatalf("%s %s Authorization = %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/notifications":
			listTraceparent = r.Header.Get("Traceparent")
			if r.URL.Query().Get("limit") != "2" {
				t.Fatalf("notifications list query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"summary":` + summaryJSON("0", "2") + `,"notifications":[` + notificationJSON("2") + `]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/notifications/summary":
			_, _ = w.Write([]byte(summaryJSON("0", "2")))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/notifications/preferences":
			preferencesKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&preferencesBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(summaryJSON("0", "2")))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/notifications/read-cursor":
			readCursorKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&readCursorBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(summaryJSON("2", "2")))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/notifications/"+notificationID+"/read":
			readKey = r.Header.Get("Idempotency-Key")
			_, _ = w.Write([]byte(summaryJSON("2", "2")))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/notifications/"+notificationID+"/dismiss":
			dismissKey = r.Header.Get("Idempotency-Key")
			_, _ = w.Write([]byte(summaryJSON("2", "2")))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/notifications/clear":
			clearKey = r.Header.Get("Idempotency-Key")
			_, _ = w.Write([]byte(summaryJSON("2", "2")))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/notifications/test":
			testKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&testBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"event_id":"evt_1","traceparent":"trace-child"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("VERSELF_TOKEN", "tok_notifications")
	t.Setenv("VERSELF_NOTIFICATIONS_API_URL", server.URL)

	traceparent := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	var listOut bytes.Buffer
	runCLI(t, &listOut, "notifications", "list", "--limit", "2", "--traceparent", traceparent)
	if listTraceparent != traceparent {
		t.Fatalf("notifications list traceparent = %q", listTraceparent)
	}
	if !strings.Contains(listOut.String(), "notification\t"+notificationID+"\tnormal\ttest\t2\tSmoke") {
		t.Fatalf("notifications list output:\n%s", listOut.String())
	}

	var summaryOut bytes.Buffer
	runCLI(t, &summaryOut, "notifications", "summary", "--json")
	if !strings.Contains(summaryOut.String(), `"unread_count": 1`) {
		t.Fatalf("notifications summary output:\n%s", summaryOut.String())
	}

	runCLI(t, nil, "notifications", "preferences", "set", "--version", "1", "--enabled", "false", "--web-enabled", "true", "--email-enabled", "false", "--idempotency-key", "notifications:preferences")
	if preferencesKey != "notifications:preferences" || preferencesBody["enabled"] != false || preferencesBody["web_enabled"] != true || preferencesBody["email_enabled"] != false {
		t.Fatalf("unexpected preferences mutation: key=%q body=%#v", preferencesKey, preferencesBody)
	}

	runCLI(t, nil, "notifications", "read-cursor", "2", "--idempotency-key", "notifications:cursor")
	if readCursorKey != "notifications:cursor" || readCursorBody["read_up_to_sequence"] != "2" {
		t.Fatalf("unexpected read cursor mutation: key=%q body=%#v", readCursorKey, readCursorBody)
	}

	runCLI(t, nil, "notifications", "read", notificationID, "--idempotency-key", "notifications:read")
	runCLI(t, nil, "notifications", "dismiss", notificationID, "--idempotency-key", "notifications:dismiss")
	runCLI(t, nil, "notifications", "clear", "--idempotency-key", "notifications:clear")
	var testOut bytes.Buffer
	runCLI(t, &testOut, "notifications", "test", "--title", "CLI", "--body", "Body", "--action-url", "https://verself.sh/runs/1", "--idempotency-key", "notifications:test")
	if readKey != "notifications:read" || dismissKey != "notifications:dismiss" || clearKey != "notifications:clear" || testKey != "notifications:test" {
		t.Fatalf("unexpected mutation keys: read=%q dismiss=%q clear=%q test=%q", readKey, dismissKey, clearKey, testKey)
	}
	if testBody["title"] != "CLI" || testBody["body"] != "Body" || testBody["action_url"] != "https://verself.sh/runs/1" {
		t.Fatalf("unexpected test notification body: %#v", testBody)
	}
	if !strings.Contains(testOut.String(), "accepted\tevt_1\ttrace-child") {
		t.Fatalf("notifications test output:\n%s", testOut.String())
	}
}

func TestEnvCommandsUseSecretsSDKBackedAPI(t *testing.T) {
	var putBody map[string]any
	var resolveNamedBody map[string]any
	var resolveAllBody map[string]any
	var deleteKey string
	var putKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer tok_secret" {
			t.Fatalf("%s %s Authorization = %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/secrets/API_TOKEN":
			putKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&putBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"secret_id":"sec_1","kind":"secret","name":"API_TOKEN","scope_level":"environment","source_id":"proj_1","env_id":"production","current_version":"1","created_at":"2026-05-07T00:00:00Z","updated_at":"2026-05-07T00:00:00Z"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/secrets:resolve":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if _, ok := body["names"]; ok {
				resolveNamedBody = body
				_, _ = w.Write([]byte(`{"values":[{"secret_id":"sec_1","kind":"secret","name":"API_TOKEN","scope_level":"environment","source_id":"proj_1","env_id":"production","current_version":"1","created_at":"2026-05-07T00:00:00Z","updated_at":"2026-05-07T00:00:00Z","value":"tok_live"}]}`))
				return
			}
			resolveAllBody = body
			_, _ = w.Write([]byte(`{"values":[{"secret_id":"sec_1","kind":"secret","name":"API_TOKEN","scope_level":"environment","source_id":"proj_1","env_id":"production","current_version":"1","created_at":"2026-05-07T00:00:00Z","updated_at":"2026-05-07T00:00:00Z","value":"tok_live"}]}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/secrets/API_TOKEN":
			deleteKey = r.Header.Get("Idempotency-Key")
			if r.URL.Query().Get("scope_level") != "environment" || r.URL.Query().Get("source_id") != "proj_1" || r.URL.Query().Get("env_id") != "production" {
				t.Fatalf("delete query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"secret_id":"sec_1","kind":"secret","name":"API_TOKEN","scope_level":"environment","source_id":"proj_1","env_id":"production","current_version":"1","created_at":"2026-05-07T00:00:00Z","updated_at":"2026-05-07T00:00:00Z"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("VERSELF_TOKEN", "tok_secret")
	t.Setenv("VERSELF_SECRETS_API_URL", server.URL)
	t.Setenv("API_TOKEN_VALUE", "tok_live")

	runCLI(t, nil,
		"env", "add", "API_TOKEN",
		"--org", "guardian",
		"--project", "proj_1",
		"--environment", "production",
		"--from-env", "API_TOKEN_VALUE",
		"--idempotency-key", "secret:add",
	)
	if putKey != "secret:add" {
		t.Fatalf("put idempotency key = %q", putKey)
	}
	if putBody["scope_level"] != "environment" || putBody["source_id"] != "proj_1" || putBody["env_id"] != "production" || putBody["value"] != "tok_live" {
		t.Fatalf("unexpected put body: %#v", putBody)
	}

	var getOut bytes.Buffer
	runCLI(t, &getOut,
		"env", "get", "API_TOKEN",
		"--org", "guardian",
		"--project", "proj_1",
		"--environment", "production",
		"--reveal-secret",
	)
	if strings.TrimSpace(getOut.String()) != "tok_live" {
		t.Fatalf("env get output:\n%s", getOut.String())
	}
	names, ok := resolveNamedBody["names"].([]any)
	if !ok || len(names) != 1 || names[0] != "API_TOKEN" || resolveNamedBody["scope_level"] != "environment" {
		t.Fatalf("unexpected named resolve body: %#v", resolveNamedBody)
	}

	pulled := filepath.Join(t.TempDir(), ".env.verself")
	runCLI(t, nil,
		"env", "pull", pulled,
		"--org", "guardian",
		"--project", "proj_1",
		"--environment", "production",
		"--yes",
	)
	pulledEnv := readFile(t, pulled)
	if !strings.Contains(pulledEnv, "API_TOKEN='tok_live'") {
		t.Fatalf("env pull output:\n%s", pulledEnv)
	}
	if resolveAllBody["scope_level"] != "environment" || resolveAllBody["source_id"] != "proj_1" || resolveAllBody["env_id"] != "production" {
		t.Fatalf("unexpected all resolve body: %#v", resolveAllBody)
	}

	runCLI(t, nil,
		"env", "rm", "API_TOKEN",
		"--org", "guardian",
		"--project", "proj_1",
		"--environment", "production",
		"--idempotency-key", "secret:rm",
	)
	if deleteKey != "secret:rm" {
		t.Fatalf("delete idempotency key = %q", deleteKey)
	}
}

func TestReposCommandsUseSourceSDKBackedAPI(t *testing.T) {
	const projectID = "11111111-1111-1111-1111-111111111111"
	const repoID = "22222222-2222-2222-2222-222222222222"
	const credentialID = "33333333-3333-3333-3333-333333333333"
	const grantID = "44444444-4444-4444-4444-444444444444"
	const workflowRunID = "55555555-5555-5555-5555-555555555555"
	repoJSON := `{"repo_id":"` + repoID + `","org_id":"370200542594579812","org_slug":"guardian","project_id":"` + projectID + `","project_slug":"api","name":"runner","description":"Builds","default_branch":"main","visibility":"private","state":"active","version":1,"backend":"forgejo","git_http_url":"https://git.example/guardian/api-runner.git","created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:00:00Z"}`
	workflowJSON := `{"workflow_run_id":"` + workflowRunID + `","org_id":"370200542594579812","project_id":"` + projectID + `","repo_id":"` + repoID + `","actor_id":"user_1","backend":"forgejo","workflow_path":".github/workflows/build.yml","ref":"main","inputs":{"target":"linux"},"state":"queued","created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:00:00Z"}`
	var createRepoKey string
	var createRepoBody map[string]any
	var credentialKey string
	var credentialBody map[string]any
	var checkoutKey string
	var checkoutBody map[string]any
	var workflowKey string
	var workflowBody map[string]any
	var treeQuery string
	var blobQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer tok_source" {
			t.Fatalf("%s %s Authorization = %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos":
			if r.URL.Query().Get("project_id") != projectID {
				t.Fatalf("repos list query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"repositories":[` + repoJSON + `]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos":
			createRepoKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&createRepoBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(repoJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/"+repoID:
			_, _ = w.Write([]byte(repoJSON))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/git-credentials":
			credentialKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&credentialBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"credential_id":"` + credentialID + `","org_id":"370200542594579812","username":"x-access-token","token":"vsrc_test","token_prefix":"vsrc_","scopes":["repo:read","repo:write"],"expires_at":"2026-05-06T01:00:00Z","created_at":"2026-05-06T00:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/"+repoID+"/refs":
			_, _ = w.Write([]byte(`{"refs":[{"name":"refs/heads/main","commit":"abc123"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/"+repoID+"/tree":
			treeQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"entries":[{"path":".github/workflows/build.yml","type":"blob","sha":"abc123","size":12}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/"+repoID+"/blob":
			blobQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"name":"README.md","path":"README.md","sha":"abc123","size":4,"encoding":"base64","content":"SGk="}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/"+repoID+"/checkout-grants":
			checkoutKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&checkoutBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"grant_id":"` + grantID + `","repo_id":"` + repoID + `","ref":"main","token":"checkout_test","expires_at":"2026-05-06T00:15:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/"+repoID+"/workflow-runs":
			_, _ = w.Write([]byte(`{"workflow_runs":[` + workflowJSON + `]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/"+repoID+"/workflow-runs":
			workflowKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&workflowBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(workflowJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/workflow-runs/"+workflowRunID:
			_, _ = w.Write([]byte(workflowJSON))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("VERSELF_TOKEN", "tok_source")
	t.Setenv("VERSELF_SOURCE_API_URL", server.URL)

	var listOut bytes.Buffer
	runCLI(t, &listOut, "repos", "list", "--project-id", projectID)
	if !strings.Contains(listOut.String(), "api/runner\t"+repoID+"\tactive") {
		t.Fatalf("repos list output:\n%s", listOut.String())
	}

	var createOut bytes.Buffer
	runCLI(t, &createOut, "repos", "create", projectID, "--description", "Builds", "--default-branch", "main", "--idempotency-key", "source:repo")
	if createRepoKey != "source:repo" {
		t.Fatalf("repo create idempotency key = %q", createRepoKey)
	}
	if createRepoBody["project_id"] != projectID || createRepoBody["description"] != "Builds" || createRepoBody["default_branch"] != "main" {
		t.Fatalf("unexpected repo create body: %#v", createRepoBody)
	}
	if !strings.Contains(createOut.String(), "api/runner\t"+repoID+"\tactive") {
		t.Fatalf("repos create output:\n%s", createOut.String())
	}

	runCLI(t, nil, "repos", "get", repoID)
	var credentialOut bytes.Buffer
	runCLI(t, &credentialOut, "repos", "credentials", "create", "--scope", "repo:read", "--scope", "repo:write", "--label", "CI", "--expires-in-seconds", "3600", "--idempotency-key", "source:credential")
	if credentialKey != "source:credential" {
		t.Fatalf("credential idempotency key = %q", credentialKey)
	}
	if credentialBody["label"] != "CI" {
		t.Fatalf("unexpected credential body: %#v", credentialBody)
	}
	scopes, ok := credentialBody["scopes"].([]any)
	if !ok || len(scopes) != 2 || scopes[0] != "repo:read" || scopes[1] != "repo:write" {
		t.Fatalf("unexpected credential scopes: %#v", credentialBody)
	}
	if !strings.Contains(credentialOut.String(), "token\tvsrc_test") {
		t.Fatalf("credential output:\n%s", credentialOut.String())
	}

	var refsOut bytes.Buffer
	runCLI(t, &refsOut, "repos", "refs", repoID)
	if !strings.Contains(refsOut.String(), "refs/heads/main\tabc123") {
		t.Fatalf("refs output:\n%s", refsOut.String())
	}
	var treeOut bytes.Buffer
	runCLI(t, &treeOut, "repos", "tree", repoID, "--ref", "main", "--path", ".github")
	if !strings.Contains(treeQuery, "ref=main") || !strings.Contains(treeQuery, "path=.github") {
		t.Fatalf("tree query = %s", treeQuery)
	}
	if !strings.Contains(treeOut.String(), "blob\tabc123\t.github/workflows/build.yml\t12") {
		t.Fatalf("tree output:\n%s", treeOut.String())
	}
	var blobOut bytes.Buffer
	runCLI(t, &blobOut, "repos", "blob", repoID, "--ref", "main", "--path", "README.md")
	if !strings.Contains(blobQuery, "ref=main") || !strings.Contains(blobQuery, "path=README.md") {
		t.Fatalf("blob query = %s", blobQuery)
	}
	if !strings.Contains(blobOut.String(), "SGk=") {
		t.Fatalf("blob output:\n%s", blobOut.String())
	}

	var checkoutOut bytes.Buffer
	runCLI(t, &checkoutOut, "repos", "checkout-grants", "create", repoID, "--ref", "main", "--idempotency-key", "source:checkout")
	if checkoutKey != "source:checkout" {
		t.Fatalf("checkout idempotency key = %q", checkoutKey)
	}
	if checkoutBody["ref"] != "main" {
		t.Fatalf("unexpected checkout body: %#v", checkoutBody)
	}
	if _, ok := checkoutBody["path_prefix"]; ok {
		t.Fatalf("checkout body must not send path_prefix: %#v", checkoutBody)
	}
	if !strings.Contains(checkoutOut.String(), "token\tcheckout_test") {
		t.Fatalf("checkout output:\n%s", checkoutOut.String())
	}

	var workflowListOut bytes.Buffer
	runCLI(t, &workflowListOut, "repos", "workflow-runs", "list", repoID)
	if !strings.Contains(workflowListOut.String(), workflowRunID+"\t"+repoID+"\tqueued") {
		t.Fatalf("workflow list output:\n%s", workflowListOut.String())
	}
	runCLI(t, nil, "repos", "workflow-runs", "dispatch", repoID, "--project-id", projectID, "--workflow-path", ".github/workflows/build.yml", "--ref", "main", "--input", "target=linux", "--idempotency-key", "source:workflow")
	if workflowKey != "source:workflow" {
		t.Fatalf("workflow idempotency key = %q", workflowKey)
	}
	if workflowBody["project_id"] != projectID || workflowBody["workflow_path"] != ".github/workflows/build.yml" || workflowBody["ref"] != "main" {
		t.Fatalf("unexpected workflow body: %#v", workflowBody)
	}
	inputs, ok := workflowBody["inputs"].(map[string]any)
	if !ok || inputs["target"] != "linux" {
		t.Fatalf("unexpected workflow inputs: %#v", workflowBody)
	}
	runCLI(t, nil, "repos", "workflow-runs", "get", workflowRunID)
}

func TestSandboxCommandsUseSDKBackedAPI(t *testing.T) {
	const executionID = "11111111-1111-1111-1111-111111111111"
	const runID = "22222222-2222-2222-2222-222222222222"
	const projectID = "33333333-3333-3333-3333-333333333333"
	const repoID = "44444444-4444-4444-4444-444444444444"
	const scheduleID = "55555555-5555-5555-5555-555555555555"
	runJSON := `{"execution_id":"` + executionID + `","run_id":"` + runID + `","org_id":"370200542594579812","actor_id":"user_1","product_id":"sandbox-ci","kind":"ci","status":"succeeded","source_kind":"github","runner_class":"linux-2vcpu","latest_attempt":{"attempt_id":"attempt_1","attempt_seq":1,"state":"succeeded","created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:01:00Z"},"created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:01:00Z"}`
	scheduleJSON := `{"schedule_id":"` + scheduleID + `","org_id":"370200542594579812","project_id":"` + projectID + `","source_repository_id":"` + repoID + `","actor_id":"user_1","display_name":"Nightly","workflow_path":".github/workflows/build.yml","ref":"main","inputs":{"target":"linux"},"interval_seconds":900,"state":"active","task_queue":"sandbox-recurring","temporal_namespace":"default","temporal_schedule_id":"verself-schedule","created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:00:00Z"}`
	analyticsWindow := `{"window_start":"2026-05-06T00:00:00Z","window_end":"2026-05-07T00:00:00Z"`
	logSearchJSON := `{"filters":{"query":"build","run_id":"` + runID + `"},"limit":1,"next_cursor":"logs_cursor","results":[{"execution_id":"` + executionID + `","attempt_id":"attempt_1","seq":1,"stream":"stdout","chunk":"build log\n","created_at":"2026-05-06T00:00:30Z","source_kind":"github"}]}`
	var createBody map[string]any
	var pauseKey string
	var resumeKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer tok_sandbox" {
			t.Fatalf("%s %s Authorization = %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/runs":
			if r.URL.Query().Get("limit") != "2" || r.URL.Query().Get("status") != "succeeded" {
				t.Fatalf("runs query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"filters":{"status":"succeeded"},"limit":2,"next_cursor":"cursor_2","runs":[` + runJSON + `]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/executions/"+executionID:
			_, _ = w.Write([]byte(runJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/executions/"+executionID+"/logs":
			_, _ = w.Write([]byte(`{"execution_id":"` + executionID + `","attempt_id":"attempt_1","logs":"build log\n"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/runs/"+runID:
			_, _ = w.Write([]byte(runJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/run-logs/search":
			if r.URL.Query().Get("limit") != "1" || r.URL.Query().Get("query") != "build" || r.URL.Query().Get("run_id") != runID {
				t.Fatalf("run logs query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(logSearchJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/run-analytics/jobs":
			_, _ = w.Write([]byte(analyticsWindow + `,"total_runs":"1","succeeded_runs":"1","failed_runs":"0","p50_duration_ms":"1000","p95_duration_ms":"1000","p99_duration_ms":"1000","by_source":[{"key":"github","count":"1"}],"by_runner_class":[],"slowest_runs":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/run-analytics/costs":
			_, _ = w.Write([]byte(analyticsWindow + `,"reserved_charge_units":"10","billed_charge_units":"9","writeoff_charge_units":"1","by_source":[],"by_runner_class":[],"by_repository":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/run-analytics/runner-sizing":
			_, _ = w.Write([]byte(analyticsWindow + `,"by_runner_class":[{"runner_class":"linux-2vcpu","run_count":"1","p95_duration_ms":"1000","avg_rootfs_provisioned_bytes":"1","avg_boot_time_us":"2","avg_block_write_bytes":"3","avg_net_tx_bytes":"4"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/execution-schedules":
			_, _ = w.Write([]byte(`{"limit":50,"next_cursor":"schedule_cursor","schedules":[` + scheduleJSON + `]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/execution-schedules":
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(scheduleJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/execution-schedules/"+scheduleID:
			_, _ = w.Write([]byte(scheduleJSON))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/execution-schedules/"+scheduleID+"/pause":
			pauseKey = r.Header.Get("Idempotency-Key")
			_, _ = w.Write([]byte(scheduleJSON))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/execution-schedules/"+scheduleID+"/resume":
			resumeKey = r.Header.Get("Idempotency-Key")
			_, _ = w.Write([]byte(scheduleJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/entitlements":
			_, _ = w.Write([]byte(`{"org_id":"370200542594579812","universal":{"scope_type":"account","product_id":"","product_display":"","bucket_id":"","bucket_display":"","sku_id":"","sku_display":"","coverage_label":"Account","available_units":"100","pending_units":"0","period_start_units":"100","spent_units":"0","sources":[]},"products":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/contracts":
			_, _ = w.Write([]byte(`{"contracts":[{"contract_id":"contract_1","product_id":"sandbox","plan_id":"sandbox-pro","cadence_kind":"monthly","status":"active","payment_state":"current","entitlement_state":"active","phase_id":"phase_1","starts_at":"2026-05-06T00:00:00Z"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/plans":
			if r.URL.Query().Get("product_id") != "sandbox" {
				t.Fatalf("plans query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"plans":[{"plan_id":"sandbox-pro","product_id":"sandbox","display_name":"Sandbox Pro","tier":"pro","billing_mode":"subscription","currency":"USD","monthly_amount_cents":"9900","annual_amount_cents":"99000","active":true,"is_default":true}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/statement":
			if r.URL.Query().Get("product_id") != "sandbox" {
				t.Fatalf("statement query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"org_id":"370200542594579812","product_id":"sandbox","period_source":"current","period_start":"2026-05-01T00:00:00Z","period_end":"2026-06-01T00:00:00Z","generated_at":"2026-05-06T00:00:00Z","currency":"USD","unit_label":"credits","totals":{"reserved_units":"0","contract_units":"100","free_tier_units":"0","promo_units":"0","purchase_units":"0","receivable_units":"0","refund_units":"0","charge_units":"5","total_due_units":"0"},"grant_summaries":[],"line_items":[]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("VERSELF_TOKEN", "tok_sandbox")
	t.Setenv("VERSELF_BILLING_API_URL", server.URL)
	t.Setenv("VERSELF_SANDBOX_API_URL", server.URL)

	var runsOut bytes.Buffer
	runCLI(t, &runsOut, "runs", "list", "--limit", "2", "--status", "succeeded")
	if !strings.Contains(runsOut.String(), executionID+"\t"+runID+"\tsucceeded\tgithub\tlinux-2vcpu") || !strings.Contains(runsOut.String(), "next_cursor\tcursor_2") {
		t.Fatalf("runs list output:\n%s", runsOut.String())
	}
	var runOut bytes.Buffer
	runCLI(t, &runOut, "runs", "get", executionID)
	if !strings.Contains(runOut.String(), executionID+"\t"+runID+"\tsucceeded") {
		t.Fatalf("runs get output:\n%s", runOut.String())
	}
	var logsOut bytes.Buffer
	runCLI(t, &logsOut, "runs", "logs", executionID)
	if logsOut.String() != "build log\n" {
		t.Fatalf("runs logs output:\n%s", logsOut.String())
	}
	var runByIDOut bytes.Buffer
	runCLI(t, &runByIDOut, "runs", "get-run", runID)
	if !strings.Contains(runByIDOut.String(), executionID+"\t"+runID+"\tsucceeded") {
		t.Fatalf("runs get-run output:\n%s", runByIDOut.String())
	}
	var logSearchOut bytes.Buffer
	runCLI(t, &logSearchOut, "runs", "search-logs", "--limit", "1", "--query", "build", "--run-id", runID)
	if !strings.Contains(logSearchOut.String(), executionID+"\tattempt_1\t1\tstdout\tbuild log") || !strings.Contains(logSearchOut.String(), "next_cursor\tlogs_cursor") {
		t.Fatalf("runs search-logs output:\n%s", logSearchOut.String())
	}
	var jobsAnalyticsOut bytes.Buffer
	runCLI(t, &jobsAnalyticsOut, "runs", "analytics", "jobs")
	if !strings.Contains(jobsAnalyticsOut.String(), "total_runs\t1") {
		t.Fatalf("runs analytics jobs output:\n%s", jobsAnalyticsOut.String())
	}
	var costsAnalyticsOut bytes.Buffer
	runCLI(t, &costsAnalyticsOut, "runs", "analytics", "costs")
	if !strings.Contains(costsAnalyticsOut.String(), "billed_charge_units\t9") {
		t.Fatalf("runs analytics costs output:\n%s", costsAnalyticsOut.String())
	}
	var sizingAnalyticsOut bytes.Buffer
	runCLI(t, &sizingAnalyticsOut, "runs", "analytics", "runner-sizing")
	if !strings.Contains(sizingAnalyticsOut.String(), "linux-2vcpu\t1\t1000") {
		t.Fatalf("runs analytics runner-sizing output:\n%s", sizingAnalyticsOut.String())
	}
	var schedulesOut bytes.Buffer
	runCLI(t, &schedulesOut, "schedules", "list")
	if !strings.Contains(schedulesOut.String(), scheduleID+"\t"+projectID+"\tactive\t900\t.github/workflows/build.yml\tNightly") || !strings.Contains(schedulesOut.String(), "next_cursor\tschedule_cursor") {
		t.Fatalf("schedules list output:\n%s", schedulesOut.String())
	}
	runCLI(t, nil,
		"schedules", "create",
		"--project-id", projectID,
		"--source-repository-id", repoID,
		"--workflow-path", ".github/workflows/build.yml",
		"--interval-seconds", "900",
		"--display-name", "Nightly",
		"--ref", "main",
		"--paused",
		"--input", "target=linux",
		"--idempotency-key", "sandbox:schedule",
	)
	if createBody["idempotency_key"] != "sandbox:schedule" || createBody["project_id"] != projectID || createBody["source_repository_id"] != repoID || createBody["paused"] != true {
		t.Fatalf("unexpected schedule create body: %#v", createBody)
	}
	runCLI(t, nil, "schedules", "get", scheduleID)
	runCLI(t, nil, "schedules", "pause", scheduleID, "--idempotency-key", "sandbox:pause")
	runCLI(t, nil, "schedules", "resume", scheduleID, "--idempotency-key", "sandbox:resume")
	if pauseKey != "sandbox:pause" || resumeKey != "sandbox:resume" {
		t.Fatalf("unexpected schedule lifecycle keys: pause=%q resume=%q", pauseKey, resumeKey)
	}

	var entitlementsOut bytes.Buffer
	runCLI(t, &entitlementsOut, "billing", "entitlements")
	if !strings.Contains(entitlementsOut.String(), "370200542594579812\t100") {
		t.Fatalf("billing entitlements output:\n%s", entitlementsOut.String())
	}
	var contractsOut bytes.Buffer
	runCLI(t, &contractsOut, "billing", "contracts")
	if !strings.Contains(contractsOut.String(), "contract_1\tsandbox\tsandbox-pro\tactive") {
		t.Fatalf("billing contracts output:\n%s", contractsOut.String())
	}
	var plansOut bytes.Buffer
	runCLI(t, &plansOut, "billing", "plans", "--product-id", "sandbox")
	if !strings.Contains(plansOut.String(), "sandbox\tsandbox-pro\tpro\t9900") {
		t.Fatalf("billing plans output:\n%s", plansOut.String())
	}
	var statementOut bytes.Buffer
	runCLI(t, &statementOut, "billing", "statement", "--product-id", "sandbox")
	if !strings.Contains(statementOut.String(), "370200542594579812\tsandbox\tcurrent\t0") {
		t.Fatalf("billing statement output:\n%s", statementOut.String())
	}
}

func TestAuthAndOrgsUseIAMSDK(t *testing.T) {
	xdgRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(xdgRoot, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(xdgRoot, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(xdgRoot, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(xdgRoot, "cache"))

	const (
		iamToken      = "tok_iam"
		secondToken   = "tok_second"
		sessionID     = "sess_01J8QK4M5N6P7Q8R9S0T1V2W3X"
		secondSession = "sess_01J8QK4M5N6P7Q8R9S0T1V2W3Y"
		accountID     = "acct_01J8QK4M5N6P7Q8R9S0T1V2W3X"
		secondAccount = "acct_01J8QK4M5N6P7Q8R9S0T1V2W3Y"
	)
	orgJSON := func(version string) string {
		return `{"orgId":"org_01J8QK0M2A7W4H3P9FQ6G1R8ZT","resourceName":"urn:verself:inst_01J8QJ4P1R7S9W2X5M6N8P0Q2A:orgs/org_01J8QK0M2A7W4H3P9FQ6G1R8ZT","displayName":"Guardian Intelligence","slug":"guardian","version":` + version + `}`
	}
	authContextJSON := func(account, session, email, subject string) string {
		return `{"account":{"accountId":"` + account + `","issuer":"https://verself.sh","subject":"` + subject + `","email":"` + email + `","emailVerified":true,"displayName":"Operator","createdAt":"2026-05-25T00:00:00Z"},"session":{"sessionId":"` + session + `","accountId":"` + account + `","channel":"cli","state":"active","current":true,"deviceLabel":"CLI","createdAt":"2026-05-25T00:00:00Z","lastSeenAt":"2026-05-25T00:00:00Z","expiresAt":"2026-05-26T00:00:00Z"},"organizations":[{"orgId":"org_01J8QK0M2A7W4H3P9FQ6G1R8ZT","displayName":"Guardian Intelligence","slug":"guardian","selected":true}],"selectedOrgId":"org_01J8QK0M2A7W4H3P9FQ6G1R8ZT","authTime":"2026-05-25T00:00:00Z","authMethods":["pwd"]}`
	}
	invitationJSON := func() string {
		return `{"orgId":"org_01J8QK0M2A7W4H3P9FQ6G1R8ZT","memberId":"member_01J8QK4M5N6P7Q8R9S0T1V2W3X","resourceName":"urn:verself:inst_01J8QJ4P1R7S9W2X5M6N8P0Q2A:orgs/org_01J8QK0M2A7W4H3P9FQ6G1R8ZT/members/member_01J8QK4M5N6P7Q8R9S0T1V2W3X","email":"invited@example.test","status":"invited","roles":["roles/admin"]}`
	}
	var createKey string
	var createBody map[string]any
	var updateKey string
	var updateBody map[string]any
	var inviteKey string
	var inviteBody map[string]any
	revokedSessions := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Header.Get("Authorization") {
		case "Bearer " + iamToken, "Bearer " + secondToken:
		default:
			t.Fatalf("%s %s Authorization = %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/api/v1/auth-context" && r.URL.Path != "/api/v1/device-sessions/"+secondSession && r.Header.Get("X-Verself-Device-Session") != sessionID {
			t.Fatalf("%s %s X-Verself-Device-Session = %q", r.Method, r.URL.Path, r.Header.Get("X-Verself-Device-Session"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth-context":
			switch r.Header.Get("Authorization") {
			case "Bearer " + iamToken:
				if r.Header.Get("X-Verself-Device-Session") != sessionID {
					t.Fatalf("auth context session header = %q", r.Header.Get("X-Verself-Device-Session"))
				}
				_, _ = w.Write([]byte(authContextJSON(accountID, sessionID, "operator@example.test", "user_iam_test")))
			case "Bearer " + secondToken:
				if r.Header.Get("X-Verself-Device-Session") != secondSession {
					t.Fatalf("second auth context session header = %q", r.Header.Get("X-Verself-Device-Session"))
				}
				_, _ = w.Write([]byte(authContextJSON(secondAccount, secondSession, "second@example.test", "user_second_test")))
			}
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/device-sessions":
			_, _ = w.Write([]byte(`{"sessions":[{"sessionId":"` + sessionID + `","accountId":"` + accountID + `","channel":"cli","state":"active","current":true,"deviceLabel":"CLI","createdAt":"2026-05-25T00:00:00Z","lastSeenAt":"2026-05-25T00:00:00Z","expiresAt":"2026-05-26T00:00:00Z"},{"sessionId":"` + secondSession + `","accountId":"` + accountID + `","channel":"browser","state":"active","current":false,"deviceLabel":"Browser","createdAt":"2026-05-25T00:00:00Z","lastSeenAt":"2026-05-25T00:00:00Z","expiresAt":"2026-05-26T00:00:00Z"}]}`))
		case r.Method == http.MethodDelete && (r.URL.Path == "/api/v1/device-sessions/"+sessionID || r.URL.Path == "/api/v1/device-sessions/"+secondSession):
			targetSession := strings.TrimPrefix(r.URL.Path, "/api/v1/device-sessions/")
			currentSession := r.Header.Get("X-Verself-Device-Session")
			if targetSession == sessionID && currentSession != sessionID {
				t.Fatalf("revoke session header = %q", r.Header.Get("X-Verself-Device-Session"))
			}
			if targetSession == secondSession && currentSession != sessionID && currentSession != secondSession {
				t.Fatalf("second revoke session header = %q", r.Header.Get("X-Verself-Device-Session"))
			}
			revokedSessions[targetSession]++
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs":
			createKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(orgJSON("1")))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs":
			_, _ = w.Write([]byte(`{"organizations":[` + orgJSON("1") + `]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/org_01J8QK0M2A7W4H3P9FQ6G1R8ZT":
			_, _ = w.Write([]byte(orgJSON("1")))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/orgs/org_01J8QK0M2A7W4H3P9FQ6G1R8ZT":
			updateKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&updateBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(orgJSON("2")))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs/org_01J8QK0M2A7W4H3P9FQ6G1R8ZT/members:invite":
			inviteKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&inviteBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(invitationJSON()))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	store, err := newStore(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	firstRef, err := store.SaveCredential(`{"access_token":"` + iamToken + `","token_type":"Bearer"}`)
	if err != nil {
		t.Fatal(err)
	}
	secondRef, err := store.SaveCredential(`{"access_token":"` + secondToken + `","token_type":"Bearer"}`)
	if err != nil {
		t.Fatal(err)
	}
	profile := ProfileRecord{Version: 1, Name: "default", ActiveAccount: accountID, IAMURL: server.URL}
	if err := store.SaveProfile(profile); err != nil {
		t.Fatal(err)
	}
	cfg, err := store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ActiveProfile = "default"
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAccount(AccountRecord{
		Version:         1,
		ProfileName:     "default",
		Handle:          accountID,
		AccountID:       accountID,
		Issuer:          "https://verself.sh",
		Subject:         "user_iam_test",
		Email:           "operator@example.test",
		DisplayName:     "Operator",
		DeviceSessionID: sessionID,
		CredentialRef:   firstRef,
		SelectedOrg:     &OrgRef{OrgID: "org_01J8QK0M2A7W4H3P9FQ6G1R8ZT", Slug: "guardian", DisplayName: "Guardian Intelligence"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAccount(AccountRecord{
		Version:         1,
		ProfileName:     "default",
		Handle:          secondAccount,
		AccountID:       secondAccount,
		Issuer:          "https://verself.sh",
		Subject:         "user_second_test",
		Email:           "second@example.test",
		DisplayName:     "Second",
		DeviceSessionID: secondSession,
		CredentialRef:   secondRef,
	}); err != nil {
		t.Fatal(err)
	}

	profilePath := filepath.Join(xdgRoot, "data", "verself", "profiles", "default.json")
	profileJSON := readFile(t, profilePath)
	if strings.Contains(profileJSON, iamToken) {
		t.Fatalf("profile stored plaintext token:\n%s", profileJSON)
	}
	if strings.Contains(profileJSON, "credential_ref") || strings.Contains(profileJSON, "selected_org") {
		t.Fatalf("profile stored account-owned fields:\n%s", profileJSON)
	}
	var accountsOut bytes.Buffer
	runCLI(t, &accountsOut, "auth", "accounts", "list")
	var accounts struct {
		Profile       string          `json:"profile"`
		ActiveAccount string          `json:"activeAccount"`
		Accounts      []AccountRecord `json:"accounts"`
	}
	if err := json.Unmarshal(accountsOut.Bytes(), &accounts); err != nil {
		t.Fatalf("decode accounts output: %v\n%s", err, accountsOut.String())
	}
	if accounts.Profile != "default" || accounts.ActiveAccount == "" || len(accounts.Accounts) != 2 {
		t.Fatalf("unexpected accounts output: %#v", accounts)
	}
	account, err := matchCLIAccount(accounts.Accounts, "operator@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if account.Email != "operator@example.test" ||
		account.Subject != "user_iam_test" ||
		account.CredentialRef == "" ||
		account.DeviceSessionID != sessionID ||
		account.SelectedOrg == nil ||
		account.SelectedOrg.OrgID != "org_01J8QK0M2A7W4H3P9FQ6G1R8ZT" {
		t.Fatalf("unexpected account record: %#v", account)
	}
	runCLI(t, nil, "auth", "accounts", "use", "operator@example.test")

	var whoami bytes.Buffer
	runCLI(t, &whoami, "auth", "whoami")
	if !strings.Contains(whoami.String(), accountID+"\toperator@example.test\torg_01J8QK0M2A7W4H3P9FQ6G1R8ZT\tGuardian Intelligence") {
		t.Fatalf("auth whoami output:\n%s", whoami.String())
	}
	var sessions bytes.Buffer
	runCLI(t, &sessions, "auth", "sessions", "list")
	if !strings.Contains(sessions.String(), sessionID+"\tactive\tcli\tCLI\tcurrent") {
		t.Fatalf("auth sessions output:\n%s", sessions.String())
	}
	runCLI(t, nil, "auth", "sessions", "revoke", secondSession)
	if revokedSessions[secondSession] != 1 {
		t.Fatalf("revoked sessions = %#v", revokedSessions)
	}

	runCLI(t, nil, "orgs", "use", "guardian")
	var orgsOut bytes.Buffer
	runCLI(t, &orgsOut, "orgs", "list")
	if !strings.Contains(orgsOut.String(), "guardian\torg_01J8QK0M2A7W4H3P9FQ6G1R8ZT") {
		t.Fatalf("orgs list output:\n%s", orgsOut.String())
	}
	runCLI(t, nil, "orgs", "update", "--version", "1", "--display-name", "Guardian Intelligence", "--idempotency-key", "iam:test")
	if updateKey != "iam:test" {
		t.Fatalf("update idempotency key = %q", updateKey)
	}
	if updateBody["displayName"] != "Guardian Intelligence" || updateBody["version"] != float64(1) {
		t.Fatalf("unexpected organization update body: %#v", updateBody)
	}

	runCLI(t, nil, "orgs", "create", "--display-name", "Guardian Intelligence", "--slug", "guardian", "--idempotency-key", "iam:create")
	if createKey != "iam:create" {
		t.Fatalf("create idempotency key = %q", createKey)
	}
	if createBody["displayName"] != "Guardian Intelligence" || createBody["slug"] != "guardian" {
		t.Fatalf("unexpected organization create body: %#v", createBody)
	}

	var inviteOut bytes.Buffer
	runCLI(t, &inviteOut, "orgs", "members", "invite", "invited@example.test", "--role", "roles/admin", "--idempotency-key", "iam:invite")
	if inviteKey != "iam:invite" {
		t.Fatalf("invite idempotency key = %q", inviteKey)
	}
	if inviteBody["email"] != "invited@example.test" {
		t.Fatalf("unexpected invite body: %#v", inviteBody)
	}
	if !strings.Contains(inviteOut.String(), "invited@example.test\tmember_01J8QK4M5N6P7Q8R9S0T1V2W3X\tinvited") {
		t.Fatalf("invite output:\n%s", inviteOut.String())
	}

	var accountLogout bytes.Buffer
	runCLI(t, &accountLogout, "auth", "accounts", "logout", "operator@example.test")
	if !strings.Contains(accountLogout.String(), `"message": "account logged out"`) {
		t.Fatalf("account logout output:\n%s", accountLogout.String())
	}
	if revokedSessions[sessionID] != 1 {
		t.Fatalf("account logout did not revoke device session; revoked sessions = %#v", revokedSessions)
	}

	runCLI(t, nil, "auth", "logout", "--profile", "default")
	if revokedSessions[secondSession] != 2 {
		t.Fatalf("profile logout did not revoke remaining device session; revoked sessions = %#v", revokedSessions)
	}
}

func TestBootstrapOverlaysExistingSiteVars(t *testing.T) {
	requireTool(t, "age-keygen")
	requireTool(t, "sops")

	xdgRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(xdgRoot, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(xdgRoot, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(xdgRoot, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(xdgRoot, "cache"))

	runCLI(t, nil,
		"company", "configure", "guardian",
		"--site", "prod",
		"--product-domain", "guardian.example",
		"--company-domain", "guardianintelligence.org",
		"--company-name", "Guardian Intelligence",
		"--owner-alias", "shovon",
		"--owner-name", "Shovon Hasan",
		"--cli-name", "guardian",
	)

	repoRoot := t.TempDir()
	siteVarsPath := filepath.Join(repoRoot, "src", "host", "sites", "prod", "vars.yml")
	if err := os.MkdirAll(filepath.Dir(siteVarsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(siteVarsPath, []byte(`---
verself_site: prod
bare_metal_host_alias: vs-dev-w0
verself_domain: old.example
platform_org_id: "370200542594579812"
platform_company_display_name: Old Company
`), 0o600); err != nil {
		t.Fatal(err)
	}

	runCLI(t, nil, "bootstrap", "--company", "guardian", "--repo-root", repoRoot, "--force")

	assertFileContains(t, siteVarsPath, []string{
		`bare_metal_host_alias: vs-dev-w0`,
		`platform_org_id: "370200542594579812"`,
		`verself_domain: "guardian.example"`,
		`platform_company_display_name: "Guardian Intelligence"`,
		`platform_owner_email: "shovon@guardianintelligence.org"`,
	})
}

func runCLI(t *testing.T, out *bytes.Buffer, args ...string) {
	t.Helper()
	if out == nil {
		out = &bytes.Buffer{}
	}
	cli := CLI{
		binary: "verself",
		in:     strings.NewReader(""),
		out:    out,
		err:    io.Discard,
		getenv: os.Getenv,
	}
	if err := cli.Run(context.Background(), args); err != nil {
		t.Fatalf("verself %s: %v", strings.Join(args, " "), err)
	}
}

func requireTool(t *testing.T, name string) {
	t.Helper()
	envName := ""
	runfile := ""
	switch name {
	case "age-keygen":
		envName = "VERSELF_AGE_KEYGEN_BIN"
		runfile = "src/tools/dev/binaries/age-keygen"
	case "sops":
		envName = "VERSELF_SOPS_BIN"
		runfile = "src/tools/dev/binaries/sops"
	}
	if envName != "" && os.Getenv(envName) != "" {
		return
	}
	if runfile != "" {
		if path := findRunfile(runfile); path != "" {
			t.Setenv(envName, path)
			return
		}
	}
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is required for bootstrap render verification: %v", name, err)
	}
	if envName != "" {
		t.Setenv(envName, path)
	}
}

func decryptSOPS(t *testing.T, repoRoot, path, identity string) string {
	t.Helper()
	cmd := exec.Command(toolBinary("VERSELF_SOPS_BIN", "sops"), "-d", path)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "SOPS_AGE_KEY="+identity)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sops -d %s: %v\n%s", path, err, string(out))
	}
	return string(out)
}

func findRunfile(rel string) string {
	testSrcDir := os.Getenv("TEST_SRCDIR")
	if testSrcDir == "" {
		return ""
	}
	candidates := []string{}
	if workspace := os.Getenv("TEST_WORKSPACE"); workspace != "" {
		candidates = append(candidates, filepath.Join(testSrcDir, workspace, rel))
	}
	candidates = append(candidates, filepath.Join(testSrcDir, "_main", rel), filepath.Join(testSrcDir, rel))
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func assertFileContains(t *testing.T, path string, values []string) {
	t.Helper()
	data := readFile(t, path)
	for _, want := range values {
		if !strings.Contains(data, want) {
			t.Fatalf("%s missing %q:\n%s", path, want, data)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func treeContains(t *testing.T, root, needle string) (bool, string) {
	t.Helper()
	var foundPath string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte(needle)) {
			foundPath = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return foundPath != "", foundPath
}
