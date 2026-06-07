# Configuration and Credentials

Guardian reports a digest of the resource graph it consumed.

```yaml
apiVersion: networking.guardianintelligence.org/v1alpha1
kind: PublicOrigin
metadata:
  name: product
spec:
  url: https://gamma.verself.sh
```

`PublicOrigin` is the shared URL resource. Components that need a public base
URL reference the origin and consume the full CRD graph through their
component-owned Nomad job.

Each service and infrastructure component owns a CRD schema for its own
configuration. The component CRD is the source for static component
configuration, provider references, backup policy, and runtime invariants.
Guardian reads one root graph and writes it into the materialized workspace;
component jobs project the resources they need.

## Configuration Classes

Committed resource graphs contain public or low-sensitivity desired state:

- public origins;
- provider account IDs;
- zone names;
- component resource names;
- component runtime configuration;
- operator recipient identities;
- backup policy names.

Offsite encrypted backups contain state that should survive the host:

- OpenBao Raft snapshots;
- signed backup manifests;
- backup object digests;
- component database snapshots;
- component-specific restore metadata.

Operator recovery authority stays outside the host:

- Shamir unseal shares;
- PGP private keys held by operators;
- hardware-backed keys;
- KMS/HSM authority;
- provider parent credentials that must be imported or rotated from the
  provider control plane.

For gamma, the present-day durability compromise is to store restore material
as encrypted blobs in Cloudflare R2. R2 is the storage location, not the root of
trust. The encrypted material remains useless without the operator-held PGP
private keys or a component-owned import token that has already been
established in OpenBao.

Longer term, environments may upgrade the root of trust to hardware-backed HSM
or external KMS authority. That is an upgrade path for stronger unseal/import
security, not a prerequisite for using reconciliation to recover gamma.

## Secret Handling

Secret values MUST NOT be embedded directly in committed resource graphs.
Secret values MUST NOT be passed through argv, environment variables, command
responses, telemetry, or durable host files.

Recovery binaries MAY request operator-held authority through an ephemeral
operator path. The request must bind the operation to verifiable facts: target
identity, repo upload digest, recovery binary digest, snapshot digest, requested
action, and recipient identities.

Implementations emit fingerprints, stable identifiers, and conditions. Command
results never include secret values.

## Provider Authority

Provider authority is a component concern. A Cloudflare component may
declare that Cloudflare account authority is required, but the base Guardian
schema does not define a file path or secret projection for the token.

During normal recovery, provider authority should come from restored and
unsealed OpenBao state. During from-zero recovery without a usable snapshot, the
component reports its own import blocker until an operator imports or rotates
the provider credential through a component-owned path.

This is a component-specific command/report condition, not a Guardian CRD.
Components use it when all autonomous sources have been exhausted and an
operator must present authority.

For Cloudflare recovery, the autonomous sources are:

- an existing Cloudflare account-admin credential in OpenBao;
- a bucket-scoped R2 recovery credential in OpenBao that can read and write the
  configured recovery bucket.

If neither exists, the Cloudflare component must fail loudly with
`CloudflareRecoveryAuthorityAvailable=False` with a component-owned reason. The
operator may then provide an account-admin token through the component-owned
import path. The preferred from-zero path is to decrypt the scoped OpenBao
import token from OpenBao `init-material.json` with an operator-held PGP
private key, then import the Cloudflare token from a local operator-only file.
Stdin JSON import remains available for tightly controlled operator sessions.
A local `secret.env` file is acceptable as an operator-held source only when it
is gitignored, readable only by the operator, and transformed into the import
command without being committed, logged, passed through argv, or written to
host-local durable files.
