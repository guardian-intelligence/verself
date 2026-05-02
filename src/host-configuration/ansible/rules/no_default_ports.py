"""Reject ad-hoc `*_port` variables outside the authored topology contract."""

from __future__ import annotations

import re

from ansiblelint.file_utils import Lintable
from ansiblelint.rules import AnsibleLintRule


# Key name ending with _port at the top level of a YAML mapping.
PORT_KEY_RE = re.compile(r"^(\s*\w*_port)\s*:")


class NoDefaultPortsRule(AnsibleLintRule):
    """Port variables must be defined in topology, not role defaults."""

    id = "no-default-ports"
    description = (
        "All service port numbers belong in the authored topology contract. Defining "
        "`*_port` variables in role `defaults/` or hand-written `group_vars/` "
        "files re-introduces scattered port ownership."
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

        # The rule's domain is role/default variable files; topology-owned
        # group_vars are allowed to carry concrete ports.
        content = file.content
        for lineno, line in enumerate(content.splitlines(), start=1):
            m = PORT_KEY_RE.match(line)
            if m:
                results.append(
                    self.create_matcherror(
                        message=(
                            f"Port variable '{m.group(1).strip()}' defined outside "
                            "the topology contract — declare it in "
                            "src/host-configuration/ansible/group_vars/all/generated/ instead."
                        ),
                        lineno=lineno,
                        filename=file,
                    )
                )
        return results
