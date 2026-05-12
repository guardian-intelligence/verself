# Verself Smithy Package

This package is the first contract-authoring slice for Verself product APIs.
It intentionally stops before service handler generation, OpenAPI projection,
SDK generation, and Buf output.

The package owns:

- `common.smithy` — shared primitives, typed operation traits, RFC 9457 error
  mixins, stable problem shapes, and future protobuf field-number traits.
- `iam.smithy` — the initial IAM resource graph and public operations.
- `smithy-build.json` — the package-local Smithy CLI configuration.

Validation targets:

```shell
bazelisk build //src/contracts/models/verself:smithy_validate
bazelisk build //src/contracts/models/verself:smithy_build
```

`smithy_validate` gates the package with Smithy model validation.
`smithy_build` runs the configured Smithy projections and stores the projection
tree as `smithy-build.tar`; the initial `source` projection contains the
validated Smithy JSON model, copied source files, manifest, and build metadata.
