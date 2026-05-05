#!/usr/bin/env python3
import argparse
import json
import os
import sys

import psycopg2


def load_json_env(name: str):
    raw = os.environ.get(name, "")
    if raw == "":
        raise ValueError(f"{name} is required")
    return json.loads(raw)


def resolve_password(component: dict, ansible_vars: dict, exposed_values: dict) -> str | None:
    password_ref = component.get("postgres", {}).get("password_ref", {})
    kind = password_ref.get("kind")
    if kind == "ansible_var":
        return ansible_vars.get(password_ref["name"])
    if kind == "secret_ref":
        return exposed_values.get(component["name"], {}).get(password_ref["expose_as"])
    return None


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", required=True)
    parser.add_argument("--port", required=True, type=int)
    parser.add_argument("--admin-user", required=True)
    args = parser.parse_args()

    try:
        components = [
            component
            for component in load_json_env("COMPONENT_POSTGRES_DESIRED")
            if component.get("postgres", {}).get("database", "")
        ]
        ansible_vars = load_json_env("COMPONENT_POSTGRES_ANSIBLE_VARS")
        exposed_values = load_json_env("COMPONENT_POSTGRES_EXPOSED_VALUES")
        admin_password = os.environ.get("COMPONENT_POSTGRES_ADMIN_PASSWORD", "")
        if admin_password == "":
            raise ValueError("COMPONENT_POSTGRES_ADMIN_PASSWORD is required")

        admin_conn = psycopg2.connect(
            host=args.host,
            port=args.port,
            dbname="postgres",
            user=args.admin_user,
            password=admin_password,
            connect_timeout=1,
        )
        admin_conn.autocommit = True
        with admin_conn.cursor() as cur:
            role_names = [component["postgres"]["owner"] for component in components]
            db_names = [component["postgres"]["database"] for component in components]
            cur.execute(
                "SELECT rolname, rolcanlogin, rolconnlimit FROM pg_roles WHERE rolname = ANY(%s)",
                (role_names,),
            )
            roles = {row[0]: {"canlogin": row[1], "connlimit": row[2]} for row in cur.fetchall()}
            cur.execute(
                """
                SELECT datname, pg_catalog.pg_get_userbyid(datdba) AS owner
                FROM pg_database
                WHERE datname = ANY(%s)
                """,
                (db_names,),
            )
            databases = {row[0]: {"owner": row[1]} for row in cur.fetchall()}
        admin_conn.close()

        missing_roles: list[dict] = []
        missing_databases: list[dict] = []
        drifted_roles: list[dict] = []
        drifted_databases: list[dict] = []
        failed_password_checks: list[dict] = []

        for component in components:
            postgres = component["postgres"]
            owner = postgres["owner"]
            database = postgres["database"]

            role = roles.get(owner)
            if role is None:
                missing_roles.append({"component": component["name"], "role": owner})
            else:
                reasons: list[str] = []
                if not role["canlogin"]:
                    reasons.append("login")
                if int(role["connlimit"]) != int(postgres["connection_limit"]):
                    reasons.append("connection_limit")
                if reasons:
                    drifted_roles.append(
                        {"component": component["name"], "role": owner, "reasons": reasons}
                    )

            db = databases.get(database)
            if db is None:
                missing_databases.append({"component": component["name"], "database": database})
            elif db["owner"] != owner:
                drifted_databases.append(
                    {"component": component["name"], "database": database, "reasons": ["owner"]}
                )

            password = resolve_password(component, ansible_vars, exposed_values)
            if role is not None and db is not None:
                if password is None:
                    failed_password_checks.append(
                        {"component": component["name"], "role": owner, "reason": "password_unresolved"}
                    )
                else:
                    try:
                        conn = psycopg2.connect(
                            host=args.host,
                            port=args.port,
                            dbname=database,
                            user=owner,
                            password=password,
                            connect_timeout=1,
                        )
                        conn.close()
                    except Exception:
                        failed_password_checks.append(
                            {"component": component["name"], "role": owner, "reason": "password_auth"}
                        )

        result = {
            "converged": not (
                missing_roles
                or missing_databases
                or drifted_roles
                or drifted_databases
                or failed_password_checks
            ),
            "binding_count": len(components),
            "missing_roles": missing_roles,
            "missing_databases": missing_databases,
            "drifted_roles": drifted_roles,
            "drifted_databases": drifted_databases,
            "failed_password_checks": failed_password_checks,
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
