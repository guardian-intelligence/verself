#!/usr/bin/env python3
import argparse
import grp
import json
import os
import pwd
import subprocess
import sys


def load_desired() -> list[dict]:
    raw = os.environ.get("SPIRE_REGISTRATIONS_DESIRED", "")
    if raw == "":
        raise ValueError("SPIRE_REGISTRATIONS_DESIRED is required")
    value = json.loads(raw)
    if not isinstance(value, list):
        raise ValueError("SPIRE_REGISTRATIONS_DESIRED must be a JSON array")
    return value


def spiffe_id(value: dict) -> str:
    trust_domain = value.get("trust_domain", "")
    path = value.get("path", "")
    return f"spiffe://{trust_domain}{path}"


def selector_parts(selector: str, uid: int) -> tuple[str, str]:
    if ":" not in selector:
        raise ValueError(f"selector {selector!r} must contain a selector type and value prefix")
    selector_type, selector_value_prefix = selector.split(":", 1)
    return selector_type, f"{selector_value_prefix}:{uid}"


def read_spire_entries(spire_server: str, socket_path: str) -> dict:
    proc = subprocess.run(
        [
            spire_server,
            "entry",
            "show",
            "-socketPath",
            socket_path,
            "-output",
            "json",
        ],
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        timeout=5,
        check=False,
    )
    if proc.returncode != 0:
        stderr = proc.stderr.strip()
        raise RuntimeError(f"spire-server entry show failed with rc={proc.returncode}: {stderr}")
    payload = json.loads(proc.stdout or "{}")
    entries = payload.get("entries", [])
    if not isinstance(entries, list):
        raise ValueError("spire-server entry show JSON did not contain an entries array")
    return {entry.get("id", ""): entry for entry in entries if entry.get("id", "") != ""}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--spire-server", required=True)
    parser.add_argument("--socket-path", required=True)
    parser.add_argument("--parent-id", required=True)
    parser.add_argument("--workload-group", required=True)
    args = parser.parse_args()

    try:
        desired = load_desired()
        spire_entries = read_spire_entries(args.spire_server, args.socket_path)

        groups: dict[str, grp.struct_group] = {}
        missing_groups: list[dict] = []
        for group_name in sorted({item["group"] for item in desired} | {args.workload_group}):
            try:
                groups[group_name] = grp.getgrnam(group_name)
            except KeyError:
                missing_groups.append({"group": group_name})

        users: dict[str, pwd.struct_passwd] = {}
        missing_users: list[dict] = []
        drifted_users: list[dict] = []
        desired_entries: list[dict] = []
        missing_registrations: list[dict] = []
        drifted_registrations: list[dict] = []

        workload_group = groups.get(args.workload_group)
        workload_members = set(workload_group.gr_mem) if workload_group is not None else set()

        for item in desired:
            key = item["key"]
            user_name = item["user"]
            group_name = item["group"]
            user = None
            user_reasons: list[str] = []
            try:
                user = pwd.getpwnam(user_name)
                users[user_name] = user
            except KeyError:
                missing_users.append({"key": key, "user": user_name})

            if user is not None:
                uid_policy = item.get("uid_policy", {})
                if uid_policy.get("kind") == "fixed" and int(uid_policy.get("value")) != int(user.pw_uid):
                    user_reasons.append("uid")
                group = groups.get(group_name)
                if group is not None and int(user.pw_gid) != int(group.gr_gid):
                    user_reasons.append("primary_group")
                if user_name not in workload_members:
                    user_reasons.append("workload_group")
                if user.pw_shell != "/usr/sbin/nologin":
                    user_reasons.append("shell")
                if user_reasons:
                    drifted_users.append({"key": key, "user": user_name, "reasons": user_reasons})

            if user is None:
                continue

            selector_type, selector_value = selector_parts(item["selector"], int(user.pw_uid))
            desired_entry = {
                "key": key,
                "entry_id": item["entry_id"],
                "parent_id": args.parent_id,
                "spiffe_id": item["spiffe_id"],
                "selector_type": selector_type,
                "selector_value": selector_value,
                "x509_svid_ttl_seconds": int(item["x509_svid_ttl_seconds"]),
            }
            desired_entries.append(desired_entry)

            existing = spire_entries.get(item["entry_id"])
            if existing is None:
                missing_registrations.append(desired_entry)
                continue

            existing_selectors = {
                (selector.get("type", ""), selector.get("value", ""))
                for selector in existing.get("selectors", [])
            }
            drift_reasons: list[str] = []
            if spiffe_id(existing.get("spiffe_id", {})) != item["spiffe_id"]:
                drift_reasons.append("spiffe_id")
            if spiffe_id(existing.get("parent_id", {})) != args.parent_id:
                drift_reasons.append("parent_id")
            if int(existing.get("x509_svid_ttl", 0)) != int(item["x509_svid_ttl_seconds"]):
                drift_reasons.append("x509_svid_ttl")
            if (selector_type, selector_value) not in existing_selectors:
                drift_reasons.append("selector")
            if drift_reasons:
                drifted = dict(desired_entry)
                drifted["reasons"] = drift_reasons
                drifted_registrations.append(drifted)

        result = {
            "converged": not (
                missing_groups
                or missing_users
                or drifted_users
                or missing_registrations
                or drifted_registrations
            ),
            "desired_count": len(desired),
            "existing_entry_count": len(spire_entries),
            "missing_groups": missing_groups,
            "missing_users": missing_users,
            "drifted_users": drifted_users,
            "missing_registrations": missing_registrations,
            "drifted_registrations": drifted_registrations,
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
