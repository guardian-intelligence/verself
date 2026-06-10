package bundle

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	ocidigest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
)

const (
	// MediaTypeBundle is the sigstore bundle media type. It doubles as the OCI
	// referrer artifactType per the sigstore OCI 1.1 convention. We invent
	// nothing here, and stock cosign needs exactly this value to discover the
	// referrer. It is a routing hint only: no trust decision is made from it.
	MediaTypeBundle = "application/vnd.dev.sigstore.bundle.v0.3+json"

	maxEnvelopeBytes = 1 << 20
)

// Store is the read surface Discover needs; *remote.Repository satisfies it.
type Store interface {
	Predecessors(context.Context, v1.Descriptor) ([]v1.Descriptor, error)
	Fetch(context.Context, v1.Descriptor) (io.ReadCloser, error)
}

// Attach pushes the envelope as an OCI referrer of subject and returns the
// referrer manifest descriptor.
func Attach(ctx context.Context, target oras.Target, subject v1.Descriptor, env *Envelope) (v1.Descriptor, error) {
	staging := memory.New()
	layer, err := oras.PushBytes(ctx, staging, MediaTypeBundle, env.raw)
	if err != nil {
		return v1.Descriptor{}, fmt.Errorf("attestation: push bundle layer: %w", err)
	}
	manifestDesc, err := oras.PackManifest(ctx, staging, oras.PackManifestVersion1_1, MediaTypeBundle, oras.PackManifestOptions{
		Subject: &subject,
		Layers:  []v1.Descriptor{layer},
	})
	if err != nil {
		return v1.Descriptor{}, fmt.Errorf("attestation: pack referrer manifest: %w", err)
	}
	if err := oras.CopyGraph(ctx, staging, target, manifestDesc, oras.DefaultCopyGraphOptions); err != nil {
		return v1.Descriptor{}, fmt.Errorf("attestation: copy referrer to target: %w", err)
	}
	return manifestDesc, nil
}

// Discovery pairs an untrusted envelope with the digest of the OCI referrer
// manifest that carried it, so callers can record registry evidence
// coordinates alongside verification results.
type Discovery struct {
	Envelope *Envelope
	// ReferrerDigest is the referrer manifest digest in canonical
	// "sha256:<hex>" form. Like artifactType, it is transport metadata, not a
	// trust input.
	ReferrerDigest string
}

// Discover fetches every sigstore-bundle referrer of subject as locked
// Envelopes. The returned envelopes are untrusted until passed through Verify.
//
// A referrer that fails to parse as a single-layer sigstore bundle is SKIPPED,
// not fatal: artifactType is a routing hint, so a malicious or unrelated
// referrer tagged as a bundle must not be able to block discovery of a
// legitimate one (a registry could otherwise inject malformed siblings as a
// denial of service). If no valid bundle is found, the caller's verification
// step fails loudly on empty evidence.
func Discover(ctx context.Context, store Store, subject v1.Descriptor) ([]Discovery, error) {
	refs, err := store.Predecessors(ctx, subject)
	if err != nil {
		return nil, fmt.Errorf("attestation: list referrers for %s: %w", subject.Digest, err)
	}
	var found []Discovery
	for _, ref := range refs {
		// artifactType filtering is efficiency only (a routing hint); every
		// candidate that passes is still fully verified by the caller.
		if ref.ArtifactType != MediaTypeBundle {
			continue
		}
		env, ok := discoverBundle(ctx, store, ref)
		if !ok {
			continue
		}
		found = append(found, Discovery{Envelope: env, ReferrerDigest: ref.Digest.String()})
	}
	return found, nil
}

// discoverBundle fetches and structurally validates one bundle referrer. It
// returns ok=false (skip) rather than an error so one bad referrer cannot abort
// discovery of the rest.
func discoverBundle(ctx context.Context, store Store, ref v1.Descriptor) (*Envelope, bool) {
	manifestBytes, err := fetchLimited(ctx, store, ref)
	if err != nil {
		return nil, false
	}
	var manifest v1.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, false
	}
	if len(manifest.Layers) != 1 || manifest.Layers[0].MediaType != MediaTypeBundle {
		return nil, false
	}
	bundleBytes, err := fetchLimited(ctx, store, manifest.Layers[0])
	if err != nil {
		return nil, false
	}
	env, err := ParseEnvelope(bundleBytes)
	if err != nil {
		return nil, false
	}
	return env, true
}

// fetchLimited reads a descriptor's bytes under the guards proven in the tracer
// bullet: sha256 only, a 1 MiB cap, and digest verification of the fetched
// bytes.
func fetchLimited(ctx context.Context, store Store, desc v1.Descriptor) ([]byte, error) {
	if err := desc.Digest.Validate(); err != nil {
		return nil, fmt.Errorf("attestation: descriptor digest %q invalid: %w", desc.Digest, err)
	}
	if desc.Digest.Algorithm() != ocidigest.SHA256 {
		return nil, fmt.Errorf("attestation: descriptor %s digest algorithm is not sha256", desc.Digest)
	}
	rc, err := store.Fetch(ctx, desc)
	if err != nil {
		return nil, fmt.Errorf("attestation: fetch %s: %w", desc.Digest, err)
	}
	defer func() { _ = rc.Close() }()
	body, err := io.ReadAll(io.LimitReader(rc, maxEnvelopeBytes+1))
	if err != nil {
		return nil, fmt.Errorf("attestation: read %s: %w", desc.Digest, err)
	}
	if len(body) > maxEnvelopeBytes {
		return nil, fmt.Errorf("attestation: descriptor %s exceeds %d bytes", desc.Digest, maxEnvelopeBytes)
	}
	verifier := desc.Digest.Verifier()
	if _, err := verifier.Write(body); err != nil {
		return nil, fmt.Errorf("attestation: verify %s bytes: %w", desc.Digest, err)
	}
	if !verifier.Verified() {
		return nil, fmt.Errorf("attestation: descriptor %s bytes do not match digest", desc.Digest)
	}
	return body, nil
}
