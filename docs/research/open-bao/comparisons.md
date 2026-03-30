# Comparisons

## OpenBao vs HashiCorp Vault

OpenBao forked from Vault 1.14.0 (last MPL 2.0 version) in November 2023 after HashiCorp's
BSL relicensing. Current version: 2.5.2 (2026-03-25).

### Features OpenBao has that Vault Community does not

| Feature | OpenBao | Vault CE | Vault Enterprise |
|---------|---------|----------|-----------------|
| Static Key Auto-Unseal | Yes (unique) | No | No |
| Namespaces (multi-tenancy) | Yes (since 2.3) | No | Yes |
| Horizontal Read Scalability | Yes (since 2.5) | No | Yes (Performance Standbys) |
| Self-Init (declarative bootstrap) | Yes (unique) | No | No |
| HTTP Audit Device | Yes | No | No |
| Audit via config file | Yes | No | No |
| CEL-based policy rules | Yes | No | Sentinel (Enterprise) |
| OCI-based plugin distribution | Yes (since 2.5) | No | No |
| PKCS#11 HSM seal (CE) | Yes | No | Yes |

### Vault Enterprise features missing from OpenBao

- Disaster Recovery Replication
- Performance Replication (cross-datacenter)
- Automated snapshot scheduling (manual snapshots work)
- Sentinel policy-as-code (OpenBao uses CEL instead)

### Assessment

Robert de Bock (Vault/OpenBao practitioner) concluded: "OpenBao is pretty capable, in some
areas more capable than Vault." For a single-node self-hosted setup, the missing Enterprise
features (replication, automated snapshots) target multi-datacenter deployments and are
irrelevant.

Sources:
- https://digitalis.io/post/choosing-a-secrets-storage-hashicorp-vault-vs-openbao
- https://bespinian.io/en/blog/openbao-os-vault-alternative/
- https://robertdebock.nl/2025/11/13/openbao-versus-vault.html

## OpenBao vs SOPS

These are fundamentally different tools operating at different layers.

| Dimension | SOPS | OpenBao |
|-----------|------|---------|
| What it is | File encryption tool | Secrets management server |
| Runtime dependency | None (CLI tool) | Running server process |
| Secret storage | Encrypted files in Git | Encrypted in Raft/Consul/PostgreSQL |
| Dynamic secrets | No | Yes (DB creds, cloud creds, PKI certs) |
| Secret rotation | Manual re-encrypt | Automatic (lease-based expiry) |
| Access control | Whoever has the KMS/Age key | Fine-grained ACL per path, per identity |
| Audit trail | Git history | Full request/response audit log |
| Operational cost | Zero | Medium (server, TLS, monitoring) |
| CI integration | Decrypt at build time | Agent injects as env vars, or API call |

### When SOPS is still better

- Single operator, single node -- zero operational overhead
- GitOps workflow -- secrets alongside code, decrypted only at deploy time
- Offline operation -- no network dependency
- **Bootstrap** -- you need SOPS (or equivalent) to deploy the OpenBao unseal key

### When OpenBao becomes necessary

- Multiple CI jobs need scoped, short-lived credentials
- Dynamic secrets (per-job DB passwords that auto-expire)
- PKI/TLS certificate automation
- Multi-tenant isolation (namespaces)
- Audit requirements beyond Git history

### They work together

SOPS supports Vault/OpenBao's Transit engine as an encryption backend. The recommended model:
SOPS manages the unseal key and initial root token (chicken-and-egg bootstrap). Everything
else lives in OpenBao.

Sources:
- https://github.com/getsops/sops
- https://medium.com/@eric.mourgaya/use-vault-as-backend-of-sops-1141fcaab07a

## OpenBao vs Infisical

Infisical is an MIT-licensed secrets management platform focused on developer experience.

| Dimension | OpenBao | Infisical |
|-----------|---------|-----------|
| License | MPL 2.0 (all features) | MIT (core), paid tiers for advanced |
| Dependencies | None (single binary + Raft) | PostgreSQL + Redis |
| Dynamic secrets | Yes (built-in) | Paid tiers only |
| PKI / cert management | Yes (built-in) | Paid tiers only |
| Encryption as a service | Yes (Transit engine) | No |
| Self-hosted complexity | Higher (unseal ceremony, ACL) | Lower (standard PostgreSQL + Redis) |
| Hardware requirements | Lightweight single binary | 4GB RAM, 4 vCPUs minimum |
| Auth methods | AppRole, LDAP, OIDC, GitHub, K8s, TLS, many more | SAML, OIDC, email/password |
| UI/DX | CLI-first, functional web UI | Modern web UI, developer-friendly |

### Why not Infisical for forge-metal

1. Dynamic secrets and PKI are behind paid tiers -- OpenBao includes them free
2. Requires PostgreSQL + Redis as dependencies -- adds operational surface
3. forge-metal already has complex infrastructure; a single binary is preferable
4. GitLab chose OpenBao over alternatives for their secrets backend
   (https://handbook.gitlab.com/handbook/engineering/architecture/design-documents/secret_manager/decisions/007_openbao/)

### When Infisical might be better

- Team-first environments where developer UX matters more than flexibility
- Existing PostgreSQL infrastructure where adding a table is cheaper than a new service
- Simpler secret management needs (KV only, no dynamic secrets)
