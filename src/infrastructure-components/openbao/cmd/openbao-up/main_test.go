package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWrappedKeyRoundTrip(t *testing.T) {
	rootKey := []byte("operator-root-secret")
	envelope, err := encryptUnsealKey(rootKey, "unseal-value")
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Version != wrappedKeyVersion {
		t.Fatalf("version = %q", envelope.Version)
	}
	if strings.Contains(envelope.Ciphertext, "unseal-value") {
		t.Fatalf("ciphertext leaked plaintext")
	}
	plaintext, err := decryptUnsealKey(rootKey, envelope)
	if err != nil {
		t.Fatal(err)
	}
	if plaintext != "unseal-value" {
		t.Fatalf("plaintext = %q", plaintext)
	}
}

func TestWrappedKeyRejectsWrongRootKey(t *testing.T) {
	envelope, err := encryptUnsealKey([]byte("operator-root-secret"), "unseal-value")
	if err != nil {
		t.Fatal(err)
	}
	_, err = decryptUnsealKey([]byte("wrong-root-secret"), envelope)
	if err == nil || !strings.Contains(err.Error(), "decrypt wrapped unseal key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadRootKeyRequiresPrivateMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "site-root.key")
	if err := os.WriteFile(path, []byte("root"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := readRootKey(path)
	if err == nil || !strings.Contains(err.Error(), "readable only by root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeStatusOutputAcceptsUninitializedSealedState(t *testing.T) {
	status, err := decodeStatusOutput([]byte(`{"initialized":false,"sealed":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if status.Initialized {
		t.Fatalf("initialized = true")
	}
	if !status.Sealed {
		t.Fatalf("sealed = false")
	}
}

func TestBaoCommandLabelRedactsSecretArguments(t *testing.T) {
	label := baoCommandLabel([]string{"operator", "unseal", "unseal-value"})
	if strings.Contains(label, "unseal-value") {
		t.Fatalf("label leaked unseal key: %s", label)
	}
	if !strings.Contains(label, "[redacted]") {
		t.Fatalf("label did not mark redacted argument: %s", label)
	}

	label = baoCommandLabel([]string{"operator", "generate-root", "-init", "-otp=otp-value", "-format=json"})
	if strings.Contains(label, "otp-value") {
		t.Fatalf("label leaked OTP: %s", label)
	}
	if !strings.Contains(label, "-otp=[redacted]") {
		t.Fatalf("label did not mark redacted OTP: %s", label)
	}
}

func TestWriteSecretFileUsesPrivateMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := writeSecretFile(path, "token-value"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "token-value\n" {
		t.Fatalf("body = %q", body)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestOpenBaoPathInUseDetection(t *testing.T) {
	if !isOpenBaoPathInUse(errors.New(`openbao POST sys/auth/jwt-nomad status 400: {"errors":["path is already in use at jwt-nomad/"]}`)) {
		t.Fatal("expected path-in-use error to be idempotent")
	}
	if isOpenBaoPathInUse(errors.New(`openbao POST sys/auth/jwt-nomad status 403: permission denied`)) {
		t.Fatal("permission errors must not be treated as convergence")
	}
}

func TestNomadRuntimeRoleBoundClaims(t *testing.T) {
	claims := nomadRuntimeRoleBoundClaims(nomadRuntimeRole{
		NomadNamespace: "default",
		JobID:          "deployment-service",
	})
	if claims["nomad_namespace"] != "default" || claims["nomad_job_id"] != "deployment-service" {
		t.Fatalf("claims = %#v", claims)
	}
	if _, ok := claims["nomad_task"]; ok {
		t.Fatalf("claims unexpectedly bind task: %#v", claims)
	}
}

func TestLoadOpenBaoRuntimeCatalog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, []byte(`{
  "schema_version": 1,
  "generated_secrets": [
    {"name": "service.generated.value", "bytes": 32, "encoding": "hex"}
  ],
  "roles": [
    {
      "name": "service-runtime",
      "nomad_namespace": "default",
      "job_id": "service",
      "paths": [
        {"path": "kv-runtime/data/secret/org/service.generated.value", "capabilities": ["read"]}
      ]
    }
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog, err := loadOpenBaoRuntimeCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.GeneratedSecrets) != 1 || len(catalog.Roles) != 1 {
		t.Fatalf("catalog = %#v", catalog)
	}
}

func TestNomadRuntimeRolePolicy(t *testing.T) {
	policy, err := nomadRuntimeRolePolicy(nomadRuntimeRole{
		Name:           "service-runtime",
		NomadNamespace: "default",
		JobID:          "service",
		Paths: []openBaoRuntimeRolePath{
			{
				Path:         "kv-runtime/data/secret/org/service.secret",
				Capabilities: []string{"create", "read", "update"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`path "kv-runtime/data/secret/org/service.secret"`, `"create"`, `"read"`, `"update"`} {
		if !strings.Contains(policy, want) {
			t.Fatalf("policy missing %q: %s", want, policy)
		}
	}
}

func TestGenerateRuntimeSecretValue(t *testing.T) {
	value, err := generateRuntimeSecretValue(openBaoGeneratedSecret{Name: "secret.hex", Bytes: 16, Encoding: "hex"})
	if err != nil {
		t.Fatal(err)
	}
	if len(value) != 32 {
		t.Fatalf("hex length = %d", len(value))
	}
	value, err = generateRuntimeSecretValue(openBaoGeneratedSecret{Name: "secret.alphanumeric", Bytes: 16, Encoding: "alphanumeric"})
	if err != nil {
		t.Fatal(err)
	}
	if len(value) != 16 {
		t.Fatalf("alphanumeric length = %d", len(value))
	}
	value, err = generateRuntimeSecretValue(openBaoGeneratedSecret{Name: "secret.password", Bytes: 24, Encoding: "password"})
	if err != nil {
		t.Fatal(err)
	}
	if len(value) != 24 {
		t.Fatalf("password length = %d", len(value))
	}
	assertContainsClass := func(name string, ok func(rune) bool) {
		t.Helper()
		for _, r := range value {
			if ok(r) {
				return
			}
		}
		t.Fatalf("password missing %s: %q", name, value)
	}
	assertContainsClass("lowercase", func(r rune) bool { return r >= 'a' && r <= 'z' })
	assertContainsClass("uppercase", func(r rune) bool { return r >= 'A' && r <= 'Z' })
	assertContainsClass("digit", func(r rune) bool { return r >= '0' && r <= '9' })
	assertContainsClass("symbol", func(r rune) bool { return strings.ContainsRune("!@#$%^&*_-+=", r) })
}

func TestPruneNomadRuntimeRolesDeletesOnlyStaleRuntimeEntries(t *testing.T) {
	deleted := map[string]bool{}
	api := func(method, path string, body any, expected ...int) (int, map[string]any, error) {
		if method == "LIST" {
			switch path {
			case "auth/jwt-nomad/role", "sys/policies/acl":
				return http.StatusOK, map[string]any{"data": map[string]any{"keys": []any{
					"analytics-service-runtime",
					"deployment-service-runtime",
					"default",
				}}}, nil
			default:
				t.Fatalf("unexpected list path %s", path)
			}
		}
		if method == http.MethodDelete {
			deleted[path] = true
			return http.StatusNoContent, map[string]any{}, nil
		}
		t.Fatalf("unexpected method %s path %s", method, path)
		return 0, nil, nil
	}
	if err := pruneNomadRuntimeRoles(api, map[string]bool{"analytics-service-runtime": true}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"auth/jwt-nomad/role/deployment-service-runtime",
		"sys/policies/acl/deployment-service-runtime",
	} {
		if !deleted[path] {
			t.Fatalf("expected delete %s, got %#v", path, deleted)
		}
	}
	for _, path := range []string{
		"auth/jwt-nomad/role/analytics-service-runtime",
		"sys/policies/acl/analytics-service-runtime",
		"auth/jwt-nomad/role/default",
		"sys/policies/acl/default",
	} {
		if deleted[path] {
			t.Fatalf("unexpected delete %s", path)
		}
	}
}
