# Resource Model

Guardian resources use the same top-level shape:

```yaml
apiVersion: <group>/<version>
kind: <Kind>
metadata:
  name: <dns-label>
spec: {}
```

Reports use the same observed-state shape:

```yaml
apiVersion: <group>/<version>
kind: <Kind>Report
metadata:
  name: <resource-name>
status:
  observedGeneration: 3
  specHash: sha256:...
  conditions:
    - type: Accepted
      status: "True"
      reason: Valid
      observedGeneration: 3
      lastTransitionTime: "2026-06-05T00:00:00Z"
```

## Problem Space

Problem-space resources are provider-neutral. A field belongs in a problem-space
resource only when the field still makes sense with multiple providers.

Examples:

- `DNSResolution`
- `PublicTLS`
- `ArtifactObjectStorage`
- `RuntimeSecretProjection`
- `ProviderAuthority`

## Provider Binding

Provider bindings select a plugin and credentials authority.

```yaml
apiVersion: guardian.verself.sh/v1alpha1
kind: ProviderBinding
metadata:
  name: cloudflare-main
spec:
  provider: cloudflare
  authorityRef: cloudflare-account-admin
  providerConfig:
    apiVersion: provider.cloudflare.guardian.verself.sh/v1alpha1
    kind: CloudflareProviderConfig
    spec: {}
```

`providerConfig.spec` is the only open extension point. Once a provider plugin
is selected, that provider's schema is strict.

## References

References are local names within the compiled specification:

- `providerRef`
- `authorityRef`
- `domainRef`
- `secretRef`

Dynamic values use a small allowlist. The first allowed dynamic value is:

- `environment.ingress.publicIPv4`

Arbitrary JSONPath, templating, and expression languages are outside the core
specification.

## Validation

Validation rejects:

- unknown fields outside explicit extension points;
- duplicate resource names for the same `apiVersion` and `kind`;
- unresolved references;
- provider bindings without an installed plugin;
- provider configs that fail the plugin schema;
- source specs that require implicit defaults to become meaningful.

## Versioning

Resource versions are immutable contracts. A released version may gain optional
fields. Required fields, renamed fields, removed fields, changed defaults, or
changed meanings require a new version.

Conversions are explicit resources or plugin entrypoints. Conversion must
produce a materialized target version that can be hashed and reviewed.

## Evidence

Conditions use positive names for desired progress:

- `Accepted`
- `ResolvedRefs`
- `ProviderConfigured`
- `Verified`
- `Recovered`

Provider plugins may add provider-specific conditions. They must document each
condition type, status, reason set, and whether the condition is stable API.

Reports may include provider request IDs, external resource fingerprints, drift
summaries, and generated artifact digests. Reports must not include secret
values.
