# Secure Documents Architecture

This document describes the target architecture for `secure-documents-service`: encrypted document custody, controlled sharing, and PAdES-grade electronic signatures.

The core thesis is:

```text
PostgreSQL says what envelopes, recipients, fields, share links, user signature assets, audit chain heads, and audit outbox events exist, and what state they are in.
ClickHouse stores the query-optimized projection of the Postgres-authored HMAC-chained audit ledger that mirrors governance-service's contract.
Object storage holds ciphertext blobs only. The plaintext data-encryption key is never persisted.
secrets-service holds the org KEK, per-recipient per-envelope signing keys, audit HMAC keys, and scoped one-time signing grants. Private key material never enters this service.
pyHanko owns the cryptographic PDF surface: digest, CMS assembly, TSA round-trip, OCSP/CRL embedding, archive timestamping. It runs in-process; the actual signing primitive is a delegated SPIFFE call to secrets-service.
Recipients prove identity at a declared assurance level before any signing call is honored. Sealed envelopes are evidence and are tombstoned, never deleted.
```

`secure-documents-service` is a Python 3.13 service built on Litestar 2.x, talking SQLAlchemy 2.0 to one Postgres database it owns, procrastinate to the same database for background jobs, and `clickhouse-connect` to the shared ClickHouse cluster for audit projection.

Reference points in this repo:

- `src/platform/docs/secrets-service.md` for KEK custody, key wrap/unwrap, and short-lived signing key issuance.
- `src/platform/docs/identity-and-iam.md` for Zitadel-fronted user auth, machine identity, and SCIM org boundaries.
- `docs/architecture/workload-identity.md` for SPIFFE/SPIRE trust domain, x509-SVID issuance, and how Python services bind a workload-identity-aware HTTP client.
- `src/governance-service/docs/audit-data-contract.md` for the OCSF-flavored audit-ledger schema this service emits into.
- `src/domain-transfer-objects/docs/wire-contracts.md` for the wire-shape conventions and generated-client patterns the public surface must obey.
- `src/mailbox-service/docs/inbound-mail.md` for the transactional outbound mail surface used to deliver envelope invitations.

External standards consulted directly when implementing:

- ETSI EN 319 142-1 (PAdES baseline profiles B-B / B-T / B-LT / B-LTA).
- ISO 32000-2 (PDF 2.0 signature dictionary, DocMDP, DSS).
- RFC 5652 (CMS), RFC 3161 (Time-Stamp Protocol), RFC 5280 (X.509 path validation).
- ETSI TS 119 612 (EU Trusted Lists) when integrating a QTSP for QES.
- Cloud Signature Consortium API v2.2 when integrating a remote-QSCD signer.
- NIST SP 800-63B-4 for authenticator assurance, restricted authenticators, and phishing-resistant passkey posture.
- Commission Implementing Regulation (EU) 2026/248 for recognised advanced electronic signature formats in EU public-sector contexts.

## Scope and non-goals

In scope:

- Encrypted custody of customer-uploaded documents with strict access control.
- Two document custody modes: server-managed custody for signing, watermarking, no-download enforcement, and page rasterization; client-custodied blind sharing for standalone documents whose DEK never reaches the server.
- PAdES B-LT signing by default, B-LTA available, B-T available for low-stakes flows. Multi-recipient envelopes with parallel or sequential routing.
- Per-recipient identity-assurance enforcement.
- Tamper-evident audit ledger authoring and projection, integrated with governance-service.
- A user-owned signature asset library (drawn / typed / uploaded) with separate full-signature and initials variants.

Out of scope for v1:

- Qualified Electronic Signatures (QES). The architecture leaves a seam at the `Signer` boundary so a `CSCSigner` against a QTSP can replace `SecretsServiceSigner`, but QES is not a v1 assurance enum because it requires a QTSP, QSCD semantics, and a different signing activation ceremony.
- Anchor-text-based field auto-placement. Fields are placed by absolute coordinates from a graphical placement UI in the frontend.
- DocSend-grade granular reading-analytics (per-page dwell time, scroll heatmaps). The audit ledger records view/download/sign events at the document level only.
- Notarization, witnessing, and seal-of-corporate-officer flows.
- E-signatures on non-PDF formats. Inputs other than `application/pdf` are rejected at upload.

## System roles

| System | Role |
|---|---|
| PostgreSQL (`secure_documents`) | Source of truth for documents, envelopes, envelope_documents, recipients, fields, signatures, share_links, user_signatures, signing_sessions, tombstones, audit_chains, secure_document_audit_outbox, and procrastinate job rows. |
| ClickHouse (`secure_document_events`) | Append-only projection of every committed envelope, document, recipient, share-link, view, sign, decline, void, expire, tombstone, and key-unwrap audit event. HMAC chain authorship lives in Postgres. |
| Object storage | Ciphertext-only blob storage. Stores original uploads, sealed signed revisions, audit-trail PDFs, and rasterized-page caches keyed by `(document_id, revision)`. |
| secrets-service | Holds the org KEK; performs DEK wrap/unwrap. Mints short-lived signing certificates per recipient per envelope from the platform CA. Performs grant-bound signature primitives and audit-chain HMAC operations over SPIFFE so that private keys and HMAC keys never enter this service. |
| iam-service | Issues step-up authentication tokens (passkey / SMS-OTP / OIDC re-auth) that signing requests must present. Owns user identity, recipient↔user binding, and recipient invitation routing for internal users. |
| mailbox-service | Outbound envelope-invitation, signing-link, completion, and decline notifications. |
| governance-service | Consumes this service's committed audit outbox stream and ingests it into the cross-service audit ledger. |
| billing-service | Consumes envelope-completed and signature-completed events for usage metering and per-org showback. |
| pyHanko | In-process Python library performing PAdES digest, CMS assembly, TSA round-trip, OCSP/CRL embedding, and DocTimeStamp. Stateless across requests. |

## Non-negotiable invariants

- Every customer-visible document state must be derivable from PostgreSQL rows. ClickHouse is evidence and read-model; it must not be required to authorize uploads, render signatures, or decide whether a share link is valid.
- The plaintext data-encryption key (DEK) for any server-managed document must never be persisted to disk on this service. Python cannot provide a hard zeroization guarantee, so the security boundary is no plaintext persistence: no plaintext object-storage writes, no plaintext logs or traces, no swap/core dumps for service workers, tmpfs-only temporary files, and best-effort clearing of mutable buffers where the library surface permits it.
- Private signing keys must never enter this service. The pyHanko `Signer` implementation is `SecretsServiceSigner`, whose `async_sign_raw` is a SPIFFE-mTLS POST to secrets-service against a one-time signing grant. The signing certificate is short-lived, minted per `(envelope_id, recipient_id)`, and revoked when the envelope is voided or the recipient's signing authority is revoked.
- Every signing call must verify, before sealing, that the recipient has presented a step-up authentication token that meets or exceeds the envelope's required identity-assurance level. Sealing without a valid step-up token is a hard error, not a degraded mode.
- Sealed PDFs in completed envelopes are evidence. They must not be hard-deleted, mutated, or re-signed. The only allowable destructive operation is tombstoning, which replaces the blob with a tombstone marker, retains all metadata and audit-chain rows, and writes a `document.tombstoned` event into the ledger with the operator identity and stated reason.
- Pre-completion documents may be tombstoned by the initiator without operator override; sealed documents in completed envelopes may be tombstoned only by an org owner role and only with a non-empty reason that is recorded in the audit chain.
- The audit ledger is append-only, HMAC-chained per envelope or standalone document, and authored in Postgres in the same transaction as the corresponding state transition. A state mutation whose audit outbox row cannot be committed is a 5xx; partial state with no audit row is forbidden. ClickHouse projection is asynchronous and retryable.
- The blob URI in `documents` is a ciphertext URI. It must not point to plaintext for any document, including drafts and uploads-in-progress. Server-managed uploads stream through an authenticating proxy that encrypts with a freshly-minted DEK before the bytes hit object storage; client-custodied uploads arrive already encrypted by the browser.
- Client-custodied documents must not have a server-held DEK wrap. Server-blind share links (`client_encrypted = TRUE`) are valid only for those documents, and every handler for them must refuse to call secrets-service unwrap, refuse server-side transformations, and serve ciphertext only.
- A share link's effective access level is the intersection of the link's declared permissions and the recipient's identity-assurance evidence at access time. Declared permissions are an upper bound, not a guarantee.
- Every cryptographic configuration choice (PAdES level, digest algorithm, signature algorithm, signing certificate thumbprint, certificate serial, signing key ID, TSA URL, OCSP responder, validation context root set, and validation-policy version) is recorded in the `signatures` row at sealing time. Forward changes to platform defaults must not retroactively alter what an old signature claims.
- Generated clients are the only supported consumer SDKs. The Litestar `openapi.json` is committed and the wire-contract gate forbids drift.

## Domain model

### Envelope

The unit of signing intent. An envelope is created in `draft`, transitions to `sent` when the initiator dispatches invitations, advances through `in_progress` as recipients sign, and reaches a terminal state of `completed`, `voided`, or `expired`.

An envelope contains one or more documents (`envelope_documents`) and one or more recipients (`recipients`). Routing is `parallel` (all signers may sign in any order) or `sequential` (next routing-order signer is invited only after the previous signer completes). Required identity-assurance is declared at the envelope level (`required_assurance`) and may be raised per-recipient.

The envelope record carries the chosen PAdES level (`pades_level`), defaulting to `B-LT`. The level is locked at the moment the envelope leaves `draft`; subsequent signatures within the envelope use that level. Mixed-level envelopes are not supported.

### Document

A document is the file. The original upload is one document row; each sealed revision produced by a signing event is another document row, linked back to its parent by `derived_from_id`. The chain forms a strict revision history per envelope-document slot, where `envelope_documents.current_document_id` advances through the chain as recipients sign. Finalization serializes on the envelope-document slot so two parallel signers cannot advance from the same parent revision.

Documents are also a stand-alone resource: an org may upload a document and create share links against it without ever creating an envelope. This is the "secure document custody" half of the service. Standalone documents choose `custody_mode = 'server_managed'` or `custody_mode = 'client_custodied'` at creation and cannot be converted silently.

### Recipient

A recipient is bound either to a verified internal user (`user_id`) in the envelope's org, or to an external email address (`email`). Exactly one of the two is set; the constraint is enforced at the database. External recipients become internal users automatically when they accept their first invitation and complete identity verification within the envelope's org. A recipient who turns out to be a verified user in a *different* Verself org is not auto-bound; the recipient row remains email-typed, and the cross-org user identity is captured on the `signatures` row at sign time as evidence (`signer_verified_user_id`, `signer_verified_org_id`).

Each recipient declares a `role` (`signer`, `approver`, `cc`, `viewer`) and, for sequential routing, a `routing_order`. Approver and viewer roles do not produce cryptographic signatures; their actions are recorded only as audit-ledger events.

`required_assurance` is per-recipient and is raised, never lowered, from the envelope's level.

### Field

A field is a placement of an input on a specific page of a specific document, bound to a specific recipient. Field kinds are `signature`, `initials`, `date_signed`, `text`, and `checkbox`. Coordinates are PDF user-space, lower-left origin, in points; rotation and CTM math live in the field-placement UI, not the database. File attachments are out of v1 because they need their own custody, malware-scanning, signing, and retention semantics.

A `signature` field carries a `user_signature_id` reference once the recipient chooses which saved signature asset to use. A `text` field carries encrypted filled-value material once filled. Fields are not the cryptographic signature artifacts; they are the input placeholders that drive the visible-stamp rendering inside pyHanko.

### Signature

The cryptographic signature artifact, one row per CMS object embedded in a sealed PDF. Each row records the parent (pre-sign) and child (post-sign) document IDs, their content hashes, the PAdES level, the signer's certificate thumbprint, certificate serial, PEM, signing key ID, signing grant ID, TSA URL, timestamp-token bytes, signed-attributes digest, and the signer's evidence (IP, user agent, geohash, step-up method, step-up token JTI).

When the signer authenticates as a verified user in *some* Verself org at the moment of signing — even an org other than the envelope's owning org — the (`signer_verified_user_id`, `signer_verified_org_id`) pair is recorded alongside the email binding. This is the service's signer-identification evidence at sign time without mutating the `recipients` row or implying any cross-org collaboration surface.

`signatures.signer_cert_pem` is stored explicitly so that any future verification can run offline against the embedded cert chain without depending on secrets-service availability.

### Share link

A capability token that grants bounded access to either an envelope (signing or viewing) or a standalone document (viewing). The raw token is delivered to the recipient out of band (email or in-app) and is never stored on the server; only `token_hmac` is stored, computed under a per-org HMAC key held in secrets-service.

A share link has two modes, constrained by the document's custody mode:

- `client_encrypted = FALSE` — server-mediated. The server unwraps the DEK and serves either decrypted bytes or a per-request transformation (watermarked image strip, rasterized page, redacted view). All "no-download" and "watermark" behaviors require this mode.
- `client_encrypted = TRUE` — server-blind. The document itself is client-custodied, so `documents.wrapped_dek` is null and secrets-service has no DEK to unwrap. The server stores only the ciphertext blob plus `link_wrapped_dek` and its public wrap parameters; the browser derives the unwrap key locally from the URL fragment, unwraps the DEK, and decrypts the document in-browser.

The modes do not coexist on the same document. A server-managed document may have server-mediated links only. A client-custodied document may have server-blind links only. Creating a server-managed revision from a client-custodied document is an explicit custody conversion that requires the client to upload plaintext to the server, emits `document.custody_converted`, and produces a new document row rather than mutating the original.

### User signature

A user-owned, reusable signature asset. Lives in this service rather than `profile-service` because every read of a saved signature happens during a signing flow this service owns. A user has zero or more `full` signatures and zero or more `initials` signatures. At signing time the recipient picks one of each (or creates a new one inline; the inline-created asset is persisted before the signing call is honored).

Storage carries encrypted object-storage blobs for the canonical PNG (transparent background, server-side render of the asset for visual stamping) and the original input data: stroke vectors for `drawn` (so the asset can be re-rendered at any DPI), text and font key for `typed`, source bytes for `uploaded`. The PNG is what flows into the pyHanko `StaticStampStyle`; the source blob is the editable source.

### Signing session

A short-lived row created by `POST /envelopes/{id}/recipients/{rid}/signing-sessions` and torn down on success or expiry. It carries the signing intent, encrypted chosen field values, signature asset references, the step-up token presented at session creation, the parent revision set, and the in-flight pyHanko state (the prepared digest, the signed-attributes blob, the signing grant ID, and the temp-blob URI of the in-progress sealed PDF). Sessions have a TTL of 15 minutes; expired sessions are reaped by a procrastinate job.

The session row exists so the signing call is idempotent: if the network drops between digest preparation and finalization, the recipient retries with the same session ID and the prepared state is reused rather than recomputed.

## Cryptographic model

### PAdES level selection

The default level is `B-LT`. Rationale: low-volume API, contracts must remain verifiable for years, OCSP responders for the platform CA may be retired, and the EU OCSP-fetch overhead at signing time is acceptable on a low-volume path.

`B-LTA` is available on a per-envelope flag for documents whose evidentiary horizon exceeds typical CA cert lifetimes (10+ years). It adds a DocTimeStamp covering the DSS dictionary and is refreshable by a procrastinate job before each contained timestamp's expiry.

`B-T` is available for low-stakes flows where OCSP fetching at sign time is undesired. It does not survive cert rotation; the validation chain must be in scope at verification time.

Every signature records its level in `signatures.pades_level`. Forward changes to default level do not migrate prior signatures.

### Signer identity

For each signer in each envelope, secrets-service mints a short-lived X.509 leaf certificate from the platform CA. The certificate is bound to `(org_id, envelope_id, recipient_id, signer_email_hash, signer_verified_user_id?, signer_verified_org_id?)` via a custom `id-pe` extension, has a TTL just longer than the envelope's expected lifetime plus a DSS-fetch buffer, and is revoked when the envelope is voided or the recipient's signing authority is revoked. The corresponding private key lives only in secrets-service.

This produces a PAdES signature with strong platform evidence for the eIDAS Advanced Electronic Signature properties: signer identification, signer-envelope binding, signing-time authentication evidence, and tamper detection. It does not assert QES and does not rely on a QTSP or QSCD. The remaining AdES legal argument depends on the recipient-specific key, the one-time signing grant, and the step-up ceremony establishing signer control of that signing operation. QES requires a Qualified Trust Service Provider (QTSP) and a Qualified Signature Creation Device (QSCD), neither of which this service operates. The seam for adding QES later is the `Signer` boundary: a `CSCSigner` against a QTSP replaces `SecretsServiceSigner` for the cryptographic operation and signing activation evidence.

### Custom pyHanko Signer

The `pyhanko.sign.signers.pdf_cms.Signer` subclass `SecretsServiceSigner` overrides one method. It never asks secrets-service to sign arbitrary caller-selected bytes by key ID. The request is scoped by a one-time signing grant created after the signing session, step-up token, parent document content HMAC, and pyHanko signed-attributes digest are known.

```python
class SecretsServiceSigner(Signer):
    def __init__(self, signing_cert, cert_registry, signing_grant_id, client):
        super().__init__(signing_cert=signing_cert, cert_registry=cert_registry)
        self.signing_grant_id = signing_grant_id
        self._client = client  # SPIFFE-mTLS-bound httpx.AsyncClient

    async def async_sign_raw(self, data, digest_algorithm, dry_run=False):
        if dry_run:
            return b"\x00" * self.SIGNATURE_SIZE   # size estimation
        resp = await self._client.post(
            f"/internal/v1/signing-grants/{self.signing_grant_id}:sign",
            json={
                "digest_algorithm": digest_algorithm,
                "data_b64": base64.b64encode(data).decode("ascii"),
            },
            timeout=1.0,
        )
        resp.raise_for_status()
        return base64.b64decode(resp.json()["signature_b64"])
```

secrets-service validates that the SPIFFE caller is `secure-documents-service`, the grant is unexpired, the grant has not been consumed with different bytes, the digest algorithm matches the grant, and `SHA-256(data)` matches the grant's `signed_attrs_sha256`. A retry with the same grant and same digest returns the same signature response; a retry with different bytes is a critical audit event.

The remaining pyHanko orchestration (PAdES level wiring, TSA, validation context, DSS update, DocTimeStamp) runs in-process. The `data` argument is the digest of the CMS signed-attributes blob, not the document digest; conflating the two is a footgun that produces syntactically valid PDFs whose signatures fail verification.

### Signing pipeline

For every recipient signing event:

1. Validate the signing session and the step-up token; reject if assurance is below the envelope's required level.
2. Render the visible stamp PDF from the chosen `user_signature` PNG plus auto-generated date and recipient-name lines.
3. Read the target `envelope_documents` rows and record `(ordinal, current_document_id, current_document_hmac_sha256)` into the signing session as the parent revision set.
4. Fetch and decrypt each parent document revision from object storage.
5. Construct `PdfSigner` with `PdfSignatureMetadata(subfilter=PADES, embed_validation_info=…, use_pades_lta=…)`, the `SecretsServiceSigner`, an `HTTPTimeStamper`, and a pre-warmed `ValidationContext` containing the platform CA roots and a fresh OCSP cache.
6. Ask pyHanko to prepare the signed attributes, then create a secrets-service signing grant bound to `(org_id, envelope_id, recipient_id, signing_session_id, parent_revision_set, signed_attrs_sha256, digest_algorithm, pades_level, step_up_token_jti)`.
7. Run pyHanko finalization. pyHanko calls `async_sign_raw`, secrets-service validates and consumes the grant, assembles the CMS signature, requests the timestamp, fetches and embeds OCSP/CRL data for B-LT, and stamps the DocTimeStamp for B-LTA.
8. In one Postgres transaction, lock each affected `envelope_documents` row `FOR UPDATE` and verify its `current_document_id` still equals the parent revision in the signing session. If not, discard the sealed temp blob and return a typed `signing.parent_revision_stale` error so the client can reprepare against the new revision.
9. Persist the sealed bytes as new ciphertext blobs, write new `documents` rows with `derivation_kind = 'sealed'`, advance `envelope_documents.current_document_id`, insert the `signatures` rows, update the recipient state, and append the HMAC-chained audit outbox rows.
10. Commit the transaction before returning success. If this completes the envelope, enqueue audit-trail rendering in the same transaction; the job appends the audit-trail PDF as `derivation_kind = 'audit_trail'` and writes `envelope.completed` through the same audit outbox path.

### Trust roots and OCSP caching

`ValidationContext` is constructed once per worker process and refreshed on a schedule (`procrastinate` job, hourly). It carries the platform CA roots, the TSA's CA roots, and an OCSP responder cache backed by Postgres (`ocsp_responses` table, TTL on `next_update`). OCSP requests during signing first consult the cache; cache misses fetch directly and write through.

Without the cache, a B-LT signature performs at least two synchronous OCSP fetches per signing event (signer cert chain + TSA cert chain), turning every signing call into a ~200ms-baseline north–south round-trip.

### PDF intake hardening

Server-managed uploads are parsed before they can enter an envelope. The intake worker rejects encrypted PDFs, non-PDF MIME/content mismatches, malformed cross-reference tables, incremental-update chains above a small fixed limit, JavaScript actions, launch actions, embedded files, XFA forms, external references, oversized page boxes, page-count bombs, object-count bombs, and decompression ratios above policy. The accepted artifact is a normalized PDF revision used for all later signing and viewing.

Parsing, rasterization, and normalization run in an isolated worker process with no network access, tmpfs scratch, disabled core dumps, tight CPU and memory limits, and an allowlisted syscall profile. The service records the intake policy version, normalized `plaintext_hmac_sha256`, `ciphertext_sha256`, page count, and parser result in the audit ledger. Client-custodied documents cannot receive the same server-side intake guarantees because plaintext never reaches the server; their displayed MIME and page metadata are untrusted client claims.

## Encryption-at-rest

A server-managed document blob in object storage is always ciphertext. The plaintext key hierarchy is:

```
plaintext bytes
    │  AES-256-GCM, per-document DEK (32 bytes, fresh on upload)
    ▼
ciphertext blob ──────────────────────────► object storage
    │
    │  DEK ──► secrets-service /v1/keys/wrap (org KEK)
    ▼
wrapped_dek ──────────────────────────────► documents.wrapped_dek
```

Read path: fetch ciphertext bytes; fetch `wrapped_dek` from the documents row; call `secrets-service /v1/keys/unwrap` over SPIFFE; AES-GCM decrypt locally; stream plaintext to whatever transformation is downstream (signing pipeline, watermark renderer, range-served viewer). The DEK is held in process memory only for the duration of the operation. The unwrap call is the audit anchor: every plaintext access is observable to governance-service via secrets-service's existing audit emission.

Per-document DEK rotation is supported via re-encrypt: a job streams ciphertext, unwraps the old DEK, decrypts, re-encrypts under a new DEK, wraps the new DEK, atomically updates `wrapped_dek` and writes the new blob; the old blob is garbage-collected after a grace period. KEK rotation is a secrets-service concern; this service participates by re-wrapping each document's DEK under the new KEK on a schedule.

## Share-link encryption

Share-link mode follows document custody mode. Server-managed documents use server-mediated links. Client-custodied documents use server-blind links. The distinction is a security boundary, not a rendering preference.

### Server-mediated mode (`client_encrypted = FALSE`)

The default mode. The server unwraps the DEK on each access, decrypts the blob, and either streams the plaintext to the recipient or applies a per-request transformation:

- **`no_download = TRUE`**: server rasterizes pages on demand (PDF.js headless or `pdf2image`), composites a per-request watermark from `watermark_template` interpolated against `{recipient_email, ts, ip, geohash}`, and serves PNG strips. The plaintext PDF never reaches the browser. Rasterized pages are cached keyed by `(document_id, page, watermark_payload_hash)` with a short TTL.
- **`no_download = FALSE`**: server streams the decrypted PDF directly. Watermark is composited into the PDF via pyHanko's stamp pipeline before streaming.

Server-mediated mode is required for envelope signing flows because the signer must see the document the server can render and the server must be able to insert the visible signature stamp.

### Server-blind mode (`client_encrypted = TRUE`)

Send-style, but only for `documents.custody_mode = 'client_custodied'`. The document DEK is generated in the browser, the PDF is encrypted in the browser before upload, and the server never receives either plaintext or a secrets-service-wrapped copy of the DEK.

```
share-link URL: https://documents.<domain>/s/{token}#{K_b64url}
```

At client-custodied document upload:

1. The browser generates a 32-byte document DEK using WebCrypto.
2. The browser encrypts the PDF with AES-256-GCM. V1 uses whole-blob encryption; resumable range decryption requires a future chunked format.
3. The browser uploads ciphertext plus encryption metadata (`client_ciphertext_alg`, nonce, ciphertext length, `ciphertext_sha256`). The server does not receive a raw plaintext hash, `plaintext_hmac_sha256`, or `wrapped_dek`.
4. The server stores a `documents` row with `custody_mode = 'client_custodied'`, `wrapped_dek IS NULL`, and `kek_id IS NULL`.

At link creation the browser:

1. Generates `K` randomly (32 bytes from WebCrypto) and a random `link_wrap_salt`.
2. Derives `unwrap_key = HKDF-SHA-256(K, salt = link_wrap_salt, info = b"verself.share-link.dek-wrap")`.
3. Computes `link_wrapped_dek = AES-256-GCM-encrypt(document_dek, key = unwrap_key, nonce = random_12)`.
4. Sends `link_wrapped_dek`, `link_wrap_nonce`, `link_wrap_salt`, and link policy to the server. The request never includes `K`, `unwrap_key`, or `document_dek`.
5. The server generates the raw path token, persists the share link, stores only `token_hmac` for that token, and returns the raw path token to the browser.
6. The browser composes the URL from the raw path token and `K` in the fragment, then delivers it to the recipient.

At access time the browser:

1. Parses `K` from `window.location.hash`.
2. Calls `GET /s/{token}/blob`. The fragment is not sent over HTTP. The server validates the token, authenticates the recipient when required, and returns ciphertext bytes plus `link_wrapped_dek`, `link_wrap_nonce`, `link_wrap_salt`, and `link_wrap_alg`.
3. Derives `unwrap_key` locally from `K` and the returned `link_wrap_salt`.
4. AES-GCM-decrypts `link_wrapped_dek` to recover the DEK.
5. AES-GCM-decrypts the ciphertext to recover the plaintext PDF.
6. Renders the PDF in-browser (PDF.js).

Backend-held data is insufficient to decrypt a client-custodied document at rest. That guarantee assumes the server does not later serve malicious JavaScript to exfiltrate `K` from a recipient's browser. The share page therefore has no third-party scripts, uses `Referrer-Policy: no-referrer`, has a narrow CSP, records the frontend asset digest in the `share_link.served_ciphertext` event, and treats any frontend-bundle drift on this route as a high-risk deployment.

As a consequence:

- Watermarking, no-download enforcement, and page rasterization are not available for these links. The browser holds the plaintext bytes; the server cannot police what it does with them.
- Audit events for these shares record access (token presented, ciphertext served) but not viewing.
- Compromise of the blob store leaks ciphertext only.
- Loss of `K` is unrecoverable. If the document creator no longer has the document DEK in the browser session or in a future client-side keyring, the link cannot be regenerated without re-uploading or explicitly converting to server-managed custody.
- Revocation stops future ciphertext service. It cannot recall recipients' cached ciphertext, fragments, or decrypted plaintext.

Both modes write distinct event kinds into the audit ledger (`share_link.served_ciphertext` for client-encrypted, `share_link.served_plaintext_view` for server-mediated) so the difference is auditable.

## Identity assurance ladder

Recipients prove identity at one of three v1 levels:

| Level | Evidence | Where the token comes from |
|---|---|---|
| `email` | A signed link delivered to the recipient's email and clicked. This is delivery evidence, not a strong authenticator. | Token issued by this service against the recipient row at `envelope.sent`. |
| `sms_otp` | An OTP delivered via SMS and verified at access time. SMS is a restricted fallback because it is not phishing-resistant and is exposed to SIM-swap and number-porting attacks. | iam-service `/v1/step-up/sms`. |
| `passkey` | WebAuthn assertion at access time, bound to the recipient's user identity. | iam-service `/v1/step-up/passkey`. |

Levels are totally ordered: `email < sms_otp < passkey`.

`envelopes.required_assurance` is a floor. `recipients.required_assurance` may raise but not lower the floor for that recipient.

The signing endpoint requires a `step-up token`: a short-lived JWT issued by iam-service, bound to `(envelope_id, recipient_id, method, issued_at)`, with a TTL of 10 minutes. The signing handler:

1. Verifies the token's signature and binding against this envelope-recipient pair.
2. Verifies the token's declared method meets or exceeds `recipients.required_assurance`.
3. Records the token JTI in `signatures.step_up_token_jti` for audit.
4. Refuses with a typed error otherwise.

A recipient may also be required to present step-up evidence merely to view a high-assurance envelope, even if no signing is performed; this is configured via `share_links.required_assurance` and enforced at access time.

The `email` level is meaningful evidence in low-stakes US contexts but is the floor. The enterprise default is `passkey`. Org owners may lower the default to `sms_otp` or `email` only through an explicit risk-acceptance setting that is recorded in governance audit; per-envelope lowering below the org default is not supported.

## Audit ledger

The authoritative ledger is a pair of Postgres tables, `audit_chains` and `secure_document_audit_outbox`, written in the same transaction as the domain mutation. ClickHouse stores the query-optimized `secure_document_events` projection. Every state-mutating operation commits its audit outbox rows before the HTTP response is returned. Failure to commit the audit row is a 5xx and the state mutation is rolled back.

### Hash chain

For each envelope or standalone document, events are HMAC-chained through a single locked chain head:

```
event_hmac = HMAC-SHA-256(
    key   = per-org-HMAC-key (held in secrets-service),
    data  = "v1" || chain_id || sequence || event_id || prev_hmac ||
            action || event_time || SHA-256(canonical-json(payload))
)
```

`prev_hmac` for the first event in a chain is 32 zero bytes. Subsequent events are appended by locking `audit_chains` with `SELECT ... FOR UPDATE`, incrementing `last_sequence`, asking secrets-service to HMAC the canonical input, inserting the outbox row, and updating the chain head in the same transaction. Standalone-document events use their own chain scoped to `(org_id, document_id)`.

Chain verification is a procrastinate job (`audit.verify_chain`) that walks each chain in `sequence` order and recomputes each `event_hmac` through secrets-service. Mismatches, sequence gaps, unexpected duplicate sequence numbers, or missing ClickHouse projections produce a `governance-service` incident.

The ledger format is OCSF-flavored and was designed to be a clean ingest into governance-service's existing cross-service audit ledger. governance-service consumes committed outbox rows via the internal SPIFFE-only `/internal/audit/export` route. A projector writes the same committed rows to ClickHouse with retry and idempotent `event_id` handling.

### Event taxonomy

```
document.uploaded
document.tombstoned
envelope.created
envelope.sent
envelope.viewed                    -- by initiator, in-app
envelope.completed
envelope.voided
envelope.expired
recipient.added
recipient.invited
recipient.viewed                   -- envelope viewed by a signer
recipient.signed
recipient.declined
recipient.bounced                  -- email bounce
field.placed
field.filled
share_link.created
share_link.revoked
share_link.served_plaintext_view
share_link.served_ciphertext
user_signature.created
user_signature.archived
key.unwrapped                      -- DEK unwrap, mirrors secrets-service event
document.custody_converted
chain.verified
chain.mismatch_detected
```

Each event carries a JSON `payload` with event-kind-specific fields. The set is closed; new event kinds require a deploy and a registry update.

## Lifecycle and tombstones

### State transitions

```
envelopes:
  draft        --send-->        sent
  sent         --first sign-->  in_progress
  in_progress  --last sign-->   completed
  any non-terminal --void-->    voided
  any non-terminal --past expires_at--> expired
```

Transitions are application-driven; the database has check constraints but no triggers. Each transition writes a Postgres audit outbox event in the same transaction before the HTTP response.

Re-opening a `voided` or `expired` envelope is not supported; the initiator clones the envelope into a new draft.

### Tombstones

Hard deletion of any document is forbidden. Two operations exist:

- **Soft-delete** (`documents.deleted_at`): marks the row as not visible to standard listing endpoints. Reversible by org owner. Available on uncompleted documents only. The blob is retained.
- **Tombstone** (`documents.tombstoned_at`, `documents.tombstone_reason`, `documents.tombstoned_by`): replaces the ciphertext blob with a fixed tombstone marker, retains the row, retains all associated `signatures`, `fields`, `envelope_documents`, and audit events. Irreversible.

Tombstone authorization:

- A document not yet referenced by any sealed signature may be tombstoned by its uploader or an org admin.
- A document referenced by any sealed signature in a `completed` envelope may be tombstoned only by an org owner role with a non-empty `tombstone_reason`. The reason is recorded verbatim in the `document.tombstoned` event payload.
- A document referenced by any sealed signature in a non-`completed` envelope cannot be tombstoned until the envelope reaches a terminal state.

A tombstoned document still appears in audit-ledger queries, in `signatures` rows for verification purposes, and in `envelope_documents` slot history. Only the bytes are gone.

### Retention

This service does not enforce platform-wide retention policy. Org-level retention rules live in governance-service and trigger tombstone calls into this service via the internal route `/internal/documents/{id}/tombstone`. The default policy is no retention limit; sealed documents persist until explicitly tombstoned.

## Service shape

### Stack

- **Runtime**: Python 3.13, uvicorn workers (one per CPU on the bare-metal node).
- **Framework**: Litestar 2.x. OpenAPI 3.1 native, plugin-driven OTel, msgspec-friendly DTOs.
- **Persistence**: SQLAlchemy 2.0 with `Mapped[]` annotations, async via `asyncpg`. Alembic migrations.
- **Background**: `procrastinate` against the same Postgres database. No Redis dependency.
- **Cryptography**: `pyhanko`, `pyhanko-certvalidator`, `cryptography>=47`.
- **Storage client**: `aioboto3` against the in-cluster object-storage-service.
- **ClickHouse**: `clickhouse-connect` async client.
- **Identity**: SPIFFE workload API via `pyspiffe`, with a thin wrapper that produces an `httpx.AsyncClient` whose mTLS context is bound to the rotating SVID. JWT verification via `python-jose` against Zitadel JWKS.
- **OTel**: `opentelemetry-instrumentation-asyncpg`, `opentelemetry-instrumentation-aiohttp-client`, `opentelemetry-instrumentation-httpx`, plus Litestar's first-party `OpenTelemetryConfig` plugin.
- **Package management**: `uv`, hermetic via `rules_python` + `pip_parse`. pyHanko is pinned exactly; `cryptography` is pinned to a tested floor.

### File layout

```
src/secure-documents-service/
├── BUILD.bazel
├── pyproject.toml
├── alembic.ini
├── migrations/versions/
├── openapi/openapi.json                # committed wire contract
├── secdocs/
│   ├── __main__.py                     # uvicorn entrypoint
│   ├── app.py                          # Litestar() construction
│   ├── config.py                       # msgspec.Struct settings
│   ├── observability/
│   │   ├── otel.py
│   │   └── logging.py
│   ├── identity/
│   │   ├── spiffe.py                   # SVID rotation, http client factory
│   │   ├── public_auth.py              # Zitadel JWT middleware
│   │   └── internal_auth.py            # SPIFFE peer-identity guard
│   ├── domain/                         # msgspec.Struct, no I/O
│   ├── persistence/
│   │   ├── tables.py                   # Mapped[] declarations
│   │   └── repositories/
│   ├── crypto/
│   │   ├── envelope_keys.py            # DEK/KEK wrap, share-link wrap
│   │   ├── signer.py                   # SecretsServiceSigner
│   │   ├── pdf_pipeline.py             # PAdES B-T / B-LT / B-LTA
│   │   ├── trust.py                    # ValidationContext, OCSP cache
│   │   └── audit_chain.py              # HMAC chain
│   ├── storage/
│   │   ├── blob.py                     # encrypted upload/download
│   │   └── streaming.py                # range, watermark, rasterize
│   ├── audit/
│   │   ├── events.py                   # event types
│   │   ├── ledger.py                   # Postgres outbox author
│   │   └── projector.py                # ClickHouse/governance projector
│   ├── jobs/
│   │   ├── send_envelope.py
│   │   ├── ocsp_refresh.py
│   │   ├── envelope_expire.py
│   │   ├── render_audit_trail.py
│   │   ├── ltv_refresh.py              # B-LTA archive timestamp refresh
│   │   ├── verify_chain.py
│   │   └── reap_signing_sessions.py
│   ├── controllers/
│   │   ├── public/                     # documents.api.<domain>
│   │   │   ├── documents.py
│   │   │   ├── envelopes.py
│   │   │   ├── share_links.py
│   │   │   ├── user_signatures.py
│   │   │   └── signing.py
│   │   └── internal/                   # SPIFFE-only
│   │       ├── audit_export.py
│   │       ├── billing_metrics.py
│   │       └── tombstone.py
│   ├── dtos/                           # Litestar DTOs
│   └── plugins/                        # custom Litestar plugins
└── tests/e2e/
```

### Litestar app construction

```python
# secdocs/app.py
def create_app() -> Litestar:
    return Litestar(
        route_handlers=[
            documents.DocumentsController,
            envelopes.EnvelopesController,
            share_links.ShareLinksController,
            user_signatures.UserSignaturesController,
            signing.SigningController,
            audit_export.AuditExportController,
            billing_metrics.BillingMetricsController,
            tombstone.TombstoneController,
        ],
        plugins=[
            SQLAlchemyPlugin(config=SQLAlchemyAsyncConfig(
                connection_string=settings.postgres_dsn,
                metadata=metadata,
                create_all=False,           # alembic owns DDL
            )),
            OpenTelemetryPlugin(config=OpenTelemetryConfig(
                tracer_provider=build_tracer_provider(settings),
                meter_provider=build_meter_provider(settings),
                scope_name="secure-documents-service",
                exclude=["/health", "/metrics"],
            )),
            LedgerLifecyclePlugin(),        # ClickHouse client + procrastinate
        ],
        middleware=[zitadel_jwt_middleware, spiffe_peer_guard],
        openapi_config=OpenAPIConfig(
            title="secure-documents-service",
            version=settings.version,
            path="/openapi.json",
            components=Components(security_schemes={
                "ZitadelJWT": SecurityScheme(type="http", scheme="bearer"),
                "SPIFFE":     SecurityScheme(type="mutualTLS"),
            }),
        ),
        debug=False,
    )
```

## PostgreSQL schema

One database, `secure_documents`. Migrations in `src/secure-documents-service/migrations/versions/`.

### `user_signatures`

```sql
CREATE TABLE user_signatures (
  id              UUID PRIMARY KEY,
  org_id          UUID NOT NULL,
  user_id         UUID NOT NULL,
  kind            TEXT NOT NULL CHECK (kind IN ('full','initials')),
  input_method    TEXT NOT NULL CHECK (input_method IN ('drawn','typed','uploaded')),
  image_blob_uri  TEXT NOT NULL,
  source_blob_uri TEXT NOT NULL,
  wrapped_dek     BYTEA NOT NULL,
  kek_id          TEXT NOT NULL,
  input_metadata  JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  archived_at     TIMESTAMPTZ
);
CREATE INDEX user_signatures_active_idx
  ON user_signatures (org_id, user_id, kind)
  WHERE archived_at IS NULL;
```

### `documents`

```sql
CREATE TABLE documents (
  id                UUID PRIMARY KEY,
  org_id            UUID NOT NULL,
  uploaded_by       UUID NOT NULL,
  custody_mode      TEXT NOT NULL CHECK (custody_mode IN ('server_managed','client_custodied')),
  name              TEXT NOT NULL,
  mime_type         TEXT NOT NULL,
  size_bytes        BIGINT NOT NULL,
  ciphertext_sha256 BYTEA NOT NULL,
  plaintext_hmac_sha256 BYTEA,
  page_count        INT,
  blob_uri          TEXT NOT NULL,
  wrapped_dek       BYTEA,
  kek_id            TEXT,
  client_ciphertext_alg TEXT,
  client_ciphertext_nonce BYTEA,
  derived_from_id   UUID REFERENCES documents(id),
  derivation_kind   TEXT NOT NULL DEFAULT 'original'
                     CHECK (derivation_kind IN ('original','sealed','audit_trail','custody_conversion')),
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at        TIMESTAMPTZ,
  tombstoned_at     TIMESTAMPTZ,
  tombstoned_by     UUID,
  tombstone_reason  TEXT,
  CHECK ((tombstoned_at IS NULL) = (tombstone_reason IS NULL)),
  CONSTRAINT server_managed_key_consistency CHECK (
    (custody_mode = 'server_managed' AND wrapped_dek IS NOT NULL AND kek_id IS NOT NULL AND plaintext_hmac_sha256 IS NOT NULL)
    OR (custody_mode = 'client_custodied' AND wrapped_dek IS NULL AND kek_id IS NULL AND plaintext_hmac_sha256 IS NULL)
  ),
  CONSTRAINT client_ciphertext_consistency CHECK (
    (custody_mode = 'client_custodied') =
      (client_ciphertext_alg IS NOT NULL AND client_ciphertext_nonce IS NOT NULL)
  )
);
CREATE INDEX documents_org_idx ON documents (org_id, created_at DESC)
  WHERE deleted_at IS NULL AND tombstoned_at IS NULL;
```

### `envelopes`

```sql
CREATE TABLE envelopes (
  id                  UUID PRIMARY KEY,
  org_id              UUID NOT NULL,
  initiator_id        UUID NOT NULL,
  subject             TEXT NOT NULL,
  message             TEXT,
  status              TEXT NOT NULL CHECK (status IN
                        ('draft','sent','in_progress','completed','voided','expired')),
  routing             TEXT NOT NULL CHECK (routing IN ('parallel','sequential')),
  pades_level         TEXT NOT NULL DEFAULT 'B-LT'
                        CHECK (pades_level IN ('B-T','B-LT','B-LTA')),
  required_assurance  TEXT NOT NULL DEFAULT 'passkey'
                        CHECK (required_assurance IN ('email','sms_otp','passkey')),
  expires_at          TIMESTAMPTZ,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  sent_at             TIMESTAMPTZ,
  completed_at        TIMESTAMPTZ,
  voided_at           TIMESTAMPTZ,
  void_reason         TEXT
);
CREATE INDEX envelopes_org_status_idx ON envelopes (org_id, status, created_at DESC);
```

### `envelope_documents`

```sql
CREATE TABLE envelope_documents (
  envelope_id          UUID NOT NULL REFERENCES envelopes(id),
  ordinal              INT NOT NULL,
  source_document_id   UUID NOT NULL REFERENCES documents(id),
  current_document_id  UUID NOT NULL REFERENCES documents(id),
  revision_version     BIGINT NOT NULL DEFAULT 0,
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (envelope_id, ordinal)
);
```

### `recipients`

```sql
CREATE TABLE recipients (
  id                  UUID PRIMARY KEY,
  envelope_id         UUID NOT NULL REFERENCES envelopes(id),
  user_id             UUID,
  email               CITEXT,
  display_name        TEXT NOT NULL,
  role                TEXT NOT NULL CHECK (role IN ('signer','approver','cc','viewer')),
  routing_order       INT,
  required_assurance  TEXT NOT NULL DEFAULT 'passkey'
                        CHECK (required_assurance IN ('email','sms_otp','passkey')),
  signed_at           TIMESTAMPTZ,
  declined_at         TIMESTAMPTZ,
  decline_reason      TEXT,
  CONSTRAINT recipient_subject CHECK ((user_id IS NOT NULL) <> (email IS NOT NULL))
);
CREATE INDEX recipients_envelope_order_idx ON recipients (envelope_id, routing_order);
```

### `fields`

```sql
CREATE TABLE fields (
  id                  UUID PRIMARY KEY,
  envelope_id         UUID NOT NULL REFERENCES envelopes(id),
  document_id         UUID NOT NULL REFERENCES documents(id),
  recipient_id        UUID NOT NULL REFERENCES recipients(id),
  kind                TEXT NOT NULL CHECK (kind IN
                        ('signature','initials','date_signed','text','checkbox')),
  page                INT NOT NULL,
  x                   DOUBLE PRECISION NOT NULL,
  y                   DOUBLE PRECISION NOT NULL,
  width               DOUBLE PRECISION NOT NULL,
  height              DOUBLE PRECISION NOT NULL,
  required            BOOLEAN NOT NULL DEFAULT TRUE,
  filled_value_ciphertext BYTEA,
  filled_value_wrapped_dek BYTEA,
  filled_value_kek_id TEXT,
  user_signature_id   UUID REFERENCES user_signatures(id),
  filled_at           TIMESTAMPTZ,
  CONSTRAINT filled_value_encrypt_consistency CHECK (
    (filled_value_ciphertext IS NULL AND filled_value_wrapped_dek IS NULL AND filled_value_kek_id IS NULL)
    OR (filled_value_ciphertext IS NOT NULL AND filled_value_wrapped_dek IS NOT NULL AND filled_value_kek_id IS NOT NULL)
  )
);
CREATE INDEX fields_envelope_recipient_idx ON fields (envelope_id, recipient_id);
```

### `signatures`

```sql
CREATE TABLE signatures (
  id                      UUID PRIMARY KEY,
  envelope_id             UUID NOT NULL REFERENCES envelopes(id),
  recipient_id            UUID NOT NULL REFERENCES recipients(id),
  document_id             UUID NOT NULL REFERENCES documents(id),
  parent_document_id      UUID NOT NULL REFERENCES documents(id),
  parent_content_hmac_sha256 BYTEA NOT NULL,
  child_content_hmac_sha256  BYTEA NOT NULL,
  pades_level             TEXT NOT NULL,
  digest_algorithm        TEXT NOT NULL,
  signature_algorithm     TEXT NOT NULL,
  signed_attrs_sha256     BYTEA NOT NULL,
  signing_key_id          TEXT NOT NULL,
  signing_grant_id        UUID NOT NULL,
  signer_cert_thumbprint  BYTEA NOT NULL,
  signer_cert_serial      TEXT NOT NULL,
  signer_cert_pem         BYTEA NOT NULL,
  cert_not_before         TIMESTAMPTZ NOT NULL,
  cert_not_after          TIMESTAMPTZ NOT NULL,
  tsa_url                 TEXT,
  tsa_response_b64        TEXT,
  validation_policy_version TEXT NOT NULL,
  signed_at               TIMESTAMPTZ NOT NULL,
  signer_ip               INET,
  signer_user_agent       TEXT,
  signer_geohash          TEXT,
  signer_verified_user_id UUID,
  signer_verified_org_id  UUID,
  step_up_assurance       TEXT NOT NULL CHECK (step_up_assurance IN ('email','sms_otp','passkey')),
  step_up_method          TEXT NOT NULL,
  step_up_token_jti       TEXT NOT NULL,
  CONSTRAINT signer_verified_pair CHECK (
    (signer_verified_user_id IS NULL) = (signer_verified_org_id IS NULL)
  )
);
CREATE INDEX signatures_envelope_idx ON signatures (envelope_id, signed_at);
```

### `share_links`

```sql
CREATE TABLE share_links (
  id                  UUID PRIMARY KEY,
  org_id              UUID NOT NULL,
  envelope_id         UUID REFERENCES envelopes(id),
  document_id         UUID REFERENCES documents(id),
  recipient_id        UUID REFERENCES recipients(id),
  token_hmac          BYTEA NOT NULL UNIQUE,
  permissions         TEXT[] NOT NULL,
  no_download         BOOLEAN NOT NULL DEFAULT FALSE,
  watermark_template  TEXT,
  required_assurance  TEXT NOT NULL DEFAULT 'email'
                        CHECK (required_assurance IN ('email','sms_otp','passkey')),
  expires_at          TIMESTAMPTZ,
  max_uses            INT,
  use_count           INT NOT NULL DEFAULT 0,
  client_encrypted    BOOLEAN NOT NULL DEFAULT FALSE,
  link_wrapped_dek    BYTEA,                          -- non-null iff client_encrypted
  link_wrap_nonce     BYTEA,                          -- non-null iff client_encrypted
  link_wrap_salt      BYTEA,                          -- non-null iff client_encrypted
  link_wrap_alg       TEXT,
  created_by          UUID NOT NULL,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at          TIMESTAMPTZ,
  CONSTRAINT one_subject CHECK (
    (envelope_id IS NOT NULL)::int + (document_id IS NOT NULL)::int = 1
  ),
  CONSTRAINT client_encrypt_consistency CHECK (
    client_encrypted = (link_wrapped_dek IS NOT NULL)
    AND client_encrypted = (link_wrap_nonce IS NOT NULL)
    AND client_encrypted = (link_wrap_salt IS NOT NULL)
    AND client_encrypted = (link_wrap_alg IS NOT NULL)
  ),
  CONSTRAINT client_encrypted_document_only CHECK (
    NOT client_encrypted OR (document_id IS NOT NULL AND envelope_id IS NULL)
  ),
  CONSTRAINT no_server_transformations_when_client_encrypted CHECK (
    NOT (client_encrypted AND (no_download OR watermark_template IS NOT NULL))
  )
);
CREATE INDEX share_links_token_idx ON share_links (token_hmac) WHERE revoked_at IS NULL;
```

### `signing_sessions`

```sql
CREATE TABLE signing_sessions (
  id                       UUID PRIMARY KEY,
  envelope_id              UUID NOT NULL REFERENCES envelopes(id),
  recipient_id             UUID NOT NULL REFERENCES recipients(id),
  step_up_token_jti        TEXT NOT NULL,
  step_up_method           TEXT NOT NULL,
  step_up_assurance        TEXT NOT NULL CHECK (step_up_assurance IN ('email','sms_otp','passkey')),
  parent_revision_set      JSONB NOT NULL DEFAULT '[]'::jsonb,
  prepared_digest_b64      TEXT,
  signed_attrs_b64         TEXT,
  signed_attrs_sha256      BYTEA,
  signing_grant_id         UUID,
  in_progress_blob_uri     TEXT,
  field_values_ciphertext  BYTEA,
  field_values_wrapped_dek BYTEA,
  field_values_kek_id      TEXT,
  user_signature_full_id   UUID REFERENCES user_signatures(id),
  user_signature_initials_id UUID REFERENCES user_signatures(id),
  created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at               TIMESTAMPTZ NOT NULL,
  completed_at             TIMESTAMPTZ,
  CONSTRAINT session_field_values_encrypt_consistency CHECK (
    (field_values_ciphertext IS NULL AND field_values_wrapped_dek IS NULL AND field_values_kek_id IS NULL)
    OR (field_values_ciphertext IS NOT NULL AND field_values_wrapped_dek IS NOT NULL AND field_values_kek_id IS NOT NULL)
  )
);
CREATE INDEX signing_sessions_active_idx
  ON signing_sessions (envelope_id, recipient_id)
  WHERE completed_at IS NULL;
```

### `ocsp_responses`

```sql
CREATE TABLE ocsp_responses (
  cert_thumbprint  BYTEA PRIMARY KEY,
  responder_uri    TEXT NOT NULL,
  response_der     BYTEA NOT NULL,
  this_update      TIMESTAMPTZ NOT NULL,
  next_update      TIMESTAMPTZ NOT NULL,
  fetched_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### `audit_chains`

```sql
CREATE TABLE audit_chains (
  id              UUID PRIMARY KEY,
  org_id          UUID NOT NULL,
  scope_kind      TEXT NOT NULL CHECK (scope_kind IN ('envelope','document')),
  scope_id        UUID NOT NULL,
  hmac_key_id     TEXT NOT NULL,
  last_sequence   BIGINT NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
  last_hmac       BYTEA NOT NULL CHECK (octet_length(last_hmac) = 32),
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (org_id, scope_kind, scope_id)
);
```

`last_hmac` is 32 zero bytes at chain creation. Event appenders lock this row with `SELECT ... FOR UPDATE`, call secrets-service for the HMAC operation, insert the outbox row below, and update `last_sequence` and `last_hmac` in the same transaction.

### `secure_document_audit_outbox`

```sql
CREATE TABLE secure_document_audit_outbox (
  event_id        UUID PRIMARY KEY,
  chain_id        UUID NOT NULL REFERENCES audit_chains(id),
  org_id          UUID NOT NULL,
  scope_kind      TEXT NOT NULL CHECK (scope_kind IN ('envelope','document')),
  scope_id        UUID NOT NULL,
  sequence        BIGINT NOT NULL CHECK (sequence > 0),
  event_time      TIMESTAMPTZ NOT NULL,
  action          TEXT NOT NULL,
  actor_id        UUID,
  envelope_id     UUID,
  document_id     UUID,
  recipient_id    UUID,
  share_link_id   UUID,
  ip              INET,
  user_agent      TEXT NOT NULL DEFAULT '',
  geohash         TEXT NOT NULL DEFAULT '',
  payload         JSONB NOT NULL,
  payload_sha256  BYTEA NOT NULL CHECK (octet_length(payload_sha256) = 32),
  prev_hmac       BYTEA NOT NULL CHECK (octet_length(prev_hmac) = 32),
  event_hmac      BYTEA NOT NULL CHECK (octet_length(event_hmac) = 32),
  hmac_key_id     TEXT NOT NULL,
  trace_id        TEXT NOT NULL DEFAULT '',
  span_id         TEXT NOT NULL DEFAULT '',
  deploy_run_key  TEXT NOT NULL DEFAULT '',
  clickhouse_projected_at TIMESTAMPTZ,
  governance_exported_at  TIMESTAMPTZ,
  projection_attempts INT NOT NULL DEFAULT 0,
  last_projection_error TEXT,
  UNIQUE (chain_id, sequence)
);
CREATE INDEX secure_document_audit_outbox_pending_idx
  ON secure_document_audit_outbox (event_time)
  WHERE clickhouse_projected_at IS NULL OR governance_exported_at IS NULL;
```

## ClickHouse schema

```sql
CREATE TABLE secure_document_events (
  event_time     DateTime64(6, 'UTC'),
  org_id         UUID,
  chain_id       UUID,
  chain_scope    LowCardinality(String),
  sequence       UInt64,
  event_id       UUID,
  envelope_id    UUID,
  document_id    UUID,
  recipient_id   UUID,
  share_link_id  UUID,
  actor_id       UUID,
  action         LowCardinality(String),
  ip             IPv6,
  user_agent     String CODEC(ZSTD(3)),
  geohash        LowCardinality(String),
  prev_hmac      FixedString(32),
  event_hmac     FixedString(32),
  hmac_key_id    LowCardinality(String),
  payload        String CODEC(ZSTD(3)),
  payload_sha256 FixedString(32),
  trace_id       String,
  span_id        String,
  deploy_run_key LowCardinality(String)
)
ENGINE = MergeTree
ORDER BY (org_id, chain_scope, chain_id, sequence)
PARTITION BY toYYYYMM(event_time)
SETTINGS index_granularity = 8192;
```

`payload` is canonical JSON, sorted keys, no whitespace, UTF-8. The ClickHouse table stores the exact canonical payload emitted from the Postgres outbox plus the Postgres-authored HMAC fields. ClickHouse writers never compute HMACs and ad-hoc `INSERT` paths are not allowed.

Materialized views project per-envelope timelines and per-recipient signing histories for the console; they are convenience read models and do not participate in chain verification.

## Security verification gates

The v1 implementation is not complete until live rehearsals produce the following ClickHouse and Postgres evidence:

- A server-managed upload writes only ciphertext to object storage, records `plaintext_hmac_sha256` and `ciphertext_sha256`, emits a governance audit row, and produces no plaintext bytes in logs, traces, or object-storage reads.
- A client-custodied blind share serves ciphertext with `link_wrapped_dek`, `link_wrap_nonce`, `link_wrap_salt`, and `link_wrap_alg`, emits `share_link.served_ciphertext`, and produces no secrets-service unwrap span for that access path.
- Two parallel signer sessions prepared against the same parent revision result in exactly one successful revision advance; the stale session returns `signing.parent_revision_stale` and does not overwrite the winner's signature.
- A signing request against secrets-service without a valid one-time grant, with mismatched `signed_attrs_sha256`, or from the wrong SPIFFE caller fails closed and emits a critical audit event.
- A burst of concurrent mutating operations on one envelope produces contiguous `secure_document_audit_outbox.sequence` values, no duplicate `(chain_id, sequence)` pairs, and a ClickHouse projection whose HMAC fields verify against the Postgres chain head.
- A tombstone of a completed sealed document requires org-owner authorization, a non-empty reason, retained signature rows, and a `document.tombstoned` event in the same chain.

## Open questions and deferred decisions

The following are intentionally deferred. Each has a clear seam, no v1 schema impact unless noted, and is listed so future work knows where it slots in.

- **Anchor-text field placement**. Anchor resolution belongs in a future templates subsystem, where anchor strings compile to absolute (page, x, y, w, h) coordinates at envelope-creation time. The runtime `fields` schema (absolute coordinates) is already the right shape and does not change. Graphical placement is the only v1 source of coordinates.
- **Witness role**. Some jurisdictions require a third-party witness for certain instruments. The enum addition is non-breaking, but the implied workflow (witness-to-principal binding, jurisdiction-specific co-presence semantics, cryptographic structure) is not. Defer the enum addition itself until a jurisdiction-specific requirement arrives, so we don't ship a half-finished role.
- **QES assurance level**. QES requires QTSP integration, remote QSCD signing activation evidence, EU Trusted List validation, and CSC API lifecycle handling. It is intentionally not a v1 enum value; adding it later is a full signer implementation and assurance-policy change, not a string addition.
- **Templates and bulk send**. Templates are a new aggregate (`templates`, `template_documents`, `template_fields` keyed by recipient-role placeholders rather than recipient IDs) and *do* change the data model. Bulk send is then thin workflow over templates: instantiate N envelopes from one template + one recipient list. Both deferred until a customer asks; templates is acknowledged as a schema delta, not pure workflow.
- **In-document comments and review tracks**. Out of v1. If added, comments are a separate `envelope_comments` aggregate, materialized into the audit-trail PDF only — never into the source document's bytes. This preserves the source document's evidentiary purity.

## Decisions locked by this document

For clarity on what the v1 implementation may not deviate from without an architecture-level revision:

- One Postgres database, owned by this service, named `secure_documents`.
- pyHanko is the cryptographic engine. The custom `Signer` boundary is the only contract; the rest of pyHanko is treated as an implementation library.
- Default PAdES level is `B-LT`. Other levels are explicit per-envelope choices.
- Default identity-assurance floor is `passkey`. May be raised by policy; may be lowered only by org owner through an audited risk-acceptance setting.
- Sealed documents are tombstoned, never deleted. Hard deletion of any row referenced by a sealed signature is forbidden.
- Share links carry an explicit `client_encrypted` boolean, but true server-blind sharing is available only for `client_custodied` documents with no server-held DEK wrap. Server-side transformations are disallowed on client-encrypted links by database constraint and service policy.
- Audit ledger authorship is HMAC-chained in Postgres per envelope or standalone document, committed before the response, projected to ClickHouse, and consumed by governance-service.
- Litestar 2.x is the framework. OpenTelemetry is wired at the framework level, not bolted on.
- The OpenAPI 3.1 spec at `/openapi.json` is the only supported consumer contract; private RPC is not supported.
- Cross-org recipient identity is captured as evidence on `signatures` (`signer_verified_user_id`, `signer_verified_org_id`) at sign time. The `recipients` row stays email-typed; no cross-org auto-binding, no cross-org social graph.
- PAdES-LTA archive-timestamp refresh is owned by `secure-documents-service` via the `ltv_refresh` procrastinate job. A platform-level archive-timestamp service is deferred until a second consumer exists.
- Read receipts are surfaced to envelope initiators only. Each recipient sees only their own envelope status, not the status of co-recipients (privacy invariant under sequential routing in particular).
- Per-recipient per-envelope signing keys and one-time signing grants are mandatory. secrets-service must not expose a generic "sign arbitrary bytes by key id" path to this service.
