"""Reject `*_port` variables defined outside CUE-rendered topology."""

from __future__ import annotations

import re

from ansiblelint.file_utils import Lintable
from ansiblelint.rules import AnsibleLintRule


# Key name ending with _port at the top level of a YAML mapping.
PORT_KEY_RE = re.compile(r"^(\s*\w*_port)\s*:")


class NoDefaultPortsRule(AnsibleLintRule):
    """Port variables must be defined in CUE topology, not in role defaults or hand-written group_vars."""

    id = "no-default-ports"
    description = (
        "All service port numbers belong in CUE topology and are projected via "
        "`aspect render --site=<site>` into the per-site deploy cache. Defining "
        "`*_port` variables in role `defaults/` or hand-written `group_vars/` "
        "files re-introduces the scattered-port problem CUE was adopted to fix."
    )
    severity = "HIGH"
    tags = ["custom", "services"]
    version_changed = "0.2.0"

    # File kinds that declare variables.
    _var_kinds = {"vars"}

    def matchyaml(self, file: Lintable) -> list:
        """Scan variable files for *_port key definitions."""
        results = []

        if file.kind not in self._var_kinds:
            return results

        # Cache-rendered files live outside ansible-lint's cwd (.cache/render/<site>/)
        # so they are never visited here; the rule's domain is the authored
        # tree only.
        content = file.content
        for lineno, line in enumerate(content.splitlines(), start=1):
            m = PORT_KEY_RE.match(line)
            if m:
                results.append(
                    self.create_matcherror(
                        message=(
                            f"Port variable '{m.group(1).strip()}' defined outside "
                            "CUE-rendered topology — declare it in src/cue-renderer/ "
                            "instead so the renderer projects it into the deploy cache."
                        ),
                        lineno=lineno,
                        filename=file,
                    )
                )
        return results
