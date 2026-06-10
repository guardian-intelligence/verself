package bundle

import (
	"context"
	"crypto"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	sgbundle "github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/signature"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/verself/attestation/predicate"
)

// ErrVerification is the tagged error for any failure of the signature/trust
// boundary. Policy checks on the returned statement live in the consuming
// service, not here.
var ErrVerification = errors.New("attestation: bundle verification failed")

// Verification reports which trusted key verified the bundle. The signer
// identity is DERIVED from verification, never self-asserted by the requester.
type Verification struct {
	KeyID KeyID
}

// VerifyOptions selects the trust audience for verification.
type VerifyOptions struct {
	// TransparencyRoot, when non-nil, requires Rekor inclusion verified against
	// this trusted material (the release path). Nil means offline key-only
	// verification (the deployment path).
	//
	// PHASE 2 / UNVERIFIED: the deployment channel uses the offline path (nil),
	// which is ground-truthed end to end. The transparency-log path is written
	// from the sigstore-go spec but has NOT been exercised against a live Rekor
	// instance; the release cutover must add a live conformance test (sign with
	// Rekor, verify inclusion, assert failure when the entry is missing) and
	// confirm TrustedMaterialCollection ordering before it carries trust. A
	// bundle signed offline cannot be verified with TransparencyRoot set — it
	// has no log entry — and will fail with a transparency-log error.
	TransparencyRoot root.TrustedMaterial
}

// Verify is the sole trust boundary of the module. It verifies the bundle's
// signature against the ring over the given subject digest, and ONLY then parses
// the (now-trusted) payload into a typed statement. The order is fixed and not
// configurable: signature before parse.
func Verify(ctx context.Context, env *Envelope, ring *Ring, subjectSHA256 string, opts VerifyOptions) (*predicate.Statement, Verification, error) {
	if env == nil {
		return nil, Verification{}, fmt.Errorf("%w: nil envelope", ErrVerification)
	}
	if ring == nil || len(ring.keys) == 0 {
		return nil, Verification{}, ErrEmptyRing
	}
	digestBytes, err := hex.DecodeString(subjectSHA256)
	if err != nil || len(digestBytes) != 32 {
		return nil, Verification{}, fmt.Errorf("%w: subject digest must be bare sha256 hex", ErrVerification)
	}

	var pb protobundle.Bundle
	if err := protojson.Unmarshal(env.raw, &pb); err != nil {
		return nil, Verification{}, fmt.Errorf("%w: parse bundle: %v", ErrVerification, err)
	}
	b, err := sgbundle.NewBundle(&pb)
	if err != nil {
		return nil, Verification{}, fmt.Errorf("%w: load bundle: %v", ErrVerification, err)
	}

	// Trusted material: pinned ring keys (always-valid via zero validity bounds),
	// optionally combined with a transparency-log trusted root for the release
	// path. A bare TrustedRoot has no PublicKeyVerifier, so the collection order
	// puts the pinned-key material first.
	keyMap := make(map[string]*root.ExpiringKey, len(ring.keys))
	for id, pk := range ring.keys {
		v, err := signature.LoadVerifier(pk.pub, crypto.SHA256)
		if err != nil {
			return nil, Verification{}, fmt.Errorf("%w: load verifier: %v", ErrVerification, err)
		}
		keyMap[string(id)] = root.NewExpiringKey(v, time.Time{}, time.Time{})
	}
	pubMaterial := root.NewTrustedPublicKeyMaterialFromMapping(keyMap)

	var trusted root.TrustedMaterial = pubMaterial
	verifierOpts := []verify.VerifierOption{verify.WithNoObserverTimestamps()}
	if opts.TransparencyRoot != nil {
		trusted = root.TrustedMaterialCollection{pubMaterial, opts.TransparencyRoot}
		verifierOpts = []verify.VerifierOption{verify.WithTransparencyLog(1), verify.WithIntegratedTimestamps(1)}
	}

	sev, err := verify.NewVerifier(trusted, verifierOpts...)
	if err != nil {
		return nil, Verification{}, fmt.Errorf("%w: build verifier: %v", ErrVerification, err)
	}

	// sigstore-go selects the key by the bundle hint and verifies against it; a
	// hint outside the ring fails here. Success therefore proves a ring key
	// signed these bytes over this subject digest.
	if _, err := sev.Verify(b, verify.NewPolicy(
		verify.WithArtifactDigest("sha256", digestBytes),
		verify.WithKey(),
	)); err != nil {
		return nil, Verification{}, fmt.Errorf("%w: %v", ErrVerification, err)
	}

	keyID := KeyID(pb.GetVerificationMaterial().GetPublicKey().GetHint())
	if _, ok := ring.keys[keyID]; !ok {
		return nil, Verification{}, fmt.Errorf("%w: verified key id %q absent from ring", ErrVerification, keyID)
	}

	// Signature is proven; only now parse the verified payload into a typed
	// statement.
	payload := pb.GetDsseEnvelope().GetPayload()
	stmt, err := predicate.Parse(payload)
	if err != nil {
		return nil, Verification{}, err
	}
	// Defense in depth: the digest we verified against must be among the
	// statement's own subjects.
	if _, ok := stmt.SubjectMatching(subjectSHA256); !ok {
		return nil, Verification{}, fmt.Errorf("%w: verified subject digest absent from statement subjects", ErrVerification)
	}
	return stmt, Verification{KeyID: keyID}, nil
}
