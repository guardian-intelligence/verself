# Verself Smithy Package

This package is the contract-authoring slice for Verself product APIs. Smithy
source is validated and projected into the generated Verself Contract IR; IAM's
public vertical also emits the first IR-derived catalogs and protobuf projection
artifact.

The package owns:

- `common.smithy` — shared primitives, typed operation traits, RFC 9457 error
  mixins, stable problem shapes, and future protobuf field-number traits.
- `iam.smithy` — the initial IAM resource graph and public operations.
- `smithy-build.json` — the package-local Smithy CLI configuration.

Validation targets:

```shell
bazelisk build //src/smithy/models/verself:smithy_validate
bazelisk build //src/smithy/models/verself:smithy_build
bazelisk build //src/smithy/models/verself:iam_public_ir
```

`smithy_validate` gates the package with Smithy model validation.
`smithy_build` runs the configured Smithy projections and stores the projection
tree as `smithy-build.tar`. Downstream service and catalog generators consume
`iam_public_ir`; they do not reinterpret Smithy build JSON directly.
