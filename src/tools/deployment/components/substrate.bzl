"""Rules for service-owned host substrate descriptors."""

ComponentSubstrateInfo = provider(
    doc = "Host substrate intent owned by a Nomad-deployed service or frontend.",
    fields = {
        "component": "Stable component key.",
        "definition": "JSON-encoded component substrate definition.",
        "descriptor": "Single component descriptor JSON file.",
        "files": "Service-owned substrate files consumed by host convergence.",
    },
)

def _repo_label(label):
    raw = str(label)
    if raw.startswith("@" + "@//"):
        return raw[2:]
    return raw

def _write_json(ctx, out, content, mnemonic, inputs = []):
    ctx.actions.run_shell(
        inputs = inputs,
        outputs = [out],
        arguments = [out.path],
        command = "cat > \"$1\" <<'EOF'\n" + content + "EOF\n",
        mnemonic = mnemonic,
        progress_message = "Writing component substrate descriptor %{label}",
    )

def _normalize_postgres(name, postgres):
    normalized = dict(postgres)
    if normalized.get("database", ""):
        normalized["owner"] = normalized.get("owner", name)
        normalized["connection_limit"] = normalized.get("connection_limit", 10)
    return normalized

def _validate_substrate_files(label, definition, srcs):
    declared_sources = {}
    for ruleset in definition.get("nftables_rulesets", []):
        source = ruleset.get("source", "")
        if not source:
            fail("%s nftables_rulesets entries must declare a workspace-relative source" % label)
        declared_sources[source] = True
    for hook in definition.get("workload", {}).get("bootstrap", []):
        source = hook.get("source", "")
        if not source:
            fail("%s workload.bootstrap entries must declare a workspace-relative source" % label)
        declared_sources[source] = True

    for src in srcs:
        if src.short_path not in declared_sources:
            fail("%s srcs entry %s is not referenced by a component substrate source" % (label, src.short_path))

    for source in declared_sources:
        found = False
        for src in srcs:
            if src.short_path == source:
                found = True
                break
        if not found:
            fail("%s component substrate source %s is not present in srcs" % (label, source))

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
        "postgres": _normalize_postgres(ctx.attr.component, json.decode(ctx.attr.postgres_json)),
        "schema_version": 1,
        "workload": json.decode(ctx.attr.workload_json),
    }
    _validate_substrate_files(ctx.label, definition, ctx.files.srcs)
    encoded = json.encode(definition)
    descriptor = ctx.actions.declare_file(ctx.label.name + ".component_substrate.json")
    substrate_files = depset(ctx.files.srcs)
    _write_json(ctx, descriptor, encoded + "\n", "ComponentSubstrateDescriptor", ctx.files.srcs)
    return [
        DefaultInfo(files = depset([descriptor], transitive = [substrate_files])),
        ComponentSubstrateInfo(
            component = ctx.attr.component,
            definition = encoded,
            descriptor = descriptor,
            files = substrate_files,
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
        "srcs": attr.label_list(allow_files = True),
        "workload_json": attr.string(mandatory = True),
    },
)

def _component_substrate_json_impl(ctx):
    definition = json.decode(ctx.attr.definition_json)
    for field in ["component", "kind", "workload"]:
        if field not in definition:
            fail("definition_json must include %s" % field)
    definition = dict(definition)
    deployment = definition.get("deployment")
    nomad_component_label = definition.pop("nomad_component_label", "")
    if deployment == None:
        if not nomad_component_label:
            fail("definition_json must include either deployment or nomad_component_label")
        deployment = {
            "nomad_component_label": nomad_component_label,
            "supervisor": "nomad",
        }
    elif nomad_component_label:
        fail("definition_json must not include both deployment and nomad_component_label")
    definition["deployment"] = deployment
    definition["label"] = _repo_label(ctx.label)
    definition["name"] = definition.pop("component")
    definition["nftables_rulesets"] = definition.get("nftables_rulesets", [])
    definition["order"] = definition.get("order", 0)
    definition["postgres"] = _normalize_postgres(definition["name"], definition.get("postgres", {}))
    definition["schema_version"] = 1
    _validate_substrate_files(ctx.label, definition, ctx.files.srcs)
    encoded = json.encode(definition)
    descriptor = ctx.actions.declare_file(ctx.label.name + ".component_substrate.json")
    substrate_files = depset(ctx.files.srcs)
    _write_json(ctx, descriptor, encoded + "\n", "ComponentSubstrateDescriptor", ctx.files.srcs)
    return [
        DefaultInfo(files = depset([descriptor], transitive = [substrate_files])),
        ComponentSubstrateInfo(
            component = definition["name"],
            definition = encoded,
            descriptor = descriptor,
            files = substrate_files,
        ),
    ]

component_substrate_json = rule(
    implementation = _component_substrate_json_impl,
    attrs = {
        "definition_json": attr.string(mandatory = True),
        "srcs": attr.label_list(allow_files = True),
    },
)

def component_substrate(name, component, kind, workload, nomad_component_label, order = 0, postgres = {}, nftables_rulesets = [], srcs = [], visibility = None):
    _component_substrate(
        name = name,
        component = component,
        kind = kind,
        nftables_rulesets_json = json.encode(nftables_rulesets),
        nomad_component_label = nomad_component_label,
        order = order,
        postgres_json = json.encode(postgres),
        srcs = srcs,
        visibility = visibility,
        workload_json = json.encode(workload),
    )

def _component_substrate_registry_impl(ctx):
    by_component = {}
    component_files = []
    for target in ctx.attr.components:
        info = target[ComponentSubstrateInfo]
        prior = by_component.get(info.component)
        if prior:
            fail("duplicate component substrate descriptor for %s: %s and %s" % (info.component, prior, target.label))
        by_component[info.component] = info
        component_files.append(info.files)
    encoded_components = [by_component[name].definition for name in sorted(by_component.keys())]
    content = '{"components":[' + ",".join(encoded_components) + '],"schema_version":1}\n'
    descriptor = ctx.actions.declare_file(ctx.label.name + ".component_substrate_registry.json")
    _write_json(ctx, descriptor, content, "ComponentSubstrateRegistry", depset(transitive = component_files))
    return [DefaultInfo(files = depset([descriptor], transitive = component_files))]

component_substrate_registry = rule(
    implementation = _component_substrate_registry_impl,
    attrs = {
        "components": attr.label_list(
            mandatory = True,
            providers = [ComponentSubstrateInfo],
        ),
    },
)
