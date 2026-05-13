"""Rules for generating artifacts from Verself contract IR."""

load("@bazel_lib//lib:copy_to_bin.bzl", "copy_to_bin")

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

def _ir_go_impl(ctx, mode, mnemonic, progress, package = "", catalog = "", binding = "", openapi_format = "", server_url = ""):
    args = ctx.actions.args()
    args.add("-ir", ctx.file.ir)
    args.add("-mode", mode)
    if package:
        args.add("-package", package)
    if binding:
        args.add("-binding", binding)
    if catalog:
        args.add("-catalog", catalog)
    if openapi_format:
        args.add("-openapi-format", openapi_format)
    if server_url:
        args.add("-server-url", server_url)
    args.add("-out", ctx.outputs.out)
    ctx.actions.run(
        executable = ctx.executable.tool,
        inputs = [ctx.file.ir],
        outputs = [ctx.outputs.out],
        arguments = [args],
        mnemonic = mnemonic,
        progress_message = progress,
    )

def _ir_contract_go_impl(ctx):
    _ir_go_impl(ctx, "contract", "VerselfIrContractGo", "Generating Go contract types %{output}", package = ctx.attr.package, binding = ctx.attr.binding)

ir_contract_go = rule(
    implementation = _ir_contract_go_impl,
    attrs = {
        "ir": attr.label(allow_single_file = True, mandatory = True),
        "out": attr.output(mandatory = True),
        "binding": attr.string(default = "Public"),
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

def _ir_openapi_impl(ctx):
    _ir_go_impl(
        ctx,
        "openapi",
        "VerselfIrOpenAPI",
        "Generating OpenAPI projection %{output}",
        openapi_format = ctx.attr.format,
        server_url = ctx.attr.server_url,
    )

ir_openapi = rule(
    implementation = _ir_openapi_impl,
    attrs = {
        "format": attr.string(mandatory = True, values = ["3.0", "3.1", "3.2"]),
        "ir": attr.label(allow_single_file = True, mandatory = True),
        "out": attr.output(mandatory = True),
        "server_url": attr.string(default = ""),
        "tool": attr.label(
            default = "//src/smithy/cmd/ir-go",
            cfg = "exec",
            executable = True,
        ),
    },
)

def verself_ir_openapi_specs(name, public_ir = None, internal_ir = None, public_server_url = "", internal_server_url = ""):
    """Declare OpenAPI projections from Verself contract IR.

    Args:
      name: Logical macro instance name. The generated target names remain the
        stable service OpenAPI compatibility names.
      public_ir: Public Verself contract IR label.
      internal_ir: Internal Verself contract IR label.
      public_server_url: Public OpenAPI server URL.
      internal_server_url: Internal OpenAPI server URL.
    """

    # Existing service OpenAPI packages expose these stable target names.
    # buildifier: disable=native-package
    native.package(default_visibility = ["//visibility:public"])
    if public_ir:
        ir_openapi(
            name = "spec_3_0",
            out = "openapi-3.0.yaml",
            format = "3.0",
            ir = public_ir,
            server_url = public_server_url,
        )
        ir_openapi(
            name = "spec_3_1",
            out = "openapi-3.1.yaml",
            format = "3.1",
            ir = public_ir,
            server_url = public_server_url,
        )
        ir_openapi(
            name = "spec_3_2",
            out = "openapi-3.2.yaml",
            format = "3.2",
            ir = public_ir,
            server_url = public_server_url,
        )
        copy_to_bin(
            name = "openapi-3.1.yaml.bin",
            srcs = [":openapi-3.1.yaml"],
        )
        copy_to_bin(
            name = "openapi-3.2.yaml.bin",
            srcs = [":openapi-3.2.yaml"],
        )
    if internal_ir:
        ir_openapi(
            name = "internal_spec_3_0",
            out = "internal-openapi-3.0.yaml",
            format = "3.0",
            ir = internal_ir,
            server_url = internal_server_url,
        )
        ir_openapi(
            name = "internal_spec_3_1",
            out = "internal-openapi-3.1.yaml",
            format = "3.1",
            ir = internal_ir,
            server_url = internal_server_url,
        )
        ir_openapi(
            name = "internal_spec_3_2",
            out = "internal-openapi-3.2.yaml",
            format = "3.2",
            ir = internal_ir,
            server_url = internal_server_url,
        )
        copy_to_bin(
            name = "internal-openapi-3.2.yaml.bin",
            srcs = [":internal-openapi-3.2.yaml"],
        )
