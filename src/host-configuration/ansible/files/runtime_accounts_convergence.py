#!/usr/bin/env python3
import argparse
import grp
import json
import os
import pwd
import sys


def load_units() -> list[dict]:
    raw = os.environ.get("COMPONENT_RUNTIME_ACCOUNT_UNITS", "")
    if raw == "":
        raise ValueError("COMPONENT_RUNTIME_ACCOUNT_UNITS is required")
    value = json.loads(raw)
    if not isinstance(value, list):
        raise ValueError("COMPONENT_RUNTIME_ACCOUNT_UNITS must be a JSON array")
    return value


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--workload-group", required=True)
    args = parser.parse_args()

    try:
        units = load_units()
        groups_by_name = {group.gr_name: group for group in grp.getgrall()}
        users_by_name = {user.pw_name: user for user in pwd.getpwall()}

        desired_primary_groups = sorted({unit["group"] for unit in units})
        desired_supplementary_groups = sorted(
            {
                group
                for unit in units
                for group in unit.get("supplementary_groups", [])
            }
            | {args.workload_group}
        )

        desired_by_user: dict[str, dict] = {}
        for unit in units:
            user_name = unit["user"]
            existing = desired_by_user.setdefault(
                user_name,
                {
                    "user": user_name,
                    "group": unit["group"],
                    "uid": unit.get("uid"),
                    "home": unit.get("home", ""),
                    "supplementary_groups": set(),
                },
            )
            if existing["group"] != unit["group"]:
                raise ValueError(f"user {user_name} has conflicting primary groups")
            if existing.get("uid") != unit.get("uid"):
                raise ValueError(f"user {user_name} has conflicting uid policy")
            if not existing["home"] and unit.get("home", ""):
                existing["home"] = unit["home"]
            existing["supplementary_groups"].update(unit.get("supplementary_groups", []))

        missing_groups = [
            {"group": group}
            for group in sorted(set(desired_primary_groups) | set(desired_supplementary_groups))
            if group not in groups_by_name
        ]

        actual_supplementary_by_user: dict[str, set[str]] = {}
        for group in groups_by_name.values():
            for member in group.gr_mem:
                actual_supplementary_by_user.setdefault(member, set()).add(group.gr_name)

        missing_users: list[dict] = []
        drifted_users: list[dict] = []
        for desired in desired_by_user.values():
            user_name = desired["user"]
            user = users_by_name.get(user_name)
            if user is None:
                missing_users.append({"user": user_name})
                continue

            reasons: list[str] = []
            desired_uid = desired.get("uid")
            if desired_uid is not None and int(desired_uid) != int(user.pw_uid):
                reasons.append("uid")

            primary_group = groups_by_name.get(desired["group"])
            if primary_group is not None and int(user.pw_gid) != int(primary_group.gr_gid):
                reasons.append("primary_group")

            expected_supplementary = set(desired["supplementary_groups"])
            actual_supplementary = actual_supplementary_by_user.get(user_name, set())
            if expected_supplementary != actual_supplementary:
                reasons.append("supplementary_groups")

            if user.pw_shell != "/usr/sbin/nologin":
                reasons.append("shell")

            desired_home = desired.get("home", "")
            if desired_home and user.pw_dir != desired_home:
                reasons.append("home")

            if reasons:
                drifted_users.append({"user": user_name, "reasons": reasons})

        result = {
            "converged": not (missing_groups or missing_users or drifted_users),
            "desired_user_count": len(desired_by_user),
            "desired_group_count": len(set(desired_primary_groups) | set(desired_supplementary_groups)),
            "missing_groups": missing_groups,
            "missing_users": missing_users,
            "drifted_users": drifted_users,
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
