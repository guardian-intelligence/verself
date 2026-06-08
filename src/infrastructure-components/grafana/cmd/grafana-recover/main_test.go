package main

import (
	"archive/tar"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateConfigRejectsAbsoluteRuntimeArtifact(t *testing.T) {
	cfg := validConfig()
	cfg.RuntimeArtifact = "/tmp/grafana-runtime.tar"
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected absolute runtime artifact error")
	}
}

func TestValidateConfigRejectsInvalidPort(t *testing.T) {
	cfg := validConfig()
	cfg.Server.HTTPPort = 0
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected invalid port error")
	}
}

func TestValidateSecretPathRequiresGenerateForGeneratedSource(t *testing.T) {
	secret := validSecretPath()
	secret.Generate = nil
	if err := validateSecretPath(secret); err == nil {
		t.Fatal("expected missing generate error")
	}
}

func TestRenderConfigDoesNotEmbedSecrets(t *testing.T) {
	cfg := validConfig()
	rendered := renderConfig(cfg)
	for _, forbidden := range []string{"admin-password", "secret-key"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("rendered config embedded secret marker %q", forbidden)
		}
	}
	if !strings.Contains(rendered, "admin_password = $__env{GF_SECURITY_ADMIN_PASSWORD}") {
		t.Fatal("rendered config omitted admin password env reference")
	}
	if !strings.Contains(rendered, "secret_key = $__env{GF_SECURITY_SECRET_KEY}") {
		t.Fatal("rendered config omitted secret key env reference")
	}
}

func TestGeneratedBase64URLSecretOmitsPadding(t *testing.T) {
	value, err := generatedSecretValue(generateSpec{Bytes: 48, Encoding: "base64url"})
	if err != nil {
		t.Fatalf("generatedSecretValue returned error: %v", err)
	}
	if strings.Contains(value, "=") {
		t.Fatalf("base64url secret contains padding: %q", value)
	}
}

func TestSafeTarTargetRejectsTraversal(t *testing.T) {
	dest := t.TempDir()
	if _, err := safeTarTarget(dest, "../escape"); err == nil {
		t.Fatal("expected traversal error")
	}
}

func TestExtractTarAddsDirectoryTraverseBits(t *testing.T) {
	artifact := filepath.Join(t.TempDir(), "runtime.tar")
	file, err := os.Create(artifact)
	if err != nil {
		t.Fatalf("create test tar: %v", err)
	}
	tw := tar.NewWriter(file)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "plugins/grafana-clickhouse-datasource",
		Typeflag: tar.TypeDir,
		Mode:     0o644,
	}); err != nil {
		t.Fatalf("write dir header: %v", err)
	}
	payload := []byte("plugin changelog")
	if err := tw.WriteHeader(&tar.Header{
		Name:     "plugins/grafana-clickhouse-datasource/CHANGELOG.md",
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     int64(len(payload)),
	}); err != nil {
		t.Fatalf("write file header: %v", err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatalf("write file payload: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close test tar writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close test tar file: %v", err)
	}

	dest := t.TempDir()
	if err := extractTar(artifact, dest); err != nil {
		t.Fatalf("extractTar returned error: %v", err)
	}
	info, err := os.Stat(filepath.Join(dest, "plugins/grafana-clickhouse-datasource"))
	if err != nil {
		t.Fatalf("stat extracted directory: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o755 {
		t.Fatalf("expected directory mode 0755, got %04o", mode)
	}
	if _, err := os.Stat(filepath.Join(dest, "plugins/grafana-clickhouse-datasource/CHANGELOG.md")); err != nil {
		t.Fatalf("stat extracted plugin file: %v", err)
	}
}

func validConfig() config {
	return config{
		RuntimeArtifact: "bazel-bin/src/infrastructure-components/grafana/grafana-runtime.tar",
		RuntimeRoot:     "/var/lib/grafana/runtime",
		ConfigPath:      "/etc/grafana/grafana.ini",
		DataDir:         "/var/lib/grafana",
		LogDir:          "/var/log/grafana",
		ReportPath:      "/run/verself/recovery/grafana/report.json",
		User:            "grafana",
		Group:           "grafana",
		Server: serverConfig{
			HTTPAddr: "127.0.0.1",
			HTTPPort: 4300,
			Domain:   "dashboard.gamma.verself.sh",
			RootURL:  "https://dashboard.gamma.verself.sh/",
		},
		Database: databaseConfig{
			Host:    "/var/run/postgresql",
			Name:    "grafana",
			User:    "grafana",
			SSLMode: "disable",
		},
		OpenBao: openBaoConfig{
			Address: "https://127.0.0.1:8200",
			CACert:  "/etc/verself/openbao/ca.pem",
			AdminPasswordRef: objectRef{
				APIVersion: "openbao.guardianintelligence.org/v1alpha1",
				Kind:       "SecretPath",
				Name:       "grafana.admin_password",
			},
			SecretKeyRef: objectRef{
				APIVersion: "openbao.guardianintelligence.org/v1alpha1",
				Kind:       "SecretPath",
				Name:       "grafana.secret_key",
			},
		},
		AuthJWT: authJWTConfig{
			Enabled:       true,
			HeaderName:    "X-Pomerium-Jwt-Assertion",
			EmailClaim:    "email",
			UsernameClaim: "email",
			JWKSetURL:     "https://dashboard.gamma.verself.sh/.well-known/pomerium/jwks.json",
			CacheTTL:      "60m",
			AutoSignUp:    true,
		},
	}
}

func validSecretPath() secretPathSpec {
	return secretPathSpec{
		Path:   "kv-runtime/data/secret/org/grafana.admin_password",
		Key:    "value",
		Source: "generated",
		Generate: &generateSpec{
			Bytes:    32,
			Encoding: "base64url",
		},
	}
}
