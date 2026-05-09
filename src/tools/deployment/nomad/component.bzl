"""Rules for component-owned Nomad deployment descriptors."""

NomadComponentInfo = provider(
    doc = "Nomad deployment metadata owned by the deployable component package.",
    fields = {
        "artifacts": "label_keyed_string_dict of artifact targets to release output names.",
        "component": "Topology component key.",
        "descriptor": "Component descriptor JSON file.",
        "deploy_phase": "Deployment phase for graph wave submission.",
        "job_id": "Nomad Job.ID.",
        "job_spec": "Single authored Nomad job spec File.",
        "provides": "Logical resources this component provides.",
        "requires": "Logical resources this component requires.",
    },
)

_ALLOWED_DEPLOY_PHASES = [
    "pre_artifact",
    "platform",
    "product",
    "edge",
]

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
    if ctx.attr.deploy_phase not in _ALLOWED_DEPLOY_PHASES:
        fail("deploy_phase must be one of %s" % ", ".join(_ALLOWED_DEPLOY_PHASES))

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
    provides = list(ctx.attr.provides)
    if not provides:
        provides = ["nomad:job:" + ctx.attr.job_id]

    descriptor = ctx.actions.declare_file(ctx.label.name + ".nomad_component.json")
    descriptor_data = {
        "schema_version": 3,
        "artifacts": artifacts,
        "component": ctx.attr.component,
        "deploy_phase": ctx.attr.deploy_phase,
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
            descriptor = descriptor,
            deploy_phase = ctx.attr.deploy_phase,
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
        "deploy_phase": attr.string(
            default = "product",
            doc = "Deployment phase. pre_artifact jobs are submitted before artifact publication; platform/product/edge jobs are submitted after artifact publication is available.",
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
