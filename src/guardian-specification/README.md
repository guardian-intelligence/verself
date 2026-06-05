# Guardian Specification

Guardian Specification defines a small configuration protocol for taking a
built repo, reaching a substrate, seeding deterministic bytes, and handing
Nomad the recovery/deployment jobs.

The command surface is intentionally small:

```sh
guardian board src/guardian-specification/examples/gamma/gamma.cue -o yaml
guardian fly src/guardian-specification/examples/gamma/gamma.cue --dry-run -o yaml
guardian fly src/guardian-specification/examples/gamma/gamma.cue
```

`board` loads a config document, checks SSH access configuration, computes a
content-addressed seed from local build artifacts and static config, and emits
a structured command result.

`fly` loads the same config document, verifies boarding readiness, wraps the
declared Nomad jobs, and plans or submits them.

## Documents

- [Overview](docs/overview.md)
- [Resource Model](docs/resource-model.md)
- [Boarding](docs/boarding.md)
- [Fly](docs/fly.md)
- [CLI](docs/cli.md)
- [Configuration and Credentials](docs/configuration-and-credentials.md)
- [Releases and Conformance](docs/releases-and-conformance.md)

## Normative Language

Normative specification documents use the terms `MUST`, `MUST NOT`, `SHOULD`,
`SHOULD NOT`, and `MAY` as defined by RFC 2119 and RFC 8174 when those terms
appear in uppercase.
