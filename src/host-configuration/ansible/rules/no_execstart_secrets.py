"""Prevent secrets from being passed through systemd ExecStart command lines."""

from __future__ import annotations

import re

from ansiblelint.file_utils import Lintable
from ansiblelint.rules import AnsibleLintRule


EXECSTART_RE = re.compile(r"^\s*ExecStart(?:Pre|Post)?=")
SECRET_ASSIGN_RE = re.compile(
    r"(?i)(?:^|\s)(?:-e\s+|--env\s+)?[A-Z0-9_]*(?:SECRET|TOKEN|PASSWORD|API[_-]?KEY)[A-Z0-9_]*\s*="
)
DATABASE_URL_WITH_CREDENTIALS_RE = re.compile(
    r"(?i)(?:^|\s)(?:-e\s+|--env\s+)?DATABASE_URL\s*=\s*"
    r"(?:postgres(?:ql)?|mysql|mariadb|redis|amqp|mongodb)://[^\s\\]*:[^\s\\@]+@"
)


class NoExecStartSecretsRule(AnsibleLintRule):
    """Systemd command lines must not carry credentials."""

    id = "no-execstart-secrets"
    description = (
        "ExecStart command lines are visible in unit text and process metadata; "
        "pass credentials through root-owned files or systemd credentials instead."
    )
    severity = "HIGH"
    tags = ["custom", "security"]
    version_changed = "0.1.0"

    def matchlines(self, file: Lintable) -> list:
        results = []
        lines = file.content.splitlines()
        index = 0
        while index < len(lines):
            line = lines[index]
            if not EXECSTART_RE.match(line):
                index += 1
                continue

            start_line = index + 1
            block = [line]
            while block[-1].rstrip().endswith("\\") and index + 1 < len(lines):
                index += 1
                block.append(lines[index])

            command = " ".join(part.strip().rstrip("\\").strip() for part in block)
            if SECRET_ASSIGN_RE.search(command) or DATABASE_URL_WITH_CREDENTIALS_RE.search(command):
                results.append(
                    self.create_matcherror(
                        message=(
                            "ExecStart contains secret-looking environment assignments; "
                            "move credentials into a root-owned file or systemd credential"
                        ),
                        lineno=start_line,
                        filename=file,
                    )
                )

            index += 1

        return results
