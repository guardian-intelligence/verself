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

    override_files = []
    override_args = []
    for target, job_id in ctx.attr.overrides.items():
        files = target.files.to_list()
        if len(files) != 1:
            fail("%s must produce exactly one override file, got %d" % (target.label, len(files)))
        override_files.append(files[0])
        override_args.append("%s=%s" % (job_id, files[0].path))

    args = ctx.actions.args()
    args.add("--cue-renderer", ctx.executable.cue_renderer.path)
    args.add("--out-dir", out_dir.path)
    args.add("--topology-dir", ctx.attr.topology_dir)
    args.add("--instance", ctx.attr.instance)
    args.add_all(artifact_args, before_each = "--artifact")
    args.add_all(override_args, before_each = "--override")

    ctx.actions.run(
        executable = ctx.executable._resolver,
        arguments = [args],
        inputs = depset(ctx.files.topology_srcs + artifact_files + override_files + [
            ctx.executable.cue_renderer,
            ctx.executable._resolver,
        ]),
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
        "overrides": attr.label_keyed_string_dict(
            allow_files = True,
            doc = "Map of per-component nomad-overrides.json file labels to Nomad job_id. The resolver merges each file's checks/update blocks into the rendered spec before stamping spec_sha256 so the digest reflects the final state.",
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
            default = Label("//src/cue-renderer/nomad:resolve_jobs.py"),
            cfg = "exec",
            executable = True,
            allow_single_file = True,
        ),
    },
)
