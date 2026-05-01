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

exec "${SCRIPT_DIR}/with-otel-agent.sh" ansible-playbook -i "${inventory}" "$@"
