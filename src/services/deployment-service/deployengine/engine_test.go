package deployengine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSiteConfigReadsNomadAddr(t *testing.T) {
	root := t.TempDir()
	writeDeployEngineTestFile(t, root, "src/host/sites/gamma/site.json", `{
  "nomad_addr": "http://127.0.0.1:4646"
}`)

	cfg, err := loadSiteConfig(root, "gamma")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NomadAddr != "http://127.0.0.1:4646" {
		t.Fatalf("nomad addr = %q", cfg.NomadAddr)
	}
}

func TestLoadSiteConfigDefaultsNomadAddr(t *testing.T) {
	root := t.TempDir()
	writeDeployEngineTestFile(t, root, "src/host/sites/gamma/site.json", `{}`)

	cfg, err := loadSiteConfig(root, "gamma")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NomadAddr != "http://127.0.0.1:4646" {
		t.Fatalf("nomad addr = %q", cfg.NomadAddr)
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
