"""Rules for generating Go/server artifacts from Verself contract IR."""

def _single_artifact(dep, attr_name):
    files = dep[DefaultInfo].files.to_list()
    if len(files) != 1:
        fail("%s must provide exactly one artifact, got %d" % (attr_name, len(files)))
    return files[0]

def _smithy_projection_file_impl(ctx):
    root = _single_artifact(ctx.attr.build, "build")
    args = ctx.actions.args()
    args.add("-root", root.path)
    args.add("-path", ctx.attr.path)
    args.add("-out", ctx.outputs.out)
    ctx.actions.run(
        executable = ctx.executable.tool,
        inputs = [root],
        outputs = [ctx.outputs.out],
        arguments = [args],
        mnemonic = "SmithyProjectionFile",
        progress_message = "Extracting Smithy projection artifact %{output}",
    )

smithy_projection_file = rule(
    implementation = _smithy_projection_file_impl,
    attrs = {
        "build": attr.label(mandatory = True),
        "out": attr.output(mandatory = True),
        "path": attr.string(mandatory = True),
        "tool": attr.label(
            default = "//src/smithy/cmd/smithy-artifact",
            cfg = "exec",
            executable = True,
        ),
    },
)

def _ir_go_impl(ctx, mode, mnemonic, progress, package = "", catalog = ""):
    args = ctx.actions.args()
    args.add("-ir", ctx.file.ir)
    args.add("-mode", mode)
    if package:
        args.add("-package", package)
    if catalog:
        args.add("-catalog", catalog)
    args.add("-out", ctx.outputs.out)
    ctx.actions.run(
        executable = ctx.executable.tool,
        inputs = [ctx.file.ir],
        outputs = [ctx.outputs.out],
        arguments = [args],
        mnemonic = mnemonic,
        progress_message = progress,
    )

def _ir_huma_go_impl(ctx):
    _ir_go_impl(ctx, "huma", "VerselfIrHumaGo", "Generating Huma bindings %{output}", package = ctx.attr.package)

ir_huma_go = rule(
    implementation = _ir_huma_go_impl,
    attrs = {
        "ir": attr.label(allow_single_file = True, mandatory = True),
        "out": attr.output(mandatory = True),
        "package": attr.string(mandatory = True),
        "tool": attr.label(
            default = "//src/smithy/cmd/ir-go",
            cfg = "exec",
            executable = True,
        ),
    },
)

def _ir_identity_catalog_go_impl(ctx):
    _ir_go_impl(ctx, "identity-catalog", "VerselfIrIdentityCatalogGo", "Generating IAM identity catalog %{output}", package = ctx.attr.package)

ir_identity_catalog_go = rule(
    implementation = _ir_identity_catalog_go_impl,
    attrs = {
        "ir": attr.label(allow_single_file = True, mandatory = True),
        "out": attr.output(mandatory = True),
        "package": attr.string(mandatory = True),
        "tool": attr.label(
            default = "//src/smithy/cmd/ir-go",
            cfg = "exec",
            executable = True,
        ),
    },
)

def _ir_catalog_json_impl(ctx):
    _ir_go_impl(ctx, "catalog-json", "VerselfIrCatalogJson", "Generating IR catalog %{output}", catalog = ctx.attr.catalog)

ir_catalog_json = rule(
    implementation = _ir_catalog_json_impl,
    attrs = {
        "catalog": attr.string(mandatory = True, values = ["iam", "audit", "observability"]),
        "ir": attr.label(allow_single_file = True, mandatory = True),
        "out": attr.output(mandatory = True),
        "tool": attr.label(
            default = "//src/smithy/cmd/ir-go",
            cfg = "exec",
            executable = True,
        ),
    },
)

def _ir_proto_projection_impl(ctx):
    _ir_go_impl(ctx, "proto", "VerselfIrProtoProjection", "Generating protobuf projection %{output}")

ir_proto_projection = rule(
    implementation = _ir_proto_projection_impl,
    attrs = {
        "ir": attr.label(allow_single_file = True, mandatory = True),
        "out": attr.output(mandatory = True),
        "tool": attr.label(
            default = "//src/smithy/cmd/ir-go",
            cfg = "exec",
            executable = True,
        ),
    },
)
