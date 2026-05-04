#!/usr/bin/env python3
"""Validate authored topology endpoint invariants.

Checked here:
  - port uniqueness across the whole topology
  - control-plane port range membership for a named service set
  - wildcard listen_host must use one of public/wireguard/guest_host
"""

from __future__ import annotations

import argparse
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import yaml

try:
    from ansiblelint.file_utils import Lintable
    from ansiblelint.rules import AnsibleLintRule
except ModuleNotFoundError:
    Lintable = None  # type: ignore[assignment]
    AnsibleLintRule = object  # type: ignore[assignment,misc]


REGISTRY_RELATIVE = Path("group_vars/all/topology/endpoints.yml")
CONTROL_PLANE_PORT_MIN = 4240
CONTROL_PLANE_PORT_MAX = 4269
PORT_KEY_RE = re.compile(r"^port$")

CONTROL_PLANE_SERVICES = {
    "billing",
    "company",
    "governance_service",
    "iam_service",
    "mailbox_service",
    "notifications_service",
    "object_storage_service",
    "profile_service",
    "projects_service",
    "sandbox_rental",
    "secrets_service",
    "source_code_hosting_service",
    "verself_web",
}

WILDCARD_LISTEN_EXPOSURES = {"public", "wireguard", "guest_host"}


@dataclass(frozen=True)
class RegistryIssue:
    message: str
    line: int = 1


@dataclass(frozen=True)
class PortAllocation:
    port: int
    path: tuple[str, ...]
    line: int


def resolve_registry_path(path: str | Path | None = None) -> Path:
    """Locate the authored endpoints.yml.

    Order of preference:
      1. Explicit `path` argument (used by `main()` and direct test invocation).
      2. CWD-relative `group_vars/all/topology/endpoints.yml`, used by
         ansible-lint when it runs from the authored Ansible directory.
    """
    if path is not None:
        return Path(path)

    return Path.cwd() / REGISTRY_RELATIVE


def dotted(path: tuple[str, ...]) -> str:
    rendered = ""
    for segment in path:
        if segment.startswith("["):
            rendered += segment
        elif rendered:
            rendered += f".{segment}"
        else:
            rendered = segment
    return rendered


def line_for(lines: dict[tuple[str, ...], int], path: tuple[str, ...]) -> int:
    while path:
        if path in lines:
            return lines[path]
        path = path[:-1]
    return 1


def collect_yaml_lines(path: Path) -> dict[tuple[str, ...], int]:
    root = yaml.compose(path.read_text(encoding="utf-8"))
    lines: dict[tuple[str, ...], int] = {}

    def walk(node: yaml.Node, current: tuple[str, ...]) -> None:
        lines.setdefault(current, node.start_mark.line + 1)
        if isinstance(node, yaml.MappingNode):
            for key_node, value_node in node.value:
                if not isinstance(key_node, yaml.ScalarNode):
                    continue
                child = (*current, str(key_node.value))
                lines[child] = key_node.start_mark.line + 1
                walk(value_node, child)
        elif isinstance(node, yaml.SequenceNode):
            for index, value_node in enumerate(node.value):
                child = (*current, f"[{index}]")
                lines[child] = value_node.start_mark.line + 1
                walk(value_node, child)

    if root is not None:
        walk(root, ())
    return lines


def load_registry(path: Path) -> tuple[dict[str, Any], dict[tuple[str, ...], int]]:
    try:
        data = yaml.safe_load(path.read_text(encoding="utf-8"))
        lines = collect_yaml_lines(path)
    except yaml.YAMLError as exc:
        mark = getattr(exc, "problem_mark", None)
        line = 1 if mark is None else mark.line + 1
        return {}, {("__error__",): line}

    if not isinstance(data, dict):
        return {}, lines
    return data, lines


def collect_ports(value: Any, base: tuple[str, ...], lines: dict[tuple[str, ...], int]) -> list[PortAllocation]:
    allocations: list[PortAllocation] = []
    if isinstance(value, dict):
        for key, child in value.items():
            child_path = (*base, str(key))
            if PORT_KEY_RE.match(str(key)):
                if isinstance(child, bool):
                    continue
                if isinstance(child, int):
                    port = child
                elif isinstance(child, str) and child.strip().isdigit():
                    port = int(child.strip())
                else:
                    continue
                allocations.append(PortAllocation(port, child_path, line_for(lines, child_path)))
                continue
            allocations.extend(collect_ports(child, child_path, lines))
    elif isinstance(value, list):
        for index, child in enumerate(value):
            allocations.extend(collect_ports(child, (*base, f"[{index}]"), lines))
    return allocations


def validate_registry(path: str | Path | None = None) -> list[RegistryIssue]:
    registry_path = resolve_registry_path(path)
    data, lines = load_registry(registry_path)
    issues: list[RegistryIssue] = []

    if not data:
        return [RegistryIssue(f"{registry_path} is empty or is not a YAML mapping", line_for(lines, ("__error__",)))]

    topology_endpoints = data.get("topology_endpoints")
    if not isinstance(topology_endpoints, dict):
        return [
            RegistryIssue(
                "topology endpoint artifact must define a top-level topology_endpoints mapping",
                line_for(lines, ("topology_endpoints",)),
            )
        ]

    allocations = collect_ports(topology_endpoints, ("topology_endpoints",), lines)
    by_port: dict[int, list[PortAllocation]] = {}
    for allocation in allocations:
        by_port.setdefault(allocation.port, []).append(allocation)

    for port, matches in sorted(by_port.items()):
        if len(matches) > 1:
            paths = ", ".join(dotted(match.path) for match in matches)
            issues.append(
                RegistryIssue(
                    f"duplicate topology endpoint port {port}: {paths}",
                    min(match.line for match in matches),
                )
            )

    for allocation in allocations:
        component = allocation.path[1] if len(allocation.path) > 1 else ""
        if component in CONTROL_PLANE_SERVICES and not (
            CONTROL_PLANE_PORT_MIN <= allocation.port <= CONTROL_PLANE_PORT_MAX
        ):
            issues.append(
                RegistryIssue(
                    f"{dotted(allocation.path)} uses {allocation.port}; Verself control-plane ports must stay in "
                    f"{CONTROL_PLANE_PORT_MIN}-{CONTROL_PLANE_PORT_MAX} unless the service is upstream-fixed",
                    allocation.line,
                )
            )

    def check_wildcard_listens(value: Any, base: tuple[str, ...]) -> None:
        if isinstance(value, dict):
            for key, child in value.items():
                child_path = (*base, str(key))
                if str(key) == "listen_host" and child == "0.0.0.0":
                    exposure = value.get("exposure")
                    if exposure not in WILDCARD_LISTEN_EXPOSURES:
                        issues.append(
                            RegistryIssue(
                                f"{dotted(child_path)} wildcard bind must use one of "
                                f"{sorted(WILDCARD_LISTEN_EXPOSURES)} exposure values, got {exposure!r}",
                                line_for(lines, child_path),
                            )
                        )
                check_wildcard_listens(child, child_path)
        elif isinstance(value, list):
            for index, child in enumerate(value):
                check_wildcard_listens(child, (*base, f"[{index}]"))

    check_wildcard_listens(topology_endpoints, ("topology_endpoints",))
    return sorted(issues, key=lambda issue: (issue.line, issue.message))


if Lintable is not None:

    class ServicesRegistryContractRule(AnsibleLintRule):
        """Topology endpoint invariants."""

        id = "services-registry-contract"
        description = "Validate authored topology endpoints.yml (port uniqueness, control-plane port range, wildcard exposure)."
        severity = "HIGH"
        tags = ["custom", "services"]
        version_changed = "0.2.0"

        _checked = False

        def matchyaml(self, file: Lintable) -> list:
            if ServicesRegistryContractRule._checked:
                return []
            ServicesRegistryContractRule._checked = True

            registry = resolve_registry_path()
            return [
                self.create_matcherror(
                    message=issue.message,
                    lineno=issue.line,
                    filename=Lintable(str(registry)),
                )
                for issue in validate_registry(registry)
            ]


def main() -> int:
    parser = argparse.ArgumentParser(description="Validate the Verself topology endpoint artifact.")
    parser.add_argument("path", nargs="?", help=f"registry path, default: ./{REGISTRY_RELATIVE}")
    args = parser.parse_args()

    registry = resolve_registry_path(args.path)
    issues = validate_registry(registry)
    for issue in issues:
        print(f"{registry}:{issue.line}: {issue.message}", file=sys.stderr)
    return 1 if issues else 0


if __name__ == "__main__":
    raise SystemExit(main())
