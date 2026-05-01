# GNU General Public License v3.0+ (this is a thin subclass of community.general.opentelemetry, GPL-3.0-or-later)
# SPDX-License-Identifier: GPL-3.0-or-later
"""Fix the upstream community.general.opentelemetry callback so it
distinguishes 'changed' from 'ok' in the per-task host status attribute.

Upstream (callback/opentelemetry.py:585) hardcodes status='ok' on
v2_runner_on_ok regardless of result._result['changed']. That collapses
'task ran and mutated the host' onto 'task ran idempotently'. Our
substrate-correctness canary needs to query 'did any task run changed in
a layer whose hash matched?' — without this fix the answer is always no
because host.status='changed' never appears in the trace.

We subclass and override the one offending hook. Everything else
(option spec, tracer setup, span shape, traceparent honoring, log
emission) is delegated to the upstream callback.
"""
from __future__ import annotations

DOCUMENTATION = r"""
author: Verself
name: verself_otel
type: notification
short_description: Verself wrapper around community.general.opentelemetry that emits host.status='changed' for tasks that mutated state.
description:
  - Subclasses community.general.opentelemetry and overrides v2_runner_on_ok to
    propagate the result['changed'] flag into the host status attribute.
  - All other behavior (span shape, attribute set, traceparent honoring,
    OTLP exporter selection) is inherited unchanged from the upstream callback.
  - Keeps the same env-var contract as upstream so existing OTEL_* environment
    wiring (OTEL_EXPORTER_OTLP_ENDPOINT, TRACEPARENT, OTEL_RESOURCE_ATTRIBUTES,
    OTEL_SERVICE_NAME) continues to work without translation.
options:
  hide_task_arguments:
    default: false
    type: bool
    env:
      - name: ANSIBLE_OPENTELEMETRY_HIDE_TASK_ARGUMENTS
    ini:
      - section: callback_opentelemetry
        key: hide_task_arguments
  enable_from_environment:
    type: str
    env:
      - name: ANSIBLE_OPENTELEMETRY_ENABLE_FROM_ENVIRONMENT
    ini:
      - section: callback_opentelemetry
        key: enable_from_environment
  otel_service_name:
    default: ansible
    type: str
    env:
      - name: OTEL_SERVICE_NAME
    ini:
      - section: callback_opentelemetry
        key: otel_service_name
  traceparent:
    default: None
    type: str
    env:
      - name: TRACEPARENT
  disable_logs:
    default: false
    type: bool
    env:
      - name: ANSIBLE_OPENTELEMETRY_DISABLE_LOGS
    ini:
      - section: callback_opentelemetry
        key: disable_logs
  disable_attributes_in_logs:
    default: false
    type: bool
    env:
      - name: ANSIBLE_OPENTELEMETRY_DISABLE_ATTRIBUTES_IN_LOGS
    ini:
      - section: callback_opentelemetry
        key: disable_attributes_in_logs
  store_spans_in_file:
    type: str
    env:
      - name: ANSIBLE_OPENTELEMETRY_STORE_SPANS_IN_FILE
    ini:
      - section: callback_opentelemetry
        key: store_spans_in_file
  otel_exporter_otlp_traces_protocol:
    type: str
    default: grpc
    choices:
      - grpc
      - http/protobuf
    env:
      - name: OTEL_EXPORTER_OTLP_TRACES_PROTOCOL
    ini:
      - section: callback_opentelemetry
        key: otel_exporter_otlp_traces_protocol
"""

from ansible_collections.community.general.plugins.callback.opentelemetry import (  # type: ignore[import-not-found]
    CallbackModule as UpstreamCallbackModule,
)


class CallbackModule(UpstreamCallbackModule):
    CALLBACK_VERSION = 2.0
    CALLBACK_TYPE = "notification"
    CALLBACK_NAME = "verself_otel"
    CALLBACK_NEEDS_WHITELIST = True

    def v2_runner_on_ok(self, result):
        # Upstream collapses changed->ok; we split them so SpanAttributes
        # ['ansible.task.host.status'] in ClickHouse otel_traces actually says
        # 'changed' for state-mutating tasks. The divergence canary depends on it.
        status = "changed" if result._result.get("changed", False) else "ok"
        self.opentelemetry.finish_task(
            self.tasks_data,
            status,
            result,
            self.dump_results(self.tasks_data[result._task._uuid], result),
        )
