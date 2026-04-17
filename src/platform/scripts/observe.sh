#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
platform_root="$(cd "${script_dir}/.." && pwd)"
repo_root="$(cd "${platform_root}/../.." && pwd)"
inventory="${INVENTORY:-${platform_root}/ansible/inventory/hosts.ini}"

if [[ ! -f "${inventory}" ]]; then
  echo "ERROR: ${inventory} not found. Run 'make provision' first." >&2
  exit 1
fi

remote_host="$(grep -m1 'ansible_host=' "${inventory}" | sed 's/.*ansible_host=\([^ ]*\).*/\1/')"
remote_user="$(grep -m1 'ansible_user=' "${inventory}" | sed 's/.*ansible_user=\([^ ]*\).*/\1/')"
if [[ -z "${remote_host}" || -z "${remote_user}" ]]; then
  echo "ERROR: could not resolve ansible_host/ansible_user from ${inventory}." >&2
  exit 1
fi

ssh_opts=(-o IPQoS=none -o ExitOnForwardFailure=yes -o ServerAliveInterval=15 -o ServerAliveCountMax=3 -o StrictHostKeyChecking=no)
if [[ -n "${SSH_OPTS:-}" ]]; then
  read -r -a ssh_opts <<<"${SSH_OPTS}"
fi

port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"
ssh -N \
  "${ssh_opts[@]}" \
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

export FORGE_METAL_OTLP_ENDPOINT="127.0.0.1:${port}"
export FORGE_METAL_DEPLOY_KIND="${FORGE_METAL_DEPLOY_KIND:-observe}"

# Derive stable correlation for the observe command's OTel spans.
# shellcheck source=src/platform/scripts/deploy_identity.sh
source "${script_dir}/deploy_identity.sh"
export FM_OBSERVE_RUN_ID="${FM_OBSERVE_RUN_ID:-${FORGE_METAL_DEPLOY_RUN_KEY}}"

cd "${repo_root}"
exec go run ./src/otel/cmd/observe --platform-root "${platform_root}" "$@"
