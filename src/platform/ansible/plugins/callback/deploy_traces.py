"""deploy_traces — emit Ansible playbook/task spans to ClickHouse via OTLP.

The callback is intentionally lazy-imported: if the OpenTelemetry SDK is not
installed on the controller, Ansible still runs and this callback becomes a
no-op after emitting one yellow warning.
"""

from __future__ import annotations

import hashlib
import os
import re
import socket
import subprocess
import time
from dataclasses import dataclass
from urllib.parse import urlparse

from ansible.plugins.callback import CallbackBase

try:
    import deploy_events as _deploy_events
except Exception:
    _deploy_events = None

DOCUMENTATION = """
    name: deploy_traces
    type: aggregate
    short_description: Emit deploy traces via OTLP
    description:
        - Captures ansible.playbook, ansible.play, and ansible.task/handler spans.
        - Exports to the controller's OTLP gRPC endpoint.
        - Falls back to no-op if the OpenTelemetry SDK is unavailable.
    requirements:
        - OpenTelemetry Python SDK and OTLP gRPC exporter when enabled
"""


def _load_otel():
    try:
        from opentelemetry import trace
        from opentelemetry.sdk.resources import Resource
        from opentelemetry.sdk.trace import TracerProvider
        from opentelemetry.sdk.trace.export import SimpleSpanProcessor
        from opentelemetry.sdk.trace.id_generator import IdGenerator
        from opentelemetry.trace import SpanKind
        from opentelemetry.trace.status import Status, StatusCode
        from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import (
            OTLPSpanExporter,
        )
    except Exception as exc:
        return {
            "enabled": False,
            "error": exc,
        }

    class DeployIdGenerator(IdGenerator):
        def __init__(self, trace_id_hex: str):
            self._trace_id = int(trace_id_hex, 16)

        def generate_span_id(self):
            span_id = 0
            while not span_id:
                span_id = int.from_bytes(os.urandom(8), "big")
            return span_id

        def generate_trace_id(self):
            return self._trace_id

    return {
        "enabled": True,
        "trace": trace,
        "Resource": Resource,
        "TracerProvider": TracerProvider,
        "SimpleSpanProcessor": SimpleSpanProcessor,
        "SpanKind": SpanKind,
        "Status": Status,
        "StatusCode": StatusCode,
        "OTLPSpanExporter": OTLPSpanExporter,
        "DeployIdGenerator": DeployIdGenerator,
    }


def _normalize_endpoint(raw: str) -> tuple[str, bool]:
    parsed = urlparse(raw)
    if parsed.scheme in {"http", "https"}:
        endpoint = parsed.netloc or parsed.path
        if not endpoint:
            endpoint = raw
        return endpoint, parsed.scheme != "https"
    return raw, True


def _endpoint_host_port(raw: str) -> tuple[str, int]:
    parsed = urlparse(raw)
    if parsed.scheme:
        host = parsed.hostname or "127.0.0.1"
        port = parsed.port or 4317
        return host, port

    text = raw.strip()
    if text.startswith("[") and "]" in text:
        host, _, tail = text.partition("]")
        host = host.lstrip("[") or "127.0.0.1"
        if tail.startswith(":") and tail[1:].isdigit():
            return host, int(tail[1:])
        return host, 4317

    if ":" in text:
        host, port_text = text.rsplit(":", 1)
        if host and port_text.isdigit():
            return host, int(port_text)

    return text or "127.0.0.1", 4317


def _endpoint_reachable(raw: str, timeout_s: float = 0.25) -> bool:
    host, port = _endpoint_host_port(raw)
    try:
        with socket.create_connection((host, port), timeout=timeout_s):
            return True
    except Exception:
        return False


def _as_bool_text(value) -> str:
    return "true" if bool(value) else "false"


def _first_nonempty(*values) -> str:
    for value in values:
        if value is None:
            continue
        text = str(value).strip()
        if text:
            return text
    return ""


def _sanitize_hostname(hostname: str) -> str:
    return re.sub(r"[^A-Za-z0-9_.-]+", "_", hostname or "unknown")


def _trace_id_from_deploy_id(deploy_id: str) -> str:
    try:
        import uuid

        return uuid.UUID(deploy_id).hex
    except Exception:
        return hashlib.sha256(deploy_id.encode("utf-8")).hexdigest()[:32]


def _identity_from_env() -> dict[str, str]:
    deploy_run_key = _first_nonempty(
        os.environ.get("FORGE_METAL_DEPLOY_RUN_KEY"),
        f"{time.strftime('%Y-%m-%d', time.gmtime())}.000000@{_sanitize_hostname(socket.gethostname())}",
    )
    deploy_id = _first_nonempty(os.environ.get("FORGE_METAL_DEPLOY_ID"))
    if not deploy_id:
        import uuid

        deploy_id = str(
            uuid.uuid5(uuid.NAMESPACE_URL, f"forge-metal:{deploy_run_key}")
        )
    trace_id = _first_nonempty(
        os.environ.get("FORGE_METAL_TRACE_ID"),
        _trace_id_from_deploy_id(deploy_id),
    )
    return {
        "deploy_id": deploy_id,
        "deploy_run_key": deploy_run_key,
        "trace_id": trace_id,
    }


def _stable_hex(*parts: str, length: int = 32) -> str:
    digest = hashlib.sha256()
    for part in parts:
        digest.update(str(part).encode("utf-8"))
        digest.update(b"\0")
    return digest.hexdigest()[:length]


def _git(*args) -> str:
    try:
        return (
            subprocess.check_output(
                ["git"] + list(args),
                stderr=subprocess.DEVNULL,
                timeout=5,
            )
            .decode()
            .strip()
        )
    except Exception:
        return ""


def _host_name(result) -> str:
    host = getattr(result, "_host", None)
    if host is None:
        return ""
    for attr in ("get_name", "name", "_name"):
        candidate = getattr(host, attr, None)
        if callable(candidate):
            try:
                value = candidate()
                if value:
                    return str(value)
            except Exception:
                continue
        elif candidate:
            return str(candidate)
    return ""


def _task_name(task) -> str:
    try:
        value = task.get_name()
        if value:
            return str(value)
    except Exception:
        pass
    return str(getattr(task, "_name", "") or "")


def _task_action(task) -> str:
    for attr in ("action", "_action"):
        value = getattr(task, attr, None)
        if value:
            return str(value)
    return ""


def _task_path(task) -> str:
    try:
        value = getattr(task, "get_path", None)
        if callable(value):
            path = value()
            if path:
                return str(path)
    except Exception:
        pass

    ds = getattr(task, "_ds", None)
    if ds is not None:
        for attr in ("ansible_pos", "_ansible_pos"):
            pos = getattr(ds, attr, None)
            if pos:
                return str(pos)
        if isinstance(ds, dict):
            for attr in ("ansible_pos", "_ansible_pos"):
                pos = ds.get(attr)
                if pos:
                    return str(pos)

    for attr in ("_file_name", "_filename", "path"):
        value = getattr(task, attr, None)
        if value:
            return str(value)
    return ""


def _task_role(task, task_path: str) -> str:
    role = getattr(task, "_role", None)
    if role is not None:
        for attr in ("get_name", "name", "_role_name", "_name"):
            value = getattr(role, attr, None)
            if callable(value):
                try:
                    resolved = value()
                    if resolved:
                        return str(resolved)
                except Exception:
                    continue
            elif value:
                return str(value)
    marker = "/roles/"
    if marker in task_path:
        tail = task_path.split(marker, 1)[1]
        return tail.split("/", 1)[0]
    return ""


def _result_message(result) -> str:
    payload = getattr(result, "_result", {}) or {}
    for key in ("msg", "stderr", "exception"):
        value = payload.get(key)
        if value:
            return str(value)
    return ""


@dataclass
class _SpanContext:
    span: object
    start_ns: int
    name: str
    kind: str
    action: str = ""
    path: str = ""


class CallbackModule(CallbackBase):
    CALLBACK_VERSION = 2.0
    CALLBACK_TYPE = "aggregate"
    CALLBACK_NAME = "deploy_traces"
    CALLBACK_NEEDS_ENABLED = True

    def __init__(self):
        super().__init__()
        if _deploy_events is not None and hasattr(_deploy_events, "get_deploy_identity"):
            self._identity = _deploy_events.get_deploy_identity()
        else:
            self._identity = _identity_from_env()
        self._playbook_file = ""
        self._playbook_name = ""
        self._playbook_tags = _first_nonempty(
            os.environ.get("FORGE_METAL_TAG_FILTER"),
            os.environ.get("ANSIBLE_TAGS"),
            os.environ.get("ANSIBLE_RUN_TAGS"),
        )
        self._deploy_kind = _first_nonempty(
            os.environ.get("FORGE_METAL_DEPLOY_KIND"),
            "ansible-playbook",
        )
        self._commit_sha = _git("rev-parse", "HEAD")
        self._dirty = _as_bool_text(_git("status", "--porcelain") != "")
        self._start_ns = time.time_ns()
        self._otel = _load_otel()
        self._otel_enabled = bool(self._otel.get("enabled"))
        self._root_span = None
        self._root_context = None
        self._current_play = None
        self._current_task = None
        self._play_had_error = False
        self._had_error = False

        if self._otel_enabled:
            endpoint_raw = os.environ.get(
                "FORGE_METAL_OTLP_ENDPOINT",
                "127.0.0.1:4317",
            )
            explicit_endpoint = "FORGE_METAL_OTLP_ENDPOINT" in os.environ
            if not explicit_endpoint and not _endpoint_reachable(endpoint_raw):
                self._otel_enabled = False
                self._provider = None
                self._tracer = None
                self._display.display(
                    "deploy_traces: 127.0.0.1:4317 unavailable on controller; export disabled (set FORGE_METAL_OTLP_ENDPOINT to override)",
                    color="yellow",
                )
                return
            endpoint, insecure = _normalize_endpoint(endpoint_raw)
            resource = self._otel["Resource"].create(
                {
                    "service.name": "ansible",
                    "service.instance.id": self._identity["deploy_run_key"],
                }
            )
            exporter = self._otel["OTLPSpanExporter"](
                endpoint=endpoint,
                insecure=insecure,
            )
            self._provider = self._otel["TracerProvider"](
                resource=resource,
                id_generator=self._otel["DeployIdGenerator"](
                    self._identity["trace_id"]
                ),
            )
            self._provider.add_span_processor(
                self._otel["SimpleSpanProcessor"](exporter)
            )
            self._tracer = self._provider.get_tracer("forge-metal.deploy_traces")
        else:
            self._provider = None
            self._tracer = None
            self._warn_missing_otel()

    def _warn_missing_otel(self):
        if getattr(self, "_warned_missing_otel", False):
            return
        self._warned_missing_otel = True
        display = getattr(self, "_display", None)
        message = (
            "deploy_traces: OpenTelemetry SDK unavailable; trace export disabled"
        )
        if display is not None:
            display.display(message, color="yellow")
        else:
            print(message)

    def _base_attributes(self):
        attrs = {
            "forge_metal.deploy_id": self._identity["deploy_id"],
            "forge_metal.deploy_run_key": self._identity["deploy_run_key"],
            "cicd.pipeline.name": self._playbook_name,
            "cicd.pipeline.run.id": self._identity["deploy_id"],
            "forge_metal.commit_sha": self._commit_sha,
            "forge_metal.dirty": self._dirty,
            "forge_metal.deploy_kind": self._deploy_kind,
        }
        if self._playbook_tags:
            attrs["forge_metal.tag_filter"] = self._playbook_tags
        return attrs

    def _start_root_span(self):
        if not self._otel_enabled or self._root_span is not None:
            return
        self._root_span = self._tracer.start_span(
            "ansible.playbook",
            kind=self._otel["SpanKind"].INTERNAL,
            start_time=self._start_ns,
            attributes=self._base_attributes(),
        )
        self._root_context = self._otel["trace"].set_span_in_context(self._root_span)

    def _current_parent_context(self):
        if self._current_play is not None:
            return self._otel["trace"].set_span_in_context(self._current_play.span)
        if self._root_context is not None:
            return self._root_context
        return None

    def _end_span(self, span, end_ns, status=None):
        if span is None:
            return
        try:
            if status is not None:
                span.set_status(status)
            span.end(end_time=end_ns)
        except Exception:
            pass

    def _start_play_span(self, play, start_ns):
        if not self._otel_enabled:
            return
        if self._current_play is not None:
            self._end_play_span(time.time_ns())
        attributes = self._base_attributes()
        attributes["forge_metal.play_name"] = _task_name(play)
        play_name = _task_name(play)
        self._current_play = _SpanContext(
            span=self._tracer.start_span(
                "ansible.play",
                context=self._root_context,
                kind=self._otel["SpanKind"].INTERNAL,
                start_time=start_ns,
                attributes=attributes,
            ),
            start_ns=start_ns,
            name=play_name,
            kind="play",
        )
        self._play_had_error = False

    def _end_play_span(self, end_ns):
        if self._current_play is None:
            return
        status_code = (
            self._otel["StatusCode"].ERROR
            if self._play_had_error
            else self._otel["StatusCode"].OK
        )
        description = "one or more tasks failed" if self._play_had_error else ""
        status = self._otel["Status"](
            status_code=status_code,
            description=description,
        )
        self._end_span(self._current_play.span, end_ns, status=status)
        self._current_play = None
        self._play_had_error = False

    def _task_span(self, result, outcome, start_ns, end_ns):
        if not self._otel_enabled:
            return

        task = getattr(result, "_task", None)
        if task is None:
            return
        task_kind = self._current_task.kind if self._current_task else "task"
        span_name = "ansible.handler" if task_kind == "handler" else "ansible.task"
        parent_context = self._current_parent_context()
        task_name = self._current_task.name if self._current_task else _task_name(task)
        task_action = self._current_task.action if self._current_task else _task_action(task)
        task_path = self._current_task.path if self._current_task else _task_path(task)
        task_role = _task_role(task, task_path)
        host_name = _host_name(result)
        changed = _as_bool_text(getattr(result, "_result", {}).get("changed", False))
        task_uuid = _first_nonempty(getattr(task, "_uuid", ""))
        play_name = self._current_play.name if self._current_play is not None else ""
        task_template_id = _stable_hex(play_name, task_path, task_name, task_action)
        task_instance_id = _stable_hex(
            self._identity["deploy_id"],
            self._identity["deploy_run_key"],
            host_name,
            task_uuid,
            task_template_id,
        )

        attributes = self._base_attributes()
        attributes.update(
            {
                "forge_metal.role": task_role,
                "forge_metal.task_name": task_name,
                "forge_metal.task_action": task_action,
                "forge_metal.task_changed": changed,
                "forge_metal.task_path": task_path,
                "forge_metal.task_template_id": task_template_id,
                "forge_metal.task_instance_id": task_instance_id,
                "forge_metal.host": host_name,
                "cicd.pipeline.task.name": task_name,
            }
        )

        span = self._tracer.start_span(
            span_name,
            context=parent_context,
            kind=self._otel["SpanKind"].INTERNAL,
            start_time=start_ns,
            attributes=attributes,
        )

        if outcome in {"failed", "unreachable"}:
            self._had_error = True
            self._play_had_error = True
            message = _result_message(result) or outcome
            status = self._otel["Status"](
                status_code=self._otel["StatusCode"].ERROR,
                description=message,
            )
        else:
            status = self._otel["Status"](
                status_code=self._otel["StatusCode"].OK
            )

        self._end_span(span, end_ns, status=status)

    def _finish_trace(self):
        if not self._otel_enabled:
            return
        end_ns = time.time_ns()
        self._end_play_span(end_ns)
        status_code = (
            self._otel["StatusCode"].ERROR
            if self._had_error
            else self._otel["StatusCode"].OK
        )
        description = "one or more tasks failed" if self._had_error else ""
        status = self._otel["Status"](
            status_code=status_code,
            description=description,
        )
        self._end_span(self._root_span, end_ns, status=status)
        try:
            self._provider.force_flush(timeout_millis=5000)
        except Exception:
            pass
        try:
            self._provider.shutdown()
        except Exception:
            pass

    def v2_playbook_on_start(self, playbook):
        self._playbook_file = str(playbook._file_name)
        self._playbook_name = os.path.basename(self._playbook_file)
        self._start_ns = time.time_ns()
        self._start_root_span()

    def v2_playbook_on_play_start(self, play):
        self._start_play_span(play, time.time_ns())

    def v2_playbook_on_task_start(self, task, is_conditional):
        self._current_task = _SpanContext(
            span=None,
            start_ns=time.time_ns(),
            name=_task_name(task),
            kind="task",
            action=_task_action(task),
            path=_task_path(task),
        )

    def v2_playbook_on_handler_task_start(self, task):
        self._current_task = _SpanContext(
            span=None,
            start_ns=time.time_ns(),
            name=_task_name(task),
            kind="handler",
            action=_task_action(task),
            path=_task_path(task),
        )

    def _record_result(self, result, outcome):
        if self._current_task is None:
            task = getattr(result, "_task", None)
            self._current_task = _SpanContext(
                span=None,
                start_ns=time.time_ns(),
                name=_task_name(task or result),
                kind="task",
                action=_task_action(task) if task is not None else "",
                path=_task_path(task) if task is not None else "",
            )
        start_ns = self._current_task.start_ns
        self._task_span(result, outcome, start_ns, time.time_ns())

    def v2_runner_on_ok(self, result):
        self._record_result(result, "ok")

    def v2_runner_on_failed(self, result, ignore_errors=False):
        self._record_result(result, "failed")

    def v2_runner_on_skipped(self, result):
        self._record_result(result, "skipped")

    def v2_runner_on_unreachable(self, result):
        self._record_result(result, "unreachable")

    def v2_playbook_on_stats(self, stats):
        self._finish_trace()
