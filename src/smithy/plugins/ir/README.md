# Verself Smithy IR Plugin

`verself-ir` is the Smithy-Build plugin that compiles a projected Smithy model
into the repository-internal Verself contract IR.

The plugin is intentionally the only place that interprets Smithy semantics for
downstream generators. Huma bindings, SDK transports, IAM/Zanzibar catalogs,
audit catalogs, observability catalogs, OpenAPI projections, and future
protobuf projections should consume generated IR rather than re-reading Smithy
traits independently.

## Build Shape

```text
.smithy files
  -> Smithy model assembly
  -> Smithy core validation
  -> Smithy projection
  -> verself-ir Smithy-Build plugin
  -> deterministic JSON IR
```

The authored source remains Smithy IDL under `src/smithy/models`. The IR is a
generated build artifact and must not become a hand-authored contract language.

## Compiler Phases

The Java plugin is split by compiler responsibility:

- `VerselfIrPlugin` owns Smithy-Build lifecycle integration only.
- `SmithyModelIndex` resolves the service, top-down operation/resource sets,
  and transitive shape closure from the projected `Model`.
- `VerselfIrModel` normalizes plugin settings into the semantic compilation
  unit.
- `VerselfIrJsonEmitter` serializes the semantic model into the stable IR JSON
  shape consumed by generators.
- `SmithyNodes`, `SmithyNames`, `SmithyDiagnostics`, and `VerselfTraitIds`
  centralize Smithy node access, naming, diagnostics, and trait identifiers.

## Libraries

The plugin compiles against repository-pinned Maven artifacts from the Smithy
`1.70.0` BOM through `rules_jvm_external`. The relevant APIs are:

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
aspect check
bazelisk test //src/smithy/plugins/ir:java_format_test
bazelisk test //src/smithy/models/verself:smithy_validate_test
bazelisk build //src/smithy/plugins/ir:verself_smithy_ir_plugin
bazelisk build //src/smithy/models/verself:smithy_build
bazelisk build //src/smithy/models/verself:iam_public_ir
bazelisk build //src/smithy/models/verself:iam_audit_catalog
bazelisk build //src/smithy/models/verself:iam_observability_catalog
bazelisk build //src/smithy/models/verself:iam_proto_projection
```

`smithy_validate` and `smithy_build` are repo-owned Bazel rules under
`//src/smithy/build`. They run Smithy through Bazel-managed Java tooling and
keep model sources, validator/plugin jars, generated projection trees, and
declared downstream outputs explicit in the action graph.

## Engineering Rules

- Fail closed. Missing security or runtime metadata should fail IR generation.
- Keep IR redundant where redundancy prevents downstream inference.
- Use Smithy indexes and resolved model APIs rather than raw IDL or raw JSON
  AST traversal.
- Keep downstream generators simple. If a generator needs to know Smithy
  closure or trait rules, move that interpretation into this plugin.
- Emit stable ordering for every object and array that downstream tools diff.
- Include source locations once operation records are emitted so diagnostics can
  point back to Smithy IDL.
