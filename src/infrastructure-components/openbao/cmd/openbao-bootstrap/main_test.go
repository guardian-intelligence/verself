package main

import (
	"errors"
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
