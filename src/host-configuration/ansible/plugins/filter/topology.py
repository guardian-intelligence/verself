from __future__ import annotations


def _spiffe_id(trust_domain: str, path: str) -> str:
    clean_domain = str(trust_domain).strip()
    clean_path = str(path).strip()
    if not clean_path.startswith("/"):
        clean_path = "/" + clean_path
    return f"spiffe://{clean_domain}{clean_path}"


def _normalize_identity(component_name: str, key: str, identity: dict, trust_domain: str) -> dict:
    normalized = dict(identity or {})
    normalized.pop("ansible_var", None)
    normalized.setdefault("restart_units", [])
    normalized.setdefault("selector", "unix:uid")
    normalized.setdefault("uid_policy", {"kind": "allocated"})
    normalized.setdefault("x509_svid_ttl_seconds", 3600)
    if "group" not in identity:
        normalized["group"] = normalized["user"]
    normalized["component"] = component_name
    normalized["key"] = f"{component_name}.{key}"
    normalized["spiffe_id"] = normalized.get("spiffe_id") or _spiffe_id(trust_domain, normalized["path"])
    return normalized


def _normalize_unit(unit: dict) -> dict:
    normalized = dict(unit or {})
    normalized.setdefault("create_home", False)
    normalized.setdefault("home", "")
    normalized.setdefault("load_credentials", [])
    normalized.setdefault("supplementary_groups", [])
    if "group" not in unit:
        normalized["group"] = normalized["user"]
    return normalized


def _normalize_postgres(postgres: dict) -> dict:
    raw = dict(postgres or {})
    database = raw.get("database", "")
    raw.setdefault("connection_limit", 10 if database else 0)
    raw.setdefault("database", "")
    raw.setdefault("owner", database if database else "")
    raw.setdefault("password_ref", {"kind": "none"})
    return raw


def _normalize_workload(workload: dict, component_order: int) -> dict:
    raw = dict(workload or {})
    raw.setdefault("auth", {"kind": "none"})
    raw.setdefault("bootstrap", [])
    raw.setdefault("bootstrap_config", {})
    raw.setdefault("directories", [])
    raw.setdefault("order", component_order)
    raw.setdefault("secret_refs", [])
    raw["units"] = [_normalize_unit(unit) for unit in raw.get("units", [])]
    return raw


def verself_topology_effective(components: list[dict], trust_domain: str) -> dict:
    effective_components = []
    spire_identities = []
    spiffe_ids = {}

    for component in components:
        normalized = dict(component or {})
        component_name = normalized["name"]
        normalized["deployment"] = {"supervisor": "systemd", **dict(normalized.get("deployment") or {})}
        normalized.setdefault("nftables_rulesets", [])
        normalized.setdefault("order", 0)

        identities = {}
        for key, identity in normalized.get("identities", {}).items():
            normalized_identity = _normalize_identity(component_name, key, identity, trust_domain)
            identities[key] = normalized_identity
            spire_identities.append(normalized_identity)
            spiffe_ids.setdefault(component_name, {})[key] = normalized_identity["spiffe_id"]
        normalized["identities"] = identities

        normalized["postgres"] = _normalize_postgres(normalized.get("postgres", {}))
        normalized["workload"] = _normalize_workload(normalized.get("workload", {}), normalized["order"])
        effective_components.append(normalized)

    return {
        "components": effective_components,
        "spire_identities": spire_identities,
        "spiffe_ids": spiffe_ids,
    }


class FilterModule:
    def filters(self):
        return {
            "verself_topology_effective": verself_topology_effective,
        }
