"""Rule that links site-authored Nomad jobs and build artifacts into release JSON."""

def _nomad_release_impl(ctx):
    out = ctx.actions.declare_file(ctx.label.name + ".json")

    index_files = ctx.attr.index.files.to_list()
    if len(index_files) != 1:
        fail("%s must produce exactly one index file, got %d" % (ctx.attr.index.label, len(index_files)))
    index_file = index_files[0]

    artifact_files = []
    artifact_args = []
    for target, output in ctx.attr.artifacts.items():
        files = target.files.to_list()
        if len(files) != 1:
            fail("%s must produce exactly one artifact file, got %d" % (target.label, len(files)))
        artifact_files.append(files[0])
        artifact_args.append("%s=%s" % (output, files[0].path))

    embedded_template_files = []
    embedded_template_args = []
    for target, placeholder in ctx.attr.embedded_templates.items():
        files = target.files.to_list()
        if len(files) != 1:
            fail("%s must produce exactly one embedded template file, got %d" % (target.label, len(files)))
        embedded_template_files.append(files[0])
        embedded_template_args.append("%s=%s" % (placeholder, files[0].path))

    args = ctx.actions.args()
    args.add("--site", ctx.attr.site)
    args.add("--jobs-target", "//%s:%s" % (ctx.label.package, ctx.label.name))
    args.add("--jobs-index", index_file.path)
    args.add("--out", out.path)
    args.add_all(artifact_args, before_each = "--artifact")
    args.add_all(embedded_template_args, before_each = "--embedded-template")

    ctx.actions.run(
        executable = ctx.executable._linker,
        arguments = [args],
        inputs = depset(ctx.files.job_specs + artifact_files + embedded_template_files + [
            index_file,
            ctx.executable._linker,
        ]),
        outputs = [out],
        mnemonic = "NomadLinkRelease",
        progress_message = "Linking Nomad release %{label}",
    )

    return DefaultInfo(files = depset([out]))

nomad_release = rule(
    implementation = _nomad_release_impl,
    attrs = {
        "site": attr.string(
            mandatory = True,
            doc = "Deployment site label stamped into release.json.",
        ),
        "artifacts": attr.label_keyed_string_dict(
            allow_files = True,
            doc = "Map of Nomad artifact tar labels to artifact output names.",
        ),
        "index": attr.label(
            allow_single_file = True,
            mandatory = True,
            doc = "Authored jobs/index.json describing the Nomad-supervised component set.",
        ),
        "embedded_templates": attr.label_keyed_string_dict(
            allow_files = True,
            doc = "Map of generated template labels to placeholder strings replaced in authored Nomad specs before digest stamping.",
        ),
        "job_specs": attr.label_list(
            allow_files = True,
            mandatory = True,
            doc = "Authored *.nomad.json base specs referenced by the index.",
        ),
        "_linker": attr.label(
            default = Label("//src/tools/deployment/nomad:link_release.py"),
            cfg = "exec",
            executable = True,
            allow_single_file = True,
        ),
    },
)
