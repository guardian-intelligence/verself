# Spec Scope

Guardian alpha keeps the base protocol small. The base spec defines the
resource graph envelope, the preflight entrypoint, substrate access/upload
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

## Operating Rule

The base spec answers two questions:

- Which resource graph should the implementation load?
- How should the implementation preflight a substrate enough for component
  control loops to run?

Component schemas answer what a component should know before it starts.
Component Nomad jobs and owner-local binaries answer what the component should
do at runtime. Runtime evidence answers what happened.

The root graph is static input. A component derives operations from its CRD and
the observed state of the host, then reports conditions and evidence through
runtime channels.

## Placement Convention

Place every new concept at the narrowest layer that can validate and use it.

| Concept | Location | Example |
| --- | --- | --- |
| Graph envelope, entrypoint, upload hooks, kernel hooks, Nomad run hook, shared origin facts, command response shape | Guardian base spec | `FlyProcedure`, `Substrate`, `PublicOrigin`, `ready_to_fly` |
| Static configuration for one deployable component | Component CRD beside the owner | `OpenBaoCluster`, `HAProxyGateway`, `ObjectStorageService` |
| Runtime behavior and convergence steps | Component-owned Nomad job and owner-local binary | `task "recover"` prestart logic |
| Provider authority, unseal material, private keys, runtime DEKs | External operator or trust path | Shamir shares, PGP private keys, provider parent tokens |
| Observed outcomes and forensic evidence | Command responses, Nomad, component reports, health endpoints, telemetry | allocation logs, `/run/verself/recovery/openbao/report.json`, `/recoveryz` |

The base spec may add a field only when the field has portable,
cross-component semantics. Component-specific behavior remains in the owner
schema and runtime implementation.

## Authoring Convention

The base Guardian schema is the shared envelope plus preflight substrate facts.
After preflight, the resource graph is static input for component-owned runtime
code.

Use this convention when authoring a graph:

- put the `FlyProcedure`, `Substrate`, and shared `PublicOrigin` resources in
  the root graph;
- put every component's static configuration in that component's CRD schema
  under `guardian/v1alpha1/schema.cue` beside the owner;
- make the component schema complete for static inputs: public names, provider
  account identifiers, OpenBao paths, artifact paths, socket paths, backup
  object names, policy names, and refs to shared resources;
- let component Nomad jobs read `.guardian/fly/document.json` from the
  materialized workspace and select the resources they own;
- express restore, initialize, unseal, migrate, wait, import, publish, and
  health-check actions in component lifecycle tasks or owner-local binaries;
- report blockers as command conditions, component reports, Nomad state,
  health endpoints, or telemetry.

The base schema has no component operation fields. Component sequencing,
Nomad submission details, component environment projection, host-local secret
paths, provider-token files, and report resources live in the owning component
runtime. Static facts needed by that runtime live in the owning component
schema.

Do not add fields named `recovery`, `recover`, `submit`, `wait`, `restore`,
`migrate`, or similar operation verbs to the base Guardian schema. If a
component needs static configuration for such an operation, name the static
fact instead. For example, model `snapshotPath`, `bucketName`, `policyName`,
`runtimeRoot`, or `publicOriginRef`; let the component's Nomad lifecycle task
decide whether to restore, create, update, wait, or no-op.

## Schema Locations

The base schema lives under:

```text
src/guardian-specification/cue/guardian/v1alpha1/schema.cue
```

Component schemas live beside the component that owns the behavior:

```text
src/infrastructure-components/openbao/guardian/v1alpha1/schema.cue
src/infrastructure-components/nftables/guardian/v1alpha1/schema.cue
src/infrastructure-components/nats/guardian/v1alpha1/schema.cue
src/infrastructure-components/nomad-observer/guardian/v1alpha1/schema.cue
src/infrastructure-components/otelcol/guardian/v1alpha1/schema.cue
src/infrastructure-components/zitadel/guardian/v1alpha1/schema.cue
src/infrastructure-components/haproxy/guardian/v1alpha1/schema.cue
src/integrations/cloudflare/control-plane/guardian/v1alpha1/schema.cue
src/services/object-storage-service/guardian/v1alpha1/schema.cue
```

Config examples import the base schema. Component schemas live beside their
owners and validate component resources through owner-local tests, component
runtime parsers, and future registry tooling. A site-specific file such as
`examples/gamma/gamma.cue` is a bundle of selected resources, provider
identifiers, origins, and substrate hooks. The site name is an operator label.

## Base Spec

The base spec owns:

- `Document`: `entrypoint` plus `resources`;
- `FlyProcedure`: the root procedure that references one `Substrate` and one
  Nomad run hook;
- `Substrate`: access, upload, and fixed executor lifecycle hook commands;
- `PublicOrigin`: shared externally visible URL facts;
- CLI response keys such as `ready_to_fly`, upload digests, hook status, and
  conditions.

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

Component CRDs describe desired inputs that component-owned code can read from
the materialized graph. They do not describe step order.

The component CUE schema is the semantic source for static configuration. Go
structs, Nomad templates, generated validators, and docs may mirror that schema
for runtime use, but they do not define additional configuration fields.
Runtime code may derive actions from the schema plus observed state, but it
must not rely on hidden Guardian-only configuration.

Component schemas should contain the full static configuration needed by that
component: provider account IDs, public resource names, OpenBao paths, Nomad
workload roles, backup object locations, runtime artifact paths, policy names,
and references to shared resources. A reader should be able to understand a
component's intended static state from its CRD and referenced shared resources.
Site examples instantiate component resources; schema fields are authored in
the owner-local `guardian/v1alpha1/schema.cue`.

Static configuration means declarative inputs that can be validated without
performing the operation: names, URLs, artifact paths, socket paths, policy
names, nonsecret provider identifiers, backup object names, and references to
other graph resources. A static field may describe where a component should find
authority; it does not contain the secret authority value.

Component schemas may include desired static state such as PostgreSQL roles,
HAProxy routes, object storage buckets, backup object names, provider account
identifiers, or service auth audiences. The owning Nomad task derives
operations such as restore, unseal, migrate, and no-op from observed component
state.

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
| How `guardian preflight` reaches, uploads to, extracts on, or verifies a substrate | `Substrate` |
| A URL that multiple components consume as public configuration | `PublicOrigin` |
| Which host daemon, service, provider, backup object, policy, or runtime path a component owns | Component CRD |
| A component operation such as init, restore, unseal, certificate issuance, bucket creation, schema migration, or health wait | Nomad lifecycle task or owner-local binary |
| A secret value or root credential | External trust path |
| A status line, digest, allocation ID, failure reason, or recovery observation | CLI response or component evidence |

When a component needs a shared base resource, it references that resource in
its own CRD. For example, an edge component references `PublicOrigin`; OpenBao
owns OpenBao-specific seal, policy, auth, and backup configuration.

## Nomad Convention

Component convergence and deployment are Nomad conventions. A component job
file is checked into the component directory and shipped inside the materialized
repo. The job file may include lifecycle tasks that:

- read `/home/ubuntu/.local/state/guardian/repo/current/workspace/.guardian/fly/document.json`;
- select the component CRD resources it owns;
- install repo-built runtime artifacts;
- restore or initialize state;
- reconcile static configuration;
- emit component health evidence;
- block loudly with stable conditions when operator authority or provider
  authority is missing.

The component job is the runtime boundary. Guardian keeps the graph available;
Nomad and component code decide how to start, retry, block, and converge.

Use `prestart` lifecycle tasks for idempotent recovery work that must complete
before the main task starts. Use a long-running task only when the component
needs continuous reconciliation after startup.

Nomad agent recovery is preflight machinery for the current alpha slice. The
pinned Nomad runtime is shipped in the materialized repo and installed by the
declared preflight playbook. Add owner-local component CRDs when static
configuration no longer fits in the playbook and Nomad-owned job file defaults.

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

Components that need external authority report a component-specific blocker and
wait for a component-owned operator path.

## Scope Test

Before adding a field or resource to the base spec, answer these questions:

- Does this describe a fact needed by multiple independent components?
- Can the fact be validated without knowing a component implementation?
- Would a component-owned CRD create ambiguity or duplication?
- Can a compatible implementation use the field without adopting Verself's
  internal services?

If any answer is no, keep the shape in the owning component CRD or Nomad job.

## References

- Kubernetes custom resources: https://kubernetes.io/docs/concepts/api-extension/custom-resources/
- Kubernetes object spec/status convention: https://kubernetes.io/docs/concepts/overview/working-with-objects/
- Nomad task lifecycle hooks: https://developer.hashicorp.com/nomad/docs/job-specification/lifecycle
