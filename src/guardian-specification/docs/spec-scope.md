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
