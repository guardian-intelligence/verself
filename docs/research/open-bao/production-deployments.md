# Production Deployments

Real-world OpenBao deployments and what they teach us.

## GitLab (largest known production user)

GitLab uses OpenBao as its native Secrets Manager backend. This is the strongest signal of
production viability.

Source: https://openbao.org/blog/cipherboy-fosdem-25-talk/
Docs: https://docs.gitlab.com/administration/secrets_manager/

### Architecture

Three-entity trust model:
- **GitLab Rails** -- admin operations (create secrets, manage policies)
- **Pipeline Workers** -- fetch secrets via OIDC JWT
- **OpenBao** -- authorization + encrypted storage

### Key decisions

| Decision | Choice | Why |
|----------|--------|-----|
| Storage backend | PostgreSQL (not Raft) | Smaller instances avoid Raft complexity; GitLab already runs PostgreSQL |
| Auth method | JWT/OIDC | Property-based: JWT claims contain repo, branch, user, environment |
| Multi-tenancy | Separate path areas per tenant | Per-project KVv2 engines, separate auth mounts |
| Feature tier | Ultimate only | Enterprise-grade feature, requires Runner 18.6+ |

### Governance involvement

GitLab achieved voting status in the OpenBao project (October 2024). Alex Scheel is "one of
the few people employed to work on OpenBao full time" (IBM). GitLab's adoption provides both
validation and ongoing engineering investment.

Source: https://handbook.gitlab.com/handbook/engineering/architecture/design-documents/secret_manager/decisions/007_openbao/

## ControlPlane (commercial support)

ControlPlane offers "Enterprise for OpenBao" -- commercial support, consulting, and managed
services. This provides an escape hatch if the project needs professional support.

Source: https://control-plane.io/enterprise-for-openbao/

## Community deployment guides

### stderr.at blog series (Feb-Mar 2026)

7-part guide covering:
1. Standalone installation
2. OpenShift/Helm deployment
3. GitOps with Argo CD
4. Authentication methods
5. Secrets engines (KV, PKI, Transit)
6. Monitoring and observability
7. Certificate management

Source: https://blog.stderr.at/openshift-platform/security/secrets-management/openbao/

### Linode deployment guide

Single-node deployment with hardening recommendations. Covers Raft storage, TLS setup,
and basic secret operations.

Source: https://www.linode.com/docs/guides/deploying-openbao-on-a-linode-instance/

### Railway one-click template

One-click deploy for quick evaluation. Not production-grade but shows OpenBao is
deployable as a single container.

## Governance and long-term viability

**Timeline:**
- Late 2023: Fork created after HashiCorp's BSL relicensing
- Originally hosted under LF Edge (Linux Foundation)
- June 2025: Migrated to OpenSSF (Open Source Security Foundation), sandbox stage

**Structural protections:**
- Linux Foundation holds admin access to all repositories and registries
- No single company can hijack or relicense the project
- MPL 2.0 is OSI-approved
- Multi-company contributor base: IBM, ControlPlane, Adfinis, GitLab, SAP, WALLIX

**OpenSSF alignment:**
- Security-focused review processes
- Integration with Sigstore and SLSA
- OpenSSF Best Practices badge application in progress

The LF governance structurally prevents a re-licensing event. Combined with GitLab's adoption
and IBM's engineering investment, this is a sustainable project for long-term dependency.

Sources:
- https://openssf.org/blog/2025/06/17/openbao-joins-the-openssf-to-advance-secure-secrets-management-in-open-source/
- https://openbao.org/blog/openbao-joins-the-openssf/
- https://www.bestpractices.dev/en/projects/9126?criteria_level=1
