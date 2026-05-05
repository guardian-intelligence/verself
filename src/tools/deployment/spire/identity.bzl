"""Rules for component-owned SPIRE workload identity descriptors."""

SpireIdentityInfo = provider(
    doc = "SPIRE workload registration intent owned by a service or component.",
    fields = {
        "component": "Owning topology component key.",
        "descriptor": "Single identity descriptor JSON file.",
        "entry_id": "Stable SPIRE registration entry ID.",
        "group": "Primary Unix group selected by the entry.",
        "key": "Stable component-local identity key.",
        "path": "SPIFFE path under the site trust domain.",
        "restart_units": "Systemd units to restart when the registration changes.",
        "selector": "SPIRE selector prefix.",
        "uid_policy": "UID policy dict.",
        "user": "Unix user selected by the entry.",
        "x509_svid_ttl_seconds": "X.509 SVID TTL.",
    },
)

def _repo_label(label):
    raw = str(label)
    canonical_main_prefix = "@" + "@//"
    if raw.startswith(canonical_main_prefix):
        return raw[2:]
    return raw

def _write_descriptor(ctx, out, content, mnemonic):
    ctx.actions.run_shell(
        outputs = [out],
        arguments = [out.path],
        command = "cat > \"$1\" <<'EOF'\n" + content + "EOF\n",
        mnemonic = mnemonic,
        progress_message = "Writing SPIRE identity descriptor %{label}",
    )

def _clean_path(path):
    clean = path.strip()
    if not clean:
        fail("path is required")
    if not clean.startswith("/"):
        clean = "/" + clean
    return clean

def _uid_policy(kind, value):
    if kind == "allocated":
        return {"kind": "allocated"}
    if kind == "fixed":
        if value <= 0:
            fail("uid_policy_value must be set when uid_policy_kind is fixed")
        return {
            "kind": "fixed",
            "value": value,
        }
    fail("unsupported uid_policy_kind %r" % kind)

def _identity_row(ctx):
    if not ctx.attr.component:
        fail("component is required")
    if not ctx.attr.key:
        fail("key is required")
    if not ctx.attr.entry_id:
        fail("entry_id is required")
    if not ctx.attr.user:
        fail("user is required")
    group = ctx.attr.group or ctx.attr.user
    return {
        "component": ctx.attr.component,
        "entry_id": ctx.attr.entry_id,
        "group": group,
        "key": ctx.attr.key,
        "label": _repo_label(ctx.label),
        "path": _clean_path(ctx.attr.path),
        "restart_units": ctx.attr.restart_units,
        "selector": ctx.attr.selector,
        "uid_policy": _uid_policy(ctx.attr.uid_policy_kind, ctx.attr.uid_policy_value),
        "user": ctx.attr.user,
        "x509_svid_ttl_seconds": ctx.attr.x509_svid_ttl_seconds,
    }

def _spire_identity_impl(ctx):
    row = _identity_row(ctx)
    descriptor = ctx.actions.declare_file(ctx.label.name + ".spire_identity.json")
    _write_descriptor(ctx, descriptor, json.encode({
        "schema_version": 1,
        "identity": row,
    }) + "\n", "SpireIdentityDescriptor")
    return [
        DefaultInfo(files = depset([descriptor])),
        SpireIdentityInfo(
            component = row["component"],
            descriptor = descriptor,
            entry_id = row["entry_id"],
            group = row["group"],
            key = row["key"],
            path = row["path"],
            restart_units = row["restart_units"],
            selector = row["selector"],
            uid_policy = row["uid_policy"],
            user = row["user"],
            x509_svid_ttl_seconds = row["x509_svid_ttl_seconds"],
        ),
    ]

spire_identity = rule(
    implementation = _spire_identity_impl,
    attrs = {
        "component": attr.string(mandatory = True),
        "entry_id": attr.string(mandatory = True),
        "group": attr.string(),
        "key": attr.string(mandatory = True),
        "path": attr.string(mandatory = True),
        "restart_units": attr.string_list(),
        "selector": attr.string(default = "unix:uid"),
        "uid_policy_kind": attr.string(default = "allocated"),
        "uid_policy_value": attr.int(),
        "user": attr.string(mandatory = True),
        "x509_svid_ttl_seconds": attr.int(default = 3600),
    },
)

def _identity_from_info(target):
    info = target[SpireIdentityInfo]
    return {
        "component": info.component,
        "entry_id": info.entry_id,
        "group": info.group,
        "key": info.key,
        "label": _repo_label(target.label),
        "path": info.path,
        "restart_units": info.restart_units,
        "selector": info.selector,
        "uid_policy": info.uid_policy,
        "user": info.user,
        "x509_svid_ttl_seconds": info.x509_svid_ttl_seconds,
    }

def _spire_identity_registry_impl(ctx):
    rows_by_key = {}
    for identity in ctx.attr.identities:
        row = _identity_from_info(identity)
        if row["key"] in rows_by_key:
            fail("duplicate SPIRE identity key %r in %s and %s" % (row["key"], rows_by_key[row["key"]]["label"], row["label"]))
        rows_by_key[row["key"]] = row
    rows = [rows_by_key[key] for key in sorted(rows_by_key.keys())]
    seen = {}
    for row in rows:
        for field in ["key", "entry_id", "path"]:
            value = row[field]
            prior = seen.get((field, value))
            if prior:
                fail("duplicate SPIRE identity %s %r in %s and %s" % (field, value, prior, row["label"]))
            seen[(field, value)] = row["label"]

    descriptor = ctx.actions.declare_file(ctx.label.name + ".spire_identity_registry.json")
    _write_descriptor(ctx, descriptor, json.encode({
        "schema_version": 1,
        "identities": rows,
    }) + "\n", "SpireIdentityRegistry")
    return [DefaultInfo(files = depset([descriptor]))]

spire_identity_registry = rule(
    implementation = _spire_identity_registry_impl,
    attrs = {
        "identities": attr.label_list(
            providers = [SpireIdentityInfo],
            mandatory = True,
        ),
    },
)
