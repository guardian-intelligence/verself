# Resource Model

Guardian resources use the same top-level shape:

```yaml
apiVersion: <group>/<version>
kind: <Kind>
metadata:
  name: <dns-label>
spec: {}
```

The envelope has no built-in `status`, generation, report, provider, reference,
or compiled-resource fields.

## Problem Space

Problem-space resources are HRDs. A field belongs in a provider-neutral HRD only
when the field still makes sense with multiple providers.

Examples:

- `DNSResolution`
- `PublicTLS`
- `ArtifactObjectStorage`
- `RuntimeSecretProjection`
- `ProviderAuthority`

## Custom DNSResolution

Anyone can publish a DNS resolution HRD. GuardianSpecification only requires
that it use the common envelope.

```yaml
apiVersion: network.example.com/v1alpha1
kind: DNSResolution
metadata:
  name: public-ingress
spec:
  providerRef: route53-main
  records: []
```

A provider plugin opts into that HRD by declaring the exact `apiVersion` and
`kind` it implements.

## Provider Binding

Provider bindings and provider configs are ordinary resources. Their schemas are
defined by HRDs and plugin manifests, not by the base envelope.

## References

References are ordinary resource-specific fields. Recommended local reference
names are:

- `providerRef`
- `authorityRef`
- `domainRef`
- `secretRef`

Dynamic values should use small allowlists. The first Verself DNSResolution
allowlist is expected to be:

- `environment.ingress.publicIPv4`

Arbitrary JSONPath, templating, and expression languages are outside the core
specification.

## Validation

Base envelope validation rejects:

- unknown top-level fields;
- duplicate resource names for the same `apiVersion` and `kind`;
- invalid `apiVersion`, `kind`, or `metadata.name`;
- missing `spec`.

HRDs and provider plugins perform resource-specific validation.

## Versioning

Resource versions are immutable contracts. A released version may gain optional
fields. Required fields, renamed fields, removed fields, changed defaults, or
changed meanings require a new version.

Conversions are HRD or plugin entrypoints. Conversion must produce a
materialized target version that can be hashed and reviewed.

## Evidence

Evidence is emitted by reconcilers, not embedded in source resources. Conditions
should use positive names for desired progress:

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
