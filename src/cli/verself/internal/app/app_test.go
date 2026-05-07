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
	runCLI(t, &checkoutOut, "repos", "checkout-grants", "create", repoID, "--ref", "main", "--path-prefix", ".github", "--idempotency-key", "source:checkout")
	if checkoutKey != "source:checkout" {
		t.Fatalf("checkout idempotency key = %q", checkoutKey)
	}
	if checkoutBody["ref"] != "main" || checkoutBody["path_prefix"] != ".github" {
		t.Fatalf("unexpected checkout body: %#v", checkoutBody)
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
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/execution-schedules":
			_, _ = w.Write([]byte(`[` + scheduleJSON + `]`))
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
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/billing/entitlements":
			_, _ = w.Write([]byte(`{"org_id":"370200542594579812","universal":{"scope_type":"account","product_id":"","product_display":"","bucket_id":"","bucket_display":"","sku_id":"","sku_display":"","coverage_label":"Account","available_units":"100","pending_units":"0","period_start_units":"100","spent_units":"0","sources":[]},"products":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/billing/contracts":
			_, _ = w.Write([]byte(`{"contracts":[{"contract_id":"contract_1","product_id":"sandbox-ci","plan_id":"ci-pro","cadence_kind":"monthly","status":"active","payment_state":"current","entitlement_state":"active","phase_id":"phase_1","starts_at":"2026-05-06T00:00:00Z"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/billing/plans":
			_, _ = w.Write([]byte(`{"plans":[{"plan_id":"ci-pro","product_id":"sandbox-ci","display_name":"CI Pro","tier":"pro","billing_mode":"subscription","currency":"USD","monthly_amount_cents":"9900","annual_amount_cents":"99000","active":true,"is_default":true}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/billing/statement":
			if r.URL.Query().Get("product_id") != "sandbox-ci" {
				t.Fatalf("statement query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"org_id":"370200542594579812","product_id":"sandbox-ci","period_source":"current","period_start":"2026-05-01T00:00:00Z","period_end":"2026-06-01T00:00:00Z","generated_at":"2026-05-06T00:00:00Z","currency":"USD","unit_label":"credits","totals":{"reserved_units":"0","contract_units":"100","free_tier_units":"0","promo_units":"0","purchase_units":"0","receivable_units":"0","refund_units":"0","charge_units":"5","total_due_units":"0"},"grant_summaries":[],"line_items":[]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("VERSELF_TOKEN", "tok_sandbox")
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

	var schedulesOut bytes.Buffer
	runCLI(t, &schedulesOut, "schedules", "list")
	if !strings.Contains(schedulesOut.String(), scheduleID+"\t"+projectID+"\tactive\t900\t.github/workflows/build.yml\tNightly") {
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
	if !strings.Contains(contractsOut.String(), "contract_1\tsandbox-ci\tci-pro\tactive") {
		t.Fatalf("billing contracts output:\n%s", contractsOut.String())
	}
	var plansOut bytes.Buffer
	runCLI(t, &plansOut, "billing", "plans")
	if !strings.Contains(plansOut.String(), "sandbox-ci\tci-pro\tpro\t9900") {
		t.Fatalf("billing plans output:\n%s", plansOut.String())
	}
	var statementOut bytes.Buffer
	runCLI(t, &statementOut, "billing", "statement", "--product-id", "sandbox-ci")
	if !strings.Contains(statementOut.String(), "370200542594579812\tsandbox-ci\tcurrent\t0") {
		t.Fatalf("billing statement output:\n%s", statementOut.String())
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
