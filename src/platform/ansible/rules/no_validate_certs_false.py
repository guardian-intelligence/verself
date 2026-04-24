"""Disallow disabling TLS certificate validation on Ansible URI calls."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

import yaml
from ansiblelint.file_utils import Lintable
from ansiblelint.rules import AnsibleLintRule


TASK_KINDS = {"tasks", "handlers", "playbook"}
URI_MODULE_KEYS = {"uri", "ansible.builtin.uri"}
TASK_CONTROL_KEYS = {
    "action",
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


@dataclass(frozen=True)
class TaskNode:
    task: dict[str, Any]
    line: int


def _scalar(node: yaml.Node) -> str | None:
    return node.value if isinstance(node, yaml.ScalarNode) else None


def _task_nodes(content: str) -> list[TaskNode]:
    data = yaml.safe_load(content)
    if data is None:
        return []

    root = yaml.compose(content)
    if root is None:
        return []

    tasks: list[TaskNode] = []

    def record_task(value: Any, node: yaml.Node) -> None:
        if isinstance(value, dict):
            tasks.append(TaskNode(value, node.start_mark.line + 1))

    def walk_tasks(value: Any, node: yaml.Node) -> None:
        if isinstance(value, list) and isinstance(node, yaml.SequenceNode):
            for item_value, item_node in zip(value, node.value, strict=False):
                record_task(item_value, item_node)
                if isinstance(item_value, dict) and isinstance(item_node, yaml.MappingNode):
                    for key_node, child_node in item_node.value:
                        key = _scalar(key_node)
                        if key in {"block", "rescue", "always"}:
                            walk_tasks(item_value.get(key), child_node)
        elif isinstance(value, dict) and isinstance(node, yaml.MappingNode):
            for key_node, child_node in node.value:
                key = _scalar(key_node)
                if key == "tasks":
                    walk_tasks(value.get(key), child_node)

    if isinstance(data, list):
        if isinstance(root, yaml.SequenceNode) and data and all(isinstance(item, dict) and "hosts" in item for item in data):
            for play_value, play_node in zip(data, root.value, strict=False):
                if isinstance(play_value, dict) and isinstance(play_node, yaml.MappingNode):
                    for key_node, child_node in play_node.value:
                        key = _scalar(key_node)
                        if key == "tasks":
                            walk_tasks(play_value.get(key), child_node)
        else:
            walk_tasks(data, root)
    elif isinstance(data, dict):
        walk_tasks(data.get("tasks"), root)

    return tasks


def _uri_args(task: dict[str, Any]) -> dict[str, Any] | None:
    action = task.get("action")
    if isinstance(action, dict):
        module = action.get("__ansible_module__") or action.get("module")
        if module in URI_MODULE_KEYS:
            return action
    elif isinstance(action, str) and action.split(maxsplit=1)[0] in URI_MODULE_KEYS:
        return {}

    for key, value in task.items():
        if key in TASK_CONTROL_KEYS:
            continue
        if key in URI_MODULE_KEYS:
            return value if isinstance(value, dict) else {}
    return None


def _is_false(value: Any) -> bool:
    return value is False or (isinstance(value, str) and value.strip().lower() == "false")


class NoValidateCertsFalseRule(AnsibleLintRule):
    """URI calls must validate TLS certificates."""

    id = "no-validate-certs-false"
    description = "Do not set validate_certs: false on ansible.builtin.uri; pin a CA with ca_path instead."
    severity = "HIGH"
    tags = ["custom", "security"]
    version_changed = "0.1.0"

    def matchyaml(self, file: Lintable) -> list:
        if file.kind not in TASK_KINDS:
            return []

        try:
            tasks = _task_nodes(file.content)
        except yaml.YAMLError:
            return []

        results = []
        for task_node in tasks:
            args = _uri_args(task_node.task)
            if args is None or not _is_false(args.get("validate_certs")):
                continue
            results.append(
                self.create_matcherror(
                    message="uri task disables TLS certificate validation; use ca_path with the pinned local CA",
                    lineno=task_node.line,
                    filename=file,
                )
            )
        return results
