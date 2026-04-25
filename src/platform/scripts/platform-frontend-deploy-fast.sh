#!/usr/bin/env bash
# Fast path for shipping platform docs UI changes: build locally, rsync
# .output/, restart the service. No Ansible, no env/systemd/nftables rechecks.
#
# Safe when you've only touched:
#   - src/viteplus-monorepo/apps/platform/src/**
#   - src/viteplus-monorepo/packages/**
#
# NOT safe (run the Ansible role via `ansible-playbook ... --tags platform`):
#   - platform.service.j2 or env.j2 templates changed
#   - nftables rules changed
#   - First-ever deploy (needs systemd unit + env + DNS + Caddy block)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${SCRIPT_DIR}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

REMOTE="${VERIFICATION_REMOTE_USER}@${VERIFICATION_REMOTE_HOST}"
MONOREPO_DIR="${VERIFICATION_REPO_ROOT}/src/viteplus-monorepo"
LOCAL_OUTPUT="${MONOREPO_DIR}/apps/platform/.output/"
REMOTE_OUTPUT="/opt/platform/apps/platform/.output/"
SERVICE_HOST="127.0.0.1"
SERVICE_PORT="4249"

step() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }

step "Build @verself/platform (local, dependency-aware via vp)"
pushd "${MONOREPO_DIR}" >/dev/null
vp run "@verself/platform#build"
popd >/dev/null

if [[ ! -d "${LOCAL_OUTPUT}" ]]; then
  echo "build did not produce ${LOCAL_OUTPUT}" >&2
  exit 1
fi

step "Rsync .output/ -> ${REMOTE}:${REMOTE_OUTPUT}"
rsync -az --delete --inplace \
  -e "ssh ${VERIFICATION_SSH_OPTS[*]}" \
  "${LOCAL_OUTPUT}" \
  "${REMOTE}:${REMOTE_OUTPUT}"

step "Restart platform"
verification_ssh sudo systemctl restart platform

step "Wait for readiness on ${SERVICE_HOST}:${SERVICE_PORT}"
verification_ssh bash -s <<EOF
for attempt in \$(seq 1 60); do
  if curl -fsS -o /dev/null "http://${SERVICE_HOST}:${SERVICE_PORT}/"; then
    exit 0
  fi
  sleep 0.25
done
echo "platform did not become ready on ${SERVICE_HOST}:${SERVICE_PORT}" >&2
exit 1
EOF

step "Done"
