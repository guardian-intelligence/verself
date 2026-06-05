# GuardianSpecification

GuardianSpecification is a CRD specification for homeostatic systems.

A homeostatic system continuously reconciles source code, credentials, provider
state, and network I/O toward a declared operating state. The specification
defines resource schemas, provider plugin contracts, validation rules, and
evidence reports. It does not require Kubernetes. Nomad tasks, CLIs, deployment
services, or other controllers may reconcile the resources.

## Core Objects

`HomeostaticResourceDefinition` defines a problem-space resource kind.

```yaml
apiVersion: guardian.verself.sh/v1alpha1
kind: HomeostaticResourceDefinition
metadata:
  name: dnsresolutions.network.guardian.verself.sh
spec:
  group: network.guardian.verself.sh
  names:
    kind: DNSResolution
    plural: dnsresolutions
  versions:
    - name: v1alpha1
      schema: cue/network/v1alpha1/dns_resolution.cue
      references:
        - providerRef
        - domainRef
      allowedValueFrom:
        - environment.ingress.publicIPv4
      conditions:
        - Accepted
        - ResolvedRefs
        - ProviderConfigured
        - Verified
        - Recovered
```

`ProviderPlugin` declares which resource kinds a provider implements.

```yaml
apiVersion: guardian.verself.sh/v1alpha1
kind: ProviderPlugin
metadata:
  name: cloudflare
spec:
  implements:
    - apiVersion: network.guardian.verself.sh/v1alpha1
      kind: DNSResolution
  providerConfigs:
    - apiVersion: provider.cloudflare.guardian.verself.sh/v1alpha1
      kind: CloudflareProviderConfig
      schema: cue/provider/cloudflare/v1alpha1/provider_config.cue
  authorities:
    - cloudflare.api_token
  conditions:
    - ProviderConfigured
    - ProviderVerified
  conformance:
    fixtures: conformance/cloudflare
```

`ProviderBinding` connects a resource to a plugin-specific provider config.

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
    spec:
      accountID: c3eaeffaadf7d4847684d4775c16d598
      zones:
        - name: product
          zone: verself.sh
          domain: gamma.verself.sh
```

`DNSResolution` is the first problem-space resource.

```yaml
apiVersion: network.guardian.verself.sh/v1alpha1
kind: DNSResolution
metadata:
  name: gamma-public-dns
spec:
  providerRef: cloudflare-main
  records:
    - domainRef: product
      name: "@"
      type: A
      valueFrom: environment.ingress.publicIPv4
```

## Public Contract

Problem-space resources define what must be true. Provider bindings define how
that resource is reconciled in a concrete environment. Provider config is the
only explicit extension point.

Unknown fields are rejected by default. Providers validate their own
`providerConfig.spec` schema.

References are local and typed: `providerRef`, `authorityRef`, `domainRef`, and
`secretRef`. Dynamic references are limited to an allowlist such as
`environment.ingress.publicIPv4`.

The source spec has no implicit defaults. Tools may produce a materialized
compiled spec, and reconcilers report the hash of the exact compiled input.

Desired state and observed state are separate. Reconciliation reports use
conditions such as `Accepted`, `ResolvedRefs`, `ProviderConfigured`, `Verified`,
and `Recovered`, with `observedGeneration` or `specHash`.

Every resource has `apiVersion` and `kind`. Released versions are immutable in
meaning. Required, renamed, removed, or semantic changes require a new version
and explicit conversion.

## Reference Patterns

- Kubernetes API conventions: resource envelopes, `spec`, `status`, and
  versioned kinds.
- Gateway API: positive conditions such as `Accepted`, `ResolvedRefs`, and
  `Programmed`.
- Crossplane: provider packages, provider configs, and provider-owned
  reconciliation.
- CUE: strict schema validation, composition, generated schemas, and authoring
  support.

Primary references:

- <https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md>
- <https://gateway-api.sigs.k8s.io/guides/implementers/>
- <https://gateway-api.sigs.k8s.io/geps/gep-1364/>
- <https://docs.crossplane.io/latest/packages/providers/>
- <https://cue.dev/docs/>
