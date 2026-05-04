"""Rules that link component-owned Nomad jobs and build artifacts into releases."""

NomadComponentInfo = provider(
    doc = "Nomad deployment metadata owned by the deployable component package.",
    fields = {
        "artifacts": "label_keyed_string_dict of artifact targets to release output names.",
        "component": "Topology component key.",
        "depends_on": "List of Nomad job IDs that must be submitted before this component.",
        "embedded_templates": "label_keyed_string_dict of template files to exact Nomad spec placeholders.",
        "job_id": "Nomad Job.ID.",
        "job_spec": "Single authored Nomad job spec File.",
    },
)

def _single_file(target, what):
    files = target.files.to_list()
    if len(files) != 1:
        fail("%s must produce exactly one %s file, got %d" % (target.label, what, len(files)))
    return files[0]

def _repo_label(label):
    raw = str(label)
    if raw.startswith("@" + "@//"):
        return raw[2:]
    return raw

def _component_rows(components):
    rows = []
    artifact_files = []
    artifact_args = []
    artifact_labels_by_output = {}
    job_spec_files = []
    job_spec_args = []
    embedded_template_files = []
    embedded_template_args = []
    embedded_template_labels_by_placeholder = {}

    for target in components:
        if NomadComponentInfo not in target:
            fail("%s must provide NomadComponentInfo" % target.label)
        info = target[NomadComponentInfo]
        job_spec_files.append(info.job_spec)
        job_spec_args.append("%s=%s" % (info.job_id, info.job_spec.path))

        artifacts = []
        for artifact_target, output in info.artifacts.items():
            artifact_file = _single_file(artifact_target, "artifact")
            prior_artifact = artifact_labels_by_output.get(output)
            if prior_artifact == None:
                artifact_labels_by_output[output] = _repo_label(artifact_target.label)
                artifact_files.append(artifact_file)
                artifact_args.append("%s=%s" % (output, artifact_file.path))
            elif prior_artifact != _repo_label(artifact_target.label):
                fail("Nomad artifact output %s is produced by both %s and %s" % (output, prior_artifact, artifact_target.label))
            artifacts.append({
                "nomad_artifact_label": _repo_label(artifact_target.label),
                "output": output,
            })

        for template_target, placeholder in info.embedded_templates.items():
            template_file = _single_file(template_target, "embedded template")
            prior_template = embedded_template_labels_by_placeholder.get(placeholder)
            if prior_template == None:
                embedded_template_labels_by_placeholder[placeholder] = _repo_label(template_target.label)
                embedded_template_files.append(template_file)
                embedded_template_args.append("%s=%s" % (placeholder, template_file.path))
            elif prior_template != _repo_label(template_target.label):
                fail("Nomad embedded template placeholder %s is provided by both %s and %s" % (placeholder, prior_template, template_target.label))

        row = {
            "artifacts": artifacts,
            "component": info.component,
            "depends_on": list(info.depends_on),
            "job_id": info.job_id,
            "job_spec": info.job_spec.short_path,
        }
        rows.append(row)

    return struct(
        artifact_args = artifact_args,
        artifact_files = artifact_files,
        embedded_template_args = embedded_template_args,
        embedded_template_files = embedded_template_files,
        job_spec_args = job_spec_args,
        job_spec_files = job_spec_files,
        rows = rows,
    )

def _nomad_component_impl(ctx):
    job_spec = _single_file(ctx.attr.job_spec, "Nomad job spec")
    if not ctx.attr.component:
        fail("component is required")
    if not ctx.attr.job_id:
        fail("job_id is required")
    return [
        DefaultInfo(files = depset([job_spec])),
        NomadComponentInfo(
            artifacts = ctx.attr.artifacts,
            component = ctx.attr.component,
            depends_on = ctx.attr.depends_on,
            embedded_templates = ctx.attr.embedded_templates,
            job_id = ctx.attr.job_id,
            job_spec = job_spec,
        ),
    ]

nomad_component = rule(
    implementation = _nomad_component_impl,
    attrs = {
        "artifacts": attr.label_keyed_string_dict(
            allow_files = True,
            doc = "Map of component-owned artifact targets to release output names.",
        ),
        "component": attr.string(
            mandatory = True,
            doc = "Topology component key.",
        ),
        "depends_on": attr.string_list(
            doc = "Nomad job IDs that must be submitted before this component.",
        ),
        "embedded_templates": attr.label_keyed_string_dict(
            allow_files = True,
            doc = "Map of template file labels to exact placeholders in the Nomad job spec.",
        ),
        "job_id": attr.string(
            mandatory = True,
            doc = "Nomad Job.ID.",
        ),
        "job_spec": attr.label(
            allow_single_file = True,
            mandatory = True,
            doc = "Authored owner-local Nomad job spec.",
        ),
    },
)

def _nomad_component_index_impl(ctx):
    out = ctx.actions.declare_file(ctx.label.name + ".json")
    component_data = _component_rows(ctx.attr.components)
    ctx.actions.write(
        output = out,
        content = json.encode({
            "components": component_data.rows,
        }) + "\n",
    )
    return DefaultInfo(files = depset([out]))

nomad_component_index = rule(
    implementation = _nomad_component_index_impl,
    attrs = {
        "components": attr.label_list(
            mandatory = True,
            providers = [NomadComponentInfo],
            doc = "Component-owned Nomad registrations included in the site.",
        ),
    },
)

def _nomad_release_impl(ctx):
    out = ctx.actions.declare_file(ctx.label.name + ".json")
    component_index = ctx.actions.declare_file(ctx.label.name + "_components.json")
    component_data = _component_rows(ctx.attr.components)
    ctx.actions.write(
        output = component_index,
        content = json.encode({
            "components": component_data.rows,
        }) + "\n",
    )

    site_config_files = ctx.attr.site_config.files.to_list()
    if len(site_config_files) != 1:
        fail("%s must produce exactly one site config file, got %d" % (ctx.attr.site_config.label, len(site_config_files)))
    site_config = site_config_files[0]

    args = ctx.actions.args()
    args.add("--site", ctx.attr.site)
    args.add("--jobs-target", "//%s:%s" % (ctx.label.package, ctx.label.name))
    args.add("--site-config", site_config.path)
    args.add("--component-index", component_index.path)
    args.add("--out", out.path)
    args.add_all(component_data.artifact_args, before_each = "--artifact")
    args.add_all(component_data.job_spec_args, before_each = "--job-spec")
    args.add_all(component_data.embedded_template_args, before_each = "--embedded-template")

    ctx.actions.run(
        executable = ctx.executable._linker,
        arguments = [args],
        inputs = depset(component_data.job_spec_files + component_data.artifact_files + component_data.embedded_template_files + [
            component_index,
            site_config,
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
        "components": attr.label_list(
            mandatory = True,
            providers = [NomadComponentInfo],
            doc = "Component-owned Nomad registrations included in the site.",
        ),
        "site_config": attr.label(
            allow_single_file = True,
            mandatory = True,
            doc = "Site-local artifact delivery and Nomad release policy.",
        ),
        "_linker": attr.label(
            default = Label("//src/tools/deployment/nomad:link_release.py"),
            cfg = "exec",
            executable = True,
            allow_single_file = True,
        ),
    },
)
