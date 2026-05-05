"""Prevent masterkeys and encryption keys from appearing as CLI arguments."""

from __future__ import annotations

import re

from ansiblelint.file_utils import Lintable
from ansiblelint.rules import AnsibleLintRule

# Matches --masterkey followed by a value (variable expansion or literal),
# but NOT --masterkeyFile or --masterkeyFromEnv which are safe alternatives.
# Also catches $MASTERKEY or ${MASTERKEY} in ExecStart lines, which systemd
# expands into /proc/<pid>/cmdline.
MASTERKEY_CLI_RE = re.compile(
    r"--masterkey\s+(?!File|FromEnv)"  # --masterkey <value>
    r"|--masterkey\s*=\s*(?!File|FromEnv)"  # --masterkey=<value>
    r"|\$\{?MASTERKEY\}?"  # ${MASTERKEY} or $MASTERKEY in ExecStart
)


class NoMasterkeyCLIRule(AnsibleLintRule):
    """Masterkeys must not be passed as CLI arguments or systemd env expansions."""

    id = "no-masterkey-cli"
    description = (
        "Passing a masterkey via --masterkey <value> or ${MASTERKEY} in a "
        "systemd ExecStart line exposes the secret in /proc/<pid>/cmdline "
        "and ps output. Use --masterkeyFile <path> instead."
    )
    severity = "HIGH"
    tags = ["custom", "security"]
    version_changed = "0.1.0"

    def matchyaml(self, file: Lintable) -> list:
        """Scan task and template files for masterkey CLI exposure."""
        results = []

        if file.kind not in {"tasks", "handlers", "playbook"}:
            return results

        content = file.content
        for lineno, line in enumerate(content.splitlines(), start=1):
            m = MASTERKEY_CLI_RE.search(line)
            if m:
                results.append(
                    self.create_matcherror(
                        message=(
                            f"Masterkey exposed as CLI argument ({m.group()!r}) — "
                            f"use --masterkeyFile <path> instead"
                        ),
                        lineno=lineno,
                        filename=file,
                    )
                )
        return results