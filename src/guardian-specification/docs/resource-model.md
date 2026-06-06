# Resource Model

Guardian alpha uses one file as the runtime input. The file is an offline
resource graph: an `entrypoint` object reference plus Kubernetes-style
resources with `apiVersion`, `kind`, `metadata`, and `spec`.

```yaml
entrypoint:
  apiVersion: guardian.guardianintelligence.org/v1alpha1
  kind: FlyProcedure
  name: gamma

resources:
  - apiVersion: guardian.guardianintelligence.org/v1alpha1
    kind: FlyProcedure
    metadata:
      name: gamma
    spec:
      substrateRef:
        apiVersion: substrate.guardianintelligence.org/v1alpha1
        kind: Substrate
        name: gamma-primary

  - apiVersion: networking.guardianintelligence.org/v1alpha1
    kind: PublicOrigin
    metadata:
      name: product
    spec:
      url: https://gamma.verself.sh
```

`board` and `fly` read the same graph. `board` stops after verified upload.
`fly` performs the same boarding work and materializes the graph in the boarded
workspace for component-owned Nomad jobs.

The input graph uses the Kubernetes-style `spec` convention for desired input.
It does not define `status`. Observed state is reported through command
responses and component evidence.

## Desired Inputs

`FlyProcedure` is the entrypoint. It references the substrate. The base
procedure stays a small entrypoint into the graph.

`Substrate` contains the access hook and upload hooks. Guardian executes them
as argv commands after preparing the local upload bundle. The hooks own access,
transfer, extraction, permissions, and remote tooling.

`PublicOrigin` is the shared URL abstraction. Components that need an external
origin reference it as a stable shared fact.

Every service and infrastructure component owns its own CRD schema. That CRD
defines the component's static configuration surface: public config, provider
references, backup policy, and component-local invariants. The Guardian base
schema validates the envelope and shared resources, then leaves
component-specific fields to the component-owned schema.

Base-spec additions are gated by the scope test in [Spec Scope](spec-scope.md).

Site differences are expressed by selecting a different root resource graph:
different component CRDs, different origins, different provider authorities,
and different substrate hooks.

## Component Schemas

Every deployable service and infrastructure component owns a small schema for
the static inputs it needs. The schema lives with the component, for example:

```text
src/infrastructure-components/openbao/guardian/v1alpha1/schema.cue
src/infrastructure-components/haproxy/guardian/v1alpha1/schema.cue
src/services/object-storage-service/guardian/v1alpha1/schema.cue
```

The component schema is the place to define component-specific static
configuration. The root graph selects component resources from those schemas.
The base Guardian schema stays limited to the common resource envelope,
boarding entrypoint, substrate lifecycle hooks, shared origins, and stable
condition vocabulary.

Component schemas should model facts that can be checked before running the
operation: artifact paths, public names, socket paths, backup object names,
provider account identifiers, OpenBao paths, policy names, workload role names,
and references to shared resources. Component runtime behavior is represented by
the component's Nomad job and recovery binary.

Static configuration should be complete enough that a component can derive its
runtime plan without hidden Guardian fields. For example, OpenBao declares
mounts, policies, seal recipients, and snapshot locations in its component CRD;
the OpenBao recovery task decides whether the live state requires init, unseal,
restore, reconcile, or no-op.

## Site Files

A site file is a selected resource graph for one deployment target. Names such
as `gamma`, `prod`, or `dev` are operator labels for different graph files.
They sit outside the base protocol's resource kind set.

Site files should contain:

- one `FlyProcedure`;
- one referenced `Substrate`;
- shared `PublicOrigin` resources;
- component CRDs for the components expected to converge on that target;
- nonsecret provider identifiers, object names, account IDs, policy names, and
  OpenBao paths needed by those component CRDs.

Site files contain static desired inputs. Runtime component actions, Nomad
submission order, component-specific environment projection, host-local secret
paths, status fields, report resources, and other runtime evidence are handled
outside the base Guardian graph.

Runtime component behavior belongs in the component's Nomad job and recovery
binary. Runtime observations belong in command responses, Nomad state,
component reports, health endpoints, and telemetry.

## Command Results

Command results contain observed outcomes: readiness, upload digest, observed
digest, hook status, condition types, and reason codes. Command results are
ephemeral CLI responses and are not config resources.

`RootTrustMaterialAvailable` is the standard condition for recovery blocked on
external root authority. Component implementations choose precise reasons such
as `UnsealQuorumIncomplete`, `ExternalSealUnavailable`, or
`ProviderRootCredentialRequired`.

## Versioning

Released document schemas are immutable contracts. A released schema may add
optional fields with compatible semantics. Required fields, renamed fields,
removed fields, changed defaults, or changed meanings require a new schema
version.

Protocol releases package schemas, examples, reference CLI behavior, and
conformance expectations.
