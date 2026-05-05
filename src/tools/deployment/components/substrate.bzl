"""Rules for service-owned host substrate descriptors."""

ComponentSubstrateInfo = provider(
    doc = "Host substrate intent owned by a Nomad-deployed service or frontend.",
    fields = {
        "component": "Stable component key.",
        "definition": "JSON-encoded component substrate definition.",
        "descriptor": "Single component descriptor JSON file.",
    },
)

def _repo_label(label):
    raw = str(label)
    if raw.startswith("@" + "@//"):
        return raw[2:]
    return raw

def _write_json(ctx, out, content, mnemonic):
    ctx.actions.run_shell(
        outputs = [out],
        arguments = [out.path],
        command = "cat > \"$1\" <<'EOF'\n" + content + "EOF\n",
        mnemonic = mnemonic,
        progress_message = "Writing component substrate descriptor %{label}",
    )

def _component_substrate_impl(ctx):
    if not ctx.attr.component:
        fail("component is required")
    definition = {
        "deployment": {
            "nomad_component_label": ctx.attr.nomad_component_label,
            "supervisor": "nomad",
        },
        "kind": ctx.attr.kind,
        "label": _repo_label(ctx.label),
        "name": ctx.attr.component,
        "nftables_rulesets": json.decode(ctx.attr.nftables_rulesets_json),
        "order": ctx.attr.order,
        "postgres": json.decode(ctx.attr.postgres_json),
        "schema_version": 1,
        "workload": json.decode(ctx.attr.workload_json),
    }
    encoded = json.encode(definition)
    descriptor = ctx.actions.declare_file(ctx.label.name + ".component_substrate.json")
    _write_json(ctx, descriptor, encoded + "\n", "ComponentSubstrateDescriptor")
    return [
        DefaultInfo(files = depset([descriptor])),
        ComponentSubstrateInfo(
            component = ctx.attr.component,
            definition = encoded,
            descriptor = descriptor,
        ),
    ]

_component_substrate = rule(
    implementation = _component_substrate_impl,
    attrs = {
        "component": attr.string(mandatory = True),
        "kind": attr.string(mandatory = True),
        "nftables_rulesets_json": attr.string(default = "[]"),
        "nomad_component_label": attr.string(mandatory = True),
        "order": attr.int(default = 0),
        "postgres_json": attr.string(default = "{}"),
        "workload_json": attr.string(mandatory = True),
    },
)

def _component_substrate_json_impl(ctx):
    definition = json.decode(ctx.attr.definition_json)
    for field in ["component", "kind", "nomad_component_label", "workload"]:
        if field not in definition:
            fail("definition_json must include %s" % field)
    definition = dict(definition)
    definition["deployment"] = {
        "nomad_component_label": definition.pop("nomad_component_label"),
        "supervisor": "nomad",
    }
    definition["label"] = _repo_label(ctx.label)
    definition["name"] = definition.pop("component")
    definition["nftables_rulesets"] = definition.get("nftables_rulesets", [])
    definition["order"] = definition.get("order", 0)
    definition["postgres"] = definition.get("postgres", {})
    definition["schema_version"] = 1
    encoded = json.encode(definition)
    descriptor = ctx.actions.declare_file(ctx.label.name + ".component_substrate.json")
    _write_json(ctx, descriptor, encoded + "\n", "ComponentSubstrateDescriptor")
    return [
        DefaultInfo(files = depset([descriptor])),
        ComponentSubstrateInfo(
            component = definition["name"],
            definition = encoded,
            descriptor = descriptor,
        ),
    ]

component_substrate_json = rule(
    implementation = _component_substrate_json_impl,
    attrs = {
        "definition_json": attr.string(mandatory = True),
    },
)

def component_substrate(name, component, kind, workload, nomad_component_label, order = 0, postgres = {}, nftables_rulesets = [], visibility = None):
    _component_substrate(
        name = name,
        component = component,
        kind = kind,
        nftables_rulesets_json = json.encode(nftables_rulesets),
        nomad_component_label = nomad_component_label,
        order = order,
        postgres_json = json.encode(postgres),
        visibility = visibility,
        workload_json = json.encode(workload),
    )

def _component_substrate_registry_impl(ctx):
    by_component = {}
    for target in ctx.attr.components:
        info = target[ComponentSubstrateInfo]
        prior = by_component.get(info.component)
        if prior:
            fail("duplicate component substrate descriptor for %s: %s and %s" % (info.component, prior, target.label))
        by_component[info.component] = info
    encoded_components = [by_component[name].definition for name in sorted(by_component.keys())]
    content = '{"components":[' + ",".join(encoded_components) + '],"schema_version":1}\n'
    descriptor = ctx.actions.declare_file(ctx.label.name + ".component_substrate_registry.json")
    _write_json(ctx, descriptor, content, "ComponentSubstrateRegistry")
    return [DefaultInfo(files = depset([descriptor]))]

component_substrate_registry = rule(
    implementation = _component_substrate_registry_impl,
    attrs = {
        "components": attr.label_list(
            mandatory = True,
            providers = [ComponentSubstrateInfo],
        ),
    },
)
