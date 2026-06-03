package main

import (
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
