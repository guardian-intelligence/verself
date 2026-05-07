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
)

func TestBootstrapRendersEncryptedCompanyCloneArtifacts(t *testing.T) {
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
	runCLI(t, nil, "company", "options", "set", "guardian", "latitude.plan", "--value", "s3-large-x86")

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
		"bazelisk build //src/cli/guardian:guardian",
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

func TestAuthOrgsAndCredentialsUseIAMSDK(t *testing.T) {
	xdgRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(xdgRoot, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(xdgRoot, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(xdgRoot, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(xdgRoot, "cache"))

	tokenPath := filepath.Join(xdgRoot, "token")
	if err := os.WriteFile(tokenPath, []byte("tok_iam_test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	orgJSON := `{"org_id":"370200542594579812","display_name":"Guardian Intelligence","slug":"guardian","version":1,"org_acl_version":1,"caller":{"user_id":"user_1","email":"shovon@example.com","login_name":"shovon","display_name":"Shovon Hasan","state":"active","role_keys":["owner"]},"member_capabilities":{"org_id":"370200542594579812","version":1,"enabled_keys":[],"updated_at":"2026-05-06T00:00:00Z","updated_by":"user_1"},"permissions":["iam:api_credential:write"]}`
	credentialJSON := func(status string) string {
		return `{"credential_id":"cred_1","org_id":"370200542594579812","subject_id":"svc_1","client_id":"client_1","display_name":"CI","status":"` + status + `","auth_method":"private_key_jwt","fingerprint":"fp_1","permissions":["projects:project:read"],"policy_version_at_issue":1,"created_at":"2026-05-06T00:00:00Z","created_by":"user_1","updated_at":"2026-05-06T00:00:00Z"}`
	}
	var createKey string
	var createBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer tok_iam_test" {
			t.Fatalf("%s %s Authorization = %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/organization":
			_, _ = w.Write([]byte(orgJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/me/organizations":
			_, _ = w.Write([]byte(`[{"org_id":"370200542594579812","display_name":"Guardian Intelligence","slug":"guardian"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/organization/api-credentials":
			createKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"credential":` + credentialJSON("active") + `,"issued_material":{"auth_method":"private_key_jwt","client_id":"client_1","token_url":"https://auth.example/oauth/v2/token","key_id":"key_1","key_content":"pem_test","fingerprint":"fp_1"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/organization/api-credentials":
			_, _ = w.Write([]byte(`{"credentials":[` + credentialJSON("active") + `]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/organization/api-credentials/cred_1":
			_, _ = w.Write([]byte(credentialJSON("active")))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/organization/api-credentials/cred_1/roll":
			_, _ = w.Write([]byte(`{"credential":` + credentialJSON("active") + `,"issued_material":{"auth_method":"private_key_jwt","client_id":"client_1","token_url":"https://auth.example/oauth/v2/token","key_id":"key_2","key_content":"pem_roll","fingerprint":"fp_2"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/organization/api-credentials/cred_1":
			_, _ = w.Write([]byte(credentialJSON("revoked")))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("VERSELF_IAM_API_URL", server.URL)

	runCLI(t, nil, "auth", "login", "--token-file", tokenPath)
	profilePath := filepath.Join(xdgRoot, "data", "verself", "profiles", "default.json")
	profile := readFile(t, profilePath)
	if strings.Contains(profile, "tok_iam_test") {
		t.Fatalf("profile stored plaintext token:\n%s", profile)
	}

	var whoami bytes.Buffer
	runCLI(t, &whoami, "auth", "whoami")
	if !strings.Contains(whoami.String(), "Shovon Hasan") {
		t.Fatalf("auth whoami output:\n%s", whoami.String())
	}

	runCLI(t, nil, "orgs", "use", "guardian")
	var createOut bytes.Buffer
	runCLI(t, &createOut, "orgs", "credentials", "create", "CI", "--permission", "projects:project:read", "--idempotency-key", "iam:test")
	if createKey != "iam:test" {
		t.Fatalf("create idempotency key = %q", createKey)
	}
	if createBody["display_name"] != "CI" || createBody["auth_method"] != "private_key_jwt" {
		t.Fatalf("unexpected credential create body: %#v", createBody)
	}
	if !strings.Contains(createOut.String(), "key_content\tpem_test") {
		t.Fatalf("credential create output:\n%s", createOut.String())
	}
	runCLI(t, nil, "orgs", "credentials", "list")
	runCLI(t, nil, "orgs", "credentials", "get", "cred_1")
	runCLI(t, nil, "orgs", "credentials", "roll", "cred_1")
	runCLI(t, nil, "orgs", "credentials", "revoke", "cred_1")
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
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s is required for bootstrap render verification: %v", name, err)
	}
}

func decryptSOPS(t *testing.T, repoRoot, path, identity string) string {
	t.Helper()
	cmd := exec.Command("sops", "-d", path)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "SOPS_AGE_KEY="+identity)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sops -d %s: %v\n%s", path, err, string(out))
	}
	return string(out)
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
