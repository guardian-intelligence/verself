# Smithy Contracts

`src/smithy` owns the canonical contract source for Verself product APIs.
Smithy models define service operations, resource DTOs, validation constraints,
auth expectations, IAM policy metadata, audit metadata, SDK behavior, and HTTP
bindings. OpenAPI is generated from the Smithy model for documentation,
ecosystem tooling, and TypeScript transport generation.

The target layering is:

```text
Smithy model in src/smithy/models
  -> generated route catalog read model for runtimes and conformance
  -> official Smithy OpenAPI projection
  -> hand-written Huma service routes that conform to the projection
  -> OpenAPI-based TypeScript transport clients
  -> curated SDK adapters
```

Connect/protobuf remains available for internal RPC, streaming, and binary
surfaces where HTTP resource semantics are a poor fit. Those contracts live
under `proto/` and do not replace the Smithy model for customer-facing
control-plane APIs.

## Directory Map

- `models/` - Smithy source of truth.
- `build/` - Bazel rules for Smithy validation and projection artifacts.
- `cmd/smithy-artifact/` - small extractor used to expose files from a
  `smithy build` output tree as Bazel outputs.
- `openapi/` - generated compatibility projections when materialized.
- `plugins/validators/` - narrow Smithy validators for Verself operation
  metadata and binding invariants, plus the route catalog generator.
- `proto/` - Connect/protobuf contracts for RPC-shaped internal surfaces.

## Contract Rule

New product API work starts in Smithy. Service code owns route implementation
and boundary conversion, while Smithy owns the wire contract, operation
metadata, and generated OpenAPI projection consumed by tooling.

## Current Tooling

The package-local contract target is `src/smithy/models/verself`. It validates
the model and emits official Smithy OpenAPI projection artifacts plus the
generated route catalog under the `source/verselfRouteCatalog/route-catalog`
projection path.

```shell
aspect check
bazelisk test //src/smithy/models/verself:smithy_validate_test
bazelisk build //src/smithy/models/verself:smithy_build
```

Service OpenAPI packages expose `openapi-3.1.yaml` targets extracted from the
Smithy build output. The files contain the official OpenAPI 3.1 projection for
the service.

Service runtimes consume the route catalog as a compact generated read model.
It carries operation IDs, HTTP bindings, auth mode, authorization policy, audit
event metadata, rate-limit bucket, request body budget, idempotency policy, SDK
hints, and stable problem types. OpenAPI remains the public HTTP contract and
does not carry Verself governance or IAM metadata.
