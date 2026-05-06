package app

import (
	"bytes"
	"context"
	"io"
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
