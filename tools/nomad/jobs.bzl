def _nomad_resolved_jobs_impl(ctx):
    out_dir = ctx.actions.declare_directory(ctx.label.name)

    artifact_files = []
    artifact_args = []
    for target, output in ctx.attr.artifacts.items():
        files = target.files.to_list()
        if len(files) != 1:
            fail("%s must produce exactly one artifact file, got %d" % (target.label, len(files)))
        artifact_files.append(files[0])
        artifact_args.append("%s=%s" % (output, files[0].path))

    args = ctx.actions.args()
    args.add("--cue-renderer", ctx.executable.cue_renderer.path)
    args.add("--out-dir", out_dir.path)
    args.add("--topology-dir", ctx.attr.topology_dir)
    args.add("--instance", ctx.attr.instance)
    args.add_all(artifact_args, before_each = "--artifact")

    ctx.actions.run(
        executable = ctx.executable._resolver,
        arguments = [args],
        inputs = depset(ctx.files.topology_srcs + artifact_files),
        tools = [ctx.executable.cue_renderer],
        outputs = [out_dir],
        mnemonic = "NomadResolveJobs",
        progress_message = "Resolving Nomad job specs %{label}",
    )

    return DefaultInfo(files = depset([out_dir]))

nomad_resolved_jobs = rule(
    implementation = _nomad_resolved_jobs_impl,
    attrs = {
        "artifacts": attr.label_keyed_string_dict(
            allow_files = True,
            doc = "Map of Nomad artifact tar labels to artifact output names.",
        ),
        "cue_renderer": attr.label(
            cfg = "exec",
            executable = True,
            mandatory = True,
        ),
        "instance": attr.string(default = "prod"),
        "topology_dir": attr.string(default = "src/cue-renderer"),
        "topology_srcs": attr.label_list(
            allow_files = True,
            mandatory = True,
        ),
        "_resolver": attr.label(
            default = Label("//tools/nomad:resolve_jobs.py"),
            cfg = "exec",
            executable = True,
            allow_single_file = True,
        ),
    },
)
