"""Keep the canonical site playbook as an orchestration graph."""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any

import yaml
from ansiblelint.file_utils import Lintable
from ansiblelint.rules import AnsibleLintRule


SITE_PLAYBOOK = Path("playbooks/site.yml")

ALLOWED_MODULE_KEYS = {
    "ansible.builtin.import_tasks",
    "ansible.builtin.include_tasks",
    "ansible.builtin.import_role",
    "ansible.builtin.include_role",
    "import_tasks",
    "include_tasks",
    "import_role",
    "include_role",
    "meta",
    "ansible.builtin.meta",
}

TASK_CONTROL_KEYS = {
    "always",
    "become",
    "become_user",
    "block",
    "changed_when",
    "check_mode",
    "delegate_to",
    "environment",
    "failed_when",
    "ignore_errors",
    "loop",
    "loop_control",
    "name",
    "no_log",
    "notify",
    "register",
    "rescue",
    "tags",
    "vars",
    "when",
    "with_items",
}

BLOCK_TASK_KEYS = {"block", "rescue", "always"}


@dataclass(frozen=True)
class TaskNode:
    task: dict[str, Any]
    line: int


def _scalar(node: yaml.Node) -> str | None:
    return node.value if isinstance(node, yaml.ScalarNode) else None


def _sequence_child(node: yaml.Node, key: str) -> yaml.SequenceNode | None:
    if not isinstance(node, yaml.MappingNode):
        return None
    for key_node, child_node in node.value:
        if _scalar(key_node) == key and isinstance(child_node, yaml.SequenceNode):
            return child_node
    return None


def _collect_tasks(task_values: list[Any], task_node: yaml.SequenceNode) -> list[TaskNode]:
    tasks: list[TaskNode] = []
    for task_value, child_node in zip(task_values, task_node.value, strict=False):
        if not isinstance(task_value, dict):
            continue
        tasks.append(TaskNode(task_value, child_node.start_mark.line + 1))
        for block_key in BLOCK_TASK_KEYS:
            nested_values = task_value.get(block_key)
            nested_node = _sequence_child(child_node, block_key)
            if isinstance(nested_values, list) and nested_node is not None:
                tasks.extend(_collect_tasks(nested_values, nested_node))
    return tasks


def _site_tasks(content: str) -> list[TaskNode]:
    data = yaml.safe_load(content)
    root = yaml.compose(content)
    if data is None or root is None:
        return []
    if not isinstance(data, list) or not isinstance(root, yaml.SequenceNode):
        return []

    tasks: list[TaskNode] = []
    for play_value, play_node in zip(data, root.value, strict=False):
        if not isinstance(play_value, dict) or not isinstance(play_node, yaml.MappingNode):
            continue
        for key_node, child_node in play_node.value:
            if _scalar(key_node) != "tasks":
                continue
            task_values = play_value.get("tasks")
            if not isinstance(task_values, list) or not isinstance(child_node, yaml.SequenceNode):
                continue
            tasks.extend(_collect_tasks(task_values, child_node))
    return tasks


def _module_keys(task: dict[str, Any]) -> set[str]:
    return {key for key in task if key not in TASK_CONTROL_KEYS}


class SiteOrchestratesOnlyRule(AnsibleLintRule):
    """site.yml may sequence roles and imported task files only."""

    id = "site-orchestrates-only"
    description = (
        "playbooks/site.yml is the host convergence graph. Put executable "
        "task bodies in roles or imported task files, then orchestrate them "
        "from site.yml."
    )
    severity = "HIGH"
    tags = ["custom", "architecture"]
    version_changed = "0.1.0"

    def matchyaml(self, file: Lintable) -> list:
        if file.kind != "playbook":
            return []

        rel_path = Path(str(file.path)).as_posix()
        if not rel_path.endswith(SITE_PLAYBOOK.as_posix()):
            return []

        results = []
        for node in _site_tasks(file.content):
            module_keys = _module_keys(node.task)
            if module_keys and not module_keys <= ALLOWED_MODULE_KEYS:
                results.append(
                    self.create_matcherror(
                        message=(
                            "site.yml task executes modules directly "
                            f"({', '.join(sorted(module_keys))}); import a task file or role instead"
                        ),
                        lineno=node.line,
                        filename=file,
                    )
                )
        return results
