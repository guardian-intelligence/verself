# Guardian Specification

Guardian Specification defines a configuration protocol for converging a built
repo on a substrate. The reference CLI resolves a profile, materializes a
verified repo tree on the target, prepares OpenBao host integration inputs,
starts the Nomad executor, and writes the resolved resource graph into the
materialized workspace for component-owned Nomad jobs.

The command surface:

```sh
guardian run bazel -- test //src/guardian-specification/...
guardian preflight gamma -o <yaml|json|toml|toon>
guardian fly gamma --dry-run -o yaml
guardian fly gamma
guardian fly run gamma -- nomad status
```

`preflight` is the first cut point in the convergence state machine. It
resolves the profile, loads the resource graph, verifies local build artifacts
are present, runs the referenced `Substrate` lifecycle hooks, verifies the
remote repo tree, prepares OpenBao integration material for Nomad, starts the
Nomad executor, and stops.

`fly` starts with the same preflight phase and then runs the configured Nomad
job hook. `fly run` converges first, then invokes a verified catalog tool
through the remote Guardian binary. Component runtime behavior is a Nomad
convention: owner job files include lifecycle tasks and consume the
materialized resource graph. The base protocol does not encode component
internals. Infrastructure components own their binaries, backup retrieval,
credential import, health checks, and stabilization logic.

## Scope Convention

The base protocol stays limited to graph loading, preflight, upload
verification, fixed Nomad executor bootstrap hooks, shared public-origin facts,
stable command responses, and cross-component conditions. Component
configuration lives in component CRDs beside the owning service or
infrastructure component. Component behavior lives in the component Nomad job
and owner-local binaries.

Static component configuration goes in the owning component schema under
`guardian/v1alpha1/schema.cue`. The root graph instantiates those schemas.
Actions such as restore, initialize, unseal, migrate, import, publish, and
health waiting are Nomad lifecycle conventions implemented by the component.
The base Guardian schema owns no component operation fields.

Treat [Spec Scope](docs/spec-scope.md) as the review checklist for every new
field. A static component fact belongs in the component CRD. An operation
belongs in a component Nomad lifecycle task or owner-local binary. Observed
state belongs in a command response, component report, health endpoint, Nomad
state, or telemetry.

Site files such as `examples/gamma/gamma.cue` select a set of resources and
nonsecret configuration for one target. Site names are operator labels outside
the base Guardian resource kind set.

## Documents

- [Overview](docs/overview.md)
- [Spec Scope](docs/spec-scope.md)
- [Resource Model](docs/resource-model.md)
- [Preflight](docs/preflight.md)
- [Fly](docs/fly.md)
- [CLI](docs/cli.md)
- [Command Observability](docs/command-observability.md)
- [Configuration and Credentials](docs/configuration-and-credentials.md)
- [Convergence Inventory](docs/convergence-inventory.md)
- [OpenBao Recovery](docs/openbao-recovery.md)
- [Releases and Conformance](docs/releases-and-conformance.md)

## Normative Language

Normative specification documents use the terms `MUST`, `MUST NOT`, `SHOULD`,
`SHOULD NOT`, and `MAY` as defined by RFC 2119 and RFC 8174 when those terms
appear in uppercase.
