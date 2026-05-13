# Smithy Contracts

`src/smithy` is the canonical contract source for Verself product APIs.
Smithy models define service operations, resource DTOs, validation constraints,
auth expectations, IAM policy metadata, audit metadata, SDK behavior, and
OpenAPI is generated from the Smithy model for documentation and ecosystem
tooling.

The target layering is:

```text
Smithy model in src/smithy/models
  -> generated Verself contract IR
  -> service handler bindings and DTO validation
  -> generated public SDK transport cores
  -> generated service-to-service clients
  -> generated OpenAPI projections for docs/import tooling
```

Connect/protobuf remains available for internal RPC, streaming, and binary
surfaces where HTTP resource semantics are a poor fit. Those contracts live
under `proto/` and do not replace the Smithy model for customer-facing
control-plane APIs.

## Directory Map

- `models/` - Smithy source of truth.
- `plugins/` - Smithy-Build plugins that compile generated artifacts from the
  projected semantic Smithy model.
- `ir/` - generated normalization boundary consumed by Huma, SDK, OpenAPI,
  IAM, audit, and observability generators.
- `internal/contract/` - Go domain model for tools that consume generated IR.
- `openapi/` - generated compatibility projections.
- `proto/` - Connect/protobuf contracts for RPC-shaped internal surfaces.

## Cutover Rule

New product API work starts in Smithy. Current Huma/OpenAPI services can keep
their existing emitted specs during migration, but the settled contract should
move here before SDKs, docs, CLI commands, or browser server functions depend on
the shape.

## Current Tooling

The first package-local contract target is `src/smithy/models/verself`.
It pins Smithy through Maven coordinates resolved by `rules_jvm_external`,
validates the model, and emits IR-derived IAM artifacts before service or SDK
integration is generated.

```shell
aspect check --kind=java
bazelisk build //src/smithy/models/verself:smithy_validate
bazelisk build //src/smithy/models/verself:smithy_build
```

`smithy_validate` runs Smithy core validation. `smithy_build` runs the
configured Smithy projections through repo-owned Bazel rules and exposes the
projection output tree for downstream generators. The Verself contract IR is the
first downstream artifact that generators should consume.
