# Contracts

`src/contracts` is the canonical contract source for Verself product APIs.
Smithy models define service operations, resource DTOs, validation constraints,
auth expectations, IAM policy metadata, audit metadata, SDK behavior, and
conformance cases. OpenAPI is generated from the Smithy model for documentation
and ecosystem tooling.

The target layering is:

```text
Smithy model in src/contracts/models
  -> service handler bindings and DTO validation
  -> generated public SDK transport cores
  -> generated service-to-service clients
  -> generated OpenAPI projections for docs/import tooling
  -> generated conformance fixtures for SDKs
```

Connect/protobuf remains available for internal RPC, streaming, and binary
surfaces where HTTP resource semantics are a poor fit. Those contracts live
under `proto/` and do not replace the Smithy model for customer-facing
control-plane APIs.

## Directory Map

- `models/` — Smithy source of truth.
- `conformance/` — generated or hand-authored fixtures that every SDK must pass.
- `openapi/` — generated compatibility projections.
- `proto/` — Connect/protobuf contracts for RPC-shaped internal surfaces.

## Cutover Rule

New product API work starts in Smithy. Current Huma/OpenAPI services can keep
their existing emitted specs during migration, but the settled contract should
move here before SDKs, docs, CLI commands, or browser server functions depend on
the shape.

## Current Tooling

The first package-local contract target is `src/contracts/models/verself`.
It pins the Smithy CLI through the dev-tools catalog and validates the model
before any service or SDK integration is generated.

```shell
bazelisk build //src/contracts/models/verself:smithy_validate
bazelisk build //src/contracts/models/verself:smithy_build
```

`smithy_validate` runs Smithy core validation. Repository-specific invariants
should move into Smithy validators or generated protocol/conformance suites
rather than package-local AST tests. `smithy_build` runs the configured Smithy
projections and archives their artifacts for downstream generators.

The active repository validator is `Verself`, implemented in
`//src/contracts/validators:verself_smithy_validators`. It is loaded onto the
Smithy CLI classpath by the Bazel Smithy targets and activated through
`metadata validators` in the model.
