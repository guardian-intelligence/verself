# Engineering Milestones

## 1. Core Resource Envelope

Define shared CUE schemas for:

- resource envelope: `apiVersion`, `kind`, `metadata.name`, `spec`;
- resource identity: `{apiVersion, kind, metadata.name}`;
- stable resource hash over identity and `spec`.

Deliverables:

- CUE source schemas;
- Go types;
- valid and invalid conformance fixtures.

Out of scope:

- report/status envelope;
- `metadata.generation`;
- provider binding schemas;
- reference schemas;
- compiled specification public kind.

## 2. HomeostaticResourceDefinition

Define `HomeostaticResourceDefinition` as the schema contract for custom
problem-space resources.

Deliverables:

- HRD CUE schema;
- validation for resource names, versions, and schemas;
- docs for versioning and conversion;
- fixture set for accepted and rejected HRDs.

## 3. DNSResolution

Define the first Verself problem-space resource. It is still just another HRD;
external projects may publish their own DNS resolution HRDs under their own API
groups.

Initial scope:

- A records;
- local `domainRef`;
- explicit `ttl`;
- `valueFrom: environment.ingress.publicIPv4`;
- `providerRef`;
- conditions: `Accepted`, `ResolvedRefs`, `ProviderConfigured`, `Verified`,
  `Recovered`.

Out of scope for the first version:

- wildcard record synthesis;
- arbitrary record templates;
- weighted records;
- health-checked failover;
- cross-document references;
- provider-specific flags such as Cloudflare `proxied`.

Deliverables:

- `DNSResolution` HRD;
- CUE schema and generated types;
- conformance fixtures;
- migration of gamma DNS records from `CloudflareRecovery`.

## 4. Cloudflare Provider Plugin

Split Cloudflare-specific fields out of `CloudflareRecovery`.

Initial provider config:

- account ID;
- zone mappings;
- account-admin authority reference;
- required Cloudflare permission metadata.

Deliverables:

- `ProviderPlugin` manifest for Cloudflare;
- `CloudflareProviderConfig` schema;
- validator for provider config;
- DNS reconciler that consumes `DNSResolution` plus `ProviderBinding`;
- live gamma dry-run evidence.

## 5. PublicTLS

Define certificate recovery as a problem-space resource.

Initial scope:

- certificate names;
- domain list;
- ACME directory URL;
- contact email;
- output artifact location;
- DNS-01 provider binding.

Deliverables:

- `PublicTLS` HRD;
- Cloudflare DNS-01 provider support;
- report conditions for challenge readiness and certificate materialization.

## 6. ArtifactObjectStorage

Define deployment artifact storage as a problem-space resource.

Initial scope:

- bucket name;
- credential capability set;
- runtime secret output refs;
- provider child credential TTL;
- persistence target.

Deliverables:

- `ArtifactObjectStorage` HRD;
- Cloudflare R2 provider support;
- OpenBao persistence verification;
- runtime secret report fingerprints.

## 7. Provider Registry

Define a registry format for open-source provider plugins.

Registry entries declare:

- plugin name and version;
- implemented resource kinds and versions;
- provider config schemas;
- required authority types and permission metadata;
- emitted conditions;
- conformance fixture paths;
- optional generated artifacts.

Deliverables:

- local registry layout;
- plugin discovery validator;
- fixture runner;
- generated docs index.

## 8. Cutover

Replace the transitional `CloudflareRecovery` document with provider-neutral
resources plus Cloudflare provider bindings.

Deliverables:

- gamma compiled Guardian specification;
- Cloudflare recovery job consumes compiled resources;
- `CloudflareRecovery` removed;
- live gamma dry-run evidence;
- Nomad plan evidence once OpenBao/Vault integration is present on the node.
