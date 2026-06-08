package main

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateConfigRejectsAbsoluteRuntimeArtifact(t *testing.T) {
	cfg := validConfig()
	cfg.RuntimeArtifact = "/tmp/forgejo-runtime.tar"
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

func TestRenderConfigHasNoLegacyPlaceholdersOrSecretValues(t *testing.T) {
	cfg := validConfig()
	rendered := renderConfig(cfg)
	for _, forbidden := range []string{"__", "secret-key", "internal-token"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("rendered config contained forbidden marker %q", forbidden)
		}
	}
	for _, required := range []string{
		"SECRET_KEY_URI = file:/proc/self/fd/3",
		"INTERNAL_TOKEN_URI = file:/proc/self/fd/4",
		"LFS_JWT_SECRET_URI = file:/proc/self/fd/5",
		"JWT_SECRET_URI = file:/proc/self/fd/6",
		"PATH = /var/lib/forgejo/runtime/current/bin/git",
		"DOMAIN = git.gamma.verself.sh",
		"ROOT_URL = https://git.gamma.verself.sh/",
	} {
		if !strings.Contains(rendered, required) {
			t.Fatalf("rendered config omitted %q", required)
		}
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

func TestSecretTempFilesAreReadableAndUnlinked(t *testing.T) {
	files, err := secretTempFiles(runtimeSecrets{secretKey: "secret-key", internalToken: "internal-token", lfsJWTSecret: "lfs-jwt-secret", oauthJWTSecret: "oauth-jwt-secret"}, os.Geteuid(), os.Getegid())
	if err != nil {
		t.Fatalf("secretTempFiles returned error: %v", err)
	}
	defer closeFiles(files)
	if len(files) != 4 {
		t.Fatalf("expected four secret files, got %d", len(files))
	}
	for i, file := range files {
		if _, err := os.Stat(file.Name()); !os.IsNotExist(err) {
			t.Fatalf("secret file %d still has a directory entry: %v", i, err)
		}
		body, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("read secret file %d: %v", i, err)
		}
		if string(body) == "" {
			t.Fatalf("secret file %d was empty", i)
		}
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
		Name:     "bin",
		Typeflag: tar.TypeDir,
		Mode:     0o644,
	}); err != nil {
		t.Fatalf("write dir header: %v", err)
	}
	payload := []byte("forgejo")
	if err := tw.WriteHeader(&tar.Header{
		Name:     "bin/forgejo",
		Typeflag: tar.TypeReg,
		Mode:     0o755,
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
	info, err := os.Stat(filepath.Join(dest, "bin"))
	if err != nil {
		t.Fatalf("stat extracted directory: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o755 {
		t.Fatalf("expected directory mode 0755, got %04o", mode)
	}
	if _, err := os.Stat(filepath.Join(dest, "bin/forgejo")); err != nil {
		t.Fatalf("stat extracted file: %v", err)
	}
}

func validConfig() config {
	return config{
		RuntimeArtifact: "bazel-bin/src/infrastructure-components/forgejo/forgejo-runtime.tar",
		RuntimeRoot:     "/var/lib/forgejo/runtime",
		ConfigPath:      "/etc/forgejo/app.ini",
		WorkDir:         "/var/lib/forgejo",
		DataDir:         "/var/lib/forgejo/data",
		LogDir:          "/var/lib/forgejo/log",
		RepositoriesDir: "/var/lib/forgejo/repositories",
		ReportPath:      "/run/verself/recovery/forgejo/report.json",
		User:            "forgejo",
		Group:           "forgejo",
		Server: serverConfig{
			HTTPAddr: "127.0.0.1",
			HTTPPort: 3000,
			Domain:   "git.gamma.verself.sh",
			RootURL:  "https://git.gamma.verself.sh/",
		},
		OpenBao: openBaoConfig{
			Address: "https://127.0.0.1:8200",
			CACert:  "/etc/verself/openbao/ca.pem",
			SecretKeyRef: objectRef{
				APIVersion: "openbao.guardianintelligence.org/v1alpha1",
				Kind:       "SecretPath",
				Name:       "forgejo.secret_key",
			},
			InternalTokenRef: objectRef{
				APIVersion: "openbao.guardianintelligence.org/v1alpha1",
				Kind:       "SecretPath",
				Name:       "forgejo.internal_token",
			},
			LFSJWTSecretRef: objectRef{
				APIVersion: "openbao.guardianintelligence.org/v1alpha1",
				Kind:       "SecretPath",
				Name:       "forgejo.lfs_jwt_secret",
			},
			OAuthJWTSecretRef: objectRef{
				APIVersion: "openbao.guardianintelligence.org/v1alpha1",
				Kind:       "SecretPath",
				Name:       "forgejo.oauth_jwt_secret",
			},
			AutomationTokenRef: objectRef{
				APIVersion: "openbao.guardianintelligence.org/v1alpha1",
				Kind:       "SecretPath",
				Name:       "source-code-hosting-service.forgejo.automation_token",
			},
		},
	}
}

func validSecretPath() secretPathSpec {
	return secretPathSpec{
		Path:   "kv-runtime/data/secret/org/forgejo.secret_key",
		Key:    "value",
		Source: "generated",
		Generate: &generateSpec{
			Bytes:    32,
			Encoding: "base64url",
		},
	}
}
