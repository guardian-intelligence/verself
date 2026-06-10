// Package transit is the L3 server-side signer: the one blessed OpenBao Transit
// signer for the attestation module. It is deliberately separate from the
// bundle package so a verifier binary (e.g. distribution-service) never links
// the KMS/Vault dependency tree. Bazel visibility restricts which targets may
// depend on this package; see src/attestation/AGENTS.md.
package transit

import (
	"bytes"
	"context"
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	"github.com/sigstore/sigstore/pkg/signature/kms"
	// hashivault registers the hashivault:// (and openbao://) KMS provider.
	_ "github.com/sigstore/sigstore/pkg/signature/kms/hashivault"
	sigopts "github.com/sigstore/sigstore/pkg/signature/options"

	"github.com/verself/attestation/bundle"
)

// Signer signs DSSE pre-authentication encodings with an OpenBao Transit ECDSA
// P-256 key. It implements sigstore-go's sign.Keypair (consumed by
// bundle.Sign). The hashivault provider's SignMessage returns only (sig, error)
// and hashes internally, so SignData recomputes the SHA-256 digest to satisfy
// the Keypair contract.
type Signer struct {
	sv     kms.SignerVerifier
	pub    crypto.PublicKey
	pubPEM []byte
	keyID  bundle.KeyID
}

// New opens a Transit signer over keyRef, a hashivault:// reference such as
// "hashivault://deployment-signing". Connection and auth come from the
// environment: the hashivault provider reads the address from VAULT_ADDR then
// BAO_ADDR, the token from VAULT_TOKEN then BAO_TOKEN then ~/.vault-token,
// and the CA bundle only from VAULT_CACERT (BAO_CACERT is not read).
func New(ctx context.Context, keyRef string) (*Signer, error) {
	sv, err := kms.Get(ctx, keyRef, crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("attestation: open transit signer %q: %w", keyRef, err)
	}
	pub, err := sv.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("attestation: fetch transit public key: %w", err)
	}
	keyID, err := bundle.DeriveKeyID(pub)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("attestation: marshal transit public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	return &Signer{sv: sv, pub: pub, pubPEM: pubPEM, keyID: keyID}, nil
}

// KeyID returns the signer's derived key identity, matching what a verifier ring
// derives from the corresponding public key.
func (s *Signer) KeyID() bundle.KeyID { return s.keyID }

// PublicKeyPEM returns a copy of the signer's PEM-encoded public key, for
// pinning into a verifier's ring config.
func (s *Signer) PublicKeyPEM() []byte {
	out := make([]byte, len(s.pubPEM))
	copy(out, s.pubPEM)
	return out
}

// --- sign.Keypair implementation -------------------------------------------

func (s *Signer) GetHashAlgorithm() protocommon.HashAlgorithm {
	return protocommon.HashAlgorithm_SHA2_256
}

func (s *Signer) GetSigningAlgorithm() protocommon.PublicKeyDetails {
	return protocommon.PublicKeyDetails_PKIX_ECDSA_P256_SHA_256
}

// GetHint returns the derived KeyID so the bundle's verification-material hint
// matches the verifier ring's key index exactly.
func (s *Signer) GetHint() []byte { return []byte(s.keyID) }

func (s *Signer) GetKeyAlgorithm() string { return "ECDSA" }

func (s *Signer) GetPublicKey() crypto.PublicKey { return s.pub }

func (s *Signer) GetPublicKeyPem() (string, error) { return string(s.pubPEM), nil }

// SignData signs the DSSE PAE. The hashivault provider hashes the message
// internally for ECDSA P-256; we recompute the SHA-256 only to return the digest
// the Keypair contract requires.
func (s *Signer) SignData(ctx context.Context, data []byte) ([]byte, []byte, error) {
	sig, err := s.sv.SignMessage(bytes.NewReader(data), sigopts.WithContext(ctx))
	if err != nil {
		return nil, nil, fmt.Errorf("attestation: transit sign: %w", err)
	}
	d := sha256.Sum256(data)
	return sig, d[:], nil
}
