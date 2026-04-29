#!/usr/bin/env bash
# Run a command with a controller-side OTLP buffer agent up.
#
# The agent (otelcol-contrib, configured by ../controller-agent/otelcol.yaml)
# binds 127.0.0.1:14317 and forwards spans over an SSH `-L` to the
# bare-metal node's 4317. Its lifetime is bound to *this* script — when
# the wrapped command exits we sleep briefly so the in-memory batch can
# drain into the agent's sending queue, then SIGTERM the agent with up
# to 90s for graceful shutdown, and only then close the SSH tunnel.
#
# That ordering — flush first, ssh closes second — is what the prior
# `ansible-with-tunnel.sh` got wrong: ansible's BSP atexit flush raced
# with the trap-EXIT ssh kill, dropping ~80 ansible.task spans on every
# failed deploy. The file_storage-backed sending queue at
# ${XDG_CACHE_HOME:-~/.cache}/verself/otelcol-controller/<site>/
# additionally survives a SIGKILL of the agent itself; the next
# invocation drains whatever the previous one couldn't.
#
# Required env:
#   VERSELF_ANSIBLE_INVENTORY  Inventory path (file or dir). Used to
#                              resolve ansible_host/ansible_user for SSH.
#
# Optional env:
#   VERSELF_SITE               Site name; namespaces the persistent
#                              queue dir. Defaults to "default".
#
# Sets for the wrapped command:
#   VERSELF_OTLP_ENDPOINT             127.0.0.1:14317
#   OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_RESOURCE_ATTRIBUTES, TRACEPARENT,
#   VERSELF_DEPLOY_RUN_KEY, VERSELF_DEPLOY_ID, ...
#                                    (via deploy_identity.sh; idempotent
#                                    when the parent already sourced it).
#
# Usage:
#   VERSELF_ANSIBLE_INVENTORY=/abs/path/to/inventory \
#     scripts/with-otel-agent.sh <command> [args...]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

if ! command -v otelcol-contrib >/dev/null 2>&1; then
  echo "ERROR: otelcol-contrib not on PATH. Run: aspect platform setup-dev" >&2
  exit 1
fi

inventory="${VERSELF_ANSIBLE_INVENTORY:-}"
if [[ -z "${inventory}" ]]; then
  echo "ERROR: VERSELF_ANSIBLE_INVENTORY is required (set by aspect render or scripts/lib/site-cache.sh)." >&2
  exit 1
fi
if [[ ! -e "${inventory}" ]]; then
  echo "ERROR: inventory ${inventory} does not exist. Run: aspect render --site=<site>" >&2
  exit 1
fi
hosts_ini="${inventory}"
if [[ -d "${inventory}" ]]; then
  hosts_ini="${inventory%/}/hosts.ini"
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

# A parallel `aspect deploy` would collide on the agent's fixed receiver
# port (14317); we fail loud rather than racing for the disk queue.
if python3 -c "import socket; socket.create_connection(('127.0.0.1', 14317), 0.25).close()" 2>/dev/null; then
  echo "ERROR: 127.0.0.1:14317 is already bound. Another aspect deploy / canary is in flight." >&2
  exit 1
fi

forward_port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"

cache_root="${XDG_CACHE_HOME:-${HOME}/.cache}/verself/otelcol-controller"
data_dir="${cache_root}/${VERSELF_SITE:-default}"
mkdir -p "${data_dir}"

agent_pid=""
tunnel_pid=""
cleanup() {
  if [[ -n "${agent_pid}" ]] && kill -0 "${agent_pid}" 2>/dev/null; then
    kill -TERM "${agent_pid}" 2>/dev/null || true
    # Drain budget: otelcol's per-pipeline graceful shutdown waits for
    # in-flight gRPC requests, then the file_storage-backed sending queue
    # tries to push remaining items through the otlp exporter (which can
    # retry over the SSH tunnel). 90s is comfortably above the worst
    # observed full-deploy span batch we've measured.
    for _ in $(seq 1 180); do
      kill -0 "${agent_pid}" 2>/dev/null || break
      sleep 0.5
    done
    kill -KILL "${agent_pid}" 2>/dev/null || true
    wait "${agent_pid}" 2>/dev/null || true
  fi
  if [[ -n "${tunnel_pid}" ]] && kill -0 "${tunnel_pid}" 2>/dev/null; then
    kill -TERM "${tunnel_pid}" 2>/dev/null || true
    wait "${tunnel_pid}" 2>/dev/null || true
  fi
}
trap cleanup EXIT

ssh -N \
  -o ExitOnForwardFailure=yes \
  -o ServerAliveInterval=15 \
  -o ServerAliveCountMax=3 \
  -o StrictHostKeyChecking=no \
  -L "${forward_port}:127.0.0.1:4317" \
  "${remote_user}@${remote_host}" </dev/null >/dev/null 2>&1 &
tunnel_pid=$!

for _ in $(seq 1 20); do
  if python3 -c "import socket; socket.create_connection(('127.0.0.1', ${forward_port}), 1).close()" 2>/dev/null; then
    break
  fi
  sleep 0.25
done
if ! python3 -c "import socket; socket.create_connection(('127.0.0.1', ${forward_port}), 1).close()" 2>/dev/null; then
  echo "ERROR: SSH forward to ${remote_user}@${remote_host} did not come up on 127.0.0.1:${forward_port}." >&2
  exit 1
fi

config="${SCRIPT_DIR}/../controller-agent/otelcol.yaml"
if [[ ! -f "${config}" ]]; then
  echo "ERROR: controller agent config not found at ${config}." >&2
  exit 1
fi

: >"${data_dir}/agent.log"
VERSELF_AGENT_FORWARD_ENDPOINT="127.0.0.1:${forward_port}" \
VERSELF_AGENT_DATA_DIR="${data_dir}" \
  otelcol-contrib --config "${config}" >"${data_dir}/agent.log" 2>&1 &
agent_pid=$!

for _ in $(seq 1 40); do
  if python3 -c "import socket; socket.create_connection(('127.0.0.1', 14317), 1).close()" 2>/dev/null; then
    break
  fi
  sleep 0.25
done
if ! python3 -c "import socket; socket.create_connection(('127.0.0.1', 14317), 1).close()" 2>/dev/null; then
  echo "ERROR: controller-side otelcol agent did not bind 127.0.0.1:14317. Check ${data_dir} for state." >&2
  exit 1
fi

export VERSELF_OTLP_ENDPOINT="127.0.0.1:14317"

# Idempotent on existing run-key / trace-id; only OTEL_EXPORTER_OTLP_ENDPOINT
# is refreshed to point at the local agent. Re-sourcing inside this
# script is the standard pattern when the parent (aspect deploy) already
# exported the identity env.
# shellcheck source=src/platform/scripts/deploy_identity.sh
source "${SCRIPT_DIR}/deploy_identity.sh"

# Run the wrapped command as a child so the EXIT trap (which drains the
# agent) fires after it returns. `exec "$@"` would replace this shell
# and skip the trap.
exit_code=0
"$@" || exit_code=$?

# Brief grace before SIGTERM so the agent's sending_queue can flush the
# ansible BSP atexit burst into the upstream tunnel. The queue persists
# anything still in flight (next agent run drains it), but a 5s window
# shifts the typical case from "spans visible after the next deploy" to
# "spans visible on the current run".
sleep 5

exit "${exit_code}"
