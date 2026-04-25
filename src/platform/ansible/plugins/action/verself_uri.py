"""verself_uri — `uri` with Verself trace + baggage correlation.

Delegates to ansible.builtin.uri. Injects exactly two observability headers
per request:
  - traceparent:  W3C TraceContext v1. Anchors the probe under the deploy
    trace (same trace-id as the playbook span). The span-id is a
    deterministic SHA-256 derived from the task identity so repeated runs
    of the same task share a probe parent span-id.
  - baggage:      W3C Baggage. Carries verself.* members that downstream
    services project onto every span they create (see src/otel/otel.go,
    baggageSpanProcessor). The product correlation header
    X-Verself-Correlation-Id is unrelated to this plane.

Deploy identity (VERSELF_DEPLOY_ID, VERSELF_DEPLOY_RUN_KEY) must be
set in the environment before ansible-playbook runs — scripts/deploy_identity.sh
is the single source of truth. The action fails fast if the identity is
missing, because guessing it silently would break downstream trace joins.
"""

from __future__ import annotations

import collections.abc as _c
import hashlib
import os
from copy import deepcopy
from pathlib import Path
from urllib.parse import quote, urlsplit

from ansible.errors import AnsibleActionFail
from ansible.module_utils.parsing.convert_bool import boolean
from ansible.plugins.action import ActionBase


DOCUMENTATION = r"""
name: verself_uri
type: action
short_description: `uri` wrapper emitting W3C traceparent + baggage
description:
  - Delegates to ansible.builtin.uri / ansible.legacy.uri.
  - Injects `traceparent` anchored to VERSELF_DEPLOY_ID's UUIDv5 trace id.
  - Injects `baggage` carrying verself.* correlation members that
    downstream services project onto their spans via the verselfotel baggage
    span processor.
  - Preserves any explicitly provided task headers.
requirements:
  - ansible-core
  - VERSELF_DEPLOY_ID and VERSELF_DEPLOY_RUN_KEY in the environment
    (set by src/platform/scripts/deploy_identity.sh).
"""


_PROBE_ORDINALS: dict[tuple[str, str, str, str], int] = {}


def _stable_hex(*parts: str, length: int) -> str:
    digest = hashlib.sha256()
    for part in parts:
        digest.update(part.encode("utf-8"))
        digest.update(b"\0")
    return digest.hexdigest()[:length]


def _require_env(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise AnsibleActionFail(
            f"verself_uri: {name} is not set. Source scripts/deploy_identity.sh "
            f"before running ansible-playbook."
        )
    return value


def _trace_id_hex() -> str:
    deploy_id = _require_env("VERSELF_DEPLOY_ID")
    return deploy_id.replace("-", "")


def _task_template_id(task, task_vars) -> str:
    task_path = (getattr(task, "get_path", lambda: "")() or "").strip()
    task_name = (task.get_name() or getattr(task, "action", "") or "").strip()
    task_action = (getattr(task, "action", "") or "").strip()
    play_name = (task_vars.get("ansible_play_name") or "").strip()
    return _stable_hex(play_name, task_path, task_name, task_action, length=32)


def _task_instance_id(task, task_vars, task_template_id: str) -> str:
    deploy_id = _require_env("VERSELF_DEPLOY_ID")
    deploy_run_key = _require_env("VERSELF_DEPLOY_RUN_KEY")
    task_uuid = getattr(task, "_uuid", "") or ""
    inventory_hostname = (task_vars.get("inventory_hostname") or "").strip()
    return _stable_hex(
        deploy_id,
        deploy_run_key,
        inventory_hostname,
        task_uuid,
        task_template_id,
        length=32,
    )


def _probe_span_id(task_instance_id: str, method: str, url: str) -> str:
    parsed = urlsplit(url)
    path = parsed.path or "/"
    key = (_require_env("VERSELF_DEPLOY_ID"), task_instance_id, method.upper(), path)
    ordinal = _PROBE_ORDINALS.get(key, 0)
    _PROBE_ORDINALS[key] = ordinal + 1
    span = _stable_hex(task_instance_id, method.upper(), path, str(ordinal), length=16)
    if span == "0" * 16:
        return "0000000000000001"
    return span


def _header_has(headers: dict, name: str) -> bool:
    return any(str(k).lower() == name.lower() for k in headers.keys())


def _baggage_value(task_template_id: str, task_instance_id: str, probe_span_id: str) -> str:
    members: list[tuple[str, str]] = [
        ("verself.deploy_id", _require_env("VERSELF_DEPLOY_ID")),
        ("verself.deploy_run_key", _require_env("VERSELF_DEPLOY_RUN_KEY")),
        ("verself.task_template_id", task_template_id),
        ("verself.task_instance_id", task_instance_id),
        ("verself.probe_id", probe_span_id),
    ]
    for env_var, key in (
        ("VERSELF_VERIFICATION_RUN", "verself.verification_run"),
        ("VERSELF_CORRELATION_ID", "verself.correlation_id"),
    ):
        value = os.environ.get(env_var, "").strip()
        if value:
            members.append((key, value))
    return ",".join(f"{k}={quote(v, safe='-._~@:/')}" for k, v in members if v)


class ActionModule(ActionBase):
    TRANSFERS_FILES = True

    def run(self, tmp=None, task_vars=None):
        self._supports_async = True
        self._supports_check_mode = False

        if task_vars is None:
            task_vars = {}

        super().run(tmp, task_vars)
        del tmp

        body_format = self._task.args.get("body_format", "raw")
        body = self._task.args.get("body")
        src = self._task.args.get("src", None)
        remote_src = boolean(self._task.args.get("remote_src", "no"), strict=False)
        headers = dict(self._task.args.get("headers") or {})
        method = (self._task.args.get("method") or "GET").strip() or "GET"
        url = (self._task.args.get("url") or "").strip()

        task_template_id = _task_template_id(self._task, task_vars)
        task_instance_id = _task_instance_id(self._task, task_vars, task_template_id)
        probe_span_id = _probe_span_id(task_instance_id, method, url) if url else ""

        if url and not _header_has(headers, "traceparent") and probe_span_id:
            headers["traceparent"] = f"00-{_trace_id_hex()}-{probe_span_id}-01"

        if not _header_has(headers, "baggage"):
            headers["baggage"] = _baggage_value(task_template_id, task_instance_id, probe_span_id)

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
