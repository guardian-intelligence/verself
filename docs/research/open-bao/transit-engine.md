# Transit Secrets Engine

"Cryptography as a service" -- handles cryptographic operations on data in transit without
storing the data itself. The application never sees raw key material.

Docs: https://openbao.org/docs/secrets/transit/
API: https://openbao.org/api-docs/secret/transit/

## Operations

| Operation | API Path | Description |
|-----------|----------|-------------|
| Encrypt | `POST /transit/encrypt/:name` | Encrypt plaintext with named key |
| Decrypt | `POST /transit/decrypt/:name` | Decrypt ciphertext |
| Rewrap | `POST /transit/rewrap/:name` | Re-encrypt with latest key version (no plaintext exposure) |
| Sign | `POST /transit/sign/:name/:hash` | Digital signature |
| Verify | `POST /transit/verify/:name/:hash` | Verify signature |
| HMAC | `POST /transit/hmac/:name/:algo` | Generate HMAC |
| Hash | `POST /transit/hash/:algo` | Hash data |
| Random | `POST /transit/random/:bytes` | Generate random bytes |
| Data key | `POST /transit/datakey/:type/:name` | Envelope encryption (generate + encrypt a data key) |
| Rotate | `POST /transit/keys/:name/rotate` | Create new key version |
| Import | `POST /transit/keys/:name/import` | Bring your own key |
| Export | `GET /transit/export/:type/:name/:version` | Export key material |

## Key types

**Symmetric encryption:**
- `aes256-gcm96` (default) -- 256-bit AES, 96-bit nonce
- `aes128-gcm96` -- 128-bit AES, 96-bit nonce
- `chacha20-poly1305` -- 256-bit key
- `xchacha20-poly1305` -- 256-bit key (extended nonce)

**Asymmetric / signing:**
- `ed25519` -- signing, verification, key derivation
- `ecdsa-p256`, `ecdsa-p384`, `ecdsa-p521`
- `rsa-2048`, `rsa-3072`, `rsa-4096`

**HMAC-only:**
- `hmac`

All key types automatically generate a separate HMAC key at creation and rotation time.

## API examples

**Encrypt** (plaintext must be base64-encoded):

```bash
curl -H "X-Vault-Token: ..." \
     -X POST \
     -d '{"plaintext": "dGhlIHF1aWNrIGJyb3duIGZveAo="}' \
     http://127.0.0.1:8200/v1/transit/encrypt/my-key

# Response:
# { "data": { "ciphertext": "vault:v1:XjsPWPjqPrBi1N2Ms2s1QM798YyFWnO4TR4lsFA=" } }
```

**Decrypt:**

```bash
curl -H "X-Vault-Token: ..." \
     -X POST \
     -d '{"ciphertext": "vault:v1:XjsPWPjqPrBi1N2Ms2s1QM798YyFWnO4TR4lsFA="}' \
     http://127.0.0.1:8200/v1/transit/decrypt/my-key

# Response:
# { "data": { "plaintext": "dGhlIHF1aWNrIGJyb3duIGZveAo=" } }
```

**Rewrap** (re-encrypt with latest key version, no plaintext exposure):

```bash
curl -H "X-Vault-Token: ..." \
     -X POST \
     -d '{"ciphertext": "vault:v1:XjsPWPjqPrBi1N2Ms2s1QM798YyFWnO4TR4lsFA="}' \
     http://127.0.0.1:8200/v1/transit/rewrap/my-key

# Response:
# { "data": { "ciphertext": "vault:v2:abcdefgh..." } }
```

Ciphertext includes a version prefix (`vault:v1:`) indicating which key version encrypted it.
Batch operations supported via `batch_input` parameter. Additional Authenticated Data (AAD)
supported via `associated_data` parameter.

## Convergent encryption

Deterministic mode: same plaintext + context always produces same ciphertext. Useful for
indexing/searching encrypted data.

Three versions exist:
- **v1**: Required client-provided nonces. Inflexible.
- **v2**: Algorithmic nonce derivation, "susceptible to offline plaintext-confirmation attacks."
- **v3** (current): "Resistant to offline plaintext-confirmation attacks" using PRF-based nonce.

Enabled at key creation: `convergent_encryption=true` (requires `derived=true`). The `context`
parameter is mandatory for every encrypt/decrypt call.

## Key versioning and rotation

**Rotation:** `POST /transit/keys/:name/rotate` creates a new key version. Old versions remain
in the keyring. New encryptions use the latest version. Old ciphertext remains decryptable.

**Version control** (`POST /transit/keys/:name/config`):
- `min_decryption_version` -- versions below this are archived (cannot decrypt)
- `min_encryption_version` -- prevents encryption with old versions
- `auto_rotate_period` -- automatic rotation on schedule

**Rotation workflow:**
1. Rotate: `POST /transit/keys/:name/rotate`
2. Rewrap existing ciphertext: `POST /transit/rewrap/:name` (server-side, no plaintext exposure)
3. Set `min_decryption_version` to archive old keys
4. Optionally `trim` to permanently delete old versions

**NIST note:** For AES-GCM, "rotation should occur before approximately 2^32 encryptions have
been performed by a key version, following the guidelines of NIST publication 800-38D."

## SOPS integration

**SOPS natively supports Vault Transit as a backend.** Since OpenBao is API-compatible, this
works directly.

`.sops.yaml`:
```yaml
creation_rules:
  - path_regex: \.yaml$
    vault_uri: "https://openbao.example.com:8200/v1/transit/keys/sops"
```

Environment:
```bash
export VAULT_ADDR="https://openbao.example.com:8200"
export VAULT_TOKEN="s.xxxxxxxxx"
```

Required policy:
```hcl
path "transit/encrypt/sops" { capabilities = ["update"] }
path "transit/decrypt/sops" { capabilities = ["update"] }
path "transit/keys/sops"    { capabilities = ["read"] }
```

This centralizes key management in OpenBao: audit logging, rotation, and access control for
SOPS operations all go through OpenBao's policy engine. However, it creates a bootstrap
dependency (SOPS needs OpenBao running to decrypt), so it's not suitable for the unseal key
itself.

## Applicability to forge-metal

**Near-term:** Not needed. KV v2 handles the secret storage use case. SOPS with age keys
handles the bootstrap case.

**Future use cases:**
- **CI artifact signing**: Transit can sign build artifacts (ed25519) without exposing the
  signing key to CI jobs. The policy restricts `sign` but not `export`.
- **Encrypting CI secrets in Forgejo**: If Forgejo stores encrypted secrets in its database,
  Transit can be the encryption backend. Forgejo never holds the raw key.
- **SOPS backend migration**: Once OpenBao is stable, migrate SOPS from age keys to Transit.
  Key rotation and audit become automatic. Only the unseal key remains in SOPS/age.
