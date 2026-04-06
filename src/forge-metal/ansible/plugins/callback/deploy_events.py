"""deploy_events — write one deploy event per ansible-playbook run to ClickHouse.

Self-inserting: after the playbook completes, the callback shells out to
scripts/clickhouse.sh (which handles SSH + SOPS password + clickhouse-client)
and pipes a single JSONEachRow directly. Falls back to writing a local JSON
file if the insert fails.

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
    short_description: Record deploy events in ClickHouse
    description:
        - Captures playbook timing, task counts, and git metadata.
        - Inserts directly into ClickHouse via scripts/clickhouse.sh.
        - Falls back to ~/.cache/forge-metal/deploy-events/ on failure.
    requirements:
        - No external dependencies (stdlib only)
"""

FALLBACK_DIR = os.environ.get(
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

    # -- helpers --

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

    @staticmethod
    def _repo_root():
        r = CallbackModule._git("rev-parse", "--show-toplevel")
        return Path(r) if r else None

    def _to_clickhouse_row(self, event):
        """Transform event dict to ClickHouse JSONEachRow format."""
        row = dict(event)
        row["ok"] = 1 if row["ok"] else 0
        row["dirty"] = 1 if row["dirty"] else 0
        row["hosts"] = json.dumps(row["hosts"])
        row["slowest_tasks"] = json.dumps(row["slowest_tasks"])
        return json.dumps(row)

    def _try_insert(self, row_json):
        """Attempt to insert via scripts/clickhouse.sh. Returns True on success."""
        root = self._repo_root()
        if not root:
            return False
        script = root / "scripts" / "clickhouse.sh"
        if not script.exists():
            return False
        try:
            subprocess.run(
                [
                    str(script),
                    "--database", "forge_metal",
                    "--query", "INSERT INTO deploy_events FORMAT JSONEachRow",
                ],
                input=(row_json + "\n").encode(),
                cwd=str(root),
                timeout=30,
                check=True,
                capture_output=True,
            )
            return True
        except Exception:
            return False

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

        hosts = {}
        for host in sorted(stats.processed):
            hosts[host] = stats.summarize(host)

        slowest = sorted(
            self._task_timings, key=lambda x: x[1], reverse=True
        )[:10]

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
            ).strftime("%Y-%m-%d %H:%M:%S.%f"),
            "completed_at": datetime.fromtimestamp(
                end_ns / 1e9, tz=timezone.utc
            ).strftime("%Y-%m-%d %H:%M:%S.%f"),
            "total_ns": end_ns - self._start_ns,
            "ok": self._counts["failed"] == 0
            and self._counts["unreachable"] == 0,
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

        row = self._to_clickhouse_row(event)

        # Write fallback file first so events survive insert failures.
        fallback = Path(FALLBACK_DIR)
        fallback.mkdir(parents=True, exist_ok=True)
        artifact = fallback / f"{self._deploy_id}.json"
        artifact.write_text(row + "\n")

        if self._try_insert(row):
            artifact.unlink(missing_ok=True)
            self._display.display(
                f"deploy_events: {self._deploy_id} inserted into ClickHouse",
                color="cyan",
            )
        else:
            self._display.display(
                f"deploy_events: insert failed, saved to {artifact}",
                color="yellow",
            )
