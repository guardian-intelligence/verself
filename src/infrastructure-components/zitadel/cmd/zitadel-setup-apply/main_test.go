package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyResourceGraphConfig(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "document.json")
	doc := map[string]any{
		"entrypoint": map[string]any{
			"apiVersion": "guardian.guardianintelligence.org/v1alpha1",
			"kind":       "FlyProcedure",
			"name":       "gamma",
		},
		"resources": []map[string]any{
			{
				"apiVersion": "zitadel.guardianintelligence.org/v1alpha1",
				"kind":       "ZitadelCluster",
				"metadata": map[string]any{
					"name": "zitadel",
				},
				"spec": map[string]any{
					"externalDomain":  "gamma.verself.sh",
					"runtimeArtifact": "bazel-bin/src/infrastructure-components/zitadel/zitadel-runtime.tar",
					"runtimeRoot":     "/var/lib/zitadel/runtime",
					"configFile":      "src/infrastructure-components/zitadel/config/config.yaml",
					"setupStepsFile":  "src/infrastructure-components/zitadel/config/setup-steps.yaml",
					"adminPATPath":    "/run/verself/recovery/zitadel/zitadel-admin/admin.pat",
					"user":            "zitadel",
					"group":           "zitadel",
					"openBao": map[string]any{
						"address": "https://127.0.0.1:8200",
						"caCert":  "/etc/verself/openbao/ca.pem",
						"masterkeySecretRef": map[string]any{
							"apiVersion": "openbao.guardianintelligence.org/v1alpha1",
							"kind":       "SecretPath",
							"name":       "zitadel.masterkey",
						},
						"adminPasswordRef": map[string]any{
							"apiVersion": "openbao.guardianintelligence.org/v1alpha1",
							"kind":       "SecretPath",
							"name":       "zitadel.admin_password",
						},
						"adminPATSecretRefs": []map[string]any{
							{
								"apiVersion": "openbao.guardianintelligence.org/v1alpha1",
								"kind":       "SecretPath",
								"name":       "auth-control-plane.zitadel.admin_token",
							},
						},
						"smtpPasswordRef": map[string]any{
							"apiVersion": "openbao.guardianintelligence.org/v1alpha1",
							"kind":       "SecretPath",
							"name":       "zitadel.smtp.password",
						},
					},
					"instance": map[string]any{},
					"smtp":     map[string]any{},
				},
			},
		},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(graphPath, body, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := applyResourceGraphConfig(config{repoRoot: dir, resourceGraph: graphPath, resourceName: "zitadel"})
	if err != nil {
		t.Fatalf("applyResourceGraphConfig: %v", err)
	}
	if cfg.domain != "gamma.verself.sh" {
		t.Fatalf("domain = %q", cfg.domain)
	}
	if cfg.zitadelBin != "/var/lib/zitadel/runtime/current/bin/zitadel" {
		t.Fatalf("zitadelBin = %q", cfg.zitadelBin)
	}
	wantSource := filepath.Join(dir, "workspace/src/infrastructure-components/zitadel/config/config.yaml")
	if cfg.zitadelConfigPath != defaultZitadelConfig {
		t.Fatalf("zitadelConfigPath = %q, want %q", cfg.zitadelConfigPath, defaultZitadelConfig)
	}
	if cfg.zitadelConfigSource != wantSource {
		t.Fatalf("zitadelConfigSource = %q, want %q", cfg.zitadelConfigSource, wantSource)
	}
	if len(cfg.adminPATSecrets) != 1 || cfg.adminPATSecrets[0] != "auth-control-plane.zitadel.admin_token" {
		t.Fatalf("admin PAT secrets = %#v", cfg.adminPATSecrets)
	}
}

func TestExtractTarRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "zitadel-runtime.tar")
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	if err := tw.WriteHeader(&tar.Header{Name: "../escape", Typeflag: tar.TypeReg, Mode: 0o755, Size: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("nope")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, archive.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	err := extractTar(artifact, filepath.Join(t.TempDir(), "release"))
	if err == nil || !strings.Contains(err.Error(), "unsafe runtime artifact tar entry") {
		t.Fatalf("extractTar error = %v", err)
	}
}

func TestExtractRuntimeArtifactPublishesTraversableRelease(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "zitadel-runtime.tar")
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	if err := tw.WriteHeader(&tar.Header{Name: "bin", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "bin/zitadel", Typeflag: tar.TypeReg, Mode: 0o755, Size: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("test")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, archive.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	release := filepath.Join(dir, "runtime/releases/sha256-test")
	if err := extractRuntimeArtifact(artifact, release); err != nil {
		t.Fatalf("extractRuntimeArtifact: %v", err)
	}
	stat, err := os.Stat(release)
	if err != nil {
		t.Fatal(err)
	}
	if got := stat.Mode().Perm(); got != 0o755 {
		t.Fatalf("release mode = %o, want 755", got)
	}
	bin, err := os.Stat(filepath.Join(release, "bin/zitadel"))
	if err != nil {
		t.Fatal(err)
	}
	if got := bin.Mode().Perm(); got != 0o755 {
		t.Fatalf("bin/zitadel mode = %o, want 755", got)
	}
}

func TestInstallRuntimeArtifactRepairsExistingPrivateRelease(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "zitadel-runtime.tar")
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	if err := tw.WriteHeader(&tar.Header{Name: "bin", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "bin/zitadel", Typeflag: tar.TypeReg, Mode: 0o755, Size: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("test")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, archive.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	digest, err := fileSHA256(artifact)
	if err != nil {
		t.Fatal(err)
	}
	release := filepath.Join(dir, "runtime/releases", strings.ReplaceAll(digest, ":", "-"))
	if err := os.MkdirAll(filepath.Join(release, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, "bin/zitadel"), []byte("test"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(release, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := installRuntimeArtifact(config{
		repoRoot:        dir,
		runtimeArtifact: "zitadel-runtime.tar",
		runtimeRoot:     filepath.Join(dir, "runtime"),
	}); err != nil {
		t.Fatalf("installRuntimeArtifact: %v", err)
	}
	stat, err := os.Stat(release)
	if err != nil {
		t.Fatal(err)
	}
	if got := stat.Mode().Perm(); got != 0o755 {
		t.Fatalf("release mode = %o, want 755", got)
	}
}

func TestPublishAdminPATAcceptsPreviouslyPublishedOpenBaoPAT(t *testing.T) {
	values := map[string]string{
		"/v1/kv-runtime/data/secret/org/auth-control-plane.zitadel.admin_token": "existing-pat",
		"/v1/kv-runtime/data/secret/org/iam-service.zitadel.admin_token":        "existing-pat",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", r.Method)
		}
		value, ok := values[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"data": map[string]string{"value": value}},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()
	cfg := config{
		adminPATPath: filepath.Join(t.TempDir(), "missing-admin.pat"),
		adminPATSecrets: []string{
			"auth-control-plane.zitadel.admin_token",
			"iam-service.zitadel.admin_token",
		},
		openBaoAddr:  server.URL,
		openBaoToken: "token",
	}
	var stdout bytes.Buffer
	if err := publishAdminPAT(context.Background(), cfg, &stdout); err != nil {
		t.Fatalf("publish admin PAT: %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, "already present in OpenBao") {
		t.Fatalf("expected OpenBao presence output, got %q", got)
	}
}

func TestReadMasterkeyFromOpenBao(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", r.Method)
		}
		if r.URL.Path != "/v1/kv-runtime/data/secret/org/zitadel.masterkey" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"data": map[string]string{"value": "01234567890123456789012345678901\n"}},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()
	got, err := readMasterkey(context.Background(), config{
		masterkeySecret: "zitadel.masterkey",
		openBaoAddr:     server.URL,
		openBaoToken:    "token",
	})
	if err != nil {
		t.Fatalf("read masterkey: %v", err)
	}
	if got != "01234567890123456789012345678901" {
		t.Fatalf("masterkey = %q", got)
	}
}

func TestBuildZitadelEnvUsesStaticStoreValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := map[string]string{
			"/v1/kv-runtime/data/secret/org/zitadel.smtp.password":  "smtp-secret",
			"/v1/kv-runtime/data/secret/org/zitadel.admin_password": "admin-secret",
		}
		value, ok := values[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"data": map[string]string{"value": value}},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	env, err := buildZitadelEnv(context.Background(), config{
		mode:                modeSetup,
		resourceGraph:       "document.json",
		domain:              "gamma.verself.sh",
		adminPATPath:        "/run/verself/recovery/zitadel/zitadel-admin/admin.pat",
		adminPasswordSecret: "zitadel.admin_password",
		smtpPasswordSecret:  "zitadel.smtp.password",
		openBaoAddr:         server.URL,
		openBaoToken:        "token",
		instanceName:        "verself",
		instanceOrgName:     "Guardian Intelligence LLC",
		humanUserName:       "anveio",
		humanFirstName:      "Anveio",
		humanLastName:       "Platform",
		humanEmail:          "anveio@gamma.verself.sh",
		machineUserName:     "verself-admin",
		machineName:         "verself admin",
		smtpHost:            "smtp.resend.com:465",
		smtpUser:            "resend",
		smtpFrom:            "anveio@gamma.verself.sh",
		smtpFromName:        "verself",
	}, "01234567890123456789012345678901")
	if err != nil {
		t.Fatalf("buildZitadelEnv: %v", err)
	}
	lookup := map[string]string{}
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			lookup[key] = value
		}
	}
	for key, want := range map[string]string{
		"ZITADEL_EXTERNALDOMAIN":                                  "gamma.verself.sh",
		"ZITADEL_DEFAULTINSTANCE_SMTPCONFIGURATION_SMTP_PASSWORD": "smtp-secret",
		"ZITADEL_FIRSTINSTANCE_ORG_HUMAN_PASSWORD":                "admin-secret",
		"ZITADEL_FIRSTINSTANCE_MACHINEKEYPATH":                    "/run/verself/recovery/zitadel/zitadel-admin/machine-key.json",
		"ZITADEL_FIRSTINSTANCE_ORG_MACHINE_MACHINEKEY_TYPE":       "1",
		"ZITADEL_FIRSTINSTANCE_ORG_MACHINE_PAT_EXPIRATIONDATE":    "2099-01-01T00:00:00Z",
		"ZITADEL_FIRSTINSTANCE_ORG_HUMAN_PASSWORDCHANGEREQUIRED":  "false",
		"ZITADEL_FIRSTINSTANCE_PATPATH":                           "/run/verself/recovery/zitadel/zitadel-admin/admin.pat",
		"ZITADEL_DEFAULTINSTANCE_SMTPCONFIGURATION_SMTP_HOST":     "smtp.resend.com:465",
		"ZITADEL_DEFAULTINSTANCE_SMTPCONFIGURATION_FROM":          "anveio@gamma.verself.sh",
		"ZITADEL_DEFAULTINSTANCE_SMTPCONFIGURATION_FROMNAME":      "verself",
		"ZITADEL_FIRSTINSTANCE_ORG_MACHINE_MACHINE_USERNAME":      "verself-admin",
		"ZITADEL_FIRSTINSTANCE_ORG_MACHINE_MACHINE_NAME":          "verself admin",
		"ZITADEL_MASTERKEY":                                       "01234567890123456789012345678901",
	} {
		if got := lookup[key]; got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestBuildZitadelEnvOmitsSMTPWhenSecretMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/kv-runtime/data/secret/org/zitadel.admin_password" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"data": map[string]string{"value": "admin-secret"}},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	env, err := buildZitadelEnv(context.Background(), config{
		mode:                modeSetup,
		resourceGraph:       "document.json",
		domain:              "gamma.verself.sh",
		adminPATPath:        "/run/verself/recovery/zitadel/zitadel-admin/admin.pat",
		adminPasswordSecret: "zitadel.admin_password",
		smtpPasswordSecret:  "zitadel.smtp.password",
		openBaoAddr:         server.URL,
		openBaoToken:        "token",
		instanceName:        "verself",
		instanceOrgName:     "Guardian Intelligence LLC",
		humanUserName:       "anveio",
		humanFirstName:      "Anveio",
		humanLastName:       "Platform",
		humanEmail:          "anveio@gamma.verself.sh",
		machineUserName:     "verself-admin",
		machineName:         "verself admin",
		smtpHost:            "smtp.resend.com:465",
		smtpUser:            "resend",
		smtpFrom:            "anveio@gamma.verself.sh",
		smtpFromName:        "verself",
	}, "01234567890123456789012345678901")
	if err != nil {
		t.Fatalf("buildZitadelEnv: %v", err)
	}
	for _, item := range env {
		if strings.HasPrefix(item, "ZITADEL_DEFAULTINSTANCE_SMTPCONFIGURATION_") {
			t.Fatalf("unexpected SMTP environment without SMTP secret: %s", item)
		}
	}
}
