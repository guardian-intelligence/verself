# Security Model

Threat analysis for a single-node OpenBao deployment with static key seal on Latitude.sh
bare metal.

## Barrier encryption model

Data is encrypted in layers:

```
data -> encryption key (keyring) -> root key -> unseal key
```

At startup, OpenBao is "sealed" -- it can access storage but cannot decrypt anything. Unsealing
provides the root key, which unlocks the keyring, which unlocks data.

With static seal: the unseal key is an AES-256-GCM key read from a file on disk. OpenBao
uses it to decrypt the root key automatically on startup.

## Attack surfaces (single-node, static seal)

### 1. Network API (port 8200)

All API requests traverse this endpoint. TLS is mandatory for production. The NixOS module
defaults the listener to `127.0.0.1:8200` (localhost only).

For forge-metal: bind to localhost, proxy through Caddy for external access with TLS.
Restrict external access to the operator's IP via firewall rules or Caddy ACLs.

### 2. Seal key material

With static seal, the key file on disk is the crown jewel. Compromise = full data access.
**No revocation capability** unlike KMS/HSM -- you cannot "delete" the key from an
attacker who has already copied it.

Mitigations:
- File permissions: `0400`, owned by the `openbao` user
- Ansible deploys it once; it never appears in logs or output
- SOPS-encrypts the key in the repository
- Consider `tmpfs` for the key file (populated by systemd `ExecStartPre`)

### 3. Memory

All secrets are decrypted in memory while unsealed. The NixOS module sets:
- `MemorySwapMax = 0` -- prevents secrets from hitting swap
- `MemoryZSwapMax = 0` -- same for compressed swap
- `LimitCORE = 0` -- no core dumps (would contain decrypted secrets)

### 4. Audit subsystem

CVE-2025-54997 (severity 9.1 Critical) showed the audit file backend could be weaponized for
RCE by a privileged operator. Fixed in 2.3.2. Always run current versions.

### 5. Storage on disk

Raft data on disk is encrypted by the barrier. Physical access to `vault.db` without the
unseal key is useless. ZFS encryption (aes-256-gcm) adds a second layer if enabled on the
pool.

## Known CVEs (2025-2026)

| CVE | Severity | Description | Fixed in |
|-----|----------|-------------|----------|
| CVE-2025-54997 | 9.1 Critical | Audit file backend RCE by privileged operator | 2.3.2 |
| CVE-2025-62705 | Medium | Audit log did not redact `[]byte` response parameters | 2.4.2 |
| CVE-2025-62513 | Medium | HTTPRawBody leaked in audit logs (ACME/OIDC data) | 2.4.2 |
| CVE-2026-33757 | Critical | JWT/OIDC authentication bypass via remote phishing | 2.5.2 |

Sources:
- https://github.com/openbao/openbao/security/advisories/GHSA-xp75-r577-cvhp
- https://zeropath.com/blog/openbao-cve-2025-54997-summary

## Mitigations for single-node deployment

| Mitigation | Implementation |
|------------|----------------|
| No swap for secrets | systemd `MemorySwapMax=0` |
| No core dumps | systemd `LimitCORE=0` |
| Revoke root token after setup | `bao token revoke <root_token>` after initial config |
| Operator uses scoped token | Create a personal token with specific policies |
| Localhost-only listener | `address = "127.0.0.1:8200"` |
| External TLS via Caddy | Caddy reverse proxy with automatic HTTPS |
| Audit logging | Enable file audit device for all operations |
| Unseal key protection | SOPS-encrypted in repo, mode 0400 on server |
| Regular backups | ZFS snapshots + `bao operator raft snapshot save` to offsite |

## Root token lifecycle

For a single-operator setup:

1. `bao operator init` produces a root token
2. Use root token to configure: enable KV v2, AppRole, policies, seed initial secrets
3. Create a personal operator token with appropriate policies
4. **Revoke the root token:** `bao token revoke <root_token>`
5. If root access is needed again: `bao operator generate-root` (requires unseal key)

This follows the principle of least privilege. The root token should not be used for
day-to-day operations.

## Comparison with current security model

| Credential | Current (SOPS + flat files) | With OpenBao |
|------------|---------------------------|--------------|
| Forgejo admin credentials | `ansible/.credentials/forgejo_admin_*` on control node | KV v2, operator-only policy |
| HyperDX admin credentials | `ansible/.credentials/hyperdx_admin_*` on control node | KV v2, operator-only policy |
| Cloudflare API token | `secrets.sops.yml` | KV v2, deploy-time policy |
| Latitude.sh API token | Environment variable (not persisted) | KV v2, operator-only policy |
| HyperDX runtime secrets | `/etc/hyperdx/hyperdx.env` (0640) | KV v2, service-read policy |
| CI job secrets | N/A (not implemented) | KV v2, per-job scoped via AppRole |

The key improvement: access control moves from "whoever has root on the box" to fine-grained
policies per identity. CI jobs cannot read operator secrets. The operator can audit who
accessed what and when.
