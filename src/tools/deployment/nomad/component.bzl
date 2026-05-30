"""Rules for component-owned Nomad deployment descriptors."""

NomadComponentInfo = provider(
    doc = "Nomad deployment metadata owned by the deployable component package.",
    fields = {
        "artifacts": "label_keyed_string_dict of artifact targets to release output names.",
        "component": "Topology component key.",
        "descriptor": "Component descriptor JSON file.",
        "digest_inputs": "Files whose content must participate in the Nomad job spec digest without being downloaded as runtime artifacts.",
        "job_id": "Nomad Job.ID.",
        "job_spec": "Single authored Nomad job spec File.",
        "pre_artifacts": "label_keyed_string_dict of artifact targets to release output names that deploy extracts onto the host before this job is submitted.",
        "provides": "Logical resources this component provides.",
        "post_deploy_canaries": "Post-deploy canary descriptors gated after Nomad rollout health.",
        "requires": "Logical resources this component requires.",
        "test_targets": "Bazel test targets that must pass before this component is deployed.",
    },
)

PostDeployCanaryInfo = provider(
    doc = "A Bazel-run post-deployment health check owned by a deployable package.",
    fields = {
        "args": "Static argv passed after `bazel run <target> --`, with deploy placeholders expanded by verself-deploy.",
        "env": "Static environment overrides, with deploy placeholders expanded by verself-deploy.",
        "kind": "Canary implementation kind: cli or browser.",
        "label": "Label of this canary declaration.",
        "size": "Canary size: medium or large.",
        "target": "Executable Bazel target to run.",
        "timeout": "Optional Go-duration timeout for this canary.",
    },
)

_ALLOWED_CANARY_KINDS = [
    "browser",
    "cli",
]

_ALLOWED_CANARY_SIZES = [
    "medium",
    "large",
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

def _canary_descriptor(canary):
    return {
        "args": list(canary.args),
        "env": dict(canary.env),
        "kind": canary.kind,
        "label": canary.label,
        "size": canary.size,
        "target": canary.target,
        "timeout": canary.timeout,
    }

def _nomad_component_impl(ctx):
    job_spec = _single_file(ctx.attr.job_spec, "Nomad job spec")
    if not ctx.attr.component:
        fail("component is required")
    if not ctx.attr.job_id:
        fail("job_id is required")

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

    requires = list(ctx.attr.requires)
    provides = list(ctx.attr.provides)
    if not provides:
        provides = ["nomad:job:" + ctx.attr.job_id]

    post_deploy_canaries = [
        _canary_descriptor(target[PostDeployCanaryInfo])
        for target in ctx.attr.post_deploy_canaries
    ]

    descriptor = ctx.actions.declare_file(ctx.label.name + ".nomad_component.json")
    descriptor_data = {
        "schema_version": 6,
        "artifacts": artifacts,
        "component": ctx.attr.component,
        "digest_inputs": digest_inputs,
        "job_id": ctx.attr.job_id,
        "job_spec": job_spec.short_path,
        "job_spec_path": job_spec.path,
        "label": _repo_label(ctx.label),
        "pre_artifacts": pre_artifacts,
        "provides": provides,
        "post_deploy_canaries": post_deploy_canaries,
        "requires": requires,
        "sites": ctx.attr.sites,
        "test_targets": ctx.attr.test_targets,
        "unit_id": ctx.attr.job_id,
    }
    _write_descriptor(ctx, descriptor, json.encode(descriptor_data) + "\n", descriptor_inputs)

    return [
        DefaultInfo(files = depset([descriptor] + default_outputs)),
        NomadComponentInfo(
            artifacts = ctx.attr.artifacts,
            component = ctx.attr.component,
            descriptor = descriptor,
            digest_inputs = ctx.attr.digest_inputs,
            job_id = ctx.attr.job_id,
            job_spec = job_spec,
            pre_artifacts = ctx.attr.pre_artifacts,
            provides = provides,
            post_deploy_canaries = post_deploy_canaries,
            requires = requires,
            test_targets = ctx.attr.test_targets,
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
            doc = "Artifact targets deploy extracts onto the host before this job is submitted. Authored Nomad specs must reference these through task env placeholders, not Nomad artifact stanzas.",
        ),
        "component": attr.string(
            mandatory = True,
            doc = "Topology component key.",
        ),
        "digest_inputs": attr.label_list(
            allow_files = True,
            doc = "Source or generated files that should force a Nomad redeploy when their content changes, without becoming Nomad runtime artifacts.",
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
        "post_deploy_canaries": attr.label_list(
            providers = [PostDeployCanaryInfo],
            doc = "Component-owned live canaries that must pass after changed Nomad jobs report rollout health.",
        ),
        "requires": attr.string_list(
            doc = "Logical resources this component requires.",
        ),
        "sites": attr.string_list(
            doc = "Sites where this component participates. Empty means all sites.",
        ),
        "test_targets": attr.string_list(
            doc = "Bazel test targets that must pass before this component is deployed.",
        ),
    },
)

def _post_deploy_canary_impl(ctx):
    if ctx.attr.kind not in _ALLOWED_CANARY_KINDS:
        fail("kind must be one of %s" % ", ".join(_ALLOWED_CANARY_KINDS))
    if ctx.attr.canary_size not in _ALLOWED_CANARY_SIZES:
        fail("canary_size must be one of %s" % ", ".join(_ALLOWED_CANARY_SIZES))

    files_to_run = ctx.attr.target[DefaultInfo].files_to_run
    if files_to_run.executable == None:
        fail("target must be executable: %s" % ctx.attr.target.label)

    return [
        DefaultInfo(files = depset()),
        PostDeployCanaryInfo(
            args = ctx.attr.args,
            env = ctx.attr.env,
            kind = ctx.attr.kind,
            label = _repo_label(ctx.label),
            size = ctx.attr.canary_size,
            target = _repo_label(ctx.attr.target.label),
            timeout = ctx.attr.canary_timeout,
        ),
    ]

post_deploy_canary = rule(
    implementation = _post_deploy_canary_impl,
    attrs = {
        "args": attr.string_list(
            doc = "Arguments passed after `bazel run <target> --`. Supported placeholders: {site}, {repo_root}, {deploy_run_key}, {sha}, {component}, {job_id}, {canary_label}, {canary_kind}, {canary_size}.",
        ),
        "env": attr.string_dict(
            doc = "Environment overrides for the canary process. Values support the same placeholders as args.",
        ),
        "kind": attr.string(
            mandatory = True,
            doc = "Canary implementation kind: cli or browser.",
        ),
        "canary_size": attr.string(
            mandatory = True,
            doc = "Canary size: medium or large.",
        ),
        "target": attr.label(
            mandatory = True,
            doc = "Executable Bazel target to run for this canary.",
        ),
        "canary_timeout": attr.string(
            doc = "Optional Go-duration timeout such as 90s or 10m. Defaults by size.",
        ),
    },
)
