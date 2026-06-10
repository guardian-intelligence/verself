# `src/attestation` — supply-chain attestation trust contract

This module is the single answer to one question, and only this question:

> **Is this a genuine, well-formed claim — made by a key in this ring — about these exact subject bytes?**

It never answers *"is this claim acceptable?"* Builder allowlists, source-repo policy, site/flavor
matching, channel rules, and admission decisions live in the consuming service
(`distribution-service` admission), operating on the **typed** statement this module returns after
verification. The moment policy leaks into this module, every consumer inherits every other
consumer's policy, and the public-trust subset stops being independently publishable.

We hand-roll **no cryptography**. Signing, DSSE envelope construction, the pre-authentication
encoding, signature verification, and trusted-root handling are all delegated to `sigstore-go` and
`sigstore/sigstore`. Our code is: a typed predicate model, a Transit-backed `Keypair`, a pinned-key
`Ring`, and OCI referrer attach/discover. Everything load-bearing for trust is upstream code that
every `cosign` user on the planet already exercises.

## Why this module exists

The pre-cutover pipeline emitted in-toto-*shaped* JSON with **no signature**. `BuilderID` and
`SignerIdentity` were self-asserted strings checked against themselves; the effective trust boundary
was registry write access. A verified tracer bullet proved the real path end to end (in-toto
statement → OpenBao Transit ECDSA P-256 via the sigstore hashivault provider → `sigstore-go` DSSE
bundle → OCI referrer in Zot → verified by **both** `sigstore-go` and stock
`cosign verify-blob-attestation --type slsaprovenance1`). This module is that path, productionized.

## Invariants (obligations, not suggestions)

1. **Bytes are sealed at construction; there is no re-marshal.** `protojson` output is deliberately
   non-deterministic (randomized whitespace). DSSE signs exact bytes. A `Statement` serializes
   **once**, in its constructor, and `Bytes()` returns that sealed buffer forever. A `Marshal()`
   method must never exist. Any code path that re-serializes after signing produces bytes that no
   longer verify.

2. **A *trusted* `Statement` comes only from `Verify`.** This kills parse-then-verify. The property
   is integrity, not confidentiality: an in-toto statement is public, unencrypted data that anyone
   can read straight off the registry, so `Envelope.Bytes()` exposing the bundle (it must, for OCI
   transport) is not a trust violation. What matters is that the *blessed path* — `bundle.Verify` —
   is the only way to obtain a `*predicate.Statement` whose signature has been checked, and
   internally its order is fixed: check payload type → verify signature against the ring → *then*
   `predicate.Parse` the verified bytes. `predicate.Parse` is exported only because `bundle` (a
   separate package) calls it; it is a decoder, not a trust boundary, and must never be called on
   unverified bytes. Consumers go through `bundle.Verify`, full stop.

3. **Closed registries.** Unknown `predicateType` fails `Parse`. Unknown `buildType` fails `Parse`.
   Predicates are typed structs — `map[string]any` appears in **no** exported signature (same drift
   argument as the OCSF builder rule; here drift means silently unverifiable claims). Adding a
   predicate type or build type means editing this module — that is the point, and it makes "what
   claims our supply chain can express" a code-reviewable question.

4. **One signature algorithm: ECDSA P-256.** Proven against Transit + cosign with no prehash edge
   cases. DSSE has no algorithm field in the envelope (the key determines the algorithm), so
   supporting exactly one algorithm eliminates the algorithm-confusion class rather than defending
   against it.

5. **Key IDs are derived, never assigned.** A key ID is `hex(sha256(DER-encoded SPKI public key))`,
   lowercase. Two configs cannot disagree about what "the prod key" means, and a signer cannot claim
   another key's identity. The DSSE/bundle `keyid` hint is **unauthenticated** — `Verify` may use it
   to select a candidate key, but trust comes only from the signature verifying against a ring key.

6. **Annotations and `artifactType` are unverified routing hints.** `Discover` may filter on the
   bundle media type to skip non-bundle referrers (stock cosign needs it to discover). Nothing
   downstream may make a trust or policy decision from a manifest annotation or `artifactType`.
   Truth lives only inside the verified envelope.

7. **An empty ring is a construction error, not a verify-everything ring.** Fail closed.

## Layering

```
predicate/   L1  typed sealed statements; in-toto + SLSA protos; go-digest. Nothing else, ever.
bundle/      L2+L3  Transit Keypair (sign) · pinned-key Ring (verify) · oras attach/discover.
conformance/ test-only  negative vectors; interop gate vs upstream + (local-only) stock cosign.
```

`predicate` may not import `bundle`. `bundle` is the only place the bundle/predicate media-type and
`artifactType` constants exist (they were duplicated string literals across three packages
pre-cutover — do not re-scatter them).

## Transit signer is server-side only

The OpenBao Transit `Keypair` lives behind a Bazel `visibility` allowlist (deployment-service,
mksk-release, trusted-builder targets). A verifier binary that tries to link the signing path must
fail at build time. Visibility is the enforcement; this sentence is not.

## Two trust audiences, two keys

- `deployment-signing` — offline, internal audience. Verified with `WithNoObserverTimestamps`. No
  Rekor.
- `release-signing` — public audience. Verified with transparency-log inclusion against the Sigstore
  trusted root. Releases are public claims about public bytes, so they earn free externally-operated
  transparency. The Sigstore trusted root is the single piece of third-party trust in the design; it
  sits only on the release path and is pinned/updated through normal dependency bumps.

## Conformance is the public-trust regression test

`conformance/` enforces one gate today: every fresh envelope verifies under the upstream
`go-securesystemslib` verifier, alongside negative controls (wrong key, tampered payload, malformed
referrer). A second, best-effort local-only gate runs `cosign verify-blob-attestation
--new-bundle-format --type slsaprovenance1` against a freshly signed envelope when `cosign` is on
`PATH`; it `t.Skip`s otherwise, so hermetic `bazelisk test` does NOT exercise it — cosign is not yet
a Bazel-pinned binary dependency. If cosign cannot verify our bundle, customers' stock tooling
cannot either, so run the interop test locally with cosign installed before changing signing format.
There are no checked-in golden vectors yet. Hardening this section means: pin cosign as a Bazel data
dependency of the conformance test, check in golden signed envelopes + public keys, and fail (not
skip) when the gate cannot run. Because `protojson` is non-deterministic, any future golden vectors
must assert **parsed equality** plus **byte-exact verification** of checked-in envelopes — never
byte equality of a fresh marshal — and are append-mostly; editing an existing vector is a
signing-format change requiring explicit sign-off.

## Upgrade paths (descoped now, format already supports them)

- **Keyless / Fulcio:** bundles carry a certificate slot; swap the `Keypair` for the Fulcio flow and
  flip admission from key-ID policy to cert-identity policy. No format change.
- **Self-hosted Rekor:** bundles carry the tlog-entry slot; point trusted material at a private log.
- **RFC 3161 timestamps / TPM-quote-gated ephemeral keys:** slots exist; the
  `release-architecture.md` two-level delegation is the documented future, not current scope.
