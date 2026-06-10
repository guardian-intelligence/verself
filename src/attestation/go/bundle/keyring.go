// Package bundle is the L2+L3 layer of the attestation module: it wraps
// sigstore-go's DSSE bundle signing and verification, an OpenBao Transit signer,
// a pinned-key trust ring, and OCI referrer attach/discover. All cryptography is
// delegated to sigstore-go and sigstore/sigstore — this package hand-rolls none.
// See src/attestation/AGENTS.md for the trust contract.
package bundle

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
)

// KeyID is a derived key identity: hex(sha256(DER-encoded SPKI public key)),
// lowercase. It is never operator-assigned, so two configs cannot disagree about
// what "the prod key" means and a signer cannot claim another key's identity.
type KeyID string

var (
	// ErrEmptyRing is returned when a verification is attempted with no trusted
	// keys. An empty ring is a construction error, not a verify-everything ring.
	ErrEmptyRing = errors.New("attestation: trusted key ring is empty")
	// ErrNotECDSAP256 is returned for any key that is not ECDSA on the P-256
	// curve — the single algorithm this module supports.
	ErrNotECDSAP256 = errors.New("attestation: only ECDSA P-256 public keys are supported")
	// ErrDuplicateKey is returned when two ring members derive the same KeyID.
	ErrDuplicateKey = errors.New("attestation: duplicate key in ring")
	// ErrNoPEMBlock is returned when no PEM block can be decoded from input.
	ErrNoPEMBlock = errors.New("attestation: no PEM block in input")
)

// DeriveKeyID is the single source of truth for key identity. The producer's
// signer hint and the verifier's ring keys MUST derive identically, so both go
// through this function.
func DeriveKeyID(pub crypto.PublicKey) (KeyID, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("attestation: marshal public key: %w", err)
	}
	sum := sha256.Sum256(der)
	return KeyID(hex.EncodeToString(sum[:])), nil
}

// PublicKey is a trusted ECDSA P-256 verification key with its derived KeyID.
type PublicKey struct {
	KeyID KeyID
	pub   *ecdsa.PublicKey
}

// ParsePublicKeyPEM parses a single PEM-encoded ECDSA P-256 public key and
// derives its KeyID. Non-ECDSA or non-P-256 keys are rejected.
func ParsePublicKeyPEM(pemBytes []byte) (PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return PublicKey{}, ErrNoPEMBlock
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return PublicKey{}, fmt.Errorf("attestation: parse public key: %w", err)
	}
	ec, ok := parsed.(*ecdsa.PublicKey)
	if !ok || ec.Curve != elliptic.P256() {
		return PublicKey{}, ErrNotECDSAP256
	}
	id, err := DeriveKeyID(ec)
	if err != nil {
		return PublicKey{}, err
	}
	return PublicKey{KeyID: id, pub: ec}, nil
}

// Ring is a closed set of trusted verification keys, indexed by derived KeyID.
type Ring struct {
	keys map[KeyID]PublicKey
}

// NewRing builds a ring from explicit keys. An empty ring is an error.
func NewRing(keys ...PublicKey) (*Ring, error) {
	if len(keys) == 0 {
		return nil, ErrEmptyRing
	}
	m := make(map[KeyID]PublicKey, len(keys))
	for _, k := range keys {
		if _, dup := m[k.KeyID]; dup {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateKey, k.KeyID)
		}
		m[k.KeyID] = k
	}
	return &Ring{keys: m}, nil
}

// NewRingFromPEM parses one or more concatenated PEM public keys into a ring.
func NewRingFromPEM(pemBytes []byte) (*Ring, error) {
	var keys []PublicKey
	rest := pemBytes
	for {
		block, remaining := pem.Decode(rest)
		if block == nil {
			break
		}
		pk, err := ParsePublicKeyPEM(pem.EncodeToMemory(block))
		if err != nil {
			return nil, err
		}
		keys = append(keys, pk)
		rest = remaining
	}
	return NewRing(keys...)
}

// KeyIDs returns the ring's key identities (unordered).
func (r *Ring) KeyIDs() []KeyID {
	out := make([]KeyID, 0, len(r.keys))
	for id := range r.keys {
		out = append(out, id)
	}
	return out
}
