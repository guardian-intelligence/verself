# Verself Smithy IR Plugin

`verself-ir` is the Smithy-Build plugin that compiles a projected Smithy model
into the repository-internal Verself contract IR.

The plugin is intentionally the only place that interprets Smithy semantics for
downstream generators. Huma bindings, SDK transports, conformance fixtures,
IAM/Zanzibar catalogs, audit catalogs, observability catalogs, OpenAPI
projections, and future protobuf projections should consume generated IR rather
than re-reading Smithy traits independently.

## Build Shape

```text
.smithy files
  -> Smithy model assembly
  -> Smithy core validation
  -> Verself Smithy validators
  -> Smithy projection
  -> verself-ir Smithy-Build plugin
  -> deterministic JSON IR
```

The authored source remains Smithy IDL under `src/smithy/models`. The IR is a
generated build artifact and must not become a hand-authored contract language.

## Libraries

The plugin compiles against the repository-pinned Smithy CLI distribution,
currently `1.70.0`, via the same `@dev_tool_smithy_cli//file` input used by the
validators. The relevant APIs are:

- `software.amazon.smithy.build.SmithyBuildPlugin` for plugin registration.
- `software.amazon.smithy.build.PluginContext` for the projected `Model`,
  plugin settings, projection name, and output file manifest.
- `software.amazon.smithy.model.Model` and Smithy shape classes for semantic
  model access.
- `software.amazon.smithy.model.knowledge.*` indexes for operation/resource
  closure, HTTP bindings, and protocol-aware facts as the IR fills out.
- `software.amazon.smithy.model.node.Node` for deterministic JSON output.

## Iteration Loop

Use these targets while developing the plugin:

```shell
bazelisk build //src/smithy/plugins/ir:verself_smithy_ir_plugin
bazelisk build //src/smithy/models/verself:smithy_validate
bazelisk build //src/smithy/models/verself:smithy_build
bazelisk build //src/smithy/models/verself:iam_public_ir
tar -tf bazel-bin/src/smithy/models/verself/smithy-build.tar | rg 'ir/verself'
```

The current implementation emits a bootstrap IR document to prove the plugin
classpath, SPI registration, Smithy projection wiring, and deterministic output
path. The next iterations should replace the bootstrap fields with the full IR
schema described in `src/smithy/ir/README.md`.

## Engineering Rules

- Fail closed. Missing security or runtime metadata should fail IR generation.
- Keep IR redundant where redundancy lets validators catch drift.
- Use Smithy indexes and resolved model APIs rather than raw IDL or raw JSON
  AST traversal.
- Keep downstream generators simple. If a generator needs to know Smithy
  closure or trait rules, move that interpretation into this plugin.
- Emit stable ordering for every object and array that downstream tools diff.
- Include source locations once operation records are emitted so diagnostics can
  point back to Smithy IDL.
