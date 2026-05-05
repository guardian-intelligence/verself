#!/usr/bin/env python3
import argparse
import grp
import json
import os
import pwd
import stat
import sys
from pathlib import Path


def load_json_env(name: str):
    raw = os.environ.get(name, "")
    if raw == "":
        raise ValueError(f"{name} is required")
    return json.loads(raw)


def parse_mode(value) -> int:
    if isinstance(value, int):
        return value
    return int(str(value), 8)


def owner_matches(st: os.stat_result, owner: str) -> bool:
    return pwd.getpwnam(owner).pw_uid == st.st_uid


def group_matches(st: os.stat_result, group: str) -> bool:
    return grp.getgrnam(group).gr_gid == st.st_gid


def metadata_reasons(path: Path, item: dict, want_directory: bool) -> list[str]:
    reasons: list[str] = []
    try:
        st = path.stat()
    except FileNotFoundError:
        return ["missing"]

    if want_directory and not stat.S_ISDIR(st.st_mode):
        reasons.append("not_directory")
    if not want_directory and not stat.S_ISREG(st.st_mode):
        reasons.append("not_file")
    try:
        if not owner_matches(st, item["owner"]):
            reasons.append("owner")
    except KeyError:
        reasons.append("owner_missing")
    try:
        if not group_matches(st, item["group"]):
            reasons.append("group")
    except KeyError:
        reasons.append("group_missing")
    if stat.S_IMODE(st.st_mode) != parse_mode(item["mode"]):
        reasons.append("mode")
    return reasons


def expected_secret_bytes(secret: dict, ansible_vars: dict) -> bytes | None:
    source = secret.get("source", {})
    kind = source.get("kind")
    if kind == "generated":
        return None
    if kind == "ansible_var":
        name = source["ansible_var"]
        if name not in ansible_vars:
            raise ValueError(f"missing ansible var value for {name}")
        return (str(ansible_vars[name]) + "\n").encode()
    if kind == "remote_src":
        return Path(source["remote_src"]).read_bytes()
    raise ValueError(f"unsupported secret source kind {kind!r}")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.parse_args()

    try:
        components = load_json_env("COMPONENT_FILESYSTEM_DESIRED")
        ansible_vars = load_json_env("COMPONENT_FILESYSTEM_ANSIBLE_VARS")

        drifted_directories: list[dict] = []
        drifted_secrets: list[dict] = []
        exposed_values: dict[str, dict[str, str]] = {}

        for component in components:
            component_name = component["name"]
            workload = component.get("workload", {})
            for directory in workload.get("directories", []):
                reasons = metadata_reasons(Path(directory["path"]), directory, True)
                if reasons:
                    drifted_directories.append(
                        {"component": component_name, "path": directory["path"], "reasons": reasons}
                    )

            for secret in workload.get("secret_refs", []):
                path = Path(secret["path"])
                reasons = metadata_reasons(path, secret, False)
                if "missing" not in reasons:
                    try:
                        expected = expected_secret_bytes(secret, ansible_vars)
                        if expected is not None and path.read_bytes() != expected:
                            reasons.append("content")
                    except FileNotFoundError:
                        reasons.append("source_missing")
                if reasons:
                    drifted_secrets.append(
                        {"component": component_name, "path": secret["path"], "reasons": reasons}
                    )

                expose_as = secret.get("expose_as")
                if expose_as and path.exists():
                    exposed_values.setdefault(component_name, {})[expose_as] = path.read_text().strip()

        result = {
            "converged": not (drifted_directories or drifted_secrets),
            "directory_count": sum(len(c.get("workload", {}).get("directories", [])) for c in components),
            "secret_count": sum(len(c.get("workload", {}).get("secret_refs", [])) for c in components),
            "drifted_directories": drifted_directories,
            "drifted_secrets": drifted_secrets,
            "exposed_values": exposed_values,
        }
        json.dump(result, sys.stdout, sort_keys=True)
        sys.stdout.write("\n")
        return 0
    except Exception as exc:
        json.dump({"error": str(exc)}, sys.stderr, sort_keys=True)
        sys.stderr.write("\n")
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
