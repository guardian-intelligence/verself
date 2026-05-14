"""Repository-owned Smithy build rules."""

load("@rules_java//java/common:java_info.bzl", "JavaInfo")

def _smithy_source_inputs(ctx):
    return depset(
        direct = [ctx.file.config] + ctx.files.srcs + ctx.files.model_deps,
        transitive = [dep[DefaultInfo].files for dep in ctx.attr.plugins],
    )

def _single_artifact(dep, attr_name):
    files = dep[DefaultInfo].files.to_list()
    if len(files) != 1:
        fail("%s must provide exactly one artifact, got %d" % (attr_name, len(files)))
    return files[0]

def _smithy_validate_impl(ctx):
    build_dir = ctx.actions.declare_directory(ctx.attr.out)
    args = ctx.actions.args()
    args.add("validate")
    args.add("--no-color")
    args.add("--severity", "NOTE")
    args.add("--config", ctx.file.config)
    args.add("--output", build_dir.path)
    args.add_all(ctx.files.model_deps)
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
    args.add_all(ctx.files.model_deps)
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
    "model_deps": attr.label_list(allow_files = [".smithy"]),
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

def _smithy_jar_model_impl(ctx):
    jar = _single_artifact(ctx.attr.jar, "jar")
    args = ctx.actions.args()
    args.add("-jar", jar.path)
    args.add("-entry", ctx.attr.entry)
    args.add("-out", ctx.outputs.out.path)
    ctx.actions.run(
        executable = ctx.executable._extractor,
        inputs = [jar],
        outputs = [ctx.outputs.out],
        arguments = [args],
        mnemonic = "SmithyJarModel",
        progress_message = "Extracting Smithy model %{output}",
        tools = [ctx.executable._extractor],
    )
    return DefaultInfo(files = depset([ctx.outputs.out]))

smithy_jar_model = rule(
    implementation = _smithy_jar_model_impl,
    attrs = {
        "entry": attr.string(mandatory = True),
        "jar": attr.label(mandatory = True),
        "out": attr.output(mandatory = True),
        "_extractor": attr.label(
            cfg = "exec",
            default = Label("//src/smithy/cmd/smithy-jar-model"),
            executable = True,
        ),
    },
)
