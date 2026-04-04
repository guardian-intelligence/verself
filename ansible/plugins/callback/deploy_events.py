"""deploy_events — write one JSON artifact per ansible-playbook run.

Zero external dependencies. The JSON file is uploaded to ClickHouse
post-deploy by scripts/upload-deploy-event.sh, which pipes through
the existing scripts/clickhouse.sh SSH tunnel.

Enable in ansible.cfg:
    callback_plugins = plugins/callback
    callbacks_enabled = deploy_events
"""

from __future__ import annotations

import json
import os
import subprocess
import time
import uuid
from datetime import datetime, timezone
from pathlib import Path

from ansible.plugins.callback import CallbackBase

DOCUMENTATION = """
    name: deploy_events
    type: aggregate
    short_description: Write deploy event JSON for ClickHouse ingestion
    description:
        - Captures playbook timing, task counts, and git metadata.
        - Writes a single JSON file per run to ~/.cache/forge-metal/deploy-events/.
        - Override output directory with DEPLOY_EVENT_DIR env var.
    requirements:
        - No external dependencies (stdlib only)
"""

DEPLOY_EVENT_DIR = os.environ.get(
    "DEPLOY_EVENT_DIR",
    str(Path.home() / ".cache" / "forge-metal" / "deploy-events"),
)


class CallbackModule(CallbackBase):
    CALLBACK_VERSION = 2.0
    CALLBACK_TYPE = "aggregate"
    CALLBACK_NAME = "deploy_events"
    CALLBACK_NEEDS_ENABLED = True

    def __init__(self):
        super().__init__()
        self._deploy_id = str(uuid.uuid4())
        self._start_ns = time.time_ns()
        self._playbook_file = ""
        self._plays = []
        self._task_timings = []  # [(task_name, duration_ns)]
        self._current_task_start = None
        self._current_task_name = None
        self._counts = {
            "ok": 0,
            "failed": 0,
            "skipped": 0,
            "changed": 0,
            "unreachable": 0,
        }

    # -- git helpers (best-effort, never fails) --

    @staticmethod
    def _git(*args):
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

    # -- Ansible hooks --

    def v2_playbook_on_start(self, playbook):
        self._playbook_file = str(playbook._file_name)
        self._start_ns = time.time_ns()

    def v2_playbook_on_play_start(self, play):
        self._plays.append(play.get_name())

    def v2_playbook_on_task_start(self, task, is_conditional):
        self._current_task_start = time.time_ns()
        self._current_task_name = task.get_name()

    def v2_playbook_on_handler_task_start(self, task):
        self._current_task_start = time.time_ns()
        self._current_task_name = task.get_name()

    def _record_timing(self):
        if self._current_task_start is not None:
            elapsed = time.time_ns() - self._current_task_start
            self._task_timings.append((self._current_task_name or "", elapsed))
            self._current_task_start = None

    def v2_runner_on_ok(self, result):
        self._record_timing()
        self._counts["ok"] += 1
        if result._result.get("changed", False):
            self._counts["changed"] += 1

    def v2_runner_on_failed(self, result, ignore_errors=False):
        self._record_timing()
        self._counts["failed"] += 1

    def v2_runner_on_skipped(self, result):
        self._record_timing()
        self._counts["skipped"] += 1

    def v2_runner_on_unreachable(self, result):
        self._record_timing()
        self._counts["unreachable"] += 1

    def v2_playbook_on_stats(self, stats):
        end_ns = time.time_ns()

        # Per-host summary from the stats object.
        hosts = {}
        for host in sorted(stats.processed):
            hosts[host] = stats.summarize(host)

        # Top 10 slowest tasks.
        slowest = sorted(self._task_timings, key=lambda x: x[1], reverse=True)[:10]

        overall_ok = (
            self._counts["failed"] == 0 and self._counts["unreachable"] == 0
        )

        event = {
            "deploy_id": self._deploy_id,
            "playbook": os.path.basename(self._playbook_file),
            "plays": self._plays,
            "commit_sha": self._git("rev-parse", "HEAD"),
            "branch": self._git("rev-parse", "--abbrev-ref", "HEAD"),
            "commit_message": self._git("log", "-1", "--format=%s"),
            "author": self._git("log", "-1", "--format=%ae"),
            "dirty": self._git("status", "--porcelain") != "",
            "started_at": datetime.fromtimestamp(
                self._start_ns / 1e9, tz=timezone.utc
            ).isoformat(),
            "completed_at": datetime.fromtimestamp(
                end_ns / 1e9, tz=timezone.utc
            ).isoformat(),
            "total_ns": end_ns - self._start_ns,
            "ok": overall_ok,
            "tasks_ok": self._counts["ok"],
            "tasks_failed": self._counts["failed"],
            "tasks_skipped": self._counts["skipped"],
            "tasks_changed": self._counts["changed"],
            "tasks_unreachable": self._counts["unreachable"],
            "task_count": len(self._task_timings),
            "hosts": hosts,
            "slowest_tasks": [
                {"name": n, "duration_ns": d} for n, d in slowest
            ],
            "ansible_version": "",
        }

        out = Path(DEPLOY_EVENT_DIR)
        out.mkdir(parents=True, exist_ok=True)
        artifact = out / f"{self._deploy_id}.json"
        artifact.write_text(json.dumps(event) + "\n")
        self._display.display(
            f"deploy_events: wrote {artifact}", color="cyan"
        )
