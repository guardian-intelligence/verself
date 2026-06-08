package main

import (
	"strings"
	"testing"
)

func TestValidateConfigRejectsAbsoluteRuntimeArtifact(t *testing.T) {
	cfg := validConfig()
	cfg.RuntimeArtifact = "/tmp/spicedb-runtime.tar"
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected absolute runtime artifact error")
	}
}

func TestValidateConfigRejectsInvalidPoolBounds(t *testing.T) {
	cfg := validConfig()
	cfg.Datastore.ReadPoolMinOpen = 9
	cfg.Datastore.ReadPoolMaxOpen = 8
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected invalid read pool bounds error")
	}
}

func TestValidateSecretPathRequiresGenerateForGeneratedSource(t *testing.T) {
	secret := validSecretPath()
	secret.Generate = nil
	if err := validateSecretPath(secret); err == nil {
		t.Fatal("expected missing generate error")
	}
}

func TestGeneratedAlphanumericSecretUsesRequestedLength(t *testing.T) {
	value, err := generatedSecretValue(generateSpec{Bytes: 64, Encoding: "alphanumeric"})
	if err != nil {
		t.Fatalf("generatedSecretValue returned error: %v", err)
	}
	if len(value) != 64 {
		t.Fatalf("generated length = %d, want 64", len(value))
	}
}

func TestGeneratedBase64URLSecretOmitsPadding(t *testing.T) {
	value, err := generatedSecretValue(generateSpec{Bytes: 32, Encoding: "base64url"})
	if err != nil {
		t.Fatalf("generatedSecretValue returned error: %v", err)
	}
	if strings.Contains(value, "=") {
		t.Fatalf("base64url secret contains padding: %q", value)
	}
}

func TestSafeTarTargetAllowsRootDirectoryEntry(t *testing.T) {
	dest := t.TempDir()
	target, err := safeTarTarget(dest, "./")
	if err != nil {
		t.Fatalf("safeTarTarget returned error: %v", err)
	}
	if target != dest {
		t.Fatalf("target = %q, want %q", target, dest)
	}
}

func TestSafeTarTargetRejectsTraversal(t *testing.T) {
	dest := t.TempDir()
	if _, err := safeTarTarget(dest, "../escape"); err == nil {
		t.Fatal("expected traversal error")
	}
}

func validConfig() config {
	return config{
		RuntimeArtifact: "bazel-bin/src/infrastructure-components/spicedb/spicedb-runtime.tar",
		RuntimeRoot:     "/var/lib/spicedb/runtime",
		HomeDir:         "/var/lib/spicedb",
		ReportPath:      "/run/verself/recovery/spicedb/report.json",
		User:            "spicedb",
		Group:           "spicedb",
		Datastore: datastoreConfig{
			Engine:           "postgres",
			ConnURI:          "postgres://spicedb@/spicedb?host=/var/run/postgresql&sslmode=disable&application_name=spicedb",
			ReadPoolMaxOpen:  8,
			ReadPoolMinOpen:  1,
			WritePoolMaxOpen: 4,
			WritePoolMinOpen: 1,
		},
		GRPC: grpcConfig{
			Host: "127.0.0.1",
			PresharedKeyRef: objectRef{
				APIVersion: "openbao.guardianintelligence.org/v1alpha1",
				Kind:       "SecretPath",
				Name:       "iam-service.spicedb.grpc_preshared_key",
			},
		},
		Metrics: metricsConfig{Host: "127.0.0.1"},
		OpenBao: openBaoConfig{
			Address: "https://127.0.0.1:8200",
			CACert:  "/etc/verself/openbao/ca.pem",
		},
	}
}

func validSecretPath() secretPathSpec {
	return secretPathSpec{
		Path:   "kv-runtime/data/secret/org/iam-service.spicedb.grpc_preshared_key",
		Key:    "value",
		Source: "generated",
		Generate: &generateSpec{
			Bytes:    64,
			Encoding: "alphanumeric",
		},
	}
}
