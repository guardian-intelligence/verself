// Package conformance is the public-trust regression test for the attestation
// module. It exercises the full Sign -> Attach -> Discover -> Verify path with a
// hermetic local P-256 keypair (no OpenBao needed), asserts the negative
// controls fail, proves stock cosign can verify our bundle, and — when BAO_ADDR
// is set — proves the real OpenBao Transit signer end to end.
//
// Because protojson is non-deterministic, vectors assert parsed equality and
// byte-exact verification, never byte equality of a fresh marshal.
package conformance

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	sigsig "github.com/sigstore/sigstore/pkg/signature"
	sigopts "github.com/sigstore/sigstore/pkg/signature/options"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"

	"github.com/verself/attestation/bundle"
	"github.com/verself/attestation/predicate"
	"github.com/verself/attestation/transit"
)

// localKeypair is a hermetic sign.Keypair over an in-process ECDSA P-256 key. It
// mirrors TransitSigner's contract — crucially, GetHint() returns
// bundle.DeriveKeyID(pub) so the bundle hint matches a verifier ring.
type localKeypair struct {
	sv     sigsig.SignerVerifier
	pub    crypto.PublicKey
	pubPEM []byte
	keyID  bundle.KeyID
}

func newLocalKeypair(t *testing.T) *localKeypair {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sv, err := sigsig.LoadSignerVerifier(priv, crypto.SHA256)
	if err != nil {
		t.Fatalf("load signer: %v", err)
	}
	id, err := bundle.DeriveKeyID(&priv.PublicKey)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	return &localKeypair{sv: sv, pub: &priv.PublicKey, pubPEM: pubPEM, keyID: id}
}

func (k *localKeypair) GetHashAlgorithm() protocommon.HashAlgorithm {
	return protocommon.HashAlgorithm_SHA2_256
}
func (k *localKeypair) GetSigningAlgorithm() protocommon.PublicKeyDetails {
	return protocommon.PublicKeyDetails_PKIX_ECDSA_P256_SHA_256
}
func (k *localKeypair) GetHint() []byte                { return []byte(k.keyID) }
func (k *localKeypair) GetKeyAlgorithm() string        { return "ECDSA" }
func (k *localKeypair) GetPublicKey() crypto.PublicKey { return k.pub }
func (k *localKeypair) GetPublicKeyPem() (string, error) {
	return string(k.pubPEM), nil
}
func (k *localKeypair) SignData(ctx context.Context, data []byte) ([]byte, []byte, error) {
	sig, err := k.sv.SignMessage(bytes.NewReader(data), sigopts.WithContext(ctx))
	if err != nil {
		return nil, nil, err
	}
	d := sha256.Sum256(data)
	return sig, d[:], nil
}

func sampleStatement(t *testing.T, artifactHex string) *predicate.Statement {
	t.Helper()
	stmt, err := predicate.NewProvenance(
		[]predicate.Subject{{Name: "verself/probe", SHA256: artifactHex}},
		predicate.SLSAProvenance{
			BuildType:          predicate.BazelOCIBuild,
			ExternalParameters: predicate.ExternalParameters{OCIRepository: "verself/probe", Site: "gamma"},
			ResolvedDependencies: []predicate.ResolvedDependency{{
				URI: "git+https://github.com/guardian-intelligence/verself-sh@" + strings.Repeat("b", 40), GitCommit: strings.Repeat("b", 40),
			}},
			BuilderID:    "spiffe://verself.gamma/svc/deployment-service",
			InvocationID: "conformance-0001",
		},
	)
	if err != nil {
		t.Fatalf("NewProvenance: %v", err)
	}
	return stmt
}

func ringFor(t *testing.T, kp *localKeypair) *bundle.Ring {
	t.Helper()
	pk, err := bundle.ParsePublicKeyPEM(kp.pubPEM)
	if err != nil {
		t.Fatalf("parse pub: %v", err)
	}
	ring, err := bundle.NewRing(pk)
	if err != nil {
		t.Fatalf("new ring: %v", err)
	}
	return ring
}

// TestAttachDiscoverVerify exercises the entire module path over an oras memory
// store: seal a statement, sign offline, attach as a referrer, discover it back,
// and verify against the ring.
func TestAttachDiscoverVerify(t *testing.T) {
	ctx := context.Background()
	kp := newLocalKeypair(t)
	artifact := []byte("verself conformance artifact")
	artifactDigest := sha256.Sum256(artifact)
	artifactHex := hex.EncodeToString(artifactDigest[:])

	stmt := sampleStatement(t, artifactHex)
	env, err := bundle.Sign(ctx, stmt, kp, bundle.SignOptions{})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	store := memory.New()
	subject, err := oras.PushBytes(ctx, store, "application/vnd.verself.conformance.blob", artifact)
	if err != nil {
		t.Fatalf("push subject blob: %v", err)
	}
	subjectManifest, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, "application/vnd.verself.conformance.artifact", oras.PackManifestOptions{
		Layers: []v1.Descriptor{subject},
	})
	if err != nil {
		t.Fatalf("pack subject manifest: %v", err)
	}
	referrer, err := bundle.Attach(ctx, store, subjectManifest, env)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}

	found, err := bundle.Discover(ctx, store, subjectManifest)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("discovered %d envelopes, want 1", len(found))
	}
	if found[0].ReferrerDigest != referrer.Digest.String() {
		t.Fatalf("discovered referrer digest %q != attached %q", found[0].ReferrerDigest, referrer.Digest)
	}

	got, verification, err := bundle.Verify(ctx, found[0].Envelope, ringFor(t, kp), artifactHex, bundle.VerifyOptions{})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verification.KeyID != kp.keyID {
		t.Fatalf("verified KeyID %q != signer %q", verification.KeyID, kp.keyID)
	}
	if _, ok := got.SubjectMatching(artifactHex); !ok {
		t.Fatalf("verified statement does not carry the artifact subject")
	}
	prov, ok := got.Predicate().(predicate.SLSAProvenance)
	if !ok || prov.BuildType != predicate.BazelOCIBuild {
		t.Fatalf("unexpected verified predicate: %+v", got.Predicate())
	}
}

// TestDiscoverSkipsMalformedReferrer confirms a malformed referrer tagged as a
// sigstore bundle (a registry-injected DoS attempt) does not block discovery of
// the legitimate bundle.
func TestDiscoverSkipsMalformedReferrer(t *testing.T) {
	ctx := context.Background()
	kp := newLocalKeypair(t)
	artifact := []byte("verself discover-robustness artifact")
	artifactDigest := sha256.Sum256(artifact)
	artifactHex := hex.EncodeToString(artifactDigest[:])
	stmt := sampleStatement(t, artifactHex)
	env, err := bundle.Sign(ctx, stmt, kp, bundle.SignOptions{})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	store := memory.New()
	subjectBlob, err := oras.PushBytes(ctx, store, "application/vnd.verself.conformance.blob", artifact)
	if err != nil {
		t.Fatalf("push subject blob: %v", err)
	}
	subjectManifest, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, "application/vnd.verself.conformance.artifact", oras.PackManifestOptions{
		Layers: []v1.Descriptor{subjectBlob},
	})
	if err != nil {
		t.Fatalf("pack subject manifest: %v", err)
	}

	// Inject a malformed referrer that carries the bundle artifactType but is
	// not a single-layer bundle.
	junk, err := oras.PushBytes(ctx, store, "application/octet-stream", []byte("not a bundle"))
	if err != nil {
		t.Fatalf("push junk: %v", err)
	}
	if _, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, bundle.MediaTypeBundle, oras.PackManifestOptions{
		Subject: &subjectManifest,
		Layers:  []v1.Descriptor{junk, junk},
	}); err != nil {
		t.Fatalf("pack malformed referrer: %v", err)
	}
	// Attach the legitimate bundle.
	if _, err := bundle.Attach(ctx, store, subjectManifest, env); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	found, err := bundle.Discover(ctx, store, subjectManifest)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("discovered %d envelopes, want exactly 1 (malformed sibling must be skipped)", len(found))
	}
	if _, _, err := bundle.Verify(ctx, found[0].Envelope, ringFor(t, kp), artifactHex, bundle.VerifyOptions{}); err != nil {
		t.Fatalf("Verify of the surviving bundle failed: %v", err)
	}
}

// TestVerifyNegativeControls confirms the trust boundary rejects every tamper.
func TestVerifyNegativeControls(t *testing.T) {
	ctx := context.Background()
	kp := newLocalKeypair(t)
	artifact := []byte("verself negative-control artifact")
	artifactDigest := sha256.Sum256(artifact)
	artifactHex := hex.EncodeToString(artifactDigest[:])
	stmt := sampleStatement(t, artifactHex)
	env, err := bundle.Sign(ctx, stmt, kp, bundle.SignOptions{})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	ring := ringFor(t, kp)

	t.Run("wrong subject digest", func(t *testing.T) {
		other := sha256.Sum256([]byte("a different artifact"))
		if _, _, err := bundle.Verify(ctx, env, ring, hex.EncodeToString(other[:]), bundle.VerifyOptions{}); err == nil {
			t.Fatalf("verify accepted a mismatched subject digest")
		}
	})

	t.Run("key outside ring", func(t *testing.T) {
		stranger := newLocalKeypair(t)
		if _, _, err := bundle.Verify(ctx, env, ringFor(t, stranger), artifactHex, bundle.VerifyOptions{}); err == nil {
			t.Fatalf("verify accepted a signature from a key outside the ring")
		}
	})

	t.Run("empty ring", func(t *testing.T) {
		if _, _, err := bundle.Verify(ctx, env, &bundle.Ring{}, artifactHex, bundle.VerifyOptions{}); err == nil {
			t.Fatalf("verify accepted an empty ring")
		}
	})

	t.Run("tampered payload", func(t *testing.T) {
		var obj map[string]any
		if err := json.Unmarshal(env.Bytes(), &obj); err != nil {
			t.Fatalf("unmarshal bundle: %v", err)
		}
		dsse := obj["dsseEnvelope"].(map[string]any)
		dsse["payload"] = "dGFtcGVyZWQ=" // base64("tampered")
		tampered, _ := json.Marshal(obj)
		badEnv, err := bundle.ParseEnvelope(tampered)
		if err != nil {
			t.Fatalf("parse tampered: %v", err)
		}
		if _, _, err := bundle.Verify(ctx, badEnv, ring, artifactHex, bundle.VerifyOptions{}); err == nil {
			t.Fatalf("verify accepted a tampered payload")
		}
	})
}

// TestCosignInterop is the public-trust gate: stock cosign must verify our
// offline bundle. Skipped if cosign is not on PATH.
func TestCosignInterop(t *testing.T) {
	cosign, err := exec.LookPath("cosign")
	if err != nil {
		t.Skip("cosign not on PATH; skipping interop gate")
	}
	ctx := context.Background()
	kp := newLocalKeypair(t)
	artifact := []byte("verself cosign-interop artifact")
	artifactDigest := sha256.Sum256(artifact)
	artifactHex := hex.EncodeToString(artifactDigest[:])
	stmt := sampleStatement(t, artifactHex)
	env, err := bundle.Sign(ctx, stmt, kp, bundle.SignOptions{})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "artifact.bin")
	bundlePath := filepath.Join(dir, "bundle.sigstore.json")
	keyPath := filepath.Join(dir, "pub.pem")
	if err := os.WriteFile(artifactPath, artifact, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundlePath, env.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, kp.pubPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(ctx, cosign, "verify-blob-attestation",
		"--key", keyPath,
		"--bundle", bundlePath,
		"--new-bundle-format",
		"--insecure-ignore-tlog",
		"--type", "slsaprovenance1",
		artifactPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cosign verify failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Verified OK") {
		t.Fatalf("cosign did not report Verified OK:\n%s", out)
	}
}

// TestTransitInterop proves the real OpenBao Transit signer end to end. Gated on
// BAO_ADDR; the key ref defaults to hashivault://deployment-signing and can be
// overridden with ATTESTATION_TRANSIT_KEY.
func TestTransitInterop(t *testing.T) {
	if os.Getenv("BAO_ADDR") == "" {
		t.Skip("BAO_ADDR unset; skipping live Transit interop")
	}
	ctx := context.Background()
	keyRef := os.Getenv("ATTESTATION_TRANSIT_KEY")
	if keyRef == "" {
		keyRef = "hashivault://deployment-signing"
	}
	signer, err := transit.New(ctx, keyRef)
	if err != nil {
		t.Fatalf("transit.New: %v", err)
	}
	artifact := []byte("verself transit-interop artifact")
	artifactDigest := sha256.Sum256(artifact)
	artifactHex := hex.EncodeToString(artifactDigest[:])
	stmt := sampleStatement(t, artifactHex)

	env, err := bundle.Sign(ctx, stmt, signer, bundle.SignOptions{})
	if err != nil {
		t.Fatalf("Sign via Transit: %v", err)
	}
	pk, err := bundle.ParsePublicKeyPEM(signer.PublicKeyPEM())
	if err != nil {
		t.Fatalf("parse transit pub: %v", err)
	}
	ring, err := bundle.NewRing(pk)
	if err != nil {
		t.Fatalf("ring: %v", err)
	}
	_, verification, err := bundle.Verify(ctx, env, ring, artifactHex, bundle.VerifyOptions{})
	if err != nil {
		t.Fatalf("Verify Transit-signed bundle: %v", err)
	}
	if verification.KeyID != signer.KeyID() {
		t.Fatalf("verified KeyID %q != signer %q", verification.KeyID, signer.KeyID())
	}
}
