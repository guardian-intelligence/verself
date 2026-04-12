"""fm_uri — `uri` with deterministic Forge Metal correlation headers.

This wrapper keeps the builtin `uri` behavior but injects correlation headers
when they are supplied via environment or Ansible vars. It also derives a
stable probe id from the deploy/task context so repeated calls are joinable in
ClickHouse without depending on span parenting.
"""

from __future__ import annotations

import collections.abc as _c
import fcntl
import hashlib
import os
import socket
import time
import uuid
from copy import deepcopy
from pathlib import Path
from urllib.parse import quote, urlsplit

from ansible.errors import AnsibleActionFail
from ansible.module_utils.parsing.convert_bool import boolean
from ansible.plugins.action import ActionBase


DOCUMENTATION = r"""
name: fm_uri
type: action
short_description: `uri` wrapper with Forge Metal correlation headers
description:
  - Delegates to ansible.builtin.uri / ansible.legacy.uri.
  - Injects deterministic Forge Metal correlation headers when they are
    available from environment variables or Ansible vars.
  - Preserves any explicitly provided task headers.
requirements:
  - ansible-core
"""

_CONTEXT_CACHE: dict[str, str] = {}
_PROBE_ORDINALS: dict[tuple[str, str, str, str], int] = {}

_HEADER_SOURCES = {
    "X-Forge-Metal-Deploy-Id": ("FORGE_METAL_DEPLOY_ID", "forge_metal_deploy_id"),
    "X-Forge-Metal-Deploy-Run-Key": (
        "FORGE_METAL_DEPLOY_RUN_KEY",
        "forge_metal_deploy_run_key",
    ),
    "X-Forge-Metal-Task-Template-Id": (
        "FORGE_METAL_TASK_TEMPLATE_ID",
        "forge_metal_task_template_id",
    ),
    "X-Forge-Metal-Task-Instance-Id": (
        "FORGE_METAL_TASK_INSTANCE_ID",
        "forge_metal_task_instance_id",
    ),
    "X-Forge-Metal-Probe-Id": ("FORGE_METAL_PROBE_ID", "forge_metal_probe_id"),
    "X-Forge-Metal-Verification-Run": (
        "FORGE_METAL_VERIFICATION_RUN",
        "forge_metal_verification_run",
    ),
    "X-Forge-Metal-Correlation-Id": (
        "FORGE_METAL_CORRELATION_ID",
        "forge_metal_correlation_id",
    ),
}


def _first_nonempty(*values):
    for value in values:
        if value is None:
            continue
        text = str(value).strip()
        if text:
            return text
    return ""


def _stable_hex(*parts: str, length: int = 32) -> str:
    digest = hashlib.sha256()
    for part in parts:
        digest.update(part.encode("utf-8"))
        digest.update(b"\0")
    return digest.hexdigest()[:length]


def _cache_dir() -> Path:
    root = os.environ.get("XDG_CACHE_HOME")
    if not root:
        root = str(Path.home() / ".cache")
    return Path(root) / "forge-metal" / "deploy-run-key"


def _generate_run_key() -> str:
    today = time.strftime("%Y-%m-%d", time.gmtime())
    cache_dir = _cache_dir()
    cache_dir.mkdir(parents=True, exist_ok=True)
    counter_path = cache_dir / f"{today}.counter"
    lock_path = cache_dir / f"{today}.lock"
    hostname = socket.gethostname().split(".")[0] or "controller"

    with lock_path.open("a+") as lock_file:
        fcntl.flock(lock_file, fcntl.LOCK_EX)
        try:
            current = int(counter_path.read_text(encoding="utf-8").strip() or "0")
        except FileNotFoundError:
            current = 0
        except ValueError:
            current = 0
        current += 1
        counter_path.write_text(str(current), encoding="utf-8")
    return f"{today}.{current:06d}@{hostname}"


def _context_value(task_vars, env_name: str, var_name: str = "") -> str:
    candidates = []
    if env_name:
        candidates.append(os.environ.get(env_name))
    if var_name:
        candidates.append(task_vars.get(var_name))
    return _first_nonempty(*candidates)


def _context() -> dict[str, str]:
    if _CONTEXT_CACHE:
        return _CONTEXT_CACHE

    run_key = _first_nonempty(
        os.environ.get("FORGE_METAL_DEPLOY_RUN_KEY"),
        _generate_run_key(),
    )
    deploy_id = _first_nonempty(
        os.environ.get("FORGE_METAL_DEPLOY_ID"),
        str(uuid.uuid5(uuid.NAMESPACE_URL, f"forge-metal:{run_key}")),
    )
    _CONTEXT_CACHE.update(
        {
            "deploy_id": deploy_id,
            "deploy_run_key": run_key,
        }
    )
    return _CONTEXT_CACHE


def _header_get(headers: dict, name: str) -> str:
    for key, value in headers.items():
        if str(key).lower() == name.lower():
            return _first_nonempty(value)
    return ""


def _header_has(headers: dict, name: str) -> bool:
    for key in headers.keys():
        if str(key).lower() == name.lower():
            return True
    return False


def _task_template_id(task, task_vars) -> str:
    task_path = _first_nonempty(
        getattr(task, "get_path", lambda: "")(),
        task_vars.get("ansible_parent_role_names", ""),
    )
    task_name = _first_nonempty(task.get_name(), getattr(task, "action", ""))
    task_action = _first_nonempty(getattr(task, "action", ""))
    play_name = _first_nonempty(task_vars.get("ansible_play_name"))
    return _stable_hex(play_name, task_path, task_name, task_action)


def _task_instance_id(task, task_vars, context: dict[str, str], task_template_id: str) -> str:
    task_uuid = _first_nonempty(getattr(task, "_uuid", ""))
    inventory_hostname = _first_nonempty(task_vars.get("inventory_hostname", ""))
    return _stable_hex(
        context["deploy_id"],
        context["deploy_run_key"],
        inventory_hostname,
        task_uuid,
        task_template_id,
    )


def _probe_ordinal(context: dict[str, str], task_instance_id: str, method: str, url: str) -> int:
    parsed = urlsplit(url)
    path = parsed.path or "/"
    key = (context["deploy_id"], task_instance_id, method.upper(), path)
    ordinal = _PROBE_ORDINALS.get(key, 0)
    _PROBE_ORDINALS[key] = ordinal + 1
    return ordinal


def _probe_id(
    context: dict[str, str],
    task_instance_id: str,
    method: str,
    url: str,
    ordinal: int,
) -> str:
    parsed = urlsplit(url)
    path = parsed.path or "/"
    return _stable_hex(
        context["deploy_id"],
        task_instance_id,
        method.upper(),
        path,
        str(ordinal),
    )


def _trace_id(context: dict[str, str]) -> str:
    deploy_id = _first_nonempty(context.get("deploy_id"))
    try:
        return uuid.UUID(deploy_id).hex
    except Exception:
        return _stable_hex(deploy_id, length=32)


def _span_id(seed: str) -> str:
    span_id = _stable_hex(seed, length=16)
    if span_id == "0" * 16:
        return "0000000000000001"
    return span_id


def _traceparent(context: dict[str, str], task_instance_id: str) -> str:
    return f"00-{_trace_id(context)}-{_span_id(task_instance_id)}-01"


def _baggage(headers: dict[str, str]) -> str:
    items: list[tuple[str, str]] = []

    def add(key: str, header_name: str):
        value = _header_get(headers, header_name)
        if value:
            items.append((key, value))

    add("forge_metal.deploy_id", "X-Forge-Metal-Deploy-Id")
    add("forge_metal.deploy_run_key", "X-Forge-Metal-Deploy-Run-Key")
    add("forge_metal.task_template_id", "X-Forge-Metal-Task-Template-Id")
    add("forge_metal.task_instance_id", "X-Forge-Metal-Task-Instance-Id")
    add("forge_metal.probe_id", "X-Forge-Metal-Probe-Id")
    add("forge_metal.verification_run", "X-Forge-Metal-Verification-Run")
    add("forge_metal.correlation_id", "X-Forge-Metal-Correlation-Id")

    encoded = [f"{k}={quote(v, safe='-._~@:/')}" for k, v in items]
    return ",".join(encoded)


class ActionModule(ActionBase):
    TRANSFERS_FILES = True

    def run(self, tmp=None, task_vars=None):
        self._supports_async = True
        self._supports_check_mode = False

        if task_vars is None:
            task_vars = {}

        super().run(tmp, task_vars)
        del tmp  # tmp no longer has any effect

        body_format = self._task.args.get("body_format", "raw")
        body = self._task.args.get("body")
        src = self._task.args.get("src", None)
        remote_src = boolean(self._task.args.get("remote_src", "no"), strict=False)
        headers = dict(self._task.args.get("headers") or {})
        context = _context()
        method = _first_nonempty(self._task.args.get("method", "GET"))
        url = _first_nonempty(self._task.args.get("url", ""))
        task_template_id = _task_template_id(self._task, task_vars)
        task_instance_id = _task_instance_id(self._task, task_vars, context, task_template_id)

        for header_name, sources in _HEADER_SOURCES.items():
            if header_name in headers:
                continue

            if header_name == "X-Forge-Metal-Deploy-Id":
                value = _context_value(task_vars, sources[0], sources[1]) or context["deploy_id"]
            elif header_name == "X-Forge-Metal-Deploy-Run-Key":
                value = _context_value(task_vars, sources[0], sources[1]) or context["deploy_run_key"]
            elif header_name == "X-Forge-Metal-Task-Template-Id":
                value = _context_value(task_vars, sources[0], sources[1]) or task_template_id
            elif header_name == "X-Forge-Metal-Task-Instance-Id":
                value = _context_value(task_vars, sources[0], sources[1]) or task_instance_id
            elif header_name == "X-Forge-Metal-Probe-Id":
                value = _context_value(task_vars, sources[0], sources[1])
                if not value and url:
                    ordinal = _probe_ordinal(context, task_instance_id, method, url)
                    value = _probe_id(context, task_instance_id, method, url, ordinal)
            else:
                env_name, var_name = sources
                value = _context_value(task_vars, env_name, var_name)

            if value:
                headers[header_name] = value

        if url and not _header_has(headers, "traceparent"):
            headers["traceparent"] = _traceparent(context, task_instance_id)

        if not _header_has(headers, "baggage"):
            baggage_value = _baggage(headers)
            if baggage_value:
                headers["baggage"] = baggage_value

        try:
            if remote_src:
                new_module_args = dict(self._task.args)
                new_module_args["headers"] = headers
                return self._execute_module(
                    module_name="ansible.legacy.uri",
                    module_args=new_module_args,
                    task_vars=task_vars,
                    wrap_async=self._task.async_val,
                )

            kwargs = {}

            if src:
                src = self._find_needle("files", src)

                tmp_src = self._connection._shell.join_path(
                    self._connection._shell.tmpdir,
                    os.path.basename(src),
                )
                kwargs["src"] = tmp_src
                self._transfer_file(src, tmp_src)
                self._fixup_perms2((self._connection._shell.tmpdir, tmp_src))
            elif body_format == "form-multipart":
                if not isinstance(body, _c.Mapping):
                    raise AnsibleActionFail(
                        "body must be mapping, cannot be type %s"
                        % body.__class__.__name__
                    )
                new_body = deepcopy(body)
                for field, value in new_body.items():
                    if not isinstance(value, _c.MutableMapping):
                        continue
                    content = value.get("content")
                    filename = value.get("filename")
                    if not filename or content:
                        continue

                    filename = self._find_needle("files", filename)

                    tmp_src = self._connection._shell.join_path(
                        self._connection._shell.tmpdir,
                        os.path.basename(filename),
                    )
                    value["filename"] = tmp_src
                    self._transfer_file(filename, tmp_src)
                    self._fixup_perms2((self._connection._shell.tmpdir, tmp_src))
                kwargs["body"] = new_body

            new_module_args = dict(self._task.args)
            new_module_args.update(kwargs)
            new_module_args["headers"] = headers

            return self._execute_module(
                "ansible.legacy.uri",
                module_args=new_module_args,
                task_vars=task_vars,
                wrap_async=self._task.async_val,
            )
        finally:
            if not self._task.async_val:
                self._remove_tmp_path(self._connection._shell.tmpdir)
