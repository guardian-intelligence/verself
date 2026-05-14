"""Rules for exposing official Smithy OpenAPI projection artifacts."""

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
    if ctx.attr.yaml:
        args.add("-yaml")
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
        "yaml": attr.bool(default = False),
    },
)

def _openapi_spec(name, projection, service, out):
    smithy_projection_file(
        name = name,
        out = out,
        build = "//src/smithy/models/verself:smithy_build",
        path = "%s/openapi/%s.openapi.json" % (projection, service),
        yaml = True,
    )

def verself_openapi_specs(name, public_projection = None, public_service = None, internal_projection = None, internal_service = None):
    """Expose Smithy OpenAPI 3.1 projections under each service package.

    Args:
      name: compatibility name for BUILD packages that call this macro.
      public_projection: Smithy projection name for the public service.
      public_service: OpenAPI artifact service name for the public projection.
      internal_projection: Smithy projection name for the internal service.
      internal_service: OpenAPI artifact service name for the internal projection.
    """

    # Existing service packages expose these stable target names.
    # buildifier: disable=native-package
    native.package(default_visibility = ["//visibility:public"])

    if public_projection:
        if not public_service:
            fail("public_service is required when public_projection is set")
        _openapi_spec(
            name = "spec_3_1",
            out = "openapi-3.1.yaml",
            projection = public_projection,
            service = public_service,
        )
        copy_to_bin(
            name = "openapi-3.1.yaml.bin",
            srcs = [":openapi-3.1.yaml"],
        )

    if internal_projection:
        if not internal_service:
            fail("internal_service is required when internal_projection is set")
        _openapi_spec(
            name = "internal_spec_3_1",
            out = "internal-openapi-3.1.yaml",
            projection = internal_projection,
            service = internal_service,
        )
        copy_to_bin(
            name = "internal-openapi-3.1.yaml.bin",
            srcs = [":internal-openapi-3.1.yaml"],
        )
