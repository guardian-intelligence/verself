#!/usr/bin/env bash
# Wrap ansible-playbook so the upstream community.general.opentelemetry
# callback can ship ansible.playbook/task spans over OTLP to the node's
# otelcol, which writes them to ClickHouse.
#
# Problem: the callback exports via OTLP gRPC. The controller (this laptop)
# has no otelcol — otelcol only runs on the target. Without an override the
# callback's export fails.
#
# Fix: open an SSH local-forward to the target's 4317, set
# VERSELF_OTLP_ENDPOINT, source scripts/deploy_identity.sh (which
# derives VERSELF_DEPLOY_ID, TRACEPARENT, OTEL_SERVICE_NAME,
# OTEL_RESOURCE_ATTRIBUTES, OTEL_EXPORTER_OTLP_ENDPOINT), then exec
# ansible-playbook.
#
# Usage:
#   scripts/ansible-with-tunnel.sh <playbook> [extra ansible-playbook args]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "${SCRIPT_DIR}/.."

inventory="ansible/inventory/hosts.ini"
if [[ ! -f "${inventory}" ]]; then
  echo "ERROR: ${inventory} not found. Provision the environment first." >&2
  exit 1
fi

remote_host="$(grep -m1 'ansible_host=' "${inventory}" | sed 's/.*ansible_host=\([^ ]*\).*/\1/')"
remote_user="$(grep -m1 'ansible_user=' "${inventory}" | sed 's/.*ansible_user=\([^ ]*\).*/\1/')"
if [[ -z "${remote_host}" || -z "${remote_user}" ]]; then
  echo "ERROR: could not resolve ansible_host/ansible_user from ${inventory}." >&2
  exit 1
fi

port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"

# Tunnel as a background child. Redirect FDs so bash doesn't flip stdin/out
# non-blocking for the eventual exec — ansible refuses non-blocking streams.
ssh -N \
    -o ExitOnForwardFailure=yes \
    -o ServerAliveInterval=15 \
    -o ServerAliveCountMax=3 \
    -o StrictHostKeyChecking=no \
    -L "${port}:127.0.0.1:4317" \
    "${remote_user}@${remote_host}" </dev/null >/dev/null 2>&1 &
tunnel_pid=$!
trap 'kill "${tunnel_pid}" 2>/dev/null || true; wait "${tunnel_pid}" 2>/dev/null || true' EXIT

for _ in $(seq 1 20); do
  if python3 -c "import socket; socket.create_connection(('127.0.0.1', ${port}), 1).close()" 2>/dev/null; then
    break
  fi
  sleep 0.25
done
if ! python3 -c "import socket; socket.create_connection(('127.0.0.1', ${port}), 1).close()" 2>/dev/null; then
  echo "ERROR: OTLP tunnel to ${remote_user}@${remote_host} did not come up on 127.0.0.1:${port}." >&2
  exit 1
fi

export VERSELF_OTLP_ENDPOINT="127.0.0.1:${port}"

# Derive deploy identity + OTel env. Sourcing sets VERSELF_DEPLOY_ID,
# TRACEPARENT, OTEL_RESOURCE_ATTRIBUTES, OTEL_EXPORTER_OTLP_ENDPOINT, etc.
# shellcheck source=src/platform/scripts/deploy_identity.sh
source "${SCRIPT_DIR}/deploy_identity.sh"

cd ansible
exec ansible-playbook "$@"
