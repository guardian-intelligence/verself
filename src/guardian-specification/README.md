# Guardian Specification

Guardian Specification defines a configuration protocol for converging a built
repo on a substrate. The reference CLI reads one resource graph, boards the
target by moving verified repo artifacts into place, and writes the resolved
resource graph into the boarded workspace for component-owned Nomad jobs.

The command surface:

```sh
guardian board src/guardian-specification/examples/gamma/gamma.cue -o <yaml|json|toml|toon>
guardian fly src/guardian-specification/examples/gamma/gamma.cue --dry-run -o yaml
guardian fly src/guardian-specification/examples/gamma/gamma.cue
```

`board` is the first cut point in the convergence state machine. It loads the
resource graph, computes a content-addressed upload bundle from local source
and build artifacts, runs the referenced `Substrate` lifecycle hooks, verifies
the extracted repo tree, and stops.

`fly` starts with the same boarding phase. Component recovery is a Nomad
convention: owner job files include prestart recovery tasks and consume the
boarded resource graph. The base protocol does not encode component internals.
Infrastructure components own their recovery binaries, backup retrieval,
credential import, health checks, and stabilization logic.

## Documents

- [Overview](docs/overview.md)
- [Resource Model](docs/resource-model.md)
- [Boarding](docs/boarding.md)
- [Fly](docs/fly.md)
- [CLI](docs/cli.md)
- [Configuration and Credentials](docs/configuration-and-credentials.md)
- [Convergence Inventory](docs/convergence-inventory.md)
- [Root Trust Material](docs/root-trust-material.md)
- [OpenBao Recovery](docs/openbao-recovery.md)
- [Releases and Conformance](docs/releases-and-conformance.md)

## Normative Language

Normative specification documents use the terms `MUST`, `MUST NOT`, `SHOULD`,
`SHOULD NOT`, and `MAY` as defined by RFC 2119 and RFC 8174 when those terms
appear in uppercase.
