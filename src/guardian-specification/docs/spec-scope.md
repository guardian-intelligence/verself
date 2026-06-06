# Spec Scope

Guardian alpha keeps the base protocol small. The base spec defines the
resource graph envelope, the boarding entrypoint, substrate access/upload
hooks, shared public-origin facts, command response shape, and stable condition
names for cross-component blockers.

The base resource graph uses the Kubernetes-style envelope:

```yaml
apiVersion: <group>/<version>
kind: <Kind>
metadata:
  name: <name>
spec: {}
```

Component resources use the same envelope and carry component-owned static
configuration. Runtime actions are implemented by the owning component.

## Placement Convention

Place every new concept at the narrowest layer that can validate and use it.

| Concept | Location | Example |
| --- | --- | --- |
| Graph envelope, entrypoint, upload hooks, shared origin facts, command response shape | Guardian base spec | `FlyProcedure`, `Substrate`, `PublicOrigin`, `ready_to_fly` |
| Static configuration for one deployable component | Component CRD beside the owner | `OpenBaoCluster`, `HAProxyGateway`, `ObjectStorageService` |
| Runtime behavior and recovery steps | Component-owned Nomad job and recovery binary | `task "recover"` prestart logic |
| Provider root authority, unseal material, private keys, runtime DEKs | External operator or trust path | Shamir shares, PGP private keys, provider parent tokens |
| Observed outcomes and forensic evidence | Command responses, Nomad, component reports, health endpoints, telemetry | allocation logs, `/run/verself/recovery/openbao/report.json`, `/recoveryz` |

The base spec may add a field only when the field has portable,
cross-component semantics. Component-specific behavior remains in the owner
schema and runtime implementation.

## Schema Locations

The base schema lives under:

```text
src/guardian-specification/cue/guardian/v1alpha1/schema.cue
```

Component schemas live beside the component that owns the behavior:

```text
src/infrastructure-components/openbao/guardian/v1alpha1/schema.cue
src/infrastructure-components/nomad/guardian/v1alpha1/schema.cue
src/infrastructure-components/haproxy/guardian/v1alpha1/schema.cue
src/integrations/cloudflare/control-plane/guardian/v1alpha1/schema.cue
src/services/object-storage-service/guardian/v1alpha1/schema.cue
```

Config examples may import the base schema and any component schemas needed by
the selected graph. A site-specific file such as `examples/gamma/gamma.cue` is
a bundle of selected resources, provider identifiers, origins, and substrate
hooks. The site name is an operator label, not a protocol concept.

## Base Spec

The base spec owns:

- `Document`: `entrypoint` plus `resources`;
- `FlyProcedure`: the root procedure that references one `Substrate`;
- `Substrate`: access and upload lifecycle hook commands;
- `PublicOrigin`: shared externally visible URL facts;
- CLI response keys such as `ready_to_fly`, upload digests, hook status, and
  conditions;
- common condition names that cross component boundaries, currently including
  `RootTrustMaterialAvailable`.

Adding a base resource requires evidence that multiple unrelated components
need the same fact and would otherwise duplicate incompatible schemas. A
component-local need stays in the component CRD.

## Component CRDs

Every infrastructure component and service owns its own CRD schema. The schema
defines static configuration and invariants for that component:

- provider account identifiers and public resource names;
- public origins and routes consumed by the component;
- backup policy identifiers and object locations;
- runtime artifact paths owned by the component;
- references to other resources in the graph.

Component CRDs do not define Guardian execution steps. They describe desired
inputs that component-owned code can read from the boarded graph.

Component schemas should contain the full static configuration needed by that
component: provider account IDs, public resource names, OpenBao paths, Nomad
workload roles, backup object locations, runtime artifact paths, policy names,
and references to shared resources. A component should not require a reader to
combine a base Guardian field with undocumented component defaults to understand
what it will reconcile.

Secret values stay out of component CRDs. A component that needs root or
provider authority declares the nonsecret authority location or recipient
identity and reports a condition when the authority is absent. For example,
Cloudflare recovery reads its account-admin credential from the OpenBao path it
owns; if that path is absent, it reports `CloudflareAccountAuthorityAvailable`
as false. The CRD does not point at a durable host file containing the provider
token.

## Field Placement

Use these checks before adding a field:

| Proposed field describes | Placement |
| --- | --- |
| How `guardian board` reaches, uploads to, extracts on, or verifies a substrate | `Substrate` |
| A URL that multiple components consume as public configuration | `PublicOrigin` |
| Which host daemon, service, provider, backup object, policy, or runtime path a component owns | Component CRD |
| A component operation such as init, restore, unseal, certificate issuance, bucket creation, schema migration, or health wait | Nomad lifecycle task or component recovery binary |
| A secret value or root credential | External trust path |
| A status line, digest, allocation ID, failure reason, or recovery observation | CLI response or component evidence |

When a component needs a shared base resource, it references that resource in
its own CRD. For example, an edge component references `PublicOrigin`; OpenBao
recovery owns OpenBao-specific seal, policy, auth, and backup configuration.

## Nomad Convention

Component recovery and deployment are Nomad conventions. A component job file is
checked into the component directory and shipped inside the boarded repo. The
job file may include lifecycle tasks that:

- read `/home/ubuntu/.local/state/guardian/repo/current/workspace/.guardian/fly/document.json`;
- select the component CRD resources it owns;
- install repo-built runtime artifacts;
- restore or initialize state;
- reconcile static configuration;
- emit component health evidence;
- block loudly with stable conditions when root trust material or provider
  authority is missing.

Guardian does not submit component jobs, order component dependencies, project
component-specific environment variables, or render component-specific Nomad
variables. The component job is the runtime boundary.

## Evidence

Command responses are ephemeral. Component evidence lives in component-owned
places:

- Nomad evaluations, deployments, allocations, task logs, and service checks;
- component report files such as `/run/verself/recovery/openbao/report.json`;
- component `/recoveryz` or service health endpoints;
- telemetry and audit events.

Evidence resources may become extension CRDs later, but they are not part of
the base protocol.

## Secrets

Secret values are outside the committed graph. Resource graphs may contain
public identifiers, provider account IDs, object names, and operator recipient
identities. They do not contain root credentials, provider token values, unseal
shares, private keys, or runtime DEKs.

Components that need external authority report a blocker such as
`RootTrustMaterialAvailable=False` and wait for a component-owned operator path.

## Scope Test

Before adding a field or resource to the base spec, answer these questions:

- Does this describe a fact needed by multiple independent components?
- Can the fact be validated without knowing a component implementation?
- Would a component-owned CRD create ambiguity or duplication?
- Can a compatible implementation use the field without adopting Verself's
  internal services?

If any answer is no, keep the shape in the owning component CRD or Nomad job.
