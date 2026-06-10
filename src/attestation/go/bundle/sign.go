package bundle

import (
	"context"
	"fmt"

	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	"github.com/sigstore/sigstore-go/pkg/sign"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/verself/attestation/predicate"
)

// PublicRekorURL is the Sigstore public-good transparency log, used by the
// release (public-audience) path only. The deployment path signs offline.
const PublicRekorURL = "https://rekor.sigstore.dev"

// Envelope is a sigstore bundle (a DSSE envelope plus verification material) in
// its canonical JSON form, used to transport the attestation to and from an OCI
// registry.
//
// The integrity property the module guarantees is NOT that the payload bytes are
// secret — an in-toto statement is public, unencrypted data that anyone can read
// straight off the registry. The property is that no code obtains a *trusted,
// typed* predicate.Statement except by passing the envelope through Verify, which
// checks the signature against the ring before parsing. Reading the raw bytes is
// not a trust violation; acting on an unverified statement is, and the blessed
// path makes that impossible by construction.
type Envelope struct {
	raw []byte
}

// Bytes returns a copy of the canonical JSON bundle bytes for OCI transport.
// These bytes are untrusted until passed through Verify.
func (e *Envelope) Bytes() []byte {
	out := make([]byte, len(e.raw))
	copy(out, e.raw)
	return out
}

// SignOptions selects the trust audience for a signature.
type SignOptions struct {
	// Rekor, when non-nil, uploads the signature to a public transparency log
	// (the release path). Nil means offline — no Rekor — which is the
	// deployment path.
	Rekor *RekorConfig
}

// RekorConfig configures the transparency-log upload for release signing.
type RekorConfig struct {
	// BaseURL is the Rekor instance, e.g. PublicRekorURL.
	BaseURL string
}

// Sign wraps a sealed statement in a Transit-signed DSSE sigstore bundle. The
// statement bytes are signed exactly as sealed; nothing re-serializes them.
//
// signer is a sign.Keypair; in production it is always a *TransitSigner (the one
// blessed signer, restricted server-side by Bazel visibility). The conformance
// suite injects a local P-256 keypair to build hermetic golden vectors. Any
// signer MUST set its hint to DeriveKeyID(publicKey) so the bundle hint matches
// a verifier ring.
func Sign(ctx context.Context, stmt *predicate.Statement, signer sign.Keypair, opts SignOptions) (*Envelope, error) {
	if stmt == nil {
		return nil, fmt.Errorf("attestation: nil statement")
	}
	if signer == nil {
		return nil, fmt.Errorf("attestation: nil signer")
	}
	content := &sign.DSSEData{Data: stmt.Bytes(), PayloadType: predicate.PayloadType}
	bopts := sign.BundleOptions{Context: ctx}
	if opts.Rekor != nil {
		bopts.TransparencyLogs = []sign.Transparency{
			sign.NewRekor(&sign.RekorOptions{BaseURL: opts.Rekor.BaseURL, Version: 1}),
		}
	}
	pb, err := sign.Bundle(content, signer, bopts)
	if err != nil {
		return nil, fmt.Errorf("attestation: sign bundle: %w", err)
	}
	raw, err := protojson.Marshal(pb)
	if err != nil {
		return nil, fmt.Errorf("attestation: marshal bundle: %w", err)
	}
	return &Envelope{raw: raw}, nil
}

// ParseEnvelope wraps raw bundle bytes for transport and discovery WITHOUT
// trusting them. It validates only that the bytes are a structurally valid
// bundle; it does not verify any signature.
func ParseEnvelope(raw []byte) (*Envelope, error) {
	var pb protobundle.Bundle
	if err := protojson.Unmarshal(raw, &pb); err != nil {
		return nil, fmt.Errorf("attestation: parse bundle: %w", err)
	}
	return &Envelope{raw: append([]byte(nil), raw...)}, nil
}
