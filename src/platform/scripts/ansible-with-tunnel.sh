#!/usr/bin/env bash
# Wrap ansible-playbook so the deploy_traces callback can ship
# ansible.playbook/play/task spans to ClickHouse on every run, not just the
# `make telemetry-proof` smoke.
#
# Problem: deploy_traces exports via OTLP gRPC to 127.0.0.1:4317. The
# controller (this laptop) has no otelcol; otelcol only runs on the target.
# Without an override, the callback detects the unreachable endpoint and
# silently disables export.
#
# Fix: open an SSH local-forward to the target's 4317, export
# FORGE_METAL_OTLP_ENDPOINT=127.0.0.1:<tunnel-port>, then exec
# ansible-playbook. The callback reads the env var, exports to the forwarded
# port, spans land on the target's otelcol, which writes to ClickHouse like
# any other OTel client.
#
# Usage:
#   scripts/ansible-with-tunnel.sh <playbook> [extra ansible-playbook args]
# Example (via Makefile):
#   make deploy TAGS=billing_service
set -euo pipefail

cd "$(dirname "$0")/.."

inventory="ansible/inventory/hosts.ini"
if [[ ! -f "${inventory}" ]]; then
  echo "ERROR: ${inventory} not found. Provision the environment first." >&2
  exit 1
fi

# First host line that carries an ansible_host=.
remote_host="$(grep -m1 'ansible_host=' "${inventory}" | sed 's/.*ansible_host=\([^ ]*\).*/\1/')"
remote_user="$(grep -m1 'ansible_user=' "${inventory}" | sed 's/.*ansible_user=\([^ ]*\).*/\1/')"
if [[ -z "${remote_host}" || -z "${remote_user}" ]]; then
  echo "ERROR: could not resolve ansible_host/ansible_user from ${inventory}." >&2
  exit 1
fi

# Pick a free local port for the tunnel.
port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"

# Tunnel runs as a background child. Redirect its FDs to /dev/null so bash
# doesn't set the shell's stdin/stdout/stderr to non-blocking mode for the
# eventual ansible-playbook exec — ansible refuses to run with non-blocking
# standard streams.
ssh -N \
    -o ExitOnForwardFailure=yes \
    -o ServerAliveInterval=15 \
    -o ServerAliveCountMax=3 \
    -o StrictHostKeyChecking=no \
    -L "${port}:127.0.0.1:4317" \
    "${remote_user}@${remote_host}" </dev/null >/dev/null 2>&1 &
tunnel_pid=$!
trap 'kill "${tunnel_pid}" 2>/dev/null || true; wait "${tunnel_pid}" 2>/dev/null || true' EXIT

# Wait for the tunnel to come up (~5s cap).
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

export FORGE_METAL_OTLP_ENDPOINT="127.0.0.1:${port}"

cd ansible
exec ansible-playbook "$@"
