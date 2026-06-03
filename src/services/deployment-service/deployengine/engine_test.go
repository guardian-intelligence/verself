package deployengine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapModeCanUseR2ControlPlanePublishing(t *testing.T) {
	exec := execution{Options: Options{Bootstrap: true}}
	if !exec.bootstrapMode() {
		t.Fatal("explicit bootstrap run should use bootstrap Nomad sequencing")
	}
}

func TestLoadSiteConfigUsesGlobalCloudflareAccount(t *testing.T) {
	root := t.TempDir()
	writeDeployEngineTestFile(t, root, "src/integrations/cloudflare/account.json", `{
  "version": "verself.cloudflare.account.v1",
  "control_plane_site": "prod",
  "account_id": "0123456789abcdef0123456789abcdef",
  "r2": {
    "deployment_artifacts_bucket": "verself-deployment-artifacts",
    "recovery_bucket": "verself-recovery"
  }
}`)
	writeDeployEngineTestFile(t, root, "src/host/sites/gamma/site.json", `{
  "artifact_delivery": {
    "kind": "cloudflare_r2_control_plane",
    "key_prefix": "sha256",
    "control_plane_addr": "https://cloudflare-r2-control-plane-internal-https",
    "checksum_algorithm": "sha256",
    "public": false
  },
  "nomad_addr": "http://127.0.0.1:4646"
}`)

	cfg, err := loadSiteConfig(root, "gamma")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ArtifactDelivery.CloudflareAccountID != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("account id = %q", cfg.ArtifactDelivery.CloudflareAccountID)
	}
	if cfg.ArtifactDelivery.Bucket != "verself-deployment-artifacts" {
		t.Fatalf("bucket = %q", cfg.ArtifactDelivery.Bucket)
	}
}

func TestLoadSiteConfigRejectsSiteCloudflareGlobals(t *testing.T) {
	root := t.TempDir()
	writeDeployEngineTestFile(t, root, "src/integrations/cloudflare/account.json", `{
  "version": "verself.cloudflare.account.v1",
  "control_plane_site": "prod",
  "account_id": "0123456789abcdef0123456789abcdef",
  "r2": {
    "deployment_artifacts_bucket": "verself-deployment-artifacts",
    "recovery_bucket": "verself-recovery"
  }
}`)
	writeDeployEngineTestFile(t, root, "src/host/sites/gamma/site.json", `{
  "artifact_delivery": {
    "kind": "cloudflare_r2_control_plane",
    "bucket": "verself-deployment-artifacts",
    "key_prefix": "sha256",
    "checksum_algorithm": "sha256",
    "public": false
  }
}`)

	_, err := loadSiteConfig(root, "gamma")
	if err == nil {
		t.Fatal("expected site Cloudflare global rejection")
	}
	if !strings.Contains(err.Error(), "artifact_delivery.bucket belongs to src/integrations/cloudflare/account.json") {
		t.Fatalf("error = %v", err)
	}
}

func writeDeployEngineTestFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
