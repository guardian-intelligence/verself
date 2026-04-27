#!/usr/bin/python

from __future__ import annotations

import os
import re
import subprocess

from ansible.module_utils.basic import AnsibleModule


RESULT_RE = re.compile(r"migrations ok: applied=(?P<applied>[0-9]+) skipped=(?P<skipped>[0-9]+)")


def main() -> None:
    module = AnsibleModule(
        argument_spec={
            "command": {"type": "path", "required": True},
            "args": {"type": "list", "elements": "str", "default": []},
            "environment": {"type": "dict", "default": {}, "no_log": True},
        },
        supports_check_mode=False,
    )

    argv = [module.params["command"], *module.params["args"]]
    env = os.environ.copy()
    env.update({str(k): str(v) for k, v in module.params["environment"].items()})

    proc = subprocess.run(argv, env=env, text=True, capture_output=True, check=False)
    if proc.returncode != 0:
        module.fail_json(
            msg="service migrator failed",
            rc=proc.returncode,
            stdout=proc.stdout,
            stderr=proc.stderr,
            cmd=argv,
        )

    match = RESULT_RE.search(proc.stdout)
    if match is None:
        module.fail_json(
            msg="service migrator did not emit the required result contract",
            stdout=proc.stdout,
            stderr=proc.stderr,
            cmd=argv,
        )

    applied = int(match.group("applied"))
    skipped = int(match.group("skipped"))
    module.exit_json(
        changed=applied > 0,
        applied=applied,
        # Ansible reserves the result key "skipped" for task control flow.
        skipped_migrations=skipped,
        stdout=proc.stdout,
        stderr=proc.stderr,
        cmd=argv,
    )


if __name__ == "__main__":
    main()
