#!/usr/bin/python
"""Idempotently reconcile one systemd unit from content hashes."""

from __future__ import annotations

import hashlib
import json
import os
import re
import subprocess
import tempfile
from pathlib import Path
from typing import Any

from ansible.module_utils.basic import AnsibleModule


DOCUMENTATION = r"""
---
module: verself_systemd_converge
short_description: Reconcile a systemd unit after its content inputs are current
description:
  - Hashes the rendered unit, executable artifact, and loaded credentials.
  - Enables and starts or restarts the unit only when the observed runtime
    inputs differ from the last successful convergence marker, or when the
    unit is inactive.
  - This keeps synchronization inside one idempotent module so deploy tasks do
    not rely on handler flushing between unit projection and readiness probes.
options:
  unit:
    description:
      - Systemd unit name, with or without C(.service).
    required: true
    type: str
    aliases: [name]
  unit_file:
    description:
      - Rendered unit file path.
    required: false
    type: path
  artifact_path:
    description:
      - Executable artifact path whose digest should trigger a restart.
    required: false
    type: path
  watched_paths:
    description:
      - Additional files whose digest should trigger a restart.
    required: false
    type: list
    elements: path
  enabled:
    description:
      - Whether the unit should be enabled.
    required: false
    type: bool
    default: true
  state_dir:
    description:
      - Directory for convergence digest markers.
    required: false
    type: path
    default: /var/lib/verself/systemd-units
"""

RETURN = r"""
active:
  description: Whether ActiveState is active after convergence.
  returned: always
  type: bool
digest:
  description: Desired content digest contract recorded for the unit.
  returned: always
  type: dict
previous_digest:
  description: Previous recorded digest contract, if any.
  returned: always
  type: dict
restarted:
  description: Whether the unit was restarted.
  returned: always
  type: bool
started:
  description: Whether the unit was started without restart.
  returned: always
  type: bool
enabled_changed:
  description: Whether systemctl enable changed the unit file state.
  returned: always
  type: bool
"""


UNIT_RE = re.compile(r"^[A-Za-z0-9_.@:-]+(?:\.service)?$")


def run_systemctl(args: list[str]) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["systemctl", *args],
        check=False,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )


def systemd_show(unit: str) -> dict[str, str]:
    completed = run_systemctl(
        [
            "show",
            "--no-pager",
            "--property=LoadState",
            "--property=ActiveState",
            "--property=UnitFileState",
            "--property=FragmentPath",
            unit,
        ]
    )
    if completed.returncode != 0:
        raise RuntimeError(completed.stderr.strip() or completed.stdout.strip())
    status: dict[str, str] = {}
    for line in completed.stdout.splitlines():
        if "=" in line:
            key, value = line.split("=", 1)
            status[key] = value
    return status


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def read_marker(path: Path) -> dict[str, Any]:
    try:
        with path.open("r", encoding="utf-8") as handle:
            payload = json.load(handle)
    except FileNotFoundError:
        return {}
    if not isinstance(payload, dict):
        return {}
    return payload


def write_marker(path: Path, payload: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
    os.chmod(path.parent, 0o700)
    fd, tmp_name = tempfile.mkstemp(prefix=path.name + ".", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            json.dump(payload, handle, sort_keys=True, separators=(",", ":"))
            handle.write("\n")
        os.chmod(tmp_name, 0o600)
        os.replace(tmp_name, path)
    except Exception:
        try:
            os.unlink(tmp_name)
        except FileNotFoundError:
            pass
        raise


def fail_completed(module: AnsibleModule, msg: str, completed: subprocess.CompletedProcess[str]) -> None:
    module.fail_json(
        msg=msg,
        cmd=completed.args,
        rc=completed.returncode,
        stdout=completed.stdout,
        stderr=completed.stderr,
    )


def main() -> None:
    module = AnsibleModule(
        argument_spec={
            "unit": {"type": "str", "required": True, "aliases": ["name"]},
            "unit_file": {"type": "path", "required": False},
            "artifact_path": {"type": "path", "required": False},
            "watched_paths": {"type": "list", "elements": "path", "default": []},
            "enabled": {"type": "bool", "default": True},
            "state_dir": {"type": "path", "default": "/var/lib/verself/systemd-units"},
        },
        supports_check_mode=True,
    )

    unit = module.params["unit"].strip()
    if not UNIT_RE.match(unit):
        module.fail_json(msg="unit must match ^[A-Za-z0-9_.@:-]+(?:[.]service)?$")
    unit_name = unit if unit.endswith(".service") else unit + ".service"
    unit_file = Path(module.params["unit_file"] or f"/etc/systemd/system/{unit_name}")
    artifact_path = module.params.get("artifact_path")
    watched_paths = [Path(path) for path in module.params["watched_paths"]]
    state_dir = Path(module.params["state_dir"])
    marker_path = state_dir / (unit_name.replace("/", "_") + ".json")
    if not module.check_mode:
        state_dir.mkdir(parents=True, mode=0o700, exist_ok=True)
        os.chmod(state_dir, 0o700)
        if marker_path.exists():
            os.chmod(marker_path, 0o600)

    if not unit_file.is_file():
        module.fail_json(msg=f"unit file {unit_file} does not exist")

    digest: dict[str, Any] = {
        "schema_version": 1,
        "unit": unit_name,
        "unit_file": str(unit_file),
        "unit_sha256": sha256_file(unit_file),
        "artifact_path": "",
        "artifact_sha256": "",
        "watched_paths": {},
    }
    if artifact_path:
        artifact = Path(artifact_path)
        if not artifact.is_file():
            module.fail_json(msg=f"artifact_path {artifact} does not exist")
        digest["artifact_path"] = str(artifact)
        digest["artifact_sha256"] = sha256_file(artifact)

    watched_digests: dict[str, str] = {}
    for watched_path in watched_paths:
        if not watched_path.is_file():
            module.fail_json(msg=f"watched path {watched_path} does not exist")
        watched_digests[str(watched_path)] = sha256_file(watched_path)
    digest["watched_paths"] = watched_digests

    previous = read_marker(marker_path)
    content_changed = previous != digest

    try:
        before = systemd_show(unit_name)
    except RuntimeError as exc:
        module.fail_json(msg=f"systemctl show failed for {unit_name}: {exc}")

    active_before = before.get("ActiveState") == "active"
    enabled_changed = False
    restarted = False
    started = False
    changed = False

    if module.params["enabled"] and before.get("UnitFileState") != "enabled":
        changed = True
        enabled_changed = True
        if not module.check_mode:
            completed = run_systemctl(["enable", unit_name])
            if completed.returncode != 0:
                fail_completed(module, f"systemctl enable failed for {unit_name}", completed)

    if content_changed:
        changed = True
        if not module.check_mode:
            completed = run_systemctl(["daemon-reload"])
            if completed.returncode != 0:
                fail_completed(module, "systemctl daemon-reload failed", completed)
            completed = run_systemctl(["restart", unit_name])
            if completed.returncode != 0:
                fail_completed(module, f"systemctl restart failed for {unit_name}", completed)
            restarted = True
    elif not active_before:
        changed = True
        if not module.check_mode:
            completed = run_systemctl(["start", unit_name])
            if completed.returncode != 0:
                fail_completed(module, f"systemctl start failed for {unit_name}", completed)
            started = True

    after = before
    if not module.check_mode:
        try:
            after = systemd_show(unit_name)
        except RuntimeError as exc:
            module.fail_json(msg=f"systemctl show after convergence failed for {unit_name}: {exc}")
        if after.get("ActiveState") != "active":
            module.fail_json(
                msg=f"{unit_name} is {after.get('ActiveState', 'unknown')} after convergence",
                unit=unit_name,
                status=after,
                restarted=restarted,
                started=started,
                content_changed=content_changed,
            )
        if changed:
            write_marker(marker_path, digest)

    module.exit_json(
        changed=changed,
        unit=unit_name,
        active=after.get("ActiveState") == "active",
        status=after,
        digest_summary={
            "schema_version": digest["schema_version"],
            "unit": unit_name,
            "artifact_path": digest["artifact_path"],
            "watched_path_count": len(watched_digests),
            "previous_marker_present": bool(previous),
        },
        marker=str(marker_path),
        enabled_changed=enabled_changed,
        restarted=restarted,
        started=started,
        content_changed=content_changed,
    )


if __name__ == "__main__":
    main()
