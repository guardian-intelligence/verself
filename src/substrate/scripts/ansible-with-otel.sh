#!/usr/bin/env bash
# Wrap ansible-playbook so the upstream community.general.opentelemetry
# callback ships ansible.playbook/task spans through the controller-side
# OTLP buffer agent (scripts/with-otel-agent.sh). The agent's
# file_storage queue decouples flush from this script's exit, fixing the
# prior race where ansible's BSP atexit flush lost spans to a same-trap
# SSH-tunnel kill.
#
# The deploy cache layout (.cache/render/<site>/) is supplied by the
# caller via VERSELF_ANSIBLE_INVENTORY; this script does not invent it.
#
# Usage:
#   VERSELF_ANSIBLE_INVENTORY=/abs/path/to/inventory \
#     scripts/ansible-with-otel.sh <playbook> [extra ansible-playbook args]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "${SCRIPT_DIR}/../ansible"

inventory="${VERSELF_ANSIBLE_INVENTORY:-}"
if [[ -z "${inventory}" ]]; then
  echo "ERROR: VERSELF_ANSIBLE_INVENTORY is required (set by aspect render)." >&2
  exit 1
fi

changed_tasks_file="${VERSELF_SUBSTRATE_CHANGED_TASKS_FILE:-}"
if [[ -z "${changed_tasks_file}" ]]; then
  exec "${SCRIPT_DIR}/with-otel-agent.sh" ansible-playbook -i "${inventory}" "$@"
fi

mkdir -p "$(dirname "${changed_tasks_file}")"
log_file="${changed_tasks_file}.ansible.log.$$"
rm -f "${changed_tasks_file}" "${log_file}"

set +e
"${SCRIPT_DIR}/with-otel-agent.sh" ansible-playbook -i "${inventory}" "$@" 2>&1 | tee "${log_file}"
ansible_rc=${PIPESTATUS[0]}
set -e

python3 - "${log_file}" "${changed_tasks_file}" "${ansible_rc}" <<'PY'
import os
import re
import sys

log_path = sys.argv[1]
out_path = sys.argv[2]
ansible_rc = int(sys.argv[3])

with open(log_path, encoding="utf-8", errors="replace") as f:
    text = f.read()

text = re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", text)
recap = text.rsplit("PLAY RECAP", 1)[1] if "PLAY RECAP" in text else ""
counts = [int(match.group(1)) for match in re.finditer(r"(?<![A-Za-z0-9_])changed=(\d+)", recap)]

if not counts and ansible_rc == 0:
    print("ERROR: could not parse Ansible changed task count from PLAY RECAP", file=sys.stderr)
    sys.exit(1)

tmp_path = out_path + ".tmp"
with open(tmp_path, "w", encoding="utf-8") as f:
    f.write(str(sum(counts)) if counts else "0")
    f.write("\n")
os.replace(tmp_path, out_path)
PY
parse_rc=$?
rm -f "${log_file}"
if [[ "${parse_rc}" -ne 0 ]]; then
  exit "${parse_rc}"
fi

exit "${ansible_rc}"
