"""Rules for deployable units outside Nomad's job artifact path."""

DeployUnitInfo = provider(
    doc = "Deployment metadata for a non-Nomad deployable unit.",
    fields = {
        "descriptor": "Deploy unit descriptor JSON file.",
        "executor": "Unit executor key.",
        "payload_kind": "Executor-local payload kind.",
        "unit_id": "Stable unit identifier.",
    },
)

def _repo_label(label):
    raw = str(label)
    canonical_main_prefix = "@" + "@//"
    if raw.startswith(canonical_main_prefix):
        return raw[2:]
    return raw

def _source_rows(files):
    rows = []
    for src in files:
        rows.append({
            "path": src.path,
            "short_path": src.short_path,
        })
    return rows

def _write_descriptor(ctx, out, content, inputs):
    ctx.actions.run_shell(
        inputs = inputs,
        outputs = [out],
        arguments = [out.path],
        command = "cat > \"$1\" <<'EOF'\n" + content + "EOF\n",
        mnemonic = "DeployUnitDescriptor",
        progress_message = "Writing deploy unit descriptor %{label}",
    )

def _deploy_unit_impl(ctx):
    if not ctx.attr.unit_id:
        fail("unit_id is required")
    if not ctx.attr.executor:
        fail("executor is required")
    if not ctx.attr.payload_kind:
        fail("payload_kind is required")

    inputs = ctx.files.srcs
    descriptor = ctx.actions.declare_file(ctx.label.name + ".deploy_unit.json")
    descriptor_data = {
        "schema_version": 1,
        "args": ctx.attr.args,
        "executor": ctx.attr.executor,
        "label": _repo_label(ctx.label),
        "payload": {
            "phase": ctx.attr.phase,
            "playbook": ctx.attr.playbook,
        },
        "payload_kind": ctx.attr.payload_kind,
        "sources": _source_rows(inputs),
        "unit_id": ctx.attr.unit_id,
    }
    _write_descriptor(ctx, descriptor, json.encode(descriptor_data) + "\n", inputs)

    return [
        DefaultInfo(files = depset([descriptor])),
        DeployUnitInfo(
            descriptor = descriptor,
            executor = ctx.attr.executor,
            payload_kind = ctx.attr.payload_kind,
            unit_id = ctx.attr.unit_id,
        ),
    ]

deploy_unit = rule(
    implementation = _deploy_unit_impl,
    attrs = {
        "args": attr.string_list(
            doc = "Executor-local arguments.",
        ),
        "executor": attr.string(
            default = "ansible",
            doc = "Deploy unit executor.",
        ),
        "payload_kind": attr.string(
            default = "ansible_playbook",
            doc = "Executor-local payload kind.",
        ),
        "phase": attr.string(
            mandatory = True,
            doc = "Ansible evidence phase or executor-local phase.",
        ),
        "playbook": attr.string(
            mandatory = True,
            doc = "Ansible playbook path relative to src/host-configuration/ansible.",
        ),
        "srcs": attr.label_list(
            allow_files = True,
            doc = "Files whose contents define this unit's desired digest.",
        ),
        "unit_id": attr.string(
            mandatory = True,
            doc = "Stable unit identifier.",
        ),
    },
)
