# GuardianSpecification

GuardianSpecification is a lean CRD specification for homeostatic systems.

A homeostatic system continuously reconciles source code, credentials, provider
state, and network I/O toward a declared operating state. The specification
defines the common resource envelope, validation rules, and plugin discovery
contracts. It does not require Kubernetes. Nomad tasks, CLIs, deployment
services, or other controllers may reconcile the resources.

The base envelope is deliberately small:

```yaml
apiVersion: network.guardian.verself.sh/v1alpha1
kind: DNSResolution
metadata:
  name: gamma-public-dns
spec: {}
```

`apiVersion`, `kind`, and `metadata.name` identify a resource. `spec` belongs
to the resource definition selected by that identity.

## Core Objects

`HomeostaticResourceDefinition` defines a problem-space resource kind. Resource
authors may publish their own HRDs for DNS resolution or any other problem
space.

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
      schema: cue/network/v1alpha1/dns_resolution.cue
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

`DNSResolution` is the first problem-space resource to define for Verself. It is
still a normal HRD. Other projects can define their own DNS resolution resource
under their own API group.

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

GuardianSpecification defines the envelope. HRDs define resource-specific
schema. Provider plugins implement those resource kinds.

Unknown envelope fields are rejected by default. HRDs and provider plugins
validate their own `spec` schemas.

References are resource-specific fields. The recommended local reference names
are `providerRef`, `authorityRef`, `domainRef`, and `secretRef`. Dynamic
references should be explicit allowlists, such as
`valueFrom: environment.ingress.publicIPv4`.

The source spec has no implicit defaults. If a resource needs defaults, tooling
must materialize them in a compiled form and hash the exact compiled input.

Desired state and observed state are separate. Conditions belong to reports
emitted by reconcilers, not to the base source envelope.

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
