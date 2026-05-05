"""Prefer community.postgresql modules over raw psql for durable object state."""

from __future__ import annotations

import re

from ansiblelint.file_utils import Lintable
from ansiblelint.rules import AnsibleLintRule

PSQL_RE = re.compile(r"{{\s*verself_bin\s*}}/psql\b|\bpsql\b")
TASK_START_RE = re.compile(r"^-\s+")
TASK_NAME_RE = re.compile(r"^-\s+name:\s*(.+?)\s*$")

# File-local exceptions for deliberately low-level SQL paths.
# The electric reconcile uses a raw psql DO block on purpose: an earlier
# module-based version silently corrupted ANY() comparisons on positional_args
# arrays (the loop no-opped and shape tables stayed on relreplident='d').
# See the comment block at the top of roles/electric/tasks/pg_setup.yml.
ALLOWED_TASKS = {
    "roles/zitadel/tasks/main.yml": {
        "Check if Login V2 is currently required",
        "Disable Login V2 via event store (use embedded Login V1)",
    },
    "roles/electric/tasks/pg_setup.yml": {
        "Reconcile publication, ownership, and replica identity for {{ electric_service_name }}",
    },
}

PROTECTED_FILES = {
    "roles/postgresql/tasks/main.yml",
    "roles/mailbox_service/tasks/database.yml",
    "roles/zitadel/tasks/main.yml",
    "roles/electric/tasks/pg_setup.yml",
}


def _normalize_task_name(raw_name: str) -> str:
    """Strip YAML quoting around a task name when present."""
    task_name = raw_name.strip()
    if len(task_name) >= 2 and task_name[0] == task_name[-1] and task_name[0] in {"'", '"'}:
        return task_name[1:-1]
    return task_name


class NoPsqlObjectStateRule(AnsibleLintRule):
    """Durable PostgreSQL object state must use community.postgresql modules."""

    id = "no-psql-object-state"
    description = (
        "Use community.postgresql modules for durable PostgreSQL object state "
        "(roles, databases, ownership, privileges, publications) instead of raw psql."
    )
    severity = "HIGH"
    tags = ["custom", "postgresql"]
    version_changed = "0.1.0"

    def matchyaml(self, file: Lintable) -> list:
        """Scan protected task files for raw psql usage outside allowlisted tasks."""
        if file.kind not in {"tasks", "handlers", "playbook"}:
            return []

        rel_path = str(file.path).replace("\\", "/")
        target = next((path for path in PROTECTED_FILES if rel_path.endswith(path)), None)
        if target is None:
            return []

        results = []
        current_name: str | None = None
        current_block: list[tuple[int, str]] = []
        current_start_line = 1
        allowed_tasks = ALLOWED_TASKS.get(target, set())

        def flush_current_block() -> None:
            if not current_block or current_name in allowed_tasks:
                return

            for lineno, line in current_block:
                if PSQL_RE.search(line.split("#", 1)[0]):
                    task_display = current_name or f"task starting on line {current_start_line}"
                    results.append(
                        self.create_matcherror(
                            message=(
                                f"Raw psql is not allowed for PostgreSQL object state in "
                                f"{task_display}; use community.postgresql modules instead"
                            ),
                            lineno=lineno,
                            filename=file,
                        )
                    )
                    return

        for lineno, line in enumerate(file.content.splitlines(), start=1):
            if TASK_START_RE.match(line):
                flush_current_block()
                current_block = [(lineno, line)]
                current_start_line = lineno
                name_match = TASK_NAME_RE.match(line)
                current_name = _normalize_task_name(name_match.group(1)) if name_match else None
            elif current_block:
                current_block.append((lineno, line))

        flush_current_block()
        return results
