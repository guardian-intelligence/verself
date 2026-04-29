#!/usr/bin/env bash
# Wrap ansible-playbook so the upstream community.general.opentelemetry
# callback can ship ansible.playbook/task spans over OTLP to the node's
# otelcol, which writes them to ClickHouse.
#
# Problem: the callback exports via OTLP gRPC. The controller (this laptop)
# has no otelcol — otelcol only runs on the target. Without an override the
# callback's export fails.
#
# Fix: open an SSH local-forward to the target's OTLP gRPC (4317), set
# VERSELF_OTLP_ENDPOINT, source scripts/deploy_identity.sh (idempotent if
# the parent already sourced it via .aspect/lib/helpers.axl::derive_deploy_env),
# then exec ansible-playbook.
#
# The deploy cache layout (.cache/render/<site>/) is supplied by the caller
# via VERSELF_ANSIBLE_INVENTORY; this script does not invent it. Failing
# loud when the env var is unset is intentional — there is no useful
# default for "which site" from this layer.
#
# Usage:
#   VERSELF_ANSIBLE_INVENTORY=/abs/path/to/inventory \
#     scripts/ansible-with-tunnel.sh <playbook> [extra ansible-playbook args]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "${SCRIPT_DIR}/.."

inventory="${VERSELF_ANSIBLE_INVENTORY:-}"
if [[ -z "${inventory}" ]]; then
  echo "ERROR: VERSELF_ANSIBLE_INVENTORY is required (set by aspect render)." >&2
  exit 1
fi
if [[ ! -e "${inventory}" ]]; then
  echo "ERROR: inventory ${inventory} does not exist. Run: aspect render --site=<site>" >&2
  exit 1
fi

# Inventory may be a directory (-i <dir> auto-loads inventory_dir/group_vars/)
# or a single file. SSH tunnel target lives in a hosts.ini at the same path
# (file mode) or beside it (directory mode).
if [[ -d "${inventory}" ]]; then
  hosts_ini="${inventory%/}/hosts.ini"
else
  hosts_ini="${inventory}"
fi
if [[ ! -f "${hosts_ini}" ]]; then
  echo "ERROR: hosts.ini not found at ${hosts_ini}." >&2
  exit 1
fi

remote_host="$(grep -m1 'ansible_host=' "${hosts_ini}" | sed 's/.*ansible_host=\([^ ]*\).*/\1/')"
remote_user="$(grep -m1 'ansible_user=' "${hosts_ini}" | sed 's/.*ansible_user=\([^ ]*\).*/\1/')"
if [[ -z "${remote_host}" || -z "${remote_user}" ]]; then
  echo "ERROR: could not resolve ansible_host/ansible_user from ${hosts_ini}." >&2
  exit 1
fi

otlp_port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"

# Tunnel as a background child. Redirect FDs so bash doesn't flip stdin/out
# non-blocking for the eventual exec — ansible refuses non-blocking streams.
ssh -N \
    -o ExitOnForwardFailure=yes \
    -o ServerAliveInterval=15 \
    -o ServerAliveCountMax=3 \
    -o StrictHostKeyChecking=no \
    -L "${otlp_port}:127.0.0.1:4317" \
    "${remote_user}@${remote_host}" </dev/null >/dev/null 2>&1 &
tunnel_pid=$!
trap 'kill "${tunnel_pid}" 2>/dev/null || true; wait "${tunnel_pid}" 2>/dev/null || true' EXIT

for _ in $(seq 1 20); do
  if python3 -c "import socket; socket.create_connection(('127.0.0.1', ${otlp_port}), 1).close()" 2>/dev/null; then
    break
  fi
  sleep 0.25
done
if ! python3 -c "import socket; socket.create_connection(('127.0.0.1', ${otlp_port}), 1).close()" 2>/dev/null; then
  echo "ERROR: OTLP tunnel to ${remote_user}@${remote_host} did not come up on 127.0.0.1:${otlp_port}." >&2
  exit 1
fi

export VERSELF_OTLP_ENDPOINT="127.0.0.1:${otlp_port}"

# Derive deploy identity + OTel env. deploy_identity.sh is idempotent on
# VERSELF_DEPLOY_RUN_KEY / VERSELF_DEPLOY_ID, so when this script is invoked
# from `aspect deploy` (which has already sourced the script via
# derive_deploy_env), the run-key and trace-id are preserved here and only
# OTEL_EXPORTER_OTLP_ENDPOINT is refreshed to point at the local tunnel.
# shellcheck source=src/platform/scripts/deploy_identity.sh
source "${SCRIPT_DIR}/deploy_identity.sh"

cd ansible
exec ansible-playbook -i "${inventory}" "$@"
