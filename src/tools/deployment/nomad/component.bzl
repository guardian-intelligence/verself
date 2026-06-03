"""Rules for component-owned Nomad deployment descriptors."""

NomadComponentInfo = provider(
    doc = "Nomad deployment metadata owned by the deployable component package.",
    fields = {
        "artifacts": "label_keyed_string_dict of artifact targets to release output names.",
        "component": "Topology component key.",
        "descriptor": "Component descriptor JSON file.",
        "deploy_phase": "Topology metadata; Nomad owns rollout behavior.",
        "digest_inputs": "Files whose content must participate in the Nomad job spec digest without being downloaded as runtime artifacts.",
        "job_id": "Nomad Job.ID.",
        "job_spec": "Single authored Nomad job spec File.",
        "pre_artifacts": "label_keyed_string_dict of artifact targets to release output names kept as build outputs.",
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

def _digest_input_records(targets):
    inputs = []
    records = []
    for target in targets:
        files_by_path = {}
        for f in target.files.to_list():
            files_by_path[f.short_path] = f
        for short_path in sorted(files_by_path.keys()):
            f = files_by_path[short_path]
            inputs.append(f)
            records.append({
                "label": _repo_label(target.label),
                "path": f.path,
                "short_path": f.short_path,
            })
    return inputs, records

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

    descriptor_inputs = [job_spec]
    default_outputs = []
    artifacts = []
    artifact_outputs = {}
    for artifact_target, output in ctx.attr.artifacts.items():
        if output in artifact_outputs:
            fail("duplicate Nomad artifact output %s in %s" % (output, ctx.label))
        artifact_outputs[output] = True
        artifact_file = _single_file(artifact_target, "artifact")
        default_outputs.append(artifact_file)
        artifacts.append({
            "label": _repo_label(artifact_target.label),
            "output": output,
            "path": artifact_file.path,
        })
    pre_artifacts = []
    for artifact_target, output in ctx.attr.pre_artifacts.items():
        if output in artifact_outputs:
            fail("duplicate Nomad artifact output %s in %s" % (output, ctx.label))
        artifact_outputs[output] = True
        artifact_file = _single_file(artifact_target, "pre-artifact")
        default_outputs.append(artifact_file)
        pre_artifacts.append({
            "label": _repo_label(artifact_target.label),
            "output": output,
            "path": artifact_file.path,
        })
    digest_input_files, digest_inputs = _digest_input_records(ctx.attr.digest_inputs)
    descriptor_inputs.extend(digest_input_files)

    descriptor = ctx.actions.declare_file(ctx.label.name + ".nomad_component.json")
    descriptor_data = {
        "schema_version": 7,
        "artifacts": artifacts,
        "component": ctx.attr.component,
        "deploy_phase": ctx.attr.deploy_phase,
        "digest_inputs": digest_inputs,
        "job_id": ctx.attr.job_id,
        "job_spec": job_spec.short_path,
        "job_spec_path": job_spec.path,
        "label": _repo_label(ctx.label),
        "pre_artifacts": pre_artifacts,
        "sites": ctx.attr.sites,
        "unit_id": ctx.attr.job_id,
    }
    _write_descriptor(ctx, descriptor, json.encode(descriptor_data) + "\n", descriptor_inputs)

    return [
        DefaultInfo(files = depset([descriptor] + default_outputs)),
        NomadComponentInfo(
            artifacts = ctx.attr.artifacts,
            component = ctx.attr.component,
            descriptor = descriptor,
            deploy_phase = ctx.attr.deploy_phase,
            digest_inputs = ctx.attr.digest_inputs,
            job_id = ctx.attr.job_id,
            job_spec = job_spec,
            pre_artifacts = ctx.attr.pre_artifacts,
        ),
        OutputGroupInfo(nomad_descriptor = depset([descriptor])),
    ]

nomad_component = rule(
    implementation = _nomad_component_impl,
    attrs = {
        "artifacts": attr.label_keyed_string_dict(
            allow_files = True,
            doc = "Map of component-owned artifact targets to release output names.",
        ),
        "pre_artifacts": attr.label_keyed_string_dict(
            allow_files = True,
            doc = "Artifact targets kept as deployable-unit build outputs.",
        ),
        "component": attr.string(
            mandatory = True,
            doc = "Topology component key.",
        ),
        "deploy_phase": attr.string(
            default = "product",
            doc = "Topology annotation for deployable units.",
        ),
        "digest_inputs": attr.label_list(
            allow_files = True,
            doc = "Source or generated files that should participate in the deployable unit build graph.",
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
        "sites": attr.string_list(
            doc = "Sites where this component participates. Empty means all sites.",
        ),
    },
)
