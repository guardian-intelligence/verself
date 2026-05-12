"""Repository-owned Smithy build rules."""

load("@rules_java//java/common:java_info.bzl", "JavaInfo")

def _smithy_source_inputs(ctx):
    return depset(
        direct = [ctx.file.config] + ctx.files.srcs,
        transitive = [dep[DefaultInfo].files for dep in ctx.attr.plugins],
    )

def _smithy_validate_impl(ctx):
    marker = ctx.actions.declare_file(ctx.attr.out)
    build_dir = ctx.actions.declare_directory(ctx.label.name + ".build")
    args = ctx.actions.args()
    args.add(ctx.executable._smithy_cli)
    args.add(ctx.file.config)
    args.add(build_dir.path)
    args.add(marker)
    ctx.actions.run_shell(
        command = """
set -euo pipefail
"$1" validate --no-color --severity NOTE --config "$2" --output "$3"
touch "$4"
""",
        arguments = [args],
        inputs = _smithy_source_inputs(ctx),
        mnemonic = "SmithyValidate",
        outputs = [build_dir, marker],
        progress_message = "Validating Smithy model %{label}",
        tools = [ctx.executable._smithy_cli],
    )
    return DefaultInfo(files = depset([marker]))

def _smithy_build_impl(ctx):
    tar = ctx.actions.declare_file(ctx.attr.out)
    build_dir = ctx.actions.declare_directory(ctx.label.name + ".build")
    args = ctx.actions.args()
    args.add(ctx.executable._smithy_cli)
    args.add(ctx.file.config)
    args.add(build_dir.path)
    args.add(tar)
    ctx.actions.run_shell(
        command = """
set -euo pipefail
"$1" build --no-color --severity NOTE --config "$2" --output "$3"
tar --sort=name --owner=0 --group=0 --numeric-owner --mtime='UTC 2000-01-01' -cf "$4" -C "$3" .
""",
        arguments = [args],
        inputs = _smithy_source_inputs(ctx),
        mnemonic = "SmithyBuild",
        outputs = [build_dir, tar],
        progress_message = "Building Smithy projection %{label}",
        tools = [ctx.executable._smithy_cli],
    )
    return DefaultInfo(files = depset([tar]))

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
