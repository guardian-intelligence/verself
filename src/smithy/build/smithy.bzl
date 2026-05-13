"""Repository-owned Smithy build rules."""

load("@rules_java//java/common:java_info.bzl", "JavaInfo")

def _smithy_source_inputs(ctx):
    return depset(
        direct = [ctx.file.config] + ctx.files.srcs,
        transitive = [dep[DefaultInfo].files for dep in ctx.attr.plugins],
    )

def _smithy_validate_impl(ctx):
    build_dir = ctx.actions.declare_directory(ctx.attr.out)
    args = ctx.actions.args()
    args.add("validate")
    args.add("--no-color")
    args.add("--severity", "NOTE")
    args.add("--config", ctx.file.config)
    args.add("--output", build_dir.path)
    ctx.actions.run(
        executable = ctx.executable._smithy_cli,
        arguments = [args],
        inputs = _smithy_source_inputs(ctx),
        mnemonic = "SmithyValidate",
        outputs = [build_dir],
        progress_message = "Validating Smithy model %{label}",
        tools = [ctx.executable._smithy_cli],
    )
    return DefaultInfo(files = depset([build_dir]))

def _smithy_build_impl(ctx):
    build_dir = ctx.actions.declare_directory(ctx.attr.out)
    args = ctx.actions.args()
    args.add("build")
    args.add("--no-color")
    args.add("--severity", "NOTE")
    args.add("--config", ctx.file.config)
    args.add("--output", build_dir.path)
    ctx.actions.run(
        executable = ctx.executable._smithy_cli,
        arguments = [args],
        inputs = _smithy_source_inputs(ctx),
        mnemonic = "SmithyBuild",
        outputs = [build_dir],
        progress_message = "Building Smithy projection %{label}",
        tools = [ctx.executable._smithy_cli],
    )
    return DefaultInfo(files = depset([build_dir]))

_COMMON_ATTRS = {
    "config": attr.label(allow_single_file = True, mandatory = True),
    "out": attr.string(mandatory = True),
    "plugins": attr.label_list(providers = [JavaInfo]),
    "srcs": attr.label_list(allow_files = [".smithy"], mandatory = True),
    "_smithy_cli": attr.label(
        cfg = "exec",
        default = Label("//src/smithy/tools:smithy_cli"),
        executable = True,
    ),
}

smithy_validate = rule(
    implementation = _smithy_validate_impl,
    attrs = _COMMON_ATTRS,
)

smithy_build = rule(
    implementation = _smithy_build_impl,
    attrs = _COMMON_ATTRS,
)
