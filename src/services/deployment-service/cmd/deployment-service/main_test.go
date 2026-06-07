package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitHubVerifierDisabledWithoutAllowLists(t *testing.T) {
	verifier, err := githubVerifier(context.Background(), "https://deployments.api.gamma.verself.sh", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if verifier != nil {
		t.Fatal("expected GitHub verifier to be disabled")
	}
}

func TestGitHubVerifierRequiresWorkflowRefsWhenEnabled(t *testing.T) {
	_, err := githubVerifier(
		context.Background(),
		"https://deployments.api.gamma.verself.sh",
		"guardian-intelligence/verself",
		"refs/heads/main",
		"",
	)
	if err == nil {
		t.Fatal("expected missing workflow refs to fail")
	}
}

func TestDeploymentAuthConfigurationRequiresGitHubOIDC(t *testing.T) {
	verifier, err := githubVerifier(context.Background(), "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if verifier != nil {
		t.Fatal("verifier should be nil without allow-lists")
	}
}

func TestParsePositiveIntRejectsZero(t *testing.T) {
	if _, err := parsePositiveInt("VERSELF_DEPLOY_BAZEL_JOBS", "0"); err == nil {
		t.Fatal("expected zero to be rejected")
	}
}

func TestParsePositiveIntAcceptsPositiveValue(t *testing.T) {
	got, err := parsePositiveInt("VERSELF_DEPLOY_BAZEL_JOBS", "4")
	if err != nil {
		t.Fatal(err)
	}
	if got != 4 {
		t.Fatalf("value = %d, want 4", got)
	}
}

func TestLoadDeploymentRuntimeConfig(t *testing.T) {
	path := writeDeploymentGuardianDocument(t, validDeploymentDocument())

	cfg, err := loadDeploymentRuntimeConfig(path, "deployment-service")
	if err != nil {
		t.Fatalf("loadDeploymentRuntimeConfig: %v", err)
	}
	if cfg.Site != "gamma" {
		t.Fatalf("Site = %q", cfg.Site)
	}
	if cfg.PublicBaseURL != "https://deployments.api.gamma.verself.sh" {
		t.Fatalf("PublicBaseURL = %q", cfg.PublicBaseURL)
	}
	if cfg.AdmissionConcurrency != 4 {
		t.Fatalf("cfg = %#v", cfg)
	}
	if len(cfg.NomadJobPaths) != 1 || cfg.NomadJobPaths[0] != "src/integrations/cloudflare/control-plane/nomad.hcl" {
		t.Fatalf("NomadJobPaths = %#v", cfg.NomadJobPaths)
	}
	if !cfg.GitHubOIDCConfigured {
		t.Fatal("GitHubOIDCConfigured = false")
	}
}

func TestLoadDeploymentRuntimeConfigRejectsPartialGitHubOIDC(t *testing.T) {
	doc := strings.ReplaceAll(validDeploymentDocument(), `"allowedWorkflowRefs":"guardian-intelligence/verself/.github/workflows/gamma-deploy.yml@refs/heads/main"`, `"allowedWorkflowRefs":""`)
	path := writeDeploymentGuardianDocument(t, doc)

	_, err := loadDeploymentRuntimeConfig(path, "deployment-service")
	if err == nil {
		t.Fatal("loadDeploymentRuntimeConfig accepted partial GitHub OIDC config")
	}
	if !strings.Contains(err.Error(), "githubOIDC") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseDeploymentRecoverOptionsRequiresAbsolutePaths(t *testing.T) {
	_, err := parseDeploymentRecoverOptions([]string{"--repo-root=relative"})
	if err == nil {
		t.Fatal("parseDeploymentRecoverOptions accepted relative repo root")
	}
	if !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("error = %v", err)
	}
}

func writeDeploymentGuardianDocument(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "document.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write document: %v", err)
	}
	return path
}

func validDeploymentDocument() string {
	return `{
  "entrypoint": {"apiVersion":"guardian.guardianintelligence.org/v1alpha1","kind":"FlyProcedure","name":"gamma"},
  "resources": [
    {
      "apiVersion":"deployment.guardianintelligence.org/v1alpha1",
      "kind":"DeploymentService",
      "metadata":{"name":"deployment-service"},
      "spec":{
        "site":"gamma",
        "publicBaseURL":"https://deployments.api.gamma.verself.sh",
        "sourceRepo":{"root":"/var/lib/deployment-service/repo","url":"https://github.com/guardian-intelligence/verself.git","initTimeout":"2m"},
        "nomad":{"address":"http://127.0.0.1:4646","jobPaths":["src/integrations/cloudflare/control-plane/nomad.hcl"]},
        "objectStorage":{"address":"https://object-storage-admin-internal-https"},
        "postgres":{"dsn":"postgres://deployment_service@/deployment_service?host=/var/run/postgresql&sslmode=disable","maxConns":4},
        "spiffe":{"endpointSocket":"unix:///run/spire-agent/sockets/agent.sock"},
        "admissionConcurrency":4,
        "recoverySSHReady":true,
        "githubOIDC":{
          "allowedRepositories":"guardian-intelligence/verself",
          "allowedRefs":"refs/heads/main",
          "allowedWorkflowRefs":"guardian-intelligence/verself/.github/workflows/gamma-deploy.yml@refs/heads/main"
        }
      }
    }
  ]
}`
}
