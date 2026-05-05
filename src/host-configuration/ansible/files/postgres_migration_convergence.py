#!/usr/bin/env python3
import argparse
import json
import os
import re
import sys
from pathlib import Path

import psycopg2
from psycopg2 import sql


MIGRATION_RE = re.compile(r"^(\d+)_.+\.up\.sql$")


def load_password() -> str:
    raw = os.environ.get("POSTGRES_ADMIN_PASSWORD", "").strip()
    if raw == "":
        raise ValueError("POSTGRES_ADMIN_PASSWORD is required")
    return raw


def migration_rows(path: Path) -> list[tuple[int, Path]]:
    rows: list[tuple[int, Path]] = []
    for entry in sorted(path.glob("*.up.sql")):
        match = MIGRATION_RE.match(entry.name)
        if match is None:
            raise ValueError(f"{entry}: migration filename must match NNN_name.up.sql")
        rows.append((int(match.group(1)), entry))
    if not rows:
        raise ValueError(f"{path}: no *.up.sql migrations found")
    versions = [version for version, _ in rows]
    if len(set(versions)) != len(versions):
        raise ValueError(f"{path}: duplicate migration version")
    return rows


def set_local_role(cur, owner: str) -> None:
    cur.execute(sql.SQL("SET LOCAL ROLE {}").format(sql.Identifier(owner)))


def ensure_schema_table(cur, owner: str) -> tuple[int, bool]:
    set_local_role(cur, owner)
    cur.execute(
        """
        CREATE TABLE IF NOT EXISTS schema_migrations (
            version BIGINT NOT NULL PRIMARY KEY,
            dirty BOOLEAN NOT NULL
        )
        """
    )
    cur.execute("SELECT version, dirty FROM schema_migrations ORDER BY version")
    rows = cur.fetchall()
    if len(rows) > 1:
        raise ValueError("schema_migrations has multiple rows; golang-migrate expects a single current version")
    if not rows:
        return 0, False
    version, dirty = rows[0]
    return int(version), bool(dirty)


def write_schema_version(conn, owner: str, version: int, dirty: bool) -> None:
    with conn.cursor() as cur:
        set_local_role(cur, owner)
        cur.execute("DELETE FROM schema_migrations")
        cur.execute(
            "INSERT INTO schema_migrations (version, dirty) VALUES (%s, %s)",
            (version, dirty),
        )
    conn.commit()


def apply_one(conn, owner: str, version: int, migration_path: Path) -> None:
    write_schema_version(conn, owner, version, True)
    try:
        with conn.cursor() as cur:
            set_local_role(cur, owner)
            cur.execute(migration_path.read_text())
            cur.execute("DELETE FROM schema_migrations")
            cur.execute(
                "INSERT INTO schema_migrations (version, dirty) VALUES (%s, false)",
                (version,),
            )
        conn.commit()
    except Exception:
        conn.rollback()
        raise


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", required=True)
    parser.add_argument("--port", required=True, type=int)
    parser.add_argument("--admin-user", required=True)
    parser.add_argument("--database", required=True)
    parser.add_argument("--owner", required=True)
    parser.add_argument("--service", required=True)
    parser.add_argument("--dir", required=True)
    args = parser.parse_args()

    try:
        admin_password = load_password()
        migrations = migration_rows(Path(args.dir))
        conn = psycopg2.connect(
            host=args.host,
            port=args.port,
            dbname=args.database,
            user=args.admin_user,
            password=admin_password,
            connect_timeout=1,
        )
        try:
            with conn.cursor() as cur:
                cur.execute("SELECT pg_advisory_lock(hashtextextended(%s, 0))", (f"verself:{args.service}:migrations",))
            conn.commit()

            with conn.cursor() as cur:
                current_version, dirty = ensure_schema_table(cur, args.owner)
            conn.commit()
            if dirty:
                raise ValueError(f"{args.service}: schema_migrations is dirty at version {current_version}")

            applied: list[int] = []
            for version, migration_path in migrations:
                if version <= current_version:
                    continue
                apply_one(conn, args.owner, version, migration_path)
                applied.append(version)
                current_version = version

            with conn.cursor() as cur:
                cur.execute("SELECT pg_advisory_unlock(hashtextextended(%s, 0))", (f"verself:{args.service}:migrations",))
            conn.commit()
        finally:
            conn.close()

        json.dump(
            {
                "service": args.service,
                "database": args.database,
                "owner": args.owner,
                "applied_versions": applied,
                "current_version": current_version,
            },
            sys.stdout,
            sort_keys=True,
        )
        sys.stdout.write("\n")
        return 0
    except Exception as exc:
        json.dump({"error": str(exc), "service": args.service, "database": args.database}, sys.stderr, sort_keys=True)
        sys.stderr.write("\n")
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
