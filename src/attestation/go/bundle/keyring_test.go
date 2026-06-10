package bundle

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"
)

func mustP256PEM(t *testing.T) ([]byte, KeyID) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	id, err := DeriveKeyID(&key.PublicKey)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), id
}

func TestDeriveKeyIDStable(t *testing.T) {
	pemBytes, id := mustP256PEM(t)
	pk, err := ParsePublicKeyPEM(pemBytes)
	if err != nil {
		t.Fatalf("ParsePublicKeyPEM: %v", err)
	}
	if pk.KeyID != id {
		t.Fatalf("parsed KeyID %q != derived %q", pk.KeyID, id)
	}
	if len(string(id)) != 64 {
		t.Fatalf("KeyID not 64 hex chars: %q", id)
	}
}

func TestRingRejectsEmptyAndDuplicate(t *testing.T) {
	if _, err := NewRing(); !errors.Is(err, ErrEmptyRing) {
		t.Fatalf("empty NewRing err = %v, want ErrEmptyRing", err)
	}
	pemBytes, _ := mustP256PEM(t)
	pk, err := ParsePublicKeyPEM(pemBytes)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := NewRing(pk, pk); !errors.Is(err, ErrDuplicateKey) {
		t.Fatalf("duplicate NewRing err = %v, want ErrDuplicateKey", err)
	}
}

func TestNewRingFromPEMMultiple(t *testing.T) {
	a, idA := mustP256PEM(t)
	b, idB := mustP256PEM(t)
	concat := append(append([]byte{}, a...), b...)
	ring, err := NewRingFromPEM(concat)
	if err != nil {
		t.Fatalf("NewRingFromPEM: %v", err)
	}
	if len(ring.keys) != 2 {
		t.Fatalf("ring has %d keys, want 2", len(ring.keys))
	}
	if _, ok := ring.keys[idA]; !ok {
		t.Fatalf("ring missing key A")
	}
	if _, ok := ring.keys[idB]; !ok {
		t.Fatalf("ring missing key B")
	}
}

func TestParsePublicKeyRejectsNonP256(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("generate p384: %v", err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	if _, err := ParsePublicKeyPEM(pemBytes); !errors.Is(err, ErrNotECDSAP256) {
		t.Fatalf("P-384 err = %v, want ErrNotECDSAP256", err)
	}
	if _, err := ParsePublicKeyPEM([]byte("not pem")); !errors.Is(err, ErrNoPEMBlock) {
		t.Fatalf("non-PEM err = %v, want ErrNoPEMBlock", err)
	}
}
