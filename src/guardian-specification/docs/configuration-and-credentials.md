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
Guardian reads one root graph and writes it into the boarded workspace;
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
