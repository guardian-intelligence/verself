"""Rules for component-owned Nomad deployment descriptors."""

NomadComponentInfo = provider(
    doc = "Nomad deployment metadata owned by the deployable component package.",
    fields = {
        "artifacts": "label_keyed_string_dict of artifact targets to release output names.",
        "component": "Topology component key.",
        "depends_on": "List of Nomad job IDs that must be submitted before this component.",
        "descriptor": "Component descriptor JSON file.",
        "job_id": "Nomad Job.ID.",
        "job_spec": "Single authored Nomad job spec File.",
        "provides": "Logical resources this component provides.",
        "requires": "Logical resources this component requires.",
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

def _write_descriptor(ctx, out, content, inputs):
    ctx.actions.run_shell(
        inputs = inputs,
        outputs = [out],
        arguments = [out.path],
        command = "cat > \"$1\" <<'EOF'\n" + content + "EOF\n",
        mnemonic = "NomadComponentDescriptor",
        progress_message = "Writing Nomad component descriptor %{label}",
    )

def _nomad_component_impl(ctx):
    job_spec = _single_file(ctx.attr.job_spec, "Nomad job spec")
    if not ctx.attr.component:
        fail("component is required")
    if not ctx.attr.job_id:
        fail("job_id is required")

    inputs = [job_spec]
    artifacts = []
    artifact_outputs = {}
    for artifact_target, output in ctx.attr.artifacts.items():
        if output in artifact_outputs:
            fail("duplicate Nomad artifact output %s in %s" % (output, ctx.label))
        artifact_outputs[output] = True
        artifact_file = _single_file(artifact_target, "artifact")
        inputs.append(artifact_file)
        artifacts.append({
            "label": _repo_label(artifact_target.label),
            "output": output,
            "path": artifact_file.path,
        })

    requires = list(ctx.attr.requires)
    for dep in ctx.attr.depends_on:
        resource = dep
        if not dep.startswith("nomad:job:"):
            resource = "nomad:job:" + dep
        if resource not in requires:
            requires.append(resource)
    provides = list(ctx.attr.provides)
    if not provides:
        provides = ["nomad:job:" + ctx.attr.job_id]

    descriptor = ctx.actions.declare_file(ctx.label.name + ".nomad_component.json")
    descriptor_data = {
        "schema_version": 2,
        "artifacts": artifacts,
        "component": ctx.attr.component,
        "depends_on": ctx.attr.depends_on,
        "job_id": ctx.attr.job_id,
        "job_spec": job_spec.short_path,
        "job_spec_path": job_spec.path,
        "label": _repo_label(ctx.label),
        "provides": provides,
        "requires": requires,
        "sites": ctx.attr.sites,
        "unit_id": ctx.attr.job_id,
    }
    _write_descriptor(ctx, descriptor, json.encode(descriptor_data) + "\n", inputs)

    return [
        DefaultInfo(files = depset([descriptor], transitive = [depset(inputs)])),
        NomadComponentInfo(
            artifacts = ctx.attr.artifacts,
            component = ctx.attr.component,
            depends_on = ctx.attr.depends_on,
            descriptor = descriptor,
            job_id = ctx.attr.job_id,
            job_spec = job_spec,
            provides = provides,
            requires = requires,
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
        "job_id": attr.string(
            mandatory = True,
            doc = "Nomad Job.ID.",
        ),
        "job_spec": attr.label(
            allow_single_file = True,
            mandatory = True,
            doc = "Authored owner-local Nomad job spec.",
        ),
        "provides": attr.string_list(
            doc = "Logical resources this component provides. Defaults to nomad:job:<job_id>.",
        ),
        "requires": attr.string_list(
            doc = "Logical resources this component requires.",
        ),
        "sites": attr.string_list(
            doc = "Sites where this component participates. Empty means all sites.",
        ),
    },
)
