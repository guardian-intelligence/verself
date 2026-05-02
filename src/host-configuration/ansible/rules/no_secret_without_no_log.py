"""Require no_log on tasks that directly handle bearer secrets."""

from __future__ import annotations

import re
from dataclasses import dataclass
from typing import Any

import yaml
from ansiblelint.file_utils import Lintable
from ansiblelint.rules import AnsibleLintRule


TASK_KINDS = {"tasks", "handlers", "playbook"}
MODULE_KEYS = {
    "slurp": "slurp",
    "ansible.builtin.slurp": "slurp",
    "uri": "uri",
    "ansible.builtin.uri": "uri",
    "set_fact": "set_fact",
    "ansible.builtin.set_fact": "set_fact",
}
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

SECRET_PATH_RE = re.compile(r"(token|password|secret|key|admin\.pat|root-token|unseal-key)", re.IGNORECASE)
SECRET_FACT_RE = re.compile(r"(token|password|secret|key|admin_pat|root_token|unseal_key)", re.IGNORECASE)
SLURP_CONTENT_RE = re.compile(r"(?:\.content|\[['\"]content['\"]\])")
URI_SECRET_KEYS = {"authorization", "x-vault-token", "url_password", "login_password", "password"}
URI_SECRET_VALUE_RE = re.compile(r"\bBearer\b", re.IGNORECASE)


@dataclass(frozen=True)
class TaskNode:
    task: dict[str, Any]
    line: int


def _is_no_log_true(task: dict[str, Any]) -> bool:
    value = task.get("no_log")
    return value is True or (isinstance(value, str) and value.strip().lower() == "true")


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


def _module(task: dict[str, Any]) -> tuple[str, Any] | None:
    action = task.get("action")
    if isinstance(action, str):
        name = action.split(maxsplit=1)[0]
        if name in MODULE_KEYS:
            return MODULE_KEYS[name], {}
    elif isinstance(action, dict):
        name = action.get("__ansible_module__") or action.get("module")
        if isinstance(name, str) and name in MODULE_KEYS:
            return MODULE_KEYS[name], action

    for key, value in task.items():
        if key in TASK_CONTROL_KEYS:
            continue
        if key in MODULE_KEYS:
            return MODULE_KEYS[key], value
    return None


def _as_mapping(value: Any) -> dict[str, Any]:
    return value if isinstance(value, dict) else {}


def _stringify(value: Any) -> str:
    if isinstance(value, str):
        return value
    if isinstance(value, dict):
        return " ".join(f"{key} {_stringify(child)}" for key, child in value.items())
    if isinstance(value, list):
        return " ".join(_stringify(child) for child in value)
    return ""


def _uri_secret_paths(value: Any, path: tuple[str, ...] = ()) -> list[str]:
    matches: list[str] = []
    if isinstance(value, dict):
        for key, child in value.items():
            child_path = (*path, str(key))
            key_l = str(key).lower()
            child_s = _stringify(child)
            if key_l in URI_SECRET_KEYS or (key_l == "headers" and _uri_secret_paths(child, child_path)):
                if key_l != "headers" or _uri_secret_paths(child, child_path):
                    matches.append(".".join(child_path))
            elif URI_SECRET_VALUE_RE.search(child_s):
                matches.append(".".join(child_path))
            matches.extend(_uri_secret_paths(child, child_path))
    elif isinstance(value, list):
        for index, child in enumerate(value):
            matches.extend(_uri_secret_paths(child, (*path, f"[{index}]")))
    return sorted(set(matches))


def _set_fact_secret_names(value: Any) -> list[str]:
    facts = _as_mapping(value)
    matches: list[str] = []
    for key, child in facts.items():
        if str(key).startswith("cacheable"):
            continue
        child_s = _stringify(child)
        if SECRET_FACT_RE.search(str(key)) and SLURP_CONTENT_RE.search(child_s):
            matches.append(str(key))
    return matches


class NoSecretWithoutNoLogRule(AnsibleLintRule):
    """Secret-bearing tasks must opt out of Ansible result logging."""

    id = "no-secret-without-no-log"
    description = "Tasks that slurp or transmit bearer secrets must set no_log: true."
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
            task = task_node.task
            if _is_no_log_true(task):
                continue

            module = _module(task)
            if module is None:
                continue
            module_name, module_args = module
            args = _as_mapping(module_args)

            if module_name == "slurp":
                src = str(args.get("src") or args.get("path") or "")
                if SECRET_PATH_RE.search(src):
                    results.append(
                        self.create_matcherror(
                            message=f"slurp of secret-looking path {src!r} must set no_log: true",
                            lineno=task_node.line,
                            filename=file,
                        )
                    )
            elif module_name == "uri":
                secret_paths = _uri_secret_paths(args)
                if secret_paths:
                    results.append(
                        self.create_matcherror(
                            message=f"uri task uses secret-bearing fields ({', '.join(secret_paths)}) and must set no_log: true",
                            lineno=task_node.line,
                            filename=file,
                        )
                    )
            elif module_name == "set_fact":
                fact_names = _set_fact_secret_names(module_args)
                if fact_names:
                    results.append(
                        self.create_matcherror(
                            message=(
                                f"set_fact derives secret-looking facts from slurp output "
                                f"({', '.join(fact_names)}) and must set no_log: true"
                            ),
                            lineno=task_node.line,
                            filename=file,
                        )
                    )

        return results
