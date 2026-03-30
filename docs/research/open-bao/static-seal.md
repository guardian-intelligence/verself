# Static Key Seal

The static key seal is an **OpenBao-only feature** not present in HashiCorp Vault. It enables
fully self-hosted auto-unseal without cloud KMS or physical HSM hardware.

Source: `wrappers/static/static.go` in https://github.com/openbao/go-kms-wrapping
RFC: https://openbao.org/docs/rfcs/static-auto-unseal/
Docs: https://openbao.org/docs/configuration/seal/static/

## How it works

The `Wrapper` struct holds two keys in memory:
- `currentKey` -- 32-byte AES-256 key for new encryptions
- `previousKey` -- optional, for decrypting data sealed under a rotated-out key

Each key has a permanent `keyId` string identifier.

## Crypto primitives

AES-256-GCM with random nonce, using Go's `crypto/aes` and `cipher.NewGCMWithRandomNonce`:

```go
func (s *Wrapper) Encrypt(ctx context.Context, plaintext []byte, opts ...wrapping.Option) (*wrapping.BlobInfo, error) {
    block, err := aes.NewCipher(s.currentKey)
    gcm, err := cipher.NewGCMWithRandomNonce(block)
    ciphertext := gcm.Seal(nil, nil, plaintext, opt.WithAad)
    ret := &wrapping.BlobInfo{
        Iv: ciphertext[:12], Ciphertext: ciphertext[12:],
        KeyInfo: &wrapping.KeyInfo{KeyId: s.currentKeyId},
    }
    return ret, nil
}
```

The 12-byte IV (96-bit nonce) is split into a separate field for backwards compatibility with
OpenBao v2.4 and lower (see https://github.com/openbao/openbao/issues/2230).

Decryption dispatches on `KeyInfo.KeyId` to select the correct key (current or previous), then
recombines `Iv + Ciphertext` before calling `gcm.Open`.

## Key loading

`SetConfig` accepts keys in 4 formats based on string length:
- 64 chars: hex-encoded
- 44 chars: base64 (standard)
- 43 chars: base64 (raw URL-safe)
- 32 chars: raw bytes

Keys can also be loaded via:
- Environment variables: `BAO_STATIC_SEAL_CURRENT_KEY`, `BAO_STATIC_SEAL_CURRENT_KEY_ID`
- File reference: `file:///path/to/key`
- Env reference: `env://VAR_NAME`

These are resolved via `parseutil.ParsePath`.

## Validation

Uses `subtle.ConstantTimeCompare` to detect key/ID mismatches:
- Same key with different IDs -> error
- Different keys with same ID -> error

## Configuration

```hcl
seal "static" {
    current_key_id  = "20260329-1"
    current_key     = "file:///etc/openbao/unseal.key"
    previous_key_id = "20260101-1"                        # optional, for rotation
    previous_key    = "file:///etc/openbao/unseal-old.key" # optional
}
```

## Key generation

```bash
openssl rand -out /etc/openbao/unseal.key 32
chmod 0400 /etc/openbao/unseal.key
chown openbao:openbao /etc/openbao/unseal.key
```

## Key rotation

1. Generate new key file
2. Move current key config to `previous_key`/`previous_key_id`
3. Set new key as `current_key`/`current_key_id`
4. Restart OpenBao -- it re-encrypts the root key with the new seal key
5. After confirming success, remove the old key file

## Security properties (from the RFC)

> "Equivalent security to KMS/HSM in environments that already store KMS credentials in a
> platform secret store."

The key distinction vs. KMS/HSM: **no revocation capability**. An attacker needs only "a single
successful decryption attempt of the root key." With KMS, you can revoke the KMS key and the
attacker's copy of the encrypted root key becomes useless.

For forge-metal's threat model (single operator, single node, physical security delegated to
Latitude.sh datacenter), static seal is appropriate. The unseal key file is protected by the
same filesystem trust boundary as everything else on the server.

## Applicability to forge-metal

- Deploy unseal key via Ansible to `/etc/openbao/unseal.key` (mode 0400, owner openbao)
- SOPS-encrypt the unseal key in the repo (the one secret SOPS still manages)
- OpenBao auto-unseals on every reboot with zero manual intervention
- Key rotation is a config change + restart, not a re-initialization
