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

Site files should avoid:

- component recovery steps;
- Nomad submission order;
- component-specific environment projection;
- host file paths for secret values;
- report resources or other runtime evidence.

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
