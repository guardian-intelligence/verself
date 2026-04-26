"""Enforce that port numbers live only in generated topology artifacts."""

from __future__ import annotations

import re
from pathlib import Path

from ansiblelint.file_utils import Lintable
from ansiblelint.rules import AnsibleLintRule


# Only generated topology artifacts are allowed to define *_port variables.
GENERATED_DIR = Path("group_vars/all/generated")

# Key name ending with _port at the top level of a YAML mapping.
PORT_KEY_RE = re.compile(r"^(\s*\w*_port)\s*:")


class NoDefaultPortsRule(AnsibleLintRule):
    """Port variables must be defined in generated topology, not in role defaults or group_vars."""

    id = "no-default-ports"
    description = (
        "All service port numbers belong in group_vars/all/generated topology artifacts. "
        "Defining *_port variables in role defaults/ or other group_vars "
        "files re-introduces the scattered-port problem."
    )
    severity = "HIGH"
    tags = ["custom", "services"]
    version_changed = "0.1.0"

    # File kinds that declare variables.
    _var_kinds = {"vars"}

    def matchyaml(self, file: Lintable) -> list:
        """Scan variable files for *_port key definitions."""
        results = []

        if file.kind not in self._var_kinds:
            return results

        # Skip generated topology artifacts.
        try:
            if (Path.cwd() / GENERATED_DIR).resolve() in file.path.resolve().parents:
                return results
        except (OSError, ValueError):
            pass

        content = file.content
        for lineno, line in enumerate(content.splitlines(), start=1):
            m = PORT_KEY_RE.match(line)
            if m:
                results.append(
                    self.create_matcherror(
                        message=(
                            f"Port variable '{m.group(1).strip()}' defined outside "
                            f"generated topology artifacts — move to {GENERATED_DIR}"
                        ),
                        lineno=lineno,
                        filename=file,
                    )
                )
        return results
