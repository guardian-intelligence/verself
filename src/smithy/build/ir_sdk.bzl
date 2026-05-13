"""Rules for generating SDK transport sources from Verself contract IR."""

def _ir_sdk_go_impl(ctx):
    args = ctx.actions.args()
    args.add("-ir", ctx.file.ir)
    args.add("-package", ctx.attr.package)
    args.add("-out", ctx.outputs.out)
    ctx.actions.run(
        executable = ctx.executable.tool,
        inputs = [ctx.file.ir],
        outputs = [ctx.outputs.out],
        arguments = [args],
        mnemonic = "VerselfIrSdkGo",
        progress_message = "Generating Go SDK transport %{output}",
    )

ir_sdk_go = rule(
    implementation = _ir_sdk_go_impl,
    attrs = {
        "ir": attr.label(allow_single_file = True, mandatory = True),
        "out": attr.output(mandatory = True),
        "package": attr.string(mandatory = True),
        "tool": attr.label(
            default = "//src/smithy/cmd/ir-sdk-go",
            cfg = "exec",
            executable = True,
        ),
    },
)

def _ir_sdk_ts_impl(ctx):
    args = ctx.actions.args()
    args.add("-ir", ctx.file.ir)
    args.add("-out", ctx.outputs.out)
    ctx.actions.run(
        executable = ctx.executable.tool,
        inputs = [ctx.file.ir],
        outputs = [ctx.outputs.out],
        arguments = [args],
        mnemonic = "VerselfIrSdkTs",
        progress_message = "Generating TypeScript SDK transport %{output}",
    )

ir_sdk_ts = rule(
    implementation = _ir_sdk_ts_impl,
    attrs = {
        "ir": attr.label(allow_single_file = True, mandatory = True),
        "out": attr.output(mandatory = True),
        "tool": attr.label(
            default = "//src/smithy/cmd/ir-sdk-ts",
            cfg = "exec",
            executable = True,
        ),
    },
)
