#!/usr/bin/python
"""Read-only systemd unit status probe for Verself playbooks."""

from __future__ import annotations

import re
import subprocess

from ansible.module_utils.basic import AnsibleModule


DOCUMENTATION = r"""
---
module: verself_systemd_status
short_description: Read systemd unit status without mutating service state
description:
  - Wraps C(systemctl show) for readiness and recovery probes.
  - Always returns C(changed=false).
  - Treats inactive and missing units as data, not task failure.
options:
  unit:
    description:
      - Unit name to inspect.
    required: true
    type: str
    aliases: [name]
  properties:
    description:
      - systemd properties to request.
    required: false
    type: list
    elements: str
"""

EXAMPLES = r"""
- name: Probe worker state
  verself_systemd_status:
    unit: sandbox-rental-recurring-worker.service
  register: worker_status
  until: worker_status.active
  retries: 30
  delay: 1
"""

RETURN = r"""
active:
  description: Whether ActiveState is active.
  returned: always
  type: bool
exists:
  description: Whether systemd knows the unit.
  returned: always
  type: bool
status:
  description: Raw requested systemd properties.
  returned: always
  type: dict
"""


DEFAULT_PROPERTIES = [
    "LoadState",
    "ActiveState",
    "SubState",
    "UnitFileState",
    "FragmentPath",
    "MainPID",
    "Result",
]
PROPERTY_RE = re.compile(r"^[A-Za-z][A-Za-z0-9]*$")


def parse_systemctl_show(stdout: str) -> dict[str, str]:
    status: dict[str, str] = {}
    for line in stdout.splitlines():
        if "=" not in line:
            continue
        key, value = line.split("=", 1)
        status[key] = value
    return status


def main() -> None:
    module = AnsibleModule(
        argument_spec={
            "unit": {"type": "str", "required": True, "aliases": ["name"]},
            "properties": {
                "type": "list",
                "elements": "str",
                "default": DEFAULT_PROPERTIES,
            },
        },
        supports_check_mode=True,
    )

    unit = module.params["unit"].strip()
    if not unit:
        module.fail_json(msg="verself_systemd_status requires a non-empty unit name")

    properties = [prop.strip() for prop in module.params["properties"]]
    invalid_properties = [prop for prop in properties if not PROPERTY_RE.match(prop)]
    if invalid_properties:
        module.fail_json(
            msg="systemd property names must match ^[A-Za-z][A-Za-z0-9]*$",
            invalid_properties=invalid_properties,
        )

    cmd = ["systemctl", "show", "--no-pager"]
    cmd.extend(f"--property={prop}" for prop in properties)
    cmd.append(unit)

    try:
        completed = subprocess.run(
            cmd,
            check=False,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
    except FileNotFoundError as exc:
        module.fail_json(msg="systemctl was not found", error=str(exc), changed=False)

    if completed.returncode != 0:
        module.fail_json(
            msg="systemctl show failed",
            cmd=cmd,
            rc=completed.returncode,
            stdout=completed.stdout,
            stderr=completed.stderr,
            changed=False,
        )

    status = parse_systemctl_show(completed.stdout)
    load_state = status.get("LoadState", "")
    active_state = status.get("ActiveState", "")

    module.exit_json(
        changed=False,
        cmd=cmd,
        rc=completed.returncode,
        stdout=completed.stdout,
        stderr=completed.stderr,
        unit=unit,
        status=status,
        exists=load_state not in {"", "not-found"},
        active=active_state == "active",
        load_state=load_state,
        active_state=active_state,
        sub_state=status.get("SubState", ""),
        unit_file_state=status.get("UnitFileState", ""),
    )


if __name__ == "__main__":
    main()
